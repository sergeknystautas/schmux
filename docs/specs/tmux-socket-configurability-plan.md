# Plan: tmux Socket Configurability and Live Migration

**Goal**: Make the tmux socket name user-configurable (default: `"schmux"`), with per-session socket affinity so active sessions survive socket changes via gradual drain.

**Architecture**: Config field flows to `TmuxServer` construction at daemon startup. Each session records its socket at birth. A `serverForSocket(name)` resolver on `session.Manager` constructs the correct `TmuxServer` for per-session operations. Package-level tmux functions are eliminated.

**Tech Stack**: Go (backend), TypeScript/React (frontend), Vitest (frontend tests), `go test` (backend tests), `./test.sh` (full suite)

**Design Spec**: `docs/specs/tmux-socket-configurability.md`

---

## Dependency Groups

| Group | Steps       | Can Parallelize | Notes                                                                    |
| ----- | ----------- | --------------- | ------------------------------------------------------------------------ |
| 1     | 1, 3, 4     | Yes             | Prerequisites + data model: fix restore bug, config field, state field   |
| 2     | 2, 5        | No (sequential) | Persist TmuxSocket (needs Step 4), then resolver (needs Steps 3+4)       |
| 3     | 6, 7, 8, 8b | Yes             | Call site migration: manager, floormanager, dashboard, remaining callers |
| 4     | 9, 10, 11   | Yes             | API contracts, daemon startup, session response + CLI                    |
| 5     | 12          | No              | Package-level function elimination (depends on Group 4)                  |
| 6     | 13, 14      | Yes             | Frontend (config form, mixed-socket banner) + E2E/scenario tests         |
| 7     | 15          | No              | End-to-end verification                                                  |

---

## Step 1: Fix daemon.go:749 — session restore uses wrong tmux server

**File**: `internal/daemon/daemon.go`

This is a pre-existing bug. The session restore loop calls the package-level `tmux.SessionExists()` (no `-L` flag), which checks the default tmux server instead of the `"schmux"` socket.

### 1a. Write failing test

No unit test — this is a one-line fix in wiring code tested by E2E. Verify by inspection.

### 1b. Implementation

At line 749, change:

```go
// Before:
exists := tmux.SessionExists(timeoutCtx, sess.TmuxSession)

// After:
exists := tmuxServer.SessionExists(timeoutCtx, sess.TmuxSession)
```

The `tmuxServer` variable is already in scope (constructed at line 423).

### 1c. Verify

```bash
go build ./cmd/schmux && go test ./internal/daemon/...
```

---

## Step 2: Persist TmuxSocket in Spawn and SpawnCommand

**File**: `internal/session/manager.go`

The isolation branch creates sessions on socket `"schmux"` but doesn't record the socket name in state. This must land before the configurability work, otherwise `TmuxSocket: ""` on upgrade maps to `"default"` — the wrong server.

### 2a. Implementation

Add `TmuxSocket` field to the session struct literal in `Spawn()` at line 892:

```go
sess := state.Session{
    ID:          sessionID,
    WorkspaceID: w.ID,
    Target:      opts.TargetName,
    Nickname:    uniqueNickname,
    PersonaID:   opts.PersonaID,
    TmuxSession: tmuxSession,
    CreatedAt:   time.Now(),
    Pid:         pid,
    TmuxSocket:  m.server.SocketName(), // ADD THIS
}
```

Same for `SpawnCommand()` at line 993:

```go
sess := state.Session{
    ID:          sessionID,
    WorkspaceID: w.ID,
    Target:      "command",
    Nickname:    uniqueNickname,
    TmuxSession: tmuxSession,
    CreatedAt:   time.Now(),
    Pid:         pid,
    TmuxSocket:  m.server.SocketName(), // ADD THIS
}
```

This requires the `TmuxSocket` field on `state.Session` (Step 4). If landing this as a prerequisite before the full spec, add the field first.

### 2b. Verify

```bash
go build ./cmd/schmux
```

---

## Step 3: Add `TmuxSocketName` config field and getter

**File**: `internal/config/config.go`

### 3a. Write test

Add to the config test file — test the getter default and explicit value:

