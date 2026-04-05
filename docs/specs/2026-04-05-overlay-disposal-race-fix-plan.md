# Plan: Fix overlay/disposal race condition

**Goal**: Eliminate all race windows between the overlay compounding system and workspace disposal, so that no overlay reads, merges, or propagations occur on a workspace whose files are being deleted.

**Architecture**: Cancel-then-reconcile with unconditional removal as the safety gate. `dispose()` becomes the single authority for the final overlay reconcile. The session-dispose background goroutine is cancelled before the synchronous reconcile runs — this is a best-effort optimization to avoid wasted work, not a hard stop. The actual safety guarantee is the unconditional `RemoveWorkspace` call, which stops all watches, cancels debounce timers, and removes the workspace from the compounder's map before `dispose()` proceeds to delete files. The propagator additionally checks workspace status to avoid writing into a disposing workspace.

**Tech Stack**: Go, `context.CancelFunc`, `sync.Mutex`

### Pre-existing bug discovered during analysis

The current `SetCompoundReconcile` callback at `daemon.go:996` checks `compoundGen[workspaceID] != 0` to decide whether to call `RemoveWorkspace`. This condition is **always true** after the first session spawn (since spawn increments the counter to >= 1 and nothing resets it to 0). This means `RemoveWorkspace` was **never called** from the workspace-dispose path — it was effectively dead code. Only the session-dispose background goroutine ever cleaned up, and if that goroutine raced or was slow, workspace entries leaked in the compounder's `workspaces` map indefinitely. This plan fixes the bug by switching to unconditional removal.

### Residual narrow window (accepted)

After `RemoveWorkspace` calls `watcher.RemoveWorkspace` (which calls `timer.Stop()` on debounce timers), a debounce callback that was already dequeued by the Go runtime may still fire. This callback calls `processFileChange(context.Background(), ...)`, which checks `c.workspaces[workspaceID]` under RLock. If it acquires the RLock before `RemoveWorkspace` acquires the write lock, it proceeds to read files. However: the file read hits ENOENT (file already deleted) → `DetermineMergeAction` returns `(MergeActionSkip, error)` → `processFileChange` logs and returns. No propagation occurs. This is a harmless log-noise window, not a correctness issue. Adding a per-workspace "removed" flag would close it but is not worth the complexity.

---

## Dependency Table

| Group | Steps     | Can Parallelize | Notes                                    |
| ----- | --------- | --------------- | ---------------------------------------- |
| 1     | Steps 1-2 | Yes             | Independent: Compounder API + propagator |
| 2     | Step 3    | No              | Depends on Step 1 (new Compounder API)   |
| 3     | Step 4    | No              | Depends on Step 3 (wiring)               |
| 4     | Step 5    | No              | Integration test, depends on all above   |

---

## Step 1: Add `CancelReconcile` API and early exit to Compounder

**Files**: `internal/compound/compound.go`, `internal/compound/compound_test.go`

### 1a. Write failing test

**File**: `internal/compound/compound_test.go`

Add a test that verifies cancelling a reconcile context aborts the reconcile mid-walk. Uses a slow LLM executor to ensure the reconcile doesn't complete before cancellation fires. Asserts that NOT all files were processed.

