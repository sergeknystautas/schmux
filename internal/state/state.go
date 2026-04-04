package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/fileutil"
)

// DashboardSXStatus tracks the last heartbeat response and certificate expiry.
type DashboardSXStatus struct {
	LastHeartbeatTime   time.Time `json:"last_heartbeat_time,omitempty"`
	LastHeartbeatStatus int       `json:"last_heartbeat_status,omitempty"`
	LastHeartbeatError  string    `json:"last_heartbeat_error,omitempty"`
	CertDomain          string    `json:"cert_domain,omitempty"`
	CertExpiresAt       time.Time `json:"cert_expires_at,omitempty"`
}

// State represents the application state.
type State struct {
	Workspaces   []Workspace                 `json:"workspaces"`
	Sessions     []Session                   `json:"sessions"`
	RepoBases    []RepoBase                  `json:"base_repos,omitempty"`
	PullRequests []contracts.PullRequest     `json:"pull_requests,omitempty"` // cached GitHub PRs
	PublicRepos  []string                    `json:"public_repos,omitempty"`  // repo URLs confirmed public on GitHub
	NeedsRestart bool                        `json:"needs_restart,omitempty"` // true if daemon needs restart for config changes to take effect
	RemoteHosts  []RemoteHost                `json:"remote_hosts,omitempty"`  // connected/cached remote hosts
	Previews     map[string]WorkspacePreview `json:"previews,omitempty"`      // persisted preview mappings (proxy port must survive restart)
	DashboardSX  *DashboardSXStatus          `json:"dashboard_sx,omitempty"`
	path         string                      // path to the state file
	logger       *log.Logger
	mu           sync.RWMutex

	// Batched save support (Issue 6 fix)
	savePending atomic.Bool // True if a save is scheduled
	saveMu      sync.Mutex  // Protects save timer
	saveTimer   *time.Timer // Timer for batched saves
}

// RemoteHost represents a connected or cached remote host.
type RemoteHost struct {
	ID          string    `json:"id"`
	ProfileID   string    `json:"profile_id"`          // References config RemoteProfile.ID
	Flavor      string    `json:"flavor"`              // The flavor string within the profile
	FlavorID    string    `json:"flavor_id,omitempty"` // DEPRECATED: kept for migration/state compat
	Hostname    string    `json:"hostname"`            // e.g., "remote-host-456.example.com"
	UUID        string    `json:"uuid"`                // Remote session UUID
	ConnectedAt time.Time `json:"connected_at"`
	ExpiresAt   time.Time `json:"expires_at"`  // +12h from connected_at
	Status      string    `json:"status"`      // "provisioning", "connecting", "connected", "disconnected"
	Provisioned bool      `json:"provisioned"` // Has the workspace been provisioned?
}

// Remote host status constants
const (
	RemoteHostStatusProvisioning = "provisioning"
	RemoteHostStatusConnecting   = "connecting"
	RemoteHostStatusConnected    = "connected"
	RemoteHostStatusDisconnected = "disconnected"
	RemoteHostStatusFailed       = "failed"
	RemoteHostStatusExpired      = "expired"
	RemoteHostStatusReconnecting = "reconnecting"
)

// Session status constants.
const (
	SessionStatusProvisioning = "provisioning"
	SessionStatusRunning      = "running"
	SessionStatusStopped      = "stopped"
	SessionStatusFailed       = "failed"
	SessionStatusQueued       = "queued"
	SessionStatusDisposing    = "disposing"
)

// Workspace status constants.
const (
	WorkspaceStatusProvisioning = "provisioning"
	WorkspaceStatusRunning      = "running"
	WorkspaceStatusFailed       = "failed"
	WorkspaceStatusDisposing    = "disposing"
	WorkspaceStatusRecyclable   = "recyclable"
)

// Workspace represents a workspace directory state.
// Multiple sessions can share the same workspace (multi-agent per directory).
type Workspace struct {
	ID                      string            `json:"id"`
	Repo                    string            `json:"repo"`
	Branch                  string            `json:"branch"`
	Path                    string            `json:"path"`
	VCS                     string            `json:"vcs,omitempty"`
	Dirty                   bool              `json:"-"`
	Ahead                   int               `json:"-"`
	Behind                  int               `json:"-"`
	LinesAdded              int               `json:"-"`
	LinesRemoved            int               `json:"-"`
	FilesChanged            int               `json:"-"`
	CommitsSyncedWithRemote bool              `json:"-"`                            // true if local HEAD matches origin/{branch}
	DefaultBranchOrphaned   bool              `json:"-"`                            // true if origin/default has no common ancestor with HEAD
	RemoteBranchExists      bool              `json:"-"`                            // true if origin/<branch> ref exists in origin query repo
	LocalUniqueCommits      int               `json:"-"`                            // commits in local not in remote (left count)
	RemoteUniqueCommits     int               `json:"-"`                            // commits in remote not in local (right count)
	RemoteHostID            string            `json:"remote_host_id,omitempty"`     // Empty for local workspaces
	RemotePath              string            `json:"remote_path,omitempty"`        // Path on remote host
	ConflictOnBranch        *string           `json:"conflict_on_branch,omitempty"` // Branch name where sync conflict was detected
	OverlayManifest         map[string]string `json:"overlay_manifest,omitempty"`   // relPath → SHA-256 hash at copy time
	PortBlock               int               `json:"port_block,omitempty"`         // 0 = unassigned; 1-indexed block for stable preview ports
	Status                  string            `json:"status,omitempty"`
	Tabs                    []Tab             `json:"tabs,omitempty"`
	ResolveConflicts        []ResolveConflict `json:"resolve_conflicts,omitempty"`
}

