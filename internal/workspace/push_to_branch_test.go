package workspace

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// setupPushTest creates a bare remote repo and a clone for testing push scenarios.
// Returns (remoteDir, cloneDir, manager, state, workspaceID).
func setupPushTest(t *testing.T) (string, string, *Manager, *state.State, string) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()

	// Create bare remote repo
	remoteDir := filepath.Join(tmpDir, "remote.git")
	runGit(t, tmpDir, "init", "--bare", remoteDir)

	// Create a working clone
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(t, tmpDir, "clone", remoteDir, "clone")
	runGit(t, cloneDir, "config", "user.email", "test@test.com")
	runGit(t, cloneDir, "config", "user.name", "Test")

	// Make initial commit on main (explicitly create branch to avoid relying on git's default)
	runGit(t, cloneDir, "checkout", "-b", "main")
	writeFile(t, cloneDir, "README.md", "test")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "initial")
	runGit(t, cloneDir, "push", "origin", "main")

	// Set up workspace manager
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	workspaceID := "test-push-001"

	return remoteDir, cloneDir, m, st, workspaceID
}

// TestPushToBranch_NoRemoteBranch pushes when origin doesn't have the branch yet.
// confirm=false is fine since no confirmation is needed for new branches.
func TestPushToBranch_NoRemoteBranch(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)

	// Create a feature branch with a commit
	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "feature.txt", "feature work")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "feature commit")

	// Add workspace to state
	st.AddWorkspace(state.Workspace{
		ID:     workspaceID,
		Repo:   remoteDir,
		Branch: "feature",
		Path:   cloneDir,
	})

	result, err := m.PushToBranch(context.Background(), workspaceID, false)

	if err != nil {
		t.Fatalf("PushToBranch() error: %v", err)
	}
	if !result.Success {
		t.Errorf("PushToBranch() should succeed for new branch, got: %+v", result)
	}
}

// TestPushToBranch_RemoteCaughtUp pushes when local is ahead (fast-forward).
// confirm=false is fine since no confirmation is needed for fast-forward.
func TestPushToBranch_RemoteCaughtUp(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)

	// Create feature branch and push it
	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "feature.txt", "v1")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "feature commit")
	runGit(t, cloneDir, "push", "origin", "feature")

	// Add another commit locally
	writeFile(t, cloneDir, "feature.txt", "v2")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "second commit")

	// Add workspace to state
	st.AddWorkspace(state.Workspace{
		ID:     workspaceID,
		Repo:   remoteDir,
		Branch: "feature",
		Path:   cloneDir,
	})

	result, err := m.PushToBranch(context.Background(), workspaceID, false)

	if err != nil {
		t.Fatalf("PushToBranch() error: %v", err)
	}
	if !result.Success {
		t.Errorf("PushToBranch() should succeed when ahead, got: %+v", result)
	}
}

// TestPushToBranch_RemoteHasNewerCommits fails when local is behind origin.
// Verifies that a helpful message is returned telling user to pull/merge first.
func TestPushToBranch_RemoteHasNewerCommits(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)

	// Create feature branch and push it
	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "feature.txt", "v1")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "feature commit")
	runGit(t, cloneDir, "push", "origin", "feature")

	// Create a second clone to simulate someone else pushing
	otherDir := filepath.Join(t.TempDir(), "other")
	runGit(t, filepath.Dir(otherDir), "clone", remoteDir, "other")
	runGit(t, otherDir, "config", "user.email", "other@test.com")
	runGit(t, otherDir, "config", "user.name", "Other")
	runGit(t, otherDir, "checkout", "feature")
	writeFile(t, otherDir, "other.txt", "other work")
	runGit(t, otherDir, "add", ".")
	runGit(t, otherDir, "commit", "-m", "other commit")
	runGit(t, otherDir, "push", "origin", "feature")

	// Now original clone is behind - fetch to update tracking refs
	runGit(t, cloneDir, "fetch", "origin")

	// Add workspace to state
	st.AddWorkspace(state.Workspace{
		ID:     workspaceID,
		Repo:   remoteDir,
		Branch: "feature",
		Path:   cloneDir,
	})

	result, err := m.PushToBranch(context.Background(), workspaceID, false)

	if err != nil {
		t.Fatalf("PushToBranch() error: %v", err)
	}
	if result.Success {
		t.Errorf("PushToBranch() should fail when behind, got: %+v", result)
	}
	if result.NeedsConfirm {
		t.Errorf("PushToBranch() should not need confirm when behind (should just fail), got: %+v", result)
	}
	if result.Message == "" {
		t.Errorf("PushToBranch() should return helpful message when behind, got: %+v", result)
	}
	if !strings.Contains(result.Message, "behind") || !strings.Contains(result.Message, "pull") {
		t.Errorf("PushToBranch() message should mention 'behind' and 'pull', got: %q", result.Message)
	}
}

