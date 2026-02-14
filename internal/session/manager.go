package session

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/provision"
	"github.com/sergeknystautas/schmux/internal/remote"
	"github.com/sergeknystautas/schmux/internal/signal"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/tmux"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

const (
	// maxNicknameAttempts is the maximum number of attempts to find a unique nickname
	// before falling back to a UUID suffix.
	maxNicknameAttempts = 100

	// processKillGracePeriod is how long to wait for SIGTERM before SIGKILL
	processKillGracePeriod = 100 * time.Millisecond
)

// Manager manages sessions.
type Manager struct {
	config          *config.Config
	state           state.StateStore
	workspace       workspace.WorkspaceManager
	remoteManager   *remote.Manager // Optional, for remote sessions
	signalCallback  func(sessionID string, sig signal.Signal)
	outputCallback  func(sessionID string, chunk []byte)
	trackers        map[string]*SessionTracker
	remoteDetectors map[string]*remoteSignalMonitor // signal detectors for remote sessions
	mu              sync.RWMutex
}

// remoteSignalMonitor holds a signal detector and its stop channel for a remote session.
type remoteSignalMonitor struct {
	detector *signal.SignalDetector
	stopCh   chan struct{}
}

// ResolvedTarget is a resolved run target with command and env info.
type ResolvedTarget struct {
	Name       string
	Kind       string
	Command    string
	Promptable bool
	Env        map[string]string
	Model      *detect.Model
}

const (
	TargetKindDetected = "detected"
	TargetKindModel    = "model"
	TargetKindUser     = "user"
)

// New creates a new session manager.
func New(cfg *config.Config, st state.StateStore, statePath string, wm workspace.WorkspaceManager) *Manager {
	return &Manager{
		config:          cfg,
		state:           st,
		workspace:       wm,
		trackers:        make(map[string]*SessionTracker),
		remoteDetectors: make(map[string]*remoteSignalMonitor),
		remoteManager:   nil,
	}
}

// SetRemoteManager sets the remote manager for remote session support.
func (m *Manager) SetRemoteManager(rm *remote.Manager) {
	m.remoteManager = rm
}

// SetSignalCallback sets the callback for signal detection from session output.
func (m *Manager) SetSignalCallback(cb func(sessionID string, sig signal.Signal)) {
	m.signalCallback = cb
}

// SetOutputCallback sets callback for terminal output chunks from local trackers.
func (m *Manager) SetOutputCallback(cb func(sessionID string, chunk []byte)) {
	m.outputCallback = cb
}

