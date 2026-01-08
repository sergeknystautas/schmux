package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/sergek/schmux/internal/config"
	"github.com/sergek/schmux/internal/state"
	"github.com/sergek/schmux/internal/tmux"
	"github.com/sergek/schmux/internal/workspace"
)

// Manager manages sessions.
type Manager struct {
	config    *config.Config
	state     *state.State
	statePath string
	workspace *workspace.Manager
}

// New creates a new session manager.
func New(cfg *config.Config, st *state.State, statePath string, wm *workspace.Manager) *Manager {
	return &Manager{
		config:    cfg,
		state:     st,
		statePath: statePath,
		workspace: wm,
	}
}

// Spawn creates a new session.
// If workspaceID is provided, spawn into that specific workspace (Existing Directory Spawn mode).
// Otherwise, find or create a workspace by repoURL/branch.
// nickname is an optional human-friendly name for the session.
func (m *Manager) Spawn(repoURL, branch, agentName, prompt, nickname string, workspaceID string) (*state.Session, error) {
	// Find agent config
	agent, found := m.config.FindAgent(agentName)
	if !found {
		return nil, fmt.Errorf("agent not found: %s", agentName)
	}

	var w *state.Workspace
	var err error

	if workspaceID != "" {
		// Spawn into specific workspace (Existing Directory Spawn mode - no git operations)
		ws, found := m.workspace.GetByID(workspaceID)
		if !found {
			return nil, fmt.Errorf("workspace not found: %s", workspaceID)
		}
		w = ws
	} else {
		// Get or create workspace (includes fetch/pull/clean)
		w, err = m.workspace.GetOrCreate(repoURL, branch)
		if err != nil {
			return nil, fmt.Errorf("failed to get workspace: %w", err)
		}
	}

	// Build agent command with prompt - properly quote the prompt to prevent command injection
	// The prompt is quoted so it's passed as a single argument to the agent
	command := fmt.Sprintf("%s %s", agent.Command, strconv.Quote(prompt))

	// Create session ID
	sessionID := fmt.Sprintf("%s-%s", w.ID, uuid.New().String()[:8])

	// Use sanitized nickname for tmux session name if provided, otherwise use sessionID
	tmuxSession := sessionID
	if nickname != "" {
		tmuxSession = sanitizeNickname(nickname)
	}

	// Create tmux session
	if err := tmux.CreateSession(tmuxSession, w.Path, command); err != nil {
		return nil, fmt.Errorf("failed to create tmux session: %w", err)
	}

	// Set up log file for pipe-pane streaming
	logPath, err := m.ensureLogFile(sessionID)
	if err != nil {
		fmt.Printf("warning: failed to create log file: %v\n", err)
	} else {
		// Force fixed window size for deterministic TUI output
		width, height := m.config.GetTerminalSize()
		if err := tmux.SetWindowSizeManual(tmuxSession); err != nil {
			fmt.Printf("warning: failed to set manual window size: %v\n", err)
		}
		if err := tmux.ResizeWindow(tmuxSession, width, height); err != nil {
			fmt.Printf("warning: failed to resize window: %v\n", err)
		}
		// Start pipe-pane to log file
		if err := tmux.StartPipePane(tmuxSession, logPath); err != nil {
			return nil, fmt.Errorf("failed to start pipe-pane (session created): %w", err)
		}
	}

	// Get the PID of the agent process from tmux pane
	pid, err := tmux.GetPanePID(tmuxSession)
	if err != nil {
		return nil, fmt.Errorf("failed to get pane PID: %w", err)
	}

	// Create session state with cached PID
	sess := state.Session{
		ID:          sessionID,
		WorkspaceID: w.ID,
		Agent:       agentName,
		Prompt:      prompt,
		Nickname:    nickname,
		TmuxSession: tmuxSession,
		CreatedAt:   time.Now(),
		Pid:         pid,
	}

	m.state.AddSession(sess)
	if err := state.Save(m.state, m.statePath); err != nil {
		return nil, fmt.Errorf("failed to save state: %w", err)
	}

	return &sess, nil
}

