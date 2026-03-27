# Visual Companion PID File Detection

**Date:** 2026-03-26
**Status:** Draft
**Branch:** feature/visual-companion-preview
**Depends on:** 2026-03-23-visual-companion-preview-design.md (implemented)

## Problem

The visual companion's server gets reparented to PID 1 via `nohup`/`disown`. Terminal output detection sees the URL (`http://localhost:58437`), passes all filters (loopback, HTTP probe, not already previewed), but discards it because the port owner is not in the session's PID tree.

The POST API works but agents don't call it — they follow the visual companion skill's flow, which doesn't include a "register with schmux" step.

## Solution

When terminal output detection rejects a port because the owner is not in the session's PID tree, check if the owner PID matches a `.superpowers/brainstorm/*/state/server.pid` file in the workspace directory. If it does, this is a visual companion server — create the preview.

This is not a broad fallback. It only triggers when a PID file exists at the specific `.superpowers/brainstorm/` path and its contents match the actual port owner from `lookupPortOwner`.

## Change

In `handleSessionOutputChunk` (`internal/dashboard/preview_autodetect.go`), after step 4 rejects a port because the owner is not in the PID tree:

```
4. lookupPortOwner(port) → owner PID
5. If owner in session's PID tree → create preview
6. If owner NOT in tree:
   a. Scan workspace_path/.superpowers/brainstorm/*/state/server.pid
   b. Read each file, parse PID (integer, trimmed)
   c. If any file contains the owner PID → create preview
   d. Otherwise → discard
```

The workspace path is already available (`ws.Path`). The glob pattern is fixed. No config needed.

## Logging

On PID file match (follows existing `[preview]` convention):

```
previewLog.Info("created", "host", lp.Host, "port", lp.Port, "session", sess.ID, "server_pid", ownerPID, "trigger", "pid-file")
```

On PID file scan with no match (debug level):

```
previewLog.Debug("pid file scan no match", "port", lp.Port, "owner", ownerPID, "workspace", ws.Path)
```

## Test Scenarios

1. Port not in PID tree, PID file matches owner → preview created
2. Port not in PID tree, PID file exists but contains different PID → no preview
3. Port not in PID tree, no PID files exist → no preview
4. Port not in PID tree, PID file contains garbage → no preview (parse fails, skip)
5. Port in PID tree → preview created via normal path (PID file not checked)
6. Multiple brainstorm sessions with PID files, one matches → preview created

## Files Changed

- `internal/dashboard/preview_autodetect.go` — add PID file check after PID tree rejection in `handleSessionOutputChunk`
- `internal/dashboard/preview_autodetect_test.go` — tests for PID file matching
