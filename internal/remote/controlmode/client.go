package controlmode

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

// Client provides a high-level interface for tmux control mode.
// It sends commands and correlates responses using a FIFO queue since tmux
// assigns sequential command IDs starting from 0, not using our local IDs.
type Client struct {
	stdin   io.Writer
	stdinMu sync.Mutex // Protects stdin writes to prevent interleaving
	parser  *Parser

	// Command correlation - FIFO queue since tmux assigns sequential IDs
	pendingQueue []chan CommandResponse
	pendingMu    sync.Mutex

	// Response channel registry to prevent leaks on timeout
	respChans   map[chan CommandResponse]bool
	respChansMu sync.Mutex

	// Output subscriptions by pane ID
	outputSubs   map[string][]chan OutputEvent
	outputSubsMu sync.RWMutex

	// Lifecycle
	running bool
	closeCh chan struct{}
}

// WindowInfo represents information about a tmux window.
type WindowInfo struct {
	WindowID   string // e.g., "@3"
	WindowName string
	PaneID     string // e.g., "%5"
}

// NewClient creates a new control mode client.
// stdin is used to send commands, parser reads from stdout.
func NewClient(stdin io.Writer, parser *Parser) *Client {
	return &Client{
		stdin:        stdin,
		parser:       parser,
		pendingQueue: make([]chan CommandResponse, 0),
		respChans:    make(map[chan CommandResponse]bool),
		outputSubs:   make(map[string][]chan OutputEvent),
		closeCh:      make(chan struct{}),
	}
}

// Start begins processing parser output.
// Call this in a goroutine before sending commands.
func (c *Client) Start() {
	c.running = true
	go c.processResponses()
	go c.processOutput()
	go c.processEvents()
}

// Close shuts down the client.
func (c *Client) Close() {
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
}

// Execute sends a command and waits for the response.
// FIFO ordering is critical: responses are matched to commands in order sent.
// Timeout/cancellation does NOT remove from queue to prevent misdelivery.
func (c *Client) Execute(ctx context.Context, cmd string) (string, error) {
	// Create response channel
	respCh := make(chan CommandResponse, 1)

	// Register channel in registry to track for cleanup
	c.respChansMu.Lock()
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
		return "", fmt.Errorf("client not running")
	}
	c.pendingQueue = append(c.pendingQueue, respCh)
	c.pendingMu.Unlock()

	// Send command (tmux control mode assigns IDs automatically based on order)
	// Commands are matched to responses in FIFO order
	// Protect stdin write with mutex to prevent concurrent command interleaving
	c.stdinMu.Lock()
	_, err := fmt.Fprintf(c.stdin, "%s\n", cmd)
	c.stdinMu.Unlock()
	if err != nil {
		// Failed to send - leave channel in queue but don't listen to it
		// processResponses will still try to deliver, but we won't be waiting
		return "", fmt.Errorf("failed to send command: %w", err)
	}

	// Wait for response
	select {
	case resp := <-respCh:
		if !resp.Success {
			fmt.Printf("[controlmode] command failed: %s\nError: %s\n", cmd, resp.Content)
			return "", fmt.Errorf("command failed: %s", resp.Content)
		}
		return resp.Content, nil
	case <-ctx.Done():
		// DO NOT remove from queue or close channel - just stop listening
		// The response will still arrive and be sent to this buffered channel (won't block)
		// Since we're no longer listening, the value is effectively discarded
		// Channel will be deregistered and cleaned up by defer
		fmt.Printf("[controlmode] command timeout: %s\n", cmd)
		return "", ctx.Err()
	case <-c.closeCh:
		// Client is closing, channel will be cleaned up by defer
		return "", fmt.Errorf("client closed")
	}
}

// processResponses routes responses to waiting commands in FIFO order.
// Handles cancelled commands (nobody listening) by sending to buffered channel anyway.
func (c *Client) processResponses() {
	for {
		select {
		case resp, ok := <-c.parser.Responses():
			if !ok {
				return
			}
			// Deliver to first waiting command (FIFO order)
			c.pendingMu.Lock()
			if len(c.pendingQueue) > 0 {
				ch := c.pendingQueue[0]
				c.pendingQueue = c.pendingQueue[1:]
				c.pendingMu.Unlock()

				// Send to buffered channel - won't block even if nobody is listening
				// Cancelled commands simply won't read from their channel
				ch <- resp
			} else {
				c.pendingMu.Unlock()
				fmt.Printf("[controlmode] WARNING: received response but no pending commands (id=%d)\n", resp.CommandID)
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
				}
			}
			c.outputSubsMu.RUnlock()
		case <-c.closeCh:
			return
		}
	}
}

