package contracts

// ClientPerformanceSessionRequest carries the ids the recording browser kept
// from its last call, both empty the first time.
type ClientPerformanceSessionRequest struct {
	WorkspaceID string `json:"workspace_id"`
	SessionID   string `json:"session_id"`
}

// ClientPerformanceSessionResponse is the pair that is valid now.
type ClientPerformanceSessionResponse struct {
	WorkspaceID string `json:"workspace_id"`
	SessionID   string `json:"session_id"`
}
