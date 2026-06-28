package contracts

// SpawnLogResult is the synchronous per-target outcome of a spawn attempt — the
// persisted subset of the dashboard's internal SessionResult.
type SpawnLogResult struct {
	Target      string `json:"target,omitempty"`
	Command     string `json:"command,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Error       string `json:"error,omitempty"`
}

// SpawnLogRecord is one line in ~/.schmux/logs/spawn.jsonl: a resolved spawn
// request plus its synchronous per-target outcome. Status is "ok" (all results
// succeeded), "failed" (all errored or none), or "partial" (mixed).
type SpawnLogRecord struct {
	TS              string           `json:"ts"`
	Repo            string           `json:"repo,omitempty"`
	Branch          string           `json:"branch,omitempty"`
	WorkspaceID     string           `json:"workspace_id,omitempty"`
	Targets         map[string]int   `json:"targets,omitempty"`
	Command         string           `json:"command,omitempty"`
	Nickname        string           `json:"nickname,omitempty"`
	Fence           bool             `json:"fence,omitempty"`
	Resume          bool             `json:"resume,omitempty"`
	RemoteProfileID string           `json:"remote_profile_id,omitempty"`
	RemoteFlavor    string           `json:"remote_flavor,omitempty"`
	Prompt          string           `json:"prompt,omitempty"`
	Status          string           `json:"status"`
	Results         []SpawnLogResult `json:"results"`
}
