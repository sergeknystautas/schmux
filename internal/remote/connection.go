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
	"sync/atomic"
	"text/template"
	"time"

	"github.com/charmbracelet/log"
	"github.com/creack/pty"
	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/pkg/shellutil"
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
	host      *state.RemoteHost
	flavor    *config.RemoteFlavor
	flavorStr string // The flavor string (e.g., "www", "gpu")
	cmd       *exec.Cmd
	client    *controlmode.Client
	parser    *controlmode.Parser
	logger    *log.Logger

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

	// Set to true once control mode is established; parseProvisioningOutput
	// must stop updating hostname/status after this point to avoid
	// overwriting the "connected" status with "connecting" from stale
	// %output matches.
	controlModeEstablished atomic.Bool

	// Cancel function for the connect context, called during Close()
	// to unblock waitForControlMode if the connection is shut down.
	connectCancel   context.CancelFunc
	connectCancelMu sync.Mutex

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

	// tmux socket name for isolation on the remote host
	tmuxSocketName string

	// PTY output subscribers for WebSocket terminal streaming
	ptySubscribers   []chan []byte
	ptySubscribersMu sync.Mutex
}

// ConnectionConfig holds configuration for creating a connection.
type ConnectionConfig struct {
	ProfileID        string
	Flavor           string // The flavor/environment identifier
	DisplayName      string
	WorkspacePath    string
	VCS              string
	ConnectCommand   string
	ReconnectCommand string
	ProvisionCommand string
	HostnameRegex    string // Custom regex for hostname extraction (first capture group)
	TmuxSocketName   string // Socket name for tmux isolation on remote host (default: "schmux")
	OnStatusChange   func(hostID, status string)
	OnProgress       func(message string)
	Logger           *log.Logger
}

