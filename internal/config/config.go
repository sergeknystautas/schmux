package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	DefaultGitCloneTimeoutMs       = 300000 // 5 minutes
	DefaultGitStatusTimeoutMs      = 30000  // 30 seconds
	DefaultXtermQueryTimeoutMs     = 5000   // 5 seconds
	DefaultXtermOperationTimeoutMs = 10000  // 10 seconds
)

// Config represents the application configuration.
type Config struct {
	WorkspacePath string               `json:"workspace_path"`
	Repos         []Repo               `json:"repos"`
	RunTargets    []RunTarget          `json:"run_targets"`
	QuickLaunch   []QuickLaunch        `json:"quick_launch"`
	Variants      []VariantConfig      `json:"variants,omitempty"`
	Terminal      *TerminalSize        `json:"terminal,omitempty"`
	Nudgenik      *NudgenikConfig      `json:"nudgenik,omitempty"`
	Sessions      *SessionsConfig      `json:"sessions,omitempty"`
	Xterm         *XtermConfig         `json:"xterm,omitempty"`
	AccessControl *AccessControlConfig `json:"access_control,omitempty"`

	// path is the file path where this config was loaded from or should be saved to.
	// Not serialized to JSON.
	path string `json:"-"`
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

// SessionsConfig represents session and git-related timing configuration.
type SessionsConfig struct {
	DashboardPollIntervalMs int `json:"dashboard_poll_interval_ms"`
	GitStatusPollIntervalMs int `json:"git_status_poll_interval_ms"`
	GitCloneTimeoutMs       int `json:"git_clone_timeout_ms"`
	GitStatusTimeoutMs      int `json:"git_status_timeout_ms"`
}

// XtermConfig represents terminal capture, timeouts, and log rotation settings.
type XtermConfig struct {
	MtimePollIntervalMs int `json:"mtime_poll_interval_ms"`
	QueryTimeoutMs      int `json:"query_timeout_ms"`
	OperationTimeoutMs  int `json:"operation_timeout_ms"`
	MaxLogSizeMB        int `json:"max_log_size_mb,omitempty"`     // max log size before rotation
	RotatedLogSizeMB    int `json:"rotated_log_size_mb,omitempty"` // target size after rotation (keeps tail)
}

// AccessControlConfig controls external access.
type AccessControlConfig struct {
	NetworkAccess bool `json:"network_access"`
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
type QuickLaunch struct {
	Name   string  `json:"name"`
	Target string  `json:"target"`
	Prompt *string `json:"prompt"`
}

// VariantConfig represents a variant in config.json.
// Used when users customize or disable variants.
type VariantConfig struct {
	Name    string            `json:"name"`
	Enabled *bool             `json:"enabled,omitempty"` // nil = enabled by default
	Env     map[string]string `json:"env,omitempty"`     // overrides
}

const (
	RunTargetTypePromptable = "promptable"
	RunTargetTypeCommand    = "command"
	RunTargetSourceUser     = "user"
	RunTargetSourceDetected = "detected"
)

// Validate validates the config including terminal settings, run targets, variants, and quick launch presets.
func (c *Config) Validate() error {
	// Validate terminal config (required for daemon operation)
	if c.Terminal == nil {
		return fmt.Errorf("%w: terminal is required (set terminal.width, terminal.height, and terminal.seed_lines)", ErrInvalidConfig)
	}
	if c.Terminal.Width <= 0 {
		return fmt.Errorf("%w: terminal.width must be > 0", ErrInvalidConfig)
	}
	if c.Terminal.Height <= 0 {
		return fmt.Errorf("%w: terminal.height must be > 0", ErrInvalidConfig)
	}
	if c.Terminal.SeedLines <= 0 {
		return fmt.Errorf("%w: terminal.seed_lines must be > 0", ErrInvalidConfig)
	}

	if err := validateRunTargets(c.RunTargets); err != nil {
		return err
	}
	if err := validateVariantConfigs(c.Variants); err != nil {
		return err
	}
	if err := validateQuickLaunch(c.QuickLaunch, c.RunTargets, c.Variants); err != nil {
		return err
	}
	if err := validateRunTargetDependencies(c.RunTargets, c.Variants, c.QuickLaunch, c.Nudgenik); err != nil {
		return err
	}
	return nil
}

// GetWorkspacePath returns the workspace directory path.
func (c *Config) GetWorkspacePath() string {
	return c.WorkspacePath
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

// GetNudgenikTarget returns the configured nudgenik target name, if any.
func (c *Config) GetNudgenikTarget() string {
	if c == nil || c.Nudgenik == nil {
		return ""
	}
	return strings.TrimSpace(c.Nudgenik.Target)
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
		WorkspacePath: "",
		Repos:         []Repo{},
		RunTargets:    []RunTarget{},
		QuickLaunch:   []QuickLaunch{},
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

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	normalizeRunTargets(cfg.RunTargets)

	// Store the config path so Save() writes to the same location
	cfg.path = configPath

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

	return &cfg, nil
}

// Save writes the config to the path it was loaded from or created with.
func (c *Config) Save() error {
	if c.path == "" {
		return fmt.Errorf("config path not set: use Load() or CreateDefault() with a path")
	}

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

	fmt.Printf("Config created at %s\n", configPath)
	fmt.Println()
	fmt.Println("Open http://localhost:7337 to complete setup in the web dashboard")

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

// GetNetworkAccess returns whether the dashboard should be accessible from the local network.
// Defaults to false (localhost only).
func (c *Config) GetNetworkAccess() bool {
	if c.AccessControl == nil {
		return false
	}
	return c.AccessControl.NetworkAccess
}
