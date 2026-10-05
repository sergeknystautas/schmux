package contracts

// WorkspaceAttachment identifies an uploaded file available to workspace agents.
type WorkspaceAttachment struct {
	Name string `json:"name"`
	Path string `json:"path"` // absolute path on the schmux server
}

// SpawnAttachment is a file staged for a spawn request before its workspace
// exists. The spawn request references it by ID.
type SpawnAttachment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SpawnImage is an inline image attached to a spawn prompt, the same shape
// the chat composer sends.
type SpawnImage struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"` // base64
}