```go
func TestGetTmuxSocketName(t *testing.T) {
    tests := []struct {
        name   string
        value  string
        expect string
    }{
        {"empty returns schmux", "", "schmux"},
        {"explicit value returned", "custom", "custom"},
        {"default keyword", "default", "default"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            cfg := &Config{TmuxSocketName: tt.value}
            if got := cfg.GetTmuxSocketName(); got != tt.expect {
                t.Errorf("GetTmuxSocketName() = %q, want %q", got, tt.expect)
            }
        })
    }
}
```

### 3b. Run test to verify it fails

```bash
go test ./internal/config/ -run TestGetTmuxSocketName
```

### 3c. Implementation

Add field after `TmuxBinary` at line 102:

```go
TmuxBinary                 string                      `json:"tmux_binary,omitempty"`
TmuxSocketName             string                      `json:"tmux_socket_name,omitempty"`
```

Add getter (follow `GetPort` pattern at line 2120):

```go
// GetTmuxSocketName returns the tmux socket name, defaulting to "schmux".
func (c *Config) GetTmuxSocketName() string {
    c.mu.RLock()
    defer c.mu.RUnlock()
    if c.TmuxSocketName == "" {
        return "schmux"
    }
    return c.TmuxSocketName
}
```

### 3d. Run test to verify it passes

```bash
go test ./internal/config/ -run TestGetTmuxSocketName
```

---

## Step 4: Add `TmuxSocket` field to `state.Session`

**File**: `internal/state/state.go`

### 4a. Write test

```go
func TestSessionTmuxSocketPersistence(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "state.json")

    st := New(path, nil)
    st.AddSession(Session{ID: "s1", TmuxSocket: "schmux", TmuxSession: "test"})
    if err := st.Save(); err != nil {
        t.Fatal(err)
    }

    loaded, err := Load(path, nil)
    if err != nil {
        t.Fatal(err)
    }
    sess, found := loaded.GetSession("s1")
    if !found {
        t.Fatal("session not found after reload")
    }
    if sess.TmuxSocket != "schmux" {
        t.Errorf("TmuxSocket = %q, want %q", sess.TmuxSocket, "schmux")
    }
}

func TestSessionTmuxSocketEmptyOnOldState(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "state.json")
    // Write state without TmuxSocket field
    data := []byte(`{"workspaces":[],"sessions":[{"id":"s1","workspace_id":"w1","target":"claude","tmux_session":"test","created_at":"2026-01-01T00:00:00Z","pid":123}],"repo_bases":[],"remote_hosts":[]}`)
    os.WriteFile(path, data, 0644)

    loaded, err := Load(path, nil)
    if err != nil {
        t.Fatal(err)
    }
    sess, _ := loaded.GetSession("s1")
    if sess.TmuxSocket != "" {
        t.Errorf("TmuxSocket = %q, want empty for old state", sess.TmuxSocket)
    }
}
```

### 4b. Run test to verify it fails

```bash
go test ./internal/state/ -run TestSessionTmuxSocket
```

### 4c. Implementation

Add field after `TmuxSession` at line 327:

```go
TmuxSession  string    `json:"tmux_session"`
TmuxSocket   string    `json:"tmux_socket,omitempty"`
```

### 4d. Run test to verify it passes

```bash
go test ./internal/state/ -run TestSessionTmuxSocket
```

---

## Step 5: Add `serverForSocket` resolver and `ServerForSocket` public accessor

**File**: `internal/session/manager.go`

### 5a. Write test

```go
func TestServerForSocket(t *testing.T) {
    server := tmux.NewTmuxServer("/usr/bin/tmux", "schmux", nil)
    m := New(&config.Config{}, state.New("", nil), "", nil, server, nil)

    t.Run("named socket", func(t *testing.T) {
        s := m.ServerForSocket("custom")
        if s == nil {
            t.Fatal("expected non-nil server")
        }
        if s.SocketName() != "custom" {
            t.Errorf("SocketName() = %q, want %q", s.SocketName(), "custom")
        }
        if s.Binary() != "/usr/bin/tmux" {
            t.Errorf("Binary() = %q, want %q", s.Binary(), "/usr/bin/tmux")
        }
    })

    t.Run("empty maps to default", func(t *testing.T) {
        s := m.ServerForSocket("")
        if s.SocketName() != "default" {
            t.Errorf("SocketName() = %q, want %q", s.SocketName(), "default")
        }
    })

    t.Run("nil server returns nil", func(t *testing.T) {
        m2 := New(&config.Config{}, state.New("", nil), "", nil, nil, nil)
        s := m2.ServerForSocket("anything")
        if s != nil {
            t.Error("expected nil server when manager has nil server")
        }
    })
}
```

