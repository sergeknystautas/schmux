package controlmode

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

// Client provides a high-level interface for tmux control mode.
// It sends commands and correlates responses using a FIFO queue since tmux
// assigns sequential command IDs starting from 0, not using our local IDs.
//
// Stale response handling has three phases:
//  1. Pre-epoch (before firstCommandSent): responses buffered from previous
//     sessions are discarded by drainBufferedResponses and the pre-epoch check.
//  2. Pre-sync (after firstCommandSent, before MarkSynced): late stale responses
//     (e.g., attach-session's own %begin/%end) that arrive after the first
//     command is sent but before the FIFO queue is aligned are discarded.
//  3. Post-sync (after MarkSynced): empty-queue responses are real FIFO desyncs.
type Client struct {
	stdin   io.Writer
	stdinMu sync.Mutex // Protects stdin writes to prevent interleaving

	parser *Parser
	logger *log.Logger

	// Command correlation - FIFO queue since tmux assigns sequential IDs
	pendingQueue []pendingCommand
	pendingMu    sync.Mutex

	// Epoch tracking: responses arriving before the first command is sent
	// are stale (from a previous control mode session) and are discarded.
	// After firstCommandSent but before synced, responses arriving with an
	// empty queue are late stale responses (e.g., from the attach-session
	// command) rather than real FIFO desyncs.
	// Protected by pendingMu to ensure atomicity with queue state checks.
	firstCommandSent bool
	synced           bool
	discardedStale   atomic.Int64

	// Response channel registry to prevent leaks on timeout
	respChans   map[chan CommandResponse]bool
	respChansMu sync.Mutex

	// Output subscriptions by pane ID
	outputSubs   map[string][]*outputSubscription
	outputSubsMu sync.RWMutex

	// Retained for diagnostics compatibility. Output fan-out now applies
	// backpressure, so the counter remains zero.
	droppedFanOut atomic.Int64

	// Pause notifications (pane IDs paused by tmux when output falls behind)
	pauseCh chan string

	// Paste-buffer-changed notifications (buffer names added/modified). Some
	// TUIs detect tmux control mode and bypass OSC 52, instead writing
	// clipboard text to tmux's internal paste buffer via `tmux load-buffer -`
	// (or set-buffer / copy-mode Enter). The OSC 52 byte path never fires for
	// these. tmux reports each buffer add/modify via %paste-buffer-changed
	// <name>, which we surface here so a higher layer can fetch the buffer
	// with show-buffer and feed it through the same clipboardState pipeline
	// as OSC 52.
	pasteBufferCh chan string

	// Lifecycle
	running   bool
	closeCh   chan struct{}
	closeOnce sync.Once
}

// pendingCommand is a sent command waiting for its response.
type pendingCommand struct {
	ch chan CommandResponse
	// want, when set, is the only content the command accepts. Other replies
	// are stale and are discarded while the command keeps its place.
	want string
}

type outputSubscription struct {
	ch        chan OutputEvent
	done      chan struct{}
	closeOnce sync.Once
}

