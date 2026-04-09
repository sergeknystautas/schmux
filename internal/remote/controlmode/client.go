package controlmode

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

// Client provides a high-level interface for tmux control mode.
// It sends commands and correlates responses using a FIFO queue since tmux
// assigns sequential command IDs starting from 0, not using our local IDs.
//
// On reconnection, tmux may emit stale responses from the previous control
// mode session before the client sends any commands. These are discarded:
// any response arriving while the pending queue is empty (i.e., before the
// first command has been sent) is treated as stale and dropped.
type Client struct {
	stdin   io.Writer
	stdinMu sync.Mutex // Protects stdin writes to prevent interleaving

	parser *Parser
	logger *log.Logger

	// Command correlation - FIFO queue since tmux assigns sequential IDs
	pendingQueue []chan CommandResponse
	pendingMu    sync.Mutex

	// Epoch tracking: responses arriving before the first command is sent
	// are stale (from a previous control mode session) and are discarded.
	// Protected by pendingMu to ensure atomicity with queue state checks.
	firstCommandSent bool
	discardedStale   atomic.Int64

	// Response channel registry to prevent leaks on timeout
	respChans   map[chan CommandResponse]bool
	respChansMu sync.Mutex

	// Output subscriptions by pane ID
	outputSubs   map[string][]chan OutputEvent
	outputSubsMu sync.RWMutex

	// Output fan-out drop counter (events dropped because subscriber couldn't keep up)
	droppedFanOut atomic.Int64

	// Pause notifications (pane IDs paused by tmux when output falls behind)
	pauseCh chan string

	// Lifecycle
	running   bool
	closeCh   chan struct{}
	closeOnce sync.Once
}

// WindowInfo represents information about a tmux window.
type WindowInfo struct {
	WindowID   string // e.g., "@3"
	WindowName string
	PaneID     string // e.g., "%5"
}

// NewClient creates a new control mode client.
// stdin is used to send commands, parser reads from stdout.
// logger is an optional structured logger; if nil, logging is disabled.
func NewClient(stdin io.Writer, parser *Parser, logger *log.Logger) *Client {
	return &Client{
		stdin:        stdin,
		parser:       parser,
		logger:       logger,
		pendingQueue: make([]chan CommandResponse, 0),
		respChans:    make(map[chan CommandResponse]bool),
		outputSubs:   make(map[string][]chan OutputEvent),
		pauseCh:      make(chan string, 10),
		closeCh:      make(chan struct{}),
	}
}

// Start begins processing parser output.
// Call this in a goroutine before sending commands.
// Any responses already buffered by the parser (stale responses from a
// previous control mode session) are drained synchronously before the
// processing goroutines start.
func (c *Client) Start() {
	c.running = true
	c.drainBufferedResponses()
	go c.processResponses()
	go c.processOutput()
	go c.processEvents()
}

// drainBufferedResponses removes any responses already sitting in the parser's
// response channel. On reconnection, tmux emits %begin/%end blocks for the
// previous session's state before the client sends any commands. These are
// stale and must be discarded to prevent FIFO queue corruption.
func (c *Client) drainBufferedResponses() {
	for {
		select {
		case resp, ok := <-c.parser.Responses():
			if !ok {
				return
			}
			c.discardedStale.Add(1)
			if c.logger != nil {
				c.logger.Debug("discarded stale response (buffered)", "cmd_id", resp.CommandID)
			}
		default:
			// Channel empty, done draining
			if n := c.discardedStale.Load(); n > 0 && c.logger != nil {
				c.logger.Info("drained stale responses from previous session", "count", n)
			}
			return
		}
	}
}

// Close shuts down the client. Safe to call multiple times.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		c.pendingMu.Lock()
		c.running = false
		close(c.closeCh)
		// Send error responses to any pending commands still waiting
		for _, ch := range c.pendingQueue {
			// Send error response - channel is buffered so won't block
			// Don't close the channel - caller may still be in select waiting for it
			ch <- CommandResponse{Success: false, Content: "client closed"}
		}
		c.pendingQueue = nil
		c.pendingMu.Unlock()

		// Close all orphaned response channels to prevent leaks
		c.respChansMu.Lock()
		for ch := range c.respChans {
			close(ch)
		}
		c.respChans = nil
		c.respChansMu.Unlock()

		c.parser.Close()
	})
}

