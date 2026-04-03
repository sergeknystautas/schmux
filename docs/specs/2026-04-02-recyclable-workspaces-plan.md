# Plan: Recyclable Workspaces

**Goal**: Disposing a workspace keeps its directory on disk and reuses it on next spawn, reducing file churn that saturates backup software.
**Architecture**: New `"recyclable"` workspace status, config flag `recycle_workspaces`, Tier 0 reuse in `GetOrCreate`, Purge API for explicit deletion. See `docs/specs/2026-04-02-recyclable-workspaces-design.md`.
**Tech Stack**: Go, TypeScript/React, Vitest, `go test`

## Changes from v1

Revised to address plan review feedback. Key changes:

1. **Tier 0 moved inside `repoLock`** (Step 4). Placing it before the lock allowed two concurrent callers to claim the same recyclable workspace — `GetWorkspaces()` returns copies, so both see "recyclable" and both proceed. Now Tier 0 runs under the lock. The `local:` early return is also moved after the lock.
2. **`dispose()` gains `skipRecycling` parameter** (Steps 3, 7). Replaced the thread-unsafe "temporarily disable `RecycleWorkspaces`" pattern. `dispose(ctx, id, force, skipRecycling)` is explicit and safe for concurrent use.
3. **`WorkspaceManager` interface updated** (Step 7). `Purge` and `PurgeAll` added to the interface in `internal/workspace/interfaces.go`. Without this, the dashboard handlers won't compile.
4. **Fixed `newTestServer` destructuring** (Steps 10, 11). The return order is `(*Server, *config.Config, *state.State)` — all tests use `server, _, st`, not `server, st, _`.
5. **Fixed package qualifier in Step 2 test**. Test is in package `state`, so no `state.` prefix.
6. **Step 3 checks `dirExists` before recycling**. Without this, a workspace whose directory was manually deleted would be marked "recyclable" with no directory on disk.
7. **Steps 4–5 are now sequential** (both modify `manager.go`).
8. **Route registration uses relative path** inside the `workspaces/{workspaceID}` route group.
9. **Step 13 (type regen) removed** — workspace status constants are Go `const` strings, not API contract structs. `go run ./cmd/gen-types` won't produce changes.

## Dependency Groups

| Group | Steps       | Can Parallelize | Notes                                                                             |
| ----- | ----------- | --------------- | --------------------------------------------------------------------------------- |
| 1     | Steps 1–2   | Yes             | Config + state constants (independent files)                                      |
| 2     | Step 3      | No              | Dispose path (depends on Group 1)                                                 |
| 3     | Step 4      | No              | Tier 0 reuse (depends on Group 2)                                                 |
| 4     | Step 5      | No              | Tiers 1–2 fix (depends on Group 3, same file)                                     |
| 5     | Step 6      | No              | Worktree branch collision (depends on Group 4)                                    |
| 6     | Step 7      | No              | Purge API + interface (depends on Group 2)                                        |
| 7     | Steps 8–9   | Yes             | Polling exclusion + crash recovery (independent, depend on Group 1)               |
| 8     | Steps 10–12 | Yes             | Dashboard: filtering, purge endpoint, recyclable indicator (depend on Groups 6,1) |
| 9     | Step 13     | No              | E2E verification + API docs (depends on all)                                      |

---

## Step 1: Add `RecycleWorkspaces` config field

**File**: `internal/config/config.go`

### 1a. Write failing test

**File**: `internal/config/config_test.go`

```go
func TestRecycleWorkspaces_DefaultFalse(t *testing.T) {
	t.Parallel()
	cfg := &Config{}
	if cfg.RecycleWorkspaces {
		t.Error("RecycleWorkspaces should default to false")
	}
}

func TestRecycleWorkspaces_ParsesFromJSON(t *testing.T) {
	t.Parallel()
	jsonData := `{"workspace_path": "/tmp/test", "recycle_workspaces": true, "repos": [], "run_targets": []}`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")
	os.WriteFile(cfgPath, []byte(jsonData), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !cfg.RecycleWorkspaces {
		t.Error("RecycleWorkspaces should be true when set in JSON")
	}
}
```

### 1b. Run test to verify it fails

```bash
go test ./internal/config/ -run "TestRecycleWorkspaces" -count=1
```

### 1c. Write implementation

**File**: `internal/config/config.go` — add field to `Config` struct after `TmuxBinary` (line 101):

```go
RecycleWorkspaces          bool                        `json:"recycle_workspaces,omitempty"`
```

Also add to the `applyConfigUpdate` method (around line 1623, near `SourceCodeManagement` assignment):

```go
c.RecycleWorkspaces = newCfg.RecycleWorkspaces
```

### 1d. Run test to verify it passes

```bash
go test ./internal/config/ -run "TestRecycleWorkspaces" -count=1
```

### 1e. Commit

```bash
git add internal/config/config.go internal/config/config_test.go
```

---

## Step 2: Add `WorkspaceStatusRecyclable` constant

