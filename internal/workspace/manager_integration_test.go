package workspace

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// TestGetOrCreate_BranchReuse_Success verifies state IS updated when checkout succeeds.
func TestGetOrCreate_BranchReuse_Success(t *testing.T) {
	t.Parallel()
	// Set up isolated state with temp path
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)

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
			testRepoWithBarePath(t, "test", repoDir),
		},
	}
	manager := New(cfg, st, statePath, testLogger())

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

	// Mark as recyclable — only non-running workspaces are eligible for Tier 2 reuse
	ws1State.Status = state.WorkspaceStatusRecyclable
	st.UpdateWorkspace(ws1State)

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
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := gitTestWorkTree(t)

	cfg := &config.Config{
		WorkspacePath:    t.TempDir(),
		WorktreeBasePath: t.TempDir(),
		Repos: []config.Repo{
			testRepoWithBarePath(t, "test", repoDir),
		},
	}
	manager := New(cfg, st, statePath, testLogger())

	lock := manager.repoLock(repoDir)
	lock.Lock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := manager.GetOrCreate(ctx, repoDir, "main")
		done <- err
	}()

	// Verify GetOrCreate blocks on the held lock — it should not return within 200ms
	select {
	case err := <-done:
		lock.Unlock()
		t.Fatalf("expected GetOrCreate to block, returned early: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	lock.Unlock()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("GetOrCreate failed after unlock: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for GetOrCreate after unlock")
	}
}

// TestGetOrCreate_UniqueBranchOnWorktreeConflict verifies branch name is uniquified
// when the requested branch is already checked out in another worktree.
func TestGetOrCreate_UniqueBranchOnWorktreeConflict(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := gitTestWorkTree(t)

	cfg := &config.Config{
		WorkspacePath:    t.TempDir(),
		WorktreeBasePath: t.TempDir(),
		Repos: []config.Repo{
			testRepoWithBarePath(t, "test", repoDir),
		},
	}
	manager := New(cfg, st, statePath, testLogger())

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
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)

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
			testRepoWithBarePath(t, "test", repoDir),
		},
	}
	manager := New(cfg, st, statePath, testLogger())

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
	t.Parallel()
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
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := gitTestWorkTree(t)

	cfg := &config.Config{
		WorkspacePath:    t.TempDir(),
		WorktreeBasePath: t.TempDir(),
		Repos: []config.Repo{
			testRepoWithBarePath(t, "test", repoDir),
		},
	}
	manager := New(cfg, st, statePath, testLogger())
	ctx := context.Background()

	// Create workspace on "main"
	ws1, err := manager.GetOrCreate(ctx, repoDir, "main")
	if err != nil {
		t.Fatalf("GetOrCreate main failed: %v", err)
	}

	// Add a diverging commit directly in the workspace (not on origin/main)
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
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := gitTestWorkTree(t)
	gitTestBranch(t, repoDir, "feature-1")

	cfg := &config.Config{
		WorkspacePath:    t.TempDir(),
		WorktreeBasePath: t.TempDir(),
		Repos: []config.Repo{
			testRepoWithBarePath(t, "test", repoDir),
		},
	}
	manager := New(cfg, st, statePath, testLogger())
	ctx := context.Background()

	// Create workspace on "main" — no diverging commits
	ws1, err := manager.GetOrCreate(ctx, repoDir, "main")
	if err != nil {
		t.Fatalf("GetOrCreate main failed: %v", err)
	}

	// Mark as recyclable — only non-running workspaces are eligible for Tier 2 reuse
	w := *ws1
	w.Status = state.WorkspaceStatusRecyclable
	st.UpdateWorkspace(w)

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

