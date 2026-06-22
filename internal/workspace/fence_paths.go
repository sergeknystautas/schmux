package workspace

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// ExtraWritablePaths returns paths outside workspacePath that the workspace's
// VCS must be able to write. The only such path today is a git worktree's
// shared ".git" common dir, which lives in the main repo, not the worktree —
// without it, `git commit` from a fenced worktree fails. Plain git clones keep
// their store inside the workspace, and sapling (or any non-git workspace)
// returns nil. fence treats the result as opaque; this is the one VCS-aware
// fence input.
func ExtraWritablePaths(workspacePath string) []string {
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	cmd.Dir = workspacePath
	out, err := cmd.Output()
	if err != nil {
		return nil // not a git repo (e.g. sapling) — nothing extra to allow
	}
	common := strings.TrimSpace(string(out))
	if common == "" {
		return nil
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(workspacePath, common)
	}
	common = filepath.Clean(common)

	rel, err := filepath.Rel(workspacePath, common)
	if err != nil || rel == "." || !strings.HasPrefix(rel, "..") {
		return nil // common dir is inside the workspace (plain clone)
	}
	return []string{common}
}