### 5b. Run test to verify it fails

```bash
go test ./internal/session/ -run TestServerForSocket
```

### 5c. Implementation

Add after the constructor block (after line 120):

```go
// serverForSocket returns a TmuxServer targeting the given socket.
// TmuxServer is stateless (~56 bytes), so construction is free.
func (m *Manager) serverForSocket(socketName string) *tmux.TmuxServer {
    if m.server == nil {
        return nil
    }
    if socketName == "" {
        socketName = "default"
    }
    return tmux.NewTmuxServer(m.server.Binary(), socketName, m.logger)
}

// ServerForSocket is the public accessor for per-session socket resolution.
// Used by daemon.go for the session restore loop.
func (m *Manager) ServerForSocket(socketName string) *tmux.TmuxServer {
    return m.serverForSocket(socketName)
}
```

### 5d. Run test to verify it passes

```bash
go test ./internal/session/ -run TestServerForSocket
```

---

## Step 6: Migrate `session/manager.go` call sites to per-session socket

**File**: `internal/session/manager.go`

This is the largest step. Every `m.server` usage that operates on an existing session changes to `m.serverForSocket(sess.TmuxSocket)`. Spawn-time operations (which create new sessions) continue using `m.server`.

### 6a. Implementation — `IsRunning` (lines 1246-1249)

```go
// Before:
if m.server != nil {
    return m.server.SessionExists(ctx, sess.TmuxSession)
}
return tmux.SessionExists(ctx, sess.TmuxSession)

// After:
server := m.serverForSocket(sess.TmuxSocket)
if server != nil {
    return server.SessionExists(ctx, sess.TmuxSession)
}
return false
```

### 6b. Implementation — `Dispose` capture (lines 1316-1319)

```go
// Before:
if m.server != nil {
    output, err = m.server.CaptureOutput(captureCtx, sess.TmuxSession)
} else {
    output, err = tmux.CaptureOutput(captureCtx, sess.TmuxSession)
}

// After:
server := m.serverForSocket(sess.TmuxSocket)
if server != nil {
    output, err = server.CaptureOutput(captureCtx, sess.TmuxSession)
}
```

### 6c. Implementation — `Dispose` exists/kill (lines 1332-1342)

```go
// Before:
if m.server != nil {
    sessionExists = m.server.SessionExists(ctx, sess.TmuxSession)
} else {
    sessionExists = tmux.SessionExists(ctx, sess.TmuxSession)
}
// ... and kill:
if m.server != nil {
    killErr = m.server.KillSession(ctx, sess.TmuxSession)
} else {
    killErr = tmux.KillSession(ctx, sess.TmuxSession)
}

// After (use same server for both):
server := m.serverForSocket(sess.TmuxSocket)
if server != nil {
    sessionExists = server.SessionExists(ctx, sess.TmuxSession)
}
// ...
if server != nil {
    killErr = server.KillSession(ctx, sess.TmuxSession)
}
```

### 6d. Implementation — `GetAttachCommand` (lines 1445-1449)

```go
// Before:
if m.server != nil {
    return m.server.GetAttachCommand(sess.TmuxSession), nil
}
return fmt.Sprintf("tmux attach -t \"=%s\"", sess.TmuxSession), nil

// After:
server := m.serverForSocket(sess.TmuxSocket)
if server != nil {
    return server.GetAttachCommand(sess.TmuxSession), nil
}
return fmt.Sprintf("tmux attach -t \"=%s\"", sess.TmuxSession), nil
```

### 6e. Implementation — `GetOutput` (lines 1459-1462)

```go
// Before:
if m.server != nil {
    return m.server.CaptureOutput(ctx, sess.TmuxSession)
}
return tmux.CaptureOutput(ctx, sess.TmuxSession)

// After:
server := m.serverForSocket(sess.TmuxSocket)
if server != nil {
    return server.CaptureOutput(ctx, sess.TmuxSession)
}
return "", fmt.Errorf("no tmux server available")
```

