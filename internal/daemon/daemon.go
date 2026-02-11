package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/dashboard"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/nudgenik"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/remote"
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
	if cfg, err := config.Load(filepath.Join(homeDir, ".schmux", "config.json")); err == nil {
		if cfg.GetAuthEnabled() && cfg.GetPublicBaseURL() != "" {
			url = cfg.GetPublicBaseURL()
		}
	}
	if startedData, err := os.ReadFile(startedFile); err == nil {
		startedAt = strings.TrimSpace(string(startedData))
	}
	return true, url, startedAt, nil
}

// Run runs the daemon (this is the entry point for the daemon process).
// If background is true, SIGINT/SIGQUIT are ignored (for start command).
// If devProxy is true, non-API requests are proxied to Vite dev server.
func Run(background bool, devProxy bool) error {
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

	// Write all schemas on startup (ensures they're always up to date)
	if err := oneshot.WriteAllSchemas(); err != nil {
		return fmt.Errorf("failed to write schemas: %w", err)
	}

	// Load config
	configPath := filepath.Join(schmuxDir, "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if cfg.GetAuthEnabled() {
		if _, err := config.EnsureSessionSecret(); err != nil {
			return fmt.Errorf("failed to initialize auth session secret: %w", err)
		}
	}

	// Compute state path
	statePath := filepath.Join(schmuxDir, "state.json")

	// Load state
	st, err := state.Load(statePath)
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// Verify we can access tmux sessions for existing sessions
	if err := validateSessionAccess(st); err != nil {
		return err
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
		fmt.Printf("[workspace] warning: failed to ensure overlay directories: %v\n", err)
		// Don't fail daemon startup for this
	}

	// Detect run targets once on daemon start and persist to config
	detectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	detectedTargets, err := detect.DetectAvailableToolsContext(detectCtx, false)
	cancel()
	if err != nil {
		fmt.Printf("[config] warning: failed to detect run targets: %v\n", err)
	} else {
		cfg.RunTargets = config.MergeDetectedRunTargets(cfg.RunTargets, detectedTargets)
		if err := cfg.Validate(); err != nil {
			fmt.Printf("[config] warning: failed to validate config after detection: %v\n", err)
		} else if err := cfg.Save(); err != nil {
			fmt.Printf("[config] warning: failed to save config after detection: %v\n", err)
		}
	}

	// Start output trackers for running sessions restored from state.
	for _, sess := range st.GetSessions() {
		timeoutCtx, cancel := context.WithTimeout(shutdownCtx, cfg.XtermQueryTimeout())
		exists := tmux.SessionExists(timeoutCtx, sess.TmuxSession)
		cancel()
		if !exists {
			continue
		}
		if err := sm.EnsureTracker(sess.ID); err != nil {
			fmt.Printf("[session] warning: failed to start tracker for %s: %v\n", sess.ID, err)
		}
	}

	// Ensure workspace directory exists
	if err := wm.EnsureWorkspaceDir(); err != nil {
		return fmt.Errorf("failed to create workspace directory: %w", err)
	}

	// Create GitHub PR discovery service
	prDiscovery := github.NewDiscovery()

	// Seed discovery from persisted state (avoids API call on restart)
	if prs := st.GetPullRequests(); len(prs) > 0 {
		prDiscovery.Seed(prs, st.GetPublicRepos())
		fmt.Printf("[daemon] loaded %d cached PRs from state\n", len(prs))
	}

	// Create dashboard server
	server := dashboard.NewServer(cfg, st, statePath, sm, wm, prDiscovery, Shutdown, devProxy)

	// Create remote manager for remote workspace support
	remoteManager := remote.NewManager(cfg, st)
	remoteManager.SetStateChangeCallback(server.BroadcastSessions)
	server.SetRemoteManager(remoteManager)
	sm.SetRemoteManager(remoteManager)

	// Mark stale remote hosts as disconnected at startup.
	// Hosts that were "connected" in state are stale (SSH/ET processes are gone).
	// Don't auto-reconnect — reconnection requires interactive auth (e.g., Yubikey)
	// that can only happen when the user clicks "Reconnect" in the dashboard.
	staleHosts := remoteManager.MarkStaleHostsDisconnected()
	if staleHosts > 0 {
		fmt.Printf("[daemon] marked %d stale remote host(s) as disconnected\n", staleHosts)
	}

	// Start background goroutine to prune expired remote hosts
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				remoteManager.PruneExpiredHosts()
			case <-shutdownCtx.Done():
				return
			}
		}
	}()

	// Create and start git watcher for filesystem-based change detection.
	// Started after server creation so broadcasts reach WebSocket clients.
	gitWatcher := workspace.NewGitWatcher(cfg, wm, server.BroadcastSessions)
	if gitWatcher != nil {
		wm.SetGitWatcher(gitWatcher)
		// Add watches for all existing local workspaces (skip remote ones)
		for _, w := range st.GetWorkspaces() {
			if w.RemoteHostID == "" {
				gitWatcher.AddWorkspace(w.ID, w.Path)
			}
		}
		gitWatcher.Start()
	}

	// Start background goroutine to update git status for all workspaces.
	// Started after EnsureWorkspaceDir to avoid race with directory creation.
	// Started after server creation so it can broadcast updates to WebSocket clients.
	go func() {
		pollInterval := time.Duration(cfg.GetGitStatusPollIntervalMs()) * time.Millisecond
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		// Do initial update immediately on startup
		select {
		case <-shutdownCtx.Done():
			return
		default:
			ctx, cancel := context.WithTimeout(shutdownCtx, cfg.GitStatusTimeout())
			// Ensure origin query repos exist for branch queries (creates if missing)
			if err := wm.EnsureOriginQueries(ctx); err != nil {
				fmt.Printf("[daemon] warning: failed to ensure origin queries: %v\n", err)
			}
			// Fetch origin query repos to get latest branch info
			wm.FetchOriginQueries(ctx)
			wm.UpdateAllGitStatus(ctx)
			cancel()
			server.BroadcastSessions()
		}
		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(shutdownCtx, cfg.GitStatusTimeout())
				// Ensure origin query repos exist (in case new repos were added)
				if err := wm.EnsureOriginQueries(ctx); err != nil {
					fmt.Printf("[daemon] warning: failed to ensure origin queries: %v\n", err)
				}
				// Fetch origin query repos to get latest branch info
				wm.FetchOriginQueries(ctx)
				wm.UpdateAllGitStatus(ctx)
				cancel()
				server.BroadcastSessions()
			case <-shutdownCtx.Done():
				return
			}
		}
	}()

	// Start background goroutine to check for inactive sessions and ask NudgeNik
	go startNudgeNikChecker(shutdownCtx, cfg, st, sm, server.BroadcastSessions)

	// Initialize PR discovery polling based on current config
	// Pass a function so poll always uses current repos list
	prDiscovery.SetTarget(cfg.GetPrReviewTarget(), func() []config.Repo { return cfg.GetRepos() })
	defer prDiscovery.Stop()

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
		fmt.Printf("[daemon] received signal %v, shutting down\n", sig)
	case err := <-serverErrChan:
		return fmt.Errorf("dashboard server error: %w", err)
	case <-shutdownChan:
		fmt.Println("[daemon] shutdown requested")
	}

	// Stop git watcher
	if gitWatcher != nil {
		gitWatcher.Stop()
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

// startNudgeNikChecker starts a background goroutine that checks for inactive sessions
// and automatically asks NudgeNik for consultation.
func startNudgeNikChecker(ctx context.Context, cfg *config.Config, st *state.State, sm *session.Manager, onUpdate func()) {
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
			checkInactiveSessionsForNudge(ctx, cfg, st, sm, onUpdate)
		case <-ctx.Done():
			return
		}
	}
}