// TestPushToBranch_RebasedSamePatches_NeedsConfirm returns needs_confirm when rebase causes divergence.
func TestPushToBranch_RebasedSamePatches_NeedsConfirm(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)

	// Create feature branch with commits and push
	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "a.txt", "a")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "commit a")
	writeFile(t, cloneDir, "b.txt", "b")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "commit b")
	runGit(t, cloneDir, "push", "origin", "feature")

	// Add commit to main on remote (simulate main advancing)
	runGit(t, cloneDir, "checkout", "main")
	writeFile(t, cloneDir, "main.txt", "main update")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "main update")
	runGit(t, cloneDir, "push", "origin", "main")

	// Rebase feature onto main
	runGit(t, cloneDir, "checkout", "feature")
	runGit(t, cloneDir, "fetch", "origin")
	runGit(t, cloneDir, "rebase", "origin/main")

	// Add workspace to state
	st.AddWorkspace(state.Workspace{
		ID:     workspaceID,
		Repo:   remoteDir,
		Branch: "feature",
		Path:   cloneDir,
	})

	// Without confirm, should return needs_confirm=true
	result, err := m.PushToBranch(context.Background(), workspaceID, false)

	if err != nil {
		t.Fatalf("PushToBranch() error: %v", err)
	}
	if result.Success {
		t.Errorf("PushToBranch() should not succeed without confirm, got: %+v", result)
	}
	if !result.NeedsConfirm {
		t.Errorf("PushToBranch() should return NeedsConfirm=true after rebase, got: %+v", result)
	}
	if len(result.DivergedCommits) == 0 {
		t.Errorf("PushToBranch() should return diverged commits, got: %+v", result)
	}
}

// TestPushToBranch_RebasedSamePatches_Confirmed pushes after rebase with confirm=true.
func TestPushToBranch_RebasedSamePatches_Confirmed(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)

	// Create feature branch with commits and push
	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "a.txt", "a")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "commit a")
	writeFile(t, cloneDir, "b.txt", "b")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "commit b")
	runGit(t, cloneDir, "push", "origin", "feature")

	// Add commit to main on remote (simulate main advancing)
	runGit(t, cloneDir, "checkout", "main")
	writeFile(t, cloneDir, "main.txt", "main update")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "main update")
	runGit(t, cloneDir, "push", "origin", "main")

	// Rebase feature onto main
	runGit(t, cloneDir, "checkout", "feature")
	runGit(t, cloneDir, "fetch", "origin")
	runGit(t, cloneDir, "rebase", "origin/main")

	// Add workspace to state
	st.AddWorkspace(state.Workspace{
		ID:     workspaceID,
		Repo:   remoteDir,
		Branch: "feature",
		Path:   cloneDir,
	})

	// With confirm=true, should push successfully
	result, err := m.PushToBranch(context.Background(), workspaceID, true)

	if err != nil {
		t.Fatalf("PushToBranch() error: %v", err)
	}
	if !result.Success {
		t.Errorf("PushToBranch() should succeed with confirm=true after rebase, got: %+v", result)
	}
}