// IsRunning checks if the agent process is still running.
// Uses the cached PID from tmux pane, which is more reliable than searching by process name.
func (m *Manager) IsRunning(sessionID string) bool {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return false
	}

	// If we don't have a PID, check if tmux session exists as fallback
	if sess.Pid == 0 {
		return tmux.SessionExists(sess.TmuxSession)
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
func (m *Manager) Dispose(sessionID string) error {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Kill tmux session (ignore error if already gone)
	tmux.KillSession(sess.TmuxSession)

	// Delete log file for this session
	if err := m.deleteLogFile(sessionID); err != nil {
		fmt.Printf("warning: failed to delete log file: %v\n", err)
	}

	// Note: workspace is NOT cleaned up on session disposal.
	// Workspaces persist and are only reset when reused for a new spawn.

	// Remove session from state
	m.state.RemoveSession(sessionID)
	if err := state.Save(m.state, m.statePath); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
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
func (m *Manager) GetOutput(sessionID string) (string, error) {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	return tmux.CaptureOutput(sess.TmuxSession)
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
func (m *Manager) RenameSession(sessionID, newNickname string) error {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	oldTmuxName := sess.TmuxSession
	newTmuxName := oldTmuxName
	if newNickname != "" {
		newTmuxName = sanitizeNickname(newNickname)
	}

	// Rename the tmux session
	if err := tmux.RenameSession(oldTmuxName, newTmuxName); err != nil {
		return fmt.Errorf("failed to rename tmux session: %w", err)
	}

	// Update session state
	sess.Nickname = newNickname
	sess.TmuxSession = newTmuxName
	m.state.UpdateSession(sess)
	if err := state.Save(m.state, m.statePath); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	return nil
}

// getLogDir returns the log directory path, creating it if needed.
func (m *Manager) getLogDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	logDir := filepath.Join(homeDir, ".schmux", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create log directory: %w", err)
	}
	return logDir, nil
}

// getLogPath returns the log file path for a session.
func (m *Manager) getLogPath(sessionID string) (string, error) {
	logDir, err := m.getLogDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(logDir, fmt.Sprintf("%s.log", sessionID)), nil
}

// ensureLogFile ensures the log file exists for a session.
func (m *Manager) ensureLogFile(sessionID string) (string, error) {
	logPath, err := m.getLogPath(sessionID)
	if err != nil {
		return "", err
	}
	fd, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return "", fmt.Errorf("failed to create log file: %w", err)
	}
	fd.Close()
	return logPath, nil
}

// deleteLogFile removes the log file for a session.
func (m *Manager) deleteLogFile(sessionID string) error {
	logPath, err := m.getLogPath(sessionID)
	if err != nil {
		return err
	}
	if err := os.Remove(logPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete log file: %w", err)
	}
	return nil
}

// pruneLogFiles removes log files for sessions not in the active list.
func (m *Manager) pruneLogFiles(activeSessions []state.Session) error {
	logDir, err := m.getLogDir()
	if err != nil {
		return err
	}
	activeIDs := make(map[string]bool)
	for _, sess := range activeSessions {
		activeIDs[sess.ID] = true
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return fmt.Errorf("failed to read log directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".log")
		if !activeIDs[sessionID] {
			logPath := filepath.Join(logDir, entry.Name())
			if err := os.Remove(logPath); err != nil {
				fmt.Printf("warning: failed to delete orphaned log %s: %v\n", entry.Name(), err)
			}
		}
	}
	return nil
}

// GetLogPath returns the log file path for a session (public for WebSocket).
func (m *Manager) GetLogPath(sessionID string) (string, error) {
	return m.getLogPath(sessionID)
}

// EnsurePipePane ensures pipe-pane is active for a session (auto-migrate old sessions).
func (m *Manager) EnsurePipePane(sessionID string) error {
	sess, found := m.state.GetSession(sessionID)
	if !found {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	// Check if pipe-pane is already active
	if tmux.IsPipePaneActive(sess.TmuxSession) {
		return nil
	}
	// Ensure log file exists
	logPath, err := m.ensureLogFile(sessionID)
	if err != nil {
		return fmt.Errorf("failed to ensure log file: %w", err)
	}
	// Set window size and start pipe-pane
	width, height := m.config.GetTerminalSize()
	if err := tmux.SetWindowSizeManual(sess.TmuxSession); err != nil {
		fmt.Printf("warning: failed to set manual window size: %v\n", err)
	}
	if err := tmux.ResizeWindow(sess.TmuxSession, width, height); err != nil {
		fmt.Printf("warning: failed to resize window: %v\n", err)
	}
	if err := tmux.StartPipePane(sess.TmuxSession, logPath); err != nil {
		return fmt.Errorf("failed to start pipe-pane: %w", err)
	}
	return nil
}

// StartLogPruner starts periodic log pruning. Returns cancel function.
func (m *Manager) StartLogPruner(interval time.Duration) func() {
	stopChan := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		m.pruneLogs() // Run once on startup
		for {
			select {
			case <-ticker.C:
				m.pruneLogs()
			case <-stopChan:
				return
			}
		}
	}()
	return func() { close(stopChan) }
}

// pruneLogs runs pruneLogFiles with current sessions.
func (m *Manager) pruneLogs() {
	activeSessions := m.state.GetSessions()
	if err := m.pruneLogFiles(activeSessions); err != nil {
		fmt.Printf("warning: log prune failed: %v\n", err)
	}
}

// sanitizeNickname sanitizes a nickname for use as a tmux session name.
// tmux session names cannot contain dots (.) or colons (:).
func sanitizeNickname(nickname string) string {
	result := strings.ReplaceAll(nickname, ".", "-")
	result = strings.ReplaceAll(result, ":", "-")
	return result
}
