package session

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/events"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/remote"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/telemetry"
	"github.com/sergeknystautas/schmux/internal/tmux"
	"github.com/sergeknystautas/schmux/internal/workspace"
	"github.com/sergeknystautas/schmux/internal/workspace/ensure"
	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

const (
	// maxNicknameAttempts is the maximum number of attempts to find a unique nickname
	// before falling back to a UUID suffix.
	maxNicknameAttempts = 100
)

// Manager manages sessions.
type Manager struct {
	config                  *config.Config
	state                   state.StateStore
	workspace               workspace.WorkspaceManager
	logger                  *log.Logger
	ensurer                 *ensure.Ensurer
	models                  *models.Manager // Model catalog, resolution, enablement
	remoteManager           *remote.Manager // Optional, for remote sessions
	eventHandlers           map[string][]events.EventHandler
	outputCallback          func(sessionID string, chunk []byte)
	trackers                map[string]*SessionTracker
	remoteDetectors         map[string]*remoteSignalMonitor // signal detectors for remote sessions
	mu                      sync.RWMutex
	compoundCallback        func(workspaceID string, isSpawn bool)             // notify compounder on session spawn/dispose
	loreCallback            func(repoName, repoURL string, isLastSession bool) // notify lore curator on session dispose
	terminalCaptureCallback func(sessionID, workspaceID, output string)        // notify on terminal capture before dispose
	telemetry               telemetry.Telemetry                                // optional, for usage tracking
}

// remoteSignalMonitor holds a watcher pane and its metadata for a remote session.
type remoteSignalMonitor struct {
	remoteEventWatcher *events.RemoteEventWatcher
	watcherWindowID    string
	watcherPaneID      string
	stopCh             chan struct{}
}

// ResolvedTarget is a resolved run target with command and env info.
type ResolvedTarget struct {
	Name       string
	Kind       string
	Command    string
	Promptable bool
	Env        map[string]string
	Model      *detect.Model
	ToolName   string // the resolved tool name (e.g., "claude", "opencode")
}

const (
	TargetKindDetected = "detected"
	TargetKindModel    = "model"
	TargetKindUser     = "user"
)

// New creates a new session manager.
func New(cfg *config.Config, st state.StateStore, statePath string, wm workspace.WorkspaceManager, logger *log.Logger) *Manager {
	if logger == nil {
		logger = log.NewWithOptions(io.Discard, log.Options{})
	}
	return &Manager{
		config:          cfg,
		state:           st,
		workspace:       wm,
		logger:          logger,
		ensurer:         ensure.New(st),
		trackers:        make(map[string]*SessionTracker),
		remoteDetectors: make(map[string]*remoteSignalMonitor),
		remoteManager:   nil,
	}
}

// SetRemoteManager sets the remote manager for remote session support.
// Must be called before Start() — not safe for concurrent use.
func (m *Manager) SetRemoteManager(rm *remote.Manager) {
	m.remoteManager = rm
}

// SetModelManager sets the model manager for model resolution.
// Must be called before Start() — not safe for concurrent use.
func (m *Manager) SetModelManager(mm *models.Manager) {
	m.models = mm
	m.ensurer.SetResolver(mm)
}

// SetHooksDir sets the centralized hooks directory on the ensurer.
// Must be called before Start() — not safe for concurrent use.
func (m *Manager) SetHooksDir(dir string) {
	m.ensurer.SetHooksDir(dir)
}

// SetEventHandlers sets the event handlers for the unified event system.
// Must be called before Start() — not safe for concurrent use.
func (m *Manager) SetEventHandlers(handlers map[string][]events.EventHandler) {
	m.eventHandlers = handlers
}

// SetOutputCallback sets the callback for terminal output chunks from local trackers.
// Must be called before Start() — not safe for concurrent use.
func (m *Manager) SetOutputCallback(cb func(sessionID string, chunk []byte)) {
	m.outputCallback = cb
}

// SetCompoundCallback sets the callback for notifying the compounder on session lifecycle events.
// Must be called before Start() — not safe for concurrent use.
func (m *Manager) SetCompoundCallback(cb func(workspaceID string, isSpawn bool)) {
	m.compoundCallback = cb
}

// SetLoreCallback sets the callback for notifying the lore system on session dispose.
// Must be called before Start() — not safe for concurrent use.
func (m *Manager) SetLoreCallback(cb func(repoName, repoURL string, isLastSession bool)) {
	m.loreCallback = cb
}

// SetTerminalCaptureCallback sets the callback for capturing terminal output before session dispose.
// Must be called before Start() — not safe for concurrent use.
func (m *Manager) SetTerminalCaptureCallback(cb func(sessionID, workspaceID, output string)) {
	m.terminalCaptureCallback = cb
}

// SetTelemetry sets the telemetry client for usage tracking.
func (m *Manager) SetTelemetry(t telemetry.Telemetry) {
	m.telemetry = t
}

// trackSessionCreated sends a telemetry event for session creation.
// Safe to call even if telemetry is nil.
func (m *Manager) trackSessionCreated(sessionID, workspaceID, target string) {
	if m.telemetry == nil {
		return
	}
	m.telemetry.Track("session_created", map[string]any{
		"session_id":   sessionID,
		"workspace_id": workspaceID,
		"target":       target,
	})
}