// Tab represents an accessory tab in a workspace (diff, git, preview, markdown, etc.).
type Tab struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Label     string            `json:"label"`
	Route     string            `json:"route"`
	Closable  bool              `json:"closable"`
	Meta      map[string]string `json:"meta,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

type ResolveConflictStep struct {
	Action             string              `json:"action"`
	Status             string              `json:"status"`
	Message            []string            `json:"message"`
	At                 string              `json:"at"`
	LocalCommit        string              `json:"local_commit,omitempty"`
	LocalCommitMessage string              `json:"local_commit_message,omitempty"`
	Files              []string            `json:"files,omitempty"`
	ConflictDiffs      map[string][]string `json:"conflict_diffs,omitempty"`
	Confidence         string              `json:"confidence,omitempty"`
	Summary            string              `json:"summary,omitempty"`
	Created            *bool               `json:"created,omitempty"`
	TmuxSession        string              `json:"tmux_session,omitempty"`
}

type ResolveConflictResolution struct {
	LocalCommit        string   `json:"local_commit"`
	LocalCommitMessage string   `json:"local_commit_message"`
	AllResolved        bool     `json:"all_resolved"`
	Confidence         string   `json:"confidence"`
	Summary            string   `json:"summary"`
	Files              []string `json:"files"`
}

type ResolveConflict struct {
	Type        string                      `json:"type"`
	WorkspaceID string                      `json:"workspace_id"`
	Status      string                      `json:"status"`
	Hash        string                      `json:"hash"`
	HashMessage string                      `json:"hash_message,omitempty"`
	TmuxSession string                      `json:"tmux_session,omitempty"`
	StartedAt   string                      `json:"started_at"`
	FinishedAt  string                      `json:"finished_at,omitempty"`
	Message     string                      `json:"message,omitempty"`
	Steps       []ResolveConflictStep       `json:"steps"`
	Resolutions []ResolveConflictResolution `json:"resolutions,omitempty"`
}

func copyStringMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func copyStringSlice(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

func copyConflictDiffs(src map[string][]string) map[string][]string {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string][]string, len(src))
	for k, v := range src {
		dst[k] = copyStringSlice(v)
	}
	return dst
}

func copyTabs(src []Tab) []Tab {
	if len(src) == 0 {
		return nil
	}
	dst := make([]Tab, len(src))
	for i, tab := range src {
		dst[i] = tab
		dst[i].Meta = copyStringMap(tab.Meta)
	}
	return dst
}

func copyResolveConflicts(src []ResolveConflict) []ResolveConflict {
	if len(src) == 0 {
		return nil
	}
	dst := make([]ResolveConflict, len(src))
	for i, conflict := range src {
		dst[i] = conflict
		dst[i].Steps = make([]ResolveConflictStep, len(conflict.Steps))
		for j, step := range conflict.Steps {
			dst[i].Steps[j] = step
			dst[i].Steps[j].Message = copyStringSlice(step.Message)
			dst[i].Steps[j].Files = copyStringSlice(step.Files)
			dst[i].Steps[j].ConflictDiffs = copyConflictDiffs(step.ConflictDiffs)
			if step.Created != nil {
				created := *step.Created
				dst[i].Steps[j].Created = &created
			}
		}
		dst[i].Resolutions = make([]ResolveConflictResolution, len(conflict.Resolutions))
		for j, resolution := range conflict.Resolutions {
			dst[i].Resolutions[j] = resolution
			dst[i].Resolutions[j].Files = copyStringSlice(resolution.Files)
		}
	}
	return dst
}

func CopyResolveConflicts(src []ResolveConflict) []ResolveConflict {
	return copyResolveConflicts(src)
}

func copyWorkspace(w Workspace) Workspace {
	w.OverlayManifest = copyStringMap(w.OverlayManifest)
	w.Tabs = copyTabs(w.Tabs)
	w.ResolveConflicts = copyResolveConflicts(w.ResolveConflicts)
	return w
}

// tabDedupKey returns the deduplication key for a tab based on kind.
func tabDedupKey(tab Tab) string {
	switch tab.Kind {
	case "diff", "git":
		return tab.Kind // singleton per workspace
	case "preview":
		return tab.Kind + ":" + tab.Meta["preview_id"]
	case "markdown":
		return tab.Kind + ":" + tab.Meta["filepath"]
	case "resolve-conflict":
		return tab.Kind + ":" + tab.Meta["hash"]
	case "commit":
		return tab.Kind + ":" + tab.Meta["hash"]
	default:
		return tab.Kind + ":" + tab.ID
	}
}

// WorkspacePreview represents a workspace preview proxy mapping.
type WorkspacePreview struct {
	ID              string    `json:"id"`
	WorkspaceID     string    `json:"workspace_id"`
	TargetHost      string    `json:"target_host"`
	TargetPort      int       `json:"target_port"`
	ProxyPort       int       `json:"proxy_port"`
	SourceSessionID string    `json:"source_session_id,omitempty"`
	Status          string    `json:"status,omitempty"` // "ready" | "degraded"
	LastError       string    `json:"last_error,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	LastUsedAt      time.Time `json:"last_used_at"`
	LastHealthyAt   time.Time `json:"last_healthy_at,omitempty"`
	ServerPID       int       `json:"server_pid,omitempty"`
}

