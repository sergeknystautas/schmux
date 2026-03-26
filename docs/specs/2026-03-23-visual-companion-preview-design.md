# Visual Companion Preview Integration

**Date:** 2026-03-23
**Status:** Approved
**Branch:** feature/visual-companion-preview

## Problem

The superpowers visual companion launches a Node.js server via `nohup ... & disown`. The server gets reparented to PID 1. Schmux's auto-detection can't create a preview for it because the server isn't in the session's PID tree.

## Solution

Two things:

1. **POST API** for explicit preview creation. Agents call it when they launch a server. This is the primary mechanism for out-of-tree servers like the visual companion.
2. **ServerPID tracking** on all previews, so reconciliation can detect when a server dies regardless of whether it's in the PID tree.

Auto-detection is unchanged in behavior — it only creates previews for ports in the session's PID tree. No fallback paths.

## Changes

### 1. Data Model

**`ServerPID int` on `WorkspacePreview`** (JSON: `server_pid`, omitempty). Tracks which OS process owns the proxied port. Populated on all previews at creation time.

**`OwnerPID int` on `ListeningPort`**. The PID-tree detection functions (`detectPortsViaLsof`, `detectPortsViaSS`) already parse the owning PID and discard it. Carry it through so auto-detected previews get ServerPID for free.

**Host preservation.** Hosts are stored as-is — `localhost`, `127.0.0.1`, `::1` are never rewritten. The proxy connects to the stored host. A server on `::1` is unreachable at `127.0.0.1`, so rewriting breaks IPv6-only servers. No loopback equivalence anywhere — `FindPreview` is exact match on host string, same as today.

### 2. Terminal Output Detection

Replace `intersectPorts` with per-port owner lookup. No fallback for out-of-tree servers.

`detectPortsFromChunk` changes from `[]int` to `[]ListeningPort` — carries the host from the parsed URL. Host is validated as loopback but preserved as-is (no rewriting). Non-loopback hosts rejected.

New flow:

1. Parse candidate host:port pairs from terminal output (loopback only, host preserved)
2. Apply existing filters (agent ports, existing previews, proxy ports)
3. HTTP probe using the as-parsed host
4. `lookupPortOwner(port)` — if owner is in session's PID tree, create preview. Otherwise discard.

`lookupPortOwner(port) (int, error)` lives in `preview` package. Uses `lsof -Pan -iTCP:PORT -sTCP:LISTEN` on macOS, `ss` on Linux. Prefers IPv4, lowest PID for ties.

**lsof parsing note:** `lsof -Pan` output has `(LISTEN)` as the last whitespace field. The NAME field (`host:port`) is second-to-last. Do not use `fields[len(fields)-1]`.

### 3. POST API

`POST /api/workspaces/{workspaceID}/previews`

```json
{
  "target_port": 52341,
  "target_host": "127.0.0.1",
  "source_session_id": "sess_abc123"
}
```

- `target_host`: optional, defaults to `127.0.0.1`. Must be loopback.
- `source_session_id`: optional. Ties preview lifecycle to session.
- Port must be 1–65535. Must be listening (`lookupPortOwner` must find a PID).
- Dedup: existing preview for same workspace + host + port (exact match) → return as-is (200). New → 201.
- Errors: 400 (bad input), 404 (workspace), 422 (port not listening), 409 (cap reached).
- Side effects: `BroadcastSessions()` on creation only. No `BroadcastPendingNavigation`.

### 4. Reconciliation

Updated `ReconcileWorkspace` flow, per preview:

1. **Session check.** If `SourceSessionID` is empty, skip to step 2. Otherwise: session gone or PID = 0 → delete.
2. **ServerPID alive.** `ServerPID > 0` and `kill -0` fails → delete.
3. **PID tree.** If `SourceSessionID` is non-empty: port in session's PID tree → keep. If not found, fall through (non-terminal).
4. **Port ownership.** `ServerPID > 0` and `lookupPortOwner(port)` matches `ServerPID` → keep. This is how POST API previews (out-of-tree) survive reconciliation.
5. All checks fail → delete.

**Batching:** Single `lsof -iTCP -sTCP:LISTEN` at start of reconciliation tick, cache results for step 4 lookups. Same lsof parsing note applies — `(LISTEN)` is last field, NAME is second-to-last.

**Migration:** Existing previews have `ServerPID = 0`. Steps 1→3→5 — identical to current behavior.

### 5. Logging

Use `logging.Sub(logger, "preview")` for the `[preview]` category. Info-level for every creation and deletion. Not optional — every creation and deletion must be logged.

**On creation** — output: `INFO [preview]: created host=localhost port=9323 ...`

```
previewLog.Info("created", "host", lp.Host, "port", lp.Port, "session", sess.ID, "server_pid", ownerPID, "trigger", "autodetect")
previewLog.Info("created", "host", host, "port", port, "session", req.SourceSessionID, "server_pid", ownerPID, "trigger", "post-api")
previewLog.Info("created", "host", lp.Host, "port", lp.Port, "session", sess.ID, "server_pid", lp.OwnerPID, "trigger", "startup-scan")
```

**On deletion** — output: `INFO [preview]: deleted id=prev_abc host=localhost port=9323 reason=server-pid-dead ...`

