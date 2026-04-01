package timelapse

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sergeknystautas/schmux/internal/session"
)

// Recorder writes a continuous NDJSON recording of session output.
// It tails the OutputLog using WaitForNew and records output, gap,
// and resize events.
type Recorder struct {
	recordingID  string
	sessionID    string
	outputLog    *session.OutputLog
	gapCh        <-chan session.SourceEvent
	file         *os.File
	startTime    time.Time
	startWaitSeq uint64 // captured at construction to avoid missing entries
	lastSeq      uint64
	seenFirst    bool // true after processing at least one entry
	bytesWritten int64
	maxBytes     int64
	stopCh       chan struct{}
	doneCh       chan struct{}
}

// NewRecorder creates a new recorder for a session.
// The recording file is created in recordingDir with permissions 0600.
func NewRecorder(
	sessionID string,
	outputLog *session.OutputLog,
	gapCh <-chan session.SourceEvent,
	recordingDir string,
	maxBytes int64,
	width, height int,
) (*Recorder, error) {
	if err := os.MkdirAll(recordingDir, 0700); err != nil {
		return nil, fmt.Errorf("create recording dir: %w", err)
	}

	recordingID := fmt.Sprintf("%s-%d", sessionID, time.Now().Unix())
	filename := filepath.Join(recordingDir, recordingID+".jsonl")

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("create recording file: %w", err)
	}

	return &Recorder{
		recordingID:  recordingID,
		sessionID:    sessionID,
		outputLog:    outputLog,
		gapCh:        gapCh,
		file:         file,
		startTime:    time.Now(),
		startWaitSeq: outputLog.CurrentSeq(),
		maxBytes:     maxBytes,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}, nil
}

// RecordingID returns the unique recording identifier.
func (r *Recorder) RecordingID() string { return r.recordingID }

// Run is the main recording loop. It blocks until Stop is called
// or the size cap is reached.
func (r *Recorder) Run() {
	defer close(r.doneCh)
	defer r.file.Close()

	// Write header
	header := Record{
		Type:        RecordHeader,
		Version:     1,
		RecordingID: r.recordingID,
		SessionID:   r.sessionID,
		StartTime:   r.startTime.Format(time.RFC3339),
	}
	if err := r.writeRecord(header); err != nil {
		return
	}

	// waitSeq is captured at construction time (startWaitSeq) so that entries
	// appended between NewRecorder and Run() are not missed.
	waitSeq := r.startWaitSeq

	for {
		// Wait for new output
		if !r.outputLog.WaitForNew(waitSeq, r.stopCh) {
			r.writeEndRecord()
			return
		}

		// Determine replay start
		var replayFrom uint64
		if r.seenFirst {
			replayFrom = r.lastSeq + 1
		} else {
			replayFrom = r.outputLog.OldestSeq()
		}

		// Check for buffer overrun
		if r.seenFirst {
			oldestSeq := r.outputLog.OldestSeq()
			if oldestSeq > r.lastSeq+1 {
				t := r.elapsed()
				gapRec := Record{
					Type:     RecordGap,
					T:        floatPtr(t),
					Reason:   "buffer_overrun",
					LostSeqs: [2]uint64{r.lastSeq + 1, oldestSeq - 1},
				}
				r.writeRecord(gapRec)
				replayFrom = oldestSeq
			}
		}

		// Replay available entries
		entries := r.outputLog.ReplayFrom(replayFrom)
		if entries == nil {
			// Data was evicted; skip ahead to oldest available
			oldest := r.outputLog.OldestSeq()
			entries = r.outputLog.ReplayFrom(oldest)
		}

		for _, entry := range entries {
			t := r.elapsed()
			rec := Record{
				Type: RecordOutput,
				T:    floatPtr(t),
				Seq:  entry.Seq,
				D:    string(entry.Data),
			}
			if err := r.writeRecord(rec); err != nil {
				return
			}
			r.lastSeq = entry.Seq
			r.seenFirst = true
		}
		// Advance wait position to current high-water mark
		waitSeq = r.outputLog.CurrentSeq()

		// Drain gap/resize events (non-blocking)
		r.drainGapCh()

		// Check size cap
		if r.maxBytes > 0 && r.bytesWritten >= r.maxBytes {
			r.writeEndRecord()
			return
		}
	}
}

// Stop signals the recorder to stop and waits for it to finish.
func (r *Recorder) Stop() {
	close(r.stopCh)
	<-r.doneCh
}

func (r *Recorder) elapsed() float64 {
	return time.Since(r.startTime).Seconds()
}

func (r *Recorder) writeRecord(rec Record) error {
	n, err := r.file.Stat()
	if err != nil {
		return err
	}
	_ = n // stat for position tracking not needed; we track bytesWritten

	err = WriteRecord(r.file, rec)
	if err == nil {
		// Estimate bytes written (re-serialization is wasteful, use stat delta)
		after, _ := r.file.Stat()
		if after != nil {
			r.bytesWritten = after.Size()
		}
	}
	return err
}

func (r *Recorder) writeEndRecord() {
	end := Record{
		Type: RecordEnd,
		T:    floatPtr(r.elapsed()),
	}
	r.writeRecord(end)
}

func (r *Recorder) drainGapCh() {
	if r.gapCh == nil {
		return
	}
	for {
		select {
		case event := <-r.gapCh:
			t := r.elapsed()
			switch event.Type {
			case session.SourceGap:
				rec := Record{
					Type:   RecordGap,
					T:      floatPtr(t),
					Reason: event.Reason,
				}
				if event.Snapshot != "" {
					rec.Snapshot = &event.Snapshot
				}
				r.writeRecord(rec)
			case session.SourceResize:
				rec := Record{
					Type:   RecordResize,
					T:      floatPtr(t),
					Width:  event.Width,
					Height: event.Height,
				}
				r.writeRecord(rec)
			}
		default:
			return
		}
	}
}