type RepoBase struct {
	RepoURL string `json:"repo_url"`
	Path    string `json:"path"`
	VCS     string `json:"vcs,omitempty"`
}

// Session represents a run target session.
type Session struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspace_id"`
	Target       string    `json:"target"`
	Nickname     string    `json:"nickname,omitempty"` // Optional human-friendly name
	PersonaID    string    `json:"persona_id,omitempty"`
	TmuxSession  string    `json:"tmux_session"`
	CreatedAt    time.Time `json:"created_at"`
	Pid          int       `json:"pid"` // PID of the target process from tmux pane
	LastOutputAt time.Time `json:"-"`   // Last time terminal had new output (in-memory only, not persisted)
	LastSignalAt time.Time `json:"-"`   // Last time agent sent a direct signal (in-memory only, not persisted)
	XtermTitle   string    `json:"-"`   // Window title from OSC 0/2 escape sequences (in-memory only, not persisted)
	// NudgeSeq is a monotonic counter for frontend notification dedup.
	// Only incremented by direct agent status events (HandleStatusEvent), NOT by
	// nudgenik polls or manual nudge clears — the UI notification sound
	// should only fire when an agent explicitly requests attention.
	NudgeSeq     uint64 `json:"nudge_seq,omitempty"`
	Nudge        string `json:"nudge,omitempty"`          // NudgeNik consultation result
	RemoteHostID string `json:"remote_host_id,omitempty"` // Empty for local sessions
	RemotePaneID string `json:"remote_pane_id,omitempty"` // tmux pane ID on remote (e.g., "%5")
	RemoteWindow string `json:"remote_window,omitempty"`  // tmux window ID on remote (e.g., "@3")
	Status       string `json:"status,omitempty"`         // "provisioning", "running", "failed", "disposing" (used for all sessions during disposal, remote sessions during lifecycle)
}

// New creates a new empty State instance.
func New(path string, logger *log.Logger) *State {
	return &State{
		Workspaces:  []Workspace{},
		Sessions:    []Session{},
		RepoBases:   []RepoBase{},
		RemoteHosts: []RemoteHost{},
		Previews:    map[string]WorkspacePreview{},
		path:        path,
		logger:      logger,
	}
}

