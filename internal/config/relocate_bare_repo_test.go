package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGitCmd runs a git command in the given directory, fataling on error.
func runGitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s failed: %v\noutput: %s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// createBareRepoWithWorktrees creates a remote repo, clones it bare at basePath/bareName,
// and adds numWorktrees worktrees. Returns the bare repo path and a slice of worktree paths.
func createBareRepoWithWorktrees(t *testing.T, basePath, bareName string, numWorktrees int) (string, []string) {
	t.Helper()

	// Create a "remote" repo with an initial commit so we have something to clone
	remoteDir := filepath.Join(basePath, "remote-origin")
	if err := os.MkdirAll(remoteDir, 0755); err != nil {
		t.Fatalf("failed to create remote dir: %v", err)
	}
	runGitCmd(t, remoteDir, "init", "--bare", "-b", "main")
	runGitCmd(t, remoteDir, "config", "gc.auto", "0")
	runGitCmd(t, remoteDir, "config", "gc.autoDetach", "false")

	// Create a temporary working copy to make an initial commit
	workDir := filepath.Join(basePath, "work-tmp")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("failed to create work dir: %v", err)
	}
	runGitCmd(t, workDir, "init", "-b", "main")
	runGitCmd(t, workDir, "config", "user.email", "test@test.com")
	runGitCmd(t, workDir, "config", "user.name", "Test")
	runGitCmd(t, workDir, "config", "gc.auto", "0")
	runGitCmd(t, workDir, "config", "gc.autoDetach", "false")

	readme := filepath.Join(workDir, "README.md")
	if err := os.WriteFile(readme, []byte("# test repo\n"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	runGitCmd(t, workDir, "add", ".")
	runGitCmd(t, workDir, "commit", "-m", "initial commit")
	runGitCmd(t, workDir, "remote", "add", "origin", remoteDir)
	runGitCmd(t, workDir, "push", "origin", "main")

	// Clone bare into the namespaced path
	bareDir := filepath.Join(basePath, bareName)
	if err := os.MkdirAll(filepath.Dir(bareDir), 0755); err != nil {
		t.Fatalf("failed to create parent for bare: %v", err)
	}
	runGitCmd(t, basePath, "clone", "--bare", remoteDir, bareName)
	runGitCmd(t, bareDir, "config", "gc.auto", "0")
	runGitCmd(t, bareDir, "config", "gc.autoDetach", "false")

	// Add worktrees
	var worktrees []string
	for i := 0; i < numWorktrees; i++ {
		branch := worktreeBranchName(i)
		wtPath := filepath.Join(basePath, "workspaces", branch)
		runGitCmd(t, bareDir, "worktree", "add", "-b", branch, wtPath)
		worktrees = append(worktrees, wtPath)
	}

	return bareDir, worktrees
}

func worktreeBranchName(i int) string {
	return "wt-" + string(rune('a'+i))
}

func TestRelocateBareRepo_RenamesAndFixesWorktrees(t *testing.T) {
	tmpDir := t.TempDir()
	reposDir := filepath.Join(tmpDir, "repos")
	if err := os.MkdirAll(reposDir, 0755); err != nil {
		t.Fatalf("failed to create repos dir: %v", err)
	}

	// Create bare repo at namespaced path: repos/facebook/react.git
	oldBarePath := filepath.Join(reposDir, "facebook", "react.git")
	newBarePath := filepath.Join(reposDir, "react.git")

	bareDir, worktrees := createBareRepoWithWorktrees(t, tmpDir, filepath.Join("repos", "facebook", "react.git"), 2)

	// Sanity: verify old path is what we expect
	if bareDir != oldBarePath {
		t.Fatalf("bareDir = %q, want %q", bareDir, oldBarePath)
	}

	// Verify worktrees work before relocation
	for _, wt := range worktrees {
		runGitCmd(t, wt, "status")
	}

	// Relocate
	err := RelocateBareRepo(oldBarePath, newBarePath)
	if err != nil {
		t.Fatalf("RelocateBareRepo() returned error: %v", err)
	}

	// 1. Old path should not exist
	if _, err := os.Stat(oldBarePath); !os.IsNotExist(err) {
		t.Errorf("old path still exists: %s", oldBarePath)
	}

	// 2. New path should exist
	info, err := os.Stat(newBarePath)
	if err != nil {
		t.Fatalf("new path doesn't exist: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("new path is not a directory")
	}

	// Resolve the new path for comparison — git writes symlink-resolved paths
	// (e.g., on macOS /var -> /private/var).
	resolvedNewBarePath, err := filepath.EvalSymlinks(newBarePath)
	if err != nil {
		t.Fatalf("failed to resolve new path: %v", err)
	}

	// 3. Worktree .git files should point to new path, not old
	for _, wt := range worktrees {
		gitFilePath := filepath.Join(wt, ".git")
		content, err := os.ReadFile(gitFilePath)
		if err != nil {
			t.Fatalf("failed to read .git file at %s: %v", gitFilePath, err)
		}
		line := strings.TrimSpace(string(content))
		if !strings.HasPrefix(line, "gitdir: ") {
			t.Fatalf(".git file has unexpected format: %q", line)
		}
		gitdir := strings.TrimPrefix(line, "gitdir: ")

		if strings.Contains(gitdir, "facebook/react.git") {
			t.Errorf("worktree .git file still references old path: %s", gitdir)
		}
		if !strings.HasPrefix(gitdir, resolvedNewBarePath) {
			t.Errorf("worktree .git file doesn't reference new path: %s (expected prefix %s)", gitdir, resolvedNewBarePath)
		}
	}

	// 4. Git operations should work in worktrees after relocation
	for _, wt := range worktrees {
		runGitCmd(t, wt, "status")
		runGitCmd(t, wt, "log", "--oneline", "-1")
	}
}

func TestRelocateBareRepo_NoBareWorktreesDir(t *testing.T) {
	tmpDir := t.TempDir()
	reposDir := filepath.Join(tmpDir, "repos")

	// Create bare repo with 0 worktrees
	oldBarePath := filepath.Join(reposDir, "facebook", "react.git")
	newBarePath := filepath.Join(reposDir, "react.git")

	bareDir, _ := createBareRepoWithWorktrees(t, tmpDir, filepath.Join("repos", "facebook", "react.git"), 0)
	if bareDir != oldBarePath {
		t.Fatalf("bareDir = %q, want %q", bareDir, oldBarePath)
	}

	// A bare repo with no worktrees shouldn't have a worktrees/ dir, but
	// git may or may not create one. Remove it to guarantee the test scenario.
	os.RemoveAll(filepath.Join(oldBarePath, "worktrees"))

	err := RelocateBareRepo(oldBarePath, newBarePath)
	if err != nil {
		t.Fatalf("RelocateBareRepo() returned error: %v", err)
	}

	// Old path gone, new path exists
	if _, err := os.Stat(oldBarePath); !os.IsNotExist(err) {
		t.Errorf("old path still exists: %s", oldBarePath)
	}
	if _, err := os.Stat(newBarePath); err != nil {
		t.Fatalf("new path doesn't exist: %v", err)
	}

	// Should still be a valid bare repo
	head := runGitCmd(t, newBarePath, "rev-parse", "HEAD")
	if head == "" {
		t.Errorf("bare repo at new path has no HEAD")
	}
}

func TestRelocateBareRepo_TargetExists(t *testing.T) {
	tmpDir := t.TempDir()
	reposDir := filepath.Join(tmpDir, "repos")

	oldBarePath := filepath.Join(reposDir, "facebook", "react.git")
	newBarePath := filepath.Join(reposDir, "react.git")

	bareDir, _ := createBareRepoWithWorktrees(t, tmpDir, filepath.Join("repos", "facebook", "react.git"), 0)
	if bareDir != oldBarePath {
		t.Fatalf("bareDir = %q, want %q", bareDir, oldBarePath)
	}

	// Create something at the target path
	if err := os.MkdirAll(newBarePath, 0755); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}

	err := RelocateBareRepo(oldBarePath, newBarePath)
	if err == nil {
		t.Fatal("RelocateBareRepo() should have returned error when target exists")
	}
	if !strings.Contains(err.Error(), "target path already exists") {
		t.Errorf("error should mention 'target path already exists', got: %v", err)
	}

	// Old path should still exist (no partial work done)
	if _, err := os.Stat(oldBarePath); err != nil {
		t.Errorf("old path should still exist after error: %v", err)
	}
}
