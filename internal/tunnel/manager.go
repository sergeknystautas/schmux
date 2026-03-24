//go:build !notunnel

package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
)

// Tunnel states
const (
	StateOff       = "off"
	StateStarting  = "starting"
	StateConnected = "connected"
	StateError     = "error"
)

// TunnelStatus represents the current tunnel state.
type TunnelStatus struct {
	State string `json:"state"`
	URL   string `json:"url,omitempty"`
	Error string `json:"error,omitempty"`
}

// ManagerConfig holds configuration for the tunnel manager.
type ManagerConfig struct {
	Disabled          func() bool // called at Start() to check if remote access is disabled
	PasswordHashSet   func() bool // called at Start() to check if password is configured
	Port              int
	BindAddress       string // server bind address; non-loopback will be rejected
	AllowAutoDownload bool   // allow auto-downloading cloudflared (default should be true)
	SchmuxBinDir      string
	TimeoutMinutes    int
	OnStatusChange    func(TunnelStatus) // callback when tunnel status changes
}

// Manager manages the cloudflared tunnel lifecycle.
type Manager struct {
	config ManagerConfig
	mu     sync.RWMutex
	status TunnelStatus
	cmd    *exec.Cmd
	cancel context.CancelFunc
	logger *log.Logger
}

// NewManager creates a new tunnel manager.
func NewManager(cfg ManagerConfig, logger *log.Logger) *Manager {
	return &Manager{
		config: cfg,
		status: TunnelStatus{State: StateOff},
		logger: logger,
	}
}

// Status returns the current tunnel status.
func (m *Manager) Status() TunnelStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *Manager) setStatus(s TunnelStatus) {
	m.mu.Lock()
	m.status = s
	cb := m.config.OnStatusChange
	m.mu.Unlock()

	if cb != nil {
		cb(s)
	}
}

// Start starts the cloudflared tunnel. Returns error if preconditions not met.
func (m *Manager) Start() error {
	if m.config.Disabled != nil && m.config.Disabled() {
		return fmt.Errorf("remote access is disabled in config")
	}
	if m.config.PasswordHashSet == nil || !m.config.PasswordHashSet() {
		return fmt.Errorf("remote access requires a password (run: schmux remote set-password)")
	}

	// Reject tunnel start when bound to a non-loopback address.
	// When the server listens on 0.0.0.0, any machine on the local network can
	// connect directly (without cloudflared), and such requests lack the
	// Cf-Connecting-IP header used to distinguish local from remote requests.
	// This would allow unauthenticated access from the LAN.
	if addr := m.config.BindAddress; addr != "" {
		ip := net.ParseIP(addr)
		if ip != nil && !ip.IsLoopback() {
			return fmt.Errorf("remote access cannot be started while the server is bound to %s (non-loopback). "+
				"Set bind_address to 127.0.0.1 in config before enabling remote access", addr)
		}
	}

	m.mu.RLock()
	if m.status.State == StateStarting || m.status.State == StateConnected {
		m.mu.RUnlock()
		return fmt.Errorf("tunnel is already %s", m.status.State)
	}
	m.mu.RUnlock()

	// Find or download cloudflared
	var binPath string
	var err error
	if m.config.AllowAutoDownload {
		binPath, err = EnsureCloudflared(m.config.SchmuxBinDir)
	} else {
		binPath, err = FindCloudflared(m.config.SchmuxBinDir)
		if err != nil {
			err = fmt.Errorf("%w (auto-download is disabled — install cloudflared manually: %s)", err, installSuggestion(runtime.GOOS))
		}
	}
	if err != nil {
		return fmt.Errorf("failed to get cloudflared: %w", err)
	}

	m.setStatus(TunnelStatus{State: StateStarting})

	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancel = cancel
	m.mu.Unlock()

	port := m.config.Port
	if port == 0 {
		port = 7337
	}

	cmd := exec.CommandContext(ctx, binPath, "tunnel", "--url", fmt.Sprintf("http://localhost:%d", port))
	cmd.Env = os.Environ()

	// cloudflared prints the URL to stderr
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		m.setStatus(TunnelStatus{State: StateError, Error: err.Error()})
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		m.setStatus(TunnelStatus{State: StateError, Error: err.Error()})
		return fmt.Errorf("failed to start cloudflared: %w", err)
	}

	m.mu.Lock()
	m.cmd = cmd
	m.mu.Unlock()

	// Parse URL from stderr in background
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if m.logger != nil {
				m.logger.Debug("cloudflared", "line", line)
			}
			if url := parseCloudflaredURL(line); url != "" {
				m.setStatus(TunnelStatus{State: StateConnected, URL: url})
			}
		}
	}()

	// Monitor process exit in background
	go func() {
		err := cmd.Wait()
		m.mu.RLock()
		currentState := m.status.State
		m.mu.RUnlock()

		// Only set error if we didn't intentionally stop it
		if currentState != StateOff {
			errMsg := ""
			if err != nil {
				errMsg = err.Error()
			}
			m.setStatus(TunnelStatus{State: StateError, Error: fmt.Sprintf("cloudflared exited unexpectedly: %s", errMsg)})
		}
	}()

	// Auto-timeout if configured
	if m.config.TimeoutMinutes > 0 {
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(m.config.TimeoutMinutes) * time.Minute):
				if m.logger != nil {
					m.logger.Info("tunnel timeout, stopping", "timeout_minutes", m.config.TimeoutMinutes)
				}
				m.Stop()
			}
		}()
	}

	return nil
}

// Stop stops the cloudflared tunnel.
func (m *Manager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	cmd := m.cmd
	m.cmd = nil
	m.cancel = nil
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
		cmd.Wait()
	}

	m.setStatus(TunnelStatus{State: StateOff})
}

var cloudflaredURLRe = regexp.MustCompile(`(https://[a-zA-Z0-9-]+\.trycloudflare\.com)`)

// parseCloudflaredURL extracts the trycloudflare.com URL from a cloudflared log line.
func parseCloudflaredURL(line string) string {
	matches := cloudflaredURLRe.FindStringSubmatch(strings.TrimSpace(line))
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

// IsAvailable reports whether the tunnel module is included in this build.
func IsAvailable() bool { return true }
