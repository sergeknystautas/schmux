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
	SendTmuxKeyName(name string) error  // send tmux key name (e.g. "C-u", "Enter") without -l flag
	CaptureVisible() (string, error)    // visible screen (no scrollback)
	CaptureLines(n int) (string, error) // last N lines of scrollback
	GetCursorState() (controlmode.CursorState, error)
	Resize(cols, rows int) error // resize terminal window
	IsAttached() bool            // reports whether the source has an active control mode connection
	Close() error
}

// SyncTriggerer is implemented by sources that support sync triggers.
type SyncTriggerer interface {
	SyncTrigger() <-chan struct{}
}

// DiagnosticsProvider is implemented by sources that expose transport diagnostics.
type DiagnosticsProvider interface {
	SourceDiagnostics() map[string]int64
}

// SessionRenamer is implemented by sources that support runtime session renames.
type SessionRenamer interface {
	SetTmuxSession(name string)
}

// HealthProbeProvider is implemented by sources that expose a health probe.
type HealthProbeProvider interface {
	GetHealthProbe() *TmuxHealthProbe
}
