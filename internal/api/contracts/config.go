package contracts

// Repo represents a git repository configuration.
type Repo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// RunTarget represents a user-supplied run target.
type RunTarget struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Command string `json:"command"`
	Source  string `json:"source,omitempty"`
}

// QuickLaunch represents a saved run preset.
type QuickLaunch struct {
	Name   string  `json:"name"`
	Target string  `json:"target"`
	Prompt *string `json:"prompt"`
}

// Variant represents a variant config entry.
type Variant struct {
	Name    string            `json:"name"`
	Enabled *bool             `json:"enabled,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// AvailableVariant represents a detected variant with metadata and configuration status.
type AvailableVariant struct {
	Name            string   `json:"name"`
	DisplayName     string   `json:"display_name"`
	BaseTool        string   `json:"base_tool"`
	RequiredSecrets []string `json:"required_secrets"`
	UsageURL        string   `json:"usage_url"`
	Configured      bool     `json:"configured"`
}

// Terminal represents terminal dimensions.
type Terminal struct {
	Width          int `json:"width"`
	Height         int `json:"height"`
	SeedLines      int `json:"seed_lines"`
	BootstrapLines int `json:"bootstrap_lines"`
}

// Nudgenik represents NudgeNik configuration.
type Nudgenik struct {
	Target         string `json:"target,omitempty"`
	ViewedBufferMs int    `json:"viewed_buffer_ms"`
	SeenIntervalMs int    `json:"seen_interval_ms"`
}

// BranchSuggest represents branch name suggestion configuration.
type BranchSuggest struct {
	Target string `json:"target,omitempty"`
}

// Sessions represents session and git-related timing configuration.
type Sessions struct {
	DashboardPollIntervalMs int `json:"dashboard_poll_interval_ms"`
	GitStatusPollIntervalMs int `json:"git_status_poll_interval_ms"`
	GitCloneTimeoutMs       int `json:"git_clone_timeout_ms"`
	GitStatusTimeoutMs      int `json:"git_status_timeout_ms"`
}

// Xterm represents terminal capture, timeouts, and log rotation settings.
type Xterm struct {
	MtimePollIntervalMs int `json:"mtime_poll_interval_ms"`
	QueryTimeoutMs      int `json:"query_timeout_ms"`
	OperationTimeoutMs  int `json:"operation_timeout_ms"`
	MaxLogSizeMB        int `json:"max_log_size_mb,omitempty"`
	RotatedLogSizeMB    int `json:"rotated_log_size_mb,omitempty"`
}

// Network controls server binding and TLS.
type Network struct {
	BindAddress   string `json:"bind_address"`
	Port          int    `json:"port"`
	PublicBaseURL string `json:"public_base_url"`
	TLS           *TLS   `json:"tls,omitempty"`
}

// TLS holds TLS cert paths.
type TLS struct {
	CertPath string `json:"cert_path"`
	KeyPath  string `json:"key_path"`
}

// AccessControl controls authentication.
type AccessControl struct {
	Enabled           bool   `json:"enabled"`
	Provider          string `json:"provider"`
	SessionTTLMinutes int    `json:"session_ttl_minutes"`
}

// ConfigResponse represents the API response for GET /api/config.
type ConfigResponse struct {
	WorkspacePath string        `json:"workspace_path"`
	Repos         []Repo        `json:"repos"`
	RunTargets    []RunTarget   `json:"run_targets"`
	QuickLaunch   []QuickLaunch `json:"quick_launch"`
	Variants      []Variant     `json:"variants,omitempty"`
	Terminal      Terminal      `json:"terminal"`
	Nudgenik      Nudgenik      `json:"nudgenik"`
	BranchSuggest BranchSuggest `json:"branch_suggest"`
	Sessions      Sessions      `json:"sessions"`
	Xterm         Xterm         `json:"xterm"`
	Network       Network       `json:"network"`
	AccessControl AccessControl `json:"access_control"`
	NeedsRestart  bool          `json:"needs_restart"`
}

// TerminalUpdate represents partial terminal updates.
type TerminalUpdate struct {
	Width          *int `json:"width,omitempty"`
	Height         *int `json:"height,omitempty"`
	SeedLines      *int `json:"seed_lines,omitempty"`
	BootstrapLines *int `json:"bootstrap_lines,omitempty"`
}

// NudgenikUpdate represents partial nudgenik updates.
type NudgenikUpdate struct {
	Target         *string `json:"target,omitempty"`
	ViewedBufferMs *int    `json:"viewed_buffer_ms,omitempty"`
	SeenIntervalMs *int    `json:"seen_interval_ms,omitempty"`
}

// BranchSuggestUpdate represents partial branch suggest updates.
type BranchSuggestUpdate struct {
	Target *string `json:"target,omitempty"`
}

// SessionsUpdate represents partial session timing updates.
type SessionsUpdate struct {
	DashboardPollIntervalMs *int `json:"dashboard_poll_interval_ms,omitempty"`
	GitStatusPollIntervalMs *int `json:"git_status_poll_interval_ms,omitempty"`
	GitCloneTimeoutMs       *int `json:"git_clone_timeout_ms,omitempty"`
	GitStatusTimeoutMs      *int `json:"git_status_timeout_ms,omitempty"`
}

// XtermUpdate represents partial xterm updates.
type XtermUpdate struct {
	MtimePollIntervalMs *int `json:"mtime_poll_interval_ms,omitempty"`
	QueryTimeoutMs      *int `json:"query_timeout_ms,omitempty"`
	OperationTimeoutMs  *int `json:"operation_timeout_ms,omitempty"`
	MaxLogSizeMB        *int `json:"max_log_size_mb,omitempty"`
	RotatedLogSizeMB    *int `json:"rotated_log_size_mb,omitempty"`
}

// NetworkUpdate represents partial network updates.
type NetworkUpdate struct {
	BindAddress   *string    `json:"bind_address,omitempty"`
	Port          *int       `json:"port,omitempty"`
	PublicBaseURL *string    `json:"public_base_url,omitempty"`
	TLS           *TLSUpdate `json:"tls,omitempty"`
}

// TLSUpdate represents partial TLS updates.
type TLSUpdate struct {
	CertPath *string `json:"cert_path,omitempty"`
	KeyPath  *string `json:"key_path,omitempty"`
}

// AccessControlUpdate represents partial access control updates.
type AccessControlUpdate struct {
	Enabled           *bool   `json:"enabled,omitempty"`
	Provider          *string `json:"provider,omitempty"`
	SessionTTLMinutes *int    `json:"session_ttl_minutes,omitempty"`
}

// ConfigUpdateRequest represents the API request for POST/PUT /api/config.
type ConfigUpdateRequest struct {
	WorkspacePath *string              `json:"workspace_path,omitempty"`
	Repos         []Repo               `json:"repos,omitempty"`
	RunTargets    []RunTarget          `json:"run_targets,omitempty"`
	QuickLaunch   []QuickLaunch        `json:"quick_launch,omitempty"`
	Variants      []Variant            `json:"variants,omitempty"`
	Nudgenik      *NudgenikUpdate      `json:"nudgenik,omitempty"`
	BranchSuggest *BranchSuggestUpdate `json:"branch_suggest,omitempty"`
	Terminal      *TerminalUpdate      `json:"terminal,omitempty"`
	Sessions      *SessionsUpdate      `json:"sessions,omitempty"`
	Xterm         *XtermUpdate         `json:"xterm,omitempty"`
	Network       *NetworkUpdate       `json:"network,omitempty"`
	AccessControl *AccessControlUpdate `json:"access_control,omitempty"`
}
