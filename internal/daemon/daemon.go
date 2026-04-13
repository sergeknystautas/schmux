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
	"github.com/sergeknystautas/schmux/internal/autolearn"
	"github.com/sergeknystautas/schmux/internal/compound"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/dashboard"
	"github.com/sergeknystautas/schmux/internal/dashboardsx"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/difftool"
	"github.com/sergeknystautas/schmux/internal/events"
	"github.com/sergeknystautas/schmux/internal/floormanager"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/nudgenik"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/remote"
	"github.com/sergeknystautas/schmux/internal/repofeed"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/spawn"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/telemetry"
	"github.com/sergeknystautas/schmux/internal/timelapse"
	"github.com/sergeknystautas/schmux/internal/tmux"
	"github.com/sergeknystautas/schmux/internal/tunnel"
	"github.com/sergeknystautas/schmux/internal/update"
	"github.com/sergeknystautas/schmux/internal/version"
	"github.com/sergeknystautas/schmux/internal/workspace"
	"github.com/sergeknystautas/schmux/internal/workspace/ensure"
)

const (
	pidFileName = "daemon.pid"
	// Inactivity threshold before asking NudgeNik
	nudgeInactivityThreshold = 15 * time.Second
)

// ErrDevRestart is returned by Run() when the daemon needs to restart
// for a dev mode workspace switch. The caller should exit with code 42.
var ErrDevRestart = errors.New("dev restart requested")

// Daemon represents the schmux daemon.
type Daemon struct {
	logger *log.Logger

	shutdownChan   chan struct{}
	shutdownOnce   sync.Once
	devRestartChan chan struct{}
	devRestartOnce sync.Once
	shutdownCtx    context.Context
	cancelFunc     context.CancelFunc

	githubStatus contracts.GitHubStatus
}

// shutdownHandles bundles the handles needed by the shutdown sequence.
type shutdownHandles struct {
	st                 *state.State
	tel                telemetry.Telemetry
	sm                 *session.Manager
	remoteManager      *remote.Manager
	tunnelMgr          *tunnel.Manager
	gitWatcher         *workspace.GitWatcher
	compounder         *compound.Compounder
	detachFloorManager func()
	server             *dashboard.Server
}

// daemonInit bundles the values produced by the config/state initialization
// phase of Run() so they can be returned from initConfigAndState.
type daemonInit struct {
	logger          *log.Logger
	cfg             *config.Config
	st              *state.State
	tel             telemetry.Telemetry
	tmuxServer      *tmux.TmuxServer
	tmuxBin         string
	homeDir         string
	schmuxDir       string
	statePath       string
	hooksDir        string
	pidFile         string
	workspaceLog    *log.Logger
	configLog       *log.Logger
	compoundLog     *log.Logger
	overlayLog      *log.Logger
	autolearnLog    *log.Logger
	sessionLog      *log.Logger
	nudgenikLog     *log.Logger
	gitWatcherLog   *log.Logger
	githubLog       *log.Logger
	remoteLog       *log.Logger
	remoteAccessLog *log.Logger
}

// heartbeatStateWriter adapts state.StateStore to dashboardsx.HeartbeatStatusWriter.
type heartbeatStateWriter struct {
	state state.StateStore
}

func (w *heartbeatStateWriter) SetHeartbeatStatus(s *dashboardsx.HeartbeatStatus) {
	existing := w.state.GetDashboardSXStatus()
	if existing == nil {
		existing = &state.DashboardSXStatus{}
	}
	existing.LastHeartbeatTime = s.Time
	existing.LastHeartbeatStatus = s.StatusCode
	existing.LastHeartbeatError = s.Error
	w.state.SetDashboardSXStatus(existing)
	w.state.Save()
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

// ValidateReadyToRun checks that the daemon is safe to start.
// It verifies no other daemon is already running. tmux availability
// is checked at session spawn time, not at startup.
func ValidateReadyToRun() error {
	schmuxDir := schmuxdir.Get()
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		return fmt.Errorf("failed to create schmux directory: %w", err)
	}

	pidFile := schmuxdir.PIDPath()

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
	schmuxDir := schmuxdir.Get()
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		return fmt.Errorf("failed to create schmux directory: %w", err)
	}

	// Open log file for daemon stdout/stderr
	logFile := filepath.Join(schmuxDir, "daemon-startup.log")
	logF, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	// Apply custom tmux settings from config (if set) before starting the server.
	tmuxBinary := "tmux"
	socketName := "schmux"
	if cfg, err := config.Load(schmuxdir.ConfigPath()); err == nil {
		if cfg.TmuxBinary != "" {
			tmuxBinary = cfg.TmuxBinary
		}
		if cfg.TmuxSocketName != "" {
			socketName = cfg.TmuxSocketName
		}
	}

	// Construct the TmuxServer that all subsystems will share.
	startupServer := tmux.NewTmuxServer(tmuxBinary, socketName, nil)

	// Start the tmux server (no-op if already running).
	ctx := context.Background()
	_ = startupServer.StartServer(ctx) // ignore error: best-effort

	// Get the path to the current executable
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Start daemon in background
	cmd := exec.Command(execPath, "daemon-run", "--background")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Dir, _ = os.Getwd()
	cmd.Stdout = logF
	cmd.Stderr = logF

	// Propagate SCHMUX_HOME to child process so the forked daemon
	// uses the same directory even when the env var is not already set.
	if d := schmuxdir.Get(); d != "" {
		cmd.Env = append(os.Environ(), "SCHMUX_HOME="+d)
	}

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
	pidFile := schmuxdir.PIDPath()

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
	// Check every 100ms, up to 10 seconds. The shutdown sequence includes remote host
	// disconnection, session manager stop, and HTTP server shutdown, which can take
	// several seconds under CPU contention (e.g., Docker containers).
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		<-ticker.C
		// Check if process still exists by sending signal 0
		if err := process.Signal(syscall.Signal(0)); err != nil {
			// Process has exited
			return nil
		}
	}

	return fmt.Errorf("timeout waiting for daemon to stop")
}

