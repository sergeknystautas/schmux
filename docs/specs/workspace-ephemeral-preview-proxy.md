# Workspace Ephemeral Preview Proxy

**Status**: Complete

## Summary

Automatically detect and proxy workspace web servers via stable per-workspace port blocks, so preview URLs persist across daemon restarts and browser storage (cookies, localStorage) survives session churn.

---

## Goals

- **Automatic port detection**: detect when a session starts a web server by monitoring terminal output and process listening ports.
- Let users open workspace web servers without manually tracking target ports.
- **Stable port allocation**: each workspace gets a fixed block of ports; preview proxy ports are deterministic and survive daemon restarts, so browser storage (cookies, localStorage) persists.
- Reuse proxy allocations for the same `(workspace, target host, target port)`.
- Support common dev servers (Vite/Next/etc.), including WebSocket upgrades.
- Clean up previews when sessions are disposed or servers stop listening.

## Non-Goals

- Embedded browser tab in dashboard.
- TLS termination/custom certificates.
- Public internet tunneling / ngrok-style exposure.
- Remote-host workspace preview support.

---

## Design Decisions

1. **Remote workspaces**: not supported; API returns explicit unsupported error.
2. **Network-access dashboard mode** (`bind_address=0.0.0.0`): preview listeners also bind to `0.0.0.0`, making proxied servers reachable externally. This is the intended behavior.
3. **Persisted mapping location**: top-level preview collection (not nested under workspace object).
4. **State schema style**: persisted fields use existing `snake_case` conventions.
5. **Lifecycle ownership**: daemon preview manager is single owner for create/reuse/health/recreate/cleanup.
6. **API routing**: introduce a dedicated workspace subrouter for preview endpoints.
7. **Create/get idempotency**: if listener exists but upstream target is down, return existing mapping with degraded status.
8. **Stable ports**: each workspace is assigned a fixed block of 10 ports on first use; `proxy_port` is deterministic and never changes for a given slot, even across daemon restarts.
9. **Loopback validation**: allow `127.0.0.1`, `::1`, `localhost`; require resolved addresses to be loopback-only.
10. **Resource limits**: enforce configurable per-workspace and global active-preview caps.
11. **Test bar**: include unit, handler, integration (HTTP+WS), and lifecycle cleanup coverage.
12. **Documentation bar**: ship with `docs/api.md`, `docs/web.md`, and architecture/operator notes.

---

## User Experience

### Primary flow (automatic detection)

1. User runs app server in workspace shell tab (e.g. Vite on `5173`).
2. Daemon detects new listening port via terminal output monitoring and/or periodic lsof/ss checks.
3. Dashboard automatically shows preview URL in workspace/session UI.
4. User clicks to open preview in external browser.

### Detection approach

1. **Terminal output scanning**: regex matches `localhost:XXXX` or `127.0.0.1:XXXX` patterns in session output.
2. **Process port verification**: use `lsof` (macOS) or `ss` (Linux) to confirm the session's PID is actually listening on detected ports.
3. **Health checking**: periodically verify upstream is still reachable; update status to `degraded` if not.
4. **Session lifecycle binding**: when session is disposed, clean up all previews associated with that session's detected ports.

### Reuse behavior

- Re-requesting preview for same workspace + target returns the same stable port.
- If proxy listener is dead, daemon rebinds to the same port and returns it.
- Browser storage persists because the port never changes for a given workspace slot.

---

## Security Model

- Preview proxy listeners follow the daemon's `bind_address`: local-only (`127.0.0.1`) in default mode, network-exposed (`0.0.0.0`) in network-access mode. This is the intended exposure mechanism.
- Proxy targets must be loopback-only (`127.0.0.1` / `::1` / `localhost`). You control what gets proxied, not who can reach the proxy.
- Daemon validates workspace ownership for all preview API operations.

---

## Backend Design

### New runtime component

Add preview proxy manager in daemon process:

- Owns active proxy listeners.
- Maintains in-memory index by `(workspaceID, targetHost, targetPort)`.
- Creates HTTP reverse proxy with WebSocket/upgrade support.
- Tracks last access timestamp for observability.

Suggested package: `internal/preview/`.

### Data model (runtime + persisted metadata)

Persist minimal metadata in state for dashboard visibility and restart recovery.

```go
// internal/state/state.go

type Workspace struct {
    // existing fields...
    PortBlock int `json:"port_block,omitempty"` // 0 = unassigned; assigned on first preview request
}

// Live listeners recreated lazily on demand; only identity/routing fields matter at rest.
type WorkspacePreview struct {
    ID            string    `json:"id"`
    WorkspaceID   string    `json:"workspace_id"`
    TargetHost    string    `json:"target_host"`
    TargetPort    int       `json:"target_port"`
    ProxyPort     int       `json:"proxy_port"`  // stable; derived from port block, never changes
    Status        string    `json:"status"`      // ready | degraded
    LastError     string    `json:"last_error,omitempty"`
    CreatedAt     time.Time `json:"created_at"`
    LastUsedAt    time.Time `json:"last_used_at"`
    LastHealthyAt time.Time `json:"last_healthy_at,omitempty"`
}
```

`State.Previews` is persisted (`json:"previews,omitempty"`). On restart, the stored `ProxyPort` is used to rebind the listener to the exact same port. Listeners are recreated lazily on first request.

### Port block allocation

```
Base port:  53000  (config: network.preview_port_base)
Block size: 10     (config: network.preview_port_block_size)

Blocks are 1-indexed: PortBlock=0 means unassigned, first real block is 1.
Formula: proxyPort = basePort + (workspace.PortBlock - 1) * blockSize + slotOffset
```

The next block to assign is derived at runtime: `max(PortBlock across all workspaces) + 1`. No counter needed in state — the workspace records are the source of truth.

A workspace's `PortBlock` is assigned the first time it needs a preview and persisted immediately. Old workspaces without a block go through the same path — no special migration needed.

### API

#### Create or get preview

`POST /api/workspaces/{id}/previews`

Body:

```json
{
  "targetHost": "127.0.0.1",
  "targetPort": 5173
}
```

Response (`200`):

```json
{
  "id": "prev_abc123",
  "workspaceId": "schmux-005",
  "targetHost": "127.0.0.1",
  "targetPort": 5173,
  "proxyPort": 53000,
  "status": "ready"
}
```

Semantics:

- Idempotent by `(workspaceId, targetHost, targetPort)`.
- If mapping exists and listener is healthy, return existing mapping (`status=ready` or `degraded`).
- If mapping exists but listener is dead, recreate listener on the same stable port.
- If upstream is down but listener is healthy, do **not** recreate eagerly; return existing mapping with `status=degraded`.

#### List previews for workspace

`GET /api/workspaces/{id}/previews`

Returns known mappings and health state.

#### Delete preview mapping

`DELETE /api/workspaces/{id}/previews/{previewId}`

Stops listener and removes mapping.

### Proxy behavior

- Use `httputil.ReverseProxy` with proper `Director`/`Rewrite` for target host/port.
- Enable connection upgrades (WebSocket).
- Forward standard proxy headers (`X-Forwarded-*`).
- No path rewriting.
- Preview manager is the only owner of listener lifecycle.

### Port allocation

- Calculate desired port from the workspace's block: `basePort + (portBlock-1)*blockSize + slotOffset`.
- `slotOffset` is the lowest index not already occupied by an active preview in this workspace.
- Bind to that specific port (not `:0`).
- If the port is in use by an external process, try the next slot in the block.
- Fail with 409 only if all slots in the block are exhausted.
- Keep listener alive while mapping is active.

### Cleanup

