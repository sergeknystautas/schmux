package floormanager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
)

const (
	floorManagerNickname = "floor-manager"
	memoryDir            = ".schmux/floor-manager"
	memoryFile           = "memory.md"
	restartDelay         = 3 * time.Second
	shiftRotationTimeout = 30 * time.Second
	monitorPollInterval  = 15 * time.Second

	// DefaultRotationThreshold and DefaultDebounceMs are aliases for the
	// canonical constants in config, kept for backward compatibility.
	DefaultRotationThreshold = config.DefaultFloorManagerRotationThreshold
	DefaultDebounceMs        = config.DefaultFloorManagerDebounceMs
)

// Manager handles the floor manager session lifecycle.
type Manager struct {
	cfg      *config.Config
	state    *state.State
	session  *session.Manager
	homePath string

	injectionCount int
	rotating       bool // guards against concurrent HandleRotation calls
	mu             sync.Mutex
	stopCh         chan struct{}
	doneCh         chan struct{}
}

// FloorManagerSessionInfo holds atomically-read session info for the floor manager.
type FloorManagerSessionInfo struct {
	SessionID   string
	TmuxSession string
}

// New creates a new floor manager Manager.
func New(cfg *config.Config, st *state.State, sm *session.Manager, homePath string) *Manager {
	m := &Manager{
		cfg:      cfg,
		state:    st,
		session:  sm,
		homePath: homePath,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	return m
}

// Start begins the floor manager lifecycle. If enabled, spawns the session
// and starts the monitor goroutine.
func (m *Manager) Start(ctx context.Context) error {
	if !m.cfg.GetFloorManagerEnabled() {
		close(m.doneCh)
		return nil
	}

	// Singleton check
	if _, exists := m.state.GetFloorManagerSession(); exists {
		// Ensure memory directory exists
		memDir := filepath.Join(m.homePath, memoryDir)
		if err := os.MkdirAll(memDir, 0755); err != nil {
			fmt.Printf("[floor-manager] warning: failed to create memory directory %s: %v\n", memDir, err)
		}
		go m.monitor(ctx)
		return nil
	}

	if err := m.spawn(ctx, false); err != nil {
		close(m.doneCh)
		return fmt.Errorf("floor manager spawn: %w", err)
	}

	go m.monitor(ctx)
	return nil
}

// Stop signals the monitor goroutine to exit and waits for it.
func (m *Manager) Stop() {
	select {
	case <-m.stopCh:
		// Already stopped
	default:
		close(m.stopCh)
	}
	<-m.doneCh
}

func (m *Manager) spawn(ctx context.Context, isRestart bool) error {
	// Defensive singleton check: if a floor manager session already exists in state,
	// skip spawning to prevent duplicates (e.g. checkAndRestart racing with rotation).
	if _, exists := m.state.GetFloorManagerSession(); exists {
		fmt.Printf("[floor-manager] session already exists in state, skipping spawn\n")
		return nil
	}

	target := m.cfg.GetFloorManagerTarget()
	if target == "" {
		return fmt.Errorf("floor_manager.target not configured")
	}

	// Ensure memory/workspace directory exists.
	// The floor manager uses ~/.schmux/floor-manager as both its memory
	// directory and its working directory.
	memDir := filepath.Join(m.homePath, memoryDir)
	if err := os.MkdirAll(memDir, 0755); err != nil {
		fmt.Printf("[floor-manager] warning: failed to create memory directory %s: %v\n", memDir, err)
	}

	// Write CLAUDE.md and AGENTS.md with static instructions
	instructions := GenerateInstructions()
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		if err := os.WriteFile(filepath.Join(memDir, name), []byte(instructions), 0644); err != nil {
			fmt.Printf("[floor-manager] warning: failed to write %s: %v\n", name, err)
		}
	}

	// Write .claude/settings.json to pre-approve schmux commands
	claudeDir := filepath.Join(memDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		fmt.Printf("[floor-manager] warning: failed to create .claude directory: %v\n", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(GenerateSettings()), 0644); err != nil {
		fmt.Printf("[floor-manager] warning: failed to write settings.json: %v\n", err)
	}

	// Spawn via session manager using WorkDir (no workspace registration needed).
	// Prompt is minimal — all instructions are in CLAUDE.md.
	// IsFloorManager is set here so it's persisted atomically with the session.
	opts := session.SpawnOptions{
		TargetName:     target,
		Prompt:         "Begin.",
		Nickname:       floorManagerNickname,
		WorkDir:        memDir,
		IsFloorManager: true,
	}

	sess, err := m.session.Spawn(ctx, opts)
	if err != nil {
		return err
	}

	_ = sess // session is saved with IsFloorManager=true by Spawn

	m.mu.Lock()
	m.injectionCount = 0
	m.mu.Unlock()

	return nil
}

func (m *Manager) monitor(ctx context.Context) {
	defer close(m.doneCh)

	ticker := time.NewTicker(monitorPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkAndRestart(ctx)
		}
	}
}

