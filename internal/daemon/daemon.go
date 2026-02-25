package daemon

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/compound"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/dashboard"
	"github.com/sergeknystautas/schmux/internal/dashboardsx"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/difftool"
	"github.com/sergeknystautas/schmux/internal/events"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/lore"
	"github.com/sergeknystautas/schmux/internal/nudgenik"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/remote"
	"github.com/sergeknystautas/schmux/internal/schema"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/subreddit"
	"github.com/sergeknystautas/schmux/internal/telemetry"
	"github.com/sergeknystautas/schmux/internal/tmux"
	"github.com/sergeknystautas/schmux/internal/tunnel"
	"github.com/sergeknystautas/schmux/internal/update"
	"github.com/sergeknystautas/schmux/internal/version"
	"github.com/sergeknystautas/schmux/internal/workspace"
	"github.com/sergeknystautas/schmux/internal/workspace/ensure"
)

const (
	pidFileName   = "daemon.pid"
	dashboardPort = 7337

	// Inactivity threshold before asking NudgeNik
	nudgeInactivityThreshold = 15 * time.Second
)

// ErrDevRestart is returned by Run() when the daemon needs to restart
// for a dev mode workspace switch. The caller should exit with code 42.
var ErrDevRestart = errors.New("dev restart requested")

// Daemon represents the schmux daemon.
type Daemon struct {
	config    *config.Config
	state     state.StateStore
	workspace workspace.WorkspaceManager
	session   *session.Manager
	server    *dashboard.Server
	logger    *log.Logger

	shutdownChan   chan struct{}
	shutdownOnce   sync.Once
	devRestartChan chan struct{}
	devRestartOnce sync.Once
	shutdownCtx    context.Context
	cancelFunc     context.CancelFunc

	githubStatus contracts.GitHubStatus
}

