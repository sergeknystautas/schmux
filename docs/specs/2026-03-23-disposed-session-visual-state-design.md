# Disposed Session/Workspace Visual State

When a session or workspace is being disposed, the sidebar should immediately communicate that it's being torn down — grayed out, non-clickable — rather than sitting there looking normal for several seconds while cleanup runs.

## Current behavior

Sessions have statuses (`provisioning`, `running`, `stopped`, `failed`, `queued`). Workspaces have no status field. Disposal removes items from state entirely, then broadcasts. Between clicking dispose and the broadcast arriving, the sidebar shows no visual change.

## Design

### Workspace status (new)

Add a first-class `Status` field to `state.Workspace` following the same pattern as sessions:

- `WorkspaceStatusProvisioning = "provisioning"` — set when workspace creation begins
- `WorkspaceStatusRunning = "running"` — set when workspace creation completes
- `WorkspaceStatusFailed = "failed"` — set when workspace creation fails (e.g., `prepare()` errors)
- `WorkspaceStatusDisposing = "disposing"` — set when disposal starts

The field is persisted to state JSON so it survives daemon restarts.

#### Wiring provisioning/running into workspace creation

In workspace manager's `GetOrCreate()`:

- **New workspace** (via `create()`): Set `Status: "provisioning"` when constructing the `state.Workspace` struct. After `prepare()` completes successfully, set `Status` to `"running"`. If `prepare()` fails, set `Status` to `"failed"`.
- **Reused workspace** (existing path match): If the workspace already has a status, leave it. If it has an empty status (pre-existing workspaces from before this change), set it to `"running"`.
- **Remote workspace creation**: Same pattern — `provisioning` at start, `running` on success, `failed` on error.

### Session status addition

Add `SessionStatusDisposing = "disposing"` to the existing session status constants in `internal/state/state.go`.

Note: The `Session.Status` field was previously only used for remote sessions (`provisioning`, `running`, `failed`). With this change, `disposing` applies to both local and remote sessions. Update the field comment to reflect this. The branches page status label (which checks `sess.Status != ""`) will display `disposing` — this is correct behavior.

### Status transitions in the manager layer

The `disposing` status must be set inside the manager's `Dispose()` method (session manager and workspace manager), not in the HTTP handlers. This ensures all callers (HTTP API, CLI, internal automation) get consistent behavior.

The manager sets the status, saves state, and returns control. The caller (handler) is responsible for triggering the WebSocket broadcast, since broadcasting is a dashboard concern.

### Dispose handler flow

All three dispose endpoints in `handlers_dispose.go` change to a two-broadcast pattern. The handler remains blocking (returns HTTP response after teardown completes), but the initial broadcast fires via WebSocket before the blocking `Dispose()` call, giving the client immediate visual feedback.

Note: `BroadcastSessions()` has a 100ms trailing debounce. This is acceptable for the disposing transition — the user just confirmed a dialog, so 100ms is imperceptible.

**Session dispose (`handleDispose`):**

1. Set session status to `disposing` and save state (via manager method)
2. `BroadcastSessions()` (client sees gray via WebSocket within ~100ms)
3. Call `s.session.Dispose()` — blocks until teardown completes
4. On success: broadcast again (session gone — already exists at line 50)
5. On failure: revert status to previous value, save state, broadcast, return error

**Workspace dispose (`handleDisposeWorkspace`):**

1. Set workspace status to `disposing` and save state (via manager method)
2. `BroadcastSessions()`
3. Call `s.workspace.Dispose()` — blocks
4. On success: broadcast again (already exists at line 94)
5. On failure: revert to `running`, save state, broadcast, return error

**Dispose all (`handleDisposeWorkspaceAll`):**

1. Set workspace status to `disposing` AND all its sessions to `disposing`, save state (via manager methods)
2. `BroadcastSessions()` (everything grays out at once)
3. Proceed with concurrent session disposal, then workspace disposal
4. Add a post-completion `BroadcastSessions()` call (currently missing from this handler)
5. On workspace disposal failure: revert workspace to `running`, save state, broadcast, return error. Individual session failures are logged but do not abort workspace disposal (existing behavior with `DisposeForce`).

