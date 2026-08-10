package contracts

// DiffFileSummary is the per-file metadata in a diff list response.
// Status values: added, modified, deleted, renamed, untracked, copied
// ("copied" is produced only by the commit-detail endpoint, whose -C diff
// detects copies; the working-tree list never emits it).
type DiffFileSummary struct {
	OldPath      string `json:"old_path,omitempty"`
	NewPath      string `json:"new_path,omitempty"`
	Status       string `json:"status,omitempty"`
	LinesAdded   int    `json:"lines_added"`
	LinesRemoved int    `json:"lines_removed"`
	IsBinary     bool   `json:"is_binary"`
}

// DiffResponse is the top-level diff list API response.
type DiffResponse struct {
	WorkspaceID string            `json:"workspace_id"`
	Repo        string            `json:"repo"`
	Branch      string            `json:"branch"`
	Files       []DiffFileSummary `json:"files"`
}

// DiffFileContentResponse is the response for GET /api/diff-file/{workspaceId}.
// A side that has no content at the requested revision (added files have no
// old side, deleted files no new side) is an empty string — the client
// interprets emptiness via the status it already has from the list response.
type DiffFileContentResponse struct {
	WorkspaceID string `json:"workspace_id"`
	Path        string `json:"path"`
	OldContent  string `json:"old_content"`
	NewContent  string `json:"new_content"`
}