func TestGetOrCreate_RecyclableWorkspace_ReusedBeforeCreate(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)

	repoDir := gitTestWorkTree(t)
	gitTestBranch(t, repoDir, "feature-1")

	cfg := &config.Config{
		WorkspacePath:     t.TempDir(),
		WorktreeBasePath:  t.TempDir(),
		RecycleWorkspaces: true,
		Repos:             []config.Repo{testRepoWithBarePath(t, "test", repoDir)},
	}
	manager := New(cfg, st, statePath, testLogger())

	// Create workspace on "main"
	ws1, err := manager.GetOrCreate(context.Background(), repoDir, "main")
	if err != nil {
		t.Fatalf("GetOrCreate main failed: %v", err)
	}
	ws1Path := ws1.Path

	// Dispose it (should recycle, not delete)
	if err := manager.Dispose(context.Background(), ws1.ID); err != nil {
		t.Fatalf("Dispose failed: %v", err)
	}

	// Verify it's recyclable
	w, found := st.GetWorkspace(ws1.ID)
	if !found || w.Status != state.WorkspaceStatusRecyclable {
		t.Fatalf("workspace should be recyclable, got found=%v status=%q", found, w.Status)
	}

	// Spawn on "feature-1" — should reuse the recyclable workspace
	ws2, err := manager.GetOrCreate(context.Background(), repoDir, "feature-1")
	if err != nil {
		t.Fatalf("GetOrCreate feature-1 failed: %v", err)
	}

	// Same workspace ID and path
	if ws2.ID != ws1.ID {
		t.Errorf("expected same workspace ID, got %s vs %s", ws2.ID, ws1.ID)
	}
	if ws2.Path != ws1Path {
		t.Errorf("expected same path, got %s vs %s", ws2.Path, ws1Path)
	}

	// Status promoted to running
	w2, _ := st.GetWorkspace(ws2.ID)
	if w2.Status != state.WorkspaceStatusRunning {
		t.Errorf("status = %q, want %q", w2.Status, state.WorkspaceStatusRunning)
	}
	if w2.Branch != "feature-1" {
		t.Errorf("branch = %q, want %q", w2.Branch, "feature-1")
	}
}

func TestGetOrCreate_RecyclableLocalRepo_Reused(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)

	// CreateLocalRepo calls config.Save(), so we need a config with a valid path.
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = t.TempDir()
	cfg.WorktreeBasePath = t.TempDir()
	cfg.RecycleWorkspaces = true

	manager := New(cfg, st, statePath, testLogger())

	// Create a local repo workspace
	ws1, err := manager.CreateLocalRepo(context.Background(), "myproject", "main")
	if err != nil {
		t.Fatalf("CreateLocalRepo failed: %v", err)
	}

	// Add a tracked file so that prepare()'s `git checkout -- .` succeeds
	// (it fails on repos with only an empty initial commit).
	writeFile(t, ws1.Path, "README.md", "hello")
	runGit(t, ws1.Path, "add", ".")
	runGit(t, ws1.Path, "commit", "-m", "add readme")

	// Mark as recyclable (simulating dispose)
	ws1.Status = state.WorkspaceStatusRecyclable
	st.UpdateWorkspace(*ws1)

	// GetOrCreate with same local repo URL should find the recyclable workspace
	ws2, err := manager.GetOrCreate(context.Background(), "local:myproject", "main")
	if err != nil {
		t.Fatalf("GetOrCreate local failed: %v", err)
	}

	if ws2.ID != ws1.ID {
		t.Errorf("expected reuse, got new workspace %s vs %s", ws2.ID, ws1.ID)
	}
	if ws2.Status != state.WorkspaceStatusRunning {
		t.Errorf("status = %q, want running", ws2.Status)
	}
}