// NewDaemon creates a new Daemon with initialized channels and context.
func NewDaemon() *Daemon {
	ctx, cancel := context.WithCancel(context.Background())
	return &Daemon{
		shutdownChan:   make(chan struct{}),
		devRestartChan: make(chan struct{}),
		shutdownCtx:    ctx,
		cancelFunc:     cancel,
	}
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
		// Use configured port if available (may differ from default dashboardPort)
		if cfgPort := cfg.GetPort(); cfgPort != 0 {
			url = fmt.Sprintf("http://localhost:%d", cfgPort)
		}
		if cfg.GetPublicBaseURL() != "" {
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
// If devMode is true, dev mode API endpoints are enabled and the daemon
// can exit with ErrDevRestart (exit code 42) for workspace switching.
func (d *Daemon) Run(background bool, devProxy bool, devMode bool) error {
	d.logger = logging.New(devMode)
	logger := d.logger
	telemetryLog := logging.Sub(logger, "telemetry")
	workspaceLog := logging.Sub(logger, "workspace")
	configLog := logging.Sub(logger, "config")
	compoundLog := logging.Sub(logger, "compound")
	overlayLog := logging.Sub(logger, "overlay")
	loreLog := logging.Sub(logger, "lore")
	sessionLog := logging.Sub(logger, "session")
	nudgenikLog := logging.Sub(logger, "nudgenik")
	gitWatcherLog := logging.Sub(logger, "git-watcher")
	stateLog := logging.Sub(logger, "state")
	githubLog := logging.Sub(logger, "github")
	remoteLog := logging.Sub(logger, "remote")
	remoteAccessLog := logging.Sub(logger, "remote-access")

	// Set package-level loggers for packages that use standalone functions
	tmux.SetLogger(logging.Sub(logger, "tmux"))
	lore.SetLogger(loreLog)
	compound.SetLogger(compoundLog)
	config.SetLogger(configLog)
	detect.SetLogger(logging.Sub(configLog, "detect"))
	update.SetLogger(logging.Sub(logger, "update"))
	tunnel.SetLogger(remoteAccessLog)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	schmuxDir := filepath.Join(homeDir, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		return fmt.Errorf("failed to create schmux directory: %w", err)
	}

	// Ensure centralized hook scripts are written to ~/.schmux/hooks/
	hooksDir, err := ensure.EnsureGlobalHookScripts(homeDir)
	if err != nil {
		logger.Warn("failed to write global hook scripts", "err", err)
	}

	// Dev mode: backup config files before loading
	if devMode {
		if err := createDevConfigBackup(schmuxDir); err != nil {
			logger.Warn("failed to create dev config backup", "err", err)
		}
		// Cleanup old backups (>3 days)
		backupDir := filepath.Join(schmuxDir, "backups")
		cleanupOldBackups(backupDir, 3*24*time.Hour)
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

	// Check dashboard.sx certificate expiry and start background services
	if cfg.GetDashboardSXEnabled() {
		if status, err := dashboardsx.GetStatus(cfg); err == nil && status.HasCert && status.DaysUntilExpiry < 30 {
			fmt.Printf("[dashboardsx] certificate expires in %d days — run 'schmux dashboardsx renew-cert'\n", status.DaysUntilExpiry)
		}

		// Start heartbeat and auto-renewal goroutines
		instanceKey, err := dashboardsx.EnsureInstanceKey()
		if err != nil {
			fmt.Printf("[dashboardsx] warning: failed to read instance key: %v\n", err)
		} else {
			serviceURL := dashboardsx.DefaultServiceURL
			if cfg.Network != nil && cfg.Network.DashboardSX != nil && cfg.Network.DashboardSX.ServiceURL != "" {
				serviceURL = cfg.Network.DashboardSX.ServiceURL
			}
			client := dashboardsx.NewClient(serviceURL, instanceKey, cfg.GetDashboardSXCode())

			go dashboardsx.StartHeartbeat(d.shutdownCtx, client)

			if email := cfg.GetDashboardSXEmail(); email != "" {
				go dashboardsx.StartAutoRenewal(d.shutdownCtx, client, email)
			}
		}
	}

	// Initialize telemetry
	var tel telemetry.Telemetry = &telemetry.NoopTelemetry{}
	if cfg.GetTelemetryEnabled() {
		// Ensure installation ID exists
		installID := cfg.GetInstallationID()
		if installID == "" {
			installID = uuid.New().String()
			cfg.SetInstallationID(installID)
			if err := cfg.Save(); err != nil {
				telemetryLog.Warn("failed to save installation ID", "err", err)
			}
		}

		tel = telemetry.New(installID, telemetryLog)
		if _, ok := tel.(*telemetry.Client); ok {
			telemetryLog.Info("anonymous usage metrics enabled (opt out: set telemetry_enabled=false in config)")
		}
	}

	// Track daemon startup
	tel.Track("daemon_started", map[string]any{
		"version": version.Version,
	})

	// Compute state path
	statePath := filepath.Join(schmuxDir, "state.json")

	// Load state
	st, err := state.Load(statePath, stateLog)
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
	ensure.SetLogger(logging.Sub(workspaceLog, "ensure"))
	wm := workspace.New(cfg, st, statePath, workspaceLog)
	sm := session.New(cfg, st, statePath, wm, sessionLog)

	// Wire telemetry to managers
	wm.SetTelemetry(tel)
	sm.SetTelemetry(tel)

	// Wire centralized hooks directory to managers
	wm.SetHooksDir(hooksDir)
	sm.SetHooksDir(hooksDir)

	// Ensure overlay directories exist for all repos
	if err := wm.EnsureOverlayDirs(cfg.GetRepos()); err != nil {
		workspaceLog.Warn("failed to ensure overlay directories", "err", err)
		// Don't fail daemon startup for this
	}

	// Ensure all workspaces have the necessary schmux configuration
	wm.EnsureAll()

	// Detect run targets once on daemon start and persist to config
	detectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	detectedTargets, err := detect.DetectAvailableToolsContext(detectCtx, false)
	cancel()
	if err != nil {
		configLog.Warn("failed to detect run targets", "err", err)
	} else {
		cfg.RunTargets = config.MergeDetectedRunTargets(cfg.RunTargets, detectedTargets)
		if err := cfg.Validate(); err != nil {
			configLog.Warn("failed to validate config after detection", "err", err)
		} else if err := cfg.Save(); err != nil {
			configLog.Warn("failed to save config after detection", "err", err)
		}
	}

	// Ensure workspace directory exists
	if err := wm.EnsureWorkspaceDir(); err != nil {
		return fmt.Errorf("failed to create workspace directory: %w", err)
	}

	// Create GitHub PR discovery service
	prDiscovery := github.NewDiscovery(githubLog)

	// Seed discovery from persisted state (avoids API call on restart)
	if prs := st.GetPullRequests(); len(prs) > 0 {
		prDiscovery.Seed(prs, st.GetPublicRepos())
		logger.Info("loaded cached PRs from state", "count", len(prs))
	}

	// Check gh CLI authentication
	d.githubStatus = github.CheckAuth(d.shutdownCtx)
	if d.githubStatus.Available {
		fmt.Printf("[daemon] GitHub authenticated as %s\n", d.githubStatus.Username)
	} else {
		fmt.Println("[daemon] GitHub not available (gh CLI not installed or not authenticated)")
	}

	// Create dashboard server
	server := dashboard.NewServer(cfg, st, statePath, sm, wm, prDiscovery, logger, d.githubStatus, dashboard.ServerOptions{
		Shutdown:    d.Shutdown,
		DevRestart:  d.DevRestart,
		DevProxy:    devProxy,
		DevMode:     devMode,
		ShutdownCtx: d.shutdownCtx,
	})

	// Create remote manager for remote workspace support
	remoteManager := remote.NewManager(cfg, st, remoteLog)
	remoteManager.SetStateChangeCallback(server.BroadcastSessions)
	server.SetRemoteManager(remoteManager)
	sm.SetRemoteManager(remoteManager)

	// Create tunnel manager for remote access
	tunnelMgr := tunnel.NewManager(tunnel.ManagerConfig{
		Disabled:          func() bool { return !cfg.GetRemoteAccessEnabled() },
		PasswordHashSet:   func() bool { return cfg.GetRemoteAccessPasswordHash() != "" },
		Port:              cfg.GetPort(),
		BindAddress:       cfg.GetBindAddress(),
		AllowAutoDownload: cfg.GetRemoteAccessAllowAutoDownload(),
		SchmuxBinDir:      filepath.Join(filepath.Dir(statePath), "bin"),
		TimeoutMinutes:    cfg.GetRemoteAccessTimeoutMinutes(),
		OnStatusChange: func(status tunnel.TunnelStatus) {
			server.BroadcastTunnelStatus(status)
			if status.State == tunnel.StateConnected && status.URL != "" {
				server.HandleTunnelConnected(status.URL)
			}
		},
	}, remoteAccessLog)
	server.SetTunnelManager(tunnelMgr)

	// Wire event system: event watcher → session manager → dashboard server
	dashHandler := events.NewDashboardHandler(func(sessionID, state, message, intent, blockers string) {
		server.HandleStatusEvent(sessionID, state, message, intent, blockers)
	})
	eventHandlers := map[string][]events.EventHandler{
		"status": {dashHandler},
	}

	// Dev mode: add monitor handler that forwards all events to WebSocket
	if devMode {
		monitorHandler := events.NewMonitorHandler(func(sessionID string, raw events.RawEvent, data []byte) {
			server.BroadcastEvent(sessionID, data)
		})
		for _, eventType := range []string{"status", "failure", "reflection", "friction"} {
			eventHandlers[eventType] = append(eventHandlers[eventType], monitorHandler)
		}
	}
	sm.SetEventHandlers(eventHandlers)

	// Start output trackers for running sessions restored from state.
	for _, sess := range st.GetSessions() {
		timeoutCtx, cancel := context.WithTimeout(d.shutdownCtx, cfg.XtermQueryTimeout())
		exists := tmux.SessionExists(timeoutCtx, sess.TmuxSession)
		cancel()
		if !exists {
			continue
		}
		if err := sm.EnsureTracker(sess.ID); err != nil {
			sessionLog.Warn("failed to start tracker", "session_id", sess.ID, "err", err)
		}
	}

	// Start signal monitors for existing remote sessions
	for _, sess := range st.GetSessions() {
		if sess.IsRemoteSession() && sess.RemotePaneID != "" {
			sm.StartRemoteSignalMonitor(sess)
		}
	}

	// Mark stale remote hosts as disconnected at startup.
	// Hosts that were "connected" in state are stale (SSH/ET processes are gone).
	// Don't auto-reconnect — reconnection requires interactive auth (e.g., Yubikey)
	// that can only happen when the user clicks "Reconnect" in the dashboard.
	staleHosts := remoteManager.MarkStaleHostsDisconnected()
	if staleHosts > 0 {
		logger.Info("marked stale remote hosts as disconnected", "count", staleHosts)
	}

	// Start background goroutine to prune expired remote hosts
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				remoteManager.PruneExpiredHosts()
			case <-d.shutdownCtx.Done():
				return
			}
		}
	}()

	// Create and start git watcher for filesystem-based change detection.
	// Started after server creation so broadcasts reach WebSocket clients.
	gitWatcher := workspace.NewGitWatcher(cfg, wm, server.BroadcastSessions, gitWatcherLog)
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

	// Create and start overlay compounder for bidirectional overlay sync
	var compounder *compound.Compounder
	if cfg.GetCompoundEnabled() {
		// Build LLM executor for conflict merges
		var llmExecutor compound.LLMExecutor
		if target := cfg.GetCompoundTarget(); target != "" {
			llmExecutor = func(ctx context.Context, prompt string, timeout time.Duration) (string, error) {
				return oneshot.ExecuteTarget(ctx, cfg, target, prompt, "", timeout, "")
			}
		}

		// Build propagator that pushes overlay changes to sibling workspaces
		propagator := func(sourceWorkspaceID, repoURL, relPath string, content []byte) {
			// Validate relPath to prevent path traversal
			if err := compound.ValidateRelPath(relPath); err != nil {
				compoundLog.Error("rejecting unsafe relPath in propagator", "path", relPath, "err", err)
				return
			}

			// Build index of workspaces by repo to avoid O(W) full scan
			repoWorkspaces := make(map[string][]state.Workspace)
			for _, w := range st.GetWorkspaces() {
				if w.RemoteHostID == "" {
					repoWorkspaces[w.Repo] = append(repoWorkspaces[w.Repo], w)
				}
			}

			// Build set of workspace IDs with active sessions to avoid O(S) scan per workspace
			activeWorkspaces := make(map[string]bool)
			for _, s := range st.GetSessions() {
				activeWorkspaces[s.WorkspaceID] = true
			}

			// Look up source branch for the event
			var sourceBranch string
			if sw, found := st.GetWorkspace(sourceWorkspaceID); found {
				sourceBranch = sw.Branch
			}

			// Track old content for diff and target workspace IDs
			var firstOldContent []byte
			var capturedOld bool
			var targetWorkspaceIDs []string

			for _, w := range repoWorkspaces[repoURL] {
				if w.ID == sourceWorkspaceID {
					continue
				}
				if !activeWorkspaces[w.ID] {
					continue
				}
				destPath := filepath.Join(w.Path, relPath)

				// Capture old content from the first target (all siblings have the same pre-propagation content)
				if !capturedOld {
					oldData, readErr := os.ReadFile(destPath)
					if readErr == nil {
						firstOldContent = oldData
					}
					capturedOld = true
				}

				if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
					compoundLog.Error("failed to create dir for propagation", "err", err)
					continue
				}
				compounder.Suppress(w.ID, relPath)
				// Preserve existing file permissions if the file already exists
				writeMode := os.FileMode(0644)
				if info, statErr := os.Stat(destPath); statErr == nil {
					writeMode = info.Mode().Perm()
				}
				if err := os.WriteFile(destPath, content, writeMode); err != nil {
					compoundLog.Error("failed to propagate", "path", relPath, "workspace_id", w.ID, "err", err)
					continue
				}
				// Update the target workspace's manifest hash (content already in memory)
				newHash := compound.HashBytes(content)
				st.UpdateOverlayManifestEntry(w.ID, relPath, newHash)
				compoundLog.Info("propagated", "path", relPath, "workspace_id", w.ID)
				targetWorkspaceIDs = append(targetWorkspaceIDs, w.ID)
			}

			// Broadcast overlay change event to dashboard
			if len(targetWorkspaceIDs) > 0 {
				overlayLog.Info("broadcasting change", "path", relPath, "source", sourceWorkspaceID, "branch", sourceBranch, "target_count", len(targetWorkspaceIDs), "targets", targetWorkspaceIDs)
				diff := difftool.UnifiedDiff(relPath, firstOldContent, content)
				server.BroadcastOverlayChange(dashboard.OverlayChangeEvent{
					RelPath:            relPath,
					SourceWorkspaceID:  sourceWorkspaceID,
					SourceBranch:       sourceBranch,
					TargetWorkspaceIDs: targetWorkspaceIDs,
					Timestamp:          time.Now().Unix(),
					UnifiedDiff:        diff,
				})
			} else {
				overlayLog.Debug("no active sibling workspaces to propagate to", "path", relPath, "source", sourceWorkspaceID)
			}
		}

		var err error
		compounder, err = compound.NewCompounder(cfg.GetCompoundDebounceMs(), llmExecutor, propagator, func(workspaceID, relPath, hash string) {
			st.UpdateOverlayManifestEntry(workspaceID, relPath, hash)
		}, compoundLog)
		if err != nil {
			compoundLog.Warn("failed to create compounder", "err", err)
		}
	}

	// Wire compounding callbacks
	if compounder != nil {
		sm.SetCompoundCallback(func(workspaceID string, isSpawn bool) {
			w, found := st.GetWorkspace(workspaceID)
			if !found || w.RemoteHostID != "" {
				return
			}
			if isSpawn {
				repoConfig, found := cfg.FindRepoByURL(w.Repo)
				if !found {
					return
				}
				overlayDir, err := workspace.OverlayDir(repoConfig.Name)
				if err != nil {
					return
				}
				manifest := w.OverlayManifest
				if manifest == nil {
					manifest = make(map[string]string)
				}
				declaredPaths := cfg.GetOverlayPaths(repoConfig.Name)
				compounder.AddWorkspace(workspaceID, w.Path, overlayDir, w.Repo, manifest, declaredPaths)
			} else {
				// Last session disposed — reconcile overlay files before the workspace goes dormant.
				// Run in a goroutine to avoid blocking the dispose HTTP handler.
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
					defer cancel()
					compounder.Reconcile(ctx, workspaceID)
					compounder.RemoveWorkspace(workspaceID)
				}()
			}
		})

		wm.SetCompoundReconcile(func(workspaceID string) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			compounder.Reconcile(ctx, workspaceID)
			compounder.RemoveWorkspace(workspaceID)
		})

		// Start watches for existing active workspaces
		activeWorkspaces := make(map[string]bool)
		for _, s := range st.GetSessions() {
			activeWorkspaces[s.WorkspaceID] = true
		}
		for wsID := range activeWorkspaces {
			w, found := st.GetWorkspace(wsID)
			if !found || w.RemoteHostID != "" {
				continue
			}
			repoConfig, found := cfg.FindRepoByURL(w.Repo)
			if !found {
				continue
			}
			overlayDir, err := workspace.OverlayDir(repoConfig.Name)
			if err != nil {
				continue
			}
			manifest := w.OverlayManifest
			if manifest == nil {
				manifest = make(map[string]string)
			}
			declaredPaths := cfg.GetOverlayPaths(repoConfig.Name)
			compounder.AddWorkspace(wsID, w.Path, overlayDir, w.Repo, manifest, declaredPaths)
		}
		compounder.Start()
		compoundLog.Info("started overlay compounding loop")
	}
	// Lore curation timer — declared at Run() scope so shutdown can clean up
	var loreCurateTimer *time.Timer
	var loreCurateMu sync.Mutex

	// Lore system: trigger curator on session dispose
	if cfg.GetLoreEnabled() {
		loreProposalDir := filepath.Join(homeDir, ".schmux", "lore-proposals")
		loreStore := lore.NewProposalStore(loreProposalDir, loreLog)

		// Wire lore store into dashboard server for API endpoints
		server.SetLoreStore(loreStore)

		loreCurator := &lore.Curator{
			InstructionFiles: cfg.GetLoreInstructionFiles(),
			BareRepo:         true,
		}
		if target := cfg.GetLoreTarget(); target != "" {
			loreCurator.Executor = func(ctx context.Context, prompt string, timeout time.Duration) (string, error) {
				return oneshot.ExecuteTarget(ctx, cfg, target, prompt, schema.LabelLoreCurator, timeout, "")
			}
		}

		// Wire lore curator into dashboard server for manual curation endpoint
		server.SetLoreCurator(loreCurator)

		// Wire streaming executor for observable curation
		if target := cfg.GetLoreTarget(); target != "" {
			server.SetStreamingExecutor(func(ctx context.Context, prompt, schemaLabel string, timeout time.Duration, dir string, onEvent func(oneshot.StreamEvent)) (string, error) {
				return oneshot.ExecuteTargetStreaming(ctx, cfg, target, prompt, schemaLabel, timeout, dir, onEvent)
			})
		}

		sm.SetLoreCallback(func(repoName, repoURL string, isLastSession bool) {
			mode := cfg.GetLoreCurateOnDispose()
			if mode == "never" || loreCurator.Executor == nil {
				return
			}
			if mode == "workspace" && !isLastSession {
				return
			}
			loreCurateMu.Lock()
			if loreCurateTimer != nil {
				loreCurateTimer.Stop()
			}
			debounce := time.Duration(cfg.GetLoreCurateDebounceMs()) * time.Millisecond
			loreCurateTimer = time.AfterFunc(debounce, func() {
				// Read raw entries from per-session event files + state file
				var allEntries []lore.Entry
				for _, w := range st.GetWorkspaces() {
					if w.Repo == repoURL && w.RemoteHostID == "" {
						entries, err := lore.ReadEntriesFromEvents(w.Path, w.ID, nil)
						if err != nil {
							continue
						}
						allEntries = append(allEntries, entries...)
					}
				}
				// Central state file for state-change records
				statePath, stateErr := lore.LoreStatePath(repoName)
				if stateErr == nil {
					stateEntries, err := lore.ReadEntries(statePath, nil)
					if err == nil {
						allEntries = append(allEntries, stateEntries...)
					}
				}

				// Apply raw filter
				rawEntries := lore.FilterRaw()(allEntries)
				if len(rawEntries) == 0 {
					loreLog.Debug("no raw entries to curate", "repo", repoName)
					return
				}

				repo, found := cfg.FindRepoByURL(repoURL)
				if !found {
					loreLog.Warn("repo not found for URL", "url", repoURL)
					return
				}
				bareDir := cfg.ResolveBareRepoDir(repo.BarePath)

				// Build prompt explicitly (same as manual curation path)
				instrFiles, fileHashes, err := loreCurator.ReadInstructionFiles(d.shutdownCtx, bareDir)
				if err != nil {
					loreLog.Error("auto-curate: failed to read instruction files", "repo", repoName, "err", err)
					return
				}
				if len(instrFiles) == 0 {
					loreLog.Error("auto-curate: no instruction files found", "repo", repoName)
					return
				}
				prompt := lore.BuildCuratorPrompt(instrFiles, rawEntries)

				// Create per-run debug directory
				curationID := fmt.Sprintf("auto-%s-%s", repoName, time.Now().UTC().Format("20060102-150405"))
				runDir := filepath.Join(homeDir, ".schmux", "lore-curator-runs", repoName, curationID)
				os.MkdirAll(runDir, 0755)

				// Write prompt.txt
				os.WriteFile(filepath.Join(runDir, "prompt.txt"), []byte(prompt), 0644)

				// Write run.sh
				if target := cfg.GetLoreTarget(); target != "" {
					cmdInfo, cmdErr := oneshot.ResolveTargetCommand(cfg, target, schema.LabelLoreCurator, false)
					if cmdErr == nil {
						var sb strings.Builder
						sb.WriteString("#!/bin/sh\n")
						sb.WriteString("# Reproduce this auto-curator run\n")
						sb.WriteString("# Generated by schmux — edit freely\n\n")
						for k, v := range cmdInfo.Env {
							fmt.Fprintf(&sb, "export %s=%q\n", k, v)
						}
						if len(cmdInfo.Env) > 0 {
							sb.WriteString("\n")
						}
						var quotedArgs []string
						for _, a := range cmdInfo.Args {
							if strings.ContainsAny(a, " \t\n\"'\\$`") {
								quotedArgs = append(quotedArgs, fmt.Sprintf("%q", a))
							} else {
								quotedArgs = append(quotedArgs, a)
							}
						}
						fmt.Fprintf(&sb, "cat \"$(dirname \"$0\")/prompt.txt\" | \\\n  %s\n", strings.Join(quotedArgs, " \\\n  "))
						os.WriteFile(filepath.Join(runDir, "run.sh"), []byte(sb.String()), 0755)
					}
				}

				loreLog.Info("auto-curate: calling LLM", "repo", repoName, "entries", len(rawEntries), "curation_id", curationID)
				start := time.Now()

				curateCtx, curateCancel := context.WithTimeout(d.shutdownCtx, 10*time.Minute)
				defer curateCancel()

				response, err := loreCurator.Executor(curateCtx, prompt, 10*time.Minute)
				elapsed := time.Since(start)
				if err != nil {
					loreLog.Error("auto-curation failed", "elapsed", elapsed.Round(time.Millisecond), "err", err)
					os.WriteFile(filepath.Join(runDir, "error.txt"), []byte(err.Error()), 0644)
					return
				}
				os.WriteFile(filepath.Join(runDir, "output.txt"), []byte(response), 0644)

				result, err := lore.ParseCuratorResponse(response)
				if err != nil {
					loreLog.Error("auto-curation parse failed", "elapsed", elapsed.Round(time.Millisecond), "err", err)
					os.WriteFile(filepath.Join(runDir, "error.txt"), []byte(err.Error()), 0644)
					return
				}

				proposal, err := lore.BuildProposal(repoName, result, instrFiles, fileHashes, rawEntries)
				if err != nil {
					loreLog.Error("auto-curation build proposal failed", "elapsed", elapsed.Round(time.Millisecond), "err", err)
					os.WriteFile(filepath.Join(runDir, "error.txt"), []byte(err.Error()), 0644)
					return
				}

				if err := loreStore.Save(proposal); err != nil {
					loreLog.Error("failed to save proposal", "err", err)
					return
				}
				loreLog.Info("auto-curate: proposal created", "repo", repoName, "proposal_id", proposal.ID, "files", len(proposal.ProposedFiles), "entries_used", len(proposal.EntriesUsed), "elapsed", elapsed.Round(time.Millisecond))

				// Mark source entries as "proposed" in the central state JSONL
				if stateErr == nil {
					if err := lore.MarkEntriesByTextFromEntries(rawEntries, statePath, "proposed", proposal.EntriesUsed, proposal.ID); err != nil {
						loreLog.Warn("failed to mark entries as proposed", "err", err)
					}
				}
			})
			loreCurateMu.Unlock()
		})
		loreLog.Info("system enabled, will curate on session dispose")

		// Prune old state-change records on startup (from central state files only —
		// raw entries in workspaces are append-only logs and don't need pruning)
		go func() {
			maxAge := time.Duration(cfg.GetLorePruneAfterDays()) * 24 * time.Hour
			for _, repo := range cfg.GetRepos() {
				statePath, err := lore.LoreStatePath(repo.Name)
				if err != nil {
					continue
				}
				pruned, err := lore.PruneEntries(statePath, maxAge)
				if err != nil {
					loreLog.Warn("prune failed", "repo", repo.Name, "err", err)
				} else if pruned > 0 {
					loreLog.Info("pruned old state entries", "count", pruned, "repo", repo.Name)
				}
			}
		}()
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
		case <-d.shutdownCtx.Done():
			return
		default:
			ctx, cancel := context.WithTimeout(d.shutdownCtx, cfg.GitStatusTimeout())
			// Ensure origin query repos exist for branch queries (creates if missing)
			if err := wm.EnsureOriginQueries(ctx); err != nil {
				logger.Warn("failed to ensure origin queries", "err", err)
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
				ctx, cancel := context.WithTimeout(d.shutdownCtx, cfg.GitStatusTimeout())
				// Ensure origin query repos exist (in case new repos were added)
				if err := wm.EnsureOriginQueries(ctx); err != nil {
					logger.Warn("failed to ensure origin queries", "err", err)
				}
				// Fetch origin query repos to get latest branch info
				wm.FetchOriginQueries(ctx)
				wm.UpdateAllGitStatus(ctx)
				cancel()
				server.BroadcastSessions()
			case <-d.shutdownCtx.Done():
				return
			}
		}
	}()

	// Start background goroutine to check for inactive sessions and ask NudgeNik
	go startNudgeNikChecker(d.shutdownCtx, cfg, st, sm, server.BroadcastSessions, nudgenikLog)

	// Start subreddit digest hourly generation if enabled
	if subreddit.IsEnabled(cfg) {
		subredditLog := logging.Sub(logger, "subreddit")
		subredditCachePath := filepath.Join(homeDir, ".schmux", "subreddit.json")
		go startSubredditHourlyGenerator(d.shutdownCtx, cfg, subredditCachePath, subredditLog)
	}

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
	var devRestart bool
	select {
	case sig := <-sigChan:
		logger.Info("received signal, shutting down", "signal", sig)
		d.cancelFunc() // Cancel shutdownCtx so background goroutines exit cleanly
	case err := <-serverErrChan:
		return fmt.Errorf("dashboard server error: %w", err)
	case <-d.shutdownChan:
		logger.Info("shutdown requested")
	case <-d.devRestartChan:
		logger.Info("dev restart requested")
		devRestart = true
	}
	// Stop pending lore curation timer
	loreCurateMu.Lock()
	if loreCurateTimer != nil {
		loreCurateTimer.Stop()
	}
	loreCurateMu.Unlock()

	// Flush any pending batched state saves and do a final save
	st.FlushPending()
	if err := st.Save(); err != nil {
		logger.Warn("final state save failed", "err", err)
	}

	// Shutdown telemetry (flush pending events)
	tel.Shutdown()

	// Stop session manager (kills tmux attach-client processes)
	sm.Stop()

	// Stop tunnel manager
	tunnelMgr.Stop()

	// Stop git watcher
	if gitWatcher != nil {
		gitWatcher.Stop()
	}

	// Stop compounder
	if compounder != nil {
		compounder.Stop()
	}

	// Stop dashboard server
	if err := server.Stop(); err != nil {
		return fmt.Errorf("failed to stop server: %w", err)
	}

	if devRestart {
		return ErrDevRestart
	}
	return nil
}

