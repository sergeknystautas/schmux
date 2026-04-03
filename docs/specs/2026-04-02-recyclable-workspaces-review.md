VERDICT: NEEDS_REVISION

## Summary Assessment

The core concept is sound and well-motivated, but the spec has a critical correctness gap in how Tier 0 interacts with existing tiers 1-2, misses several side effects of the current dispose path, and understates the file churn caused by `prepare()` itself.

## Critical Issues (must fix)

### 1. Existing tiers 1-2 will silently reuse recyclable workspaces without promotion

The spec proposes adding Tier 0 "before existing tiers," but the existing Tier 1 (same branch, line 397-427 of `manager.go`) and Tier 2 (different branch, line 430-471) filter only on `hasActiveSessions()`. A recyclable workspace has no sessions, so tiers 1-2 will happily match it first. Worse, tier 1's backfill only sets status to running when `w.Status == ""` (line 420-421):

```go
if w.Status == "" {
    w.Status = state.WorkspaceStatusRunning
```

A workspace with `status == "recyclable"` would be returned **still marked recyclable**, which would cause the dashboard to hide an actively-used workspace.

**Fix**: Either (a) add explicit status filtering in tiers 1-2 to skip recyclable workspaces (forcing them through Tier 0), or (b) change the backfill to `if w.Status != state.WorkspaceStatusRunning` so any reuse path promotes to running. Option (b) is simpler but loses the semantic distinction the spec wants between "reuse idle" and "reuse recyclable."

### 2. Preview proxy cleanup happens in the handler, not in `dispose()` -- recycling will leak proxies

The spec says to add an early return inside `dispose()` to skip file deletion. But preview cleanup (`previewManager.DeleteWorkspace`) runs _after_ `dispose()` returns in `handleDisposeWorkspace` (line 119-124 of `handlers_dispose.go`). If `dispose()` returns nil (success) with recycling, the handler still calls `previewManager.DeleteWorkspace`. This is actually correct for the dispose case -- but when the recyclable workspace is reused later, its previews are already deleted. More importantly, if the spec intends the workspace to be reusable, it should **still** clean up previews on recycle-dispose (which it does, since the handler runs). But this means `Purge()` must also call `previewManager.DeleteWorkspace`, and the spec does not mention this.

The real issue: `handleDisposeWorkspaceAll` calls `DisposeForce` (line 221). The spec says `DisposeForce` "always deletes" as the escape hatch. But `handleDisposeWorkspaceAll` is the "dispose workspace + all sessions" button, which is the _normal_ way users dispose workspaces with sessions. This means recycling is bypassed for the most common dispose flow, significantly reducing the feature's value.

### 3. `MarkWorkspaceDisposing` sets status to "disposing" before `dispose()` runs

