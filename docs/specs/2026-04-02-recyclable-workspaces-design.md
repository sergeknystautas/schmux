# Recyclable Workspaces

## Changes from v1

Revised to address review feedback. Key changes:

1. **`force` no longer bypasses recycling.** `DisposeForce` means "skip safety checks," not "skip recycling." The escape hatch for actual file deletion is the new `Purge` API. This fixes the critical issue where `handleDisposeWorkspaceAll` (the normal "dispose workspace + sessions" button) used `DisposeForce` and would have bypassed recycling entirely.
2. **Tiers 1–2 now promote recyclable workspaces to running.** The backfill condition changes from `w.Status == ""` to always setting `WorkspaceStatusRunning` on reuse, preventing recyclable workspaces from being returned still marked recyclable.
3. **Crash recovery for stale "disposing" status.** Daemon startup scans for workspaces stuck in "disposing" and recovers them.
4. **Honest file churn table.** `git clean -fd` in `prepare()` deletes untracked files (build artifacts, `node_modules`). The comparison table now reflects this.
5. **Worktree branch reservation handling.** If Tier 0 skips a recyclable workspace (divergence check) and `create()` would fail due to the branch being locked, the conflicting recyclable workspace is purged first.
6. **Local repos included.** `GetOrCreate` checks for recyclable workspaces before the `local:` early return, so local repos get the same reuse path as remote repos. `prepare()` already handles repos without a remote origin (skips fetch/pull).
7. **Server-side filtering.** `buildSessionsResponse` filters out recyclable workspaces. Separate endpoint for purge UI.
8. **Polling exclusion.** `UpdateAllVCSStatus` and `EnsureAll` skip recyclable workspaces.

## Problem

When a workspace is disposed, schmux deletes the entire directory — thousands of files removed, then thousands recreated when a new workspace is spawned on the same repo. Backup software (Time Machine, Backblaze, etc.) treats every file event as a change to sync, saturating upload bandwidth even though the actual content difference between branches is typically single-digit percentages.

The workspace reuse mechanism (`GetOrCreate` tiers 1–2) already avoids this churn by reusing idle workspaces via `git checkout`. But explicit workspace disposal bypasses reuse entirely — it deletes the directory and removes the workspace from state, forcing full recreation on next spawn.

## Design

Add a `recycle_workspaces` config option. When enabled, disposing a workspace keeps the directory on disk and marks the workspace as `"recyclable"` in state. On next spawn for the same repo, the recyclable workspace is reused via `git checkout` — only files that differ between branches are touched.

```
                Current                              With recycle_workspaces
                ───────                              ──────────────────────

dispose:  running → delete files → remove state      running → set status "recyclable"
                    (all files removed)               (files stay, state stays)

spawn:    find idle workspace OR create new           find recyclable workspace → prepare()
          (full directory creation if new)            → promote to "running"
                                                     (only changed files touched)
```

### Core Decisions

| Decision         | Choice                                           | Rationale                                                                                                                                                                                                       |
| ---------------- | ------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Config field     | `recycle_workspaces: bool`                       | Opt-in. Default `false` preserves current behavior.                                                                                                                                                             |
| Recyclable state | New workspace status `"recyclable"`              | Distinct from `"running"` so dashboard can hide them. Distinct from deleted so reuse can find them.                                                                                                             |
| Reuse priority   | Tier 0 (before existing tiers 1–2)               | Recyclable workspaces should be consumed before idle ones. An idle workspace might get a session moments later; a recyclable one is explicitly abandoned.                                                       |
| `force` meaning  | `force` = skip safety checks, NOT skip recycling | `handleDisposeWorkspaceAll` uses `DisposeForce`. If force bypassed recycling, the most common dispose flow would never recycle. `force` only controls whether active-session and git-safety checks are skipped. |
| Escape hatch     | New `Purge` API for actual file deletion         | Separate concern from dispose. Purge is explicit "I want disk space back."                                                                                                                                      |
| Dashboard        | Server-side filtering in `buildSessionsResponse` | Recyclable workspaces excluded from WebSocket broadcasts. Separate endpoint for purge UI.                                                                                                                       |
| Local repos      | Same recycling as remote repos                   | `GetOrCreate` checks for recyclable workspaces before the `local:` early return. `prepare()` already handles no-origin repos (skips fetch/pull).                                                                |

### One-Way Doors

None. Everything here is reversible:

- Turning `recycle_workspaces` off reverts to current delete-on-dispose behavior.
- Recyclable workspaces can be purged at any time.
- No state schema changes beyond a new valid value for `Status`.

### Two-Way Doors