// Shutdown triggers a graceful shutdown. Safe to call multiple times.
func (d *Daemon) Shutdown() {
	d.shutdownOnce.Do(func() { close(d.shutdownChan) })
	if d.cancelFunc != nil {
		d.cancelFunc()
	}
}

// DevRestart triggers a dev mode restart. The daemon will exit with
// ErrDevRestart, which the caller should translate to exit code 42.
func (d *Daemon) DevRestart() {
	d.devRestartOnce.Do(func() {
		close(d.devRestartChan)
	})
	if d.cancelFunc != nil {
		d.cancelFunc()
	}
}

// startNudgeNikChecker starts a background goroutine that checks for inactive sessions
// and automatically asks NudgeNik for consultation.
func startNudgeNikChecker(ctx context.Context, cfg *config.Config, st *state.State, sm *session.Manager, onUpdate func(), logger *log.Logger) {
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
			checkInactiveSessionsForNudge(ctx, cfg, st, sm, onUpdate, logger)
		case <-ctx.Done():
			return
		}
	}
}

// checkInactiveSessionsForNudge checks all sessions for inactivity and asks NudgeNik if needed.
func checkInactiveSessionsForNudge(ctx context.Context, cfg *config.Config, st *state.State, sm *session.Manager, onUpdate func(), logger *log.Logger) {
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
		logger.Info("asking", "session_id", sess.ID, "target", targetName)
		nudge := askNudgeNikForSession(ctx, cfg, sess, logger)
		if nudge != "" {
			sess.Nudge = nudge
			if err := st.UpdateSession(sess); err != nil {
				logger.Error("failed to save nudge", "session_id", sess.ID, "err", err)
			} else if err := st.Save(); err != nil {
				logger.Error("failed to persist state", "session_id", sess.ID, "err", err)
			} else {
				logger.Info("saved nudge", "session_id", sess.ID)
				if onUpdate != nil {
					onUpdate()
				}
			}
		}
	}
}

