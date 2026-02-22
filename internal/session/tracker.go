package session

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
	"github.com/sergeknystautas/schmux/internal/signal"
	"github.com/sergeknystautas/schmux/internal/state"
)

const trackerRestartDelay = 500 * time.Millisecond
const trackerActivityDebounce = 500 * time.Millisecond
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
	fileWatcher    *signal.FileWatcher
	outputCallback func([]byte)

	mu        sync.RWMutex
	cmClient  *controlmode.Client
	cmParser  *controlmode.Parser
	cmCmd     *exec.Cmd
	cmStdin   io.WriteCloser
	lastEvent time.Time

	// Tracker-level subscriber fan-out (survives reconnections)
	subsMu sync.Mutex
	subs   []chan controlmode.OutputEvent

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}

	lastRetryLog time.Time

	Counters TrackerCounters

	// Terminal size tracking for diagnostics
	LastTerminalCols int
	LastTerminalRows int
}

// IsAttached reports whether the tracker currently has an active control mode attachment.
func (t *SessionTracker) IsAttached() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.cmClient != nil
}

// NewSessionTracker creates a tracker for a session.
// If signalFilePath is non-empty and signalCallback is non-nil, a FileWatcher
// is created to detect signal changes via filesystem notifications.
func NewSessionTracker(sessionID, tmuxSession string, st state.StateStore, signalFilePath string, signalCallback func(signal.Signal), outputCallback func([]byte)) *SessionTracker {
	t := &SessionTracker{
		sessionID:      sessionID,
		tmuxSession:    tmuxSession,
		state:          st,
		outputCallback: outputCallback,
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
	}
	if signalFilePath != "" && signalCallback != nil {
		fw, err := signal.NewFileWatcher(sessionID, signalFilePath, signalCallback)
		if err != nil {
			fmt.Printf("[tracker] %s - failed to create file watcher: %v\n", sessionID, err)
		} else {
			t.fileWatcher = fw
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
		t.closeControlMode()
		if t.fileWatcher != nil {
			t.fileWatcher.Stop()
		}
		// Close all subscriber channels
		t.subsMu.Lock()
		for _, ch := range t.subs {
			close(ch)
		}
		t.subs = nil
		t.subsMu.Unlock()
		<-t.doneCh
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
func (t *SessionTracker) SubscribeOutput() <-chan controlmode.OutputEvent {
	ch := make(chan controlmode.OutputEvent, 100)
	t.subsMu.Lock()
	t.subs = append(t.subs, ch)
	t.subsMu.Unlock()
	return ch
}

// UnsubscribeOutput removes an output subscription and closes its channel.
func (t *SessionTracker) UnsubscribeOutput(ch <-chan controlmode.OutputEvent) {
	t.subsMu.Lock()
	defer t.subsMu.Unlock()
	for i, sub := range t.subs {
		if sub == ch {
			close(sub)
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

	t.subsMu.Lock()
	subs := make([]chan controlmode.OutputEvent, len(t.subs))
	copy(subs, t.subs)
	t.subsMu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- event:
		default:
			// Slow consumer — drop event to avoid blocking
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
	t.LastTerminalCols = cols
	t.LastTerminalRows = rows

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

// DiagnosticCounters returns a snapshot of pipeline counters including parser drop counts.
func (t *SessionTracker) DiagnosticCounters() map[string]int64 {
	result := map[string]int64{
		"eventsDelivered":       t.Counters.EventsDelivered.Load(),
		"bytesDelivered":        t.Counters.BytesDelivered.Load(),
		"controlModeReconnects": t.Counters.Reconnects.Load(),
	}
	t.mu.RLock()
	if t.cmParser != nil {
		result["eventsDropped"] = t.cmParser.DroppedOutputs()
	}
	t.mu.RUnlock()
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
				fmt.Printf("[tracker] %s stopping: tmux session no longer exists\n", t.sessionID)
				return
			}
			t.Counters.Reconnects.Add(1)
			now := time.Now()
			if t.shouldLogRetry(now) {
				fmt.Printf("[tracker] %s control mode failed: %v\n", t.sessionID, err)
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start tmux in control mode (-C, canonical mode with echo)
	// Note: -CC (non-canonical) requires a TTY via tcgetattr, which fails
	// when launched from exec.Command. -C works without a TTY, and the parser
	// ignores command echo since it only processes %-prefixed protocol lines.
	cmd := exec.CommandContext(ctx, "tmux", "-C", "attach-session", "-t", "="+target)
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
	parser := controlmode.NewParser(stdout, t.sessionID)
	go parser.Run()
	client := controlmode.NewClient(stdin, parser)
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

	// Subscribe to output from the control mode client and fan out to
	// tracker-level subscribers (which survive reconnections)
	outputCh := client.SubscribeOutput(paneID)
	defer client.UnsubscribeOutput(paneID, outputCh)

	for {
		select {
		case event, ok := <-outputCh:
			if !ok {
				return io.EOF
			}

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
			t.fanOut(event)

			// Also invoke the output callback (preview autodetect)
			if t.outputCallback != nil {
				t.outputCallback([]byte(event.Data))
			}

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

// extractNudgeState parses the state field from a JSON nudge string.
// The nudge is stored as JSON like {"state":"Completed","summary":"Done","source":"agent"}.
// If the nudge is empty or not valid JSON, returns the raw string as a fallback.
func extractNudgeState(nudge string) string {
	if nudge == "" {
		return ""
	}
	var parsed struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal([]byte(nudge), &parsed); err != nil {
		return nudge // if not JSON, treat as raw state string
	}
	return parsed.State
}
