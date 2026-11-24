package workspace

import (
	"context"
	"os"
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
	cfg := &config.Config{}
	cfg.WorkspacePath = t.TempDir()
	cfg.WorktreeBasePath = t.TempDir()
	cfg.Repos = []config.Repo{
		testRepoWithBarePath(t, "test", repoDir),
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

	// Mark as failed — only non-running workspaces are eligible for Tier 2 reuse
	ws1State.Status = state.WorkspaceStatusFailed
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

	cfg := &config.Config{}
	cfg.WorkspacePath = t.TempDir()
	cfg.WorktreeBasePath = t.TempDir()
	cfg.Repos = []config.Repo{
		testRepoWithBarePath(t, "test", repoDir),
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

	cfg := &config.Config{}
	cfg.WorkspacePath = t.TempDir()
	cfg.WorktreeBasePath = t.TempDir()
	cfg.Repos = []config.Repo{
		testRepoWithBarePath(t, "test", repoDir),
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

	cfg := &config.Config{}
	cfg.WorkspacePath = t.TempDir()
	cfg.WorktreeBasePath = t.TempDir()
	cfg.SourceCodeManagement = config.SourceCodeManagementGit
	cfg.Repos = []config.Repo{
		testRepoWithBarePath(t, "test", repoDir),
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

	cfg := &config.Config{}
	cfg.WorkspacePath = t.TempDir()
	cfg.WorktreeBasePath = t.TempDir()
	cfg.Repos = []config.Repo{
		testRepoWithBarePath(t, "test", repoDir),
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

	cfg := &config.Config{}
	cfg.WorkspacePath = t.TempDir()
	cfg.WorktreeBasePath = t.TempDir()
	cfg.Repos = []config.Repo{
		testRepoWithBarePath(t, "test", repoDir),
	}
	manager := New(cfg, st, statePath, testLogger())
	ctx := context.Background()

	// Create workspace on "main" — no diverging commits
	ws1, err := manager.GetOrCreate(ctx, repoDir, "main")
	if err != nil {
		t.Fatalf("GetOrCreate main failed: %v", err)
	}

	// Mark as failed — only non-running workspaces are eligible for Tier 2 reuse
	w := *ws1
	w.Status = state.WorkspaceStatusFailed
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

func TestGetOrCreate_BranchReuse_PromotesLeftoverRecyclableRecord(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)

	repoDir := gitTestWorkTree(t)
	gitTestBranch(t, repoDir, "feature-1")

	cfg := &config.Config{}
	cfg.WorkspacePath = t.TempDir()
	cfg.WorktreeBasePath = t.TempDir()
	cfg.Repos = []config.Repo{testRepoWithBarePath(t, "test", repoDir)}
	manager := New(cfg, st, statePath, testLogger())

	ws1, err := manager.GetOrCreate(context.Background(), repoDir, "main")
	if err != nil {
		t.Fatalf("GetOrCreate main failed: %v", err)
	}

	// A leftover record from before recycling was removed
	w := *ws1
	w.Status = "recyclable"
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

// TestGetOrCreate_ReuseKeepsFreshOverlayManifest verifies that promoting a
// reused workspace to running does not write the pre-reuse overlay manifest
// back over the one recorded when the overlays were re-copied.
func TestGetOrCreate_ReuseKeepsFreshOverlayManifest(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tests := []struct {
		name   string
		branch string
	}{
		{"same branch", "main"},
		{"different branch", "feature-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "state.json")
			st := state.New(statePath, nil)

			repoDir := gitTestWorkTree(t)
			writeFile(t, repoDir, ".gitignore", "local.conf\n")
			runGit(t, repoDir, "add", ".gitignore")
			runGit(t, repoDir, "commit", "-m", "ignore local.conf")
			gitTestBranch(t, repoDir, "feature-1")

			// Overlays live under the package's temporary HOME, keyed by repo name.
			repo := testRepoWithBarePath(t, "reuse-manifest-"+tt.branch, repoDir)
			overlayDir, err := OverlayDir(repo.Name)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(overlayDir, 0755); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(overlayDir) })
			writeFile(t, overlayDir, "local.conf", "old")

			cfg := &config.Config{}
			cfg.WorkspacePath = t.TempDir()
			cfg.WorktreeBasePath = t.TempDir()
			cfg.Repos = []config.Repo{repo}
			manager := New(cfg, st, statePath, testLogger())

			ws1, err := manager.GetOrCreate(context.Background(), repoDir, "main")
			if err != nil {
				t.Fatalf("GetOrCreate main failed: %v", err)
			}
			oldHash := ws1.OverlayManifest["local.conf"]
			if oldHash == "" {
				t.Fatalf("manifest after create = %v, want a local.conf entry", ws1.OverlayManifest)
			}

			w := *ws1
			w.Status = state.WorkspaceStatusFailed
			st.UpdateWorkspace(w)
			writeFile(t, overlayDir, "local.conf", "new")

			ws2, err := manager.GetOrCreate(context.Background(), repoDir, tt.branch)
			if err != nil {
				t.Fatalf("GetOrCreate %s failed: %v", tt.branch, err)
			}
			if ws2.ID != ws1.ID {
				t.Fatalf("workspace ID = %q, want reuse of %q", ws2.ID, ws1.ID)
			}

			got, _ := st.GetWorkspace(ws2.ID)
			if hash := got.OverlayManifest["local.conf"]; hash == oldHash || hash == "" {
				t.Errorf("manifest local.conf hash = %q after reuse, want the re-copied file's hash (pre-reuse hash %q)", hash, oldHash)
			}
			if ws2.OverlayManifest["local.conf"] != got.OverlayManifest["local.conf"] {
				t.Errorf("returned manifest local.conf hash = %q, want state's %q", ws2.OverlayManifest["local.conf"], got.OverlayManifest["local.conf"])
			}
		})
	}
}

// TestGetOrCreate_RemoteBranchKeepsBranchPoint verifies that a workspace
// created for a branch that already exists on origin ends up exactly at the
// remote branch tip. Auto-sync from default must not rebase a published
// branch onto origin/<default> — rewriting its history diverges the local
// copy from the remote, which blocks push (non-fast-forward) and dispose
// ("unpushed commits").
func TestGetOrCreate_RemoteBranchKeepsBranchPoint(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)

	repoDir := gitTestWorkTree(t)
	gitTestBranch(t, repoDir, "feature-1") // branch exists on origin, back on main
	// Land a commit on main after the branch point so the branch and main diverge.
	writeFile(t, repoDir, "main-new.txt", "landed after branch")
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "main: post-branch commit")
	remoteTip := strings.TrimSpace(runGitOut(t, repoDir, "rev-parse", "feature-1"))

	cfg := &config.Config{}
	cfg.WorkspacePath = t.TempDir()
	cfg.WorktreeBasePath = t.TempDir()
	cfg.Repos = []config.Repo{testRepoWithBarePath(t, "test", repoDir)}
	manager := New(cfg, st, statePath, testLogger())

	ws, err := manager.GetOrCreate(context.Background(), repoDir, "feature-1")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	head := strings.TrimSpace(runGitOut(t, ws.Path, "rev-parse", "HEAD"))
	if head != remoteTip {
		log := runGitOut(t, ws.Path, "log", "--oneline", "-10")
		t.Errorf("expected HEAD at origin/feature-1 tip %s, got %s; git log:\n%s", remoteTip[:8], head[:8], log)
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

	cfg := &config.Config{}
	cfg.WorkspacePath = t.TempDir()
	cfg.WorktreeBasePath = t.TempDir()
	cfg.Repos = []config.Repo{
		testRepoWithBarePath(t, "test", repoDir),
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