// processEvents handles async events (not currently used but available).
func (c *Client) processEvents() {
	for {
		select {
		case _, ok := <-c.parser.Events():
			if !ok {
				return
			}
			// Could broadcast events to subscribers if needed
		case <-c.closeCh:
			return
		}
	}
}

// SubscribeOutput subscribes to output from a specific pane.
// Returns a channel that receives output events.
func (c *Client) SubscribeOutput(paneID string) <-chan OutputEvent {
	ch := make(chan OutputEvent, 100)
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

// CreateWindow creates a new window with a command.
// Returns the window ID and pane ID.
func (c *Client) CreateWindow(ctx context.Context, name, workdir, command string) (windowID, paneID string, err error) {
	// Build command
	cmd := fmt.Sprintf("new-window -n %s -c %s -P -F '#{window_id} #{pane_id}' %s",
		shellutil.Quote(name), shellutil.Quote(workdir), shellutil.Quote(command))

	output, err := c.Execute(ctx, cmd)
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
	_, err := c.Execute(ctx, fmt.Sprintf("kill-window -t %s", windowID))
	return err
}

// SendKeys sends keys to a pane.
// Splits input into printable text (sent with -l for literal mode) and
// special characters (sent as tmux key names). This is necessary because
// tmux control mode command parsing can mishandle raw control characters
// embedded in the command string.
func (c *Client) SendKeys(ctx context.Context, paneID, keys string) error {
	i := 0
	for i < len(keys) {
		// Find run of printable characters (ASCII 32-126)
		j := i
		for j < len(keys) && keys[j] >= 32 && keys[j] < 127 {
			j++
		}

		// Send printable text run with -l (literal mode)
		if j > i {
			if _, err := c.Execute(ctx, fmt.Sprintf("send-keys -t %s -l %s", paneID, shellutil.Quote(keys[i:j]))); err != nil {
				return err
			}
			i = j
			continue
		}

		// Handle special character at position i
		ch := keys[i]
		var keyName string
		advance := 1

		switch ch {
		case '\r', '\n':
			keyName = "Enter"
		case '\t':
			keyName = "Tab"
		case 127:
			keyName = "BSpace"
		case '\x1b':
			// Check for escape sequences (e.g., arrow keys: ESC [ A)
			if i+2 < len(keys) && keys[i+1] == '[' {
				// CSI sequence: ESC [ ... <final byte 0x40-0x7E>
				end := i + 2
				for end < len(keys) && (keys[end] < 0x40 || keys[end] > 0x7E) {
					end++
				}
				if end < len(keys) {
					// Map common CSI sequences to tmux key names
					seq := keys[i : end+1]
					switch seq {
					case "\x1b[A":
						keyName = "Up"
					case "\x1b[B":
						keyName = "Down"
					case "\x1b[C":
						keyName = "Right"
					case "\x1b[D":
						keyName = "Left"
					case "\x1b[H":
						keyName = "Home"
					case "\x1b[F":
						keyName = "End"
					case "\x1b[2~":
						keyName = "Insert"
					case "\x1b[3~":
						keyName = "DC" // Delete
					case "\x1b[5~":
						keyName = "PageUp"
					case "\x1b[6~":
						keyName = "PageDown"
					case "\x1b[Z":
						keyName = "BTab" // Shift-Tab
					default:
						// Unknown CSI sequence — skip it
						advance = end + 1 - i
						i += advance
						continue
					}
					advance = end + 1 - i
				} else {
					keyName = "Escape"
				}
			} else if i+2 < len(keys) && keys[i+1] == 'O' {
				// SS3 sequence (e.g., ESC O P for F1)
				switch keys[i+2] {
				case 'P':
					keyName = "F1"
				case 'Q':
					keyName = "F2"
				case 'R':
					keyName = "F3"
				case 'S':
					keyName = "F4"
				default:
					keyName = "Escape"
					advance = 1
				}
				if keyName != "Escape" {
					advance = 3
				}
			} else {
				keyName = "Escape"
			}
		default:
			if ch < 32 {
				// Control characters: Ctrl-A = 0x01, Ctrl-B = 0x02, etc.
				keyName = fmt.Sprintf("C-%c", 'a'+ch-1)
			}
		}

		if keyName != "" {
			if _, err := c.Execute(ctx, fmt.Sprintf("send-keys -t %s %s", paneID, keyName)); err != nil {
				return err
			}
		}
		i += advance
	}
	return nil
}

// SendEnter sends an Enter key to a pane.
func (c *Client) SendEnter(ctx context.Context, paneID string) error {
	_, err := c.Execute(ctx, fmt.Sprintf("send-keys -t %s Enter", paneID))
	return err
}

// ListWindows returns all windows in the current session.
func (c *Client) ListWindows(ctx context.Context) ([]WindowInfo, error) {
	output, err := c.Execute(ctx, "list-windows -F '#{window_id} #{window_name} #{pane_id}'")
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
	output, err := c.Execute(ctx, fmt.Sprintf("display-message -p -t %s '#{pane_pid} #{pane_title}'", paneID))
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
	_, err := c.Execute(ctx, fmt.Sprintf("resize-window -t %s -x %d -y %d", windowID, width, height))
	return err
}

// SetOption sets a tmux option.
func (c *Client) SetOption(ctx context.Context, option, value string) error {
	_, err := c.Execute(ctx, fmt.Sprintf("set-option %s %s", option, value))
	return err
}

// CapturePaneLines captures the last N lines from a pane.
// Returns the raw output including ANSI escape sequences (colors, formatting).
func (c *Client) CapturePaneLines(ctx context.Context, paneID string, lines int) (string, error) {
	// Use -e flag to include ANSI escape sequences (colors, bold, etc.)
	// Without -e, tmux strips all formatting from the output
	cmd := fmt.Sprintf("capture-pane -e -t %s -p -S -%d", paneID, lines)
	return c.Execute(ctx, cmd)
}

// WaitForReady waits for the control mode session to be ready.
// This is called after connection to ensure tmux is responsive.
func (c *Client) WaitForReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Send a simple command and verify we get a response
	_, err := c.Execute(ctx, "display-message -p 'ready'")
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
	output, err := c.Execute(ctx, fmt.Sprintf("list-panes -t %s -F '#{pane_id}'", windowID))
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

	fmt.Printf("[controlmode] RunCommand: workdir=%s cmd=%s\n", workdir, command)

	// Create a hidden window with the default shell (no command = default shell).
	// This avoids all tmux command-quoting issues because we don't embed the
	// VCS command in the new-window invocation.
	output, err := c.Execute(ctx, "new-window -d -n schmux-cmd -P -F '#{window_id} #{pane_id}'")
	if err != nil {
		return "", fmt.Errorf("failed to create command window: %w", err)
	}

	parts := strings.Fields(strings.TrimSpace(output))
	if len(parts) != 2 {
		return "", fmt.Errorf("unexpected new-window output: %q", output)
	}
	windowID := parts[0]
	paneID := parts[1]

	fmt.Printf("[controlmode] RunCommand: created window=%s pane=%s\n", windowID, paneID)

	// Ensure the window is always cleaned up
	defer func() {
		killCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if killErr := c.KillWindow(killCtx, windowID); killErr != nil {
			fmt.Printf("[controlmode] RunCommand: failed to kill window %s: %v\n", windowID, killErr)
		} else {
			fmt.Printf("[controlmode] RunCommand: killed window %s\n", windowID)
		}
	}()

	// Brief wait for the shell to initialize
	time.Sleep(200 * time.Millisecond)

	// Build the full command to type into the shell.
	// Begin/end sentinels on their own lines let us cleanly extract just the output,
	// ignoring the shell's command echo line.
	fullCmd := fmt.Sprintf("echo %s; cd %s && %s; echo %s",
		beginSentinel, shellutil.Quote(workdir), command, endSentinel)

	fmt.Printf("[controlmode] RunCommand: typing into pane %s: %s\n", paneID, fullCmd)

	// Send command as literal keystrokes via send-keys -l.
	// This bypasses tmux's command parser entirely — the text goes straight to the
	// shell in the pane. tmuxQuote handles only the tmux protocol quoting layer.
	_, err = c.Execute(ctx, fmt.Sprintf("send-keys -t %s -l %s", paneID, tmuxQuote(fullCmd)))
	if err != nil {
		return "", fmt.Errorf("failed to send command keys: %w", err)
	}
	// Press Enter to execute
	_, err = c.Execute(ctx, fmt.Sprintf("send-keys -t %s Enter", paneID))
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
			captured, captureErr := c.Execute(ctx, fmt.Sprintf("capture-pane -t %s -p -S -50000", paneID))
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
			result := strings.TrimSpace(captured[contentStart:endIdx])

			fmt.Printf("[controlmode] RunCommand: captured %d bytes from pane %s\n", len(result), paneID)
			return result, nil
		}
	}
}
