package workspace

import (
	"context"
	"io/fs"
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

// copyDir recursively copies src into dst (which must already exist).
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
	if err != nil {
		t.Fatalf("copyDir: %v", err)
	}
}

// gitTestWorkTree creates a working git tree with an initial commit.
// Returns the path to the repo (auto-cleanup via t.TempDir).
func gitTestWorkTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	copyDir(t, templateRepoDir, dir)
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

// testRepoWithBarePath returns a config.Repo with a unique BarePath per test.
// Uses the test name to prevent collisions when tests run in parallel.
func testRepoWithBarePath(t *testing.T, name, url string) config.Repo {
	t.Helper()
	// Replace slashes in test names to avoid nested directories
	safeName := strings.ReplaceAll(t.Name(), "/", "_")
	return config.Repo{
		Name:     name,
		URL:      url,
		BarePath: safeName + "-" + name + ".git",
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

func TestValidateBranchName(t *testing.T) {
	t.Parallel()
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

		// Valid: uppercase (allowed)
		{"uppercase", "Feature", false},
		{"uppercase mixed", "featureTest", false},

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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())
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
	t.Parallel()
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
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())
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
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a bare "remote" repo from template
	tmpDir := t.TempDir()
	bareDir := filepath.Join(tmpDir, "remote.git")
	runGit(t, tmpDir, "clone", "--bare", templateRepoDir, bareDir)

	// Create a local clone
	localDir := filepath.Join(tmpDir, "local")
	runGit(t, tmpDir, "clone", bareDir, "local")

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

	// Push the feature branch to origin using explicit refspec to avoid
	// push.default=upstream resolving to main. Keep -u off so @{u} stays origin/main.
	runGit(t, localDir, "push", "origin", "feature/test:feature/test")

	// Set up the workspace manager
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	// Add workspace to state
	w := state.Workspace{
		ID:     "test-001",
		Repo:   bareDir,
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

// TestCheckGitSafety_DeletedFilesAreSafe verifies that deleted tracked files
// do not block disposal. Deletions in a worktree are not data loss because
// commits live in the bare clone. This also ensures a partially-deleted
// worktree (from an interrupted git worktree remove) can be re-disposed.
func TestCheckGitSafety_DeletedFilesAreSafe(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tests := []struct {
		name          string
		setup         func(t *testing.T, dir string) // mutate the worktree
		wantSafe      bool
		wantModified  int
		wantUntracked int
	}{
		{
			name: "unstaged deletions only",
			setup: func(t *testing.T, dir string) {
				// Simulate partial worktree removal: delete tracked files from disk
				writeFile(t, dir, "a.txt", "a")
				writeFile(t, dir, "b.txt", "b")
				runGit(t, dir, "add", ".")
				runGit(t, dir, "commit", "-m", "add files")
				os.Remove(filepath.Join(dir, "a.txt"))
				os.Remove(filepath.Join(dir, "b.txt"))
			},
			wantSafe:      true,
			wantModified:  0,
			wantUntracked: 0,
		},
		{
			name: "staged deletions only",
			setup: func(t *testing.T, dir string) {
				writeFile(t, dir, "a.txt", "a")
				runGit(t, dir, "add", ".")
				runGit(t, dir, "commit", "-m", "add file")
				runGit(t, dir, "rm", "a.txt")
			},
			wantSafe:      true,
			wantModified:  0,
			wantUntracked: 0,
		},
		{
			name: "mixed deletions and modifications",
			setup: func(t *testing.T, dir string) {
				writeFile(t, dir, "a.txt", "a")
				writeFile(t, dir, "b.txt", "b")
				runGit(t, dir, "add", ".")
				runGit(t, dir, "commit", "-m", "add files")
				os.Remove(filepath.Join(dir, "a.txt"))         // deletion — safe
				writeFile(t, dir, "b.txt", "modified content") // modification — unsafe
			},
			wantSafe:      false,
			wantModified:  1,
			wantUntracked: 0,
		},
		{
			name: "deletions plus untracked files",
			setup: func(t *testing.T, dir string) {
				writeFile(t, dir, "a.txt", "a")
				runGit(t, dir, "add", ".")
				runGit(t, dir, "commit", "-m", "add file")
				os.Remove(filepath.Join(dir, "a.txt"))          // deletion — safe
				writeFile(t, dir, "untracked.txt", "new stuff") // untracked — unsafe
			},
			wantSafe:      false,
			wantModified:  0,
			wantUntracked: 1,
		},
		{
			name: "clean worktree",
			setup: func(t *testing.T, dir string) {
				// no changes after initial commit
			},
			wantSafe:      true,
			wantModified:  0,
			wantUntracked: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := gitTestWorkTree(t)
			tt.setup(t, dir)

			tmpDir := t.TempDir()
			statePath := filepath.Join(tmpDir, "state.json")
			cfg := &config.Config{WorkspacePath: tmpDir}
			st := state.New(statePath, nil)
			m := New(cfg, st, statePath, testLogger())

			st.AddWorkspace(state.Workspace{
				ID:   "test-001",
				Repo: "test",
				Path: dir,
			})

			safety, err := m.checkGitSafety(context.Background(), "test-001")
			if err != nil {
				t.Fatalf("checkGitSafety() error: %v", err)
			}
			if safety.Safe != tt.wantSafe {
				t.Errorf("Safe = %v, want %v (reason: %s)", safety.Safe, tt.wantSafe, safety.Reason)
			}
			if safety.ModifiedFiles != tt.wantModified {
				t.Errorf("ModifiedFiles = %d, want %d", safety.ModifiedFiles, tt.wantModified)
			}
			if safety.UntrackedFiles != tt.wantUntracked {
				t.Errorf("UntrackedFiles = %d, want %d", safety.UntrackedFiles, tt.wantUntracked)
			}
		})
	}
}

func TestGitRemoteBranchExists(t *testing.T) {
	t.Parallel()
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
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())
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

// gitCommitHash returns the commit hash for a ref in the given directory.
func gitCommitHash(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse %s in %s: %v", ref, dir, err)
	}
	return strings.TrimSpace(string(output))
}

