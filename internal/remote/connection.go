// Package remote provides remote workspace management via tmux control mode.
package remote

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/creack/pty"
	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
	"github.com/sergeknystautas/schmux/internal/state"
)

const (
	// DefaultHostExpiry is how long a remote host connection is valid.
	DefaultHostExpiry = 12 * time.Hour

	// ControlModeReadyTimeout is how long to wait for control mode to be ready.
	ControlModeReadyTimeout = 30 * time.Second
)

// PendingSession represents a session waiting for connection to be ready.
type PendingSession struct {
	SessionID  string
	Name       string
	WorkDir    string
	Command    string
	CompleteCh chan PendingSessionResult
}

// PendingSessionResult contains the result of a queued session creation.
type PendingSessionResult struct {
	WindowID string
	PaneID   string
	Error    error
}

// Connection represents a connection to a remote host via tmux control mode.
type Connection struct {
	host   *state.RemoteHost
	flavor *config.RemoteFlavor
	cmd    *exec.Cmd
	client *controlmode.Client
	parser *controlmode.Parser

	// PTY for interactive terminal (used during provisioning for auth prompts)
	pty    *os.File
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	// Parsed from remote connection output
	hostname string
	uuid     string

	// Custom hostname regex (if set, overrides the default hostnameRegex)
	customHostnameRegex *regexp.Regexp

	// Provisioning session ID (local tmux session for interactive terminal)
	provisioningSessionID string

	// Output buffer for provisioning (protected by provisioningMu)
	provisioningOutput strings.Builder
	provisioningMu     sync.Mutex

	// Session queuing during provisioning
	pendingSessions   []PendingSession
	pendingSessionsMu sync.Mutex

	// Synchronization
	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once

	// Callbacks
	onStatusChange func(hostID, status string)
	onProgress     func(message string)

	// Pipe for forwarding PTY data to control mode parser.
	// parseProvisioningOutput is the sole PTY reader and tees data here.
	controlPipeWriter *io.PipeWriter

	// PTY output subscribers for WebSocket terminal streaming
	ptySubscribers   []chan []byte
	ptySubscribersMu sync.Mutex
}

// ConnectionConfig holds configuration for creating a connection.
type ConnectionConfig struct {
	FlavorID         string
	Flavor           string // The flavor/environment identifier
	DisplayName      string
	WorkspacePath    string
	VCS              string
	ConnectCommand   string
	ReconnectCommand string
	ProvisionCommand string
	HostnameRegex    string // Custom regex for hostname extraction (first capture group)
	OnStatusChange   func(hostID, status string)
	OnProgress       func(message string)
}

// Regexes for parsing remote connection output
// These can be customized based on your remote infrastructure
var (
	// Matches: Establish ControlMaster connection to <hostname>
	hostnameRegex = regexp.MustCompile(`Establish ControlMaster connection to (\S+)`)
	// Matches: uuid: <identifier> or similar patterns
	uuidRegex = regexp.MustCompile(`(?:uuid|UUID|session-id):\s*(\S+)`)
)

// getHostnameRegex returns the custom hostname regex if set, otherwise the default.
func (c *Connection) getHostnameRegex() *regexp.Regexp {
	if c.customHostnameRegex != nil {
		return c.customHostnameRegex
	}
	return hostnameRegex
}

// NewConnection creates a new remote connection.
func NewConnection(cfg ConnectionConfig) *Connection {
	hostID := fmt.Sprintf("remote-%s", uuid.New().String()[:8])
	now := time.Now()

	conn := &Connection{
		host: &state.RemoteHost{
			ID:          hostID,
			FlavorID:    cfg.FlavorID,
			Status:      state.RemoteHostStatusProvisioning,
			ConnectedAt: now,
			ExpiresAt:   now.Add(DefaultHostExpiry),
		},
		flavor: &config.RemoteFlavor{
			ID:               cfg.FlavorID,
			Flavor:           cfg.Flavor,
			DisplayName:      cfg.DisplayName,
			WorkspacePath:    cfg.WorkspacePath,
			VCS:              cfg.VCS,
			ConnectCommand:   cfg.ConnectCommand,
			ReconnectCommand: cfg.ReconnectCommand,
			ProvisionCommand: cfg.ProvisionCommand,
		},
		onStatusChange:        cfg.OnStatusChange,
		onProgress:            cfg.OnProgress,
		provisioningSessionID: fmt.Sprintf("provision-%s", hostID),
	}

	// Compile custom hostname regex if provided
	if cfg.HostnameRegex != "" {
		if re, err := regexp.Compile(cfg.HostnameRegex); err == nil {
			conn.customHostnameRegex = re
		} else {
			fmt.Printf("[remote %s] invalid hostname_regex %q, using default: %v\n", hostID, cfg.HostnameRegex, err)
		}
	}

	return conn
}

