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
	Workspaces   []Workspace `json:"workspaces"`
	Sessions     []Session   `json:"sessions"`
	BaseRepos    []BaseRepo  `json:"base_repos,omitempty"`    // bare clones that host worktrees
	NeedsRestart bool        `json:"needs_restart,omitempty"` // true if daemon needs restart for config changes to take effect
	path         string      // path to the state file
	mu           sync.RWMutex
}

// Workspace represents a workspace directory state.
// Multiple sessions can share the same workspace (multi-agent per directory).
type Workspace struct {
	ID              string `json:"id"`
	Repo            string `json:"repo"`
	Branch          string `json:"branch"`
	Path            string `json:"path"`
	GitDirty        bool   `json:"-"`
	GitAhead        int    `json:"-"`
	GitBehind       int    `json:"-"`
	GitLinesAdded   int    `json:"-"`
	GitLinesRemoved int    `json:"-"`
}

// BaseRepo tracks a bare clone that hosts worktrees.
type BaseRepo struct {
	RepoURL string `json:"repo_url"` // e.g., "git@github.com:user/repo.git"
	Path    string `json:"path"`     // e.g., "~/.schmux/repos/myrepo.git"
}

// Session represents a run target session.
type Session struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspace_id"`
	Target       string    `json:"target"`
	Nickname     string    `json:"nickname,omitempty"` // Optional human-friendly name
	TmuxSession  string    `json:"tmux_session"`
	CreatedAt    time.Time `json:"created_at"`
	Pid          int       `json:"pid"`             // PID of the target process from tmux pane
	LastOutputAt time.Time `json:"-"`               // Last time terminal had new output (in-memory only, not persisted)
	Nudge        string    `json:"nudge,omitempty"` // NudgeNik consultation result
}

// New creates a new empty State instance.
func New(path string) *State {
	return &State{
		Workspaces: []Workspace{},
		Sessions:   []Session{},
		BaseRepos:  []BaseRepo{},
		path:       path,
	}
}

// Load loads the state from the given path.
// Returns an empty state if the file doesn't exist.
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return New(path), nil
		}
		return nil, fmt.Errorf("failed to read state: %w", err)
	}

	var st State
	st.path = path
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	// Initialize BaseRepos if nil (existing state files)
	if st.BaseRepos == nil {
		st.BaseRepos = []BaseRepo{}
	}

	return &st, nil
}

// Save saves the state to its configured path.
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.path == "" {
		return fmt.Errorf("state path is empty, cannot save")
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(s.path, data, 0644); err != nil {
		return fmt.Errorf("failed to write state: %w", err)
	}

	return nil
}

// AddWorkspace adds a workspace to the state.
func (s *State) AddWorkspace(w Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Workspaces = append(s.Workspaces, w)
	return nil
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
// Returns an error if the workspace is not found.
func (s *State) UpdateWorkspace(w Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.Workspaces {
		if existing.ID == w.ID {
			s.Workspaces[i] = w
			return nil
		}
	}
	return fmt.Errorf("workspace not found: %s", w.ID)
}

// AddSession adds a session to the state.
func (s *State) AddSession(sess Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Sessions = append(s.Sessions, sess)
	return nil
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
// Returns an error if the session is not found.
func (s *State) UpdateSession(sess Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.Sessions {
		if existing.ID == sess.ID {
			s.Sessions[i] = sess
			return nil
		}
	}
	return fmt.Errorf("session not found: %s", sess.ID)
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
func (s *State) RemoveSession(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sess := range s.Sessions {
		if sess.ID == id {
			s.Sessions = append(s.Sessions[:i], s.Sessions[i+1:]...)
			return nil
		}
	}
	return nil
}

// RemoveWorkspace removes a workspace from the state.
func (s *State) RemoveWorkspace(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, w := range s.Workspaces {
		if w.ID == id {
			s.Workspaces = append(s.Workspaces[:i], s.Workspaces[i+1:]...)
			return nil
		}
	}
	return nil
}

// GetBaseRepos returns all base repos.
func (s *State) GetBaseRepos() []BaseRepo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.BaseRepos == nil {
		return []BaseRepo{}
	}
	repos := make([]BaseRepo, len(s.BaseRepos))
	copy(repos, s.BaseRepos)
	return repos
}

// AddBaseRepo adds a base repo to the state.
func (s *State) AddBaseRepo(br BaseRepo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Check for existing entry with same URL
	for i, existing := range s.BaseRepos {
		if existing.RepoURL == br.RepoURL {
			// Update existing entry
			s.BaseRepos[i] = br
			return nil
		}
	}
	s.BaseRepos = append(s.BaseRepos, br)
	return nil
}

// GetBaseRepoByURL returns a base repo by its URL.
func (s *State) GetBaseRepoByURL(repoURL string) (BaseRepo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, br := range s.BaseRepos {
		if br.RepoURL == repoURL {
			return br, true
		}
	}
	return BaseRepo{}, false
}

// SetNeedsRestart sets the needs_restart flag.
func (s *State) SetNeedsRestart(needsRestart bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.NeedsRestart = needsRestart
	return nil
}

// GetNeedsRestart returns the needs_restart flag.
func (s *State) GetNeedsRestart() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.NeedsRestart
}
