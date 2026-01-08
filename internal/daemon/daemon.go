package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sergek/schmux/internal/config"
	"github.com/sergek/schmux/internal/dashboard"
	"github.com/sergek/schmux/internal/session"
	"github.com/sergek/schmux/internal/state"
	"github.com/sergek/schmux/internal/tmux"
	"github.com/sergek/schmux/internal/workspace"
)

const (
	pidFileName   = "daemon.pid"
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
	// Check tmux dependency before forking
	if err := checkTmux(); err != nil {
		return err
	}

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
func Status() (running bool, url string, startedAt string, err error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false, "", "", fmt.Errorf("failed to get home directory: %w", err)
	}

	pidFile := filepath.Join(homeDir, ".schmux", pidFileName)
	startedFile := filepath.Join(homeDir, ".schmux", "daemon.started")

	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "", "", nil
		}
		return false, "", "", fmt.Errorf("failed to read PID file: %w", err)
	}

	var pid int
	if _, err := fmt.Sscanf(string(pidData), "%d", &pid); err != nil {
		return false, "", "", fmt.Errorf("failed to parse PID: %w", err)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false, "", "", fmt.Errorf("failed to find process: %w", err)
	}

	// Check if process is running
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return false, "", "", nil
	}

	url = fmt.Sprintf("http://localhost:%d", dashboardPort)
	if startedData, err := os.ReadFile(startedFile); err == nil {
		startedAt = strings.TrimSpace(string(startedData))
	}
	return true, url, startedAt, nil
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
	startedFile := filepath.Join(schmuxDir, "daemon.started")

	// Write PID file
	pid := os.Getpid()
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", pid)), 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer os.Remove(pidFile)

	// Record daemon start time
	startedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err := os.WriteFile(startedFile, []byte(startedAt+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to write daemon start time: %w", err)
	}

	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Compute state path
	homeDir, err = os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	statePath := filepath.Join(homeDir, ".schmux", "state.json")

	// Load state
	st, err := state.Load(statePath)
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// Create managers
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)

	// Bootstrap log streams for active sessions with missing pipe-pane.
	seedLines := cfg.GetTerminalSeedLines()
	if seedLines <= 0 {
		return fmt.Errorf("terminal.seed_lines must be configured")
	}
	for _, sess := range st.GetSessions() {
		if !tmux.SessionExists(sess.TmuxSession) {
			continue
		}
		if tmux.IsPipePaneActive(sess.TmuxSession) {
			continue
		}

		logPath, err := sm.GetLogPath(sess.ID)
		if err != nil {
			return fmt.Errorf("failed to get log path for %s: %w", sess.ID, err)
		}

		snapshot, err := tmux.CaptureLastLines(sess.TmuxSession, seedLines)
		if err != nil {
			return fmt.Errorf("failed to capture %d lines for %s: %w", seedLines, sess.ID, err)
		}

		if err := os.WriteFile(logPath, []byte(snapshot), 0644); err != nil {
			return fmt.Errorf("failed to seed log file for %s: %w", sess.ID, err)
		}

		if err := tmux.StartPipePane(sess.TmuxSession, logPath); err != nil {
			return fmt.Errorf("failed to attach pipe-pane for %s: %w", sess.ID, err)
		}
	}

	// Start log pruner (every 60 minutes)
	pruneInterval := 60 * time.Minute
	stopLogPruner := sm.StartLogPruner(pruneInterval)
	defer stopLogPruner()

	// Ensure workspace directory exists
	if err := wm.EnsureWorkspaceDir(); err != nil {
		return fmt.Errorf("failed to create workspace directory: %w", err)
	}

	// Create dashboard server
	server := dashboard.NewServer(cfg, st, statePath, sm)

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

// checkTmux verifies that tmux is installed and accessible.
func checkTmux() error {
	cmd := exec.Command("tmux", "-V")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux is not installed or not accessible.\n-> %w", err)
	}
	// tmux -V outputs version info like "tmux 3.3a", this confirms it's working
	if len(output) == 0 {
		return fmt.Errorf("tmux command produced no output")
	}
	return nil
}
