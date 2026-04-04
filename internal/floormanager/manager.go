package floormanager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/tmux"
	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

const (
	tmuxSessionName = "schmux-floor-manager"
	monitorInterval = 15 * time.Second
	restartDelay    = 3 * time.Second
	shiftTimeout    = 30 * time.Second
)

// Manager manages the floor manager singleton agent session.
type Manager struct {
	cfg    *config.Config
	sm     *session.Manager // used only for ResolveTarget and session name lookups
	server *tmux.TmuxServer
	logger *log.Logger

	workDir     string // ~/.schmux/floor-manager/
	sessionName string // tmux session name (defaults to tmuxSessionName constant)
	schmuxBin   string // absolute path to the current schmux binary

	mu             sync.Mutex
	tmuxSession    string
	injectionCount int
	rotating       bool
	stopCh         chan struct{}
	stopped        bool

	// tracker streams terminal output for the dashboard WebSocket.
	tracker *session.SessionRuntime

	// shiftDone is signaled when schmux end-shift is called
	shiftDone chan struct{}
}

// New creates a new floor manager Manager.
func New(cfg *config.Config, sm *session.Manager, server *tmux.TmuxServer, homeDir string, logger *log.Logger) *Manager {
	// Resolve the path to the currently running schmux binary so the FM
	// invokes the same build rather than whatever "schmux" is on PATH.
	schmuxBin := "schmux" // fallback
	if exe, err := os.Executable(); err == nil {
		schmuxBin = exe
	}

	return &Manager{
		cfg:         cfg,
		sm:          sm,
		server:      server,
		logger:      logger,
		workDir:     filepath.Join(homeDir, ".schmux", "floor-manager"),
		sessionName: tmuxSessionName,
		schmuxBin:   schmuxBin,
		stopCh:      make(chan struct{}),
	}
}

// Start spawns the floor manager session and starts the monitor goroutine.
func (m *Manager) Start(ctx context.Context) error {
	if err := m.spawn(ctx); err != nil {
		return fmt.Errorf("failed to spawn floor manager: %w", err)
	}
	go m.monitor(ctx)
	return nil
}

// Detach stops the floor manager's monitoring and tracker without killing the
// tmux session. The session survives so the next daemon start can reconnect.
func (m *Manager) Detach() {
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return
	}
	m.stopped = true
	close(m.stopCh)
	tracker := m.tracker
	m.tracker = nil
	m.mu.Unlock()

	if tracker != nil {
		tracker.Stop()
	}
}

// Stop stops the floor manager and kills its tmux session.
// Use Detach() instead if the session should survive (e.g. daemon shutdown).
func (m *Manager) Stop() {
	m.mu.Lock()
	tmuxSess := m.tmuxSession
	m.tmuxSession = ""
	m.mu.Unlock()

	m.Detach()

	if tmuxSess != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if m.server != nil {
			m.server.KillSession(ctx, tmuxSess)
		} else {
			tmux.KillSession(ctx, tmuxSess)
		}
	}
}

// TmuxSession returns the current tmux session name.
func (m *Manager) TmuxSession() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tmuxSession
}

// Tracker returns the session tracker for WebSocket terminal streaming.
func (m *Manager) Tracker() *session.SessionRuntime {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tracker
}

// Running returns whether the floor manager tmux session is alive.
func (m *Manager) Running() bool {
	m.mu.Lock()
	sess := m.tmuxSession
	m.mu.Unlock()
	if sess == "" {
		return false
	}
	if m.server != nil {
		return m.server.SessionExists(context.Background(), sess)
	}
	return tmux.SessionExists(context.Background(), sess)
}

// InjectionCount returns the current injection count.
func (m *Manager) InjectionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.injectionCount
}

// IncrementInjectionCount adds n to the injection count and checks if rotation is needed.
func (m *Manager) IncrementInjectionCount(n int) {
	m.mu.Lock()
	m.injectionCount += n
	count := m.injectionCount
	threshold := 0
	if m.cfg != nil {
		threshold = m.cfg.GetFloorManagerRotationThreshold()
	}
	m.mu.Unlock()

	if threshold > 0 && count >= threshold {
		go m.handleShiftRotation(context.Background())
	}
}

// ResetInjectionCount resets the count to zero.
func (m *Manager) ResetInjectionCount() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.injectionCount = 0
}