```
previewLog.Info("deleted", "id", p.ID, "host", p.TargetHost, "port", p.TargetPort, "reason", "server-pid-dead", "server_pid", p.ServerPID)
previewLog.Info("deleted", "id", p.ID, "host", p.TargetHost, "port", p.TargetPort, "reason", "session-gone", "session", p.SourceSessionID)
previewLog.Info("deleted", "id", p.ID, "host", p.TargetHost, "port", p.TargetPort, "reason", "port-owner-changed", "server_pid", p.ServerPID, "current_owner", currentOwner)
previewLog.Info("deleted", "id", p.ID, "host", p.TargetHost, "port", p.TargetPort, "reason", "all-checks-failed")
```

### 6. Agent Instructions

Add a "Web Preview Registration" section to the signaling instructions injected into agent session files (`SignalingInstructions` in `internal/workspace/ensure/manager.go`). Tells agents to POST when they launch a web server:

```
curl -s -X POST "http://localhost:7337/api/workspaces/$SCHMUX_WORKSPACE_ID/previews" \
  -H "Content-Type: application/json" \
  -d '{"target_port": PORT, "source_session_id": "$SCHMUX_SESSION_ID"}'
```

### 7. Visual Companion Integration

The visual companion skill (`superpowers:brainstorming`) launches a Node.js server via `start-server.sh`, which uses `nohup ... & disown`. The server gets reparented to PID 1 and is invisible to schmux's PID-tree auto-detection.

With the POST API and agent instructions in place, the flow is:

1. Agent runs `start-server.sh --project-dir ...` → gets back JSON: `{"type":"server-started","port":52341,"url":"http://localhost:52341",...}`
2. Agent reads the port from the JSON response
3. Agent calls `curl -s -X POST "http://localhost:7337/api/workspaces/$SCHMUX_WORKSPACE_ID/previews" -H "Content-Type: application/json" -d '{"target_port": 52341, "source_session_id": "$SCHMUX_SESSION_ID"}'`
4. Schmux creates the preview, the proxy appears in the dashboard
5. Reconciliation keeps it alive via ServerPID ownership (step 4 in reconciliation)
6. When the server dies (30-minute idle timeout or explicit kill), reconciliation detects ServerPID is dead and deletes the preview

No changes to the visual companion skill itself are needed — the agent instructions tell the agent what to do. The skill outputs the port, the agent registers it.

### 8. `CreateOrGet` Signature

Add `serverPID int` parameter and `created bool` return:

```
CreateOrGet(ctx, ws, targetHost, targetPort, sourceSessionID, serverPID) (WorkspacePreview, bool, error)
```

`created` distinguishes 201 vs 200 in the POST handler. `NormalizeTargetHost` validates loopback but does not rewrite — host stored as-is.

## Test Scenarios

### Terminal Output Detection

1. Port in PID tree, localhost → preview created with ServerPID
2. Port NOT in PID tree → no preview
3. Non-loopback host → no preview
4. Not listening → no preview
5. Listening but not HTTP → no preview
6. Cooldown: same workspace:port within 45s → no duplicate
7. Multiple ports in one chunk → each evaluated independently
8. Already previewed → skipped
9. Proxy port → skipped
10. Agent port → skipped
11. Host preserved: `::1` URL → stored as `::1`, proxy connects to `::1`

### POST API

12. Happy path with session → 201, ServerPID populated
13. Happy path without session → 201, workspace-scoped
14. Port not listening → 422
15. Non-loopback host → 400
16. Invalid port → 400
17. Workspace not found → 404
18. Dedup → 200, existing preview returned
19. Dedup different session → 200, original session preserved
20. Workspace cap → 409
21. Global cap → 409
22. Creation broadcasts `BroadcastSessions`, not `BroadcastPendingNavigation`
23. Dedup does not broadcast

### Reconciliation

24. ServerPID dead → delete
25. ServerPID alive, in PID tree → keep
26. ServerPID alive, out of tree, still owns port → keep
27. ServerPID alive, out of tree, different owner → delete
28. ServerPID = 0 (legacy) → existing PID tree check
29. Session dead → delete
30. Session alive, PID = 0 → delete
31. Session-less, server alive → keep
32. Session-less, server dead → delete

### Edge Cases

33. Server dies, different process takes port → PID mismatch, delete
34. Preview created via POST, server dies before first reconciliation → caught next tick
35. Startup scan populates ServerPID via OwnerPID
36. lookupPortOwner multi-listener → lowest IPv4 PID
37. Host preservation: stored as-is, proxy connects to stored host

## Files Changed

- `internal/state/state.go` — `ServerPID` on `WorkspacePreview`
- `internal/preview/manager.go` — `OwnerPID` on `ListeningPort`; `lookupPortOwner`; `CreateOrGet` signature (serverPID param, created bool return); `NormalizeTargetHost` validates but does not rewrite; reconciliation steps 2-4; `PortOwnerCache` for batch reconciliation
- `internal/dashboard/preview_autodetect.go` — `detectPortsFromChunk` returns `[]ListeningPort` with host; replace `intersectPorts` with per-port `lookupPortOwner` + PID tree check (no fallback); populate `OwnerPID` in detection functions
- `internal/dashboard/preview_reconcile.go` — batch cache per tick
- `internal/dashboard/handlers_workspace.go` — POST handler; `ServerPID` and `SourceSessionID` in response
- `internal/dashboard/server.go` — register POST route
- `internal/workspace/ensure/manager.go` — preview registration in agent instructions
- `assets/dashboard/src/lib/types.ts` — `server_pid` on preview type
- `docs/api.md` — POST endpoint
- `docs/preview.md` — updated capabilities
- Tests in `manager_test.go`, `preview_autodetect_test.go`, `handlers_workspace_test.go`