// StartRemoteSignalMonitor creates a signal detector for a remote session and starts
// a goroutine that subscribes to the remote output channel.
// The goroutine automatically reconnects when the output channel closes (e.g., due to
// a dropped remote connection), retrying until explicitly stopped via StopRemoteSignalMonitor.
func (m *Manager) StartRemoteSignalMonitor(sess state.Session) {
	if m.remoteManager == nil || sess.RemotePaneID == "" || sess.RemoteHostID == "" {
		return
	}
	if m.signalCallback == nil {
		return
	}

	m.mu.Lock()
	if m.remoteDetectors[sess.ID] != nil {
		m.mu.Unlock()
		return // already running
	}

	sessionID := sess.ID
	hostID := sess.RemoteHostID
	paneID := sess.RemotePaneID
	signalCb := m.signalCallback
	stopCh := make(chan struct{})
	m.remoteDetectors[sess.ID] = &remoteSignalMonitor{
		detector: nil, // created fresh on each connection
		stopCh:   stopCh,
	}
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.remoteDetectors, sessionID)
			m.mu.Unlock()
		}()

		flushTicker := time.NewTicker(signal.FlushTimeout)
		defer flushTicker.Stop()

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
				// Not connected yet — wait 2s and retry
				select {
				case <-stopCh:
					return
				case <-time.After(2 * time.Second):
					continue
				}
			}

			// Create a fresh detector for this connection attempt
			detector := signal.NewSignalDetector(sessionID, func(sig signal.Signal) {
				signalCb(sessionID, sig)
			})
			detector.SetNearMissCallback(func(line string) {
				fmt.Printf("[signal] %s - potential missed signal (remote): %q\n", sessionID, line)
			})

			// Update the monitor's detector reference
			m.mu.Lock()
			if mon := m.remoteDetectors[sessionID]; mon != nil {
				mon.detector = detector
			}
			m.mu.Unlock()

			// Parse scrollback for missed signals — suppress callback to avoid
			// re-emitting old signals from before the daemon restart.
			detector.Suppress(true)
			capCtx, capCancel := context.WithTimeout(context.Background(), 2*time.Second)
			scrollback, err := conn.CapturePaneLines(capCtx, paneID, 200)
			capCancel()
			if err == nil && scrollback != "" {
				detector.Feed([]byte(scrollback))
				detector.Flush()
			}
			detector.Suppress(false)

			// Recover signals emitted during daemon downtime: if the most recent
			// signal from scrollback differs from the stored nudge, fire the
			// callback so the dashboard reflects the agent's actual state.
			if lastSig := detector.LastSignal(); lastSig != nil {
				storedSess, ok := m.state.GetSession(sessionID)
				if ok {
					nudgeState := signal.MapStateToNudge(lastSig.State)
					if storedSess.Nudge != nudgeState {
						detector.EmitSignal(*lastSig)
					}
				}
			}

			// Subscribe to output
			outputCh := conn.SubscribeOutput(paneID)

		inner:
			for {
				select {
				case <-stopCh:
					detector.Flush()
					conn.UnsubscribeOutput(paneID, outputCh)
					return
				case event, ok := <-outputCh:
					if !ok {
						detector.Flush()
						break inner
					}
					if event.Data != "" {
						detector.Feed([]byte(event.Data))
					}
				case <-flushTicker.C:
					if detector.ShouldFlush() {
						detector.Flush()
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

// StopRemoteSignalMonitor stops the signal detector goroutine for a remote session.
func (m *Manager) StopRemoteSignalMonitor(sessionID string) {
	m.mu.Lock()
	mon := m.remoteDetectors[sessionID]
	delete(m.remoteDetectors, sessionID)
	m.mu.Unlock()
	if mon != nil {
		close(mon.stopCh)
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
	})

	// Build command with remote mode (uses inline content instead of local file paths)
	command, err := buildCommand(resolved, prompt, nil, false, true)
	if err != nil {
		return nil, err
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
		// Queue the session creation
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
				// Re-read session from state to avoid overwriting concurrent changes.
				current, ok := m.state.GetSession(sessionID)
				if !ok {
					fmt.Printf("[session] queued session %s: session no longer in state\n", sessionID)
					return
				}
				if result.Error != nil {
					fmt.Printf("[session] queued session %s failed: %v\n", sessionID, result.Error)
					current.Status = "failed"
				} else {
					fmt.Printf("[session] queued session %s succeeded (window=%s, pane=%s)\n",
						sessionID, result.WindowID, result.PaneID)
					current.Status = "running"
					current.RemoteWindow = result.WindowID
					current.RemotePaneID = result.PaneID
				}
				if err := m.state.UpdateSession(current); err != nil {
					fmt.Printf("[session] queued session %s: failed to update state: %v\n", sessionID, err)
				}
				if err := m.state.Save(); err != nil {
					fmt.Printf("[session] queued session %s: failed to save state: %v\n", sessionID, err)
				}
				if current.Status == "running" {
					m.StartRemoteSignalMonitor(current)
				}
			case <-ctx.Done():
				return
			}
		}()

		return &sess, nil
	}

	// Connected - create immediately (existing code path)
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

	return &sess, nil
}