// TestUpdateLocalDefaultBranch_FastForwardsAfterFetch verifies that after new commits
// are pushed to the remote, fetching the bare clone and calling updateLocalDefaultBranch
// advances refs/heads/main to match refs/remotes/origin/main.
func TestUpdateLocalDefaultBranch_FastForwardsAfterFetch(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())
	ctx := context.Background()

	// Create "remote" repo with initial commit
	remoteDir := gitTestWorkTree(t)
	cfg.Repos = []config.Repo{testRepoWithBarePath(t, "test", remoteDir)}

	// Create bare clone (worktree base)
	bareRepoPath, err := m.gitBackend.EnsureRepoBase(ctx, remoteDir, "")
	if err != nil {
		t.Fatalf("ensureWorktreeBase() failed: %v", err)
	}

	// Pre-populate default branch cache
	m.setDefaultBranch(remoteDir, "main")

	// Record the initial local main commit
	initialHash := gitCommitHash(t, bareRepoPath, "refs/heads/main")

	// Add new commits to the remote
	writeFile(t, remoteDir, "new1.txt", "new content 1")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "remote commit 1")
	writeFile(t, remoteDir, "new2.txt", "new content 2")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "remote commit 2")

	remoteMainHash := gitCommitHash(t, remoteDir, "HEAD")

	// Sanity: local main should still be at the initial commit
	if got := gitCommitHash(t, bareRepoPath, "refs/heads/main"); got != initialHash {
		t.Fatalf("local main moved before fetch, expected %s got %s", initialHash, got)
	}

	// Fetch to update origin/main
	if err := m.gitFetch(ctx, bareRepoPath); err != nil {
		t.Fatalf("gitFetch() failed: %v", err)
	}

	// Verify origin/main is updated but local main is still stale
	if got := gitCommitHash(t, bareRepoPath, "refs/remotes/origin/main"); got != remoteMainHash {
		t.Fatalf("origin/main not updated after fetch, expected %s got %s", remoteMainHash, got)
	}
	if got := gitCommitHash(t, bareRepoPath, "refs/heads/main"); got != initialHash {
		t.Fatalf("local main should still be stale after fetch alone, expected %s got %s", initialHash, got)
	}

	// Call updateLocalDefaultBranch — should fast-forward local main
	m.updateLocalDefaultBranch(ctx, "", RefreshTriggerExplicit, bareRepoPath, remoteDir, nil)

	// Verify local main now matches origin/main
	if got := gitCommitHash(t, bareRepoPath, "refs/heads/main"); got != remoteMainHash {
		t.Errorf("updateLocalDefaultBranch() did not advance local main: got %s, want %s", got, remoteMainHash)
	}
}