### 6f. Implementation — `RenameSession` (lines 1501-1504)

```go
// Before:
if m.server != nil {
    renameErr = m.server.RenameSession(ctx, oldTmuxName, newTmuxName)
} else {
    renameErr = tmux.RenameSession(ctx, oldTmuxName, newTmuxName)
}

// After:
server := m.serverForSocket(sess.TmuxSocket)
if server != nil {
    renameErr = server.RenameSession(ctx, oldTmuxName, newTmuxName)
}
```

### 6g. Implementation — `ensureTrackerFromSession` (line 1629)

```go
// Before:
ls := NewLocalSource(sess.ID, sess.TmuxSession, m.server, m.logger)

// After:
ls := NewLocalSource(sess.ID, sess.TmuxSession, m.serverForSocket(sess.TmuxSocket), m.logger)
```

### 6h. Implementation — `GetAttachArgs` (new method)

Add after `GetAttachCommand`:

```go
// GetAttachArgs returns structured attach command components for safe exec.Command construction.
// Unlike GetAttachCommand (which returns a formatted string), this avoids shell injection.
func (m *Manager) GetAttachArgs(sessionID string) (binary, socketName, sessionName string, err error) {
    sess, found := m.state.GetSession(sessionID)
    if !found {
        return "", "", "", fmt.Errorf("session not found: %s", sessionID)
    }
    server := m.serverForSocket(sess.TmuxSocket)
    if server == nil {
        return "", "", "", fmt.Errorf("no tmux server available")
    }
    return server.Binary(), server.SocketName(), sess.TmuxSession, nil
}
```

### 6i. Verify

```bash
go build ./cmd/schmux && go test ./internal/session/...
```

---

## Step 7: Migrate `floormanager/manager.go` dual-path call sites

**File**: `internal/floormanager/manager.go`

Floor manager always uses `m.server` (the config socket). These are NOT per-session — they manage the FM's own tmux session. The migration here is removing the `if m.server != nil` / `else tmux.X()` dual paths, since `m.server` is always non-nil in production.

### 7a. Implementation

For each dual-path site (lines 114-117, 146, 207-211, 224-228, 265-271, 280-284, 424-427), replace:

```go
// Before:
if m.server != nil {
    m.server.KillSession(ctx, tmuxSess)
} else {
    tmux.KillSession(ctx, tmuxSess)
}

// After:
m.server.KillSession(ctx, tmuxSess)
```

Same pattern for `SessionExists`, `CreateSession`. The `m.server` nil guard is unnecessary — FM is always constructed with a non-nil server from daemon.go.

**Test note:** `TestRunning_NonexistentTmux` at `manager_test.go:326` constructs a bare `&Manager{}` with nil server and calls `Running()`. After removing the fallback, this will nil-pointer panic. Fix by keeping a nil guard in `Running()`: `if m.server == nil { return false }`.

### 7b. Verify

```bash
go test ./internal/floormanager/...
```

---

## Step 8: Migrate `dashboard/websocket.go` fallback call sites

**File**: `internal/dashboard/websocket.go`

### 8a. Implementation — capture fallback (lines 300-305)

The dashboard needs to resolve the server from the session's socket. Add a helper on `Server`:

```go
// serverForSession returns the TmuxServer for a session's socket.
func (s *Server) serverForSession(sess state.Session) *tmux.TmuxServer {
    return s.session.ServerForSocket(sess.TmuxSocket)
}
```

Then at line 300:

```go
// Before:
if s.tmuxServer != nil {
    bootstrap, err = s.tmuxServer.CaptureLastLines(capCtx, sess.TmuxSession, bootstrapCaptureLines, true)
} else {
    bootstrap, err = tmux.CaptureLastLines(capCtx, sess.TmuxSession, bootstrapCaptureLines, true)
}

// After:
if server := s.serverForSession(sess); server != nil {
    bootstrap, err = server.CaptureLastLines(capCtx, sess.TmuxSession, bootstrapCaptureLines, true)
}
```

### 8b. Implementation — cursor fallback (lines 343-347)

