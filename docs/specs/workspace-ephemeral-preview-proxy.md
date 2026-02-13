# Workspace Ephemeral Preview Proxy

**Status**: In Progress

## Summary

Automatically detect and proxy workspace web servers via ephemeral ports, with stable preview URLs in the dashboard.

---

## Goals

- **Automatic port detection**: detect when a session starts a web server by monitoring terminal output and process listening ports.
- Let users open workspace web servers without manually tracking target ports.
- Use daemon-assigned ephemeral ports (e.g. `127.0.0.1:51853`) that forward to workspace-local targets (e.g. `127.0.0.1:3000`).
- Reuse proxy allocations for the same `(workspace, target host, target port)`.
- Support common dev servers (Vite/Next/etc.), including WebSocket upgrades.
- Clean up previews when sessions are disposed or servers stop listening.

## Non-Goals

- Embedded browser tab in dashboard.
- TLS termination/custom certificates.
- Public/non-local exposure.
- Remote-host workspace preview support.

---

## Design Decisions

1. **Remote workspaces**: not supported; API returns explicit unsupported error.
2. **Network-access dashboard mode** (`bind_address=0.0.0.0`): preview UX disabled unless user is local to daemon host.
3. **Persisted mapping location**: top-level preview collection (not nested under workspace object).
4. **State schema style**: persisted fields use existing `snake_case` conventions.
5. **Lifecycle ownership**: daemon preview manager is single owner for create/reuse/health/recreate/cleanup.
6. **API routing**: introduce a dedicated workspace subrouter for preview endpoints.
7. **Create/get idempotency**: if listener exists but upstream target is down, return existing mapping with degraded status.
8. **Restart contract**: `preview_id` is stable identity; `proxy_port`/URL may change after restart or recreation.
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

- Re-requesting preview for same workspace + target returns the existing ephemeral port.
- If proxy is unhealthy/stale, daemon recreates it and returns a new port.

---

## Security Model

- Preview proxies must bind to `127.0.0.1` only.
- Proxy targets allowed only to loopback destinations (`127.0.0.1` / `localhost`).
- Requests are local-only and not exposed on LAN.
- Daemon validates workspace ownership for all preview API operations.

---

## Backend Design

### New runtime component

Add preview proxy manager in daemon process:

- Owns active proxy listeners.
- Maintains in-memory index by `(workspaceID, targetHost, targetPort)`.
- Creates HTTP reverse proxy with WebSocket/upgrade support.
- Tracks last access timestamp for idle cleanup.

Suggested package: `internal/preview/`.

### Data model (runtime + persisted metadata)

Persist minimal metadata in state for dashboard visibility and restart recovery intent.

```go
// internal/state/state.go
// Metadata only; live listeners recreated lazily on demand.
type WorkspacePreview struct {
    ID            string    `json:"id"`
    WorkspaceID   string    `json:"workspace_id"`
    TargetHost    string    `json:"target_host"` // default loopback
    TargetPort    int       `json:"target_port"`
    ProxyPort     int       `json:"proxy_port"`  // assigned ephemeral local port
    Status        string    `json:"status"`      // ready | degraded
    LastError     string    `json:"last_error,omitempty"`
    CreatedAt     time.Time `json:"created_at"`
    LastUsedAt    time.Time `json:"last_used_at"`
    LastHealthyAt time.Time `json:"last_healthy_at,omitempty"`
}
```

Persist previews in a top-level collection (avoid nested workspace slices):

```go
type State struct {
    // existing fields...
    Previews map[string]WorkspacePreview `json:"previews,omitempty"` // keyed by preview ID
}
```

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
  "proxyPort": 51853,
  "url": "http://127.0.0.1:51853",
  "status": "ready",
  "stableIdentity": "preview_id"
}
```

Semantics:

- Idempotent by `(workspaceId, targetHost, targetPort)`.
- If mapping exists and listener is healthy, return existing mapping (`status=ready` or `degraded`).
- If mapping exists but listener is dead, recreate listener (may change `proxyPort`).
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

- Use OS ephemeral allocation (`net.Listen("tcp", "127.0.0.1:0")`).
- Record assigned port from listener address.
- Keep listener alive while mapping is active.

### Cleanup

- Remove all preview mappings when workspace is disposed.
- Also remove mappings/listeners when workspace is removed by non-dispose reconciliation paths (e.g., scanner/state sync).
- Optional idle GC: close listeners not used for configurable duration (e.g. 60 min).
- On daemon restart, mappings may be restored lazily (rebind on first request) to avoid startup failures.
- UI must treat persisted URLs as stale-prone and fetch fresh mapping before open.

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

## Frontend Changes

- Display detected preview URLs in workspace/session UI automatically.
- Click to open preview in external browser (`window.open(url, "_blank")`).
- Show preview status (ready/degraded) with visual indicator.
- In network-access mode, show explicit message when preview open is unavailable for remote clients.

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

### Handler tests

- Routing/method/status contract for preview endpoints.
- Create/get degraded-state behavior.
- Remote workspace rejection and network-access-mode behavior.

### Integration tests

- Start test upstream HTTP server, create preview, verify content through proxy URL.
- Verify WebSocket upgrade passthrough.
- Verify workspace dispose cleans all previews.
- Verify non-dispose workspace removal paths clean listeners.

### Manual verification

1. Run workspace server on `5173`.
2. Create preview via dashboard.
3. Open returned URL and confirm app loads + HMR works.
4. Repeat create request; verify same proxy reused.
5. Dispose workspace; verify preview URL no longer serves target.

---

## Rollout

1. ~~Backend preview manager + create/get endpoint.~~ ✓
2. ~~Dedicated workspace subrouter for preview endpoints.~~ ✓
3. ~~Automatic port detection from terminal output and process listening ports.~~ ✓
4. Dashboard preview display with automatic updates.
5. Health checking and degraded status handling.
6. Add idle GC and restart-lazy-restore hardening.

---

## Future Follow-Ups (Out of Scope)

- Embedded preview tab (iframe/webview) with persisted in-app tab state.
- Remote-host preview tunneling/proxying model.
