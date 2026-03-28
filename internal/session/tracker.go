package session

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/events"
	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/tmux"
)

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

// SessionTracker maintains a long-lived control mode attachment for a tmux session.
// It tracks output activity and forwards terminal output to subscribers via fan-out.
// Subscribers survive control mode reconnections — the tracker-level fan-out
// re-subscribes to the new control mode client automatically.
type SessionTracker struct {
	sessionID      string
	tmuxSession    string
	paneID         string
	state          state.StateStore
	eventWatcher   *events.EventWatcher
	outputCallback func([]byte)
	logger         *log.Logger

	mu        sync.RWMutex
	cmClient  *controlmode.Client
	cmParser  *controlmode.Parser
	cmCmd     *exec.Cmd
	cmStdin   io.WriteCloser
	lastEvent time.Time

	// Tracker-level subscriber fan-out (survives reconnections)
	subsMu sync.Mutex
	subs   []chan SequencedOutput

	stopOnce   sync.Once
	stopCh     chan struct{}
	doneCh     chan struct{}
	stopCtx    context.Context
	stopCancel context.CancelFunc

	lastRetryLog time.Time

	Counters TrackerCounters

	// Sequenced output log for replay-based bootstrap and gap recovery
	outputLog *OutputLog

	// Sync trigger: signaled when tmux pauses output delivery (pause-after).
	// Websocket handler listens on this to send an immediate sync to the frontend.
	syncTrigger chan struct{}

	// Terminal size tracking for diagnostics (accessed from multiple goroutines)
	LastTerminalCols atomic.Int32
	LastTerminalRows atomic.Int32

	// SyncCheckEnabled controls whether pause-after flow control is enabled.
	// When false (default), tmux pause-after is not set, avoiding the stdinMu
	// race condition between ContinuePane and sync commands that can extend
	// pane pause duration during TUI redraws.
	SyncCheckEnabled bool
}

// IsAttached reports whether the tracker currently has an active control mode attachment.
func (t *SessionTracker) IsAttached() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.cmClient != nil
}

// OutputLog returns the sequenced output log for this session.
func (t *SessionTracker) OutputLog() *OutputLog {
	return t.outputLog
}

// SyncTrigger returns a channel that fires when the tracker detects a tmux
// output pause (via pause-after). Listeners should perform an immediate
// capture-pane sync to resync the frontend.
func (t *SessionTracker) SyncTrigger() <-chan struct{} {
	return t.syncTrigger
}

