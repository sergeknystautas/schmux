package contracts

// ResolveConflictStep is one step within a conflict resolution process.
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

// ResolveConflictResolution is one resolution within a conflict resolution process.
type ResolveConflictResolution struct {
	LocalCommit        string   `json:"local_commit"`
	LocalCommitMessage string   `json:"local_commit_message"`
	AllResolved        bool     `json:"all_resolved"`
	Confidence         string   `json:"confidence"`
	Summary            string   `json:"summary"`
	Files              []string `json:"files"`
}

// ResolveConflict represents a sync conflict and its resolution state.
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

// RemoteProfileFlavor represents a flavor (machine type) within a remote profile.
type RemoteProfileFlavor struct {
	Flavor           string `json:"flavor"`
	DisplayName      string `json:"display_name,omitempty"`
	WorkspacePath    string `json:"workspace_path,omitempty"`
	ProvisionCommand string `json:"provision_command,omitempty"`
}
