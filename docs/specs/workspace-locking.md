# Workspace Locking Spec

## Problem

The workspace Manager runs git operations from two concurrent paths:

1. **Sync operations** — `LinearSyncFromDefault` and `LinearSyncResolveConflict` perform multi-step git sequences (fetch, rebase, commit, reset) that must run atomically.
2. **Status refreshes** — `UpdateGitStatus`, triggered by the git-watcher's fsnotify events, runs `git fetch`, `git status`, `git rev-list`, `git diff`, and `git merge-base`.

These run concurrently with no coordination. The git-watcher sees rebase-merge directories appear during a rebase and fires status refreshes that run `git fetch` mid-rebase, causing spurious rebase failures.

Currently there is a `workspaceLockedFn` callback where the dashboard server tells the workspace Manager whether a workspace is locked by checking dashboard-layer conflict resolution state. This is:

- **Wrong layer**: the Manager owns the sync operations but asks the dashboard whether they're running.
- **Incomplete**: only covers `LinearSyncResolveConflict`, not `LinearSyncFromDefault`.
- **Circular**: dashboard → Manager callback → dashboard state lookup.

## Design

All locking moves into the workspace Manager. The dashboard callback is deleted.

### Lock semantics

- **Exclusive**: only one sync operation can hold the lock per workspace at a time.
- **Fail-fast for competing syncs**: `LockWorkspace` returns false immediately if another sync already holds the lock.
- **Waits for git status**: `LockWorkspace` blocks until any in-flight `UpdateGitStatus` on that workspace completes, preventing corrupted/partial git state.
- **Single holder**: because only one operation holds the lock, `UnlockWorkspace` is always called by the same operation that locked. No tokens or refcounts needed.

### Per-workspace gate

A per-workspace `sync.RWMutex` ("gate") coordinates git status and sync operations:

- `UpdateGitStatus` holds an `RLock` while running — allows concurrent status checks but prevents sync from starting.
- `LockWorkspace` acquires a full `Lock` — blocks until all in-flight `UpdateGitStatus` calls on that workspace finish.
- Both double-check `IsWorkspaceLocked` after acquiring the gate to handle races.

### Manager struct changes

Add:

```go
lockedWorkspaces   map[string]bool
lockedWorkspacesMu sync.RWMutex
workspaceGates     map[string]*sync.RWMutex  // per-workspace gate
workspaceGatesMu   sync.Mutex                // protects the map
```

Delete:

```go
workspaceLockedFn  func(workspaceID string) bool
```

### New methods on Manager

```go
// LockWorkspace attempts to lock a workspace for a sync operation.
// Returns true if the lock was acquired, false if already locked.
func (m *Manager) LockWorkspace(workspaceID string) bool {
    m.lockedWorkspacesMu.Lock()
    defer m.lockedWorkspacesMu.Unlock()
    if m.lockedWorkspaces[workspaceID] {
        return false
    }
    m.lockedWorkspaces[workspaceID] = true
    return true
}

func (m *Manager) UnlockWorkspace(workspaceID string) {
    m.lockedWorkspacesMu.Lock()
    defer m.lockedWorkspacesMu.Unlock()
    delete(m.lockedWorkspaces, workspaceID)
}

func (m *Manager) IsWorkspaceLocked(workspaceID string) bool {
    m.lockedWorkspacesMu.RLock()
    defer m.lockedWorkspacesMu.RUnlock()
    return m.lockedWorkspaces[workspaceID]
}
```

### Deleted method

```go
func (m *Manager) SetWorkspaceLockedFn(fn func(workspaceID string) bool)
```

### Init

In `New()`, initialize the map:

```go
lockedWorkspaces: make(map[string]bool),
```

### Callers

**LinearSyncFromDefault** — fail-fast if already locked:

```go
func (m *Manager) LinearSyncFromDefault(ctx context.Context, workspaceID string) (*LinearSyncResult, error) {
    if !m.LockWorkspace(workspaceID) {
        return nil, ErrWorkspaceLocked
    }
    defer m.UnlockWorkspace(workspaceID)
    // ... existing logic unchanged
}
```

**LinearSyncResolveConflict** — fail-fast if already locked:

