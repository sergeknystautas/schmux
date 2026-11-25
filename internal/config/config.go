package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/fileutil"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/types"
	"github.com/sergeknystautas/schmux/internal/version"
)

// pkgLogger is the package-level logger for config operations.
// Set via SetLogger from the daemon initialization.
var pkgLogger *log.Logger

// SetLogger sets the package-level logger for config operations.
func SetLogger(l *log.Logger) {
	pkgLogger = l
}

var (
	ErrConfigNotFound = errors.New("config file not found")
	ErrInvalidConfig  = errors.New("invalid config")
)

const (
	// Default timeout values in milliseconds
	DefaultGitCloneTimeoutMs          = 300000  // 5 minutes
	DefaultGitStatusPollIntervalMs    = 10000   // 10 seconds
	DefaultGitStatusWatchDebounceMs   = 1000    // 1 second
	DefaultGitStatusTimeoutMs         = 30000   // 30 seconds
	DefaultXtermQueryTimeoutMs        = 5000    // 5 seconds
	DefaultXtermOperationTimeoutMs    = 10000   // 10 seconds
	DefaultExternalDiffCleanupAfterMs = 3600000 // 1 hour
	DefaultConflictResolveTimeoutMs   = 300000  // 5 minutes
	DefaultPreviewMaxPerWorkspace     = 3
	DefaultPreviewMaxGlobal           = 20
	DefaultPreviewPortBase            = 53000
	DefaultPreviewPortBlockSize       = 10
	DefaultDisposeGracePeriodMs       = 5000 // 5 seconds; agents that ignore the pty hangup (codex) never exit on their own, so a long grace only delays SIGTERM

	// Default auth session TTL in minutes
	DefaultAuthSessionTTLMinutes = 1440

	// DefaultAuthProvider is the default access-control provider written by
	// CreateDefault and returned by GetAuthProvider when AccessControl is
	// non-nil with an empty Provider field. The dashboard form roundtrips
	// this exact string back through the API on every save, so any change
	// here must be reflected in CreateDefault — see the round-trip test in
	// internal/dashboard/handlers_config_test.go.
	DefaultAuthProvider = "github"
)

// Source code management constants
const (
	SourceCodeManagementGitWorktree = "git-worktree" // default: use git worktrees
	SourceCodeManagementGit         = "git"          // vanilla full clone
)

// ConfigData holds all JSON-serializable fields. Extracted so Reload()
// can swap all JSON fields atomically without touching runtime fields.
type SecurityConfig struct {
	AllowInsecureModes bool `json:"allow_insecure_modes,omitempty"`
}

type ConfigData struct {
	ConfigVersion              string                      `json:"config_version,omitempty"`
	WorkspacePath              string                      `json:"workspace_path"`
	WorktreeBasePath           string                      `json:"base_repos_path,omitempty"`        // path for bare clones (worktree base repos)
	SourceCodeManagement       string                      `json:"source_code_management,omitempty"` // "git-worktree" (default) or "git"
	Repos                      []Repo                      `json:"repos"`
	RunTargets                 []RunTarget                 `json:"run_targets"`
	QuickLaunch                []QuickLaunch               `json:"quick_launch"`
	ExternalDiffCommands       []ExternalDiffCommand       `json:"external_diff_commands,omitempty"`
	ExternalDiffCleanupAfterMs int                         `json:"external_diff_cleanup_after_ms,omitempty"`
	Pastebin                   []string                    `json:"pastebin,omitempty"`
	Nudgenik                   *NudgenikConfig             `json:"nudgenik,omitempty"`
	BranchSuggest              *BranchSuggestConfig        `json:"branch_suggest,omitempty"`
	ConflictResolve            *ConflictResolveConfig      `json:"conflict_resolve,omitempty"`
	FlightName                 *FlightNameConfig           `json:"flight_name,omitempty"`
	FlightPilot                *FlightPilotConfig          `json:"flight_pilot,omitempty"`
	Compound                   *CompoundConfig             `json:"compound,omitempty"`
	Overlay                    *OverlayConfig              `json:"overlay,omitempty"`
	Lore                       *LoreConfig                 `json:"lore,omitempty"`
	Autolearn                  *AutolearnConfig            `json:"autolearn,omitempty"`
	Sessions                   *SessionsConfig             `json:"sessions,omitempty"`
	Xterm                      *XtermConfig                `json:"xterm,omitempty"`
	Network                    *NetworkConfig              `json:"network,omitempty"`
	AccessControl              *AccessControlConfig        `json:"access_control,omitempty"`
	PrReview                   *PrReviewConfig             `json:"pr_review,omitempty"`
	CommitMessage              *CommitMessageConfig        `json:"commit_message,omitempty"`
	Desync                     *DesyncConfig               `json:"desync,omitempty"`
	IOWorkspaceTelemetry       *IOWorkspaceTelemetryConfig `json:"io_workspace_telemetry,omitempty"`
	ClientPerformance          *ClientPerformanceConfig    `json:"client_performance,omitempty"`
	FenceAnalyze               *FenceAnalyzeConfig         `json:"fence_analyze,omitempty"`
	Notifications              *NotificationsConfig        `json:"notifications,omitempty"`
	RemoteAccess               *RemoteAccessConfig         `json:"remote_access,omitempty"`
	Models                     *ModelsConfig               `json:"models,omitempty"`
	CommStyles                 map[string]string           `json:"comm_styles,omitempty"`
	Subreddit                  *SubredditConfig            `json:"subreddit,omitempty"`
	Repofeed                   *RepofeedConfig             `json:"repofeed,omitempty"`
	BuildMonitor               *BuildMonitorConfig         `json:"build_monitor,omitempty"`
	FloorManager               *FloorManagerConfig         `json:"floor_manager,omitempty"`
	SaplingCommands            SaplingCommands             `json:"sapling_commands,omitempty"`
	BuiltInSkills              map[string]bool             `json:"built_in_skills,omitempty"` // Deprecated: no longer used. Kept for config compatibility.
	TmuxBinary                 string                      `json:"tmux_binary,omitempty"`
	TmuxSocketName             string                      `json:"tmux_socket_name,omitempty"`
	MinFreeDiskSpaceMiB        int64                       `json:"min_free_disk_space_mib,omitempty"`
	UI                         UIConfig                    `json:"ui,omitempty"`
	ChatSessions               bool                        `json:"chat_sessions,omitempty"`
	ChatStorage                *ChatStorageConfig          `json:"chat_storage,omitempty"`
	ChatLoadProfilingEnabled   bool                        `json:"chat_load_profiling_enabled,omitempty"`
	PersonasEnabled            bool                        `json:"personas_enabled,omitempty"`
	CommStylesEnabled          bool                        `json:"comm_styles_enabled,omitempty"`
	BackburnerEnabled          bool                        `json:"backburner_enabled,omitempty"`
	FenceMode                  string                      `json:"fence_mode,omitempty"`
	FenceCommit                bool                        `json:"fence_commit,omitempty"`
	FenceBuildMonitor          bool                        `json:"fence_build_monitor,omitempty"`
	ClipboardSyncEnabled       *bool                       `json:"clipboard_sync_enabled,omitempty"`
	Timelapse                  *TimelapseConfig            `json:"timelapse,omitempty"`

	// Telemetry settings
	Telemetry      *TelemetryConfig `json:"telemetry,omitempty"`
	InstallationID string           `json:"installation_id,omitempty"` // UUID for anonymous tracking
	Security       *SecurityConfig  `json:"security,omitempty"`
	Ollama         *OllamaConfig    `json:"ollama,omitempty"`
}

type OllamaConfig struct {
	Endpoint string `json:"endpoint,omitempty"`
}

// Config represents the application configuration.
type Config struct {
	ConfigData // embedded — all JSON-serializable fields

	// Runtime fields (not serialized)

	// path is the file path where this config was loaded from or should be saved to.
	// Not serialized to JSON.
	path string `json:"-"`

	// mu protects concurrent reads/writes to Config fields.
	mu sync.RWMutex `json:"-"`

	// repoURLCache is a lazily-built cache mapping repo URL to Repo.
	// Not serialized to JSON. Built on first call to FindRepoByURL.
	// Invalidated by Save() when repos change.
	// Protected by repoURLMu for concurrent access safety.
	repoURLCache map[string]Repo `json:"-"`
	repoURLMu    sync.RWMutex    `json:"-"`

	// lastSaveCompletedAt records when the last Save() completed, so the
	// config file watcher can ignore fsnotify events caused by our own writes.
	// Protected by mu.
	lastSaveCompletedAt time.Time `json:"-"`

	// listenPort overrides the configured dashboard port for this process
	// only and is never saved; valid when hasListenPort. Protected by mu.
	listenPort    int  `json:"-"`
	hasListenPort bool `json:"-"`
}

// PrReviewConfig holds configuration for GitHub PR review sessions.
type PrReviewConfig struct {
	Target string `json:"target,omitempty"` // run target to use for PR review sessions
}

// CommitMessageConfig holds configuration for commit message generation.
type CommitMessageConfig struct {
	Target string `json:"target,omitempty"` // run target to use for commit message generation
}

// DesyncConfig holds configuration for desync diagnostic capture sessions.
type DesyncConfig struct {
	Enabled *bool  `json:"enabled,omitempty"` // enable/disable desync diagnostics
	Target  string `json:"target,omitempty"`  // run target to invoke after diagnostic capture
}

// TelemetryConfig holds telemetry settings.
type TelemetryConfig struct {
	Enabled *bool        `json:"enabled,omitempty"` // default true
	Command ShellCommand `json:"command,omitempty"` // argv-array external command for telemetry events
}

// IOWorkspaceTelemetryConfig holds configuration for I/O workspace telemetry collection.
type IOWorkspaceTelemetryConfig struct {
	Enabled *bool  `json:"enabled,omitempty"` // enable/disable I/O workspace telemetry
	Target  string `json:"target,omitempty"`  // run target for telemetry processing
}

// ClientPerformanceConfig gates the browser performance recorder and names
// the repo and target of the chat an agent diagnoses recordings in.
type ClientPerformanceConfig struct {
	Enabled *bool  `json:"enabled,omitempty"` // enable/disable the recorder and sidebar pane
	Repo    string `json:"repo,omitempty"`    // configured repo name; must be a schmux checkout
	Target  string `json:"target,omitempty"`  // chat target for the performance chat
}

// FenceAnalyzeConfig gates the "Analyze fence" button and the agent target it spawns.
type FenceAnalyzeConfig struct {
	Enabled *bool  `json:"enabled,omitempty"` // enable/disable the Analyze fence button
	Target  string `json:"target,omitempty"`  // run target for the fence-analysis agent
}

// NotificationsConfig holds configuration for dashboard notifications.
type NotificationsConfig struct {
	SoundDisabled           bool  `json:"sound_disabled,omitempty"`             // disable attention sounds (default: false = sounds enabled)
	ConfirmBeforeClose      bool  `json:"confirm_before_close,omitempty"`       // show browser "Leave site?" dialog on tab close (default: false = no confirmation)
	SuggestDisposeAfterPush *bool `json:"suggest_dispose_after_push,omitempty"` // prompt to dispose workspace after pushing to main (default: true)
}

// RemoteAccessNotifyConfig configures push notifications for remote access.
type RemoteAccessNotifyConfig struct {
	NtfyTopic string `json:"ntfy_topic,omitempty"`
	Command   string `json:"command,omitempty"`
}

// RemoteAccessConfig configures remote access via Cloudflare tunnel.
type RemoteAccessConfig struct {
	Enabled           *bool                     `json:"enabled,omitempty"`
	TimeoutMinutes    int                       `json:"timeout_minutes,omitempty"`
	PasswordHash      string                    `json:"password_hash,omitempty"`
	AllowAutoDownload *bool                     `json:"allow_auto_download,omitempty"` // allow auto-downloading cloudflared binary (default: false)
	Notify            *RemoteAccessNotifyConfig `json:"notify,omitempty"`

	// Deprecated: Use Enabled instead. Kept for backward compatibility with existing configs.
	// If both are set, Enabled takes precedence.
	Disabled *bool `json:"disabled,omitempty"`
}

// NudgenikConfig represents configuration for the NudgeNik assistant.
type NudgenikConfig struct {
	Targets        []string `json:"targets,omitempty"`
	ViewedBufferMs int      `json:"viewed_buffer_ms,omitempty"`
	SeenIntervalMs int      `json:"seen_interval_ms,omitempty"`
}

// SubredditConfig represents configuration for the subreddit digest feature.
type SubredditConfig struct {
	Enabled       bool            `json:"enabled,omitempty"`        // Whether subreddit digest is enabled
	Target        string          `json:"target,omitempty"`         // LLM target for generation, empty = disabled
	Interval      int             `json:"interval,omitempty"`       // Polling interval in minutes, default 30
	CheckingRange int             `json:"checking_range,omitempty"` // Lookback for new commits in hours, default 48
	MaxPosts      int             `json:"max_posts,omitempty"`      // Max posts per repo, default 30
	MaxAge        int             `json:"max_age,omitempty"`        // Max post age in days, default 14
	Repos         map[string]bool `json:"repos,omitempty"`          // Per-repo enabled/disabled (default true)
}

// BuildMonitorConfig represents configuration for the build monitor feature.
type BuildMonitorConfig struct {
	Enabled bool `json:"enabled,omitempty"`
	// IntervalSeconds is the seconds between check passes (build monitor
	// units AND workspace CI/PR status); <=0 means default (60), floor 15.
	// Replaces the legacy minutes-based "interval" key, which is ignored.
	IntervalSeconds int    `json:"interval_seconds,omitempty"`
	Target          string `json:"target,omitempty"` // agent target for remediation sessions; empty disables launching
	// AutoWorkspaceOnFirstFailure launches a remediation workspace + session
	// automatically when a workflow first enters the failing state.
	AutoWorkspaceOnFirstFailure bool `json:"auto_workspace_on_first_failure,omitempty"`
	// Chat launches remediation sessions as chat sessions (kind "chat").
	Chat  bool                              `json:"chat,omitempty"`
	Repos map[string]BuildMonitorRepoConfig `json:"repos,omitempty"`
}

// BuildMonitorRepoConfig represents per-repo build monitor configuration.
// An enabled repo watches every active workflow on its default branch.
type BuildMonitorRepoConfig struct {
	Enabled     bool   `json:"enabled,omitempty"`
	GitHubLogin string `json:"github_login"`
}

