package contracts

// CommitDetailResponse represents the API response for GET /api/workspaces/{workspaceId}/commit-detail/{hash}.
type CommitDetailResponse struct {
	Hash        string     `json:"hash"`
	ShortHash   string     `json:"short_hash"`
	AuthorName  string     `json:"author_name"`
	AuthorEmail string     `json:"author_email"`
	Timestamp   string     `json:"timestamp"`
	Message     string     `json:"message"`
	Parents     []string   `json:"parents"`
	IsMerge     bool       `json:"is_merge"`
	Files       []FileDiff `json:"files"`
}

// FileDiff represents a single file's diff in a commit.
// This struct is shared with the diff endpoint.
type FileDiff struct {
	OldPath      string `json:"old_path,omitempty"`
	NewPath      string `json:"new_path,omitempty"`
	OldContent   string `json:"old_content,omitempty"`
	NewContent   string `json:"new_content,omitempty"`
	Status       string `json:"status,omitempty"` // added, modified, deleted, renamed
	LinesAdded   int    `json:"lines_added"`
	LinesRemoved int    `json:"lines_removed"`
	IsBinary     bool   `json:"is_binary"`
}
