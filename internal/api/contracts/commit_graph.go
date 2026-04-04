package contracts

// CommitGraphResponse represents the API response for GET /api/workspaces/{workspaceId}/commit-graph.
type CommitGraphResponse struct {
	Repo                     string                       `json:"repo"`
	Nodes                    []CommitGraphNode            `json:"nodes"`
	Branches                 map[string]CommitGraphBranch `json:"branches"`
	MainAheadCount           int                          `json:"main_ahead_count"`                      // commits on origin/main ahead of HEAD
	MainAheadNewestTimestamp string                       `json:"main_ahead_newest_timestamp,omitempty"` // timestamp of newest commit ahead on main
	MainAheadNextHash        string                       `json:"main_ahead_next_hash,omitempty"`        // next main commit hash that would be rebased
	LocalTruncated           bool                         `json:"local_truncated,omitempty"`             // true when local branch commits were truncated
	DirtyState               *CommitGraphDirtyState       `json:"dirty_state,omitempty"`
}

// CommitGraphDirtyState represents uncommitted changes in the workspace.
type CommitGraphDirtyState struct {
	FilesChanged int `json:"files_changed"`
	LinesAdded   int `json:"lines_added"`
	LinesRemoved int `json:"lines_removed"`
}

// CommitGraphNode represents a single commit node in the graph.
type CommitGraphNode struct {
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

// CommitGraphBranch represents branch metadata in the graph response.
type CommitGraphBranch struct {
	Head         string   `json:"head"`
	IsMain       bool     `json:"is_main"`
	WorkspaceIDs []string `json:"workspace_ids"`
}