// Spawn creates a new session.
// If workspaceID is provided, spawn into that specific workspace (Existing Directory Spawn mode).
// If workspaceID is provided with newBranch, create a new workspace branching from the source workspace's branch.
// Otherwise, find or create a workspace by repoURL/branch.
// nickname is an optional human-friendly name for the session.
// prompt is only used if the target is promptable.
// resume enables resume mode, which uses the agent's resume command instead of a prompt.
func (m *Manager) Spawn(ctx context.Context, repoURL, branch, targetName, prompt, nickname string, workspaceID string, resume bool, newBranch string) (*state.Session, error) {
	resolved, err := m.ResolveTarget(ctx, targetName)
	if err != nil {
		return nil, err
	}

	var w *state.Workspace

	if workspaceID != "" && newBranch != "" {
		// Create new workspace branching from source workspace's branch
		w, err = m.workspace.CreateFromWorkspace(ctx, workspaceID, newBranch)
		if err != nil {
			return nil, fmt.Errorf("failed to create workspace from source: %w", err)
		}
	} else if workspaceID != "" {
		// Spawn into specific workspace (Existing Directory Spawn mode - no git operations)
		ws, found := m.workspace.GetByID(workspaceID)
		if !found {
			return nil, fmt.Errorf("workspace not found: %s", workspaceID)
		}
		w = ws
	} else {
		// Get or create workspace (includes fetch/pull/clean)
		w, err = m.workspace.GetOrCreate(ctx, repoURL, branch)
		if err != nil {
			return nil, fmt.Errorf("failed to get workspace: %w", err)
		}
	}

	// Provision agent instruction files with signaling instructions
	// Only for agents that don't support CLI-based instruction injection
	baseTool := detect.GetBaseToolName(targetName)
	if provision.SupportsSystemPromptFlag(baseTool) {
		if err := provision.EnsureSignalingInstructionsFile(); err != nil {
			// Log warning but don't fail spawn - signaling is optional
			fmt.Printf("[session] warning: failed to ensure signaling instructions file: %v\n", err)
		}
	} else {
		if err := provision.EnsureAgentInstructions(w.Path, targetName); err != nil {
			// Log warning but don't fail spawn - signaling is optional
			fmt.Printf("[session] warning: failed to provision agent instructions: %v\n", err)
		}
	}

	// Resolve model if target is a model kind
	var model *detect.Model
	if resolved.Kind == TargetKindModel {
		if m, ok := detect.FindModel(resolved.Name); ok {
			model = &m
		}
	}

	// Create session ID
	sessionID := fmt.Sprintf("%s-%s", w.ID, uuid.New().String()[:8])

	// Inject schmux signaling environment variables
	resolved.Env = mergeEnvMaps(resolved.Env, map[string]string{
		"SCHMUX_ENABLED":      "1",
		"SCHMUX_SESSION_ID":   sessionID,
		"SCHMUX_WORKSPACE_ID": w.ID,
	})

	command, err := buildCommand(resolved, prompt, model, resume)
	if err != nil {
		return nil, err
	}

	// Generate unique nickname if provided (auto-suffix if duplicate)
	uniqueNickname := nickname
	if nickname != "" {
		uniqueNickname = m.generateUniqueNickname(nickname)
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

	// Force fixed window size for deterministic TUI output
	width, height := m.config.GetTerminalSize()
	if err := tmux.SetWindowSizeManual(ctx, tmuxSession); err != nil {
		fmt.Printf("[session] warning: failed to set manual window size: %v\n", err)
	}
	if err := tmux.ResizeWindow(ctx, tmuxSession, width, height); err != nil {
		fmt.Printf("[session] warning: failed to resize window: %v\n", err)
	}

	// Configure status bar: process on left, time on right, clear center
	if err := tmux.SetOption(ctx, tmuxSession, "status-left", "#{pane_current_command} "); err != nil {
		fmt.Printf("[session] warning: failed to set status-left: %v\n", err)
	}
	if err := tmux.SetOption(ctx, tmuxSession, "window-status-format", ""); err != nil {
		fmt.Printf("[session] warning: failed to set window-status-format: %v\n", err)
	}
	if err := tmux.SetOption(ctx, tmuxSession, "window-status-current-format", ""); err != nil {
		fmt.Printf("[session] warning: failed to set window-status-current-format: %v\n", err)
	}
	if err := tmux.SetOption(ctx, tmuxSession, "status-right", ""); err != nil {
		fmt.Printf("[session] warning: failed to set status-right: %v\n", err)
	}

	// Get the PID of the agent process from tmux pane
	pid, err := tmux.GetPanePID(ctx, tmuxSession)
	if err != nil {
		return nil, fmt.Errorf("failed to get pane PID: %w", err)
	}

	// Create session state with cached PID (no Prompt field)
	sess := state.Session{
		ID:          sessionID,
		WorkspaceID: w.ID,
		Target:      targetName,
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

	return &sess, nil
}

// SpawnCommand spawns a session running a raw shell command.
// Used for quick launch presets with a direct command (no target resolution).
func (m *Manager) SpawnCommand(ctx context.Context, repoURL, branch, command, nickname, workspaceID string, newBranch string) (*state.Session, error) {
	var w *state.Workspace
	var err error

	if workspaceID != "" && newBranch != "" {
		// Create new workspace branching from source workspace's branch
		w, err = m.workspace.CreateFromWorkspace(ctx, workspaceID, newBranch)
		if err != nil {
			return nil, fmt.Errorf("failed to create workspace from source: %w", err)
		}
	} else if workspaceID != "" {
		// Spawn into specific workspace (Existing Directory Spawn mode - no git operations)
		ws, found := m.workspace.GetByID(workspaceID)
		if !found {
			return nil, fmt.Errorf("workspace not found: %s", workspaceID)
		}
		w = ws
	} else {
		// Get or create workspace (includes fetch/pull/clean)
		w, err = m.workspace.GetOrCreate(ctx, repoURL, branch)
		if err != nil {
			return nil, fmt.Errorf("failed to get workspace: %w", err)
		}
	}

	// Create session ID
	sessionID := fmt.Sprintf("%s-%s", w.ID, uuid.New().String()[:8])

	// Inject schmux signaling environment variables into the command
	schmuxEnv := map[string]string{
		"SCHMUX_ENABLED":      "1",
		"SCHMUX_SESSION_ID":   sessionID,
		"SCHMUX_WORKSPACE_ID": w.ID,
	}
	commandWithEnv := fmt.Sprintf("%s %s", buildEnvPrefix(schmuxEnv), command)

	// Generate unique nickname if provided (auto-suffix if duplicate)
	uniqueNickname := nickname
	if nickname != "" {
		uniqueNickname = m.generateUniqueNickname(nickname)
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

	// Force fixed window size for deterministic TUI output
	width, height := m.config.GetTerminalSize()
	if err := tmux.SetWindowSizeManual(ctx, tmuxSession); err != nil {
		fmt.Printf("[session] warning: failed to set manual window size: %v\n", err)
	}
	if err := tmux.ResizeWindow(ctx, tmuxSession, width, height); err != nil {
		fmt.Printf("[session] warning: failed to resize window: %v\n", err)
	}

	// Configure status bar: process on left, time on right, clear center
	if err := tmux.SetOption(ctx, tmuxSession, "status-left", "#{pane_current_command} "); err != nil {
		fmt.Printf("[session] warning: failed to set status-left: %v\n", err)
	}
	if err := tmux.SetOption(ctx, tmuxSession, "window-status-format", ""); err != nil {
		fmt.Printf("[session] warning: failed to set window-status-format: %v\n", err)
	}
	if err := tmux.SetOption(ctx, tmuxSession, "window-status-current-format", ""); err != nil {
		fmt.Printf("[session] warning: failed to set window-status-current-format: %v\n", err)
	}
	if err := tmux.SetOption(ctx, tmuxSession, "status-right", ""); err != nil {
		fmt.Printf("[session] warning: failed to set status-right: %v\n", err)
	}

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

	return &sess, nil
}

// ResolveTarget resolves a target name to a command and env.
func (m *Manager) ResolveTarget(_ context.Context, targetName string) (ResolvedTarget, error) {
	// Check if it's a model (handles aliases like "opus", "sonnet", "haiku")
	model, ok := detect.FindModel(targetName)
	if ok {
		// Apply version override from config if present
		if override := m.config.GetModelVersion(model.ID); override != "" {
			model.ModelValue = override
		}

		// Verify the base tool is detected
		detectedTools := config.DetectedToolsFromConfig(m.config)
		baseToolDetected := false
		for _, tool := range detectedTools {
			if tool.Name == model.BaseTool {
				baseToolDetected = true
				break
			}
		}
		if !baseToolDetected {
			return ResolvedTarget{}, fmt.Errorf("model %s requires base tool %s which is not available", model.ID, model.BaseTool)
		}
		baseTarget, found := m.config.GetDetectedRunTarget(model.BaseTool)
		if !found {
			return ResolvedTarget{}, fmt.Errorf("model %s requires base tool %s which is not available", model.ID, model.BaseTool)
		}
		secrets, err := config.GetEffectiveModelSecrets(model)
		if err != nil {
			return ResolvedTarget{}, fmt.Errorf("failed to load secrets for model %s: %w", model.ID, err)
		}
		if err := ensureModelSecrets(model, secrets); err != nil {
			return ResolvedTarget{}, err
		}
		env := mergeEnvMaps(model.BuildEnv(), secrets)
		return ResolvedTarget{
			Name:       model.ID,
			Kind:       TargetKindModel,
			Command:    baseTarget.Command,
			Promptable: true,
			Env:        env,
			Model:      &model,
		}, nil
	}

	if target, found := m.config.GetRunTarget(targetName); found {
		kind := TargetKindUser
		if target.Source == config.RunTargetSourceDetected {
			kind = TargetKindDetected
		}
		return ResolvedTarget{
			Name:       target.Name,
			Kind:       kind,
			Command:    target.Command,
			Promptable: target.Type == config.RunTargetTypePromptable,
		}, nil
	}

	return ResolvedTarget{}, fmt.Errorf("target not found: %s", targetName)
}

// shellQuote quotes a string for safe use in shell commands using single quotes.
// Single quotes preserve everything literally, including newlines.
// Embedded single quotes are handled with the '\” trick.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func buildCommand(target ResolvedTarget, prompt string, model *detect.Model, resume bool, remoteMode ...bool) (string, error) {
	isRemote := len(remoteMode) > 0 && remoteMode[0]
	trimmedPrompt := strings.TrimSpace(prompt)

	// Resolve the base tool name for signaling injection
	baseTool := detect.GetBaseToolName(target.Name)
	if model != nil {
		baseTool = model.BaseTool
	}

	// Handle resume mode
	if resume {
		// For models, use the base tool name instead of model ID
		toolName := target.Name
		if model != nil {
			toolName = model.BaseTool
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
	if model != nil && model.ModelFlag != "" {
		// Inject model flag for tools like Codex that use CLI flags instead of env vars
		baseCommand = fmt.Sprintf("%s %s %s", baseCommand, model.ModelFlag, shellQuote(model.ModelValue))
	}

	// Inject signaling instructions via CLI flag for supported tools
	baseCommand = appendSignalingFlags(baseCommand, baseTool, isRemote)

	if target.Promptable {
		if trimmedPrompt == "" {
			return "", fmt.Errorf("prompt is required for target %s", target.Name)
		}
		command := fmt.Sprintf("%s %s", baseCommand, shellQuote(prompt))
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
func appendSignalingFlags(cmd, baseTool string, isRemote bool) string {
	if !provision.SupportsSystemPromptFlag(baseTool) {
		return cmd
	}
	if isRemote {
		// Remote mode: always use inline content (file paths are local-only)
		switch baseTool {
		case "claude":
			return fmt.Sprintf("%s --append-system-prompt %s", cmd, shellQuote(provision.SignalingInstructions))
		default:
			// Codex and others: no reliable remote inline injection mechanism
			return cmd
		}
	}
	// Local mode: use file-based injection
	switch baseTool {
	case "claude":
		return fmt.Sprintf("%s --append-system-prompt-file %s", cmd, shellQuote(provision.SignalingInstructionsFilePath()))
	case "codex":
		return fmt.Sprintf("%s -c %s", cmd, shellQuote("model_instructions_file="+provision.SignalingInstructionsFilePath()))
	default:
		return fmt.Sprintf("%s --append-system-prompt %s", cmd, shellQuote(provision.SignalingInstructions))
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
		parts = append(parts, fmt.Sprintf("%s=%s", k, shellQuote(env[k])))
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

func ensureModelSecrets(model detect.Model, secrets map[string]string) error {
	return config.EnsureModelSecrets(model, secrets)
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

// killProcessGroup kills a process and its entire process group.
// On Unix, a negative PID to syscall.Kill signals the entire process group.
func killProcessGroup(pid int) error {
	// First try to kill the process group (negative PID)
	// Use syscall.Kill directly since os.FindProcess doesn't handle negative PIDs correctly
	if err := syscall.Kill(-pid, syscall.SIGTERM); err == nil {
		// Successfully sent SIGTERM to process group
		// Wait for graceful shutdown
		time.Sleep(processKillGracePeriod)

		// Check if process group is still alive and force kill if needed
		if err := syscall.Kill(-pid, syscall.Signal(0)); err == nil {
			// Process group still exists, send SIGKILL
			if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
				// Log but don't fail - process may have exited
				fmt.Printf("[session] warning: failed to send SIGKILL to process group %d: %v\n", -pid, err)
			}
		}
		return nil
	}

	// Fallback: process group may not exist, try killing the process directly
	// Check if process exists first
	if err := syscall.Kill(pid, syscall.Signal(0)); err != nil {
		// Process doesn't exist, nothing to do
		return nil
	}

	// Send SIGTERM for graceful shutdown
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		// Process may already be dead, which is fine
		return nil
	}

	// Wait for graceful shutdown
	time.Sleep(processKillGracePeriod)

	// Force kill if still running
	if err := syscall.Kill(pid, syscall.Signal(0)); err == nil {
		// Process still exists, send SIGKILL
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			fmt.Printf("[session] warning: failed to send SIGKILL to process %d: %v\n", pid, err)
		}
	}

	return nil
}

// findProcessesInWorkspace finds all processes with a working directory in the given workspace path.
// Returns a list of PIDs. Returns empty slice if no processes found (not an error).
func findProcessesInWorkspace(workspacePath string) ([]int, error) {
	// Normalize workspace path for proper matching
	workspacePath = filepath.Clean(workspacePath)
	// Ensure path ends with separator for proper prefix matching
	workspacePrefix := workspacePath + string(filepath.Separator)

	// Use ps to find processes with cwd matching the workspace path
	cmd := exec.Command("ps", "-eo", "pid,cwd")
	output, err := cmd.Output()
	// If ps fails, return empty (no processes to kill)
	// This handles cases where ps returns exit status 1 due to no matches
	if err != nil {
		return nil, nil
	}

	var pids []int
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pidStr := fields[0]
		cwd := fields[1]

		// Check if the working directory matches or is within the workspace
		// Use proper path separator to avoid matching similar paths (e.g., /workspace vs /workspace-backup)
		if cwd == workspacePath || strings.HasPrefix(cwd, workspacePrefix) {
			pid, err := strconv.Atoi(pidStr)
			if err != nil {
				continue
			}
			pids = append(pids, pid)
		}
	}

	return pids, nil
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

	// Track what we've done for the summary
	var warnings []string
	processesKilled := 0
	orphanKilled := 0
	tmuxKilled := false

	// Get the workspace for process cleanup fallback
	ws, found := m.workspace.GetByID(sess.WorkspaceID)
	if found {
		// Step 1: Kill the tracked process group (if we have a PID)
		if sess.Pid > 0 {
			if err := killProcessGroup(sess.Pid); err != nil {
				warnings = append(warnings, fmt.Sprintf("failed to kill process group %d: %v", sess.Pid, err))
			} else {
				processesKilled = 1
			}
		}

		// Step 2: Fallback - find and kill any orphaned processes in the workspace directory
		// This catches processes that may have escaped the process group
		// Check context before doing expensive process scan
		if ctx.Err() == nil {
			orphanPIDs, _ := findProcessesInWorkspace(ws.Path)
			for _, pid := range orphanPIDs {
				// Check context before each kill
				if ctx.Err() != nil {
					break
				}
				// Skip the tracked PID since we already tried to kill it
				if sess.Pid > 0 && pid == sess.Pid {
					continue
				}
				if err := killProcessGroup(pid); err != nil {
					warnings = append(warnings, fmt.Sprintf("failed to kill orphaned process %d: %v", pid, err))
				} else {
					orphanKilled++
				}
			}
		}
	}

	// Step 3: Kill tmux session (ignore error if already gone - that's success)
	if err := tmux.KillSession(ctx, sess.TmuxSession); err == nil {
		tmuxKilled = true
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

	// Print summary
	summary := fmt.Sprintf("Disposed session %s: killed %d process group", sessionID, processesKilled)
	if orphanKilled > 0 {
		summary += fmt.Sprintf(" + %d orphaned process(es)", orphanKilled)
	}
	if tmuxKilled {
		summary += " + tmux session"
	}
	fmt.Printf("[session] %s\n", summary)

	// Print warnings if any
	for _, w := range warnings {
		fmt.Printf("[session]   warning: %s\n", w)
	}

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
	fmt.Printf("[session] %s\n", summary)

	// Print warnings if any
	for _, w := range warnings {
		fmt.Printf("[session]   warning: %s\n", w)
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

	// Build signal file path from workspace path
	var signalFilePath string
	if ws, found := m.workspace.GetByID(sess.WorkspaceID); found && ws.Path != "" {
		signalFilePath = filepath.Join(ws.Path, ".schmux", "signal")
	}

	var cb func(signal.Signal)
	if m.signalCallback != nil {
		sessionID := sess.ID
		signalCb := m.signalCallback
		cb = func(sig signal.Signal) {
			signalCb(sessionID, sig)
		}
	}
	var outputCb func([]byte)
	if m.outputCallback != nil {
		sessionID := sess.ID
		handler := m.outputCallback
		outputCb = func(chunk []byte) {
			handler(sessionID, chunk)
		}
	}
	tracker := NewSessionTracker(sess.ID, sess.TmuxSession, m.state, signalFilePath, cb, outputCb)
	m.trackers[sess.ID] = tracker

	// Recover signal state from file (replaces scrollback recovery)
	if fw := tracker.fileWatcher; fw != nil {
		if lastSig := fw.ReadCurrent(); lastSig != nil {
			storedSess, ok := m.state.GetSession(sess.ID)
			if ok {
				nudgeState := signal.MapStateToNudge(lastSig.State)
				if storedSess.Nudge != nudgeState && cb != nil {
					cb(*lastSig)
				}
			}
		}
	}

	m.mu.Unlock()
	tracker.Start()
	return tracker
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