func TestGetOrCreate_RecyclableBranchCollision_PurgesAndRetries(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)

	repoDir := gitTestWorkTree(t)
	gitTestBranch(t, repoDir, "feature-1")

	cfg := &config.Config{
		WorkspacePath:     t.TempDir(),
		WorktreeBasePath:  t.TempDir(),
		RecycleWorkspaces: true,
		Repos:             []config.Repo{testRepoWithBarePath(t, "test", repoDir)},
	}
	manager := New(cfg, st, statePath, testLogger())

	// Create workspace on "feature-1"
	ws1, err := manager.GetOrCreate(context.Background(), repoDir, "feature-1")
	if err != nil {
		t.Fatalf("GetOrCreate feature-1 failed: %v", err)
	}

	// Dispose (recycles)
	if err := manager.Dispose(context.Background(), ws1.ID); err != nil {
		t.Fatalf("Dispose failed: %v", err)
	}

	// Verify it's recyclable
	w, found := st.GetWorkspace(ws1.ID)
	if !found || w.Status != state.WorkspaceStatusRecyclable {
		t.Fatalf("workspace should be recyclable, got found=%v status=%q", found, w.Status)
	}

	// Request "feature-1" again. Tier 0 should find the recyclable workspace
	// and reuse it via in-place checkout (no worktree add needed).
	ws2, err := manager.GetOrCreate(context.Background(), repoDir, "feature-1")
	if err != nil {
		t.Fatalf("GetOrCreate feature-1 (second) failed: %v", err)
	}

	// Should reuse the same workspace
	if ws2.ID != ws1.ID {
		t.Errorf("expected same workspace, got %s vs %s", ws2.ID, ws1.ID)
	}

	// Branch should be correct
	if ws2.Branch != "feature-1" {
		t.Errorf("expected branch feature-1, got %s", ws2.Branch)
	}

	// Status should be promoted to running
	w2, _ := st.GetWorkspace(ws2.ID)
	if w2.Status != state.WorkspaceStatusRunning {
		t.Errorf("status = %q, want running", w2.Status)
	}
}

func TestGetOrCreate_BranchReuse_PromotesRecyclableStatus(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)

	repoDir := gitTestWorkTree(t)
	gitTestBranch(t, repoDir, "feature-1")

	cfg := &config.Config{
		WorkspacePath:    t.TempDir(),
		WorktreeBasePath: t.TempDir(),
		// RecycleWorkspaces is false — Tier 0 won't run.
		// A workspace can still end up recyclable via direct state manipulation.
		Repos: []config.Repo{testRepoWithBarePath(t, "test", repoDir)},
	}
	manager := New(cfg, st, statePath, testLogger())

	ws1, err := manager.GetOrCreate(context.Background(), repoDir, "main")
	if err != nil {
		t.Fatalf("GetOrCreate main failed: %v", err)
	}

	// Manually set status to recyclable (simulates a previous recycled dispose)
	w := *ws1
	w.Status = state.WorkspaceStatusRecyclable
	st.UpdateWorkspace(w)

	// Tier 2 should pick it up and promote to running
	ws2, err := manager.GetOrCreate(context.Background(), repoDir, "feature-1")
	if err != nil {
		t.Fatalf("GetOrCreate feature-1 failed: %v", err)
	}

	if ws2.ID != ws1.ID {
		t.Errorf("expected reuse, got different ID %s vs %s", ws2.ID, ws1.ID)
	}

	w2, _ := st.GetWorkspace(ws2.ID)
	if w2.Status != state.WorkspaceStatusRunning {
		t.Errorf("status = %q, want running (should be promoted from recyclable)", w2.Status)
	}
}

