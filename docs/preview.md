# Preview Proxy

## What it does

Detects dev servers launched by agent sessions, creates reverse-proxy listeners on stable ports, and cleans up proxies when the originating session dies. Previews are workspace-scoped, session-owned, and persisted across daemon restarts.

## Key files

| File                                           | Purpose                                                                                                                                                      |
| ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `internal/preview/manager.go`                  | Core preview lifecycle: `CreateOrGet`, `Delete`, `DeleteBySession`, `DeleteWorkspace`, `ReconcileWorkspace`, stable port allocation, reverse proxy setup     |
| `internal/preview/manager_test.go`             | Unit tests for caps, port allocation, session cleanup, reconcile                                                                                             |
| `internal/dashboard/preview_autodetect.go`     | `scanExistingSessionsForPreviews` (startup scan), `handleSessionOutputChunk` (terminal URL detection), `detectListeningPortsByPID` (PID-tree port ownership) |
| `internal/dashboard/preview_reconcile.go`      | 5-second reconcile loop calling `ReconcileWorkspace` per local workspace                                                                                     |
| `internal/dashboard/handlers_dispose.go`       | `handleDispose` calls `DeleteBySession` on session disposal                                                                                                  |
| `internal/dashboard/handlers_workspace.go`     | `handlePreviewsList` (GET), `handlePreviewsCreate` (POST), `handlePreviewsDelete` (DELETE)                                                                   |
| `internal/dashboard/server.go`                 | Route registration: `/api/workspaces/{workspaceID}/previews` and `/api/workspaces/{workspaceID}/previews/{previewID}`                                        |
| `internal/state/state.go`                      | `WorkspacePreview` struct with `SourceSessionID`, `ProxyPort`, `PortBlock` on workspace                                                                      |
| `assets/dashboard/src/lib/previewKeepAlive.ts` | Iframe parking lot: LRU cache of up to 10 iframes, show/hide/refresh/back operations                                                                         |
| `assets/dashboard/src/routes/PreviewPage.tsx`  | Route `/preview/:workspaceId/:previewId` — preview iframe container                                                                                          |

## Architecture decisions

- **Session affinity via `SourceSessionID` instead of workspace-only scoping.** Every preview records which session created it. When a session is disposed, all its previews are deleted immediately via `DeleteBySession` -- no health check, no grace period. This prevents stranded previews that outlive their originating session.
- **ServerPID tracking on all previews.** Every preview records the PID of the process that owns the proxied port (`ServerPID`). Reconciliation uses this for fast liveness checks (`kill -0`) and port ownership verification (`lookupPortOwner`). For PID-tree-detected previews, ServerPID comes from `ListeningPort.OwnerPID`. For POST API previews, it comes from `lookupPortOwner` at creation time.
- **5-step reconciliation.** Per preview: (1) session check, (2) ServerPID alive check, (3) PID tree check (non-terminal), (4) port ownership check (keeps POST API previews alive), (5) delete. Steps are ordered by cost. A batch `lsof` cache is built once per tick for step 4 lookups.
- **Stable port allocation via per-workspace port blocks.** Each workspace gets a block of ports (default: 10 ports starting at base 53000). `PortBlock` is persisted on the workspace, so previews get the same proxy port across daemon restarts. Port assignment picks the lowest free slot in the block, skipping ports occupied by external processes.
- **POST endpoint for explicit preview creation.** `POST /api/workspaces/{id}/previews` lets agents register out-of-PID-tree servers (e.g., the visual companion launched via `nohup`/`disown`). The endpoint verifies the port is listening, looks up the owner PID, and creates a preview with `ServerPID` tracking. Agent instructions injected into session files tell agents to call this when they launch a web server.
- **Auto-detection has two triggers.** `scanExistingSessionsForPreviews` runs at daemon startup to pick up dev servers that started while the daemon was down. `handleSessionOutputChunk` fires on every terminal output chunk, regex-matches `http(s)://` URLs, does a per-port `lookupPortOwner` to verify the port is in the session's PID tree, and creates previews for verified ports. Only loopback hosts are accepted; non-loopback URLs are discarded. A 45-second cooldown prevents repeated creation attempts for the same workspace:port. No fallback for out-of-PID-tree servers — those use the POST API.
- **Daemon restart handled by the reconcile loop, not a separate startup path.** On restart, persisted previews have `SourceSessionID` and stable `ProxyPort`. The first reconcile tick (+5s) checks each preview's source session PID. If the PID still owns the port, `ensureListener` recreates the proxy. If not, the preview is deleted. `scanExistingSessionsForPreviews` handles net-new detection for sessions that started servers while the daemon was down.
- **Target host restricted to loopback, preserved as-is.** `NormalizeTargetHost` only allows `127.0.0.1`, `::1`, and `localhost` — but does not rewrite them. The stored host is what the proxy connects to. This prevents IPv6-only servers from breaking when the host is rewritten to `127.0.0.1`. The `networkAccess` config flag controls whether the proxy listener binds to `0.0.0.0` (for remote access) or `127.0.0.1`.
- **Sensitive headers stripped before forwarding.** The reverse proxy's custom `Director` removes `Cookie`, `Authorization`, and `X-CSRF-Token` headers before forwarding to the upstream dev server. Without this, schmux session cookies would leak to the proxied application.
- **Iframe parking lot for instant preview switching.** The frontend keeps up to 10 iframes alive in a hidden parking lot div. Navigating between previews moves iframes in and out of the visible area without reloading them. LRU eviction removes the oldest iframe when the cap is reached.