// Host returns the current host state.
func (c *Connection) Host() state.RemoteHost {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return *c.host
}

// Flavor returns the flavor configuration.
func (c *Connection) Flavor() config.RemoteFlavor {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return *c.flavor
}

// Client returns the control mode client for this connection.
// Returns nil if not connected.
func (c *Connection) Client() *controlmode.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client
}

// IsConnected returns true if the connection is active.
func (c *Connection) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client != nil && c.host.Status == state.RemoteHostStatusConnected
}

// Connect establishes a new connection to a remote host.
// This spawns the remote connection command in a PTY for interactive terminal support.
func (c *Connection) Connect(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("connection already closed")
	}

	// Get connection command template and execute it
	templateStr := c.flavor.GetConnectCommandTemplate()

	// Parse template
	tmpl, err := template.New("connect").Parse(templateStr)
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("invalid connect command template: %w", err)
	}

	// Execute template with flavor data
	type ConnectTemplateData struct {
		Flavor string
	}

	data := ConnectTemplateData{
		Flavor: c.flavor.Flavor,
	}

	var cmdStr strings.Builder
	if err := tmpl.Execute(&cmdStr, data); err != nil {
		c.mu.Unlock()
		return fmt.Errorf("failed to execute connect command template: %w", err)
	}

	// Parse the command string into args
	cmdLine := cmdStr.String()
	args := strings.Fields(cmdLine)
	if len(args) == 0 {
		c.mu.Unlock()
		return fmt.Errorf("connect command template produced empty command")
	}

	c.cmd = exec.Command(args[0], args[1:]...)

	// Start command with PTY for interactive terminal (auth prompts work)
	ptmx, err := pty.StartWithSize(c.cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("failed to start remote connection with PTY: %w", err)
	}
	c.pty = ptmx

	// Use PTY for both reading and writing
	c.stdin = ptmx
	c.stdout = ptmx

	c.mu.Unlock()

	fmt.Printf("[remote %s] PTY started (pid=%d), provisioning session=%s\n",
		c.host.ID, c.cmd.Process.Pid, c.provisioningSessionID)

	// Monitor context cancellation during setup - kill process if context is canceled.
	// Once Connect() returns, the monitoring stops so the caller's defer cancel()
	// doesn't kill the long-lived connection.
	connectDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			fmt.Printf("[remote %s] context canceled during connection, killing process\n", c.host.ID)
			c.Close()
		case <-connectDone:
			// Connect completed, stop monitoring this context
		}
	}()

	// Create pipe so parseProvisioningOutput (sole PTY reader) can forward
	// data to the control mode parser without two goroutines competing on the PTY fd.
	controlPR, controlPW := io.Pipe()
	c.controlPipeWriter = controlPW

	// Parse PTY output for hostname and UUID during provisioning.
	// This is the ONLY goroutine that reads from the PTY.
	// It broadcasts raw bytes to WebSocket subscribers and tees to the control mode pipe.
	go c.parseProvisioningOutput(c.pty)

	// Wait for control mode to be ready (reads from pipe, not PTY directly)
	fmt.Printf("[remote %s] waiting for control mode...\n", c.host.ID)
	if err := c.waitForControlMode(ctx, controlPR); err != nil {
		close(connectDone)
		c.Close()
		return err
	}

	fmt.Printf("[remote %s] control mode ready, connected to %s\n", c.host.ID, c.hostname)

	// Stop the context monitoring goroutine - the connection is established
	// and should live independently of the setup context.
	close(connectDone)

	return nil
}