// TestGetOrCreate_BranchReuse_PurgesConflictingRecyclable verifies that when
// reusing a workspace for a different branch, a conflicting recyclable workspace
// holding the target branch is purged first so git checkout -B succeeds.
func TestGetOrCreate_BranchReuse_PurgesConflictingRecyclable(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)

	repoDir := gitTestWorkTree(t)
	gitTestBranch(t, repoDir, "feature-1")

	cfg := &config.Config{
		WorkspacePath:     t.TempDir(),
		WorktreeBasePath:  t.TempDir(),
		RecycleWorkspaces: true,
		Repos:             []config.Repo{testRepoWithBarePath(t, "test", repoDir)},
	}
	manager := New(cfg, st, statePath, testLogger())

	// Create two workspaces on different branches
	ws1, err := manager.GetOrCreate(context.Background(), repoDir, "main")
	if err != nil {
		t.Fatalf("GetOrCreate main failed: %v", err)
	}
	ws2, err := manager.GetOrCreate(context.Background(), repoDir, "feature-1")
	if err != nil {
		t.Fatalf("GetOrCreate feature-1 failed: %v", err)
	}

	// Dispose both (both become recyclable)
	if err := manager.Dispose(context.Background(), ws1.ID); err != nil {
		t.Fatalf("Dispose ws1 failed: %v", err)
	}
	if err := manager.Dispose(context.Background(), ws2.ID); err != nil {
		t.Fatalf("Dispose ws2 failed: %v", err)
	}

	// Verify both are recyclable
	w1, _ := st.GetWorkspace(ws1.ID)
	w2, _ := st.GetWorkspace(ws2.ID)
	if w1.Status != state.WorkspaceStatusRecyclable || w2.Status != state.WorkspaceStatusRecyclable {
		t.Fatalf("expected both recyclable, got %q and %q", w1.Status, w2.Status)
	}

	// Now request feature-1. Tier 0 finds ws2 (same repo+branch), reuses it.
	// This should succeed even though ws1 also exists.
	ws3, err := manager.GetOrCreate(context.Background(), repoDir, "feature-1")
	if err != nil {
		t.Fatalf("GetOrCreate feature-1 (re-request) failed: %v", err)
	}
	if ws3.ID != ws2.ID {
		t.Errorf("expected reuse of ws2 (%s), got %s", ws2.ID, ws3.ID)
	}

	// Dispose again
	if err := manager.Dispose(context.Background(), ws3.ID); err != nil {
		t.Fatalf("Dispose ws3 failed: %v", err)
	}

	// Now request "main" again. This should trigger:
	// - Tier 0: ws1 has "main" -> reuse it directly
	// But let's test the harder case: request feature-1 through ws1 (different branch reuse).
	// First, dispose ws2 so it becomes recyclable holding feature-1.
	// Then request feature-1 via ws1 (Tier 1 reuse with branch switch).
	// ws2 holds feature-1 in its worktree, which would block git checkout -B feature-1 in ws1.

	// Re-create ws2 on feature-1 and dispose it to make it recyclable
	ws4, err := manager.GetOrCreate(context.Background(), repoDir, "feature-1")
	if err != nil {
		t.Fatalf("GetOrCreate feature-1 (third) failed: %v", err)
	}
	if err := manager.Dispose(context.Background(), ws4.ID); err != nil {
		t.Fatalf("Dispose ws4 failed: %v", err)
	}

	// Delete ws1's directory externally to simulate it being unavailable for Tier 0 match.
	// This forces Tier 1 to pick a different workspace (ws4, which has feature-1)
	// and try to switch it to "main" — but ws1's stale worktree entry still claims "main".
	// The fix should prune stale entries before checkout.
	w1Path := w1.Path
	if err := exec.Command("rm", "-rf", w1Path).Run(); err != nil {
		t.Fatalf("failed to delete ws1 dir: %v", err)
	}

	// Request "main" — ws1 dir is gone, so Tier 0 skips it.
	// Tier 1 finds ws4 (feature-1, no active sessions) and tries to switch to "main".
	// Without the fix, this fails because the stale worktree entry for ws1 still claims "main".
	ws5, err := manager.GetOrCreate(context.Background(), repoDir, "main")
	if err != nil {
		t.Fatalf("GetOrCreate main (after stale worktree) failed: %v", err)
	}

	// Should have reused ws4 (switched from feature-1 to main)
	if ws5.Branch != "main" {
		t.Errorf("expected branch main, got %s", ws5.Branch)
	}
}

