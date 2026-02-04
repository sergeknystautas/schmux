# Linear Sync Conflict Resolution Spec

Rework `LinearSyncResolveConflict` to resolve conflicts **before** continuing the rebase, using non-interactive one-shot LLM calls instead of spawning interactive sessions.

## Current Behavior

1. Get oldest unrebased commit from the default branch
2. Create WIP commit to preserve local changes
3. Attempt `git rebase <hash>`
4. On conflict: `git add -A` + `git rebase --continue` (blindly accepts conflict markers)
5. Spawn an interactive LLM session to clean up the committed mess
6. Unwind WIP commit

Problems: conflict markers get committed into history, LLM cleans up after the fact rather than resolving the actual conflict, and the rebase is already "done" before any real resolution happens.

## New Behavior

### Overview

Rebase one commit from the default branch. When a conflict occurs during replay of local commits, leave the rebase paused, run a non-interactive one-shot LLM call to resolve the conflicted files in-place, then programmatically `git add` + `git rebase --continue`. Repeat for each local commit that conflicts. Accumulate JSON results from each LLM call and return them all via WebSocket broadcast.

### Flow

1. Get the oldest unrebased commit from the default branch (`git log --oneline --reverse HEAD..<default-ref>`, take first)
2. Create WIP commit (`git add -A` + `git commit -m "WIP: <timestamp>"`) to preserve local uncommitted work
3. `git rebase <hash>` — rebase one default-branch commit; git replays all local commits on top
4. **Loop** (for each local commit that conflicts during replay):
   a. Rebase pauses — working directory contains conflict markers in affected files
   b. Identify conflicted files (`git diff --name-only --diff-filter=U`)
   c. If no unmerged files but rebase is still in progress, try `git rebase --continue` (handles auto-resolved conflicts)
   d. Run a **one-shot** LLM call against the configured `conflict_resolve.target`:
      - Input: list of conflicted files and their contents (with markers)
      - Output: resolved file contents as structured JSON
   e. Apply the resolved files to the working directory
   f. `git add <resolved files>`
   g. `git rebase --continue`
   h. Record the one-shot result (which files, what the LLM did) in an accumulator
   i. If another conflict occurs on the next local commit, go to (a)
   j. If clean, rebase continues to the next local commit (or finishes)
5. Unwind WIP commit (`git reset --mixed HEAD~1`)
6. Return combined results: success/failure, number of conflicts resolved, per-conflict LLM results

### Key Differences from Current

| Aspect | Current | New |
|--------|---------|-----|
| When conflicts are resolved | After rebase is already completed | While rebase is paused, before continuing |
| Conflict markers in history | Yes (committed then cleaned up) | No (resolved before commit) |
| LLM execution mode | Interactive session (tmux) | Non-interactive one-shot |
| LLM scope | Free-form agent in a shell | Focused: resolve these files, return JSON |
| Number of LLM calls | 1 (for all conflicts) | N (one per conflicting local commit) |
| API response | Returns quickly, resolution is async | POST returns 202 immediately; progress via WebSocket |

### One-Shot LLM Calls

Uses the same `conflict_resolve.target` configuration that exists today. Invoked via the existing `oneshot.ExecuteTarget(ctx, cfg, targetName, prompt, timeout)` from `internal/oneshot/oneshot.go` — the same mechanism used by branch suggest and nudgenik. Returns a string that we parse as JSON.