**Idempotency:** If a session or workspace already has `disposing` status when a dispose request arrives, return 200 OK immediately (no-op). This prevents double-dispose from concurrent keyboard + click or repeated API calls.

### Go response types

`WorkspaceResponseItem` in `handlers_sessions.go` needs a new field:

```go
Status string `json:"status,omitempty"`
```

`buildSessionsResponse()` must copy the workspace status into the response item (e.g., `Status: ws.Status`).

`SessionResponseItem` already has a `Status` field that flows to the client.

### TypeScript types

Both `SessionResponse` and `WorkspaceResponse` are manually maintained in `assets/dashboard/src/lib/types.ts` (not auto-generated via `gen-types`). Both need a `status?: string` field added. `SessionResponse` does not currently have one despite the Go side sending it — add it. `WorkspaceResponse` needs it added as well.

### Client — Sidebar rendering

In `AppShell.tsx`, when `workspace.status === 'disposing'`, the workspace header div gets a `nav-workspace--disposing` CSS class. When `sess.status === 'disposing'`, the session row gets `nav-session--disposing`.

CSS for both classes in `global.css`: reduced opacity (`0.5`), `pointer-events: none` (prevents clicks), muted text color.

Keyboard navigation shortcuts (Cmd+Up/Down via `findNextWorkspaceWithSessions`) should skip workspaces where `workspace.status === 'disposing'`.

### Preventing double-dispose

All dispose triggers must check for `disposing` status and no-op:

- **Dispose buttons** in `WorkspaceHeader.tsx`, `SessionDetailPage.tsx`, and `SessionTabs.tsx`: disabled when status is `disposing`
- **Keyboard shortcuts**: `W` key in `SessionDetailPage.tsx` and `Shift+W` in `AppShell.tsx` must check status before triggering disposal
- **Sidebar click handlers**: no-op when session or workspace status is `disposing`

### Edge cases

**Disposal failure:** If disposal fails after setting `disposing` status, the manager reverts session status to its previous value (likely `stopped`) and workspace status to `running`. Save state; the handler broadcasts so the client ungrays the item.

**Rollback model:** Rollback only applies when `Dispose()` returns an error. If `Dispose()` succeeds, the item is removed from state entirely (no rollback needed). State removal within `Dispose()` is atomic — there is no partial-removal scenario.

**Daemon crash during disposing:** Because the `disposing` status is persisted via `state.Save()`, a daemon restart will show the item as disposing. On startup, the daemon should find all items with `disposing` status and retry disposal. If retry fails, revert to `running` (workspaces) or `stopped` (sessions) and log a warning.

**Remote workspace + disconnected host:** `disposing` status takes priority over `remote_host_status`. If something is disposing, it is grayed out regardless of remote host state.

**Other pages:** Existing "navigate home if workspace/session missing" logic in SessionDetailPage, DiffPage, GitCommitPage, etc. continues to work. The item remains in state during teardown; once removed, the final broadcast triggers the existing navigation.

**Pre-existing workspaces:** Workspaces created before this change will have an empty `Status` field. The client treats empty/absent status the same as `running` — only `disposing` (and `provisioning`/`failed`) trigger special visual states.

### API documentation

Update `docs/api.md` to document:

- New `status` field on workspace response objects
- New `disposing` value for session `status` field
- Workspace status lifecycle: `provisioning` → `running` → `disposing`

### Test plan

**Backend:**

- Status set to `disposing` before teardown begins (unit test on manager methods)
- Status reverted on disposal failure
- Idempotent 200 OK when disposing an already-disposing session/workspace
- Dispose-all sets all statuses atomically before teardown
- `buildSessionsResponse()` includes workspace status
- Startup reconciliation retries stuck `disposing` items
- Workspace creation sets `provisioning` → `running` lifecycle
- Workspace creation failure sets `failed` status

**Frontend:**

- Sidebar applies `nav-workspace--disposing` / `nav-session--disposing` CSS classes
- Disposing items are not clickable
- Dispose buttons disabled when status is `disposing`
- Keyboard shortcuts no-op when status is `disposing`
- Keyboard navigation (Cmd+Up/Down) skips disposing workspaces
- Pre-existing workspaces with empty status render normally