// Execute sends a command and waits for the response.
// FIFO ordering is critical: responses are matched to commands in order sent.
// Timeout/cancellation does NOT remove from queue to prevent misdelivery.
func (c *Client) Execute(ctx context.Context, cmd string) (string, time.Duration, error) {
	// Create response channel
	respCh := make(chan CommandResponse, 1)

	// Register channel in registry to track for cleanup
	c.respChansMu.Lock()
	if c.respChans == nil {
		c.respChansMu.Unlock()
		return "", 0, fmt.Errorf("client closed")
	}
	c.respChans[respCh] = true
	c.respChansMu.Unlock()

	// Deregister channel after use (on normal completion or timeout)
	defer func() {
		c.respChansMu.Lock()
		delete(c.respChans, respCh)
		c.respChansMu.Unlock()
	}()

	// Add to queue under lock
	c.pendingMu.Lock()
	if !c.running {
		c.pendingMu.Unlock()
		return "", 0, fmt.Errorf("client not running")
	}
	c.pendingQueue = append(c.pendingQueue, respCh)
	c.firstCommandSent = true
	c.pendingMu.Unlock()

	// Send command (tmux control mode assigns IDs automatically based on order)
	// Commands are matched to responses in FIFO order
	// Protect stdin write with mutex to prevent concurrent command interleaving
	mutexStart := time.Now()
	c.stdinMu.Lock()
	mutexWait := time.Since(mutexStart)
	_, err := fmt.Fprintf(c.stdin, "%s\n", cmd)
	c.stdinMu.Unlock()
	if err != nil {
		// Failed to send - leave channel in queue but don't listen to it
		// processResponses will still try to deliver, but we won't be waiting
		return "", mutexWait, fmt.Errorf("failed to send command: %w", err)
	}

	// Wait for response
	select {
	case resp := <-respCh:
		if !resp.Success {
			if c.logger != nil {
				c.logger.Error("command failed", "cmd", cmd, "err", resp.Content)
			}
			return "", mutexWait, fmt.Errorf("command failed: %s", resp.Content)
		}
		return resp.Content, mutexWait, nil
	case <-ctx.Done():
		// DO NOT remove from queue or close channel - just stop listening
		// The response will still arrive and be sent to this buffered channel (won't block)
		// Since we're no longer listening, the value is effectively discarded
		// Channel will be deregistered and cleaned up by defer
		if c.logger != nil {
			c.logger.Error("command timeout", "cmd", cmd)
		}
		return "", mutexWait, ctx.Err()
	case <-c.closeCh:
		// Client is closing, channel will be cleaned up by defer
		return "", mutexWait, fmt.Errorf("client closed")
	}
}

// processResponses routes responses to waiting commands in FIFO order.
// Handles cancelled commands (nobody listening) by sending to buffered channel anyway.
// Responses arriving before the first command is sent are stale (from a previous
// control mode session after daemon restart) and are discarded.
func (c *Client) processResponses() {
	for {
		select {
		case resp, ok := <-c.parser.Responses():
			if !ok {
				return
			}

			c.pendingMu.Lock()
			if len(c.pendingQueue) > 0 {
				// Deliver to first waiting command (FIFO order)
				ch := c.pendingQueue[0]
				c.pendingQueue = c.pendingQueue[1:]
				c.pendingMu.Unlock()

				// Send to buffered channel - won't block even if nobody is listening
				// Cancelled commands simply won't read from their channel
				ch <- resp
			} else if !c.firstCommandSent {
				// No command has been sent yet — this response is stale,
				// from a previous control mode session (e.g., daemon restart).
				c.pendingMu.Unlock()
				c.discardedStale.Add(1)
				if c.logger != nil {
					c.logger.Debug("discarded stale response (pre-epoch)", "cmd_id", resp.CommandID)
				}
			} else {
				c.pendingMu.Unlock()
				if c.logger != nil {
					c.logger.Warn("received response but no pending commands", "cmd_id", resp.CommandID)
				}
			}
		case <-c.closeCh:
			return
		}
	}
}