- Remove all preview mappings when workspace is disposed.
- Also remove mappings/listeners when workspace is removed by non-dispose reconciliation paths (e.g., scanner/state sync).
- On daemon restart, listeners are recreated lazily on first request to the same stable port. The URL never changes, so the UI needs no special handling.
- Listeners are closed only when a workspace is disposed or a preview is explicitly deleted. No idle timeout — breaking a URL because nobody used it for an hour is worse than holding a socket open.

### Validation rules

- Reject non-loopback targets.
- Allowed host input set: `127.0.0.1`, `::1`, `localhost`.
- Resolve hostnames and require all resolved addresses to be loopback.
- Remote workspaces (`RemoteHostID != ""`) return unsupported error.

### Resource limits

- Add configurable caps:
  - max active previews per workspace
  - max active previews globally
- Exceeding caps returns conflict/error with actionable message.

---

## Configuration

New fields in the `network` config section:

```json
{
  "network": {
    "preview_port_base": 53000,
    "preview_port_block_size": 10
  }
}
```

Treat these as write-once: changing them after workspaces have been allocated invalidates existing port assignments.

---

## Frontend Changes

- Display detected preview URLs in workspace/session UI automatically.
- Click to open preview in external browser (`window.open(url, "_blank")`).
- Show preview status (ready/degraded) with visual indicator.
- In network-access mode, preview URLs are externally reachable — this is expected behavior, not an error.

---

## Error Handling

Common errors:

- `400`: invalid host/port, non-loopback target.
- `422`: remote workspace previews unsupported.
- `403/404`: workspace missing or unauthorized.
- `409`: port/listener conflict during recreation.
- `429/409`: preview cap exceeded.
- `502`: proxy target unreachable (server not started yet).

Dashboard behavior:

- Show actionable error toast (e.g., "No server listening on 5173 in this workspace yet").
- Keep last entered target port for quick retry.

---

## Observability

Add daemon logs/metrics:

- preview created/reused/deleted
- proxy request count by preview ID
- 4xx/5xx upstream errors
- listener start/stop failures

---

## Testing Plan

### Unit tests

- Preview manager idempotent mapping behavior.
- Loopback-only validation.
- IPv6 loopback/localhost resolution validation.
- Listener recreation when stale.
- State persistence serialization.
- Resource limit enforcement.
- Port block assignment on first preview request (including workspaces that predate the feature).
- Next block derived correctly from max across existing workspaces.
- Slot offset selection skips occupied ports within block.

### Handler tests

- Routing/method/status contract for preview endpoints.
- Create/get degraded-state behavior.
- Remote workspace rejection and network-access-mode behavior.

### Integration tests

- Start test upstream HTTP server, create preview, verify content through proxy URL.
- Verify WebSocket upgrade passthrough.
- Verify workspace dispose cleans all previews.
- Verify non-dispose workspace removal paths clean listeners.
- Verify proxy port is identical after simulated daemon restart (same block + slot → same port).
- Verify two workspaces receive non-overlapping port blocks.

### Manual verification

1. Run workspace server on `5173`.
2. Create preview via dashboard.
3. Open returned URL and confirm app loads + HMR works.
4. Repeat create request; verify same proxy port reused.
5. Restart daemon; verify preview URL still works on the same port.
6. Dispose workspace; verify preview URL no longer serves target.

---

## Rollout

1. ~~Backend preview manager + create/get endpoint.~~ ✓
2. ~~Dedicated workspace subrouter for preview endpoints.~~ ✓
3. ~~Automatic port detection from terminal output and process listening ports.~~ ✓
4. ~~Dashboard preview display with automatic updates.~~ ✓
5. ~~Health checking and degraded status handling.~~ ✓
6. ~~Stable port blocks per workspace.~~ ✓
7. ~~Add idle GC and restart-lazy-restore hardening.~~ ✓ (idle GC removed; restart handled by persisted proxy ports)

---

## Future Follow-Ups (Out of Scope)

- Embedded preview tab (iframe/webview) with persisted in-app tab state.
- Remote-host preview tunneling/proxying model.