// askNudgeNikForSession captures the session output and asks NudgeNik for consultation.
func askNudgeNikForSession(ctx context.Context, cfg *config.Config, sess state.Session, logger *log.Logger) string {
	result, err := nudgenik.AskForSession(ctx, cfg, sess)
	if err != nil {
		switch {
		case errors.Is(err, nudgenik.ErrDisabled):
			// Silently skip - nudgenik is disabled
		case errors.Is(err, nudgenik.ErrNoResponse):
			logger.Info("no response extracted", "session_id", sess.ID)
		case errors.Is(err, nudgenik.ErrTargetNotFound):
			logger.Warn("target not found in config")
		case errors.Is(err, nudgenik.ErrTargetNoSecrets):
			logger.Warn("target missing required secrets")
		default:
			logger.Error("failed to ask", "session_id", sess.ID, "err", err)
		}
		return ""
	}

	payload, err := json.Marshal(result)
	if err != nil {
		logger.Error("failed to serialize result", "session_id", sess.ID, "err", err)
		return ""
	}

	return string(payload)
}

// startSubredditHourlyGenerator starts a background goroutine that generates
// subreddit digests hourly.
func startSubredditHourlyGenerator(ctx context.Context, cfg *config.Config, cachePath string, logger *log.Logger) {
	// Run every hour
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	// Wait a bit before first check to let daemon start
	select {
	case <-time.After(30 * time.Second):
		// Ready to start generating
	case <-ctx.Done():
		return
	}

	// Do initial generation on startup
	generateSubredditDigest(ctx, cfg, cachePath, logger)

	for {
		select {
		case <-ticker.C:
			generateSubredditDigest(ctx, cfg, cachePath, logger)
		case <-ctx.Done():
			return
		}
	}
}