```go
func (m *Manager) LinearSyncResolveConflict(ctx context.Context, workspaceID string, onStep ResolveConflictStepFunc) (*LinearSyncResolveConflictResult, error) {
    if !m.LockWorkspace(workspaceID) {
        return nil, ErrWorkspaceLocked
    }
    defer m.UnlockWorkspace(workspaceID)
    // ... existing logic unchanged
}
```

**UpdateGitStatus** — replace callback check:

```go
// Before:
if m.workspaceLockedFn != nil && m.workspaceLockedFn(workspaceID) {
    return nil, ErrWorkspaceLocked
}

// After:
if m.IsWorkspaceLocked(workspaceID) {
    return nil, ErrWorkspaceLocked
}
```

### Dashboard changes

**Delete** from `server.go` NewServer():

```go
if mgr, ok := wm.(*workspace.Manager); ok {
    mgr.SetWorkspaceLockedFn(func(workspaceID string) bool {
        state := s.getLinearSyncResolveConflictState(workspaceID)
        return state != nil && state.Status == "in_progress"
    })
}
```

No other dashboard changes needed. The dashboard still manages its own conflict resolution UI state separately — that's display state, not locking.

## Status refresh after unlock

Not a problem. The sync handlers (`handleLinearSyncFromMain`, `handleLinearSyncResolveConflict`) already call `UpdateGitStatus` after the sync function returns. Since the `defer UnlockWorkspace` runs when the sync function returns, the lock is released before the handler calls `UpdateGitStatus`. No stale state.

## Scope

This spec covers `LinearSyncFromDefault` and `LinearSyncResolveConflict` — the two multi-step rebase flows where the watcher race causes actual bugs.

Other mutating git operations (`LinearSyncToDefault`, `PushToBranch`) are single git commands and less susceptible to watcher interference. They can be added to workspace locking later if needed.

## Separation from dashboard state

The dashboard maintains `linearSyncResolveConflictStates` for the frontend UI (steps, summaries, conflict hunks). The Manager maintains `lockedWorkspaces` for concurrency control. These are independent concerns that happen to overlap in time for `LinearSyncResolveConflict`. `LinearSyncFromDefault` has a lock but no dashboard state — that's correct and expected.

## Visual lockdown

Previously only `LinearSyncResolveConflict` locked down the UI — it created a `LinearSyncResolveConflictState`, broadcast it via WebSocket, and the frontend disabled all tabs when `status === "in_progress"`. `LinearSyncFromDefault` had no visual effect despite holding the same backend lock.

Now lock state is communicated via a dedicated `workspace_locked` WebSocket message type, sent in real-time (not debounced) when lock state changes. Both sync operations get the same tab lockdown.

### Why not use the sessions broadcast?

The sessions broadcast (`type: "sessions"`) is a debounced snapshot-replace of the full workspace/session state. Putting lock state there has two problems:

1. **Race**: the 100ms debounce means clients may never see `locked: true` if the sync is fast, or if another broadcast resets the debounce timer.
2. **Clobber**: any per-workspace state merged into the `workspaces` array on the frontend (like sync progress) gets wiped on the next snapshot-replace.

Lock and progress state have different lifecycles than session state. They update rapidly and independently. They belong in separate frontend state, same pattern as `linearSyncResolveConflictStates`.

### `workspace_locked` message type

A new lightweight WebSocket message sent directly to all `/ws/dashboard` connections (no debounce):

```json
{
  "type": "workspace_locked",
  "workspace_id": "schmux-003",
  "locked": true,
  "sync_progress": { "current": 5, "total": 496 }
}
```

- `locked: true` — sent when `LockWorkspace` succeeds
- `locked: false` — sent when `UnlockWorkspace` is called
- `sync_progress` — optional, included when `LinearSyncFromDefault` reports rebase progress (current/total). Omitted on lock/unlock messages that don't have progress context.

This replaces both the `locked` field on the sessions broadcast payload and the `linear_sync_progress` message type.

### Backend: lock state callback

The Manager gets a callback that fires on lock/unlock:

```go
onLockChangeFn func(workspaceID string, locked bool)
```

Set via `SetOnLockChangeFn`. Called from `LockWorkspace` (with `true`) and `UnlockWorkspace` (with `false`). The Server wires this to `BroadcastWorkspaceLocked`.

