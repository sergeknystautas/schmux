# Config WebSocket Streaming Spec

## Overview

Add config to the existing `/ws/dashboard` WebSocket payload. Currently it only sends sessions/workspaces. Add full `ConfigResponse` (same as GET `/api/config`) to the message so the frontend gets both sessions and config in one message, without polling `/api/config`.

## Current State

**`/ws/dashboard` at `internal/dashboard/server.go:204`:**

- Handler: `handleDashboardWebSocket` (lines 516-572)
- Registry: `sessionsConns map[*wsConn]bool` (line 89)
- Broadcast: `BroadcastSessions()` with 500ms debounce (lines 474-513)
- Current message format:
  ```json
  {
    "type": "sessions",
    "workspaces": [...]  // from buildSessionsResponse()
  }
  ```

**Triggers that call `BroadcastSessions()`:**

- Session spawn/dispose
- Session nudge updates
- Config updates (handlers.go calls it after save)

## Implementation

### 1. Add Config to Broadcast Payload

**In `internal/dashboard/server.go`, modify `BroadcastSessions()`:**

```go
// BroadcastSessions sends the current sessions and config state to all connected WebSocket clients.
// Debounces to avoid overwhelming clients during rapid changes.
func (s *Server) BroadcastSessions() {
    // Existing debounce logic (keep as-is)
    s.lastBroadcastMu.Lock()
    if time.Since(s.lastBroadcast) < 500*time.Millisecond {
        s.lastBroadcastMu.Unlock()
        return
    }
    s.lastBroadcast = time.Now()
    s.lastBroadcastMu.Unlock()

    // Build the sessions response
    workspaces := s.buildSessionsResponse()

    // Build the config response (same as handleConfigGet)
    cfg := s.buildConfigResponse()

    // Marshal to JSON with both sessions and config
    payload, err := json.Marshal(map[string]interface{}{
        "type":       "dashboard",
        "workspaces": workspaces,
        "config":     cfg,
    })
    if err != nil {
        fmt.Printf("[ws/dashboard] failed to marshal response: %v\n", err)
        return
    }

    // Send to all connected clients (existing logic)
    s.sessionsConnsMu.RLock()
    conns := make([]*wsConn, 0, len(s.sessionsConns))
    for conn := range s.sessionsConns {
        conns = append(conns, conn)
    }
    s.sessionsConnsMu.RUnlock()

    for _, conn := range conns {
        if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
            s.UnregisterDashboardConn(conn)
            conn.Close()
        }
    }
}
```

### 2. Extract buildConfigResponse Method

**In `internal/dashboard/handlers.go`:**

Extract the config building logic from `handleConfigGet` (lines 836-951) into a method:

```go
func (s *Server) buildConfigResponse() ConfigResponse {
    // Move existing handleConfigGet logic here
    // Return ConfigResponse struct
}
```

Then `handleConfigGet` becomes:

```go
func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, s.buildConfigResponse())
}
```

### 3. Update Initial Message in handleDashboardWebSocket

**In `internal/dashboard/server.go` at line ~550:**

Change the initial message from:

```go
payload, err := json.Marshal(map[string]interface{}{
    "type":       "sessions",
    "workspaces": data,
})
```

To:

```go
payload, err := json.Marshal(map[string]interface{}{
    "type":       "dashboard",
    "workspaces": s.buildSessionsResponse(),
    "config":     s.buildConfigResponse(),
})
```

### 4. Add Broadcast Trigger for needs_restart Changes

**In `internal/state/state.go`:**

Currently `SetNeedsRestart` doesn't trigger a broadcast. Need to add `BroadcastSessions()` call after setting the flag.

**Option A:** Pass `*Server` to State (requires refactor)
**Option B:** Call `BroadcastSessions()` directly from handlers that change `needs_restart`

In `internal/dashboard/handlers.go`, after calling `st.SetNeedsRestart(true)`:

```go
st.SetNeedsRestart(true)
go s.BroadcastSessions()
```

## Files to Modify

1. **`internal/dashboard/server.go`**
   - `BroadcastSessions()` - Add config to payload
   - `handleDashboardWebSocket()` - Add config to initial message

2. **`internal/dashboard/handlers.go`**
   - Add `buildConfigResponse()` method (extract from handleConfigGet)
   - Refactor `handleConfigGet` to use `buildConfigResponse()`
   - Add `go s.BroadcastSessions()` after `SetNeedsRestart()` calls

3. **Frontend**
   - Change message type handling from `"sessions"` to `"dashboard"`
   - Read `config` from the same message

## Success Criteria

- [ ] `/ws/dashboard` message includes both `workspaces` and `config`
- [ ] Message type changes from `"sessions"` to `"dashboard"`
- [ ] Config changes trigger broadcast (already does via existing call)
- [ ] Frontend receives config in same message as sessions
- [ ] No new WebSocket endpoint - uses existing `/ws/dashboard`
