package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// runGit executes a git command in the given directory.
// Fails the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

// gitTestWorkTree creates a working git tree with an initial commit.
// Returns the path to the repo (auto-cleanup via t.TempDir).
func gitTestWorkTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Initialize repo on main branch
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test User")

	// Create initial commit
	writeFile(t, dir, "README.md", "test repo")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	return dir
}

// gitTestBranch creates a new branch with a commit in the test repo.
func gitTestBranch(t *testing.T, repoDir, branchName string) {
	t.Helper()
	runGit(t, repoDir, "checkout", "-b", branchName)
	writeFile(t, repoDir, "branch.txt", branchName)
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", branchName)
	runGit(t, repoDir, "checkout", "-") // return to previous branch
}

// writeFile creates a file with content for testing.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", name, err)
	}
}

// currentBranch returns the current git branch name.
func currentBranch(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}
	return strings.TrimSpace(string(output))
}

func TestExtractRepoName(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		// SSH URLs
		{"git@github.com:user/myrepo.git", "myrepo"},
		{"git@github.com:user/myrepo", "myrepo"},
		{"git@gitlab.com:org/project.git", "project"},

		// HTTPS URLs
		{"https://github.com/user/myrepo.git", "myrepo"},
		{"https://github.com/user/myrepo", "myrepo"},
		{"https://gitlab.com/org/subgroup/project.git", "project"},

		// File URLs (used in tests)
		{"file:///tmp/test-repo", "test-repo"},
		{"/tmp/local-repo", "local-repo"},

		// Edge cases
		{"repo.git", "repo"},
		{"repo", "repo"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := extractRepoName(tt.url)
			if got != tt.want {
				t.Errorf("extractRepoName(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestIsWorktree(t *testing.T) {
	// Test with non-existent path
	t.Run("non-existent path", func(t *testing.T) {
		if isWorktree("/nonexistent/path") {
			t.Error("isWorktree should return false for non-existent path")
		}
	})

	// Test with .git directory (full clone)
	t.Run("full clone with .git directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitDir := filepath.Join(tmpDir, ".git")
		if err := os.Mkdir(gitDir, 0755); err != nil {
			t.Fatalf("failed to create .git dir: %v", err)
		}

		if isWorktree(tmpDir) {
			t.Error("isWorktree should return false for .git directory")
		}
	})

	// Test with .git file (worktree)
	t.Run("worktree with .git file", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitFile := filepath.Join(tmpDir, ".git")
		if err := os.WriteFile(gitFile, []byte("gitdir: /some/path"), 0644); err != nil {
			t.Fatalf("failed to create .git file: %v", err)
		}

		if !isWorktree(tmpDir) {
			t.Error("isWorktree should return true for .git file")
		}
	})
}

func TestResolveBaseRepoFromWorktree(t *testing.T) {
	t.Run("valid worktree .git file", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitFile := filepath.Join(tmpDir, ".git")
		content := "gitdir: /home/user/.schmux/repos/myrepo.git/worktrees/myrepo-001"
		if err := os.WriteFile(gitFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create .git file: %v", err)
		}

		got, err := resolveBaseRepoFromWorktree(tmpDir)
		if err != nil {
			t.Fatalf("resolveBaseRepoFromWorktree() error = %v", err)
		}
		want := "/home/user/.schmux/repos/myrepo.git"
		if got != want {
			t.Errorf("resolveBaseRepoFromWorktree() = %q, want %q", got, want)
		}
	})

	t.Run("invalid format - no gitdir prefix", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitFile := filepath.Join(tmpDir, ".git")
		if err := os.WriteFile(gitFile, []byte("invalid content"), 0644); err != nil {
			t.Fatalf("failed to create .git file: %v", err)
		}

		_, err := resolveBaseRepoFromWorktree(tmpDir)
		if err == nil {
			t.Error("resolveBaseRepoFromWorktree() should error on invalid format")
		}
	})

	t.Run("invalid format - no worktrees path", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitFile := filepath.Join(tmpDir, ".git")
		if err := os.WriteFile(gitFile, []byte("gitdir: /some/other/path"), 0644); err != nil {
			t.Fatalf("failed to create .git file: %v", err)
		}

		_, err := resolveBaseRepoFromWorktree(tmpDir)
		if err == nil {
			t.Error("resolveBaseRepoFromWorktree() should error when no /worktrees/ in path")
		}
	})

	t.Run("missing .git file", func(t *testing.T) {
		tmpDir := t.TempDir()
		_, err := resolveBaseRepoFromWorktree(tmpDir)
		if err == nil {
			t.Error("resolveBaseRepoFromWorktree() should error on missing .git file")
		}
	})
}