```go
// Before:
if s.tmuxServer != nil {
    cliState, cliErr = s.tmuxServer.GetCursorState(curCtx2, sess.TmuxSession)
} else {
    cliState, cliErr = tmux.GetCursorState(curCtx2, sess.TmuxSession)
}

// After:
if server := s.serverForSession(sess); server != nil {
    cliState, cliErr = server.GetCursorState(curCtx2, sess.TmuxSession)
}
```

### 8c. Implementation — CR/FM terminal bootstrap captures (lines 873-879, 982-989)

Apply same pattern. FM terminal bootstrap (line 982) should continue using `s.tmuxServer` since FM sessions are always on the config socket.

### 8d. Verify

```bash
go test ./internal/dashboard/...
```

---

## Step 8b: Migrate remaining dual-path callers (environment, debug, localsource, tests)

**Files**: `internal/dashboard/handlers_environment.go`, `internal/dashboard/handlers_debug_tmux.go`, `internal/session/localsource.go`, `internal/floormanager/injector_test.go`, `internal/session/tracker_bench_test.go`

These callers use package-level tmux functions in `else` branches. Step 12 deletes those functions, so these must be migrated first.

### 8b-a. Implementation — `handlers_environment.go`

At line 158, replace the dual-path:

```go
// Before:
if s.tmuxServer != nil {
    tmuxEnv, err = s.tmuxServer.ShowEnvironment(r.Context())
} else {
    tmuxEnv, err = tmux.ShowEnvironment(r.Context())
}

// After:
if s.tmuxServer != nil {
    tmuxEnv, err = s.tmuxServer.ShowEnvironment(r.Context())
}
```

At line 207, same pattern for `SetEnvironment`:

```go
// Before:
setEnvFn := tmux.SetEnvironment
if s.tmuxServer != nil {
    setEnvFn = s.tmuxServer.SetEnvironment
}

// After (use tmuxServer directly):
if s.tmuxServer == nil {
    http.Error(w, "tmux server not available", http.StatusServiceUnavailable)
    return
}
// ... use s.tmuxServer.SetEnvironment(r.Context(), req.Key, value) directly
```

### 8b-b. Implementation — `handlers_debug_tmux.go`

At line 56, replace the `else` branch using `tmux.Binary()`:

```go
// Before:
out, err := exec.CommandContext(ctx, tmux.Binary(), "list-sessions", ...).Output()

// After — remove the else branch entirely; s.tmuxServer is always non-nil in production:
if s.tmuxServer == nil {
    return 0, fmt.Errorf("tmux server not available")
}
```

### 8b-c. Implementation — `localsource.go`

At line 206, replace `tmux.Binary()` fallback:

```go
// Before:
if s.server != nil {
    cmd = exec.CommandContext(ctx, s.server.Binary(), "-L", s.server.SocketName(), "-C", "attach-session", "-t", "="+target)
} else {
    cmd = exec.CommandContext(ctx, tmux.Binary(), "-C", "attach-session", "-t", "="+target)
}

// After — s.server is always non-nil with per-session socket resolution:
cmd = exec.CommandContext(ctx, s.server.Binary(), "-L", s.server.SocketName(), "-C", "attach-session", "-t", "="+target)
```

### 8b-d. Implementation — test files

In `injector_test.go` and `tracker_bench_test.go`, replace package-level `tmux.CreateSession()`, `tmux.KillSession()`, `tmux.CaptureOutput()` calls with `TmuxServer` method calls. Create a test helper:

```go
testServer := tmux.NewTmuxServer("tmux", "test-schmux", nil)
testServer.CreateSession(ctx, name, dir, cmd)
// ... later:
testServer.KillSession(ctx, name)
```

### 8b-e. Verify

```bash
go build ./cmd/schmux && go test ./internal/dashboard/... ./internal/session/... ./internal/floormanager/...
```

---

## Step 9: Add `TmuxSocketName` to API contracts and config handler

**Files**: `internal/api/contracts/config.go`, `internal/dashboard/handlers_config.go`

### 9a. Implementation — contracts

In `ConfigResponse`, add after `TmuxBinary` (line 179):

```go
TmuxBinary                 string                 `json:"tmux_binary,omitempty"`
TmuxSocketName             string                 `json:"tmux_socket_name,omitempty"`
```

In `ConfigUpdateRequest`, add after `TmuxBinary` (line 320):