// RepofeedConfig controls the cross-developer intent federation system.
type RepofeedConfig struct {
	Enabled                 bool            `json:"enabled,omitempty"`
	PublishIntervalSeconds  int             `json:"publish_interval_seconds,omitempty"`
	FetchIntervalSeconds    int             `json:"fetch_interval_seconds,omitempty"`
	CompletedRetentionHours int             `json:"completed_retention_hours,omitempty"`
	Repos                   map[string]bool `json:"repos,omitempty"`
	IntentTarget            string          `json:"intent_target,omitempty"`
}

// FloorManagerConfig configures the floor manager singleton agent.
type FloorManagerConfig struct {
	Enabled           *bool  `json:"enabled,omitempty"`
	Target            string `json:"target,omitempty"`
	RotationThreshold int    `json:"rotation_threshold,omitempty"`
	DebounceMs        int    `json:"debounce_ms,omitempty"`
}

// TimelapseConfig controls terminal session recording.
type TimelapseConfig struct {
	Enabled           *bool `json:"enabled,omitempty"`           // default true
	RetentionDays     *int  `json:"retentionDays,omitempty"`     // default 7
	MaxFileSizeMB     *int  `json:"maxFileSizeMB,omitempty"`     // default 50
	MaxTotalStorageMB *int  `json:"maxTotalStorageMB,omitempty"` // default 500
}

// ChatStorageConfig controls the chat storage cleanup cleaner. Pointers
// distinguish omitted (use default) from zero (retention_days=0 is
// meaningful; the grace period still applies).
type ChatStorageConfig struct {
	RetentionDays     *int `json:"retention_days,omitempty"`       // default 7, >= 0
	MaxTotalStorageMB *int `json:"max_total_storage_mb,omitempty"` // default 4096, >= 1
}

// UIConfig holds dashboard sidebar preferences. Panels maps panel ids to
// their feature switch; an absent key leaves that panel disabled.
// SkipEmptyWorkspaces is nil when unset, which GetSkipEmptyWorkspaces
// resolves to true. TimeSortIntervalSeconds is nil when unset, which
// GetTimeSortIntervalSeconds resolves to 2.
type UIConfig struct {
	Panels                  map[string]bool `json:"panels,omitempty"`
	SkipEmptyWorkspaces     *bool           `json:"skip_empty_workspaces,omitempty"`
	TimeSortIntervalSeconds *int            `json:"time_sort_interval_seconds,omitempty"`
}

// BranchSuggestConfig represents configuration for branch name suggestion.
type BranchSuggestConfig struct {
	Targets []string `json:"targets,omitempty"`
}

