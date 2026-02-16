package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
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
	Disabled       bool
	PinHashSet     bool
	Port           int
	SchmuxBinDir   string
	TimeoutMinutes int
	OnStatusChange func(TunnelStatus) // callback when tunnel status changes
}

// Manager manages the cloudflared tunnel lifecycle.
type Manager struct {
	config ManagerConfig
	mu     sync.RWMutex
	status TunnelStatus
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

// NewManager creates a new tunnel manager.
func NewManager(cfg ManagerConfig) *Manager {
	return &Manager{
		config: cfg,
		status: TunnelStatus{State: StateOff},
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
	if m.config.Disabled {
		return fmt.Errorf("remote access is disabled in config")
	}
	if !m.config.PinHashSet {
		return fmt.Errorf("remote access requires a PIN (run: schmux remote set-pin)")
	}

	m.mu.RLock()
	if m.status.State == StateStarting || m.status.State == StateConnected {
		m.mu.RUnlock()
		return fmt.Errorf("tunnel is already %s", m.status.State)
	}
	m.mu.RUnlock()

	// Find or download cloudflared
	binPath, err := EnsureCloudflared(m.config.SchmuxBinDir)
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
			fmt.Printf("[remote-access] %s\n", line)
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
				fmt.Printf("[remote-access] tunnel timeout after %d minutes, stopping\n", m.config.TimeoutMinutes)
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
