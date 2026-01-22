package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/dashboard"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/nudgenik"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/tmux"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

const (
	pidFileName   = "daemon.pid"
	dashboardPort = 7337

	// Inactivity threshold before asking NudgeNik
	nudgeInactivityThreshold = 15 * time.Second
)

var (
	shutdownChan = make(chan struct{})
	shutdownCtx  context.Context
	cancelFunc   context.CancelFunc
)

func init() {
	shutdownCtx, cancelFunc = context.WithCancel(context.Background())
}

// Daemon represents the schmux daemon.
type Daemon struct {
	config    *config.Config
	state     state.StateStore
	workspace workspace.WorkspaceManager
	session   *session.Manager
	server    *dashboard.Server
}

// ValidateReadyToRun checks if the system is ready to run the daemon.
// It verifies tmux is available, the schmux directory exists, and
// that no daemon is already running. Called by both 'start' and 'daemon-run'
// before they diverge.
func ValidateReadyToRun() error {
	// Check tmux dependency before forking
	if err := tmux.TmuxChecker.Check(); err != nil {
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

	return nil
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

	// Open log file for daemon stdout/stderr
	logFile := filepath.Join(schmuxDir, "daemon-startup.log")
	logF, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	// Get the path to the current executable
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Start daemon in background
	cmd := exec.Command(execPath, "daemon-run", "--background")
	cmd.Dir, _ = os.Getwd()
	cmd.Stdout = logF
	cmd.Stderr = logF

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Wait a bit for daemon to start
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-time.After(100 * time.Millisecond):
		// Daemon started successfully
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for daemon to start")
	}

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
// If background is true, SIGINT/SIGQUIT are ignored (for start command).
func Run(background bool) error {
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
	configPath := filepath.Join(schmuxDir, "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Compute state path
	statePath := filepath.Join(schmuxDir, "state.json")

	// Load state
	st, err := state.Load(statePath)
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// Clear needs_restart flag on daemon start (config changes now taking effect)
	if st.GetNeedsRestart() {
		st.SetNeedsRestart(false)
		st.Save()
	}

	// Create managers
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)

	// Ensure overlay directories exist for all repos
	if err := wm.EnsureOverlayDirs(cfg.GetRepos()); err != nil {
		fmt.Printf("warning: failed to ensure overlay directories: %v\n", err)
		// Don't fail daemon startup for this
	}

	// Detect run targets once on daemon start and persist to config
	detectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	detectedTargets, err := detect.DetectAvailableToolsContext(detectCtx, false)
	cancel()
	if err != nil {
		fmt.Printf("warning: failed to detect run targets: %v\n", err)
	} else {
		cfg.RunTargets = config.MergeDetectedRunTargets(cfg.RunTargets, detectedTargets)
		if err := cfg.Validate(); err != nil {
			fmt.Printf("warning: failed to validate config after detection: %v\n", err)
		} else if err := cfg.Save(); err != nil {
			fmt.Printf("warning: failed to save config after detection: %v\n", err)
		}
	}

	// Initialize LastOutputAt from log file mtimes for existing sessions
	for _, sess := range st.GetSessions() {
		logPath, err := sm.GetLogPath(sess.ID)
		if err != nil {
			continue
		}
		if info, err := os.Stat(logPath); err == nil {
			sess.LastOutputAt = info.ModTime()
			if err := st.UpdateSession(sess); err != nil {
				fmt.Printf("warning: failed to update session %s: %v\n", sess.ID, err)
			}
		}
	}

	// Start background goroutine to monitor log file mtimes for all sessions
	go func() {
		pollInterval := time.Duration(cfg.GetXtermMtimePollIntervalMs()) * time.Millisecond
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(shutdownCtx, cfg.XtermQueryTimeout())
				for _, sess := range st.GetSessions() {
					if !sm.IsRunning(ctx, sess.ID) {
						continue
					}
					logPath, err := sm.GetLogPath(sess.ID)
					if err != nil {
						continue
					}
					if info, err := os.Stat(logPath); err == nil {
						if info.ModTime().After(sess.LastOutputAt) {
							st.UpdateSessionLastOutput(sess.ID, info.ModTime())
						}
					}
				}
				cancel()
			case <-shutdownCtx.Done():
				return
			}
		}
	}()

	// Start background goroutine to update git status for all workspaces
	go func() {
		pollInterval := time.Duration(cfg.GetGitStatusPollIntervalMs()) * time.Millisecond
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		// Do initial update after a short delay to let daemon start
		select {
		case <-time.After(500 * time.Millisecond):
			ctx, cancel := context.WithTimeout(shutdownCtx, cfg.GitStatusTimeout())
			wm.UpdateAllGitStatus(ctx)
			cancel()
		case <-shutdownCtx.Done():
			return
		}
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(shutdownCtx, cfg.GitStatusTimeout())
				wm.UpdateAllGitStatus(ctx)
				cancel()
			case <-shutdownCtx.Done():
				return
			}
		}
	}()

	// Start background goroutine to check for inactive sessions and ask NudgeNik
	go startNudgeNikChecker(shutdownCtx, cfg, st, sm)

	// Bootstrap log streams for active sessions with missing pipe-pane.
	seedLines := cfg.GetTerminalSeedLines()
	if seedLines <= 0 {
		return fmt.Errorf("terminal.seed_lines must be configured")
	}
	for _, sess := range st.GetSessions() {
		if err := bootstrapSession(shutdownCtx, sess, sm, cfg, seedLines); err != nil {
			return err
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
	server := dashboard.NewServer(cfg, st, statePath, sm, wm, Shutdown)

	// Log where dashboard assets are being served from
	server.LogDashboardAssetPath()

	// Start async version check
	server.StartVersionCheck()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	if background {
		// Ignore SIGINT/SIGQUIT when running in background (started via 'start' command)
		// This prevents Ctrl-C from killing the daemon when tailing logs
		signal.Ignore(syscall.SIGINT, syscall.SIGQUIT)
		signal.Notify(sigChan, syscall.SIGTERM)
	} else {
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	}

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
	if cancelFunc != nil {
		cancelFunc()
	}
}

// bootstrapSession bootstraps a session's log streaming if needed.
func bootstrapSession(ctx context.Context, sess state.Session, sm *session.Manager, cfg *config.Config, seedLines int) error {
	// Check if session exists
	timeoutCtx, cancel := context.WithTimeout(ctx, cfg.XtermQueryTimeout())
	if !tmux.SessionExists(timeoutCtx, sess.TmuxSession) {
		cancel()
		return nil
	}
	cancel()

	// Check if pipe-pane is active
	timeoutCtx, cancel = context.WithTimeout(ctx, cfg.XtermQueryTimeout())
	pipePaneActive := tmux.IsPipePaneActive(timeoutCtx, sess.TmuxSession)
	cancel()

	// Get log path
	logPath, err := sm.GetLogPath(sess.ID)
	if err != nil {
		return fmt.Errorf("failed to get log path for %s: %w", sess.ID, err)
	}

	// Check if log file exists
	_, err = os.Stat(logPath)
	logFileExists := !os.IsNotExist(err)

	// Skip if pipe-pane is active AND log file exists (everything is good)
	if pipePaneActive && logFileExists {
		return nil
	}

	// If pipe-pane is active but log is missing, stop the old pipe-pane
	if pipePaneActive && !logFileExists {
		fmt.Printf("[bootstrap] %s: pipe-pane active but log missing, stopping pipe-pane\n", sess.ID)
		timeoutCtx, cancel = context.WithTimeout(ctx, cfg.XtermOperationTimeout())
		if err := tmux.StopPipePane(timeoutCtx, sess.TmuxSession); err != nil {
			cancel()
			return fmt.Errorf("failed to stop pipe-pane for %s: %w", sess.ID, err)
		}
		cancel()
	}

	if !pipePaneActive {
		fmt.Printf("[bootstrap] %s: pipe-pane not active, bootstrapping\n", sess.ID)
	}

	// Bootstrap: capture screen content and write to log, start pipe-pane
	timeoutCtx, cancel = context.WithTimeout(ctx, cfg.XtermOperationTimeout())
	snapshot, err := tmux.CaptureLastLines(timeoutCtx, sess.TmuxSession, seedLines)
	cancel()
	if err != nil {
		return fmt.Errorf("failed to capture %d lines for %s: %w", seedLines, sess.ID, err)
	}

	if err := os.WriteFile(logPath, []byte(snapshot), 0644); err != nil {
		return fmt.Errorf("failed to seed log file for %s: %w", sess.ID, err)
	}

	timeoutCtx, cancel = context.WithTimeout(ctx, cfg.XtermOperationTimeout())
	if err := tmux.StartPipePane(timeoutCtx, sess.TmuxSession, logPath); err != nil {
		cancel()
		return fmt.Errorf("failed to attach pipe-pane for %s: %w", sess.ID, err)
	}
	cancel()
	fmt.Printf("[bootstrap] %s: pipe-pane started\n", sess.ID)
	return nil
}

// startNudgeNikChecker starts a background goroutine that checks for inactive sessions
// and automatically asks NudgeNik for consultation.
func startNudgeNikChecker(ctx context.Context, cfg *config.Config, st *state.State, sm *session.Manager) {
	// Check every 15 seconds
	pollInterval := 15 * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Wait a bit before first check to let daemon start
	select {
	case <-time.After(10 * time.Second):
		// Ready to start checking
	case <-ctx.Done():
		return
	}

	for {
		select {
		case <-ticker.C:
			checkInactiveSessionsForNudge(ctx, cfg, st, sm)
		case <-ctx.Done():
			return
		}
	}
}

// checkInactiveSessionsForNudge checks all sessions for inactivity and asks NudgeNik if needed.
func checkInactiveSessionsForNudge(ctx context.Context, cfg *config.Config, st *state.State, sm *session.Manager) {
	now := time.Now()
	sessions := st.GetSessions()

	for _, sess := range sessions {
		// Skip if already has a nudge
		if sess.Nudge != "" {
			continue
		}

		// Skip if session is not running
		timeoutCtx, cancel := context.WithTimeout(ctx, cfg.XtermQueryTimeout())
		running := sm.IsRunning(timeoutCtx, sess.ID)
		cancel()
		if !running {
			continue
		}

		// Check if inactive for threshold
		if !sess.LastOutputAt.IsZero() && now.Sub(sess.LastOutputAt) < nudgeInactivityThreshold {
			continue
		}

		// Session is inactive and has no nudge, ask NudgeNik
		targetName := "claude"
		if cfg != nil {
			if configured := cfg.GetNudgenikTarget(); configured != "" {
				targetName = configured
			}
		}
		fmt.Printf("[nudgenik] asking %s for session %s\n", targetName, sess.ID)
		nudge := askNudgeNikForSession(ctx, cfg, sess)
		if nudge != "" {
			sess.Nudge = nudge
			if err := st.UpdateSession(sess); err != nil {
				fmt.Printf("[nudgenik] failed to save nudge for %s: %v\n", sess.ID, err)
			} else if err := st.Save(); err != nil {
				fmt.Printf("[nudgenik] failed to persist state for %s: %v\n", sess.ID, err)
			} else {
				fmt.Printf("[nudgenik] saved nudge for %s\n", sess.ID)
			}
		}
	}
}

// askNudgeNikForSession captures the session output and asks NudgeNik for consultation.
func askNudgeNikForSession(ctx context.Context, cfg *config.Config, sess state.Session) string {
	result, err := nudgenik.AskForSession(ctx, cfg, sess)
	if err != nil {
		switch {
		case errors.Is(err, nudgenik.ErrNoResponse):
			fmt.Printf("[nudgenik] no response extracted from session %s\n", sess.ID)
		case errors.Is(err, nudgenik.ErrTargetNotFound):
			fmt.Printf("[nudgenik] target not found in config\n")
		case errors.Is(err, nudgenik.ErrTargetNoSecrets):
			fmt.Printf("[nudgenik] target missing required secrets\n")
		default:
			fmt.Printf("[nudgenik] failed to ask for session %s: %v\n", sess.ID, err)
		}
		return ""
	}

	payload, err := json.Marshal(result)
	if err != nil {
		fmt.Printf("[nudgenik] failed to serialize result for session %s: %v\n", sess.ID, err)
		return ""
	}

	return string(payload)
}
