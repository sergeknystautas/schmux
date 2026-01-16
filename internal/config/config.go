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

	// Default timeout values in seconds
	DefaultGitCloneTimeoutSeconds      = 300 // 5 minutes
	DefaultGitStatusTimeoutSeconds     = 30  // 30 seconds
	DefaultTmuxQueryTimeoutSeconds     = 5   // 5 seconds
	DefaultTmuxOperationTimeoutSeconds = 10  // 10 seconds
)

// Config represents the application configuration.
type Config struct {
	WorkspacePath string             `json:"workspace_path"`
	Repos         []Repo             `json:"repos"`
	RunTargets    []RunTarget        `json:"run_targets"`
	QuickLaunch   []QuickLaunch      `json:"quick_launch"`
	Variants      []VariantConfig    `json:"variants,omitempty"`
	Nudgenik      *NudgenikConfig    `json:"nudgenik,omitempty"`
	Terminal      *TerminalSize      `json:"terminal,omitempty"`
	Internal      *InternalIntervals `json:"internal,omitempty"`
	NetworkAccess bool               `json:"network_access,omitempty"` // true = bind to 0.0.0.0 (LAN), false = 127.0.0.1 (localhost only)
}

// TerminalSize represents terminal dimensions.
type TerminalSize struct {
	Width          int `json:"width"`
	Height         int `json:"height"`
	SeedLines      int `json:"seed_lines"`
	BootstrapLines int `json:"bootstrap_lines,omitempty"`
}

// InternalIntervals represents timing intervals for internal polling and caching.
type InternalIntervals struct {
	MtimePollIntervalMs     int       `json:"mtime_poll_interval_ms"`
	SessionsPollIntervalMs  int       `json:"sessions_poll_interval_ms"`
	ViewedBufferMs          int       `json:"viewed_buffer_ms"`
	SessionSeenIntervalMs   int       `json:"session_seen_interval_ms"`
	GitStatusPollIntervalMs int       `json:"git_status_poll_interval_ms"`
	Timeouts                *Timeouts `json:"timeouts,omitempty"`
}

