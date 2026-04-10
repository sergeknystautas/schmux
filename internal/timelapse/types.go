package timelapse

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
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

// Record is a parsed event from a timelapse recording.
// The recorder writes asciicast v2 (.cast) format directly.
// ReadCastEvents parses those files back into Record structs.
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
	D           string     `json:"d,omitempty"`           // output (terminal data)
	Reason      string     `json:"reason,omitempty"`      // gap
	LostSeqs    [2]uint64  `json:"lostSeqs,omitempty"`    // gap: [first, last]
	Snapshot    *string    `json:"snapshot,omitempty"`    // gap: nullable screen content
}

// ReadCastEvents reads an asciicast v2 file (.cast format).
// The first line is a JSON header object; subsequent lines are [timestamp, type, data] arrays.
// Events are returned as Record structs via the callback.
// Event type "o" maps to RecordOutput; "r" maps to RecordResize (data is "WxH").
func ReadCastEvents(r io.Reader, fn func(Record) bool) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line

	firstLine := true
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		if firstLine {
			firstLine = false
			// Parse asciicast v2 header: JSON object with version, width, height, etc.
			var hdr struct {
				Version   int    `json:"version"`
				Width     int    `json:"width"`
				Height    int    `json:"height"`
				Timestamp int64  `json:"timestamp"`
				Title     string `json:"title"`
			}
			if err := json.Unmarshal(line, &hdr); err != nil {
				return fmt.Errorf("unmarshal cast header: %w", err)
			}
			rec := Record{
				Type:        RecordHeader,
				Version:     hdr.Version,
				Width:       hdr.Width,
				Height:      hdr.Height,
				RecordingID: hdr.Title,
			}
			if hdr.Timestamp > 0 {
				rec.StartTime = fmt.Sprintf("%d", hdr.Timestamp)
			}
			if !fn(rec) {
				return nil
			}
			continue
		}

		// Parse event line: [timestamp, "type", "data"]
		var raw [3]json.RawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			return fmt.Errorf("unmarshal cast event: %w", err)
		}

		var timestamp float64
		if err := json.Unmarshal(raw[0], &timestamp); err != nil {
			return fmt.Errorf("unmarshal event timestamp: %w", err)
		}

		var eventType string
		if err := json.Unmarshal(raw[1], &eventType); err != nil {
			return fmt.Errorf("unmarshal event type: %w", err)
		}

		var data string
		if err := json.Unmarshal(raw[2], &data); err != nil {
			return fmt.Errorf("unmarshal event data: %w", err)
		}

		t := timestamp
		switch eventType {
		case "o":
			rec := Record{
				Type: RecordOutput,
				T:    &t,
				D:    data,
			}
			if !fn(rec) {
				return nil
			}
		case "r":
			rec := Record{
				Type: RecordResize,
				T:    &t,
			}
			// Parse "WxH" from data
			parts := strings.SplitN(data, "x", 2)
			if len(parts) == 2 {
				if w, err := strconv.Atoi(parts[0]); err == nil {
					rec.Width = w
				}
				if h, err := strconv.Atoi(parts[1]); err == nil {
					rec.Height = h
				}
			}
			if !fn(rec) {
				return nil
			}
		}
	}
	return scanner.Err()
}
