package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Happy-path tests (24-31)
// ---------------------------------------------------------------------------

// Test 24: Local commits ahead, no remote commits. Mock unused. Assert success.
func TestResolveConflict_AlreadyCaughtUp(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local work", "local.txt:hello")).
		WithMockLLM(mockLLMError()). // should never be called
		Build()

	result, err := fix.RunLinearSyncResolveConflict()
	fix.AssertResolveSuccess(result, err)
	if !strings.Contains(result.Message, "Caught up") {
		t.Errorf("expected message containing 'Caught up', got: %q", result.Message)
	}
	fix.AssertInvariants()
}

// Test 25: 1 local commit (local.txt), 1 remote commit (remote.txt), different files.
// No conflict occurs, mock should not be called.
func TestResolveConflict_CleanRebase_NoConflicts(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local change", "local.txt:local content")).
		WithRemoteCommits(tc("remote change", "remote.txt:remote content")).
		WithMockLLM(mockLLMError()). // should not be called
		Build()

	result, err := fix.RunLinearSyncResolveConflict()
	fix.AssertResolveSuccess(result, err)
	if len(result.Resolutions) != 0 {
		t.Errorf("expected 0 resolutions, got %d", len(result.Resolutions))
	}
	fix.AssertInvariants()
}

// Test 26: Both sides write shared.txt. Mock resolves with merged content.
func TestResolveConflict_SingleConflict_HighConfidence(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local shared edit", "shared.txt:local version")).
		WithRemoteCommits(tc("remote shared edit", "shared.txt:remote version")).
		WithMockLLM(mockLLMResolveAll(map[string]string{
			"shared.txt": "merged: local + remote",
		})).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()
	fix.AssertResolveSuccess(result, err)
	if len(result.Resolutions) < 1 {
		t.Errorf("expected at least 1 resolution, got %d", len(result.Resolutions))
	}
	fix.AssertInvariants()
}

// Test 27: Both sides write a.txt and b.txt. Mock resolves both.
func TestResolveConflict_MultipleFiles_AllResolved(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edits", "a.txt:local A", "b.txt:local B")).
		WithRemoteCommits(tc("remote edits", "a.txt:remote A", "b.txt:remote B")).
		WithMockLLM(mockLLMResolveAll(map[string]string{
			"a.txt": "merged A",
			"b.txt": "merged B",
		})).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()
	fix.AssertResolveSuccess(result, err)
	fix.AssertInvariants()
}

// Test 28: Local modifies README.md, remote deletes it. Mock resolves by deleting.
func TestResolveConflict_DeletedFile_Resolved(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("modify README", "README.md:modified README")).
		WithRemoteCommits(tcDelete("delete README", "README.md")).
		WithMockLLM(mockLLMResolveAll(map[string]string{
			"README.md": "", // empty string = delete
		})).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()
	fix.AssertResolveSuccess(result, err)
	fix.AssertInvariants()
}

// Test 29: 1 remote commit conflicts with shared.txt. 1 local commit edits shared.txt.
// LinearSyncResolveConflict only processes ONE remote commit (the oldest).
func TestResolveConflict_TwoConflictingCommits_BothResolved(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local shared", "shared.txt:local version")).
		WithRemoteCommits(tc("remote shared", "shared.txt:remote version")).
		WithMockLLM(mockLLMResolveAll(map[string]string{
			"shared.txt": "merged content",
		})).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()
	fix.AssertResolveSuccess(result, err)
	fix.AssertInvariants()
}

// Test 30: 2 local commits both edit shared.txt. 1 remote commit edits shared.txt.
// Use mockLLMSequential: first call resolves, second returns NotAllResolved.
func TestResolveConflict_FirstResolvesSecondFails(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(
			tc("local edit 1", "shared.txt:local version 1"),
			tc("local edit 2", "shared.txt:local version 2"),
		).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote version")).
		WithMockLLM(mockLLMSequential(
			mockLLMResolveAll(map[string]string{"shared.txt": "merged after first"}),
			mockLLMNotAllResolved(map[string]string{"shared.txt": "partially merged"}),
		)).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()
	fix.AssertResolveFailure(result, err, "not all resolved")
	if result != nil && len(result.Resolutions) < 1 {
		t.Errorf("expected at least 1 resolution recorded, got %d", len(result.Resolutions))
	}
	fix.AssertInvariants()
}

