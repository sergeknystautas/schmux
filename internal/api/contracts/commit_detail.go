package contracts

// CommitDetailResponse represents the API response for GET /api/workspaces/{workspaceId}/commit-detail/{hash}.
type CommitDetailResponse struct {
	Hash        string            `json:"hash"`
	ShortHash   string            `json:"short_hash"`
	AuthorName  string            `json:"author_name"`
	AuthorEmail string            `json:"author_email"`
	Timestamp   string            `json:"timestamp"`
	Message     string            `json:"message"`
	Parents     []string          `json:"parents"`
	IsMerge     bool              `json:"is_merge"`
	Files       []DiffFileSummary `json:"files"`
}