// ConnectionConfigFromResolved creates a ConnectionConfig from a ResolvedFlavor,
// copying all resolved fields. Callers set OnStatusChange, OnProgress,
// and Logger separately.
func ConnectionConfigFromResolved(r config.ResolvedFlavor) ConnectionConfig {
	return ConnectionConfig{
		ProfileID:        r.ProfileID,
		Flavor:           r.Flavor,
		DisplayName:      r.FlavorDisplayName,
		WorkspacePath:    r.WorkspacePath,
		VCS:              r.VCS,
		ConnectCommand:   r.ConnectCommand,
		ReconnectCommand: r.ReconnectCommand,
		ProvisionCommand: r.ProvisionCommand,
		HostnameRegex:    r.HostnameRegex,
	}
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
			ProfileID:   cfg.ProfileID,
			Flavor:      cfg.Flavor,
			Status:      state.RemoteHostStatusProvisioning,
			ConnectedAt: now,
			ExpiresAt:   now.Add(DefaultHostExpiry),
		},
		flavor: &config.RemoteFlavor{
			ID:               cfg.ProfileID,
			Flavor:           cfg.Flavor,
			DisplayName:      cfg.DisplayName,
			WorkspacePath:    cfg.WorkspacePath,
			VCS:              cfg.VCS,
			ConnectCommand:   cfg.ConnectCommand,
			ReconnectCommand: cfg.ReconnectCommand,
			ProvisionCommand: cfg.ProvisionCommand,
		},
		flavorStr:             cfg.Flavor,
		tmuxSocketName:        cfg.TmuxSocketName,
		logger:                cfg.Logger,
		onStatusChange:        cfg.OnStatusChange,
		onProgress:            cfg.OnProgress,
		provisioningSessionID: fmt.Sprintf("provision-%s", hostID),
	}

	// Compile custom hostname regex if provided
	if cfg.HostnameRegex != "" {
		if re, err := regexp.Compile(cfg.HostnameRegex); err == nil {
			conn.customHostnameRegex = re
		} else if conn.logger != nil {
			conn.logger.Warn("invalid hostname_regex, using default", "host_id", hostID, "regex", cfg.HostnameRegex, "err", err)
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

// FlavorStr returns the flavor string (e.g., "www", "gpu").
func (c *Connection) FlavorStr() string {
	return c.flavorStr
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
	templateStr := c.flavor.GetConnectCommandTemplate(c.tmuxSocketName)

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

	// Parse the command string into args (supports quoted arguments for paths with spaces)
	cmdLine := cmdStr.String()
	args, err := shellutil.Split(cmdLine)
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("failed to parse connect command: %w", err)
	}
	if len(args) == 0 {
		c.mu.Unlock()
		return fmt.Errorf("connect command template produced empty command")
	}

	c.cmd = exec.Command(args[0], args[1:]...)

	if c.logger != nil {
		c.logger.Info("executing connect command", "host_id", c.host.ID, "cmd", cmdLine)
	}

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

	if c.logger != nil {
		c.logger.Info("PTY started", "host_id", c.host.ID, "pid", c.cmd.Process.Pid, "provisioning_session", c.provisioningSessionID)
	}

	// Monitor SSH process lifecycle. This goroutine waits for the process to exit
	// and updates the connection status to "disconnected" when it dies.
	// It is also the sole caller of cmd.Wait() to reap the process.
	go c.monitorProcess()

	// Monitor context cancellation during setup - kill process if context is canceled.
	// Once Connect() returns, the monitoring stops so the caller's defer cancel()
	// doesn't kill the long-lived connection.
	connectDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			if c.logger != nil {
				c.logger.Warn("context canceled during connection, killing process", "host_id", c.host.ID)
			}
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
	if c.logger != nil {
		c.logger.Info("waiting for control mode", "host_id", c.host.ID)
	}
	if err := c.waitForControlMode(ctx, controlPR); err != nil {
		close(connectDone)
		c.Close()
		return err
	}

	if c.logger != nil {
		c.logger.Info("control mode ready", "host_id", c.host.ID, "hostname", c.hostname)
	}

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
	templateStr := c.flavor.GetReconnectCommandTemplate(c.tmuxSocketName)

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

	// Parse the command string into args (supports quoted arguments for paths with spaces)
	cmdLine := cmdStr.String()
	args, err := shellutil.Split(cmdLine)
	if err != nil {
		c.mu.Unlock()
		return fmt.Errorf("failed to parse reconnect command: %w", err)
	}
	if len(args) == 0 {
		c.mu.Unlock()
		return fmt.Errorf("reconnect command template produced empty command")
	}

	c.cmd = exec.Command(args[0], args[1:]...)

	if c.logger != nil {
		c.logger.Info("executing reconnect command", "host_id", c.host.ID, "cmd", cmdLine)
	}

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

	if c.logger != nil {
		c.logger.Info("PTY started for reconnection", "host_id", c.host.ID, "pid", c.cmd.Process.Pid, "provisioning_session", c.provisioningSessionID)
	}

	// Monitor SSH process lifecycle (same as Connect).
	go c.monitorProcess()

	// Monitor context cancellation during setup - kill process if context is canceled.
	// Once Reconnect() returns, the monitoring stops so the caller's defer cancel()
	// doesn't kill the long-lived SSH process after reconnection succeeds.
	connectDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			if c.logger != nil {
				c.logger.Warn("context canceled during reconnection, killing process", "host_id", c.host.ID)
			}
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
	if c.logger != nil {
		c.logger.Info("reconnecting, waiting for control mode", "host_id", c.host.ID)
	}
	if err := c.waitForControlMode(ctx, controlPR); err != nil {
		close(connectDone)
		c.Close()
		return err
	}

	if c.logger != nil {
		c.logger.Info("control mode ready after reconnection", "host_id", c.host.ID, "hostname", c.hostname)
	}

	// Stop the context monitoring goroutine - the connection is established
	// and should live independently of the setup context.
	close(connectDone)

	// Rediscover sessions after reconnection
	if err := c.rediscoverSessions(ctx); err != nil {
		if c.logger != nil {
			c.logger.Warn("failed to rediscover sessions", "err", err)
		}
		// Don't fail reconnection if rediscovery fails
	}

	return nil
}

// parseProvisioningOutput reads PTY output and extracts hostname and UUID.
// It also broadcasts raw bytes to PTY subscribers for WebSocket terminal streaming
// and forwards data to the control mode parser via controlPipeWriter.
// This MUST be the only goroutine reading from the PTY.
func (c *Connection) parseProvisioningOutput(r io.Reader) {
	if c.logger != nil {
		c.logger.Debug("parseProvisioningOutput started", "host_id", c.host.ID)
	}
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
					if c.logger != nil {
						c.logger.Debug("control pipe write error (expected during shutdown)", "host_id", c.host.ID, "err", werr)
					}
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

					// Check for hostname/UUID only during provisioning.
					// After control mode is established, PTY output is tmux protocol
					// data and any regex matches would be false positives from
					// %output events (e.g., shell prompts containing hostnames).
					if !c.controlModeEstablished.Load() {
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
					}
				} else {
					lineBuf.WriteByte(b)
				}
			}
		}
		if err != nil {
			if c.logger != nil {
				controlModeReady := c.controlModeEstablished.Load()
				c.mu.RLock()
				hn := c.hostname
				c.mu.RUnlock()
				c.logger.Info("PTY read ended", "host_id", c.host.ID, "hostname", hn, "control_mode_ready", controlModeReady, "err", err)
			}
			break
		}
	}

	// Flush any remaining partial line
	if lineBuf.Len() > 0 {
		line := strings.TrimRight(lineBuf.String(), "\r")
		if c.onProgress != nil {
			c.onProgress(line)
		}
		if !c.controlModeEstablished.Load() {
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
	}

	// Close the control pipe writer so the control mode parser gets EOF
	if c.controlPipeWriter != nil {
		c.controlPipeWriter.Close()
	}

	if c.logger != nil {
		c.logger.Debug("parseProvisioningOutput exited", "host_id", c.host.ID)
	}
}