// EndShift signals that the FM has finished saving memory during a shift rotation.
func (m *Manager) EndShift() {
	m.mu.Lock()
	ch := m.shiftDone
	m.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (m *Manager) spawn(ctx context.Context) error {
	// Create working directory
	if err := os.MkdirAll(m.workDir, 0755); err != nil {
		return fmt.Errorf("failed to create work dir: %w", err)
	}

	// Write instruction files
	if err := m.writeInstructionFiles(); err != nil {
		return fmt.Errorf("failed to write instruction files: %w", err)
	}

	// If a session already exists (e.g. leftover from a previous daemon run),
	// reconnect to it instead of trying to create a duplicate.
	reconnected := false
	sessionExists := false
	if m.server != nil {
		sessionExists = m.server.SessionExists(ctx, m.sessionName)
	} else {
		sessionExists = tmux.SessionExists(ctx, m.sessionName)
	}
	if sessionExists {
		m.logger.Info("reconnecting to existing floor manager session", "tmux_session", m.sessionName)
		reconnected = true
	} else {
		// Build the command
		command, err := m.buildFMCommand(ctx, "Begin.")
		if err != nil {
			return fmt.Errorf("failed to build command: %w", err)
		}

		// Create tmux session
		var createErr error
		if m.server != nil {
			createErr = m.server.CreateSession(ctx, m.sessionName, m.workDir, command)
		} else {
			createErr = tmux.CreateSession(ctx, m.sessionName, m.workDir, command)
		}
		if createErr != nil {
			return fmt.Errorf("failed to create tmux session: %w", createErr)
		}
	}

	// Create a tracker for terminal streaming via WebSocket
	source := session.NewLocalSource("floor-manager", m.sessionName, m.server, nil)
	source.Start()
	tracker := session.NewSessionRuntime(
		"floor-manager",
		source,
		nil, // no state store
		"",  // no event file
		nil, // no event handlers
		nil, // no output callback
		nil, // no logger
	)
	tracker.Start()

	m.mu.Lock()
	m.tmuxSession = m.sessionName
	m.injectionCount = 0
	m.tracker = tracker
	m.mu.Unlock()

	if reconnected {
		m.logger.Info("floor manager reconnected", "tmux_session", m.sessionName)
	} else {
		m.logger.Info("floor manager spawned", "tmux_session", m.sessionName)
	}
	return nil
}

func (m *Manager) spawnResume(ctx context.Context) error {
	// If a session already exists, reconnect to it.
	reconnected := false
	sessionExistsResume := false
	if m.server != nil {
		sessionExistsResume = m.server.SessionExists(ctx, m.sessionName)
	} else {
		sessionExistsResume = tmux.SessionExists(ctx, m.sessionName)
	}
	if sessionExistsResume {
		m.logger.Info("reconnecting to existing floor manager session for resume", "tmux_session", m.sessionName)
		reconnected = true
	} else {
		command, err := m.buildFMResumeCommand(ctx)
		if err != nil {
			return err
		}

		var createErr error
		if m.server != nil {
			createErr = m.server.CreateSession(ctx, m.sessionName, m.workDir, command)
		} else {
			createErr = tmux.CreateSession(ctx, m.sessionName, m.workDir, command)
		}
		if createErr != nil {
			return createErr
		}
	}

	// Create a tracker for terminal streaming via WebSocket
	source := session.NewLocalSource("floor-manager", m.sessionName, m.server, nil)
	source.Start()
	tracker := session.NewSessionRuntime(
		"floor-manager",
		source,
		nil, // no state store
		"",  // no event file
		nil, // no event handlers
		nil, // no output callback
		nil, // no logger
	)
	tracker.Start()

	m.mu.Lock()
	m.tmuxSession = m.sessionName
	m.tracker = tracker
	m.mu.Unlock()

	if reconnected {
		m.logger.Info("floor manager reconnected", "tmux_session", m.sessionName)
	} else {
		m.logger.Info("floor manager resumed", "tmux_session", m.sessionName)
	}
	return nil
}

func (m *Manager) monitor(ctx context.Context) {
	ticker := time.NewTicker(monitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !m.Running() {
				m.logger.Info("floor manager session exited, restarting")
				m.checkAndRestart(ctx)
			}
		}
	}
}