// TestPushToBranch_RebasedWithExtraOriginCommits_NeedsConfirm returns needs_confirm with commits that would be lost.
func TestPushToBranch_RebasedWithExtraOriginCommits_NeedsConfirm(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)

	// Create feature branch with commits and push
	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "a.txt", "a")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "commit a")
	runGit(t, cloneDir, "push", "origin", "feature")

	// Add commit to main (so rebase will change commit hashes)
	runGit(t, cloneDir, "checkout", "main")
	writeFile(t, cloneDir, "main.txt", "main update")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "main update")
	runGit(t, cloneDir, "push", "origin", "main")

	// Someone else adds a commit to origin/feature
	otherDir := filepath.Join(t.TempDir(), "other")
	runGit(t, filepath.Dir(otherDir), "clone", remoteDir, "other")
	runGit(t, otherDir, "config", "user.email", "other@test.com")
	runGit(t, otherDir, "config", "user.name", "Other")
	runGit(t, otherDir, "checkout", "feature")
	writeFile(t, otherDir, "other.txt", "other work")
	runGit(t, otherDir, "add", ".")
	runGit(t, otherDir, "commit", "-m", "other commit")
	runGit(t, otherDir, "push", "origin", "feature")

	// Meanwhile, we rebase our feature onto updated main
	runGit(t, cloneDir, "checkout", "feature")
	runGit(t, cloneDir, "fetch", "origin")
	runGit(t, cloneDir, "rebase", "origin/main")

	// Add workspace to state
	st.AddWorkspace(state.Workspace{
		ID:     workspaceID,
		Repo:   remoteDir,
		Branch: "feature",
		Path:   cloneDir,
	})

	// Without confirm, should return needs_confirm with diverged commits
	result, err := m.PushToBranch(context.Background(), workspaceID, false)

	if err != nil {
		t.Fatalf("PushToBranch() error: %v", err)
	}
	if result.Success {
		t.Errorf("PushToBranch() should not succeed without confirm, got: %+v", result)
	}
	if !result.NeedsConfirm {
		t.Errorf("PushToBranch() should return NeedsConfirm=true, got: %+v", result)
	}
	if len(result.DivergedCommits) == 0 {
		t.Errorf("PushToBranch() should return diverged commits, got: %+v", result)
	}
	// The "other commit" should be listed as diverged
	foundOther := false
	for _, c := range result.DivergedCommits {
		if strings.Contains(c, "other commit") {
			foundOther = true
			break
		}
	}
	if !foundOther {
		t.Errorf("PushToBranch() diverged commits should include 'other commit', got: %v", result.DivergedCommits)
	}
}

// TestPushToBranch_RebasedWithExtraOriginCommits_Confirmed pushes with confirm=true, overwriting origin commits.
func TestPushToBranch_RebasedWithExtraOriginCommits_Confirmed(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)

	// Create feature branch with commits and push
	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "a.txt", "a")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "commit a")
	runGit(t, cloneDir, "push", "origin", "feature")

	// Add commit to main (so rebase will change commit hashes)
	runGit(t, cloneDir, "checkout", "main")
	writeFile(t, cloneDir, "main.txt", "main update")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "main update")
	runGit(t, cloneDir, "push", "origin", "main")

	// Someone else adds a commit to origin/feature
	otherDir := filepath.Join(t.TempDir(), "other")
	runGit(t, filepath.Dir(otherDir), "clone", remoteDir, "other")
	runGit(t, otherDir, "config", "user.email", "other@test.com")
	runGit(t, otherDir, "config", "user.name", "Other")
	runGit(t, otherDir, "checkout", "feature")
	writeFile(t, otherDir, "other.txt", "other work")
	runGit(t, otherDir, "add", ".")
	runGit(t, otherDir, "commit", "-m", "other commit")
	runGit(t, otherDir, "push", "origin", "feature")

	// Meanwhile, we rebase our feature onto updated main
	runGit(t, cloneDir, "checkout", "feature")
	runGit(t, cloneDir, "fetch", "origin")
	runGit(t, cloneDir, "rebase", "origin/main")

	// Add workspace to state
	st.AddWorkspace(state.Workspace{
		ID:     workspaceID,
		Repo:   remoteDir,
		Branch: "feature",
		Path:   cloneDir,
	})

	// With confirm=true, should push (overwriting the "other" commit)
	result, err := m.PushToBranch(context.Background(), workspaceID, true)

	if err != nil {
		t.Fatalf("PushToBranch() error: %v", err)
	}
	if !result.Success {
		t.Errorf("PushToBranch() should succeed with confirm=true, got: %+v", result)
	}
}
