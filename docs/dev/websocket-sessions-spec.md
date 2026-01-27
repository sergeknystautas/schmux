# WebSocket Dashboard Spec

Replace polling of `/api/sessions` with a WebSocket at `/ws/dashboard` that pushes full state on every change.

## Goals

- Real-time updates for git status, session running state, and all workspace/session data
- Eliminate 5-second polling lag
- Simpler client code (no polling interval management)
- Connection state replaces `/api/healthz` polling for connectivity indicator
- Extensible protocol to support future message types (e.g., config updates)

## Protocol

### Endpoint

`/ws/dashboard`

### Message Format

Server sends typed messages on connect and on every change:

**Sessions message** (type: "sessions"):
```json
{
  "type": "sessions",
  "workspaces": [
    {
      "id": "schmux-001",
      "repo": "git@github.com:user/repo.git",
      "branch": "main",
      "short_repo": "repo",
      "path": "/Users/user/.schmux/workspaces/schmux-001",
      "session_count": 2,
      "sessions": [
        {
          "id": "session-abc123",
          "target": "claude",
          "nickname": "",
          "running": true,
          "created_at": "2024-01-15T10:30:00Z",
          "last_output_at": "2024-01-15T10:35:00Z",
          "nudge_state": "",
          "nudge_summary": ""
        }
      ],
      "git_ahead": 3,
      "git_behind": 0,
      "git_dirty": true,
      "git_lines_added": 42,
      "git_lines_removed": 10,
      "git_files_changed": 5
    }
  ]
}
```

**Future: Config message** (type: "config"):
```json
{
  "type": "config",
  "config": { ... }
}
```

The `workspaces` payload is the same shape as the current `/api/sessions` response.

### Client Behavior

1. Connect to `/ws/dashboard`
2. On message: check `type` field and dispatch to appropriate handler
3. For `type: "sessions"`: replace entire workspaces state
4. On close/error: show disconnected indicator, attempt reconnect with backoff
5. No polling - WebSocket connection state IS the health indicator

### No Client-to-Server Messages

Client does not send messages. This is a one-way push channel.

## Server Implementation

### Broadcaster

Add to `dashboard.Server`:

```go
type Server struct {
    // ... existing fields ...

    // Sessions WebSocket connections
    sessionsConns   map[*websocket.Conn]bool
    sessionsConnsMu sync.RWMutex
}

func (s *Server) RegisterSessionsConn(conn *websocket.Conn)
func (s *Server) UnregisterSessionsConn(conn *websocket.Conn)
func (s *Server) BroadcastSessions()
```

`BroadcastSessions()` builds the same response as `handleListSessions()` and sends to all connected clients.

### Broadcast Triggers

Call `BroadcastSessions()` after:

1. **Workspace status goroutine** (existing git status loop in `daemon.go`)
   - After each `UpdateAllGitStatus()` pass completes
   - This already runs every `git_status_poll_interval_ms` (default 30s)
   - Also check `IsRunning()` for all sessions in this pass

2. **Session spawn** (`handleSpawnPost`)
   - After successful spawn

3. **Session dispose** (`handleDispose`)
   - After successful dispose

4. **Workspace dispose** (`handleDisposeWorkspace`)
   - After successful dispose

5. **Nickname update** (`handleUpdateNickname`)
   - After successful update

6. **Nudge update** (WebSocket input handler clears nudge)
   - After nudge cleared

7. **Log mtime changes** (existing daemon goroutine)
   - Throttle to every 5-10 seconds, not on every mtime change

### Debouncing

Multiple triggers in quick succession should not spam clients. Add a simple debounce:

- Track `lastBroadcast time.Time`
- If < 500ms since last broadcast, skip (or queue one pending broadcast)
- Ensures clients don't get overwhelmed during rapid changes (e.g., disposing multiple sessions)

### WebSocket Handler

