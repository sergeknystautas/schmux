package contracts

// GitHubConnectPlanStep is one step of the connect flow's detection plan.
// Step values: "set_origin", "create_repo", "update_config",
// "link_workspaces", "initial_push".
type GitHubConnectPlanStep struct {
	Step   string `json:"step"`
	Needed bool   `json:"needed"`
	Reason string `json:"reason"`
}

// GitHubConnectStatus is the response for GET /api/workspaces/{id}/github-connect.
type GitHubConnectStatus struct {
	Eligible         bool                    `json:"eligible"`
	GH               GitHubStatus            `json:"gh"`
	Owners           []string                `json:"owners,omitempty"` // gh username first, then org logins
	OriginURL        string                  `json:"origin_url,omitempty"`
	RemoteReachable  bool                    `json:"remote_reachable"`
	RemoteHasRefs    bool                    `json:"remote_has_refs"`
	ConfigURLIsLocal bool                    `json:"config_url_is_local"`
	StateRepoIsLocal bool                    `json:"state_repo_is_local"`
	Plan             []GitHubConnectPlanStep `json:"plan"`
	Name             string                  `json:"name"`           // prefill: schmux repo name
	DefaultBranch    string                  `json:"default_branch"` // prefill: "main"
}

// GitHubConnectRequest is the body for POST /api/workspaces/{id}/github-connect.
// Owner/Name/Visibility are ignored when repo creation is not in the plan.
type GitHubConnectRequest struct {
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	Visibility    string `json:"visibility"`     // "private" (default) | "public"
	DefaultBranch string `json:"default_branch"` // defaults to "main"
}

// GitHubConnectStepResult reports one executed step.
// Status values: "done", "skipped", "failed", "not_run".
type GitHubConnectStepResult struct {
	Step   string `json:"step"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// GitHubConnectResult is the response for POST /api/workspaces/{id}/github-connect.
type GitHubConnectResult struct {
	Success bool                      `json:"success"`
	RepoURL string                    `json:"repo_url,omitempty"`
	Steps   []GitHubConnectStepResult `json:"steps"`
}