// Load loads the state from the given path.
// Returns an empty state if the file doesn't exist.
func Load(path string, logger *log.Logger) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return New(path, logger), nil
		}
		return nil, fmt.Errorf("failed to read state: %w", err)
	}

	var st State
	st.path = path
	st.logger = logger
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	// Initialize RepoBases if nil (existing state files)
	if st.RepoBases == nil {
		st.RepoBases = []RepoBase{}
	}

	// Initialize RemoteHosts if nil (existing state files)
	if st.RemoteHosts == nil {
		st.RemoteHosts = []RemoteHost{}
	}

	// Migrate: populate ProfileID from deprecated FlavorID.
	// Note: Flavor field cannot be populated here (no config access).
	// Hosts with FlavorID but no Flavor are pre-migration and will be
	// cleaned up when they expire or disconnect.
	for i := range st.RemoteHosts {
		h := &st.RemoteHosts[i]
		if h.ProfileID == "" && h.FlavorID != "" {
			h.ProfileID = h.FlavorID
			h.FlavorID = ""
		}
	}

	// Initialize Previews if nil (existing state files)
	if st.Previews == nil {
		st.Previews = map[string]WorkspacePreview{}
	}

	// Migrate: seed baseline tabs via addTabLocked (dedup-safe).
	if st.logger != nil {
		for _, w := range st.Workspaces {
			st.logger.Info("TAB-DEBUG: Load migration starting", "workspace", w.ID, "existing_tab_count", len(w.Tabs))
			for _, t := range w.Tabs {
				st.logger.Info("TAB-DEBUG: existing tab", "workspace", w.ID, "tab_id", t.ID, "kind", t.Kind, "label", t.Label)
			}
		}
	}
	tabsMigrated := false
	for i := range st.Workspaces {
		w := &st.Workspaces[i]
		if w.Tabs == nil {
			w.Tabs = []Tab{}
		}
		vcs := w.VCS
		if vcs == "" || vcs == "git" || vcs == "git-worktree" || vcs == "git-clone" || vcs == "sapling" {
			now := time.Now()
			// Reverse order: addTabLocked prepends, so git first → diff second → [diff, git]
			if st.addTabLocked(w.ID, Tab{ID: "sys-git-" + w.ID, Kind: "git", Label: "commit graph", Route: "/git/" + w.ID, Closable: false, CreatedAt: now}) == nil {
				tabsMigrated = true
			}
			if st.addTabLocked(w.ID, Tab{ID: "sys-diff-" + w.ID, Kind: "diff", Label: "Diff", Route: "/diff/" + w.ID, Closable: false, CreatedAt: now}) == nil {
				tabsMigrated = true
			}
		}
		for _, preview := range st.Previews {
			if preview.WorkspaceID != w.ID {
				continue
			}
			if st.addTabLocked(w.ID, Tab{
				ID: "sys-preview-" + preview.ID, Kind: "preview",
				Label: fmt.Sprintf("web:%d", preview.TargetPort), Route: "/preview/" + w.ID + "/" + preview.ID,
				Closable: true, Meta: map[string]string{"preview_id": preview.ID}, CreatedAt: preview.CreatedAt,
			}) == nil {
				tabsMigrated = true
			}
		}
	}

	// Reset LastOutputAt for all loaded sessions to avoid treating restored
	// sessions as "recently active" on startup, which would block git status updates.
	for i := range st.Sessions {
		st.Sessions[i].LastOutputAt = time.Time{}
	}

	// Migrate legacy model IDs in session targets
	if migrateSessionTargets(st.Sessions) || tabsMigrated {
		// Best-effort save to persist migration
		_ = st.saveNow()
	}

	return &st, nil
}

// migrateSessionTargets updates legacy model IDs in session targets.
// Returns true if any targets were migrated.
func migrateSessionTargets(sessions []Session) bool {
	changed := false
	for i := range sessions {
		if sessions[i].Target != "" {
			newTarget := detect.MigrateModelID(sessions[i].Target)
			if newTarget != sessions[i].Target {
				sessions[i].Target = newTarget
				changed = true
			}
		}
	}
	return changed
}

// Save saves the state to its configured path immediately.
// Uses atomic write pattern (temp file + rename) to prevent corruption.
// For critical operations that need immediate persistence. For rapid updates,
// consider using SaveBatched() instead to avoid I/O saturation.
func (s *State) Save() error {
	return s.saveNow()
}

// SaveBatched schedules a batched save with 500ms debounce.
// Multiple rapid calls will be coalesced into a single save operation.
// Use this for non-critical state updates during rapid operations (e.g.,
// status transitions during connection) to avoid I/O saturation.
func (s *State) SaveBatched() {
	const batchWindow = 500 * time.Millisecond

	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	// If save already pending, reset the timer
	if s.savePending.Load() {
		if s.saveTimer != nil {
			s.saveTimer.Reset(batchWindow)
		}
		return
	}

	// Mark save as pending
	s.savePending.Store(true)

	// Create timer to save after debounce window
	s.saveTimer = time.AfterFunc(batchWindow, func() {
		s.savePending.Store(false)
		if err := s.saveNow(); err != nil {
			if s.logger != nil {
				s.logger.Error("batched save failed", "err", err)
			}
		}
	})
}

// FlushPending stops any pending SaveBatched timer and performs a synchronous
// save if a batched save was pending. Called during shutdown to prevent data loss
// from a timer firing after the daemon has exited Run().
func (s *State) FlushPending() {
	s.saveMu.Lock()
	if s.saveTimer != nil {
		s.saveTimer.Stop()
	}
	pending := s.savePending.Swap(false)
	s.saveMu.Unlock()
	if pending {
		if err := s.saveNow(); err != nil {
			if s.logger != nil {
				s.logger.Error("flush pending save failed", "err", err)
			}
		}
	}
}

// saveNow performs the actual save operation (internal implementation).
// Copies data under RLock and writes to disk outside the lock to avoid
// holding the lock during I/O.
func (s *State) saveNow() error {
	s.mu.RLock()
	path := s.path
	data, err := json.MarshalIndent(s, "", "  ")
	s.mu.RUnlock()

	if path == "" {
		return fmt.Errorf("state path is empty, cannot save")
	}

	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	if err := fileutil.AtomicWriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	return nil
}

