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
	"github.com/sergeknystautas/schmux/internal/fileutil"
	"github.com/sergeknystautas/schmux/internal/jsoncompat"
	"github.com/sergeknystautas/schmux/internal/types"
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
	Tabs         []Tab                       `json:"tabs,omitempty"`
	RepoBases    []RepoBase                  `json:"base_repos,omitempty"`
	Repos        []Repo                      `json:"repos"`
	PullRequests []contracts.PullRequest     `json:"pull_requests,omitempty"` // cached GitHub PRs
	NeedsRestart bool                        `json:"needs_restart,omitempty"` // true if daemon needs restart for config changes to take effect
	Previews     map[string]WorkspacePreview `json:"previews,omitempty"`      // persisted preview mappings (proxy port must survive restart)
	DashboardSX  *DashboardSXStatus          `json:"dashboard_sx,omitempty"`
	path         string                      // path to the state file
	logger       *log.Logger
	mu           sync.RWMutex

	// Batched save support (Issue 6 fix)
	savePending atomic.Bool // True if a save is scheduled
	saveMu      sync.Mutex  // Protects save timer
	saveTimer   *time.Timer // Timer for batched saves
	extraFields jsoncompat.Fields
}

// Session status constants.
const (
	SessionStatusDisposing = "disposing"
)

// Workspace status constants.
const (
	WorkspaceStatusProvisioning = "provisioning"
	WorkspaceStatusRunning      = "running"
	WorkspaceStatusFailed       = "failed"
	WorkspaceStatusDisposing    = "disposing"
)

// SchmuxDataDir returns the schmux data directory within a workspace.
// For sapling repos, uses .sl/schmux/ to avoid working tree pollution. Sapling
// treats the literal name .sl/ as a control-dir name and excludes it from
// `sl status` even when the actual control dir is .hg/ (mercurial-compat mode).
// For git repos, uses .schmux/ (hidden via .git/info/exclude).
// Use this when VCS type is unknown and the path is local (can be stat'd).
func SchmuxDataDir(workspacePath string) string {
	if isSaplingWorkspace(workspacePath) {
		return filepath.Join(workspacePath, ".sl", "schmux")
	}
	return filepath.Join(workspacePath, ".schmux")
}

