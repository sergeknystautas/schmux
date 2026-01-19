package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