// NewClient creates a new control mode client.
// stdin is used to send commands, parser reads from stdout.
// logger is an optional structured logger; if nil, logging is disabled.
func NewClient(stdin io.Writer, parser *Parser, logger *log.Logger) *Client {
	return &Client{
		stdin:         stdin,
		parser:        parser,
		logger:        logger,
		pendingQueue:  make([]pendingCommand, 0),
		respChans:     make(map[chan CommandResponse]bool),
		outputSubs:    make(map[string][]*outputSubscription),
		pauseCh:       make(chan string, 10),
		pasteBufferCh: make(chan string, 32),
		closeCh:       make(chan struct{}),
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

// MarkSynced signals that the caller's sync protocol has completed and the
// FIFO queue is now aligned. After this, any response arriving with an empty
// queue is a real FIFO desync (not a late stale response from attach-session).
func (c *Client) MarkSynced() {
	c.pendingMu.Lock()
	c.synced = true
	c.pendingMu.Unlock()
}

// syncSentinel is the text Sync asks tmux to echo back.
const syncSentinel = "__SCHMUX_SYNC__"

// Sync aligns the FIFO queue: it asks tmux to echo a sentinel, discards the
// replies that arrive before it, and marks the client synced. It sends one
// command, so no reply is left outstanding for the next command to receive.
// Resending the sentinel instead would leave its second reply behind.
func (c *Client) Sync(ctx context.Context) error {
	cmd := fmt.Sprintf("display-message -p '%s'", syncSentinel)
	if _, _, err := c.execute(ctx, cmd, syncSentinel); err != nil {
		return err
	}
	c.MarkSynced()
	return nil
}

// Close shuts down the client. Safe to call multiple times.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		c.pendingMu.Lock()
		c.running = false
		close(c.closeCh)
		// Send error responses to any pending commands still waiting
		for _, pending := range c.pendingQueue {
			// Send error response - channel is buffered so won't block
			// Don't close the channel - caller may still be in select waiting for it
			pending.ch <- CommandResponse{Success: false, Content: "client closed"}
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
	return c.execute(ctx, cmd, "")
}

// execute sends a command and waits for its response. A non-empty want makes
// the command accept only a reply with that content (see pendingCommand).
func (c *Client) execute(ctx context.Context, cmd, want string) (string, time.Duration, error) {
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

	// tmux control mode uses newlines as command terminators — each line is
	// one command. Embedded newlines split a single command into multiple
	// commands, corrupting the FIFO protocol. This happens when shell-quoted
	// arguments contain literal newlines (e.g., persona prompts injected via
	// --append-system-prompt). Collapse to spaces.
	cmd = strings.ReplaceAll(cmd, "\n", " ")

	// CRITICAL: Queue append and stdin write must be atomic. tmux assigns
	// sequential command IDs based on the order commands arrive on stdin.
	// Responses are matched to callers in FIFO order from pendingQueue.
	// If goroutine A appends to the queue first but goroutine B writes to
	// stdin first, the queue order [A,B] won't match the send order [B,A],
	// causing every subsequent response to be delivered to the wrong caller.
	//
	// Fix: acquire stdinMu first (serializes senders), then append to queue
	// under pendingMu, then write. Whoever sends first also queues first.
	mutexStart := time.Now()
	c.stdinMu.Lock()
	mutexWait := time.Since(mutexStart)

	c.pendingMu.Lock()
	if !c.running {
		c.pendingMu.Unlock()
		c.stdinMu.Unlock()
		return "", mutexWait, fmt.Errorf("client not running")
	}
	c.pendingQueue = append(c.pendingQueue, pendingCommand{ch: respCh, want: want})
	c.firstCommandSent = true
	c.pendingMu.Unlock()

	_, err := fmt.Fprintf(c.stdin, "%s\n", cmd)
	c.stdinMu.Unlock()
	if err != nil {
		// Failed to send - leave channel in queue but don't listen to it
		// processResponses will still try to deliver, but we won't be waiting
		return "", mutexWait, fmt.Errorf("failed to send command: %w", err)
	}

	// Log non-trivial commands (skip per-keystroke send-keys and health probe noise).
	if c.logger != nil && !strings.HasPrefix(cmd, "send-keys") && !strings.HasPrefix(cmd, "display-message") {
		c.pendingMu.Lock()
		qDepth := len(c.pendingQueue)
		c.pendingMu.Unlock()
		c.logger.Debug("Execute: sent", "cmd", cmd, "mutex_wait", mutexWait, "queue_depth", qDepth)
	}

	// Wait for response
	sendTime := time.Now()
	select {
	case resp := <-respCh:
		rtt := time.Since(sendTime)
		if !resp.Success {
			if c.logger != nil {
				c.logger.Error("command failed", "cmd", cmd, "err", resp.Content, "rtt", rtt)
			}
			return "", mutexWait, fmt.Errorf("command failed: %s", resp.Content)
		}
		if c.logger != nil && rtt > 2*time.Second {
			c.logger.Warn("Execute: slow response", "cmd", cmd, "rtt", rtt)
		}
		return resp.Content, mutexWait, nil
	case <-ctx.Done():
		elapsed := time.Since(sendTime)
		if c.logger != nil {
			c.pendingMu.Lock()
			qDepth := len(c.pendingQueue)
			c.pendingMu.Unlock()
			c.logger.Error("command timeout", "cmd", cmd, "waited", elapsed, "queue_depth", qDepth)
		}
		return "", mutexWait, ctx.Err()
	case <-c.closeCh:
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
				pending := c.pendingQueue[0]
				if pending.want != "" && strings.TrimSpace(resp.Content) != pending.want {
					// A stale reply ahead of the sync reply (typically the
					// attach-session response). The sync command keeps waiting.
					c.pendingMu.Unlock()
					c.discardedStale.Add(1)
					if c.logger != nil {
						c.logger.Debug("discarded stale response (during sync)", "cmd_id", resp.CommandID, "content_len", len(resp.Content))
					}
					continue
				}
				c.pendingQueue = c.pendingQueue[1:]
				c.pendingMu.Unlock()

				// Send to buffered channel - won't block even if nobody is listening
				// Cancelled commands simply won't read from their channel
				pending.ch <- resp
			} else if !c.firstCommandSent {
				// No command has been sent yet — this response is stale,
				// from a previous control mode session (e.g., daemon restart).
				c.pendingMu.Unlock()
				c.discardedStale.Add(1)
				if c.logger != nil {
					c.logger.Debug("discarded stale response (pre-epoch)", "cmd_id", resp.CommandID)
				}
			} else if !c.synced {
				// Between first command sent and sync completion. This is a
				// late stale response (typically the attach-session response)
				// that arrived after the sync command was already queued.
				c.pendingMu.Unlock()
				c.discardedStale.Add(1)
				if c.logger != nil {
					c.logger.Debug("discarded late stale response (pre-sync)", "cmd_id", resp.CommandID, "content_len", len(resp.Content))
				}
			} else {
				c.pendingMu.Unlock()
				if c.logger != nil {
					c.logger.Error("FIFO desync: response arrived with empty queue", "cmd_id", resp.CommandID, "success", resp.Success, "content_len", len(resp.Content))
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
			if !c.deliverOutput(event) {
				return
			}
		case <-c.closeCh:
			return
		}
	}
}

func (c *Client) deliverOutput(event OutputEvent) bool {
	c.outputSubsMu.RLock()
	subs := append([]*outputSubscription(nil), c.outputSubs[event.PaneID]...)
	c.outputSubsMu.RUnlock()
	for _, sub := range subs {
		select {
		case sub.ch <- event:
		case <-sub.done:
		case <-c.closeCh:
			return false
		}
	}
	return true
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
			case "paste-buffer-changed", "paste-changed":
				// tmux fires this whenever a paste buffer is added or modified
				// (load-buffer, set-buffer, copy-mode Enter). Args[0] is the
				// buffer name (e.g. "buffer0"). Drop on overflow — same
				// pattern as pauseCh — since the dashboard can recover the
				// content via show-buffer on demand.
				//
				// Two event names because the notification was renamed in
				// tmux 3.4 (commit 8edece2c, Oct 2022): older tmux (3.3a and
				// below, e.g. Debian bookworm) emits %paste-changed; tmux
				// >= 3.4 emits %paste-buffer-changed. We accept both so the
				// daemon works on either.
				if len(event.Args) >= 1 {
					select {
					case c.pasteBufferCh <- event.Args[0]:
					default:
						if c.logger != nil {
							c.logger.Debug("paste-buffer-changed dropped (channel full)", "buffer", event.Args[0])
						}
					}
				}
				// paste-buffer-deleted / paste-deleted is intentionally
				// ignored: we only act on additions/modifications.
			}
		case <-c.closeCh:
			return
		}
	}
}