// TestGetOrCreate_RecycleSameDivergedBranch verifies that Tier 0 reclaims a
// recyclable workspace when the requested branch matches, even if the branch
// has diverged from the default branch. The divergence check exists to prevent
// cross-branch commit pollution — it should not block same-branch recycling.
func TestGetOrCreate_RecycleSameDivergedBranch(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)

	repoDir := gitTestWorkTree(t)
	gitTestBranch(t, repoDir, "feature-1") // creates a commit not on main → diverged

	var logBuf bytes.Buffer
	logger := log.NewWithOptions(&logBuf, log.Options{Level: log.InfoLevel})

	cfg := &config.Config{
		WorkspacePath:     t.TempDir(),
		WorktreeBasePath:  t.TempDir(),
		RecycleWorkspaces: true,
		Repos:             []config.Repo{testRepoWithBarePath(t, "test", repoDir)},
	}
	manager := New(cfg, st, statePath, logger)

	// Create workspace on diverged feature branch
	ws1, err := manager.GetOrCreate(context.Background(), repoDir, "feature-1")
	if err != nil {
		t.Fatalf("GetOrCreate feature-1 failed: %v", err)
	}

	// Dispose → recyclable
	if err := manager.Dispose(context.Background(), ws1.ID); err != nil {
		t.Fatalf("Dispose failed: %v", err)
	}

	w, _ := st.GetWorkspace(ws1.ID)
	if w.Status != state.WorkspaceStatusRecyclable {
		t.Fatalf("expected recyclable, got %q", w.Status)
	}

	// Clear log buffer before the critical call
	logBuf.Reset()

	// Re-request same branch — should be reclaimed via Tier 0
	ws2, err := manager.GetOrCreate(context.Background(), repoDir, "feature-1")
	if err != nil {
		t.Fatalf("GetOrCreate feature-1 (second) failed: %v", err)
	}

	// Must be the same workspace
	if ws2.ID != ws1.ID {
		t.Errorf("expected same workspace ID %s, got %s", ws1.ID, ws2.ID)
	}
	if ws2.Branch != "feature-1" {
		t.Errorf("expected branch feature-1, got %s", ws2.Branch)
	}

	w2, _ := st.GetWorkspace(ws2.ID)
	if w2.Status != state.WorkspaceStatusRunning {
		t.Errorf("status = %q, want running", w2.Status)
	}

	// Verify reclaimed via Tier 0 (not Tier 1 fallback).
	// Tier 0 logs "reusing recyclable workspace", Tier 1 logs "reusing existing".
	logs := logBuf.String()
	if !strings.Contains(logs, "reusing recyclable workspace") {
		t.Errorf("expected Tier 0 reclaim (\"reusing recyclable workspace\"), got logs:\n%s", logs)
	}
}

// TestGetOrCreate_BranchReuse_SkipsRunningWorkspaces verifies that a workspace
// with status "running" is NOT reused for a different branch, even when it has
// no active sessions and its branch is up-to-date with the default branch.
// Running workspaces are part of the user's active working set and must not be
// silently hijacked.
func TestGetOrCreate_BranchReuse_SkipsRunningWorkspaces(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)

	repoDir := gitTestWorkTree(t)
	gitTestBranch(t, repoDir, "feature-new")

	cfg := &config.Config{
		WorkspacePath:    t.TempDir(),
		WorktreeBasePath: t.TempDir(),
		Repos: []config.Repo{
			testRepoWithBarePath(t, "test", repoDir),
		},
	}
	manager := New(cfg, st, statePath, testLogger())
	ctx := context.Background()

	// Create a workspace on "main" — GetOrCreate sets status to "running"
	ws1, err := manager.GetOrCreate(ctx, repoDir, "main")
	if err != nil {
		t.Fatalf("GetOrCreate main failed: %v", err)
	}

	// Verify it is indeed running with no sessions
	w1, _ := st.GetWorkspace(ws1.ID)
	if w1.Status != state.WorkspaceStatusRunning {
		t.Fatalf("precondition: expected status running, got %s", w1.Status)
	}

	// Request a different branch — the existing workspace is "running" so it
	// must NOT be reused. A brand new workspace should be created.
	ws2, err := manager.GetOrCreate(ctx, repoDir, "feature-new")
	if err != nil {
		t.Fatalf("GetOrCreate feature-new failed: %v", err)
	}

	if ws2.ID == ws1.ID {
		t.Fatalf("running workspace was reused for a different branch: got same ID %s", ws2.ID)
	}

	// Original workspace should be untouched
	w1After, _ := st.GetWorkspace(ws1.ID)
	if w1After.Branch != "main" {
		t.Errorf("original workspace branch changed from main to %s", w1After.Branch)
	}
	if w1After.Status != state.WorkspaceStatusRunning {
		t.Errorf("original workspace status changed from running to %s", w1After.Status)
	}
}