// ConflictResolveConfig represents configuration for conflict resolution.
type ConflictResolveConfig struct {
	Target    string `json:"target,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

// FlightNameConfig represents configuration for naming flights.
type FlightNameConfig struct {
	Target string `json:"target,omitempty"`
}

// FlightPilotConfig is the flight manager's pilot configuration: the chat
// target every flight's pilot session is spawned with.
type FlightPilotConfig struct {
	Target string `json:"target,omitempty"`
}

// CompoundConfig represents configuration for the overlay compounding loop.
type CompoundConfig struct {
	Target           string `json:"target,omitempty"`             // LLM target for merging (falls back to nudgenik target)
	DebounceMs       int    `json:"debounce_ms,omitempty"`        // debounce interval in ms (default 2000)
	Enabled          *bool  `json:"enabled,omitempty"`            // explicitly enable/disable (default: true)
	SuppressionTTLMs int    `json:"suppression_ttl_ms,omitempty"` // suppression window in ms (default 5000)
}

// OverlayConfig represents global overlay path configuration.
type OverlayConfig struct {
	Paths []string `json:"paths,omitempty"` // additional global overlay paths
}

// LoreConfig represents configuration for the lore (continual learning) system.
type LoreConfig struct {
	Enabled          *bool    `json:"enabled,omitempty"`            // explicitly enable/disable (default: true)
	Target           string   `json:"llm_target,omitempty"`         // LLM target for curator (falls back to compound target)
	AutoPR           *bool    `json:"auto_pr,omitempty"`            // auto-create PR after pushing lore branch (default: false)
	CurateOnDispose  string   `json:"curate_on_dispose,omitempty"`  // "session", "workspace", or "never" (default: "session")
	CurateDebounceMs int      `json:"curate_debounce_ms,omitempty"` // debounce for auto-curation (default 30000)
	PruneAfterDays   int      `json:"prune_after_days,omitempty"`   // days before pruning applied/dismissed entries (default 30)
	InstructionFiles []string `json:"instruction_files,omitempty"`  // instruction file patterns to manage
	PublicRuleMode   string   `json:"public_rule_mode,omitempty"`   // "direct_push" (default) or "create_pr"

	// curateOnDisposeRaw stores the raw JSON value for backward compatibility.
	// Old configs may have a boolean value (true → "session", false → "never").
	curateOnDisposeRaw json.RawMessage `json:"-"`
}

// UnmarshalJSON implements custom JSON unmarshaling for LoreConfig to handle
// backward compatibility where curate_on_dispose was a boolean.
func (lc *LoreConfig) UnmarshalJSON(data []byte) error {
	// Use an alias type to avoid infinite recursion
	type loreConfigAlias LoreConfig
	var alias loreConfigAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		// If standard unmarshal fails (e.g., curate_on_dispose is a bool),
		// parse the raw JSON to extract the field manually.
		var raw map[string]json.RawMessage
		if err2 := json.Unmarshal(data, &raw); err2 != nil {
			return err
		}
		// Remove curate_on_dispose from the map and retry
		codRaw := raw["curate_on_dispose"]
		delete(raw, "curate_on_dispose")
		sanitized, _ := json.Marshal(raw)
		if err2 := json.Unmarshal(sanitized, &alias); err2 != nil {
			return err
		}
		alias.curateOnDisposeRaw = codRaw
	}
	*lc = LoreConfig(alias)
	return nil
}

// GetPublicRuleMode returns the configured public rule mode, defaulting to "direct_push".
func (lc *LoreConfig) GetPublicRuleMode() string {
	if lc == nil || lc.PublicRuleMode == "" {
		return "direct_push"
	}
	return lc.PublicRuleMode
}

// AutolearnConfig represents configuration for the autolearn (continual learning) system.
// This is the successor to LoreConfig with the same fields.
type AutolearnConfig struct {
	Enabled          *bool    `json:"enabled,omitempty"`            // explicitly enable/disable (default: true)
	CurateOnDispose  string   `json:"curate_on_dispose,omitempty"`  // "session", "workspace", or "never" (default: "session")
	CurateDebounceMs int      `json:"curate_debounce_ms,omitempty"` // debounce for auto-curation (default 30000)
	Target           string   `json:"llm_target,omitempty"`         // LLM target for curator (falls back to compound target)
	AutoPR           *bool    `json:"auto_pr,omitempty"`            // auto-create PR after pushing autolearn branch (default: false)
	InstructionFiles []string `json:"instruction_files,omitempty"`  // instruction file patterns to manage
	PublicRuleMode   string   `json:"public_rule_mode,omitempty"`   // "direct_push" (default) or "create_pr"
	PruneAfterDays   int      `json:"prune_after_days,omitempty"`   // days before pruning applied/dismissed entries (default 30)

	// curateOnDisposeRaw stores the raw JSON value for backward compatibility.
	// Old configs may have a boolean value (true → "session", false → "never").
	curateOnDisposeRaw json.RawMessage `json:"-"`
}

// UnmarshalJSON implements custom JSON unmarshaling for AutolearnConfig to handle
// backward compatibility where curate_on_dispose was a boolean.
func (ac *AutolearnConfig) UnmarshalJSON(data []byte) error {
	type autolearnConfigAlias AutolearnConfig
	var alias autolearnConfigAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		var raw map[string]json.RawMessage
		if err2 := json.Unmarshal(data, &raw); err2 != nil {
			return err
		}
		codRaw := raw["curate_on_dispose"]
		delete(raw, "curate_on_dispose")
		sanitized, _ := json.Marshal(raw)
		if err2 := json.Unmarshal(sanitized, &alias); err2 != nil {
			return err
		}
		alias.curateOnDisposeRaw = codRaw
	}
	*ac = AutolearnConfig(alias)
	return nil
}

// GetPublicRuleMode returns the configured public rule mode, defaulting to "direct_push".
func (ac *AutolearnConfig) GetPublicRuleMode() string {
	if ac == nil || ac.PublicRuleMode == "" {
		return "direct_push"
	}
	return ac.PublicRuleMode
}

// SessionsConfig represents session and git-related timing configuration.
type SessionsConfig struct {
	DashboardPollIntervalMs  int   `json:"dashboard_poll_interval_ms"`
	GitStatusPollIntervalMs  int   `json:"git_status_poll_interval_ms"`
	GitCloneTimeoutMs        int   `json:"git_clone_timeout_ms"`
	GitStatusTimeoutMs       int   `json:"git_status_timeout_ms"`
	GitStatusWatchEnabled    *bool `json:"git_status_watch_enabled,omitempty"`
	GitStatusWatchDebounceMs int   `json:"git_status_watch_debounce_ms,omitempty"`
	DisposeGracePeriodMs     int   `json:"dispose_grace_period_ms,omitempty"`
}

// XtermConfig represents terminal capture and timeout settings.
type XtermConfig struct {
	QueryTimeoutMs     int   `json:"query_timeout_ms"`
	OperationTimeoutMs int   `json:"operation_timeout_ms"`
	UseWebGL           *bool `json:"use_webgl,omitempty"`
}

// DashboardSXConfig holds dashboard.sx HTTPS provisioning configuration.
type DashboardSXConfig struct {
	Enabled    bool   `json:"enabled"`
	Code       string `json:"code,omitempty"`
	Email      string `json:"email,omitempty"`
	IP         string `json:"ip,omitempty"`
	ServiceURL string `json:"service_url,omitempty"`
}

// NetworkConfig controls server binding and TLS.
type NetworkConfig struct {
	BindAddress            string             `json:"bind_address,omitempty"`
	Port                   int                `json:"port,omitempty"`
	PublicBaseURL          string             `json:"public_base_url,omitempty"`
	PreviewMaxPerWorkspace int                `json:"preview_max_per_workspace,omitempty"`
	PreviewMaxGlobal       int                `json:"preview_max_global,omitempty"`
	PreviewPortBase        int                `json:"preview_port_base,omitempty"`
	PreviewPortBlockSize   int                `json:"preview_port_block_size,omitempty"`
	TLS                    *TLSConfig         `json:"tls,omitempty"`
	DashboardSX            *DashboardSXConfig `json:"dashboardsx,omitempty"`
	DashboardHostname      string             `json:"dashboard_hostname,omitempty"`
}

// TLSConfig holds TLS certificate paths.
type TLSConfig struct {
	CertPath string `json:"cert_path,omitempty"`
	KeyPath  string `json:"key_path,omitempty"`
}

// AccessControlConfig controls authentication.
type AccessControlConfig struct {
	Enabled           bool   `json:"enabled"`
	Provider          string `json:"provider,omitempty"`
	SessionTTLMinutes int    `json:"session_ttl_minutes,omitempty"`
}

// Repo represents a git repository configuration.
type Repo struct {
	Name                  string   `json:"name"`
	URL                   string   `json:"url"`
	BarePath              string   `json:"bare_path,omitempty"`
	VCS                   string   `json:"vcs,omitempty"`
	OverlayPaths          []string `json:"overlay_paths,omitempty"`
	OverlayNudgeDismissed bool     `json:"overlay_nudge_dismissed,omitempty"`
	GitHubLogin           string   `json:"github_login,omitempty"`
}

// ShellCommand is an argv-array config value for shell-executed commands
// (e.g. sapling_commands.create_workspace, telemetry, external diff tools). The value on disk is a JSON array of strings; each
// element is a text/template slot rendered independently by
// internal/cmdtemplate.Render so a templated value can never produce more
// than one argv element regardless of contents. See spec
// docs/specs/meta-distribution-hardening-final.md §2.1.
//
// The custom UnmarshalJSON rejects the legacy string form with a clear
// error pointing at `schmux config migrate` (spec §2.4). There is
// intentionally no silent auto-conversion: schmux is not in broad
// distribution yet, so a hard schema break is the safest option.
type ShellCommand []string

// UnmarshalJSON accepts the new []string argv form. The legacy string form
// is rejected with an error that points at the migration CLI; any other
// JSON shape is rejected as a type error.
func (s *ShellCommand) UnmarshalJSON(data []byte) error {
	// New argv-array form.
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*s = arr
		return nil
	}
	// Legacy string form — reject with guidance.
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		return fmt.Errorf("legacy string-form command (%q) is no longer supported; run `schmux config migrate` to convert your config to argv-array form (see docs/specs/meta-distribution-hardening-final.md §2.4)", str)
	}
	return fmt.Errorf("command must be a JSON array of strings")
}

// MarshalJSON serialises the argv as a JSON array. A nil ShellCommand is
// emitted as JSON null (omitempty handles the "field absent" case).
func (s ShellCommand) MarshalJSON() ([]byte, error) {
	if s == nil {
		return []byte("null"), nil
	}
	return json.Marshal([]string(s))
}

// SaplingCommands holds the shell commands used to manage sapling workspaces.
// Each command is an argv-array template rendered by internal/cmdtemplate.Render
// so a templated value can never expand into multiple shell tokens.
type SaplingCommands struct {
	CreateWorkspace ShellCommand `json:"create_workspace,omitempty"`
	RemoveWorkspace ShellCommand `json:"remove_workspace,omitempty"`
	CheckRepoBase   ShellCommand `json:"check_repo_base,omitempty"`
	CreateRepoBase  ShellCommand `json:"create_repo_base,omitempty"`
	ListWorkspaces  ShellCommand `json:"list_workspaces,omitempty"`
}

func (sc SaplingCommands) GetCreateWorkspace() ShellCommand {
	if len(sc.CreateWorkspace) > 0 {
		return sc.CreateWorkspace
	}
	return ShellCommand{"sl", "clone", "{{.RepoIdentifier}}", "{{.DestPath}}"}
}

func (sc SaplingCommands) GetRemoveWorkspace() ShellCommand {
	if len(sc.RemoveWorkspace) > 0 {
		return sc.RemoveWorkspace
	}
	return ShellCommand{"rm", "-rf", "{{.WorkspacePath}}"}
}

func (sc SaplingCommands) GetCreateRepoBase() ShellCommand {
	if len(sc.CreateRepoBase) > 0 {
		return sc.CreateRepoBase
	}
	return ShellCommand{"sl", "clone", "{{.RepoIdentifier}}", "{{.BasePath}}"}
}

// RunTarget represents a user-supplied run target.
type RunTarget struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// QuickLaunch is a type alias for contracts.QuickLaunch. The contract
// package owns the schema so persisted and API shapes cannot drift.
type QuickLaunch = contracts.QuickLaunch

// ExternalDiffCommand represents an external diff tool configuration.
// The Command field is an argv-array template rendered by
// internal/cmdtemplate.Render. Tools that need file paths read them from
// LOCAL/REMOTE/MERGED env vars rather than templated argv slots.
type ExternalDiffCommand struct {
	Name    string       `json:"name"`
	Command ShellCommand `json:"command"`
}

// ModelsConfig holds model-related configuration.
type ModelsConfig struct {
	Enabled map[string]string `json:"enabled,omitempty"` // modelID -> preferred tool
}

// Migration represents a single config transformation.
type Migration struct {
	// Name identifies this migration (for logging/debugging)
	Name string

	// Detect returns true if this migration needs to be applied.
	// It receives the raw JSON (for detecting old field names) and
	// the parsed config (for detecting missing values).
	Detect func(rawJSON map[string]json.RawMessage, cfg *Config) bool

	// Apply transforms the config. Receives both raw JSON (for reading
	// old field names) and the parsed config struct.
	Apply func(rawJSON map[string]json.RawMessage, cfg *Config) error
}

// migrations is the registry of all migrations, in dependency order.
// Each migration self-selects via its Detect function.
var migrations = []Migration{
	{
		Name: "rename_source_code_manager_to_management",
		Detect: func(raw map[string]json.RawMessage, cfg *Config) bool {
			_, hasOldField := raw["source_code_manager"]
			// Only run if old field exists and new field is not already set
			return hasOldField && cfg.SourceCodeManagement == ""
		},
		Apply: func(raw map[string]json.RawMessage, cfg *Config) error {
			var val string
			// Handle null gracefully - treat as empty string
			if len(raw["source_code_manager"]) == 0 || string(raw["source_code_manager"]) == "null" {
				return nil
			}
			if err := json.Unmarshal(raw["source_code_manager"], &val); err != nil {
				// If unmarshal fails (non-string value), log and skip rather than fail
				// This allows the config to load even if user edited it incorrectly
				return nil
			}
			cfg.SourceCodeManagement = val
			return nil
		},
	},
	{
		Name: "drop_variants_field",
		Detect: func(raw map[string]json.RawMessage, cfg *Config) bool {
			_, hasOldField := raw["variants"]
			return hasOldField
		},
		Apply: func(raw map[string]json.RawMessage, cfg *Config) error {
			// Just drop the variants field - it's no longer used
			// Models are now built-in and don't require user configuration
			return nil
		},
	},
	{
		Name: "target_string_to_targets_list",
		Detect: func(raw map[string]json.RawMessage, cfg *Config) bool {
			for _, section := range []string{"nudgenik", "branch_suggest"} {
				var sec map[string]json.RawMessage
				if data, ok := raw[section]; ok && json.Unmarshal(data, &sec) == nil {
					if _, has := sec["target"]; has {
						return true
					}
				}
			}
			return false
		},
		Apply: func(raw map[string]json.RawMessage, cfg *Config) error {
			migrateSection := func(secRaw json.RawMessage, targets *[]string) {
				var sec map[string]json.RawMessage
				if err := json.Unmarshal(secRaw, &sec); err != nil {
					return
				}
				legacy, ok := sec["target"]
				if !ok || len(legacy) == 0 || string(legacy) == "null" {
					return
				}
				var target string
				if err := json.Unmarshal(legacy, &target); err != nil {
					return // non-string value: leave as-is, load continues
				}
				if strings.TrimSpace(target) == "" {
					return // blank legacy value stays disabled (spec)
				}
				if len(*targets) == 0 {
					*targets = []string{target}
				}
			}
			if cfg.Nudgenik != nil {
				migrateSection(raw["nudgenik"], &cfg.Nudgenik.Targets)
			}
			if cfg.BranchSuggest != nil {
				migrateSection(raw["branch_suggest"], &cfg.BranchSuggest.Targets)
			}
			return nil
		},
	},
	{
		Name: "migrate_legacy_model_ids",
		Detect: func(raw map[string]json.RawMessage, cfg *Config) bool {
			return cfg.hasLegacyModelIDs()
		},
		Apply: func(raw map[string]json.RawMessage, cfg *Config) error {
			cfg.migrateModelIDs()
			return nil
		},
	},
	{
		Name: "drop_run_target_bridge_fields",
		Detect: func(raw map[string]json.RawMessage, _ *Config) bool {
			type legacyTarget struct {
				Source string `json:"source"`
				Type   string `json:"type"`
			}
			var targets []legacyTarget
			if data, ok := raw["run_targets"]; ok {
				if json.Unmarshal(data, &targets) == nil {
					for _, t := range targets {
						if t.Source != "" || t.Type != "" {
							return true
						}
					}
				}
			}
			return false
		},
		Apply: func(raw map[string]json.RawMessage, cfg *Config) error {
			type legacyTarget struct {
				Name    string `json:"name"`
				Command string `json:"command"`
				Source  string `json:"source"`
			}
			var old []legacyTarget
			if data, ok := raw["run_targets"]; ok {
				if err := json.Unmarshal(data, &old); err != nil {
					return err
				}
			}
			var cleaned []RunTarget
			for _, t := range old {
				source := t.Source
				if source == "" {
					source = "user"
				}
				if source == "detected" || source == "model" {
					continue // drop bridge-injected entries
				}
				if t.Name != "" && t.Command != "" {
					cleaned = append(cleaned, RunTarget{Name: t.Name, Command: t.Command})
				}
			}
			cfg.RunTargets = cleaned
			return nil
		},
	},
	{
		Name: "rewrite_tool_name_targets_to_model_ids",
		Detect: func(_ map[string]json.RawMessage, cfg *Config) bool {
			for _, ql := range cfg.QuickLaunch {
				if types.IsBuiltinToolName(ql.Target) {
					return true
				}
			}
			if cfg.Nudgenik != nil {
				for _, t := range cfg.Nudgenik.Targets {
					if types.IsBuiltinToolName(t) {
						return true
					}
				}
			}
			if cfg.Compound != nil && types.IsBuiltinToolName(cfg.Compound.Target) {
				return true
			}
			return false
		},
		Apply: func(_ map[string]json.RawMessage, cfg *Config) error {
			resolve := func(toolName string) string {
				return toolName // historical migration, already ran for existing users
			}

			for i := range cfg.QuickLaunch {
				if types.IsBuiltinToolName(cfg.QuickLaunch[i].Target) {
					cfg.QuickLaunch[i].Target = resolve(cfg.QuickLaunch[i].Target)
				}
			}
			if cfg.Nudgenik != nil {
				for i, t := range cfg.Nudgenik.Targets {
					if types.IsBuiltinToolName(t) {
						cfg.Nudgenik.Targets[i] = resolve(t)
					}
				}
			}
			if cfg.Compound != nil && types.IsBuiltinToolName(cfg.Compound.Target) {
				cfg.Compound.Target = resolve(cfg.Compound.Target)
			}
			return nil
		},
	},
	{
		Name: "migrate_telemetry_stanza",
		Detect: func(raw map[string]json.RawMessage, _ *Config) bool {
			_, hasOld := raw["telemetry_enabled"]
			_, hasNew := raw["telemetry"]
			return hasOld && !hasNew
		},
		Apply: func(raw map[string]json.RawMessage, cfg *Config) error {
			var enabled bool
			if err := json.Unmarshal(raw["telemetry_enabled"], &enabled); err != nil {
				// If the value is malformed, default to true
				enabled = true
			}
			cfg.Telemetry = &TelemetryConfig{Enabled: &enabled}
			delete(raw, "telemetry_enabled")
			return nil
		},
	},
}

// hasLegacyModelIDs returns true if any config field contains a legacy model ID.
func (c *Config) hasLegacyModelIDs() bool {
	legacy := types.LegacyIDMigrations()
	isLegacy := func(id string) bool {
		if id == "" {
			return false
		}
		newID, ok := legacy[id]
		return ok && newID != id
	}

	// Quick launch targets
	for _, ql := range c.QuickLaunch {
		if isLegacy(ql.Target) {
			return true
		}
	}

	// Nested config targets
	if c.Nudgenik != nil {
		for _, t := range c.Nudgenik.Targets {
			if isLegacy(t) {
				return true
			}
		}
	}
	if c.BranchSuggest != nil {
		for _, t := range c.BranchSuggest.Targets {
			if isLegacy(t) {
				return true
			}
		}
	}
	if c.ConflictResolve != nil && isLegacy(c.ConflictResolve.Target) {
		return true
	}
	if c.FlightName != nil && isLegacy(c.FlightName.Target) {
		return true
	}
	if c.FlightPilot != nil && isLegacy(c.FlightPilot.Target) {
		return true
	}
	if c.PrReview != nil && isLegacy(c.PrReview.Target) {
		return true
	}
	if c.CommitMessage != nil && isLegacy(c.CommitMessage.Target) {
		return true
	}
	if c.Desync != nil && isLegacy(c.Desync.Target) {
		return true
	}
	if c.FloorManager != nil && isLegacy(c.FloorManager.Target) {
		return true
	}
	if c.Lore != nil && isLegacy(c.Lore.Target) {
		return true
	}
	if c.Compound != nil && isLegacy(c.Compound.Target) {
		return true
	}
	if c.Subreddit != nil && isLegacy(c.Subreddit.Target) {
		return true
	}
	if c.IOWorkspaceTelemetry != nil && isLegacy(c.IOWorkspaceTelemetry.Target) {
		return true
	}
	if c.ClientPerformance != nil && isLegacy(c.ClientPerformance.Target) {
		return true
	}
	if c.FenceAnalyze != nil && isLegacy(c.FenceAnalyze.Target) {
		return true
	}

	// Enabled models map
	if c.Models != nil && c.Models.Enabled != nil {
		for id := range c.Models.Enabled {
			if isLegacy(id) {
				return true
			}
		}
	}

	return false
}

// migrateModelIDs updates legacy model IDs to vendor-defined IDs in config fields.
func (c *Config) migrateModelIDs() {
	// Quick launch targets
	for i, ql := range c.QuickLaunch {
		if ql.Target != "" {
			c.QuickLaunch[i].Target = types.MigrateModelID(ql.Target)
		}
	}

	// Nested config targets
	migrateTarget := func(field *string) {
		if field != nil && *field != "" {
			*field = types.MigrateModelID(*field)
		}
	}

	migrateTargets := func(targets *[]string) {
		for i, t := range *targets {
			if t != "" {
				(*targets)[i] = types.MigrateModelID(t)
			}
		}
	}
	if c.Nudgenik != nil {
		migrateTargets(&c.Nudgenik.Targets)
	}
	if c.BranchSuggest != nil {
		migrateTargets(&c.BranchSuggest.Targets)
	}
	if c.ConflictResolve != nil {
		migrateTarget(&c.ConflictResolve.Target)
	}
	if c.FlightName != nil {
		migrateTarget(&c.FlightName.Target)
	}
	if c.FlightPilot != nil {
		migrateTarget(&c.FlightPilot.Target)
	}
	if c.PrReview != nil {
		migrateTarget(&c.PrReview.Target)
	}
	if c.CommitMessage != nil {
		migrateTarget(&c.CommitMessage.Target)
	}
	if c.Desync != nil {
		migrateTarget(&c.Desync.Target)
	}
	if c.FloorManager != nil {
		migrateTarget(&c.FloorManager.Target)
	}
	if c.Lore != nil {
		migrateTarget(&c.Lore.Target)
	}
	if c.Compound != nil {
		migrateTarget(&c.Compound.Target)
	}
	if c.Subreddit != nil {
		migrateTarget(&c.Subreddit.Target)
	}
	if c.IOWorkspaceTelemetry != nil {
		migrateTarget(&c.IOWorkspaceTelemetry.Target)
	}
	if c.ClientPerformance != nil {
		migrateTarget(&c.ClientPerformance.Target)
	}
	if c.FenceAnalyze != nil {
		migrateTarget(&c.FenceAnalyze.Target)
	}

	// Enabled models map
	if c.Models != nil && c.Models.Enabled != nil {
		newEnabled := make(map[string]string, len(c.Models.Enabled))
		for id, tool := range c.Models.Enabled {
			newEnabled[types.MigrateModelID(id)] = tool
		}
		c.Models.Enabled = newEnabled
	}
}

// Validate validates the config including terminal settings, run targets, models, and quick launch presets.
func (c *Config) Validate() error {
	_, err := c.validate(true)
	return err
}

// ValidateForSave validates the config but returns auth-related issues as warnings.
func (c *Config) ValidateForSave() ([]string, error) {
	return c.validate(false)
}

// ValidateAuthEnabled runs strict access-control validation and returns an
// error if auth is enabled but its prerequisites (TLS cert/key, public_base_url,
// GitHub client_id/secret) are missing or unreadable. Returns nil when auth is
// disabled. Used to gate enabling auth so the dashboard can never enter an
// auth-enabled-but-unworkable state.
func (c *Config) ValidateAuthEnabled() error {
	_, err := c.validateAccessControl(true)
	return err
}

func (c *Config) validate(strict bool) ([]string, error) {
	if c.MinFreeDiskSpaceMiB < 0 {
		return nil, fmt.Errorf("min_free_disk_space_mib must be 0 or greater")
	}
	if c.ChatStorage != nil {
		if c.ChatStorage.RetentionDays != nil && *c.ChatStorage.RetentionDays < 0 {
			return nil, fmt.Errorf("chat_storage.retention_days must be 0 or greater")
		}
		if c.ChatStorage.MaxTotalStorageMB != nil && *c.ChatStorage.MaxTotalStorageMB < 1 {
			return nil, fmt.Errorf("chat_storage.max_total_storage_mb must be 1 or greater")
		}
	}
	if err := validateRunTargets(c.RunTargets); err != nil {
		return nil, err
	}
	if err := validateQuickLaunch(c.QuickLaunch); err != nil {
		return nil, err
	}
	if err := validateNudgenikConfig(c.Nudgenik); err != nil {
		return nil, err
	}
	if err := validateBranchSuggestConfig(c.BranchSuggest); err != nil {
		return nil, err
	}
	if err := validateCompoundConfig(c.Compound); err != nil {
		return nil, err
	}
	warnings, err := c.validateAccessControl(strict)
	if err != nil {
		return nil, err
	}
	return warnings, nil
}

func (c *Config) expandNetworkPaths(homeDir string) {
	if homeDir == "" || c.Network == nil || c.Network.TLS == nil {
		return
	}
	if strings.HasPrefix(c.Network.TLS.CertPath, "~") {
		c.Network.TLS.CertPath = filepath.Join(homeDir, strings.TrimPrefix(c.Network.TLS.CertPath, "~"))
	}
	if strings.HasPrefix(c.Network.TLS.KeyPath, "~") {
		c.Network.TLS.KeyPath = filepath.Join(homeDir, strings.TrimPrefix(c.Network.TLS.KeyPath, "~"))
	}
}

// GetWorkspacePath returns the workspace directory path.
func (c *Config) GetWorkspacePath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.WorkspacePath
}

// IsBuiltinEnabled is deprecated. Built-in skills have been removed;
// skills are now managed per-repo via the emergence system.
// Kept temporarily for config compatibility.
func (c *Config) IsBuiltinEnabled(_ string) bool {
	return false
}

// GetMinFreeDiskSpaceBytes returns the configured free-disk threshold in
// bytes. Returns 0 when the threshold is unset (the check is disabled).
func (c *Config) GetMinFreeDiskSpaceBytes() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.MinFreeDiskSpaceMiB <= 0 {
		return 0
	}
	return c.MinFreeDiskSpaceMiB * (1 << 20)
}

// GetWorktreeBasePath returns the path for bare clones (worktree base repos).
// Defaults to ~/.schmux/repos if not set.
func (c *Config) GetWorktreeBasePath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.WorktreeBasePath != "" {
		return c.WorktreeBasePath
	}
	return filepath.Join(schmuxdir.Get(), "repos")
}

// BareBaseNamesOnDisk returns the names (without the ".git" suffix) of every
// bare base directory present in the worktree base dir. Used so repo-name
// generation can avoid names already taken by a base on disk, not just names
// in config.
func (c *Config) BareBaseNamesOnDisk() []string {
	entries, err := os.ReadDir(c.GetWorktreeBasePath())
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), ".git") {
			names = append(names, strings.TrimSuffix(e.Name(), ".git"))
		}
	}
	return names
}

// ConflictingBaseNames returns the names (without ".git") of bare bases on disk
// whose origin differs from repoURL. A base whose origin matches repoURL is the
// same repo and is omitted so it can be reused. Pass the result to
// repoNameFromURL so a new repo whose derived name collides with a foreign base
// gets disambiguated onto its own base.
func (c *Config) ConflictingBaseNames(repoURL string) []string {
	base := c.GetWorktreeBasePath()
	var names []string
	for _, name := range c.BareBaseNamesOnDisk() {
		if bareRepoOrigin(filepath.Join(base, name+".git")) != repoURL {
			names = append(names, name)
		}
	}
	return names
}

// ResolveBareRepoDir returns the full directory path for a bare repo,
// checking both the worktree base path (repos/) and the query path (query/).
// Returns the first path where the repo exists on disk, falling back to the
// worktree base path if neither exists.
func (c *Config) ResolveBareRepoDir(barePath string) string {
	reposPath := c.GetWorktreeBasePath()
	queryPath := c.GetQueryRepoPath()

	for _, basePath := range []string{reposPath, queryPath} {
		if basePath == "" {
			continue
		}
		fullPath := filepath.Join(basePath, barePath)
		if _, err := os.Stat(fullPath); err == nil {
			return fullPath
		}
	}

	// Fallback to repos path (original behavior)
	return filepath.Join(reposPath, barePath)
}

// GetQueryRepoPath returns the path for query repos used for branch/commit querying.
// Always ~/.schmux/query/ - separate from worktree base repos.
func (c *Config) GetQueryRepoPath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return filepath.Join(schmuxdir.Get(), "query")
}

// GetSourceCodeManagement returns the configured source code management mode.
// Defaults to "git-worktree" if not set.
func (c *Config) GetSourceCodeManagement() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.SourceCodeManagement == "" {
		return SourceCodeManagementGitWorktree
	}
	return c.SourceCodeManagement
}

// UseWorktrees returns true if the source code management mode is git-worktree.
func (c *Config) UseWorktrees() bool {
	return c.GetSourceCodeManagement() == SourceCodeManagementGitWorktree
}

// GetRepos returns the list of repositories.
func (c *Config) GetRepos() []Repo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Repos
}

// GetRunTargets returns the list of run targets.
func (c *Config) GetRunTargets() []RunTarget {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.RunTargets
}

// GetQuickLaunch returns the list of quick launch presets.
func (c *Config) GetQuickLaunch() []QuickLaunch {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.QuickLaunch
}

// GetExternalDiffCommands returns the list of external diff commands.
func (c *Config) GetExternalDiffCommands() []ExternalDiffCommand {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ExternalDiffCommands
}

// GetExternalDiffCleanupAfterMs returns the diff temp cleanup delay in ms.
func (c *Config) GetExternalDiffCleanupAfterMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ExternalDiffCleanupAfterMs > 0 {
		return c.ExternalDiffCleanupAfterMs
	}
	return DefaultExternalDiffCleanupAfterMs
}

func (c *Config) GetPastebin() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Pastebin
}

// GetNudgenikTarget returns the configured nudgenik target name, if any.
func (c *Config) GetNudgenikTarget() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.getNudgenikTargetLocked()
}

// GetNudgenikTargets returns the configured nudgenik target chain: trimmed,
// blank entries dropped, order preserved. Index 0 is the primary.
func (c *Config) GetNudgenikTargets() []string {
	if c == nil || c.Nudgenik == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return CleanTargets(c.Nudgenik.Targets)
}

// getNudgenikTargetLocked is the lock-free implementation. Caller must hold mu.
func (c *Config) getNudgenikTargetLocked() string {
	if c == nil || c.Nudgenik == nil {
		return ""
	}
	targets := CleanTargets(c.Nudgenik.Targets)
	if len(targets) == 0 {
		return ""
	}
	return targets[0]
}

// GetSubredditEnabled returns whether the subreddit digest is enabled.
func (c *Config) GetSubredditEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Subreddit == nil {
		return false
	}
	return c.Subreddit.Enabled
}

// GetSubredditTarget returns the configured subreddit target name, if any.
func (c *Config) GetSubredditTarget() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Subreddit == nil {
		return ""
	}
	return strings.TrimSpace(c.Subreddit.Target)
}

// GetSubredditInterval returns the polling interval in minutes, defaulting to 30.
func (c *Config) GetSubredditInterval() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Subreddit == nil || c.Subreddit.Interval <= 0 {
		return 30
	}
	return c.Subreddit.Interval
}

// GetSubredditCheckingRange returns the lookback for new commits in hours, defaulting to 48.
func (c *Config) GetSubredditCheckingRange() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Subreddit == nil || c.Subreddit.CheckingRange <= 0 {
		return 48
	}
	return c.Subreddit.CheckingRange
}

// GetSubredditMaxPosts returns the max posts per repo, defaulting to 30.
func (c *Config) GetSubredditMaxPosts() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Subreddit == nil || c.Subreddit.MaxPosts <= 0 {
		return 30
	}
	return c.Subreddit.MaxPosts
}

// GetSubredditMaxAge returns the max post age in days, defaulting to 14.
func (c *Config) GetSubredditMaxAge() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Subreddit == nil || c.Subreddit.MaxAge <= 0 {
		return 14
	}
	return c.Subreddit.MaxAge
}

// GetSubredditRepoEnabled returns whether a specific repo is enabled for subreddit generation.
// Returns true by default if the repo is not in the map.
func (c *Config) GetSubredditRepoEnabled(repoSlug string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Subreddit == nil || c.Subreddit.Repos == nil {
		return true
	}
	enabled, exists := c.Subreddit.Repos[repoSlug]
	if !exists {
		return true
	}
	return enabled
}

// GetSubredditRepos returns the per-repo enabled/disabled map.
func (c *Config) GetSubredditRepos() map[string]bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Subreddit == nil || c.Subreddit.Repos == nil {
		return nil
	}
	// Return a copy to avoid race conditions
	result := make(map[string]bool, len(c.Subreddit.Repos))
	for k, v := range c.Subreddit.Repos {
		result[k] = v
	}
	return result
}

// GetEventMonitorEnabled reports whether Event Monitor diagnostics are on.
func (c *Config) GetEventMonitorEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.UI.Panels["eventMonitor"]
}

// GetTmuxDiagnosticsEnabled reports whether tmux diagnostics are on.
func (c *Config) GetTmuxDiagnosticsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.UI.Panels["tmuxDiagnostic"]
}

// GetServerLoadPanelEnabled reports whether the Server Load panel is enabled.
func (c *Config) GetServerLoadPanelEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.UI.Panels["serverLoad"]
}

// GetPlanUsagePanelEnabled reports whether the Plan Usage panel is enabled.
func (c *Config) GetPlanUsagePanelEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.UI.Panels["planUsage"]
}

// GetUIPanels returns a copy of the sidebar panel preferences, nil when
// none are set.
func (c *Config) GetUIPanels() map[string]bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.UI.Panels == nil {
		return nil
	}
	result := make(map[string]bool, len(c.UI.Panels))
	for k, v := range c.UI.Panels {
		result[k] = v
	}
	return result
}

// GetChatSessions reports whether chat-kind sessions may be spawned.
func (c *Config) GetChatSessions() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ChatSessions
}

// GetChatLoadProfilingEnabled gates extra local chat load diagnostics.
func (c *Config) GetChatLoadProfilingEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ChatLoadProfilingEnabled
}

// GetChatStorageRetentionDays returns the chat transcript retention period in
// days. Default 7. Zero is meaningful: ended transcripts become age-eligible
// immediately after the one-hour grace period.
func (c *Config) GetChatStorageRetentionDays() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ChatStorage == nil || c.ChatStorage.RetentionDays == nil {
		return 7
	}
	return *c.ChatStorage.RetentionDays
}

// GetChatStorageMaxTotalStorageMB returns the chat storage budget in MiB.
// Default 4096. Always >= 1 when present.
func (c *Config) GetChatStorageMaxTotalStorageMB() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ChatStorage == nil || c.ChatStorage.MaxTotalStorageMB == nil {
		return 4096
	}
	return *c.ChatStorage.MaxTotalStorageMB
}

// GetPersonasEnabled returns whether the personas feature is enabled.
func (c *Config) GetPersonasEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.PersonasEnabled
}

// GetAllowInsecureModes returns whether insecure file modes are allowed.
// When false (the default), the daemon refuses to start if chmod fails.
func (c *Config) GetAllowInsecureModes() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Security == nil {
		return false
	}
	return c.Security.AllowInsecureModes
}

// GetCommStylesEnabled returns whether the comm styles feature is enabled.
func (c *Config) GetCommStylesEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.CommStylesEnabled
}

// GetBackburnerEnabled returns whether the backburner workspace feature is enabled.
func (c *Config) GetBackburnerEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.BackburnerEnabled
}

// GetSkipEmptyWorkspaces returns whether workspace keyboard navigation
// should skip workspaces without sessions. Defaults to true when unset,
// so existing configs keep the prior Cmd+Up/Down behavior (opt-out).
func (c *Config) GetSkipEmptyWorkspaces() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.UI.SkipEmptyWorkspaces == nil {
		return true
	}
	return *c.UI.SkipEmptyWorkspaces
}

// GetTimeSortIntervalSeconds returns the minimum time between sidebar
// reorders in time sort. Defaults to 2 seconds when unset or non-positive.
func (c *Config) GetTimeSortIntervalSeconds() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.UI.TimeSortIntervalSeconds == nil || *c.UI.TimeSortIntervalSeconds <= 0 {
		return 2
	}
	return *c.UI.TimeSortIntervalSeconds
}

// Fence mode values gate the fence feature and the spawn-checkbox default.
// Empty (the omitted default) means FenceModeOptionalOff — today's behavior.
const (
	FenceModeDisabled    = "disabled"
	FenceModeOptionalOff = "optional_off"
	FenceModeOptionalOn  = "optional_on"
)

// GetFenceMode returns the configured fence mode, normalizing empty/unknown
// values to FenceModeOptionalOff so callers always get one of the three values.
func (c *Config) GetFenceMode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	switch c.FenceMode {
	case FenceModeDisabled, FenceModeOptionalOn:
		return c.FenceMode
	default:
		return FenceModeOptionalOff
	}
}

// GetFenceBuildMonitor reports whether build-monitor remediation sessions
// should launch inside the fence sandbox (sandbox + skip-approvals).
func (c *Config) GetFenceBuildMonitor() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.FenceBuildMonitor
}

// GetClipboardSyncEnabled returns whether terminal clipboard sync is enabled:
// OSC 52 forwarding via tmux set-clipboard and the dashboard clipboard banner.
// Defaults to true when unset, so existing configs keep current behavior (opt-out).
func (c *Config) GetClipboardSyncEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ClipboardSyncEnabled == nil {
		return true
	}
	return *c.ClipboardSyncEnabled
}

// GetRepofeedEnabled returns whether the repofeed system is enabled.
func (c *Config) GetRepofeedEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Repofeed == nil {
		return false
	}
	return c.Repofeed.Enabled
}

// GetRepofeedPublishInterval returns the publish interval in seconds, defaulting to 30.
func (c *Config) GetRepofeedPublishInterval() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Repofeed == nil || c.Repofeed.PublishIntervalSeconds <= 0 {
		return 30
	}
	return c.Repofeed.PublishIntervalSeconds
}

// GetRepofeedFetchInterval returns the fetch interval in seconds, defaulting to 60.
func (c *Config) GetRepofeedFetchInterval() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Repofeed == nil || c.Repofeed.FetchIntervalSeconds <= 0 {
		return 60
	}
	return c.Repofeed.FetchIntervalSeconds
}

// GetRepofeedCompletedRetention returns the completed activity retention in hours, defaulting to 48.
func (c *Config) GetRepofeedCompletedRetention() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Repofeed == nil || c.Repofeed.CompletedRetentionHours <= 0 {
		return 48
	}
	return c.Repofeed.CompletedRetentionHours
}

// GetRepofeedRepoEnabled returns whether a specific repo is enabled for repofeed.
// Returns false by default if the repo is not in the map.
func (c *Config) GetRepofeedRepoEnabled(slug string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Repofeed == nil || c.Repofeed.Repos == nil {
		return false
	}
	enabled, ok := c.Repofeed.Repos[slug]
	if !ok {
		return false
	}
	return enabled
}

// GetRepofeedRepos returns the per-repo enabled/disabled map.
func (c *Config) GetRepofeedRepos() map[string]bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Repofeed == nil || c.Repofeed.Repos == nil {
		return nil
	}
	result := make(map[string]bool, len(c.Repofeed.Repos))
	for k, v := range c.Repofeed.Repos {
		result[k] = v
	}
	return result
}

// GetBuildMonitorEnabled returns whether the build monitor feature is enabled.
func (c *Config) GetBuildMonitorEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.BuildMonitor != nil && c.BuildMonitor.Enabled
}

// GetBuildMonitorRepos returns a copy of the build monitor repo configurations.
func (c *Config) GetBuildMonitorRepos() map[string]BuildMonitorRepoConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.BuildMonitor == nil || c.BuildMonitor.Repos == nil {
		return nil
	}
	out := make(map[string]BuildMonitorRepoConfig, len(c.BuildMonitor.Repos))
	for k, v := range c.BuildMonitor.Repos {
		out[k] = v
	}
	return out
}

// GetBuildMonitorRepoEnabled returns whether a specific repo is enabled for build monitoring.
// Returns false by default if the repo is not in the map.
func (c *Config) GetBuildMonitorRepoEnabled(slug string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.BuildMonitor == nil || c.BuildMonitor.Repos == nil {
		return false
	}
	r, ok := c.BuildMonitor.Repos[slug]
	return ok && r.Enabled
}

// GetBuildMonitorRepo returns the build monitor configuration for a specific repo.
func (c *Config) GetBuildMonitorRepo(slug string) (BuildMonitorRepoConfig, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.BuildMonitor == nil || c.BuildMonitor.Repos == nil {
		return BuildMonitorRepoConfig{}, false
	}
	r, ok := c.BuildMonitor.Repos[slug]
	return r, ok
}

// GetGitHubLogin returns the GitHub OAuth login configured for a repo.
// Repo-level config takes precedence over the legacy build-monitor field.
func (c *Config) GetGitHubLogin(repoURL string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, repo := range c.Repos {
		if repo.URL == repoURL {
			if repo.GitHubLogin != "" {
				return repo.GitHubLogin
			}
			if c.BuildMonitor != nil && c.BuildMonitor.Repos != nil {
				if bmCfg, ok := c.BuildMonitor.Repos[repoSlug(repo.Name)]; ok {
					return bmCfg.GitHubLogin
				}
			}
			break
		}
	}
	return ""
}

// repoSlug creates a URL-safe slug from a repo name. Mirrors
// dashboard.repoSlug — BuildMonitorConfig.Repos is keyed by this slug.
func repoSlug(name string) string {
	result := make([]byte, 0, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			result = append(result, c)
		} else if c >= 'A' && c <= 'Z' {
			result = append(result, c+32) // lowercase
		} else {
			result = append(result, '-')
		}
	}
	return string(result)
}

// GetBuildMonitorIntervalSeconds returns the check pass interval, defaulting
// to 60s (floor 15s).
func (c *Config) GetBuildMonitorIntervalSeconds() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.BuildMonitor == nil || c.BuildMonitor.IntervalSeconds <= 0 {
		return 60 * time.Second
	}
	if c.BuildMonitor.IntervalSeconds < 15 {
		return 15 * time.Second
	}
	return time.Duration(c.BuildMonitor.IntervalSeconds) * time.Second
}

// GetBuildMonitorTarget returns the agent target for remediation sessions,
// trimmed. Empty means launching is not configured.
func (c *Config) GetBuildMonitorTarget() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.BuildMonitor == nil {
		return ""
	}
	return strings.TrimSpace(c.BuildMonitor.Target)
}

// GetBuildMonitorAutoWorkspace returns whether the first hard failure
// auto-launches a remediation workspace.
func (c *Config) GetBuildMonitorAutoWorkspace() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.BuildMonitor != nil && c.BuildMonitor.AutoWorkspaceOnFirstFailure
}

// GetBuildMonitorChat reports whether remediation sessions launch as chat
// sessions.
func (c *Config) GetBuildMonitorChat() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.BuildMonitor != nil && c.BuildMonitor.Chat
}

// GetRepofeedIntentTarget returns the configured repofeed intent summarization target name, if any.
func (c *Config) GetRepofeedIntentTarget() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Repofeed == nil {
		return ""
	}
	return strings.TrimSpace(c.Repofeed.IntentTarget)
}

// GetFloorManagerEnabled returns whether the floor manager is enabled.
func (c *Config) GetFloorManagerEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.FloorManager == nil || c.FloorManager.Enabled == nil {
		return false
	}
	return *c.FloorManager.Enabled
}

// GetFloorManagerTarget returns the configured floor manager target name.
func (c *Config) GetFloorManagerTarget() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.FloorManager == nil {
		return ""
	}
	return strings.TrimSpace(c.FloorManager.Target)
}

// GetFloorManagerRotationThreshold returns the rotation threshold, defaulting to 150.
func (c *Config) GetFloorManagerRotationThreshold() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.FloorManager == nil || c.FloorManager.RotationThreshold <= 0 {
		return 150
	}
	return c.FloorManager.RotationThreshold
}

// GetFloorManagerDebounceMs returns the debounce interval in ms, defaulting to 2000.
func (c *Config) GetFloorManagerDebounceMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.FloorManager == nil || c.FloorManager.DebounceMs <= 0 {
		return 2000
	}
	return c.FloorManager.DebounceMs
}

// GetTimelapseEnabled returns whether timelapse recording is enabled (default true).
func (c *Config) GetTimelapseEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Timelapse == nil || c.Timelapse.Enabled == nil {
		return true
	}
	return *c.Timelapse.Enabled
}

// GetTimelapseRetentionDays returns the recording retention period (default 7).
func (c *Config) GetTimelapseRetentionDays() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Timelapse == nil || c.Timelapse.RetentionDays == nil {
		return 7
	}
	return *c.Timelapse.RetentionDays
}

// GetTimelapseMaxFileSizeMB returns the max per-recording size in MB (default 50).
func (c *Config) GetTimelapseMaxFileSizeMB() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Timelapse == nil || c.Timelapse.MaxFileSizeMB == nil {
		return 50
	}
	return *c.Timelapse.MaxFileSizeMB
}

// GetTimelapseMaxTotalStorageMB returns the max total storage in MB (default 500).
func (c *Config) GetTimelapseMaxTotalStorageMB() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Timelapse == nil || c.Timelapse.MaxTotalStorageMB == nil {
		return 500
	}
	return *c.Timelapse.MaxTotalStorageMB
}

// GetBranchSuggestTarget returns the primary branch suggestion target.
func (c *Config) GetBranchSuggestTarget() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.BranchSuggest == nil {
		return ""
	}
	targets := CleanTargets(c.BranchSuggest.Targets)
	if len(targets) == 0 {
		return ""
	}
	return targets[0]
}

// GetBranchSuggestTargets returns the configured branch suggestion target
// chain: trimmed, blank entries dropped, order preserved.
func (c *Config) GetBranchSuggestTargets() []string {
	if c == nil || c.BranchSuggest == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return CleanTargets(c.BranchSuggest.Targets)
}

// GetCompoundTarget returns the configured compound target name.
// Falls back to the nudgenik target if not explicitly configured.
func (c *Config) GetCompoundTarget() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.getCompoundTargetLocked()
}

// getCompoundTargetLocked is the lock-free implementation. Caller must hold mu.
func (c *Config) getCompoundTargetLocked() string {
	if c == nil || c.Compound == nil || strings.TrimSpace(c.Compound.Target) == "" {
		return c.getNudgenikTargetLocked()
	}
	return strings.TrimSpace(c.Compound.Target)
}

// GetCompoundDebounceMs returns the compound debounce interval in milliseconds.
func (c *Config) GetCompoundDebounceMs() int {
	if c == nil {
		return 2000
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Compound == nil || c.Compound.DebounceMs <= 0 {
		return 2000
	}
	return c.Compound.DebounceMs
}

// GetCompoundEnabled returns whether compounding is enabled.
func (c *Config) GetCompoundEnabled() bool {
	if c == nil {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Compound == nil || c.Compound.Enabled == nil {
		return true // enabled by default
	}
	return *c.Compound.Enabled
}

// GetCompoundSuppressionTTLMs returns the suppression TTL in milliseconds.
func (c *Config) GetCompoundSuppressionTTLMs() int {
	if c == nil {
		return 5000
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Compound == nil || c.Compound.SuppressionTTLMs <= 0 {
		return 5000
	}
	return c.Compound.SuppressionTTLMs
}

// applyLoreAlias populates Autolearn from the legacy Lore section when only
// Lore is configured. Lore is a read-only fallback for old config files; all
// writes go to the Autolearn section.
func (c *Config) applyLoreAlias() {
	if c.Autolearn == nil && c.Lore != nil {
		c.Autolearn = &AutolearnConfig{
			Enabled:          c.Lore.Enabled,
			CurateOnDispose:  c.Lore.CurateOnDispose,
			CurateDebounceMs: c.Lore.CurateDebounceMs,
			Target:           c.Lore.Target,
			AutoPR:           c.Lore.AutoPR,
			InstructionFiles: c.Lore.InstructionFiles,
			PublicRuleMode:   c.Lore.PublicRuleMode,
			PruneAfterDays:   c.Lore.PruneAfterDays,
		}
	}
}

// DefaultInstructionFiles are the instruction file patterns checked by the lore curator.
var DefaultInstructionFiles = []string{
	"CLAUDE.md",
	"AGENTS.md",
	".cursorrules",
	".github/copilot-instructions.md",
	"CONVENTIONS.md",
}

// GetLoreEnabled returns whether the lore system is enabled.
// Defaults to true if not explicitly configured.
func (c *Config) GetLoreEnabled() bool {
	if c == nil {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Lore == nil || c.Lore.Enabled == nil {
		return true
	}
	return *c.Lore.Enabled
}

// GetLoreTarget returns the configured lore curator LLM target.
// Falls back to the compound target if not explicitly configured.
func (c *Config) GetLoreTarget() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Lore != nil && c.Lore.Target != "" {
		return c.Lore.Target
	}
	return c.getCompoundTargetLocked()
}

// GetLoreTargetRaw returns the explicitly configured lore curator LLM target
// without any fallback. Returns "" if no target is set.
// Use this for config UI display; use GetLoreTarget() for runtime behavior.
func (c *Config) GetLoreTargetRaw() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Lore == nil {
		return ""
	}
	return c.Lore.Target
}

// GetLoreAutoPR returns whether to auto-create a PR after pushing a lore branch.
// Defaults to false.
func (c *Config) GetLoreAutoPR() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Lore == nil || c.Lore.AutoPR == nil {
		return false
	}
	return *c.Lore.AutoPR
}

// GetLoreCurateOnDispose returns the curate-on-dispose mode.
// Returns "session", "workspace", or "never". Defaults to "session".
func (c *Config) GetLoreCurateOnDispose() string {
	if c == nil {
		return "session"
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Lore == nil {
		return "session"
	}
	// If the string value is already set, use it directly
	if c.Lore.CurateOnDispose != "" {
		switch c.Lore.CurateOnDispose {
		case "session", "workspace", "never":
			return c.Lore.CurateOnDispose
		default:
			return "session"
		}
	}
	// Check the raw JSON for backward compatibility with boolean values
	if c.Lore.curateOnDisposeRaw != nil {
		raw := string(c.Lore.curateOnDisposeRaw)
		if raw == "false" {
			return "never"
		}
		if raw == "true" {
			return "session"
		}
	}
	return "session"
}

// GetLorePublicRuleMode returns the configured public rule mode.
// Returns "direct_push" or "create_pr". Defaults to "direct_push".
func (c *Config) GetLorePublicRuleMode() string {
	if c == nil {
		return "direct_push"
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Lore == nil {
		return "direct_push"
	}
	return c.Lore.GetPublicRuleMode()
}

// GetLoreCurateDebounceMs returns the debounce interval for auto-curation in milliseconds.
// Defaults to 30000 (30 seconds).
func (c *Config) GetLoreCurateDebounceMs() int {
	if c == nil {
		return 30000
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Lore == nil || c.Lore.CurateDebounceMs <= 0 {
		return 30000
	}
	return c.Lore.CurateDebounceMs
}

// GetLorePruneAfterDays returns the number of days before pruning applied/dismissed entries.
// Defaults to 30.
func (c *Config) GetLorePruneAfterDays() int {
	if c == nil {
		return 30
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Lore == nil || c.Lore.PruneAfterDays <= 0 {
		return 30
	}
	return c.Lore.PruneAfterDays
}

// GetLoreInstructionFiles returns the instruction file patterns managed by the lore curator.
// Defaults to DefaultInstructionFiles if not configured.
func (c *Config) GetLoreInstructionFiles() []string {
	if c == nil {
		return DefaultInstructionFiles
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Lore != nil && len(c.Lore.InstructionFiles) > 0 {
		return c.Lore.InstructionFiles
	}
	return DefaultInstructionFiles
}

// GetAutolearnEnabled returns whether the autolearn system is enabled.
// Defaults to true if not explicitly configured.
func (c *Config) GetAutolearnEnabled() bool {
	if c == nil {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Autolearn == nil || c.Autolearn.Enabled == nil {
		return true
	}
	return *c.Autolearn.Enabled
}

// GetAutolearnTarget returns the configured autolearn curator LLM target.
// Falls back to the compound target if not explicitly configured.
func (c *Config) GetAutolearnTarget() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Autolearn != nil && c.Autolearn.Target != "" {
		return c.Autolearn.Target
	}
	return c.getCompoundTargetLocked()
}

// GetAutolearnTargetRaw returns the explicitly configured autolearn curator LLM target
// without any fallback. Returns "" if no target is set.
// Use this for config UI display; use GetAutolearnTarget() for runtime behavior.
func (c *Config) GetAutolearnTargetRaw() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Autolearn == nil {
		return ""
	}
	return c.Autolearn.Target
}

// GetAutolearnAutoPR returns whether to auto-create a PR after pushing an autolearn branch.
// Defaults to false if not explicitly configured.
func (c *Config) GetAutolearnAutoPR() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Autolearn == nil || c.Autolearn.AutoPR == nil {
		return false
	}
	return *c.Autolearn.AutoPR
}

// GetAutolearnCurateOnDispose returns the autolearn curate-on-dispose mode.
// Returns "session", "workspace", or "never". Defaults to "session".
func (c *Config) GetAutolearnCurateOnDispose() string {
	if c == nil {
		return "session"
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Autolearn == nil {
		return "session"
	}
	if c.Autolearn.CurateOnDispose != "" {
		switch c.Autolearn.CurateOnDispose {
		case "session", "workspace", "never":
			return c.Autolearn.CurateOnDispose
		default:
			return "session"
		}
	}
	if c.Autolearn.curateOnDisposeRaw != nil {
		raw := string(c.Autolearn.curateOnDisposeRaw)
		if raw == "false" {
			return "never"
		}
		if raw == "true" {
			return "session"
		}
	}
	return "session"
}

// GetAutolearnDebounceMs returns the autolearn debounce interval for auto-curation in milliseconds.
// Defaults to 30000 (30 seconds).
func (c *Config) GetAutolearnDebounceMs() int {
	if c == nil {
		return 30000
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Autolearn == nil || c.Autolearn.CurateDebounceMs <= 0 {
		return 30000
	}
	return c.Autolearn.CurateDebounceMs
}

// GetAutolearnPruneAfterDays returns the number of days before pruning applied/dismissed autolearn entries.
// Defaults to 30.
func (c *Config) GetAutolearnPruneAfterDays() int {
	if c == nil {
		return 30
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Autolearn == nil || c.Autolearn.PruneAfterDays <= 0 {
		return 30
	}
	return c.Autolearn.PruneAfterDays
}

// GetAutolearnInstructionFiles returns the instruction file patterns managed by the autolearn curator.
// Defaults to DefaultInstructionFiles if not configured.
func (c *Config) GetAutolearnInstructionFiles() []string {
	if c == nil {
		return DefaultInstructionFiles
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Autolearn != nil && len(c.Autolearn.InstructionFiles) > 0 {
		return c.Autolearn.InstructionFiles
	}
	return DefaultInstructionFiles
}

// GetAutolearnPublicRuleMode returns the configured autolearn public rule mode.
// Returns "direct_push" or "create_pr". Defaults to "direct_push".
func (c *Config) GetAutolearnPublicRuleMode() string {
	if c == nil {
		return "direct_push"
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Autolearn == nil {
		return "direct_push"
	}
	return c.Autolearn.GetPublicRuleMode()
}

// DefaultOverlayPaths are always watched for all repos.
// Note: .schmux/lore.jsonl is NOT an overlay path — lore is one-directional
// (workspaces write, backend reads) and should not be broadcast via compounding.
var DefaultOverlayPaths = []string{
	".claude/settings.local.json",
}

// GetOverlayPaths returns the deduplicated union of hardcoded defaults,
// global config paths, and repo-specific paths for the given repo name.
func (c *Config) GetOverlayPaths(repoName string) []string {
	if c == nil {
		return append([]string(nil), DefaultOverlayPaths...)
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	seen := make(map[string]bool)
	var paths []string

	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}

	for _, p := range DefaultOverlayPaths {
		add(p)
	}
	if c.Overlay != nil {
		for _, p := range c.Overlay.Paths {
			add(p)
		}
	}
	for _, repo := range c.Repos {
		if repo.Name == repoName {
			for _, p := range repo.OverlayPaths {
				add(p)
			}
			break
		}
	}
	return paths
}

// GetConflictResolveTarget returns the configured conflict resolution target name, if any.
func (c *Config) GetConflictResolveTarget() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ConflictResolve == nil {
		return ""
	}
	return strings.TrimSpace(c.ConflictResolve.Target)
}

// GetFlightNameTarget returns the configured flight name target, if any.
func (c *Config) GetFlightNameTarget() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.FlightName == nil {
		return ""
	}
	return strings.TrimSpace(c.FlightName.Target)
}

// GetFlightPilotTarget returns the configured flight pilot target, if any.
func (c *Config) GetFlightPilotTarget() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.FlightPilot == nil {
		return ""
	}
	return strings.TrimSpace(c.FlightPilot.Target)
}

// GetConflictResolveTimeoutMs returns the per-call conflict resolution timeout in ms.
// Defaults to 120000 (2 minutes).
func (c *Config) GetConflictResolveTimeoutMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ConflictResolve == nil || c.ConflictResolve.TimeoutMs <= 0 {
		return DefaultConflictResolveTimeoutMs
	}
	return c.ConflictResolve.TimeoutMs
}

// GetPrReviewTarget returns the configured target for PR review sessions.
func (c *Config) GetPrReviewTarget() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.PrReview == nil {
		return ""
	}
	return strings.TrimSpace(c.PrReview.Target)
}

// GetCommitMessageTarget returns the configured target for commit message generation.
func (c *Config) GetCommitMessageTarget() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.CommitMessage == nil {
		return ""
	}
	return strings.TrimSpace(c.CommitMessage.Target)
}

// GetDesyncEnabled returns whether desync diagnostics are enabled.
func (c *Config) GetDesyncEnabled() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Desync == nil || c.Desync.Enabled == nil {
		return false
	}
	return *c.Desync.Enabled
}

// GetDesyncTarget returns the configured target for desync diagnostic capture sessions.
func (c *Config) GetDesyncTarget() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Desync == nil {
		return ""
	}
	return strings.TrimSpace(c.Desync.Target)
}

// GetIOWorkspaceTelemetryEnabled returns whether I/O workspace telemetry is enabled.
func (c *Config) GetIOWorkspaceTelemetryEnabled() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.IOWorkspaceTelemetry == nil || c.IOWorkspaceTelemetry.Enabled == nil {
		return false
	}
	return *c.IOWorkspaceTelemetry.Enabled
}

// GetIOWorkspaceTelemetryTarget returns the configured target for I/O workspace telemetry.
func (c *Config) GetIOWorkspaceTelemetryTarget() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.IOWorkspaceTelemetry == nil {
		return ""
	}
	return strings.TrimSpace(c.IOWorkspaceTelemetry.Target)
}

// GetClientPerformanceEnabled returns whether client performance recording is enabled.
func (c *Config) GetClientPerformanceEnabled() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ClientPerformance == nil || c.ClientPerformance.Enabled == nil {
		return false
	}
	return *c.ClientPerformance.Enabled
}

// GetClientPerformanceRepo returns the configured repo name for the performance chat.
func (c *Config) GetClientPerformanceRepo() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ClientPerformance == nil {
		return ""
	}
	return strings.TrimSpace(c.ClientPerformance.Repo)
}

// GetClientPerformanceTarget returns the configured target for the performance chat.
func (c *Config) GetClientPerformanceTarget() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ClientPerformance == nil {
		return ""
	}
	return strings.TrimSpace(c.ClientPerformance.Target)
}

// GetFenceAnalyzeEnabled returns whether the Analyze fence button is enabled.
func (c *Config) GetFenceAnalyzeEnabled() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.FenceAnalyze == nil || c.FenceAnalyze.Enabled == nil {
		return false
	}
	return *c.FenceAnalyze.Enabled
}

// GetFenceAnalyzeTarget returns the configured target for the fence-analysis agent.
func (c *Config) GetFenceAnalyzeTarget() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.FenceAnalyze == nil {
		return ""
	}
	return strings.TrimSpace(c.FenceAnalyze.Target)
}

// GetNotificationSoundEnabled returns whether notification sounds are enabled.
// Defaults to true (sounds enabled) unless explicitly disabled.
func (c *Config) GetNotificationSoundEnabled() bool {
	if c == nil {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Notifications == nil {
		return true
	}
	return !c.Notifications.SoundDisabled
}

// GetConfirmBeforeClose returns whether the browser should show a "Leave site?" dialog on tab close.
// Defaults to false (no confirmation).
func (c *Config) GetConfirmBeforeClose() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Notifications == nil {
		return false
	}
	return c.Notifications.ConfirmBeforeClose
}

// GetSuggestDisposeAfterPush returns whether to prompt disposing workspace after pushing to main.
// Defaults to true (prompt enabled) unless explicitly disabled.
func (c *Config) GetSuggestDisposeAfterPush() bool {
	if c == nil {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Notifications == nil || c.Notifications.SuggestDisposeAfterPush == nil {
		return true
	}
	return *c.Notifications.SuggestDisposeAfterPush
}

// FindRepo finds a repository by name.
func (c *Config) FindRepo(name string) (Repo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, repo := range c.Repos {
		if repo.Name == name {
			return repo, true
		}
	}
	return Repo{}, false
}

// FindRepoByURL finds a repository by its URL.
// Uses a lazily-built cache for O(1) lookups. Thread-safe.
func (c *Config) FindRepoByURL(url string) (Repo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	c.repoURLMu.RLock()
	if c.repoURLCache != nil {
		repo, found := c.repoURLCache[url]
		c.repoURLMu.RUnlock()
		return repo, found
	}
	c.repoURLMu.RUnlock()

	c.repoURLMu.Lock()
	defer c.repoURLMu.Unlock()
	// Double-check after acquiring write lock
	if c.repoURLCache == nil {
		c.repoURLCache = make(map[string]Repo, len(c.Repos))
		for _, repo := range c.Repos {
			c.repoURLCache[repo.URL] = repo
		}
	}
	repo, found := c.repoURLCache[url]
	return repo, found
}

// GetRunTarget finds a run target by name.
func (c *Config) GetRunTarget(name string) (RunTarget, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, target := range c.RunTargets {
		if target.Name == name {
			return target, true
		}
	}
	return RunTarget{}, false
}

// Reload reloads the configuration from disk and replaces this Config struct.
func (c *Config) Reload() error {
	c.mu.RLock()
	configPath := c.path
	c.mu.RUnlock()

	if configPath == "" {
		return fmt.Errorf("config path not set: use Load() or CreateDefault() with a path")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var newCfg Config
	if err := json.Unmarshal(data, &newCfg); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}
	newCfg.applyLoreAlias()

	// Validate before applying (matches Load behavior)
	if err := newCfg.Validate(); err != nil {
		return err
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	// Expand workspace path (handle ~)
	if newCfg.WorkspacePath != "" && newCfg.WorkspacePath[0] == '~' {
		newCfg.WorkspacePath = filepath.Join(homeDir, newCfg.WorkspacePath[1:])
	}
	// Expand base repos path (handle ~)
	if newCfg.WorktreeBasePath != "" && newCfg.WorktreeBasePath[0] == '~' {
		newCfg.WorktreeBasePath = filepath.Join(homeDir, newCfg.WorktreeBasePath[1:])
	}
	newCfg.expandNetworkPaths(homeDir)

	// Populate bare_path for repos (matches Load behavior)
	newCfg.path = configPath
	newCfg.populateBarePaths()

	// Replace all JSON fields atomically, preserving runtime fields (mu, caches).
	c.mu.Lock()
	c.ConfigData = newCfg.ConfigData
	c.path = configPath
	c.mu.Unlock()

	// Invalidate repo URL cache
	c.invalidateRepoURLCache()

	return nil
}

// CreateDefault creates a default config with the given config file path.
// The path is stored so that subsequent Save() calls write to the same location.
// If build defaults are embedded (via build_defaults.json), they are overlaid
// onto the Go defaults so that deployment-specific values take effect on first run.
func CreateDefault(configPath string) *Config {
	cfg := &Config{
		ConfigData: ConfigData{
			ConfigVersion:              version.Version,
			WorkspacePath:              filepath.Join(filepath.Dir(configPath), "workspaces"),
			Repos:                      []Repo{},
			RunTargets:                 []RunTarget{},
			QuickLaunch:                []QuickLaunch{},
			ExternalDiffCommands:       []ExternalDiffCommand{},
			ExternalDiffCleanupAfterMs: DefaultExternalDiffCleanupAfterMs,
			Pastebin:                   []string{},
		},
		path: configPath,
	}

	// Apply embedded build defaults (no-op when build_defaults.json is absent).
	if err := applyBuildDefaults(cfg); err != nil {
		if pkgLogger != nil {
			pkgLogger.Warn("failed to apply build defaults", "err", err)
		}
	}

	// Resolve template variables (e.g. ${USER}) in the seeded config.
	if cfgJSON, err := json.Marshal(cfg); err == nil {
		resolved := resolveConfigTemplates(cfgJSON)
		_ = json.Unmarshal(resolved, cfg)
	}

	return cfg
}

// Load loads the configuration from the specified path.
// The path is stored so that subsequent Save() calls write to the same location.
// Load reads and parses the config file at configPath.
// NOTE: Load may modify the config file on disk as a side effect. Both Migrate()
// and populateBarePaths() perform one-time migrations that call Save() to persist
// detected changes (e.g., renamed fields, bare repo path detection). These saves
// are best-effort — if they fail, the in-memory config is still correct and the
// migration will be re-attempted on the next load.
func Load(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	// First pass: unmarshal into struct (for better error messages)
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		// Try to extract line and column from JSON errors
		if syntaxErr, ok := err.(*json.SyntaxError); ok {
			line, col := offsetToLineCol(data, syntaxErr.Offset)
			return nil, fmt.Errorf("%w: %s (line %d, column %d)", ErrInvalidConfig, syntaxErr.Error(), line, col)
		}
		if typeErr, ok := err.(*json.UnmarshalTypeError); ok {
			line, col := offsetToLineCol(data, typeErr.Offset)
			return nil, fmt.Errorf("%w: field %q expects %s, got %s (line %d, column %d)",
				ErrInvalidConfig, typeErr.Field, typeErr.Type, typeErr.Value, line, col)
		}
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	// Second pass: unmarshal to map to preserve old field names for migrations
	// (Now we know the JSON is valid)
	var rawJSON map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawJSON); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	// Store the config path early so Save() works during migration
	cfg.path = configPath

	// Apply migrations - each detects if it needs to run
	if err := cfg.Migrate(rawJSON); err != nil {
		return nil, fmt.Errorf("config migration failed: %w", err)
	}

	// Alias: populate Autolearn from Lore if only Lore is configured
	cfg.applyLoreAlias()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	// Validate config (workspace_path can be empty during wizard setup)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Expand workspace path (handle ~) - allow empty during wizard setup
	if cfg.WorkspacePath != "" && cfg.WorkspacePath[0] == '~' {
		cfg.WorkspacePath = filepath.Join(homeDir, cfg.WorkspacePath[1:])
	}
	// Expand worktree base path (handle ~)
	if cfg.WorktreeBasePath != "" && cfg.WorktreeBasePath[0] == '~' {
		cfg.WorktreeBasePath = filepath.Join(homeDir, cfg.WorktreeBasePath[1:])
	}
	cfg.expandNetworkPaths(homeDir)

	// Populate bare_path for repos that don't have it (migration)
	cfg.populateBarePaths()

	return &cfg, nil
}

// Migrate runs detection-based migrations on the config.
// Each migration in the registry checks if it needs to run via its Detect function.
// If any migration runs, the config is auto-saved to disk (best-effort).
func (c *Config) Migrate(rawJSON map[string]json.RawMessage) error {
	var ranAny []string
	for _, m := range migrations {
		if m.Detect(rawJSON, c) {
			if err := m.Apply(rawJSON, c); err != nil {
				return fmt.Errorf("migration %q failed: %w", m.Name, err)
			}
			ranAny = append(ranAny, m.Name)
		}
	}
	if len(ranAny) > 0 {
		// Log which migrations ran
		for _, name := range ranAny {
			fmt.Fprintf(os.Stderr, "[config] migration applied: %s\n", name)
		}
		// Best-effort save: if it fails (e.g., read-only config), the in-memory
		// config is still migrated correctly. Next load will attempt migration again.
		if err := c.Save(); err != nil {
			// Log warning but don't fail the load
			fmt.Fprintf(os.Stderr, "[config] warning: migration succeeded but could not save to disk: %v\n", err)
		}
	}
	return nil
}

// Save writes the config to the path it was loaded from or created with.
func (c *Config) Save() error {
	// Update config version under write lock, then marshal under read lock
	c.mu.Lock()
	configPath := c.path
	if configPath == "" {
		c.mu.Unlock()
		return fmt.Errorf("config path not set: use Load() or CreateDefault() with a path")
	}
	c.ConfigVersion = version.Version
	c.mu.Unlock()

	// Marshal under RLock, then release before file I/O
	c.mu.RLock()
	data, err := json.MarshalIndent(c, "", "  ")
	c.mu.RUnlock()

	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Mark the start of the save to suppress fsnotify events from our own write.
	// Set this BEFORE the file operation to ensure the grace window covers the
	// entire duration of the atomic write (including the rename operation that
	// generates filesystem events).
	c.mu.Lock()
	c.lastSaveCompletedAt = time.Now()
	c.mu.Unlock()

	// Ensure the directory exists
	schmuxDir := filepath.Dir(configPath)
	if schmuxDir != "." && schmuxDir != "" {
		if err := os.MkdirAll(schmuxDir, 0700); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
	}

	if err := fileutil.AtomicWriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Invalidate the repo URL cache since repos may have changed
	c.invalidateRepoURLCache()

	return nil
}

// DisableAuthRaw sets access_control.enabled=false in the config file at path
// WITHOUT running validation. It exists for `schmux auth disable` to recover
// from a lockout where the on-disk config may fail strict validation (Load()
// would reject it). It round-trips through the Config struct, matching Save's
// serialization, but deliberately skips Validate().
func DisableAuthRaw(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}
	if cfg.AccessControl == nil || !cfg.AccessControl.Enabled {
		return nil // already disabled
	}
	cfg.AccessControl.Enabled = false
	out, err := json.MarshalIndent(&cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	return nil
}

// TimeSinceLastSave returns the duration since the last Save() completed.
// Used by the config watcher to suppress fsnotify events from self-writes.
func (c *Config) TimeSinceLastSave() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.lastSaveCompletedAt.IsZero() {
		return time.Hour // never saved — treat as very old
	}
	return time.Since(c.lastSaveCompletedAt)
}

// FilePath returns the config file path.
func (c *Config) FilePath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.path
}

// ConfigExists checks if the config file exists.
func ConfigExists() bool {
	configPath := filepath.Join(schmuxdir.Get(), "config.json")
	_, err := os.Stat(configPath)
	return err == nil
}

// EnsureExists checks if config exists, and creates a default one if not.
// Returns true if config exists or was created, false on error.
func EnsureExists() (bool, error) {
	if ConfigExists() {
		return true, nil
	}

	configPath := filepath.Join(schmuxdir.Get(), "config.json")
	cfg := CreateDefault(configPath)

	if err := cfg.Save(); err != nil {
		return false, fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("Created default config at %s\n", configPath)

	return true, nil
}

// GetDashboardPollIntervalMs returns the dashboard sessions polling interval in ms. Defaults to 5000ms.
func (c *Config) GetDashboardPollIntervalMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Sessions == nil || c.Sessions.DashboardPollIntervalMs <= 0 {
		return 5000
	}
	return c.Sessions.DashboardPollIntervalMs
}

// GetNudgenikViewedBufferMs returns the viewed timestamp buffer in ms. Defaults to 5000ms.
func (c *Config) GetNudgenikViewedBufferMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Nudgenik == nil || c.Nudgenik.ViewedBufferMs <= 0 {
		return 5000
	}
	return c.Nudgenik.ViewedBufferMs
}

// GetNudgenikSeenIntervalMs returns the interval for marking sessions as seen in ms. Defaults to 2000ms.
func (c *Config) GetNudgenikSeenIntervalMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Nudgenik == nil || c.Nudgenik.SeenIntervalMs <= 0 {
		return 2000
	}
	return c.Nudgenik.SeenIntervalMs
}

// GetGitStatusPollIntervalMs returns the git status polling interval in ms. Defaults to 10000ms.
func (c *Config) GetGitStatusPollIntervalMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Sessions == nil || c.Sessions.GitStatusPollIntervalMs <= 0 {
		return 10000
	}
	return c.Sessions.GitStatusPollIntervalMs
}

// GetGitStatusWatchEnabled returns whether the git status file watcher is enabled. Defaults to true.
func (c *Config) GetGitStatusWatchEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Sessions == nil || c.Sessions.GitStatusWatchEnabled == nil {
		return true
	}
	return *c.Sessions.GitStatusWatchEnabled
}

// GetGitStatusWatchDebounceMs returns the git status watcher debounce interval in ms. Defaults to 1000.
func (c *Config) GetGitStatusWatchDebounceMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Sessions == nil || c.Sessions.GitStatusWatchDebounceMs <= 0 {
		return DefaultGitStatusWatchDebounceMs
	}
	return c.Sessions.GitStatusWatchDebounceMs
}

// GitStatusWatchDebounce returns the git status watcher debounce interval as a time.Duration.
func (c *Config) GitStatusWatchDebounce() time.Duration {
	return time.Duration(c.GetGitStatusWatchDebounceMs()) * time.Millisecond
}

// GetGitCloneTimeoutMs returns the git clone timeout in ms. Defaults to 300000 (5 min).
func (c *Config) GetGitCloneTimeoutMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Sessions == nil || c.Sessions.GitCloneTimeoutMs <= 0 {
		return DefaultGitCloneTimeoutMs
	}
	return c.Sessions.GitCloneTimeoutMs
}

// GetGitStatusTimeoutMs returns the git status timeout in ms. Defaults to 30000.
func (c *Config) GetGitStatusTimeoutMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Sessions == nil || c.Sessions.GitStatusTimeoutMs <= 0 {
		return DefaultGitStatusTimeoutMs
	}
	return c.Sessions.GitStatusTimeoutMs
}

// GetDisposeGracePeriodMs returns the dispose grace period in ms. Defaults to 5000 (5s).
func (c *Config) GetDisposeGracePeriodMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Sessions == nil || c.Sessions.DisposeGracePeriodMs <= 0 {
		return DefaultDisposeGracePeriodMs
	}
	return c.Sessions.DisposeGracePeriodMs
}

// DisposeGracePeriod returns the dispose grace period as a time.Duration.
func (c *Config) DisposeGracePeriod() time.Duration {
	return time.Duration(c.GetDisposeGracePeriodMs()) * time.Millisecond
}

// GetXtermQueryTimeoutMs returns the xterm query timeout in ms. Defaults to 5000.
func (c *Config) GetXtermQueryTimeoutMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Xterm == nil || c.Xterm.QueryTimeoutMs <= 0 {
		return DefaultXtermQueryTimeoutMs
	}
	return c.Xterm.QueryTimeoutMs
}

// GetXtermOperationTimeoutMs returns the xterm operation timeout in ms. Defaults to 10000.
func (c *Config) GetXtermOperationTimeoutMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Xterm == nil || c.Xterm.OperationTimeoutMs <= 0 {
		return DefaultXtermOperationTimeoutMs
	}
	return c.Xterm.OperationTimeoutMs
}

// GetXtermUseWebGL returns whether the WebGL renderer should be used. Defaults to true.
func (c *Config) GetXtermUseWebGL() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Xterm == nil || c.Xterm.UseWebGL == nil {
		return true
	}
	return *c.Xterm.UseWebGL
}

// GitCloneTimeout returns the git clone timeout as a time.Duration.
func (c *Config) GitCloneTimeout() time.Duration {
	return time.Duration(c.GetGitCloneTimeoutMs()) * time.Millisecond
}

// GitStatusTimeout returns the git status timeout as a time.Duration.
func (c *Config) GitStatusTimeout() time.Duration {
	return time.Duration(c.GetGitStatusTimeoutMs()) * time.Millisecond
}

// XtermQueryTimeout returns the xterm query timeout as a time.Duration.
func (c *Config) XtermQueryTimeout() time.Duration {
	return time.Duration(c.GetXtermQueryTimeoutMs()) * time.Millisecond
}

// XtermOperationTimeout returns the xterm operation timeout as a time.Duration.
func (c *Config) XtermOperationTimeout() time.Duration {
	return time.Duration(c.GetXtermOperationTimeoutMs()) * time.Millisecond
}

// GetBindAddress returns the address to bind the server to.
// Defaults to "127.0.0.1" (localhost only).
func (c *Config) GetBindAddress() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.getBindAddressLocked()
}

// getBindAddressLocked is the lock-free implementation. Caller must hold mu.
func (c *Config) getBindAddressLocked() string {
	if c.Network == nil || c.Network.BindAddress == "" {
		return "127.0.0.1"
	}
	return c.Network.BindAddress
}

// GetNetworkAccess returns whether the dashboard should be accessible from the local network.
// Returns true for any non-loopback bind address.
func (c *Config) GetNetworkAccess() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	addr := c.getBindAddressLocked()
	return addr != "" && addr != "127.0.0.1" && addr != "::1"
}

// GetPort returns the dashboard port: the SetListenPort override when set,
// else the configured port, defaulting to 7337.
func (c *Config) GetPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.hasListenPort {
		return c.listenPort
	}
	if c.Network == nil || c.Network.Port <= 0 {
		return 7337
	}
	return c.Network.Port
}

// SetListenPort overrides the dashboard port for this process without saving
// it. The daemon sets it from SCHMUX_PORT, where 0 asks the OS for a free
// port; the dashboard server then sets the port it bound, so the origin
// checks and every other GetPort reader see the real one.
func (c *Config) SetListenPort(port int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.listenPort = port
	c.hasListenPort = true
}

// GetTmuxSocketName returns the tmux socket name, defaulting to "schmux".
func (c *Config) GetTmuxSocketName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.TmuxSocketName == "" {
		return "schmux"
	}
	return c.TmuxSocketName
}

// GetPreviewMaxPerWorkspace returns the per-workspace preview limit.
func (c *Config) GetPreviewMaxPerWorkspace() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Network == nil || c.Network.PreviewMaxPerWorkspace <= 0 {
		return DefaultPreviewMaxPerWorkspace
	}
	return c.Network.PreviewMaxPerWorkspace
}

// GetPreviewMaxGlobal returns the global preview limit.
func (c *Config) GetPreviewMaxGlobal() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Network == nil || c.Network.PreviewMaxGlobal <= 0 {
		return DefaultPreviewMaxGlobal
	}
	return c.Network.PreviewMaxGlobal
}

// GetPreviewPortBase returns the base port for preview port block allocation.
func (c *Config) GetPreviewPortBase() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Network == nil || c.Network.PreviewPortBase <= 0 {
		return DefaultPreviewPortBase
	}
	return c.Network.PreviewPortBase
}

// GetPreviewPortBlockSize returns the number of ports per workspace preview block.
func (c *Config) GetPreviewPortBlockSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Network == nil || c.Network.PreviewPortBlockSize <= 0 {
		return DefaultPreviewPortBlockSize
	}
	return c.Network.PreviewPortBlockSize
}

// GetPublicBaseURL returns the public base URL for the dashboard.
func (c *Config) GetPublicBaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Network == nil {
		return ""
	}
	return strings.TrimSpace(c.Network.PublicBaseURL)
}

// GetTLSCertPath returns the TLS certificate path.
func (c *Config) GetTLSCertPath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.getTLSCertPathLocked()
}

// getTLSCertPathLocked is the lock-free implementation. Caller must hold mu.
func (c *Config) getTLSCertPathLocked() string {
	if c.Network == nil || c.Network.TLS == nil {
		return ""
	}
	return strings.TrimSpace(c.Network.TLS.CertPath)
}

// GetTLSKeyPath returns the TLS key path.
func (c *Config) GetTLSKeyPath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.getTLSKeyPathLocked()
}

// getTLSKeyPathLocked is the lock-free implementation. Caller must hold mu.
func (c *Config) getTLSKeyPathLocked() string {
	if c.Network == nil || c.Network.TLS == nil {
		return ""
	}
	return strings.TrimSpace(c.Network.TLS.KeyPath)
}

// GetTLSEnabled returns whether TLS is configured.
func (c *Config) GetTLSEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.getTLSCertPathLocked() != "" && c.getTLSKeyPathLocked() != ""
}

// GetDashboardHostname returns the configured dashboard hostname.
// Returns empty if the hostname doesn't resolve to a local interface,
// allowing callers to fall back to localhost.
func (c *Config) GetDashboardHostname() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Network == nil {
		return ""
	}
	hostname := strings.TrimSpace(c.Network.DashboardHostname)
	if hostname == "" {
		return ""
	}
	if !isLocalHostname(hostname) {
		return ""
	}
	return hostname
}

// isLocalHostname checks if a hostname resolves to an IP on this machine.
func isLocalHostname(hostname string) bool {
	// Match the machine's own hostname even if it doesn't resolve in DNS.
	if machineHost, err := os.Hostname(); err == nil && strings.EqualFold(hostname, machineHost) {
		return true
	}
	addrs, err := net.LookupHost(hostname)
	if err != nil {
		return false
	}
	localAddrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		for _, la := range localAddrs {
			if ipnet, ok := la.(*net.IPNet); ok && ipnet.IP.String() == a {
				return true
			}
		}
	}
	return false
}

// GetDashboardURL returns the full URL for the dashboard.
// If DashboardHostname is set, composes scheme://hostname:port.
// Otherwise falls back to GetPublicBaseURL.
func (c *Config) GetDashboardURL() string {
	hostname := c.GetDashboardHostname()
	if hostname == "" {
		return c.GetPublicBaseURL()
	}
	scheme := "http"
	if c.GetTLSEnabled() {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%d", scheme, hostname, c.GetPort())
}

// GetDashboardSXEnabled returns whether dashboard.sx HTTPS is enabled.
func (c *Config) GetDashboardSXEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Network == nil || c.Network.DashboardSX == nil {
		return false
	}
	return c.Network.DashboardSX.Enabled
}

// GetDashboardSXCode returns the dashboard.sx code.
func (c *Config) GetDashboardSXCode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Network == nil || c.Network.DashboardSX == nil {
		return ""
	}
	return c.Network.DashboardSX.Code
}

// GetDashboardSXIP returns the dashboard.sx IP address.
func (c *Config) GetDashboardSXIP() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Network == nil || c.Network.DashboardSX == nil {
		return ""
	}
	return c.Network.DashboardSX.IP
}

// GetDashboardSXEmail returns the email used for Let's Encrypt certificate provisioning.
func (c *Config) GetDashboardSXEmail() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Network == nil || c.Network.DashboardSX == nil {
		return ""
	}
	return c.Network.DashboardSX.Email
}

// GetDashboardSXHostname returns the full dashboard.sx hostname (e.g. "12345.dashboard.sx").
func (c *Config) GetDashboardSXHostname() string {
	code := c.GetDashboardSXCode()
	if code == "" {
		return ""
	}
	return code + ".dashboard.sx"
}

// GetAuthEnabled returns whether auth is enabled.
func (c *Config) GetAuthEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.AccessControl == nil {
		return false
	}
	return c.AccessControl.Enabled
}

// GetAuthProvider returns the auth provider (default: github).
func (c *Config) GetAuthProvider() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.AccessControl == nil || strings.TrimSpace(c.AccessControl.Provider) == "" {
		return DefaultAuthProvider
	}
	return c.AccessControl.Provider
}

// GetAuthSessionTTLMinutes returns the session TTL in minutes.
func (c *Config) GetAuthSessionTTLMinutes() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.AccessControl == nil || c.AccessControl.SessionTTLMinutes <= 0 {
		return DefaultAuthSessionTTLMinutes
	}
	return c.AccessControl.SessionTTLMinutes
}

func (c *Config) validateAccessControl(strict bool) ([]string, error) {
	if c.AccessControl == nil || !c.AccessControl.Enabled {
		return nil, nil
	}

	var warnings []string
	publicBaseURL := c.GetPublicBaseURL()
	if publicBaseURL == "" {
		warnings = append(warnings, "network.public_base_url is required when auth is enabled")
	} else if !IsValidPublicBaseURL(publicBaseURL) {
		warnings = append(warnings, "network.public_base_url must be https (http://localhost allowed)")
	}

	if provider := c.GetAuthProvider(); provider != "github" {
		warnings = append(warnings, fmt.Sprintf("access_control.auth.provider must be \"github\" (got %q)", provider))
	}

	certPath := c.GetTLSCertPath()
	keyPath := c.GetTLSKeyPath()
	if certPath == "" {
		warnings = append(warnings, "network.tls.cert_path is required when auth is enabled")
	}
	if keyPath == "" {
		warnings = append(warnings, "network.tls.key_path is required when auth is enabled")
	}
	if certPath != "" {
		if _, err := os.Stat(certPath); err != nil {
			warnings = append(warnings, fmt.Sprintf("network.tls.cert_path not readable: %v", err))
		}
	}
	if keyPath != "" {
		if _, err := os.Stat(keyPath); err != nil {
			warnings = append(warnings, fmt.Sprintf("network.tls.key_path not readable: %v", err))
		}
	}

	secrets, err := GetAuthSecrets()
	if err != nil {
		if strict {
			return nil, err
		}
		warnings = append(warnings, fmt.Sprintf("failed to read secrets.json: %v", err))
	} else {
		clientID := ""
		clientSecret := ""
		if secrets.GitHub != nil {
			clientID = strings.TrimSpace(secrets.GitHub.ClientID)
			clientSecret = strings.TrimSpace(secrets.GitHub.ClientSecret)
		}
		if clientID == "" {
			warnings = append(warnings, "auth.github.client_id is required when auth is enabled")
		}
		if clientSecret == "" {
			warnings = append(warnings, "auth.github.client_secret is required when auth is enabled")
		}
	}

	if strict && len(warnings) > 0 {
		return nil, fmt.Errorf("%w: auth config invalid: %s", ErrInvalidConfig, strings.Join(warnings, "; "))
	}
	return warnings, nil
}

// IsValidPublicBaseURL checks if a public base URL is valid for auth.
func IsValidPublicBaseURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	if parsed.Scheme == "http" {
		host := strings.Split(parsed.Host, ":")[0]
		return host == "localhost"
	}
	return false
}

// offsetToLineCol converts a byte offset to line and column numbers (1-indexed).
func offsetToLineCol(data []byte, offset int64) (line, col int) {
	line = 1
	col = 1
	for i := int64(0); i < offset && i < int64(len(data)); i++ {
		if data[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

// GetRemoteAccessEnabled returns whether remote access is enabled.
// Defaults to false (disabled). Users must explicitly set "enabled": true.
// For backward compatibility, "disabled": true in existing configs is respected
// (inverted to enabled=false). If both fields are set, "enabled" takes precedence.
func (c *Config) GetRemoteAccessEnabled() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.RemoteAccess == nil {
		return false
	}
	// New field takes precedence
	if c.RemoteAccess.Enabled != nil {
		return *c.RemoteAccess.Enabled
	}
	// Backward compat: invert old "disabled" field
	if c.RemoteAccess.Disabled != nil {
		return !*c.RemoteAccess.Disabled
	}
	return false
}

// GetRemoteAccessTimeoutMinutes returns the tunnel auto-kill timeout in minutes.
// Defaults to 120 (2 hours) when not configured. Set to -1 in config to disable.
func (c *Config) GetRemoteAccessTimeoutMinutes() int {
	if c == nil {
		return 120
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.RemoteAccess == nil || c.RemoteAccess.TimeoutMinutes == 0 {
		return 120
	}
	if c.RemoteAccess.TimeoutMinutes < 0 {
		return 0
	}
	return c.RemoteAccess.TimeoutMinutes
}

// GetRemoteAccessNtfyTopic returns the ntfy.sh topic for push notifications.
func (c *Config) GetRemoteAccessNtfyTopic() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.RemoteAccess == nil || c.RemoteAccess.Notify == nil {
		return ""
	}
	return strings.TrimSpace(c.RemoteAccess.Notify.NtfyTopic)
}

// GetRemoteAccessNotifyCommand returns the custom notification command.
func (c *Config) GetRemoteAccessNotifyCommand() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.RemoteAccess == nil || c.RemoteAccess.Notify == nil {
		return ""
	}
	return strings.TrimSpace(c.RemoteAccess.Notify.Command)
}

// GetRemoteAccessPasswordHash returns the bcrypt-hashed password for remote access auth.
func (c *Config) GetRemoteAccessPasswordHash() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.RemoteAccess == nil {
		return ""
	}
	return c.RemoteAccess.PasswordHash
}

// SetRemoteAccessPasswordHash sets the bcrypt-hashed password for remote access auth.
func (c *Config) SetRemoteAccessPasswordHash(hash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.RemoteAccess == nil {
		c.RemoteAccess = &RemoteAccessConfig{}
	}
	c.RemoteAccess.PasswordHash = hash
}

// GetRemoteAccessAllowAutoDownload returns whether auto-downloading cloudflared is allowed.
// Defaults to false (disabled). Set to true in config to allow unverified binary downloads.
func (c *Config) GetRemoteAccessAllowAutoDownload() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.RemoteAccess == nil || c.RemoteAccess.AllowAutoDownload == nil {
		return false
	}
	return *c.RemoteAccess.AllowAutoDownload
}

// GetTelemetryEnabled returns whether telemetry is enabled.
// Defaults to true if not explicitly configured.
func (c *Config) GetTelemetryEnabled() bool {
	if c == nil {
		return true
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Telemetry == nil || c.Telemetry.Enabled == nil {
		return true
	}
	return *c.Telemetry.Enabled
}

// GetTelemetryCommand returns the external command for telemetry events.
// Returns nil if not configured. The argv-array form prevents the templated
// shell injection bug class (spec §2.1).
func (c *Config) GetTelemetryCommand() ShellCommand {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Telemetry == nil {
		return nil
	}
	return c.Telemetry.Command
}

// GetInstallationID returns the installation ID for telemetry.
// Returns empty string if not set.
func (c *Config) GetInstallationID() string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.InstallationID
}

// SetInstallationID sets the installation ID.
func (c *Config) SetInstallationID(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.InstallationID = id
}

// populateBarePaths detects and populates bare_path for repos that don't have it.
// This is a one-time migration for existing repos - new repos get bare_path set on creation.
// Detection order:
//  1. Check worktree base dir (repos/) for legacy flat path (repo.git) that matches URL
//  2. Check query dir (query/) for legacy flat path that matches URL
//  3. Check worktree base dir for namespaced path (owner/repo.git) that matches URL
//  4. Check query dir for namespaced path that matches URL
//  5. Fall back to {name}.git if nothing exists on disk
func (c *Config) populateBarePaths() {
	var changed bool
	reposPath := c.GetWorktreeBasePath()
	queryPath := c.GetQueryRepoPath()

	for i := range c.Repos {
		repo := &c.Repos[i]

		if repo.BarePath != "" {
			// Validate that the configured bare_path actually exists on disk.
			// If it doesn't, re-detect — the repo may have been cloned under
			// a namespaced path (e.g. "owner/repo.git") while the config has
			// the flat name ("repo.git").
			exists := false
			for _, basePath := range []string{reposPath, queryPath} {
				fullPath := filepath.Join(basePath, repo.BarePath)
				if _, err := os.Stat(fullPath); err == nil {
					exists = true
					break
				}
			}
			if exists {
				continue // bare_path is valid
			}
			// Only correct if we find the actual repo on disk (no fallback)
			if found := findBarePathOnDisk([]string{reposPath, queryPath}, repo.URL); found != "" && found != repo.BarePath {
				fmt.Fprintf(os.Stderr, "[config] corrected bare_path for repo %q: %s → %s\n", repo.Name, repo.BarePath, found)
				repo.BarePath = found
				changed = true
			}
			continue
		}

		// Empty BarePath — detect with fallback for new repos
		barePath := detectExistingBarePath([]string{reposPath, queryPath}, repo.URL, repo.Name)
		if barePath != "" {
			repo.BarePath = barePath
			changed = true
			fmt.Fprintf(os.Stderr, "[config] migrated bare_path for repo %q: %s\n", repo.Name, barePath)
		}
	}

	if changed {
		// Best-effort save
		if err := c.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "[config] warning: could not save migrated bare_paths: %v\n", err)
		}
	}
}

// detectExistingBarePath detects the bare path for a repo by checking what's on disk.
// Checks multiple base paths (repos/, query/) in order.
// Returns the relative path (e.g., "schmux.git" or "owner/repo.git").
func detectExistingBarePath(basePaths []string, repoURL, repoName string) string {
	if found := findBarePathOnDisk(basePaths, repoURL); found != "" {
		return found
	}

	// Fall back to {name}.git for new repos or if nothing on disk
	if repoName != "" {
		return repoName + ".git"
	}

	// Last resort: use URL-derived name
	legacyName := extractRepoNameFromURL(repoURL)
	if legacyName != "" {
		return legacyName + ".git"
	}

	return ""
}

// findBarePathOnDisk looks for an existing bare repo on disk that matches the given URL.
// Returns the relative path if found, or empty string if nothing matches on disk.
func findBarePathOnDisk(basePaths []string, repoURL string) string {
	legacyName := extractRepoNameFromURL(repoURL)
	owner, repo := parseGitHubURL(repoURL)

	for _, basePath := range basePaths {
		// 1. Check for legacy flat path (repo.git)
		if legacyName != "" {
			legacyPath := filepath.Join(basePath, legacyName+".git")
			if bareRepoMatchesURL(legacyPath, repoURL) {
				return legacyName + ".git"
			}
		}

		// 2. Check for namespaced GitHub path (owner/repo.git)
		if owner != "" && repo != "" {
			namespacedPath := filepath.Join(basePath, owner, repo+".git")
			if bareRepoMatchesURL(namespacedPath, repoURL) {
				return filepath.Join(owner, repo+".git")
			}
		}
	}

	return ""
}

// extractRepoNameFromURL extracts the repository name from a URL.
// Handles: git@github.com:user/myrepo.git, https://github.com/user/myrepo.git, etc.
func extractRepoNameFromURL(repoURL string) string {
	// Strip .git suffix
	name := strings.TrimSuffix(repoURL, ".git")

	// Get last path component (handle both / and : separators)
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if idx := strings.LastIndex(name, ":"); idx >= 0 {
		name = name[idx+1:]
	}

	return name
}

// parseGitHubURL extracts owner and repo from a GitHub URL.
// Returns empty strings if not a GitHub URL.
func parseGitHubURL(repoURL string) (owner, repo string) {
	// Handle git@github.com:owner/repo.git
	if strings.HasPrefix(repoURL, "git@github.com:") {
		path := strings.TrimPrefix(repoURL, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
		return "", ""
	}

	// Handle https://github.com/owner/repo.git
	if strings.HasPrefix(repoURL, "https://github.com/") {
		path := strings.TrimPrefix(repoURL, "https://github.com/")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
		return "", ""
	}

	return "", ""
}

// bareRepoMatchesURL checks if a bare repo at the given path has the expected origin URL.
// bareRepoOrigin returns the recorded remote.origin.url of a bare repo, or
// "" if it cannot be read (absent, not a git repo, or no origin configured).
func bareRepoOrigin(repoPath string) string {
	if _, err := os.Stat(repoPath); err != nil {
		return "" // Doesn't exist
	}
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return "" // Can't verify
	}
	return strings.TrimSpace(string(output))
}

func bareRepoMatchesURL(repoPath, expectedURL string) bool {
	return bareRepoOrigin(repoPath) == expectedURL
}

// GetCommStyles returns the comm styles map (base tool name -> style name).
func (c *Config) GetCommStyles() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.CommStyles == nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(c.CommStyles))
	for k, v := range c.CommStyles {
		result[k] = v
	}
	return result
}

// GetEnabledModels returns the enabled models map (modelID -> preferred tool).
func (c *Config) GetEnabledModels() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Models == nil {
		return nil
	}
	result := make(map[string]string, len(c.Models.Enabled))
	for modelID, runner := range c.Models.Enabled {
		result[modelID] = runner
	}
	return result
}

// SetEnabledModels sets the enabled models map.
func (c *Config) SetEnabledModels(enabled map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Models == nil {
		c.Models = &ModelsConfig{}
	}
	c.Models.Enabled = enabled
}

// PreferredTool returns the user's preferred tool for a model, or empty string.
func (c *Config) PreferredTool(modelID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Models == nil || c.Models.Enabled == nil {
		return ""
	}
	return c.Models.Enabled[modelID]
}

// AddRepoOverlayPaths adds overlay paths to a repo's config, deduplicating against existing paths.
func (c *Config) AddRepoOverlayPaths(repoName string, paths []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.Repos {
		if c.Repos[i].Name == repoName {
			existing := make(map[string]bool)
			for _, p := range c.Repos[i].OverlayPaths {
				existing[p] = true
			}
			for _, p := range paths {
				if !existing[p] {
					c.Repos[i].OverlayPaths = append(c.Repos[i].OverlayPaths, p)
					existing[p] = true
				}
			}
			return
		}
	}
}