// SubscribeOutput subscribes to output from a specific pane.
// Returns a channel that receives output events.
func (c *Client) SubscribeOutput(paneID string) <-chan OutputEvent {
	sub := &outputSubscription{
		ch:   make(chan OutputEvent, 1000),
		done: make(chan struct{}),
	}
	c.outputSubsMu.Lock()
	c.outputSubs[paneID] = append(c.outputSubs[paneID], sub)
	c.outputSubsMu.Unlock()
	return sub.ch
}

// UnsubscribeOutput removes a subscription and releases a producer waiting on
// that subscriber. The data channel stays open: callers already own their stop
// condition, and closing it here would race with a selected send.
func (c *Client) UnsubscribeOutput(paneID string, ch <-chan OutputEvent) {
	c.outputSubsMu.Lock()
	subs := c.outputSubs[paneID]
	var removed *outputSubscription
	for i, sub := range subs {
		if sub.ch == ch {
			c.outputSubs[paneID] = append(subs[:i], subs[i+1:]...)
			removed = sub
			break
		}
	}
	c.outputSubsMu.Unlock()
	if removed != nil {
		removed.closeOnce.Do(func() { close(removed.done) })
	}
}

// DroppedFanOut is retained for diagnostics compatibility. Output fan-out now
// applies backpressure instead of dropping, so this counter remains zero.
func (c *Client) DroppedFanOut() int64 {
	return c.droppedFanOut.Load()
}

