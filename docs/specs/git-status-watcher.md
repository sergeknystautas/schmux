# Git Status Watcher Spec

## Context

Git status updates are currently driven by a polling loop in the daemon. We want git status to update immediately when git changes occur, without relying on user-visible wrappers or manual refresh. This spec adds file system watchers on `.git` metadata and uses those events to trigger git status refreshes and WebSocket broadcasts.

This spec complements the WebSocket dashboard spec in `docs/dev/websocket-sessions-spec.md` by providing better triggers for `BroadcastSessions()`.

## Goals

- Update workspace git status as soon as git metadata changes (commit, checkout, merge, reset, rebase, etc.).
- Work on macOS and Linux.
- Be invisible to users (no PATH wrappers or hooks required).
- Reduce unnecessary polling while preserving a slow fallback loop.

## Non-goals

- Detect arbitrary working tree edits that do not touch git metadata.
- Replace the WebSocket protocol or client payload shape.
- Provide per-command attribution ("git commit" vs "git checkout").

## Design

### Overview

A `GitWatcher` component watches each workspace's git metadata directory using `fsnotify`. When changes are observed, a debounced status refresh runs for that workspace and broadcasts to WebSocket clients.

The existing poller remains as a slow fallback (10s interval). Every refresh (watcher or poller) is a full check including `git fetch`. There is no local-only status path and no quiet gating.

### Watch Targets

Watch the resolved git directory for each workspace, plus `refs/` and `logs/` subtrees.

Resolved git directory rules:

1. If `<workspace>/.git` is a directory, use that as the git dir.
2. If `<workspace>/.git` is a file containing `gitdir: <path>`, resolve `<path>` relative to the workspace and use that.

Paths watched (within the git dir):

- The git dir itself (catches HEAD, index, packed-refs, FETCH_HEAD changes)
- `refs/` (entire tree, recursively)
- `logs/` (entire tree, recursively)

For worktrees, also watches the shared worktree base's `refs/` directory.

### Worktree Handling

A worktree's `.git` is a file pointing to `<base-repo>/worktrees/<name>/`. The watcher watches both:

- The worktree-specific gitdir (HEAD, index)
- The shared worktree base's `refs/` directory

Multiple workspaces can map to the same worktree base path. The `watchedPaths` map is `map[string][]string` (path → list of workspace IDs) so one worktree base event triggers debounce for all workspaces sharing it.

### New Directories

`fsnotify` doesn't auto-watch new subdirectories. On CREATE events for directories under a watched path, the watcher adds a watch for the new dir (handles `git fetch` creating new remote tracking branches).

### Event Handling

- Any fs event for watched paths triggers a per-workspace debounce timer.
- Debounce window: 1000ms (configurable via `git_status_watch_debounce_ms`).
- After the debounce window expires, run a full git status refresh and broadcast.

### Concurrency

Both the watcher and poller can call `UpdateGitStatus` concurrently for the same workspace. This is safe — both compute the same git status, last writer wins. No per-workspace mutex needed.

### Lifecycle

- Start watcher after workspace directory initialization and server creation.
- Add watches for all existing workspaces at daemon startup.
- On workspace create/dispose, add/remove watches via `Manager.SetGitWatcher()`.
- Stop watcher on daemon shutdown.

### Fallback and Resilience

- If the watcher fails to start for a workspace, log a warning and continue with polling only.
- If `NewGitWatcher` fails entirely, the poller provides full coverage at 10s intervals.

## Config

Optional config keys (defaults shown):

```json
"sessions": {
  "git_status_watch_enabled": true,
  "git_status_watch_debounce_ms": 1000,
  "git_status_poll_interval_ms": 10000,
  "git_status_timeout_ms": 30000
}
```

- `git_status_watch_enabled` allows opt-out for debugging or environments without fs events.
- `git_status_poll_interval_ms` is the slow fallback interval (was 2s, now 10s).
- `git_status_watch_debounce_ms` controls how long to wait after the last fs event before refreshing.

## Testing

Unit tests (`git_watcher_test.go`):

- `TestResolveGitDir_RegularClone` — `.git/` directory case
- `TestResolveGitDir_Worktree` — `.git` file with gitdir pointer
- `TestResolveGitDir_WorktreeRelativePath` — relative gitdir path
- `TestResolveSharedBaseRefs` — shared worktree base refs resolution
- `TestWatcherDisabledByConfig` — returns nil when disabled
- `TestWatcherEnabledByDefault` — enabled by default
- `TestDebounceCollapse` — multiple rapid events use debounce
- `TestAddRemoveWorkspace` — watch paths added/removed correctly
- `TestNewDirsWatched` — new subdirs under refs/ get watched on CREATE events

Manual tests:

- Start daemon, spawn a session, run `git commit` in the workspace, verify dashboard updates within ~1s
- Disable watcher via config, verify 10s polling still works
