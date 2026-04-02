package timelapse

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/sergeknystautas/schmux/internal/session"
)

// Recorder writes a continuous asciicast v2 recording (.cast) of session output.
// It tails the OutputLog using WaitForNew and writes events directly in the
// asciicast format so recordings are immediately playable with asciinema.
type Recorder struct {
	recordingID  string
	sessionID    string
	outputLog    *session.OutputLog
	gapCh        <-chan session.SourceEvent
	file         *os.File
	startTime    time.Time
	startWaitSeq uint64 // captured at construction to avoid missing entries
	lastSeq      uint64
	seenFirst    bool   // true after processing at least one entry
	utf8Pending  []byte // buffered incomplete UTF-8 sequence from previous chunk
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
	filename := filepath.Join(recordingDir, recordingID+".cast")

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
		startWaitSeq: 0, // start from beginning to capture events that arrived before Run()
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

	// Write asciicast v2 header (includes sessionId as custom field for querying)
	header := fmt.Sprintf(`{"version":2,"width":80,"height":24,"timestamp":%d,"title":"%s","env":{"TERM":"xterm-256color"}}`,
		r.startTime.Unix(), r.sessionID)
	r.writeLine(header)

	waitSeq := r.startWaitSeq

	for {
		// Wait for new output
		if !r.outputLog.WaitForNew(waitSeq, r.stopCh) {
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
				replayFrom = oldestSeq
			}
		}

		// Replay available entries
		entries := r.outputLog.ReplayFrom(replayFrom)
		if entries == nil {
			oldest := r.outputLog.OldestSeq()
			entries = r.outputLog.ReplayFrom(oldest)
		}

		for _, entry := range entries {
			r.writeOutputEvent(entry.Data)
			r.lastSeq = entry.Seq
			r.seenFirst = true
		}
		waitSeq = r.outputLog.CurrentSeq()

		// Drain gap/resize events (non-blocking)
		r.drainGapCh()

		// Check size cap
		if r.maxBytes > 0 && r.bytesWritten >= r.maxBytes {
			return
		}
	}
}

// Stop signals the recorder to stop and waits for it to finish.
func (r *Recorder) Stop() {
	close(r.stopCh)
	<-r.doneCh
}

// writeOutputEvent writes an asciicast output event, buffering incomplete
// UTF-8 sequences to avoid splitting multi-byte characters across events.
func (r *Recorder) writeOutputEvent(data []byte) {
	// Prepend any pending bytes from the previous chunk
	if len(r.utf8Pending) > 0 {
		data = append(r.utf8Pending, data...)
		r.utf8Pending = nil
	}

	// Check for incomplete UTF-8 at the end
	if len(data) > 0 && !utf8.Valid(data) {
		// Find the last valid UTF-8 boundary
		trimmed := trimIncompleteUTF8(data)
		if len(trimmed) < len(data) {
			r.utf8Pending = make([]byte, len(data)-len(trimmed))
			copy(r.utf8Pending, data[len(trimmed):])
			data = trimmed
		}
	}

	if len(data) == 0 {
		return
	}

	t := r.elapsed()
	escaped := jsonEscapeBytes(data)
	line := fmt.Sprintf("[%.6f,\"o\",%s]", t, escaped)
	r.writeLine(line)
}

// writeResizeEvent writes an asciicast resize event (custom type "r").
func (r *Recorder) writeResizeEvent(width, height int) {
	t := r.elapsed()
	line := fmt.Sprintf("[%.6f,\"r\",\"%dx%d\"]", t, width, height)
	r.writeLine(line)
}

func (r *Recorder) writeLine(line string) {
	n, _ := fmt.Fprintln(r.file, line)
	r.bytesWritten += int64(n)
	r.file.Sync() // ensure data is visible to readers immediately
}

func (r *Recorder) elapsed() float64 {
	return time.Since(r.startTime).Seconds()
}

func (r *Recorder) drainGapCh() {
	if r.gapCh == nil {
		return
	}
	for {
		select {
		case event := <-r.gapCh:
			if event.Type == session.SourceResize {
				r.writeResizeEvent(event.Width, event.Height)
			}
		default:
			return
		}
	}
}

// trimIncompleteUTF8 returns data with any trailing incomplete UTF-8
// sequence removed. The removed bytes should be prepended to the next chunk.
func trimIncompleteUTF8(data []byte) []byte {
	if len(data) == 0 {
		return data
	}

	// Walk backwards from the end to find the start of the last rune
	for i := len(data) - 1; i >= 0 && i >= len(data)-4; i-- {
		b := data[i]

		if b < 0x80 {
			// ASCII byte — everything up to and including this is valid
			return data[:i+1]
		}

		if b&0xC0 == 0xC0 {
			// This is a leading byte — check if the sequence is complete
			var expectedLen int
			switch {
			case b&0xE0 == 0xC0:
				expectedLen = 2
			case b&0xF0 == 0xE0:
				expectedLen = 3
			case b&0xF8 == 0xF0:
				expectedLen = 4
			}
			remaining := len(data) - i
			if remaining < expectedLen {
				// Incomplete — trim from this byte
				return data[:i]
			}
			// Complete — all data is valid
			return data
		}
		// Continuation byte (10xxxxxx) — keep looking for the leading byte
	}

	return data
}
