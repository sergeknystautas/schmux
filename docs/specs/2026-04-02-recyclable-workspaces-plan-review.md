VERDICT: NEEDS_REVISION

## Summary Assessment

The plan is well-structured and covers most of the design spec, but has several critical issues: a race condition in Tier 0 that can cause two callers to claim the same recyclable workspace, a thread-safety problem in the temporary-disable-recycling pattern, a wrong variable binding in a test that will compile but test the wrong thing, and a missing interface update that will cause compile failures.

## Critical Issues (must fix)

### 1. Tier 0 race condition -- two callers can claim the same recyclable workspace (Step 4)

The plan places Tier 0 **before** the `repoLock` acquisition (line 392) and argues this is safe because "the worst case of a race is two spawns both trying to claim the same recyclable workspace -- one will find it already promoted to 'running' and skip it."

This argument is flawed. `GetWorkspaces()` returns a **copy** of the workspace slice (state.go line 445-446). Both goroutines take a snapshot where the workspace is still `"recyclable"`. Both proceed to:

1. Call `prepare()` -- which checks `hasActiveSessions` (no sessions yet, so passes)
2. Set `w.Branch = branch` on their **local copy**
3. Call `UpdateWorkspace(w)` -- both succeed (they're updating the same workspace by ID)
4. Return the same workspace to two different callers

Both callers then attach sessions to the same workspace directory. This is a data-corruption scenario, not a benign race.

**Fix**: Move Tier 0 inside the `repoLock`. The plan acknowledges this as an option ("if you prefer to keep it under the lock") but incorrectly dismisses the need. For local repos, the `local:` early return must be moved after the lock (or Tier 0 for local repos must use a separate lock).

### 2. Thread-unsafe temporary-disable-recycling pattern (Steps 6 and 7)

`purgeRecyclableWithBranch` and `Purge` both temporarily set `m.config.RecycleWorkspaces = false`, call `dispose()`, then restore it. This mutates a shared config field without holding any lock. `Config.RecycleWorkspaces` is a `bool` on the shared `*config.Config`, and any concurrent `dispose()` call on another goroutine reading `m.config.RecycleWorkspaces` will see the temporarily-false value and permanently delete a workspace that should have been recycled.

The `Config` struct has a `mu sync.RWMutex` for field access, but the plan directly writes to `m.config.RecycleWorkspaces` without acquiring it. Even if it did acquire it, holding the config lock during a potentially 60-second dispose would be a bottleneck.

**Fix**: Add a `force bool` or `skipRecycling bool` parameter to the internal `dispose()` method, or create a dedicated `purge()` method that always deletes. The temporary-mutate pattern is inherently unsafe in concurrent code.

### 3. Step 10 test destructures `newTestServer` wrong (Step 10)

The plan's test code:

```go
server, st, _ := newTestServer(t)
```

The actual function signature is:

```go
func newTestServer(t *testing.T) (*Server, *config.Config, *state.State)
```

So `st` would be `*config.Config`, not `*state.State`. The correct destructuring (as used everywhere else in the codebase) is:

```go
server, _, st := newTestServer(t)
```

This would compile but silently test the wrong thing -- `st.AddWorkspace` would fail at compile time since `*config.Config` has no `AddWorkspace` method, so actually this would be a compile error. Still, the variable binding is reversed.

### 4. `Purge` and `PurgeAll` must be added to `WorkspaceManager` interface (Step 7 / Step 11)

The dashboard's `Server` struct holds `workspace workspace.WorkspaceManager` (an interface). Step 11's handlers call `s.workspace.Purge()` and `s.workspace.PurgeAll()`. These methods are added to the concrete `Manager` struct in Step 7, but **never added to the `WorkspaceManager` interface** in `internal/workspace/interfaces.go`. This means the code will not compile.

**Fix**: Add `Purge(ctx context.Context, workspaceID string) error` and `PurgeAll(ctx context.Context, repoURL string) (int, error)` to the `WorkspaceManager` interface in `internal/workspace/interfaces.go`.

### 5. Step 2 test uses wrong package qualifier (Step 2)

The test is placed in `internal/state/state_test.go`, which uses package `state` (not `state_test`). The plan's test code references `state.WorkspaceStatusRecyclable`, but since the test is in the same package, it should be just `WorkspaceStatusRecyclable`. This will not compile.

### 6. Missing `RecycleWorkspaces` in config hot-reload (Step 1)

The plan says to add `c.RecycleWorkspaces = newCfg.RecycleWorkspaces` to the `applyConfigUpdate` method "around line 1623, near `SourceCodeManagement` assignment." The `SourceCodeManagement` assignment is at line 1624, but the method ends with `c.mu.Unlock()` at line 1652. Looking at the pattern, several fields from the struct (like `TmuxBinary`, `Timelapse`, `Subreddit`, `Repofeed`, `FloorManager`, `SaplingCommands`, `BuiltInSkills`) are also missing from the hot-reload path. This is likely intentional for fields that require restart.

However, `RecycleWorkspaces` should genuinely be hot-reloadable (it's a simple behavioral flag). The plan is correct to add it, but should note that the line reference "around line 1623" is actually where the existing field-by-field copy block runs under `c.mu.Lock()` (lines 1620-1652). The assignment should go inside this locked section, e.g., after line 1650.

## Suggestions (nice to have)

### 1. Step 3 is larger than it appears

Step 3 modifies the core `dispose()` function, which has complex branching for worktrees vs. clones vs. sapling, and interacts with multiple subsystems (overlays, watches, difftool, state). The implementation description says to insert recycling logic "after the watch removal block (line 1142) and before the file deletion logic (line 1144)." But the actual code at lines 1144-1195 handles multiple VCS backends, worktree cleanup, branch cleanup, and state removal. Understanding where to correctly insert the early return requires reading and comprehending all of that. This is more like 5-10 minutes, not 2-5.

### 2. Step 12 is underspecified

Step 12 is "Add recyclable workspace indicator to dashboard UI" but provides no concrete component code, no test, and just says "the exact component placement depends on the current workspace list layout." This makes it hard to estimate or parallelize. It should either have concrete code or be explicitly marked as a design-iteration step.

### 3. Step 13 may be a no-op

The plan says to regenerate TypeScript types and check for `WorkspaceStatusRecyclable`. But workspace status constants are Go `const` strings, not struct fields in `internal/api/contracts/`. The type generator processes struct definitions from `internal/api/contracts/*.go`. Unless `WorkspaceStatusRecyclable` is added to a contract struct or enum definition there, regeneration will produce no change. The plan should verify whether the constant needs to be in a contracts file.

### 4. Step 9 crash recovery claim needs nuance

The plan claims "no code change needed" for crash recovery because `DisposeForce` now recycles. This is correct for the recycling-enabled case. But the design spec (line 97-101) describes more detailed recovery logic:

- "If the directory still exists -> set status to 'recyclable' (if recycle_workspaces is on) or 'running' (if off)"
- "If the directory is gone -> remove the workspace from state"

The existing code at daemon.go line 785 calls `DisposeForce`, which would attempt a full dispose (including attempting `os.RemoveAll` on the directory). When recycling is off, this works. When recycling is on, `DisposeForce` will set status to "recyclable" -- which matches the design. But the "directory is gone" case is already handled by `dispose()` (line 1117-1121 sets `dirExists = false`). So the claim is technically correct, but the plan should verify that `dispose()` with `dirExists=false` and `RecycleWorkspaces=true` does the right thing. Currently, the recycling early-return in Step 3's implementation runs unconditionally and would mark a workspace as "recyclable" even when its directory is gone. This is a bug in the Step 3 implementation -- it should check `dirExists` before recycling.

### 5. Step 4 test for local repos may not work as written

`TestGetOrCreate_RecyclableLocalRepo_Reused` creates a local repo workspace, manually sets its status to recyclable, then calls `GetOrCreate` with `"local:myproject"`. But the plan's Tier 0 code runs before the `local:` early return only if the Tier 0 block is placed before line 387. The test assumes Tier 0 will match `w.Repo == repoURL` where `w.Repo` is the URL set by `CreateLocalRepo`. Need to verify what `CreateLocalRepo` sets as `w.Repo` -- it may not be `"local:myproject"`.

### 6. Step 11 `handlePurgeWorkspace` route placement

The plan says to register `r.Delete("/workspaces/{workspaceID}/purge", s.handlePurgeWorkspace)` inside the `workspaces/{workspaceID}` route group. But looking at the route group definition (server.go line 691-725), routes inside it use relative paths like `r.Post("/dispose", ...)`, not full paths. The registration should be:

```go
r.Delete("/purge", s.handlePurgeWorkspace)
```

inside the `r.Route("/workspaces/{workspaceID}", ...)` block, not `r.Delete("/workspaces/{workspaceID}/purge", ...)`.

### 7. `handleGetRecyclableWorkspaces` calls `s.config.FindRepoByURL`

The plan's handler calls `s.config.FindRepoByURL(ws.Repo)`. This method does exist on `*config.Config` (verified at config.go line 1537-1539). However, the workspace manager's internal `findRepoByURL` uses `m.config.GetRepos()` and iterates differently (manager.go line 808-815). The config-level `FindRepoByURL` uses a URL cache and handles URL normalization. Make sure the semantics match what's needed.

### 8. Dependency group for Step 5 may cause file conflicts with Step 4

Steps 4 and 5 are in the same dependency group (Group 3, "can parallelize"). Both modify `internal/workspace/manager.go`. Step 4 inserts Tier 0 code into `GetOrCreate`, and Step 5 modifies the status backfill conditions in the same function. If done in parallel by two agents, they will have merge conflicts on `manager.go`.

## Verified Claims (things you confirmed are correct)

1. **Line 86 for `WorkspaceStatusDisposing`** -- confirmed at state.go line 86. The constant block is at lines 82-87.

2. **Line 101 for `TmuxBinary`** -- confirmed at config.go line 101. Placing `RecycleWorkspaces` after it is reasonable.

3. **Line 420/462 for backfill conditions** -- confirmed. Tier 1 backfill at line 420, Tier 2 at line 462. Both check `w.Status == ""`.

4. **Line 474 for `create()` call** -- confirmed at manager.go line 474.

5. **Line 977 for `UpdateAllVCSStatus`** -- the function is at line 971, with the workspace iteration at line 977-981.

6. **Line 1026 for `EnsureAll`** -- confirmed at manager.go line 1025-1033.

7. **Line 1085 for `dispose()`** -- confirmed at manager.go line 1085.

8. **Line 1205 for `difftool.CleanupWorkspaceTempDirs`** -- confirmed at manager.go line 1205.

9. **`FindRepoByURL` exists on `*config.Config`** -- confirmed at config.go line 1537-1539. The method exists and is tested.

10. **Chi router handles `/workspaces/purge` alongside `/workspaces/{workspaceID}`** -- confirmed. There's already a `/workspaces/scan` route at the same level (server.go line 624), proving chi handles literal paths before parameterized ones correctly.

11. **`newTestServer` returns `(*Server, *config.Config, *state.State)`** -- confirmed at api*contract_test.go line 28. All existing tests use `server, *, st` ordering.

12. **`handleDisposeWorkspaceAll` uses `DisposeForce`** -- confirmed at handlers_dispose.go line 221. The design's decision that `force` should not bypass recycling is correct for preserving the most common dispose flow.

13. **Daemon crash recovery code at line 780** -- confirmed at daemon.go lines 778-801. Calls `wm.DisposeForce()` for stuck "disposing" workspaces.

14. **`prepare()` checks `hasActiveSessions`** -- confirmed at manager.go line 710-711.

15. **`GetWorkspaces()` returns a copy** -- confirmed at state.go lines 445-446. This is relevant to the Tier 0 race condition analysis.