// Status returns the status of the daemon.
func Status() (running bool, url string, startedAt string, err error) {
	pidFile := schmuxdir.PIDPath()
	startedFile := filepath.Join(schmuxdir.Get(), "daemon.started")

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

	// Read the daemon URL from the breadcrumb file
	urlFile := filepath.Join(schmuxdir.Get(), "daemon.url")
	if data, err := os.ReadFile(urlFile); err == nil {
		url = strings.TrimSpace(string(data))
	} else {
		// Fallback for backward compatibility (daemon started before this change)
		url = fmt.Sprintf("http://localhost:%d", 7337)
		if cfg, err := config.Load(schmuxdir.ConfigPath()); err == nil {
			if cfgPort := cfg.GetPort(); cfgPort != 0 {
				url = fmt.Sprintf("http://localhost:%d", cfgPort)
			}
			if cfg.GetPublicBaseURL() != "" {
				url = cfg.GetPublicBaseURL()
			}
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
	di, err := d.initConfigAndState(devMode)
	if err != nil {
		return err
	}
	defer os.Remove(di.pidFile)

	// Unpack frequently used values for readability — matches the variable
	// names the rest of Run() already uses, minimising the diff.
	logger := di.logger
	cfg := di.cfg
	st := di.st
	tel := di.tel
	tmuxServer := di.tmuxServer
	tmuxBin := di.tmuxBin
	homeDir := di.homeDir
	schmuxDir := di.schmuxDir
	statePath := di.statePath
	hooksDir := di.hooksDir
	workspaceLog := di.workspaceLog
	configLog := di.configLog
	compoundLog := di.compoundLog
	overlayLog := di.overlayLog
	autolearnLog := di.autolearnLog
	sessionLog := di.sessionLog
	nudgenikLog := di.nudgenikLog
	gitWatcherLog := di.gitWatcherLog
	githubLog := di.githubLog
	remoteLog := di.remoteLog
	remoteAccessLog := di.remoteAccessLog

	d.initDashboardSX(cfg, st, logger)

	wm, sm, autolearnInstructionsDir := d.initManagers(cfg, st, tmuxServer, statePath, hooksDir, tel, schmuxDir, workspaceLog, sessionLog)

	server, mm, prDiscovery, err := d.initDashboard(cfg, st, wm, sm, tmuxServer, statePath, schmuxDir, devProxy, devMode, logger, configLog, githubLog)
	if err != nil {
		return err
	}

	remoteManager, tunnelMgr, detachFloorManager, eventHandlers := d.wireCallbacks(cfg, st, sm, wm, mm, server, tmuxServer, statePath, homeDir, logger, remoteLog, remoteAccessLog)

	gitWatcher := d.restoreSessions(cfg, st, sm, wm, remoteManager, tmuxServer, tmuxBin, server, logger, sessionLog, gitWatcherLog)

	compounder := d.initCompound(cfg, st, sm, wm, server, compoundLog, overlayLog)

	d.initAutolearn(cfg, st, server, schmuxDir, autolearnInstructionsDir, autolearnLog, logger)

	d.startBackgroundJobs(cfg, st, sm, wm, server, prDiscovery, eventHandlers, logger, nudgenikLog, schmuxDir)
	defer prDiscovery.Stop()

	devRestart, err := d.startAndWait(server, cfg, schmuxDir, background, logger)
	if err != nil {
		return err
	}

	if err := d.shutdown(shutdownHandles{
		st:                 st,
		tel:                tel,
		sm:                 sm,
		remoteManager:      remoteManager,
		tunnelMgr:          tunnelMgr,
		gitWatcher:         gitWatcher,
		compounder:         compounder,
		detachFloorManager: detachFloorManager,
		server:             server,
	}); err != nil {
		return fmt.Errorf("failed to stop server: %w", err)
	}

	if devRestart {
		return ErrDevRestart
	}
	return nil
}

// initConfigAndState sets up logging, loads config and state, initialises
// telemetry, and performs early filesystem housekeeping. It returns a
// daemonInit containing every value the rest of Run() needs.
//
// NOTE: the PID-file defer is NOT placed here — callers must defer
// os.Remove(di.pidFile) themselves so the cleanup fires at the right time.
func (d *Daemon) initConfigAndState(devMode bool) (*daemonInit, error) {
	d.logger = logging.New(devMode)
	logger := d.logger
	telemetryLog := logging.Sub(logger, "telemetry")
	workspaceLog := logging.Sub(logger, "workspace")
	configLog := logging.Sub(logger, "config")
	compoundLog := logging.Sub(logger, "compound")
	overlayLog := logging.Sub(logger, "overlay")
	autolearnLog := logging.Sub(logger, "autolearn")
	sessionLog := logging.Sub(logger, "session")
	nudgenikLog := logging.Sub(logger, "nudgenik")
	gitWatcherLog := logging.Sub(logger, "git-watcher")
	stateLog := logging.Sub(logger, "state")
	githubLog := logging.Sub(logger, "github")
	remoteLog := logging.Sub(logger, "remote")
	remoteAccessLog := logging.Sub(logger, "remote-access")

	// Set package-level loggers for packages that use standalone functions
	autolearn.SetLogger(autolearnLog)
	compound.SetLogger(compoundLog)
	config.SetLogger(configLog)
	detect.SetLogger(logging.Sub(configLog, "detect"))
	update.SetLogger(logging.Sub(logger, "update"))
	tunnel.SetLogger(remoteAccessLog)
	dashboardsx.SetLogger(logging.Sub(logger, "dashboardsx"))

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	schmuxDir := schmuxdir.Get()
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create schmux directory: %w", err)
	}

	cleanupOrphanedLoreWorktrees()

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
		backupDir := schmuxdir.BackupsDir()
		cleanupOldBackups(backupDir, 3*24*time.Hour)
	}

	pidFile := schmuxdir.PIDPath()
	startedFile := filepath.Join(schmuxDir, "daemon.started")

	// Write PID file
	pid := os.Getpid()
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", pid)), 0644); err != nil {
		return nil, fmt.Errorf("failed to write PID file: %w", err)
	}

	// Record daemon start time
	startedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err := os.WriteFile(startedFile, []byte(startedAt+"\n"), 0644); err != nil {
		return nil, fmt.Errorf("failed to write daemon start time: %w", err)
	}

	// Write all schemas on startup (ensures they're always up to date)
	if err := oneshot.WriteAllSchemas(); err != nil {
		return nil, fmt.Errorf("failed to write schemas: %w", err)
	}

	// Load config
	configPath := schmuxdir.ConfigPath()
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	if cfg.TmuxBinary != "" {
		logger.Info("using custom tmux binary", "path", cfg.TmuxBinary)
	}

	// Construct the TmuxServer that all subsystems will share.
	tmuxBin := cfg.TmuxBinary
	if tmuxBin == "" {
		tmuxBin = "tmux"
	}
	tmuxServer := tmux.NewTmuxServer(tmuxBin, cfg.GetTmuxSocketName(), logging.Sub(logger, "tmux"))

	if cfg.GetAuthEnabled() {
		if _, err := config.EnsureSessionSecret(); err != nil {
			return nil, fmt.Errorf("failed to initialize auth session secret: %w", err)
		}
	}

	// Initialize telemetry
	var tel telemetry.Telemetry = &telemetry.NoopTelemetry{}

	// Check environment variable kill switches
	telemetryDisabled := os.Getenv("SCHMUX_TELEMETRY_OFF") != "" || os.Getenv("DO_NOT_TRACK") != ""

	if cfg.GetTelemetryEnabled() && !telemetryDisabled {
		// Ensure installation ID exists
		installID := cfg.GetInstallationID()
		if installID == "" {
			installID = uuid.New().String()
			cfg.SetInstallationID(installID)
			if err := cfg.Save(); err != nil {
				telemetryLog.Warn("failed to save installation ID", "err", err)
			}
		}

		if cmd := cfg.GetTelemetryCommand(); cmd != "" {
			tel = telemetry.NewCommandTelemetry(cmd, installID, telemetryLog)
			telemetryLog.Info("telemetry via external command")
		} else {
			tel = telemetry.New(installID, telemetryLog)
			if _, ok := tel.(*telemetry.Client); ok {
				telemetryLog.Info("anonymous usage metrics enabled (opt out via config or environment)")
			}
		}
	}

	// Track daemon startup
	tel.Track("daemon_started", map[string]any{
		"version": version.Version,
	})

	// Compute state path
	statePath := schmuxdir.StatePath()

	// Load state
	st, err := state.Load(statePath, stateLog)
	if err != nil {
		return nil, fmt.Errorf("failed to load state: %w", err)
	}

	// Verify we can access tmux sessions for existing sessions
	if err := validateSessionAccess(st); err != nil {
		return nil, err
	}

	// Clear needs_restart flag on daemon start (config changes now taking effect)
	if st.GetNeedsRestart() {
		st.SetNeedsRestart(false)
		st.Save()
	}

	// Normalize bare repo paths — rename non-conforming directories to {name}.git
	config.NormalizeBarePaths(cfg, st)

	return &daemonInit{
		logger:          logger,
		cfg:             cfg,
		st:              st,
		tel:             tel,
		tmuxServer:      tmuxServer,
		tmuxBin:         tmuxBin,
		homeDir:         homeDir,
		schmuxDir:       schmuxDir,
		statePath:       statePath,
		hooksDir:        hooksDir,
		pidFile:         pidFile,
		workspaceLog:    workspaceLog,
		configLog:       configLog,
		compoundLog:     compoundLog,
		overlayLog:      overlayLog,
		autolearnLog:    autolearnLog,
		sessionLog:      sessionLog,
		nudgenikLog:     nudgenikLog,
		gitWatcherLog:   gitWatcherLog,
		githubLog:       githubLog,
		remoteLog:       remoteLog,
		remoteAccessLog: remoteAccessLog,
	}, nil
}