// TestUpdateLocalDefaultBranch_SkipsWhenCheckedOutInWorktree verifies that
// updateLocalDefaultBranch does NOT update refs/heads/main when the main branch
// is checked out in a worktree (would be unsafe).
func TestUpdateLocalDefaultBranch_SkipsWhenCheckedOutInWorktree(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())
	ctx := context.Background()

	// Create "remote" repo
	remoteDir := gitTestWorkTree(t)
	cfg.Repos = []config.Repo{testRepoWithBarePath(t, "test", remoteDir)}

	// Create bare clone
	bareRepoPath, err := m.gitBackend.EnsureRepoBase(ctx, remoteDir, "")
	if err != nil {
		t.Fatalf("ensureWorktreeBase() failed: %v", err)
	}

	m.setDefaultBranch(remoteDir, "main")

	// Check out main in a worktree
	worktreePath := filepath.Join(tmpDir, "wt-main")
	runGit(t, bareRepoPath, "worktree", "add", worktreePath, "main")

	initialHash := gitCommitHash(t, bareRepoPath, "refs/heads/main")

	// Add commits to remote and fetch
	writeFile(t, remoteDir, "new.txt", "new content")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "remote commit")
	if err := m.gitFetch(ctx, bareRepoPath); err != nil {
		t.Fatalf("gitFetch() failed: %v", err)
	}

	// Call updateLocalDefaultBranch — should skip because main is checked out
	m.updateLocalDefaultBranch(ctx, "", RefreshTriggerExplicit, bareRepoPath, remoteDir, nil)

	// Local main should NOT have moved
	if got := gitCommitHash(t, bareRepoPath, "refs/heads/main"); got != initialHash {
		t.Errorf("updateLocalDefaultBranch() should not update when branch is checked out in worktree: got %s, want %s", got, initialHash)
	}
}

// TestUpdateLocalDefaultBranch_SkipsOnDivergedBranches verifies that
// updateLocalDefaultBranch does NOT update when local and remote have diverged
// (not a fast-forward).
func TestUpdateLocalDefaultBranch_SkipsOnDivergedBranches(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())
	ctx := context.Background()

	// Create "remote" repo
	remoteDir := gitTestWorkTree(t)
	cfg.Repos = []config.Repo{testRepoWithBarePath(t, "test", remoteDir)}

	// Create bare clone
	bareRepoPath, err := m.gitBackend.EnsureRepoBase(ctx, remoteDir, "")
	if err != nil {
		t.Fatalf("ensureWorktreeBase() failed: %v", err)
	}

	m.setDefaultBranch(remoteDir, "main")

	// Create a local-only commit on refs/heads/main in the bare clone (simulate divergence)
	// First, make a commit in a temp worktree on main, then remove the worktree
	divergeWorktree := filepath.Join(tmpDir, "diverge-wt")
	runGit(t, bareRepoPath, "worktree", "add", divergeWorktree, "main")
	writeFile(t, divergeWorktree, "local-only.txt", "local commit")
	runGit(t, divergeWorktree, "add", ".")
	runGit(t, divergeWorktree, "commit", "-m", "local divergent commit")
	runGit(t, bareRepoPath, "worktree", "remove", divergeWorktree)

	localHash := gitCommitHash(t, bareRepoPath, "refs/heads/main")

	// Add different commits to remote and fetch
	writeFile(t, remoteDir, "remote-only.txt", "remote commit")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "remote divergent commit")
	if err := m.gitFetch(ctx, bareRepoPath); err != nil {
		t.Fatalf("gitFetch() failed: %v", err)
	}

	// Call updateLocalDefaultBranch — should skip because branches diverged
	m.updateLocalDefaultBranch(ctx, "", RefreshTriggerExplicit, bareRepoPath, remoteDir, nil)

	// Local main should NOT have moved
	if got := gitCommitHash(t, bareRepoPath, "refs/heads/main"); got != localHash {
		t.Errorf("updateLocalDefaultBranch() should not update on diverged branches: got %s, want %s", got, localHash)
	}
}