**File**: `internal/state/state.go`

### 2a. Write failing test

**File**: `internal/state/state_test.go`

```go
func TestWorkspaceStatusRecyclable_Constant(t *testing.T) {
	if WorkspaceStatusRecyclable != "recyclable" {
		t.Errorf("WorkspaceStatusRecyclable = %q, want %q", WorkspaceStatusRecyclable, "recyclable")
	}
}
```

### 2b. Run test to verify it fails

```bash
go test ./internal/state/ -run "TestWorkspaceStatusRecyclable" -count=1
```

### 2c. Write implementation

**File**: `internal/state/state.go` — add constant after `WorkspaceStatusDisposing` (line 86):

```go
const (
	WorkspaceStatusProvisioning = "provisioning"
	WorkspaceStatusRunning      = "running"
	WorkspaceStatusFailed       = "failed"
	WorkspaceStatusDisposing    = "disposing"
	WorkspaceStatusRecyclable   = "recyclable"
)
```

### 2d. Run test to verify it passes

```bash
go test ./internal/state/ -run "TestWorkspaceStatusRecyclable" -count=1
```

### 2e. Commit

```bash
git add internal/state/state.go internal/state/state_test.go
```

---

## Step 3: Recycle in `dispose()` instead of deleting files

**File**: `internal/workspace/manager.go`

### 3a. Write failing test

**File**: `internal/workspace/manager_test.go`

```go
func TestDispose_RecycleWorkspaces_KeepsDirectory(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{
		WorkspacePath:     tmpDir,
		RecycleWorkspaces: true,
	}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	workspaceID := "test-001"
	workspacePath := filepath.Join(tmpDir, workspaceID)
	os.MkdirAll(workspacePath, 0755)
	exec.Command("git", "init", "-q", workspacePath).Run()

	st.AddWorkspace(state.Workspace{
		ID:     workspaceID,
		Repo:   "test",
		Branch: "main",
		Path:   workspacePath,
		Status: state.WorkspaceStatusRunning,
	})

	err := m.Dispose(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("Dispose() error = %v", err)
	}

	// Directory should still exist
	if _, err := os.Stat(workspacePath); os.IsNotExist(err) {
		t.Error("workspace directory should NOT be deleted when recycle_workspaces is true")
	}

	// Workspace should still be in state with "recyclable" status
	w, found := st.GetWorkspace(workspaceID)
	if !found {
		t.Fatal("workspace should still exist in state")
	}
	if w.Status != state.WorkspaceStatusRecyclable {
		t.Errorf("status = %q, want %q", w.Status, state.WorkspaceStatusRecyclable)
	}
}

func TestDispose_RecycleWorkspaces_ForceStillRecycles(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{
		WorkspacePath:     tmpDir,
		RecycleWorkspaces: true,
	}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	workspaceID := "test-001"
	workspacePath := filepath.Join(tmpDir, workspaceID)
	os.MkdirAll(workspacePath, 0755)
	exec.Command("git", "init", "-q", workspacePath).Run()

	st.AddWorkspace(state.Workspace{
		ID:     workspaceID,
		Repo:   "test",
		Branch: "main",
		Path:   workspacePath,
		Status: state.WorkspaceStatusRunning,
	})

	// DisposeForce should also recycle (force only skips safety checks)
	err := m.DisposeForce(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("DisposeForce() error = %v", err)
	}

	if _, err := os.Stat(workspacePath); os.IsNotExist(err) {
		t.Error("workspace directory should NOT be deleted when recycle_workspaces is true, even with force")
	}

	w, found := st.GetWorkspace(workspaceID)
	if !found {
		t.Fatal("workspace should still exist in state")
	}
	if w.Status != state.WorkspaceStatusRecyclable {
		t.Errorf("status = %q, want %q", w.Status, state.WorkspaceStatusRecyclable)
	}
}

func TestDispose_RecycleDisabled_StillDeletesDirectory(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{
		WorkspacePath:     tmpDir,
		RecycleWorkspaces: false,
	}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	workspaceID := "test-001"
	workspacePath := filepath.Join(tmpDir, workspaceID)
	os.MkdirAll(workspacePath, 0755)
	exec.Command("git", "init", "-q", workspacePath).Run()

	st.AddWorkspace(state.Workspace{
		ID:     workspaceID,
		Repo:   "test",
		Branch: "main",
		Path:   workspacePath,
	})

	err := m.Dispose(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("Dispose() error = %v", err)
	}

	// Directory should be deleted (original behavior)
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Error("workspace directory should be deleted when recycle_workspaces is false")
	}

	// Workspace should be removed from state
	_, found := st.GetWorkspace(workspaceID)
	if found {
		t.Error("workspace should be removed from state")
	}
}
```

### 3b. Run test to verify it fails

```bash
go test ./internal/workspace/ -run "TestDispose_Recycle" -count=1
```

### 3c. Write implementation

**File**: `internal/workspace/manager.go`