// waitForControlMode waits for tmux control mode to be ready.
// The reader parameter provides the data source for the control mode parser.
func (c *Connection) waitForControlMode(ctx context.Context, reader io.Reader) error {
	// Create parser with the provided reader
	c.parser = controlmode.NewParser(reader, c.logger, c.host.ID)
	c.client = controlmode.NewClient(c.stdin, c.parser, c.logger)

	// Start the parser in background
	go c.parser.Run()

	// Wait for the parser to see the first control mode protocol line (%)
	// before sending any commands. During provisioning, SSH/auth output
	// comes first and tmux hasn't entered control mode yet - sending
	// commands too early means they go to the shell and are lost.
	// Uses the parent context timeout (5 minutes from StartConnect) rather than
	// a short fixed timeout, because OD provisioning (SSH auth, reservation,
	// ControlMaster setup) can take 30+ seconds before tmux even starts.
	if c.logger != nil {
		c.logger.Info("waiting for control mode protocol", "host_id", c.host.ID)
	}

	select {
	case <-c.parser.ControlModeReady():
		if c.logger != nil {
			c.logger.Info("control mode protocol detected, sending ready check", "host_id", c.host.ID)
		}
	case <-ctx.Done():
		return fmt.Errorf("control mode not ready: %w", ctx.Err())
	}

	// Start the client (processes responses/output/events)
	c.client.Start()

	// Now it's safe to send commands - tmux is in control mode
	if err := c.client.WaitForReady(ctx, ControlModeReadyTimeout); err != nil {
		return fmt.Errorf("control mode not ready: %w", err)
	}

	// Update status to connected
	c.controlModeEstablished.Store(true)
	c.mu.Lock()
	c.host.Status = state.RemoteHostStatusConnected
	c.host.ConnectedAt = time.Now()
	c.host.ExpiresAt = time.Now().Add(DefaultHostExpiry)
	c.mu.Unlock()

	c.notifyStatusChange()

	// Hostname fallback: if provisioning output didn't contain a hostname
	// (regex didn't match), try to extract it from the remote tmux server.
	c.mu.RLock()
	hostnameEmpty := c.hostname == ""
	c.mu.RUnlock()

	if hostnameEmpty {
		if c.logger != nil {
			c.logger.Info("hostname not extracted from provisioning output, trying tmux fallback", "host_id", c.host.ID)
		}
		fallbackCtx, fallbackCancel := context.WithTimeout(ctx, 5*time.Second)
		defer fallbackCancel()

		if resp, _, err := c.client.Execute(fallbackCtx, "display-message -p '#{host}'"); err == nil {
			h := strings.TrimSpace(resp)
			if h != "" {
				c.mu.Lock()
				c.hostname = h
				c.host.Hostname = h
				c.mu.Unlock()
				c.notifyStatusChange()
				if c.logger != nil {
					c.logger.Info("hostname extracted via tmux fallback", "host_id", c.host.ID, "hostname", h)
				}
			}
		} else {
			if c.logger != nil {
				c.logger.Warn("tmux hostname fallback failed, leaving hostname empty", "host_id", c.host.ID, "err", err)
			}
		}
	}

	// Set window-size to manual so each window can be independently resized.
	// Without this, tmux constrains all windows to the control mode client's
	// PTY size (80x24), ignoring per-window resize-window commands.
	if err := c.client.SetOption(ctx, "window-size", "manual"); err != nil {
		if c.logger != nil {
			c.logger.Warn("failed to set window-size manual", "host_id", c.host.ID, "err", err)
		}
	}

	// Set DISPLAY in the tmux global environment so all panes (including AI agents)
	// can access the X11 clipboard via xclip. This must happen BEFORE sessions are
	// spawned so the agent process inherits DISPLAY at startup.
	// DISPLAY=:99 is the conventional Xvfb display started during provisioning.
	if _, _, err := c.client.Execute(ctx, "setenv -g DISPLAY :99"); err != nil {
		if c.logger != nil {
			c.logger.Warn("failed to set DISPLAY in tmux environment", "host_id", c.host.ID, "err", err)
		}
	}

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

		// Cancel the connect context to unblock waitForControlMode
		c.connectCancelMu.Lock()
		if c.connectCancel != nil {
			c.connectCancel()
		}
		c.connectCancelMu.Unlock()

		// Close control pipe writer (unblocks parseProvisioningOutput if blocked on write)
		if c.controlPipeWriter != nil {
			c.controlPipeWriter.Close()
		}

		// Close client
		if c.client != nil {
			c.client.Close()
		}

		// Kill the process BEFORE closing the PTY. On some kernels, closing
		// a PTY fd doesn't unblock a blocked Read() — the process kill is
		// what actually tears down the backing fd and unblocks readers.
		// Don't call cmd.Wait() here — the monitorProcess goroutine is the
		// sole caller of Wait() to avoid double-wait races.
		if c.cmd != nil && c.cmd.Process != nil {
			c.cmd.Process.Kill()
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

		// Notify pending session callers so they don't block forever.
		c.pendingSessionsMu.Lock()
		for _, p := range c.pendingSessions {
			p.CompleteCh <- PendingSessionResult{Error: fmt.Errorf("connection closed")}
			close(p.CompleteCh)
		}
		c.pendingSessions = nil
		c.pendingSessionsMu.Unlock()
	})

	return closeErr
}

