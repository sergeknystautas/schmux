package session

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/events"
	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
	"github.com/sergeknystautas/schmux/internal/state"
)

// Runnable is implemented by types that can be started in a goroutine and stopped.
type Runnable interface {
	Run()
	Stop()
}

const trackerRestartDelay = 500 * time.Millisecond
const trackerActivityDebounce = 500 * time.Millisecond

// SequencedOutput wraps an output event with its output log sequence number.
// The Seq is assigned atomically during fanOut so WebSocket handlers can use
// it directly instead of racing on CurrentSeq().
type SequencedOutput struct {
	controlmode.OutputEvent
	Seq uint64
}

const trackerRetryLogInterval = 15 * time.Second

// isPermanentError detects tmux errors that indicate the session is gone forever.
// These errors should cause the tracker to exit rather than retry indefinitely.
func isPermanentError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "can't find session") ||
		strings.Contains(msg, "no session found")
}

// TrackerCounters holds atomic pipeline counters for diagnostics.
type TrackerCounters struct {
	EventsDelivered atomic.Int64
	BytesDelivered  atomic.Int64
	Reconnects      atomic.Int64
	FanOutDrops     atomic.Int64 // Events dropped because a subscriber channel was full
	WsConnections   atomic.Int64 // total WS terminal connections opened for this session
	WsWriteErrors   atomic.Int64 // WS write failures that caused disconnect
}

// SessionRuntime drains events from a ControlSource, maintains a sequenced
// output log, and fans out to subscribers. The source owns reconnection
// logic; the runtime just processes events.
type SessionRuntime struct {
	sessionID      string
	source         ControlSource
	state          state.StateStore
	eventWatcher   *events.EventWatcher
	outputCallback func([]byte)
	logger         *log.Logger

	lastEvent time.Time

	// Tracker-level subscriber fan-out (survives reconnections)
	subsMu sync.Mutex
	subs   []chan SequencedOutput

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}

	Counters TrackerCounters

	// Sequenced output log for replay-based bootstrap and gap recovery
	outputLog *OutputLog

	// gapCh receives Gap and Resize events for the recorder (Phase 1).
	// nil when recording is not active.
	gapCh chan SourceEvent

	// Terminal size tracking for diagnostics (accessed from multiple goroutines)
	LastTerminalCols atomic.Int32
	LastTerminalRows atomic.Int32

	// HealthProbe provides access to the source's health probe (if available).
	// Points to the LocalSource's probe for local sessions; empty probe for others.
	HealthProbe *TmuxHealthProbe

	// RecorderFactory, if set, is called from run() to create a recorder.
	// The returned Runnable is started in a goroutine and stopped on exit.
	RecorderFactory func(outputLog *OutputLog, gapCh <-chan SourceEvent) Runnable
}

// Source returns the underlying ControlSource.
func (t *SessionRuntime) Source() ControlSource {
	return t.source
}

// IsAttached reports whether the source currently has an active attachment.
func (t *SessionRuntime) IsAttached() bool {
	return t.source.IsAttached()
}

// OutputLog returns the sequenced output log for this session.
func (t *SessionRuntime) OutputLog() *OutputLog {
	return t.outputLog
}

// SyncTrigger returns a channel that fires when the source detects a tmux
// output pause (via pause-after). Returns nil for sources that don't support it.
func (t *SessionRuntime) SyncTrigger() <-chan struct{} {
	if st, ok := t.source.(SyncTriggerer); ok {
		return st.SyncTrigger()
	}
	return nil
}

