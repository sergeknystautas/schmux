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
	configPath := filepath.Join(tmpDir, "test-config.json")

	// Create a valid config
	validConfig := Config{
		WorkspacePath: tmpDir,
		Repos: []Repo{
			{Name: "myproject", URL: "git@github.com:user/myproject.git"},
		},
		RunTargets: []RunTarget{
			{Name: "test-agent", Type: RunTargetTypePromptable, Command: "echo test"},
		},
		Terminal: &TerminalSize{
			Width:     120,
			Height:    40,
			SeedLines: 100,
		},
	}

	data, err := json.MarshalIndent(validConfig, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Load with explicit path
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.WorkspacePath != tmpDir {
		t.Errorf("WorkspacePath = %q, want %q", cfg.WorkspacePath, tmpDir)
	}

	// Verify Save() works (path should be set from Load)
	cfg.WorkspacePath = tmpDir + "/updated"
	if err := cfg.Save(); err != nil {
		t.Errorf("Save() failed: %v", err)
	}

	// Reload and verify
	cfg2, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() after save failed: %v", err)
	}
	if cfg2.WorkspacePath != tmpDir+"/updated" {
		t.Errorf("WorkspacePath after reload = %q, want %q", cfg2.WorkspacePath, tmpDir+"/updated")
	}
}

func TestGetWorkspacePath(t *testing.T) {
	cfg := &Config{
		WorkspacePath: "/tmp/workspaces",
	}

	path := cfg.GetWorkspacePath()
	if path != "/tmp/workspaces" {
		t.Errorf("got %q, want %q", path, "/tmp/workspaces")
	}
}

func TestGetRepos(t *testing.T) {
	repos := []Repo{
		{Name: "test1", URL: "git@github.com:test1/test1.git"},
		{Name: "test2", URL: "git@github.com:test2/test2.git"},
	}
	cfg := &Config{Repos: repos}

	got := cfg.GetRepos()
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestGetRunTargets(t *testing.T) {
	targets := []RunTarget{
		{Name: "glm-4.7", Type: RunTargetTypePromptable, Command: "~/bin/glm-4.7"},
		{Name: "zsh", Type: RunTargetTypeCommand, Command: "zsh"},
	}
	cfg := &Config{RunTargets: targets}

	got := cfg.GetRunTargets()
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestGetTerminalSize(t *testing.T) {
	t.Run("returns configured size", func(t *testing.T) {
		cfg := &Config{
			Terminal: &TerminalSize{Width: 120, Height: 40},
		}
		w, h := cfg.GetTerminalSize()
		if w != 120 || h != 40 {
			t.Errorf("got %d,%d, want 120,40", w, h)
		}
	})

	t.Run("returns 0,0 when not configured", func(t *testing.T) {
		cfg := &Config{}
		w, h := cfg.GetTerminalSize()
		if w != 0 || h != 0 {
			t.Errorf("got %d,%d, want 0,0", w, h)
		}
	})

	t.Run("returns 0,0 when terminal is nil", func(t *testing.T) {
		cfg := &Config{Terminal: nil}
		w, h := cfg.GetTerminalSize()
		if w != 0 || h != 0 {
			t.Errorf("got %d,%d, want 0,0", w, h)
		}
	})
}

func TestGetTerminalSeedLines(t *testing.T) {
	t.Run("returns configured seed lines", func(t *testing.T) {
		cfg := &Config{
			Terminal: &TerminalSize{SeedLines: 100},
		}
		got := cfg.GetTerminalSeedLines()
		if got != 100 {
			t.Errorf("got %d, want 100", got)
		}
	})

	t.Run("returns 0 when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetTerminalSeedLines()
		if got != 0 {
			t.Errorf("got %d, want 0", got)
		}
	})
}

func TestCreateDefault(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.json")
	cfg := CreateDefault(configPath)

	// WorkspacePath should be empty by default
	if cfg.WorkspacePath != "" {
		t.Errorf("WorkspacePath = %q, want empty", cfg.WorkspacePath)
	}

	if cfg.Terminal == nil {
		t.Fatal("Terminal should not be nil")
	}

	if cfg.Terminal.Width != DefaultTerminalWidth {
		t.Errorf("Width = %d, want %d", cfg.Terminal.Width, DefaultTerminalWidth)
	}

	if cfg.Terminal.Height != DefaultTerminalHeight {
		t.Errorf("Height = %d, want %d", cfg.Terminal.Height, DefaultTerminalHeight)
	}

	if cfg.Terminal.SeedLines != DefaultTerminalSeedLines {
		t.Errorf("SeedLines = %d, want %d", cfg.Terminal.SeedLines, DefaultTerminalSeedLines)
	}

	// Save should work since path is set
	cfg2 := CreateDefault(filepath.Join(tmpDir, "saved-config.json"))
	if err := cfg2.Save(); err != nil {
		t.Errorf("Save() failed: %v", err)
	}
}

func TestSave_RequiresPath(t *testing.T) {
	// Creating a config directly without a path should fail on Save
	cfg := &Config{
		WorkspacePath: "/tmp/test",
		Terminal: &TerminalSize{
			Width:     120,
			Height:    40,
			SeedLines: 100,
		},
	}

	err := cfg.Save()
	if err == nil {
		t.Fatal("Save() should fail when path is not set")
	}
	if err.Error() != "config path not set: use Load() or CreateDefault() with a path" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestReload_RequiresPath(t *testing.T) {
	// Creating a config directly without a path should fail on Reload
	cfg := &Config{
		WorkspacePath: "/tmp/test",
		Terminal: &TerminalSize{
			Width:     120,
			Height:    40,
			SeedLines: 100,
		},
	}

	err := cfg.Reload()
	if err == nil {
		t.Fatal("Reload() should fail when path is not set")
	}
	if err.Error() != "config path not set: use Load() or CreateDefault() with a path" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConfigExists(t *testing.T) {
	t.Run("returns false when config doesn't exist", func(t *testing.T) {
		// We can't easily test this without mocking home directory
		// Just verify the function runs
		exists := ConfigExists()
		_ = exists // Don't assert - depends on environment
	})
}

func TestGetXtermMtimePollIntervalMs(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Xterm: &XtermConfig{MtimePollIntervalMs: 1000},
		}
		got := cfg.GetXtermMtimePollIntervalMs()
		if got != 1000 {
			t.Errorf("got %d, want 1000", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetXtermMtimePollIntervalMs()
		if got != 5000 {
			t.Errorf("got %d, want 5000 (default)", got)
		}
	})
}

func TestGetDashboardPollIntervalMs(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Sessions: &SessionsConfig{DashboardPollIntervalMs: 2000},
		}
		got := cfg.GetDashboardPollIntervalMs()
		if got != 2000 {
			t.Errorf("got %d, want 2000", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetDashboardPollIntervalMs()
		if got != 5000 {
			t.Errorf("got %d, want 5000 (default)", got)
		}
	})
}

func TestGetNudgenikViewedBufferMs(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Nudgenik: &NudgenikConfig{ViewedBufferMs: 3000},
		}
		got := cfg.GetNudgenikViewedBufferMs()
		if got != 3000 {
			t.Errorf("got %d, want 3000", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetNudgenikViewedBufferMs()
		if got != 5000 {
			t.Errorf("got %d, want 5000 (default)", got)
		}
	})
}