// checkInactiveSessionsForNudge checks all sessions for inactivity and asks NudgeNik if needed.
func checkInactiveSessionsForNudge(ctx context.Context, cfg *config.Config, st *state.State, sm *session.Manager, onUpdate func()) {
	// Check if nudgenik is enabled (non-empty target)
	target := cfg.GetNudgenikTarget()
	if target == "" {
		return
	}

	now := time.Now()
	sessions := st.GetSessions()

	for _, sess := range sessions {
		// Skip if already has a nudge
		if sess.Nudge != "" {
			continue
		}

		// Skip nudgenik if session has recent direct signal (< 5 minutes).
		// Direct signaling is more reliable and cheaper when available.
		if !sess.LastSignalAt.IsZero() && now.Sub(sess.LastSignalAt) < 5*time.Minute {
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
		targetName := cfg.GetNudgenikTarget()
		fmt.Printf("[nudgenik] %s - asking %s\n", sess.ID, targetName)
		nudge := askNudgeNikForSession(ctx, cfg, sess)
		if nudge != "" {
			sess.Nudge = nudge
			if err := st.UpdateSession(sess); err != nil {
				fmt.Printf("[nudgenik] %s - failed to save nudge: %v\n", sess.ID, err)
			} else if err := st.Save(); err != nil {
				fmt.Printf("[nudgenik] %s - failed to persist state: %v\n", sess.ID, err)
			} else {
				fmt.Printf("[nudgenik] %s - saved nudge\n", sess.ID)
				if onUpdate != nil {
					onUpdate()
				}
			}
		}
	}
}

// askNudgeNikForSession captures the session output and asks NudgeNik for consultation.
func askNudgeNikForSession(ctx context.Context, cfg *config.Config, sess state.Session) string {
	result, err := nudgenik.AskForSession(ctx, cfg, sess)
	if err != nil {
		switch {
		case errors.Is(err, nudgenik.ErrDisabled):
			// Silently skip - nudgenik is disabled
		case errors.Is(err, nudgenik.ErrNoResponse):
			fmt.Printf("[nudgenik] %s - no response extracted\n", sess.ID)
		case errors.Is(err, nudgenik.ErrTargetNotFound):
			fmt.Printf("[nudgenik] target not found in config\n")
		case errors.Is(err, nudgenik.ErrTargetNoSecrets):
			fmt.Printf("[nudgenik] target missing required secrets\n")
		default:
			fmt.Printf("[nudgenik] %s - failed to ask: %v\n", sess.ID, err)
		}
		return ""
	}

	payload, err := json.Marshal(result)
	if err != nil {
		fmt.Printf("[nudgenik] %s - failed to serialize result: %v\n", sess.ID, err)
		return ""
	}

	return string(payload)
}

// validateSessionAccess checks for user mismatch between daemon and tmux server.
// Returns an error if sessions exist and tmux is running under a different user.
func validateSessionAccess(st *state.State) error {
	sessions := st.GetSessions()
	if len(sessions) == 0 {
		return nil
	}

	currentUID := os.Getuid()

	// Check if we have a tmux server running (socket exists)
	ourSocket := fmt.Sprintf("/tmp/tmux-%d/default", currentUID)
	if _, err := os.Stat(ourSocket); err == nil {
		// We have a tmux server, we can access sessions
		return nil
	}

	// We don't have a tmux server - check if another user does
	otherOwners := findOtherTmuxServerOwners(currentUID)
	if len(otherOwners) == 0 {
		// No tmux servers at all - sessions are stale but that's not a user mismatch
		return nil
	}

	// There's a tmux server owned by someone else - that's the problem
	currentUser := "unknown"
	if u, err := user.Current(); err == nil {
		currentUser = u.Username
	}

	var msg strings.Builder
	msg.WriteString("Tmux server running under different user\n")
	msg.WriteString(fmt.Sprintf("  schmux daemon running as: %s (uid %d)\n", currentUser, currentUID))
	msg.WriteString(fmt.Sprintf("  Tmux server owned by: %s\n", strings.Join(otherOwners, ", ")))
	msg.WriteString("Run the daemon as the same user that owns the tmux server.")

	return errors.New(msg.String())
}

// findOtherTmuxServerOwners finds tmux servers owned by users other than currentUID.
// Only returns users whose tmux server socket actually exists.
func findOtherTmuxServerOwners(currentUID int) []string {
	owners := []string{}
	entries, err := filepath.Glob("/tmp/tmux-*")
	if err != nil {
		return owners
	}

	for _, entry := range entries {
		// Extract UID from directory name (e.g., /tmp/tmux-501)
		base := filepath.Base(entry)
		if !strings.HasPrefix(base, "tmux-") {
			continue
		}
		uidStr := strings.TrimPrefix(base, "tmux-")
		uid, err := strconv.Atoi(uidStr)
		if err != nil {
			continue
		}

		// Skip our own UID
		if uid == currentUID {
			continue
		}

		// Check if the socket actually exists (server is running)
		socketPath := filepath.Join(entry, "default")
		if _, err := os.Stat(socketPath); err != nil {
			continue
		}

		// Look up username for this UID
		u, err := user.LookupId(strconv.Itoa(uid))
		if err != nil {
			owners = append(owners, fmt.Sprintf("uid %d", uid))
		} else {
			owners = append(owners, fmt.Sprintf("%s (uid %d)", u.Username, uid))
		}
	}

	return owners
}