// isSaplingWorkspace reports whether workspacePath is a sapling working copy.
// Sapling repos may use either .sl/ (sapling-native) or .hg/ (mercurial-compat
// mode, used when sapling is configured to share storage with mercurial-style
// tooling) as the control directory. Either may be a regular directory or a
// symlink (e.g. when the working copy is backed by a virtual filesystem).
func isSaplingWorkspace(workspacePath string) bool {
	for _, name := range []string{".sl", ".hg"} {
		if info, err := os.Stat(filepath.Join(workspacePath, name)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// SchmuxDataDirRelative returns the VCS-appropriate relative directory name
// for schmux workspace data.
func SchmuxDataDirRelative(vcs string) string {
	if vcs == "sapling" {
		return filepath.Join(".sl", "schmux")
	}
	return ".schmux"
}

// Workspace represents a workspace directory state.
// Multiple sessions can share the same workspace (multi-agent per directory).
type Workspace struct {
	ID                      string            `json:"id"`
	Repo                    string            `json:"repo"`
	Branch                  string            `json:"branch"`
	Path                    string            `json:"path"`
	VCS                     string            `json:"vcs,omitempty"`
	Label                   string            `json:"label,omitempty"` // Optional human-friendly display label (used by sapling workspaces today)
	Dirty                   bool              `json:"-"`
	Ahead                   int               `json:"-"`
	Behind                  int               `json:"-"`
	LinesAdded              int               `json:"-"`
	LinesRemoved            int               `json:"-"`
	FilesChanged            int               `json:"-"`
	CommitsSyncedWithRemote bool              `json:"-"`                            // true if local HEAD matches origin/{branch}
	DefaultBranchOrphaned   bool              `json:"-"`                            // true if origin/default has no common ancestor with HEAD
	RemoteBranchExists      bool              `json:"-"`                            // true if branch ref exists on any remote
	RemoteBranchIsFork      bool              `json:"-"`                            // true if remote branch is on a non-origin remote (fork)
	LocalUniqueCommits      int               `json:"-"`                            // commits in local not in remote (left count)
	RemoteUniqueCommits     int               `json:"-"`                            // commits in remote not in local (right count)
	RemoteHeadSHA           string            `json:"-"`                            // commit SHA of the remote branch head ("" when no remote branch)
	RemoteBranchURL         string            `json:"-"`                            // fork remote URL when RemoteBranchIsFork ("" otherwise)
	ConflictOnBranch        *string           `json:"conflict_on_branch,omitempty"` // Branch name where sync conflict was detected
	OverlayManifest         map[string]string `json:"overlay_manifest,omitempty"`   // relPath → SHA-256 hash at copy time
	PortBlock               int               `json:"port_block,omitempty"`         // 0 = unassigned; 1-indexed block for stable preview ports
	Status                  string            `json:"status,omitempty"`
	ResolveConflicts        []ResolveConflict `json:"resolve_conflicts,omitempty"`
	Backburner              bool              `json:"backburner,omitempty"`
	IntentShared            bool              `json:"intent_shared,omitempty"`
	// OwnerFlightID names the flight that took off in this workspace. It is
	// stamped once by the flight system at takeoff and never cleared; the
	// workspace record's removal removes it.
	OwnerFlightID string `json:"owner_flight_id,omitempty"`
	// Dedicated names the dedicated workspace this is; stamped only on a
	// workspace schmux created for that name; the record's removal removes it.
	Dedicated string    `json:"dedicated,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// Tab represents an accessory tab in a workspace (diff, git, preview, markdown, etc.).
type Tab struct {
	ID          string            `json:"id"`
	WorkspaceID string            `json:"workspace_id"`
	Kind        string            `json:"kind"`
	Label       string            `json:"label"`
	Route       string            `json:"route"`
	Closable    bool              `json:"closable"`
	Meta        map[string]string `json:"meta,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

// ResolveConflictStep is a type alias for contracts.ResolveConflictStep.
type ResolveConflictStep = contracts.ResolveConflictStep

// ResolveConflictResolution is a type alias for contracts.ResolveConflictResolution.
type ResolveConflictResolution = contracts.ResolveConflictResolution

// ResolveConflict is a type alias for contracts.ResolveConflict.
type ResolveConflict = contracts.ResolveConflict

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
	case "markdown", "html":
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

// SessionKindChat marks a session driven over Claude's stream-json protocol
// and rendered as a chat instead of a terminal. Empty Kind means terminal.
const SessionKindChat = "chat"

// Session represents a run target session.
type Session struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspace_id"`
	Target       string    `json:"target"`
	Nickname     string    `json:"nickname,omitempty"` // Optional human-friendly name
	PersonaID    string    `json:"persona_id,omitempty"`
	StyleID      string    `json:"style_id,omitempty"`
	TmuxSession  string    `json:"tmux_session"`
	TmuxSocket   string    `json:"tmux_socket,omitempty"` // Socket name at creation time (empty = "default" for pre-isolation sessions)
	CreatedAt    time.Time `json:"created_at"`
	Pid          int       `json:"pid"` // PID of the target process from tmux pane
	LastOutputAt time.Time `json:"-"`   // Terminal output or headless chat activity (in-memory only)
	LastSignalAt time.Time `json:"-"`   // Last time agent sent a direct signal (in-memory only, not persisted)
	XtermTitle   string    `json:"-"`   // Window title from OSC 0/2 escape sequences (in-memory only, not persisted)
	// NudgeSeq is a monotonic counter for frontend notification dedup.
	// Incremented by direct agent status events and changed headless chat Nudges,
	// NOT by nudgenik polls, activity timestamps, or manual nudge clears.
	NudgeSeq uint64 `json:"nudge_seq,omitempty"`
	Nudge    string `json:"nudge,omitempty"`  // Waiting-for state, summary, and source JSON
	Status   string `json:"status,omitempty"` // "disposing" while the session is being torn down; empty otherwise
	Fence    bool   `json:"fence,omitempty"`  // True if spawned inside the fence sandbox (set once at spawn)
	// ResumeID is the harness-native conversation id (Claude session_id /
	// OpenCode session id), captured via hooks. Empty until captured. Enables
	// the Restart action.
	ResumeID string `json:"resume_id,omitempty"`
	// Kind is "" for terminal sessions or SessionKindChat.
	Kind string `json:"kind,omitempty"`
	// ChatProtocol is the wire dialect the chat session's harness process
	// speaks (internal/chat protocol name), fixed at spawn from the target's
	// descriptor. Empty on chat sessions spawned before the field existed,
	// when claude-stream-json was the only protocol; see EffectiveChatProtocol.
	ChatProtocol string `json:"chat_protocol,omitempty"`
	// SignedOut is true when the session's harness login is known absent:
	// set by the session's own failed turn (sign-out statement matcher) or
	// the harness status tool, cleared by the status tool. Chat sessions
	// and sign-in helpers. See docs/chat-sessions.md, Signed-out
	// recovery.
	SignedOut bool `json:"signed_out,omitempty"`
	// SignInProtocol marks a local terminal session as a sign-in helper for
	// the given harness protocol (e.g. "claude-stream-json" or
	// "codex-app-server"). Presence is the helper identity; the field
	// value is the protocol key consumed by authcheck.Run. Chat sessions
	// and ordinary terminals leave this empty. See
	// docs/chat-sessions.md, Sign-in helper.
	SignInProtocol string `json:"sign_in_protocol,omitempty"`
}

// DefaultChatProtocol is the protocol of chat sessions persisted before
// ChatProtocol existed. Must equal internal/chat.ProtocolClaude.
const DefaultChatProtocol = "claude-stream-json"

// EffectiveChatProtocol returns the chat protocol a chat session's process
// speaks, applying the pre-field default.
func (s Session) EffectiveChatProtocol() string {
	if s.ChatProtocol == "" {
		return DefaultChatProtocol
	}
	return s.ChatProtocol
}

// New creates a new empty State instance.
func New(path string, logger *log.Logger) *State {
	return &State{
		Workspaces: []Workspace{},
		Sessions:   []Session{},
		Tabs:       []Tab{},
		RepoBases:  []RepoBase{},
		Repos:      []Repo{},
		Previews:   map[string]WorkspacePreview{},
		path:       path,
		logger:     logger,
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
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state fields: %w", err)
	}
	st.extraFields = jsoncompat.UnknownFields(top, State{})
	// public_repos held PR discovery's visibility list, which was removed.
	delete(st.extraFields, "public_repos")

	// Migrate legacy nested tabs: unmarshal workspace tabs from old format
	// and lift them to top-level State.Tabs with dedup.
	type workspaceWithTabs struct {
		ID   string `json:"id"`
		Tabs []Tab  `json:"tabs,omitempty"`
	}
	var legacy struct {
		Workspaces []workspaceWithTabs `json:"workspaces"`
	}
	// Best-effort: if this fails, we just skip migration (no legacy tabs)
	if json.Unmarshal(data, &legacy) == nil {
		// Build dedup index from top-level tabs (new format wins)
		seen := make(map[string]bool, len(st.Tabs))
		for _, t := range st.Tabs {
			seen[t.WorkspaceID+"\x00"+tabDedupKey(t)] = true
		}
		// Sweep nested tabs, skipping any already present at top level
		for _, ws := range legacy.Workspaces {
			for _, tab := range ws.Tabs {
				tab.WorkspaceID = ws.ID
				key := tab.WorkspaceID + "\x00" + tabDedupKey(tab)
				if !seen[key] {
					st.Tabs = append(st.Tabs, tab)
					seen[key] = true
				}
			}
		}
	}

	// Initialize Tabs if nil (new state files without tabs)
	if st.Tabs == nil {
		st.Tabs = []Tab{}
	}

	// Initialize RepoBases if nil (existing state files)
	if st.RepoBases == nil {
		st.RepoBases = []RepoBase{}
	}

	// Initialize Repos if nil (existing state files)
	if st.Repos == nil {
		st.Repos = []Repo{}
	}

	// Initialize Previews if nil (existing state files)
	if st.Previews == nil {
		st.Previews = map[string]WorkspacePreview{}
	}

	// Remote workspaces are no longer supported. This runs after the nested-tabs
	// lift above so a lifted tab of a dropped workspace goes with it.
	dropped, err := dropRemoteRecords(&st, data)
	if err != nil {
		return nil, fmt.Errorf("failed to read remote records: %w", err)
	}
	if dropped > 0 {
		if logger != nil {
			logger.Printf("[state] dropped %d remote workspace, session, and host records (remote workspaces are no longer supported)", dropped)
		}
		// Best-effort save: the migration runs on every load, so a failed
		// save does not bring the records back.
		_ = st.saveNow()
	}

	// Reset LastOutputAt for all loaded sessions to avoid treating restored
	// sessions as "recently active" on startup, which would block git status updates.
	for i := range st.Sessions {
		st.Sessions[i].LastOutputAt = time.Time{}
	}

	// Migrate legacy model IDs in session targets
	if migrateSessionTargets(st.Sessions) {
		// Best-effort save to persist migration
		_ = st.saveNow()
	}

	return &st, nil
}

// dropRemoteRecords removes the workspaces, sessions, tabs, and hosts that the
// removed remote workspace feature left in state. A remote workspace stores its
// remote path in Path, so a record left in place could make a local code path
// act on a local directory. It reads the raw JSON because the fields are gone
// from the structs, and it never touches a workspace directory. A decode error
// is returned rather than skipped: a skipped remote record would become a local
// one. Returns the number of workspaces, sessions, and hosts dropped.
func dropRemoteRecords(st *State, data []byte) (dropped int, err error) {
	var legacy struct {
		Workspaces []struct {
			ID           string `json:"id"`
			RemoteHostID string `json:"remote_host_id"`
		} `json:"workspaces"`
		Sessions []struct {
			ID           string `json:"id"`
			WorkspaceID  string `json:"workspace_id"`
			RemoteHostID string `json:"remote_host_id"`
		} `json:"sessions"`
		RemoteHosts []json.RawMessage `json:"remote_hosts"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return 0, err
	}

	droppedWorkspaces := make(map[string]bool)
	for _, ws := range legacy.Workspaces {
		if ws.RemoteHostID == "" {
			continue
		}
		droppedWorkspaces[ws.ID] = true
		_ = st.RemoveWorkspace(ws.ID)
		dropped++
	}
	for _, sess := range legacy.Sessions {
		if sess.RemoteHostID == "" && !droppedWorkspaces[sess.WorkspaceID] {
			continue
		}
		_ = st.RemoveSession(sess.ID)
		dropped++
	}
	dropped += len(legacy.RemoteHosts)
	delete(st.extraFields, "remote_hosts")
	return dropped, nil
}

// migrateSessionTargets updates legacy model IDs in session targets.
// Returns true if any targets were migrated.
func migrateSessionTargets(sessions []Session) bool {
	changed := false
	for i := range sessions {
		if sessions[i].Target != "" {
			newTarget := types.MigrateModelID(sessions[i].Target)
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
	data, err := jsoncompat.MarshalIndentWithFields(s, s.extraFields, "", "  ")
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
	s.Workspaces = append(s.Workspaces, w)
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

// FindOwnedWorkspace returns the first workspace in the slice whose
// OwnerFlightID equals flightID. An empty flightID never matches.
func FindOwnedWorkspace(workspaces []Workspace, flightID string) (Workspace, bool) {
	if flightID == "" {
		return Workspace{}, false
	}
	for _, w := range workspaces {
		if w.OwnerFlightID == flightID {
			return w, true
		}
	}
	return Workspace{}, false
}

// FindDedicatedWorkspace returns the first workspace in the slice whose
// Dedicated name equals name. An empty name never matches.
func FindDedicatedWorkspace(workspaces []Workspace, name string) (Workspace, bool) {
	if name == "" {
		return Workspace{}, false
	}
	for _, w := range workspaces {
		if w.Dedicated == name {
			return w, true
		}
	}
	return Workspace{}, false
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
	var matched []Tab
	for _, t := range s.Tabs {
		if t.WorkspaceID == workspaceID {
			matched = append(matched, t)
		}
	}
	if result := copyTabs(matched); result != nil {
		return result
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
	// Validate workspace exists
	found := false
	for _, w := range s.Workspaces {
		if w.ID == workspaceID {
			found = true
			break
		}
	}
	if !found {
		if s.logger != nil {
			s.logger.Warn("AddTab: workspace not found", "workspace", workspaceID, "tab", tab.ID, "kind", tab.Kind)
		}
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}
	tab.WorkspaceID = workspaceID
	key := tabDedupKey(tab)
	for i, existing := range s.Tabs {
		if existing.WorkspaceID == workspaceID && tabDedupKey(existing) == key {
			if s.logger != nil {
				s.logger.Debug("AddTab: updated existing", "workspace", workspaceID, "tab", tab.ID, "kind", tab.Kind, "dedup_key", key)
			}
			s.Tabs[i] = tab
			return nil
		}
	}
	if s.logger != nil {
		s.logger.Debug("AddTab: created", "workspace", workspaceID, "tab", tab.ID, "kind", tab.Kind)
	}
	s.Tabs = append([]Tab{tab}, s.Tabs...)
	return nil
}

// RemoveTab removes a tab by ID from a workspace.
// Returns an error if the workspace is not found.
func (s *State) RemoveTab(workspaceID, tabID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for _, w := range s.Workspaces {
		if w.ID == workspaceID {
			found = true
			break
		}
	}
	if !found {
		if s.logger != nil {
			s.logger.Warn("RemoveTab: workspace not found", "workspace", workspaceID, "tab", tabID)
		}
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}
	for i, t := range s.Tabs {
		if t.WorkspaceID == workspaceID && t.ID == tabID {
			if s.logger != nil {
				s.logger.Info("RemoveTab: removed", "workspace", workspaceID, "tab", tabID, "kind", t.Kind)
			}
			s.Tabs = append(s.Tabs[:i], s.Tabs[i+1:]...)
			return nil
		}
	}
	return nil
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

// UpdateSessionResumeID sets the harness-native conversation id on a session,
// in memory. Returns true only when the value changed (so callers persist and
// broadcast just once). Persistence/broadcast are the caller's responsibility,
// matching the other in-memory session setters.
func (s *State) UpdateSessionResumeID(sessionID, resumeID string) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Sessions {
		if s.Sessions[i].ID == sessionID {
			if s.Sessions[i].ResumeID == resumeID {
				return false
			}
			s.Sessions[i].ResumeID = resumeID
			return true
		}
	}
	return false
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
			// Cascade: remove tabs belonging to this workspace
			filtered := s.Tabs[:0]
			for _, t := range s.Tabs {
				if t.WorkspaceID != id {
					filtered = append(filtered, t)
				}
			}
			s.Tabs = filtered
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

// RemoveRepoBase removes the repo base associated with repoURL from state.
func (s *State) RemoveRepoBase(repoURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, wb := range s.RepoBases {
		if wb.RepoURL == repoURL {
			s.RepoBases = append(s.RepoBases[:i], s.RepoBases[i+1:]...)
			return nil
		}
	}
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

// IsChat reports whether this is a chat-kind session.
func (sess *Session) IsChat() bool { return sess.Kind == SessionKindChat }