// NewSessionTracker creates a tracker for a session.
// If eventFilePath is non-empty and eventHandlers is non-nil, an EventWatcher
// is created for the unified event system.
func NewSessionTracker(sessionID, tmuxSession string, st state.StateStore, eventFilePath string, eventHandlers map[string][]events.EventHandler, outputCallback func([]byte), logger *log.Logger) *SessionTracker {
	stopCtx, stopCancel := context.WithCancel(context.Background())
	t := &SessionTracker{
		sessionID:      sessionID,
		tmuxSession:    tmuxSession,
		state:          st,
		outputCallback: outputCallback,
		logger:         logger,
		outputLog:      NewOutputLog(50000), // 50,000 entries ≈ 5MB at ~100 bytes/event
		syncTrigger:    make(chan struct{}, 1),
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
		stopCtx:        stopCtx,
		stopCancel:     stopCancel,
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
func (t *SessionTracker) Start() {
	go t.run()
}

// Stop terminates the tracker and closes the control mode connection.
func (t *SessionTracker) Stop() {
	t.stopOnce.Do(func() {
		close(t.stopCh)
		// Cancel the context used by attachControlMode() so the tmux process
		// is killed immediately even if closeControlMode() races with cmd storage.
		t.stopCancel()
		t.closeControlMode()
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

// SetTmuxSession updates the target tmux session name.
func (t *SessionTracker) SetTmuxSession(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tmuxSession = name
}

// SubscribeOutput returns a buffered channel that receives output events for this session.
// Multiple subscribers are supported. Subscriptions survive control mode reconnections.
func (t *SessionTracker) SubscribeOutput() <-chan SequencedOutput {
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
func (t *SessionTracker) UnsubscribeOutput(ch <-chan SequencedOutput) {
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
func (t *SessionTracker) fanOut(event controlmode.OutputEvent) {
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

// SendInput sends terminal input to the session via control mode.
func (t *SessionTracker) SendInput(data string) error {
	t.mu.RLock()
	client := t.cmClient
	paneID := t.paneID
	t.mu.RUnlock()
	if client == nil {
		return fmt.Errorf("not attached")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return client.SendKeys(ctx, paneID, data)
}

// Resize updates the terminal dimensions via control mode.
func (t *SessionTracker) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("invalid size %dx%d", cols, rows)
	}
	// Store the terminal size for diagnostics
	t.LastTerminalCols.Store(int32(cols))
	t.LastTerminalRows.Store(int32(rows))

	t.mu.RLock()
	client := t.cmClient
	t.mu.RUnlock()
	if client == nil {
		return fmt.Errorf("not attached")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return client.ResizeWindow(ctx, t.paneID, cols, rows)
}

// CaptureLastLines captures scrollback from tmux via control mode.
func (t *SessionTracker) CaptureLastLines(ctx context.Context, lines int) (string, error) {
	t.mu.RLock()
	client := t.cmClient
	paneID := t.paneID
	t.mu.RUnlock()
	if client == nil {
		return "", fmt.Errorf("not attached")
	}
	return client.CapturePaneLines(ctx, paneID, lines)
}

// CapturePane captures the visible screen of the pane (no scrollback).
// Returns the raw output including ANSI escape sequences.
func (t *SessionTracker) CapturePane(ctx context.Context) (string, error) {
	t.mu.RLock()
	client := t.cmClient
	paneID := t.paneID
	t.mu.RUnlock()
	if client == nil {
		return "", fmt.Errorf("not attached")
	}
	return client.CapturePaneVisible(ctx, paneID)
}

// GetCursorState returns the cursor position and visibility for the tracked pane.
func (t *SessionTracker) GetCursorState(ctx context.Context) (controlmode.CursorState, error) {
	t.mu.RLock()
	client := t.cmClient
	paneID := t.paneID
	t.mu.RUnlock()
	if client == nil {
		return controlmode.CursorState{}, fmt.Errorf("not attached")
	}
	return client.GetCursorState(ctx, paneID)
}

// GetCursorPosition returns the cursor position (x, y) for the tracked pane.
func (t *SessionTracker) GetCursorPosition(ctx context.Context) (x, y int, err error) {
	t.mu.RLock()
	client := t.cmClient
	paneID := t.paneID
	t.mu.RUnlock()
	if client == nil {
		return 0, 0, fmt.Errorf("not attached")
	}
	return client.GetCursorPosition(ctx, paneID)
}

// DiagnosticCounters returns a snapshot of pipeline counters including drop counts
// at all three fan-out layers: parser, client, and tracker.
func (t *SessionTracker) DiagnosticCounters() map[string]int64 {
	result := map[string]int64{
		"eventsDelivered":       t.Counters.EventsDelivered.Load(),
		"bytesDelivered":        t.Counters.BytesDelivered.Load(),
		"controlModeReconnects": t.Counters.Reconnects.Load(),
		"fanOutDrops":           t.Counters.FanOutDrops.Load(),
		"wsConnections":         t.Counters.WsConnections.Load(),
		"wsWriteErrors":         t.Counters.WsWriteErrors.Load(),
	}
	t.mu.RLock()
	if t.cmParser != nil {
		result["eventsDropped"] = t.cmParser.DroppedOutputs()
	}
	if t.cmClient != nil {
		result["clientFanOutDrops"] = t.cmClient.DroppedFanOut()
	}
	t.mu.RUnlock()
	if t.outputLog != nil {
		result["currentSeq"] = int64(t.outputLog.CurrentSeq())
		result["logOldestSeq"] = int64(t.outputLog.OldestSeq())
		result["logTotalBytes"] = t.outputLog.TotalBytes()
	}
	return result
}

func (t *SessionTracker) run() {
	defer close(t.doneCh)

	for {
		select {
		case <-t.stopCh:
			return
		default:
		}

		err := t.attachControlMode()
		if err != nil && err != io.EOF {
			// Check for permanent errors (session no longer exists)
			if isPermanentError(err) {
				if t.logger != nil {
					t.logger.Debug("stopping: tmux session no longer exists", "session", t.sessionID)
				}
				return
			}
			t.Counters.Reconnects.Add(1)
			now := time.Now()
			if t.shouldLogRetry(now) {
				if t.logger != nil {
					t.logger.Warn("control mode failed", "session", t.sessionID, "err", err)
				}
			}
		}

		if t.waitOrStop(trackerRestartDelay) {
			return
		}
	}
}

func (t *SessionTracker) attachControlMode() error {
	t.mu.RLock()
	target := t.tmuxSession
	t.mu.RUnlock()

	ctx, cancel := context.WithCancel(t.stopCtx)
	defer cancel()

	// Start tmux in control mode (-C, canonical mode with echo)
	// Note: -CC (non-canonical) requires a TTY via tcgetattr, which fails
	// when launched from exec.Command. -C works without a TTY, and the parser
	// ignores command echo since it only processes %-prefixed protocol lines.
	cmd := exec.CommandContext(ctx, tmux.Binary(), "-C", "attach-session", "-t", "="+target)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("failed to start control mode: %w", err)
	}

	// Create parser and client
	parser := controlmode.NewParser(stdout, t.logger, t.sessionID)
	go parser.Run()
	client := controlmode.NewClient(stdin, parser, t.logger)
	client.Start()

	// Wait for control mode to be ready
	readyCtx, readyCancel := context.WithTimeout(ctx, 10*time.Second)
	select {
	case <-parser.ControlModeReady():
		readyCancel()
	case <-readyCtx.Done():
		readyCancel()
		stdin.Close()
		cmd.Process.Kill()
		cmd.Wait()
		return fmt.Errorf("control mode not ready within timeout")
	}

	// Synchronize the FIFO command queue. When tmux enters control mode
	// via attach-session, it may send an implicit initial response (for the
	// attach itself). If this response arrives after we've already queued our
	// first command, it shifts the queue — each command receives the previous
	// command's response. We detect and absorb this offset by sending a
	// sentinel command and verifying its response content.
	const sentinel = "__SCHMUX_SYNC__"
	syncCtx, syncCancel := context.WithTimeout(ctx, 5*time.Second)
	for attempts := 0; attempts < 3; attempts++ {
		output, err := client.Execute(syncCtx, fmt.Sprintf("display-message -p '%s'", sentinel))
		if err != nil {
			syncCancel()
			stdin.Close()
			cmd.Process.Kill()
			cmd.Wait()
			return fmt.Errorf("control mode sync failed: %w", err)
		}
		if strings.TrimSpace(output) == sentinel {
			break // FIFO queue is synchronized
		}
		// Got a stale response (implicit attach response), try again
	}
	syncCancel()

	// Discover pane ID
	paneID, err := t.discoverPaneID(ctx, client)
	if err != nil {
		stdin.Close()
		cmd.Process.Kill()
		cmd.Wait()
		return fmt.Errorf("failed to discover pane ID: %w", err)
	}

	// Store references
	t.mu.Lock()
	t.cmClient = client
	t.cmParser = parser
	t.cmCmd = cmd
	t.cmStdin = stdin
	t.paneID = paneID
	t.mu.Unlock()

	defer t.closeControlMode()

	// Enable pause-after so tmux sends %pause instead of silently dropping
	// output when this control mode client falls behind. Only enabled when
	// sync check is active — pause-after triggers sync commands that contend
	// with ContinuePane on stdinMu, amplifying TUI redraw stutter.
	if t.SyncCheckEnabled {
		pauseCtx, pauseCancel := context.WithTimeout(ctx, 5*time.Second)
		if err := client.EnablePauseAfter(pauseCtx, 1); err != nil {
			if t.logger != nil {
				t.logger.Warn("failed to enable pause-after", "session", t.sessionID[:8], "err", err)
			}
		}
		pauseCancel()
	}

	// Subscribe to output from the control mode client and fan out to
	// tracker-level subscribers (which survive reconnections)
	outputCh := client.SubscribeOutput(paneID)
	defer client.UnsubscribeOutput(paneID, outputCh)

	var lastCMEventTime time.Time
	for {
		select {
		case event, ok := <-outputCh:
			if !ok {
				return io.EOF
			}

			// Activity tracking (debounced)
			now := time.Now()
			if t.logger != nil && !lastCMEventTime.IsZero() {
				gap := now.Sub(lastCMEventTime)
				if gap > 500*time.Millisecond {
					t.logger.Info("control mode output gap",
						"session", t.sessionID[:8],
						"gap_ms", gap.Milliseconds(),
						"ch_depth", len(outputCh),
						"data_len", len(event.Data),
					)
				}
			}
			lastCMEventTime = now
			shouldUpdate := t.lastEvent.IsZero() || now.Sub(t.lastEvent) >= trackerActivityDebounce
			if shouldUpdate {
				t.lastEvent = now
				if t.state != nil {
					t.state.UpdateSessionLastOutput(t.sessionID, now)
				}
			}

			// Fan out to all tracker-level subscribers
			t.fanOut(event)

			// Also invoke the output callback (preview autodetect)
			if t.outputCallback != nil {
				t.outputCallback([]byte(event.Data))
			}

		case pausedPane := <-client.Pauses():
			if t.logger != nil {
				t.logger.Info("tmux paused output, triggering sync and continue",
					"session", t.sessionID[:8], "pane", pausedPane)
			}
			// Signal websocket handler to do an immediate capture-pane sync
			select {
			case t.syncTrigger <- struct{}{}:
			default:
			}
			// Resume output delivery
			contCtx, contCancel := context.WithTimeout(ctx, 2*time.Second)
			if err := client.ContinuePane(contCtx, pausedPane); err != nil && t.logger != nil {
				t.logger.Warn("failed to continue pane", "pane", pausedPane, "err", err)
			}
			contCancel()

		case <-t.stopCh:
			return io.EOF
		}
	}
}

func (t *SessionTracker) discoverPaneID(ctx context.Context, client *controlmode.Client) (string, error) {
	output, err := client.Execute(ctx, "list-panes -F '#{pane_id}'")
	if err != nil {
		return "", err
	}
	paneID := strings.TrimSpace(output)
	if paneID == "" {
		return "", fmt.Errorf("no pane found")
	}
	// Return first pane if multiple
	if idx := strings.Index(paneID, "\n"); idx > 0 {
		paneID = paneID[:idx]
	}
	// Validate pane ID format (%N where N is a number)
	if len(paneID) < 2 || paneID[0] != '%' {
		return "", fmt.Errorf("invalid pane ID format: %q", paneID)
	}
	return paneID, nil
}

func (t *SessionTracker) closeControlMode() {
	t.mu.Lock()
	stdin := t.cmStdin
	cmd := t.cmCmd
	client := t.cmClient
	t.cmClient = nil
	t.cmParser = nil
	t.cmCmd = nil
	t.cmStdin = nil
	t.mu.Unlock()

	if client != nil {
		client.Close()
	}
	if stdin != nil {
		stdin.Close()
	}
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
		cmd.Wait()
	}
}

func (t *SessionTracker) shouldLogRetry(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lastRetryLog.IsZero() || now.Sub(t.lastRetryLog) >= trackerRetryLogInterval {
		t.lastRetryLog = now
		return true
	}
	return false
}

func (t *SessionTracker) waitOrStop(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return false
	case <-t.stopCh:
		return true
	}
}