```go
TmuxBinary                 *string                     `json:"tmux_binary,omitempty"`
TmuxSocketName             *string                     `json:"tmux_socket_name,omitempty"`
```

### 9b. Implementation — config handler

In `handleConfigUpdate`, capture old value at line 266:

```go
oldTmuxBinary := cfg.TmuxBinary
oldTmuxSocketName := cfg.TmuxSocketName  // ADD
```

Add handler block after the `TmuxBinary` block (after line 739):

```go
if req.TmuxSocketName != nil {
    name := strings.TrimSpace(*req.TmuxSocketName)
    if name != "" && !isValidSocketName(name) {
        http.Error(w, "Invalid socket name: must contain only alphanumeric characters, hyphens, and underscores", http.StatusBadRequest)
        return
    }
    cfg.TmuxSocketName = name
}
```

Add the validation helper:

```go
// isValidSocketName checks that a socket name contains only safe characters.
func isValidSocketName(name string) bool {
    for _, c := range name {
        if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
            return false
        }
    }
    return len(name) > 0
}
```

Update `NeedsRestart` at line 756:

```go
// Before:
if !reflect.DeepEqual(oldNetwork, cfg.Network) || !reflect.DeepEqual(oldAccessControl, cfg.AccessControl) || cfg.TmuxBinary != oldTmuxBinary {

// After:
if !reflect.DeepEqual(oldNetwork, cfg.Network) || !reflect.DeepEqual(oldAccessControl, cfg.AccessControl) || cfg.TmuxBinary != oldTmuxBinary || cfg.TmuxSocketName != oldTmuxSocketName {
```

### 9c. Implementation — regenerate TypeScript types

```bash
go run ./cmd/gen-types
```

### 9d. Implementation — wire config to response builder

In `handleConfigGet` (function at `handlers_config.go:50`), the `ConfigResponse` is built as a struct literal starting around line 92. Add after the `TmuxBinary` field (approx line 152):

```go
TmuxBinary:     s.config.TmuxBinary,
TmuxSocketName: cfg.GetTmuxSocketName(),  // ADD
```

### 9e. Verify

```bash
go build ./cmd/schmux && go test ./internal/dashboard/...
```

---

## Step 10: Update daemon startup for multi-socket restore

**File**: `internal/daemon/daemon.go`

### 10a. Implementation — use config socket name (line 423)

```go
// Before:
tmuxServer := tmux.NewTmuxServer(tmuxBin, "schmux", logging.Sub(logger, "tmux"))

// After:
tmuxServer := tmux.NewTmuxServer(tmuxBin, cfg.GetTmuxSocketName(), logging.Sub(logger, "tmux"))
```

### 10b. Implementation — use config socket in Start() (line 188)

```go
// Before:
startupServer := tmux.NewTmuxServer(tmuxBinary, "schmux", nil)

// After:
socketName := "schmux"
if cfg, err := config.Load(filepath.Join(schmuxDir, "config.json")); err == nil && cfg.TmuxSocketName != "" {
    socketName = cfg.TmuxSocketName
}
startupServer := tmux.NewTmuxServer(tmuxBinary, socketName, nil)
```

Note: the config load for `TmuxBinary` already exists at line 183. Extend it:

```go
if cfg, err := config.Load(filepath.Join(schmuxDir, "config.json")); err == nil {
    if cfg.TmuxBinary != "" {
        tmuxBinary = cfg.TmuxBinary
    }
    if cfg.TmuxSocketName != "" {
        socketName = cfg.TmuxSocketName
    }
}
```

### 10c. Implementation — multi-socket startup before restore loop

Insert before line 746:

```go
// Start tmux servers for all sockets that restored sessions live on.
activeSocketSet := map[string]bool{cfg.GetTmuxSocketName(): true}
for _, sess := range st.GetSessions() {
    socket := sess.TmuxSocket
    if socket == "" {
        socket = "default"
    }
    activeSocketSet[socket] = true
}
for socket := range activeSocketSet {
    if socket == cfg.GetTmuxSocketName() {
        continue // already started above
    }
    server := tmux.NewTmuxServer(tmuxBin, socket, nil)
    if err := server.StartServer(ctx); err != nil {
        logger.Warn("failed to start tmux server for socket", "socket", socket, "err", err)
    }
}
```