func (m *Manager) checkAndRestart(ctx context.Context) {
	sess, found := m.state.GetFloorManagerSession()
	if !found {
		// No floor manager session in state — this can happen if a rotation
		// disposed the old session but the subsequent spawn failed. Attempt
		// a fresh spawn so the floor manager recovers automatically.
		if m.cfg.GetFloorManagerEnabled() {
			fmt.Printf("[floor-manager] no session found in state, attempting fresh spawn\n")
			if err := m.spawn(ctx, true); err != nil {
				fmt.Printf("[floor-manager] recovery spawn failed: %v\n", err)
			}
		}
		return
	}

	if m.session.IsRunning(ctx, sess.ID) {
		return
	}

	fmt.Printf("[floor-manager] session exited, attempting resume-first restart after %s\n", restartDelay)

	select {
	case <-ctx.Done():
		fmt.Printf("[floor-manager] restart cancelled: %v\n", ctx.Err())
		return
	case <-time.After(restartDelay):
	}

	// Resume-first: try spawning with Resume mode (uses agent's resume command).
	// This preserves conversation context without needing a fresh prompt.
	if err := m.spawnResume(ctx); err != nil {
		fmt.Printf("[floor-manager] resume failed (%v), falling back to fresh spawn\n", err)
		if err := m.spawn(ctx, true); err != nil {
			fmt.Printf("[floor-manager] fresh spawn also failed: %v\n", err)
		}
	}
}

// spawnResume attempts to restart the floor manager using the agent's resume mode,
// which preserves the previous conversation context.
func (m *Manager) spawnResume(ctx context.Context) error {
	target := m.cfg.GetFloorManagerTarget()
	if target == "" {
		return fmt.Errorf("floor_manager.target not configured")
	}

	// Ensure memory directory exists for resume too
	memDir := filepath.Join(m.homePath, memoryDir)
	if err := os.MkdirAll(memDir, 0755); err != nil {
		fmt.Printf("[floor-manager] warning: failed to create memory directory %s: %v\n", memDir, err)
	}

	// Spawn via session manager with Resume: true and WorkDir (no workspace needed).
	// Prompt is intentionally empty — resume mode uses the agent's built-in
	// resume command to restore the previous conversation context.
	opts := session.SpawnOptions{
		TargetName:     target,
		Nickname:       floorManagerNickname,
		Resume:         true,
		WorkDir:        memDir,
		IsFloorManager: true,
	}

	sess, err := m.session.Spawn(ctx, opts)
	if err != nil {
		return err
	}

	_ = sess // session is saved with IsFloorManager=true by Spawn

	m.mu.Lock()
	m.injectionCount = 0
	m.mu.Unlock()

	return nil
}

// GetSessionID returns the floor manager's session ID, or empty if none exists.
func (m *Manager) GetSessionID() string {
	sess, found := m.state.GetFloorManagerSession()
	if !found {
		return ""
	}
	return sess.ID
}

// GetSessionInfo atomically reads both the session ID and tmux session name.
// Returns nil if no floor manager session exists.
func (m *Manager) GetSessionInfo() *FloorManagerSessionInfo {
	sess, found := m.state.GetFloorManagerSession()
	if !found {
		return nil
	}
	return &FloorManagerSessionInfo{
		SessionID:   sess.ID,
		TmuxSession: sess.TmuxSession,
	}
}

// HandleRotation performs a context rotation: waits for the agent to save its
// memory file, disposes the current session, and spawns a fresh one.
// Guarded against concurrent calls — only one rotation runs at a time.
// If skipFinalizeWait is true, the 5-second finalize wait is skipped (the
// caller already ensured the agent had time to save memory).
func (m *Manager) HandleRotation(ctx context.Context, skipFinalizeWait bool) {
	m.mu.Lock()
	if m.rotating {
		m.mu.Unlock()
		fmt.Printf("[floor-manager] rotation already in progress, skipping\n")
		return
	}
	m.rotating = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.rotating = false
		m.mu.Unlock()
	}()

	sess, found := m.state.GetFloorManagerSession()
	if !found {
		return
	}

	fmt.Printf("[floor-manager] rotation requested, restarting with fresh context\n")

	// Give agent time to finalize memory file (skip if caller already waited)
	if !skipFinalizeWait {
		select {
		case <-ctx.Done():
			fmt.Printf("[floor-manager] rotation cancelled during finalize wait: %v\n", ctx.Err())
			return
		case <-time.After(5 * time.Second):
		}
	}

	// Dispose current session
	m.session.Dispose(ctx, sess.ID)

	// Spawn fresh with memory file
	select {
	case <-ctx.Done():
		fmt.Printf("[floor-manager] rotation cancelled during restart wait: %v\n", ctx.Err())
		return
	case <-time.After(restartDelay):
	}
	if err := m.spawn(ctx, false); err != nil {
		fmt.Printf("[floor-manager] rotation spawn failed: %v\n", err)
	}
}

// GetInjectionCount returns the number of signals injected in the current session.
func (m *Manager) GetInjectionCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.injectionCount
}

// IncrementInjectionCount atomically increments the injection count by n
// and returns the new total. This allows batched flushes (where multiple
// signals are sent at once) to increment correctly.
func (m *Manager) IncrementInjectionCount(n int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.injectionCount += n
	return m.injectionCount
}

// GetRotationThreshold returns the configured rotation threshold.
func (m *Manager) GetRotationThreshold() int {
	return m.cfg.GetFloorManagerRotationThreshold()
}