// StartRemoteSignalMonitor creates a watcher pane on the remote host that monitors
// the event file and emits sentinel-wrapped output. A goroutine subscribes to the
// watcher pane's output and dispatches events via handlers.
// The goroutine automatically reconnects when the output channel closes (e.g., due to
// a dropped remote connection), retrying until explicitly stopped via StopRemoteSignalMonitor.
func (m *Manager) StartRemoteSignalMonitor(sess state.Session) {
	if m.remoteManager == nil || sess.RemotePaneID == "" || sess.RemoteHostID == "" {
		return
	}
	if len(m.eventHandlers) == 0 {
		return
	}

	m.mu.Lock()
	if m.remoteDetectors[sess.ID] != nil {
		m.mu.Unlock()
		return // already running
	}

	sessionID := sess.ID
	hostID := sess.RemoteHostID

	// Determine workspace path for the event file before inserting into the map,
	// so we never create a dangling entry with an unclosed stopCh.
	workspacePath := ""
	if ws, ok := m.state.GetWorkspace(sess.WorkspaceID); ok {
		if ws.RemotePath != "" {
			workspacePath = ws.RemotePath
		} else {
			workspacePath = ws.Path
		}
	}
	if workspacePath == "" {
		m.mu.Unlock()
		eventsLog := logging.Sub(m.logger, "events")
		eventsLog.Warn("cannot start remote watcher: no workspace path", "session", sessionID)
		return
	}
	eventsFilePath := filepath.Join(workspacePath, ".schmux", "events", sessionID+".jsonl")

	stopCh := make(chan struct{})
	m.remoteDetectors[sess.ID] = &remoteSignalMonitor{
		stopCh: stopCh,
	}
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.remoteDetectors, sessionID)
			m.mu.Unlock()
		}()

		for {
			// Check for stop before attempting connection
			select {
			case <-stopCh:
				return
			default:
			}

			// Wait for connection to be available
			conn := m.remoteManager.GetConnection(hostID)
			if conn == nil || !conn.IsConnected() {
				select {
				case <-stopCh:
					return
				case <-time.After(2 * time.Second):
					continue
				}
			}

			// Create watcher pane: a hidden tmux window running the file watcher script
			shortID := sessionID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			windowName := "schmux-events-" + shortID

			ctx := context.Background()
			windowID, paneID, err := conn.CreateSession(ctx, windowName, workspacePath, "")
			if err != nil {
				eventsLog := logging.Sub(m.logger, "events")
				eventsLog.Error("failed to create watcher window", "session", sessionID, "err", err)
				select {
				case <-stopCh:
					return
				case <-time.After(2 * time.Second):
					continue
				}
			}

			// Wait briefly for shell init
			select {
			case <-stopCh:
				// Clean up watcher window before returning
				conn.KillSession(ctx, windowID)
				return
			case <-time.After(200 * time.Millisecond):
			}

			// Type the event watcher script into the pane
			watcherScript := events.RemoteWatcherScript(eventsFilePath)
			if err := conn.SendKeys(ctx, paneID, watcherScript+"\n"); err != nil {
				eventsLog := logging.Sub(m.logger, "events")
				eventsLog.Error("failed to send watcher script", "session", sessionID, "err", err)
				conn.KillSession(ctx, windowID)
				select {
				case <-stopCh:
					return
				case <-time.After(2 * time.Second):
					continue
				}
			}

			// Create the event watcher
			remoteEvWatcher := events.NewRemoteEventWatcher(sessionID, m.eventHandlers)

			// Update the monitor reference
			m.mu.Lock()
			if mon := m.remoteDetectors[sessionID]; mon != nil {
				mon.remoteEventWatcher = remoteEvWatcher
				mon.watcherWindowID = windowID
				mon.watcherPaneID = paneID
			}
			m.mu.Unlock()

			// Subscribe to output from the watcher pane
			outputCh := conn.SubscribeOutput(paneID)

		inner:
			for {
				select {
				case <-stopCh:
					conn.UnsubscribeOutput(paneID, outputCh)
					conn.KillSession(ctx, windowID)
					return
				case event, ok := <-outputCh:
					if !ok {
						break inner
					}
					if event.Data != "" {
						remoteEvWatcher.ProcessOutput(event.Data)
					}
				}
			}

			// Channel closed (connection dropped) — wait 1s before retrying
			select {
			case <-stopCh:
				return
			case <-time.After(1 * time.Second):
				// retry
			}
		}
	}()
}

// StopRemoteSignalMonitor stops the watcher pane and goroutine for a remote session.
func (m *Manager) StopRemoteSignalMonitor(sessionID string) {
	m.mu.Lock()
	mon := m.remoteDetectors[sessionID]
	delete(m.remoteDetectors, sessionID)
	m.mu.Unlock()
	if mon == nil {
		return
	}
	close(mon.stopCh)

	// Kill the watcher window if we have a connection
	if mon.watcherWindowID != "" && m.remoteManager != nil {
		sess, ok := m.state.GetSession(sessionID)
		if ok {
			conn := m.remoteManager.GetConnection(sess.RemoteHostID)
			if conn != nil && conn.IsConnected() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				conn.KillSession(ctx, mon.watcherWindowID)
				cancel()
			}
		}
	}
}

// GetRemoteManager returns the remote manager (may be nil).
func (m *Manager) GetRemoteManager() *remote.Manager {
	return m.remoteManager
}

