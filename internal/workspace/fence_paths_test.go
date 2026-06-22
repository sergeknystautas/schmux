package workspace

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestExtraWritablePathsWorktree(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "main")
	if err := exec.Command("mkdir", main).Run(); err != nil {
		t.Fatal(err)
	}
	gitInit(t, main)

	wt := filepath.Join(root, "wt")
	cmd := exec.Command("git", "worktree", "add", "-q", wt, "-b", "feature")
	cmd.Dir = main
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}

	got := ExtraWritablePaths(wt)
	if len(got) != 1 {
		t.Fatalf("ExtraWritablePaths(worktree) = %v, want one path", got)
	}
	// The shared .git common dir lives in the main repo, outside the worktree.
	if !strings.HasSuffix(got[0], ".git") {
		t.Errorf("path %q does not look like a git common dir", got[0])
	}
	if strings.HasPrefix(got[0], wt) {
		t.Errorf("path %q is inside the worktree; should be outside", got[0])
	}
}

func TestExtraWritablePathsPlainClone(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	if got := ExtraWritablePaths(dir); got != nil {
		t.Errorf("ExtraWritablePaths(plain clone) = %v, want nil", got)
	}
}

func TestExtraWritablePathsNonGit(t *testing.T) {
	if got := ExtraWritablePaths(t.TempDir()); got != nil {
		t.Errorf("ExtraWritablePaths(non-git) = %v, want nil", got)
	}
}