**First**, change the `dispose()` signature to add `skipRecycling`:

```go
// Before:
func (m *Manager) dispose(ctx context.Context, workspaceID string, force bool) error {

// After:
func (m *Manager) dispose(ctx context.Context, workspaceID string, force bool, skipRecycling bool) error {
```

Update the two callers:

```go
func (m *Manager) Dispose(ctx context.Context, workspaceID string) error {
	return m.dispose(ctx, workspaceID, false, false)
}

func (m *Manager) DisposeForce(ctx context.Context, workspaceID string) error {
	return m.dispose(ctx, workspaceID, true, false)
}
```

**Then**, in `dispose()`, after the watch removal block (line 1142) and before the file deletion logic (line 1144), insert:

```go
	// Clean up diff temp dirs (in OS temp, not workspace — no backup churn)
	if err := difftool.CleanupWorkspaceTempDirs(workspaceID); err != nil {
		m.logger.Warn("failed to cleanup diff temp dirs", "id", workspaceID, "err", err)
	}

	// Recycle: keep directory on disk, mark as recyclable for future reuse.
	// Only recycle if the directory actually exists — recycling a missing directory
	// would create a stale entry that Tier 0 can't reuse.
	if m.config.RecycleWorkspaces && !skipRecycling && dirExists {
		w.Status = state.WorkspaceStatusRecyclable
		if err := m.state.UpdateWorkspace(w); err != nil {
			return fmt.Errorf("failed to mark workspace as recyclable: %w", err)
		}
		if err := m.state.Save(); err != nil {
			return fmt.Errorf("failed to save state: %w", err)
		}

		// Clean up in-memory maps
		m.workspaceConfigsMu.Lock()
		delete(m.workspaceConfigs, workspaceID)
		m.workspaceConfigsMu.Unlock()

		m.lockedWorkspacesMu.Lock()
		delete(m.lockedWorkspaces, workspaceID)
		m.lockedWorkspacesMu.Unlock()

		m.workspaceGatesMu.Lock()
		delete(m.workspaceGates, workspaceID)
		m.workspaceGatesMu.Unlock()

		m.logger.Info("recycled (directory preserved)", "id", workspaceID)
		return nil
	}
```

Move the existing `difftool.CleanupWorkspaceTempDirs` call (currently at line 1205) into the block above so it runs in both paths. Remove the duplicate at the original location, or guard it with a comment that it only runs in the non-recycle path.

Note: `dirExists` is already computed at line 1117–1121 in the existing code. The recycling check uses it to avoid creating recyclable entries for workspaces whose directories are gone.

### 3d. Run test to verify it passes

```bash
go test ./internal/workspace/ -run "TestDispose_Recycle" -count=1
```

### 3e. Commit

```bash
git add internal/workspace/manager.go internal/workspace/manager_test.go
```

---

## Step 4: Add Tier 0 reuse of recyclable workspaces in `GetOrCreate`

**File**: `internal/workspace/manager.go`

### 4a. Write failing test

**File**: `internal/workspace/manager_integration_test.go`

```go
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
	workspacePath := t.TempDir()

	cfg := &config.Config{
		WorkspacePath:     workspacePath,
		WorktreeBasePath:  t.TempDir(),
		RecycleWorkspaces: true,
	}
	manager := New(cfg, st, statePath, testLogger())

	// Create a local repo workspace
	ws1, err := manager.CreateLocalRepo(context.Background(), "myproject", "main")
	if err != nil {
		t.Fatalf("CreateLocalRepo failed: %v", err)
	}

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
```

### 4b. Run test to verify it fails

```bash
go test ./internal/workspace/ -run "TestGetOrCreate_Recyclable" -count=1
```

### 4c. Write implementation

**File**: `internal/workspace/manager.go` — restructure `GetOrCreate()` to move the `local:` early return after the lock and insert Tier 0 under the lock.

The new structure of `GetOrCreate()` becomes:

```go
func (m *Manager) GetOrCreate(ctx context.Context, repoURL, branch string) (*state.Workspace, error) {
	if err := ValidateBranchName(branch); err != nil {
		return nil, fmt.Errorf("failed to get workspace: %w", err)
	}

	// Acquire per-repo lock. Both local and remote repos go through this now,
	// so Tier 0 is protected from concurrent callers claiming the same workspace.
	lock := m.repoLock(repoURL)
	lock.Lock()
	defer lock.Unlock()

	// Tier 0: Reuse a recyclable workspace for the same repo.
	if m.config.RecycleWorkspaces {
		for _, w := range m.state.GetWorkspaces() {
			if w.Status != state.WorkspaceStatusRecyclable || w.Repo != repoURL {
				continue
			}
			// Verify directory still exists
			if _, err := os.Stat(w.Path); os.IsNotExist(err) {
				m.logger.Warn("recyclable workspace directory missing, cleaning up", "id", w.ID)
				m.state.RemoveWorkspace(w.ID)
				m.state.Save()
				continue
			}
			// Divergence safety check (skip for non-git or local repos)
			if IsGitVCS(w.VCS) && !strings.HasPrefix(repoURL, "local:") && !m.isUpToDateWithDefault(ctx, w.Path, repoURL) {
				m.logger.Info("recyclable workspace diverged, skipping", "id", w.ID, "branch", w.Branch)
				continue
			}
			m.logger.Info("reusing recyclable workspace", "id", w.ID, "old_branch", w.Branch, "new_branch", branch)

			if err := m.prepare(ctx, w.ID, branch); err != nil {
				m.logger.Warn("failed to prepare recyclable workspace, skipping", "id", w.ID, "err", err)
				continue
			}
			// Re-copy overlay files
			if repoConfig, found := m.findRepoByURL(repoURL); found {
				if manifest, err := m.copyOverlayFiles(ctx, repoConfig.Name, w.Path); err != nil {
					m.logger.Warn("failed to re-copy overlay files", "err", err)
				} else if manifest != nil {
					m.state.UpdateOverlayManifest(w.ID, manifest)
				}
			}
			// Promote to running
			w.Branch = branch
			w.Status = state.WorkspaceStatusRunning
			if err := m.state.UpdateWorkspace(w); err != nil {
				return nil, fmt.Errorf("failed to update workspace: %w", err)
			}
			m.state.Save()
			// Re-add filesystem watches
			if m.gitWatcher != nil {
				m.gitWatcher.AddWorkspace(w.ID, w.Path)
			}
			return &w, nil
		}
	}

	// Handle local repositories — moved after Tier 0 so local repos can be recycled,
	// but still before Tiers 1–2 which don't apply to local repos.
	if strings.HasPrefix(repoURL, "local:") {
		repoName := strings.TrimPrefix(repoURL, "local:")
		return m.CreateLocalRepo(ctx, repoName, branch)
	}

	// Tier 1: existing same-branch reuse ...
	// Tier 2: existing different-branch reuse ...
	// Tier 3: create() ...
```

Key change: the `local:` early return and the `repoLock` acquisition have swapped positions. Previously the lock was at line 392 and `local:` at line 387. Now the lock comes first, then Tier 0, then the `local:` fallback. This is safe because `repoLock` is keyed by `repoURL` (including `local:` URLs), so different repos don't contend.

### 4d. Run test to verify it passes

```bash
go test ./internal/workspace/ -run "TestGetOrCreate_Recyclable" -count=1
```

### 4e. Commit

```bash
git add internal/workspace/manager.go internal/workspace/manager_integration_test.go
```

---

## Step 5: Fix Tiers 1–2 status promotion for recyclable workspaces

**File**: `internal/workspace/manager.go`

### 5a. Write failing test

**File**: `internal/workspace/manager_integration_test.go`

```go
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
```

### 5b. Run test to verify it fails

```bash
go test ./internal/workspace/ -run "TestGetOrCreate_BranchReuse_PromotesRecyclable" -count=1
```

### 5c. Write implementation

**File**: `internal/workspace/manager.go` — change two backfill blocks:

**Tier 1** (around line 420):

```go
// Before:
if w.Status == "" {
    w.Status = state.WorkspaceStatusRunning
    m.state.UpdateWorkspace(w)
}

// After:
if w.Status != state.WorkspaceStatusRunning {
    w.Status = state.WorkspaceStatusRunning
    m.state.UpdateWorkspace(w)
}
```

**Tier 2** (around line 462):

```go
// Before:
if w.Status == "" {
    w.Status = state.WorkspaceStatusRunning
}

// After:
if w.Status != state.WorkspaceStatusRunning {
    w.Status = state.WorkspaceStatusRunning
}
```

### 5d. Run test to verify it passes

```bash
go test ./internal/workspace/ -run "TestGetOrCreate_BranchReuse_PromotesRecyclable" -count=1
```

### 5e. Commit

```bash
git add internal/workspace/manager.go internal/workspace/manager_integration_test.go
```

---

## Step 6: Handle worktree branch reservation collision

**File**: `internal/workspace/manager.go`

### 6a. Write failing test

**File**: `internal/workspace/manager_integration_test.go`

```go
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

	// Create another workspace on "main" to consume the recyclable one
	ws2, err := manager.GetOrCreate(context.Background(), repoDir, "main")
	if err != nil {
		t.Fatalf("GetOrCreate main failed: %v", err)
	}
	// ws2 reused ws1's workspace, now on "main"
	if ws2.ID != ws1.ID {
		t.Fatalf("expected reuse, got different workspace")
	}

	// Now dispose ws2 (recycles again, currently on "main")
	if err := manager.Dispose(context.Background(), ws2.ID); err != nil {
		t.Fatalf("Dispose ws2 failed: %v", err)
	}

	// Spawn on "feature-1" again. Tier 0 should find the recyclable workspace
	// (now on "main") and reuse it. This should NOT hit a branch collision
	// because checkout happens in-place, not via worktree add.
	ws3, err := manager.GetOrCreate(context.Background(), repoDir, "feature-1")
	if err != nil {
		t.Fatalf("GetOrCreate feature-1 (second) failed: %v", err)
	}

	if ws3.ID != ws1.ID {
		t.Errorf("expected same workspace, got %s vs %s", ws3.ID, ws1.ID)
	}
}
```