// Pauses returns a channel that receives pane IDs when tmux pauses output
// delivery for that pane (because the control mode client fell behind).
func (c *Client) Pauses() <-chan string {
	return c.pauseCh
}

// PasteBuffers returns a channel that receives buffer names when tmux fires
// %paste-buffer-changed (load-buffer / set-buffer / copy-mode Enter). The
// receiver should fetch the buffer content via `show-buffer -b <name>` and
// feed it through the same clipboard pipeline as OSC 52. Drops on overflow.
func (c *Client) PasteBuffers() <-chan string {
	return c.pasteBufferCh
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
		if run.Hex {
			cmd = fmt.Sprintf("send-keys -H -t %s %s", paneID, run.Text)
		} else if run.Literal {
			// `--` terminates option parsing so a run.Text starting with `-`
			// (e.g. a pasted line like "-world") is treated as the key
			// argument rather than a flag cluster. Without it, tmux rejects
			// the command with "unknown flag -w" and Client.SendKeys aborts
			// the remainder of the paste.
			cmd = fmt.Sprintf("send-keys -t %s -l -- %s", paneID, shellutil.Quote(run.Text))
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

// ResizeWindow resizes a window to specific dimensions.
func (c *Client) ResizeWindow(ctx context.Context, windowID string, width, height int) error {
	_, _, err := c.Execute(ctx, fmt.Sprintf("resize-window -t %s -x %d -y %d", windowID, width, height))
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

// CursorState holds the cursor position, visibility, and terminal mode state for a pane.
type CursorState struct {
	X           int
	Y           int
	Visible     bool
	AlternateOn bool // true when the pane is in alternate screen mode (TUI apps)
	// Mouse tracking modes — set when the application running in the pane has
	// enabled mouse reporting via escape sequences.
	MouseStandard bool // mode 1000: basic press/release tracking
	MouseButton   bool // mode 1002: button-event tracking (press/release + drag)
	MouseAny      bool // mode 1003: any-event tracking (all motion)
	MouseSGR      bool // mode 1006: SGR extended encoding
}

// GetCursorState returns the cursor position and visibility for a pane.
func (c *Client) GetCursorState(ctx context.Context, paneID string) (CursorState, error) {
	output, _, err := c.Execute(ctx, fmt.Sprintf(
		"display-message -p -t %s '#{cursor_x} #{cursor_y} #{cursor_flag} #{alternate_on} #{mouse_standard_flag} #{mouse_button_flag} #{mouse_any_flag} #{mouse_sgr_flag}'",
		paneID,
	))
	if err != nil {
		return CursorState{}, fmt.Errorf("failed to get cursor state: %w", err)
	}
	parts := strings.Split(strings.TrimSpace(output), " ")
	if len(parts) < 3 {
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
	// Extended fields — parsed tolerantly for backward compatibility with
	// older tmux versions that may not support all format variables.
	if len(parts) >= 4 {
		cs.AlternateOn = parts[3] == "1"
	}
	if len(parts) >= 8 {
		cs.MouseStandard = parts[4] == "1"
		cs.MouseButton = parts[5] == "1"
		cs.MouseAny = parts[6] == "1"
		cs.MouseSGR = parts[7] == "1"
	}
	return cs, nil
}
