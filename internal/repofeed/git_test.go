//go:build !norepofeed

package repofeed

import (
	"os/exec"
	"testing"
)

// initBareRepo creates a bare git repo for testing.
func initBareRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	return dir
}

func TestReadAllDevFiles_NoBranch(t *testing.T) {
	bareDir := initBareRepo(t)
	g := &GitOps{BareDir: bareDir, Branch: "dev-repofeed"}

	files, err := g.ReadAllDevFiles()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("got %d files, want 0 for nonexistent branch", len(files))
	}
}
