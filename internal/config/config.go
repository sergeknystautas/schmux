package config

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
)

// Config represents the application configuration.
type Config struct {
	WorkspacePath string             `json:"workspace_path"`
	Repos         []Repo             `json:"repos"`
	Agents        []Agent            `json:"agents"`
	Terminal      *TerminalSize      `json:"terminal,omitempty"`
	Internal      *InternalIntervals `json:"internal,omitempty"`
	mu            sync.RWMutex
}

// TerminalSize represents terminal dimensions.
type TerminalSize struct {
	Width     int `json:"width"`
	Height    int `json:"height"`
	SeedLines int `json:"seed_lines"`
}

// InternalIntervals represents timing intervals for internal polling and caching.
type InternalIntervals struct {
	MtimePollIntervalMs    int `json:"mtime_poll_interval_ms"`
	SessionsPollIntervalMs int `json:"sessions_poll_interval_ms"`
	ViewedBufferMs         int `json:"viewed_buffer_ms"`
	SessionSeenIntervalMs  int `json:"session_seen_interval_ms"`
	GitStatusPollIntervalMs int `json:"git_status_poll_interval_ms"`
}

// Repo represents a git repository configuration.
type Repo struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Agent represents an AI agent configuration.
type Agent struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

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
  "agents": [{"name": "claude", "command": "claude"}],
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
	// Validate agents
	for _, agent := range cfg.Agents {
		if agent.Name == "" {
			return nil, fmt.Errorf("%w: agent name is required", ErrInvalidConfig)
		}
		if agent.Command == "" {
			return nil, fmt.Errorf("%w: agent command is required for %s", ErrInvalidConfig, agent.Name)
		}
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

// GetWorkspacePath returns the workspace directory path.
func (c *Config) GetWorkspacePath() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.WorkspacePath
}

// GetRepos returns the list of repositories.
func (c *Config) GetRepos() []Repo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Repos
}

// GetAgents returns the list of agents.
func (c *Config) GetAgents() []Agent {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Agents
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

// FindAgent finds an agent by name.
func (c *Config) FindAgent(name string) (Agent, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, agent := range c.Agents {
		if agent.Name == name {
			return agent, true
		}
	}
	return Agent{}, false
}

// GetTerminalSize returns the terminal size. Returns 0,0 if not configured.
func (c *Config) GetTerminalSize() (width, height int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Terminal != nil && c.Terminal.Width > 0 && c.Terminal.Height > 0 {
		return c.Terminal.Width, c.Terminal.Height
	}
	return 0, 0 // not configured
}

// GetTerminalSeedLines returns the required seed_lines value.
func (c *Config) GetTerminalSeedLines() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Terminal == nil || c.Terminal.SeedLines <= 0 {
		return 0
	}
	return c.Terminal.SeedLines
}

// Reload reloads the configuration from disk and updates this Config struct in place.
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

	// Update the existing config in place
	c.mu.Lock()
	c.WorkspacePath = newCfg.WorkspacePath
	c.Repos = newCfg.Repos
	c.Agents = newCfg.Agents
	c.Terminal = newCfg.Terminal
	c.Internal = newCfg.Internal
	c.mu.Unlock()

	return nil
}

// CreateDefault creates a default config with the given workspace path.
func CreateDefault(workspacePath string) *Config {
	return &Config{
		WorkspacePath: workspacePath,
		Repos:         []Repo{},
		Agents:        []Agent{},
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Internal == nil || c.Internal.MtimePollIntervalMs <= 0 {
		return 5000
	}
	return c.Internal.MtimePollIntervalMs
}

// GetSessionsPollIntervalMs returns the sessions API polling interval in ms. Defaults to 5000ms.
func (c *Config) GetSessionsPollIntervalMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Internal == nil || c.Internal.SessionsPollIntervalMs <= 0 {
		return 5000
	}
	return c.Internal.SessionsPollIntervalMs
}

// GetViewedBufferMs returns the viewed timestamp buffer in ms. Defaults to 5000ms.
func (c *Config) GetViewedBufferMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Internal == nil || c.Internal.ViewedBufferMs <= 0 {
		return 5000
	}
	return c.Internal.ViewedBufferMs
}

// GetSessionSeenIntervalMs returns the interval for marking sessions as viewed in ms. Defaults to 2000ms.
func (c *Config) GetSessionSeenIntervalMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Internal == nil || c.Internal.SessionSeenIntervalMs <= 0 {
		return 2000
	}
	return c.Internal.SessionSeenIntervalMs
}

// GetGitStatusPollIntervalMs returns the git status polling interval in ms. Defaults to 10000ms.
func (c *Config) GetGitStatusPollIntervalMs() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.Internal == nil || c.Internal.GitStatusPollIntervalMs <= 0 {
		return 10000
	}
	return c.Internal.GitStatusPollIntervalMs
}