### 6b. Run test to verify it fails

```bash
go test ./internal/workspace/ -run "TestGetOrCreate_RecyclableBranchCollision" -count=1 -timeout=60s
```

### 6c. Write implementation

**File**: `internal/workspace/manager.go` — in `GetOrCreate()`, wrap the existing `create()` call (line 474) with error detection and retry:

```go
	// Create a new workspace
	w, err := m.create(ctx, repoURL, branch)
	if err != nil {
		// If create failed because a recyclable worktree holds the branch,
		// purge the conflicting workspace and retry.
		if m.config.RecycleWorkspaces && strings.Contains(err.Error(), "already checked out") {
			if purged := m.purgeRecyclableWithBranch(ctx, repoURL, branch); purged {
				m.logger.Info("purged conflicting recyclable workspace, retrying create", "branch", branch)
				w, err = m.create(ctx, repoURL, branch)
			}
		}
		if err != nil {
			return nil, err
		}
	}
```

Add the helper method:

```go
// purgeRecyclableWithBranch finds and deletes a recyclable workspace that holds the given branch.
// Returns true if a workspace was purged.
func (m *Manager) purgeRecyclableWithBranch(ctx context.Context, repoURL, branch string) bool {
	for _, w := range m.state.GetWorkspaces() {
		if w.Status == state.WorkspaceStatusRecyclable && w.Repo == repoURL && w.Branch == branch {
			m.logger.Info("purging recyclable workspace holding branch", "id", w.ID, "branch", branch)
			// Use skipRecycling=true to force real deletion
			if err := m.dispose(ctx, w.ID, true, true); err != nil {
				m.logger.Warn("failed to purge recyclable workspace", "id", w.ID, "err", err)
				return false
			}
			return true
		}
	}
	return false
}
```

### 6d. Run test to verify it passes

```bash
go test ./internal/workspace/ -run "TestGetOrCreate_RecyclableBranchCollision" -count=1 -timeout=60s
```

### 6e. Commit

```bash
git add internal/workspace/manager.go internal/workspace/manager_integration_test.go
```

---

## Step 7: Add `Purge` and `PurgeAll` methods + interface update

**Files**: `internal/workspace/manager.go`, `internal/workspace/interfaces.go`

### 7a. Write failing test

**File**: `internal/workspace/manager_test.go`

```go
func TestPurge_DeletesRecyclableWorkspace(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{
		WorkspacePath:     tmpDir,
		RecycleWorkspaces: true,
	}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	workspaceID := "test-001"
	workspacePath := filepath.Join(tmpDir, workspaceID)
	os.MkdirAll(workspacePath, 0755)
	exec.Command("git", "init", "-q", workspacePath).Run()

	st.AddWorkspace(state.Workspace{
		ID:     workspaceID,
		Repo:   "test",
		Branch: "main",
		Path:   workspacePath,
		Status: state.WorkspaceStatusRecyclable,
	})

	err := m.Purge(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("Purge() error = %v", err)
	}

	// Directory should be deleted
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Error("workspace directory should be deleted after purge")
	}

	// Workspace should be removed from state
	_, found := st.GetWorkspace(workspaceID)
	if found {
		t.Error("workspace should be removed from state after purge")
	}
}

func TestPurge_RejectsNonRecyclableWorkspace(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	st.AddWorkspace(state.Workspace{
		ID:     "test-001",
		Repo:   "test",
		Branch: "main",
		Path:   filepath.Join(tmpDir, "test-001"),
		Status: state.WorkspaceStatusRunning,
	})

	err := m.Purge(context.Background(), "test-001")
	if err == nil {
		t.Error("Purge() should reject non-recyclable workspace")
	}
}

func TestPurgeAll_DeletesAllRecyclable(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{
		WorkspacePath:     tmpDir,
		RecycleWorkspaces: true,
	}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	// Create two recyclable and one running workspace
	for i, status := range []string{state.WorkspaceStatusRecyclable, state.WorkspaceStatusRecyclable, state.WorkspaceStatusRunning} {
		id := fmt.Sprintf("test-%03d", i+1)
		path := filepath.Join(tmpDir, id)
		os.MkdirAll(path, 0755)
		exec.Command("git", "init", "-q", path).Run()
		st.AddWorkspace(state.Workspace{
			ID: id, Repo: "test", Branch: "main", Path: path, Status: status,
		})
	}

	purged, err := m.PurgeAll(context.Background(), "")
	if err != nil {
		t.Fatalf("PurgeAll() error = %v", err)
	}
	if purged != 2 {
		t.Errorf("PurgeAll() purged %d, want 2", purged)
	}

	// Running workspace should still exist
	_, found := st.GetWorkspace("test-003")
	if !found {
		t.Error("running workspace should not be purged")
	}
}
```