// AddWorkspace adds a workspace to the state.
func (s *State) AddWorkspace(w Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Check for existing entry with same ID (upsert)
	for i, existing := range s.Workspaces {
		if existing.ID == w.ID {
			s.Workspaces[i] = w
			return nil
		}
	}
	// Append first so addTabLocked can find the workspace
	needsSeed := w.Tabs == nil
	if needsSeed {
		w.Tabs = []Tab{}
	}
	s.Workspaces = append(s.Workspaces, w)
	// Seed baseline tabs for VCS-capable workspaces via addTabLocked (dedup-safe)
	if needsSeed {
		vcs := w.VCS
		if vcs == "" || vcs == "git" || vcs == "git-worktree" || vcs == "git-clone" || vcs == "sapling" {
			now := time.Now()
			// Reverse order: addTabLocked prepends, so git first → diff second → [diff, git]
			s.addTabLocked(w.ID, Tab{ID: "sys-git-" + w.ID, Kind: "git", Label: "commit graph", Route: "/git/" + w.ID, Closable: false, CreatedAt: now})
			s.addTabLocked(w.ID, Tab{ID: "sys-diff-" + w.ID, Kind: "diff", Label: "Diff", Route: "/diff/" + w.ID, Closable: false, CreatedAt: now})
		}
	}
	return nil
}

// GetWorkspace returns a workspace by ID.
func (s *State) GetWorkspace(id string) (Workspace, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, w := range s.Workspaces {
		if w.ID == id {
			return copyWorkspace(w), true
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
	for i, w := range s.Workspaces {
		workspaces[i] = copyWorkspace(w)
	}
	return workspaces
}

// FindWorkspaceByRepoBranch returns the first workspace matching the given repo and branch.
func (s *State) FindWorkspaceByRepoBranch(repo, branch string) (Workspace, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, w := range s.Workspaces {
		if w.Repo == repo && w.Branch == branch {
			return copyWorkspace(w), true
		}
	}
	return Workspace{}, false
}

// UpdateWorkspace updates a workspace in the state.
// Returns an error if the workspace is not found.
func (s *State) UpdateWorkspace(w Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.Workspaces {
		if existing.ID == w.ID {
			s.Workspaces[i] = copyWorkspace(w)
			return nil
		}
	}
	return fmt.Errorf("workspace not found: %s", w.ID)
}

// GetWorkspaceTabs returns a copy of the tabs for the given workspace.
func (s *State) GetWorkspaceTabs(workspaceID string) []Tab {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, w := range s.Workspaces {
		if w.ID == workspaceID {
			return copyTabs(w.Tabs)
		}
	}
	return []Tab{}
}

func (s *State) GetWorkspaceResolveConflicts(workspaceID string) []ResolveConflict {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, w := range s.Workspaces {
		if w.ID == workspaceID {
			return copyResolveConflicts(w.ResolveConflicts)
		}
	}
	return []ResolveConflict{}
}

func (s *State) GetResolveConflict(workspaceID, hash string) (ResolveConflict, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, w := range s.Workspaces {
		if w.ID != workspaceID {
			continue
		}
		for _, conflict := range w.ResolveConflicts {
			if conflict.Hash == hash {
				return copyResolveConflicts([]ResolveConflict{conflict})[0], true
			}
		}
		return ResolveConflict{}, false
	}
	return ResolveConflict{}, false
}

func (s *State) UpsertResolveConflict(workspaceID string, conflict ResolveConflict) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, w := range s.Workspaces {
		if w.ID != workspaceID {
			continue
		}
		next := copyResolveConflicts([]ResolveConflict{conflict})[0]
		for j, existing := range w.ResolveConflicts {
			if existing.Hash == conflict.Hash {
				s.Workspaces[i].ResolveConflicts[j] = next
				return nil
			}
		}
		s.Workspaces[i].ResolveConflicts = append([]ResolveConflict{next}, s.Workspaces[i].ResolveConflicts...)
		return nil
	}
	return fmt.Errorf("workspace not found: %s", workspaceID)
}

func (s *State) RemoveResolveConflict(workspaceID, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, w := range s.Workspaces {
		if w.ID != workspaceID {
			continue
		}
		for j, existing := range w.ResolveConflicts {
			if existing.Hash == hash {
				next := make([]ResolveConflict, 0, len(w.ResolveConflicts)-1)
				next = append(next, w.ResolveConflicts[:j]...)
				next = append(next, w.ResolveConflicts[j+1:]...)
				s.Workspaces[i].ResolveConflicts = next
				return nil
			}
		}
		return nil
	}
	return fmt.Errorf("workspace not found: %s", workspaceID)
}

// AddTab adds a tab to a workspace. Idempotent by (kind, dedup_key): if a tab
// with the same dedup key already exists, it is updated in place.
func (s *State) AddTab(workspaceID string, tab Tab) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addTabLocked(workspaceID, tab)
}

