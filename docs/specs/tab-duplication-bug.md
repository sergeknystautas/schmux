# Tab Duplication Bug — Investigation

## The Problem

Workspaces accumulate duplicate "commit graph" (kind=git) tabs. Visible in the dashboard UI when navigating between workspaces with keyboard shortcuts. Persists until page reload (and across daemon restarts since it's persisted to state.json).

## Confirmed Evidence

### state.json has the duplicate right now

```
vellum-001 tabs (3):
  [0] id=sys-diff-vellum-001 kind=diff label=Diff
  [1] id=sys-git-vellum-001  kind=git  label=commit graph
  [2] id=sys-git-vellum-001  kind=git  label=commit graph
```

Same ID, same kind, same label. Two identical entries in the persisted JSON array.

### Timeline from TAB-DEBUG logs

| Time         | Event                                                                                  |
| ------------ | -------------------------------------------------------------------------------------- |
| 13:58:46     | Load: 3 tabs — **markdown**, diff, git (clean)                                         |
| 13:58:46     | Migration: updated existing git (dedup working), updated existing diff (dedup working) |
| 13:59:28     | Session vellum-001-b69c829a disposed                                                   |
| ~14:00–14:47 | Session events only. **No TAB-DEBUG entries.** No AddTab, no RemoveTab logged.         |
| 14:48:01     | Load: 3 tabs — diff, git, **git** (DUPLICATE already in state.json)                    |
| 14:48:01     | Migration: updated existing git (first match only), updated diff                       |

### What happened between 13:58 and 14:48

- The **markdown tab disappeared** from the persisted state
- A **second git tab appeared** in its place
- No AddTab call was logged (we would have seen "TAB-DEBUG: added new tab")
- The tab count stayed at 3 — one was replaced, not appended

### Creation-time duplicate in commonworks-001

Later investigation found a second class of evidence in a freshly created workspace:

- `commonworks-001` was created at **21:08:21** on **April 1, 2026**
- Its persisted tabs were:

```
commonworks-001 tabs (3):
  [0] id=sys-diff-commonworks-001 kind=diff label=Diff
  [1] id=sys-git-commonworks-001  kind=git  label=commit graph
  [2] id=sys-git-commonworks-001  kind=git  label=commit graph
```

- Both git entries had the exact same `created_at`:
  - `2026-04-01T21:08:21.067432-07:00`
  - `2026-04-01T21:08:21.067432-07:00`
- That timestamp matches the workspace creation window in `daemon-startup.log`

This is important because it suggests at least one duplication path happens at workspace creation time, not only after a later tab removal. It is still useful to compare the duplicate tab timestamp to the most recent daemon start time: if they match too closely, the creation flow may be double-writing; if they do not, a later whole-workspace rewrite is more likely.

## Ruled Out

### addTabLocked — RULED OUT, NOT THE CAUSE

- The dedup logic works correctly: kind=git dedup key is "git" (singleton)
- Every log entry shows "updated existing tab" — never "added new tab" for git kind
- The duplicate was already in state.json BEFORE Load ran
- addTabLocked only runs during AddTab calls and Load migration — neither logged a new git tab
- **Do not investigate further.**

### Load migration — RULED OUT, NOT THE CAUSE

- Migration calls addTabLocked which correctly dedup-matches the first git tab
- It updates in place and returns — never adds a second
- The duplicate is present BEFORE migration runs (visible in the "existing tab" dump)
- **Do not investigate further.**

## All Write Operations to Workspace Tabs

Every code path in `internal/state/state.go` that writes to the Tabs slice:

### 1. Load migration — nil init (line 257-258)

```go
if w.Tabs == nil {
    w.Tabs = []Tab{}
}
```

Initializes nil to empty. Cannot create duplicates.

### 2. AddWorkspace — nil init (line 416-418)

```go
needsSeed := w.Tabs == nil
if needsSeed {
    w.Tabs = []Tab{}
}
```

Initializes nil to empty for new workspaces. Cannot create duplicates.

### 3. AddWorkspace — upsert replaces entire workspace (line 411)

```go
s.Workspaces[i] = w
```

**DANGER**: Replaces the entire workspace struct including Tabs. If the incoming `w` was obtained via `GetWorkspace()` (shallow copy sharing the Tabs backing array), and RemoveTab modified that backing array in the meantime, the stale slice header with the old length would be written back, exposing orphaned elements.

### 4. UpdateWorkspace — replaces entire workspace (line 475)

```go
s.Workspaces[i] = w
```

**DANGER**: Same as #3. Any caller doing Get → modify field → Update writes back whatever Tabs slice header it holds, including stale length from before a RemoveTab.

### 5. addTabLocked — dedup update in place (line 513)

```go
s.Workspaces[i].Tabs[j] = tab
```

Updates a single element. Cannot create duplicates. **RULED OUT.**

### 6. addTabLocked — prepend new tab (line 520)

```go
s.Workspaces[i].Tabs = append([]Tab{tab}, s.Workspaces[i].Tabs...)
```

Creates a new backing array. Only runs when dedup check finds no match. **RULED OUT.**

### 7. UpdateTab — update in place by ID (line 546)

```go
s.Workspaces[i].Tabs[j] = tab
```

Updates a single element. Cannot create duplicates.

### 8. RemoveTab — delete by shifting (line 569)

```go
s.Workspaces[i].Tabs = append(w.Tabs[:j], w.Tabs[j+1:]...)
```

**KEY OPERATION**: Shifts elements left in the backing array. When removing index 0 from [markdown, diff, git]:

- Backing array becomes [diff, git, git] (orphaned last element)
- New slice header has len=2 (correct)
- But any stale slice header with len=3 pointing to same array now sees [diff, git, git]

### 9. handlers_sessions.go — builds broadcast response (line 226)

```go
workspaceMap[ws.ID].Tabs = tabItems
```

Writes to a local response struct, not to state. Not relevant.

## Callers of UpdateWorkspace (the likely vector)

There are **15 non-test callers** of `UpdateWorkspace`:

- `internal/workspace/manager.go` — lines 422, 465, 483, 489, 919, 962, 1048, 1064
- `internal/workspace/scan.go` — line 122
- `internal/session/manager.go` — line 407
- `internal/preview/manager.go` — line 530
- `internal/dashboard/handlers_sync.go` — lines 177, 529

Every one of these follows the pattern: `GetWorkspace() → modify fields → UpdateWorkspace()`. The workspace returned by GetWorkspace is a value copy whose `Tabs` slice header shares the internal backing array.

## Leading Theory: GetWorkspace + RemoveTab + UpdateWorkspace race

1. Caller A calls `GetWorkspace("vellum-001")` → gets workspace copy with Tabs header {len=3, ptr=X} pointing to [markdown, diff, git]
2. RemoveTab removes markdown (index 0) → shifts array in place → array at ptr X becomes [diff, git, git] → s.Workspaces[i].Tabs gets header {len=2, ptr=X}
3. Caller A still holds header {len=3, ptr=X} → sees [diff, git, git]
4. Caller A modifies some other field (e.g., LinesAdded), calls `UpdateWorkspace(w)` → writes `s.Workspaces[i] = w`
5. Now s.Workspaces[i].Tabs = {len=3, ptr=X} = [diff, git, git]
6. Save() writes [diff, git, git] to disk

This explains:

- Why the tab count stayed at 3
- Why the markdown disappeared and a duplicate git appeared
- Why no AddTab was logged
- Why the duplicate was already in state.json

## Reproduction

- Have multiple workspaces with sessions
- Navigate between workspaces using Cmd+Up/Down keyboard shortcuts
- Observe duplicate "commit graph" tabs appearing
- Check `~/.schmux/state.json` to confirm duplicates are persisted

## Next Steps

- Confirm theory by checking which UpdateWorkspace callers run on the vellum-001 workspace during the 13:58–14:48 window (git status updates, session dispose, etc.)
- Fix: either deep-copy Tabs in GetWorkspace/GetWorkspaces, or make RemoveTab allocate a new backing array instead of shifting in place
- Remove the `UpdateTab` pattern for system-owned tabs (scenario 7). System tabs should follow the diff-tab model and be derived from workspace state rather than mutated in place.
