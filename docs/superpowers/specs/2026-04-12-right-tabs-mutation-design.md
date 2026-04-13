# Right Tabs Mutation: Workspace Manager Ownership

## Problem

Tab lifecycle is scattered across 5 creation sites (HTTP handler, sync handler, preview manager, `AddWorkspace`, `Load` migration), each constructing `state.Tab` structs independently with duplicated route patterns, label formats, ID schemes, and inconsistent save/broadcast behavior. The client constructs routes that should be server-owned. Adding a new tab kind requires touching multiple files and copy-pasting construction logic.

## Design

The workspace manager owns the full tab lifecycle. Callers say **what** they want. The workspace manager handles **how** — ID, route, label, closable policy, dedup, save, and broadcast.

### Core Mechanism: Every Mutation Saves

All public tab methods go through a single internal helper that guarantees save and broadcast after every mutation. Save is not a step callers perform — it is the mechanism.

```go
// mutateTabsAndSave wraps every tab mutation. No tab method bypasses this.
func (m *Manager) mutateTabsAndSave(fn func() error) error {
    if err := fn(); err != nil {
        return err
    }
    m.state.Save()
    m.broadcastFn()
    return nil
}
```

`broadcastFn` is a callback set during daemon wiring, same pattern as the existing `onLockChangeFn` on the workspace manager.

### Public API

```go
// Open methods — one per tab kind, typed params, returns tab for callers that need route/ID
func (m *Manager) OpenCommitTab(wsID, hash string) (*state.Tab, error)
func (m *Manager) OpenMarkdownTab(wsID, filepath string) (*state.Tab, error)
func (m *Manager) OpenPreviewTab(wsID, previewID string, port int) (*state.Tab, error)
func (m *Manager) OpenResolveConflictTab(wsID, hash string) (*state.Tab, error)

// Close — generic, hooks handle kind-specific cleanup
func (m *Manager) CloseTab(wsID, tabID string) error

// Seed — creates non-closable system tabs (diff, git) for new workspaces
func (m *Manager) SeedSystemTabs(wsID string) error
```

Every method goes through `mutateTabsAndSave`. No exceptions.

### Tab Construction Rules

Each `Open*` method encapsulates all knowledge about its tab kind:

| Kind               | ID Scheme                          | Route                              | Label                   | Closable | Meta         |
| ------------------ | ---------------------------------- | ---------------------------------- | ----------------------- | -------- | ------------ |
| `diff`             | `sys-diff-{wsID}`                  | `/diff/{wsID}`                     | (none — client-derived) | false    | none         |
| `git`              | `sys-git-{wsID}`                   | `/commits/{wsID}`                  | `commit graph`          | false    | none         |
| `commit`           | UUID                               | `/commits/{wsID}/{shortHash}`      | `commit {shortHash}`    | true     | `hash`       |
| `markdown`         | UUID                               | `/diff/{wsID}/md/{filepath}`       | filename only           | true     | `filepath`   |
| `preview`          | `sys-preview-{previewID}`          | `/preview/{wsID}/{previewID}`      | `web:{port}`            | true     | `preview_id` |
| `resolve-conflict` | `sys-resolve-conflict-{shortHash}` | `/resolve-conflict/{wsID}/{tabID}` | (none — client-derived) | true     | `hash`       |

Route patterns, label formats, and ID schemes exist in exactly one place.

### Client-Derived Display

The `diff` and `resolve-conflict` tabs have no server-side label. The frontend derives their display from workspace data it already receives via WebSocket:

- **diff**: the frontend renders the file count from the workspace's git stats
- **resolve-conflict**: the frontend determines closability from the resolve-conflict record state

The broadcaster code in `handlers_sessions.go:231-263` that rewrites diff labels and resolve-conflict closability is a bug and is deleted as part of this refactor. The broadcaster serves tabs as persisted, with no field rewriting.

### Tab Close Hooks

```go
type TabCloseHook interface {
    OnTabClose(wsID string, tab state.Tab) error
}

func (m *Manager) RegisterTabCloseHook(kind string, hook TabCloseHook)
```

`CloseTab` flow (inside `mutateTabsAndSave`):

1. Fetch tab by ID, verify it exists
2. Check closable — call hook's `CanClose` if registered, otherwise fall back to `tab.Closable`
3. Call `state.RemoveTab` — removes tab from in-memory state
4. Call the registered hook for that kind (if any) — hook does sidecar cleanup
5. Save + broadcast

