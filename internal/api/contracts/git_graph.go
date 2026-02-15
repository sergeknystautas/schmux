package contracts

// GitGraphResponse represents the API response for GET /api/workspaces/{workspaceId}/git-graph.
type GitGraphResponse struct {
	Repo                     string                    `json:"repo"`
	Nodes                    []GitGraphNode            `json:"nodes"`
	Branches                 map[string]GitGraphBranch `json:"branches"`
	MainAheadCount           int                       `json:"main_ahead_count"`                      // commits on origin/main ahead of HEAD
	MainAheadNewestTimestamp string                    `json:"main_ahead_newest_timestamp,omitempty"` // timestamp of newest commit ahead on main
	LocalTruncated           bool                      `json:"local_truncated,omitempty"`             // true when local branch commits were truncated
	DirtyState               *GitGraphDirtyState       `json:"dirty_state,omitempty"`
}

// GitGraphDirtyState represents uncommitted changes in the workspace.
type GitGraphDirtyState struct {
	FilesChanged int `json:"files_changed"`
	LinesAdded   int `json:"lines_added"`
	LinesRemoved int `json:"lines_removed"`
}

// GitGraphNode represents a single commit node in the graph.
type GitGraphNode struct {
	Hash         string   `json:"hash"`
	ShortHash    string   `json:"short_hash"`
	Message      string   `json:"message"`
	Author       string   `json:"author"`
	Timestamp    string   `json:"timestamp"`
	Parents      []string `json:"parents"`
	Branches     []string `json:"branches"`
	IsHead       []string `json:"is_head"`
	WorkspaceIDs []string `json:"workspace_ids"`
}

// GitGraphBranch represents branch metadata in the graph response.
type GitGraphBranch struct {
	Head         string   `json:"head"`
	IsMain       bool     `json:"is_main"`
	WorkspaceIDs []string `json:"workspace_ids"`
}