// generateSubredditDigest generates a new subreddit digest if the cache is stale.
func generateSubredditDigest(ctx context.Context, cfg *config.Config, cachePath string, logger *log.Logger) {
	// Check if cache is stale (older than 1 hour)
	cache, err := subreddit.ReadCache(cachePath)
	if err == nil && !cache.IsStale(1*time.Hour) {
		logger.Debug("cache is fresh, skipping generation")
		return
	}

	// Build repo info list
	var repos []subreddit.RepoInfo
	for _, r := range cfg.GetRepos() {
		repos = append(repos, subreddit.RepoInfo{
			Name:          r.Name,
			BarePath:      r.BarePath,
			DefaultBranch: "main", // Use main as default; gatherRepoCommits falls back to this anyway
		})
	}

	// Gather commits from all repos
	commits, err := subreddit.GatherCommits(ctx, repos, cfg.GetWorktreeBasePath(), cfg.GetSubredditHours())
	if err != nil {
		logger.Warn("failed to gather commits", "err", err)
		// Continue with empty commits - the digest will show "quiet period"
	}

	logger.Info("generating digest", "commits", len(commits))

	// Generate new digest
	newCache, err := subreddit.Generate(ctx, cfg, cfg, nil, commits, cachePath, 0)
	if err != nil {
		if errors.Is(err, subreddit.ErrDisabled) {
			logger.Debug("subreddit disabled, skipping")
			return
		}
		logger.Error("failed to generate digest", "err", err)
		return
	}

	// Write cache
	if err := subreddit.WriteCache(cachePath, newCache); err != nil {
		logger.Error("failed to write cache", "err", err)
		return
	}

	logger.Info("digest generated", "commits", newCache.CommitCount)
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

// createDevConfigBackup creates a tar.gz backup of config.json, secrets.json, and state.json
// in the ~/.schmux/backups/ directory. Missing files are skipped silently.
// The backup filename format is: config-<timestamp>_<cwd-basename>.tar.gz
func createDevConfigBackup(schmuxDir string) error {
	// Create backups directory
	backupDir := filepath.Join(schmuxDir, "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Get current working directory name for the backup filename
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	dirName := filepath.Base(cwd)

	// Generate timestamp in UTC
	timestamp := time.Now().UTC().Format("2006-01-02T15-04-05")

	// Create backup filename
	backupFilename := fmt.Sprintf("config-%s_%s.tar.gz", timestamp, dirName)
	backupPath := filepath.Join(backupDir, backupFilename)

	// Create the tar.gz file
	backupFile, err := os.Create(backupPath)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer backupFile.Close()

	gzWriter := gzip.NewWriter(backupFile)
	defer gzWriter.Close()

	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	// Files to backup (in order)
	filesToBackup := []string{"config.json", "secrets.json", "state.json"}

	for _, filename := range filesToBackup {
		filePath := filepath.Join(schmuxDir, filename)

		// Skip if file doesn't exist
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			continue
		}

		// Read file content
		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", filename, err)
		}

		// Get file info for header
		info, err := os.Stat(filePath)
		if err != nil {
			return fmt.Errorf("failed to stat %s: %w", filename, err)
		}

		// Create tar header
		header := &tar.Header{
			Name:     filename,
			Size:     info.Size(),
			Mode:     int64(info.Mode()),
			ModTime:  info.ModTime(),
			Typeflag: tar.TypeReg,
		}

		// Write header
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write header for %s: %w", filename, err)
		}

		// Write file content
		if _, err := io.Copy(tarWriter, bytes.NewReader(content)); err != nil {
			return fmt.Errorf("failed to write %s to archive: %w", filename, err)
		}
	}

	return nil
}

// cleanupOldBackups deletes backup files older than maxAge from the backup directory.
// Only files matching the pattern "config-*.tar.gz" are considered.
func cleanupOldBackups(backupDir string, maxAge time.Duration) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}

	cutoff := time.Now().Add(-maxAge)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Only consider config backup files
		if !strings.HasPrefix(name, "config-") || !strings.HasSuffix(name, ".tar.gz") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Delete if older than cutoff
		if info.ModTime().Before(cutoff) {
			filePath := filepath.Join(backupDir, name)
			if err := os.Remove(filePath); err == nil {
				// Log deletion (we don't have access to logger here, but could pass it in)
				// For now, silently delete
			}
		}
	}
}
