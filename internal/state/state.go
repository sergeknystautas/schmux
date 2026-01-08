package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// State represents the application state.
type State struct {
	Workspaces []Workspace `json:"workspaces"`
	Sessions   []Session   `json:"sessions"`
	mu         sync.RWMutex
}

// Workspace represents a workspace directory state.
// Multiple sessions can share the same workspace (multi-agent per directory).
type Workspace struct {
	ID     string `json:"id"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Path   string `json:"path"`
}

// Session represents an agent session.
type Session struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspace_id"`
	Agent        string    `json:"agent"`
	Prompt       string    `json:"prompt"`
	Nickname     string    `json:"nickname,omitempty"` // Optional human-friendly name
	TmuxSession  string    `json:"tmux_session"`
	CreatedAt    time.Time `json:"created_at"`
	Pid          int       `json:"pid"` // PID of the agent process from tmux pane
	LastOutputAt time.Time `json:"-"`   // Last time terminal had new output (in-memory only, not persisted)
}

var (
	globalState *State
	once        sync.Once
)

// New creates a new empty State instance.
// Use this for testing or when you need multiple independent state instances.
func New() *State {
	return &State{
		Workspaces: []Workspace{},
		Sessions:   []Session{},
	}
}

// Get returns the global state singleton instance.
// Use this for the main application.
func Get() *State {
	once.Do(func() {
		globalState = New()
	})
	return globalState
}

// Load loads the state from ~/.schmux/state.json.
func Load() (*State, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	statePath := filepath.Join(homeDir, ".schmux", "state.json")

	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty state if file doesn't exist
			return Get(), nil
		}
		return nil, fmt.Errorf("failed to read state: %w", err)
	}

	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	globalState = &st
	return globalState, nil
}

// Save saves the state to ~/.schmux/state.json.
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	stateDir := filepath.Join(homeDir, ".schmux")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	statePath := filepath.Join(stateDir, "state.json")

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(statePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write state: %w", err)
	}

	return nil
}

// AddWorkspace adds a workspace to the state.
func (s *State) AddWorkspace(w Workspace) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Workspaces = append(s.Workspaces, w)
}

// GetWorkspace returns a workspace by ID.
func (s *State) GetWorkspace(id string) (Workspace, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, w := range s.Workspaces {
		if w.ID == id {
			return w, true
		}
	}
	return Workspace{}, false
}

// GetWorkspaces returns all workspaces.
// Returns a copy to prevent callers from modifying internal state.
func (s *State) GetWorkspaces() []Workspace {
	s.mu.RLock()
	defer s.mu.RUnlock()
	workspaces := make([]Workspace, len(s.Workspaces))
	copy(workspaces, s.Workspaces)
	return workspaces
}

// UpdateWorkspace updates a workspace in the state.
func (s *State) UpdateWorkspace(w Workspace) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.Workspaces {
		if existing.ID == w.ID {
			s.Workspaces[i] = w
			return
		}
	}
}

// AddSession adds a session to the state.
func (s *State) AddSession(sess Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Sessions = append(s.Sessions, sess)
}

// GetSession returns a session by ID.
func (s *State) GetSession(id string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sess := range s.Sessions {
		if sess.ID == id {
			return sess, true
		}
	}
	return Session{}, false
}

// GetSessions returns all sessions.
// Returns a copy to prevent callers from modifying internal state.
func (s *State) GetSessions() []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sessions := make([]Session, len(s.Sessions))
	copy(sessions, s.Sessions)
	return sessions
}

// UpdateSession updates a session in the state.
func (s *State) UpdateSession(sess Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.Sessions {
		if existing.ID == sess.ID {
			s.Sessions[i] = sess
			return
		}
	}
}

// UpdateSessionLastOutput atomically updates just the LastOutputAt field.
// This is safe to call from concurrent goroutines (e.g., WebSocket handlers).
func (s *State) UpdateSessionLastOutput(sessionID string, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Sessions {
		if s.Sessions[i].ID == sessionID {
			s.Sessions[i].LastOutputAt = t
			return
		}
	}
}

// RemoveSession removes a session from the state.
func (s *State) RemoveSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sess := range s.Sessions {
		if sess.ID == id {
			s.Sessions = append(s.Sessions[:i], s.Sessions[i+1:]...)
			return
		}
	}
}

// RemoveWorkspace removes a workspace from the state.
func (s *State) RemoveWorkspace(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, w := range s.Workspaces {
		if w.ID == id {
			s.Workspaces = append(s.Workspaces[:i], s.Workspaces[i+1:]...)
			return
		}
	}
}
