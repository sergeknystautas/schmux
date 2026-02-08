package config

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/version"
)

var (
	ErrConfigNotFound = errors.New("config file not found")
	ErrInvalidConfig  = errors.New("invalid config")
)

const (
	// Default terminal dimensions
	DefaultTerminalWidth     = 120
	DefaultTerminalHeight    = 40
	DefaultTerminalSeedLines = 100
	DefaultBootstrapLines    = 20000

	// Default log rotation
	DefaultMaxLogSizeMB     = 50 // 50MB
	DefaultRotatedLogSizeMB = 1  // 1MB

	// Default timeout values in milliseconds
	DefaultGitCloneTimeoutMs          = 300000  // 5 minutes
	DefaultGitStatusPollIntervalMs    = 10000   // 10 seconds
	DefaultGitStatusWatchDebounceMs   = 1000    // 1 second
	DefaultGitStatusTimeoutMs         = 30000   // 30 seconds
	DefaultXtermQueryTimeoutMs        = 5000    // 5 seconds
	DefaultXtermOperationTimeoutMs    = 10000   // 10 seconds
	DefaultExternalDiffCleanupAfterMs = 3600000 // 1 hour
	DefaultConflictResolveTimeoutMs   = 300000  // 5 minutes

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
	ConfigVersion              string                 `json:"config_version,omitempty"`
	WorkspacePath              string                 `json:"workspace_path"`
	WorktreeBasePath           string                 `json:"base_repos_path,omitempty"`        // path for bare clones (worktree base repos)
	SourceCodeManagement       string                 `json:"source_code_management,omitempty"` // "git-worktree" (default) or "git"
	Repos                      []Repo                 `json:"repos"`
	RunTargets                 []RunTarget            `json:"run_targets"`
	QuickLaunch                []QuickLaunch          `json:"quick_launch"`
	ExternalDiffCommands       []ExternalDiffCommand  `json:"external_diff_commands,omitempty"`
	ExternalDiffCleanupAfterMs int                    `json:"external_diff_cleanup_after_ms,omitempty"`
	Terminal                   *TerminalSize          `json:"terminal,omitempty"`
	Nudgenik                   *NudgenikConfig        `json:"nudgenik,omitempty"`
	BranchSuggest              *BranchSuggestConfig   `json:"branch_suggest,omitempty"`
	ConflictResolve            *ConflictResolveConfig `json:"conflict_resolve,omitempty"`
	Sessions                   *SessionsConfig        `json:"sessions,omitempty"`
	Xterm                      *XtermConfig           `json:"xterm,omitempty"`
	Network                    *NetworkConfig         `json:"network,omitempty"`
	AccessControl              *AccessControlConfig   `json:"access_control,omitempty"`
	PrReview                   *PrReviewConfig        `json:"pr_review,omitempty"`
	Notifications              *NotificationsConfig   `json:"notifications,omitempty"`
	RemoteFlavors              []RemoteFlavor         `json:"remote_flavors,omitempty"`
	RemoteWorkspace            *RemoteWorkspaceConfig `json:"remote_workspace,omitempty"`

	// path is the file path where this config was loaded from or should be saved to.
	// Not serialized to JSON.
	path string `json:"-"`
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
	// Schmux will automatically append "-- tmux -CC new-session -A -s schmux" to this command.
	//
	// Available template variables:
	//   {{.Flavor}} - Remote flavor identifier (from the Flavor field above)
	//
	// Examples:
	//   SSH: "ssh {{.Flavor}}"
	//   Custom: "cloud-ssh connect {{.Flavor}}"
	//   AWS SSM: "aws ssm start-session --target {{.Flavor}}"
	//
	// If empty, defaults to "ssh {{.Flavor}}".
	//
	// Note: You only specify how to reach the host. Schmux handles tmux control mode internally.
	ConnectCommand string `json:"connect_command,omitempty"`

	// ReconnectCommand is a Go template for reconnecting to an existing remote host.
	// Schmux will automatically append "-- tmux -CC new-session -A -s schmux" to this command.
	//
	// Available template variables:
	//   {{.Hostname}} - Remote host hostname (discovered after initial connection)
	//   {{.Flavor}} - Remote flavor identifier
	//
	// Examples:
	//   SSH: "ssh {{.Hostname}}"
	//   Custom: "cloud-ssh reconnect {{.Hostname}}"
	//
	// If empty, uses ConnectCommand with Hostname instead of Flavor.
	//
	// Note: You only specify how to reach the host. Schmux handles tmux control mode internally.
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

// PrReviewConfig holds configuration for GitHub PR review sessions.
type PrReviewConfig struct {
	Target string `json:"target,omitempty"` // run target to use for PR review sessions
}

// NotificationsConfig holds configuration for dashboard notifications.
type NotificationsConfig struct {
	SoundDisabled bool `json:"sound_disabled,omitempty"` // disable attention sounds (default: false = sounds enabled)
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

// TerminalSize represents terminal dimensions.
type TerminalSize struct {
	Width          int `json:"width"`
	Height         int `json:"height"`
	SeedLines      int `json:"seed_lines"`
	BootstrapLines int `json:"bootstrap_lines,omitempty"`
}

// NudgenikConfig represents configuration for the NudgeNik assistant.
type NudgenikConfig struct {
	Target         string `json:"target,omitempty"`
	ViewedBufferMs int    `json:"viewed_buffer_ms,omitempty"`
	SeenIntervalMs int    `json:"seen_interval_ms,omitempty"`
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

// SessionsConfig represents session and git-related timing configuration.
type SessionsConfig struct {
	DashboardPollIntervalMs  int   `json:"dashboard_poll_interval_ms"`
	GitStatusPollIntervalMs  int   `json:"git_status_poll_interval_ms"`
	GitCloneTimeoutMs        int   `json:"git_clone_timeout_ms"`
	GitStatusTimeoutMs       int   `json:"git_status_timeout_ms"`
	GitStatusWatchEnabled    *bool `json:"git_status_watch_enabled,omitempty"`
	GitStatusWatchDebounceMs int   `json:"git_status_watch_debounce_ms,omitempty"`
}

// XtermConfig represents terminal capture, timeouts, and log rotation settings.
type XtermConfig struct {
	MtimePollIntervalMs int `json:"mtime_poll_interval_ms"`
	QueryTimeoutMs      int `json:"query_timeout_ms"`
	OperationTimeoutMs  int `json:"operation_timeout_ms"`
	MaxLogSizeMB        int `json:"max_log_size_mb,omitempty"`     // max log size before rotation
	RotatedLogSizeMB    int `json:"rotated_log_size_mb,omitempty"` // target size after rotation (keeps tail)
}

// NetworkConfig controls server binding and TLS.
type NetworkConfig struct {
	BindAddress   string     `json:"bind_address,omitempty"`
	Port          int        `json:"port,omitempty"`
	PublicBaseURL string     `json:"public_base_url,omitempty"`
	TLS           *TLSConfig `json:"tls,omitempty"`
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
	Name string `json:"name"`
	URL  string `json:"url"`
}

// RunTarget represents a user-supplied run target.
type RunTarget struct {
	Name    string `json:"name"`
	Type    string `json:"type"`    // "promptable" or "command"
	Command string `json:"command"` // shell command to run
	Source  string `json:"source,omitempty"`
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

const (
	RunTargetTypePromptable = "promptable"
	RunTargetTypeCommand    = "command"
	RunTargetSourceUser     = "user"
	RunTargetSourceDetected = "detected"
	RunTargetSourceModel    = "model"
)

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
	// Validate terminal config (required for daemon operation)
	if c.Terminal == nil {
		return nil, fmt.Errorf("%w: terminal is required (set terminal.width, terminal.height, and terminal.seed_lines)", ErrInvalidConfig)
	}
	if c.Terminal.Width <= 0 {
		return nil, fmt.Errorf("%w: terminal.width must be > 0", ErrInvalidConfig)
	}
	if c.Terminal.Height <= 0 {
		return nil, fmt.Errorf("%w: terminal.height must be > 0", ErrInvalidConfig)
	}
	if c.Terminal.SeedLines <= 0 {
		return nil, fmt.Errorf("%w: terminal.seed_lines must be > 0", ErrInvalidConfig)
	}

	if err := validateRunTargets(c.RunTargets); err != nil {
		return nil, err
	}
	if err := validateQuickLaunch(c.QuickLaunch, c.RunTargets); err != nil {
		return nil, err
	}
	if err := validateRunTargetDependencies(c.RunTargets, c.QuickLaunch, c.Nudgenik); err != nil {
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
	return c.WorkspacePath
}

// GetWorktreeBasePath returns the path for bare clones (worktree base repos).
// Defaults to ~/.schmux/repos if not set.
func (c *Config) GetWorktreeBasePath() string {
	if c.WorktreeBasePath != "" {
		return c.WorktreeBasePath
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".schmux", "repos")
}

// GetQueryRepoPath returns the path for query repos used for branch/commit querying.
// Always ~/.schmux/query/ - separate from worktree base repos.
func (c *Config) GetQueryRepoPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".schmux", "query")
}

// GetSourceCodeManagement returns the configured source code management mode.
// Defaults to "git-worktree" if not set.
func (c *Config) GetSourceCodeManagement() string {
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
	return c.Repos
}

// GetRunTargets returns the list of run targets.
func (c *Config) GetRunTargets() []RunTarget {
	return c.RunTargets
}

// GetQuickLaunch returns the list of quick launch presets.
func (c *Config) GetQuickLaunch() []QuickLaunch {
	return c.QuickLaunch
}

// GetExternalDiffCommands returns the list of external diff commands.
func (c *Config) GetExternalDiffCommands() []ExternalDiffCommand {
	return c.ExternalDiffCommands
}

// GetExternalDiffCleanupAfterMs returns the diff temp cleanup delay in ms.
func (c *Config) GetExternalDiffCleanupAfterMs() int {
	if c.ExternalDiffCleanupAfterMs > 0 {
		return c.ExternalDiffCleanupAfterMs
	}
	return DefaultExternalDiffCleanupAfterMs
}

// GetNudgenikTarget returns the configured nudgenik target name, if any.
func (c *Config) GetNudgenikTarget() string {
	if c == nil || c.Nudgenik == nil {
		return ""
	}
	return strings.TrimSpace(c.Nudgenik.Target)
}

// GetBranchSuggestTarget returns the configured branch suggestion target name, if any.
func (c *Config) GetBranchSuggestTarget() string {
	if c == nil || c.BranchSuggest == nil {
		return ""
	}
	return strings.TrimSpace(c.BranchSuggest.Target)
}

// GetConflictResolveTarget returns the configured conflict resolution target name, if any.
func (c *Config) GetConflictResolveTarget() string {
	if c == nil || c.ConflictResolve == nil {
		return ""
	}
	return strings.TrimSpace(c.ConflictResolve.Target)
}

// GetConflictResolveTimeoutMs returns the per-call conflict resolution timeout in ms.
// Defaults to 120000 (2 minutes).
func (c *Config) GetConflictResolveTimeoutMs() int {
	if c.ConflictResolve == nil || c.ConflictResolve.TimeoutMs <= 0 {
		return DefaultConflictResolveTimeoutMs
	}
	return c.ConflictResolve.TimeoutMs
}

// GetPrReviewTarget returns the configured target for PR review sessions.
func (c *Config) GetPrReviewTarget() string {
	if c == nil || c.PrReview == nil {
		return ""
	}
	return strings.TrimSpace(c.PrReview.Target)
}

// GetNotificationSoundEnabled returns whether notification sounds are enabled.
// Defaults to true (sounds enabled) unless explicitly disabled.
func (c *Config) GetNotificationSoundEnabled() bool {
	if c == nil || c.Notifications == nil {
		return true
	}
	return !c.Notifications.SoundDisabled
}

// GetDetectedRunTarget finds a detected run target by name.
func (c *Config) GetDetectedRunTarget(name string) (RunTarget, bool) {
	for _, target := range c.RunTargets {
		if target.Name == name && target.Source == RunTargetSourceDetected {
			return target, true
		}
	}
	return RunTarget{}, false
}

// GetDetectedRunTargets returns detected run targets.
func (c *Config) GetDetectedRunTargets() []RunTarget {
	var out []RunTarget
	for _, target := range c.RunTargets {
		if target.Source == RunTargetSourceDetected {
			out = append(out, target)
		}
	}
	return out
}

// FindRepo finds a repository by name.
func (c *Config) FindRepo(name string) (Repo, bool) {
	for _, repo := range c.Repos {
		if repo.Name == name {
			return repo, true
		}
	}
	return Repo{}, false
}

// GetRunTarget finds a run target by name.
func (c *Config) GetRunTarget(name string) (RunTarget, bool) {
	for _, target := range c.RunTargets {
		if target.Name == name {
			return target, true
		}
	}
	return RunTarget{}, false
}

// GetTerminalSize returns the terminal size. Returns 0,0 if not configured.
func (c *Config) GetTerminalSize() (width, height int) {
	if c.Terminal != nil && c.Terminal.Width > 0 && c.Terminal.Height > 0 {
		return c.Terminal.Width, c.Terminal.Height
	}
	return 0, 0 // not configured
}

// GetTerminalSeedLines returns the required seed_lines value.
func (c *Config) GetTerminalSeedLines() int {
	if c.Terminal == nil || c.Terminal.SeedLines <= 0 {
		return 0
	}
	return c.Terminal.SeedLines
}

// GetTerminalBootstrapLines returns the number of lines to send on WebSocket connect.
// Defaults to DefaultBootstrapLines if not set.
func (c *Config) GetTerminalBootstrapLines() int {
	if c.Terminal == nil || c.Terminal.BootstrapLines <= 0 {
		return DefaultBootstrapLines
	}
	return c.Terminal.BootstrapLines
}

// Reload reloads the configuration from disk and replaces this Config struct.
func (c *Config) Reload() error {
	if c.path == "" {
		return fmt.Errorf("config path not set: use Load() or CreateDefault() with a path")
	}

	data, err := os.ReadFile(c.path)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var newCfg Config
	if err := json.Unmarshal(data, &newCfg); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
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

	// Preserve the existing path
	existingPath := c.path
	newCfg.path = existingPath

	// Replace entire config
	*c = newCfg

	return nil
}

// CreateDefault creates a default config with the given config file path.
// The path is stored so that subsequent Save() calls write to the same location.
func CreateDefault(configPath string) *Config {
	return &Config{
		ConfigVersion:              version.Version,
		WorkspacePath:              "",
		Repos:                      []Repo{},
		RunTargets:                 []RunTarget{},
		QuickLaunch:                []QuickLaunch{},
		ExternalDiffCommands:       []ExternalDiffCommand{},
		ExternalDiffCleanupAfterMs: DefaultExternalDiffCleanupAfterMs,
		Terminal: &TerminalSize{
			Width:     DefaultTerminalWidth,
			Height:    DefaultTerminalHeight,
			SeedLines: DefaultTerminalSeedLines,
		},
		path: configPath,
	}
}

// Load loads the configuration from the specified path.
// The path is stored so that subsequent Save() calls write to the same location.
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

	normalizeRunTargets(cfg.RunTargets)

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
	if c.path == "" {
		return fmt.Errorf("config path not set: use Load() or CreateDefault() with a path")
	}

	// Update config version to current binary version
	c.ConfigVersion = version.Version

	// Ensure the directory exists
	schmuxDir := filepath.Dir(c.path)
	if schmuxDir != "." && schmuxDir != "" {
		if err := os.MkdirAll(schmuxDir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
	}

	// Marshal with indentation for readability
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to a temporary file first, then rename for atomicity
	tmpPath := c.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	if err := os.Rename(tmpPath, c.path); err != nil {
		os.Remove(tmpPath) // Clean up temp file
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// ConfigExists checks if the config file exists.
func ConfigExists() bool {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	configPath := filepath.Join(homeDir, ".schmux", "config.json")
	_, err = os.Stat(configPath)
	return err == nil
}

// EnsureExists checks if config exists, and offers to create one interactively if not.
// Returns true if config exists or was created, false if user declined or error occurred.
//
// Note: There is a TOCTOU race between ConfigExists() and Save(). If another process
// creates the config file between the check and save, this will overwrite it.
// This is acceptable for an interactive first-run flow where racing is unlikely.
func EnsureExists() (bool, error) {
	if ConfigExists() {
		return true, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false, fmt.Errorf("failed to get home directory: %w", err)
	}

	fmt.Println("Welcome to schmux!")
	fmt.Println()
	fmt.Println("No config file found at ~/.schmux/config.json")
	fmt.Println()
	fmt.Print("Would you like to create one now? [Y/n] ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read response: %w", err)
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response == "n" || response == "no" {
		fmt.Println("Config not created. Please create ~/.schmux/config.json manually to continue.")
		return false, nil
	}

	// Create default config with the config path set
	configPath := filepath.Join(homeDir, ".schmux", "config.json")
	cfg := CreateDefault(configPath)

	// Save config
	if err := cfg.Save(); err != nil {
		return false, fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("[config] created at %s\n", configPath)
	fmt.Println()
	fmt.Println("[config] open http://localhost:7337 to complete setup in the web dashboard")

	return true, nil
}

// GetXtermMtimePollIntervalMs returns the mtime polling interval in ms. Defaults to 5000ms.
func (c *Config) GetXtermMtimePollIntervalMs() int {
	if c.Xterm == nil || c.Xterm.MtimePollIntervalMs <= 0 {
		return 5000
	}
	return c.Xterm.MtimePollIntervalMs
}

// GetDashboardPollIntervalMs returns the dashboard sessions polling interval in ms. Defaults to 5000ms.
func (c *Config) GetDashboardPollIntervalMs() int {
	if c.Sessions == nil || c.Sessions.DashboardPollIntervalMs <= 0 {
		return 5000
	}
	return c.Sessions.DashboardPollIntervalMs
}

// GetNudgenikViewedBufferMs returns the viewed timestamp buffer in ms. Defaults to 5000ms.
func (c *Config) GetNudgenikViewedBufferMs() int {
	if c.Nudgenik == nil || c.Nudgenik.ViewedBufferMs <= 0 {
		return 5000
	}
	return c.Nudgenik.ViewedBufferMs
}

// GetNudgenikSeenIntervalMs returns the interval for marking sessions as seen in ms. Defaults to 2000ms.
func (c *Config) GetNudgenikSeenIntervalMs() int {
	if c.Nudgenik == nil || c.Nudgenik.SeenIntervalMs <= 0 {
		return 2000
	}
	return c.Nudgenik.SeenIntervalMs
}

// GetGitStatusPollIntervalMs returns the git status polling interval in ms. Defaults to 10000ms.
func (c *Config) GetGitStatusPollIntervalMs() int {
	if c.Sessions == nil || c.Sessions.GitStatusPollIntervalMs <= 0 {
		return 10000
	}
	return c.Sessions.GitStatusPollIntervalMs
}

// GetGitStatusWatchEnabled returns whether the git status file watcher is enabled. Defaults to true.
func (c *Config) GetGitStatusWatchEnabled() bool {
	if c.Sessions == nil || c.Sessions.GitStatusWatchEnabled == nil {
		return true
	}
	return *c.Sessions.GitStatusWatchEnabled
}

// GetGitStatusWatchDebounceMs returns the git status watcher debounce interval in ms. Defaults to 1000.
func (c *Config) GetGitStatusWatchDebounceMs() int {
	if c.Sessions == nil || c.Sessions.GitStatusWatchDebounceMs <= 0 {
		return DefaultGitStatusWatchDebounceMs
	}
	return c.Sessions.GitStatusWatchDebounceMs
}

// GitStatusWatchDebounce returns the git status watcher debounce interval as a time.Duration.
func (c *Config) GitStatusWatchDebounce() time.Duration {
	return time.Duration(c.GetGitStatusWatchDebounceMs()) * time.Millisecond
}

// GetXtermMaxLogSizeMB returns the max log size in MB before rotation. Defaults to 50MB.
func (c *Config) GetXtermMaxLogSizeMB() int64 {
	if c.Xterm == nil || c.Xterm.MaxLogSizeMB <= 0 {
		return DefaultMaxLogSizeMB
	}
	return int64(c.Xterm.MaxLogSizeMB)
}

// GetXtermRotatedLogSizeMB returns the target log size in MB after rotation. Defaults to 1MB.
func (c *Config) GetXtermRotatedLogSizeMB() int64 {
	if c.Xterm == nil || c.Xterm.RotatedLogSizeMB <= 0 {
		return DefaultRotatedLogSizeMB
	}
	return int64(c.Xterm.RotatedLogSizeMB)
}

// GetGitCloneTimeoutMs returns the git clone timeout in ms. Defaults to 300000 (5 min).
func (c *Config) GetGitCloneTimeoutMs() int {
	if c.Sessions == nil || c.Sessions.GitCloneTimeoutMs <= 0 {
		return DefaultGitCloneTimeoutMs
	}
	return c.Sessions.GitCloneTimeoutMs
}

// GetGitStatusTimeoutMs returns the git status timeout in ms. Defaults to 30000.
func (c *Config) GetGitStatusTimeoutMs() int {
	if c.Sessions == nil || c.Sessions.GitStatusTimeoutMs <= 0 {
		return DefaultGitStatusTimeoutMs
	}
	return c.Sessions.GitStatusTimeoutMs
}

// GetXtermQueryTimeoutMs returns the xterm query timeout in ms. Defaults to 5000.
func (c *Config) GetXtermQueryTimeoutMs() int {
	if c.Xterm == nil || c.Xterm.QueryTimeoutMs <= 0 {
		return DefaultXtermQueryTimeoutMs
	}
	return c.Xterm.QueryTimeoutMs
}

// GetXtermOperationTimeoutMs returns the xterm operation timeout in ms. Defaults to 10000.
func (c *Config) GetXtermOperationTimeoutMs() int {
	if c.Xterm == nil || c.Xterm.OperationTimeoutMs <= 0 {
		return DefaultXtermOperationTimeoutMs
	}
	return c.Xterm.OperationTimeoutMs
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
	if c.Network == nil || c.Network.BindAddress == "" {
		return "127.0.0.1"
	}
	return c.Network.BindAddress
}

// GetNetworkAccess returns whether the dashboard should be accessible from the local network.
// This is a convenience method that checks if bind_address is "0.0.0.0".
func (c *Config) GetNetworkAccess() bool {
	return c.GetBindAddress() == "0.0.0.0"
}

// GetPort returns the dashboard port. Defaults to 7337.
func (c *Config) GetPort() int {
	if c.Network == nil || c.Network.Port <= 0 {
		return 7337
	}
	return c.Network.Port
}

// GetPublicBaseURL returns the public base URL for the dashboard.
func (c *Config) GetPublicBaseURL() string {
	if c.Network == nil {
		return ""
	}
	return strings.TrimSpace(c.Network.PublicBaseURL)
}

// GetTLSCertPath returns the TLS certificate path.
func (c *Config) GetTLSCertPath() string {
	if c.Network == nil || c.Network.TLS == nil {
		return ""
	}
	return strings.TrimSpace(c.Network.TLS.CertPath)
}

// GetTLSKeyPath returns the TLS key path.
func (c *Config) GetTLSKeyPath() string {
	if c.Network == nil || c.Network.TLS == nil {
		return ""
	}
	return strings.TrimSpace(c.Network.TLS.KeyPath)
}

// GetTLSEnabled returns whether TLS is configured.
func (c *Config) GetTLSEnabled() bool {
	return c.GetTLSCertPath() != "" && c.GetTLSKeyPath() != ""
}

// GetAuthEnabled returns whether auth is enabled.
func (c *Config) GetAuthEnabled() bool {
	if c.AccessControl == nil {
		return false
	}
	return c.AccessControl.Enabled
}

// GetAuthProvider returns the auth provider (default: github).
func (c *Config) GetAuthProvider() string {
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

// DetectedToolsFromConfig returns detected tools as detect.Tool slices from the config.
// This is a shared helper used by multiple packages (session, oneshot, nudgenik).
func DetectedToolsFromConfig(cfg *Config) []detect.Tool {
	detectedTargets := cfg.GetDetectedRunTargets()
	tools := make([]detect.Tool, 0, len(detectedTargets))
	for _, target := range detectedTargets {
		tools = append(tools, detect.Tool{Name: target.Name, Command: target.Command})
	}
	return tools
}

// EnsureModelSecrets validates that all required secrets for a model are non-empty.
// This is a shared helper used by multiple packages (session, oneshot, nudgenik).
func EnsureModelSecrets(model detect.Model, secrets map[string]string) error {
	for _, key := range model.RequiredSecrets {
		val := strings.TrimSpace(secrets[key])
		if val == "" {
			return fmt.Errorf("%w: model %s missing required secret: %s", ErrInvalidConfig, model.ID, key)
		}
	}
	return nil
}

// GetRemoteFlavors returns the list of remote flavors.
func (c *Config) GetRemoteFlavors() []RemoteFlavor {
	if c.RemoteFlavors == nil {
		return []RemoteFlavor{}
	}
	return c.RemoteFlavors
}

// GetRemoteFlavor returns a remote flavor by ID.
func (c *Config) GetRemoteFlavor(id string) (RemoteFlavor, bool) {
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
	if c.RemoteWorkspace != nil && c.RemoteWorkspace.VSCodeCommandTemplate != "" {
		return c.RemoteWorkspace.VSCodeCommandTemplate
	}
	// Default to standard VS Code Remote-SSH format
	return `{{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}`
}

// GetConnectCommandTemplate returns the full connection command template for this flavor.
// This includes both the user's connection command and the tmux control mode invocation.
// Users only configure the connection part; schmux appends the tmux parts automatically.
func (rf *RemoteFlavor) GetConnectCommandTemplate() string {
	var baseCmd string
	if rf.ConnectCommand != "" {
		baseCmd = rf.ConnectCommand
	} else {
		// Default to standard SSH connection
		baseCmd = `ssh {{.Flavor}}`
	}
	// Append tmux control mode invocation
	return baseCmd + ` -- tmux -CC new-session -A -s schmux`
}

// GetReconnectCommandTemplate returns the full reconnection command template for this flavor.
// This includes both the user's reconnection command and the tmux control mode invocation.
// Users only configure the connection part; schmux appends the tmux parts automatically.
func (rf *RemoteFlavor) GetReconnectCommandTemplate() string {
	var baseCmd string
	if rf.ReconnectCommand != "" {
		baseCmd = rf.ReconnectCommand
	} else if rf.ConnectCommand != "" {
		// Use ConnectCommand as base (user should use {{.Hostname}} in it for reconnect)
		baseCmd = rf.ConnectCommand
	} else {
		// Default to standard SSH reconnection
		baseCmd = `ssh {{.Hostname}}`
	}
	// Append tmux control mode invocation
	return baseCmd + ` -- tmux -CC new-session -A -s schmux`
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
