package contracts

// ClientPerformanceSessionResponse is the pair that is valid now.
type ClientPerformanceSessionResponse struct {
	WorkspaceID string `json:"workspace_id"`
	SessionID   string `json:"session_id"`
}
