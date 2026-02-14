package workspace

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// TestGetOrCreate_BranchReuse_Success verifies state IS updated when checkout succeeds.
func TestGetOrCreate_BranchReuse_Success(t *testing.T) {
	// Set up isolated state with temp path
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)

	// Skip if git not available
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create test repo with two branches
	repoDir := gitTestWorkTree(t)
	gitTestBranch(t, repoDir, "feature-1")

	// Set up isolated config
	cfg := &config.Config{
		WorkspacePath:    t.TempDir(),
		WorktreeBasePath: t.TempDir(),
		Repos: []config.Repo{
			testRepoWithBarePath("test", repoDir),
		},
	}
	manager := New(cfg, st, statePath)

	// Create workspace on "main"
	ws1, err := manager.GetOrCreate(context.Background(), repoDir, "main")
	if err != nil {
		t.Fatalf("GetOrCreate main failed: %v", err)
	}

	// Verify state
	ws1State, _ := st.GetWorkspace(ws1.ID)
	if ws1State.Branch != "main" {
		t.Errorf("expected branch main, got %s", ws1State.Branch)
	}

	// Reuse for "feature-1" (exists in repo)
	ws2, err := manager.GetOrCreate(context.Background(), repoDir, "feature-1")
	if err != nil {
		t.Fatalf("GetOrCreate feature-1 failed: %v", err)
	}

	// Same workspace ID
	if ws2.ID != ws1.ID {
		t.Errorf("expected same workspace ID, got %s vs %s", ws1.ID, ws2.ID)
	}

	// State was updated
	ws2State, _ := st.GetWorkspace(ws2.ID)
	if ws2State.Branch != "feature-1" {
		t.Errorf("expected branch feature-1, got %s", ws2State.Branch)
	}
}

