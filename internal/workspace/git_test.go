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

// testRepoWithBarePath returns a config.Repo with BarePath set to <name>.git.
func testRepoWithBarePath(name, url string) config.Repo {
	return config.Repo{
		Name:     name,
		URL:      url,
		BarePath: name + ".git",
	}
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

func TestValidateBranchName(t *testing.T) {
	tests := []struct {
		name    string
		branch  string
		wantErr bool
	}{
		// Valid branch names
		{"simple lowercase", "main", false},
		{"with numbers", "feature123", false},
		{"with underscore", "feature_test", false},
		{"consecutive underscores", "feature__test", true},
		{"with hyphen", "feature-branch", false},
		{"consecutive hyphens", "feature--test", true},
		{"with slash", "feature/test", false},
		{"consecutive slashes invalid", "feature//test", true},
		{"with period", "feature.test", false},
		{"consecutive periods invalid", "feature..test", true},
		{"mixed separators", "feature/test.branch_name-123", false},

		// Invalid: starts/ends with separator
		{"starts with slash", "/feature", true},
		{"ends with slash", "feature/", true},
		{"starts with period", ".feature", true},
		{"ends with period", "feature.", true},

		// Invalid: empty or whitespace
		{"empty", "", true},
		{"whitespace only", " ", true},

		// Invalid: uppercase
		{"uppercase", "Feature", true},
		{"uppercase mixed", "featureTest", true},

		// Invalid: special characters
		{"at sign", "feature@branch", true},
		{"hash", "feature#branch", true},
		{"space", "feature branch", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBranchName(tt.branch)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBranchName(%q) error = %v, wantErr %v", tt.branch, err, tt.wantErr)
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

func TestResolveWorktreeBaseFromWorktree(t *testing.T) {
	t.Run("valid worktree .git file", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitFile := filepath.Join(tmpDir, ".git")
		content := "gitdir: /home/user/.schmux/repos/myrepo.git/worktrees/myrepo-001"
		if err := os.WriteFile(gitFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create .git file: %v", err)
		}

		got, err := resolveWorktreeBaseFromWorktree(tmpDir)
		if err != nil {
			t.Fatalf("resolveWorktreeBaseFromWorktree() error = %v", err)
		}
		want := "/home/user/.schmux/repos/myrepo.git"
		if got != want {
			t.Errorf("resolveWorktreeBaseFromWorktree() = %q, want %q", got, want)
		}
	})

	t.Run("invalid format - no gitdir prefix", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitFile := filepath.Join(tmpDir, ".git")
		if err := os.WriteFile(gitFile, []byte("invalid content"), 0644); err != nil {
			t.Fatalf("failed to create .git file: %v", err)
		}

		_, err := resolveWorktreeBaseFromWorktree(tmpDir)
		if err == nil {
			t.Error("resolveWorktreeBaseFromWorktree() should error on invalid format")
		}
	})

	t.Run("invalid format - no worktrees path", func(t *testing.T) {
		tmpDir := t.TempDir()
		gitFile := filepath.Join(tmpDir, ".git")
		if err := os.WriteFile(gitFile, []byte("gitdir: /some/other/path"), 0644); err != nil {
			t.Fatalf("failed to create .git file: %v", err)
		}

		_, err := resolveWorktreeBaseFromWorktree(tmpDir)
		if err == nil {
			t.Error("resolveWorktreeBaseFromWorktree() should error when no /worktrees/ in path")
		}
	})

	t.Run("missing .git file", func(t *testing.T) {
		tmpDir := t.TempDir()
		_, err := resolveWorktreeBaseFromWorktree(tmpDir)
		if err == nil {
			t.Error("resolveWorktreeBaseFromWorktree() should error on missing .git file")
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

// TestCheckGitSafety_PushedToOriginBranch verifies that checkGitSafety reports
// Safe=true when commits have been pushed to origin/<branch>, even if the branch's
// upstream tracking ref (@{u}) points to a different branch (e.g., origin/main).
// This reproduces the bug where "git push origin <branch>" succeeds but disposal
// still reports unpushed commits because @{u} points to origin/main.
func TestCheckGitSafety_PushedToOriginBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a "remote" repo with an initial commit
	tmpDir := t.TempDir()
	remoteDir := gitTestWorkTree(t)

	// Create a local clone
	localDir := filepath.Join(tmpDir, "local")
	runGit(t, tmpDir, "clone", remoteDir, "local")
	runGit(t, localDir, "config", "user.email", "test@test.com")
	runGit(t, localDir, "config", "user.name", "Test")

	// Create a feature branch FROM origin/main (simulating schmux's addWorktree
	// with: git worktree add -b feature/test path origin/main)
	// This sets @{u} to origin/main due to branch.autoSetupMerge
	runGit(t, localDir, "checkout", "-b", "feature/test", "origin/main")

	// Make 3 commits on the feature branch
	writeFile(t, localDir, "file1.txt", "one")
	runGit(t, localDir, "add", ".")
	runGit(t, localDir, "commit", "-m", "commit 1")
	writeFile(t, localDir, "file2.txt", "two")
	runGit(t, localDir, "add", ".")
	runGit(t, localDir, "commit", "-m", "commit 2")
	writeFile(t, localDir, "file3.txt", "three")
	runGit(t, localDir, "add", ".")
	runGit(t, localDir, "commit", "-m", "commit 3")

	// Push the feature branch to origin (without -u, so @{u} stays origin/main)
	runGit(t, localDir, "push", "origin", "feature/test")

	// Set up the workspace manager
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath)
	m := New(cfg, st, statePath)

	// Add workspace to state
	w := state.Workspace{
		ID:     "test-001",
		Repo:   remoteDir,
		Branch: "feature/test",
		Path:   localDir,
	}
	st.AddWorkspace(w)

	// Run checkGitSafety - should be Safe since all commits are pushed
	ctx := context.Background()
	safety, err := m.checkGitSafety(ctx, "test-001")
	if err != nil {
		t.Fatalf("checkGitSafety() error: %v", err)
	}

	if !safety.Safe {
		t.Errorf("checkGitSafety() Safe = false, want true (commits are pushed to origin/feature/test)\n"+
			"Reason: %s\nAheadCommits: %d", safety.Reason, safety.AheadCommits)
	}
	if safety.AheadCommits != 0 {
		t.Errorf("checkGitSafety() AheadCommits = %d, want 0", safety.AheadCommits)
	}
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
