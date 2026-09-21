package contracts

// WorkspaceAttachment identifies an uploaded file available to workspace agents.
type WorkspaceAttachment struct {
	Name string `json:"name"`
	Path string `json:"path"` // absolute path on the schmux server
}