// NewSessionRuntime creates a runtime that drains events from a ControlSource.
// If eventFilePath is non-empty and eventHandlers is non-nil, an EventWatcher
// is created for the unified event system.
func NewSessionRuntime(sessionID string, source ControlSource, st state.StateStore, eventFilePath string, eventHandlers map[string][]events.EventHandler, outputCallback func([]byte), logger *log.Logger) *SessionRuntime {
	var healthProbe *TmuxHealthProbe
	if hp, ok := source.(HealthProbeProvider); ok {
		healthProbe = hp.GetHealthProbe()
	} else {
		healthProbe = NewTmuxHealthProbe()
	}

	t := &SessionRuntime{
		sessionID:      sessionID,
		source:         source,
		state:          st,
		outputCallback: outputCallback,
		logger:         logger,
		outputLog:      NewOutputLog(50000), // 50,000 entries ≈ 5MB at ~100 bytes/event
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
		HealthProbe:    healthProbe,
	}
	if eventFilePath != "" && eventHandlers != nil && len(eventHandlers) > 0 {
		ew, err := events.NewEventWatcher(eventFilePath, sessionID, eventHandlers)
		if err != nil {
			if t.logger != nil {
				t.logger.Warn("failed to create event watcher", "session", sessionID, "err", err)
			}
		} else {
			t.eventWatcher = ew
		}
	}
	return t
}

// Start launches the tracker loop in a background goroutine.
func (t *SessionRuntime) Start() {
	go t.run()
}

// Stop terminates the tracker by closing the source and cleaning up.
func (t *SessionRuntime) Stop() {
	t.stopOnce.Do(func() {
		close(t.stopCh)
		t.source.Close()
		if t.eventWatcher != nil {
			t.eventWatcher.Stop()
		}
		// Close all subscriber channels
		t.subsMu.Lock()
		for _, ch := range t.subs {
			close(ch)
		}
		t.subs = nil
		t.subsMu.Unlock()
		// Wait for run() to exit with a timeout.
		select {
		case <-t.doneCh:
		case <-time.After(5 * time.Second):
		}
	})
}

// SetTmuxSession updates the target tmux session name on the underlying source.
// No-op for sources that don't support runtime renames.
func (t *SessionRuntime) SetTmuxSession(name string) {
	if sr, ok := t.source.(SessionRenamer); ok {
		sr.SetTmuxSession(name)
	}
}

// SubscribeOutput returns a buffered channel that receives output events for this session.
// Multiple subscribers are supported. Subscriptions survive control mode reconnections.
func (t *SessionRuntime) SubscribeOutput() <-chan SequencedOutput {
	ch := make(chan SequencedOutput, 1000)
	t.subsMu.Lock()
	t.subs = append(t.subs, ch)
	t.subsMu.Unlock()
	return ch
}

// UnsubscribeOutput removes an output subscription.
// The channel is NOT closed here — closing during fanOut iteration would panic
// (send on closed channel). Subscribers detect session end via sessionDead or
// context cancellation. Stop() closes all channels safely after run() exits.
func (t *SessionRuntime) UnsubscribeOutput(ch <-chan SequencedOutput) {
	t.subsMu.Lock()
	defer t.subsMu.Unlock()
	for i, sub := range t.subs {
		if sub == ch {
			t.subs = append(t.subs[:i], t.subs[i+1:]...)
			return
		}
	}
}

// fanOut sends an output event to all subscribers. Slow consumers are skipped
// (non-blocking send) to avoid one client blocking others.
func (t *SessionRuntime) fanOut(event controlmode.OutputEvent) {
	t.Counters.EventsDelivered.Add(1)
	t.Counters.BytesDelivered.Add(int64(len(event.Data)))

	// Record in sequenced log (before fan-out, so replay is authoritative).
	// Capture the returned seq so subscribers get the correct sequence number
	// (using CurrentSeq()-1 after the fact is racy when multiple events arrive).
	seq := t.outputLog.Append([]byte(event.Data))

	seqEvent := SequencedOutput{OutputEvent: event, Seq: seq}

	t.subsMu.Lock()
	subs := make([]chan SequencedOutput, len(t.subs))
	copy(subs, t.subs)
	t.subsMu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- seqEvent:
		default:
			// Slow consumer — drop event to avoid blocking
			t.Counters.FanOutDrops.Add(1)
		}
	}
}

// SendInput sends terminal input to the session via the source.
func (t *SessionRuntime) SendInput(data string) (controlmode.SendKeysTimings, error) {
	return t.source.SendKeys(data)
}

