# Overlay Compounding

## What it does

The overlay compounding loop provides continuous bidirectional sync for overlay-managed files. When an agent modifies a file that was originally copied from `~/.schmux/overlays/<repo>/` (e.g., `.claude/settings.json` gaining new permissions), the change is merged back to the overlay and propagated to all other active workspaces for the same repo.

## Key files

| File                            | Purpose                                                                                                       |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `internal/compound/compound.go` | `Compounder` struct: lifecycle, orchestration of watch/merge/propagate, reconciliation                        |
| `internal/compound/watcher.go`  | `Watcher` struct: fsnotify setup, per-file debouncing, anti-echo suppression, pending dir tracking            |
| `internal/compound/merge.go`    | Merge engine: three-path decision (skip/fast-path/LLM), `BuildMergePrompt()`, JSONL line-union, atomic writes |
| `internal/compound/manifest.go` | `FileHash()`, `HashBytes()`, `IsBinary()`, `ValidateRelPath()`                                                |

## Architecture decisions

- **Three merge paths, checked in order.** (1) Skip: workspace file hash matches manifest hash (unchanged). (2) Fast path: overlay hash matches manifest hash (overlay unchanged since copy, workspace is strictly newer -- direct copy, no LLM). (3) LLM merge: both overlay and workspace diverged from the base. This avoids LLM calls for the common case where only one side changed.
- **Hash-based, not content-based diffing.** The manifest stores only SHA-256 hashes, not full file content. For LLM merge, only the two current versions are sent (overlay + workspace) with instructions to union them. Sufficient for config files where the merge semantic is "keep everything from both."
- **JSONL files get line-level union without LLM.** `mergeJSONLLines()` deduplicates by exact line content, preserving overlay order then appending workspace-only lines. This handles event files efficiently but treats `{"a":1,"b":2}` and `{"b":2,"a":1}` as different lines.
- **Anti-echo suppression prevents infinite loops.** When the propagator writes to a workspace, it registers the file in a per-workspace suppress map with a TTL (default 5s). The watcher ignores fsnotify events for suppressed files.
- **Atomic writes via temp file + rename.** `atomicWriteFile()` prevents partial reads if the watcher fires during a write.
- **Declared paths for files that do not exist yet.** `AddWorkspaceWithDeclaredPaths()` tracks directories that are missing as "pending" and watches the workspace root to detect their creation. When a pending directory appears, `retryPendingDirs()` installs the watch and scans for files that were created before the watch was established (closing the race).

## Gotchas

- The `Compounder` receives callback functions (`PropagateFunc`, `ManifestUpdateFunc`) rather than direct references to the workspace or state managers. Wiring happens in `daemon.go`.
- The LLM executor has the same signature as NudgeNik's oneshot function. If no LLM target is configured, the merge falls back to last-write-wins.
- Binary files (null byte in first 8KB) and large files (>100KB) always use last-write-wins, skipping the LLM.
- File deletions are intentionally not propagated. The watcher only handles `Write` and `Create` events. Deleted overlay files remain in other workspaces until manually removed.
- The `ValidateRelPath()` check rejects empty paths, absolute paths, and `..` traversal before any file I/O occurs.
- Debounce timers are per-file (keyed `workspaceID:relPath`), not global. A burst of changes to the same file resets the timer; changes to different files debounce independently.
- Suppression entries are swept every 30 seconds by a background ticker in the watcher event loop.
- When the last session on a workspace is disposed, watches are removed. On workspace dispose, a final reconciliation runs before teardown.

## Common modification patterns

- **To change the merge prompt:** Edit `BuildMergePrompt()` in `internal/compound/merge.go`.
- **To add a new merge path (e.g., JSON-aware structural merge):** Add a case in `executeLLMMerge()` in `internal/compound/merge.go`, checking file extension before the LLM call.
- **To change debounce or suppression timing:** Pass different values to `NewCompounder()` and `NewWatcher()` in `daemon.go`. The config fields are `compound.debounce_ms` and the suppression TTL constant.
- **To watch additional files beyond the overlay manifest:** Include them in the `declaredPaths` parameter when calling `Compounder.AddWorkspace()`.
- **To add dashboard indicators for compounding activity:** The `Compounder` already logs sync and propagation events. Hook into these log points or add a callback to the `Compounder` struct.
