# Overlay Compounding Loop Design

## Problem

Schmux overlays are one-directional: files from `~/.schmux/overlays/<repo>/` are copied into workspaces at creation time, but changes agents make to those files (e.g., `.claude/settings.json` gaining new "always allow" permissions) are lost when the workspace is disposed. Users must manually copy changed files back to the overlay directory.

## Solution

A continuous bidirectional sync system that watches overlay-managed files in active workspaces, merges changes back to the overlay, and propagates updates to all other active workspaces for the same repo.

```
Workspace A modifies .claude/settings.json
    │
    ▼
[File Watcher] detects change (fsnotify, 2s debounce)
    │
    ▼
[Merge Engine] overlay unchanged since copy?
    ├─ yes → direct copy to overlay (fast path, no LLM)
    └─ no  → LLM merge(overlay current, workspace new) → write overlay
                │
                ▼
         [Propagator] copies updated overlay file → Workspaces B, C, D...
                      (skips Workspace A, the source of the change)
```

## Architecture

### New Package: `internal/compound/`

| File          | Responsibility                                      |
| ------------- | --------------------------------------------------- |
| `compound.go` | `Compounder` struct — lifecycle, orchestration      |
| `manifest.go` | Manifest tracking (file list + content hashes)      |
| `merge.go`    | Merge engine (fast path, LLM merge, skip detection) |
| `watcher.go`  | fsnotify setup, debouncing, anti-echo suppression   |

### Integration Point

The `Compounder` is created in `daemon.go` alongside the workspace and session managers. It receives:

- `*workspace.Manager` — for listing workspaces, overlay paths, finding repo configs
- `*state.State` — for reading workspace state
- An LLM oneshot function — same mechanism as NudgeNik (configurable target)

## Component 1: Manifest

When `CopyOverlay` runs, it returns a manifest: the list of files copied and their SHA-256 content hashes at copy time.

### State Change

Add `OverlayManifest` to `state.Workspace`:

```go
type Workspace struct {
    // ... existing fields ...
    OverlayManifest map[string]string `json:"overlay_manifest,omitempty"` // relPath → SHA-256 hash at copy time
}
```

### CopyOverlay Change

`CopyOverlay` gains a return value:

```go
func CopyOverlay(ctx context.Context, srcDir, destDir string) (map[string]string, error)
```

The returned map is `relPath → sha256hex` for each file successfully copied. The caller stores this on the workspace state.

## Component 2: File Watcher

Uses `fsnotify` (already a dependency) to watch overlay-managed files in each active workspace.

### Watch Setup

When a workspace gets its first session (or on daemon startup for workspaces with active sessions), the compounder:

1. Reads the workspace's `OverlayManifest`
2. Adds an `fsnotify` watch on each file's parent directory (fsnotify watches directories, not files)
3. Filters events to only overlay-managed files

### Debouncing

File writes often arrive in bursts. Per-file 2-second debounce: if the file changes again within 2 seconds, reset the timer. Only fire after 2 seconds of quiet.

### Anti-Echo

When the propagator writes to a workspace, it registers the file in a per-workspace "suppress" set (`map[string]time.Time`). The watcher checks this set and ignores events for suppressed files. Entries expire after 5 seconds.

### Watch Teardown

When the last session on a workspace is disposed, remove the watches. On workspace dispose, also remove watches (the directory is about to be deleted).

## Component 3: Merge Engine

Three paths, checked in order:

### Path 1: Skip (no change)

Read the workspace file's content. If its SHA-256 matches the manifest hash, the file hasn't changed — do nothing. This also catches propagation writes (the workspace file now matches the overlay because we just wrote it).

### Path 2: Fast Path (overlay unchanged)

Read the current overlay file's SHA-256. If it matches the manifest hash, the overlay hasn't changed since the copy — the workspace version is strictly newer. Copy workspace file directly to the overlay. Update the manifest hash for this workspace.

### Path 3: LLM Merge (conflict)

Both the overlay and the workspace have diverged from the base (manifest hash). Three-way merge via LLM:

- **Base**: the content at copy time (we don't store this — see note below)
- **Overlay current**: read from `~/.schmux/overlays/<repo>/<file>`
- **Workspace current**: read from workspace path

**Note on base content**: We only store the hash, not the full base content. For the LLM merge, we send just the overlay current and workspace current versions with instructions to union/merge them. This is sufficient for config files where the merge semantic is "keep everything from both."

**LLM Prompt**:

```
Merge these two versions of a configuration file. Both have been
modified independently from a common base.

Rules:
- For JSON files with arrays: union the arrays (keep all unique entries)
- For key-value settings: keep entries from both versions
- Never remove entries that exist in either version
- If values conflict for the same key, prefer VERSION B (the workspace version)
- Output ONLY the merged file content, no explanation or markdown fencing

VERSION A (current overlay):
<content>

VERSION B (workspace modification):
<content>
```

**LLM Target**: New configurable target in config, similar to NudgeNik. Falls back to the nudgenik target if not configured.

### Safety Checks

- **Binary files** (null byte in first 512 bytes): skip LLM, use last-write-wins
- **Large files** (>100KB): skip LLM, use last-write-wins
- **LLM failure** (timeout, empty response, invalid JSON for .json files): fall back to last-write-wins, log warning
- **Identical content**: if workspace file content equals overlay content, skip entirely

## Component 4: Propagator

After the overlay is updated, push the change to all other active workspaces for the same repo.

### Target Selection

Find all workspaces where:

- `w.Repo` matches the source workspace's repo URL
- Workspace has at least one active session
- Workspace ID != the source workspace

### Write

Copy the overlay file to each target workspace using the existing `copyFile` helper. Update the target workspace's manifest hash to the new overlay hash (so the watcher's skip path catches the echo).

### Anti-Echo Coordination

Before writing, register `(workspaceID, relPath)` in the suppress set. The watcher ignores the resulting fsnotify event.

## Lifecycle

### Daemon Startup

1. Create `Compounder` in `daemon.go`
2. For each workspace with active sessions, start watching overlay-managed files
3. Run a one-time reconciliation: for each overlay-managed file in each active workspace, check if it has diverged from the overlay and trigger the merge engine if so

### Session Spawn

After a session is spawned, if this is the workspace's first session, start the compounder watches for that workspace.

### Session Dispose

If this was the workspace's last session, stop the compounder watches (but don't reconcile — the workspace persists and will be reconciled if reused).

### Workspace Dispose

1. Run a final reconciliation: check all overlay-managed files, merge any that have changed
2. Remove watches
3. Proceed with existing disposal flow

### Workspace Reuse (prepare)

When a workspace is reused for a new spawn, the overlay is re-copied. The manifest is regenerated with fresh hashes.

## Dashboard Integration (Future)

Optional — not in initial implementation:

- Log compounding events to the daemon log (always)
- Show a small indicator on workspace cards: "synced .claude/settings.json"
- Compounding activity feed in a sidebar section

## Configuration

New optional config field in `~/.schmux/config.json`:

```json
{
  "compound": {
    "enabled": true,
    "debounce_ms": 2000,
    "llm_target": "claude-haiku"
  }
}
```

Defaults: enabled=true (if overlays exist), debounce=2000ms, llm_target falls back to nudgenik target.

## Implementation Steps

1. **Manifest tracking**: Modify `CopyOverlay` to return manifest, store on workspace state
2. **Compounder skeleton**: Create `internal/compound/` package with lifecycle management
3. **File watcher**: Implement fsnotify watches with debouncing and anti-echo
4. **Merge engine**: Implement three-path merge (skip, fast, LLM)
5. **Propagator**: Implement selective copy to sibling workspaces
6. **Integration**: Wire into daemon.go, session spawn/dispose, workspace dispose
7. **Config**: Add compound config section
8. **Tests**: Unit tests for manifest, merge logic, anti-echo; integration test with temp directories
9. **Reconciliation**: Startup and dispose-time reconciliation passes
