package session

import (
	"fmt"
	"os"
	"strconv"
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
	config     *config.Config
	state      *state.State
	workspace  *workspace.Manager
}

// New creates a new session manager.
func New(cfg *config.Config, st *state.State, wm *workspace.Manager) *Manager {
	return &Manager{
		config:    cfg,
		state:     st,
		workspace: wm,
	}
}

// Spawn creates a new session.
// If workspaceID is provided, spawn into that specific workspace (Existing Directory Spawn mode).
// Otherwise, find or create a workspace by repo/branch.
func (m *Manager) Spawn(repo, branch, agentName, prompt string, workspaceID string) (*state.Session, error) {
	// Find agent config
	agent, found := m.config.FindAgent(agentName)
	if !found {
		return nil, fmt.Errorf("agent not found: %s", agentName)
	}

	var w *state.Workspace
	var isNew bool
	var err error

	if workspaceID != "" {
		// Spawn into specific workspace (Existing Directory Spawn mode)
		ws, found := m.workspace.GetByID(workspaceID)
		if !found {
			return nil, fmt.Errorf("workspace not found: %s", workspaceID)
		}
		w = ws
		isNew = false
	} else {
		// Get or create workspace by repo/branch
		w, isNew, err = m.workspace.GetOrCreate(repo, branch)
		if err != nil {
			return nil, fmt.Errorf("failed to get workspace: %w", err)
		}

		// Prepare workspace (git operations) only for new workspaces
		if isNew {
			if err := m.workspace.Prepare(w.ID, branch); err != nil {
				return nil, fmt.Errorf("failed to prepare workspace: %w", err)
			}
		}
	}

	// Build agent command with prompt - properly quote the prompt to prevent command injection
	// The prompt is quoted so it's passed as a single argument to the agent
	command := fmt.Sprintf("%s %s", agent.Command, strconv.Quote(prompt))

	// Create session ID
	sessionID := fmt.Sprintf("%s-%s", w.ID, uuid.New().String()[:8])

	// Create tmux session
	tmuxSession := sessionID
	if err := tmux.CreateSession(tmuxSession, w.Path, command); err != nil {
		return nil, fmt.Errorf("failed to create tmux session: %w", err)
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
		Branch:      branch,
		Prompt:      prompt,
		TmuxSession: tmuxSession,
		CreatedAt:   time.Now(),
		Pid:         pid,
	}

	m.state.AddSession(sess)
	if err := m.state.Save(); err != nil {
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

	// Kill tmux session
	if err := tmux.KillSession(sess.TmuxSession); err != nil {
		// Log but don't fail - session might already be gone
		fmt.Printf("warning: failed to kill tmux session: %v\n", err)
	}

	// Clean up workspace (reset git state)
	if err := m.workspace.Cleanup(sess.WorkspaceID); err != nil {
		// Keep workspace as-is on cleanup failure (per spec)
		fmt.Printf("warning: failed to cleanup workspace: %v\n", err)
	}

	// Remove session from state
	m.state.RemoveSession(sessionID)
	if err := m.state.Save(); err != nil {
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
