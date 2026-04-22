package session

import "github.com/sergeknystautas/schmux/internal/remote/controlmode"

// SourceEventType identifies the kind of event emitted by a ControlSource.
type SourceEventType int

const (
	SourceOutput SourceEventType = iota
	SourceGap
	SourceResize
	SourceClosed
	// SourcePasteBuffer is fired when tmux notifies via %paste-buffer-changed
	// that a paste buffer was added or modified (load-buffer, set-buffer,
	// copy-mode Enter). The source has already fetched the buffer content
	// via show-buffer and applied the same byte-level defang as the OSC 52
	// path. Data carries the post-defang text; ByteCount and
	// StrippedControlChars carry the pre-defang stats. Tracker funnels these
	// through the same clipboardCh / clipboardState pipeline as OSC 52 so
	// the user sees a single approve/reject banner regardless of which path
	// the TUI took.
	SourcePasteBuffer
)

// SourceEvent is the unified event type emitted by all ControlSource implementations.
//
// Data type rationale: Data is string (not []byte) because the upstream
// controlmode.OutputEvent.Data is string. The conversion to []byte happens
// at the OutputLog boundary: outputLog.Append([]byte(event.Data)).
type SourceEvent struct {
	Type     SourceEventType
	Data     string // SourceOutput, SourcePasteBuffer — terminal bytes / clipboard text
	Reason   string // SourceGap
	Snapshot string // SourceGap: capture-pane on reconnect
	Width    int    // SourceResize
	Height   int    // SourceResize
	Err      error  // SourceClosed: nil = clean, non-nil = permanent
	// SourcePasteBuffer-only fields. ByteCount is the pre-defang decoded byte
	// length; StrippedControlChars is how many C0/DEL bytes the defang dropped.
	// Mirrors ClipboardRequest's like-named fields so tracker can pass them
	// straight through to the dashboard banner.
	ByteCount            int
	StrippedControlChars int
}

// ControlSource is the input boundary for SessionRuntime.
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