// processOutput routes output events to subscribers.
func (c *Client) processOutput() {
	for {
		select {
		case event, ok := <-c.parser.Output():
			if !ok {
				return
			}
			c.outputSubsMu.RLock()
			subs := c.outputSubs[event.PaneID]
			for _, ch := range subs {
				select {
				case ch <- event:
				default:
					// Drop if subscriber can't keep up
					c.droppedFanOut.Add(1)
				}
			}
			c.outputSubsMu.RUnlock()
		case <-c.closeCh:
			return
		}
	}
}

// processEvents handles async events, routing pause notifications.
func (c *Client) processEvents() {
	for {
		select {
		case event, ok := <-c.parser.Events():
			if !ok {
				return
			}
			switch event.Type {
			case "exit":
				// %exit is sent when the control mode client is about to
				// disconnect (session destroyed, server shutting down, etc.).
				// The reason arg is critical for diagnosing unexpected drops.
				reason := strings.Join(event.Args, " ")
				if c.logger != nil {
					c.logger.Warn("tmux control mode exit", "reason", reason)
				}
			case "session-changed":
				if c.logger != nil {
					c.logger.Info("tmux session changed", "args", strings.Join(event.Args, " "))
				}
			case "pause":
				if len(event.Args) > 0 {
					if c.logger != nil {
						c.logger.Info("pane paused by tmux (output fell behind)", "pane", event.Args[0])
					}
					select {
					case c.pauseCh <- event.Args[0]:
					default:
					}
				}
			}
		case <-c.closeCh:
			return
		}
	}
}

// SubscribeOutput subscribes to output from a specific pane.
// Returns a channel that receives output events.
func (c *Client) SubscribeOutput(paneID string) <-chan OutputEvent {
	ch := make(chan OutputEvent, 1000)
	c.outputSubsMu.Lock()
	c.outputSubs[paneID] = append(c.outputSubs[paneID], ch)
	c.outputSubsMu.Unlock()
	return ch
}

// UnsubscribeOutput removes a subscription.
func (c *Client) UnsubscribeOutput(paneID string, ch <-chan OutputEvent) {
	c.outputSubsMu.Lock()
	defer c.outputSubsMu.Unlock()
	subs := c.outputSubs[paneID]
	for i, sub := range subs {
		if sub == ch {
			c.outputSubs[paneID] = append(subs[:i], subs[i+1:]...)
			close(sub)
			break
		}
	}
}

// DroppedFanOut returns the number of output events dropped at the client fan-out layer
// because a subscriber channel was full.
func (c *Client) DroppedFanOut() int64 {
	return c.droppedFanOut.Load()
}

// Pauses returns a channel that receives pane IDs when tmux pauses output
// delivery for that pane (because the control mode client fell behind).
func (c *Client) Pauses() <-chan string {
	return c.pauseCh
}

// EnablePauseAfter sets the pause-after flag on this control mode client.
// When set, tmux sends %pause instead of silently dropping output when the
// client falls behind by the specified number of seconds.
func (c *Client) EnablePauseAfter(ctx context.Context, seconds int) error {
	_, _, err := c.Execute(ctx, fmt.Sprintf("refresh-client -f pause-after=%d", seconds))
	return err
}

// ContinuePane resumes output delivery for a paused pane.
func (c *Client) ContinuePane(ctx context.Context, paneID string) error {
	_, _, err := c.Execute(ctx, fmt.Sprintf("refresh-client -A %s:continue", paneID))
	return err
}

// DiscardedStale returns the number of stale responses discarded during startup.
// These are responses from a previous control mode session that arrived before
// the client sent its first command.
func (c *Client) DiscardedStale() int64 {
	return c.discardedStale.Load()
}

// CreateWindow creates a new window with a command.
// Returns the window ID and pane ID.
// If command is empty, the default shell is started (omitting the command
// argument entirely so tmux doesn't receive an empty string that exits immediately).
func (c *Client) CreateWindow(ctx context.Context, name, workdir, command string) (windowID, paneID string, err error) {
	// Build command — omit the command arg when empty so tmux starts the default shell
	var cmd string
	if command == "" {
		cmd = fmt.Sprintf("new-window -n %s -c %s -P -F '#{window_id} #{pane_id}'",
			shellutil.Quote(name), shellutil.Quote(workdir))
	} else {
		cmd = fmt.Sprintf("new-window -n %s -c %s -P -F '#{window_id} #{pane_id}' %s",
			shellutil.Quote(name), shellutil.Quote(workdir), shellutil.Quote(command))
	}

	output, _, err := c.Execute(ctx, cmd)
	if err != nil {
		return "", "", fmt.Errorf("failed to create window: %w", err)
	}

	// Parse output: "@3 %5"
	parts := strings.Fields(strings.TrimSpace(output))
	if len(parts) != 2 {
		return "", "", fmt.Errorf("unexpected new-window output: %q", output)
	}

	return parts[0], parts[1], nil
}