// Timeouts represents timeout values for external operations (in seconds).
type Timeouts struct {
	GitCloneSeconds      int `json:"git_clone_seconds"`      // default: 300 (5 min)
	GitStatusSeconds     int `json:"git_status_seconds"`     // default: 30
	TmuxQuerySeconds     int `json:"tmux_query_seconds"`     // default: 5
	TmuxOperationSeconds int `json:"tmux_operation_seconds"` // default: 10
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

// NudgenikConfig represents configuration for the NudgeNik assistant.
type NudgenikConfig struct {
	Target string `json:"target,omitempty"`
}

const (
	RunTargetTypePromptable = "promptable"
	RunTargetTypeCommand    = "command"
	RunTargetSourceUser     = "user"
	RunTargetSourceDetected = "detected"
)

// Load loads the configuration from ~/.schmux/config.json.
func Load() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := filepath.Join(homeDir, ".schmux", "config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			exampleConfig := fmt.Sprintf(`{
  "workspace_path": "~/schmux-workspaces",
  "repos": [{"name": "myproject", "url": "git@github.com:user/myproject.git"}],
  "run_targets": [{"name": "glm-4.7", "type": "promptable", "command": "~/bin/glm-4.7"}],
  "terminal": {"width": %d, "height": %d, "seed_lines": %d}
}`, DefaultTerminalWidth, DefaultTerminalHeight, DefaultTerminalSeedLines)
			return nil, fmt.Errorf("%w: %s\n\nNo config file found. Please create it manually:\n\n  %s\n\nExample config:\n%s\n",
				ErrConfigNotFound, configPath, configPath, exampleConfig)
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	normalizeRunTargets(cfg.RunTargets)

	// Validate config (workspace_path can be empty during wizard setup)
	// Validate repos
	for _, repo := range cfg.Repos {
		if repo.Name == "" {
			return nil, fmt.Errorf("%w: repo name is required", ErrInvalidConfig)
		}
		if repo.URL == "" {
			return nil, fmt.Errorf("%w: repo URL is required for %s", ErrInvalidConfig, repo.Name)
		}
	}
	if err := validateRunTargets(cfg.RunTargets); err != nil {
		return nil, err
	}

	if err := validateVariantConfigs(cfg.Variants); err != nil {
		return nil, err
	}

	if err := validateQuickLaunch(cfg.QuickLaunch, cfg.RunTargets, cfg.Variants); err != nil {
		return nil, err
	}
	if err := validateRunTargetDependencies(cfg.RunTargets, cfg.Variants, cfg.QuickLaunch, cfg.Nudgenik); err != nil {
		return nil, err
	}

	// Expand workspace path (handle ~) - allow empty during wizard setup
	if cfg.WorkspacePath != "" && cfg.WorkspacePath[0] == '~' {
		cfg.WorkspacePath = filepath.Join(homeDir, cfg.WorkspacePath[1:])
	}

	// Validate terminal config (width, height, and seed_lines are required)
	if cfg.Terminal == nil {
		return nil, fmt.Errorf("%w: terminal is required (set terminal.width, terminal.height, and terminal.seed_lines)", ErrInvalidConfig)
	}
	if cfg.Terminal.Width <= 0 {
		return nil, fmt.Errorf("%w: terminal.width must be > 0", ErrInvalidConfig)
	}
	if cfg.Terminal.Height <= 0 {
		return nil, fmt.Errorf("%w: terminal.height must be > 0", ErrInvalidConfig)
	}
	if cfg.Terminal.SeedLines <= 0 {
		return nil, fmt.Errorf("%w: terminal.seed_lines must be > 0", ErrInvalidConfig)
	}

	return &cfg, nil
}

// Validate validates run targets, variants, and quick launch presets.
func (c *Config) Validate() error {
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
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	configPath := filepath.Join(homeDir, ".schmux", "config.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	var newCfg Config
	if err := json.Unmarshal(data, &newCfg); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	// Expand workspace path (handle ~)
	if newCfg.WorkspacePath != "" && newCfg.WorkspacePath[0] == '~' {
		newCfg.WorkspacePath = filepath.Join(homeDir, newCfg.WorkspacePath[1:])
	}

	// Replace entire config
	*c = newCfg

	return nil
}

// CreateDefault creates a default config with the given workspace path.
func CreateDefault(workspacePath string) *Config {
	return &Config{
		WorkspacePath: workspacePath,
		Repos:         []Repo{},
		RunTargets:    []RunTarget{},
		QuickLaunch:   []QuickLaunch{},
		Terminal: &TerminalSize{
			Width:     DefaultTerminalWidth,
			Height:    DefaultTerminalHeight,
			SeedLines: DefaultTerminalSeedLines,
		},
	}
}

// Save writes the config to ~/.schmux/config.json.
func (c *Config) Save() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	schmuxDir := filepath.Join(homeDir, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		return fmt.Errorf("failed to create schmux directory: %w", err)
	}

	configPath := filepath.Join(schmuxDir, "config.json")

	// Marshal with indentation for readability
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to a temporary file first, then rename for atomicity
	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	if err := os.Rename(tmpPath, configPath); err != nil {
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

	// Detect available AI agent tools
	// Create default config with empty workspace path (user will set in wizard)
	cfg := CreateDefault("")

	// Save config
	if err := cfg.Save(); err != nil {
		return false, fmt.Errorf("failed to save config: %w", err)
	}

	configPath := filepath.Join(homeDir, ".schmux", "config.json")
	fmt.Printf("Config created at %s\n", configPath)
	fmt.Println()
	fmt.Println("Open http://localhost:7337 to complete setup in the web dashboard")

	return true, nil
}

// GetMtimePollIntervalMs returns the mtime polling interval in ms. Defaults to 5000ms.
func (c *Config) GetMtimePollIntervalMs() int {
	if c.Internal == nil || c.Internal.MtimePollIntervalMs <= 0 {
		return 5000
	}
	return c.Internal.MtimePollIntervalMs
}

// GetSessionsPollIntervalMs returns the sessions API polling interval in ms. Defaults to 5000ms.
func (c *Config) GetSessionsPollIntervalMs() int {
	if c.Internal == nil || c.Internal.SessionsPollIntervalMs <= 0 {
		return 5000
	}
	return c.Internal.SessionsPollIntervalMs
}

// GetViewedBufferMs returns the viewed timestamp buffer in ms. Defaults to 5000ms.
func (c *Config) GetViewedBufferMs() int {
	if c.Internal == nil || c.Internal.ViewedBufferMs <= 0 {
		return 5000
	}
	return c.Internal.ViewedBufferMs
}

// GetSessionSeenIntervalMs returns the interval for marking sessions as viewed in ms. Defaults to 2000ms.
func (c *Config) GetSessionSeenIntervalMs() int {
	if c.Internal == nil || c.Internal.SessionSeenIntervalMs <= 0 {
		return 2000
	}
	return c.Internal.SessionSeenIntervalMs
}

// GetGitStatusPollIntervalMs returns the git status polling interval in ms. Defaults to 10000ms.
func (c *Config) GetGitStatusPollIntervalMs() int {
	if c.Internal == nil || c.Internal.GitStatusPollIntervalMs <= 0 {
		return 10000
	}
	return c.Internal.GitStatusPollIntervalMs
}

// GetTimeouts returns the Timeouts config, or defaults if not set.
func (c *Config) GetTimeouts() *Timeouts {
	// Return existing Timeouts if available
	if c.Internal != nil && c.Internal.Timeouts != nil {
		return c.Internal.Timeouts
	}

	// Return defaults
	return &Timeouts{
		GitCloneSeconds:      DefaultGitCloneTimeoutSeconds,
		GitStatusSeconds:     DefaultGitStatusTimeoutSeconds,
		TmuxQuerySeconds:     DefaultTmuxQueryTimeoutSeconds,
		TmuxOperationSeconds: DefaultTmuxOperationTimeoutSeconds,
	}
}

// GetGitCloneTimeoutSeconds returns the git clone timeout in seconds. Defaults to 300 (5 min).
func (c *Config) GetGitCloneTimeoutSeconds() int {
	if c.Internal == nil || c.Internal.Timeouts == nil || c.Internal.Timeouts.GitCloneSeconds <= 0 {
		return DefaultGitCloneTimeoutSeconds
	}
	return c.Internal.Timeouts.GitCloneSeconds
}

// GetGitStatusTimeoutSeconds returns the git status timeout in seconds. Defaults to 30.
func (c *Config) GetGitStatusTimeoutSeconds() int {
	if c.Internal == nil || c.Internal.Timeouts == nil || c.Internal.Timeouts.GitStatusSeconds <= 0 {
		return DefaultGitStatusTimeoutSeconds
	}
	return c.Internal.Timeouts.GitStatusSeconds
}

// GetTmuxQueryTimeoutSeconds returns the tmux query timeout in seconds. Defaults to 5.
func (c *Config) GetTmuxQueryTimeoutSeconds() int {
	if c.Internal == nil || c.Internal.Timeouts == nil || c.Internal.Timeouts.TmuxQuerySeconds <= 0 {
		return DefaultTmuxQueryTimeoutSeconds
	}
	return c.Internal.Timeouts.TmuxQuerySeconds
}

// GetTmuxOperationTimeoutSeconds returns the tmux operation timeout in seconds. Defaults to 10.
func (c *Config) GetTmuxOperationTimeoutSeconds() int {
	if c.Internal == nil || c.Internal.Timeouts == nil || c.Internal.Timeouts.TmuxOperationSeconds <= 0 {
		return DefaultTmuxOperationTimeoutSeconds
	}
	return c.Internal.Timeouts.TmuxOperationSeconds
}

// GitCloneTimeout returns the git clone timeout as a time.Duration.
func (c *Config) GitCloneTimeout() time.Duration {
	return time.Duration(c.GetGitCloneTimeoutSeconds()) * time.Second
}

// GitStatusTimeout returns the git status timeout as a time.Duration.
func (c *Config) GitStatusTimeout() time.Duration {
	return time.Duration(c.GetGitStatusTimeoutSeconds()) * time.Second
}

// TmuxQueryTimeout returns the tmux query timeout as a time.Duration.
func (c *Config) TmuxQueryTimeout() time.Duration {
	return time.Duration(c.GetTmuxQueryTimeoutSeconds()) * time.Second
}

// TmuxOperationTimeout returns the tmux operation timeout as a time.Duration.
func (c *Config) TmuxOperationTimeout() time.Duration {
	return time.Duration(c.GetTmuxOperationTimeoutSeconds()) * time.Second
}

// GetNetworkAccess returns whether the dashboard should be accessible from the local network.
// Defaults to false (localhost only).
func (c *Config) GetNetworkAccess() bool {
	return c.NetworkAccess
}