// TestUpdateLocalDefaultBranch_NewWorktreeGetsLatestMain is an end-to-end test
// that verifies the full workflow: remote gets new commits → fetch → local main
// is updated → new worktree created on main gets the latest commits.
func TestUpdateLocalDefaultBranch_NewWorktreeGetsLatestMain(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())
	ctx := context.Background()

	// Create "remote" repo
	remoteDir := gitTestWorkTree(t)
	cfg.Repos = []config.Repo{testRepoWithBarePath(t, "test", remoteDir)}

	// Create bare clone
	bareRepoPath, err := m.gitBackend.EnsureRepoBase(ctx, remoteDir, "")
	if err != nil {
		t.Fatalf("ensureWorktreeBase() failed: %v", err)
	}

	m.setDefaultBranch(remoteDir, "main")

	// Add new commits to remote after bare clone was created
	writeFile(t, remoteDir, "after-clone.txt", "added after clone")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "post-clone commit")

	remoteMainHash := gitCommitHash(t, remoteDir, "HEAD")

	// Fetch and update local default branch
	if err := m.gitFetch(ctx, bareRepoPath); err != nil {
		t.Fatalf("gitFetch() failed: %v", err)
	}
	m.updateLocalDefaultBranch(ctx, "", RefreshTriggerExplicit, bareRepoPath, remoteDir, nil)

	// Create a worktree on main — should get the latest commit
	worktreePath := filepath.Join(tmpDir, "wt-main")
	if err := m.gitBackend.CreateWorkspace(ctx, bareRepoPath, "main", worktreePath); err != nil {
		t.Fatalf("addWorktree() failed: %v", err)
	}

	// Verify the worktree HEAD matches the remote's latest main
	worktreeHash := gitCommitHash(t, worktreePath, "HEAD")
	if worktreeHash != remoteMainHash {
		t.Errorf("new worktree on main has stale commit: got %s, want %s", worktreeHash, remoteMainHash)
	}

	// Verify the file from the post-clone commit exists
	afterClonePath := filepath.Join(worktreePath, "after-clone.txt")
	if _, err := os.Stat(afterClonePath); os.IsNotExist(err) {
		t.Error("new worktree on main is missing after-clone.txt — local main was not updated")
	}
}

// TestHasCommonAncestor_NormalBranch verifies that branches with shared history
// return true from hasCommonAncestor.
func TestHasCommonAncestor_NormalBranch(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create repo with initial commit on main
	remoteDir := gitTestWorkTree(t)

	// Create a feature branch (shares history with main)
	runGit(t, remoteDir, "checkout", "-b", "feature")
	writeFile(t, remoteDir, "feature.txt", "feature content")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "feature commit")

	// Clone and set up manager
	tmpDir := t.TempDir()
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(t, tmpDir, "clone", remoteDir, "clone")

	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())
	ctx := context.Background()

	// HEAD is on main, origin/feature shares ancestry
	if !m.hasCommonAncestor(ctx, cloneDir, "origin/feature") {
		t.Error("hasCommonAncestor() returned false for branches with shared history")
	}

	// Also verify against origin/main (trivially the same)
	if !m.hasCommonAncestor(ctx, cloneDir, "origin/main") {
		t.Error("hasCommonAncestor() returned false for origin/main which is HEAD's upstream")
	}
}