// KillWindow kills a window by ID.
func (c *Client) KillWindow(ctx context.Context, windowID string) error {
	_, _, err := c.Execute(ctx, fmt.Sprintf("kill-window -t %s", windowID))
	return err
}

// SendKeys sends keys to a pane.
// Splits input into printable text (sent with -l for literal mode) and
// special characters (sent as tmux key names). This is necessary because
// tmux control mode command parsing can mishandle raw control characters
// embedded in the command string.
func (c *Client) SendKeys(ctx context.Context, paneID, keys string) (SendKeysTimings, error) {
	var timings SendKeysTimings
	runs := ClassifyKeyRuns(nil, keys)
	timings.ExecuteCount = len(runs)
	for _, run := range runs {
		var cmd string
		if run.Literal {
			cmd = fmt.Sprintf("send-keys -t %s -l %s", paneID, shellutil.Quote(run.Text))
		} else {
			cmd = fmt.Sprintf("send-keys -t %s %s", paneID, run.Text)
		}
		execStart := time.Now()
		_, mutexWait, err := c.Execute(ctx, cmd)
		if err != nil {
			return timings, err
		}
		execDur := time.Since(execStart)
		timings.MutexWait += mutexWait
		timings.ExecuteNet += max(0, execDur-mutexWait)
	}
	return timings, nil
}

// SendEnter sends an Enter key to a pane.
func (c *Client) SendEnter(ctx context.Context, paneID string) error {
	_, _, err := c.Execute(ctx, fmt.Sprintf("send-keys -t %s Enter", paneID))
	return err
}

// ListWindows returns all windows in the current session.
func (c *Client) ListWindows(ctx context.Context) ([]WindowInfo, error) {
	output, _, err := c.Execute(ctx, "list-windows -F '#{window_id} #{window_name} #{pane_id}'")
	if err != nil {
		return nil, err
	}

	var windows []WindowInfo
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 3 {
			windows = append(windows, WindowInfo{
				WindowID:   parts[0],
				WindowName: parts[1],
				PaneID:     parts[2],
			})
		}
	}

	return windows, nil
}

// GetPaneInfo returns information about a specific pane.
func (c *Client) GetPaneInfo(ctx context.Context, paneID string) (pid int, title string, err error) {
	output, _, err := c.Execute(ctx, fmt.Sprintf("display-message -p -t %s '#{pane_pid} #{pane_title}'", paneID))
	if err != nil {
		return 0, "", err
	}

	parts := strings.SplitN(strings.TrimSpace(output), " ", 2)
	if len(parts) < 1 {
		return 0, "", fmt.Errorf("unexpected pane info: %q", output)
	}

	if _, err := fmt.Sscanf(parts[0], "%d", &pid); err != nil {
		return 0, "", fmt.Errorf("failed to parse pid: %w", err)
	}

	if len(parts) > 1 {
		title = parts[1]
	}

	return pid, title, nil
}

// ResizeWindow resizes a window to specific dimensions.
func (c *Client) ResizeWindow(ctx context.Context, windowID string, width, height int) error {
	_, _, err := c.Execute(ctx, fmt.Sprintf("resize-window -t %s -x %d -y %d", windowID, width, height))
	return err
}

// SetOption sets a tmux option.
func (c *Client) SetOption(ctx context.Context, option, value string) error {
	_, _, err := c.Execute(ctx, fmt.Sprintf("set-option %s %s", option, value))
	return err
}

