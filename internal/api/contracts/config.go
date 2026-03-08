package contracts

// Repo represents a git repository configuration.
type Repo struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	BarePath string `json:"bare_path,omitempty"` // path relative to repos/query dirs
}

// RepoConfig represents repository-specific configuration from .schmux/config.json.
type RepoConfig struct {
	QuickLaunch []QuickLaunch `json:"quick_launch,omitempty"`
}

// RepoWithConfig represents a repository with its loaded configuration.
type RepoWithConfig struct {
	Name          string      `json:"name"`
	URL           string      `json:"url"`
	DefaultBranch string      `json:"default_branch,omitempty"` // Omitted if not detected
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
	ID              string   `json:"id"`                         // e.g., "claude-opus-4-6"
	DisplayName     string   `json:"display_name"`               // e.g., "Claude Opus 4.6"
	Provider        string   `json:"provider"`                   // e.g., "anthropic"
	Configured      bool     `json:"configured"`                 // true if at least one runner is configured
	Runners         []string `json:"runners"`                    // tool names that can run this model
	RequiredSecrets []string `json:"required_secrets,omitempty"` // secrets needed for this model
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
	QueryTimeoutMs     int `json:"query_timeout_ms"`
	OperationTimeoutMs int `json:"operation_timeout_ms"`
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

// ConfigResponse represents the API response for GET /api/config.
type ConfigResponse struct {
	WorkspacePath              string                `json:"workspace_path"`
	SourceCodeManagement       string                `json:"source_code_management"`
	Repos                      []RepoWithConfig      `json:"repos"`
	RunTargets                 []RunTarget           `json:"run_targets"`
	QuickLaunch                []QuickLaunch         `json:"quick_launch"`
	ExternalDiffCommands       []ExternalDiffCommand `json:"external_diff_commands,omitempty"`
	ExternalDiffCleanupAfterMs int                   `json:"external_diff_cleanup_after_ms,omitempty"`
	Runners                    map[string]RunnerInfo `json:"runners"` // tool name -> runner info
	Models                     []Model               `json:"models"`
	EnabledModels              map[string]string     `json:"enabled_models,omitempty"` // modelID -> preferred tool
	Nudgenik                   Nudgenik              `json:"nudgenik"`
	BranchSuggest              BranchSuggest         `json:"branch_suggest"`
	ConflictResolve            ConflictResolve       `json:"conflict_resolve"`
	Sessions                   Sessions              `json:"sessions"`
	Xterm                      Xterm                 `json:"xterm"`
	Network                    Network               `json:"network"`
	AccessControl              AccessControl         `json:"access_control"`
	PrReview                   PrReview              `json:"pr_review"`
	CommitMessage              CommitMessage         `json:"commit_message"`
	Desync                     Desync                `json:"desync"`
	IOWorkspaceTelemetry       IOWorkspaceTelemetry  `json:"io_workspace_telemetry"`
	Notifications              Notifications         `json:"notifications"`
	Lore                       Lore                  `json:"lore"`
	Subreddit                  Subreddit             `json:"subreddit"`
	FloorManager               FloorManager          `json:"floor_manager"`
	RemoteAccess               RemoteAccess          `json:"remote_access"`
	SystemCapabilities         SystemCapabilities    `json:"system_capabilities"`
	NeedsRestart               bool                  `json:"needs_restart"`
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
	QueryTimeoutMs     *int `json:"query_timeout_ms,omitempty"`
	OperationTimeoutMs *int `json:"operation_timeout_ms,omitempty"`
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
	FloorManager               *FloorManagerUpdate         `json:"floor_manager,omitempty"`
	RemoteAccess               *RemoteAccessUpdate         `json:"remote_access,omitempty"`
	EnabledModels              *map[string]string          `json:"enabled_models,omitempty"`
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
}

// LoreUpdate represents partial lore config updates.
type LoreUpdate struct {
	Enabled         *bool   `json:"enabled,omitempty"`
	LLMTarget       *string `json:"llm_target,omitempty"`
	CurateOnDispose *string `json:"curate_on_dispose,omitempty"`
	AutoPR          *bool   `json:"auto_pr,omitempty"`
}

// Subreddit represents subreddit digest configuration in the API response.
type Subreddit struct {
	Target        string          `json:"target"`         // LLM target for generation, empty = disabled
	Interval      int             `json:"interval"`       // Polling interval in minutes, default 30
	CheckingRange int             `json:"checking_range"` // Lookback for new commits in hours, default 48
	MaxPosts      int             `json:"max_posts"`      // Max posts per repo, default 30
	MaxAge        int             `json:"max_age"`        // Max post age in days, default 14
	Repos         map[string]bool `json:"repos"`          // Per-repo enabled/disabled
}

// SubredditUpdate represents partial subreddit config updates.
type SubredditUpdate struct {
	Target        *string         `json:"target,omitempty"`
	Interval      *int            `json:"interval,omitempty"`
	CheckingRange *int            `json:"checking_range,omitempty"`
	MaxPosts      *int            `json:"max_posts,omitempty"`
	MaxAge        *int            `json:"max_age,omitempty"`
	Repos         map[string]bool `json:"repos,omitempty"`
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