### 7b. Run test to verify it fails

```bash
go test ./internal/workspace/ -run "TestPurge" -count=1
```

### 7c. Write implementation

**File**: `internal/workspace/manager.go` — add after `DisposeForce`:

```go
// Purge permanently deletes a recyclable workspace (files + state).
// Unlike Dispose, this always deletes regardless of RecycleWorkspaces config.
// Returns error if workspace is not recyclable.
func (m *Manager) Purge(ctx context.Context, workspaceID string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}
	if w.Status != state.WorkspaceStatusRecyclable {
		return fmt.Errorf("workspace %s is not recyclable (status: %s)", workspaceID, w.Status)
	}

	// force=true (skip safety checks), skipRecycling=true (actually delete)
	return m.dispose(ctx, workspaceID, true, true)
}

// PurgeAll permanently deletes all recyclable workspaces.
// If repoURL is non-empty, only purges workspaces for that repo.
// Returns the number of workspaces purged.
func (m *Manager) PurgeAll(ctx context.Context, repoURL string) (int, error) {
	var toPurge []string
	for _, w := range m.state.GetWorkspaces() {
		if w.Status != state.WorkspaceStatusRecyclable {
			continue
		}
		if repoURL != "" && w.Repo != repoURL {
			continue
		}
		toPurge = append(toPurge, w.ID)
	}

	purged := 0
	for _, id := range toPurge {
		if err := m.Purge(ctx, id); err != nil {
			m.logger.Warn("failed to purge workspace", "id", id, "err", err)
			continue
		}
		purged++
	}
	return purged, nil
}
```

**File**: `internal/workspace/interfaces.go` — add `Purge` and `PurgeAll` to the `WorkspaceManager` interface, after `DisposeForce` (line 115):

```go
	// Purge permanently deletes a recyclable workspace (files + state).
	Purge(ctx context.Context, workspaceID string) error

	// PurgeAll permanently deletes all recyclable workspaces for a repo (or all repos if empty).
	PurgeAll(ctx context.Context, repoURL string) (int, error)
```

Without this, `s.workspace.Purge()` in the dashboard handlers won't compile — `Server.workspace` is typed as `WorkspaceManager` (interface), not `*Manager` (concrete).

### 7d. Run test to verify it passes

```bash
go test ./internal/workspace/ -run "TestPurge" -count=1
```

### 7e. Commit

```bash
git add internal/workspace/manager.go internal/workspace/manager_test.go
```

---

## Step 8: Exclude recyclable workspaces from `UpdateAllVCSStatus` and `EnsureAll`

**File**: `internal/workspace/manager.go`

### 8a. Write failing test

**File**: `internal/workspace/manager_test.go`

```go
func TestUpdateAllVCSStatus_SkipsRecyclable(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	recyclablePath := filepath.Join(tmpDir, "recyclable-001")
	os.MkdirAll(recyclablePath, 0755)
	exec.Command("git", "init", "-q", recyclablePath).Run()

	st.AddWorkspace(state.Workspace{
		ID:     "recyclable-001",
		Repo:   "test",
		Branch: "main",
		Path:   recyclablePath,
		Status: state.WorkspaceStatusRecyclable,
	})

	// UpdateAllVCSStatus should not panic or error on recyclable workspaces.
	// More importantly, it should skip them entirely (we verify by checking
	// that git status fields remain zero — not updated).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.UpdateAllVCSStatus(ctx)

	w, _ := st.GetWorkspace("recyclable-001")
	// Status should still be recyclable (not modified by polling)
	if w.Status != state.WorkspaceStatusRecyclable {
		t.Errorf("status changed to %q during polling, expected recyclable", w.Status)
	}
}
```

### 8b. Run test to verify it fails

```bash
go test ./internal/workspace/ -run "TestUpdateAllVCSStatus_SkipsRecyclable" -count=1 -timeout=30s
```

### 8c. Write implementation

**File**: `internal/workspace/manager.go`

**In `UpdateAllVCSStatus`** (line 977), add the recyclable filter:

```go
	for _, w := range workspaces {
		if w.RemoteHostID != "" {
			continue
		}
		if w.Status == state.WorkspaceStatusRecyclable {
			continue
		}
		localWorkspaces = append(localWorkspaces, w)
	}
```

**In `EnsureAll`** (line 1026), add the same filter:

```go
	for _, w := range m.state.GetWorkspaces() {
		if w.RemoteHostID != "" {
			continue
		}
		if w.Status == state.WorkspaceStatusRecyclable {
			continue
		}
		if err := m.ensurer.ForWorkspace(w.ID); err != nil {
			m.logger.Warn("failed to ensure workspace", "id", w.ID, "err", err)
		}
	}
```