// addTabLocked is the lock-free core of AddTab. Caller must hold s.mu.
func (s *State) addTabLocked(workspaceID string, tab Tab) error {
	for i, w := range s.Workspaces {
		if w.ID != workspaceID {
			continue
		}
		key := tabDedupKey(tab)
		for j, existing := range w.Tabs {
			if tabDedupKey(existing) == key {
				s.Workspaces[i].Tabs[j] = tab
				if s.logger != nil {
					s.logger.Info("TAB-DEBUG: updated existing tab", "workspace", workspaceID, "dedup_key", key, "tab_id", tab.ID, "existing_id", existing.ID, "kind", tab.Kind, "label", tab.Label)
				}
				return nil
			}
		}
		s.Workspaces[i].Tabs = append([]Tab{tab}, s.Workspaces[i].Tabs...)
		// if s.logger != nil {
		// 	caller := "unknown"
		// 	if pc, _, _, ok := runtime.Caller(2); ok {
		// 		if fn := runtime.FuncForPC(pc); fn != nil {
		// 			caller = fn.Name()
		// 		}
		// 	}
		// 	s.logger.Info("TAB-DEBUG: added new tab", "workspace", workspaceID, "dedup_key", key, "tab_id", tab.ID, "kind", tab.Kind, "label", tab.Label, "total_tabs", len(s.Workspaces[i].Tabs), "caller", caller)
		// }
		return nil
	}
	return fmt.Errorf("workspace not found: %s", workspaceID)
}

// RemoveTab removes a tab by ID from a workspace.
// Returns an error if the workspace is not found.
func (s *State) RemoveTab(workspaceID, tabID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, w := range s.Workspaces {
		if w.ID != workspaceID {
			continue
		}
		for j, existing := range w.Tabs {
			if existing.ID == tabID {
				next := make([]Tab, 0, len(w.Tabs)-1)
				next = append(next, w.Tabs[:j]...)
				next = append(next, w.Tabs[j+1:]...)
				s.Workspaces[i].Tabs = next
				return nil
			}
		}
		return nil
	}
	return fmt.Errorf("workspace not found: %s", workspaceID)
}

// AddSession adds a session to the state.
func (s *State) AddSession(sess Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Check for existing entry with same ID (upsert)
	for i, existing := range s.Sessions {
		if existing.ID == sess.ID {
			s.Sessions[i] = sess
			return nil
		}
	}
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

// UpdateSessionFunc applies fn to the session with the given ID while holding
// the write lock, preventing lost updates from concurrent modifications.
func (s *State) UpdateSessionFunc(id string, fn func(sess *Session)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sess := range s.Sessions {
		if sess.ID == id {
			fn(&s.Sessions[i])
			return true
		}
	}
	return false
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

// UpdateSessionXtermTitle atomically updates just the XtermTitle field.
// Returns true if the title actually changed.
func (s *State) UpdateSessionXtermTitle(sessionID, title string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Sessions {
		if s.Sessions[i].ID == sessionID {
			if s.Sessions[i].XtermTitle == title {
				return false
			}
			s.Sessions[i].XtermTitle = title
			return true
		}
	}
	return false
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

// IncrementNudgeSeq atomically increments the NudgeSeq counter and returns the new value.
func (s *State) IncrementNudgeSeq(sessionID string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Sessions {
		if s.Sessions[i].ID == sessionID {
			s.Sessions[i].NudgeSeq++
			return s.Sessions[i].NudgeSeq
		}
	}
	return 0
}

// GetNudgeSeq returns the current NudgeSeq for a session.
func (s *State) GetNudgeSeq(sessionID string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sess := range s.Sessions {
		if sess.ID == sessionID {
			return sess.NudgeSeq
		}
	}
	return 0
}

// UpdateSessionNudge atomically updates just the Nudge field for a session.
// Use this instead of UpdateSession when only the nudge needs to change,
// to avoid overwriting concurrent updates to other session fields.
func (s *State) UpdateSessionNudge(sessionID, nudge string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Sessions {
		if s.Sessions[i].ID == sessionID {
			s.Sessions[i].Nudge = nudge
			return nil
		}
	}
	return fmt.Errorf("session not found: %s", sessionID)
}

// ClearSessionNudge atomically clears the Nudge field if it is non-empty.
// Returns true if the nudge was cleared, false if it was already empty.
func (s *State) ClearSessionNudge(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Sessions {
		if s.Sessions[i].ID == sessionID {
			if s.Sessions[i].Nudge == "" {
				return false
			}
			s.Sessions[i].Nudge = ""
			return true
		}
	}
	return false
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
			for previewID, preview := range s.Previews {
				if preview.WorkspaceID == id {
					delete(s.Previews, previewID)
				}
			}
			return nil
		}
	}
	return nil
}

// GetPreviews returns all stored preview mappings.
func (s *State) GetPreviews() []WorkspacePreview {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]WorkspacePreview, 0, len(s.Previews))
	for _, preview := range s.Previews {
		result = append(result, preview)
	}
	return result
}

