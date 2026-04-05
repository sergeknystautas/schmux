VERDICT: NEEDS_REVISION

## Summary Assessment

The plan correctly identifies the three race windows and proposes a sound cancel-then-reconcile architecture. However, it has two critical issues: a double-cancel race on the same `context.CancelFunc`, and a missing race window in the debounce callback that is not fully covered by the proposed changes.

## Critical Issues (must fix)

### C1: Double-cancel of the same `context.CancelFunc` creates a data race

In the proposed Step 3b session-dispose callback, the goroutine does `defer cancel()` and `CancelReconcile` also calls the same `cancel()` stored via `SetReconcileCancel`. When `CancelReconcile` fires (from the workspace-dispose path), it calls `cancel()`. Then when the goroutine exits (either naturally or due to cancellation), the deferred `cancel()` fires again on the same `CancelFunc`.

While calling `cancel()` twice is safe per the Go spec (`context.WithCancel` explicitly allows multiple calls), the real problem is the `CancelReconcile` method does `delete(c.reconcileCancels, workspaceID)` under `c.mu.Lock()`, but the goroutine's `defer cancel()` calls the same function without any coordination. This is not itself a data race (since `cancel()` is safe to call concurrently), but it creates a subtle correctness issue: after `CancelReconcile` deletes the entry and calls `cancel()`, the goroutine's deferred `cancel()` is a no-op -- which is fine. However, consider this sequence:

1. Session-dispose goroutine starts, stores `cancel` via `SetReconcileCancel`
2. New session spawns for same workspace, `AddWorkspace` called, generation incremented
3. Session-dispose goroutine finishes naturally, `defer cancel()` fires (harmless)
4. Second session disposes, creates a NEW `ctx2, cancel2`, stores `cancel2` via `SetReconcileCancel`
5. Workspace dispose calls `CancelReconcile` -- cancels `cancel2` (correct)

This sequence actually works. But the plan should explicitly document that `cancel()` is idempotent and that the `defer cancel()` in the goroutine is purely for resource cleanup when no workspace disposal occurs. The current wording does not address this and could confuse an implementer.

**More importantly**: there is a real timing issue. If `CancelReconcile` is called, and the goroutine has already passed the `ctx.Err()` check but is mid-`processFileChange`, the file I/O in `DetermineMergeAction` and `ExecuteMerge` (fast-path) does NOT check `ctx.Err()`. The cancellation takes effect only at the next iteration of the `Reconcile` loop. For a typical overlay with 2-3 files, the reconcile could complete fully before the cancellation is observed. This means the "cancel" is not actually cancelling -- it is a best-effort signal that may arrive too late, and the workspace-dispose reconcile runs a second full reconcile. This is functionally correct but the plan presents cancellation as if it reliably stops the goroutine mid-reconcile, which is misleading.

**Fix**: The plan should acknowledge that cancellation is coarse-grained (between files only) and that the real safety comes from `RemoveWorkspace` being unconditional in the workspace-dispose callback, not from the cancellation being immediate. Also add `ctx.Err()` check at the top of `processFileChange` before file I/O for finer granularity.

### C2: Debounce timer race window not fully closed (Window 2)

The plan's Step 3 focuses on the background reconcile goroutine but does not address the debounce timer race in `onFileChange`. Here is the scenario:

1. An agent modifies a file at T=0
2. Debounce timer set for T=100ms (debounceMs=100)
3. At T=50ms, workspace dispose starts: `CancelReconcile` + synchronous `Reconcile` + `RemoveWorkspace`
4. `RemoveWorkspace` calls `watcher.RemoveWorkspace` which cancels debounce timers under `w.mu.Lock()`
5. But `time.AfterFunc` callbacks are scheduled by the Go runtime. If the timer's goroutine was already scheduled (the runtime picked it up between T=50ms and the `timer.Stop()` call), `timer.Stop()` returns `false` and the callback is already running or queued.
6. The callback checks `w.stopped` (which is `false` -- only set by `Stop()`, not `RemoveWorkspace`). It then calls `w.onChange(workspaceID, relPath)` which calls `c.processFileChange(context.Background(), workspaceID, relPath)`.
7. `processFileChange` checks `c.workspaces[workspaceID]` -- if `RemoveWorkspace` has already deleted it, this is safe (returns early). But if the timing is tight and `processFileChange` acquires the RLock before `RemoveWorkspace` acquires the write lock, it gets the workspace info and proceeds to do file I/O on workspace files that are about to be deleted.