// Reconnect reconnects to an existing host by hostname.
func (c *Connection) Reconnect(ctx context.Context, hostname string) error {
	c.mu.Lock()
	c.hostname = hostname
	c.host.Hostname = hostname
	c.host.Status = state.RemoteHostStatusConnecting

	// Get reconnection command template and execute it
	templateStr := c.flavor.GetReconnectCommandTemplate()

	// Parse template
	tmpl, err := template.New("reconnect").Parse(templateStr)
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("invalid reconnect command template: %w", err)
	}

	// Execute template with reconnection data
	type ReconnectTemplateData struct {
		Hostname string
		Flavor   string
	}

	data := ReconnectTemplateData{
		Hostname: hostname,
		Flavor:   c.flavor.Flavor,
	}

	var cmdStr strings.Builder
	if err := tmpl.Execute(&cmdStr, data); err != nil {
		c.mu.Unlock()
		return fmt.Errorf("failed to execute reconnect command template: %w", err)
	}

	// Parse the command string into args
	cmdLine := cmdStr.String()
	args := strings.Fields(cmdLine)
	if len(args) == 0 {
		c.mu.Unlock()
		return fmt.Errorf("reconnect command template produced empty command")
	}

	c.cmd = exec.Command(args[0], args[1:]...)

	// Start command with PTY for interactive terminal
	ptmx, err := pty.StartWithSize(c.cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("failed to start remote reconnection with PTY: %w", err)
	}
	c.pty = ptmx
	c.stdin = ptmx
	c.stdout = ptmx
	c.mu.Unlock()

	c.notifyStatusChange()

	fmt.Printf("[remote %s] PTY started for reconnection (pid=%d), provisioning session=%s\n",
		c.host.ID, c.cmd.Process.Pid, c.provisioningSessionID)

	// Monitor context cancellation during setup - kill process if context is canceled.
	// Once Reconnect() returns, the monitoring stops so the caller's defer cancel()
	// doesn't kill the long-lived SSH process after reconnection succeeds.
	connectDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			fmt.Printf("[remote %s] context canceled during reconnection, killing process\n", c.host.ID)
			c.Close()
		case <-connectDone:
			// Reconnect completed, stop monitoring this context
		}
	}()

	// Create pipe so parseProvisioningOutput (sole PTY reader) can forward
	// data to the control mode parser without two goroutines competing on the PTY fd.
	controlPR, controlPW := io.Pipe()
	c.controlPipeWriter = controlPW

	// Parse PTY output during reconnection.
	// This is the ONLY goroutine that reads from the PTY.
	// It broadcasts raw bytes to WebSocket subscribers and tees to the control mode pipe.
	go c.parseProvisioningOutput(c.pty)

	// Wait for control mode (reads from pipe, not PTY directly)
	fmt.Printf("[remote %s] reconnecting, waiting for control mode...\n", c.host.ID)
	if err := c.waitForControlMode(ctx, controlPR); err != nil {
		close(connectDone)
		c.Close()
		return err
	}

	fmt.Printf("[remote %s] control mode ready after reconnection to %s\n", c.host.ID, c.hostname)

	// Stop the context monitoring goroutine - the connection is established
	// and should live independently of the setup context.
	close(connectDone)

	// Rediscover sessions after reconnection
	if err := c.rediscoverSessions(ctx); err != nil {
		fmt.Printf("[remote] warning: failed to rediscover sessions: %v\n", err)
		// Don't fail reconnection if rediscovery fails
	}

	return nil
}

