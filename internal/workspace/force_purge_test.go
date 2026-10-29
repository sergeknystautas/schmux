package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
)

// localBranchExists reports whether refs/heads/<branch> exists in repoPath.
func localBranchExists(t *testing.T, repoPath, branch string) bool {
	t.Helper()
	return gitErrIn(t, repoPath, "show-ref", "--verify", "--quiet", "refs/heads/"+branch) == nil
}

func TestDisposeForcePurge_RemovesWorkspaceAndLocalBranchButNotRemote(t *testing.T) {
	tests := []struct {
		name    string
		recycle bool
	}{
		{"recycling off", false},
		{"recycling on is bypassed", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, st, basePath, wsPath, remotePath := deleteWorktreeFixture(t)
			m.config.RecycleWorkspaces = tt.recycle
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
			if localBranchExists(t, basePath, "feature/x") {
				t.Fatal("local branch feature/x survived even though the remote has it")
			}
			if !remoteHas(t, remotePath, "feature/x") {
				t.Fatal("remote branch feature/x was modified")
			}
		})
	}
}

func TestDisposeForcePurge_KeepsBranchReferencedByAnotherWorktree(t *testing.T) {
	m, st, basePath, wsPath, _ := deleteWorktreeFixture(t)
	// The workspace is really on feature/w, but its record names feature/x,
	// which a second, unrelated worktree has checked out.
	gitIn(t, wsPath, "checkout", "-b", "feature/w")
	other := filepath.Join(filepath.Dir(wsPath), "other-worktree")
	gitIn(t, basePath, "worktree", "add", other, "feature/x")
	w, _ := st.GetWorkspace("talkback-001")
	w.Branch = "feature/x"
	if err := st.UpdateWorkspace(w); err != nil {
		t.Fatal(err)
	}

	if err := m.DisposeForcePurge(context.Background(), "talkback-001"); err != nil {
		t.Fatalf("DisposeForcePurge() error = %v, want nil", err)
	}

	if !localBranchExists(t, basePath, "feature/x") {
		t.Fatal("branch checked out in another worktree was deleted")
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

func TestCleanupFlightBranch_AfterWorkspaceRecordIsGone(t *testing.T) {
	m, st, basePath, wsPath, remotePath := deleteWorktreeFixture(t)
	branch := "schmux/flight/fl-1234"
	gitIn(t, basePath, "branch", branch, "main")
	gitIn(t, basePath, "push", "origin", branch+":"+branch)
	if err := st.AddRepoBase(state.RepoBase{RepoURL: remotePath, Path: basePath, VCS: "git"}); err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveWorkspace("talkback-001"); err != nil {
		t.Fatal(err)
	}
	gitIn(t, basePath, "worktree", "remove", "--force", wsPath)

	if err := m.CleanupFlightBranch(context.Background(), remotePath, branch); err != nil {
		t.Fatalf("CleanupFlightBranch() error = %v", err)
	}
	if localBranchExists(t, basePath, branch) {
		t.Fatal("local flight branch survived cleanup")
	}
	if !remoteHas(t, remotePath, branch) {
		t.Fatal("cleanup changed the remote branch")
	}
	if err := m.CleanupFlightBranch(context.Background(), remotePath, branch); err != nil {
		t.Fatalf("retry CleanupFlightBranch() error = %v", err)
	}
}

func TestCleanupFlightBranch_DeletesSuggestedBranch(t *testing.T) {
	m, st, basePath, wsPath, remotePath := deleteWorktreeFixture(t)
	branch := "fix/password-reset"
	gitIn(t, basePath, "branch", branch, "main")
	gitIn(t, basePath, "push", "origin", branch+":"+branch)
	if err := st.AddRepoBase(state.RepoBase{RepoURL: remotePath, Path: basePath, VCS: "git"}); err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveWorkspace("talkback-001"); err != nil {
		t.Fatal(err)
	}
	gitIn(t, basePath, "worktree", "remove", "--force", wsPath)

	if err := m.CleanupFlightBranch(context.Background(), remotePath, branch); err != nil {
		t.Fatalf("CleanupFlightBranch() error = %v", err)
	}
	if localBranchExists(t, basePath, branch) {
		t.Fatal("local suggested branch survived cleanup")
	}
	if !remoteHas(t, remotePath, branch) {
		t.Fatal("cleanup changed the remote branch")
	}
	if err := m.CleanupFlightBranch(context.Background(), remotePath, branch); err != nil {
		t.Fatalf("retry CleanupFlightBranch() error = %v", err)
	}
}

func TestCleanupFlightBranch_KeepsDefaultAndCheckedOutBranches(t *testing.T) {
	for _, tt := range []struct {
		name   string
		branch string
		setup  func(t *testing.T, m *Manager, basePath, remotePath string)
	}{
		{"the default branch", "main", func(t *testing.T, m *Manager, basePath, remotePath string) {
			// Detach the base checkout so only the default-branch guard keeps main.
			m.setDefaultBranch(remotePath, "main")
			gitIn(t, basePath, "checkout", "--detach")
		}},
		{"a branch checked out in a worktree", "feature/x", func(*testing.T, *Manager, string, string) {}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, st, basePath, _, remotePath := deleteWorktreeFixture(t)
			if err := st.AddRepoBase(state.RepoBase{RepoURL: remotePath, Path: basePath, VCS: "git"}); err != nil {
				t.Fatal(err)
			}
			tt.setup(t, m, basePath, remotePath)

			if err := m.CleanupFlightBranch(context.Background(), remotePath, tt.branch); err != nil {
				t.Fatalf("CleanupFlightBranch() error = %v, want nil", err)
			}
			if !localBranchExists(t, basePath, tt.branch) {
				t.Fatalf("branch %q was deleted", tt.branch)
			}
		})
	}
}
