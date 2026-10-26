package contracts

// ClientPerformanceSessionRequest selects a tracked workspace, or launches a fresh one.
type ClientPerformanceSessionRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

// ClientPerformanceSessionResponse is the pair that is valid now.
type ClientPerformanceSessionResponse struct {
	WorkspaceID string `json:"workspace_id"`
	SessionID   string `json:"session_id"`
}