// initManagers creates the workspace and session managers, wires timelapse,
// telemetry, hooks, I/O telemetry, overlay dirs, and calls EnsureAll. It
// returns the two managers plus the autolearn instructions directory (which
// is used later in Run()).
func (d *Daemon) initManagers(
	cfg *config.Config,
	st *state.State,
	tmuxServer *tmux.TmuxServer,
	statePath string,
	hooksDir string,
	tel telemetry.Telemetry,
	schmuxDir string,
	workspaceLog *log.Logger,
	sessionLog *log.Logger,
) (*workspace.Manager, *session.Manager, string) {
	// Create managers
	ensure.SetLogger(logging.Sub(workspaceLog, "ensure"))
	// Wire autolearn instruction store for private layer injection at spawn time
	autolearnInstructionsDir := filepath.Join(schmuxDir, "autolearn", "instructions")
	ensure.SetInstructionStore(autolearn.NewInstructionStore(autolearnInstructionsDir))
	wm := workspace.New(cfg, st, statePath, workspaceLog)
	// SetBroadcastFn will be wired after server creation (see below)
	sm := session.New(cfg, st, statePath, wm, tmuxServer, sessionLog)

	// Wire timelapse recording if enabled
	if cfg.GetTimelapseEnabled() {
		recordingsDir := schmuxdir.RecordingsDir()
		maxBytes := int64(cfg.GetTimelapseMaxFileSizeMB()) * 1024 * 1024

		if notice := timelapse.ShowFirstRunNotice(recordingsDir); notice != "" {
			sessionLog.Info(notice)
		}

		sm.SetRecorderFactory(func(sessionID string, outputLog *session.OutputLog, gapCh <-chan session.SourceEvent) session.Runnable {
			rec, err := timelapse.NewRecorder(sessionID, outputLog, gapCh, recordingsDir, maxBytes, 80, 24)
			if err != nil {
				sessionLog.Warn("failed to create timelapse recorder", "session", sessionID, "err", err)
				return nil
			}
			return rec
		})
	}

	// Wire telemetry to managers
	wm.SetTelemetry(tel)
	sm.SetTelemetry(tel)

	// Wire centralized hooks directory to managers
	wm.SetHooksDir(hooksDir)
	sm.SetHooksDir(hooksDir)

	// Wire I/O workspace telemetry if enabled
	if cfg.GetIOWorkspaceTelemetryEnabled() {
		ioTel := workspace.NewIOWorkspaceTelemetry()
		wm.SetIOWorkspaceTelemetry(ioTel)
	}

	// Ensure overlay directories exist for all repos
	if err := wm.EnsureOverlayDirs(cfg.GetRepos()); err != nil {
		workspaceLog.Warn("failed to ensure overlay directories", "err", err)
		// Don't fail daemon startup for this
	}

	// Ensure all workspaces have the necessary schmux configuration
	wm.EnsureAll()

	return wm, sm, autolearnInstructionsDir
}