### 10d. Implementation — fix restore loop (lines 747-757)

```go
// Before:
for _, sess := range st.GetSessions() {
    timeoutCtx, cancel := context.WithTimeout(d.shutdownCtx, cfg.XtermQueryTimeout())
    exists := tmux.SessionExists(timeoutCtx, sess.TmuxSession)
    cancel()
    ...
}

// After:
for _, sess := range st.GetSessions() {
    timeoutCtx, cancel := context.WithTimeout(d.shutdownCtx, cfg.XtermQueryTimeout())
    server := sm.ServerForSocket(sess.TmuxSocket)
    exists := false
    if server != nil {
        exists = server.SessionExists(timeoutCtx, sess.TmuxSession)
    }
    cancel()
    ...
}
```

### 10e. Verify

```bash
go build ./cmd/schmux && go test ./internal/daemon/...
```

---

## Step 11: Add `tmux_socket` and `tmux_session` to session API response, fix CLI attach and status

**Files**: `internal/dashboard/handlers_sessions.go`, `assets/dashboard/src/lib/types.ts`, `cmd/schmux/attach.go`, `cmd/schmux/main.go`

The CLI communicates with the daemon via HTTP. For `attach.go` to use the correct socket, the session API response must include `tmux_socket` and `tmux_session` fields. This step adds those fields and uses them in the CLI.

### 11a. Implementation — add fields to `SessionResponseItem`

Add to `SessionResponseItem` after `AttachCmd` (line 32 of `handlers_sessions.go`):

```go
AttachCmd    string `json:"attach_cmd"`
TmuxSocket  string `json:"tmux_socket,omitempty"`
TmuxSession string `json:"tmux_session,omitempty"`
```

Map them in the response builder (around line 380):

```go
TmuxSocket:  sess.TmuxSocket,
TmuxSession: sess.TmuxSession,
```

### 11b. Implementation — add fields to frontend TypeScript type

Add to `SessionResponse` in `types.ts` (after `attach_cmd` at line 11):

```typescript
attach_cmd: string;
tmux_socket?: string;
tmux_session?: string;
```

### 11c. Implementation — fix attach.go (line 65)

Use the new `tmux_socket` and `tmux_session` fields from the API response. **Do NOT use `exec.Command("sh", "-c", ...)` — shell injection risk.**

```go
// Before (line 65):
tmuxCmd := exec.Command(tmux.Binary(), "-L", "schmux", "attach", "-t", tmuxSession)

// After — use structured fields from API response:
tmuxBin := "tmux"
tmuxCmd := exec.Command(tmuxBin, "-L", sess.TmuxSocket, "attach", "-t", "="+sess.TmuxSession)
```

Update the CLI session struct (in `attach.go` or the API client) to include `TmuxSocket` and `TmuxSession` fields parsed from the JSON response.

Remove `parseTmuxSession` function (lines 73-119) — it becomes dead code.

### 11b. Implementation — main.go status (line 102)

```go
// Before:
fmt.Println("tmux socket: schmux (inspect with `tmux -L schmux ls`)")

// After — read socket from config:
socketName := "schmux"
if cfg, err := config.Load(filepath.Join(schmuxDir, "config.json")); err == nil {
    socketName = cfg.GetTmuxSocketName()
}
fmt.Printf("tmux socket: %s (inspect with `tmux -L %s ls`)\n", socketName, socketName)
```

### 11c. Verify

```bash
go build ./cmd/schmux && go test ./cmd/schmux/...
```

---

## Step 12: Delete package-level tmux functions

**File**: `internal/tmux/tmux.go`

### 12a. Implementation

Delete the following package-level symbols (lines 326-598 of the branch, exact lines in current code):

- `ShowEnvironment` (pkg-level), `SetEnvironment` (pkg-level)
- `CreateSession` (pkg-level), `SessionExists` (pkg-level), `GetPanePID` (pkg-level)
- `CaptureOutput` (pkg-level), `CaptureLastLines` (pkg-level), `KillSession` (pkg-level)
- `ListSessions` (pkg-level), `SetOption` (pkg-level), `ConfigureStatusBar` (pkg-level)
- `ResizeWindow` (pkg-level), `RenameSession` (pkg-level), `GetCursorState` (pkg-level)
- `binary` var (line 22), `pkgLogger` var (line 17), `Binary()` func (line 25)