**Ordering rationale:** RemoveTab (step 3) runs before the hook (step 4). If save fails after both, the daemon restarts with the old persisted state — both the tab and the sidecar record (preview, resolve-conflict) reload from disk, which is consistent. If the hook ran first and save failed, the sidecar would be cleaned up but the tab would reappear on restart — an orphan. By removing the tab first, a save failure restores both to their pre-mutation state.

**If the hook itself fails:** RemoveTab already ran in memory. The workspace manager rolls back by re-adding the tab (via `state.AddTab` with the same tab struct), then returns the hook error without saving. No partial mutation reaches disk.

Hooks registered at daemon startup:

- **Preview manager** registers for `"preview"` — stops listener, removes preview record
- **Dashboard server** registers for `"resolve-conflict"` — removes resolve-conflict record, cleans up in-memory sync state

Hooks do cleanup but do not save. The workspace manager saves once after all mutations complete.

### Closable Override

The close hook interface needs a way to say "this tab is not closable right now" without the workspace manager knowing why. Extend the interface:

```go
type TabCloseHook interface {
    CanClose(wsID string, tab state.Tab) (bool, error)
    OnTabClose(wsID string, tab state.Tab) error
}
```

The resolve-conflict hook's `CanClose` checks whether the sync goroutine is in-progress. The workspace manager calls `CanClose` before proceeding. If no hook is registered for a tab's kind, the workspace manager falls back to `tab.Closable`. This moves the "is it closable?" logic out of the HTTP handler and into the hook that owns the domain knowledge.

## What Changes

### Deleted

- **`state.Load()` tab migration** (state.go lines 405-442) — one-time migration code that runs on every restart, clobbering `CreatedAt` timestamps. The migration existed to compensate for callers like `preview/manager.go:192` that add tabs without saving, leaving a window where a concurrent save from stale state could overwrite the tab. `mutateTabsAndSave` closes this window by saving immediately after every mutation, making the migration unnecessary.
- **Tab struct construction** from `handlers_tabs.go`, `handlers_sync.go`, `preview/manager.go` — all replaced by workspace manager method calls.
- **`allowedClientTabKinds` map** — the workspace manager's typed methods implicitly define what exists.
- **Kind-specific cascading logic** from `handleTabDelete` — replaced by close hooks.

### Modified

**`state.AddWorkspace`** loses its tab seeding.

### All Workspace Creation Goes Through the Workspace Manager

Three callers currently bypass the workspace manager and call `state.AddWorkspace` directly:

- **`workspace/scan.go` `Scan()`** — discovers local filesystem workspaces not yet in state
- **`remote/manager.go` `ensureWorkspaceForHost()`** — creates workspaces for remote hosts
- **`session/manager.go` `SpawnRemote()`** — creates workspaces when spawning remote sessions

These callers must be changed to go through the workspace manager for workspace creation.

Additionally, the workspace manager's own internal creation methods (`GetOrCreate` at manager.go:704, `CreateLocalRepo` at manager.go:790, `CreateFromWorkspace` at manager.go:1681) also call `state.AddWorkspace` directly. These switch to the same centralized internal method.

All 6 production `state.AddWorkspace` call sites converge on a single internal method in the workspace manager that creates the workspace and seeds system tabs:

```go
// addWorkspace creates a workspace in state and seeds its system tabs.
// All workspace creation — internal and external — flows through this method.
func (m *Manager) addWorkspace(ws state.Workspace) error
```

The existing public creation methods (`GetOrCreate`, `CreateLocalRepo`, `CreateFromWorkspace`) call `addWorkspace` instead of `state.AddWorkspace`. External callers (`scan.go`, `remote/manager.go`, `session/manager.go`) call a public wrapper. `scan.go` already lives in the workspace package and has access to the manager. `remote/manager.go` and `session/manager.go` need a reference to the workspace manager — they get one during daemon wiring, same as other cross-manager references in the codebase.

**`handlers_tabs.go`** becomes a thin routing layer:

```go
func (s *Server) handleTabCreate(w http.ResponseWriter, r *http.Request) {
    wsID := chi.URLParam(r, "workspaceID")
    // decode {kind, ...params}
    // switch kind:
    //   "commit":   tab, err = s.wsManager.OpenCommitTab(wsID, params.Hash)
    //   "markdown": tab, err = s.wsManager.OpenMarkdownTab(wsID, params.Filepath)
    // return {id: tab.ID, route: tab.Route, status: "ok"}
}

func (s *Server) handleTabDelete(w http.ResponseWriter, r *http.Request) {
    wsID := chi.URLParam(r, "workspaceID")
    tabID := chi.URLParam(r, "tabID")
    err := s.wsManager.CloseTab(wsID, tabID)
    // return {status: "ok"} or error
}
```

