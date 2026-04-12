package contracts

// Repo represents a git repository configuration.
type Repo struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	BarePath string `json:"bare_path,omitempty"`
	VCS      string `json:"vcs,omitempty"`
}

type SaplingCommandsUpdate struct {
	CreateWorkspace string `json:"create_workspace,omitempty"`
	RemoveWorkspace string `json:"remove_workspace,omitempty"`
	CheckRepoBase   string `json:"check_repo_base,omitempty"`
	CreateRepoBase  string `json:"create_repo_base,omitempty"`
	ListWorkspaces  string `json:"list_workspaces,omitempty"`
}

// RepoConfig represents repository-specific configuration from .schmux/config.json.
type RepoConfig struct {
	QuickLaunch []QuickLaunch `json:"quick_launch,omitempty"`
}

// RepoWithConfig represents a repository with its loaded configuration.
type RepoWithConfig struct {
	Name          string      `json:"name"`
	URL           string      `json:"url"`
	VCS           string      `json:"vcs,omitempty"`
	DefaultBranch string      `json:"default_branch,omitempty"`
	Config        *RepoConfig `json:"config,omitempty"`
}

// RunTarget represents a user-supplied run target.
type RunTarget struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// QuickLaunch represents a saved run preset.
// Either Command (shell command) or Target+Prompt (AI agent) should be set, not both.
type QuickLaunch struct {
	Name      string  `json:"name"`
	Command   string  `json:"command,omitempty"`    // shell command to run directly
	Target    string  `json:"target,omitempty"`     // run target (claude, codex, model, etc.)
	Prompt    *string `json:"prompt,omitempty"`     // prompt for the target
	PersonaID string  `json:"persona_id,omitempty"` // optional behavioral persona
}

// ExternalDiffCommand represents an external diff tool configuration.
type ExternalDiffCommand struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// RunnerInfo describes a tool/runner at the top level of ConfigResponse.
type RunnerInfo struct {
	Available    bool     `json:"available"`              // true if the tool is detected
	Capabilities []string `json:"capabilities,omitempty"` // tool modes: "interactive", "oneshot", "streaming"
}

