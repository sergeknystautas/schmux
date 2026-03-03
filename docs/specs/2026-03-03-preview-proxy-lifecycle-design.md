# Preview Proxy Lifecycle Design

## Problem

Preview proxies are stranded after the originating session dies. The session PID is gone but the preview tab persists in the dashboard because:

1. Previews have no session affinity — they're workspace-scoped with no record of which session created them
2. The reconcile loop only TCP-dials the upstream port. If the dev server process was orphaned (still running after session death), the check succeeds and the preview stays
3. The `entries` map check (added in 0044dd8) is a tautology — always true for running daemon, always false after restart
4. The `handlePreviewsCreate` POST endpoint exists but shouldn't — previews are internal, created only by auto-detection

### History

Previews were originally ephemeral (`json:"-"`, not persisted). Commit e5cbadf changed them to persisted (`json:"previews,omitempty"`) so stable port allocations survive daemon restarts. But no session ownership was added, creating the current gap.

## Design

### 1. Add `SourceSessionID` to `WorkspacePreview`

```go
type WorkspacePreview struct {
    // ... existing fields ...
    SourceSessionID string `json:"source_session_id,omitempty"`
}
```

Set by auto-detection code (`scanExistingSessionsForPreviews` and `handleSessionOutputChunk`) when creating previews.

### 2. Delete `handlePreviewsCreate` POST endpoint

Previews are created internally by auto-detection only. Remove the POST handler and its route registration.

### 3. Direct cleanup on session disposal

When a session is disposed, delete all previews where `SourceSessionID` matches. No health check, no grace period.

Add `DeleteBySession(sessionID string) error` to the preview manager. Call it from `handleDispose` alongside the existing reconcile call.

### 4. PID-tree verification in reconcile loop

Replace the current reconcile logic (entries map check + TCP dial) with PID-tree ownership verification:

For each persisted preview:

1. Look up the source session. If session doesn't exist → delete preview
2. Get the session's PID. If PID is dead → delete preview
3. Run `detectListeningPortsByPID(pid)` against the source session's PID
4. If the PID tree does NOT own the target port → delete preview
5. If the PID tree DOES own the target port → ensure proxy listener is running (call `ensureListener`)

This replaces both the `entries` map tautology check AND the generic `checkUpstream` TCP dial.

### 5. Daemon restart handled naturally

On restart, persisted previews have `SourceSessionID` and stable port numbers. The reconcile loop (first tick at +5s) checks each preview's source session PID:

- PID still owns the port → `ensureListener` recreates the proxy on the persisted stable port
- PID doesn't own the port / session is dead → delete the preview

No separate startup codepath needed. The `scanExistingSessionsForPreviews` startup scan continues to handle net-new detection for sessions that started ports while the daemon was down.

### 6. Remove `entries` map check from reconcile

The `m.entries[preview.ID]` check serves no purpose. Proxy listener liveness is implicitly handled: if the listener needs to exist, `ensureListener` creates it. If it shouldn't exist, the preview gets deleted.

## What stays the same

- `DeleteWorkspace` — still removes all previews for a disposed workspace
- Stable port allocation — persisted PortBlock and ProxyPort survive restarts
- `touch()` debouncing on proxied requests
- Frontend preview iframe lifecycle (driven by WebSocket broadcast)
- `scanExistingSessionsForPreviews` on startup (now sets `SourceSessionID`)
- `handleSessionOutputChunk` auto-detection (now sets `SourceSessionID`)