// parseProvisioningOutput reads PTY output and extracts hostname and UUID.
// It also broadcasts raw bytes to PTY subscribers for WebSocket terminal streaming
// and forwards data to the control mode parser via controlPipeWriter.
// This MUST be the only goroutine reading from the PTY.
func (c *Connection) parseProvisioningOutput(r io.Reader) {
	fmt.Printf("[remote %s] parseProvisioningOutput started\n", c.host.ID)
	buf := make([]byte, 4096)
	var lineBuf strings.Builder
	pipeOpen := true
	hnRegex := c.getHostnameRegex()

	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]

			// Broadcast raw bytes to PTY subscribers (WebSocket terminals)
			c.broadcastPTYOutput(chunk)

			// Forward to control mode parser pipe
			if pipeOpen && c.controlPipeWriter != nil {
				if _, werr := c.controlPipeWriter.Write(chunk); werr != nil {
					fmt.Printf("[remote %s] control pipe write error (expected during shutdown): %v\n", c.host.ID, werr)
					pipeOpen = false
				}
			}

			// Accumulate raw output
			c.provisioningMu.Lock()
			c.provisioningOutput.Write(chunk)
			c.provisioningMu.Unlock()

			// Parse line by line for hostname/UUID extraction
			for _, b := range chunk {
				if b == '\n' {
					line := strings.TrimRight(lineBuf.String(), "\r")
					lineBuf.Reset()

					// Emit progress via callback if set
					if c.onProgress != nil {
						c.onProgress(line)
					}

					// Check for hostname
					if matches := hnRegex.FindStringSubmatch(line); matches != nil {
						c.mu.Lock()
						c.hostname = matches[1]
						c.host.Hostname = matches[1]
						c.host.Status = state.RemoteHostStatusConnecting
						c.mu.Unlock()
						c.notifyStatusChange()
					}

					// Check for session UUID
					if matches := uuidRegex.FindStringSubmatch(line); matches != nil {
						c.mu.Lock()
						c.uuid = matches[1]
						c.host.UUID = matches[1]
						c.mu.Unlock()
					}
				} else {
					lineBuf.WriteByte(b)
				}
			}
		}
		if err != nil {
			break
		}
	}

	// Flush any remaining partial line
	if lineBuf.Len() > 0 {
		line := strings.TrimRight(lineBuf.String(), "\r")
		if c.onProgress != nil {
			c.onProgress(line)
		}
		if matches := hnRegex.FindStringSubmatch(line); matches != nil {
			c.mu.Lock()
			c.hostname = matches[1]
			c.host.Hostname = matches[1]
			c.host.Status = state.RemoteHostStatusConnecting
			c.mu.Unlock()
			c.notifyStatusChange()
		}
		if matches := uuidRegex.FindStringSubmatch(line); matches != nil {
			c.mu.Lock()
			c.uuid = matches[1]
			c.host.UUID = matches[1]
			c.mu.Unlock()
		}
	}

	// Close the control pipe writer so the control mode parser gets EOF
	if c.controlPipeWriter != nil {
		c.controlPipeWriter.Close()
	}

	fmt.Printf("[remote %s] parseProvisioningOutput exited\n", c.host.ID)
}

// waitForControlMode waits for tmux control mode to be ready.
// The reader parameter provides the data source for the control mode parser.
func (c *Connection) waitForControlMode(ctx context.Context, reader io.Reader) error {
	// Create parser with the provided reader
	c.parser = controlmode.NewParser(reader, c.host.ID)
	c.client = controlmode.NewClient(c.stdin, c.parser)

	// Start the parser in background
	go c.parser.Run()

	// Wait for the parser to see the first control mode protocol line (%)
	// before sending any commands. During provisioning, SSH/auth output
	// comes first and tmux hasn't entered control mode yet - sending
	// commands too early means they go to the shell and are lost.
	fmt.Printf("[remote %s] waiting for control mode protocol...\n", c.host.ID)
	waitCtx, cancel := context.WithTimeout(ctx, ControlModeReadyTimeout)
	defer cancel()

	select {
	case <-c.parser.ControlModeReady():
		fmt.Printf("[remote %s] control mode protocol detected, sending ready check\n", c.host.ID)
	case <-waitCtx.Done():
		return fmt.Errorf("control mode not ready: %w", waitCtx.Err())
	}

	// Start the client (processes responses/output/events)
	c.client.Start()

	// Now it's safe to send commands - tmux is in control mode
	if err := c.client.WaitForReady(waitCtx, ControlModeReadyTimeout); err != nil {
		return fmt.Errorf("control mode not ready: %w", err)
	}

	// Update status to connected
	c.mu.Lock()
	c.host.Status = state.RemoteHostStatusConnected
	c.host.ConnectedAt = time.Now()
	c.host.ExpiresAt = time.Now().Add(DefaultHostExpiry)
	c.mu.Unlock()

	c.notifyStatusChange()

	// Connection ready - drain pending session queue
	c.drainPendingQueue(ctx)

	return nil
}

// Close closes the connection and cleans up resources.
func (c *Connection) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.host.Status = state.RemoteHostStatusDisconnected
		c.mu.Unlock()

		c.notifyStatusChange()

		// Close control pipe writer (unblocks parseProvisioningOutput if blocked on write)
		if c.controlPipeWriter != nil {
			c.controlPipeWriter.Close()
		}

		// Close client
		if c.client != nil {
			c.client.Close()
		}

		// Close PTY (this also closes stdin/stdout since they point to it)
		if c.pty != nil {
			c.pty.Close()
		}

		// Close stderr if separate (shouldn't be with PTY but check anyway)
		if c.stderr != nil {
			c.stderr.Close()
		}

		// Close PTY subscriber channels
		c.ptySubscribersMu.Lock()
		for _, ch := range c.ptySubscribers {
			close(ch)
		}
		c.ptySubscribers = nil
		c.ptySubscribersMu.Unlock()

		// Kill the process
		if c.cmd != nil && c.cmd.Process != nil {
			c.cmd.Process.Kill()
			c.cmd.Wait()
		}
	})

	return closeErr
}

