package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// State represents the application state.
type State struct {
	Workspaces    []Workspace             `json:"workspaces"`
	Sessions      []Session               `json:"sessions"`
	WorktreeBases []WorktreeBase          `json:"base_repos,omitempty"`    // bare clones that host worktrees
	PullRequests  []contracts.PullRequest `json:"pull_requests,omitempty"` // cached GitHub PRs
	PublicRepos   []string                `json:"public_repos,omitempty"`  // repo URLs confirmed public on GitHub
	NeedsRestart  bool                    `json:"needs_restart,omitempty"` // true if daemon needs restart for config changes to take effect
	path          string                  // path to the state file
	mu            sync.RWMutex
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
	GitFilesChanged int    `json:"-"`
}

// WorktreeBase tracks a bare clone that hosts worktrees.
type WorktreeBase struct {
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
	LastSignalAt time.Time `json:"-"`               // Last time agent sent a direct signal (in-memory only)
	Nudge        string    `json:"nudge,omitempty"` // NudgeNik consultation result
}

// New creates a new empty State instance.
func New(path string) *State {
	return &State{
		Workspaces:    []Workspace{},
		Sessions:      []Session{},
		WorktreeBases: []WorktreeBase{},
		path:          path,
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

	// Initialize WorktreeBases if nil (existing state files)
	if st.WorktreeBases == nil {
		st.WorktreeBases = []WorktreeBase{}
	}

	// Reset LastOutputAt for all loaded sessions to avoid treating restored
	// sessions as "recently active" on startup, which would block git status updates.
	for i := range st.Sessions {
		st.Sessions[i].LastOutputAt = time.Time{}
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

// UpdateSessionLastSignal atomically updates just the LastSignalAt field.
// This is safe to call from concurrent goroutines (e.g., WebSocket handlers).
func (s *State) UpdateSessionLastSignal(sessionID string, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Sessions {
		if s.Sessions[i].ID == sessionID {
			s.Sessions[i].LastSignalAt = t
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

// GetWorktreeBases returns all worktree bases.
func (s *State) GetWorktreeBases() []WorktreeBase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.WorktreeBases == nil {
		return []WorktreeBase{}
	}
	bases := make([]WorktreeBase, len(s.WorktreeBases))
	copy(bases, s.WorktreeBases)
	return bases
}

// AddWorktreeBase adds a worktree base to the state.
func (s *State) AddWorktreeBase(wb WorktreeBase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Check for existing entry with same URL
	for i, existing := range s.WorktreeBases {
		if existing.RepoURL == wb.RepoURL {
			// Update existing entry
			s.WorktreeBases[i] = wb
			return nil
		}
	}
	s.WorktreeBases = append(s.WorktreeBases, wb)
	return nil
}

// GetWorktreeBaseByURL returns a worktree base by its URL.
func (s *State) GetWorktreeBaseByURL(repoURL string) (WorktreeBase, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, wb := range s.WorktreeBases {
		if wb.RepoURL == repoURL {
			return wb, true
		}
	}
	return WorktreeBase{}, false
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

// GetPullRequests returns a copy of the stored pull requests.
func (s *State) GetPullRequests() []contracts.PullRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]contracts.PullRequest, len(s.PullRequests))
	copy(result, s.PullRequests)
	return result
}

// SetPullRequests replaces the stored pull requests.
func (s *State) SetPullRequests(prs []contracts.PullRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PullRequests = prs
}

// GetPublicRepos returns a copy of the stored public repo URLs.
func (s *State) GetPublicRepos() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]string, len(s.PublicRepos))
	copy(result, s.PublicRepos)
	return result
}

// SetPublicRepos replaces the stored public repo URLs.
func (s *State) SetPublicRepos(repos []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PublicRepos = repos
}