The plan's `RemoveWorkspace` cleanup in Step 1c (calling `cancel()` and `delete(c.reconcileCancels, workspaceID)`) does not address this debounce path because `onFileChange` uses `context.Background()`, not the cancellable context.

The existing `Watcher.RemoveWorkspace` already cancels debounce timers (line 168-173), and `processFileChange` checks workspace membership (line 148-151), so this window is narrow. But `timer.Stop()` is documented as not guaranteed to prevent the callback from running if it was already dequeued. The plan should either:

- Acknowledge this as an accepted narrow window with harmless errors (the file I/O will fail gracefully), or
- Add a workspace "removed" flag checked by `onFileChange` before calling `processFileChange`

### C3: The `compoundGen[workspaceID] != 0` condition in the current `SetCompoundReconcile` callback is wrong, and the plan replaces it incorrectly

The current code at line 996 checks `compoundGen[workspaceID] != 0`. This is checking whether the generation counter is non-zero, which would be true if the workspace was ever added (since `AddWorkspace` increments it). The intent is to check staleness, but `!= 0` is always true after the first spawn. This means the current code NEVER calls `RemoveWorkspace` from the workspace-dispose path -- it was always considered "stale".

The plan's proposed replacement removes this check entirely and calls `RemoveWorkspace` unconditionally, which is the correct fix. However, the plan claims "The generation counter is still used by the session-dispose callback to prevent stale goroutines from removing re-added workspaces -- that logic is unchanged." This is true but the plan does not call out that the current `!= 0` check was a bug that meant `RemoveWorkspace` was effectively dead code in the workspace-dispose path. This should be documented as a discovered bug, since it means the existing code has been leaking workspace entries in the compounder's `workspaces` map for the entire time.

**Fix**: Add a note to the plan that the `!= 0` check was a pre-existing bug, and that the fix (unconditional removal) corrects this.

## Suggestions (nice to have)

### S1: Step 1a test is weak -- does not verify cancellation actually stops work

The test in Step 1a creates 3 small files, starts `Reconcile` in a goroutine, then immediately calls `CancelReconcile`. With 3 tiny text files, `Reconcile` likely completes before `CancelReconcile` is even called. The test only asserts the goroutine exits within 2 seconds, which would pass even without cancellation. A stronger test would:

- Use more files (e.g., 50) or inject a slow LLM executor to ensure cancellation actually interrupts work
- Assert that NOT all files were processed after cancellation

### S2: Step 5 test timing is fragile

The `TestCompounder_RemoveWorkspace_StopsAllOperations` test does `c.Start()` then immediately `c.RemoveWorkspace("ws-001")` then `time.Sleep(300ms)`. The debounce window is set to 100ms but no file change event is triggered between `Start()` and `RemoveWorkspace()`. The fsnotify watcher would only fire if the file was modified AFTER `Start()`. The test writes to the file AFTER `RemoveWorkspace`, which means the watcher is already removed -- so this test does not actually exercise window 2 (debounce timer firing during disposal). Consider writing a test where the file is modified BEFORE `RemoveWorkspace` but after `Start()`, with enough delay for the debounce timer to be armed.

### S3: Consider adding `ctx` parameter to `processFileChange` for the debounce path

Currently `onFileChange` hardcodes `context.Background()`. If a workspace is being disposed, this means debounce-triggered file processing runs with an uncancellable context. While `processFileChange` checks workspace membership, passing a cancellable context would provide defense in depth.

### S4: Step 2 has no unit test