// notifyStatusChange calls the status change callback if set.
func (c *Connection) notifyStatusChange() {
	if c.onStatusChange != nil {
		c.mu.RLock()
		hostID := c.host.ID
		status := c.host.Status
		c.mu.RUnlock()
		c.onStatusChange(hostID, status)
	}
}

// ProvisioningOutput returns the captured provisioning output.
func (c *Connection) ProvisioningOutput() string {
	c.provisioningMu.Lock()
	defer c.provisioningMu.Unlock()
	return c.provisioningOutput.String()
}

// ProvisioningSessionID returns the local tmux session ID used for provisioning.
// Returns empty string if not provisioning via local tmux.
func (c *Connection) ProvisioningSessionID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.provisioningSessionID
}

// PTY returns the pseudo-terminal file for interactive I/O during provisioning.
// Returns nil if connection is not using PTY.
func (c *Connection) PTY() *os.File {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pty
}

// ResizePTY resizes the provisioning PTY to the given dimensions.
func (c *Connection) ResizePTY(cols, rows uint16) error {
	c.mu.RLock()
	ptmx := c.pty
	c.mu.RUnlock()
	if ptmx == nil {
		return fmt.Errorf("no PTY available")
	}
	return pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols})
}

// SubscribePTYOutput creates a channel that receives raw PTY output bytes.
// Used by WebSocket handlers to stream provisioning terminal output.
func (c *Connection) SubscribePTYOutput() chan []byte {
	ch := make(chan []byte, 100)
	c.ptySubscribersMu.Lock()
	c.ptySubscribers = append(c.ptySubscribers, ch)
	c.ptySubscribersMu.Unlock()
	return ch
}

// UnsubscribePTYOutput removes a PTY output subscriber.
func (c *Connection) UnsubscribePTYOutput(ch chan []byte) {
	c.ptySubscribersMu.Lock()
	defer c.ptySubscribersMu.Unlock()
	for i, sub := range c.ptySubscribers {
		if sub == ch {
			c.ptySubscribers = append(c.ptySubscribers[:i], c.ptySubscribers[i+1:]...)
			return
		}
	}
}

// broadcastPTYOutput sends raw PTY output to all subscribers.
func (c *Connection) broadcastPTYOutput(data []byte) {
	c.ptySubscribersMu.Lock()
	defer c.ptySubscribersMu.Unlock()
	for _, ch := range c.ptySubscribers {
		dataCopy := make([]byte, len(data))
		copy(dataCopy, data)
		select {
		case ch <- dataCopy:
		default:
			// Drop if subscriber is slow
		}
	}
}

// Hostname returns the connected hostname.
func (c *Connection) Hostname() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hostname
}

// Status returns the current connection status.
func (c *Connection) Status() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.host != nil {
		return c.host.Status
	}
	return "disconnected"
}

// CreateSession creates a new session (tmux window) on the remote host.
func (c *Connection) CreateSession(ctx context.Context, name, workdir, command string) (windowID, paneID string, err error) {
	if !c.IsConnected() {
		return "", "", fmt.Errorf("not connected")
	}
	return c.client.CreateWindow(ctx, name, workdir, command)
}

// KillSession kills a session (tmux window) on the remote host.
func (c *Connection) KillSession(ctx context.Context, windowID string) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}
	return c.client.KillWindow(ctx, windowID)
}

// SendKeys sends keys to a pane on the remote host.
func (c *Connection) SendKeys(ctx context.Context, paneID, keys string) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}
	return c.client.SendKeys(ctx, paneID, keys)
}

// SubscribeOutput subscribes to output from a pane.
func (c *Connection) SubscribeOutput(paneID string) <-chan controlmode.OutputEvent {
	if c.client == nil {
		ch := make(chan controlmode.OutputEvent)
		close(ch)
		return ch
	}
	return c.client.SubscribeOutput(paneID)
}

// UnsubscribeOutput removes an output subscription for a pane.
func (c *Connection) UnsubscribeOutput(paneID string, ch <-chan controlmode.OutputEvent) {
	if c.client != nil {
		c.client.UnsubscribeOutput(paneID, ch)
	}
}