func TestGetNudgenikSeenIntervalMs(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Nudgenik: &NudgenikConfig{SeenIntervalMs: 1500},
		}
		got := cfg.GetNudgenikSeenIntervalMs()
		if got != 1500 {
			t.Errorf("got %d, want 1500", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetNudgenikSeenIntervalMs()
		if got != 2000 {
			t.Errorf("got %d, want 2000 (default)", got)
		}
	})
}

func TestGetGitStatusPollIntervalMs(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Sessions: &SessionsConfig{GitStatusPollIntervalMs: 5000},
		}
		got := cfg.GetGitStatusPollIntervalMs()
		if got != 5000 {
			t.Errorf("got %d, want 5000", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetGitStatusPollIntervalMs()
		if got != 10000 {
			t.Errorf("got %d, want 10000 (default)", got)
		}
	})
}

func TestGetGitCloneTimeoutMs(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Sessions: &SessionsConfig{GitCloneTimeoutMs: 600000},
		}
		got := cfg.GetGitCloneTimeoutMs()
		if got != 600000 {
			t.Errorf("got %d, want 600000", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetGitCloneTimeoutMs()
		if got != DefaultGitCloneTimeoutMs {
			t.Errorf("got %d, want %d", got, DefaultGitCloneTimeoutMs)
		}
	})
}

func TestGetGitStatusTimeoutMs(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Sessions: &SessionsConfig{GitStatusTimeoutMs: 60000},
		}
		got := cfg.GetGitStatusTimeoutMs()
		if got != 60000 {
			t.Errorf("got %d, want 60000", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetGitStatusTimeoutMs()
		if got != DefaultGitStatusTimeoutMs {
			t.Errorf("got %d, want %d", got, DefaultGitStatusTimeoutMs)
		}
	})
}

func TestGetXtermQueryTimeoutMs(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Xterm: &XtermConfig{QueryTimeoutMs: 10000},
		}
		got := cfg.GetXtermQueryTimeoutMs()
		if got != 10000 {
			t.Errorf("got %d, want 10000", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetXtermQueryTimeoutMs()
		if got != DefaultXtermQueryTimeoutMs {
			t.Errorf("got %d, want %d", got, DefaultXtermQueryTimeoutMs)
		}
	})
}

func TestGetXtermOperationTimeoutMs(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Xterm: &XtermConfig{OperationTimeoutMs: 20000},
		}
		got := cfg.GetXtermOperationTimeoutMs()
		if got != 20000 {
			t.Errorf("got %d, want 20000", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetXtermOperationTimeoutMs()
		if got != DefaultXtermOperationTimeoutMs {
			t.Errorf("got %d, want %d", got, DefaultXtermOperationTimeoutMs)
		}
	})
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
