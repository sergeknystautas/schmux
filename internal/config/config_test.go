package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
  "terminal": {"width": 120, "height": 40, "seed_lines": 100},
  "access_control": {
    "enabled": "true"
  }
}`,
			wantField:    "access_control.enabled",
			wantContains: "line",
		},
		{
			name: "string instead of int",
			json: `{
  "workspace_path": "/test",
  "repos": [],
  "run_targets": [],
  "terminal": {"width": "120", "height": 40, "seed_lines": 100}
}`,
			wantField:    "terminal.width",
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

func TestGetOverlayPaths_DefaultsOnly(t *testing.T) {
	cfg := &Config{}
	paths := cfg.GetOverlayPaths("myrepo")
	// Should include hardcoded defaults
	if len(paths) < 2 {
		t.Fatalf("expected at least 2 default paths, got %d", len(paths))
	}
	found := make(map[string]bool)
	for _, p := range paths {
		found[p] = true
	}
	if !found[".claude/settings.json"] {
		t.Error("missing .claude/settings.json from defaults")
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
	if !found[".claude/settings.json"] {
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
			Paths: []string{".claude/settings.json"}, // duplicate of default
		},
		Repos: []Repo{
			{Name: "myrepo", URL: "url", OverlayPaths: []string{".claude/settings.json"}},
		},
	}
	paths := cfg.GetOverlayPaths("myrepo")
	count := 0
	for _, p := range paths {
		if p == ".claude/settings.json" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 occurrence of .claude/settings.json, got %d", count)
	}
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