func (m *Manager) checkAndRestart(ctx context.Context) {
	// Stop the old tracker before creating a new one to avoid leaking
	// goroutines that try to attach to the dead tmux session.
	m.mu.Lock()
	oldTracker := m.tracker
	m.tracker = nil
	m.mu.Unlock()
	if oldTracker != nil {
		oldTracker.Stop()
	}

	// Try resume first
	if err := m.spawnResume(ctx); err != nil {
		m.logger.Warn("resume failed, trying fresh spawn", "err", err)
		// Fallback to fresh spawn
		if err := m.spawn(ctx); err != nil {
			m.logger.Error("failed to restart floor manager", "err", err)
			// Will retry on next monitor tick
		}
	}
}

func (m *Manager) handleShiftRotation(ctx context.Context) {
	m.mu.Lock()
	if m.rotating || m.stopped {
		m.mu.Unlock()
		return
	}
	m.rotating = true
	m.shiftDone = make(chan struct{}, 1)
	tmuxSess := m.tmuxSession
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.rotating = false
		m.shiftDone = nil
		m.mu.Unlock()
	}()

	// Send [SHIFT] warning — clear partial input first to prevent collision
	shiftMsg := fmt.Sprintf("[SHIFT] Forced rotation imminent. Save your summary to memory.md, then run `%s end-shift`. Do not acknowledge this message to the operator.", m.schmuxBin)
	_ = tmux.SendKeys(ctx, tmuxSess, "C-u")
	if err := tmux.SendLiteral(ctx, tmuxSess, shiftMsg); err != nil {
		m.logger.Warn("failed to send [SHIFT] to floor manager", "err", err)
	} else {
		_ = tmux.SendKeys(ctx, tmuxSess, "Enter")
	}

	// Wait for end-shift or timeout
	m.mu.Lock()
	ch := m.shiftDone
	m.mu.Unlock()

	select {
	case <-ch:
		m.logger.Info("floor manager acknowledged end-shift")
	case <-time.After(shiftTimeout):
		m.logger.Warn("floor manager did not end-shift within timeout, force rotating")
	case <-m.stopCh:
		return
	case <-ctx.Done():
		return
	}

	m.HandleRotation(ctx)
}

// HandleRotation disposes the current session and spawns a fresh one.
func (m *Manager) HandleRotation(ctx context.Context) {
	m.mu.Lock()
	tmuxSess := m.tmuxSession
	m.tmuxSession = ""
	tracker := m.tracker
	m.tracker = nil
	m.mu.Unlock()

	if tracker != nil {
		tracker.Stop()
	}

	if tmuxSess != "" {
		if m.server != nil {
			_ = m.server.KillSession(ctx, tmuxSess)
		} else {
			_ = tmux.KillSession(ctx, tmuxSess)
		}
	}

	time.Sleep(restartDelay)

	if err := m.spawn(ctx); err != nil {
		m.logger.Error("failed to respawn after rotation", "err", err)
	}
}

func (m *Manager) writeInstructionFiles() error {
	instructions := GenerateInstructions(m.schmuxBin)
	settings := GenerateSettings(m.schmuxBin)

	// Write CLAUDE.md
	if err := os.WriteFile(filepath.Join(m.workDir, "CLAUDE.md"), []byte(instructions), 0644); err != nil {
		return err
	}

	// Write AGENTS.md (identical)
	if err := os.WriteFile(filepath.Join(m.workDir, "AGENTS.md"), []byte(instructions), 0644); err != nil {
		return err
	}

	// Write .claude/settings.json
	claudeDir := filepath.Join(m.workDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(settings), 0644); err != nil {
		return err
	}

	// Create empty memory.md only if it doesn't exist
	memPath := filepath.Join(m.workDir, "memory.md")
	if _, err := os.Stat(memPath); os.IsNotExist(err) {
		if err := os.WriteFile(memPath, []byte("# Floor Manager Memory\n\nNo previous session context.\n"), 0644); err != nil {
			return err
		}
	}

	return nil
}

// resolveSessionName looks up a session nickname by ID, falling back to the ID itself.
func (m *Manager) resolveSessionName(sessionID string) string {
	if m.sm != nil {
		sess, err := m.sm.GetSession(sessionID)
		if err == nil && sess.Nickname != "" {
			return sess.Nickname
		}
	}
	return sessionID
}