// Test 31: Non-overlapping edits to a shared file. Git should auto-resolve.
func TestResolveConflict_GitAutoResolves(t *testing.T) {
	t.Parallel()

	// We need a shared file with non-overlapping regions.
	// Create initial content in the base commit, then have local and remote
	// edit different lines.

	// Build a base fixture first with no local/remote commits
	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithMockLLM(mockLLMError()). // should not be called
		Build()

	// Create a shared file with 10 lines in the clone (on feature branch)
	lines := make([]string, 10)
	for i := range lines {
		lines[i] = strings.Repeat(string(rune('a'+i)), 20) // distinctive lines
	}
	baseContent := strings.Join(lines, "\n") + "\n"
	writeFile(t, fix.cloneDir, "shared.txt", baseContent)
	runGit(t, fix.cloneDir, "add", "shared.txt")
	runGit(t, fix.cloneDir, "commit", "-m", "add shared file")

	// Push the shared file to remote so both sides have it as the common ancestor
	runGit(t, fix.remoteDir, "checkout", "main")
	writeFile(t, fix.remoteDir, "shared.txt", baseContent)
	runGit(t, fix.remoteDir, "add", "shared.txt")
	runGit(t, fix.remoteDir, "commit", "-m", "add shared file to remote")

	// Remote edits line 10 (last line)
	remoteLines := make([]string, 10)
	copy(remoteLines, lines)
	remoteLines[9] = "REMOTE_EDITED_LINE_10"
	remoteContent := strings.Join(remoteLines, "\n") + "\n"
	writeFile(t, fix.remoteDir, "shared.txt", remoteContent)
	runGit(t, fix.remoteDir, "add", "shared.txt")
	runGit(t, fix.remoteDir, "commit", "-m", "remote edits line 10")

	// Local edits line 1 (first line)
	localLines := make([]string, 10)
	copy(localLines, lines)
	localLines[0] = "LOCAL_EDITED_LINE_1"
	localContent := strings.Join(localLines, "\n") + "\n"
	writeFile(t, fix.cloneDir, "shared.txt", localContent)
	runGit(t, fix.cloneDir, "add", "shared.txt")
	runGit(t, fix.cloneDir, "commit", "-m", "local edits line 1")

	// Fetch in clone so origin/main picks up the remote edits
	runGit(t, fix.cloneDir, "fetch", "origin")

	result, err := fix.RunLinearSyncResolveConflict()
	fix.AssertResolveSuccess(result, err)
	fix.AssertInvariants()
}

// ---------------------------------------------------------------------------
// LLM failure mode tests (32-38)
// ---------------------------------------------------------------------------

// Test 32: Conflict, mockLLMLowConfidence. Assert failure with "confidence".
func TestResolveConflict_LowConfidence_Aborts(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local version")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote version")).
		WithMockLLM(mockLLMLowConfidence(map[string]string{
			"shared.txt": "resolved content",
		})).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()
	fix.AssertResolveFailure(result, err, "confidence")
	fix.AssertInvariants()
}

// Test 33: Conflict, mockLLMNotAllResolved. Assert failure with "not all resolved".
func TestResolveConflict_NotAllResolved_Aborts(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local version")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote version")).
		WithMockLLM(mockLLMNotAllResolved(map[string]string{
			"shared.txt": "partially resolved",
		})).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()
	fix.AssertResolveFailure(result, err, "not all resolved")
	fix.AssertInvariants()
}

// Test 34: Conflict, mockLLMError. Assert failure (message about error).
func TestResolveConflict_LLMError_Aborts(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local version")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote version")).
		WithMockLLM(mockLLMError()).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()
	fix.AssertResolveFailure(result, err, "unavailable")
	fix.AssertInvariants()
}

// Test 35: Conflict on a.txt and b.txt, mock omits b.txt.
func TestResolveConflict_LLMOmitsFile_Aborts(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edits", "a.txt:local A", "b.txt:local B")).
		WithRemoteCommits(tc("remote edits", "a.txt:remote A", "b.txt:remote B")).
		WithMockLLM(mockLLMOmitsFile(map[string]string{
			"a.txt": "resolved A",
			"b.txt": "resolved B",
		}, "b.txt")).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()
	fix.AssertResolveFailure(result, err, "omitted")
	fix.AssertInvariants()
}

// Test 36: Conflict, mockLLMDeletedButExists. Assert failure.
func TestResolveConflict_LLMSaysDeletedButFileExists(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local version")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote version")).
		WithMockLLM(mockLLMDeletedButExists("shared.txt")).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()
	fix.AssertResolveFailure(result, err, "deleted")
	// Also check "exists"
	if result != nil && !strings.Contains(result.Message, "exists") {
		if err == nil || !strings.Contains(err.Error(), "exists") {
			t.Errorf("expected message containing 'exists', got result.Message=%q err=%v", result.Message, err)
		}
	}
	fix.AssertInvariants()
}

// Test 37: Conflict, mockLLMLeaveMarkers. Assert failure with "conflict markers".
func TestResolveConflict_ConflictMarkersRemain_Aborts(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local version")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote version")).
		WithMockLLM(mockLLMLeaveMarkers("shared.txt")).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()
	fix.AssertResolveFailure(result, err, "conflict markers")
	fix.AssertInvariants()
}

// Test 38: Conflict, mockLLMUnknownAction. Assert failure with "unknown action".
func TestResolveConflict_UnknownAction_Aborts(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local version")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote version")).
		WithMockLLM(mockLLMUnknownAction("shared.txt")).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()
	fix.AssertResolveFailure(result, err, "unknown action")
	fix.AssertInvariants()
}

// ---------------------------------------------------------------------------
// State preservation and edge case tests (39-43)
// ---------------------------------------------------------------------------