// initDashboard detects available tools, creates the model manager, starts
// background registry fetch, ensures the workspace directory, creates PR
// discovery, checks GitHub auth, and creates the dashboard server.
func (d *Daemon) initDashboard(
	cfg *config.Config,
	st *state.State,
	wm *workspace.Manager,
	sm *session.Manager,
	tmuxServer *tmux.TmuxServer,
	statePath string,
	schmuxDir string,
	devProxy bool,
	devMode bool,
	logger *log.Logger,
	configLog *log.Logger,
	githubLog *log.Logger,
) (*dashboard.Server, *models.Manager, *github.Discovery, error) {
	// Detect available tools for model catalog
	detectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	detectedTargets, err := detect.DetectAvailableToolsContext(detectCtx, false)
	cancel()
	if err != nil {
		configLog.Warn("failed to detect tools", "err", err)
		detectedTargets = nil
	}

	if len(detectedTargets) > 0 {
		var names []string
		for _, t := range detectedTargets {
			names = append(names, t.Name)
		}
		configLog.Info("agents detected", "agents", strings.Join(names, ", "))
	}

	// Create model manager (single owner for catalog, resolution, enablement)
	// schmuxDir is already computed earlier in this function
	modelsLog := logging.Sub(logger, "models")
	mm := models.New(cfg, detectedTargets, schmuxDir, modelsLog)

	// Detect VCS and tmux availability
	detectedVCS := detect.DetectVCS()
	detectedTmux := detect.DetectTmux()

	// Start background registry fetch
	mm.StartBackgroundFetch(d.shutdownCtx)

	// Ensure workspace directory exists
	if err := wm.EnsureWorkspaceDir(); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create workspace directory: %w", err)
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
		logger.Info("GitHub authenticated", "user", d.githubStatus.Username)
	} else {
		logger.Info("GitHub not available (gh CLI not installed or not authenticated)")
	}

	// Create dashboard server
	server := dashboard.NewServer(cfg, st, statePath, sm, wm, prDiscovery, logger, d.githubStatus, tmuxServer, dashboard.ServerOptions{
		Shutdown:     d.Shutdown,
		DevRestart:   d.DevRestart,
		DevProxy:     devProxy,
		DevMode:      devMode,
		ShutdownCtx:  d.shutdownCtx,
		DetectedVCS:  detectedVCS,
		DetectedTmux: detectedTmux,
	})

	// Wire workspace manager broadcast to trigger dashboard updates after tab mutations
	wm.SetBroadcastFn(func() { go server.BroadcastSessions() })

	return server, mm, prDiscovery, nil
}

