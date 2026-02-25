package config

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/version"
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
	}

	data, err := json.MarshalIndent(&validConfig, "", "  ")
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

func TestCreateDefault(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.json")
	cfg := CreateDefault(configPath)

	// WorkspacePath should be empty by default
	if cfg.WorkspacePath != "" {
		t.Errorf("WorkspacePath = %q, want empty", cfg.WorkspacePath)
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
		// Save and restore HOME to test with a known directory
		origHome := os.Getenv("HOME")
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)
		defer os.Setenv("HOME", origHome)

		exists := ConfigExists()
		if exists {
			t.Error("expected ConfigExists() to return false with empty HOME")
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

func TestGetSubredditTarget(t *testing.T) {
	t.Run("returns empty string when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetSubredditTarget()
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})

	t.Run("returns empty string when subreddit config exists but target is empty", func(t *testing.T) {
		cfg := &Config{Subreddit: &SubredditConfig{}}
		got := cfg.GetSubredditTarget()
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})

	t.Run("returns configured target", func(t *testing.T) {
		cfg := &Config{Subreddit: &SubredditConfig{Target: "sonnet"}}
		got := cfg.GetSubredditTarget()
		if got != "sonnet" {
			t.Errorf("got %q, want %q", got, "sonnet")
		}
	})

	t.Run("trims whitespace from target", func(t *testing.T) {
		cfg := &Config{Subreddit: &SubredditConfig{Target: "  sonnet  "}}
		got := cfg.GetSubredditTarget()
		if got != "sonnet" {
			t.Errorf("got %q, want %q", got, "sonnet")
		}
	})
}

func TestGetSubredditHours(t *testing.T) {
	t.Run("returns default 24 when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetSubredditHours()
		if got != 24 {
			t.Errorf("got %d, want 24 (default)", got)
		}
	})

	t.Run("returns default 24 when hours is zero", func(t *testing.T) {
		cfg := &Config{Subreddit: &SubredditConfig{Hours: 0}}
		got := cfg.GetSubredditHours()
		if got != 24 {
			t.Errorf("got %d, want 24 (default)", got)
		}
	})

	t.Run("returns configured hours", func(t *testing.T) {
		cfg := &Config{Subreddit: &SubredditConfig{Hours: 48}}
		got := cfg.GetSubredditHours()
		if got != 48 {
			t.Errorf("got %d, want 48", got)
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

func TestGetDisposeGracePeriodMs(t *testing.T) {
	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{
			Sessions: &SessionsConfig{DisposeGracePeriodMs: 60000},
		}
		got := cfg.GetDisposeGracePeriodMs()
		if got != 60000 {
			t.Errorf("got %d, want 60000", got)
		}
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		cfg := &Config{}
		got := cfg.GetDisposeGracePeriodMs()
		if got != DefaultDisposeGracePeriodMs {
			t.Errorf("got %d, want %d", got, DefaultDisposeGracePeriodMs)
		}
	})

	t.Run("returns default when sessions nil", func(t *testing.T) {
		cfg := &Config{Sessions: nil}
		got := cfg.GetDisposeGracePeriodMs()
		if got != DefaultDisposeGracePeriodMs {
			t.Errorf("got %d, want %d", got, DefaultDisposeGracePeriodMs)
		}
	})

	t.Run("returns default when zero", func(t *testing.T) {
		cfg := &Config{
			Sessions: &SessionsConfig{DisposeGracePeriodMs: 0},
		}
		got := cfg.GetDisposeGracePeriodMs()
		if got != DefaultDisposeGracePeriodMs {
			t.Errorf("got %d, want %d", got, DefaultDisposeGracePeriodMs)
		}
	})

	t.Run("DisposeGracePeriod returns duration", func(t *testing.T) {
		cfg := &Config{
			Sessions: &SessionsConfig{DisposeGracePeriodMs: 15000},
		}
		got := cfg.DisposeGracePeriod()
		want := 15 * time.Second
		if got != want {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestDisposeGracePeriodMs_JSONRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Write config with dispose_grace_period_ms set (include required fields)
	raw := `{
		"workspace_path": "` + tmpDir + `",
		"repos": [],
		"run_targets": [],
		"terminal": { "width": 120, "height": 30, "seed_lines": 1000 },
		"sessions": {
			"dispose_grace_period_ms": 45000
		}
	}`
	if err := os.WriteFile(configPath, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	got := cfg.GetDisposeGracePeriodMs()
	if got != 45000 {
		t.Errorf("GetDisposeGracePeriodMs() = %d, want 45000", got)
	}

	// Save and reload to verify round-trip
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	cfg2, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() after save failed: %v", err)
	}

	got2 := cfg2.GetDisposeGracePeriodMs()
	if got2 != 45000 {
		t.Errorf("After round-trip: GetDisposeGracePeriodMs() = %d, want 45000", got2)
	}
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

func TestConfigVersion_CreateDefault(t *testing.T) {
	cfg := CreateDefault("/tmp/test-config.json")

	if cfg.ConfigVersion != version.Version {
		t.Errorf("ConfigVersion = %q, want %q", cfg.ConfigVersion, version.Version)
	}
}

func TestConfigVersion_LoadWithoutVersion_BackwardsCompatible(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.json")

	// Create a config without config_version (old format)
	oldConfig := `{
		"workspace_path": "/tmp/workspaces",
		"repos": [],
		"run_targets": [],
		"quick_launch": [],
		"terminal": {
			"width": 120,
			"height": 40,
			"seed_lines": 100
		}
	}`

	if err := os.WriteFile(configPath, []byte(oldConfig), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Should load successfully
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// ConfigVersion should be empty (old config)
	if cfg.ConfigVersion != "" {
		t.Errorf("ConfigVersion = %q, want empty (old config)", cfg.ConfigVersion)
	}
}

func TestConfigVersion_SaveUpdatesToCurrentVersion(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.json")

	// Create a config with an old version
	oldConfig := `{
		"config_version": "1.0.0",
		"workspace_path": "/tmp/workspaces",
		"repos": [],
		"run_targets": [],
		"quick_launch": [],
		"terminal": {
			"width": 120,
			"height": 40,
			"seed_lines": 100
		}
	}`

	if err := os.WriteFile(configPath, []byte(oldConfig), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Initially has old version
	if cfg.ConfigVersion != "1.0.0" {
		t.Errorf("ConfigVersion before Save = %q, want 1.0.0", cfg.ConfigVersion)
	}

	// Save should update version
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// Reload to verify
	cfg2, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() after save failed: %v", err)
	}

	// Version should now be current
	if cfg2.ConfigVersion != version.Version {
		t.Errorf("ConfigVersion after Save = %q, want %q", cfg2.ConfigVersion, version.Version)
	}
}

func TestLoad_JSONSyntaxErrorIncludesLineColumn(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "bad-config.json")

	tests := []struct {
		name         string
		json         string
		wantLine     int
		wantCol      int
		wantContains string
	}{
		{
			name: "missing colon on line 3",
			json: `{
  "workspace_path": "/test",
  "network" {
    "port": 7337
  }
}`,
			wantLine:     3,
			wantCol:      13,
			wantContains: "line 3",
		},
		{
			name: "missing comma on line 2",
			json: `{
  "workspace_path": "/test"
  "repos": []
}`,
			wantLine:     3,
			wantCol:      3,
			wantContains: "line 3",
		},
		{
			name: "invalid value on line 4",
			json: `{
  "workspace_path": "/test",
  "repos": [],
  "terminal": invalid
}`,
			wantLine:     4,
			wantCol:      15,
			wantContains: "line 4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(configPath, []byte(tt.json), 0644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}

			_, err := Load(configPath)
			if err == nil {
				t.Fatal("Load() should have failed with invalid JSON")
			}

			errStr := err.Error()
			if !strings.Contains(errStr, tt.wantContains) {
				t.Errorf("error %q should contain %q", errStr, tt.wantContains)
			}

			// Verify it mentions column
			if !strings.Contains(errStr, "column") {
				t.Errorf("error %q should contain 'column'", errStr)
			}
		})
	}
}

func TestLoad_JSONTypeErrorIncludesLineColumn(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "bad-config.json")

	tests := []struct {
		name         string
		json         string
		wantField    string
		wantContains string
	}{
		{
			name: "string instead of bool",
			json: `{
  "workspace_path": "/test",
  "repos": [],
  "run_targets": [],
  "access_control": {
    "enabled": "true"
  }
}`,
			wantField:    "access_control.enabled",
			wantContains: "line",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(configPath, []byte(tt.json), 0644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}

			_, err := Load(configPath)
			if err == nil {
				t.Fatal("Load() should have failed with type error")
			}

			errStr := err.Error()
			if !strings.Contains(errStr, tt.wantField) {
				t.Errorf("error %q should contain field %q", errStr, tt.wantField)
			}
			if !strings.Contains(errStr, tt.wantContains) {
				t.Errorf("error %q should contain %q", errStr, tt.wantContains)
			}
			if !strings.Contains(errStr, "column") {
				t.Errorf("error %q should contain 'column'", errStr)
			}
		})
	}
}

func TestOffsetToLineCol(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		offset   int64
		wantLine int
		wantCol  int
	}{
		{
			name:     "beginning of file",
			data:     "hello\nworld",
			offset:   0,
			wantLine: 1,
			wantCol:  1,
		},
		{
			name:     "middle of first line",
			data:     "hello\nworld",
			offset:   3,
			wantLine: 1,
			wantCol:  4,
		},
		{
			name:     "beginning of second line",
			data:     "hello\nworld",
			offset:   6,
			wantLine: 2,
			wantCol:  1,
		},
		{
			name:     "middle of second line",
			data:     "hello\nworld",
			offset:   8,
			wantLine: 2,
			wantCol:  3,
		},
		{
			name:     "multiline json",
			data:     "{\n  \"key\": \"value\"\n}",
			offset:   15,
			wantLine: 2,
			wantCol:  14,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, col := offsetToLineCol([]byte(tt.data), tt.offset)
			if line != tt.wantLine || col != tt.wantCol {
				t.Errorf("offsetToLineCol(%q, %d) = (%d, %d), want (%d, %d)",
					tt.data, tt.offset, line, col, tt.wantLine, tt.wantCol)
			}
		})
	}
}

func TestConfigVersion_MigrateCalled(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.json")

	// Create a valid config
	validConfig := Config{
		WorkspacePath: tmpDir,
		Repos:         []Repo{},
		RunTargets:    []RunTarget{},
	}

	data, err := json.MarshalIndent(&validConfig, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Load should call Migrate() and not error
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify config loaded correctly
	if cfg.WorkspacePath != tmpDir {
		t.Errorf("WorkspacePath = %q, want %q", cfg.WorkspacePath, tmpDir)
	}
}

func TestMigration_SourceCodeManagerRenamed(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.json")

	// Create an old config with source_code_manager field
	oldConfig := `{
		"workspace_path": "/tmp/workspaces",
		"source_code_manager": "git",
		"repos": [],
		"run_targets": [],
		"terminal": {
			"width": 120,
			"height": 40,
			"seed_lines": 100
		}
	}`

	if err := os.WriteFile(configPath, []byte(oldConfig), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Load should trigger migration
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify the migration copied the value to the new field
	if cfg.SourceCodeManagement != "git" {
		t.Errorf("SourceCodeManagement = %q, want \"git\"", cfg.SourceCodeManagement)
	}

	// GetSourceCodeManagement should return the migrated value
	if got := cfg.GetSourceCodeManagement(); got != "git" {
		t.Errorf("GetSourceCodeManagement() = %q, want \"git\"", got)
	}

	// Reload to verify the migration was saved to disk
	cfg2, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() after migration failed: %v", err)
	}

	// The new field should persist
	if cfg2.SourceCodeManagement != "git" {
		t.Errorf("SourceCodeManagement after reload = %q, want \"git\"", cfg2.SourceCodeManagement)
	}
}

func TestMigration_BothFieldsPresent_NewFieldWins(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.json")

	// Create a config with both old and new fields
	// Migration should NOT overwrite the new field
	bothFieldsConfig := `{
		"workspace_path": "/tmp/workspaces",
		"source_code_manager": "git",
		"source_code_management": "git-worktree",
		"repos": [],
		"run_targets": [],
		"terminal": {
			"width": 120,
			"height": 40,
			"seed_lines": 100
		}
	}`

	if err := os.WriteFile(configPath, []byte(bothFieldsConfig), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Load should not trigger migration (Detect returns false since new field is set)
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// New field value should be preserved (not overwritten by old field)
	if cfg.SourceCodeManagement != "git-worktree" {
		t.Errorf("SourceCodeManagement = %q, want \"git-worktree\" (should not be overwritten)", cfg.SourceCodeManagement)
	}

	// GetSourceCodeManagement should prefer the new field
	if got := cfg.GetSourceCodeManagement(); got != "git-worktree" {
		t.Errorf("GetSourceCodeManagement() = %q, want \"git-worktree\"", got)
	}
}

func TestMigration_NullValueHandledGracefully(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.json")

	// Create a config with null value for old field
	nullConfig := `{
		"workspace_path": "/tmp/workspaces",
		"source_code_manager": null,
		"repos": [],
		"run_targets": [],
		"terminal": {
			"width": 120,
			"height": 40,
			"seed_lines": 100
		}
	}`

	if err := os.WriteFile(configPath, []byte(nullConfig), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Load should not fail - null should be handled gracefully
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed with null value: %v", err)
	}

	// New field should remain empty (null was treated as empty)
	if cfg.SourceCodeManagement != "" {
		t.Errorf("SourceCodeManagement = %q, want empty (null handled as empty)", cfg.SourceCodeManagement)
	}

	// GetSourceCodeManagement should return default
	if got := cfg.GetSourceCodeManagement(); got != "git-worktree" {
		t.Errorf("GetSourceCodeManagement() = %q, want \"git-worktree\" (default)", got)
	}
}

// Note: Non-string values are caught by struct unmarshal before migration runs,
// which provides good error messages to users. The migration's Apply function
// does handle errors gracefully, but struct validation happens first.

func TestMigration_DoesNotRunWhenOnlyNewFieldPresent(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.json")

	// Create a config with only the new field
	newConfig := `{
		"workspace_path": "/tmp/workspaces",
		"source_code_management": "git-worktree",
		"repos": [],
		"run_targets": [],
		"terminal": {
			"width": 120,
			"height": 40,
			"seed_lines": 100
		}
	}`

	if err := os.WriteFile(configPath, []byte(newConfig), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Load should not trigger migration (no old field)
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Value should remain as-is
	if cfg.SourceCodeManagement != "git-worktree" {
		t.Errorf("SourceCodeManagement = %q, want \"git-worktree\"", cfg.SourceCodeManagement)
	}
}

func TestMigration_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.json")

	// Create an old config with source_code_manager field
	oldConfig := `{
		"workspace_path": "/tmp/workspaces",
		"source_code_manager": "git",
		"repos": [],
		"run_targets": [],
		"terminal": {
			"width": 120,
			"height": 40,
			"seed_lines": 100
		}
	}`

	if err := os.WriteFile(configPath, []byte(oldConfig), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// First load - should migrate
	cfg1, err := Load(configPath)
	if err != nil {
		t.Fatalf("First Load() failed: %v", err)
	}
	if cfg1.SourceCodeManagement != "git" {
		t.Errorf("After first load, SourceCodeManagement = %q, want \"git\"", cfg1.SourceCodeManagement)
	}

	// Second load - should not re-migrate (detect returns false)
	cfg2, err := Load(configPath)
	if err != nil {
		t.Fatalf("Second Load() failed: %v", err)
	}
	if cfg2.SourceCodeManagement != "git" {
		t.Errorf("After second load, SourceCodeManagement = %q, want \"git\"", cfg2.SourceCodeManagement)
	}
}

func TestGetSourceCodeManagement(t *testing.T) {
	tests := []struct {
		name  string
		field string
		want  string
	}{
		{
			name:  "field set to git",
			field: "git",
			want:  "git",
		},
		{
			name:  "field set to git-worktree",
			field: "git-worktree",
			want:  "git-worktree",
		},
		{
			name:  "field empty - returns default",
			field: "",
			want:  "git-worktree",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				SourceCodeManagement: tt.field,
			}
			got := cfg.GetSourceCodeManagement()
			if got != tt.want {
				t.Errorf("GetSourceCodeManagement() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRemoteFlavorCRUD(t *testing.T) {
	t.Run("AddRemoteFlavor generates ID from flavor string", func(t *testing.T) {
		cfg := &Config{}
		err := cfg.AddRemoteFlavor(RemoteFlavor{
			Flavor:        "gpu:ml-large",
			DisplayName:   "GPU ML Large",
			WorkspacePath: "~/workspace",
		})
		if err != nil {
			t.Fatalf("AddRemoteFlavor failed: %v", err)
		}

		if len(cfg.RemoteFlavors) != 1 {
			t.Fatalf("expected 1 flavor, got %d", len(cfg.RemoteFlavors))
		}

		// ID should be generated from flavor string
		if cfg.RemoteFlavors[0].ID != "gpu_ml_large" {
			t.Errorf("ID = %q, want %q", cfg.RemoteFlavors[0].ID, "gpu_ml_large")
		}

		// VCS should default to git
		if cfg.RemoteFlavors[0].VCS != "git" {
			t.Errorf("VCS = %q, want %q", cfg.RemoteFlavors[0].VCS, "git")
		}
	})

	t.Run("AddRemoteFlavor validates required fields", func(t *testing.T) {
		cfg := &Config{}

		// Missing flavor
		err := cfg.AddRemoteFlavor(RemoteFlavor{
			DisplayName:   "Test",
			WorkspacePath: "~/test",
		})
		if err == nil {
			t.Error("expected error for missing flavor")
		}

		// Missing display name
		err = cfg.AddRemoteFlavor(RemoteFlavor{
			Flavor:        "test:flavor",
			WorkspacePath: "~/test",
		})
		if err == nil {
			t.Error("expected error for missing display_name")
		}

		// Missing workspace path
		err = cfg.AddRemoteFlavor(RemoteFlavor{
			Flavor:      "test:flavor",
			DisplayName: "Test",
		})
		if err == nil {
			t.Error("expected error for missing workspace_path")
		}
	})

	t.Run("AddRemoteFlavor rejects invalid VCS", func(t *testing.T) {
		cfg := &Config{}
		err := cfg.AddRemoteFlavor(RemoteFlavor{
			Flavor:        "test:flavor",
			DisplayName:   "Test",
			WorkspacePath: "~/test",
			VCS:           "mercurial",
		})
		if err == nil {
			t.Error("expected error for invalid VCS")
		}
	})

	t.Run("AddRemoteFlavor rejects duplicate ID", func(t *testing.T) {
		cfg := &Config{}
		rf := RemoteFlavor{
			Flavor:        "test:flavor",
			DisplayName:   "Test",
			WorkspacePath: "~/test",
		}

		if err := cfg.AddRemoteFlavor(rf); err != nil {
			t.Fatalf("first add failed: %v", err)
		}

		err := cfg.AddRemoteFlavor(rf)
		if err == nil {
			t.Error("expected error for duplicate ID")
		}
	})

	t.Run("GetRemoteFlavor returns flavor by ID", func(t *testing.T) {
		cfg := &Config{
			RemoteFlavors: []RemoteFlavor{
				{ID: "flavor1", Flavor: "test:1", DisplayName: "Test 1", WorkspacePath: "~/1"},
				{ID: "flavor2", Flavor: "test:2", DisplayName: "Test 2", WorkspacePath: "~/2"},
			},
		}

		rf, found := cfg.GetRemoteFlavor("flavor2")
		if !found {
			t.Fatal("flavor2 not found")
		}
		if rf.DisplayName != "Test 2" {
			t.Errorf("DisplayName = %q, want %q", rf.DisplayName, "Test 2")
		}

		_, found = cfg.GetRemoteFlavor("nonexistent")
		if found {
			t.Error("expected nonexistent to not be found")
		}
	})

	t.Run("UpdateRemoteFlavor modifies existing flavor", func(t *testing.T) {
		cfg := &Config{
			RemoteFlavors: []RemoteFlavor{
				{ID: "flavor1", Flavor: "test:1", DisplayName: "Test 1", WorkspacePath: "~/1", VCS: "git"},
			},
		}

		err := cfg.UpdateRemoteFlavor(RemoteFlavor{
			ID:            "flavor1",
			Flavor:        "test:1-updated",
			DisplayName:   "Test 1 Updated",
			WorkspacePath: "~/1-updated",
			VCS:           "sapling",
		})
		if err != nil {
			t.Fatalf("UpdateRemoteFlavor failed: %v", err)
		}

		rf, _ := cfg.GetRemoteFlavor("flavor1")
		if rf.DisplayName != "Test 1 Updated" {
			t.Errorf("DisplayName = %q, want %q", rf.DisplayName, "Test 1 Updated")
		}
		if rf.VCS != "sapling" {
			t.Errorf("VCS = %q, want %q", rf.VCS, "sapling")
		}
	})

	t.Run("UpdateRemoteFlavor fails for nonexistent ID", func(t *testing.T) {
		cfg := &Config{}
		err := cfg.UpdateRemoteFlavor(RemoteFlavor{
			ID:            "nonexistent",
			Flavor:        "test",
			DisplayName:   "Test",
			WorkspacePath: "~/test",
		})
		if err == nil {
			t.Error("expected error for nonexistent ID")
		}
	})

	t.Run("RemoveRemoteFlavor removes flavor", func(t *testing.T) {
		cfg := &Config{
			RemoteFlavors: []RemoteFlavor{
				{ID: "flavor1", Flavor: "test:1", DisplayName: "Test 1", WorkspacePath: "~/1"},
				{ID: "flavor2", Flavor: "test:2", DisplayName: "Test 2", WorkspacePath: "~/2"},
			},
		}

		if err := cfg.RemoveRemoteFlavor("flavor1"); err != nil {
			t.Fatalf("RemoveRemoteFlavor failed: %v", err)
		}

		if len(cfg.RemoteFlavors) != 1 {
			t.Fatalf("expected 1 flavor, got %d", len(cfg.RemoteFlavors))
		}
		if cfg.RemoteFlavors[0].ID != "flavor2" {
			t.Errorf("remaining flavor ID = %q, want %q", cfg.RemoteFlavors[0].ID, "flavor2")
		}
	})

	t.Run("RemoveRemoteFlavor fails for nonexistent ID", func(t *testing.T) {
		cfg := &Config{}
		err := cfg.RemoveRemoteFlavor("nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent ID")
		}
	})
}

func TestGenerateRemoteFlavorID(t *testing.T) {
	tests := []struct {
		flavor string
		want   string
	}{
		{"simple", "simple"},
		{"docker:devenv", "docker_devenv"},
		{"gpu:ml-large", "gpu_ml_large"},
		{"Test:With:Multiple:Colons", "test_with_multiple_colons"},
		{"spaces are replaced", "spaces_are_replaced"},
	}

	for _, tt := range tests {
		t.Run(tt.flavor, func(t *testing.T) {
			got := GenerateRemoteFlavorID(tt.flavor)
			if got != tt.want {
				t.Errorf("GenerateRemoteFlavorID(%q) = %q, want %q", tt.flavor, got, tt.want)
			}
		})
	}
}

// TestRemoteFlavorTemplateValidation tests that invalid template syntax is caught
// at config load time (Issue 5 fix).
func TestRemoteFlavorTemplateValidation(t *testing.T) {
	tests := []struct {
		name      string
		flavor    RemoteFlavor
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid connect template",
			flavor: RemoteFlavor{
				Flavor:         "test",
				DisplayName:    "Test",
				WorkspacePath:  "/workspace",
				ConnectCommand: "ssh {{.Flavor}}",
			},
			wantError: false,
		},
		{
			name: "valid reconnect template",
			flavor: RemoteFlavor{
				Flavor:           "test",
				DisplayName:      "Test",
				WorkspacePath:    "/workspace",
				ReconnectCommand: "ssh {{.Hostname}}",
			},
			wantError: false,
		},
		{
			name: "valid provision template",
			flavor: RemoteFlavor{
				Flavor:           "test",
				DisplayName:      "Test",
				WorkspacePath:    "/workspace",
				ProvisionCommand: "cd {{.WorkspacePath}} && git clone {{.Repo}}",
			},
			wantError: false,
		},
		{
			name: "invalid connect template syntax",
			flavor: RemoteFlavor{
				Flavor:         "test",
				DisplayName:    "Test",
				WorkspacePath:  "/workspace",
				ConnectCommand: "ssh {{.Flavor",
			},
			wantError: true,
			errorMsg:  "connect_command has invalid template syntax",
		},
		{
			name: "invalid reconnect template - undefined variable",
			flavor: RemoteFlavor{
				Flavor:           "test",
				DisplayName:      "Test",
				WorkspacePath:    "/workspace",
				ReconnectCommand: "ssh {{.UndefinedVar}}",
			},
			wantError: true,
			errorMsg:  "reconnect_command template execution failed",
		},
		{
			name: "invalid provision template - unclosed action",
			flavor: RemoteFlavor{
				Flavor:           "test",
				DisplayName:      "Test",
				WorkspacePath:    "/workspace",
				ProvisionCommand: "git clone {{.Repo",
			},
			wantError: true,
			errorMsg:  "provision_command has invalid template syntax",
		},
		{
			name: "empty templates are valid",
			flavor: RemoteFlavor{
				Flavor:        "test",
				DisplayName:   "Test",
				WorkspacePath: "/workspace",
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRemoteFlavor(tt.flavor)
			if tt.wantError {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errorMsg)
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestValidateCommandTemplate tests the template validation helper
func TestValidateCommandTemplate(t *testing.T) {
	tests := []struct {
		name      string
		tmplStr   string
		fieldName string
		testData  map[string]string
		wantError bool
	}{
		{
			name:      "valid simple template",
			tmplStr:   "echo {{.Value}}",
			fieldName: "test_field",
			testData:  map[string]string{"Value": "hello"},
			wantError: false,
		},
		{
			name:      "valid complex template",
			tmplStr:   "ssh {{.User}}@{{.Host}} -p {{.Port}}",
			fieldName: "connect",
			testData:  map[string]string{"User": "root", "Host": "example.com", "Port": "22"},
			wantError: false,
		},
		{
			name:      "invalid syntax - unclosed action",
			tmplStr:   "echo {{.Value",
			fieldName: "test_field",
			testData:  map[string]string{"Value": "hello"},
			wantError: true,
		},
		{
			name:      "undefined variable",
			tmplStr:   "echo {{.Missing}}",
			fieldName: "test_field",
			testData:  map[string]string{"Value": "hello"},
			wantError: true,
		},
		{
			name:      "empty template is valid",
			tmplStr:   "",
			fieldName: "test_field",
			testData:  map[string]string{},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCommandTemplate(tt.tmplStr, tt.fieldName, tt.testData)
			if tt.wantError && err == nil {
				t.Errorf("expected error, got nil")
			} else if !tt.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestExtractRepoNameFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"git@github.com:user/myrepo.git", "myrepo"},
		{"git@github.com:user/myrepo", "myrepo"},
		{"https://github.com/user/myrepo.git", "myrepo"},
		{"https://github.com/user/myrepo", "myrepo"},
		{"https://gitlab.com/org/subgroup/project.git", "project"},
		{"file:///tmp/test-repo", "test-repo"},
		{"repo.git", "repo"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := extractRepoNameFromURL(tt.url)
			if got != tt.want {
				t.Errorf("extractRepoNameFromURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		url       string
		wantOwner string
		wantRepo  string
	}{
		{"git@github.com:facebook/react.git", "facebook", "react"},
		{"git@github.com:myfork/schmux", "myfork", "schmux"},
		{"https://github.com/user/myrepo.git", "user", "myrepo"},
		{"https://github.com/user/myrepo", "user", "myrepo"},
		{"https://gitlab.com/org/project.git", "", ""},
		{"file:///tmp/test-repo", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			owner, repo := parseGitHubURL(tt.url)
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("parseGitHubURL(%q) = (%q, %q), want (%q, %q)", tt.url, owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

func TestDetectExistingBarePath_NewRepo(t *testing.T) {
	// For a new repo with nothing on disk, should return {name}.git
	tmpDir := t.TempDir()

	got := detectExistingBarePath([]string{tmpDir}, "https://github.com/user/myrepo.git", "myrepo")
	want := "myrepo.git"
	if got != want {
		t.Errorf("detectExistingBarePath() = %q, want %q", got, want)
	}
}

func TestDetectExistingBarePath_NewRepoWithNamespace(t *testing.T) {
	// For a new GitHub repo with nothing on disk, should still return {name}.git (not owner/repo.git)
	tmpDir := t.TempDir()

	got := detectExistingBarePath([]string{tmpDir}, "https://github.com/facebook/react.git", "react")
	want := "react.git"
	if got != want {
		t.Errorf("detectExistingBarePath() = %q, want %q", got, want)
	}
}

func TestPopulateBarePaths_NewRepo(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := CreateDefault(configPath)
	cfg.Repos = []Repo{
		{Name: "myrepo", URL: "https://github.com/user/myrepo.git"},
	}

	// populateBarePaths should set bare_path for new repos
	cfg.populateBarePaths()

	if cfg.Repos[0].BarePath != "myrepo.git" {
		t.Errorf("BarePath = %q, want %q", cfg.Repos[0].BarePath, "myrepo.git")
	}
}

func TestPopulateBarePaths_CorrectsMismatch(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Create a bare repo at the namespaced path (owner/repo.git)
	repoURL := "https://github.com/myowner/myrepo.git"
	reposDir := filepath.Join(tmpDir, "repos")
	namespacedDir := filepath.Join(reposDir, "myowner", "myrepo.git")
	if err := os.MkdirAll(namespacedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Init a bare repo and set origin URL
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = namespacedDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	runGit("init", "--bare")
	runGit("remote", "add", "origin", repoURL)

	cfg := CreateDefault(configPath)
	cfg.WorktreeBasePath = reposDir
	cfg.Repos = []Repo{
		// Config has flat "myrepo.git" but repo is at "myowner/myrepo.git"
		{Name: "myrepo", URL: repoURL, BarePath: "myrepo.git"},
	}

	cfg.populateBarePaths()

	want := filepath.Join("myowner", "myrepo.git")
	if cfg.Repos[0].BarePath != want {
		t.Errorf("BarePath = %q, want %q (should be corrected to match disk)", cfg.Repos[0].BarePath, want)
	}
}

func TestGetOverlayPaths_DefaultsOnly(t *testing.T) {
	cfg := &Config{}
	paths := cfg.GetOverlayPaths("myrepo")
	// Should include hardcoded defaults
	if len(paths) < 1 {
		t.Fatalf("expected at least 1 default path, got %d", len(paths))
	}
	found := make(map[string]bool)
	for _, p := range paths {
		found[p] = true
	}
	if !found[".claude/settings.local.json"] {
		t.Error("missing .claude/settings.local.json from defaults")
	}
}

func TestGetOverlayPaths_WithGlobalAndRepoConfig(t *testing.T) {
	cfg := &Config{
		Overlay: &OverlayConfig{
			Paths: []string{".tool-versions"},
		},
		Repos: []Repo{
			{Name: "myrepo", URL: "git@github.com:org/myrepo.git", OverlayPaths: []string{".env"}},
		},
	}
	paths := cfg.GetOverlayPaths("myrepo")
	found := make(map[string]bool)
	for _, p := range paths {
		found[p] = true
	}
	if !found[".claude/settings.local.json"] {
		t.Error("missing hardcoded default")
	}
	if !found[".tool-versions"] {
		t.Error("missing global config path")
	}
	if !found[".env"] {
		t.Error("missing repo-specific path")
	}
}

func TestGetOverlayPaths_Deduplication(t *testing.T) {
	cfg := &Config{
		Overlay: &OverlayConfig{
			Paths: []string{".claude/settings.local.json"}, // duplicate of default
		},
		Repos: []Repo{
			{Name: "myrepo", URL: "url", OverlayPaths: []string{".claude/settings.local.json"}},
		},
	}
	paths := cfg.GetOverlayPaths("myrepo")
	count := 0
	for _, p := range paths {
		if p == ".claude/settings.local.json" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 occurrence of .claude/settings.json, got %d", count)
	}
}

func TestGetLoreEnabled_Default(t *testing.T) {
	c := &Config{}
	if !c.GetLoreEnabled() {
		t.Error("expected lore enabled by default")
	}
}

func TestGetLoreEnabled_Explicit(t *testing.T) {
	enabled := false
	c := &Config{Lore: &LoreConfig{Enabled: &enabled}}
	if c.GetLoreEnabled() {
		t.Error("expected lore disabled when explicitly set to false")
	}
}

func TestGetLoreTarget_FallsBackToCompound(t *testing.T) {
	c := &Config{Compound: &CompoundConfig{Target: "claude-haiku"}}
	if got := c.GetLoreTarget(); got != "claude-haiku" {
		t.Errorf("expected fallback to compound target, got %q", got)
	}
}

func TestGetLoreTarget_OwnTarget(t *testing.T) {
	c := &Config{
		Compound: &CompoundConfig{Target: "claude-haiku"},
		Lore:     &LoreConfig{Target: "claude-sonnet"},
	}
	if got := c.GetLoreTarget(); got != "claude-sonnet" {
		t.Errorf("expected lore-specific target, got %q", got)
	}
}

func TestGetLoreInstructionFiles_Defaults(t *testing.T) {
	c := &Config{}
	files := c.GetLoreInstructionFiles()
	expected := []string{"CLAUDE.md", "AGENTS.md", ".cursorrules", ".github/copilot-instructions.md", "CONVENTIONS.md"}
	if len(files) != len(expected) {
		t.Fatalf("expected %d files, got %d", len(expected), len(files))
	}
	for i, f := range expected {
		if files[i] != f {
			t.Errorf("expected files[%d]=%q, got %q", i, f, files[i])
		}
	}
}

func TestGetLoreCurateDebounceMs_Default(t *testing.T) {
	c := &Config{}
	if got := c.GetLoreCurateDebounceMs(); got != 30000 {
		t.Errorf("expected 30000, got %d", got)
	}
}

func TestGetLoreAutoPR_Default(t *testing.T) {
	c := &Config{}
	if c.GetLoreAutoPR() {
		t.Error("expected auto_pr false by default")
	}
}

func TestGetLoreCurateOnDispose(t *testing.T) {
	t.Run("default is session", func(t *testing.T) {
		c := &Config{}
		if got := c.GetLoreCurateOnDispose(); got != "session" {
			t.Errorf("expected %q, got %q", "session", got)
		}
	})

	t.Run("nil lore config defaults to session", func(t *testing.T) {
		c := &Config{Lore: nil}
		if got := c.GetLoreCurateOnDispose(); got != "session" {
			t.Errorf("expected %q, got %q", "session", got)
		}
	})

	t.Run("string value session", func(t *testing.T) {
		c := &Config{Lore: &LoreConfig{CurateOnDispose: "session"}}
		if got := c.GetLoreCurateOnDispose(); got != "session" {
			t.Errorf("expected %q, got %q", "session", got)
		}
	})

	t.Run("string value workspace", func(t *testing.T) {
		c := &Config{Lore: &LoreConfig{CurateOnDispose: "workspace"}}
		if got := c.GetLoreCurateOnDispose(); got != "workspace" {
			t.Errorf("expected %q, got %q", "workspace", got)
		}
	})

	t.Run("string value never", func(t *testing.T) {
		c := &Config{Lore: &LoreConfig{CurateOnDispose: "never"}}
		if got := c.GetLoreCurateOnDispose(); got != "never" {
			t.Errorf("expected %q, got %q", "never", got)
		}
	})

	t.Run("invalid string defaults to session", func(t *testing.T) {
		c := &Config{Lore: &LoreConfig{CurateOnDispose: "bogus"}}
		if got := c.GetLoreCurateOnDispose(); got != "session" {
			t.Errorf("expected %q, got %q", "session", got)
		}
	})

	t.Run("backward compat bool true becomes session", func(t *testing.T) {
		c := &Config{Lore: &LoreConfig{
			curateOnDisposeRaw: json.RawMessage("true"),
		}}
		if got := c.GetLoreCurateOnDispose(); got != "session" {
			t.Errorf("expected %q, got %q", "session", got)
		}
	})

	t.Run("backward compat bool false becomes never", func(t *testing.T) {
		c := &Config{Lore: &LoreConfig{
			curateOnDisposeRaw: json.RawMessage("false"),
		}}
		if got := c.GetLoreCurateOnDispose(); got != "never" {
			t.Errorf("expected %q, got %q", "never", got)
		}
	})
}

func TestLoreConfig_UnmarshalJSON_BackwardCompat(t *testing.T) {
	t.Run("bool true in JSON", func(t *testing.T) {
		input := `{"curate_on_dispose": true, "llm_target": "claude"}`
		var lc LoreConfig
		if err := json.Unmarshal([]byte(input), &lc); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if lc.Target != "claude" {
			t.Errorf("expected target %q, got %q", "claude", lc.Target)
		}
		// Build config to check getter
		c := &Config{Lore: &lc}
		if got := c.GetLoreCurateOnDispose(); got != "session" {
			t.Errorf("expected %q, got %q", "session", got)
		}
	})

	t.Run("bool false in JSON", func(t *testing.T) {
		input := `{"curate_on_dispose": false}`
		var lc LoreConfig
		if err := json.Unmarshal([]byte(input), &lc); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		c := &Config{Lore: &lc}
		if got := c.GetLoreCurateOnDispose(); got != "never" {
			t.Errorf("expected %q, got %q", "never", got)
		}
	})

	t.Run("string value in JSON", func(t *testing.T) {
		input := `{"curate_on_dispose": "workspace"}`
		var lc LoreConfig
		if err := json.Unmarshal([]byte(input), &lc); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		c := &Config{Lore: &lc}
		if got := c.GetLoreCurateOnDispose(); got != "workspace" {
			t.Errorf("expected %q, got %q", "workspace", got)
		}
	})

	t.Run("absent field defaults to session", func(t *testing.T) {
		input := `{"llm_target": "claude"}`
		var lc LoreConfig
		if err := json.Unmarshal([]byte(input), &lc); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		c := &Config{Lore: &lc}
		if got := c.GetLoreCurateOnDispose(); got != "session" {
			t.Errorf("expected %q, got %q", "session", got)
		}
	})
}

func TestDefaultOverlayPaths_IncludesSettingsLocal(t *testing.T) {
	found := false
	for _, p := range DefaultOverlayPaths {
		if p == ".claude/settings.local.json" {
			found = true
			break
		}
	}
	if !found {
		t.Error("DefaultOverlayPaths should include .claude/settings.local.json")
	}
}

func TestDefaultOverlayPaths_ExcludesLoreJsonl(t *testing.T) {
	for _, p := range DefaultOverlayPaths {
		if p == ".schmux/lore.jsonl" {
			t.Error("DefaultOverlayPaths should NOT include .schmux/lore.jsonl (lore is one-directional, not overlay-synced)")
		}
	}
}

func TestGetRemoteAccessEnabled(t *testing.T) {
	t.Run("defaults to false when nil", func(t *testing.T) {
		cfg := &Config{}
		if cfg.GetRemoteAccessEnabled() {
			t.Error("expected GetRemoteAccessEnabled() = false when RemoteAccess is nil")
		}
	})

	t.Run("returns true when explicitly enabled", func(t *testing.T) {
		enabled := true
		cfg := &Config{RemoteAccess: &RemoteAccessConfig{Enabled: &enabled}}
		if !cfg.GetRemoteAccessEnabled() {
			t.Error("expected GetRemoteAccessEnabled() = true")
		}
	})

	t.Run("returns false when explicitly disabled", func(t *testing.T) {
		enabled := false
		cfg := &Config{RemoteAccess: &RemoteAccessConfig{Enabled: &enabled}}
		if cfg.GetRemoteAccessEnabled() {
			t.Error("expected GetRemoteAccessEnabled() = false when explicitly set to false")
		}
	})

	t.Run("backward compat: disabled=true means enabled=false", func(t *testing.T) {
		disabled := true
		cfg := &Config{RemoteAccess: &RemoteAccessConfig{Disabled: &disabled}}
		if cfg.GetRemoteAccessEnabled() {
			t.Error("expected GetRemoteAccessEnabled() = false when Disabled=true (backward compat)")
		}
	})

	t.Run("backward compat: disabled=false means enabled=true", func(t *testing.T) {
		disabled := false
		cfg := &Config{RemoteAccess: &RemoteAccessConfig{Disabled: &disabled}}
		if !cfg.GetRemoteAccessEnabled() {
			t.Error("expected GetRemoteAccessEnabled() = true when Disabled=false (backward compat)")
		}
	})

	t.Run("enabled takes precedence over disabled", func(t *testing.T) {
		enabled := true
		disabled := true
		cfg := &Config{RemoteAccess: &RemoteAccessConfig{Enabled: &enabled, Disabled: &disabled}}
		if !cfg.GetRemoteAccessEnabled() {
			t.Error("expected Enabled to take precedence over Disabled")
		}
	})
}

func TestGetRemoteAccessTimeoutMinutes(t *testing.T) {
	t.Run("defaults to 120 when nil", func(t *testing.T) {
		cfg := &Config{}
		if cfg.GetRemoteAccessTimeoutMinutes() != 120 {
			t.Errorf("expected 120, got %d", cfg.GetRemoteAccessTimeoutMinutes())
		}
	})

	t.Run("defaults to 120 when zero", func(t *testing.T) {
		cfg := &Config{RemoteAccess: &RemoteAccessConfig{TimeoutMinutes: 0}}
		if cfg.GetRemoteAccessTimeoutMinutes() != 120 {
			t.Errorf("expected 120, got %d", cfg.GetRemoteAccessTimeoutMinutes())
		}
	})

	t.Run("negative disables timeout", func(t *testing.T) {
		cfg := &Config{RemoteAccess: &RemoteAccessConfig{TimeoutMinutes: -1}}
		if cfg.GetRemoteAccessTimeoutMinutes() != 0 {
			t.Errorf("expected 0, got %d", cfg.GetRemoteAccessTimeoutMinutes())
		}
	})

	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{RemoteAccess: &RemoteAccessConfig{TimeoutMinutes: 480}}
		if cfg.GetRemoteAccessTimeoutMinutes() != 480 {
			t.Errorf("expected 480, got %d", cfg.GetRemoteAccessTimeoutMinutes())
		}
	})
}

func TestGetRemoteAccessNtfyTopic(t *testing.T) {
	t.Run("defaults to empty when nil", func(t *testing.T) {
		cfg := &Config{}
		if cfg.GetRemoteAccessNtfyTopic() != "" {
			t.Errorf("expected empty, got %q", cfg.GetRemoteAccessNtfyTopic())
		}
	})

	t.Run("returns trimmed value", func(t *testing.T) {
		cfg := &Config{RemoteAccess: &RemoteAccessConfig{
			Notify: &RemoteAccessNotifyConfig{NtfyTopic: "  my-topic  "},
		}}
		if cfg.GetRemoteAccessNtfyTopic() != "my-topic" {
			t.Errorf("expected 'my-topic', got %q", cfg.GetRemoteAccessNtfyTopic())
		}
	})
}

func TestGetRemoteAccessNotifyCommand(t *testing.T) {
	t.Run("defaults to empty when nil", func(t *testing.T) {
		cfg := &Config{}
		if cfg.GetRemoteAccessNotifyCommand() != "" {
			t.Errorf("expected empty, got %q", cfg.GetRemoteAccessNotifyCommand())
		}
	})

	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{RemoteAccess: &RemoteAccessConfig{
			Notify: &RemoteAccessNotifyConfig{Command: "echo $SCHMUX_REMOTE_URL"},
		}}
		if cfg.GetRemoteAccessNotifyCommand() != "echo $SCHMUX_REMOTE_URL" {
			t.Errorf("unexpected value: %q", cfg.GetRemoteAccessNotifyCommand())
		}
	})
}

func TestPopulateBarePaths_AlreadySet(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := CreateDefault(configPath)
	cfg.Repos = []Repo{
		{Name: "myrepo", URL: "https://github.com/user/myrepo.git", BarePath: "custom/path.git"},
	}

	// populateBarePaths should NOT overwrite existing bare_path
	cfg.populateBarePaths()

	if cfg.Repos[0].BarePath != "custom/path.git" {
		t.Errorf("BarePath = %q, want %q (should not be overwritten)", cfg.Repos[0].BarePath, "custom/path.git")
	}
}

func TestValidate_NegativeCases(t *testing.T) {
	prompt := "do something"

	tests := []struct {
		name         string
		cfg          *Config
		wantContains string
	}{
		// validateRunTargets errors
		{
			name: "empty run target name",
			cfg: &Config{
				RunTargets: []RunTarget{
					{Name: "", Type: RunTargetTypePromptable, Command: "echo hi"},
				},
			},
			wantContains: "name is required",
		},
		{
			name: "missing command",
			cfg: &Config{
				RunTargets: []RunTarget{
					{Name: "my-agent", Type: RunTargetTypePromptable, Command: ""},
				},
			},
			wantContains: "command is required",
		},
		{
			name: "invalid type",
			cfg: &Config{
				RunTargets: []RunTarget{
					{Name: "my-agent", Type: "bogus", Command: "echo hi"},
				},
			},
			wantContains: "invalid type",
		},
		{
			name: "duplicate target names",
			cfg: &Config{
				RunTargets: []RunTarget{
					{Name: "agent", Type: RunTargetTypePromptable, Command: "echo a"},
					{Name: "agent", Type: RunTargetTypeCommand, Command: "echo b"},
				},
			},
			wantContains: "duplicate run target name",
		},
		// validateQuickLaunch errors
		{
			name: "empty quick launch name",
			cfg: &Config{
				RunTargets: []RunTarget{
					{Name: "agent", Type: RunTargetTypePromptable, Command: "echo hi"},
				},
				QuickLaunch: []QuickLaunch{
					{Name: "", Target: "agent", Prompt: &prompt},
				},
			},
			wantContains: "name is required",
		},
		{
			name: "duplicate quick launch names",
			cfg: &Config{
				RunTargets: []RunTarget{
					{Name: "agent", Type: RunTargetTypePromptable, Command: "echo hi"},
				},
				QuickLaunch: []QuickLaunch{
					{Name: "preset", Target: "agent", Prompt: &prompt},
					{Name: "preset", Target: "agent", Prompt: &prompt},
				},
			},
			wantContains: "duplicate quick launch name",
		},
		{
			name: "empty target in quick launch",
			cfg: &Config{
				RunTargets: []RunTarget{
					{Name: "agent", Type: RunTargetTypePromptable, Command: "echo hi"},
				},
				QuickLaunch: []QuickLaunch{
					{Name: "preset", Target: "", Prompt: &prompt},
				},
			},
			wantContains: "target is required",
		},
		{
			name: "target not found in quick launch",
			cfg: &Config{
				RunTargets: []RunTarget{
					{Name: "agent", Type: RunTargetTypePromptable, Command: "echo hi"},
				},
				QuickLaunch: []QuickLaunch{
					{Name: "preset", Target: "nonexistent", Prompt: &prompt},
				},
			},
			wantContains: "target not found",
		},
		{
			name: "promptable target without prompt in quick launch",
			cfg: &Config{
				RunTargets: []RunTarget{
					{Name: "agent", Type: RunTargetTypePromptable, Command: "echo hi"},
				},
				QuickLaunch: []QuickLaunch{
					{Name: "preset", Target: "agent"},
				},
			},
			wantContains: "requires prompt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantContains)
			}
			if !strings.Contains(err.Error(), tt.wantContains) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantContains)
			}
		})
	}
}

func TestGetDashboardSXEnabled(t *testing.T) {
	t.Run("nil network", func(t *testing.T) {
		cfg := &Config{}
		if cfg.GetDashboardSXEnabled() {
			t.Error("expected false for nil Network")
		}
	})

	t.Run("nil dashboardsx", func(t *testing.T) {
		cfg := &Config{Network: &NetworkConfig{}}
		if cfg.GetDashboardSXEnabled() {
			t.Error("expected false for nil DashboardSX")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		cfg := &Config{Network: &NetworkConfig{
			DashboardSX: &DashboardSXConfig{Enabled: false},
		}}
		if cfg.GetDashboardSXEnabled() {
			t.Error("expected false for disabled")
		}
	})

	t.Run("enabled", func(t *testing.T) {
		cfg := &Config{Network: &NetworkConfig{
			DashboardSX: &DashboardSXConfig{Enabled: true},
		}}
		if !cfg.GetDashboardSXEnabled() {
			t.Error("expected true for enabled")
		}
	})
}

func TestGetDashboardSXCode(t *testing.T) {
	cfg := &Config{Network: &NetworkConfig{
		DashboardSX: &DashboardSXConfig{Code: "12345"},
	}}
	if got := cfg.GetDashboardSXCode(); got != "12345" {
		t.Errorf("GetDashboardSXCode() = %q, want %q", got, "12345")
	}
}

func TestGetDashboardSXHostname(t *testing.T) {
	t.Run("with code", func(t *testing.T) {
		cfg := &Config{Network: &NetworkConfig{
			DashboardSX: &DashboardSXConfig{Code: "12345"},
		}}
		if got := cfg.GetDashboardSXHostname(); got != "12345.dashboard.sx" {
			t.Errorf("GetDashboardSXHostname() = %q, want %q", got, "12345.dashboard.sx")
		}
	})

	t.Run("empty code", func(t *testing.T) {
		cfg := &Config{}
		if got := cfg.GetDashboardSXHostname(); got != "" {
			t.Errorf("GetDashboardSXHostname() = %q, want empty", got)
		}
	})
}

func TestDashboardSXConfig_JSONRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := CreateDefault(configPath)
	cfg.Network = &NetworkConfig{
		DashboardSX: &DashboardSXConfig{
			Enabled: true,
			Code:    "12345",
			Email:   "test@example.com",
			IP:      "192.168.1.100",
		},
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !loaded.GetDashboardSXEnabled() {
		t.Error("DashboardSX.Enabled should be true after roundtrip")
	}
	if got := loaded.GetDashboardSXCode(); got != "12345" {
		t.Errorf("DashboardSX.Code = %q, want %q", got, "12345")
	}
	if got := loaded.GetDashboardSXEmail(); got != "test@example.com" {
		t.Errorf("DashboardSX.Email = %q, want %q", got, "test@example.com")
	}
	if got := loaded.GetDashboardSXIP(); got != "192.168.1.100" {
		t.Errorf("DashboardSX.IP = %q, want %q", got, "192.168.1.100")
	}
}

func TestGetDashboardSXEmail(t *testing.T) {
	t.Run("nil network", func(t *testing.T) {
		cfg := &Config{}
		if got := cfg.GetDashboardSXEmail(); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("nil dashboardsx", func(t *testing.T) {
		cfg := &Config{Network: &NetworkConfig{}}
		if got := cfg.GetDashboardSXEmail(); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("with email", func(t *testing.T) {
		cfg := &Config{Network: &NetworkConfig{
			DashboardSX: &DashboardSXConfig{Email: "user@example.com"},
		}}
		if got := cfg.GetDashboardSXEmail(); got != "user@example.com" {
			t.Errorf("GetDashboardSXEmail() = %q, want %q", got, "user@example.com")
		}
	})
}

func TestResolveBareRepoDir_Found(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	reposDir := filepath.Join(tmpDir, "repos")
	os.MkdirAll(filepath.Join(reposDir, "myrepo.git"), 0755)

	cfg := CreateDefault(configPath)
	cfg.WorktreeBasePath = reposDir

	got := cfg.ResolveBareRepoDir("myrepo.git")
	want := filepath.Join(reposDir, "myrepo.git")
	if got != want {
		t.Errorf("ResolveBareRepoDir() = %q, want %q", got, want)
	}
}

func TestResolveBareRepoDir_FallbackWhenMissing(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	reposDir := filepath.Join(tmpDir, "repos")
	os.MkdirAll(reposDir, 0755)

	cfg := CreateDefault(configPath)
	cfg.WorktreeBasePath = reposDir

	// Repo doesn't exist anywhere — should fall back to repos dir
	got := cfg.ResolveBareRepoDir("myrepo.git")
	want := filepath.Join(reposDir, "myrepo.git")
	if got != want {
		t.Errorf("ResolveBareRepoDir() = %q, want %q", got, want)
	}
}

func TestFindRepoByURL(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Repos: []Repo{
			{Name: "project-a", URL: "git@github.com:user/project-a.git"},
			{Name: "project-b", URL: "https://github.com/user/project-b.git"},
		},
	}

	t.Run("finds repo by SSH URL", func(t *testing.T) {
		repo, found := cfg.FindRepoByURL("git@github.com:user/project-a.git")
		if !found {
			t.Fatal("expected to find repo")
		}
		if repo.Name != "project-a" {
			t.Errorf("Name = %q, want 'project-a'", repo.Name)
		}
	})

	t.Run("finds repo by HTTPS URL", func(t *testing.T) {
		repo, found := cfg.FindRepoByURL("https://github.com/user/project-b.git")
		if !found {
			t.Fatal("expected to find repo")
		}
		if repo.Name != "project-b" {
			t.Errorf("Name = %q, want 'project-b'", repo.Name)
		}
	})

	t.Run("returns false for unknown URL", func(t *testing.T) {
		_, found := cfg.FindRepoByURL("https://github.com/user/unknown.git")
		if found {
			t.Error("expected found=false for unknown URL")
		}
	})

	t.Run("second call uses cache", func(t *testing.T) {
		// Call again — should hit the cache path
		repo, found := cfg.FindRepoByURL("git@github.com:user/project-a.git")
		if !found {
			t.Fatal("expected to find repo from cache")
		}
		if repo.Name != "project-a" {
			t.Errorf("Name = %q, want 'project-a'", repo.Name)
		}
	})
}

func TestGetRunTarget(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		RunTargets: []RunTarget{
			{Name: "claude", Type: RunTargetTypePromptable, Command: "claude"},
			{Name: "codex", Type: RunTargetTypePromptable, Command: "codex"},
			{Name: "my-script", Type: RunTargetTypeCommand, Command: "bash run.sh"},
		},
	}

	t.Run("finds target by name", func(t *testing.T) {
		target, found := cfg.GetRunTarget("claude")
		if !found {
			t.Fatal("expected to find target")
		}
		if target.Command != "claude" {
			t.Errorf("Command = %q, want 'claude'", target.Command)
		}
	})

	t.Run("returns false for unknown target", func(t *testing.T) {
		_, found := cfg.GetRunTarget("nonexistent")
		if found {
			t.Error("expected found=false")
		}
	})
}

func TestGetDetectedRunTarget(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		RunTargets: []RunTarget{
			{Name: "claude", Source: RunTargetSourceDetected, Command: "claude"},
			{Name: "my-script", Source: "", Command: "bash run.sh"},
		},
	}

	t.Run("finds detected target", func(t *testing.T) {
		target, found := cfg.GetDetectedRunTarget("claude")
		if !found {
			t.Fatal("expected to find detected target")
		}
		if target.Command != "claude" {
			t.Errorf("Command = %q, want 'claude'", target.Command)
		}
	})

	t.Run("skips non-detected target", func(t *testing.T) {
		_, found := cfg.GetDetectedRunTarget("my-script")
		if found {
			t.Error("expected found=false for non-detected target")
		}
	})
}

func TestGetDetectedRunTargets(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		RunTargets: []RunTarget{
			{Name: "claude", Source: RunTargetSourceDetected, Command: "claude"},
			{Name: "codex", Source: RunTargetSourceDetected, Command: "codex"},
			{Name: "my-script", Source: "", Command: "bash"},
		},
	}

	targets := cfg.GetDetectedRunTargets()
	if len(targets) != 2 {
		t.Fatalf("expected 2 detected targets, got %d", len(targets))
	}
}

func TestGetBindAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		network *NetworkConfig
		want    string
	}{
		{"nil network defaults to localhost", nil, "127.0.0.1"},
		{"empty bind address defaults to localhost", &NetworkConfig{}, "127.0.0.1"},
		{"custom bind address", &NetworkConfig{BindAddress: "0.0.0.0"}, "0.0.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Network: tt.network}
			got := cfg.GetBindAddress()
			if got != tt.want {
				t.Errorf("GetBindAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetNetworkAccess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		network *NetworkConfig
		want    bool
	}{
		{"nil network is not accessible", nil, false},
		{"localhost is not accessible", &NetworkConfig{BindAddress: "127.0.0.1"}, false},
		{"0.0.0.0 is accessible", &NetworkConfig{BindAddress: "0.0.0.0"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Network: tt.network}
			got := cfg.GetNetworkAccess()
			if got != tt.want {
				t.Errorf("GetNetworkAccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPort(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		network *NetworkConfig
		want    int
	}{
		{"nil network defaults to 7337", nil, 7337},
		{"zero port defaults to 7337", &NetworkConfig{Port: 0}, 7337},
		{"negative port defaults to 7337", &NetworkConfig{Port: -1}, 7337},
		{"custom port", &NetworkConfig{Port: 8080}, 8080},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Network: tt.network}
			got := cfg.GetPort()
			if got != tt.want {
				t.Errorf("GetPort() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestUseWorktrees(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		scm  string
		want bool
	}{
		{"default (empty) uses worktrees", "", true},
		{"explicit git-worktree", SourceCodeManagementGitWorktree, true},
		{"git-clone does not use worktrees", SourceCodeManagementGit, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{SourceCodeManagement: tt.scm}
			got := cfg.UseWorktrees()
			if got != tt.want {
				t.Errorf("UseWorktrees() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsValidPublicBaseURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		url  string
		want bool
	}{
		{"https://example.com", true},
		{"https://my.domain.io:8443", true},
		{"http://localhost", true},
		{"http://localhost:7337", true},
		{"http://example.com", false},    // HTTP only allowed for localhost
		{"ftp://example.com", false},     // wrong scheme
		{"", false},                      // empty
		{"not-a-url", false},             // no scheme
		{"://missing-scheme", false},     // malformed
		{"https://", false},              // no host
		{"http://127.0.0.1:7337", false}, // HTTP not allowed for non-localhost
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := IsValidPublicBaseURL(tt.url)
			if got != tt.want {
				t.Errorf("IsValidPublicBaseURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestGetCompoundEnabled(t *testing.T) {
	t.Parallel()
	t.Run("defaults to true when nil", func(t *testing.T) {
		cfg := &Config{}
		if !cfg.GetCompoundEnabled() {
			t.Error("expected true by default")
		}
	})

	t.Run("returns true when Enabled is nil", func(t *testing.T) {
		cfg := &Config{Compound: &CompoundConfig{}}
		if !cfg.GetCompoundEnabled() {
			t.Error("expected true when Enabled is nil")
		}
	})

	t.Run("returns false when explicitly disabled", func(t *testing.T) {
		disabled := false
		cfg := &Config{Compound: &CompoundConfig{Enabled: &disabled}}
		if cfg.GetCompoundEnabled() {
			t.Error("expected false when explicitly disabled")
		}
	})

	t.Run("returns true when explicitly enabled", func(t *testing.T) {
		enabled := true
		cfg := &Config{Compound: &CompoundConfig{Enabled: &enabled}}
		if !cfg.GetCompoundEnabled() {
			t.Error("expected true when explicitly enabled")
		}
	})
}

func TestGetCompoundDebounceMs(t *testing.T) {
	t.Parallel()
	t.Run("defaults to 2000 when nil", func(t *testing.T) {
		cfg := &Config{}
		if got := cfg.GetCompoundDebounceMs(); got != 2000 {
			t.Errorf("got %d, want 2000", got)
		}
	})

	t.Run("defaults to 2000 when zero", func(t *testing.T) {
		cfg := &Config{Compound: &CompoundConfig{DebounceMs: 0}}
		if got := cfg.GetCompoundDebounceMs(); got != 2000 {
			t.Errorf("got %d, want 2000", got)
		}
	})

	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{Compound: &CompoundConfig{DebounceMs: 5000}}
		if got := cfg.GetCompoundDebounceMs(); got != 5000 {
			t.Errorf("got %d, want 5000", got)
		}
	})
}

func TestGetNotificationSoundEnabled(t *testing.T) {
	t.Parallel()
	t.Run("defaults to true when nil", func(t *testing.T) {
		cfg := &Config{}
		if !cfg.GetNotificationSoundEnabled() {
			t.Error("expected true by default")
		}
	})

	t.Run("returns true when SoundDisabled is false", func(t *testing.T) {
		cfg := &Config{Notifications: &NotificationsConfig{SoundDisabled: false}}
		if !cfg.GetNotificationSoundEnabled() {
			t.Error("expected true when SoundDisabled is false")
		}
	})

	t.Run("returns false when SoundDisabled is true", func(t *testing.T) {
		cfg := &Config{Notifications: &NotificationsConfig{SoundDisabled: true}}
		if cfg.GetNotificationSoundEnabled() {
			t.Error("expected false when SoundDisabled is true")
		}
	})
}

func TestGetConfirmBeforeClose(t *testing.T) {
	t.Parallel()
	t.Run("defaults to false when nil", func(t *testing.T) {
		cfg := &Config{}
		if cfg.GetConfirmBeforeClose() {
			t.Error("expected false by default")
		}
	})

	t.Run("returns configured value", func(t *testing.T) {
		cfg := &Config{Notifications: &NotificationsConfig{ConfirmBeforeClose: true}}
		if !cfg.GetConfirmBeforeClose() {
			t.Error("expected true when configured")
		}
	})
}

func TestGetModelVersion(t *testing.T) {
	t.Parallel()
	t.Run("returns empty when nil", func(t *testing.T) {
		cfg := &Config{}
		if got := cfg.GetModelVersion("claude-sonnet"); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("returns empty when versions nil", func(t *testing.T) {
		cfg := &Config{Models: &ModelsConfig{}}
		if got := cfg.GetModelVersion("claude-sonnet"); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("returns configured version", func(t *testing.T) {
		cfg := &Config{Models: &ModelsConfig{
			Versions: map[string]string{"claude-sonnet": "claude-sonnet-4-5-20250929"},
		}}
		got := cfg.GetModelVersion("claude-sonnet")
		if got != "claude-sonnet-4-5-20250929" {
			t.Errorf("got %q, want 'claude-sonnet-4-5-20250929'", got)
		}
	})

	t.Run("returns empty for unknown model", func(t *testing.T) {
		cfg := &Config{Models: &ModelsConfig{
			Versions: map[string]string{"claude-sonnet": "v1"},
		}}
		if got := cfg.GetModelVersion("claude-opus"); got != "" {
			t.Errorf("got %q, want empty for unknown model", got)
		}
	})
}

func TestDetectedToolsFromConfig(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		RunTargets: []RunTarget{
			{Name: "claude", Source: RunTargetSourceDetected, Command: "/usr/bin/claude"},
			{Name: "codex", Source: RunTargetSourceDetected, Command: "/usr/bin/codex"},
			{Name: "my-script", Source: "", Command: "bash run.sh"}, // user target, not detected
		},
	}

	tools := DetectedToolsFromConfig(cfg)
	if len(tools) != 2 {
		t.Fatalf("expected 2 detected tools, got %d", len(tools))
	}
	if tools[0].Name != "claude" || tools[0].Command != "/usr/bin/claude" {
		t.Errorf("tools[0] = %+v, want Name='claude' Command='/usr/bin/claude'", tools[0])
	}
	if tools[1].Name != "codex" || tools[1].Command != "/usr/bin/codex" {
		t.Errorf("tools[1] = %+v, want Name='codex' Command='/usr/bin/codex'", tools[1])
	}
}

func TestMergeDetectedRunTargets(t *testing.T) {
	t.Parallel()
	t.Run("preserves user targets and adds detected", func(t *testing.T) {
		existing := []RunTarget{
			{Name: "my-script", Type: RunTargetTypeCommand, Command: "bash run.sh", Source: ""},
			{Name: "old-detected", Type: RunTargetTypePromptable, Command: "old", Source: RunTargetSourceDetected},
		}
		detected := []detect.Tool{
			{Name: "claude", Command: "/usr/bin/claude"},
			{Name: "codex", Command: "/usr/bin/codex"},
		}

		merged := MergeDetectedRunTargets(existing, detected)

		// Should have: user target + 2 detected (old detected target is dropped)
		if len(merged) != 3 {
			t.Fatalf("expected 3 merged targets, got %d", len(merged))
		}

		// First should be the user target
		if merged[0].Name != "my-script" || merged[0].Source != "" {
			t.Errorf("merged[0] should be user target, got %+v", merged[0])
		}

		// Detected targets should have correct Source
		if merged[1].Source != RunTargetSourceDetected {
			t.Errorf("merged[1].Source = %q, want 'detected'", merged[1].Source)
		}
		if merged[2].Source != RunTargetSourceDetected {
			t.Errorf("merged[2].Source = %q, want 'detected'", merged[2].Source)
		}
	})

	t.Run("empty existing returns only detected", func(t *testing.T) {
		detected := []detect.Tool{{Name: "claude", Command: "claude"}}
		merged := MergeDetectedRunTargets(nil, detected)
		if len(merged) != 1 {
			t.Fatalf("expected 1, got %d", len(merged))
		}
	})

	t.Run("empty detected returns only user targets", func(t *testing.T) {
		existing := []RunTarget{
			{Name: "script", Command: "bash", Source: ""},
		}
		merged := MergeDetectedRunTargets(existing, nil)
		if len(merged) != 1 {
			t.Fatalf("expected 1, got %d", len(merged))
		}
	})
}

func TestIsTargetPromptable(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		RunTargets: []RunTarget{
			{Name: "my-script", Type: RunTargetTypeCommand, Command: "bash"},
			{Name: "my-agent", Type: RunTargetTypePromptable, Command: "agent"},
		},
	}
	detected := []RunTarget{
		{Name: "claude", Type: RunTargetTypePromptable, Command: "claude", Source: RunTargetSourceDetected},
	}

	tests := []struct {
		name           string
		target         string
		wantPromptable bool
		wantFound      bool
	}{
		{"user promptable target", "my-agent", true, true},
		{"user command target", "my-script", false, true},
		{"detected tool", "claude", true, true},
		{"unknown target", "nonexistent", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			promptable, found := IsTargetPromptable(cfg, detected, tt.target)
			if promptable != tt.wantPromptable {
				t.Errorf("promptable = %v, want %v", promptable, tt.wantPromptable)
			}
			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
			}
		})
	}
}

func TestTimeoutDurationConverters(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		Sessions: &SessionsConfig{
			GitCloneTimeoutMs:  30000,
			GitStatusTimeoutMs: 5000,
		},
		Xterm: &XtermConfig{
			QueryTimeoutMs:     1000,
			OperationTimeoutMs: 2000,
		},
	}

	tests := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"GitCloneTimeout", cfg.GitCloneTimeout(), 30 * time.Second},
		{"GitStatusTimeout", cfg.GitStatusTimeout(), 5 * time.Second},
		{"XtermQueryTimeout", cfg.XtermQueryTimeout(), time.Second},
		{"XtermOperationTimeout", cfg.XtermOperationTimeout(), 2 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}
