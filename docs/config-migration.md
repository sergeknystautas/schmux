# Config Migration Guide (v0.5 → New Schema)

This doc explains how to migrate `~/.schmux/config.json` to the new layout.

## Summary of Changes

- `internal` is removed and split into new top-level blocks.
- Timeouts are now **milliseconds** everywhere.
- `tmux` config is now `xterm`.
- Some fields are renamed for clarity.

## New Top-Level Order

```
workspace_path
repos
run_targets
quick_launch
terminal
nudgenik
sessions
xterm
access_control
```

## Field Mapping

| Old Path | New Path | Notes |
| --- | --- | --- |
| `terminal.*` | `terminal.*` | Unchanged |
| `nudgenik.target` | `nudgenik.target` | Unchanged |
| `internal.viewed_buffer_ms` | `nudgenik.viewed_buffer_ms` | Moved |
| `internal.session_seen_interval_ms` | `nudgenik.seen_interval_ms` | Renamed + moved |
| `internal.sessions_poll_interval_ms` | `sessions.dashboard_poll_interval_ms` | Renamed + moved |
| `internal.git_status_poll_interval_ms` | `sessions.git_status_poll_interval_ms` | Moved |
| `internal.timeouts.git_clone_seconds` | `sessions.git_clone_timeout_ms` | **seconds → ms** |
| `internal.timeouts.git_status_seconds` | `sessions.git_status_timeout_ms` | **seconds → ms** |
| `internal.mtime_poll_interval_ms` | `xterm.mtime_poll_interval_ms` | Renamed block |
| `internal.timeouts.tmux_query_seconds` | `xterm.query_timeout_ms` | **seconds → ms** |
| `internal.timeouts.tmux_operation_seconds` | `xterm.operation_timeout_ms` | **seconds → ms** |
| `internal.max_log_size_mb` | `xterm.max_log_size_mb` | Renamed block |
| `internal.rotated_log_size_mb` | `xterm.rotated_log_size_mb` | Renamed block |
| `internal.network_access` | `access_control.network_access` | Renamed block |

## Unit Conversion (Seconds → Milliseconds)

If you previously had:

```
git_clone_seconds: 300
git_status_seconds: 30
tmux_query_seconds: 5
tmux_operation_seconds: 10
```

Convert to:

```
git_clone_timeout_ms: 300000
git_status_timeout_ms: 30000
query_timeout_ms: 5000
operation_timeout_ms: 10000
```

## Example Migration

### Before (old schema)

```json
{
  "workspace_path": "/Users/sergek/dev/schmux-workspaces",
  "repos": [],
  "run_targets": [],
  "quick_launch": [],
  "terminal": {
    "width": 120,
    "height": 40,
    "seed_lines": 100,
    "bootstrap_lines": 20000
  },
  "internal": {
    "mtime_poll_interval_ms": 5000,
    "sessions_poll_interval_ms": 5000,
    "viewed_buffer_ms": 5000,
    "session_seen_interval_ms": 2000,
    "git_status_poll_interval_ms": 10000,
    "max_log_size_mb": 50,
    "rotated_log_size_mb": 1,
    "timeouts": {
      "git_clone_seconds": 300,
      "git_status_seconds": 30,
      "tmux_query_seconds": 5,
      "tmux_operation_seconds": 10
    },
    "network_access": false
  }
}
```

### After (new schema)

```json
{
  "workspace_path": "/Users/sergek/dev/schmux-workspaces",
  "repos": [],
  "run_targets": [],
  "quick_launch": [],
  "terminal": {
    "width": 120,
    "height": 40,
    "seed_lines": 100,
    "bootstrap_lines": 20000
  },
  "nudgenik": {
    "target": "",
    "viewed_buffer_ms": 5000,
    "seen_interval_ms": 2000
  },
  "sessions": {
    "dashboard_poll_interval_ms": 5000,
    "git_status_poll_interval_ms": 10000,
    "git_clone_timeout_ms": 300000,
    "git_status_timeout_ms": 30000
  },
  "xterm": {
    "mtime_poll_interval_ms": 5000,
    "query_timeout_ms": 5000,
    "operation_timeout_ms": 10000,
    "max_log_size_mb": 50,
    "rotated_log_size_mb": 1
  },
  "access_control": {
    "network_access": false
  }
}
```

## Notes

- If `nudgenik.target` is empty and you only want the defaults for buffer/seen intervals, you can omit `nudgenik` entirely.
- Any missing values will fall back to defaults in code.