// SendTmuxKeyName sends a tmux key name (e.g. "C-u", "Enter") to the session.
func (t *SessionRuntime) SendTmuxKeyName(name string) error {
	return t.source.SendTmuxKeyName(name)
}

// Resize updates the terminal dimensions via the source.
func (t *SessionRuntime) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("invalid size %dx%d", cols, rows)
	}
	t.LastTerminalCols.Store(int32(cols))
	t.LastTerminalRows.Store(int32(rows))

	// Forward resize to timelapse recorder if active
	if t.gapCh != nil {
		select {
		case t.gapCh <- SourceEvent{Type: SourceResize, Width: cols, Height: rows}:
		default:
		}
	}

	return t.source.Resize(cols, rows)
}

// CaptureLastLines captures scrollback via the source.
func (t *SessionRuntime) CaptureLastLines(ctx context.Context, lines int) (string, error) {
	return t.source.CaptureLines(lines)
}

// CapturePane captures the visible screen of the pane (no scrollback).
func (t *SessionRuntime) CapturePane(ctx context.Context) (string, error) {
	return t.source.CaptureVisible()
}

// GetCursorState returns the cursor position and visibility for the tracked pane.
func (t *SessionRuntime) GetCursorState(ctx context.Context) (controlmode.CursorState, error) {
	return t.source.GetCursorState()
}

// GetCursorPosition returns the cursor position (x, y) for the tracked pane.
func (t *SessionRuntime) GetCursorPosition(ctx context.Context) (x, y int, err error) {
	cs, err := t.source.GetCursorState()
	if err != nil {
		return 0, 0, err
	}
	return cs.X, cs.Y, nil
}

// DiagnosticCounters returns a snapshot of pipeline counters including drop counts
// at all fan-out layers.
func (t *SessionRuntime) DiagnosticCounters() map[string]int64 {
	result := map[string]int64{
		"eventsDelivered":       t.Counters.EventsDelivered.Load(),
		"bytesDelivered":        t.Counters.BytesDelivered.Load(),
		"controlModeReconnects": t.Counters.Reconnects.Load(),
		"fanOutDrops":           t.Counters.FanOutDrops.Load(),
		"wsConnections":         t.Counters.WsConnections.Load(),
		"wsWriteErrors":         t.Counters.WsWriteErrors.Load(),
	}
	// Source-specific diagnostics (e.g. parser/client counters)
	if dp, ok := t.source.(DiagnosticsProvider); ok {
		for k, v := range dp.SourceDiagnostics() {
			result[k] = v
		}
	}
	if t.outputLog != nil {
		result["currentSeq"] = int64(t.outputLog.CurrentSeq())
		result["logOldestSeq"] = int64(t.outputLog.OldestSeq())
		result["logTotalBytes"] = t.outputLog.TotalBytes()
	}
	return result
}

func (t *SessionRuntime) run() {
	defer close(t.doneCh)

	// Start timelapse recorder if factory is set
	if t.RecorderFactory != nil {
		t.gapCh = make(chan SourceEvent, 100)
		recorder := t.RecorderFactory(t.outputLog, t.gapCh)
		if recorder != nil {
			go recorder.Run()
			defer recorder.Stop()
		}
	}

	for event := range t.source.Events() {
		switch event.Type {
		case SourceOutput:
			// Activity tracking (debounced)
			now := time.Now()
			shouldUpdate := t.lastEvent.IsZero() || now.Sub(t.lastEvent) >= trackerActivityDebounce
			if shouldUpdate {
				t.lastEvent = now
				if t.state != nil {
					t.state.UpdateSessionLastOutput(t.sessionID, now)
				}
			}

			// Fan out to all tracker-level subscribers
			t.fanOut(controlmode.OutputEvent{Data: event.Data})

			// Also invoke the output callback (preview autodetect)
			if t.outputCallback != nil {
				t.outputCallback([]byte(event.Data))
			}

		case SourceGap:
			if t.gapCh != nil {
				t.gapCh <- event
			}

		case SourceResize:
			if t.gapCh != nil {
				t.gapCh <- event
			}

		case SourceClosed:
			return
		}
	}
}
