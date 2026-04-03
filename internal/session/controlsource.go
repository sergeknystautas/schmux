package session

import "github.com/sergeknystautas/schmux/internal/remote/controlmode"

// SourceEventType identifies the kind of event emitted by a ControlSource.
type SourceEventType int

const (
	SourceOutput SourceEventType = iota
	SourceGap
	SourceResize
	SourceClosed
)

// SourceEvent is the unified event type emitted by all ControlSource implementations.
//
// Data type rationale: Data is string (not []byte) because the upstream
// controlmode.OutputEvent.Data is string. The conversion to []byte happens
// at the OutputLog boundary: outputLog.Append([]byte(event.Data)).
type SourceEvent struct {
	Type     SourceEventType
	Data     string // SourceOutput — matches controlmode.OutputEvent.Data (string)
	Reason   string // SourceGap
	Snapshot string // SourceGap: capture-pane on reconnect
	Width    int    // SourceResize
	Height   int    // SourceResize
	Err      error  // SourceClosed: nil = clean, non-nil = permanent
}

// ControlSource is the input boundary for SessionTracker.
// Implementations own reconnection logic; the tracker just drains Events().
type ControlSource interface {
	Events() <-chan SourceEvent
	SendKeys(keys string) (controlmode.SendKeysTimings, error)
	CaptureVisible() (string, error)    // visible screen (no scrollback)
	CaptureLines(n int) (string, error) // last N lines of scrollback
	GetCursorState() (controlmode.CursorState, error)
	Resize(cols, rows int) error // resize terminal window
	Close() error
}
