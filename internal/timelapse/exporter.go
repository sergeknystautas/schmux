package timelapse

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

const fillerDuration = 0.3 // compressed filler intervals become 300ms

// Exporter converts a timelapse recording (.jsonl) to an asciicast v2 file (.cast).
// It uses a two-pass pipeline:
// Pass 1: Classify intervals (content vs filler) and collect keyframes
// Pass 2: Write compressed .cast file
type Exporter struct {
	recordingPath string
	outputPath    string
	progressFn    func(pct float64)
}

// NewExporter creates an exporter for a recording.
func NewExporter(recordingPath, outputPath string, progressFn func(float64)) *Exporter {
	return &Exporter{
		recordingPath: recordingPath,
		outputPath:    outputPath,
		progressFn:    progressFn,
	}
}

// Export runs the two-pass export pipeline.
func (e *Exporter) Export() error {
	// Read recording header for dimensions
	header, err := e.readHeader()
	if err != nil {
		return fmt.Errorf("read header: %w", err)
	}

	width, height := header.Width, header.Height
	if width == 0 {
		width = 80
	}
	if height == 0 {
		height = 24
	}

	// Pass 1: Classify intervals
	e.reportProgress(0.1)
	pass1Data, err := os.ReadFile(e.recordingPath)
	if err != nil {
		return fmt.Errorf("read recording: %w", err)
	}

	emu := NewScreenEmulator(width, height)
	intervals, err := ClassifyIntervals(bytes.NewReader(pass1Data), emu)
	if err != nil {
		return fmt.Errorf("classify intervals: %w", err)
	}

	e.reportProgress(0.4)

	// Pass 2: Write .cast file with compressed timestamps
	emu.Reset()
	outFile, err := os.OpenFile(e.outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer outFile.Close()

	// Calculate durations for header
	var originalDuration float64
	var compressedDuration float64
	if len(intervals) > 0 {
		originalDuration = intervals[len(intervals)-1].End
		for _, iv := range intervals {
			if iv.Type == Content {
				compressedDuration += iv.End - iv.Start
			} else {
				compressedDuration += fillerDuration
			}
		}
	}

	castHeader := CastHeader{
		Width:            width,
		Height:           height,
		Duration:         compressedDuration,
		Title:            header.RecordingID,
		OriginalDuration: originalDuration,
		CompressionRatio: func() float64 {
			if originalDuration > 0 {
				return compressedDuration / originalDuration
			}
			return 1
		}(),
		RecordingID: header.RecordingID,
	}

	cw, err := NewCastWriter(outFile, castHeader)
	if err != nil {
		return fmt.Errorf("create cast writer: %w", err)
	}

	e.reportProgress(0.5)

	// Replay recording and write events with compressed timestamps
	err = e.writeCompressedEvents(bytes.NewReader(pass1Data), cw, emu, intervals)
	if err != nil {
		return fmt.Errorf("write events: %w", err)
	}

	e.reportProgress(1.0)
	return nil
}

func (e *Exporter) readHeader() (Record, error) {
	f, err := os.Open(e.recordingPath)
	if err != nil {
		return Record{}, err
	}
	defer f.Close()

	var header Record
	ReadRecords(f, func(rec Record) bool {
		if rec.Type == RecordHeader {
			header = rec
			return false
		}
		return true
	})
	return header, nil
}

func (e *Exporter) writeCompressedEvents(recording io.Reader, cw *CastWriter, emu *ScreenEmulator, intervals []Interval) error {
	var records []Record
	ReadRecords(recording, func(rec Record) bool {
		records = append(records, rec)
		return true
	})

	totalRecords := len(records)
	var compressedT float64
	var lastInterval *Interval // track filler→content transitions

	for i, rec := range records {
		if rec.Type != RecordOutput || rec.T == nil {
			continue
		}

		origT := *rec.T

		// Always feed to emulator so its state stays current
		emu.Write([]byte(rec.D))

		// If no intervals were classified, pass through all events
		if len(intervals) == 0 {
			cw.WriteEvent(compressedT, rec.D)
			compressedT += 0.001
			continue
		}

		iv := findInterval(intervals, origT)
		if iv == nil {
			// Events outside classified intervals are treated as filler
			continue
		}

		switch iv.Type {
		case Content:
			// At each content interval boundary, emit a single keyframe
			// showing the full screen state. This is cleaner than replaying
			// individual events (which include spinner noise within the
			// same 500ms window as the scroll).
			if lastInterval == nil || lastInterval != iv {
				baseT := compressedTimeBase(intervals, iv)
				compressedT = baseT
				keyframe := emu.RenderKeyframe()
				cw.WriteEvent(compressedT, keyframe)
			}

		case Filler:
			// Skip — emulator still processes it for state tracking
		}

		lastInterval = iv

		if totalRecords > 0 && i%100 == 0 {
			pct := 0.5 + 0.5*float64(i)/float64(totalRecords)
			e.reportProgress(pct)
		}
	}

	return nil
}

// compressedTimeBase returns the compressed start time for an interval.
func compressedTimeBase(intervals []Interval, target *Interval) float64 {
	var t float64
	for i := range intervals {
		if &intervals[i] == target {
			return t
		}
		if intervals[i].Type == Content {
			t += intervals[i].End - intervals[i].Start
		} else {
			t += fillerDuration
		}
	}
	return t
}

// findInterval returns the interval containing time t, or nil.
func findInterval(intervals []Interval, t float64) *Interval {
	for i := range intervals {
		if t >= intervals[i].Start && t <= intervals[i].End {
			return &intervals[i]
		}
	}
	return nil
}

func (e *Exporter) reportProgress(pct float64) {
	if e.progressFn != nil {
		e.progressFn(pct)
	}
}