// GetWorkspacePreviews returns all previews for the given workspace.
func (s *State) GetWorkspacePreviews(workspaceID string) []WorkspacePreview {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]WorkspacePreview, 0)
	for _, preview := range s.Previews {
		if preview.WorkspaceID == workspaceID {
			result = append(result, preview)
		}
	}
	return result
}

// GetPreview returns a preview by ID.
func (s *State) GetPreview(id string) (WorkspacePreview, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	preview, ok := s.Previews[id]
	return preview, ok
}

// FindPreview returns a preview for workspace+target tuple.
func (s *State) FindPreview(workspaceID, targetHost string, targetPort int) (WorkspacePreview, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, preview := range s.Previews {
		if preview.WorkspaceID == workspaceID && preview.TargetHost == targetHost && preview.TargetPort == targetPort {
			return preview, true
		}
	}
	return WorkspacePreview{}, false
}

// UpsertPreview inserts or updates a preview mapping.
func (s *State) UpsertPreview(preview WorkspacePreview) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Previews == nil {
		s.Previews = map[string]WorkspacePreview{}
	}
	s.Previews[preview.ID] = preview
	return nil
}

// RemovePreview removes a preview by ID.
func (s *State) RemovePreview(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Previews, id)
	return nil
}

// RemoveWorkspacePreviews removes all previews for a workspace.
func (s *State) RemoveWorkspacePreviews(workspaceID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for previewID, preview := range s.Previews {
		if preview.WorkspaceID == workspaceID {
			delete(s.Previews, previewID)
			removed++
		}
	}
	return removed
}

// UpdateOverlayManifest updates the overlay manifest for a workspace.
func (s *State) UpdateOverlayManifest(workspaceID string, manifest map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Workspaces {
		if s.Workspaces[i].ID == workspaceID {
			s.Workspaces[i].OverlayManifest = manifest
			return
		}
	}
}

// UpdateOverlayManifestEntry updates a single entry in a workspace's overlay manifest.
func (s *State) UpdateOverlayManifestEntry(workspaceID, relPath, hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Workspaces {
		if s.Workspaces[i].ID == workspaceID {
			if s.Workspaces[i].OverlayManifest == nil {
				s.Workspaces[i].OverlayManifest = make(map[string]string)
			}
			s.Workspaces[i].OverlayManifest[relPath] = hash
			return
		}
	}
}

// GetRepoBases returns all repo bases.
func (s *State) GetRepoBases() []RepoBase {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.RepoBases == nil {
		return []RepoBase{}
	}
	bases := make([]RepoBase, len(s.RepoBases))
	copy(bases, s.RepoBases)
	return bases
}

func (s *State) AddRepoBase(wb RepoBase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Check for existing entry with same URL
	for i, existing := range s.RepoBases {
		if existing.RepoURL == wb.RepoURL {
			// Update existing entry
			s.RepoBases[i] = wb
			return nil
		}
	}
	s.RepoBases = append(s.RepoBases, wb)
	return nil
}

func (s *State) GetRepoBaseByURL(repoURL string) (RepoBase, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, wb := range s.RepoBases {
		if wb.RepoURL == repoURL {
			return wb, true
		}
	}
	return RepoBase{}, false
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

// GetDashboardSXStatus returns a copy of the dashboard.sx status, or nil.
func (s *State) GetDashboardSXStatus() *DashboardSXStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.DashboardSX == nil {
		return nil
	}
	cp := *s.DashboardSX
	return &cp
}

