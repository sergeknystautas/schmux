package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// deleteFixture builds a workspace on feature/x with a bare origin that already
// has main and feature/x. Returns the manager and the bare remote's path.
func deleteFixture(t *testing.T) (m *Manager, st *state.State, wsPath, remotePath string) {
	t.Helper()
	tmpDir := t.TempDir()
	remotePath = filepath.Join(tmpDir, "remote.git")
	if err := os.MkdirAll(remotePath, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, remotePath, "init", "--bare", "-b", "main")

	statePath := filepath.Join(tmpDir, "state.json")
	cfg := config.CreateDefault(filepath.Join(tmpDir, "config.json"))
	cfg.WorkspacePath = tmpDir
	cfg.Repos = []config.Repo{{Name: "talkback", URL: remotePath, BarePath: "talkback.git"}}
	st = state.New(statePath, nil)

	wsPath = filepath.Join(tmpDir, "talkback-001")
	if err := os.MkdirAll(wsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wsPath, "init", "-b", "main")
	gitIn(t, wsPath, "config", "user.email", "test@test")
	gitIn(t, wsPath, "config", "user.name", "test")
	gitIn(t, wsPath, "remote", "add", "origin", remotePath)
	gitIn(t, wsPath, "commit", "--allow-empty", "-m", "Initial commit")
	gitIn(t, wsPath, "push", "origin", "main:refs/heads/main")
	gitIn(t, wsPath, "checkout", "-b", "feature/x")
	gitIn(t, wsPath, "commit", "--allow-empty", "-m", "feature work")
	gitIn(t, wsPath, "push", "origin", "feature/x:refs/heads/feature/x")
	gitIn(t, wsPath, "fetch", "origin")

	st.AddWorkspace(state.Workspace{
		ID: "talkback-001", Repo: remotePath, Branch: "feature/x", Path: wsPath,
	})
	m = New(cfg, st, statePath, testLogger())
	return m, st, wsPath, remotePath
}

// deleteWorktreeFixture is deleteFixture but with the workspace as a real git
// worktree of a base clone, which is what production workspaces are. Only this
// shape exercises cleanupLocalBranch, since that runs against the worktree base.
func deleteWorktreeFixture(t *testing.T) (m *Manager, st *state.State, basePath, wsPath, remotePath string) {
	t.Helper()
	tmpDir := t.TempDir()
	remotePath = filepath.Join(tmpDir, "remote.git")
	if err := os.MkdirAll(remotePath, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, remotePath, "init", "--bare", "-b", "main")

	basePath = filepath.Join(tmpDir, "base")
	gitIn(t, tmpDir, "clone", remotePath, basePath)
	gitIn(t, basePath, "config", "user.email", "test@test")
	gitIn(t, basePath, "config", "user.name", "test")
	gitIn(t, basePath, "commit", "--allow-empty", "-m", "init")
	gitIn(t, basePath, "push", "origin", "main:refs/heads/main")
	gitIn(t, basePath, "branch", "feature/x", "main")
	gitIn(t, basePath, "push", "origin", "feature/x:refs/heads/feature/x")
	gitIn(t, basePath, "fetch", "origin")

	wsPath = filepath.Join(tmpDir, "talkback-001")
	gitIn(t, basePath, "worktree", "add", wsPath, "feature/x")
	gitIn(t, wsPath, "commit", "--allow-empty", "-m", "feature work")
	gitIn(t, wsPath, "push", "origin", "feature/x:refs/heads/feature/x")
	gitIn(t, wsPath, "fetch", "origin")

	statePath := filepath.Join(tmpDir, "state.json")
	cfg := config.CreateDefault(filepath.Join(tmpDir, "config.json"))
	cfg.WorkspacePath = tmpDir
	cfg.Repos = []config.Repo{{Name: "talkback", URL: remotePath, BarePath: "talkback.git"}}
	st = state.New(statePath, nil)
	st.AddWorkspace(state.Workspace{
		ID: "talkback-001", Repo: remotePath, Branch: "feature/x", Path: wsPath,
	})
	m = New(cfg, st, statePath, testLogger())
	return m, st, basePath, wsPath, remotePath
}

// remoteHas reports whether the bare remote still has refs/heads/<branch>.
func remoteHas(t *testing.T, remotePath, branch string) bool {
	t.Helper()
	out := gitIn(t, remotePath, "for-each-ref", "--format=%(refname)", "refs/heads/"+branch)
	return out != ""
}

// gitErrIn runs git and returns the error instead of failing the test.
func gitErrIn(t *testing.T, dir string, args ...string) error {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}

func TestDeleteRemoteBranch_RefusesWhenNotMerged(t *testing.T) {
	m, _, _, remotePath := deleteFixture(t)
	// feature/x carries a commit that never reached main.
	err := m.DeleteRemoteBranch(context.Background(), "talkback-001")
	if !errors.Is(err, ErrRemoteBranchNotMerged) {
		t.Fatalf("DeleteRemoteBranch() error = %v, want ErrRemoteBranchNotMerged", err)
	}
	if !remoteHas(t, remotePath, "feature/x") {
		t.Fatal("remote branch was deleted despite unmerged commits")
	}
}

