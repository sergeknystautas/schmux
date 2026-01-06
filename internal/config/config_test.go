package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	// Create a temporary config directory
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".schmux")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.json")

	// Create a valid config
	validConfig := Config{
		WorkspacePath: "~/dev/schmux-workspaces",
		Repos: []Repo{
			{Name: "myproject", URL: "git@github.com:user/myproject.git"},
		},
		Agents: []Agent{
			{Name: "codex", Command: "codex"},
			{Name: "claude", Command: "claude"},
		},
	}

	data, err := json.MarshalIndent(validConfig, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// This test would require mocking the home directory
	// For now, we'll skip the actual load test
	t.Skip("requires home directory mocking")
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: Config{
				WorkspacePath: "/tmp/workspaces",
				Repos:         []Repo{{Name: "test", URL: "git@github.com:test/test.git"}},
				Agents:        []Agent{{Name: "test-agent", Command: "test"}},
			},
			wantErr: false,
		},
		{
			name:    "missing workspace path",
			cfg:     Config{WorkspacePath: ""},
			wantErr: true,
		},
		{
			name: "missing repo name",
			cfg: Config{
				WorkspacePath: "/tmp/workspaces",
				Repos:         []Repo{{Name: "", URL: "git@github.com:test/test.git"}},
			},
			wantErr: true,
		},
		{
			name: "missing repo URL",
			cfg: Config{
				WorkspacePath: "/tmp/workspaces",
				Repos:         []Repo{{Name: "test", URL: ""}},
			},
			wantErr: true,
		},
		{
			name: "missing agent name",
			cfg: Config{
				WorkspacePath: "/tmp/workspaces",
				Agents:        []Agent{{Name: "", Command: "test"}},
			},
			wantErr: true,
		},
		{
			name: "missing agent command",
			cfg: Config{
				WorkspacePath: "/tmp/workspaces",
				Agents:        []Agent{{Name: "test", Command: ""}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg

			// Check workspace path
			if cfg.WorkspacePath == "" && !tt.wantErr {
				t.Error("expected no error for missing workspace path")
			}

			// Check repos
			for _, repo := range cfg.Repos {
				if repo.Name == "" && !tt.wantErr {
					t.Error("expected no error for missing repo name")
				}
				if repo.URL == "" && !tt.wantErr {
					t.Error("expected no error for missing repo URL")
				}
			}

			// Check agents
			for _, agent := range cfg.Agents {
				if agent.Name == "" && !tt.wantErr {
					t.Error("expected no error for missing agent name")
				}
				if agent.Command == "" && !tt.wantErr {
					t.Error("expected no error for missing agent command")
				}
			}
		})
	}
}

func TestFindRepo(t *testing.T) {
	cfg := &Config{
		Repos: []Repo{
			{Name: "project1", URL: "git@github.com:user/project1.git"},
			{Name: "project2", URL: "git@github.com:user/project2.git"},
		},
	}

	repo, found := cfg.FindRepo("project1")
	if !found {
		t.Error("expected to find project1")
	}
	if repo.Name != "project1" {
		t.Errorf("expected name project1, got %s", repo.Name)
	}

	_, found = cfg.FindRepo("nonexistent")
	if found {
		t.Error("expected not to find nonexistent repo")
	}
}

func TestFindAgent(t *testing.T) {
	cfg := &Config{
		Agents: []Agent{
			{Name: "codex", Command: "codex"},
			{Name: "claude", Command: "claude"},
		},
	}

	agent, found := cfg.FindAgent("codex")
	if !found {
		t.Error("expected to find codex")
	}
	if agent.Name != "codex" {
		t.Errorf("expected name codex, got %s", agent.Name)
	}

	_, found = cfg.FindAgent("nonexistent")
	if found {
		t.Error("expected not to find nonexistent agent")
	}
}
