//go:build notimelapse

package timelapse

import (
	"io"
	"time"

	"github.com/sergeknystautas/schmux/internal/session"
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
type Record struct {
	Type        RecordType `json:"type"`
	Version     int        `json:"version,omitempty"`
	RecordingID string     `json:"recordingId,omitempty"`
	SessionID   string     `json:"sessionId,omitempty"`
	Width       int        `json:"width,omitempty"`
	Height      int        `json:"height,omitempty"`
	StartTime   string     `json:"startTime,omitempty"`
	T           *float64   `json:"t,omitempty"`
	Seq         uint64     `json:"seq,omitempty"`
	D           string     `json:"d,omitempty"`
	Reason      string     `json:"reason,omitempty"`
	LostSeqs    [2]uint64  `json:"lostSeqs,omitempty"`
	Snapshot    *string    `json:"snapshot,omitempty"`
}

func ReadCastEvents(_ io.Reader, _ func(Record) bool) error { return nil }

// Recorder is a no-op stub.
type Recorder struct{}

func NewRecorder(_ string, _ *session.OutputLog, _ <-chan session.SourceEvent, _ string, _ int64, _, _ int) (*Recorder, error) {
	return &Recorder{}, nil
}

func (r *Recorder) RecordingID() string { return "" }
func (r *Recorder) Run()                {}
func (r *Recorder) Stop()               {}

// RecordingInfo holds metadata for a single recording file.
type RecordingInfo struct {
	RecordingID   string
	SessionID     string
	StartTime     time.Time
	ModTime       time.Time
	Duration      float64
	FileSize      int64
	Width         int
	Height        int
	InProgress    bool
	HasCompressed bool
	Path          string
}

func ListRecordings(_ string) ([]RecordingInfo, error) { return nil, nil }

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

// CastWriter is a no-op stub.
type CastWriter struct{}

func NewCastWriter(_ io.Writer, _ CastHeader) (*CastWriter, error) { return &CastWriter{}, nil }

func (c *CastWriter) WriteEvent(_ float64, _ string) error { return nil }

// IntervalType classifies a time interval in the recording.
type IntervalType int

const (
	Content IntervalType = iota
	Filler
)

// Interval represents a classified time range in the recording.
type Interval struct {
	Type  IntervalType
	Start float64
	End   float64
}

// ScreenEmulator is a no-op stub.
type ScreenEmulator struct{}

func NewScreenEmulator(_, _ int) *ScreenEmulator     { return &ScreenEmulator{} }
func (e *ScreenEmulator) Write(_ []byte)             {}
func (e *ScreenEmulator) Resize(_, _ int)            {}
func (e *ScreenEmulator) CellGrid(_, _ int) [][]rune { return nil }

// Exporter is a no-op stub.
type Exporter struct{}

func NewExporter(_, _ string, _ func(float64)) *Exporter { return &Exporter{} }
func (e *Exporter) Export() error                        { return nil }

func ShowFirstRunNotice(_ string) string { return "" }

// IsAvailable reports whether the timelapse module is included in this build.
func IsAvailable() bool { return false }