```go
func (s *Server) handleSessionsWebSocket(w http.ResponseWriter, r *http.Request) {
    // Authenticate (reuse existing auth check)
    if !s.authenticateRequest(r) {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }

    // Upgrade connection
    conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        return
    }
    defer conn.Close()

    // Register
    s.RegisterSessionsConn(conn)
    defer s.UnregisterSessionsConn(conn)

    // Send initial full state
    s.sendSessionsState(conn)

    // Keep connection alive, handle pings
    for {
        _, _, err := conn.ReadMessage()
        if err != nil {
            break
        }
    }
}
```

### Passing Broadcaster to Daemon

The workspace status goroutine lives in `daemon.go` but needs to trigger broadcasts on the dashboard server. Options:

**Option A**: Pass a callback function to the workspace manager
```go
wm.SetOnStatusChange(func() {
    server.BroadcastSessions()
})
```

**Option B**: Use a channel that the server listens on
```go
statusChangeChan := make(chan struct{}, 1)
// daemon goroutine sends to channel
// server goroutine reads and broadcasts
```

**Option C**: Move the status goroutine into the dashboard server

Recommend **Option A** - minimal change, clear intent.

## Client Implementation

### New Hook: `useSessionsWebSocket`

```typescript
// hooks/useSessionsWebSocket.ts

export function useSessionsWebSocket() {
  const [workspaces, setWorkspaces] = useState<WorkspaceResponse[]>([]);
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    const ws = new WebSocket(`ws://${location.host}/ws/sessions`);

    ws.onopen = () => setConnected(true);
    ws.onclose = () => setConnected(false);
    ws.onerror = () => setConnected(false);

    ws.onmessage = (event) => {
      const data = JSON.parse(event.data);
      setWorkspaces(data.workspaces);
    };

    return () => ws.close();
  }, []);

  return { workspaces, connected };
}
```

### Update SessionsContext

Replace polling with WebSocket:

```typescript
// Before: useEffect with setInterval polling /api/sessions
// After: useSessionsWebSocket hook

export function SessionsProvider({ children }: { children: React.ReactNode }) {
  const { workspaces, connected } = useSessionsWebSocket();

  // ... rest of context
}
```

### Remove useConnectionMonitor

The `connected` state from the WebSocket replaces the `/api/healthz` polling. The connection indicator shows WebSocket connection state instead.

### Keep /api/healthz for Initial Load

Keep the endpoint for:
- Version info on initial page load
- CLI health checks
- External monitoring

But stop polling it from the dashboard.

## Migration Path

1. **Phase 1**: Add `/ws/sessions` endpoint alongside existing polling
   - Client can use either
   - Verify WebSocket works correctly

2. **Phase 2**: Switch client to WebSocket
   - Remove polling code
   - Update connection indicator

3. **Phase 3**: Clean up
   - Remove any polling-only code paths
   - Update docs

## Testing

- Unit tests for broadcaster registration/unregistration
- Unit tests for debouncing
- E2E test: spawn session, verify WebSocket receives update
- E2E test: dispose session, verify WebSocket receives update
- E2E test: git status change propagates to client
- Manual test: multiple browser tabs, all receive updates

## Files to Modify

### Server

- `internal/dashboard/server.go` - Add sessions WebSocket fields, registration methods
- `internal/dashboard/websocket.go` - Add `handleSessionsWebSocket`, broadcast logic
- `internal/dashboard/handlers.go` - Extract response building to reusable function
- `internal/daemon/daemon.go` - Hook broadcast into status goroutine
- `internal/workspace/manager.go` - Add callback for status changes (Option A)

### Client

- `assets/dashboard/src/hooks/useSessionsWebSocket.ts` - New hook
- `assets/dashboard/src/contexts/SessionsContext.tsx` - Replace polling with WebSocket
- `assets/dashboard/src/hooks/useConnectionMonitor.ts` - Remove or repurpose
- `assets/dashboard/src/components/ConnectionIndicator.tsx` - Use WebSocket state

## Open Questions

1. **Reconnection backoff** - Exponential backoff with max delay? Or fixed interval?

2. **Stale state on reconnect** - After reconnect, should server send full state immediately? (Yes, current design does this)

3. **Multiple tabs** - Each tab gets its own connection. Is this acceptable? (Probably fine for local use)
