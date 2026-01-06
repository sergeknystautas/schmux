package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sergek/schmux/internal/config"
	"github.com/sergek/schmux/internal/dashboard"
	"github.com/sergek/schmux/internal/state"
	"github.com/sergek/schmux/internal/session"
	"github.com/sergek/schmux/internal/workspace"
)

const (
	pidFileName = "daemon.pid"
	dashboardPort = 7337
)

var (
	shutdownChan = make(chan struct{})
)

// Daemon represents the schmux daemon.
type Daemon struct {
	config    *config.Config
	state     *state.State
	workspace *workspace.Manager
	session   *session.Manager
	server    *dashboard.Server
}

// Start starts the daemon in the background.
func Start() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	schmuxDir := filepath.Join(homeDir, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		return fmt.Errorf("failed to create schmux directory: %w", err)
	}

	pidFile := filepath.Join(schmuxDir, pidFileName)

	// Check if already running
	if _, err := os.Stat(pidFile); err == nil {
		// PID file exists, check if process is running
		pidData, err := os.ReadFile(pidFile)
		if err != nil {
			return fmt.Errorf("failed to read PID file: %w", err)
		}

		var pid int
		if _, err := fmt.Sscanf(string(pidData), "%d", &pid); err == nil {
			process, err := os.FindProcess(pid)
			if err == nil {
				if err := process.Signal(syscall.Signal(0)); err == nil {
					return fmt.Errorf("daemon is already running (PID %d)", pid)
				}
			}
		}

		// Process not running, remove stale PID file
		os.Remove(pidFile)
	}

	// Get the path to the current executable
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Start daemon in background
	cmd := exec.Command(execPath, "daemon-run")
	cmd.Dir, _ = os.Getwd()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Wait a bit for daemon to start
	time.Sleep(100 * time.Millisecond)

	return nil
}

// Stop stops the daemon.
func Stop() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	pidFile := filepath.Join(homeDir, ".schmux", pidFileName)

	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("daemon is not running")
		}
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	var pid int
	if _, err := fmt.Sscanf(string(pidData), "%d", &pid); err != nil {
		return fmt.Errorf("failed to parse PID: %w", err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	// Send SIGTERM
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM: %w", err)
	}

	// Wait for process to exit by polling (process.Wait() doesn't work for non-child processes)
	// Check every 100ms, up to 5 seconds
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		// Check if process still exists by sending signal 0
		if err := process.Signal(syscall.Signal(0)); err != nil {
			// Process has exited
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for daemon to stop")
}

// Status returns the status of the daemon.
func Status() (running bool, url string, err error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false, "", fmt.Errorf("failed to get home directory: %w", err)
	}

	pidFile := filepath.Join(homeDir, ".schmux", pidFileName)

	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "", nil
		}
		return false, "", fmt.Errorf("failed to read PID file: %w", err)
	}

	var pid int
	if _, err := fmt.Sscanf(string(pidData), "%d", &pid); err != nil {
		return false, "", fmt.Errorf("failed to parse PID: %w", err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false, "", fmt.Errorf("failed to find process: %w", err)
	}

	// Check if process is running
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return false, "", nil
	}

	url = fmt.Sprintf("http://localhost:%d", dashboardPort)
	return true, url, nil
}

// Run runs the daemon (this is the entry point for the daemon process).
func Run() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	schmuxDir := filepath.Join(homeDir, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		return fmt.Errorf("failed to create schmux directory: %w", err)
	}

	pidFile := filepath.Join(schmuxDir, pidFileName)

	// Write PID file
	pid := os.Getpid()
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", pid)), 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer os.Remove(pidFile)

	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Load state
	st, err := state.Load()
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// Create managers
	wm := workspace.New(cfg, st)
	sm := session.New(cfg, st, wm)

	// Ensure workspace directory exists
	if err := wm.EnsureWorkspaceDir(); err != nil {
		return fmt.Errorf("failed to create workspace directory: %w", err)
	}

	// Create dashboard server
	server := dashboard.NewServer(cfg, st, sm)

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start dashboard server in background
	serverErrChan := make(chan error, 1)
	go func() {
		if err := server.Start(); err != nil {
			serverErrChan <- err
		}
	}()

	// Wait for shutdown signal or server error
	select {
	case sig := <-sigChan:
		fmt.Printf("Received signal %v, shutting down...\n", sig)
	case err := <-serverErrChan:
		return fmt.Errorf("dashboard server error: %w", err)
	case <-shutdownChan:
		fmt.Println("Shutdown requested")
	}

	// Stop dashboard server
	if err := server.Stop(); err != nil {
		return fmt.Errorf("failed to stop server: %w", err)
	}

	return nil
}

// Shutdown triggers a graceful shutdown.
func Shutdown() {
	close(shutdownChan)
}