// Model represents an AI model with metadata and configuration status.
type Model struct {
	ID                string   `json:"id"`                             // e.g., "claude-opus-4-6"
	DisplayName       string   `json:"display_name"`                   // e.g., "Claude Opus 4.6"
	Provider          string   `json:"provider"`                       // e.g., "anthropic"
	Configured        bool     `json:"configured"`                     // true if at least one runner is configured
	Runners           []string `json:"runners"`                        // tool names that can run this model
	RequiredSecrets   []string `json:"required_secrets,omitempty"`     // secrets needed for this model
	ContextWindow     int      `json:"context_window,omitempty"`       // context window in tokens
	MaxOutput         int      `json:"max_output,omitempty"`           // max output tokens
	CostInputPerMTok  float64  `json:"cost_input_per_mtok,omitempty"`  // $/million input tokens
	CostOutputPerMTok float64  `json:"cost_output_per_mtok,omitempty"` // $/million output tokens
	Reasoning         bool     `json:"reasoning,omitempty"`            // supports reasoning
	ReleaseDate       string   `json:"release_date,omitempty"`         // release date
	IsDefault         bool     `json:"is_default,omitempty"`           // is a default_* model
	IsUserDefined     bool     `json:"is_user_defined,omitempty"`      // is a user-defined model
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

// ConflictResolve represents conflict resolution configuration.
type ConflictResolve struct {
	Target    string `json:"target,omitempty"`
	TimeoutMs int    `json:"timeout_ms"`
}

// Sessions represents session and git-related timing configuration.
type Sessions struct {
	DashboardPollIntervalMs int `json:"dashboard_poll_interval_ms"`
	GitStatusPollIntervalMs int `json:"git_status_poll_interval_ms"`
	GitCloneTimeoutMs       int `json:"git_clone_timeout_ms"`
	GitStatusTimeoutMs      int `json:"git_status_timeout_ms"`
}

// Xterm represents terminal capture and timeout settings.
type Xterm struct {
	QueryTimeoutMs     int  `json:"query_timeout_ms"`
	OperationTimeoutMs int  `json:"operation_timeout_ms"`
	UseWebGL           bool `json:"use_webgl"`
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

// SystemCapabilities reports which optional system tools are available.
type SystemCapabilities struct {
	ITerm2Available bool `json:"iterm2_available"`
}

// DashboardSXStatus represents dashboard.sx heartbeat and certificate status.
type DashboardSXStatus struct {
	LastHeartbeatTime   string `json:"last_heartbeat_time,omitempty"`
	LastHeartbeatStatus int    `json:"last_heartbeat_status,omitempty"`
	LastHeartbeatError  string `json:"last_heartbeat_error,omitempty"`
	CertDomain          string `json:"cert_domain,omitempty"`
	CertExpiresAt       string `json:"cert_expires_at,omitempty"`
}

// ConfigResponse represents the API response for GET /api/config.
type ConfigResponse struct {
	WorkspacePath              string                 `json:"workspace_path"`
	SourceCodeManagement       string                 `json:"source_code_management"`
	Repos                      []RepoWithConfig       `json:"repos"`
	RunTargets                 []RunTarget            `json:"run_targets"`
	QuickLaunch                []QuickLaunch          `json:"quick_launch"`
	ExternalDiffCommands       []ExternalDiffCommand  `json:"external_diff_commands,omitempty"`
	ExternalDiffCleanupAfterMs int                    `json:"external_diff_cleanup_after_ms,omitempty"`
	Pastebin                   []string               `json:"pastebin,omitempty"`
	Runners                    map[string]RunnerInfo  `json:"runners"` // tool name -> runner info
	Models                     []Model                `json:"models"`
	EnabledModels              map[string]string      `json:"enabled_models,omitempty"` // modelID -> preferred tool
	CommStyles                 map[string]string      `json:"comm_styles,omitempty"`
	Nudgenik                   Nudgenik               `json:"nudgenik"`
	BranchSuggest              BranchSuggest          `json:"branch_suggest"`
	ConflictResolve            ConflictResolve        `json:"conflict_resolve"`
	Sessions                   Sessions               `json:"sessions"`
	Xterm                      Xterm                  `json:"xterm"`
	Network                    Network                `json:"network"`
	AccessControl              AccessControl          `json:"access_control"`
	PrReview                   PrReview               `json:"pr_review"`
	CommitMessage              CommitMessage          `json:"commit_message"`
	Desync                     Desync                 `json:"desync"`
	IOWorkspaceTelemetry       IOWorkspaceTelemetry   `json:"io_workspace_telemetry"`
	Notifications              Notifications          `json:"notifications"`
	Lore                       Lore                   `json:"lore"`
	Subreddit                  Subreddit              `json:"subreddit"`
	Repofeed                   Repofeed               `json:"repofeed"`
	FloorManager               FloorManager           `json:"floor_manager"`
	Timelapse                  Timelapse              `json:"timelapse"`
	RemoteAccess               RemoteAccess           `json:"remote_access"`
	SaplingCommands            *SaplingCommandsUpdate `json:"sapling_commands,omitempty"`
	TmuxBinary                 string                 `json:"tmux_binary,omitempty"`
	TmuxSocketName             string                 `json:"tmux_socket_name,omitempty"`
	RecycleWorkspaces          bool                   `json:"recycle_workspaces,omitempty"`
	LocalEchoRemote            bool                   `json:"local_echo_remote,omitempty"`
	DebugUI                    bool                   `json:"debug_ui,omitempty"`
	PersonasEnabled            bool                   `json:"personas_enabled,omitempty"`
	CommStylesEnabled          bool                   `json:"comm_styles_enabled,omitempty"`
	SystemCapabilities         SystemCapabilities     `json:"system_capabilities"`
	NeedsRestart               bool                   `json:"needs_restart"`
	DashboardSXStatus          *DashboardSXStatus     `json:"dashboard_sx_status,omitempty"`
}

// Desync represents desync diagnostic capture configuration in the API response.
type Desync struct {
	Enabled bool   `json:"enabled"`
	Target  string `json:"target"`
}

// IOWorkspaceTelemetry represents I/O workspace telemetry configuration in the API response.
type IOWorkspaceTelemetry struct {
	Enabled bool   `json:"enabled"`
	Target  string `json:"target"`
}

// IOWorkspaceTelemetryUpdate represents partial I/O workspace telemetry config updates.
type IOWorkspaceTelemetryUpdate struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Target  *string `json:"target,omitempty"`
}

// PrReview represents PR review configuration in the API response.
type PrReview struct {
	Target string `json:"target"`
}

// CommitMessage represents commit message generation configuration.
type CommitMessage struct {
	Target string `json:"target"`
}

// Notifications represents dashboard notification settings.
type Notifications struct {
	SoundDisabled           bool `json:"sound_disabled"`
	ConfirmBeforeClose      bool `json:"confirm_before_close"`
	SuggestDisposeAfterPush bool `json:"suggest_dispose_after_push"`
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

// ConflictResolveUpdate represents partial conflict resolve updates.
type ConflictResolveUpdate struct {
	Target    *string `json:"target,omitempty"`
	TimeoutMs *int    `json:"timeout_ms,omitempty"`
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
	QueryTimeoutMs     *int  `json:"query_timeout_ms,omitempty"`
	OperationTimeoutMs *int  `json:"operation_timeout_ms,omitempty"`
	UseWebGL           *bool `json:"use_webgl,omitempty"`
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

// TLSValidateRequest is the request body for POST /api/tls/validate.
type TLSValidateRequest struct {
	CertPath string `json:"cert_path"`
	KeyPath  string `json:"key_path"`
}

// TLSValidateResponse is the response from POST /api/tls/validate.
type TLSValidateResponse struct {
	Valid    bool   `json:"valid"`
	Hostname string `json:"hostname,omitempty"`
	Expires  string `json:"expires,omitempty"`
	Error    string `json:"error,omitempty"`
}

// AccessControlUpdate represents partial access control updates.
type AccessControlUpdate struct {
	Enabled           *bool   `json:"enabled,omitempty"`
	Provider          *string `json:"provider,omitempty"`
	SessionTTLMinutes *int    `json:"session_ttl_minutes,omitempty"`
}

// ConfigUpdateRequest represents the API request for POST/PUT /api/config.
type ConfigUpdateRequest struct {
	WorkspacePath              *string                     `json:"workspace_path,omitempty"`
	SourceCodeManagement       *string                     `json:"source_code_management,omitempty"`
	Repos                      []Repo                      `json:"repos,omitempty"`
	RunTargets                 []RunTarget                 `json:"run_targets,omitempty"`
	QuickLaunch                []QuickLaunch               `json:"quick_launch,omitempty"`
	ExternalDiffCommands       []ExternalDiffCommand       `json:"external_diff_commands,omitempty"`
	ExternalDiffCleanupAfterMs *int                        `json:"external_diff_cleanup_after_ms,omitempty"`
	Pastebin                   []string                    `json:"pastebin,omitempty"`
	Nudgenik                   *NudgenikUpdate             `json:"nudgenik,omitempty"`
	BranchSuggest              *BranchSuggestUpdate        `json:"branch_suggest,omitempty"`
	ConflictResolve            *ConflictResolveUpdate      `json:"conflict_resolve,omitempty"`
	Sessions                   *SessionsUpdate             `json:"sessions,omitempty"`
	Xterm                      *XtermUpdate                `json:"xterm,omitempty"`
	Network                    *NetworkUpdate              `json:"network,omitempty"`
	AccessControl              *AccessControlUpdate        `json:"access_control,omitempty"`
	PrReview                   *PrReviewUpdate             `json:"pr_review,omitempty"`
	CommitMessage              *CommitMessageUpdate        `json:"commit_message,omitempty"`
	Desync                     *DesyncUpdate               `json:"desync,omitempty"`
	IOWorkspaceTelemetry       *IOWorkspaceTelemetryUpdate `json:"io_workspace_telemetry,omitempty"`
	Notifications              *NotificationsUpdate        `json:"notifications,omitempty"`
	Lore                       *LoreUpdate                 `json:"lore,omitempty"`
	Subreddit                  *SubredditUpdate            `json:"subreddit,omitempty"`
	Repofeed                   *RepofeedUpdate             `json:"repofeed,omitempty"`
	FloorManager               *FloorManagerUpdate         `json:"floor_manager,omitempty"`
	Timelapse                  *TimelapseUpdate            `json:"timelapse,omitempty"`
	RemoteAccess               *RemoteAccessUpdate         `json:"remote_access,omitempty"`
	EnabledModels              *map[string]string          `json:"enabled_models,omitempty"`
	CommStyles                 *map[string]string          `json:"comm_styles,omitempty"`
	SaplingCommands            *SaplingCommandsUpdate      `json:"sapling_commands,omitempty"`
	TmuxBinary                 *string                     `json:"tmux_binary,omitempty"`
	TmuxSocketName             *string                     `json:"tmux_socket_name,omitempty"`
	RecycleWorkspaces          *bool                       `json:"recycle_workspaces,omitempty"`
	LocalEchoRemote            *bool                       `json:"local_echo_remote,omitempty"`
	DebugUI                    *bool                       `json:"debug_ui,omitempty"`
	PersonasEnabled            *bool                       `json:"personas_enabled,omitempty"`
	CommStylesEnabled          *bool                       `json:"comm_styles_enabled,omitempty"`
}

// DesyncUpdate represents partial desync diagnostic config updates.
type DesyncUpdate struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Target  *string `json:"target,omitempty"`
}

// PrReviewUpdate represents partial PR review config updates.
type PrReviewUpdate struct {
	Target *string `json:"target,omitempty"`
}

// CommitMessageUpdate represents partial commit message config updates.
type CommitMessageUpdate struct {
	Target *string `json:"target,omitempty"`
}

// NotificationsUpdate represents partial notifications config updates.
type NotificationsUpdate struct {
	SoundDisabled           *bool `json:"sound_disabled,omitempty"`
	ConfirmBeforeClose      *bool `json:"confirm_before_close,omitempty"`
	SuggestDisposeAfterPush *bool `json:"suggest_dispose_after_push,omitempty"`
}

// Lore represents lore system configuration in the API response.
type Lore struct {
	Enabled         bool   `json:"enabled"`
	LLMTarget       string `json:"llm_target"`
	CurateOnDispose string `json:"curate_on_dispose"`
	AutoPR          bool   `json:"auto_pr"`
	PublicRuleMode  string `json:"public_rule_mode,omitempty"`
}

// LoreUpdate represents partial lore config updates.
type LoreUpdate struct {
	Enabled         *bool   `json:"enabled,omitempty"`
	LLMTarget       *string `json:"llm_target,omitempty"`
	CurateOnDispose *string `json:"curate_on_dispose,omitempty"`
	AutoPR          *bool   `json:"auto_pr,omitempty"`
	PublicRuleMode  *string `json:"public_rule_mode,omitempty"`
}

// Subreddit represents subreddit digest configuration in the API response.
type Subreddit struct {
	Enabled       bool            `json:"enabled"`        // Whether subreddit digest is enabled
	Target        string          `json:"target"`         // LLM target for generation, empty = disabled
	Interval      int             `json:"interval"`       // Polling interval in minutes, default 30
	CheckingRange int             `json:"checking_range"` // Lookback for new commits in hours, default 48
	MaxPosts      int             `json:"max_posts"`      // Max posts per repo, default 30
	MaxAge        int             `json:"max_age"`        // Max post age in days, default 14
	Repos         map[string]bool `json:"repos"`          // Per-repo enabled/disabled
}

// SubredditUpdate represents partial subreddit config updates.
type SubredditUpdate struct {
	Enabled       *bool           `json:"enabled,omitempty"`
	Target        *string         `json:"target,omitempty"`
	Interval      *int            `json:"interval,omitempty"`
	CheckingRange *int            `json:"checking_range,omitempty"`
	MaxPosts      *int            `json:"max_posts,omitempty"`
	MaxAge        *int            `json:"max_age,omitempty"`
	Repos         map[string]bool `json:"repos,omitempty"`
}

// Repofeed represents repofeed configuration in the API response.
type Repofeed struct {
	Enabled                 bool            `json:"enabled"`
	PublishIntervalSeconds  int             `json:"publish_interval_seconds"`
	FetchIntervalSeconds    int             `json:"fetch_interval_seconds"`
	CompletedRetentionHours int             `json:"completed_retention_hours"`
	Repos                   map[string]bool `json:"repos"`
}

// RepofeedUpdate represents partial repofeed config updates.
type RepofeedUpdate struct {
	Enabled                 *bool           `json:"enabled,omitempty"`
	PublishIntervalSeconds  *int            `json:"publish_interval_seconds,omitempty"`
	FetchIntervalSeconds    *int            `json:"fetch_interval_seconds,omitempty"`
	CompletedRetentionHours *int            `json:"completed_retention_hours,omitempty"`
	Repos                   map[string]bool `json:"repos,omitempty"`
}

// FloorManager represents floor manager configuration in the API response.
type FloorManager struct {
	Enabled           bool   `json:"enabled"`
	Target            string `json:"target"`
	RotationThreshold int    `json:"rotation_threshold"`
	DebounceMs        int    `json:"debounce_ms"`
}

// FloorManagerUpdate represents partial floor manager config updates.
type FloorManagerUpdate struct {
	Enabled           *bool   `json:"enabled,omitempty"`
	Target            *string `json:"target,omitempty"`
	RotationThreshold *int    `json:"rotation_threshold,omitempty"`
	DebounceMs        *int    `json:"debounce_ms,omitempty"`
}

// Timelapse represents timelapse recording configuration in the API response.
type Timelapse struct {
	Enabled           bool `json:"enabled"`
	RetentionDays     int  `json:"retention_days"`
	MaxFileSizeMB     int  `json:"max_file_size_mb"`
	MaxTotalStorageMB int  `json:"max_total_storage_mb"`
}

// TimelapseUpdate represents partial timelapse config updates.
type TimelapseUpdate struct {
	Enabled           *bool `json:"enabled,omitempty"`
	RetentionDays     *int  `json:"retention_days,omitempty"`
	MaxFileSizeMB     *int  `json:"max_file_size_mb,omitempty"`
	MaxTotalStorageMB *int  `json:"max_total_storage_mb,omitempty"`
}

// RemoteAccess represents remote access configuration in the API response.
type RemoteAccess struct {
	Enabled         bool               `json:"enabled"`
	TimeoutMinutes  int                `json:"timeout_minutes"`
	PasswordHashSet bool               `json:"password_hash_set"`
	Notify          RemoteAccessNotify `json:"notify"`
}

// RemoteAccessNotify represents notification settings.
type RemoteAccessNotify struct {
	NtfyTopic string `json:"ntfy_topic"`
	Command   string `json:"command"`
}

// RemoteAccessUpdate represents partial remote access config updates.
type RemoteAccessUpdate struct {
	Enabled        *bool                     `json:"enabled,omitempty"`
	TimeoutMinutes *int                      `json:"timeout_minutes,omitempty"`
	Notify         *RemoteAccessNotifyUpdate `json:"notify,omitempty"`
}

// RemoteAccessNotifyUpdate represents partial notification config updates.
type RemoteAccessNotifyUpdate struct {
	NtfyTopic *string `json:"ntfy_topic,omitempty"`
	Command   *string `json:"command,omitempty"`
}