Retain:

- `ValidateBinary()`, `StripAnsi()`, `IsPromptLine()`, `IsSeparatorLine()`, `IsChoiceLine()`, `IsAgentStatusLine()`, `ExtractLatestResponse()`, `MaxExtractedLines`, `CursorState`, `ansiRegex`

### 12b. Fix compilation errors

After deletion, `go build` will show every remaining caller of deleted functions. These should all have been migrated in Steps 6-8 and 10. If any remain, they are migration gaps — fix them by routing through `serverForSocket` or `m.server`.

### 12c. Verify

```bash
go build ./cmd/schmux && go test ./...
```

---

## Step 13: Frontend — config form, mixed-socket banner, session detail

**Files**: `assets/dashboard/src/routes/config/`, `assets/dashboard/src/routes/HomePage.tsx`, `assets/dashboard/src/components/`

### 13a. Implementation — config form socket name field

Add socket name field to the settings page config form in `useConfigForm.ts`. Follow the `tmux_binary` field pattern:

- Add `tmuxSocketName` to `ConfigSnapshot` and `ConfigFormState`
- Add input field in the config UI next to the tmux binary field
- Help text: `"schmux" = isolated server (recommended), "default" = shared with your tmux sessions`
- Note beneath: "Takes effect for new sessions. Existing sessions continue on their current socket."

### 13b. Implementation — mixed-socket banner (spec Section 8)

Add a banner component to `HomePage.tsx` that appears when sessions span multiple sockets:

```typescript
// Derive unique sockets from sessions
const activeSockets = new Set(
  sessions.filter((s) => s.running).map((s) => s.tmux_socket || 'default')
);

// Show banner only when multiple sockets are active
if (activeSockets.size > 1) {
  // Render: "Socket transition in progress — N sessions on 'X', M sessions on 'Y'"
}
```

The `tmux_socket` field is available from the session response (added in Step 11). The configured socket name comes from the config response (added in Step 9).

### 13c. Implementation — session detail socket display

In the session metadata panel, show the socket name next to PID, workspace, and agent type. Only show when the socket differs from the configured default (to avoid noise).

### 13d. Verify

```bash
go run ./cmd/build-dashboard && ./test.sh --quick
```

---

## Step 14: Update E2E and scenario test helpers

**Files**: `internal/e2e/e2e.go`, `test/scenarios/generated/helpers-terminal.ts`

### 14a. Implementation — E2E helpers

The E2E helpers hardcode `-L schmux` in 4 locations. Since the default config socket is `"schmux"`, these work without changes for now. For robustness, parameterize:

At `e2e.go:551` and `560` (`SendKeysToTmux`):

```go
// Use the socket name from the test's daemon config (always "schmux" for E2E)
socketName := "schmux"
```

At `e2e.go:895` and `1019` (`GetTmuxSessions`, artifact capture):

```go
// Same parameterization
```

### 14b. Implementation — scenario test helpers

At `helpers-terminal.ts`, parameterize the 5 hardcoded `-L schmux` sites (lines 41, 61, 194, 297, 356). Extract socket from the session's `attach_cmd` field which is already available via the API.

### 14c. Implementation — update docs/api.md

Add `tmux_socket_name` to the config section and `tmux_socket` to the session response section.

### 14d. Verify

```bash
./test.sh --quick
```

---

## Step 15: End-to-end verification

### 15a. Run full test suite

```bash
./test.sh
```

### 15b. Manual smoke test

1. `go build ./cmd/schmux && ./schmux start`
2. Open dashboard, verify Settings page shows "tmux socket name" field with value "schmux"
3. Spawn a session — verify `schmux status` shows the socket name
4. Verify `tmux -L schmux ls` shows the session
5. Change socket name to "default" in Settings — verify restart banner appears
6. Restart daemon — spawn another session
7. Verify new session is on the "default" socket (`tmux ls` shows it)
8. Verify old session (if still running) continues on "schmux" socket
9. Verify attach command works for both sessions
10. Dispose both sessions — verify correct socket is targeted for each

### 15c. Verify all tests pass

```bash
./test.sh --all
```
