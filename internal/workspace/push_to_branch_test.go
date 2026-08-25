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

	// Create bare remote from template repo
	remoteDir := filepath.Join(tmpDir, "remote.git")
	runGit(t, tmpDir, "clone", "--bare", templateRepoDir, remoteDir)

	// Create a working clone
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(t, tmpDir, "clone", remoteDir, "clone")

	// Set up workspace manager
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{}
	cfg.WorkspacePath = tmpDir
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

	result, err := m.PushToBranch(context.Background(), workspaceID, false, "", "")

	if err != nil {
		t.Fatalf("PushToBranch() error: %v", err)
	}
	if !result.Success {
		t.Errorf("PushToBranch() should succeed for new branch, got: %+v", result)
	}
}

// TestPushToBranch_RemoteBranchDeletedOutOfBand pushes after the remote branch
// was deleted elsewhere (another client, GitHub UI), leaving a stale
// origin/<branch> tracking ref in the workspace clone. The stale ref must not
// arm the force-with-lease against a branch that no longer exists - the push
// should recreate the branch instead of failing with "(stale info)".
func TestPushToBranch_RemoteBranchDeletedOutOfBand(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)

	// Create feature branch and push it
	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "feature.txt", "v1")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "feature commit")
	runGit(t, cloneDir, "push", "origin", "feature")

	// Delete the branch on the remote out-of-band, directly in the bare repo,
	// so the workspace clone keeps its stale origin/feature tracking ref.
	runGit(t, remoteDir, "update-ref", "-d", "refs/heads/feature")

	// Add another local commit so there is something to push
	writeFile(t, cloneDir, "feature.txt", "v2")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "second commit")

	st.AddWorkspace(state.Workspace{
		ID:     workspaceID,
		Repo:   remoteDir,
		Branch: "feature",
		Path:   cloneDir,
	})

	result, err := m.PushToBranch(context.Background(), workspaceID, false, "", "")
	if err != nil {
		t.Fatalf("PushToBranch() error: %v", err)
	}
	if !result.Success {
		t.Fatalf("PushToBranch() should recreate a remotely-deleted branch, got: %+v", result)
	}
	localHead := strings.TrimSpace(runGitOut(t, cloneDir, "rev-parse", "HEAD"))
	if got := strings.TrimSpace(runGitOut(t, remoteDir, "rev-parse", "refs/heads/feature")); got != localHead {
		t.Errorf("remote feature = %s, want local HEAD %s", got, localHead)
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

	result, err := m.PushToBranch(context.Background(), workspaceID, false, "", "")

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

	result, err := m.PushToBranch(context.Background(), workspaceID, false, "", "")

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
	result, err := m.PushToBranch(context.Background(), workspaceID, false, "", "")

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
	result, err := m.PushToBranch(context.Background(), workspaceID, true, "", "")

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
	result, err := m.PushToBranch(context.Background(), workspaceID, false, "", "")

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
	result, err := m.PushToBranch(context.Background(), workspaceID, true, "", "")

	if err != nil {
		t.Fatalf("PushToBranch() error: %v", err)
	}
	if !result.Success {
		t.Errorf("PushToBranch() should succeed with confirm=true, got: %+v", result)
	}
}

// PushToBranch on the default branch would force-with-lease push
// origin/<default>, bypassing
// LinearSyncToDefault's fast-forward-only guarantee. Rejected regardless of
// confirm — this closes the only force route to the default branch.
func TestPushToBranch_DefaultBranchRejected(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")

	before := strings.TrimSpace(runGitOut(t, remoteDir, "rev-parse", "main"))
	writeFile(t, cloneDir, "a.txt", "a")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "local commit on main")

	st.AddWorkspace(state.Workspace{
		ID:     workspaceID,
		Repo:   remoteDir,
		Branch: "main",
		Path:   cloneDir,
	})

	for _, confirm := range []bool{false, true} {
		result, err := m.PushToBranch(context.Background(), workspaceID, confirm, "", "")
		if err != nil {
			t.Fatalf("PushToBranch(confirm=%v) error: %v", confirm, err)
		}
		if result.Success {
			t.Errorf("PushToBranch(confirm=%v) must not succeed on the default branch, got: %+v", confirm, result)
		}
		if !strings.Contains(result.Message, "default") {
			t.Errorf("PushToBranch(confirm=%v) message should mention the default branch, got: %q", confirm, result.Message)
		}
	}
	if got := strings.TrimSpace(runGitOut(t, remoteDir, "rev-parse", "main")); got != before {
		t.Errorf("origin/main moved from %s to %s despite rejection", before, got)
	}
}

// TestPushToBranch_ConfirmExpectedLocalMismatch: HEAD moved since review → fail, no push.
func TestPushToBranch_ConfirmExpectedLocalMismatch(t *testing.T) {
	t.Parallel()
	m, st, workspaceID := divergedSetup(t)
	w, _ := st.GetWorkspace(workspaceID)

	// Reviewed HEAD
	headBefore := strings.TrimSpace(runGitOut(t, w.Path, "rev-parse", "HEAD"))
	remoteHead := strings.TrimSpace(runGitOut(t, w.Path, "rev-parse", "origin/feature"))

	// Agent commits after the review
	writeFile(t, w.Path, "late.txt", "late")
	runGit(t, w.Path, "add", ".")
	runGit(t, w.Path, "commit", "-m", "late local commit")

	result, err := m.PushToBranch(context.Background(), workspaceID, true, headBefore, remoteHead)
	if err != nil {
		t.Fatalf("PushToBranch() error: %v", err)
	}
	if result.Success {
		t.Errorf("PushToBranch() should fail when HEAD changed since review, got %+v", result)
	}
	if !strings.Contains(result.Message, "changed since review") {
		t.Errorf("Message should mention 'changed since review', got %q", result.Message)
	}
	// Remote untouched: still one commit ahead of the reviewed remote head
	if got := strings.TrimSpace(runGitOut(t, w.Path, "rev-parse", "origin/feature")); got != remoteHead {
		t.Errorf("origin/feature moved despite failed push: %q → %q", remoteHead, got)
	}
}