```go
func TestCompounder_CancelReconcile(t *testing.T) {
	overlayDir := t.TempDir()
	wsDir := t.TempDir()

	// Create enough files with three-way divergence to force LLM merge path
	// (workspace != manifest, overlay != manifest, workspace != overlay).
	// The slow executor ensures reconcile is still running when we cancel.
	os.MkdirAll(filepath.Join(overlayDir, "dir"), 0755)
	os.MkdirAll(filepath.Join(wsDir, "dir"), 0755)
	manifest := make(map[string]string)
	for i := range 20 {
		name := fmt.Sprintf("file-%02d.txt", i)
		relPath := filepath.Join("dir", name)
		original := fmt.Sprintf("original-%d", i)
		// All three copies differ → forces LLM merge path
		os.WriteFile(filepath.Join(overlayDir, relPath), []byte(fmt.Sprintf("overlay-%d", i)), 0644)
		os.WriteFile(filepath.Join(wsDir, relPath), []byte(fmt.Sprintf("workspace-%d", i)), 0644)
		manifest[relPath] = HashBytes([]byte(original))
	}

	// Slow LLM executor: sleeps 200ms per call, ensures cancellation is observable
	var mergeCount atomic.Int32
	slowExecutor := func(ctx context.Context, prompt string, timeout time.Duration) (string, error) {
		mergeCount.Add(1)
		select {
		case <-time.After(200 * time.Millisecond):
			return "merged", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	c, err := NewCompounder(100, 5*time.Second, slowExecutor, nil, nil, log.NewWithOptions(io.Discard, log.Options{}))
	if err != nil {
		t.Fatalf("NewCompounder() error = %v", err)
	}
	defer c.Stop()

	c.AddWorkspace("ws-001", wsDir, overlayDir, "repo", manifest, nil)

	// Start reconcile in a goroutine
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.Reconcile(ctx, "ws-001")
		close(done)
	}()

	// Let a few files process, then cancel
	time.Sleep(300 * time.Millisecond)
	c.SetReconcileCancel("ws-001", cancel)
	c.CancelReconcile("ws-001")

	// Reconcile goroutine should exit promptly
	select {
	case <-done:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("Reconcile did not exit after CancelReconcile")
	}

	// Verify cancellation actually stopped work early
	processed := mergeCount.Load()
	if processed >= 20 {
		t.Errorf("expected cancellation to stop reconcile early, but all 20 files were processed (mergeCount=%d)", processed)
	}
	t.Logf("cancellation stopped reconcile after %d/%d merges", processed, 20)
}
```

### 1b. Run test to verify it fails

```bash
go test ./internal/compound/ -run TestCompounder_CancelReconcile -count=1
```

This fails because `SetReconcileCancel` and `CancelReconcile` don't exist yet.

### 1c. Write implementation

**File**: `internal/compound/compound.go`

**Change 1**: Add a `reconcileCancels` map to the `Compounder` struct and initialize it in `NewCompounder`:

```go
// In the Compounder struct, add after the existing fields:
	reconcileCancels map[string]context.CancelFunc // workspaceID → cancel func for background reconcile

// In NewCompounder, initialize the map:
	reconcileCancels: make(map[string]context.CancelFunc),
```

**Change 2**: Add two new methods:

```go
// SetReconcileCancel stores a cancel function for a workspace's background reconcile goroutine.
// Calling cancel() is idempotent per the Go spec — safe to call from both CancelReconcile
// and the goroutine's defer.
func (c *Compounder) SetReconcileCancel(workspaceID string, cancel context.CancelFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reconcileCancels[workspaceID] = cancel
}

// CancelReconcile cancels any in-flight background reconcile for the workspace and removes
// the cancel func. This is best-effort — with few overlay files, the reconcile may complete
// before the cancellation is observed. The real safety guarantee is RemoveWorkspace, which
// unconditionally stops all watches and removes the workspace from the map.
func (c *Compounder) CancelReconcile(workspaceID string) {
	c.mu.Lock()
	cancel := c.reconcileCancels[workspaceID]
	delete(c.reconcileCancels, workspaceID)
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
```

