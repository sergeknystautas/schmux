package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
)

func TestNormalizeBarePaths_RenamesNonConforming(t *testing.T) {
	tmpDir := gitTestTempDir(t)
	reposDir := filepath.Join(tmpDir, "repos")
	configPath := filepath.Join(tmpDir, "config.json")

	// Create a bare repo at the namespaced path: repos/facebook/react.git
	bareDir, worktrees := createBareRepoWithWorktrees(t, tmpDir, filepath.Join("repos", "facebook", "react.git"), 1)
	oldBarePath := filepath.Join(reposDir, "facebook", "react.git")
	if bareDir != oldBarePath {
		t.Fatalf("bareDir = %q, want %q", bareDir, oldBarePath)
	}

	// Set up config with non-conforming BarePath
	cfg := CreateDefault(configPath)
	cfg.WorktreeBasePath = reposDir
	cfg.Repos = []Repo{
		{Name: "react", URL: "https://github.com/facebook/react.git", BarePath: "facebook/react.git"},
	}
	cfg.Save()

	// Set up state with the old path
	st := state.New(filepath.Join(tmpDir, "state.json"), nil)
	st.AddRepoBase(state.RepoBase{RepoURL: "https://github.com/facebook/react.git", Path: oldBarePath})

	NormalizeBarePaths(cfg, st)

	// Config should be updated
	if cfg.Repos[0].BarePath != "react.git" {
		t.Errorf("BarePath = %q, want %q", cfg.Repos[0].BarePath, "react.git")
	}

	// Old path should not exist, new path should
	newBarePath := filepath.Join(reposDir, "react.git")
	if _, err := os.Stat(oldBarePath); !os.IsNotExist(err) {
		t.Errorf("old path should not exist")
	}
	if _, err := os.Stat(newBarePath); err != nil {
		t.Errorf("new path should exist: %v", err)
	}

	// State should be updated
	rb, found := st.GetRepoBaseByURL("https://github.com/facebook/react.git")
	if !found {
		t.Fatal("repo base should exist in state")
	}
	if rb.Path != newBarePath {
		t.Errorf("state path = %q, want %q", rb.Path, newBarePath)
	}

	// Worktrees should still work after relocation
	for _, wt := range worktrees {
		runGitCmd(t, wt, "status")
	}
}

func TestNormalizeBarePaths_SkipsAlreadyConforming(t *testing.T) {
	tmpDir := gitTestTempDir(t)
	reposDir := filepath.Join(tmpDir, "repos")
	configPath := filepath.Join(tmpDir, "config.json")

	// Create a bare repo at the correct path
	createBareRepoWithWorktrees(t, tmpDir, filepath.Join("repos", "react.git"), 0)

	cfg := CreateDefault(configPath)
	cfg.WorktreeBasePath = reposDir
	cfg.Repos = []Repo{
		{Name: "react", URL: "https://github.com/facebook/react.git", BarePath: "react.git"},
	}

	st := state.New(filepath.Join(tmpDir, "state.json"), nil)

	NormalizeBarePaths(cfg, st)

	// Should remain unchanged
	if cfg.Repos[0].BarePath != "react.git" {
		t.Errorf("BarePath = %q, want %q", cfg.Repos[0].BarePath, "react.git")
	}
}

func TestNormalizeBarePaths_SkipsSapling(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := CreateDefault(configPath)
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	cfg.Repos = []Repo{
		{Name: "myrepo", URL: "myrepo-id", VCS: "sapling", BarePath: "custom-path"},
	}

	st := state.New(filepath.Join(tmpDir, "state.json"), nil)

	NormalizeBarePaths(cfg, st)

	// Sapling repos should not be touched
	if cfg.Repos[0].BarePath != "custom-path" {
		t.Errorf("BarePath = %q, want %q (sapling should be skipped)", cfg.Repos[0].BarePath, "custom-path")
	}
}

func TestNormalizeBarePaths_SkipsWhenNotOnDisk(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := CreateDefault(configPath)
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	cfg.Repos = []Repo{
		{Name: "react", URL: "https://github.com/facebook/react.git", BarePath: "facebook/react.git"},
	}

	st := state.New(filepath.Join(tmpDir, "state.json"), nil)

	NormalizeBarePaths(cfg, st)

	// Nothing on disk, BarePath should remain unchanged
	if cfg.Repos[0].BarePath != "facebook/react.git" {
		t.Errorf("BarePath = %q, want %q (should skip when not on disk)", cfg.Repos[0].BarePath, "facebook/react.git")
	}
}

func TestNormalizeBarePaths_SkipsOnCollision(t *testing.T) {
	tmpDir := gitTestTempDir(t)
	reposDir := filepath.Join(tmpDir, "repos")
	configPath := filepath.Join(tmpDir, "config.json")

	// Create bare repo at old (non-conforming) path
	createBareRepoWithWorktrees(t, tmpDir, filepath.Join("repos", "facebook", "react.git"), 0)

	// Create something at the target path to simulate a collision.
	// Use a plain directory — createBareRepoWithWorktrees would collide on the
	// shared remote-origin helper dir.
	targetPath := filepath.Join(reposDir, "react.git")
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}

	cfg := CreateDefault(configPath)
	cfg.WorktreeBasePath = reposDir
	cfg.Repos = []Repo{
		{Name: "react", URL: "https://github.com/facebook/react.git", BarePath: "facebook/react.git"},
	}

	st := state.New(filepath.Join(tmpDir, "state.json"), nil)

	NormalizeBarePaths(cfg, st)

	// Should not rename — target already exists
	if cfg.Repos[0].BarePath != "facebook/react.git" {
		t.Errorf("BarePath = %q, want %q (should skip on collision)", cfg.Repos[0].BarePath, "facebook/react.git")
	}
}

func TestNormalizeBarePaths_NormalizesQueryDir(t *testing.T) {
	tmpDir := gitTestTempDir(t)
	// Simulate ~/.schmux layout by setting HOME to tmpDir
	t.Setenv("HOME", tmpDir)

	schmuxDir := filepath.Join(tmpDir, ".schmux")
	reposDir := filepath.Join(schmuxDir, "repos")
	queryDir := filepath.Join(schmuxDir, "query")
	configPath := filepath.Join(schmuxDir, "config.json")

	// Create a bare repo in query/ at the non-conforming path
	createBareRepoWithWorktrees(t, tmpDir, filepath.Join(".schmux", "query", "facebook", "react.git"), 0)

	cfg := CreateDefault(configPath)
	cfg.WorktreeBasePath = reposDir
	cfg.Repos = []Repo{
		{Name: "react", URL: "https://github.com/facebook/react.git", BarePath: "facebook/react.git"},
	}

	st := state.New(filepath.Join(schmuxDir, "state.json"), nil)

	NormalizeBarePaths(cfg, st)

	// BarePath should be updated
	if cfg.Repos[0].BarePath != "react.git" {
		t.Errorf("BarePath = %q, want %q", cfg.Repos[0].BarePath, "react.git")
	}

	// Query repo should be renamed
	oldQueryPath := filepath.Join(queryDir, "facebook", "react.git")
	newQueryPath := filepath.Join(queryDir, "react.git")
	if _, err := os.Stat(oldQueryPath); !os.IsNotExist(err) {
		t.Errorf("old query path should not exist")
	}
	if _, err := os.Stat(newQueryPath); err != nil {
		t.Errorf("new query path should exist: %v", err)
	}
}
