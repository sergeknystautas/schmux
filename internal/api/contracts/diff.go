package contracts

// DiffFileDiff is the per-file structure in a diff response.
type DiffFileDiff struct {
	OldPath      string `json:"old_path,omitempty"`
	NewPath      string `json:"new_path,omitempty"`
	OldContent   string `json:"old_content,omitempty"`
	NewContent   string `json:"new_content,omitempty"`
	Status       string `json:"status,omitempty"`
	LinesAdded   int    `json:"lines_added"`
	LinesRemoved int    `json:"lines_removed"`
	IsBinary     bool   `json:"is_binary"`
}

// DiffResponse is the top-level diff API response.
type DiffResponse struct {
	WorkspaceID string         `json:"workspace_id"`
	Repo        string         `json:"repo"`
	Branch      string         `json:"branch"`
	Files       []DiffFileDiff `json:"files"`
}
