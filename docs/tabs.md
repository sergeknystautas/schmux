# Tab Systems

The dashboard has two independent tab systems in the workspace tab bar:

1. **Session tabs** — one per running agent session. Server-owned lifecycle, client-owned order.
2. **Accessory tabs** — workspace-scoped UI tabs for views like diff, git graph, previews. Ownership varies by kind.

Both render in the same `SessionTabs` component but are separate drag zones — sessions and accessories cannot be dragged between groups.

## Session Tabs

Each session gets a tab. Tabs are reorderable via drag-and-drop (desktop only). Custom order is stored client-side in localStorage keyed per workspace (`schmux:tab-order:{workspaceId}`).

Session tab lifecycle:

- Created by the server when a session spawns
- Removed by the server when a session is disposed
- Order is a client-side concern — the server provides sessions in its own order, the client re-sorts for display

### Drag-and-Drop

Uses `@dnd-kit/sortable` with a `PointerSensor` (5px activation distance to distinguish clicks from drags). During an active drag, the session list is frozen (snapshotted) so WebSocket updates don't disrupt the drag. On drag-end, the freeze releases and the new order is reconciled.

Disabled during workspace locks and on mobile (viewport ≤ 768px).

See `docs/specs/2026-03-23-draggable-session-tabs-design.md` for the full design.

## Accessory Tabs

### Tab Entity

```go
type Tab struct {
    ID        string            `json:"id"`
    Kind      string            `json:"kind"`
    Label     string            `json:"label"`
    Route     string            `json:"route"`
    Closable  bool              `json:"closable"`
    Meta      map[string]string `json:"meta,omitempty"`
    CreatedAt time.Time         `json:"created_at"`
}
```

Tabs live inside the Workspace struct as `Tabs []Tab`, persisted in `state.json`, and broadcast to all clients via WebSocket.

For tab kinds that need substantial persisted page state, the tab stays lightweight and points to adjacent workspace-owned state instead of storing that process data in the tab itself. For example, `resolve-conflict` tabs use `meta.hash` to point at persisted `workspace.resolve_conflicts` records.

The diff tab's `label` is empty in state.json and derived at broadcast time from workspace git stats (`"{n} files changed"`). This avoids high-churn writes to state for a value that changes with every file edit.

### Ownership

Who creates a tab depends on **who knows the tab needs to exist**:

| Kind               | Created by                                        | Rationale                            |
| ------------------ | ------------------------------------------------- | ------------------------------------ |
| `diff`             | Server — on workspace creation                    | Always present for VCS workspaces    |
| `git`              | Server — on workspace creation                    | Always present for VCS workspaces    |
| `preview`          | Server — when preview detected                    | Server owns preview lifecycle        |
| `resolve-conflict` | Server — one per conflict event                   | Server detects conflicts during sync |
| `commit`           | Client API — user clicks commit hash in git graph | User-initiated navigation            |
| `markdown`         | Client API — user clicks "view" in diff           | User-initiated navigation            |

**Rule:** If the server is the source of truth for _when something exists_ (workspace has VCS, preview detected, conflict occurred), the server creates the tab. If the **user's action** is what creates the need (clicking a hash, clicking "view"), the client calls `POST /api/workspaces/{id}/tabs` and the server stores and broadcasts.

The server is a persistence and broadcast layer for client-initiated tabs. It does not make UI decisions in data-fetch handlers.

### Lifecycle

| Kind               | Removed by                  | Closable |
| ------------------ | --------------------------- | -------- |
| `diff`             | Server — workspace disposal | No       |
| `git`              | Server — workspace disposal | No       |
| `preview`          | User — close button         | Yes      |
| `resolve-conflict` | User — close button         | Yes      |
| `commit`           | User — close button         | Yes      |
| `markdown`         | User — close button         | Yes      |

Closing a tab calls `DELETE /api/workspaces/{id}/tabs/{tabId}`. For preview tabs, this cascades to `previewManager.Delete()` for proxy teardown.

### Idempotency

Tab creation is idempotent by `(kind, dedup_key)` within a workspace:

| Kind               | Dedup key                       |
| ------------------ | ------------------------------- |
| `diff`             | (singleton — one per workspace) |
| `git`              | (singleton — one per workspace) |
| `preview`          | preview ID                      |
| `resolve-conflict` | commit hash                     |
| `commit`           | commit hash                     |
| `markdown`         | filepath                        |

### Migration

On state load, existing workspaces missing tabs get baseline tabs seeded (diff, git for VCS workspaces; preview tabs for existing previews). Runs once per workspace on upgrade.

### Adding a New Tab Kind

1. Pick a `kind` string
2. Decide ownership: server-created or client-initiated via API
3. Add a route for the tab's page

No changes to `SessionTabs.tsx`, `AppShell.tsx`, or prop types needed.