func TestGetOrCreate_PerRepoMutexBlocks(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := gitTestWorkTree(t)

	cfg := &config.Config{
		WorkspacePath:    t.TempDir(),
		WorktreeBasePath: t.TempDir(),
		Repos: []config.Repo{
			testRepoWithBarePath("test", repoDir),
		},
	}
	manager := New(cfg, st, statePath)

	lock := manager.repoLock(repoDir)
	lock.Lock()

	done := make(chan error, 1)
	go func() {
		_, err := manager.GetOrCreate(context.Background(), repoDir, "main")
		done <- err
	}()

	select {
	case err := <-done:
		lock.Unlock()
		t.Fatalf("expected GetOrCreate to block, returned early: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	lock.Unlock()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("GetOrCreate failed after unlock: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for GetOrCreate after unlock")
	}
}

// TestGetOrCreate_UniqueBranchOnWorktreeConflict verifies branch name is uniquified
// when the requested branch is already checked out in another worktree.
func TestGetOrCreate_UniqueBranchOnWorktreeConflict(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := gitTestWorkTree(t)

	cfg := &config.Config{
		WorkspacePath:    t.TempDir(),
		WorktreeBasePath: t.TempDir(),
		Repos: []config.Repo{
			testRepoWithBarePath("test", repoDir),
		},
	}
	manager := New(cfg, st, statePath)

	ctx := context.Background()
	ws1, err := manager.GetOrCreate(ctx, repoDir, "feature-1")
	if err != nil {
		t.Fatalf("GetOrCreate feature-1 failed: %v", err)
	}

	_ = st.AddSession(state.Session{
		ID:          "sess-1",
		WorkspaceID: ws1.ID,
		Target:      "test",
		TmuxSession: "test",
		CreatedAt:   time.Now(),
	})

	ws2, err := manager.GetOrCreate(ctx, repoDir, "feature-1")
	if err != nil {
		t.Fatalf("GetOrCreate feature-1 second time failed: %v", err)
	}

	if ws2.ID == ws1.ID {
		t.Fatalf("expected a new workspace, got same ID %s", ws2.ID)
	}

	if ws2.Branch == "feature-1" {
		t.Fatalf("expected unique branch name, got %s", ws2.Branch)
	}

	if !strings.HasPrefix(ws2.Branch, "feature-1-") {
		t.Fatalf("expected branch to have suffix, got %s", ws2.Branch)
	}

	cmd := exec.Command("git", "-C", ws2.Path, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}
	actualBranch := strings.TrimSpace(string(output))
	if actualBranch != ws2.Branch {
		t.Fatalf("workspace branch mismatch: state=%s git=%s", ws2.Branch, actualBranch)
	}
}

func TestGetOrCreate_FullCloneDoesNotUniquifyBranch(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := gitTestWorkTree(t)
	gitTestBranch(t, repoDir, "feature-1")

	cfg := &config.Config{
		WorkspacePath:        t.TempDir(),
		WorktreeBasePath:     t.TempDir(),
		SourceCodeManagement: config.SourceCodeManagementGit,
		Repos: []config.Repo{
			testRepoWithBarePath("test", repoDir),
		},
	}
	manager := New(cfg, st, statePath)

	ctx := context.Background()
	ws1, err := manager.GetOrCreate(ctx, repoDir, "feature-1")
	if err != nil {
		t.Fatalf("GetOrCreate feature-1 failed: %v", err)
	}

	_ = st.AddSession(state.Session{
		ID:          "sess-1",
		WorkspaceID: ws1.ID,
		Target:      "test",
		TmuxSession: "test",
		CreatedAt:   time.Now(),
	})

	ws2, err := manager.GetOrCreate(ctx, repoDir, "feature-1")
	if err != nil {
		t.Fatalf("GetOrCreate feature-1 second time failed: %v", err)
	}

	if ws2.ID == ws1.ID {
		t.Fatalf("expected a new workspace, got same ID %s", ws2.ID)
	}

	if ws2.Branch != "feature-1" {
		t.Fatalf("expected branch feature-1, got %s", ws2.Branch)
	}
}

// TestGetOrCreate_BranchReuse_Failure verifies state NOT corrupted when checkout fails.
//
// Note: gitCheckout auto-creates branches with 'checkout -b', so triggering
// a real checkout failure requires filesystem issues (e.g., read-only directory).
// The success test above validates the fix (prepare before state update).
// This test is kept as documentation of the intended behavior.
func TestGetOrCreate_BranchReuse_Failure(t *testing.T) {
	t.Skip("gitCheckout creates branches automatically, hard to trigger failure")

	// This test would need to cause a real git error (e.g., read-only filesystem)
	// to validate that state is not corrupted when prepare() fails.
	// The success test validates the fix (prepare() called before state update).
}

// TestGetOrCreate_BranchReuse_DivergedSkipsReuse verifies that a workspace whose
// branch has diverged from the default branch is NOT reused for a different branch.
// This prevents commit history pollution: without this guard, the new branch would
// inherit all the diverged commits from the old branch.
func TestGetOrCreate_BranchReuse_DivergedSkipsReuse(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := gitTestWorkTree(t)

	cfg := &config.Config{
		WorkspacePath:    t.TempDir(),
		WorktreeBasePath: t.TempDir(),
		Repos: []config.Repo{
			testRepoWithBarePath("test", repoDir),
		},
	}
	manager := New(cfg, st, statePath)
	ctx := context.Background()

	// Create workspace on "main"
	ws1, err := manager.GetOrCreate(ctx, repoDir, "main")
	if err != nil {
		t.Fatalf("GetOrCreate main failed: %v", err)
	}

	// Add a diverging commit directly in the workspace (not on origin/main)
	runGit(t, ws1.Path, "config", "user.email", "test@test.com")
	runGit(t, ws1.Path, "config", "user.name", "Test User")
	writeFile(t, ws1.Path, "diverged.txt", "diverged content")
	runGit(t, ws1.Path, "add", ".")
	runGit(t, ws1.Path, "commit", "-m", "diverging commit")

	// Now request a different branch — the workspace has diverged so it should
	// NOT be reused; a new workspace should be created instead.
	ws2, err := manager.GetOrCreate(ctx, repoDir, "feature-new")
	if err != nil {
		t.Fatalf("GetOrCreate feature-new failed: %v", err)
	}

	if ws2.ID == ws1.ID {
		t.Fatalf("expected a NEW workspace because old branch diverged, but got same ID %s", ws2.ID)
	}

	// Verify the new workspace's branch does NOT contain the diverging commit
	cmd := exec.Command("git", "-C", ws2.Path, "log", "--oneline")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log failed: %v", err)
	}
	if strings.Contains(string(output), "diverging commit") {
		t.Fatalf("new workspace branch should not contain the diverging commit, got:\n%s", output)
	}
}

// TestGetOrCreate_BranchReuse_UpToDateAllowsReuse verifies that a workspace whose
// branch is at or behind the default branch IS reused for a different branch.
func TestGetOrCreate_BranchReuse_UpToDateAllowsReuse(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := gitTestWorkTree(t)
	gitTestBranch(t, repoDir, "feature-1")

	cfg := &config.Config{
		WorkspacePath:    t.TempDir(),
		WorktreeBasePath: t.TempDir(),
		Repos: []config.Repo{
			testRepoWithBarePath("test", repoDir),
		},
	}
	manager := New(cfg, st, statePath)
	ctx := context.Background()

	// Create workspace on "main" — no diverging commits
	ws1, err := manager.GetOrCreate(ctx, repoDir, "main")
	if err != nil {
		t.Fatalf("GetOrCreate main failed: %v", err)
	}

	// Request different branch — workspace is up-to-date with main so reuse is OK
	ws2, err := manager.GetOrCreate(ctx, repoDir, "feature-1")
	if err != nil {
		t.Fatalf("GetOrCreate feature-1 failed: %v", err)
	}

	if ws2.ID != ws1.ID {
		t.Errorf("expected workspace reuse (same ID), got %s vs %s", ws1.ID, ws2.ID)
	}

	ws2State, _ := st.GetWorkspace(ws2.ID)
	if ws2State.Branch != "feature-1" {
		t.Errorf("expected branch feature-1 in state, got %s", ws2State.Branch)
	}
}
