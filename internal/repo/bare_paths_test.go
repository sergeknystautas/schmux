package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// newManagerIn builds a manager whose config, state, and worktree base path
// live under dir.
func newManagerIn(t *testing.T, dir string) (*Manager, *state.State, *config.Config) {
	t.Helper()
	cfg := config.CreateDefault(filepath.Join(dir, "config.json"))
	cfg.WorktreeBasePath = filepath.Join(dir, "repos")
	st := state.New(filepath.Join(dir, "state.json"), nil)
	return New(cfg, st, nil), st, cfg
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
	m, st, _ := newManagerIn(t, t.TempDir())

	if err := st.AddRepo(state.Repo{Name: "myrepo", URL: "https://github.com/user/myrepo.git"}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	// PopulateBarePaths should set bare_path for new repos
	m.PopulateBarePaths()

	got, _ := m.Get("myrepo")
	if got.BarePath != "myrepo.git" {
		t.Errorf("BarePath = %q, want %q", got.BarePath, "myrepo.git")
	}
}

func TestPopulateBarePaths_CorrectsMismatch(t *testing.T) {
	dir := t.TempDir()
	m, st, cfg := newManagerIn(t, dir)

	// Create a bare repo at the namespaced path (owner/repo.git)
	repoURL := "https://github.com/myowner/myrepo.git"
	namespacedDir := filepath.Join(cfg.GetWorktreeBasePath(), "myowner", "myrepo.git")
	if err := os.MkdirAll(namespacedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, namespacedDir, "init", "--bare")
	runGitCmd(t, namespacedDir, "remote", "add", "origin", repoURL)

	if err := st.AddRepo(state.Repo{
		// State has flat "myrepo.git" but the repo is at "myowner/myrepo.git"
		Name: "myrepo", URL: repoURL, BarePath: "myrepo.git",
	}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	m.PopulateBarePaths()

	want := filepath.Join("myowner", "myrepo.git")
	got, _ := m.Get("myrepo")
	if got.BarePath != want {
		t.Errorf("BarePath = %q, want %q (should be corrected to match disk)", got.BarePath, want)
	}
}

func TestPopulateBarePaths_AlreadySet(t *testing.T) {
	m, st, _ := newManagerIn(t, t.TempDir())

	if err := st.AddRepo(state.Repo{
		Name: "myrepo", URL: "https://github.com/user/myrepo.git", BarePath: "custom/path.git",
	}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	// PopulateBarePaths should NOT overwrite existing bare_path
	m.PopulateBarePaths()

	got, _ := m.Get("myrepo")
	if got.BarePath != "custom/path.git" {
		t.Errorf("BarePath = %q, want %q (should not be overwritten)", got.BarePath, "custom/path.git")
	}
}

func TestAddPendingThenPopulate_NamespacedClone(t *testing.T) {
	m, _, cfg := newManagerIn(t, t.TempDir())

	repoURL := "https://github.com/owner/repo.git"
	namespacedDir := filepath.Join(cfg.GetWorktreeBasePath(), "owner", "repo.git")
	if err := os.MkdirAll(namespacedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, namespacedDir, "init", "--bare")
	runGitCmd(t, namespacedDir, "remote", "add", "origin", repoURL)

	pending := filepath.Join(t.TempDir(), "pending-repos.jsonl")
	writePending(t, pending, `{"name":"repo","url":"`+repoURL+`"}`)

	if err := m.AddPending(pending); err != nil {
		t.Fatalf("AddPending: %v", err)
	}
	m.PopulateBarePaths()

	want := filepath.Join("owner", "repo.git")
	got, _ := m.Get("repo")
	if got.BarePath != want {
		t.Errorf("BarePath = %q, want %q", got.BarePath, want)
	}
}

func TestNormalizeBarePaths_RenamesNonConforming(t *testing.T) {
	tmpDir := gitTestTempDir(t)
	reposDir := filepath.Join(tmpDir, "repos")

	// Create a bare repo at the namespaced path: repos/facebook/react.git
	bareDir, worktrees := createBareRepoWithWorktrees(t, tmpDir, filepath.Join("repos", "facebook", "react.git"), 1)
	oldBarePath := filepath.Join(reposDir, "facebook", "react.git")
	if bareDir != oldBarePath {
		t.Fatalf("bareDir = %q, want %q", bareDir, oldBarePath)
	}

	m, st, _ := newManagerIn(t, tmpDir)
	if err := st.AddRepo(state.Repo{
		Name: "react", URL: "https://github.com/facebook/react.git", BarePath: "facebook/react.git",
	}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	m.NormalizeBarePaths()

	// The stored entry should be updated
	got, _ := m.Get("react")
	if got.BarePath != "react.git" {
		t.Errorf("BarePath = %q, want %q", got.BarePath, "react.git")
	}

	// Old path should not exist, new path should
	newBarePath := filepath.Join(reposDir, "react.git")
	if _, err := os.Stat(oldBarePath); !os.IsNotExist(err) {
		t.Errorf("old path should not exist")
	}
	if _, err := os.Stat(newBarePath); err != nil {
		t.Errorf("new path should exist: %v", err)
	}

	// Worktrees should still work after relocation
	for _, wt := range worktrees {
		runGitCmd(t, wt, "status")
	}
}

func TestNormalizeBarePaths_SkipsAlreadyConforming(t *testing.T) {
	tmpDir := gitTestTempDir(t)

	// Create a bare repo at the correct path
	createBareRepoWithWorktrees(t, tmpDir, filepath.Join("repos", "react.git"), 0)

	m, st, _ := newManagerIn(t, tmpDir)
	if err := st.AddRepo(state.Repo{
		Name: "react", URL: "https://github.com/facebook/react.git", BarePath: "react.git",
	}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	m.NormalizeBarePaths()

	// Should remain unchanged
	got, _ := m.Get("react")
	if got.BarePath != "react.git" {
		t.Errorf("BarePath = %q, want %q", got.BarePath, "react.git")
	}
}

func TestNormalizeBarePaths_SkipsSapling(t *testing.T) {
	m, st, _ := newManagerIn(t, t.TempDir())

	if err := st.AddRepo(state.Repo{
		Name: "myrepo", URL: "myrepo-id", VCS: "sapling", BarePath: "custom-path",
	}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	m.NormalizeBarePaths()

	// Sapling repos should not be touched
	got, _ := m.Get("myrepo")
	if got.BarePath != "custom-path" {
		t.Errorf("BarePath = %q, want %q (sapling should be skipped)", got.BarePath, "custom-path")
	}
}

func TestNormalizeBarePaths_SkipsWhenNotOnDisk(t *testing.T) {
	m, st, _ := newManagerIn(t, t.TempDir())

	if err := st.AddRepo(state.Repo{
		Name: "react", URL: "https://github.com/facebook/react.git", BarePath: "facebook/react.git",
	}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	m.NormalizeBarePaths()

	// Nothing on disk, BarePath should remain unchanged
	got, _ := m.Get("react")
	if got.BarePath != "facebook/react.git" {
		t.Errorf("BarePath = %q, want %q (should skip when not on disk)", got.BarePath, "facebook/react.git")
	}
}

func TestNormalizeBarePaths_SkipsOnCollision(t *testing.T) {
	tmpDir := gitTestTempDir(t)
	reposDir := filepath.Join(tmpDir, "repos")

	// Create bare repo at old (non-conforming) path
	createBareRepoWithWorktrees(t, tmpDir, filepath.Join("repos", "facebook", "react.git"), 0)

	// Create something at the target path to simulate a collision.
	// Use a plain directory — createBareRepoWithWorktrees would collide on the
	// shared remote-origin helper dir.
	targetPath := filepath.Join(reposDir, "react.git")
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}

	m, st, _ := newManagerIn(t, tmpDir)
	if err := st.AddRepo(state.Repo{
		Name: "react", URL: "https://github.com/facebook/react.git", BarePath: "facebook/react.git",
	}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	m.NormalizeBarePaths()

	// Should not rename — target already exists
	got, _ := m.Get("react")
	if got.BarePath != "facebook/react.git" {
		t.Errorf("BarePath = %q, want %q (should skip on collision)", got.BarePath, "facebook/react.git")
	}
}

func TestNormalizeBarePaths_NormalizesQueryDir(t *testing.T) {
	tmpDir := gitTestTempDir(t)
	// Simulate ~/.schmux layout by setting HOME to tmpDir
	t.Setenv("HOME", tmpDir)

	schmuxDir := filepath.Join(tmpDir, ".schmux")
	queryDir := filepath.Join(schmuxDir, "query")

	// Create a bare repo in query/ at the non-conforming path
	createBareRepoWithWorktrees(t, tmpDir, filepath.Join(".schmux", "query", "facebook", "react.git"), 0)

	m, st, _ := newManagerIn(t, schmuxDir)
	if err := st.AddRepo(state.Repo{
		Name: "react", URL: "https://github.com/facebook/react.git", BarePath: "facebook/react.git",
	}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	m.NormalizeBarePaths()

	// BarePath should be updated
	got, _ := m.Get("react")
	if got.BarePath != "react.git" {
		t.Errorf("BarePath = %q, want %q", got.BarePath, "react.git")
	}

	// Query repo should be renamed
	oldQueryPath := filepath.Join(queryDir, "facebook", "react.git")
	newQueryPath := filepath.Join(queryDir, "react.git")
	if _, err := os.Stat(oldQueryPath); !os.IsNotExist(err) {
		t.Errorf("old query path should not exist")
	}
	if _, err := os.Stat(newQueryPath); err != nil {
		t.Errorf("new query path should not exist: %v", err)
	}
}