// SpawnRemote creates a new session on a remote host.
// flavorID identifies the remote flavor to connect to.
// targetName is the agent to run (e.g., "claude").
// prompt is only used if the target is promptable.
// nickname is an optional human-friendly name for the session.
func (m *Manager) SpawnRemote(ctx context.Context, flavorID, targetName, prompt, nickname string) (*state.Session, error) {
	if m.remoteManager == nil {
		return nil, fmt.Errorf("remote manager not configured")
	}

	resolved, err := m.ResolveTarget(ctx, targetName)
	if err != nil {
		return nil, err
	}

	// Connect to or get existing connection for this flavor
	conn, err := m.remoteManager.Connect(ctx, flavorID)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to remote host: %w", err)
	}

	host := conn.Host()
	flavor := conn.Flavor()

	// Create session ID
	sessionID := fmt.Sprintf("remote-%s-%s", flavorID, uuid.New().String()[:8])

	// Get or create a workspace for this remote host+flavor
	// Use deterministic ID so all sessions on same host+flavor share a workspace
	workspaceID := fmt.Sprintf("remote-%s", host.ID)
	ws, found := m.state.GetWorkspace(workspaceID)
	if !found {
		// Use hostname as the branch name (shown in sidebar/header)
		branch := host.Hostname
		if branch == "" {
			branch = flavor.DisplayName
		}
		// Create new workspace for this remote host
		ws = state.Workspace{
			ID:           workspaceID,
			Repo:         flavor.DisplayName,
			Branch:       branch,
			Path:         flavor.WorkspacePath,
			RemoteHostID: host.ID,
			RemotePath:   flavor.WorkspacePath,
		}
		if err := m.state.AddWorkspace(ws); err != nil {
			return nil, fmt.Errorf("failed to add workspace to state: %w", err)
		}
	} else if ws.Branch == "remote" && host.Hostname != "" {
		// Update existing workspace that still has the old "remote" branch name
		ws.Branch = host.Hostname
		m.state.UpdateWorkspace(ws)
	}

	// Inject schmux signaling environment variables
	resolved.Env = mergeEnvMaps(resolved.Env, map[string]string{
		"SCHMUX_ENABLED":      "1",
		"SCHMUX_SESSION_ID":   sessionID,
		"SCHMUX_WORKSPACE_ID": workspaceID,
		"SCHMUX_EVENTS_FILE":  filepath.Join(flavor.WorkspacePath, ".schmux", "events", sessionID+".jsonl"),
	})

	// Build command with remote mode (uses inline content instead of local file paths)
	command, err := buildCommand(resolved, prompt, nil, false, true)
	if err != nil {
		return nil, err
	}

	// For tools with hook support, prepend hooks provisioning to the command
	// so hooks are in place before the agent starts (it captures hooks at startup).
	baseTool := m.models.ResolveTargetToTool(targetName)
	if adapter := detect.GetAdapter(baseTool); adapter != nil && adapter.SupportsHooks() {
		command, err = adapter.WrapRemoteCommand(command)
		if err != nil {
			m.logger.Warn("failed to wrap command with hooks provisioning", "err", err)
		}
	}

	// Generate unique nickname if provided
	uniqueNickname := nickname
	if nickname != "" {
		uniqueNickname = m.generateUniqueNickname(nickname)
	}

	// Use nickname as window name if provided, otherwise use sessionID
	windowName := sessionID
	if uniqueNickname != "" {
		windowName = sanitizeNickname(uniqueNickname)
	}

	// Check if connection is ready
	if !conn.IsConnected() {
		// Queue the session creation (directory will be created when connection is ready)
		resultCh := conn.QueueSession(ctx, sessionID, windowName, flavor.WorkspacePath, command)

		// Create session with status="provisioning"
		sess := state.Session{
			ID:           sessionID,
			WorkspaceID:  ws.ID,
			Target:       targetName,
			Nickname:     uniqueNickname,
			TmuxSession:  windowName,
			CreatedAt:    time.Now(),
			Pid:          0, // No local PID for remote sessions
			RemoteHostID: host.ID,
			RemotePaneID: "", // Will be set when queue is drained
			RemoteWindow: "", // Will be set when queue is drained
			Status:       "provisioning",
		}

		// Save immediately with provisioning status
		if err := m.state.AddSession(sess); err != nil {
			return nil, fmt.Errorf("failed to add session to state: %w", err)
		}
		if err := m.state.Save(); err != nil {
			return nil, fmt.Errorf("failed to save state: %w", err)
		}

		// Wait for queue to process (async)
		go func() {
			select {
			case result := <-resultCh:
				var updatedStatus string
				var updatedSess state.Session
				ok := m.state.UpdateSessionFunc(sessionID, func(sess *state.Session) {
					if result.Error != nil {
						m.logger.Error("queued session failed", "session", sessionID, "err", result.Error)
						sess.Status = "failed"
					} else {
						m.logger.Info("queued session succeeded", "session", sessionID, "window", result.WindowID, "pane", result.PaneID)
						sess.Status = "running"
						sess.RemoteWindow = result.WindowID
						sess.RemotePaneID = result.PaneID
					}
					updatedStatus = sess.Status
					updatedSess = *sess
				})
				if !ok {
					m.logger.Warn("queued session: session no longer in state", "session", sessionID)
					return
				}
				if err := m.state.Save(); err != nil {
					m.logger.Error("queued session: failed to save state", "session", sessionID, "err", err)
				}
				if updatedStatus == "running" {
					// Ensure .schmux/events directory exists on remote host
					qConn := m.remoteManager.GetConnection(host.ID)
					if qConn != nil && qConn.IsConnected() {
						mkCtx, mkCancel := context.WithTimeout(context.Background(), 5*time.Second)
						if _, mkErr := qConn.RunCommand(mkCtx, flavor.WorkspacePath, "mkdir -p .schmux/events"); mkErr != nil {
							m.logger.Warn("failed to create .schmux/events directory", "session", sessionID, "err", mkErr)
						}
						mkCancel()
					}
					m.StartRemoteSignalMonitor(updatedSess)
				}
			}
		}()

		// Track session creation (queued)
		m.trackSessionCreated(sess.ID, sess.WorkspaceID, sess.Target)

		return &sess, nil
	}

	// Connected - create immediately (existing code path)

	// Ensure .schmux/events directory exists on remote host
	mkdirCtx, mkdirCancel := context.WithTimeout(ctx, 5*time.Second)
	if _, err := conn.RunCommand(mkdirCtx, flavor.WorkspacePath, "mkdir -p .schmux/events"); err != nil {
		m.logger.Warn("failed to create .schmux/events directory on remote host", "err", err)
	}
	mkdirCancel()

	windowID, paneID, err := conn.CreateSession(ctx, windowName, flavor.WorkspacePath, command)
	if err != nil {
		return nil, fmt.Errorf("failed to create remote session: %w", err)
	}

	// Create session state
	sess := state.Session{
		ID:           sessionID,
		WorkspaceID:  ws.ID,
		Target:       targetName,
		Nickname:     uniqueNickname,
		TmuxSession:  windowName,
		CreatedAt:    time.Now(),
		Pid:          0, // No local PID for remote sessions
		RemoteHostID: host.ID,
		RemotePaneID: paneID,
		RemoteWindow: windowID,
		Status:       "running",
	}

	if err := m.state.AddSession(sess); err != nil {
		return nil, fmt.Errorf("failed to add session to state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	m.StartRemoteSignalMonitor(sess)

	// Track session creation (immediate)
	m.trackSessionCreated(sess.ID, sess.WorkspaceID, sess.Target)

	return &sess, nil
}

// SpawnOptions holds parameters for Spawn and SpawnCommand.
type SpawnOptions struct {
	RepoURL          string
	Branch           string
	TargetName       string
	Prompt           string
	Command          string
	Nickname         string
	WorkspaceID      string
	Resume           bool
	NewBranch        string
	PersonaID        string
	PersonaPrompt    string   // Pre-resolved persona prompt content (set by handler)
	ImageAttachments []string // base64-encoded PNGs (decoded and written during spawn)
}

// resolveWorkspace resolves the target workspace from SpawnOptions.
// If WorkspaceID+NewBranch are set, creates a new workspace branching from the source.
// If only WorkspaceID is set, looks up the existing workspace.
// Otherwise, finds or creates a workspace by RepoURL/Branch.
func (m *Manager) resolveWorkspace(ctx context.Context, opts SpawnOptions) (*state.Workspace, error) {
	if opts.WorkspaceID != "" && opts.NewBranch != "" {
		w, err := m.workspace.CreateFromWorkspace(ctx, opts.WorkspaceID, opts.NewBranch)
		if err != nil {
			return nil, fmt.Errorf("failed to create workspace from source: %w", err)
		}
		return w, nil
	}
	if opts.WorkspaceID != "" {
		ws, found := m.workspace.GetByID(opts.WorkspaceID)
		if !found {
			return nil, fmt.Errorf("workspace not found: %s", opts.WorkspaceID)
		}
		return ws, nil
	}
	w, err := m.workspace.GetOrCreate(ctx, opts.RepoURL, opts.Branch)
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace: %w", err)
	}
	return w, nil
}