The plan explicitly notes "We'll verify this via the integration test in Step 5" for the propagator status check. But Step 5 does not actually test the propagator at all -- it tests `RemoveWorkspace` and `CancelReconcile`. The propagator change (adding `WorkspaceStatusDisposing`/`WorkspaceStatusRecyclable` checks) has no test coverage. Consider at minimum adding a comment in the plan about how this will be verified.

### S5: Plan does not address `RemoveWorkspace` concurrency with `Reconcile`

If the workspace-dispose callback calls `CancelReconcile` then immediately calls `compounder.Reconcile()` (the authoritative reconcile), and simultaneously the background goroutine is still unwinding from the cancelled context (it might be in the middle of `processFileChange`), there could be concurrent access to `info.Manifest` -- one from the old goroutine's `processFileChange` (line 154, RLock) and one from the new authoritative `Reconcile` writing to `info.Manifest` (line 197, full Lock). This is actually safe because of the mutex, but the plan should note that the authoritative reconcile might read stale manifest data if the cancelled goroutine partially updated it.

## Verified Claims (things you confirmed are correct)

1. **File paths are accurate**: `internal/compound/compound.go`, `internal/compound/watcher.go`, `internal/compound/compound_test.go`, `internal/daemon/daemon.go`, `internal/workspace/manager.go`, `internal/state/state.go`, `internal/compound/merge.go` all exist and contain the structures/functions referenced.

2. **Line numbers are approximately correct**: The propagator closure is at lines 839-925, `SetCompoundCallback` at 946-989, `SetCompoundReconcile` at 991-1001, `dispose()` at 1210-1379. All within a few lines of the plan's references.

3. **`state` package is already imported in `daemon.go`**: Confirmed at line 45 (`"github.com/sergeknystautas/schmux/internal/state"`). The propagator status check will compile.

4. **`WorkspaceStatusDisposing` and `WorkspaceStatusRecyclable` constants exist**: Confirmed in `internal/state/state.go` at lines 88-89.

5. **`Reconcile` checks `ctx.Err()` between files**: Confirmed at line 123 of `compound.go`. The check is at the top of each iteration of the file loop.

6. **`Watcher.RemoveWorkspace` cancels debounce timers**: Confirmed at lines 167-173 of `watcher.go`. Timers are stopped and deleted.

7. **`processFileChange` checks workspace membership**: Confirmed at lines 148-151 of `compound.go`. If the workspace was removed from `c.workspaces`, the function returns early.

8. **The `dispose()` flow calls `compoundReconcile` before file deletion**: Confirmed at lines 1260-1261 of `manager.go`. The callback runs before `gitWatcher.RemoveWorkspace` (line 1265) and file deletion (line 1303+).

9. **`MarkWorkspaceDisposing` sets status before `dispose()` is called**: Confirmed in `handlers_dispose.go` -- the handler calls `MarkWorkspaceDisposing` (line 96/156) before `Dispose`/`DisposeForce` (line 111/222).

10. **Workspace status is available in the propagator loop**: The propagator iterates `st.GetWorkspaces()` which returns `state.Workspace` structs that include the `Status` field.

11. **`defer cancel()` is safe to call multiple times on a `context.CancelFunc`**: Per Go specification, calling a `CancelFunc` more than once after the first call does nothing.

12. **Generation counter logic in the session-dispose callback is correct**: The counter is incremented on `AddWorkspace` (spawn) and checked after `Reconcile` completes. If a new spawn happened during reconcile, the generation won't match and `RemoveWorkspace` is skipped.

13. **Test commands are correct**: `go test ./internal/compound/ -run TestName -count=1` and `./test.sh --quick` are valid for this codebase per the CLAUDE.md instructions.

14. **Tasks are approximately bite-sized**: Steps 1-4 are each 2-5 minutes of focused work. Step 5 is slightly larger but reasonable.

15. **Dependency groups avoid file conflicts**: Group 1 (Steps 1-2) touches `compound.go` and `daemon.go` respectively. Group 2 (Step 3) touches only `daemon.go`. No file conflicts between parallel steps in Group 1.
