package lore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initBareRepo creates a bare repo with a CLAUDE.md file for testing.
func initBareRepo(t *testing.T) (bareDir string) {
	t.Helper()
	dir := t.TempDir()

	// Create a normal repo first
	normalDir := filepath.Join(dir, "normal")
	os.MkdirAll(normalDir, 0755)
	run(t, normalDir, "git", "init")
	run(t, normalDir, "git", "config", "user.email", "test@test.com")
	run(t, normalDir, "git", "config", "user.name", "test")
	os.WriteFile(filepath.Join(normalDir, "CLAUDE.md"), []byte("# Project\n"), 0644)
	run(t, normalDir, "git", "add", "CLAUDE.md")
	run(t, normalDir, "git", "commit", "-m", "initial")

	// Clone as bare
	bareDir = filepath.Join(dir, "bare.git")
	run(t, dir, "git", "clone", "--bare", normalDir, bareDir)
	return bareDir
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v: %s", name, args, err, string(output))
	}
}

func TestApplyProposal(t *testing.T) {
	bareDir := initBareRepo(t)
	workDir := t.TempDir() // where temp worktrees go

	proposal := &Proposal{
		ID:   "prop-test-001",
		Repo: "myrepo",
		ProposedFiles: map[string]string{
			"CLAUDE.md": "# Project\n\n## Build\nAlways use go run ./cmd/build-dashboard\n",
		},
	}

	result, err := ApplyProposal(context.Background(), proposal, bareDir, workDir)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if result.Branch == "" {
		t.Error("expected a branch name")
	}

	// Verify the branch exists in the bare repo
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+result.Branch)
	cmd.Dir = bareDir
	if err := cmd.Run(); err != nil {
		t.Errorf("branch %s should exist in bare repo", result.Branch)
	}

	// Verify the file content on the branch
	showCmd := exec.Command("git", "show", result.Branch+":CLAUDE.md")
	showCmd.Dir = bareDir
	output, err := showCmd.Output()
	if err != nil {
		t.Fatalf("git show failed: %v", err)
	}
	if string(output) != "# Project\n\n## Build\nAlways use go run ./cmd/build-dashboard\n" {
		t.Errorf("unexpected file content on branch: %s", string(output))
	}

	// Verify temp worktree was cleaned up
	entries, _ := os.ReadDir(workDir)
	if len(entries) != 0 {
		t.Errorf("expected temp worktree to be cleaned up, found %d entries", len(entries))
	}
}
