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
// Data is raw terminal bytes — we JSON-escape manually to avoid
// json.Marshal replacing invalid UTF-8 with \uFFFD.
func (c *CastWriter) WriteEvent(timestamp float64, data string) error {
	// asciicast v2 event: [timestamp, "o", data]
	// Manually build JSON to preserve raw bytes in the data string
	escapedData := jsonEscapeBytes([]byte(data))
	line := fmt.Sprintf("[%.6f,\"o\",%s]\n", timestamp, escapedData)
	_, err := c.w.Write([]byte(line))
	return err
}

// WriteKeyframe writes a keyframe (clear + full redraw) at the given timestamp.
func (c *CastWriter) WriteKeyframe(timestamp float64, keyframe string) error {
	return c.WriteEvent(timestamp, keyframe)
}

// jsonEscapeBytes produces a JSON string literal from raw bytes,
// escaping control characters and quotes but preserving all bytes.
func jsonEscapeBytes(b []byte) string {
	var buf []byte
	buf = append(buf, '"')
	for _, c := range b {
		switch c {
		case '"':
			buf = append(buf, '\\', '"')
		case '\\':
			buf = append(buf, '\\', '\\')
		case '\n':
			buf = append(buf, '\\', 'n')
		case '\r':
			buf = append(buf, '\\', 'r')
		case '\t':
			buf = append(buf, '\\', 't')
		case '\b':
			buf = append(buf, '\\', 'b')
		case '\f':
			buf = append(buf, '\\', 'f')
		default:
			if c < 0x20 {
				buf = append(buf, fmt.Sprintf("\\u%04x", c)...)
			} else {
				buf = append(buf, c)
			}
		}
	}
	buf = append(buf, '"')
	return string(buf)
}

// Offset returns the current time offset in the compressed timeline.
func (c *CastWriter) Offset() float64 {
	return c.offset
}

// SetOffset sets the current time offset.
func (c *CastWriter) SetOffset(t float64) {
	c.offset = t
}