// wireCallbacks wires model manager, remote manager, tunnel manager, event
// handlers, and floor manager into the daemon subsystems. It returns the four
// values needed by the rest of Run(): remoteManager, tunnelMgr,
// detachFloorManager (closure), and eventHandlers.
func (d *Daemon) wireCallbacks(
	cfg *config.Config,
	st *state.State,
	sm *session.Manager,
	wm *workspace.Manager,
	mm *models.Manager,
	server *dashboard.Server,
	tmuxServer *tmux.TmuxServer,
	statePath string,
	homeDir string,
	logger *log.Logger,
	remoteLog *log.Logger,
	remoteAccessLog *log.Logger,
) (remoteManager *remote.Manager, tunnelMgr *tunnel.Manager, detachFloorManager func(), eventHandlers map[string][]events.EventHandler) {
	// Wire model manager into server, session manager, workspace manager, and oneshot
	server.SetModelManager(mm)
	sm.SetModelManager(mm)
	wm.SetModelManager(mm)
	oneshot.SetModelManager(mm)

	// Create remote manager for remote workspace support
	remoteManager = remote.NewManager(cfg, st, remoteLog)
	remoteManager.SetWorkspaceManager(wm)
	remoteManager.SetStateChangeCallback(server.BroadcastSessions)
	server.SetRemoteManager(remoteManager)
	sm.SetRemoteManager(remoteManager)
	wm.SetRemoteRunner(remoteManager)
	remoteManager.SetOnConnectCallback(func(hostID string) {
		// Trigger immediate VCS status update for workspaces on this host
		for _, ws := range st.GetWorkspaces() {
			if ws.RemoteHostID == hostID {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				wm.UpdateVCSStatus(ctx, ws.ID)
				cancel()
			}
		}
		server.BroadcastSessions()
	})

	// Create tunnel manager for remote access
	tunnelMgr = tunnel.NewManager(tunnel.ManagerConfig{
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
	eventHandlers = map[string][]events.EventHandler{
		"status": {dashHandler},
	}

	// Monitor handler: always registered, checks debug_ui config per event.
	// Orthogonal to devMode — debug_ui controls diagnostics independently.
	monitorHandler := events.NewMonitorHandler(func(sessionID string, raw events.RawEvent, data []byte) {
		if cfg.GetDebugUI() {
			server.BroadcastEvent(sessionID, data)
		}
	})
	for _, eventType := range []string{"status", "failure", "reflection", "friction"} {
		eventHandlers[eventType] = append(eventHandlers[eventType], monitorHandler)
	}
	sm.SetEventHandlers(eventHandlers)

	// Floor manager
	fmLog := logging.Sub(logger, "floor-manager")
	var fm *floormanager.Manager
	var fmInjector *floormanager.Injector
	var fmMu sync.Mutex

	startFloorManager := func() {
		fmMu.Lock()
		defer fmMu.Unlock()
		if fm != nil {
			return // already running
		}
		fm = floormanager.New(cfg, sm, tmuxServer, homeDir, fmLog)
		fmInjector = floormanager.NewInjector(fm, cfg.GetFloorManagerDebounceMs(), fmLog)
		server.SetFloorManager(fm)
		eventHandlers["status"] = append(eventHandlers["status"], fmInjector)
		sm.SetEventHandlers(eventHandlers)
		if err := fm.Start(d.shutdownCtx); err != nil {
			fmLog.Error("failed to start floor manager", "err", err)
			fm = nil
			fmInjector = nil
			server.SetFloorManager(nil)
		}
	}

	// stopFloorManager kills the FM session (for explicit user disable).
	stopFloorManager := func() {
		fmMu.Lock()
		defer fmMu.Unlock()
		if fm == nil {
			return
		}
		fmInjector.Stop()
		fm.Stop() // kills the tmux session
		server.SetFloorManager(nil)
		// Rebuild event handlers without the injector
		eventHandlers["status"] = []events.EventHandler{dashHandler, monitorHandler}
		sm.SetEventHandlers(eventHandlers)
		fm = nil
		fmInjector = nil
	}

	// detachFloorManager stops monitoring but leaves the tmux session alive
	// so the next daemon start can reconnect to it.
	detachFloorManager = func() {
		fmMu.Lock()
		defer fmMu.Unlock()
		if fm == nil {
			return
		}
		fmInjector.Stop()
		fm.Detach() // keeps the tmux session alive
		server.SetFloorManager(nil)
		eventHandlers["status"] = []events.EventHandler{dashHandler, monitorHandler}
		sm.SetEventHandlers(eventHandlers)
		fm = nil
		fmInjector = nil
	}

	if cfg.GetFloorManagerEnabled() {
		startFloorManager()
	}

	// Wire config toggle callback for floor manager
	server.SetFloorManagerToggle(func(enabled bool) {
		if enabled {
			startFloorManager()
		} else {
			stopFloorManager()
		}
	})

	return
}

// restoreSessions starts tmux servers for restored sessions, resumes output
// trackers and remote signal monitors, reconciles stuck disposals, and creates
// the filesystem git watcher.
func (d *Daemon) restoreSessions(
	cfg *config.Config,
	st *state.State,
	sm *session.Manager,
	wm *workspace.Manager,
	remoteManager *remote.Manager,
	tmuxServer *tmux.TmuxServer,
	tmuxBin string,
	server *dashboard.Server,
	logger *log.Logger,
	sessionLog *log.Logger,
	gitWatcherLog *log.Logger,
) *workspace.GitWatcher {
	// Start tmux servers for all sockets that restored sessions live on.
	activeSocketSet := map[string]bool{cfg.GetTmuxSocketName(): true}
	for _, sess := range st.GetSessions() {
		socket := sess.TmuxSocket
		if socket == "" {
			socket = "default"
		}
		activeSocketSet[socket] = true
	}
	for socket := range activeSocketSet {
		if socket == cfg.GetTmuxSocketName() {
			continue // already started above
		}
		srv := tmux.NewTmuxServer(tmuxBin, socket, nil)
		if err := srv.StartServer(d.shutdownCtx); err != nil {
			logger.Warn("failed to start tmux server for socket", "socket", socket, "err", err)
		}
	}

	// Start output trackers for running sessions restored from state.
	for _, sess := range st.GetSessions() {
		timeoutCtx, cancel := context.WithTimeout(d.shutdownCtx, cfg.XtermQueryTimeout())
		server := sm.ServerForSocket(sess.TmuxSocket)
		exists := false
		if server != nil {
			exists = server.SessionExists(timeoutCtx, sess.TmuxSession)
		}
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

	// Reconcile workspaces/sessions stuck in "disposing" status from a previous crash.
	// When recycle_workspaces is enabled, DisposeForce will recycle (mark as recyclable)
	// instead of deleting, which is the correct recovery behavior — the workspace
	// directory is preserved for future reuse.
	// Run in a goroutine to avoid blocking daemon startup (disposal can take up to 60s).
	go func() {
		for _, w := range st.GetWorkspaces() {
			if w.Status == state.WorkspaceStatusDisposing {
				logger.Info("retrying stuck workspace disposal", "workspace_id", w.ID)
				retryCtx, retryCancel := context.WithTimeout(context.Background(), 60*time.Second)
				if err := wm.DisposeForce(retryCtx, w.ID); err != nil {
					logger.Warn("stuck workspace disposal failed, leaving as disposing", "workspace_id", w.ID, "err", err)
				}
				retryCancel()
			}
		}
		for _, sess := range st.GetSessions() {
			if sess.Status == state.SessionStatusDisposing {
				logger.Info("retrying stuck session disposal", "session_id", sess.ID)
				retryCtx, retryCancel := context.WithTimeout(context.Background(), 30*time.Second)
				if err := sm.Dispose(retryCtx, sess.ID); err != nil {
					logger.Warn("stuck session disposal failed, leaving as disposing", "session_id", sess.ID, "err", err)
				}
				retryCancel()
			}
		}
		server.BroadcastSessions()
	}()

	// Create and start git watcher for filesystem-based change detection.
	// Started after server creation so broadcasts reach WebSocket clients.
	gitWatcher := workspace.NewGitWatcher(cfg, wm, server.BroadcastSessions, gitWatcherLog)
	if gitWatcher != nil {
		wm.SetGitWatcher(gitWatcher)
		// Add watches for all existing local git workspaces (skip remote and non-git)
		for _, w := range st.GetWorkspaces() {
			if w.RemoteHostID == "" && workspace.IsGitVCS(w.VCS) {
				gitWatcher.AddWorkspace(w.ID, w.Path)
			}
		}
		gitWatcher.Start()
	}

	return gitWatcher
}

// initDashboardSX populates dashboard.sx status in state and starts the
// heartbeat and auto-renewal background goroutines.
func (d *Daemon) initDashboardSX(cfg *config.Config, st *state.State, logger *log.Logger) {
	// Populate dashboard.sx status and start background services
	if cfg.GetDashboardSXEnabled() {
		// Populate cert info in state
		dxStatus := st.GetDashboardSXStatus()
		if dxStatus == nil {
			dxStatus = &state.DashboardSXStatus{}
		}
		if domain, err := dashboardsx.GetCertDomain(); err == nil {
			dxStatus.CertDomain = domain
		}
		if expiry, err := dashboardsx.GetCertExpiry(); err == nil {
			dxStatus.CertExpiresAt = expiry
		}
		st.SetDashboardSXStatus(dxStatus)
		st.Save()

		// Start heartbeat and auto-renewal goroutines
		instanceKey, err := dashboardsx.EnsureInstanceKey()
		if err != nil {
			logger.Warn("failed to read instance key", "err", err)
		} else {
			serviceURL := dashboardsx.DefaultServiceURL
			if cfg.Network != nil && cfg.Network.DashboardSX != nil && cfg.Network.DashboardSX.ServiceURL != "" {
				serviceURL = cfg.Network.DashboardSX.ServiceURL
			}
			client := dashboardsx.NewClient(serviceURL, instanceKey, cfg.GetDashboardSXCode())

			writer := &heartbeatStateWriter{state: st}
			go dashboardsx.StartHeartbeat(d.shutdownCtx, client, writer)

			if email := cfg.GetDashboardSXEmail(); email != "" {
				go dashboardsx.StartAutoRenewal(d.shutdownCtx, client, email)
			}
		}
	}
}

// initAutolearn wires the spawn entry system (spawn store + metadata store),
// runs the one-time actions migration, and sets up the autolearn subsystem
// (batch store, pending-merge store, instruction store, executor).
func (d *Daemon) initAutolearn(
	cfg *config.Config,
	st *state.State,
	server *dashboard.Server,
	schmuxDir string,
	autolearnInstructionsDir string,
	autolearnLog *log.Logger,
	logger *log.Logger,
) {
	// Spawn entry system: spawn entries store + metadata store
	emergenceBaseDir := filepath.Join(schmuxDir, "emergence")
	spawnStore := spawn.NewStore(emergenceBaseDir)
	spawnMetadataStore := spawn.NewMetadataStore(emergenceBaseDir)
	server.SetSpawnStore(spawnStore)
	server.SetSpawnMetadataStore(spawnMetadataStore)
	ensure.SetSpawnStores(spawnStore, spawnMetadataStore)
	ensure.SetRepoNameResolver(func(repoURL string) (string, bool) {
		repo, found := cfg.FindRepoByURL(repoURL)
		return repo.Name, found
	})

	// One-time migration from old actions registry to spawn store
	actionBaseDir := filepath.Join(schmuxDir, "actions")
	if _, err := os.Stat(actionBaseDir); err == nil {
		count, migErr := spawn.MigrateFromActions(actionBaseDir, spawnStore)
		if migErr != nil {
			logger.Warn("actions migration failed", "err", migErr)
		} else if count > 0 {
			migratedDir := actionBaseDir + ".migrated"
			if err := os.Rename(actionBaseDir, migratedDir); err != nil {
				logger.Warn("failed to rename actions dir after migration", "err", err)
			} else {
				logger.Info("migrated actions to emergence", "count", count)
			}
		}
	}

	// Autolearn system: wire stores, executor, and instruction store
	if cfg.GetAutolearnEnabled() {
		autolearnBatchDir := filepath.Join(schmuxDir, "autolearn", "batches")
		autolearnStore := autolearn.NewBatchStore(autolearnBatchDir, autolearnLog)
		server.SetAutolearnStore(autolearnStore)

		autolearnPendingMergeDir := filepath.Join(schmuxDir, "autolearn", "pending-merges")
		autolearnPendingMergeStore := autolearn.NewPendingMergeStore(autolearnPendingMergeDir, autolearnLog)
		server.SetAutolearnPendingMergeStore(autolearnPendingMergeStore)

		server.SetAutolearnInstructionStore(autolearn.NewInstructionStore(autolearnInstructionsDir))

		if target := cfg.GetAutolearnTarget(); target != "" {
			autolearnExecutor := func(ctx context.Context, prompt, schemaLabel string, timeout time.Duration) (string, error) {
				return oneshot.ExecuteTarget(ctx, cfg, target, prompt, schemaLabel, timeout, "")
			}
			server.SetAutolearnExecutor(autolearnExecutor)
		}

		autolearnLog.Info("autolearn system enabled")
	}
}

// startAndWait starts the dashboard server, waits for it to bind, writes the
// daemon.url breadcrumb, then blocks until a shutdown signal, server error,
// shutdown channel, or dev-restart channel fires. Returns whether a dev
// restart was requested.
func (d *Daemon) startAndWait(
	server *dashboard.Server,
	cfg *config.Config,
	schmuxDir string,
	background bool,
	logger *log.Logger,
) (devRestart bool, err error) {
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

	// Wait for the server to bind before writing daemon.url
	var urlFile string
	select {
	case boundAddr := <-server.BoundAddr:
		if boundAddr == nil {
			return false, fmt.Errorf("dashboard server failed to bind")
		}
		scheme := "http"
		if cfg.GetTLSEnabled() {
			scheme = "https"
		}
		urlFile = filepath.Join(schmuxDir, "daemon.url")
		daemonURL := fmt.Sprintf("%s://%s", scheme, boundAddr.String())
		if err := os.WriteFile(urlFile, []byte(daemonURL+"\n"), 0644); err != nil {
			return false, fmt.Errorf("failed to write daemon URL file: %w", err)
		}
		defer os.Remove(urlFile)
		logger.Info("daemon URL written", "url", daemonURL, "file", urlFile)
	case err := <-serverErrChan:
		return false, fmt.Errorf("dashboard server error: %w", err)
	}

	// Wait for shutdown signal or server error
	select {
	case sig := <-sigChan:
		logger.Info("received signal, shutting down", "signal", sig)
		d.cancelFunc() // Cancel shutdownCtx so background goroutines exit cleanly
	case err := <-serverErrChan:
		return false, fmt.Errorf("dashboard server error: %w", err)
	case <-d.shutdownChan:
		logger.Info("shutdown requested")
	case <-d.devRestartChan:
		logger.Info("dev restart requested")
		devRestart = true
	}

	return devRestart, nil
}

// startBackgroundJobs launches all background goroutines and one-time setup
// that runs after the dashboard server is created: git status polling,
// NudgeNik checker, subreddit scheduler, repofeed publisher/consumer, and
// PR discovery polling.
func (d *Daemon) startBackgroundJobs(
	cfg *config.Config,
	st *state.State,
	sm *session.Manager,
	wm *workspace.Manager,
	server *dashboard.Server,
	prDiscovery *github.Discovery,
	eventHandlers map[string][]events.EventHandler,
	logger *log.Logger,
	nudgenikLog *log.Logger,
	schmuxDir string,
) {
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
			// Fetch origin queries and update workspace git status concurrently.
			// These operate on independent repos and don't depend on each other.
			var pollWg sync.WaitGroup
			pollWg.Add(2)
			go func() {
				defer pollWg.Done()
				wm.FetchOriginQueries(ctx)
			}()
			go func() {
				defer pollWg.Done()
				wm.UpdateAllVCSStatus(ctx)
			}()
			pollWg.Wait()
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
				// Fetch origin queries and update workspace git status concurrently.
				var pollWg sync.WaitGroup
				pollWg.Add(2)
				go func() {
					defer pollWg.Done()
					wm.FetchOriginQueries(ctx)
				}()
				go func() {
					defer pollWg.Done()
					wm.UpdateAllVCSStatus(ctx)
				}()
				pollWg.Wait()
				cancel()
				server.BroadcastSessions()
			case <-d.shutdownCtx.Done():
				return
			}
		}
	}()

	// Start background goroutine to check for inactive sessions and ask NudgeNik
	go startNudgeNikChecker(d.shutdownCtx, cfg, st, sm, server.BroadcastSessions, nudgenikLog)

	// Start subreddit digest hourly scheduler unconditionally.
	// The scheduler checks config on each run, so this also covers the case
	// where subreddit digest is enabled after the daemon has already started.
	subredditLog := logging.Sub(logger, "subreddit")
	subredditDir := filepath.Join(schmuxDir, "subreddit")
	go startSubredditHourlyGenerator(d.shutdownCtx, cfg, subredditDir, server, subredditLog)

	// Start repofeed intent publisher and consumer
	repofeedLog := logging.Sub(logger, "repofeed")
	devEmail := getGitConfigValue(d.shutdownCtx, "user.email")
	devName := getGitConfigValue(d.shutdownCtx, "user.name")
	repofeedPublisher := repofeed.NewPublisher(repofeed.PublisherConfig{
		DeveloperEmail: devEmail,
		DisplayName:    devName,
	})
	repofeedConsumer := repofeed.NewConsumer(repofeed.ConsumerConfig{
		OwnEmail: devEmail,
	})
	server.SetRepofeedPublisher(repofeedPublisher)
	server.SetRepofeedConsumer(repofeedConsumer)

	if cfg.GetRepofeedEnabled() {
		eventHandlers["status"] = append(eventHandlers["status"], repofeedPublisher)
		sm.SetEventHandlers(eventHandlers)
		repofeedLog.Info("repofeed publisher registered", "email", devEmail)
	}
	go startRepofeedConsumer(d.shutdownCtx, cfg, repofeedConsumer, server, repofeedLog)

	// One-time migration: delete old subreddit.json file
	oldCachePath := filepath.Join(schmuxDir, "subreddit.json")
	if _, err := os.Stat(oldCachePath); err == nil {
		os.Remove(oldCachePath)
		subredditLog.Info("migrated old subreddit.json to new per-repo format")
	}

	// Initialize PR discovery polling based on current config
	// Pass a function so poll always uses current repos list
	prDiscovery.SetTarget(cfg.GetPrReviewTarget(), func() []config.Repo { return cfg.GetRepos() })
}