### 8d. Run test to verify it passes

```bash
go test ./internal/workspace/ -run "TestUpdateAllVCSStatus_SkipsRecyclable" -count=1 -timeout=30s
```

### 8e. Commit

```bash
git add internal/workspace/manager.go internal/workspace/manager_test.go
```

---

## Step 9: Crash recovery for stale "disposing" status

**File**: `internal/daemon/daemon.go`

### 9a. Write failing test

This is tricky to unit test in isolation since the crash recovery is in the daemon startup goroutine. Instead, verify the behavior by testing the logic directly. The existing crash recovery (line 780) calls `DisposeForce`. With `RecycleWorkspaces` enabled, `DisposeForce` now recycles instead of deleting. This means the existing crash recovery code **already does the right thing** — a stuck "disposing" workspace will be recycled (set to "recyclable") instead of deleted.

However, the current code retries the _full_ disposal, including safety checks and file deletion attempts. With recycling, the `dispose()` function's early return handles this correctly. No code change needed here beyond what Step 3 already provides.

**Verify**: Add a comment in `daemon.go` near line 780 explaining the interaction:

```go
	// Reconcile workspaces/sessions stuck in "disposing" status from a previous crash.
	// When recycle_workspaces is enabled, DisposeForce will recycle (mark as recyclable)
	// instead of deleting, which is the correct recovery behavior — the workspace
	// directory is preserved for future reuse.
	// Run in a goroutine to avoid blocking daemon startup (disposal can take up to 60s).
```

### 9b. Commit

```bash
git add internal/daemon/daemon.go
```

---

## Step 10: Server-side filtering in `buildSessionsResponse`

**File**: `internal/dashboard/handlers_sessions.go`

### 10a. Write failing test

**File**: `internal/dashboard/handlers_sessions_test.go`

Find an existing test for `buildSessionsResponse` or the `/api/sessions` endpoint. Add a test case that creates a recyclable workspace and verifies it's excluded from the response.

```go
func TestBuildSessionsResponse_ExcludesRecyclableWorkspaces(t *testing.T) {
	server, _, st := newTestServer(t)

	st.AddWorkspace(state.Workspace{
		ID:     "active-001",
		Repo:   "test",
		Branch: "main",
		Path:   "/tmp/active",
		Status: state.WorkspaceStatusRunning,
	})
	st.AddWorkspace(state.Workspace{
		ID:     "recycled-001",
		Repo:   "test",
		Branch: "old-branch",
		Path:   "/tmp/recycled",
		Status: state.WorkspaceStatusRecyclable,
	})

	response := server.buildSessionsResponse()

	for _, item := range response {
		if item.ID == "recycled-001" {
			t.Error("recyclable workspace should not appear in buildSessionsResponse")
		}
	}

	found := false
	for _, item := range response {
		if item.ID == "active-001" {
			found = true
			break
		}
	}
	if !found {
		t.Error("active workspace should appear in buildSessionsResponse")
	}
}
```

### 10b. Run test to verify it fails

```bash
go test ./internal/dashboard/ -run "TestBuildSessionsResponse_ExcludesRecyclable" -count=1
```

### 10c. Write implementation

**File**: `internal/dashboard/handlers_sessions.go` — in `buildSessionsResponse()` (line 97), add a filter at the start of the workspace loop:

```go
	for _, ws := range workspaces {
		// Hide recyclable workspaces from the dashboard
		if ws.Status == state.WorkspaceStatusRecyclable {
			continue
		}
		// ... existing logic
```

### 10d. Run test to verify it passes

```bash
go test ./internal/dashboard/ -run "TestBuildSessionsResponse_ExcludesRecyclable" -count=1
```

### 10e. Commit

```bash
git add internal/dashboard/handlers_sessions.go internal/dashboard/handlers_sessions_test.go
```

---

## Step 11: Add purge HTTP endpoints

**File**: `internal/dashboard/handlers_dispose.go`, `internal/dashboard/server.go`

### 11a. Write failing test

**File**: `internal/dashboard/handlers_dispose_test.go`

```go
func TestHandlePurgeWorkspace(t *testing.T) {
	server, _, st := newTestServer(t)

	workspacePath := filepath.Join(t.TempDir(), "test-001")
	os.MkdirAll(workspacePath, 0755)
	exec.Command("git", "init", "-q", workspacePath).Run()

	st.AddWorkspace(state.Workspace{
		ID:     "test-001",
		Repo:   "test",
		Branch: "main",
		Path:   workspacePath,
		Status: state.WorkspaceStatusRecyclable,
	})

	req := makeWorkspaceRequest(t, http.MethodDelete, "/api/workspaces/test-001/purge", "test-001", nil)
	rr := httptest.NewRecorder()
	server.handlePurgeWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	_, found := st.GetWorkspace("test-001")
	if found {
		t.Error("workspace should be removed after purge")
	}
}

func TestHandlePurgeAll(t *testing.T) {
	server, _, st := newTestServer(t)

	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("test-%03d", i+1)
		path := filepath.Join(t.TempDir(), id)
		os.MkdirAll(path, 0755)
		exec.Command("git", "init", "-q", path).Run()
		st.AddWorkspace(state.Workspace{
			ID: id, Repo: "test", Branch: "main", Path: path,
			Status: state.WorkspaceStatusRecyclable,
		})
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/workspaces/purge", nil)
	rr := httptest.NewRecorder()
	server.handlePurgeAll(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}
```