// monitorProcess waits for the SSH process to exit and triggers connection cleanup.
// This is the sole goroutine that calls cmd.Wait() to reap the process and avoid
// zombie processes. When the SSH process dies (network failure, remote close, kill),
// this updates the connection status to "disconnected" so the dashboard reflects reality.
func (c *Connection) monitorProcess() {
	if c.cmd == nil {
		return
	}

	// Wait for the process to exit. This blocks until the process terminates.
	// It is the ONLY place cmd.Wait() is called to avoid double-wait races.
	err := c.cmd.Wait()

	c.mu.RLock()
	hostID := c.host.ID
	hostname := c.hostname
	status := c.host.Status
	c.mu.RUnlock()

	if status == state.RemoteHostStatusDisconnected {
		// Already disconnected (Close() was called first), just log
		if c.logger != nil {
			c.logger.Info("SSH process exited (already disconnected)", "host_id", hostID, "hostname", hostname, "err", err)
		}
		return
	}

	exitCode := -1
	if c.cmd.ProcessState != nil {
		exitCode = c.cmd.ProcessState.ExitCode()
	}
	if c.logger != nil {
		c.logger.Warn("SSH process exited unexpectedly", "host_id", hostID, "hostname", hostname, "exit_code", exitCode, "err", err)
	}

	// Trigger connection cleanup (sets status to disconnected, notifies callbacks).
	// closeOnce ensures this is safe even if Close() was already called.
	c.Close()
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

// SetConnectCancel stores a cancel function that will be called during Close()
// to unblock any pending waitForControlMode select.
func (c *Connection) SetConnectCancel(cancel context.CancelFunc) {
	c.connectCancelMu.Lock()
	c.connectCancel = cancel
	c.connectCancelMu.Unlock()
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

// UnsubscribePTYOutput removes a PTY output subscriber and closes its channel.
func (c *Connection) UnsubscribePTYOutput(ch chan []byte) {
	c.ptySubscribersMu.Lock()
	defer c.ptySubscribersMu.Unlock()
	for i, sub := range c.ptySubscribers {
		if sub == ch {
			c.ptySubscribers = append(c.ptySubscribers[:i], c.ptySubscribers[i+1:]...)
			close(ch)
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

// FindSessionByName finds a session (tmux window) by name on the remote host.
// Returns nil if no window with that name exists.
func (c *Connection) FindSessionByName(ctx context.Context, name string) (*controlmode.WindowInfo, error) {
	if !c.IsConnected() {
		return nil, fmt.Errorf("not connected")
	}
	return c.client.FindWindowByName(ctx, name)
}

// SendKeys sends keys to a pane on the remote host.
func (c *Connection) SendKeys(ctx context.Context, paneID, keys string) (controlmode.SendKeysTimings, error) {
	if !c.IsConnected() {
		return controlmode.SendKeysTimings{}, fmt.Errorf("not connected")
	}
	return c.client.SendKeys(ctx, paneID, keys)
}

// ExecuteHealthProbe runs a lightweight no-op command for RTT measurement.
func (c *Connection) ExecuteHealthProbe(ctx context.Context) (string, time.Duration, error) {
	if !c.IsConnected() {
		return "", 0, fmt.Errorf("not connected")
	}
	return c.client.Execute(ctx, "display-message -p ok")
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

// GetCursorState returns the cursor position and visibility for a pane.
func (c *Connection) GetCursorState(ctx context.Context, paneID string) (controlmode.CursorState, error) {
	if !c.IsConnected() {
		return controlmode.CursorState{}, fmt.Errorf("not connected")
	}
	return c.client.GetCursorState(ctx, paneID)
}

// GetCursorPosition returns the cursor position (x, y) for a pane.
func (c *Connection) GetCursorPosition(ctx context.Context, paneID string) (x, y int, err error) {
	if !c.IsConnected() {
		return 0, 0, fmt.Errorf("not connected")
	}
	return c.client.GetCursorPosition(ctx, paneID)
}

// RunCommand executes a command on the remote host and returns its output.
// It uses a hidden tmux window to run the command without stealing focus.
func (c *Connection) RunCommand(ctx context.Context, workdir, command string) (string, error) {
	if !c.IsConnected() {
		return "", fmt.Errorf("not connected")
	}
	return c.client.RunCommand(ctx, workdir, command)
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
	n := len(c.pendingSessions)
	c.pendingSessionsMu.Unlock()

	if c.logger != nil {
		c.logger.Info("queued session", "session_id", sessionID, "pending", n)
	}

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

	if c.logger != nil {
		c.logger.Info("draining pending sessions", "count", len(pending))
	}

	for _, p := range pending {
		windowID, paneID, err := c.client.CreateWindow(ctx, p.Name, p.WorkDir, p.Command)
		if err != nil {
			if c.logger != nil {
				c.logger.Error("failed to create queued session", "session_id", p.SessionID, "err", err)
			}
			p.CompleteCh <- PendingSessionResult{Error: fmt.Errorf("failed to create queued session: %w", err)}
		} else {
			if c.logger != nil {
				c.logger.Info("created queued session", "session_id", p.SessionID, "window", windowID, "pane", paneID)
			}
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

	if c.logger != nil {
		c.logger.Info("rediscovered windows", "count", len(windows), "hostname", c.hostname)
	}

	// Note: The actual reconciliation with state happens in Manager.Reconnect()
	// This method just verifies the connection works and logs what was found
	return nil
}

// Provision executes the provision command on the remote host if configured.
// This should be called once after the initial connection is established.
// Returns nil if no provision command is configured or if already provisioned.
func (c *Connection) Provision(ctx context.Context, provisionCmd string) error {
	if provisionCmd == "" {
		if c.logger != nil {
			c.logger.Info("no provision command configured, skipping provisioning")
		}
		return nil
	}

	// Check that control mode is established (client is ready to send commands).
	// Don't use IsConnected() here because the caller may have temporarily set
	// status to "provisioning" for UI feedback while the provision command runs.
	if c.client == nil || !c.controlModeEstablished.Load() {
		return fmt.Errorf("not connected")
	}

	if c.logger != nil {
		c.logger.Info("provisioning workspace", "hostname", c.hostname)
	}

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
	if c.logger != nil {
		c.logger.Info("executing provision command", "host_id", c.host.ID, "cmd", command)
	}

	// Run provision command in a hidden tmux window (shell command, not tmux command).
	// Use an independent context — the caller's context (from Connect/Reconnect)
	// may have little time remaining after SSH setup. Package installation can
	// take minutes on first run.
	provisionCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	output, err := c.RunCommand(provisionCtx, c.flavor.WorkspacePath, command)
	if err != nil {
		if c.logger != nil {
			c.logger.Error("provision command failed", "host_id", c.host.ID, "cmd", command, "err", err)
		}
		return fmt.Errorf("provision command failed: %w", err)
	}

	if c.logger != nil {
		// Log output at Info level so provisioning progress is always visible
		c.logger.Info("provision completed", "host_id", c.host.ID, "output_len", len(output))
		if output != "" {
			// Truncate long output to avoid flooding logs
			logOutput := output
			if len(logOutput) > 500 {
				logOutput = logOutput[:500] + "... (truncated)"
			}
			c.logger.Info("provision output", "host_id", c.host.ID, "output", logOutput)
		}
	}

	return nil
}
