package contracts

// PreviewResponse represents a preview in API responses.
type PreviewResponse struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	TargetHost      string `json:"target_host"`
	TargetPort      int    `json:"target_port"`
	ProxyPort       int    `json:"proxy_port"`
	Status          string `json:"status"`
	LastError       string `json:"last_error,omitempty"`
	ServerPID       int    `json:"server_pid,omitempty"`
	SourceSessionID string `json:"source_session_id,omitempty"`
}
