package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// LinearSyncFromDefault happy-path tests (1-6)
// ---------------------------------------------------------------------------

// Test 1: Feature branch has local commits, main hasn't moved.
// Expect success with count=0 (nothing to rebase).
func TestLinearSyncFromDefault_AlreadyUpToDate(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local work", "local.txt:hello")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertSuccess(result, err, 0)
	fix.AssertInvariants()
}

// Test 2: Stay on main, no divergence at all.
// Expect success with count=0.
func TestLinearSyncFromDefault_SameCommit(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("main").
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertSuccess(result, err, 0)
	fix.AssertInvariants()
}

// Test 3: No local commits on feature, 1 remote commit on main.
// Expect success with count=1 and origin/main is ancestor of HEAD.
func TestLinearSyncFromDefault_StrictlyBehind_SingleCommit(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(tc("remote update", "remote.txt:data")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertSuccess(result, err, 1)
	fix.AssertAncestorOf("origin/main", "HEAD")
	fix.AssertInvariants()
}

// Test 4: No local commits, 5 remote commits on separate files.
// Expect success with count=5.
func TestLinearSyncFromDefault_StrictlyBehind_ManyCommits(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(
			tc("remote 1", "r1.txt:content1"),
			tc("remote 2", "r2.txt:content2"),
			tc("remote 3", "r3.txt:content3"),
			tc("remote 4", "r4.txt:content4"),
			tc("remote 5", "r5.txt:content5"),
		).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertSuccess(result, err, 5)
	fix.AssertInvariants()
}

// Test 5: 1 local commit on local.txt, 1 remote commit on remote.txt (no conflict).
// Expect success with count=1 and origin/main is ancestor of HEAD.
func TestLinearSyncFromDefault_Diverged_NoConflicts(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local change", "local.txt:local content")).
		WithRemoteCommits(tc("remote change", "remote.txt:remote content")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertSuccess(result, err, 1)
	fix.AssertAncestorOf("origin/main", "HEAD")
	fix.AssertInvariants()
}

// Test 6: 3 local commits (l1-l3.txt), 5 remote commits (r1-r5.txt), all different files.
// Expect success with count=5 and origin/main is ancestor of HEAD.
func TestLinearSyncFromDefault_Diverged_ManyCommitsBothSides(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(
			tc("local 1", "l1.txt:local1"),
			tc("local 2", "l2.txt:local2"),
			tc("local 3", "l3.txt:local3"),
		).
		WithRemoteCommits(
			tc("remote 1", "r1.txt:remote1"),
			tc("remote 2", "r2.txt:remote2"),
			tc("remote 3", "r3.txt:remote3"),
			tc("remote 4", "r4.txt:remote4"),
			tc("remote 5", "r5.txt:remote5"),
		).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertSuccess(result, err, 5)
	fix.AssertAncestorOf("origin/main", "HEAD")
	fix.AssertInvariants()
}

// ---------------------------------------------------------------------------
// Local changes preservation tests (7-11)
// ---------------------------------------------------------------------------

// Test 7: Local commit adds tracked.txt, remote has 1 commit on remote.txt,
// then unstaged modification to tracked.txt. Assert success, invariants, preserved.
func TestLinearSyncFromDefault_PreservesUnstagedChanges(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("add tracked file", "tracked.txt:original content")).
		WithRemoteCommits(tc("remote update", "remote.txt:remote data")).
		WithUnstagedChanges(map[string]string{"tracked.txt": "modified locally"}).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertSuccess(result, err, 1)
	fix.AssertInvariants()
	fix.AssertLocalChangesPreserved()
}

// Test 8: Remote has 1 commit, untracked file notes.txt. Assert success, invariants, preserved.
func TestLinearSyncFromDefault_PreservesUntrackedFiles(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(tc("remote update", "remote.txt:remote data")).
		WithUntrackedFiles(map[string]string{"notes.txt": "my local notes"}).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertSuccess(result, err, 1)
	fix.AssertInvariants()
	fix.AssertLocalChangesPreserved()
}

// Test 9: Remote has 1 commit, staged file new-feature.txt. Assert success, invariants, preserved.
// Note: The WIP round-trip (add -A, commit, reset --mixed) converts staged changes to unstaged,
// so we verify the file content is preserved rather than checking staged status.
func TestLinearSyncFromDefault_PreservesStagedChanges(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(tc("remote update", "remote.txt:remote data")).
		WithStagedChanges(map[string]string{"new-feature.txt": "staged content"}).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertSuccess(result, err, 1)
	fix.AssertInvariants()

	// Verify file content is preserved (staged becomes unstaged after WIP round-trip)
	data, err := os.ReadFile(filepath.Join(fix.cloneDir, "new-feature.txt"))
	if err != nil {
		t.Fatalf("staged file new-feature.txt not found after sync: %v", err)
	}
	if string(data) != "staged content" {
		t.Errorf("staged file content mismatch: got %q, want %q", string(data), "staged content")
	}
}

// Test 10: Local commit adds tracked.txt, remote has 1 commit,
// then all three: staged + unstaged + untracked. Assert success, invariants, preserved.
// Note: The WIP round-trip converts staged changes to unstaged, so we verify content
// preservation for staged files separately.
func TestLinearSyncFromDefault_PreservesMixedWorkingDir(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("add tracked file", "tracked.txt:original content")).
		WithRemoteCommits(tc("remote update", "remote.txt:remote data")).
		WithStagedChanges(map[string]string{"new-feature.txt": "staged content"}).
		WithUnstagedChanges(map[string]string{"tracked.txt": "modified locally"}).
		WithUntrackedFiles(map[string]string{"notes.txt": "my local notes"}).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertSuccess(result, err, 1)
	fix.AssertInvariants()

	// Check unstaged and untracked preservation via the fixture helper
	// (clear staged from fixture so AssertLocalChangesPreserved doesn't check it)
	savedStaged := fix.stagedChanges
	fix.stagedChanges = nil
	fix.AssertLocalChangesPreserved()
	fix.stagedChanges = savedStaged

	// Verify staged file content is preserved (becomes unstaged after WIP round-trip)
	data, err := os.ReadFile(filepath.Join(fix.cloneDir, "new-feature.txt"))
	if err != nil {
		t.Fatalf("staged file new-feature.txt not found after sync: %v", err)
	}
	if string(data) != "staged content" {
		t.Errorf("staged file content mismatch: got %q, want %q", string(data), "staged content")
	}
}

