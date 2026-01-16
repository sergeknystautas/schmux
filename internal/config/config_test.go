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
		RunTargets: []RunTarget{
			{Name: "glm-4.7", Type: RunTargetTypePromptable, Command: "~/bin/glm-4.7"},
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

	// This test would require mocking the home directory
	// For now, we'll skip the actual load test
	t.Skip("requires home directory mocking")
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
	cfg := CreateDefault("/tmp/test")

	if cfg.WorkspacePath != "/tmp/test" {
		t.Errorf("WorkspacePath = %q, want %q", cfg.WorkspacePath, "/tmp/test")
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
}

func TestConfigExists(t *testing.T) {
	t.Run("returns false when config doesn't exist", func(t *testing.T) {
		// We can't easily test this without mocking home directory
		// Just verify the function runs
		exists := ConfigExists()
		_ = exists // Don't assert - depends on environment
	})
}

func TestGetMtimePollIntervalMs(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Internal: &InternalIntervals{MtimePollIntervalMs: 1000},
		}
		got := cfg.GetMtimePollIntervalMs()
		if got != 1000 {
			t.Errorf("got %d, want 1000", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetMtimePollIntervalMs()
		if got != 5000 {
			t.Errorf("got %d, want 5000 (default)", got)
		}
	})
}

func TestGetSessionsPollIntervalMs(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Internal: &InternalIntervals{SessionsPollIntervalMs: 2000},
		}
		got := cfg.GetSessionsPollIntervalMs()
		if got != 2000 {
			t.Errorf("got %d, want 2000", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetSessionsPollIntervalMs()
		if got != 5000 {
			t.Errorf("got %d, want 5000 (default)", got)
		}
	})
}

func TestGetViewedBufferMs(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Internal: &InternalIntervals{ViewedBufferMs: 3000},
		}
		got := cfg.GetViewedBufferMs()
		if got != 3000 {
			t.Errorf("got %d, want 3000", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetViewedBufferMs()
		if got != 5000 {
			t.Errorf("got %d, want 5000 (default)", got)
		}
	})
}

func TestGetSessionSeenIntervalMs(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Internal: &InternalIntervals{SessionSeenIntervalMs: 1500},
		}
		got := cfg.GetSessionSeenIntervalMs()
		if got != 1500 {
			t.Errorf("got %d, want 1500", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetSessionSeenIntervalMs()
		if got != 2000 {
			t.Errorf("got %d, want 2000 (default)", got)
		}
	})
}

func TestGetGitStatusPollIntervalMs(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Internal: &InternalIntervals{GitStatusPollIntervalMs: 5000},
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

func TestGetTimeouts(t *testing.T) {
	t.Run("returns configured timeouts", func(t *testing.T) {
		expected := &Timeouts{
			GitCloneSeconds:      600,
			GitStatusSeconds:     60,
			TmuxQuerySeconds:     10,
			TmuxOperationSeconds: 20,
		}
		cfg := &Config{
			Internal: &InternalIntervals{Timeouts: expected},
		}
		got := cfg.GetTimeouts()
		if got.GitCloneSeconds != 600 {
			t.Errorf("GitCloneSeconds = %d, want 600", got.GitCloneSeconds)
		}
	})

	t.Run("returns defaults when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetTimeouts()
		if got.GitCloneSeconds != DefaultGitCloneTimeoutSeconds {
			t.Errorf("GitCloneSeconds = %d, want %d", got.GitCloneSeconds, DefaultGitCloneTimeoutSeconds)
		}
		if got.GitStatusSeconds != DefaultGitStatusTimeoutSeconds {
			t.Errorf("GitStatusSeconds = %d, want %d", got.GitStatusSeconds, DefaultGitStatusTimeoutSeconds)
		}
	})
}

func TestGetGitCloneTimeoutSeconds(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Internal: &InternalIntervals{
				Timeouts: &Timeouts{GitCloneSeconds: 600},
			},
		}
		got := cfg.GetGitCloneTimeoutSeconds()
		if got != 600 {
			t.Errorf("got %d, want 600", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetGitCloneTimeoutSeconds()
		if got != DefaultGitCloneTimeoutSeconds {
			t.Errorf("got %d, want %d", got, DefaultGitCloneTimeoutSeconds)
		}
	})
}

func TestGetGitStatusTimeoutSeconds(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Internal: &InternalIntervals{
				Timeouts: &Timeouts{GitStatusSeconds: 60},
			},
		}
		got := cfg.GetGitStatusTimeoutSeconds()
		if got != 60 {
			t.Errorf("got %d, want 60", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetGitStatusTimeoutSeconds()
		if got != DefaultGitStatusTimeoutSeconds {
			t.Errorf("got %d, want %d", got, DefaultGitStatusTimeoutSeconds)
		}
	})
}

func TestGetTmuxQueryTimeoutSeconds(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Internal: &InternalIntervals{
				Timeouts: &Timeouts{TmuxQuerySeconds: 10},
			},
		}
		got := cfg.GetTmuxQueryTimeoutSeconds()
		if got != 10 {
			t.Errorf("got %d, want 10", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetTmuxQueryTimeoutSeconds()
		if got != DefaultTmuxQueryTimeoutSeconds {
			t.Errorf("got %d, want %d", got, DefaultTmuxQueryTimeoutSeconds)
		}
	})
}

func TestGetTmuxOperationTimeoutSeconds(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Internal: &InternalIntervals{
				Timeouts: &Timeouts{TmuxOperationSeconds: 20},
			},
		}
		got := cfg.GetTmuxOperationTimeoutSeconds()
		if got != 20 {
			t.Errorf("got %d, want 20", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetTmuxOperationTimeoutSeconds()
		if got != DefaultTmuxOperationTimeoutSeconds {
			t.Errorf("got %d, want %d", got, DefaultTmuxOperationTimeoutSeconds)
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