// buildFMCommand constructs the agent launch command for the floor manager.
// Unlike regular session commands, the FM does NOT inject SCHMUX_EVENTS_FILE,
// SCHMUX_WORKSPACE_ID, or signaling hooks — it only needs the agent binary
// with a prompt.
func (m *Manager) buildFMCommand(ctx context.Context, prompt string) (string, error) {
	resolved, err := m.resolveTarget(ctx)
	if err != nil {
		return "", err
	}

	// Build the base command with model flag if applicable
	baseCommand := resolved.Command
	if resolved.Model != nil && resolved.ToolName != "" {
		adapter := detect.GetAdapter(resolved.ToolName)
		if adapter != nil {
			flag := adapter.ModelFlag()
			if spec, ok := resolved.Model.RunnerFor(resolved.ToolName); ok && spec.ModelValue != "" && flag != "" {
				baseCommand = fmt.Sprintf("%s %s %s", baseCommand, flag, shellutil.Quote(spec.ModelValue))
			}
		}
	}

	// FM gets minimal env: just SCHMUX_ENABLED and SCHMUX_SESSION_ID
	env := mergeEnv(resolved.Env, map[string]string{
		"SCHMUX_ENABLED":    "1",
		"SCHMUX_SESSION_ID": "floor-manager",
	})

	if resolved.Promptable && prompt != "" {
		command := fmt.Sprintf("%s %s", baseCommand, shellutil.Quote(prompt))
		return fmt.Sprintf("%s %s", buildEnvPrefix(env), command), nil
	}

	return fmt.Sprintf("%s %s", buildEnvPrefix(env), baseCommand), nil
}

// buildFMResumeCommand constructs the resume command for the floor manager.
func (m *Manager) buildFMResumeCommand(ctx context.Context) (string, error) {
	resolved, err := m.resolveTarget(ctx)
	if err != nil {
		return "", err
	}

	// Use the resolved tool name from session, fall back to first runner key, then Name
	toolName := resolved.ToolName
	if toolName == "" && resolved.Model != nil {
		toolName = resolved.Model.FirstRunnerKey()
	}
	if toolName == "" {
		toolName = resolved.Name
	}

	parts, err := detect.BuildCommandParts(toolName, resolved.Command, detect.ToolModeResume, "", resolved.Model)
	if err != nil {
		return "", fmt.Errorf("resume not supported for target: %w", err)
	}

	cmd := joinParts(parts)

	env := mergeEnv(resolved.Env, map[string]string{
		"SCHMUX_ENABLED":    "1",
		"SCHMUX_SESSION_ID": "floor-manager",
	})

	return fmt.Sprintf("%s %s", buildEnvPrefix(env), cmd), nil
}

// resolvedFMTarget holds a resolved target with optional model info.
type resolvedFMTarget struct {
	Name       string
	ToolName   string // the resolved tool name (e.g., "claude", "opencode")
	Command    string
	Promptable bool
	Env        map[string]string
	Model      *detect.Model
}

func (m *Manager) resolveTarget(ctx context.Context) (resolvedFMTarget, error) {
	targetName := m.cfg.GetFloorManagerTarget()
	if targetName == "" {
		return resolvedFMTarget{}, fmt.Errorf("no floor manager target configured")
	}

	resolved, err := m.sm.ResolveTarget(ctx, targetName)
	if err != nil {
		return resolvedFMTarget{}, fmt.Errorf("failed to resolve target %q: %w", targetName, err)
	}

	return resolvedFMTarget{
		Name:       resolved.Name,
		ToolName:   resolved.ToolName,
		Command:    resolved.Command,
		Promptable: resolved.Promptable,
		Env:        resolved.Env,
		Model:      resolved.Model,
	}, nil
}

// mergeEnv merges two env maps, with overrides taking precedence.
func mergeEnv(base, overrides map[string]string) map[string]string {
	result := make(map[string]string, len(base)+len(overrides))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overrides {
		result[k] = v
	}
	return result
}

// buildEnvPrefix builds "KEY1=val1 KEY2=val2" prefix for shell command.
func buildEnvPrefix(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}
	parts := make([]string, 0, len(env))
	for k, v := range env {
		parts = append(parts, fmt.Sprintf("%s=%s", k, shellutil.Quote(v)))
	}
	return joinParts(parts)
}

// joinParts joins string parts with spaces.
func joinParts(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += " "
		}
		result += p
	}
	return result
}