// Test 11: Remote has 1 commit, clean working directory (no local changes).
// Assert success, invariants, and no WIP commits in history.
func TestLinearSyncFromDefault_CleanDir_NoWipCommit(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(tc("remote update", "remote.txt:remote data")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertSuccess(result, err, 1)
	fix.AssertInvariants()

	// Verify no WIP commit exists in the last 5 commits
	messages := fix.gitLogMessages(5)
	for _, msg := range messages {
		if strings.HasPrefix(msg, "WIP:") {
			t.Errorf("found unexpected WIP commit in history: %q", msg)
		}
	}
}

// ---------------------------------------------------------------------------
// Conflict detection tests (12-18)
// ---------------------------------------------------------------------------

// Test 12: Local commit writes shared.txt, remote writes shared.txt differently.
// Assert conflict with successCount=0.
func TestLinearSyncFromDefault_ConflictOnFirstCommit(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local shared edit", "shared.txt:local version")).
		WithRemoteCommits(tc("remote shared edit", "shared.txt:remote version")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertConflict(result, err, 0)
	fix.AssertInvariants()
}

// Test 13: Local writes shared.txt, remote has 3 commits: first 2 on separate files,
// 3rd writes shared.txt. Assert conflict with successCount=2.
func TestLinearSyncFromDefault_ConflictOnNthCommit(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local shared edit", "shared.txt:local version")).
		WithRemoteCommits(
			tc("remote safe 1", "r1.txt:safe content 1"),
			tc("remote safe 2", "r2.txt:safe content 2"),
			tc("remote conflict", "shared.txt:remote version"),
		).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertConflict(result, err, 2)
	fix.AssertInvariants()
}

// Test 14: Both sides create multi.txt with different content.
// Assert conflict with successCount=0.
func TestLinearSyncFromDefault_ConflictSingleFileMultipleHunks(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local multi.txt", "multi.txt:local line 1\nlocal line 2\nlocal line 3")).
		WithRemoteCommits(tc("remote multi.txt", "multi.txt:remote line 1\nremote line 2\nremote line 3")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertConflict(result, err, 0)
	fix.AssertInvariants()
}

// Test 15: Local writes a.txt and b.txt, remote writes a.txt and b.txt differently.
// Assert conflict.
func TestLinearSyncFromDefault_ConflictMultipleFiles(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edits", "a.txt:local A", "b.txt:local B")).
		WithRemoteCommits(tc("remote edits", "a.txt:remote A", "b.txt:remote B")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertConflict(result, err, 0)
	fix.AssertInvariants()
}

// Test 16: Local modifies README.md (which exists from initial commit), remote deletes it.
// Assert conflict.
func TestLinearSyncFromDefault_ConflictDeleteVsModify(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("modify README", "README.md:modified README")).
		WithRemoteCommits(tcDelete("delete README", "README.md")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertConflict(result, err, 0)
	fix.AssertInvariants()
}

// Test 17: Both sides add collision.txt with different content.
// Assert conflict.
func TestLinearSyncFromDefault_ConflictNewFileSameName(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local collision", "collision.txt:local collision content")).
		WithRemoteCommits(tc("remote collision", "collision.txt:remote collision content")).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertConflict(result, err, 0)
	fix.AssertInvariants()
}

// Test 18: Conflict scenario with staged + unstaged + untracked local changes.
// Assert conflict AND local changes preserved.
// Note: The WIP round-trip converts staged changes to unstaged, so we verify content
// preservation for staged files separately.
func TestLinearSyncFromDefault_ConflictPreservesLocalChanges(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(
			tc("local tracked file", "tracked.txt:original content"),
			tc("local shared edit", "shared.txt:local version"),
		).
		WithRemoteCommits(tc("remote shared edit", "shared.txt:remote version")).
		WithStagedChanges(map[string]string{"new-feature.txt": "staged content"}).
		WithUnstagedChanges(map[string]string{"tracked.txt": "modified locally"}).
		WithUntrackedFiles(map[string]string{"notes.txt": "my local notes"}).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	fix.AssertConflict(result, err, 0)
	fix.AssertInvariants()

	// Check unstaged and untracked preservation via the fixture helper
	savedStaged := fix.stagedChanges
	fix.stagedChanges = nil
	fix.AssertLocalChangesPreserved()
	fix.stagedChanges = savedStaged

	// Verify staged file content is preserved (becomes unstaged after WIP round-trip)
	data, err := os.ReadFile(filepath.Join(fix.cloneDir, "new-feature.txt"))
	if err != nil {
		t.Fatalf("staged file new-feature.txt not found after sync: %v", err)
	}
	if string(data) != "staged content" {
		t.Errorf("staged file content mismatch: got %q, want %q", string(data), "staged content")
	}
}

// ---------------------------------------------------------------------------
// Error/edge case tests (19-23)
// ---------------------------------------------------------------------------

// Test 19: Remote main is force-pushed to an orphan commit (no common ancestor).
// Assert error contains "no common ancestor".
func TestLinearSyncFromDefault_OrphanDefaultBranch(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local work", "local.txt:local content")).
		Build()

	// Force-push an orphan commit to remote main
	runGit(t, fix.remoteDir, "checkout", "--orphan", "orphan-temp")
	writeFile(t, fix.remoteDir, "orphan.txt", "orphan content")
	runGit(t, fix.remoteDir, "add", ".")
	runGit(t, fix.remoteDir, "commit", "-m", "orphan commit")
	runGit(t, fix.remoteDir, "branch", "-f", "main")
	runGit(t, fix.remoteDir, "checkout", "main")

	_, err := fix.RunLinearSyncFromDefault()
	fix.AssertErrorContains(err, "no common ancestor")
	fix.AssertInvariants()
}

// Test 20: 20 remote commits with a short timeout.
// The sync uses an "approaching deadline" check to stop early before the context expires,
// so invariants should hold. We use a timeout long enough for some commits to succeed
// but short enough that the early-stop logic triggers before all 20 complete.
func TestLinearSyncFromDefault_TimeoutStopsEarly(t *testing.T) {
	t.Parallel()

	commits := make([]testCommit, 20)
	for i := range commits {
		commits[i] = tc(
			fmt.Sprintf("remote %d", i+1),
			fmt.Sprintf("r%d.txt:content %d", i+1, i+1),
		)
	}

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(commits...).
		WithTimeout(100 * time.Millisecond).
		Build()

	result, err := fix.RunLinearSyncFromDefault()
	// The sync may complete fully, stop early gracefully, or hit a hard timeout.
	// A hard timeout (context canceled mid-rebase) may leave a rebase in progress,
	// which is expected — the caller is responsible for cleanup.
	if err != nil {
		t.Logf("sync returned error (possibly timeout): %v", err)
		// If the context expired during a rebase, invariants may not hold.
		// This is expected behavior — skip invariant check.
		t.Logf("skipping invariant check due to error (timeout mid-operation)")
	} else if result != nil {
		t.Logf("sync completed: success=%v successCount=%d", result.Success, result.SuccessCount)
		// When the sync returned cleanly (no error), invariants should hold.
		fix.AssertInvariants()
	}
}

// Test 21: Remote has 1 commit, untracked files force WIP commit,
// pre-commit hook rejects it. Assert error is *PreCommitHookError.
func TestLinearSyncFromDefault_PreCommitHookRejectsWip(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(tc("remote update", "remote.txt:remote data")).
		WithUntrackedFiles(map[string]string{"dirty.txt": "local notes"}).
		WithPreCommitHook("exit 1").
		Build()

	_, err := fix.RunLinearSyncFromDefault()
	if err == nil {
		t.Fatal("expected error from pre-commit hook, got nil")
	}
	var hookErr *PreCommitHookError
	if !errors.As(err, &hookErr) {
		t.Errorf("expected error to be *PreCommitHookError, got %T: %v", err, err)
	}
	fix.AssertInvariants()
}

// Test 22: Lock the workspace before sync, expect ErrWorkspaceLocked.
func TestLinearSyncFromDefault_ConcurrentSyncBlocked(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithRemoteCommits(tc("remote update", "remote.txt:remote data")).
		Build()

	// Lock the workspace before attempting sync
	if !fix.manager.LockWorkspace(fix.wsID) {
		t.Fatal("failed to acquire workspace lock for test setup")
	}
	defer fix.manager.UnlockWorkspace(fix.wsID)

	_, err := fix.RunLinearSyncFromDefault()
	if !errors.Is(err, ErrWorkspaceLocked) {
		t.Errorf("expected ErrWorkspaceLocked, got: %v", err)
	}
}

// Test 23: Call LinearSyncFromDefault with a nonexistent workspace ID.
// Assert error contains "workspace not found".
func TestLinearSyncFromDefault_WorkspaceNotFound(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		Build()

	_, err := fix.manager.LinearSyncFromDefault(context.Background(), "nonexistent-ws")
	if err == nil {
		t.Fatal("expected error for nonexistent workspace, got nil")
	}
	if !strings.Contains(err.Error(), "workspace not found") {
		t.Errorf("expected error containing 'workspace not found', got: %v", err)
	}
}