Each call handles one paused-rebase conflict (one local commit's worth of conflicted files).

#### Prompt

The one-shot call receives:

- The commit hash from the default branch being rebased
- The local commit hash that is currently being replayed
- The local commit message
- For each conflicted file: the full file contents including conflict markers

Example prompt shape:

```
You are resolving a git rebase conflict.

One commit from the default branch is being rebased. During replay of a local
commit, git produced conflicts in the files below.

Default branch commit: <hash>
Local commit being replayed: <hash> "<commit message>"

Conflicted files:

--- internal/config/loader.go ---
<full file contents with <<<<<<< / ======= / >>>>>>> markers>

--- internal/config/types.go ---
<full file contents with markers>

Resolve each file so that the intent of BOTH sides is preserved. Return your
result as JSON.
```

#### Expected Output

```json
{
  "all_resolved": true,
  "confidence": "high",
  "summary": "Local added validation logic, incoming renamed the config field. Kept both by applying the rename to the new validation code.",
  "files": {
    "internal/config/loader.go": "<full resolved file contents>",
    "internal/config/types.go": "<full resolved file contents>"
  }
}
```

Fields:
- **all_resolved** — `true` if the LLM believes it resolved all conflicts in all files, `false` if it couldn't
- **confidence** — `"high"`, `"medium"`, or `"low"` — how confident it is that both sides' behavior is preserved
- **summary** — human-readable description of what it did
- **files** — map of file path to full resolved file contents (no conflict markers)

#### Decision Logic

After each one-shot call:

1. If `all_resolved` is `false` or `confidence` is not `"high"`:
   - `git rebase --abort`
   - `git reset --mixed HEAD~1` (unwind WIP commit — after abort, the WIP commit is back on top of the branch)
   - Return failure to the caller with the LLM's summary explaining what went wrong
2. If `all_resolved` is `true` and `confidence` is `"high"`:
   - Write resolved file contents to the working directory
   - `git add <resolved files>`
   - `git rebase --continue`
   - Append the result to the accumulator
   - Continue the loop (next local commit may also conflict)

### Real-Time Progress via WebSocket

The original design used a synchronous HTTP request that blocked until the entire operation completed. This caused three problems in practice:

1. **No user feedback** — the operation takes minutes (N sequential LLM calls), but the UI showed nothing
2. **HTTP timeout** — the browser kills the connection before the response arrives, showing "Failed to fetch" even when the backend succeeded
3. **No visible workspace lock** — the backend mutex prevents concurrent operations, but the UI gave no indication the workspace was busy

#### Architecture: Fire-and-Forget POST + Stateful WS Broadcast

The operation is split into two parts:

1. **HTTP POST** `/api/workspaces/{id}/linear-sync-resolve-conflict` — kicks off the operation in a background goroutine and returns **immediately** with `202 Accepted`. No long-lived HTTP connection.

2. **WebSocket** `/ws/dashboard` — the existing dashboard WebSocket broadcasts a **new message type** `linear_sync_resolve_conflict` containing the full operation state. The backend updates this state at every step and triggers a broadcast. The frontend renders whatever the latest snapshot contains.

This follows the app's existing convention: the dashboard WS sends full state on every broadcast (not deltas), so missed messages don't matter. The client just replaces what it has with whatever arrives.

#### Operation State Object

The backend holds a `LinearSyncResolveConflictState` struct in memory, keyed by workspace ID. This is the JSON payload broadcast over the WebSocket. It captures every action taken.

```json
{
  "type": "linear_sync_resolve_conflict",
  "workspace_id": "beats-of-the-ancient-001",
  "status": "in_progress",
  "hash": "7fa489e",
  "started_at": "2026-02-03T14:30:00",
  "finished_at": null,
  "message": "",
  "steps": [
    {
      "action": "check_behind",
      "status": "done",
      "message": "5 commits behind origin/main, rebasing 7fa489e",
      "at": "2026-02-03T14:30:00"
    },
    {
      "action": "wip_commit",
      "status": "done",
      "message": "No local changes, skipped WIP commit",
      "created": false,
      "at": "2026-02-03T14:30:01"
    },
    {
      "action": "rebase_start",
      "status": "done",
      "message": "git rebase 7fa489e — conflict detected",
      "at": "2026-02-03T14:30:01"
    },
    {
      "action": "conflict_detected",
      "status": "done",
      "local_commit": "abc123",
      "local_commit_message": "Add config validation",
      "files": ["internal/config/loader.go", "internal/config/types.go"],
      "message": "Conflict on abc123 — 2 files",
      "at": "2026-02-03T14:30:02"
    },
    {
      "action": "llm_call",
      "status": "in_progress",
      "local_commit": "abc123",
      "files": ["internal/config/loader.go", "internal/config/types.go"],
      "message": "Calling LLM to resolve 2 files...",
      "at": "2026-02-03T14:30:02"
    }
  ]
}
```

As the operation progresses, more steps are appended and existing steps get their `status` updated. The full object is broadcast after every mutation.

#### Step Actions

Each step has an `action` field identifying what happened:

| Action | When | Key fields |
|--------|------|------------|
| `check_behind` | Start: counted commits behind | `message` |
| `wip_commit` | WIP commit created or skipped | `created`, `message` |
| `rebase_start` | `git rebase <hash>` executed | `message` (clean or conflict) |
| `conflict_detected` | Unmerged files identified | `local_commit`, `local_commit_message`, `files` |
| `llm_call` | One-shot LLM call started/completed | `local_commit`, `files`, `confidence`, `summary` |
| `write_files` | Resolved files written to disk | `files` |
| `rebase_continue` | `git rebase --continue` executed | `message` |
| `abort` | `git rebase --abort` executed | `message` (reason) |
| `wip_unwind` | WIP commit unwound | `message` |
| `done` | Operation completed | (top-level `status` and `message` updated) |

Each step also has:
- `status`: `"in_progress"` or `"done"` or `"failed"`
- `message`: human-readable description
- `at`: ISO timestamp

#### Top-Level Status

The top-level `status` field on the state object is one of:

- `"in_progress"` — operation is running
- `"done"` — completed successfully
- `"failed"` — aborted due to error, low confidence, or rebase failure

When `status` is `"done"` or `"failed"`, `finished_at` is set and `message` contains the final summary. The state object remains in memory until the user dismisses it (via `DELETE /api/workspaces/{id}/linear-sync-resolve-conflict-state`) or starts a new operation.

#### HTTP Endpoints

**POST** `/api/workspaces/{id}/linear-sync-resolve-conflict`

- Returns `202 Accepted` with `{"started": true, "workspace_id": "..."}` immediately
- Returns `409 Conflict` if an operation is already in progress for this workspace
- Returns `404` if workspace not found
- Kicks off the operation in a background goroutine

**DELETE** `/api/workspaces/{id}/linear-sync-resolve-conflict-state`

- Clears the in-memory state for the workspace (dismisses a completed/failed result)
- Returns `200 OK`
- Only works when `status` is `"done"` or `"failed"` — returns `409` if still in progress

#### WebSocket Integration

The dashboard WebSocket (`/ws/dashboard`) already sends `{type: "sessions", workspaces: [...]}` messages. We add a second message type:

```json
{
  "type": "linear_sync_resolve_conflict",
  "workspace_id": "beats-of-the-ancient-001",
  "status": "in_progress",
  "hash": "7fa489e",
  "started_at": "...",
  "finished_at": null,
  "message": "",
  "steps": [...]
}
```

The `doBroadcast` method is extended: after sending the `sessions` payload, it also sends any active `linear_sync_resolve_conflict` states as separate messages (one per active operation). This keeps the two concerns decoupled — the sessions payload doesn't change shape, and the conflict resolve state is its own message type.

The initial connection handler (`handleDashboardWebSocket`) also sends any active conflict resolve states on connect, same as it sends the initial sessions state.

The backend calls `BroadcastSessions()` after every step mutation, which triggers the debounced broadcast. The 500ms debounce means rapid steps may coalesce, but since we send full state, no information is lost.

#### Backend State Management

##### Storage

The `Server` struct gets a new field:

```go
linearSyncResolveConflictStates   map[string]*LinearSyncResolveConflictState // workspace ID -> state
linearSyncResolveConflictStatesMu sync.RWMutex
```

This is purely in-memory. Not persisted to disk. The state object is the single source of truth for whether an operation is active, what it's doing, and what it did.

##### Lifecycle

1. **Creation** — the POST handler creates the state with `status: "in_progress"`, inserts it into the map, then launches the goroutine. The insert happens *before* the goroutine starts, so there's no race between a second POST and the goroutine's first step.

2. **Mutation during operation** — the goroutine appends steps and updates step statuses. After each mutation, it calls `BroadcastSessions()` to push the updated state to all WS clients. The state is protected by `linearSyncResolveConflictStatesMu`.

3. **Completion** — when the operation finishes (success or failure), the goroutine sets `status` to `"done"` or `"failed"`, sets `finished_at`, and broadcasts one final time. The state remains in the map.

4. **Dismissal** — the user calls `DELETE /api/workspaces/{id}/linear-sync-resolve-conflict-state`. The handler removes the state from the map and broadcasts (so the frontend clears its view). Only works when `status` is `"done"` or `"failed"` — returns 409 if still in progress.

5. **Auto-clear on retry** — if a POST arrives and the existing state for that workspace is `"done"` or `"failed"`, the POST auto-clears it and starts a new operation. The user doesn't have to explicitly dismiss before retrying.

6. **Server restart** — the in-memory state is gone. Two sub-cases:
   - If the server restarted *mid-operation*, the git repo may be in a broken state (mid-rebase, WIP commit present). On startup, we scan all workspaces and check `rebaseInProgress(path)`. If a rebase is orphaned (in progress but no active conflict resolve state), we `git rebase --abort` and log a warning. This is a best-effort recovery — the workspace goes back to its pre-rebase state.
   - If the server restarted *after completion*, the state is simply gone. No problem — the result was transient.

7. **Goroutine panic** — the goroutine has a `defer recover()` that catches panics and sets the state to `"failed"` with a message like `"internal error: panic during conflict resolution"`. This ensures the workspace is never stuck in a permanent `"in_progress"` state.

##### Locking

The state object *is* the lock indicator:

- **POST idempotency** — the POST handler checks the map under `linearSyncResolveConflictStatesMu`. If a state exists with `status: "in_progress"`, return 409. No double-starts.
- **Frontend lock** — the frontend checks whether a conflict resolve state exists for this workspace with `status === "in_progress"`. If so, all sync/dispose/spawn buttons are disabled. Works across tabs because the WS broadcasts to all clients.
- **Backend git safety** — the existing `Manager.repoLock(repoURL)` mutex is still held during git operations, preventing concurrent git commands on the same repo. This is independent of the state object — it's a separate layer of protection for operations that don't go through the conflict resolve flow (e.g., other sync operations, git status polling).
- **Unlock** — when the goroutine finishes (sets status to `"done"` or `"failed"`), the frontend receives the broadcast and re-enables the buttons. The repo mutex is released when the goroutine exits the git-operation section.

##### Callback Pattern

The `LinearSyncResolveConflict` method on the workspace Manager is refactored to accept a callback `func(step Step)` that the handler goroutine uses to update the state and trigger broadcasts. The Manager itself doesn't know about WebSockets — it just calls the callback at each point. The handler goroutine wires the callback to: acquire lock → append step to state → release lock → broadcast.

#### Frontend

The `useSessionsWebSocket` hook is extended to handle the new message type:

```typescript
if (data.type === 'linear_sync_resolve_conflict') {
  setLinearSyncResolveConflictStates(prev => ({
    ...prev,
    [data.workspace_id]: data
  }));
}
```

This state is exposed via the `SessionsContext`.

##### Tab UI

The workspace gets a dedicated route `/resolve-conflict/:workspaceId` rendered by `LinearSyncResolveConflictPage`. A tab appears in `SessionTabs` when a conflict resolve state exists for the workspace. The tab is labeled **"Resolve conflict on \<hash\>"** (showing the first 7 characters of the hash, or "..." if not yet known). When the operation is in progress, the tab shows a spinner.

Clicking "sync from main conflict" in the workspace header navigates to this route immediately (before the POST response returns), so the user sees the page instantly. The progress appears as the WS broadcasts arrive.

The page shows:

- **Header**: status indicator (spinner while in_progress, check/x when done/failed), hash being rebased
- **Step list**: each step rendered as a line item with its status icon, timestamp, and message. The currently in-progress step has a spinner.
- **Final summary**: when done/failed, a banner with the outcome
- **Dismiss button**: when done/failed, calls DELETE to clear the state

While the operation is active (`status: "in_progress"`), all sync/dispose buttons in the workspace header are disabled. This is determined by checking whether a conflict resolve state exists for this workspace with `status === "in_progress"`.

### Accumulated Results

The `resolutions` array is computed from the steps when the operation completes and included in the final state object:

```json
{
  "type": "linear_sync_resolve_conflict",
  "workspace_id": "...",
  "status": "done",
  "hash": "7fa489e",
  "started_at": "...",
  "finished_at": "...",
  "message": "Rebased 7fa489e with 2 conflict(s) resolved",
  "resolutions": [
    {
      "local_commit": "111aaa",
      "local_commit_message": "Add config validation",
      "all_resolved": true,
      "confidence": "high",
      "summary": "Local added validation logic, incoming renamed the config field. Kept both.",
      "files": ["internal/config/loader.go", "internal/config/types.go"]
    }
  ],
  "steps": [...]
}
```

The `resolutions` array is only present when the operation is complete (either `"done"` or `"failed"`). The step-by-step `steps` array is always present.

### Workspace Locking

Three levels of locking:

1. **Backend mutex** — `Manager.repoLock(repoURL)` is held for the duration of the git operations, preventing concurrent Manager-level operations on the same repo (other syncs, clones, git status updates). This is the same mutex used today.

2. **UI lock** — the existence of a conflict resolve state with `status: "in_progress"` for a workspace disables all sync/dispose buttons in the frontend. This is purely client-side, derived from the WS state. If the user opens a second tab, the WS broadcast delivers the state there too.

3. **POST idempotency** — the POST endpoint returns `409` if an operation is already in progress. This prevents double-starts.

Note: running tmux sessions (agents) on the workspace are *not* frozen during this operation. The mutex prevents concurrent Manager operations, but agents can still write to the working directory. In practice this is acceptable — the user is choosing to run this operation and can coordinate with their agents. If this becomes a problem in the future, SIGSTOP/SIGCONT on workspace sessions is a viable option.

### Identifying the Local Commit During Rebase

When a rebase pauses on a conflict, the local commit being replayed is available via `git rev-parse REBASE_HEAD`. The commit message can be read via `git log -1 --format=%s REBASE_HEAD`. These are used to populate `local_commit` and `local_commit_message` in the steps and in the accumulated results.

### Detecting Rebase State

After `git rebase` or `git rebase --continue`:

- **Exit code 0 + no rebase directory** — rebase is complete.
- **Exit code 0 + rebase directory still present** — more commits to replay. Check for unmerged files.
- **Exit code non-zero + rebase directory present + unmerged files** — next commit conflicted. Loop back to LLM resolution.
- **Exit code non-zero + rebase directory present + no unmerged files** — git may have auto-resolved content conflicts. Try `git rebase --continue`. If that succeeds, check if rebase is done or loop. If that also fails with no unmerged files, abort.
- **Exit code non-zero + no rebase directory** — real failure. Abort.

We check both `.git/rebase-merge/` and `.git/rebase-apply/` for rebase detection, and `git diff --name-only --diff-filter=U` for unmerged files.

### Timeouts

Per-one-shot timeout is configured via `conflict_resolve.timeout_ms` in config (default 120s). There is no total timeout — the operation runs until complete or failed. The HTTP POST returns immediately, so there's no HTTP timeout concern.

### No Resume on Partial Failure

If the operation fails partway through, the rebase is aborted entirely and the workspace is restored to its pre-operation state. The `steps` array in the final state shows exactly what happened and where it failed. The user can retry (which starts fresh) or resolve manually.

### What Can Go Wrong

- **LLM reports low confidence or can't resolve** — `git rebase --abort`, unwind WIP, state set to `"failed"` with the LLM's summary in the abort step.
- **LLM times out or errors** — same: abort, unwind, report in steps.
- **LLM says high confidence but `git rebase --continue` fails with no unmerged files** — the resolution was broken. Abort and report.
- **Many local commits conflict** — N sequential one-shot LLM calls. Each is a visible step in the state. The user can watch progress in real time.
- **WIP commit left behind** — if `git add -A` picks up unexpected files (e.g., IDE artifacts), a WIP commit is created when the user expected none. The step log makes this visible (`wip_commit` step with `created: true`). The abort/unwind path must handle this correctly regardless.