// SetDashboardSXStatus sets the dashboard.sx status.
func (s *State) SetDashboardSXStatus(status *DashboardSXStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.DashboardSX = status
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

// GetRemoteHosts returns a copy of all remote hosts.
func (s *State) GetRemoteHosts() []RemoteHost {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.RemoteHosts == nil {
		return []RemoteHost{}
	}
	result := make([]RemoteHost, len(s.RemoteHosts))
	copy(result, s.RemoteHosts)
	return result
}

// GetRemoteHost returns a remote host by ID.
func (s *State) GetRemoteHost(id string) (RemoteHost, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, rh := range s.RemoteHosts {
		if rh.ID == id {
			return rh, true
		}
	}
	return RemoteHost{}, false
}

// GetRemoteHostByFlavorID returns a remote host by flavor ID.
// DEPRECATED: Use GetRemoteHostByProfileAndFlavor instead.
func (s *State) GetRemoteHostByFlavorID(flavorID string) (RemoteHost, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, rh := range s.RemoteHosts {
		if rh.FlavorID == flavorID {
			return rh, true
		}
	}
	return RemoteHost{}, false
}

// GetRemoteHostsByFlavorID returns all remote hosts matching the given flavor ID.
// DEPRECATED: Use GetRemoteHostsByProfileAndFlavor instead.
func (s *State) GetRemoteHostsByFlavorID(flavorID string) []RemoteHost {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var hosts []RemoteHost
	for _, rh := range s.RemoteHosts {
		if rh.FlavorID == flavorID {
			hosts = append(hosts, rh)
		}
	}
	return hosts
}

// GetRemoteHostByProfileAndFlavor returns the first remote host matching the given profile ID and flavor.
func (s *State) GetRemoteHostByProfileAndFlavor(profileID, flavor string) (RemoteHost, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, rh := range s.RemoteHosts {
		if rh.ProfileID == profileID && rh.Flavor == flavor {
			return rh, true
		}
	}
	return RemoteHost{}, false
}

// GetRemoteHostsByProfileAndFlavor returns all remote hosts matching the given profile ID and flavor.
func (s *State) GetRemoteHostsByProfileAndFlavor(profileID, flavor string) []RemoteHost {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var hosts []RemoteHost
	for _, rh := range s.RemoteHosts {
		if rh.ProfileID == profileID && rh.Flavor == flavor {
			hosts = append(hosts, rh)
		}
	}
	return hosts
}

// GetRemoteHostsByProfileID returns all remote hosts matching the given profile ID.
func (s *State) GetRemoteHostsByProfileID(profileID string) []RemoteHost {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var hosts []RemoteHost
	for _, rh := range s.RemoteHosts {
		if rh.ProfileID == profileID {
			hosts = append(hosts, rh)
		}
	}
	return hosts
}

// GetRemoteHostByHostname returns a remote host by hostname.
func (s *State) GetRemoteHostByHostname(hostname string) (RemoteHost, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, rh := range s.RemoteHosts {
		if rh.Hostname == hostname {
			return rh, true
		}
	}
	return RemoteHost{}, false
}

// AddRemoteHost adds a remote host to state.
func (s *State) AddRemoteHost(rh RemoteHost) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Check for existing entry with same ID
	for i, existing := range s.RemoteHosts {
		if existing.ID == rh.ID {
			// Update existing entry
			s.RemoteHosts[i] = rh
			return nil
		}
	}
	s.RemoteHosts = append(s.RemoteHosts, rh)
	return nil
}

// UpdateRemoteHost updates an existing remote host.
func (s *State) UpdateRemoteHost(rh RemoteHost) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.RemoteHosts {
		if existing.ID == rh.ID {
			s.RemoteHosts[i] = rh
			return nil
		}
	}
	return fmt.Errorf("remote host not found: %s", rh.ID)
}

// UpdateRemoteHostStatus atomically updates just the status of a remote host.
func (s *State) UpdateRemoteHostStatus(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.RemoteHosts {
		if existing.ID == id {
			s.RemoteHosts[i].Status = status
			return nil
		}
	}
	return fmt.Errorf("remote host not found: %s", id)
}

// UpdateRemoteHostProvisioned atomically updates the provisioned status of a remote host.
func (s *State) UpdateRemoteHostProvisioned(id string, provisioned bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.RemoteHosts {
		if existing.ID == id {
			s.RemoteHosts[i].Provisioned = provisioned
			return nil
		}
	}
	return fmt.Errorf("remote host not found: %s", id)
}

// RemoveRemoteHost removes a remote host by ID.
func (s *State) RemoveRemoteHost(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, rh := range s.RemoteHosts {
		if rh.ID == id {
			s.RemoteHosts = append(s.RemoteHosts[:i], s.RemoteHosts[i+1:]...)
			return nil
		}
	}
	return nil
}

// IsRemoteSession returns true if the session is on a remote host.
func (sess *Session) IsRemoteSession() bool {
	return sess.RemoteHostID != ""
}

// IsRemoteWorkspace returns true if the workspace is on a remote host.
func (ws *Workspace) IsRemoteWorkspace() bool {
	return ws.RemoteHostID != ""
}

// GetSessionsByRemoteHostID returns all sessions for a given remote host ID.
func (s *State) GetSessionsByRemoteHostID(hostID string) []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []Session
	for _, sess := range s.Sessions {
		if sess.RemoteHostID == hostID {
			result = append(result, sess)
		}
	}
	return result
}

// GetWorkspacesByRemoteHostID returns all workspaces for a given remote host ID.
func (s *State) GetWorkspacesByRemoteHostID(hostID string) []Workspace {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []Workspace
	for _, w := range s.Workspaces {
		if w.RemoteHostID == hostID {
			result = append(result, w)
		}
	}
	return result
}