// TestGitPullRebase_MultipleBranchesConfig reproduces "Cannot rebase onto multiple branches"
// by manually crafting a broken .git/config with multiple merge refs, then verifies
// that schmux's gitPullRebase with explicit origin/<branch> works around it.
func TestGitPullRebase_MultipleBranchesConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a remote repo
	remoteDir := gitTestWorkTree(t)
	runGit(t, remoteDir, "checkout", "-b", "feature")
	writeFile(t, remoteDir, "feature.txt", "feature")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "feature")
	runGit(t, remoteDir, "checkout", "main")

	// Clone it
	tmpDir := t.TempDir()
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(t, tmpDir, "clone", remoteDir, "clone")

	// Manually break .git/config by adding duplicate merge ref
	gitConfigPath := filepath.Join(cloneDir, ".git", "config")
	configContent, _ := os.ReadFile(gitConfigPath)

	brokenConfig := string(configContent)
	if !strings.Contains(brokenConfig, "[branch \"main\"]") {
		brokenConfig += "\n[branch \"main\"]\n\tremote = origin\n\tmerge = refs/heads/main\n"
	}
	brokenConfig += "\tmerge = refs/heads/feature\n"

	if err := os.WriteFile(gitConfigPath, []byte(brokenConfig), 0644); err != nil {
		t.Fatalf("failed to write broken config: %v", err)
	}

	// Verify raw "git pull --rebase" fails with the error
	cmd := exec.Command("git", "-C", cloneDir, "pull", "--rebase")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("git pull --rebase should have failed with multiple merge refs")
	}
	if !strings.Contains(string(output), "Cannot rebase onto multiple branches") {
		t.Logf("Raw git pull error: %v: %s", err, output)
	} else {
		t.Log("Confirmed: raw 'git pull --rebase' fails with broken config")
	}

	// Now test that schmux's gitPullRebase with explicit branch works
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath)
	m := New(cfg, st, statePath)
	ctx := context.Background()

	// This should work because we explicitly specify origin/main
	err = m.gitPullRebase(ctx, cloneDir, "main")
	if err != nil {
		t.Errorf("gitPullRebase with explicit branch should work: %v", err)
	} else {
		t.Log("SUCCESS: gitPullRebase(origin main) works despite broken upstream config")
	}
}

// TestGitPullRebase_WithBranchParameter tests that gitPullRebase takes
// a branch parameter and explicitly pulls from origin/<branch>.
func TestGitPullRebase_WithBranchParameter(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a remote repo
	remoteDir := gitTestWorkTree(t)
	runGit(t, remoteDir, "checkout", "-b", "feature")
	writeFile(t, remoteDir, "feature.txt", "feature")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "feature")
	runGit(t, remoteDir, "checkout", "main")

	// Clone it
	tmpDir := t.TempDir()
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(t, tmpDir, "clone", remoteDir, "clone")

	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath)
	m := New(cfg, st, statePath)
	ctx := context.Background()

	// gitPullRebase with explicit origin/<branch> should work
	err := m.gitPullRebase(ctx, cloneDir, "main")
	if err != nil {
		t.Errorf("gitPullRebase(main) failed: %v", err)
	}

	// Switch to feature branch and pull
	runGit(t, cloneDir, "checkout", "feature")
	err = m.gitPullRebase(ctx, cloneDir, "feature")
	if err != nil {
		t.Errorf("gitPullRebase(feature) failed: %v", err)
	}

	t.Log("gitPullRebase() takes branch parameter - explicitly pulls from origin/<branch>")
}

func TestGitRemoteBranchExists(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	remoteDir := gitTestWorkTree(t)
	runGit(t, remoteDir, "checkout", "-b", "feature")
	writeFile(t, remoteDir, "feature.txt", "feature")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "feature")
	runGit(t, remoteDir, "checkout", "main")

	tmpDir := t.TempDir()
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(t, tmpDir, "clone", remoteDir, "clone")

	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath)
	m := New(cfg, st, statePath)
	ctx := context.Background()

	exists, err := m.gitRemoteBranchExists(ctx, cloneDir, "main")
	if err != nil {
		t.Fatalf("gitRemoteBranchExists(main) error: %v", err)
	}
	if !exists {
		t.Error("gitRemoteBranchExists(main) expected true")
	}

	exists, err = m.gitRemoteBranchExists(ctx, cloneDir, "feature")
	if err != nil {
		t.Fatalf("gitRemoteBranchExists(feature) error: %v", err)
	}
	if !exists {
		t.Error("gitRemoteBranchExists(feature) expected true")
	}

	exists, err = m.gitRemoteBranchExists(ctx, cloneDir, "missing-branch")
	if err != nil {
		t.Fatalf("gitRemoteBranchExists(missing-branch) error: %v", err)
	}
	if exists {
		t.Error("gitRemoteBranchExists(missing-branch) expected false")
	}
}
