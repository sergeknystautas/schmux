package contracts

// GitGraphResponse represents the API response for GET /api/repos/{repoName}/git-graph.
type GitGraphResponse struct {
	Repo     string                    `json:"repo"`
	Nodes    []GitGraphNode            `json:"nodes"`
	Branches map[string]GitGraphBranch `json:"branches"`
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