// CapturePaneLines captures the last N lines from a pane.
// Returns the raw output including ANSI escape sequences (colors, formatting).
func (c *Client) CapturePaneLines(ctx context.Context, paneID string, lines int) (string, error) {
	// Use -e flag to include ANSI escape sequences (colors, bold, etc.)
	// Without -e, tmux strips all formatting from the output
	cmd := fmt.Sprintf("capture-pane -e -t %s -p -S -%d", paneID, lines)
	output, _, err := c.Execute(ctx, cmd)
	return output, err
}

// CapturePaneVisible captures only the visible screen of a pane (no scrollback).
// Returns the raw output including ANSI escape sequences (colors, formatting).
func (c *Client) CapturePaneVisible(ctx context.Context, paneID string) (string, error) {
	cmd := fmt.Sprintf("capture-pane -e -t %s -p", paneID)
	output, _, err := c.Execute(ctx, cmd)
	return output, err
}

// CursorState holds the cursor position and visibility for a pane.
type CursorState struct {
	X       int
	Y       int
	Visible bool
}

// GetCursorState returns the cursor position and visibility for a pane.
func (c *Client) GetCursorState(ctx context.Context, paneID string) (CursorState, error) {
	output, _, err := c.Execute(ctx, fmt.Sprintf("display-message -p -t %s '#{cursor_x} #{cursor_y} #{cursor_flag}'", paneID))
	if err != nil {
		return CursorState{}, fmt.Errorf("failed to get cursor state: %w", err)
	}
	parts := strings.Split(strings.TrimSpace(output), " ")
	if len(parts) != 3 {
		return CursorState{}, fmt.Errorf("unexpected cursor state format: %q", output)
	}
	var cs CursorState
	if _, err := fmt.Sscanf(parts[0], "%d", &cs.X); err != nil {
		return CursorState{}, fmt.Errorf("failed to parse cursor_x: %w", err)
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &cs.Y); err != nil {
		return CursorState{}, fmt.Errorf("failed to parse cursor_y: %w", err)
	}
	// cursor_flag: 0 = hidden, 1 = visible
	cs.Visible = parts[2] == "1"
	return cs, nil
}

// GetCursorPosition returns the cursor position (x, y) for a pane.
func (c *Client) GetCursorPosition(ctx context.Context, paneID string) (x, y int, err error) {
	cs, err := c.GetCursorState(ctx, paneID)
	if err != nil {
		return 0, 0, err
	}
	return cs.X, cs.Y, nil
}

// WaitForReady waits for the control mode session to be ready.
// This is called after connection to ensure tmux is responsive.
func (c *Client) WaitForReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Send a simple command and verify we get a response
	_, _, err := c.Execute(ctx, "display-message -p 'ready'")
	return err
}

// FindWindowByName finds a window by name.
func (c *Client) FindWindowByName(ctx context.Context, name string) (*WindowInfo, error) {
	windows, err := c.ListWindows(ctx)
	if err != nil {
		return nil, err
	}
	for _, w := range windows {
		if w.WindowName == name {
			return &w, nil
		}
	}
	return nil, nil
}

// ExtractPaneIDFromOutput extracts a pane ID from %output line.
var paneIDRegex = regexp.MustCompile(`%\d+`)

// GetWindowPaneID returns the pane ID for a window.
func (c *Client) GetWindowPaneID(ctx context.Context, windowID string) (string, error) {
	output, _, err := c.Execute(ctx, fmt.Sprintf("list-panes -t %s -F '#{pane_id}'", windowID))
	if err != nil {
		return "", err
	}
	paneID := strings.TrimSpace(output)
	if paneID == "" {
		return "", fmt.Errorf("no pane found for window %s", windowID)
	}
	// Return first pane if multiple
	if idx := strings.Index(paneID, "\n"); idx > 0 {
		paneID = paneID[:idx]
	}
	return paneID, nil
}

// tmuxQuote quotes a string for safe use in tmux commands using double quotes.
// Unlike shellutil.Quote (which uses the '\” trick that tmux doesn't support),
// tmux double quotes handle embedded single quotes naturally.
// In tmux double quotes: \ " and $ need to be escaped.
func tmuxQuote(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "$", "\\$")
	return "\"" + s + "\""
}

