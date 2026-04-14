package contracts

// SessionResponseItem represents a session in the API response.
type SessionResponseItem struct {
	ID           string `json:"id"`
	Target       string `json:"target"`
	Branch       string `json:"branch"`
	BranchURL    string `json:"branch_url,omitempty"`
	Nickname     string `json:"nickname,omitempty"`
	XtermTitle   string `json:"xterm_title,omitempty"`
	CreatedAt    string `json:"created_at"`
	LastOutputAt string `json:"last_output_at,omitempty"`
	Running      bool   `json:"running"`
	Status       string `json:"status,omitempty"` // "provisioning", "running", "failed" for remote sessions
	AttachCmd    string `json:"attach_cmd"`
	TmuxSocket   string `json:"tmux_socket,omitempty"`
	TmuxSession  string `json:"tmux_session,omitempty"`
	NudgeState   string `json:"nudge_state,omitempty"`
	NudgeSummary string `json:"nudge_summary,omitempty"`
	NudgeSeq     uint64 `json:"nudge_seq,omitempty"`
	// Model metadata (populated when target resolves to a model)
	Model *SessionModelInfo `json:"model,omitempty"`
	// Remote session fields
	RemoteHostID     string `json:"remote_host_id,omitempty"`
	RemotePaneID     string `json:"remote_pane_id,omitempty"`
	RemoteHostname   string `json:"remote_hostname,omitempty"`
	RemoteFlavorName string `json:"remote_flavor_name,omitempty"`
	// Persona fields (denormalized from persona manager at broadcast time)
	PersonaID    string `json:"persona_id,omitempty"`
	PersonaIcon  string `json:"persona_icon,omitempty"`
	PersonaColor string `json:"persona_color,omitempty"`
	PersonaName  string `json:"persona_name,omitempty"`
	// Style ID (from session state)
	StyleID string `json:"style_id,omitempty"`
}

// SessionModelInfo contains model metadata for a session.
type SessionModelInfo struct {
	ContextWindow     int     `json:"context_window,omitempty"`
	CostInputPerMTok  float64 `json:"cost_input_per_mtok,omitempty"`
	CostOutputPerMTok float64 `json:"cost_output_per_mtok,omitempty"`
}

// WorkspaceResponseItem represents a workspace in the API response.
type WorkspaceResponseItem struct {
	ID                      string                `json:"id"`
	Repo                    string                `json:"repo"`
	RepoName                string                `json:"repo_name,omitempty"`
	DefaultBranch           string                `json:"default_branch,omitempty"`
	Branch                  string                `json:"branch"`
	BranchURL               string                `json:"branch_url,omitempty"`
	Path                    string                `json:"path"`
	SessionCount            int                   `json:"session_count"`
	Sessions                []SessionResponseItem `json:"sessions"`
	QuickLaunch             []string              `json:"quick_launch,omitempty"`
	Ahead                   int                   `json:"ahead"`
	Behind                  int                   `json:"behind"`
	LinesAdded              int                   `json:"lines_added"`
	LinesRemoved            int                   `json:"lines_removed"`
	FilesChanged            int                   `json:"files_changed"`
	RemoteHostID            string                `json:"remote_host_id,omitempty"`
	RemoteHostStatus        string                `json:"remote_host_status,omitempty"`
	RemoteFlavorName        string                `json:"remote_flavor_name,omitempty"`
	RemoteFlavor            string                `json:"remote_flavor,omitempty"`
	VCS                     string                `json:"vcs,omitempty"`                // "git", "sapling", etc. Omitted defaults to "git".
	ConflictOnBranch        string                `json:"conflict_on_branch,omitempty"` // Branch where sync conflict was detected
	CommitsSyncedWithRemote bool                  `json:"commits_synced_with_remote"`   // true if local HEAD matches origin/{branch}
	DefaultBranchOrphaned   bool                  `json:"default_branch_orphaned"`      // true if origin/default has no common ancestor with HEAD
	RemoteBranchExists      bool                  `json:"remote_branch_exists"`         // true if origin/{branch} exists
	LocalUniqueCommits      int                   `json:"local_unique_commits"`         // commits in local not in remote
	RemoteUniqueCommits     int                   `json:"remote_unique_commits"`        // commits in remote not in local
	Previews                []PreviewResponse     `json:"previews,omitempty"`
	Tabs                    []Tab                 `json:"tabs"`
	ResolveConflicts        []ResolveConflict     `json:"resolve_conflicts,omitempty"`
	Status                  string                `json:"status,omitempty"`
	Backburner              bool                  `json:"backburner,omitempty"`
	IntentShared            bool                  `json:"intent_shared,omitempty"`
}