// CapturePaneLines captures the last N lines from a pane for scrollback.
func (c *Connection) CapturePaneLines(ctx context.Context, paneID string, lines int) (string, error) {
	if !c.IsConnected() {
		return "", fmt.Errorf("not connected")
	}
	return c.client.CapturePaneLines(ctx, paneID, lines)
}

// ListSessions lists all sessions (windows) on the remote host.
func (c *Connection) ListSessions(ctx context.Context) ([]controlmode.WindowInfo, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("not connected")
	}
	return c.client.ListWindows(ctx)
}

// QueueSession adds a session to the pending queue if connection is not ready.
// Returns a channel that will receive the result when the session is created.
func (c *Connection) QueueSession(ctx context.Context, sessionID, name, workdir, command string) <-chan PendingSessionResult {
	ch := make(chan PendingSessionResult, 1)

	c.pendingSessionsMu.Lock()
	c.pendingSessions = append(c.pendingSessions, PendingSession{
		SessionID:  sessionID,
		Name:       name,
		WorkDir:    workdir,
		Command:    command,
		CompleteCh: ch,
	})
	c.pendingSessionsMu.Unlock()

	fmt.Printf("[remote] queued session %s (pending: %d)\n", sessionID, len(c.pendingSessions))

	return ch
}

// drainPendingQueue processes all pending sessions after connection is ready.
func (c *Connection) drainPendingQueue(ctx context.Context) {
	c.pendingSessionsMu.Lock()
	pending := c.pendingSessions
	c.pendingSessions = nil
	c.pendingSessionsMu.Unlock()

	if len(pending) == 0 {
		return
	}

	fmt.Printf("[remote] draining %d pending session(s)\n", len(pending))

	for _, p := range pending {
		windowID, paneID, err := c.client.CreateWindow(ctx, p.Name, p.WorkDir, p.Command)
		if err != nil {
			fmt.Printf("[remote] failed to create queued session %s: %v\n", p.SessionID, err)
			p.CompleteCh <- PendingSessionResult{Error: fmt.Errorf("failed to create queued session: %w", err)}
		} else {
			fmt.Printf("[remote] created queued session %s (window=%s, pane=%s)\n", p.SessionID, windowID, paneID)
			p.CompleteCh <- PendingSessionResult{WindowID: windowID, PaneID: paneID, Error: nil}
		}
		close(p.CompleteCh)
	}
}

// rediscoverSessions lists windows on the remote host after reconnection.
// Returns the discovered windows for the manager to reconcile with state.
func (c *Connection) rediscoverSessions(ctx context.Context) error {
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	windows, err := c.client.ListWindows(ctx)
	if err != nil {
		return fmt.Errorf("failed to list windows: %w", err)
	}

	fmt.Printf("[remote] rediscovered %d window(s) on host %s\n", len(windows), c.hostname)

	// Note: The actual reconciliation with state happens in Manager.Reconnect()
	// This method just verifies the connection works and logs what was found
	return nil
}

// Provision executes the provision command on the remote host if configured.
// This should be called once after the initial connection is established.
// Returns nil if no provision command is configured or if already provisioned.
func (c *Connection) Provision(ctx context.Context, provisionCmd string) error {
	if provisionCmd == "" {
		fmt.Printf("[remote] no provision command configured, skipping provisioning\n")
		return nil
	}

	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	fmt.Printf("[remote] provisioning workspace on host %s\n", c.hostname)

	// Parse and execute provision command template
	tmpl, err := template.New("provision").Parse(provisionCmd)
	if err != nil {
		return fmt.Errorf("invalid provision command template: %w", err)
	}

	// Execute template with provision data
	type ProvisionTemplateData struct {
		WorkspacePath string
		VCS           string
	}

	data := ProvisionTemplateData{
		WorkspacePath: c.flavor.WorkspacePath,
		VCS:           c.flavor.VCS,
	}

	var cmdStr strings.Builder
	if err := tmpl.Execute(&cmdStr, data); err != nil {
		return fmt.Errorf("failed to execute provision command template: %w", err)
	}

	command := cmdStr.String()
	fmt.Printf("[remote] executing provision command: %s\n", command)

	// Execute provision command with timeout (5 minutes default)
	provisionCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	output, err := c.client.Execute(provisionCtx, command)
	if err != nil {
		return fmt.Errorf("provision command failed: %w", err)
	}

	fmt.Printf("[remote] provision completed successfully\n")
	if output != "" {
		fmt.Printf("[remote] provision output: %s\n", output)
	}

	return nil
}