## Gotchas

- **TOCTOU race in port allocation.** `isPortFree` does a probe bind, but the port can be claimed between the check and the actual `net.Listen` in `ensureListener`. The code handles bind failures gracefully -- this check only skips obviously-occupied ports during allocation.
- **Reconcile runs every 5 seconds for all local workspaces.** If a workspace has no previews, `ReconcileWorkspace` returns immediately. But with many workspaces, the `lsof`/`ss` calls for PID-tree detection add up. Each call has a 750ms timeout.
- **`touch()` debounces state writes.** `LastUsedAt` is updated at most every 30 seconds to avoid write amplification on every proxied request. The actual update is persisted on the next state change (e.g., reconcile or preview creation), not immediately.
- **IPv4 preference over IPv6.** When both `127.0.0.1` and `::1` are detected for the same port, the code prefers IPv4. This is consistent across `detectPortsViaSS`, `detectPortsViaLsof`, and `lookupPortOwner`.
- **Cap enforcement holds the mutex.** `CreateOrGet` holds `m.mu` across the cap check, port pick, and state upsert to prevent TOCTOU races where concurrent calls could pick the same port slot or both pass the cap check. The lock is released before `ensureListener` to avoid holding it during `net.Listen`.
- **Proxy port block is 1-indexed.** Block 1 maps to ports `portBase + 0..blockSize-1`, block 2 maps to `portBase + blockSize..2*blockSize-1`, etc. The block number is stored on the workspace, not the preview.
- **TLS support is opt-in.** If `tlsEnabled` is set, `ensureListener` calls `server.ServeTLS` instead of `server.Serve`. The cert and key paths must be configured. This is for environments that require HTTPS on all local ports.
- **Remote workspace previews are not supported.** `CreateOrGet` returns `ErrRemoteUnsupported` if the workspace has a `RemoteHostID`. Port detection relies on local `lsof`/`ss` commands that cannot inspect remote processes.

## Common modification patterns

- **Add a new field to preview state:** Edit `WorkspacePreview` in `internal/state/state.go`, populate it in `CreateOrGet` or the auto-detect code in `internal/dashboard/preview_autodetect.go`, and consume it in the frontend via the WebSocket broadcast payload (previews are included in the sessions broadcast).
- **Change the reconcile interval:** Edit the `time.NewTicker` duration in `internal/dashboard/preview_reconcile.go` (currently 5 seconds).
- **Change the auto-detect cooldown:** Edit `previewAutoDetectCooldown` in `internal/dashboard/preview_autodetect.go` (currently 45 seconds).
- **Change max previews per workspace or globally:** Pass different values to `preview.NewManager` in `internal/dashboard/server.go`. Defaults are 3 per workspace, 20 global.
- **Support remote workspace previews:** Implement SSH-tunneled port forwarding in `CreateOrGet` (when `ws.RemoteHostID != ""`), replace local `detectListeningPortsByPID` with a remote command, and update `ReconcileWorkspace` to check remote PID ownership.
- **Add a new preview API endpoint:** Register the route under `/api/workspaces/{workspaceID}/previews` in `internal/dashboard/server.go`, implement the handler in `internal/dashboard/handlers_workspace.go`, and call the appropriate `preview.Manager` method.
- **Change the port block size or base port:** Pass different `portBase` and `blockSize` values to `preview.NewManager`. Existing workspaces keep their assigned `PortBlock` number; only new workspaces pick up the changed range.