**Change 3**: Update `RemoveWorkspace` to clean up any stored cancel func (defensive — prevents leaks if `CancelReconcile` wasn't called first):

```go
func (c *Compounder) RemoveWorkspace(workspaceID string) {
	c.watcher.RemoveWorkspace(workspaceID)
	c.mu.Lock()
	delete(c.workspaces, workspaceID)
	if cancel, ok := c.reconcileCancels[workspaceID]; ok {
		cancel()
		delete(c.reconcileCancels, workspaceID)
	}
	c.mu.Unlock()
}
```

**Change 4**: Add early `ctx.Err()` check at the top of `processFileChange` (compound.go line 139), before the workspace lookup and any file I/O. This provides finer-grained cancellation within individual files, not just between files in the `Reconcile` loop:

```go
func (c *Compounder) processFileChange(ctx context.Context, workspaceID, relPath string) {
	// Early exit if context is cancelled (e.g., workspace being disposed)
	if ctx.Err() != nil {
		return
	}

	// Validate relPath to prevent path traversal
	// ... (rest unchanged)
```

### 1d. Run test to verify it passes

```bash
go test ./internal/compound/ -run TestCompounder_CancelReconcile -count=1
```

### 1e. Verify no regressions

```bash
go test ./internal/compound/ -count=1
```

---

## Step 2: Add workspace status check to propagator

**File**: `internal/daemon/daemon.go`

### 2a. Write failing test

This is in the propagator closure which is an inline function in daemon.go — not easily unit-testable in isolation. We'll verify this via the integration test in Step 5. For now, this step is a code change with manual reasoning verification.

### 2b. Write implementation

In the propagator closure (daemon.go, inside the `for _, w := range repoWorkspaces[repoURL]` loop, around line 871), add a status check after the source-workspace skip and before the active-sessions skip:

```go
// Existing: skip source workspace
if w.ID == sourceWorkspaceID {
    continue
}
// NEW: skip workspaces that are being disposed or are recyclable
if w.Status == state.WorkspaceStatusDisposing || w.Status == state.WorkspaceStatusRecyclable {
    continue
}
// Existing: skip workspaces without active sessions
if !activeWorkspaces[w.ID] {
    continue
}
```

This requires the `state` package import, which is already present in daemon.go.

### 2c. Verify compilation

```bash
go build ./internal/daemon/
```

---

## Step 3: Wire cancel-then-reconcile in daemon callbacks

**File**: `internal/daemon/daemon.go`

This is the core step. Two changes to the daemon callback wiring (lines ~946-1001).

### 3a. Understand the current flow (no test — wiring change)

Current flow has two independent paths:

1. **Session dispose** (line 969-987): background goroutine does `Reconcile()` then `RemoveWorkspace()`
2. **Workspace dispose** (line 991-1001): synchronous `Reconcile()`, conditional `RemoveWorkspace()`

These race. The fix makes workspace dispose cancel the session-dispose goroutine.

### 3b. Write implementation

**Change 1: Session-dispose callback** — store the cancel func so workspace dispose can cancel it.

Replace the `!isSpawn` branch (lines 969-987) with:

```go
} else {
    // Last session disposed — reconcile overlay files before the workspace goes dormant.
    // Run in a goroutine to avoid blocking the dispose HTTP handler.
    compoundGenMu.Lock()
    gen := compoundGen[workspaceID]
    compoundGenMu.Unlock()

    ctx, cancel := context.WithCancel(context.Background())
    compounder.SetReconcileCancel(workspaceID, cancel)

    go func() {
        defer cancel() // clean up context resources when goroutine exits naturally

        reconcileCtx, reconcileCancel := context.WithTimeout(ctx, 2*time.Minute)
        defer reconcileCancel()

        compounder.Reconcile(reconcileCtx, workspaceID)
        compoundGenMu.Lock()
        stale := compoundGen[workspaceID] != gen
        compoundGenMu.Unlock()
        if stale {
            compoundLog.Info("skipping RemoveWorkspace (workspace re-added during reconcile)", "workspace_id", workspaceID)
            return
        }
        compounder.RemoveWorkspace(workspaceID)
    }()
}
```

Key change: the goroutine's context is derived from a cancellable parent. `SetReconcileCancel` stores the cancel func. If `dispose()` calls `CancelReconcile`, the `ctx` is cancelled, `reconcileCtx` inherits the cancellation, and `Reconcile` exits early at its `ctx.Err()` check (both between files in the Reconcile loop and at the top of `processFileChange`).

Note on `defer cancel()`: calling a `CancelFunc` multiple times is safe per the Go spec — the second call is a no-op. The goroutine's `defer cancel()` is for resource cleanup when no workspace disposal occurs (the goroutine finishes naturally). If `CancelReconcile` already called it, the defer is harmless.

**Change 2: Workspace-dispose callback** — cancel background goroutine first, then do authoritative reconcile + unconditional remove.

Replace `SetCompoundReconcile` (lines 991-1001) with:

```go
wm.SetCompoundReconcile(func(workspaceID string) {
    // Cancel any in-flight background reconcile from session dispose.
    // Best-effort: with few overlay files the goroutine may have already finished.
    // The real safety guarantee is RemoveWorkspace below.
    compounder.CancelReconcile(workspaceID)

    // Run the authoritative reconcile synchronously.
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()
    compounder.Reconcile(ctx, workspaceID)

    // Unconditionally remove workspace from the compounder.
    // This stops all watches and cancels debounce timers BEFORE
    // dispose() proceeds to delete files. This is the hard safety gate.
    compounder.RemoveWorkspace(workspaceID)
})
```

This replaces the old `compoundGen[workspaceID] != 0` conditional — which was always true after the first spawn, meaning `RemoveWorkspace` was effectively dead code (see "Pre-existing bug" above) — with unconditional removal. The generation counter is still used by the session-dispose callback to prevent stale goroutines from removing re-added workspaces — that logic is unchanged. But the workspace-dispose path no longer defers to the session-dispose goroutine; it takes full ownership.

**Note on manifest consistency**: If the cancelled background goroutine partially updated the manifest (processed some files before cancellation), the authoritative reconcile may re-process those files. This is harmless — `DetermineMergeAction` will see that the hashes now match (workspace == manifest) and skip them.

### 3c. Verify compilation

```bash
go build ./internal/daemon/
```

### 3d. Verify no regressions

```bash
go test ./internal/compound/ -count=1
```

---

## Step 4: Remove redundant unconditional `RemoveWorkspace` safety net

**File**: `internal/workspace/manager.go`

### 4a. Rationale (no code change needed)

After Step 3, `compoundReconcile` already calls `RemoveWorkspace()` unconditionally. The existing `dispose()` flow at lines 1259-1262:

```go
if m.compoundReconcile != nil {
    m.compoundReconcile(workspaceID)
}
```

...already guarantees removal before file deletion. No additional change is needed in `manager.go` — the callback does the work.

Verify the ordering is correct:

1. Line 1260-1262: `compoundReconcile()` → cancels goroutine, reconciles, removes workspace
2. Line 1265-1267: `gitWatcher.RemoveWorkspace()` → removes git watches
3. Line 1303+: file deletion → safe, overlay system is fully off

This step is a verification checkpoint — no code change required.

---

## Step 5: End-to-end test — overlay quiesces before file deletion

**File**: `internal/compound/compound_test.go`

### 5a. Write tests

Three tests covering the full lifecycle and edge cases.

```go
func TestCompounder_RemoveWorkspace_StopsArmedDebounce(t *testing.T) {
	// This test exercises Race Window 2: a debounce timer is armed BEFORE
	// RemoveWorkspace, and we verify it does NOT fire after removal.
	overlayDir := t.TempDir()
	wsDir := t.TempDir()

	relPath := filepath.Join(".claude", "settings.json")
	os.MkdirAll(filepath.Join(overlayDir, ".claude"), 0755)
	os.MkdirAll(filepath.Join(wsDir, ".claude"), 0755)
	originalContent := `{"permissions": ["read"]}`
	os.WriteFile(filepath.Join(overlayDir, relPath), []byte(originalContent), 0644)
	os.WriteFile(filepath.Join(wsDir, relPath), []byte(originalContent), 0644)
	manifest := map[string]string{relPath: HashBytes([]byte(originalContent))}

	var propagateCount atomic.Int32

	// Use a long debounce (500ms) so the timer is still armed when we remove
	c, err := NewCompounder(500, 5*time.Second, nil, func(sourceWorkspaceID, repoURL, rp string, content []byte) {
		propagateCount.Add(1)
	}, nil, log.NewWithOptions(io.Discard, log.Options{}))
	if err != nil {
		t.Fatalf("NewCompounder() error = %v", err)
	}
	defer c.Stop()

	c.AddWorkspace("ws-001", wsDir, overlayDir, "repo", manifest, nil)
	c.Start()

	// Modify file to arm the debounce timer (fsnotify will detect this)
	os.WriteFile(filepath.Join(wsDir, relPath), []byte(`{"permissions": ["read", "write"]}`), 0644)

	// Give fsnotify time to deliver the event and arm the debounce timer,
	// but NOT enough for the 500ms debounce to fire
	time.Sleep(100 * time.Millisecond)

	// Remove workspace — this should cancel the armed debounce timer
	c.RemoveWorkspace("ws-001")

	// Wait well past the debounce window
	time.Sleep(700 * time.Millisecond)

	if propagateCount.Load() != 0 {
		t.Errorf("expected 0 propagations after RemoveWorkspace cancelled debounce, got %d", propagateCount.Load())
	}

	// Reconcile after removal should be a no-op (workspace not in map)
	c.Reconcile(context.Background(), "ws-001")
	if propagateCount.Load() != 0 {
		t.Errorf("expected 0 propagations after Reconcile on removed workspace, got %d", propagateCount.Load())
	}
}

func TestCompounder_RemoveWorkspace_Idempotent(t *testing.T) {
	c, err := NewCompounder(100, 5*time.Second, nil, nil, nil, log.NewWithOptions(io.Discard, log.Options{}))
	if err != nil {
		t.Fatalf("NewCompounder() error = %v", err)
	}
	defer c.Stop()

	// RemoveWorkspace on a workspace that was never added should not panic
	c.RemoveWorkspace("nonexistent")
	c.RemoveWorkspace("nonexistent") // double remove

	// Add and remove twice
	wsDir := t.TempDir()
	overlayDir := t.TempDir()
	c.AddWorkspace("ws-001", wsDir, overlayDir, "repo", nil, nil)
	c.RemoveWorkspace("ws-001")
	c.RemoveWorkspace("ws-001") // idempotent
}

func TestCompounder_CancelReconcile_NoGoroutine(t *testing.T) {
	// CancelReconcile when no background goroutine is running should be a no-op
	c, err := NewCompounder(100, 5*time.Second, nil, nil, nil, log.NewWithOptions(io.Discard, log.Options{}))
	if err != nil {
		t.Fatalf("NewCompounder() error = %v", err)
	}
	defer c.Stop()

	// Should not panic
	c.CancelReconcile("nonexistent")
	c.CancelReconcile("nonexistent") // double cancel
}
```

### 5b. Run tests

```bash
go test ./internal/compound/ -count=1 -race
```

### 5c. Run full test suite

```bash
./test.sh --quick
```

---

## Summary of changes

| File                                 | Change                                                                                                                                                                                                                                                                                            |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/compound/compound.go`      | Add `reconcileCancels` map, `SetReconcileCancel()`, `CancelReconcile()` methods. Update `RemoveWorkspace()` to clean up cancel funcs. Add `ctx.Err()` early exit at top of `processFileChange`.                                                                                                   |
| `internal/compound/compound_test.go` | Add `TestCompounder_CancelReconcile` (slow executor, verifies early exit), `TestCompounder_RemoveWorkspace_StopsArmedDebounce` (arms timer before removal), `TestCompounder_RemoveWorkspace_Idempotent`, `TestCompounder_CancelReconcile_NoGoroutine`.                                            |
| `internal/daemon/daemon.go`          | Session-dispose callback: create cancellable context, store via `SetReconcileCancel`. Workspace-dispose callback: call `CancelReconcile`, then synchronous reconcile, then unconditional `RemoveWorkspace` (fixes pre-existing `!= 0` bug). Propagator: skip `disposing`/`recyclable` workspaces. |

No changes to `internal/workspace/manager.go` — the fix is entirely in the compound package and daemon wiring.