// TestPushToBranch_ConfirmStaleLease: origin moved since review → lease fails, no overwrite.
func TestPushToBranch_ConfirmStaleLease(t *testing.T) {
	t.Parallel()
	m, st, workspaceID := divergedSetup(t)
	w, _ := st.GetWorkspace(workspaceID)

	localHead := strings.TrimSpace(runGitOut(t, w.Path, "rev-parse", "HEAD"))
	reviewedRemote := strings.TrimSpace(runGitOut(t, w.Path, "rev-parse", "origin/feature"))

	// Someone pushes again after the review
	remoteDir := w.Repo
	thirdDir := filepath.Join(t.TempDir(), "third")
	runGit(t, filepath.Dir(thirdDir), "clone", remoteDir, "third")
	runGit(t, thirdDir, "config", "user.email", "third@test.com")
	runGit(t, thirdDir, "config", "user.name", "Third")
	runGit(t, thirdDir, "checkout", "feature")
	writeFile(t, thirdDir, "d.txt", "d")
	runGit(t, thirdDir, "add", ".")
	runGit(t, thirdDir, "commit", "-m", "commit d")
	runGit(t, thirdDir, "push", "origin", "feature")

	result, err := m.PushToBranch(context.Background(), workspaceID, true, localHead, reviewedRemote)
	if err == nil && result.Success {
		t.Fatalf("PushToBranch() should fail with a stale lease, got %+v", result)
	}
	// The unreviewed commit d must still be on origin
	if got := strings.TrimSpace(runGitOut(t, w.Path, "log", "-1", "--format=%s", "refs/remotes/origin/feature")); got != "commit d" {
		// fetch fresh to check (push attempt may have updated refs)
		runGit(t, w.Path, "fetch", "origin")
		got = strings.TrimSpace(runGitOut(t, w.Path, "log", "-1", "--format=%s", "origin/feature"))
		if got != "commit d" {
			t.Errorf("origin/feature head = %q, want %q (unreviewed commit must survive)", got, "commit d")
		}
	}
}

// TestPushToBranch_ConfirmFreshSnapshot: reviewed tuple still accurate → push succeeds.
func TestPushToBranch_ConfirmFreshSnapshot(t *testing.T) {
	t.Parallel()
	m, st, workspaceID := divergedSetup(t)
	w, _ := st.GetWorkspace(workspaceID)

	localHead := strings.TrimSpace(runGitOut(t, w.Path, "rev-parse", "HEAD"))
	remoteHead := strings.TrimSpace(runGitOut(t, w.Path, "rev-parse", "origin/feature"))

	result, err := m.PushToBranch(context.Background(), workspaceID, true, localHead, remoteHead)
	if err != nil {
		t.Fatalf("PushToBranch() error: %v", err)
	}
	if !result.Success {
		t.Errorf("PushToBranch() should succeed with a fresh snapshot, got %+v", result)
	}
	// origin/feature now matches local HEAD
	runGit(t, w.Path, "fetch", "origin")
	if got := strings.TrimSpace(runGitOut(t, w.Path, "rev-parse", "origin/feature")); got != localHead {
		t.Errorf("origin/feature = %q, want local HEAD %q", got, localHead)
	}
}

// TestPushToBranch_ConfirmBehindStillRejected: behind stays pull/merge-only, even confirmed.
func TestPushToBranch_ConfirmBehindStillRejected(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)

	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "a.txt", "a")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "commit a")
	runGit(t, cloneDir, "push", "origin", "feature")

	otherDir := filepath.Join(t.TempDir(), "other")
	runGit(t, filepath.Dir(otherDir), "clone", remoteDir, "other")
	runGit(t, otherDir, "config", "user.email", "other@test.com")
	runGit(t, otherDir, "config", "user.name", "Other")
	runGit(t, otherDir, "checkout", "feature")
	writeFile(t, otherDir, "c.txt", "c")
	runGit(t, otherDir, "add", ".")
	runGit(t, otherDir, "commit", "-m", "remote commit c")
	runGit(t, otherDir, "push", "origin", "feature")
	runGit(t, cloneDir, "fetch", "origin")

	st.AddWorkspace(state.Workspace{ID: workspaceID, Repo: remoteDir, Branch: "feature", Path: cloneDir})

	localHead := strings.TrimSpace(runGitOut(t, cloneDir, "rev-parse", "HEAD"))
	remoteHead := strings.TrimSpace(runGitOut(t, cloneDir, "rev-parse", "origin/feature"))

	result, err := m.PushToBranch(context.Background(), workspaceID, true, localHead, remoteHead)
	if err != nil {
		t.Fatalf("PushToBranch() error: %v", err)
	}
	if result.Success {
		t.Errorf("PushToBranch() must reject behind even with confirm, got %+v", result)
	}
	if !strings.Contains(result.Message, "behind") {
		t.Errorf("Message should mention 'behind', got %q", result.Message)
	}
}
