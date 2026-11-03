package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// localBranchExists reports whether refs/heads/<branch> exists in repoPath.
func localBranchExists(t *testing.T, repoPath, branch string) bool {
	t.Helper()
	return gitErrIn(t, repoPath, "show-ref", "--verify", "--quiet", "refs/heads/"+branch) == nil
}

// A force purge removes the workspace and nothing else: the local branch is
// kept whether or not the remote has it, and the remote is never touched.
func TestDisposeForcePurge_RemovesWorkspaceAndKeepsEveryBranch(t *testing.T) {
	tests := []struct {
		name            string
		recycle         bool
		remoteHasBranch bool
	}{
		{"recycling off, remote has the branch", false, true},
		{"recycling on is bypassed", true, true},
		{"local-only branch", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, st, basePath, wsPath, remotePath := deleteWorktreeFixture(t)
			m.config.RecycleWorkspaces = tt.recycle
			if !tt.remoteHasBranch {
				// Ordinary dispose reaps a local branch the remote lacks; a
				// flight purge must not.
				gitIn(t, remotePath, "branch", "-D", "feature/x")
				gitIn(t, basePath, "update-ref", "-d", "refs/remotes/origin/feature/x")
			}
			// Uncommitted work must not block a force purge.
			if err := os.WriteFile(filepath.Join(wsPath, "dirty.txt"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}

			if err := m.DisposeForcePurge(context.Background(), "talkback-001"); err != nil {
				t.Fatalf("DisposeForcePurge() error = %v, want nil", err)
			}

			if _, ok := st.GetWorkspace("talkback-001"); ok {
				t.Fatal("workspace still in state; recycling was not bypassed")
			}
			if _, err := os.Stat(wsPath); !os.IsNotExist(err) {
				t.Fatalf("workspace directory still present: stat err = %v", err)
			}
			if !localBranchExists(t, basePath, "feature/x") {
				t.Fatal("local branch feature/x was deleted by the purge")
			}
			if remoteHas(t, remotePath, "feature/x") != tt.remoteHasBranch {
				t.Fatal("remote branch feature/x was modified")
			}
		})
	}
}

func TestDisposeForcePurge_UnknownWorkspace(t *testing.T) {
	m, _, _, _, _ := deleteWorktreeFixture(t)

	if err := m.DisposeForcePurge(context.Background(), "nope-001"); err == nil {
		t.Fatal("DisposeForcePurge() error = nil, want not-found error")
	}
}

func TestDisposeForcePurge_BranchAlreadyGone(t *testing.T) {
	m, st, basePath, wsPath, _ := deleteWorktreeFixture(t)
	// Remove the worktree out from under the manager and delete its branch.
	gitIn(t, basePath, "worktree", "remove", "--force", wsPath)
	gitIn(t, basePath, "branch", "-D", "feature/x")

	if err := m.DisposeForcePurge(context.Background(), "talkback-001"); err != nil {
		t.Fatalf("DisposeForcePurge() error = %v, want nil (idempotent)", err)
	}
	if _, ok := st.GetWorkspace("talkback-001"); ok {
		t.Fatal("workspace still in state")
	}
}