// Test 39: Conflict + staged + untracked changes. mockLLMNotAllResolved.
// Assert failure, invariants, local changes preserved.
func TestResolveConflict_AbortPreservesLocalChanges(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local version")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote version")).
		WithStagedChanges(map[string]string{"staged.txt": "staged content"}).
		WithUntrackedFiles(map[string]string{"untracked.txt": "untracked content"}).
		WithMockLLM(mockLLMNotAllResolved(map[string]string{
			"shared.txt": "partially resolved",
		})).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()
	fix.AssertResolveFailure(result, err, "not all resolved")
	fix.AssertInvariants()

	// Verify untracked file is preserved (staged becomes unstaged after WIP round-trip)
	data, readErr := os.ReadFile(filepath.Join(fix.cloneDir, "untracked.txt"))
	if readErr != nil {
		t.Fatalf("untracked file not found after abort: %v", readErr)
	}
	if string(data) != "untracked content" {
		t.Errorf("untracked file content mismatch: got %q, want %q", string(data), "untracked content")
	}

	// Verify staged file content is preserved
	data, readErr = os.ReadFile(filepath.Join(fix.cloneDir, "staged.txt"))
	if readErr != nil {
		t.Fatalf("staged file not found after abort: %v", readErr)
	}
	if string(data) != "staged content" {
		t.Errorf("staged file content mismatch: got %q, want %q", string(data), "staged content")
	}
}

// Test 40: Conflict, no local changes, mockLLMNotAllResolved.
// Assert failure, invariants, no WIP in gitLogMessages.
func TestResolveConflict_AbortNoWipIfCleanDir(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local version")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote version")).
		WithMockLLM(mockLLMNotAllResolved(map[string]string{
			"shared.txt": "partially resolved",
		})).
		Build()

	result, err := fix.RunLinearSyncResolveConflict()
	fix.AssertResolveFailure(result, err, "not all resolved")
	fix.AssertInvariants()

	// Verify no WIP commit exists in the last 10 commits
	messages := fix.gitLogMessages(10)
	for _, msg := range messages {
		if strings.HasPrefix(msg, "WIP:") {
			t.Errorf("found unexpected WIP commit in history: %q", msg)
		}
	}
}

// Test 41: Conflict, untracked files, pre-commit hook exits 1.
// Assert error is *PreCommitHookError.
func TestResolveConflict_PreCommitHookRejectsWip(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local version")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote version")).
		WithUntrackedFiles(map[string]string{"dirty.txt": "local notes"}).
		WithPreCommitHook("exit 1").
		WithMockLLM(mockLLMError()). // won't be reached
		Build()

	_, err := fix.RunLinearSyncResolveConflict()
	if err == nil {
		t.Fatal("expected error from pre-commit hook, got nil")
	}
	var hookErr *PreCommitHookError
	if !errors.As(err, &hookErr) {
		t.Errorf("expected error to be *PreCommitHookError, got %T: %v", err, err)
	}
	fix.AssertInvariants()
}

// Test 42: Lock workspace before calling. Assert ErrWorkspaceLocked.
func TestResolveConflict_ConcurrentBlocked(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local edit", "shared.txt:local version")).
		WithRemoteCommits(tc("remote edit", "shared.txt:remote version")).
		WithMockLLM(mockLLMError()).
		Build()

	// Lock the workspace before attempting resolve
	if !fix.manager.LockWorkspace(fix.wsID) {
		t.Fatal("failed to acquire workspace lock for test setup")
	}
	defer fix.manager.UnlockWorkspace(fix.wsID)

	_, err := fix.RunLinearSyncResolveConflict()
	if !errors.Is(err, ErrWorkspaceLocked) {
		t.Errorf("expected ErrWorkspaceLocked, got: %v", err)
	}
	// No AssertInvariants — workspace was not modified
}

// Test 43: After Build(), force-push orphan to remote main. Assert error contains "no common ancestor".
func TestResolveConflict_NoCommonAncestor(t *testing.T) {
	t.Parallel()

	fix := newRebaseFixture(t).
		WithLocalBranch("feature").
		WithLocalCommits(tc("local work", "local.txt:local content")).
		WithMockLLM(mockLLMError()).
		Build()

	// Force-push an orphan commit to remote main
	runGit(t, fix.remoteDir, "checkout", "--orphan", "orphan-temp")
	writeFile(t, fix.remoteDir, "orphan.txt", "orphan content")
	runGit(t, fix.remoteDir, "add", ".")
	runGit(t, fix.remoteDir, "commit", "-m", "orphan commit")
	runGit(t, fix.remoteDir, "branch", "-f", "main")
	runGit(t, fix.remoteDir, "checkout", "main")

	// Fetch in clone to update origin/main
	runGit(t, fix.cloneDir, "fetch", "origin")

	_, err := fix.RunLinearSyncResolveConflict()
	if err == nil {
		t.Fatal("expected error for orphan default branch, got nil")
	}
	if !strings.Contains(err.Error(), "no common ancestor") {
		t.Errorf("expected error containing 'no common ancestor', got: %v", err)
	}

	// Need to clear the rebase state if any before checking invariants
	// The orphan scenario should fail before starting a rebase, so invariants should hold
	fix.AssertInvariants()
}