// writeImageAttachments decodes base64 image data and writes files to the
// workspace's .schmux/attachments/ directory. Returns absolute file paths.
// Individual decode/write failures are skipped (partial success is possible).
func writeImageAttachments(workspacePath string, images []string) ([]string, error) {
	dir := filepath.Join(workspacePath, ".schmux", "attachments")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create attachments directory: %w", err)
	}

	var paths []string
	for _, b64 := range images {
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			continue
		}
		filename := fmt.Sprintf("img-%s.png", uuid.New().String()[:8])
		filePath := filepath.Join(dir, filename)
		if err := os.WriteFile(filePath, data, 0644); err != nil {
			continue
		}
		paths = append(paths, filePath)
	}
	return paths, nil
}

// appendImagePathsToPrompt appends image file paths to the prompt text.
func appendImagePathsToPrompt(prompt string, paths []string) string {
	if len(paths) == 0 {
		return prompt
	}
	var sb strings.Builder
	sb.WriteString(prompt)
	sb.WriteString("\n\nImage attachments:")
	for i, p := range paths {
		sb.WriteString(fmt.Sprintf("\nImage #%d: %s", i+1, p))
	}
	return sb.String()
}

// Spawn creates a new session.
// If WorkspaceID is provided, spawn into that specific workspace (Existing Directory Spawn mode).
// If WorkspaceID is provided with NewBranch, create a new workspace branching from the source workspace's branch.
// Otherwise, find or create a workspace by RepoURL/Branch.
// Nickname is an optional human-friendly name for the session.
// Prompt is only used if the target is promptable.
// Resume enables resume mode, which uses the agent's resume command instead of a prompt.
func (m *Manager) Spawn(ctx context.Context, opts SpawnOptions) (*state.Session, error) {
	resolved, err := m.ResolveTarget(ctx, opts.TargetName)
	if err != nil {
		return nil, err
	}

	w, err := m.resolveWorkspace(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Provision agent signaling mechanism
	baseTool := m.models.ResolveTargetToTool(opts.TargetName)

	// Ensure workspace has all necessary schmux configuration (hooks, scripts, git exclude)
	if err := m.ensurer.ForSpawn(w.ID, opts.TargetName); err != nil {
		m.logger.Warn("failed to ensure workspace config", "err", err)
	}

	// Session-level signaling setup (not workspace-level)
	if adapter := detect.GetAdapter(baseTool); adapter != nil {
		switch adapter.SignalingStrategy() {
		case detect.SignalingHooks:
			// Handled by ensurer.ForSpawn above
		case detect.SignalingCLIFlag:
			if err := ensure.SignalingInstructionsFile(); err != nil {
				m.logger.Warn("failed to ensure signaling instructions file", "err", err)
			}
		case detect.SignalingInstructionFile:
			if err := ensure.AgentInstructions(w.Path, opts.TargetName, w.Repo); err != nil {
				m.logger.Warn("failed to provision agent instructions", "err", err)
			}
		}
	}

	// Ensure .schmux/events directory exists for event-based signaling
	eventsDir := filepath.Join(w.Path, ".schmux", "events")
	if err := os.MkdirAll(eventsDir, 0755); err != nil {
		m.logger.Warn("failed to create .schmux/events directory", "err", err)
	}

	// Write image attachments to workspace and append paths to prompt.
	// Note: in multi-target spawns, each agent call writes its own copy of
	// the images. This is acceptable — files are small and git-excluded.
	if len(opts.ImageAttachments) > 0 {
		imgPaths, err := writeImageAttachments(w.Path, opts.ImageAttachments)
		if err != nil {
			m.logger.Warn("failed to write image attachments", "err", err)
		}
		if len(imgPaths) > 0 {
			opts.Prompt = appendImagePathsToPrompt(opts.Prompt, imgPaths)
		}
	}

	// Use model from resolution (already populated by ResolveTarget)
	model := resolved.Model

	// Create session ID
	sessionID := fmt.Sprintf("%s-%s", w.ID, uuid.New().String()[:8])

	// Inject schmux signaling environment variables
	resolved.Env = mergeEnvMaps(resolved.Env, map[string]string{
		"SCHMUX_ENABLED":      "1",
		"SCHMUX_SESSION_ID":   sessionID,
		"SCHMUX_WORKSPACE_ID": w.ID,
		"SCHMUX_EVENTS_FILE":  filepath.Join(w.Path, ".schmux", "events", sessionID+".jsonl"),
	})

	// Write initial spawn event with full prompt
	if opts.Prompt != "" {
		eventsFile := filepath.Join(w.Path, ".schmux", "events", sessionID+".jsonl")
		spawnEvt := events.StatusEvent{
			Type:    "status",
			State:   "working",
			Message: "Session spawned",
			Intent:  opts.Prompt,
		}
		if err := events.AppendEvent(eventsFile, spawnEvt); err != nil {
			m.logger.Warn("failed to write spawn prompt event", "err", err)
		}
	}

	command, err := buildCommand(resolved, opts.Prompt, model, opts.Resume, false)
	if err != nil {
		return nil, err
	}

	// Inject persona prompt if provided
	if opts.PersonaPrompt != "" {
		personaFilePath := filepath.Join(w.Path, ".schmux", fmt.Sprintf("persona-%s.md", sessionID))
		if err := os.WriteFile(personaFilePath, []byte(opts.PersonaPrompt), 0644); err != nil {
			m.logger.Warn("failed to write persona file", "err", err)
		} else {
			command = appendPersonaFlags(command, baseTool, personaFilePath)

			// Inject spawn-time env vars (e.g., OPENCODE_CONFIG_CONTENT for persona)
			if adapter := detect.GetAdapter(baseTool); adapter != nil {
				spawnEnv := adapter.SpawnEnv(detect.SpawnContext{
					WorkspacePath: w.Path,
					SessionID:     sessionID,
					PersonaPath:   personaFilePath,
				})
				if len(spawnEnv) > 0 {
					resolved.Env = mergeEnvMaps(resolved.Env, spawnEnv)
				}
			}
		}
	}

	// Generate unique nickname if provided (auto-suffix if duplicate)
	uniqueNickname := opts.Nickname
	if opts.Nickname != "" {
		uniqueNickname = m.generateUniqueNickname(opts.Nickname)
	}

	// Use sanitized unique nickname for tmux session name if provided, otherwise use sessionID
	tmuxSession := sessionID
	if uniqueNickname != "" {
		tmuxSession = sanitizeNickname(uniqueNickname)
	}

	// Create tmux session
	if err := tmux.CreateSession(ctx, tmuxSession, w.Path, command); err != nil {
		return nil, fmt.Errorf("failed to create tmux session: %w", err)
	}

	// Configure status bar: process on left, time on right, clear center
	tmux.ConfigureStatusBar(ctx, tmuxSession)

	// Get the PID of the agent process from tmux pane
	pid, err := tmux.GetPanePID(ctx, tmuxSession)
	if err != nil {
		return nil, fmt.Errorf("failed to get pane PID: %w", err)
	}

	// Create session state with cached PID (no Prompt field)
	sess := state.Session{
		ID:          sessionID,
		WorkspaceID: w.ID,
		Target:      opts.TargetName,
		Nickname:    uniqueNickname,
		PersonaID:   opts.PersonaID,
		TmuxSession: tmuxSession,
		CreatedAt:   time.Now(),
		Pid:         pid,
	}

	if err := m.state.AddSession(sess); err != nil {
		return nil, fmt.Errorf("failed to add session to state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	// Notify compounder about new session
	if m.compoundCallback != nil {
		w, found := m.state.GetWorkspace(sess.WorkspaceID)
		if found {
			m.compoundCallback(w.ID, true)
		}
	}

	m.ensureTrackerFromSession(sess)

	// Track session creation
	m.trackSessionCreated(sess.ID, sess.WorkspaceID, sess.Target)

	return &sess, nil
}

// SpawnCommand spawns a session running a raw shell command.
// Used for quick launch presets with a direct command (no target resolution).
func (m *Manager) SpawnCommand(ctx context.Context, opts SpawnOptions) (*state.Session, error) {
	w, err := m.resolveWorkspace(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Create session ID
	sessionID := fmt.Sprintf("%s-%s", w.ID, uuid.New().String()[:8])

	// Ensure .schmux/events directory exists for event-based signaling
	eventsDirCmd := filepath.Join(w.Path, ".schmux", "events")
	if err := os.MkdirAll(eventsDirCmd, 0755); err != nil {
		m.logger.Warn("failed to create .schmux/events directory", "err", err)
	}

	// Inject schmux signaling environment variables into the command
	schmuxEnv := map[string]string{
		"SCHMUX_ENABLED":      "1",
		"SCHMUX_SESSION_ID":   sessionID,
		"SCHMUX_WORKSPACE_ID": w.ID,
		"SCHMUX_EVENTS_FILE":  filepath.Join(w.Path, ".schmux", "events", sessionID+".jsonl"),
	}
	commandWithEnv := fmt.Sprintf("%s %s", buildEnvPrefix(schmuxEnv), opts.Command)

	// Generate unique nickname if provided (auto-suffix if duplicate)
	uniqueNickname := opts.Nickname
	if opts.Nickname != "" {
		uniqueNickname = m.generateUniqueNickname(opts.Nickname)
	}

	// Use sanitized unique nickname for tmux session name if provided, otherwise use sessionID
	tmuxSession := sessionID
	if uniqueNickname != "" {
		tmuxSession = sanitizeNickname(uniqueNickname)
	}

	// Create tmux session with the raw command
	if err := tmux.CreateSession(ctx, tmuxSession, w.Path, commandWithEnv); err != nil {
		return nil, fmt.Errorf("failed to create tmux session: %w", err)
	}

	// Configure status bar: process on left, time on right, clear center
	tmux.ConfigureStatusBar(ctx, tmuxSession)

	// Get the PID of the process from tmux pane
	pid, err := tmux.GetPanePID(ctx, tmuxSession)
	if err != nil {
		return nil, fmt.Errorf("failed to get pane PID: %w", err)
	}

	// Create session state (Target uses a stable value for command-based sessions)
	sess := state.Session{
		ID:          sessionID,
		WorkspaceID: w.ID,
		Target:      "command",
		Nickname:    uniqueNickname,
		TmuxSession: tmuxSession,
		CreatedAt:   time.Now(),
		Pid:         pid,
	}

	if err := m.state.AddSession(sess); err != nil {
		return nil, fmt.Errorf("failed to add session to state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	m.ensureTrackerFromSession(sess)

	// Track session creation
	m.trackSessionCreated(sess.ID, sess.WorkspaceID, sess.Target)

	return &sess, nil
}

// ResolveTarget resolves a target name to a command and env.
func (m *Manager) ResolveTarget(_ context.Context, targetName string) (ResolvedTarget, error) {
	// Check if it's a model (handles aliases like "opus", "sonnet", "haiku")
	if m.models != nil && m.models.IsModelID(targetName) {
		resolved, err := m.models.ResolveModel(targetName)
		if err == nil {
			return ResolvedTarget{
				Name:       resolved.Model.ID,
				Kind:       TargetKindModel,
				Command:    resolved.Command,
				Promptable: true,
				Env:        resolved.Env,
				Model:      &resolved.Model,
				ToolName:   resolved.ToolName,
			}, nil
		}
		// Model exists in catalog but can't be resolved (e.g., no detected tool).
		// Fall through to builtin tool name check for remote sessions.
	}

	if target, found := m.config.GetRunTarget(targetName); found {
		return ResolvedTarget{
			Name:       target.Name,
			Kind:       TargetKindUser,
			Command:    target.Command,
			Promptable: false,
		}, nil
	}

	// Fallback: builtin tool names (e.g., "claude", "codex") can be resolved
	// directly using the tool name as the command. This handles remote sessions
	// where the tool isn't detected locally but is expected on the remote host.
	if detect.IsBuiltinToolName(targetName) {
		return ResolvedTarget{
			Name:       targetName,
			Kind:       TargetKindModel,
			Command:    targetName,
			Promptable: true,
			ToolName:   targetName,
		}, nil
	}

	return ResolvedTarget{}, fmt.Errorf("target not found: %s", targetName)
}

func buildCommand(target ResolvedTarget, prompt string, model *detect.Model, resume bool, remoteMode bool) (string, error) {
	isRemote := remoteMode
	trimmedPrompt := strings.TrimSpace(prompt)

	// Resolve the base tool name for signaling injection
	baseTool := target.ToolName
	if baseTool == "" {
		if detect.IsBuiltinToolName(target.Name) {
			baseTool = target.Name
		}
	}

	// Handle resume mode
	if resume {
		// For models, use the tool name instead of model ID
		toolName := target.ToolName
		if toolName == "" {
			toolName = target.Name
		}
		parts, err := detect.BuildCommandParts(toolName, target.Command, detect.ToolModeResume, "", model)
		if err != nil {
			return "", err
		}
		cmd := strings.Join(parts, " ")

		// Inject signaling instructions via CLI flag for supported tools
		cmd = appendSignalingFlags(cmd, baseTool, isRemote)

		// Resume mode still needs model env vars for third-party models
		if len(target.Env) > 0 {
			return fmt.Sprintf("%s %s", buildEnvPrefix(target.Env), cmd), nil
		}
		return cmd, nil
	}

	// Build the base command with optional model flag injection
	baseCommand := target.Command
	if model != nil && target.ToolName != "" {
		adapter := detect.GetAdapter(target.ToolName)
		if adapter != nil {
			flag := adapter.ModelFlag()
			if spec, ok := model.RunnerFor(target.ToolName); ok && spec.ModelValue != "" && flag != "" {
				baseCommand = fmt.Sprintf("%s %s %s", baseCommand, flag, shellutil.Quote(spec.ModelValue))
			}
		}
	}

	// Inject signaling instructions via CLI flag for supported tools
	baseCommand = appendSignalingFlags(baseCommand, baseTool, isRemote)

	if target.Promptable {
		if trimmedPrompt == "" {
			return "", fmt.Errorf("prompt is required for target %s", target.Name)
		}
		command := fmt.Sprintf("%s %s", baseCommand, shellutil.Quote(prompt))
		if len(target.Env) > 0 {
			return fmt.Sprintf("%s %s", buildEnvPrefix(target.Env), command), nil
		}
		return command, nil
	}

	if trimmedPrompt != "" {
		return "", fmt.Errorf("prompt is not allowed for command target %s", target.Name)
	}
	if len(target.Env) > 0 {
		return fmt.Sprintf("%s %s", buildEnvPrefix(target.Env), baseCommand), nil
	}
	return baseCommand, nil
}

// appendSignalingFlags appends CLI flags for signaling instruction injection.
// In remote mode, uses inline content (--append-system-prompt) since local file
// paths like ~/.schmux/signaling.md don't exist on the remote host.
// Tools using hooks are skipped because they handle signaling via workspace config.
func appendSignalingFlags(cmd, baseTool string, isRemote bool) string {
	adapter := detect.GetAdapter(baseTool)
	if adapter == nil {
		return cmd
	}

	strategy := adapter.SignalingStrategy()

	if strategy == detect.SignalingHooks {
		// Hooks-based tools handle signaling via workspace config, not CLI flags
		return cmd
	}

	if strategy == detect.SignalingCLIFlag {
		if isRemote {
			// Remote mode: CLI-flag tools reference local file paths that don't exist
			// on the remote host, so skip signaling injection.
			return cmd
		}
		// Local mode: use adapter's signaling args
		sigArgs := adapter.SignalingArgs(ensure.SignalingInstructionsFilePath())
		for _, arg := range sigArgs {
			cmd = fmt.Sprintf("%s %s", cmd, shellutil.Quote(arg))
		}
		return cmd
	}

	// SignalingInstructionFile: no CLI flags needed (handled by ensure package)
	return cmd
}

// appendPersonaFlags injects persona prompt via CLI flag for tools that support it.
// Only tools with PersonaCLIFlag injection method get flags appended.
// Other tools use instruction file append or SpawnEnv for persona injection.
func appendPersonaFlags(cmd, baseTool, personaFilePath string) string {
	adapter := detect.GetAdapter(baseTool)
	if adapter == nil {
		return cmd
	}

	switch adapter.PersonaInjection() {
	case detect.PersonaCLIFlag:
		args := adapter.PersonaArgs(personaFilePath)
		for _, arg := range args {
			cmd = fmt.Sprintf("%s %s", cmd, shellutil.Quote(arg))
		}
		return cmd
	default:
		// PersonaInstructionFile: handled by ensure package (instruction file append)
		// PersonaConfigOverlay: handled by SpawnEnv (environment variable)
		return cmd
	}
}

func buildEnvPrefix(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, shellutil.Quote(env[k])))
	}
	return strings.Join(parts, " ")
}

func mergeEnvMaps(base, overrides map[string]string) map[string]string {
	if base == nil && overrides == nil {
		return nil
	}
	out := make(map[string]string, len(base)+len(overrides))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overrides {
		out[k] = v
	}
	return out
}

// IsRunning checks if the agent process is still running.
// Uses the cached PID from tmux pane, which is more reliable than searching by process name.
// For remote sessions, checks if the remote connection is active.
func (m *Manager) IsRunning(ctx context.Context, sessionID string) bool {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return false
	}

	// Handle remote sessions
	if sess.IsRemoteSession() {
		if m.remoteManager == nil {
			return false
		}
		conn := m.remoteManager.GetConnection(sess.RemoteHostID)
		if conn == nil || !conn.IsConnected() {
			return false
		}
		// Connection is active - check if session has been created
		// Session is only running if it has a RemotePaneID (created on the remote host)
		// If pane ID is empty, the session is still provisioning
		return sess.RemotePaneID != ""
	}

	// Local session handling
	// If we don't have a PID, check if tmux session exists as fallback
	if sess.Pid == 0 {
		return tmux.SessionExists(ctx, sess.TmuxSession)
	}

	// Check if the process is still running
	process, err := os.FindProcess(sess.Pid)
	if err != nil {
		return false
	}

	// Send signal 0 to check if process exists
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return false
	}

	return true
}

// Dispose disposes of a session.
func (m *Manager) Dispose(ctx context.Context, sessionID string) error {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Handle remote sessions
	if sess.IsRemoteSession() {
		return m.disposeRemoteSession(ctx, sess)
	}

	// Capture terminal output BEFORE killing the session
	if m.terminalCaptureCallback != nil && ctx.Err() == nil {
		captureCtx, captureCancel := context.WithTimeout(ctx, 5*time.Second)
		output, err := tmux.CaptureOutput(captureCtx, sess.TmuxSession)
		captureCancel()
		if err != nil {
			m.logger.Warn("failed to capture terminal output", "session", sessionID, "err", err)
		} else if output != "" {
			m.terminalCaptureCallback(sessionID, sess.WorkspaceID, output)
		}
	}

	// Kill tmux session (tmux sends SIGHUP to all processes - handles cleanup)
	// If session exists but kill fails, return error to avoid orphaning processes
	if tmux.SessionExists(ctx, sess.TmuxSession) {
		if err := tmux.KillSession(ctx, sess.TmuxSession); err != nil {
			return fmt.Errorf("failed to kill tmux session %s: %w", sess.TmuxSession, err)
		}
	}

	m.stopTracker(sessionID)

	// Note: workspace is NOT cleaned up on session disposal.
	// Workspaces persist and are only reset when reused for a new spawn.

	// Remove session from state
	if err := m.state.RemoveSession(sessionID); err != nil {
		return fmt.Errorf("failed to remove session from state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	// Notify compounder if this was the last session for the workspace
	isLastSession := true
	for _, s := range m.state.GetSessions() {
		if s.WorkspaceID == sess.WorkspaceID {
			isLastSession = false
			break
		}
	}
	if m.compoundCallback != nil && isLastSession {
		m.compoundCallback(sess.WorkspaceID, false)
	}

	// Notify lore system on session dispose (always fires; daemon decides based on config)
	if m.loreCallback != nil {
		w, found := m.state.GetWorkspace(sess.WorkspaceID)
		if found {
			// Find repo name from URL
			repoConfig, repoFound := m.config.FindRepoByURL(w.Repo)
			if repoFound {
				go m.loreCallback(repoConfig.Name, w.Repo, isLastSession)
			}
		}
	}

	m.logger.Info("disposed session", "session", sessionID)

	return nil
}

// disposeRemoteSession disposes of a remote session via control mode.
func (m *Manager) disposeRemoteSession(ctx context.Context, sess state.Session) error {
	var warnings []string
	windowKilled := false

	// Kill the remote window via control mode if connected
	if m.remoteManager != nil {
		conn := m.remoteManager.GetConnection(sess.RemoteHostID)
		if conn != nil && conn.IsConnected() && sess.RemoteWindow != "" {
			if err := conn.KillSession(ctx, sess.RemoteWindow); err != nil {
				warnings = append(warnings, fmt.Sprintf("failed to kill remote window: %v", err))
			} else {
				windowKilled = true
			}
		}
	}

	// Stop signal monitor for remote session
	m.StopRemoteSignalMonitor(sess.ID)

	// DO NOT remove the workspace for remote sessions - it's shared across all
	// sessions on the same remote host. The workspace persists until the host
	// is disconnected or expired.

	// Remove session from state
	if err := m.state.RemoveSession(sess.ID); err != nil {
		return fmt.Errorf("failed to remove session from state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	// Print summary
	summary := fmt.Sprintf("Disposed remote session %s", sess.ID)
	if windowKilled {
		summary += " (killed remote window)"
	}
	m.logger.Info(summary)

	// Print warnings if any
	for _, w := range warnings {
		m.logger.Warn(w)
	}

	return nil
}

// GetAttachCommand returns the tmux attach command for a session.
func (m *Manager) GetAttachCommand(sessionID string) (string, error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	return tmux.GetAttachCommand(sess.TmuxSession), nil
}

// GetOutput returns the current terminal output for a session.
func (m *Manager) GetOutput(ctx context.Context, sessionID string) (string, error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	return tmux.CaptureOutput(ctx, sess.TmuxSession)
}

// GetAllSessions returns all sessions.
func (m *Manager) GetAllSessions() []state.Session {
	return m.state.GetSessions()
}

// GetSession returns a session by ID.
func (m *Manager) GetSession(sessionID string) (*state.Session, error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return &sess, nil
}

// RenameSession updates a session's nickname and renames the tmux session.
// The nickname is sanitized before use as the tmux session name.
// Returns an error if the new nickname conflicts with an existing session.
func (m *Manager) RenameSession(ctx context.Context, sessionID, newNickname string) error {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Check if new nickname conflicts with an existing session
	if conflictingID := m.nicknameExists(newNickname, sessionID); conflictingID != "" {
		return fmt.Errorf("nickname %q already in use by session %s", newNickname, conflictingID)
	}

	oldTmuxName := sess.TmuxSession
	newTmuxName := oldTmuxName
	if newNickname != "" {
		newTmuxName = sanitizeNickname(newNickname)
	}

	// Rename the tmux session
	if err := tmux.RenameSession(ctx, oldTmuxName, newTmuxName); err != nil {
		return fmt.Errorf("failed to rename tmux session: %w", err)
	}

	// Update session state
	sess.Nickname = newNickname
	sess.TmuxSession = newTmuxName
	if err := m.state.UpdateSession(sess); err != nil {
		return fmt.Errorf("failed to update session in state: %w", err)
	}
	if err := m.state.Save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	m.updateTrackerSessionName(sessionID, newTmuxName)

	return nil
}

// sanitizeNickname sanitizes a nickname for use as a tmux session name.
// tmux session names cannot contain dots (.) or colons (:).
func sanitizeNickname(nickname string) string {
	result := strings.ReplaceAll(nickname, ".", "-")
	result = strings.ReplaceAll(result, ":", "-")
	return result
}

// nicknameExists checks if a nickname (or its sanitized tmux session name) already exists.
// Returns the conflicting session ID if found, empty string otherwise.
// excludeSessionID is used during rename to skip the session being renamed.
func (m *Manager) nicknameExists(nickname, excludeSessionID string) string {
	if nickname == "" {
		return ""
	}
	tmuxName := sanitizeNickname(nickname)
	sessions := m.state.GetSessions()
	for _, sess := range sessions {
		// Skip the session being edited (for rename operations)
		if sess.ID == excludeSessionID {
			continue
		}
		// Check if tmux session name matches (nicknames are sanitized for tmux)
		if sess.TmuxSession == tmuxName {
			return sess.ID
		}
	}
	return ""
}

// generateUniqueNickname generates a unique nickname by trying the base name,
// then "name (1)", "name (2)", etc. until a unique name is found.
func (m *Manager) generateUniqueNickname(baseNickname string) string {
	if baseNickname == "" {
		return ""
	}
	// Try base name first
	if m.nicknameExists(baseNickname, "") == "" {
		return baseNickname
	}
	// Try numbered suffixes
	for i := 1; i <= maxNicknameAttempts; i++ {
		candidate := fmt.Sprintf("%s (%d)", baseNickname, i)
		if m.nicknameExists(candidate, "") == "" {
			return candidate
		}
	}
	// Fallback: use base nickname with a UUID suffix (should never happen in practice)
	return fmt.Sprintf("%s-%s", baseNickname, uuid.New().String()[:8])
}

// EnsureTracker makes sure a running tracker exists for the session.
func (m *Manager) EnsureTracker(sessionID string) error {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	m.ensureTrackerFromSession(sess)
	return nil
}

// GetTracker returns the tracker for a session, creating one if needed.
func (m *Manager) GetTracker(sessionID string) (*SessionTracker, error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return m.ensureTrackerFromSession(sess), nil
}

func (m *Manager) ensureTrackerFromSession(sess state.Session) *SessionTracker {
	m.mu.Lock()
	if existing := m.trackers[sess.ID]; existing != nil {
		existing.SetTmuxSession(sess.TmuxSession)
		m.mu.Unlock()
		return existing
	}

	// Build event file path from workspace path
	var eventFilePath string
	if ws, found := m.workspace.GetByID(sess.WorkspaceID); found && ws.Path != "" {
		eventFilePath = filepath.Join(ws.Path, ".schmux", "events", sess.ID+".jsonl")
	}

	var outputCb func([]byte)
	if m.outputCallback != nil {
		sessionID := sess.ID
		handler := m.outputCallback
		outputCb = func(chunk []byte) {
			handler(sessionID, chunk)
		}
	}
	tracker := NewSessionTracker(sess.ID, sess.TmuxSession, m.state, eventFilePath, m.eventHandlers, outputCb, m.logger)
	m.trackers[sess.ID] = tracker

	m.mu.Unlock()

	tracker.Start()
	return tracker
}

// Stop stops all running session trackers, killing their tmux attach-client processes.
func (m *Manager) Stop() {
	m.mu.Lock()
	trackers := m.trackers
	m.trackers = make(map[string]*SessionTracker)
	m.mu.Unlock()
	for _, tracker := range trackers {
		tracker.Stop()
	}
}

func (m *Manager) stopTracker(sessionID string) {
	m.mu.Lock()
	tracker := m.trackers[sessionID]
	delete(m.trackers, sessionID)
	m.mu.Unlock()
	if tracker != nil {
		tracker.Stop()
	}
}

func (m *Manager) updateTrackerSessionName(sessionID, tmuxSession string) {
	m.mu.RLock()
	tracker := m.trackers[sessionID]
	m.mu.RUnlock()
	if tracker != nil {
		tracker.SetTmuxSession(tmuxSession)
	}
}

// PruneLogFiles removes log files in logsDir that don't belong to any active session.
// Active session IDs are provided as a set. Log files are expected to be named "{sessionID}.log".
func PruneLogFiles(logsDir string, activeIDs map[string]bool) (removed int) {
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".log")
		if !activeIDs[sessionID] {
			logPath := filepath.Join(logsDir, entry.Name())
			if os.Remove(logPath) == nil {
				removed++
			}
		}
	}
	return removed
}
