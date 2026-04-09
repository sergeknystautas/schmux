package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/fileutil"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
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
	DefaultDisposeGracePeriodMs       = 30000 // 30 seconds

	// Default auth session TTL in minutes
	DefaultAuthSessionTTLMinutes = 1440
)

// Source code management constants
const (
	SourceCodeManagementGitWorktree = "git-worktree" // default: use git worktrees
	SourceCodeManagementGit         = "git"          // vanilla full clone
)

// Config represents the application configuration.
type Config struct {
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
	Compound                   *CompoundConfig             `json:"compound,omitempty"`
	Overlay                    *OverlayConfig              `json:"overlay,omitempty"`
	Lore                       *LoreConfig                 `json:"lore,omitempty"`
	Sessions                   *SessionsConfig             `json:"sessions,omitempty"`
	Xterm                      *XtermConfig                `json:"xterm,omitempty"`
	Network                    *NetworkConfig              `json:"network,omitempty"`
	AccessControl              *AccessControlConfig        `json:"access_control,omitempty"`
	PrReview                   *PrReviewConfig             `json:"pr_review,omitempty"`
	CommitMessage              *CommitMessageConfig        `json:"commit_message,omitempty"`
	Desync                     *DesyncConfig               `json:"desync,omitempty"`
	IOWorkspaceTelemetry       *IOWorkspaceTelemetryConfig `json:"io_workspace_telemetry,omitempty"`
	Notifications              *NotificationsConfig        `json:"notifications,omitempty"`
	RemoteFlavors              []RemoteFlavor              `json:"remote_flavors,omitempty"`
	RemoteProfiles             []RemoteProfile             `json:"remote_profiles,omitempty"`
	RemoteWorkspace            *RemoteWorkspaceConfig      `json:"remote_workspace,omitempty"`
	RemoteAccess               *RemoteAccessConfig         `json:"remote_access,omitempty"`
	Models                     *ModelsConfig               `json:"models,omitempty"`
	CommStyles                 map[string]string           `json:"comm_styles,omitempty"`
	Subreddit                  *SubredditConfig            `json:"subreddit,omitempty"`
	Repofeed                   *RepofeedConfig             `json:"repofeed,omitempty"`
	FloorManager               *FloorManagerConfig         `json:"floor_manager,omitempty"`
	SaplingCommands            SaplingCommands             `json:"sapling_commands,omitempty"`
	BuiltInSkills              map[string]bool             `json:"built_in_skills,omitempty"`
	TmuxBinary                 string                      `json:"tmux_binary,omitempty"`
	TmuxSocketName             string                      `json:"tmux_socket_name,omitempty"`
	RecycleWorkspaces          bool                        `json:"recycle_workspaces,omitempty"`
	LocalEchoRemote            bool                        `json:"local_echo_remote,omitempty"`
	DebugUI                    bool                        `json:"debug_ui,omitempty"`
	Timelapse                  *TimelapseConfig            `json:"timelapse,omitempty"`

	// Telemetry settings
	TelemetryEnabled *bool  `json:"telemetry_enabled,omitempty"` // default true
	InstallationID   string `json:"installation_id,omitempty"`   // UUID for anonymous tracking

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
}

// RemoteFlavor represents a remote host flavor configuration.
// Each flavor defines a type of remote environment that can be connected to.
type RemoteFlavor struct {
	ID            string `json:"id"`             // e.g., "gpu_ml_large" (auto-generated if not provided)
	Flavor        string `json:"flavor"`         // e.g., "gpu:ml-large" (the flavor/environment identifier)
	DisplayName   string `json:"display_name"`   // e.g., "GPU ML Large" (shown in UI)
	VCS           string `json:"vcs"`            // "git" or "sapling"
	WorkspacePath string `json:"workspace_path"` // e.g., "~/workspace" (path on remote host)

	// ConnectCommand is a Go template for the command to connect to a remote host.
	// Schmux will automatically append "tmux -CC new-session -A -s schmux" to this command.
	// If your transport requires a separator (e.g., "--" for SSH), include it in your command.
	//
	// Available template variables:
	//   {{.Flavor}} - Remote flavor identifier (from the Flavor field above)
	//
	// Examples:
	//   SSH: "ssh -tt {{.Flavor}} --"
	//   Custom: "cloud-ssh connect {{.Flavor}}"
	//   Docker: "docker exec -it {{.Flavor}}"
	//   AWS SSM: "aws ssm start-session --target {{.Flavor}}"
	//
	// If empty, defaults to "ssh -tt {{.Flavor}} --".
	//
	// Note: Schmux appends the tmux control mode command automatically.
	ConnectCommand string `json:"connect_command,omitempty"`

	// ReconnectCommand is a Go template for reconnecting to an existing remote host.
	// Schmux will automatically append "tmux -CC new-session -A -s schmux" to this command.
	// If your transport requires a separator (e.g., "--" for SSH), include it in your command.
	//
	// Available template variables:
	//   {{.Hostname}} - Remote host hostname (discovered after initial connection)
	//   {{.Flavor}} - Remote flavor identifier
	//
	// Examples:
	//   SSH: "ssh -tt {{.Hostname}} --"
	//   Custom: "cloud-ssh reconnect {{.Hostname}}"
	//
	// If empty, uses ConnectCommand with Hostname instead of Flavor.
	//
	// Note: Schmux appends the tmux control mode command automatically.
	ReconnectCommand string `json:"reconnect_command,omitempty"`

	// ProvisionCommand is a Go template for provisioning the workspace on first connection.
	// This runs ONCE after the initial connection, before creating any sessions.
	//
	// Available template variables:
	//   {{.WorkspacePath}} - The configured workspace_path
	//   {{.Repo}} - Repository URL (from spawn request)
	//   {{.Branch}} - Branch name (from spawn request)
	//   {{.VCS}} - "git" or "sapling"
	//
	// Examples:
	//   Git: "git clone {{.Repo}} {{.WorkspacePath}} && cd {{.WorkspacePath}} && git checkout {{.Branch}}"
	//   Docker: "git clone {{.Repo}} {{.WorkspacePath}} && cd {{.WorkspacePath}} && npm install"
	//
	// If empty, assumes workspace is pre-provisioned (e.g., cloud development environments).
	//
	// Note: Provisioning happens once per host. Reconnecting skips this step.
	ProvisionCommand string `json:"provision_command,omitempty"`

	// VSCodeCommandTemplate is a Go template for launching VS Code on remote workspaces.
	// This allows per-flavor VSCode configuration (e.g., different SSH configs per remote).
	//
	// Available template variables:
	//   {{.VSCodePath}} - Path to the local VS Code executable
	//   {{.Hostname}} - Remote host hostname
	//   {{.Path}} - Workspace path on remote host
	//
	// Examples:
	//   Standard Remote-SSH: "{{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}"
	//   Custom remote: "{{.VSCodePath}} --folder-uri vscode-remote://custom+{{.Hostname}}{{.Path}}"
	//   Jump host: "{{.VSCodePath}} --remote ssh-remote+jump-{{.Hostname}} {{.Path}}"
	//
	// If empty, uses the global remote_workspace.vscode_command_template setting.
	// If that's also empty, defaults to: "{{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}"
	VSCodeCommandTemplate string `json:"vscode_command_template,omitempty"`

	// HostnameRegex is a regular expression for extracting the hostname from provisioning
	// command STDOUT output. The first capture group is used as the hostname.
	//
	// Examples:
	//   SSH ControlMaster: "Establish ControlMaster connection to (\\S+)"
	//   Custom banner: "Connected to host: (\\S+)"
	//   IP address: "allocated (\\d+\\.\\d+\\.\\d+\\.\\d+)"
	//
	// If empty, defaults to: "Establish ControlMaster connection to (\\S+)"
	HostnameRegex string `json:"hostname_regex,omitempty"`
}