func TestDeleteRemoteBranch_DeletesWhenMerged(t *testing.T) {
	m, _, wsPath, remotePath := deleteFixture(t)
	gitIn(t, wsPath, "push", "origin", "feature/x:refs/heads/main")

	if err := m.DeleteRemoteBranch(context.Background(), "talkback-001"); err != nil {
		t.Fatalf("DeleteRemoteBranch() error = %v, want nil", err)
	}
	if remoteHas(t, remotePath, "feature/x") {
		t.Fatal("remote branch still present after delete")
	}
	// Section 5 of the spec: the delete prunes the local tracking ref, which is
	// what later lets cleanupLocalBranch reap the local branch during disposal.
	if err := gitErrIn(t, wsPath, "show-ref", "--verify", "--quiet", "refs/remotes/origin/feature/x"); err == nil {
		t.Fatal("refs/remotes/origin/feature/x survived the delete")
	}
}

func TestDeleteRemoteBranch_AlreadyGoneSucceeds(t *testing.T) {
	m, _, _, remotePath := deleteFixture(t)
	gitIn(t, remotePath, "update-ref", "-d", "refs/heads/feature/x")

	if err := m.DeleteRemoteBranch(context.Background(), "talkback-001"); err != nil {
		t.Fatalf("DeleteRemoteBranch() on absent branch error = %v, want nil", err)
	}
}

func TestDeleteRemoteBranch_LeaseRejectsConcurrentPush(t *testing.T) {
	m, _, wsPath, remotePath := deleteFixture(t)
	gitIn(t, wsPath, "push", "origin", "feature/x:refs/heads/main")

	// Another clone advances feature/x after our fixture's tracking ref settled.
	other := filepath.Join(t.TempDir(), "other")
	gitIn(t, filepath.Dir(other), "clone", remotePath, other)
	gitIn(t, other, "config", "user.email", "other@test")
	gitIn(t, other, "config", "user.name", "other")
	gitIn(t, other, "checkout", "feature/x")
	gitIn(t, other, "commit", "--allow-empty", "-m", "concurrent work")
	gitIn(t, other, "push", "origin", "feature/x")

	// The fetch inside DeleteRemoteBranch sees the new head, which is no longer
	// contained in main, so containment refuses before the lease is even tried.
	err := m.DeleteRemoteBranch(context.Background(), "talkback-001")
	if err == nil {
		t.Fatal("DeleteRemoteBranch() succeeded despite a concurrent push")
	}
	if !remoteHas(t, remotePath, "feature/x") {
		t.Fatal("remote branch was deleted despite a concurrent push")
	}
}

func TestDeleteRemoteBranch_RefusesUnqualifiedWorkspaces(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(w *state.Workspace)
	}{
		{"default branch", func(w *state.Workspace) { w.Branch = "main" }},
		{"fork branch", func(w *state.Workspace) { w.RemoteBranchIsFork = true }},
		{"remote host", func(w *state.Workspace) { w.RemoteHostID = "host-1" }},
		{"sapling", func(w *state.Workspace) { w.VCS = "sapling" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, st, _, remotePath := deleteFixture(t)
			w, _ := st.GetWorkspace("talkback-001")
			tt.mutate(&w)
			if err := st.UpdateWorkspace(w); err != nil {
				t.Fatal(err)
			}
			err := m.DeleteRemoteBranch(context.Background(), "talkback-001")
			if !errors.Is(err, ErrRemoteBranchNotDeletable) {
				t.Fatalf("DeleteRemoteBranch() error = %v, want ErrRemoteBranchNotDeletable", err)
			}
			if !remoteHas(t, remotePath, "feature/x") {
				t.Fatal("remote branch was deleted for an unqualified workspace")
			}
		})
	}
}

func TestDeleteRemoteBranch_ThenDisposeReapsLocalBranch(t *testing.T) {
	m, _, basePath, wsPath, _ := deleteWorktreeFixture(t)
	gitIn(t, wsPath, "push", "origin", "feature/x:refs/heads/main")

	if err := m.DeleteRemoteBranch(context.Background(), "talkback-001"); err != nil {
		t.Fatal(err)
	}
	// Spec section 5: with recycling off, disposal now reaps the local branch,
	// because cleanupLocalBranch no longer sees a remote branch to preserve it for.
	m.config.RecycleWorkspaces = false
	if err := m.DisposeForce(context.Background(), "talkback-001"); err != nil {
		t.Fatalf("DisposeForce() error = %v", err)
	}
	if out := gitIn(t, basePath, "branch", "--list", "feature/x"); out != "" {
		t.Fatalf("local branch survived disposal: %q", out)
	}
}

func TestDeleteRemoteBranch_RecyclingKeepsWorkspace(t *testing.T) {
	m, st, basePath, wsPath, _ := deleteWorktreeFixture(t)
	gitIn(t, wsPath, "push", "origin", "feature/x:refs/heads/main")
	if err := m.DeleteRemoteBranch(context.Background(), "talkback-001"); err != nil {
		t.Fatal(err)
	}
	// Spec section 5: DisposeForce passes skipRecycling=false, so with recycling
	// on the workspace becomes recyclable and keeps its local branch. This spec
	// deliberately does not override that.
	m.config.RecycleWorkspaces = true
	if err := m.DisposeForce(context.Background(), "talkback-001"); err != nil {
		t.Fatalf("DisposeForce() error = %v", err)
	}
	w, found := st.GetWorkspace("talkback-001")
	if !found {
		t.Fatal("workspace was removed despite recycling being on")
	}
	if w.Status != state.WorkspaceStatusRecyclable {
		t.Fatalf("workspace status = %q, want recyclable", w.Status)
	}
	if out := gitIn(t, basePath, "branch", "--list", "feature/x"); out == "" {
		t.Fatal("local branch was deleted despite recycling being on")
	}
}