// initCompound creates and starts the overlay compounder for bidirectional
// overlay sync, wiring callbacks into the session and workspace managers.
// Returns nil if compound is disabled or creation fails.
func (d *Daemon) initCompound(
	cfg *config.Config,
	st *state.State,
	sm *session.Manager,
	wm *workspace.Manager,
	server *dashboard.Server,
	compoundLog *log.Logger,
	overlayLog *log.Logger,
) *compound.Compounder {
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
				// Skip workspaces that are being disposed or are recyclable —
				// writing files into a workspace that is being torn down is pointless.
				if w.Status == state.WorkspaceStatusDisposing || w.Status == state.WorkspaceStatusRecyclable {
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
				compoundLog.Debug("propagated", "path", relPath, "workspace_id", w.ID)
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
		compounder, err = compound.NewCompounder(cfg.GetCompoundDebounceMs(), time.Duration(cfg.GetCompoundSuppressionTTLMs())*time.Millisecond, llmExecutor, propagator, func(workspaceID, relPath, hash string) {
			st.UpdateOverlayManifestEntry(workspaceID, relPath, hash)
		}, compoundLog)
		if err != nil {
			compoundLog.Warn("failed to create compounder", "err", err)
		}
	}

	// Wire compounding callbacks
	if compounder != nil {
		// Per-workspace generation counter prevents stale dispose goroutines from
		// removing watches that were re-added by a subsequent AddWorkspace call.
		// Race: dispose goroutine starts Reconcile (up to 2min), new session spawns
		// and calls AddWorkspace, then the stale goroutine calls RemoveWorkspace —
		// tearing down the newly-added watches.
		var compoundGenMu sync.Mutex
		compoundGen := make(map[string]int64)

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
				compoundGenMu.Lock()
				compoundGen[workspaceID]++
				compoundGenMu.Unlock()
				compounder.AddWorkspace(workspaceID, w.Path, overlayDir, w.Repo, manifest, declaredPaths)
			} else {
				// Last session disposed — reconcile overlay files before the workspace goes dormant.
				// Run in a goroutine to avoid blocking the dispose HTTP handler.
				compoundGenMu.Lock()
				gen := compoundGen[workspaceID]
				compoundGenMu.Unlock()

				ctx, cancel := context.WithCancel(context.Background())
				compounder.SetReconcileCancel(workspaceID, cancel)

				go func() {
					defer cancel() // clean up context resources when goroutine exits naturally

					reconcileCtx, reconcileCancel := context.WithTimeout(ctx, 2*time.Minute)
					defer reconcileCancel()

					compounder.Reconcile(reconcileCtx, workspaceID)
					compoundGenMu.Lock()
					stale := compoundGen[workspaceID] != gen
					compoundGenMu.Unlock()
					if stale {
						compoundLog.Info("skipping RemoveWorkspace (workspace re-added during reconcile)", "workspace_id", workspaceID)
						return
					}
					compounder.RemoveWorkspace(workspaceID)
				}()
			}
		})

		wm.SetCompoundReconcile(func(workspaceID string) {
			// Cancel any in-flight background reconcile from session dispose.
			// Best-effort: with few overlay files the goroutine may have already finished.
			// The real safety guarantee is RemoveWorkspace below.
			compounder.CancelReconcile(workspaceID)

			// Run the authoritative reconcile synchronously.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			compounder.Reconcile(ctx, workspaceID)

			// Unconditionally remove workspace from the compounder.
			// This stops all watches and cancels debounce timers BEFORE
			// dispose() proceeds to delete files. This is the hard safety gate.
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
			compoundGenMu.Lock()
			compoundGen[wsID]++
			compoundGenMu.Unlock()
			compounder.AddWorkspace(wsID, w.Path, overlayDir, w.Repo, manifest, declaredPaths)
		}
		compounder.Start()
		compoundLog.Info("started overlay compounding loop")
	}

	return compounder
}