// RemoteProfile represents a remote host profile configuration.
// A profile groups shared connection settings with one or more flavors (machine types).
type RemoteProfile struct {
	ID                    string                `json:"id"`
	DisplayName           string                `json:"display_name"`
	VCS                   string                `json:"vcs"`
	WorkspacePath         string                `json:"workspace_path"`
	ConnectCommand        string                `json:"connect_command,omitempty"`
	ReconnectCommand      string                `json:"reconnect_command,omitempty"`
	ProvisionCommand      string                `json:"provision_command,omitempty"`
	HostnameRegex         string                `json:"hostname_regex,omitempty"`
	VSCodeCommandTemplate string                `json:"vscode_command_template,omitempty"`
	Flavors               []RemoteProfileFlavor `json:"flavors"`
}

// RemoteProfileFlavor represents a flavor (machine type) within a remote profile.
// Flavor-level fields override the profile-level defaults when non-empty.
type RemoteProfileFlavor struct {
	Flavor           string `json:"flavor"`
	DisplayName      string `json:"display_name,omitempty"`
	WorkspacePath    string `json:"workspace_path,omitempty"`
	ProvisionCommand string `json:"provision_command,omitempty"`
}

// ResolvedFlavor holds the merged result of a profile and one of its flavors.
// All fields are resolved (flavor overrides applied on top of profile defaults).
type ResolvedFlavor struct {
	ProfileID             string
	ProfileDisplayName    string
	Flavor                string
	FlavorDisplayName     string
	VCS                   string
	WorkspacePath         string
	ConnectCommand        string
	ReconnectCommand      string
	ProvisionCommand      string
	HostnameRegex         string
	VSCodeCommandTemplate string
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

// IOWorkspaceTelemetryConfig holds configuration for I/O workspace telemetry collection.
type IOWorkspaceTelemetryConfig struct {
	Enabled *bool  `json:"enabled,omitempty"` // enable/disable I/O workspace telemetry
	Target  string `json:"target,omitempty"`  // run target for telemetry processing
}

// NotificationsConfig holds configuration for dashboard notifications.
type NotificationsConfig struct {
	SoundDisabled           bool  `json:"sound_disabled,omitempty"`             // disable attention sounds (default: false = sounds enabled)
	ConfirmBeforeClose      bool  `json:"confirm_before_close,omitempty"`       // show browser "Leave site?" dialog on tab close (default: false = no confirmation)
	SuggestDisposeAfterPush *bool `json:"suggest_dispose_after_push,omitempty"` // prompt to dispose workspace after pushing to main (default: true)
}

// RemoteWorkspaceConfig holds configuration for remote workspace operations.
type RemoteWorkspaceConfig struct {
	// VSCodeCommandTemplate is a Go template for launching VS Code on remote workspaces.
	// Available template variables:
	//   {{.Hostname}} - Remote host hostname
	//   {{.Path}} - Remote workspace path
	//   {{.VSCodePath}} - Path to the local VSCode executable
	//
	// Examples:
	//   Standard Remote-SSH: "{{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}"
	//   Custom remote: "{{.VSCodePath}} --folder-uri vscode-remote://custom+{{.Hostname}}{{.Path}}"
	//
	// If empty, defaults to standard VS Code Remote-SSH format.
	VSCodeCommandTemplate string `json:"vscode_command_template,omitempty"`
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
	Target         string `json:"target,omitempty"`
	ViewedBufferMs int    `json:"viewed_buffer_ms,omitempty"`
	SeenIntervalMs int    `json:"seen_interval_ms,omitempty"`
}

// SubredditConfig represents configuration for the subreddit digest feature.
type SubredditConfig struct {
	Target        string          `json:"target,omitempty"`         // LLM target for generation, empty = disabled
	Interval      int             `json:"interval,omitempty"`       // Polling interval in minutes, default 30
	CheckingRange int             `json:"checking_range,omitempty"` // Lookback for new commits in hours, default 48
	MaxPosts      int             `json:"max_posts,omitempty"`      // Max posts per repo, default 30
	MaxAge        int             `json:"max_age,omitempty"`        // Max post age in days, default 14
	Repos         map[string]bool `json:"repos,omitempty"`          // Per-repo enabled/disabled (default true)
}

// RepofeedConfig controls the cross-developer intent federation system.
type RepofeedConfig struct {
	Enabled                 bool            `json:"enabled,omitempty"`
	PublishIntervalSeconds  int             `json:"publish_interval_seconds,omitempty"`
	FetchIntervalSeconds    int             `json:"fetch_interval_seconds,omitempty"`
	CompletedRetentionHours int             `json:"completed_retention_hours,omitempty"`
	Repos                   map[string]bool `json:"repos,omitempty"`
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

// BranchSuggestConfig represents configuration for branch name suggestion.
type BranchSuggestConfig struct {
	Target string `json:"target,omitempty"`
}

// ConflictResolveConfig represents configuration for conflict resolution.
type ConflictResolveConfig struct {
	Target    string `json:"target,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
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
}

type SaplingCommands struct {
	CreateWorkspace string `json:"create_workspace,omitempty"`
	RemoveWorkspace string `json:"remove_workspace,omitempty"`
	CheckRepoBase   string `json:"check_repo_base,omitempty"`
	CreateRepoBase  string `json:"create_repo_base,omitempty"`
	ListWorkspaces  string `json:"list_workspaces,omitempty"`
}

func (sc SaplingCommands) GetCreateWorkspace() string {
	if sc.CreateWorkspace != "" {
		return sc.CreateWorkspace
	}
	return "sl clone {{.RepoIdentifier}} {{.DestPath}}"
}

func (sc SaplingCommands) GetRemoveWorkspace() string {
	if sc.RemoveWorkspace != "" {
		return sc.RemoveWorkspace
	}
	return "rm -rf {{.WorkspacePath}}"
}

func (sc SaplingCommands) GetCreateRepoBase() string {
	if sc.CreateRepoBase != "" {
		return sc.CreateRepoBase
	}
	return "sl clone {{.RepoIdentifier}} {{.BasePath}}"
}

// RunTarget represents a user-supplied run target.
type RunTarget struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// QuickLaunch represents a saved run preset.
// Either Command (shell command) or Target+Prompt (AI agent) should be set, not both.
type QuickLaunch struct {
	Name    string  `json:"name"`
	Command string  `json:"command,omitempty"` // shell command to run directly
	Target  string  `json:"target,omitempty"`  // run target (claude, codex, model, etc.)
	Prompt  *string `json:"prompt,omitempty"`  // prompt for the target
}

// ExternalDiffCommand represents an external diff tool configuration.
type ExternalDiffCommand struct {
	Name    string `json:"name"`
	Command string `json:"command"`
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
				if detect.IsBuiltinToolName(ql.Target) {
					return true
				}
			}
			if cfg.Nudgenik != nil && detect.IsBuiltinToolName(cfg.Nudgenik.Target) {
				return true
			}
			if cfg.Compound != nil && detect.IsBuiltinToolName(cfg.Compound.Target) {
				return true
			}
			return false
		},
		Apply: func(_ map[string]json.RawMessage, cfg *Config) error {
			resolve := func(toolName string) string {
				return toolName // historical migration, already ran for existing users
			}

			for i := range cfg.QuickLaunch {
				if detect.IsBuiltinToolName(cfg.QuickLaunch[i].Target) {
					cfg.QuickLaunch[i].Target = resolve(cfg.QuickLaunch[i].Target)
				}
			}
			if cfg.Nudgenik != nil && detect.IsBuiltinToolName(cfg.Nudgenik.Target) {
				cfg.Nudgenik.Target = resolve(cfg.Nudgenik.Target)
			}
			if cfg.Compound != nil && detect.IsBuiltinToolName(cfg.Compound.Target) {
				cfg.Compound.Target = resolve(cfg.Compound.Target)
			}
			return nil
		},
	},
	{
		Name: "migrate_remote_flavors_to_profiles",
		Detect: func(_ map[string]json.RawMessage, cfg *Config) bool {
			return len(cfg.RemoteFlavors) > 0 && len(cfg.RemoteProfiles) == 0
		},
		Apply: func(_ map[string]json.RawMessage, cfg *Config) error {
			cfg.MigrateRemoteFlavorsToProfiles()
			return nil
		},
	},
}

// hasLegacyModelIDs returns true if any config field contains a legacy model ID.
func (c *Config) hasLegacyModelIDs() bool {
	legacy := detect.LegacyIDMigrations()
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
	if c.Nudgenik != nil && isLegacy(c.Nudgenik.Target) {
		return true
	}
	if c.BranchSuggest != nil && isLegacy(c.BranchSuggest.Target) {
		return true
	}
	if c.ConflictResolve != nil && isLegacy(c.ConflictResolve.Target) {
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
			c.QuickLaunch[i].Target = detect.MigrateModelID(ql.Target)
		}
	}

	// Nested config targets
	migrateTarget := func(field *string) {
		if field != nil && *field != "" {
			*field = detect.MigrateModelID(*field)
		}
	}

	if c.Nudgenik != nil {
		migrateTarget(&c.Nudgenik.Target)
	}
	if c.BranchSuggest != nil {
		migrateTarget(&c.BranchSuggest.Target)
	}
	if c.ConflictResolve != nil {
		migrateTarget(&c.ConflictResolve.Target)
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

	// Enabled models map
	if c.Models != nil && c.Models.Enabled != nil {
		newEnabled := make(map[string]string, len(c.Models.Enabled))
		for id, tool := range c.Models.Enabled {
			newEnabled[detect.MigrateModelID(id)] = tool
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

func (c *Config) validate(strict bool) ([]string, error) {
	if err := validateRunTargets(c.RunTargets); err != nil {
		return nil, err
	}
	if err := validateQuickLaunch(c.QuickLaunch); err != nil {
		return nil, err
	}
	if err := validateNudgenikConfig(c.Nudgenik); err != nil {
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

// IsBuiltinEnabled returns whether a built-in skill is enabled.
// Skills are enabled by default unless explicitly disabled in the config.
func (c *Config) IsBuiltinEnabled(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.BuiltInSkills == nil {
		return true
	}
	enabled, exists := c.BuiltInSkills[name]
	if !exists {
		return true
	}
	return enabled
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

// getNudgenikTargetLocked is the lock-free implementation. Caller must hold mu.
func (c *Config) getNudgenikTargetLocked() string {
	if c == nil || c.Nudgenik == nil {
		return ""
	}
	return strings.TrimSpace(c.Nudgenik.Target)
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

// GetDebugUI returns whether the debug UI is enabled via config.
func (c *Config) GetDebugUI() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.DebugUI
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
// Returns true by default if the repo is not in the map.
func (c *Config) GetRepofeedRepoEnabled(slug string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Repofeed == nil || c.Repofeed.Repos == nil {
		return true
	}
	enabled, ok := c.Repofeed.Repos[slug]
	if !ok {
		return true
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

// GetBranchSuggestTarget returns the configured branch suggestion target name, if any.
func (c *Config) GetBranchSuggestTarget() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.BranchSuggest == nil {
		return ""
	}
	return strings.TrimSpace(c.BranchSuggest.Target)
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.Compound == nil || c.Compound.DebounceMs <= 0 {
		return 2000
	}
	return c.Compound.DebounceMs
}

// GetCompoundEnabled returns whether compounding is enabled.
func (c *Config) GetCompoundEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.Compound == nil || c.Compound.Enabled == nil {
		return true // enabled by default
	}
	return *c.Compound.Enabled
}

// GetCompoundSuppressionTTLMs returns the suppression TTL in milliseconds.
func (c *Config) GetCompoundSuppressionTTLMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.Compound == nil || c.Compound.SuppressionTTLMs <= 0 {
		return 5000
	}
	return c.Compound.SuppressionTTLMs
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.Lore == nil || c.Lore.Enabled == nil {
		return true
	}
	return *c.Lore.Enabled
}

// GetLoreTarget returns the configured lore curator LLM target.
// Falls back to the compound target if not explicitly configured.
func (c *Config) GetLoreTarget() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c != nil && c.Lore != nil && c.Lore.Target != "" {
		return c.Lore.Target
	}
	return c.getCompoundTargetLocked()
}

// GetLoreTargetRaw returns the explicitly configured lore curator LLM target
// without any fallback. Returns "" if no target is set.
// Use this for config UI display; use GetLoreTarget() for runtime behavior.
func (c *Config) GetLoreTargetRaw() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.Lore == nil {
		return ""
	}
	return c.Lore.Target
}

// GetLoreAutoPR returns whether to auto-create a PR after pushing a lore branch.
// Defaults to false.
func (c *Config) GetLoreAutoPR() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.Lore == nil || c.Lore.AutoPR == nil {
		return false
	}
	return *c.Lore.AutoPR
}

// GetLoreCurateOnDispose returns the curate-on-dispose mode.
// Returns "session", "workspace", or "never". Defaults to "session".
func (c *Config) GetLoreCurateOnDispose() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.Lore == nil {
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.Lore == nil {
		return "direct_push"
	}
	return c.Lore.GetPublicRuleMode()
}

// GetLoreCurateDebounceMs returns the debounce interval for auto-curation in milliseconds.
// Defaults to 30000 (30 seconds).
func (c *Config) GetLoreCurateDebounceMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.Lore == nil || c.Lore.CurateDebounceMs <= 0 {
		return 30000
	}
	return c.Lore.CurateDebounceMs
}

// GetLorePruneAfterDays returns the number of days before pruning applied/dismissed entries.
// Defaults to 30.
func (c *Config) GetLorePruneAfterDays() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.Lore == nil || c.Lore.PruneAfterDays <= 0 {
		return 30
	}
	return c.Lore.PruneAfterDays
}

// GetLoreInstructionFiles returns the instruction file patterns managed by the lore curator.
// Defaults to DefaultInstructionFiles if not configured.
func (c *Config) GetLoreInstructionFiles() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c != nil && c.Lore != nil && len(c.Lore.InstructionFiles) > 0 {
		return c.Lore.InstructionFiles
	}
	return DefaultInstructionFiles
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
	if c != nil && c.Overlay != nil {
		for _, p := range c.Overlay.Paths {
			add(p)
		}
	}
	if c != nil {
		for _, repo := range c.Repos {
			if repo.Name == repoName {
				for _, p := range repo.OverlayPaths {
					add(p)
				}
				break
			}
		}
	}
	return paths
}

// GetConflictResolveTarget returns the configured conflict resolution target name, if any.
func (c *Config) GetConflictResolveTarget() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.ConflictResolve == nil {
		return ""
	}
	return strings.TrimSpace(c.ConflictResolve.Target)
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.PrReview == nil {
		return ""
	}
	return strings.TrimSpace(c.PrReview.Target)
}

// GetCommitMessageTarget returns the configured target for commit message generation.
func (c *Config) GetCommitMessageTarget() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.CommitMessage == nil {
		return ""
	}
	return strings.TrimSpace(c.CommitMessage.Target)
}

// GetDesyncEnabled returns whether desync diagnostics are enabled.
func (c *Config) GetDesyncEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.Desync == nil || c.Desync.Enabled == nil {
		return false
	}
	return *c.Desync.Enabled
}

// GetDesyncTarget returns the configured target for desync diagnostic capture sessions.
func (c *Config) GetDesyncTarget() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.Desync == nil {
		return ""
	}
	return strings.TrimSpace(c.Desync.Target)
}

// GetIOWorkspaceTelemetryEnabled returns whether I/O workspace telemetry is enabled.
func (c *Config) GetIOWorkspaceTelemetryEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.IOWorkspaceTelemetry == nil || c.IOWorkspaceTelemetry.Enabled == nil {
		return false
	}
	return *c.IOWorkspaceTelemetry.Enabled
}

// GetIOWorkspaceTelemetryTarget returns the configured target for I/O workspace telemetry.
func (c *Config) GetIOWorkspaceTelemetryTarget() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.IOWorkspaceTelemetry == nil {
		return ""
	}
	return strings.TrimSpace(c.IOWorkspaceTelemetry.Target)
}

// GetNotificationSoundEnabled returns whether notification sounds are enabled.
// Defaults to true (sounds enabled) unless explicitly disabled.
func (c *Config) GetNotificationSoundEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.Notifications == nil {
		return true
	}
	return !c.Notifications.SoundDisabled
}

// GetConfirmBeforeClose returns whether the browser should show a "Leave site?" dialog on tab close.
// Defaults to false (no confirmation).
func (c *Config) GetConfirmBeforeClose() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.Notifications == nil {
		return false
	}
	return c.Notifications.ConfirmBeforeClose
}

// GetSuggestDisposeAfterPush returns whether to prompt disposing workspace after pushing to main.
// Defaults to true (prompt enabled) unless explicitly disabled.
func (c *Config) GetSuggestDisposeAfterPush() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.Notifications == nil || c.Notifications.SuggestDisposeAfterPush == nil {
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

	// Replace all fields under write lock, preserving mutexes and cache.
	c.mu.Lock()
	c.ConfigVersion = newCfg.ConfigVersion
	c.WorkspacePath = newCfg.WorkspacePath
	c.WorktreeBasePath = newCfg.WorktreeBasePath
	c.SourceCodeManagement = newCfg.SourceCodeManagement
	c.RecycleWorkspaces = newCfg.RecycleWorkspaces
	c.LocalEchoRemote = newCfg.LocalEchoRemote
	c.DebugUI = newCfg.DebugUI
	c.Repos = newCfg.Repos
	c.RunTargets = newCfg.RunTargets
	c.QuickLaunch = newCfg.QuickLaunch
	c.ExternalDiffCommands = newCfg.ExternalDiffCommands
	c.ExternalDiffCleanupAfterMs = newCfg.ExternalDiffCleanupAfterMs
	c.Pastebin = newCfg.Pastebin
	c.Nudgenik = newCfg.Nudgenik
	c.BranchSuggest = newCfg.BranchSuggest
	c.ConflictResolve = newCfg.ConflictResolve
	c.Compound = newCfg.Compound
	c.Overlay = newCfg.Overlay
	c.Lore = newCfg.Lore
	c.Sessions = newCfg.Sessions
	c.Xterm = newCfg.Xterm
	c.Network = newCfg.Network
	c.AccessControl = newCfg.AccessControl
	c.PrReview = newCfg.PrReview
	c.CommitMessage = newCfg.CommitMessage
	c.Desync = newCfg.Desync
	c.Notifications = newCfg.Notifications
	c.RemoteFlavors = newCfg.RemoteFlavors
	c.RemoteProfiles = newCfg.RemoteProfiles
	c.RemoteWorkspace = newCfg.RemoteWorkspace
	c.RemoteAccess = newCfg.RemoteAccess
	c.Models = newCfg.Models
	c.TelemetryEnabled = newCfg.TelemetryEnabled
	c.InstallationID = newCfg.InstallationID
	c.path = configPath
	c.mu.Unlock()

	// Invalidate repo URL cache
	c.repoURLMu.Lock()
	c.repoURLCache = nil
	c.repoURLMu.Unlock()

	return nil
}

// CreateDefault creates a default config with the given config file path.
// The path is stored so that subsequent Save() calls write to the same location.
// If build defaults are embedded (via build_defaults.json), they are overlaid
// onto the Go defaults so that deployment-specific values take effect on first run.
func CreateDefault(configPath string) *Config {
	cfg := &Config{
		ConfigVersion:              version.Version,
		WorkspacePath:              "",
		Repos:                      []Repo{},
		RunTargets:                 []RunTarget{},
		QuickLaunch:                []QuickLaunch{},
		ExternalDiffCommands:       []ExternalDiffCommand{},
		ExternalDiffCleanupAfterMs: DefaultExternalDiffCleanupAfterMs,
		Pastebin:                   []string{},
		path:                       configPath,
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
	c.repoURLMu.Lock()
	c.repoURLCache = nil
	c.repoURLMu.Unlock()

	return nil
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
	fmt.Println("Run 'schmux status' to get the dashboard URL and complete setup.")

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

// GetDisposeGracePeriodMs returns the dispose grace period in ms. Defaults to 30000 (30s).
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
// This is a convenience method that checks if bind_address is "0.0.0.0".
func (c *Config) GetNetworkAccess() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.getBindAddressLocked() == "0.0.0.0"
}

// GetPort returns the dashboard port. Defaults to 7337.
func (c *Config) GetPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Network == nil || c.Network.Port <= 0 {
		return 7337
	}
	return c.Network.Port
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
	if c.AccessControl == nil {
		return ""
	}
	if strings.TrimSpace(c.AccessControl.Provider) == "" {
		return "github"
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

// EnsureModelSecrets validates that all required secrets for a model are non-empty.
// Checks the first runner's RequiredSecrets since model-level RequiredSecrets was removed.
// This is a shared helper used by multiple packages (session, oneshot, nudgenik).
func EnsureModelSecrets(model detect.Model, secrets map[string]string) error {
	for _, key := range model.FirstRunnerRequiredSecrets() {
		val := strings.TrimSpace(secrets[key])
		if val == "" {
			return fmt.Errorf("%w: model %s missing required secret: %s", ErrInvalidConfig, model.ID, key)
		}
	}
	return nil
}

// GetRemoteAccessEnabled returns whether remote access is enabled.
// Defaults to false (disabled). Users must explicitly set "enabled": true.
// For backward compatibility, "disabled": true in existing configs is respected
// (inverted to enabled=false). If both fields are set, "enabled" takes precedence.
func (c *Config) GetRemoteAccessEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.RemoteAccess == nil {
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.RemoteAccess == nil || c.RemoteAccess.TimeoutMinutes == 0 {
		return 120
	}
	if c.RemoteAccess.TimeoutMinutes < 0 {
		return 0
	}
	return c.RemoteAccess.TimeoutMinutes
}

// GetRemoteAccessNtfyTopic returns the ntfy.sh topic for push notifications.
func (c *Config) GetRemoteAccessNtfyTopic() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.RemoteAccess == nil || c.RemoteAccess.Notify == nil {
		return ""
	}
	return strings.TrimSpace(c.RemoteAccess.Notify.NtfyTopic)
}

// GetRemoteAccessNotifyCommand returns the custom notification command.
func (c *Config) GetRemoteAccessNotifyCommand() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.RemoteAccess == nil || c.RemoteAccess.Notify == nil {
		return ""
	}
	return strings.TrimSpace(c.RemoteAccess.Notify.Command)
}

// GetRemoteAccessPasswordHash returns the bcrypt-hashed password for remote access auth.
func (c *Config) GetRemoteAccessPasswordHash() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.RemoteAccess == nil {
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.RemoteAccess == nil || c.RemoteAccess.AllowAutoDownload == nil {
		return false
	}
	return *c.RemoteAccess.AllowAutoDownload
}

// GetRemoteFlavors returns the list of remote flavors.
func (c *Config) GetRemoteFlavors() []RemoteFlavor {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.RemoteFlavors == nil {
		return []RemoteFlavor{}
	}
	return c.RemoteFlavors
}

// GetRemoteFlavor returns a remote flavor by ID.
func (c *Config) GetRemoteFlavor(id string) (RemoteFlavor, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, rf := range c.RemoteFlavors {
		if rf.ID == id {
			return rf, true
		}
	}
	return RemoteFlavor{}, false
}

// GetRemoteVSCodeCommandTemplate returns the VSCode command template for remote workspaces.
// Returns a default template if not configured.
func (c *Config) GetRemoteVSCodeCommandTemplate() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.RemoteWorkspace != nil && c.RemoteWorkspace.VSCodeCommandTemplate != "" {
		return c.RemoteWorkspace.VSCodeCommandTemplate
	}
	// Default to standard VS Code Remote-SSH format
	return `{{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}`
}

// defaultSSHCommand returns the default SSH command with keepalive options.
// ServerAliveInterval sends a probe every 15s; ServerAliveCountMax=3 means
// the connection is closed after 45s of unresponsive server. This prevents
// silent connection drops through NAT, proxies, VPNs, and bastion hosts.
func defaultSSHCommand(hostTemplate string) string {
	return fmt.Sprintf(`ssh -tt -o ServerAliveInterval=15 -o ServerAliveCountMax=3 %s --`, hostTemplate)
}

// remoteTmuxControlSuffix returns the tmux control mode suffix for remote commands.
// Uses -L for socket isolation on the remote host and -s for session naming.
func remoteTmuxControlSuffix(socketName string) string {
	return fmt.Sprintf(` tmux -L %s -CC new-session -A -s %s`, socketName, socketName)
}

// remoteTmuxAttachSuffix returns the tmux attach suffix for human-friendly remote attach commands.
func remoteTmuxAttachSuffix(socketName string) string {
	return fmt.Sprintf(` tmux -L %s attach-session -t %s`, socketName, socketName)
}

// GetConnectCommandTemplate returns the full connection command template for this flavor.
// This includes both the user's connection command and the tmux control mode suffix.
// Users configure the connection part (including any separators like "--" for SSH);
// schmux appends tmux with socket isolation (-L) automatically.
// socketName controls the tmux socket on the remote host (typically from config.GetTmuxSocketName()).
func (rf *RemoteFlavor) GetConnectCommandTemplate(socketName string) string {
	var baseCmd string
	if rf.ConnectCommand != "" {
		baseCmd = rf.ConnectCommand
	} else {
		baseCmd = defaultSSHCommand("{{.Flavor}}")
	}
	return baseCmd + remoteTmuxControlSuffix(socketName)
}

// GetReconnectCommandTemplate returns the full reconnection command template for this flavor.
func (rf *RemoteFlavor) GetReconnectCommandTemplate(socketName string) string {
	var baseCmd string
	if rf.ReconnectCommand != "" {
		baseCmd = rf.ReconnectCommand
	} else if rf.ConnectCommand != "" {
		baseCmd = rf.ConnectCommand
	} else {
		baseCmd = defaultSSHCommand("{{.Hostname}}")
	}
	return baseCmd + remoteTmuxControlSuffix(socketName)
}

// GetAttachCommandTemplate returns a human-friendly attach command for this flavor.
// Unlike GetReconnectCommandTemplate, this uses `attach-session` without -CC
// (control mode), so users get a normal interactive tmux session.
func (rf *RemoteFlavor) GetAttachCommandTemplate(socketName string) string {
	var baseCmd string
	if rf.ReconnectCommand != "" {
		baseCmd = rf.ReconnectCommand
	} else if rf.ConnectCommand != "" {
		baseCmd = rf.ConnectCommand
	} else {
		baseCmd = defaultSSHCommand("{{.Hostname}}")
	}
	return baseCmd + remoteTmuxAttachSuffix(socketName)
}

// AddRemoteFlavor adds a new remote flavor to the config.
// If no ID is provided, one is generated from the flavor string.
func (c *Config) AddRemoteFlavor(rf RemoteFlavor) error {
	if err := validateRemoteFlavor(rf); err != nil {
		return err
	}
	if rf.VCS == "" {
		rf.VCS = "git"
	}

	// Generate ID if not provided
	if rf.ID == "" {
		rf.ID = generateRemoteFlavorID(rf.Flavor)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Check for duplicate ID
	for _, existing := range c.RemoteFlavors {
		if existing.ID == rf.ID {
			return fmt.Errorf("%w: remote flavor with ID %q already exists", ErrInvalidConfig, rf.ID)
		}
	}

	c.RemoteFlavors = append(c.RemoteFlavors, rf)
	return nil
}

// validateRemoteFlavor validates a remote flavor configuration.
func validateRemoteFlavor(rf RemoteFlavor) error {
	if rf.Flavor == "" {
		return fmt.Errorf("%w: flavor string is required", ErrInvalidConfig)
	}
	if rf.DisplayName == "" {
		return fmt.Errorf("%w: display_name is required", ErrInvalidConfig)
	}
	if rf.WorkspacePath == "" {
		return fmt.Errorf("%w: workspace_path is required", ErrInvalidConfig)
	}
	if rf.VCS != "" && rf.VCS != "git" && rf.VCS != "sapling" {
		return fmt.Errorf("%w: vcs must be 'git' or 'sapling'", ErrInvalidConfig)
	}

	// Length validation
	if len(rf.DisplayName) > 100 {
		return fmt.Errorf("%w: display_name too long (max 100 characters)", ErrInvalidConfig)
	}
	if len(rf.Flavor) > 200 {
		return fmt.Errorf("%w: flavor too long (max 200 characters)", ErrInvalidConfig)
	}
	if len(rf.WorkspacePath) > 500 {
		return fmt.Errorf("%w: workspace_path too long (max 500 characters)", ErrInvalidConfig)
	}

	// Shell injection validation for flavor - strengthen against metacharacters
	dangerousChars := ";|&$`\\\n\r\t<>(){}[]"
	if strings.ContainsAny(rf.Flavor, dangerousChars) {
		return fmt.Errorf("%w: flavor contains shell metacharacters", ErrInvalidConfig)
	}

	// Workspace path validation - check for dangerous characters
	if strings.ContainsAny(rf.WorkspacePath, "$`\\;|&\n\r") {
		return fmt.Errorf("%w: workspace_path contains dangerous characters", ErrInvalidConfig)
	}

	// Workspace path validation
	if !strings.HasPrefix(rf.WorkspacePath, "~") && !strings.HasPrefix(rf.WorkspacePath, "/") {
		return fmt.Errorf("%w: workspace_path must be absolute or start with ~", ErrInvalidConfig)
	}

	// Template validation for command templates
	if rf.ConnectCommand != "" {
		testData := map[string]string{"Flavor": "test-flavor"}
		if err := validateCommandTemplate(rf.ConnectCommand, "connect_command", testData); err != nil {
			return err
		}
	}

	if rf.ReconnectCommand != "" {
		testData := map[string]string{"Hostname": "test.example.com", "Flavor": "test-flavor"}
		if err := validateCommandTemplate(rf.ReconnectCommand, "reconnect_command", testData); err != nil {
			return err
		}
	}

	if rf.ProvisionCommand != "" {
		testData := map[string]string{"WorkspacePath": "/workspace", "Repo": "https://github.com/test/repo"}
		if err := validateCommandTemplate(rf.ProvisionCommand, "provision_command", testData); err != nil {
			return err
		}
	}

	if rf.HostnameRegex != "" {
		re, err := regexp.Compile(rf.HostnameRegex)
		if err != nil {
			return fmt.Errorf("%w: hostname_regex is not valid regex: %v", ErrInvalidConfig, err)
		}
		if re.NumSubexp() < 1 {
			return fmt.Errorf("%w: hostname_regex must contain at least one capture group", ErrInvalidConfig)
		}
	}

	return nil
}

// validateCommandTemplate validates that a template string is valid Go template syntax
// and can be executed with the provided test data.
func validateCommandTemplate(tmplStr, fieldName string, testData map[string]string) error {
	// Parse the template with strict error mode for undefined variables
	tmpl, err := template.New(fieldName).Option("missingkey=error").Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("%w: %s has invalid template syntax: %v", ErrInvalidConfig, fieldName, err)
	}

	// Try to execute with test data to catch undefined variable errors
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, testData); err != nil {
		return fmt.Errorf("%w: %s template execution failed: %v", ErrInvalidConfig, fieldName, err)
	}

	return nil
}

// UpdateRemoteFlavor updates an existing remote flavor.
func (c *Config) UpdateRemoteFlavor(rf RemoteFlavor) error {
	if rf.ID == "" {
		return fmt.Errorf("%w: flavor ID is required", ErrInvalidConfig)
	}
	if err := validateRemoteFlavor(rf); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for i, existing := range c.RemoteFlavors {
		if existing.ID == rf.ID {
			c.RemoteFlavors[i] = rf
			return nil
		}
	}
	return fmt.Errorf("%w: remote flavor not found: %s", ErrInvalidConfig, rf.ID)
}

// RemoveRemoteFlavor removes a remote flavor by ID.
func (c *Config) RemoveRemoteFlavor(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, rf := range c.RemoteFlavors {
		if rf.ID == id {
			c.RemoteFlavors = append(c.RemoteFlavors[:i], c.RemoteFlavors[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: remote flavor not found: %s", ErrInvalidConfig, id)
}

// generateRemoteFlavorID generates a sanitized ID from a flavor string.
// e.g., "gpu:ml-large" -> "gpu_ml_large"
func generateRemoteFlavorID(flavor string) string {
	// Replace non-alphanumeric characters with underscore
	result := strings.Builder{}
	for _, c := range flavor {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			result.WriteRune(c)
		} else {
			result.WriteRune('_')
		}
	}
	return strings.ToLower(result.String())
}

// GenerateRemoteFlavorID is the exported version of generateRemoteFlavorID.
func GenerateRemoteFlavorID(flavor string) string {
	return generateRemoteFlavorID(flavor)
}

// ResolveProfileFlavor merges a profile's defaults with a specific flavor's overrides.
// Returns an error if the flavor string is not found in the profile's Flavors list.
func ResolveProfileFlavor(profile RemoteProfile, flavorStr string) (ResolvedFlavor, error) {
	for _, f := range profile.Flavors {
		if f.Flavor == flavorStr {
			resolved := ResolvedFlavor{
				ProfileID:             profile.ID,
				ProfileDisplayName:    profile.DisplayName,
				Flavor:                f.Flavor,
				VCS:                   profile.VCS,
				ConnectCommand:        profile.ConnectCommand,
				ReconnectCommand:      profile.ReconnectCommand,
				HostnameRegex:         profile.HostnameRegex,
				VSCodeCommandTemplate: profile.VSCodeCommandTemplate,
			}

			// FlavorDisplayName: flavor's DisplayName if non-empty, else the flavor string itself
			if f.DisplayName != "" {
				resolved.FlavorDisplayName = f.DisplayName
			} else {
				resolved.FlavorDisplayName = f.Flavor
			}

			// WorkspacePath: flavor value if non-empty, else profile value
			if f.WorkspacePath != "" {
				resolved.WorkspacePath = f.WorkspacePath
			} else {
				resolved.WorkspacePath = profile.WorkspacePath
			}

			// ProvisionCommand: flavor value if non-empty, else profile value
			if f.ProvisionCommand != "" {
				resolved.ProvisionCommand = f.ProvisionCommand
			} else {
				resolved.ProvisionCommand = profile.ProvisionCommand
			}

			return resolved, nil
		}
	}
	return ResolvedFlavor{}, fmt.Errorf("%w: flavor %q not found in profile %q", ErrInvalidConfig, flavorStr, profile.ID)
}

// GetConnectCommandTemplate returns the full connection command template for this profile.
func (p *RemoteProfile) GetConnectCommandTemplate(socketName string) string {
	var baseCmd string
	if p.ConnectCommand != "" {
		baseCmd = p.ConnectCommand
	} else {
		baseCmd = defaultSSHCommand("{{.Flavor}}")
	}
	return baseCmd + remoteTmuxControlSuffix(socketName)
}

// GetReconnectCommandTemplate returns the full reconnection command template for this profile.
func (p *RemoteProfile) GetReconnectCommandTemplate(socketName string) string {
	var baseCmd string
	if p.ReconnectCommand != "" {
		baseCmd = p.ReconnectCommand
	} else if p.ConnectCommand != "" {
		baseCmd = p.ConnectCommand
	} else {
		baseCmd = defaultSSHCommand("{{.Hostname}}")
	}
	return baseCmd + remoteTmuxControlSuffix(socketName)
}

// GetConnectCommandTemplate returns the full connection command template for this resolved flavor.
func (rf *ResolvedFlavor) GetConnectCommandTemplate(socketName string) string {
	var baseCmd string
	if rf.ConnectCommand != "" {
		baseCmd = rf.ConnectCommand
	} else {
		baseCmd = defaultSSHCommand("{{.Flavor}}")
	}
	return baseCmd + remoteTmuxControlSuffix(socketName)
}

// GetReconnectCommandTemplate returns the full reconnection command template for this resolved flavor.
func (rf *ResolvedFlavor) GetReconnectCommandTemplate(socketName string) string {
	var baseCmd string
	if rf.ReconnectCommand != "" {
		baseCmd = rf.ReconnectCommand
	} else if rf.ConnectCommand != "" {
		baseCmd = rf.ConnectCommand
	} else {
		baseCmd = defaultSSHCommand("{{.Hostname}}")
	}
	return baseCmd + remoteTmuxControlSuffix(socketName)
}

// GetRemoteProfiles returns the list of remote profiles.
func (c *Config) GetRemoteProfiles() []RemoteProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.RemoteProfiles == nil {
		return []RemoteProfile{}
	}
	return c.RemoteProfiles
}

// GetRemoteProfile returns a remote profile by ID.
func (c *Config) GetRemoteProfile(id string) (RemoteProfile, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, p := range c.RemoteProfiles {
		if p.ID == id {
			return p, true
		}
	}
	return RemoteProfile{}, false
}

// AddRemoteProfile adds a new remote profile to the config.
// If no ID is provided, one is generated from the first flavor string.
func (c *Config) AddRemoteProfile(p RemoteProfile) error {
	if err := validateRemoteProfile(p); err != nil {
		return err
	}
	if p.VCS == "" {
		p.VCS = "git"
	}

	// Generate ID if not provided
	if p.ID == "" {
		if len(p.Flavors) > 0 {
			p.ID = generateRemoteFlavorID(p.Flavors[0].Flavor)
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Check for duplicate ID
	for _, existing := range c.RemoteProfiles {
		if existing.ID == p.ID {
			return fmt.Errorf("%w: remote profile with ID %q already exists", ErrInvalidConfig, p.ID)
		}
	}

	c.RemoteProfiles = append(c.RemoteProfiles, p)
	return nil
}

// UpdateRemoteProfile updates an existing remote profile.
func (c *Config) UpdateRemoteProfile(p RemoteProfile) error {
	if p.ID == "" {
		return fmt.Errorf("%w: profile ID is required", ErrInvalidConfig)
	}
	if err := validateRemoteProfile(p); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for i, existing := range c.RemoteProfiles {
		if existing.ID == p.ID {
			c.RemoteProfiles[i] = p
			return nil
		}
	}
	return fmt.Errorf("%w: remote profile not found: %s", ErrInvalidConfig, p.ID)
}

// RemoveRemoteProfile removes a remote profile by ID.
func (c *Config) RemoveRemoteProfile(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, p := range c.RemoteProfiles {
		if p.ID == id {
			c.RemoteProfiles = append(c.RemoteProfiles[:i], c.RemoteProfiles[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: remote profile not found: %s", ErrInvalidConfig, id)
}

// validateRemoteProfile validates a remote profile configuration.
func validateRemoteProfile(p RemoteProfile) error {
	if len(p.Flavors) == 0 {
		return fmt.Errorf("%w: profile must have at least one flavor", ErrInvalidConfig)
	}
	if p.DisplayName == "" {
		return fmt.Errorf("%w: display_name is required", ErrInvalidConfig)
	}
	if p.WorkspacePath == "" {
		return fmt.Errorf("%w: workspace_path is required", ErrInvalidConfig)
	}
	if p.VCS != "" && p.VCS != "git" && p.VCS != "sapling" {
		return fmt.Errorf("%w: vcs must be 'git' or 'sapling'", ErrInvalidConfig)
	}

	// Check for empty and duplicate flavor strings
	seen := make(map[string]bool)
	for _, f := range p.Flavors {
		if f.Flavor == "" {
			return fmt.Errorf("%w: flavor string is required", ErrInvalidConfig)
		}
		if seen[f.Flavor] {
			return fmt.Errorf("%w: duplicate flavor %q in profile", ErrInvalidConfig, f.Flavor)
		}
		seen[f.Flavor] = true
	}

	return nil
}

// MigrateRemoteFlavorsToProfiles converts old RemoteFlavor entries into RemoteProfile entries.
// Each old flavor becomes a profile with one child flavor. The existing auto-generated ID is preserved.
// This is idempotent: it skips if RemoteProfiles already has entries.
func (c *Config) MigrateRemoteFlavorsToProfiles() {
	if len(c.RemoteProfiles) > 0 {
		return
	}

	for _, old := range c.RemoteFlavors {
		profile := RemoteProfile{
			ID:                    old.ID,
			DisplayName:           old.DisplayName,
			VCS:                   old.VCS,
			WorkspacePath:         old.WorkspacePath,
			ConnectCommand:        old.ConnectCommand,
			ReconnectCommand:      old.ReconnectCommand,
			ProvisionCommand:      old.ProvisionCommand,
			HostnameRegex:         old.HostnameRegex,
			VSCodeCommandTemplate: old.VSCodeCommandTemplate,
			Flavors: []RemoteProfileFlavor{
				{
					Flavor:      old.Flavor,
					DisplayName: old.DisplayName,
				},
			},
		}
		c.RemoteProfiles = append(c.RemoteProfiles, profile)
	}
}

// GetTelemetryEnabled returns whether telemetry is enabled.
// Defaults to true if not explicitly configured.
func (c *Config) GetTelemetryEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil || c.TelemetryEnabled == nil {
		return true
	}
	return *c.TelemetryEnabled
}

// GetInstallationID returns the installation ID for telemetry.
// Returns empty string if not set.
func (c *Config) GetInstallationID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c == nil {
		return ""
	}
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
func bareRepoMatchesURL(repoPath, expectedURL string) bool {
	if _, err := os.Stat(repoPath); err != nil {
		return false // Doesn't exist
	}

	// Check git remote origin URL
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return false // Can't verify
	}

	return strings.TrimSpace(string(output)) == expectedURL
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
	if c.Models == nil || c.Models.Enabled == nil {
		return nil
	}
	return c.Models.Enabled
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