No save, no broadcast, no cascading cleanup — the workspace manager handles all of it.

**`ensureResolveConflictTab`** becomes:

```go
func (s *Server) ensureResolveConflictTab(workspaceID, hash string) {
    if hash == "" {
        return
    }
    if _, err := s.wsManager.OpenResolveConflictTab(workspaceID, hash); err != nil {
        logging.Sub(s.logger, "workspace").Warn("failed to add resolve-conflict tab", "err", err)
    }
}
```

**`preview/manager.go`** tab creation becomes:

```go
_, _ = m.wsManager.OpenPreviewTab(ws.ID, preview.ID, preview.TargetPort)
```

Preview manager no longer imports or constructs `state.Tab`.

### API Contract Change

**Create tab request** changes from:

```json
{
  "kind": "commit",
  "label": "commit abc1234",
  "route": "/commits/ws1/abc1234",
  "closable": true,
  "meta": { "hash": "abc123..." }
}
```

To:

```json
{ "kind": "commit", "hash": "abc123def456..." }
```

Each kind has its own params. The server constructs everything else.

**Create tab response** changes from `{id, status}` to `{id, route, status}`. The `route` is needed so the frontend can set up pending navigation.

### Frontend Changes

**`api.ts` `createTab`** signature changes to accept kind-specific params and return route:

```typescript
export async function createTab(
  workspaceId: string,
  params: { kind: 'commit'; hash: string } | { kind: 'markdown'; filepath: string }
): Promise<{ id: string; route: string; status: string }>;
```

**CommitHistoryDAG.tsx** becomes:

```typescript
const { route } = await createTab(workspaceId, { kind: 'commit', hash: ln.node.hash });
setPendingNavigation({ type: 'tab', workspaceId, tabRoute: route });
```

No local route construction. No label construction. No closable flag. No meta assembly.

**DiffPage.tsx** becomes:

```typescript
const { route } = await createTab(workspaceId, { kind: 'markdown', filepath });
setPendingNavigation({ type: 'tab', workspaceId, tabRoute: route });
```

**SessionTabs.tsx** close callers: unchanged. They call `closeTab(wsID, tabID)` and handle success/error.

**SessionTabs.tsx** display changes required for client-derived tabs:

- **diff tab label**: currently rendered from `tab.label` (which the broadcaster rewrites to "N files changed"). Must change to derive label from `workspace.files_changed` in the component. The workspace already has `lines_added`/`lines_removed` used for the badge — `files_changed` is the same pattern.
- **resolve-conflict close button**: currently rendered from `tab.closable` (which the broadcaster rewrites based on sync state). Must change to derive closability from the workspace's `resolve_conflicts` records. The component already reads resolve-conflict state for the spinner badge — closability uses the same data.

### Unchanged

- `state.AddTab` / `RemoveTab` — still the CRUD layer, still handles dedup via `tabDedupKey`
- `Tab` struct shape — unchanged
- `tabDedupKey` — unchanged
- Tab rendering in the frontend — tabs still have the same shape from the WebSocket broadcast
- `closeTab()` API endpoint — still `DELETE /api/workspaces/{wsID}/tabs/{tabID}`

## Testing

- Each `Open*` method: verify correct ID scheme, route, closable, meta for its kind
- `CloseTab`: verify hook is called, tab is removed, state is saved
- `CloseTab` with non-closable tab: verify rejection
- `CloseTab` with hook's `CanClose` returning false: verify rejection
- `CloseTab` hook failure: verify tab is rolled back, no partial mutation saved
- `SeedSystemTabs`: verify diff + git tabs created for VCS workspaces, not for non-VCS
- `AddWorkspace`: verify system tabs are seeded as part of creation
- Dedup: calling `Open*` twice with same params updates rather than duplicates
- `mutateTabsAndSave`: verify every public method triggers save and broadcast
- Load() no longer seeds tabs: verify tabs survive daemon restart without re-creation
- API contract: verify create response includes `route`
- Frontend: verify pending navigation works with server-returned route
- Broadcaster: verify diff and resolve-conflict tabs are served as persisted with no field rewriting
- Non-manager workspace creation paths (`scan.go`, `remote/manager.go`, `session/manager.go`) go through workspace manager and produce workspaces with correct system tabs
