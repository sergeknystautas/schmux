package contracts

// BuildMonitorFailedJob describes one failed CI job surfaced on a unit's
// workflow row.
type BuildMonitorFailedJob struct {
	ID      int64  `json:"id,omitempty"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
}

// BuildMonitorWorkflow is the latest-run snapshot for one active workflow in
// a monitored repo. Mirrors buildmonitor.WorkflowState; we duplicate here so
// the wire contract is independent of the internal monitor package.
type BuildMonitorWorkflow struct {
	Name        string                  `json:"name"`
	Path        string                  `json:"path"`
	RunID       int64                   `json:"run_id,omitempty"`
	RunNumber   int                     `json:"run_number,omitempty"`
	Status      string                  `json:"status,omitempty"`
	Conclusion  string                  `json:"conclusion,omitempty"`
	HTMLURL     string                  `json:"html_url,omitempty"`
	HeadSHA     string                  `json:"head_sha,omitempty"`
	SessionID   string                  `json:"session_id,omitempty"`
	LaunchError string                  `json:"launch_error,omitempty"`
	FailedJobs  []BuildMonitorFailedJob `json:"failed_jobs,omitempty"`
}

// BuildMonitorUnit is one monitored repo's status snapshot.
type BuildMonitorUnit struct {
	Slug     string `json:"slug"`
	RepoName string `json:"repo_name"`
	Repo     string `json:"repo"`
	Branch   string `json:"branch,omitempty"`
	HeadSHA  string `json:"head_sha,omitempty"`
	// Status is the derived head status: "queued" | "in_progress" |
	// "failure" | "success". Absent when no head or no runs.
	Status                 string                 `json:"status,omitempty"`
	Workflows              []BuildMonitorWorkflow `json:"workflows,omitempty"`
	CheckedAt              string                 `json:"checked_at,omitempty"`
	LastError              string                 `json:"last_error,omitempty"`
	Configured             bool                   `json:"configured"`
	GitHubLogin            string                 `json:"github_login,omitempty"`
	RemediationWorkspaceID string                 `json:"remediation_workspace_id,omitempty"`
}

// BuildMonitorResponse is the JSON shape for GET /api/build-monitor.
type BuildMonitorResponse struct {
	Enabled          bool               `json:"enabled"`
	LaunchConfigured bool               `json:"launch_configured"`
	Units            []BuildMonitorUnit `json:"units"`
}