// RunCommand executes a command in a hidden tmux window and returns its output.
// Instead of embedding the command in the new-window invocation (which has tmux
// quoting issues with single quotes in VCS commands), it creates a window with
// the default shell, types the command via send-keys, and polls capture-pane.
func (c *Client) RunCommand(ctx context.Context, workdir, command string) (string, error) {
	beginSentinel := fmt.Sprintf("__SCHMUX_BEGIN_%s__", uuid.New().String()[:8])
	endSentinel := fmt.Sprintf("__SCHMUX_END_%s__", uuid.New().String()[:8])

	if c.logger != nil {
		c.logger.Info("RunCommand", "workdir", workdir, "cmd", command)
	}

	// Create a hidden window with the default shell (no command = default shell).
	// This avoids all tmux command-quoting issues because we don't embed the
	// VCS command in the new-window invocation.
	output, _, err := c.Execute(ctx, "new-window -d -n schmux-cmd -P -F '#{window_id} #{pane_id}'")
	if err != nil {
		return "", fmt.Errorf("failed to create command window: %w", err)
	}

	parts := strings.Fields(strings.TrimSpace(output))
	if len(parts) != 2 {
		return "", fmt.Errorf("unexpected new-window output: %q", output)
	}
	windowID := parts[0]
	paneID := parts[1]

	if c.logger != nil {
		c.logger.Info("RunCommand: created window", "window", windowID, "pane", paneID)
	}

	// Ensure the window is always cleaned up
	defer func() {
		killCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if killErr := c.KillWindow(killCtx, windowID); killErr != nil {
			if c.logger != nil {
				c.logger.Error("RunCommand: failed to kill window", "window", windowID, "err", killErr)
			}
		} else {
			if c.logger != nil {
				c.logger.Debug("RunCommand: killed window", "window", windowID)
			}
		}
	}()

	// Brief wait for the shell to initialize
	time.Sleep(200 * time.Millisecond)

	// Build the full command to type into the shell.
	// Begin/end sentinels on their own lines let us cleanly extract just the output,
	// ignoring the shell's command echo line.
	fullCmd := fmt.Sprintf("echo %s; cd %s && %s; echo %s",
		beginSentinel, shellutil.Quote(workdir), command, endSentinel)

	if c.logger != nil {
		c.logger.Debug("RunCommand: typing into pane", "pane", paneID, "cmd", fullCmd)
	}

	// Send command as literal keystrokes via send-keys -l.
	// This bypasses tmux's command parser entirely — the text goes straight to the
	// shell in the pane. tmuxQuote handles only the tmux protocol quoting layer.
	_, _, err = c.Execute(ctx, fmt.Sprintf("send-keys -t %s -l %s", paneID, tmuxQuote(fullCmd)))
	if err != nil {
		return "", fmt.Errorf("failed to send command keys: %w", err)
	}
	// Press Enter to execute
	_, _, err = c.Execute(ctx, fmt.Sprintf("send-keys -t %s Enter", paneID))
	if err != nil {
		return "", fmt.Errorf("failed to send Enter: %w", err)
	}

	// Poll capture-pane until end sentinel appears on its own line
	const pollInterval = 200 * time.Millisecond
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	beginMarker := "\n" + beginSentinel + "\n"

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-c.closeCh:
			return "", fmt.Errorf("client closed")
		case <-ticker.C:
			captured, _, captureErr := c.Execute(ctx, fmt.Sprintf("capture-pane -t %s -p -S -50000", paneID))
			if captureErr != nil {
				return "", fmt.Errorf("capture-pane failed: %w", captureErr)
			}

			// Find end sentinel on its own line (last occurrence to skip command echo)
			endIdx := strings.LastIndex(captured, "\n"+endSentinel)
			if endIdx < 0 {
				continue
			}

			// Find begin sentinel on its own line
			beginIdx := strings.Index(captured, beginMarker)
			if beginIdx < 0 {
				continue
			}

			// Extract content between sentinels
			contentStart := beginIdx + len(beginMarker)
			// When the command produces no output, the end marker immediately
			// follows the begin marker and contentStart can exceed endIdx.
			var result string
			if contentStart <= endIdx {
				result = strings.TrimSpace(captured[contentStart:endIdx])
			}

			if c.logger != nil {
				c.logger.Debug("RunCommand: captured output", "bytes", len(result), "pane", paneID)
			}
			return result, nil
		}
	}
}
