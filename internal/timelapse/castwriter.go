package timelapse

import (
	"encoding/json"
	"fmt"
	"io"
)

// CastHeader is the asciicast v2 header.
type CastHeader struct {
	Version          int               `json:"version"`
	Width            int               `json:"width"`
	Height           int               `json:"height"`
	Duration         float64           `json:"duration,omitempty"`
	Title            string            `json:"title,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	OriginalDuration float64           `json:"-"`
	CompressionRatio float64           `json:"-"`
	RecordingID      string            `json:"-"`
}

// CastWriter writes asciicast v2 format (NDJSON with header + events).
type CastWriter struct {
	w      io.Writer
	offset float64
}

// NewCastWriter creates a CastWriter and writes the header.
func NewCastWriter(w io.Writer, header CastHeader) (*CastWriter, error) {
	header.Version = 2
	if header.Env == nil {
		header.Env = map[string]string{"TERM": "xterm-256color"}
	}

	data, err := json.Marshal(header)
	if err != nil {
		return nil, fmt.Errorf("marshal cast header: %w", err)
	}
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		return nil, err
	}

	return &CastWriter{w: w}, nil
}

// WriteEvent writes an output event at the given timestamp.
func (c *CastWriter) WriteEvent(timestamp float64, data string) error {
	// asciicast v2 event: [timestamp, "o", data]
	event := [3]interface{}{timestamp, "o", data}
	line, err := json.Marshal(event)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = c.w.Write(line)
	return err
}

// WriteKeyframe writes a keyframe (clear + full redraw) at the given timestamp.
func (c *CastWriter) WriteKeyframe(timestamp float64, keyframe string) error {
	return c.WriteEvent(timestamp, keyframe)
}

// Offset returns the current time offset in the compressed timeline.
func (c *CastWriter) Offset() float64 {
	return c.offset
}

// SetOffset sets the current time offset.
func (c *CastWriter) SetOffset(t float64) {
	c.offset = t
}