The handler calls `MarkWorkspaceDisposing` (line 95 of `handlers_dispose.go`) _before_ calling `dispose()`. Inside `dispose()`, the spec's early return would set status to "recyclable". But the workspace arrives at `dispose()` with `status == "disposing"`, not `status == "running"`. If schmux crashes between `MarkWorkspaceDisposing` and the `dispose()` early return, the workspace is stuck in "disposing" status -- it's not running (so the dashboard grays it out), not recyclable (so Tier 0 won't find it), and not removed from state (so it blocks number reuse). This is a crash-safety gap. The spec needs to define how stale "disposing" workspaces are recovered on daemon restart.

### 4. `git clean -fd` in `prepare()` causes significant file churn, understated in the comparison table

The spec claims "0 file changes" for dispose+respawn-same-branch with recycling. But `prepare()` runs `git clean -fd` (line 746) which deletes all untracked files. For a typical workspace these include build artifacts, `node_modules`, IDE caches, etc. After cleaning, overlay re-copy recreates some files. This is not zero churn -- it can be thousands of file deletions depending on what the agent left behind. The spec's file churn table should be revised to be honest about this cost, or the dispose path should be modified to skip prepare for recyclable workspaces that already have the right branch checked out (matching what Tier 1 does for running workspaces that match).

### 5. Worktree branch reservation blocks reuse across workspaces

A recyclable workspace holds a git worktree with a branch checked out. Git does not allow two worktrees to have the same branch checked out simultaneously. If a user disposes workspace-001 on branch `feature/foo` (it becomes recyclable), then tries to spawn a new workspace on `feature/foo`, Tier 0 would find and reuse it -- but if Tier 0 fails for any reason (e.g. divergence check), `create()` would fail at `git worktree add` because the branch is already checked out in the recyclable worktree. The spec does not address this failure mode. The fix should either (a) have the create path detect this and fall through to purging the conflicting recyclable workspace, or (b) have the recyclable dispose path checkout a detached HEAD to release the branch.

## Suggestions (nice to have)

### A. `UpdateAllVCSStatus` and `EnsureAll` will run on recyclable workspaces

`UpdateAllVCSStatus` (line 971) iterates all non-remote workspaces and runs git status on each. `EnsureAll` (line 1025) does the same for schmux config bootstrapping. Neither filters by status. This means recyclable workspaces incur ongoing git polling overhead and unnecessary I/O. Consider filtering out recyclable workspaces from both functions, since their git state is irrelevant until reuse.

### B. `difftool.CleanupWorkspaceTempDirs` is skipped by the early return

The spec's early return in `dispose()` would skip `difftool.CleanupWorkspaceTempDirs` (line 1205), leaving diff temp directories on disk for recyclable workspaces. These are in the OS temp dir, not in the workspace, so they don't contribute to the backup churn problem, but they accumulate if workspaces are recycled many times without purging.

### C. Consider the `handleDisposeWorkspaceAll` flow

As noted in critical issue 2, the "Dispose All" flow uses `DisposeForce`, which bypasses recycling entirely. Since this is the normal user flow when a workspace has sessions, the spec should decide whether `handleDisposeWorkspaceAll` should recycle too. If so, the handler needs to be modified to call `Dispose` (non-force) after sessions are disposed, rather than `DisposeForce`.

### D. Dashboard needs explicit filtering, not implicit hiding

The `buildSessionsResponse` function (line 91 of `handlers_sessions.go`) iterates `s.state.GetWorkspaces()` without filtering by status. Recyclable workspaces would appear in every WebSocket broadcast, and the dashboard would need frontend filtering to hide them. The spec should specify whether filtering happens server-side (in `buildSessionsResponse`) or client-side (in `AppShell.tsx`). Server-side is better for bandwidth and simplicity -- but then the purge UI needs a separate endpoint to fetch recyclable workspaces.

### E. Race between concurrent dispose and spawn

The dispose path and GetOrCreate share the per-repo lock (`repoLock`). But `dispose()` does not acquire this lock. If a spawn and a recycle-dispose for the same repo race, `GetOrCreate` could find a workspace mid-transition (status being set from "disposing" to "recyclable"). The per-repo lock in GetOrCreate protects against concurrent spawns but not against concurrent dispose. This is not a new bug (it exists today), but recycling makes it more likely to manifest because recyclable workspaces persist in state longer.

### F. Local repositories (`local:` prefix) are excluded from GetOrCreate but should be addressed

The spec doesn't mention local repos (prefixed `local:` in repoURL). GetOrCreate returns early for local repos at line 387-389, always creating fresh workspaces. This is probably fine since local repos are typically not backed up, but the spec should explicitly state that recycling does not apply to local repos.

## Verified Claims (things confirmed as correct in the codebase)

1. **`prepare()` does what the spec claims**: fetch, checkout-dot, clean, checkout-branch, pull-rebase -- verified at lines 703-766 of `manager.go`.

2. **`dispose()` runs overlay reconciliation before file deletion**: `compoundReconcile` at line 1135-1137, then watch removal at 1139-1142, then file deletion starting at 1146. The spec's insertion point after watch removal and before file deletion is correct.

3. **`DisposeForce` bypasses safety checks**: `dispose()` with `force=true` skips `hasActiveSessions` (line 1112) and `checkGitSafety` (line 1124). The spec correctly identifies this as the escape hatch.

4. **Workspace status constants exist as described**: `WorkspaceStatusProvisioning`, `WorkspaceStatusRunning`, `WorkspaceStatusFailed`, `WorkspaceStatusDisposing` defined at lines 82-87 of `state.go`. Adding `WorkspaceStatusRecyclable` is straightforward.

5. **`findNextWorkspaceNumber` counts all workspaces for the repo**: lines 817-835. A recyclable workspace would indeed block its number from being reused, which is the correct behavior (prevents directory name collisions since the directory still exists).

6. **`hasActiveSessions` checks session count, not status**: line 369-376. It returns true if any session has a matching `WorkspaceID`, regardless of session or workspace status. Since sessions are cleaned up during dispose, a recyclable workspace will correctly have zero sessions.

7. **`checkGitSafety` would not conflict with recycling**: The spec's early return is placed after `checkGitSafety` runs (when `force=false`), so unsafe workspaces would still fail the dispose. This means a workspace with unpushed commits cannot be recycled, which is the correct safety behavior.

8. **The per-repo lock in `GetOrCreate` protects Tier 0**: The lock at line 392-394 ensures only one spawn per repo runs at a time, preventing two spawns from claiming the same recyclable workspace.
