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

// AccessControl controls external access.
type AccessControl struct {
	NetworkAccess bool `json:"network_access"`
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
	Sessions      Sessions      `json:"sessions"`
	Xterm         Xterm         `json:"xterm"`
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

// AccessControlUpdate represents partial access control updates.
type AccessControlUpdate struct {
	NetworkAccess *bool `json:"network_access,omitempty"`
}

// ConfigUpdateRequest represents the API request for POST/PUT /api/config.
type ConfigUpdateRequest struct {
	WorkspacePath *string              `json:"workspace_path,omitempty"`
	Repos         []Repo               `json:"repos,omitempty"`
	RunTargets    []RunTarget          `json:"run_targets,omitempty"`
	QuickLaunch   []QuickLaunch        `json:"quick_launch,omitempty"`
	Variants      []Variant            `json:"variants,omitempty"`
	Nudgenik      *NudgenikUpdate      `json:"nudgenik,omitempty"`
	Terminal      *TerminalUpdate      `json:"terminal,omitempty"`
	Sessions      *SessionsUpdate      `json:"sessions,omitempty"`
	Xterm         *XtermUpdate         `json:"xterm,omitempty"`
	AccessControl *AccessControlUpdate `json:"access_control,omitempty"`
}
