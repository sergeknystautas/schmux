package timelapse

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// RecordType identifies the kind of record in a timelapse recording.
type RecordType string

const (
	RecordHeader RecordType = "header"
	RecordOutput RecordType = "output"
	RecordResize RecordType = "resize"
	RecordGap    RecordType = "gap"
	RecordEnd    RecordType = "end"
)

// Record is a single line in a timelapse NDJSON recording file.
type Record struct {
	Type        RecordType `json:"type"`
	Version     int        `json:"version,omitempty"`     // header
	RecordingID string     `json:"recordingId,omitempty"` // header
	SessionID   string     `json:"sessionId,omitempty"`   // header
	Width       int        `json:"width,omitempty"`       // header, resize
	Height      int        `json:"height,omitempty"`      // header, resize
	StartTime   string     `json:"startTime,omitempty"`   // header (RFC3339)
	T           *float64   `json:"t,omitempty"`           // all except header — pointer to avoid omitting t=0.0
	Seq         uint64     `json:"seq,omitempty"`         // output
	D           string     `json:"d,omitempty"`           // output (raw terminal data)
	Reason      string     `json:"reason,omitempty"`      // gap
	LostSeqs    [2]uint64  `json:"lostSeqs,omitempty"`    // gap: [first, last]
	Snapshot    *string    `json:"snapshot,omitempty"`    // gap: nullable screen content
}

// floatPtr creates a *float64 for use in Record.T fields.
func floatPtr(f float64) *float64 { return &f }

// WriteRecord writes a single Record as a JSON line to w.
func WriteRecord(w io.Writer, r Record) error {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// ReadRecords reads NDJSON records from r, returning them one at a time via a callback.
// Stops on EOF or the first parse error.
func ReadRecords(r io.Reader, fn func(Record) bool) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line
	for scanner.Scan() {
		var rec Record
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			return fmt.Errorf("unmarshal record: %w", err)
		}
		if !fn(rec) {
			return nil
		}
	}
	return scanner.Err()
}