// TestHasCommonAncestor_OrphanBranch verifies that an orphan branch (no shared history)
// returns false from hasCommonAncestor.
func TestHasCommonAncestor_OrphanBranch(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create repo with initial commit on main
	remoteDir := gitTestWorkTree(t)

	// Create an orphan branch (no shared history with main)
	runGit(t, remoteDir, "checkout", "--orphan", "orphan-branch")
	writeFile(t, remoteDir, "orphan.txt", "orphan content")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "orphan commit")
	runGit(t, remoteDir, "checkout", "main")

	// Clone and set up manager
	tmpDir := t.TempDir()
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(t, tmpDir, "clone", remoteDir, "clone")

	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())
	ctx := context.Background()

	// HEAD is on main, origin/orphan-branch has no common ancestor
	if m.hasCommonAncestor(ctx, cloneDir, "origin/orphan-branch") {
		t.Error("hasCommonAncestor() returned true for orphan branch with no shared history")
	}
}

func TestCountLinesCapped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		content  string
		maxBytes int
		want     int
	}{
		{
			name:     "simple file with newlines",
			content:  "line1\nline2\nline3\n",
			maxBytes: 1000,
			want:     3,
		},
		{
			name:     "file without trailing newline",
			content:  "line1\nline2\nline3",
			maxBytes: 1000,
			want:     3,
		},
		{
			name:     "empty file",
			content:  "",
			maxBytes: 1000,
			want:     0,
		},
		{
			name:     "single line no newline",
			content:  "hello",
			maxBytes: 1000,
			want:     1,
		},
		{
			name:     "single line with newline",
			content:  "hello\n",
			maxBytes: 1000,
			want:     1,
		},
		{
			name:     "capped before end of file",
			content:  "aaa\nbbb\nccc\nddd\neee\n",
			maxBytes: 8, // only reads "aaa\nbbb\n" (8 bytes)
			want:     2,
		},
		{
			name:     "cap mid-line counts partial line",
			content:  "aaa\nbbbb",
			maxBytes: 6, // reads "aaa\nbb" (6 bytes), partial "bb" counts as a line
			want:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "test.txt")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}
			got, err := countLinesCapped(path, tt.maxBytes)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("countLinesCapped(%q, %d) = %d, want %d", tt.content, tt.maxBytes, got, tt.want)
			}
		})
	}
}

func TestCountLinesCapped_FileNotFound(t *testing.T) {
	t.Parallel()
	_, err := countLinesCapped("/nonexistent/file.txt", 1000)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestGitCheckoutDot_EmptyTree(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "commit", "--allow-empty", "-m", "empty")

	m := &Manager{logger: testLogger()}
	if err := m.gitCheckoutDot(context.Background(), dir); err != nil {
		t.Fatalf("gitCheckoutDot on empty tree should be a no-op, got: %v", err)
	}
}

func TestGitCheckoutDot_StaleIndexLock(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := gitTestWorkTree(t)

	// Create a stale index.lock
	gitDir := filepath.Join(dir, ".git")
	lockPath := filepath.Join(gitDir, "index.lock")
	if err := os.WriteFile(lockPath, nil, 0644); err != nil {
		t.Fatalf("failed to create index.lock: %v", err)
	}

	m := &Manager{logger: testLogger()}
	if err := m.gitCheckoutDot(context.Background(), dir); err != nil {
		t.Fatalf("gitCheckoutDot should recover from stale index.lock, got: %v", err)
	}

	// Lock file should have been removed
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("stale index.lock should have been removed")
	}
}