// shutdown performs the orderly shutdown sequence: flush state, stop all
// subsystems, and stop the dashboard server.
func (d *Daemon) shutdown(h shutdownHandles) error {
	// Flush any pending batched state saves and do a final save
	h.st.FlushPending()
	if err := h.st.Save(); err != nil {
		d.logger.Warn("final state save failed", "err", err)
	}

	// Shutdown telemetry (flush pending events)
	h.tel.Shutdown()

	// Stop session manager (kills tmux attach-client processes)
	h.sm.Stop()

	// Disconnect all remote hosts (cancels pending connect goroutines, kills SSH processes)
	h.remoteManager.DisconnectAll()

	// Stop tunnel manager
	h.tunnelMgr.Stop()

	// Stop git watcher
	if h.gitWatcher != nil {
		h.gitWatcher.Stop()
	}

	// Stop compounder
	if h.compounder != nil {
		h.compounder.Stop()
	}

	// Detach floor manager — leave tmux session alive for reconnection on restart
	h.detachFloorManager()

	// Stop dashboard server
	return h.server.Stop()
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
		nudge := askNudgeNikForSession(ctx, cfg, sess, sm, logger)
		if nudge != "" {
			if ok := st.UpdateSessionFunc(sess.ID, func(s *state.Session) { s.Nudge = nudge }); !ok {
				logger.Error("failed to save nudge (session not found)", "session_id", sess.ID)
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
// Captures via SessionRuntime so both local and remote sessions are handled correctly.
func askNudgeNikForSession(ctx context.Context, cfg *config.Config, sess state.Session, sm *session.Manager, logger *log.Logger) string {
	// Capture via tracker (handles local and remote sessions via ControlSource)
	tracker, err := sm.GetTracker(sess.ID)
	if err != nil {
		logger.Error("failed to get tracker", "session_id", sess.ID, "err", err)
		return ""
	}

	captureCtx, cancel := context.WithTimeout(ctx, cfg.XtermOperationTimeout())
	content, err := tracker.CaptureLastLines(captureCtx, 100)
	cancel()
	if err != nil {
		logger.Error("failed to capture", "session_id", sess.ID, "err", err)
		return ""
	}

	result, err := nudgenik.AskForCapture(ctx, cfg, content)
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
// subreddit posts on a configured interval.
func startSubredditHourlyGenerator(ctx context.Context, cfg *config.Config, subredditDir string, server *dashboard.Server, logger *log.Logger) {
	// Wait a bit before first check to let daemon start.
	initialDelay := 30 * time.Second
	nextTime := time.Now().Add(initialDelay)
	server.SetNextSubredditGeneration(nextTime)
	interval := time.Duration(cfg.GetSubredditInterval()) * time.Minute
	logger.Info("subreddit scheduler started", "initial_delay", initialDelay, "interval", interval, "next_at", nextTime)

	timer := time.NewTimer(initialDelay)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			logger.Debug("subreddit scheduler tick")
			generateSubredditPosts(ctx, server, logger)

			nextTime = time.Now().Add(interval)
			server.SetNextSubredditGeneration(nextTime)
			logger.Debug("subreddit next scheduled", "next_at", nextTime)

			timer.Reset(interval)
		case <-ctx.Done():
			return
		}
	}
}

// generateSubredditPosts generates subreddit posts for all enabled repos.
func generateSubredditPosts(ctx context.Context, server *dashboard.Server, logger *log.Logger) {
	if err := server.GenerateSubredditForAllRepos(ctx); err != nil {
		logger.Error("subreddit generation failed", "err", err)
	}
}

// startRepofeedConsumer starts the background fetch loop for repofeed data.
func startRepofeedConsumer(ctx context.Context, cfg *config.Config, consumer *repofeed.Consumer, server *dashboard.Server, logger *log.Logger) {
	if !cfg.GetRepofeedEnabled() {
		logger.Debug("repofeed consumer disabled")
		return
	}

	interval := time.Duration(cfg.GetRepofeedFetchInterval()) * time.Second
	logger.Info("repofeed consumer started", "interval", interval)

	timer := time.NewTimer(30 * time.Second) // initial delay
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			logger.Debug("repofeed consumer tick")

			var allFiles []*repofeed.DeveloperFile
			for _, repo := range cfg.GetRepos() {
				slug := repofeed.RepoSlug(repo.Name)
				if !cfg.GetRepofeedRepoEnabled(slug) {
					continue
				}
				bareDir := cfg.ResolveBareRepoDir(repo.BarePath)
				if bareDir == "" {
					continue
				}
				gitOps := &repofeed.GitOps{BareDir: bareDir, Branch: "dev-repofeed"}

				if err := gitOps.FetchFromRemote("origin"); err != nil {
					logger.Debug("repofeed fetch failed (branch may not exist yet)", "repo", repo.Name, "err", err)
				}

				files, err := gitOps.ReadAllDevFiles()
				if err != nil {
					logger.Debug("repofeed read failed", "repo", repo.Name, "err", err)
					continue
				}
				allFiles = append(allFiles, files...)
			}

			consumer.UpdateFromFiles(allFiles)
			server.BroadcastRepofeed()
			timer.Reset(interval)
		case <-ctx.Done():
			return
		}
	}
}

// getGitConfigValue reads a global git config value.
func getGitConfigValue(ctx context.Context, key string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "config", "--global", key)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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
	backupDir := schmuxdir.BackupsDir()
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

// cleanupOrphanedLoreWorktrees removes temporary directories from previous
// lore push operations that may have been left behind by a daemon crash.
func cleanupOrphanedLoreWorktrees() {
	pattern := filepath.Join(os.TempDir(), "lore-push-*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}
	for _, dir := range matches {
		os.RemoveAll(dir)
	}
}