### Backend: sync progress callback

The existing `syncProgressFn` callback stays. The Server wires it to send a `workspace_locked` message with `sync_progress` included. This replaces `BroadcastSyncProgress`.

### Backend: handler changes

- Remove `go s.BroadcastSessions()` before and after `LinearSyncFromDefault` in `handleLinearSyncFromMain`. Lock/unlock broadcasts are now automatic via the callback.
- Remove `Locked` field from `WorkspaceResponseItem` and `buildSessionsResponse`. Lock state is no longer in the sessions snapshot.
- `ErrWorkspaceLocked` returns 409 Conflict, not 500.

### Frontend: separate state

In `useSessionsWebSocket`, workspace lock state is stored in its own `Record<string, WorkspaceLockState>`, not merged into the `workspaces` array:

```typescript
type WorkspaceLockState = {
  locked: boolean;
  syncProgress?: { current: number; total: number };
};
```

The `workspace_locked` message handler updates this state. The `sessions` message handler does its snapshot-replace of `workspaces` without touching lock state. No clobber.

Components read lock state from this separate record, same pattern as `linearSyncResolveConflictStates`.

### Frontend: tab lockdown

In `SessionTabs.tsx`, derive `isLocked`:

```typescript
const lockState = workspaceLockStates[workspace?.id ?? ''];
const isLocked = resolveInProgress || lockState?.locked;
```

Replace `resolveInProgress` with `isLocked` for all tab disabling, spawn menu closing, and button disabling. Auto-navigation: `resolveInProgress` → `/resolve-conflict/`, `lockState?.locked` → `/git/`.

Same pattern in `WorkspaceHeader.tsx` for the dispose button.

### Frontend: sync progress display

In `GitHistoryDAG.tsx`, read progress from the lock state record:

```typescript
const lockState = workspaceLockStates[ws?.id ?? ''];
// Show "Rebasing 2/496 commits" when lockState?.syncProgress exists
```

No longer reads `ws.sync_progress` from the workspace object.

### What this does NOT do

- No navigation to `/resolve-conflict/` for clean sync
- No `LinearSyncResolveConflictState` created for clean sync
- Clean sync remains synchronous (handler returns result directly)
- The `linearSyncResolveConflictStates` mechanism is unchanged

## HTTP status for locked workspaces

When `LinearSyncFromDefault` returns `ErrWorkspaceLocked`, the handler returns 409 Conflict (not 500). This matches how `handleLinearSyncResolveConflict` already returns 409 for "operation already in progress".

## Files changed

1. `internal/workspace/manager.go` — add `onLockChangeFn` callback, call from Lock/Unlock
2. `internal/dashboard/server.go` — add `BroadcastWorkspaceLocked`, wire `SetOnLockChangeFn` and `SetSyncProgressFn`, delete `BroadcastSyncProgress`
3. `internal/dashboard/handlers.go` — remove `Locked` field from `WorkspaceResponseItem` and `buildSessionsResponse`
4. `internal/dashboard/handlers_sync.go` — remove pre/post-sync broadcasts, add 409 for `ErrWorkspaceLocked`
5. `assets/dashboard/src/lib/types.ts` — remove `locked` and `sync_progress` from `WorkspaceResponse`, add `WorkspaceLockState`
6. `assets/dashboard/src/hooks/useSessionsWebSocket.ts` — add `workspaceLockStates` state, handle `workspace_locked` message, remove `linear_sync_progress` handler
7. `assets/dashboard/src/components/SessionTabs.tsx` — read lock state from separate record
8. `assets/dashboard/src/components/WorkspaceHeader.tsx` — read lock state from separate record
9. `assets/dashboard/src/components/GitHistoryDAG.tsx` — read sync progress from lock state record
10. `docs/api.md` — document `workspace_locked` WS message type, 409 for locked workspace

## Not changed

- Git-watcher behavior is unchanged. It still fires events and calls `UpdateGitStatus`. The lock just makes `UpdateGitStatus` bail early.
- Dashboard conflict resolution UI state is unchanged. That's for the frontend, not for locking.
- `repoLock` in `LinearSyncResolveConflict` is unchanged. It's a repo-level mutex for serializing concurrent resolve-conflict calls on the same repo. Different concern.