- Dashboard UX for recyclable workspaces (collapsed section vs. badge vs. hidden)
- LRU eviction policy for recyclable workspaces (defer — add only if disk usage becomes a problem)
- Whether to recycle remote workspaces (defer — they have no local directory, so the problem doesn't apply)

## Config

New top-level field in `~/.schmux/config.json`:

```json
{
  "recycle_workspaces": true
}
```

Added to the `Config` struct in `internal/config/config.go`:

```go
RecycleWorkspaces bool `json:"recycle_workspaces,omitempty"`
```

Default: `false`.

## State

The `Status` field on `state.Workspace` gains a new valid value:

```
"provisioning" → "running" → "recyclable"
                           → "disposing" → (removed)
```

With recycling enabled, the dispose path becomes: `"running" → "disposing" → "recyclable"`. The handler still calls `MarkWorkspaceDisposing` for visual feedback, then `dispose()` transitions to `"recyclable"` instead of deleting.

No new fields are needed on the workspace struct. The existing `Path`, `Repo`, `Branch`, and `VCS` fields provide everything needed to find and reuse a recyclable workspace.

### Crash Recovery

On daemon startup, scan for workspaces with `status == "disposing"`:

- If the directory still exists on disk → set status to `"recyclable"` (if `recycle_workspaces` is on) or `"running"` (if off). The workspace was mid-dispose when the daemon crashed; recovering to a usable state is better than leaving it stuck.
- If the directory is gone → remove the workspace from state (the deletion completed but state cleanup didn't).

This is implemented in the existing daemon startup sequence, alongside the `EnsureAll` call.

## Dispose Path

### `dispose()` in `manager.go`

The recycling check happens inside `dispose()`, regardless of the `force` parameter. `force` controls safety checks (active sessions, git safety), not recycling:

```
func dispose(ctx, workspaceID, force):
    // ... existing safety checks (skipped when force=true) ...
    // ... overlay reconciliation ...
    // ... watch removal ...
    // ... difftool temp dir cleanup (still runs — it's in OS temp, not workspace) ...

    if config.RecycleWorkspaces:
        1. Set workspace status to "recyclable"
        2. Save state
        3. Clean up in-memory maps (workspaceConfigs, lockedWorkspaces, workspaceGates)
        4. Return — skip file deletion and state removal

    // ... existing deletion logic (only when recycling is off) ...
```

What still runs during recycle-dispose:

- Session cleanup (sessions are still disposed by the handler before calling dispose)
- Overlay reconciliation (copy changed overlays back to source)
- Filesystem watch removal (no need to watch a recyclable workspace)
- Diff temp dir cleanup (in OS temp, not workspace dir — doesn't cause backup churn)
- In-memory map cleanup

What is skipped:

- `git worktree remove` / `os.RemoveAll` (the whole point)
- `git worktree prune`
- `git branch -D` on the bare repo
- State removal (`state.RemoveWorkspace`)

### `handleDisposeWorkspaceAll` in `handlers_dispose.go`

No handler changes needed. The handler already calls `DisposeForce`, and `force` no longer bypasses recycling. The flow is:

1. Handler marks workspace as "disposing" (visual feedback)
2. Handler disposes all sessions concurrently
3. Handler calls `DisposeForce` → `dispose(ctx, id, force=true)`
4. `dispose()` skips safety checks (force=true) but still checks `RecycleWorkspaces`
5. If recycling: status transitions `"disposing" → "recyclable"`, files stay
6. If not recycling: existing deletion runs

## GetOrCreate: Tier 0

Insert before existing tiers in `manager.go:GetOrCreate()`, **before** the local repo early-return (line 387). This ensures both local and remote repos check for recyclable workspaces first:

```
Tier 0 — Recyclable workspace, same repo:
    1. Iterate workspaces with status == "recyclable" and matching repoURL
    2. Verify directory still exists on disk
       - If missing: purge stale state (remove from state, prune worktrees), continue search
    3. Apply divergence safety check (isUpToDateWithDefault)
       - If diverged: skip this workspace, continue search
    4. Call prepare() — fetch, discard local changes, clean, checkout target branch, pull
    5. Re-copy overlay files
    6. Update branch in state
    7. Promote status to "running"
    8. Re-add filesystem watches
    9. Save state
    10. Return reused workspace
```

If no recyclable workspace is found or all are skipped, fall through to tiers 1–3 unchanged.

### Tiers 1–2: Status Promotion Fix

The existing backfill at lines 420–422 and 462–464 currently only promotes when `w.Status == ""`:

```go
if w.Status == "" {
    w.Status = state.WorkspaceStatusRunning
```

Change to always promote to running on reuse:

```go
if w.Status != state.WorkspaceStatusRunning {
    w.Status = state.WorkspaceStatusRunning
```

This ensures that if a recyclable workspace somehow reaches tiers 1–2 (e.g., Tier 0 skipped it due to divergence but Tier 2 accepts it), it gets properly promoted.

### Worktree Branch Reservation

When `create()` fails because the target branch is already checked out in a recyclable worktree:

1. `create()` returns a `git worktree add` error mentioning the branch is "already checked out"
2. `GetOrCreate` detects this specific error
3. Finds the recyclable workspace holding the branch
4. Purges it (runs the real deletion: `worktree remove`, state removal)
5. Retries `create()`

This is a fallback for the edge case where Tier 0 finds the recyclable workspace but skips it (divergence check), and then `create()` collides with its branch reservation. The purge-and-retry is safe because the workspace was already disposable.

## Purge API

New methods on the workspace manager:

```
Purge(ctx, workspaceID)    — delete a single recyclable workspace (files + state)
PurgeAll(ctx, repoURL)     — delete all recyclable workspaces for a repo
PurgeAll(ctx, "")          — delete all recyclable workspaces across all repos
```

These run the existing deletion logic: `worktree remove` / `os.RemoveAll`, `worktree prune`, branch cleanup, state removal, preview cleanup (`previewManager.DeleteWorkspace`). Essentially the current `dispose()` code path but restricted to recyclable workspaces.

New HTTP endpoints:

```
DELETE /api/workspaces/{id}/purge      → Purge(ctx, id)
DELETE /api/workspaces/purge?repo=URL  → PurgeAll(ctx, repoURL)
DELETE /api/workspaces/purge           → PurgeAll(ctx, "")
```

## Dashboard

### Workspace Broadcasts (Server-Side Filtering)

`buildSessionsResponse` in `handlers_sessions.go` filters out workspaces with `status == "recyclable"`. They do not appear in WebSocket broadcasts or the `/api/sessions` response. This keeps the main UI clean without requiring frontend filtering logic.

### Recyclable Workspace Indicator

A separate lightweight endpoint provides recyclable workspace counts for the dashboard:

```
GET /api/workspaces/recyclable → { "total": 3, "by_repo": { "schmux": 2, "other": 1 } }
```

The dashboard shows this below the workspace list as a collapsed indicator:

```
┌──────────────────────────────────────┐
│  schmux-004   main   ● running      │
│  schmux-005   feat/x ● running      │
│                                      │
│  ▸ 3 recyclable workspaces  [Purge] │
└──────────────────────────────────────┘
```

Clicking "Purge" calls `DELETE /api/workspaces/purge` and removes all recyclable workspaces.

### Dispose Button

No change to the dispose button's label or position. When `recycle_workspaces` is enabled, the button still says "Dispose" — the recycling is transparent. The user's mental model remains "I'm done with this workspace."

### Settings Page

The `recycle_workspaces` toggle should appear in the config editor at `/config`.

## Background Polling Exclusion

### `UpdateAllVCSStatus`

Add a status filter to skip recyclable workspaces. They don't need git status polling — their VCS state is irrelevant until reuse, and polling them wastes I/O:

```go
for _, w := range workspaces {
    if w.RemoteHostID != "" || w.Status == state.WorkspaceStatusRecyclable {
        continue
    }
    // ... existing polling logic
}
```

### `EnsureAll`

Same filter — don't run workspace config bootstrapping on recyclable workspaces:

```go
for _, w := range m.state.GetWorkspaces() {
    if w.RemoteHostID != "" || w.Status == state.WorkspaceStatusRecyclable {
        continue
    }
    // ... existing ensure logic
}
```

## File Churn Comparison

For a repo with 10,000 tracked files where branches differ by 50 files:

| Operation                | Current               | With Recycling             |
| ------------------------ | --------------------- | -------------------------- |
| Dispose                  | 10,000 file deletions | 0 file changes             |
| Respawn same branch      | 10,000 file creations | ~0 tracked file changes\*  |
| Respawn different branch | 10,000 file creations | ~50 tracked file changes\* |
| **Total backup events**  | **20,000–30,000**     | **~50–500**                |

\*`prepare()` runs `git clean -fd` which deletes untracked files (build artifacts, `node_modules`, IDE caches). The actual churn depends on what the previous agent left behind. For a clean workspace this is near-zero; for one with large build artifacts it could be hundreds of deletions. This is still orders of magnitude less than full directory recreation, and these untracked files would need to be regenerated regardless of the approach.

## Testing

- **Unit tests**: `GetOrCreate` finds and reuses recyclable workspaces before creating new ones (Tier 0).
- **Unit tests**: `dispose()` skips file deletion when `RecycleWorkspaces` is true.
- **Unit tests**: `dispose()` with `force=true` still recycles (force only skips safety checks).
- **Unit tests**: `dispose()` on a `local:` repo recycles the same as remote repos.
- **Unit tests**: Tiers 1–2 promote recyclable workspaces to running status on reuse.
- **Unit tests**: `Purge` / `PurgeAll` delete files and remove state for recyclable workspaces only.
- **Unit tests**: Recyclable workspace with manually-deleted directory is handled gracefully (purge stale state, continue search).
- **Unit tests**: Worktree branch collision during `create()` triggers purge-and-retry of the conflicting recyclable workspace.
- **Unit tests**: Daemon startup recovers workspaces stuck in "disposing" status.
- **Unit tests**: `buildSessionsResponse` excludes recyclable workspaces.
- **Unit tests**: `UpdateAllVCSStatus` skips recyclable workspaces.
- **E2E**: Full cycle — spawn, dispose (recycled), spawn again on same repo — verify the workspace directory is reused, not recreated.
- **E2E**: Full cycle with `handleDisposeWorkspaceAll` (dispose workspace + sessions) — verify recycling works through the force path.
