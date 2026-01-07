package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var (
	ErrConfigNotFound = errors.New("config file not found")
	ErrInvalidConfig  = errors.New("invalid config")
)

// Config represents the application configuration.
type Config struct {
	WorkspacePath string  `json:"workspace_path"`
	Repos         []Repo  `json:"repos"`
	Agents        []Agent `json:"agents"`
	mu            sync.RWMutex
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
			return nil, fmt.Errorf("%w: %s", ErrConfigNotFound, configPath)
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}

	// Validate config
	if cfg.WorkspacePath == "" {
		return nil, fmt.Errorf("%w: workspace_path is required", ErrInvalidConfig)
	}

	// Expand workspace path (handle ~)
	if cfg.WorkspacePath[0] == '~' {
		cfg.WorkspacePath = filepath.Join(homeDir, cfg.WorkspacePath[1:])
	}

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