### 11b. Run test to verify it fails

```bash
go test ./internal/dashboard/ -run "TestHandlePurge" -count=1
```

### 11c. Write implementation

**File**: `internal/dashboard/handlers_dispose.go` — add handlers:

```go
func (s *Server) handlePurgeWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		writeJSONError(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := s.workspace.Purge(ctx, workspaceID); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.previewManager != nil {
		s.previewManager.DeleteWorkspace(workspaceID)
	}

	go s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handlePurgeAll(w http.ResponseWriter, r *http.Request) {
	repoURL := r.URL.Query().Get("repo")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	purged, err := s.workspace.PurgeAll(ctx, repoURL)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	go s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"purged": purged,
	})
}

func (s *Server) handleGetRecyclableWorkspaces(w http.ResponseWriter, r *http.Request) {
	workspaces := s.state.GetWorkspaces()
	total := 0
	byRepo := make(map[string]int)
	for _, ws := range workspaces {
		if ws.Status != state.WorkspaceStatusRecyclable {
			continue
		}
		total++
		// Use repo name from config if available, fallback to URL
		repoName := ws.Repo
		if rc, found := s.config.FindRepoByURL(ws.Repo); found {
			repoName = rc.Name
		}
		byRepo[repoName]++
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total":   total,
		"by_repo": byRepo,
	})
}
```

**File**: `internal/dashboard/server.go` — register routes:

Inside the `r.Route("/workspaces/{workspaceID}", ...)` block (line 691), add with the other workspace-specific routes (after line 724, near `dispose` and `dispose-all`):

```go
				r.Delete("/purge", s.handlePurgeWorkspace)
```

Note: routes inside the route group use **relative paths** — the `{workspaceID}` prefix is already provided by the group.

In the authenticated write group (after line 644), add the non-ID-scoped routes:

```go
			r.Delete("/workspaces/purge", s.handlePurgeAll)
			r.Get("/workspaces/recyclable", s.handleGetRecyclableWorkspaces)
```

### 11d. Run test to verify it passes

```bash
go test ./internal/dashboard/ -run "TestHandlePurge" -count=1
```

### 11e. Commit

```bash
git add internal/dashboard/handlers_dispose.go internal/dashboard/handlers_dispose_test.go internal/dashboard/server.go
```

---

## Step 12: Add recyclable workspace indicator to dashboard UI

**File**: `assets/dashboard/src/components/AppShell.tsx` (or the workspace list component)

### 12a. Implementation

This step is frontend-only. Add a component that:

1. Fetches `GET /api/workspaces/recyclable` on mount
2. If `total > 0`, renders a collapsed indicator below the workspace list:
   ```
   ▸ N recyclable workspaces  [Purge]
   ```
3. "Purge" button calls `DELETE /api/workspaces/purge`
4. Refreshes count after purge

The exact component placement depends on the current workspace list layout. This is a two-way door — the UX can be iterated after the core feature works.

### 12b. Run tests

```bash
./test.sh --quick
```

### 12c. Commit

```bash
git add assets/dashboard/src/
```

---

## Step 13: End-to-end verification

Note: TypeScript type regeneration (`go run ./cmd/gen-types`) is **not needed** — workspace status constants are Go `const` strings, not struct fields in `internal/api/contracts/`. The type generator processes API contract structs, not constants. The frontend already receives workspace status as a string field in the WebSocket response.

### 13a. Run full test suite

```bash
./test.sh
```

### 13b. Manual verification

1. Set `"recycle_workspaces": true` in `~/.schmux/config.json`
2. Spawn a workspace on any repo/branch
3. Dispose the workspace (via dashboard "Dispose" button)
4. Verify the workspace directory still exists on disk
5. Spawn a new workspace on the same repo (different branch)
6. Verify it reuses the same directory (same workspace ID, same path)
7. Verify the dashboard shows the workspace as running
8. Check the "recyclable workspaces" indicator and purge button work
9. Verify `./test.sh` still passes with `recycle_workspaces: false` (default)

### 13c. Update API docs

**File**: `docs/api.md` — document the new endpoints:

```
DELETE /api/workspaces/{id}/purge     — Purge a recyclable workspace
DELETE /api/workspaces/purge?repo=URL — Purge all recyclable workspaces (optional repo filter)
GET    /api/workspaces/recyclable     — Get recyclable workspace counts
```

Document the new `recycle_workspaces` config field and the `"recyclable"` workspace status.
