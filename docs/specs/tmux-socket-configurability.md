# tmux Socket Configurability and Live Migration

**Date:** 2026-04-05
**Status:** Design
**Builds on:** `tmux-isolation-design-final.md` (the `review/wide-spectrum` branch)
**Motivated by:** Operational deployments need to choose between isolated and shared tmux servers, and transition between modes without disrupting active agents.

---

## Problem

The `review/wide-spectrum` branch hardcodes the tmux socket name to `"schmux"` and the design spec explicitly marks configurability and migration as out of scope. This creates two problems:

1. **No user choice.** Some deployments want tmux isolation (schmux owns its server, invisible to `tmux ls`). Others want the shared server (existing tooling, monitoring, muscle memory with `tmux attach`). The hardcoded socket name forces all users onto the isolated path with no opt-out.

2. **Hard-cut upgrade disruption.** When the isolation branch ships, all existing sessions on the default tmux server are orphaned. For an operational installation with 8 agents each hours into a task, "just kill them and respawn" is not acceptable. The same disruption recurs any time a user changes their socket preference.

## Prerequisites

**Fix daemon.go:749 (pre-existing bug).** The session restore loop calls `tmux.SessionExists()` (package-level, no `-L` flag), bypassing socket isolation entirely. This already breaks the isolation branch — sessions on the `"schmux"` socket are checked against the default server. Must be changed to `tmuxServer.SessionExists()` before or alongside this spec.

## Constraints

- tmux does not support moving a session between servers. A session is bound to its server at creation time. The process running inside (Claude Code, Codex) cannot be transparently relocated.
- `TmuxServer` is stateless: `{binary, socketName, logger}`. Construction is free (~56 bytes, no connections, no lifecycle). No caching or pooling needed at observed call frequencies (~1/sec worst case).
- The `ControlSource` abstraction insulates `SessionRuntime` from tmux details. Socket awareness enters the pipeline only at `LocalSource.attach()`.

## Design

### 1. Config field

Add `tmux_socket_name` to `config.json`:

```go
type Config struct {
    // ...existing fields...
    TmuxBinary     string `json:"tmux_binary,omitempty"`
    TmuxSocketName string `json:"tmux_socket_name,omitempty"` // default: "schmux"
}

func (c *Config) GetTmuxSocketName() string {
    if c.TmuxSocketName == "" {
        return "schmux"
    }
    return c.TmuxSocketName
}
```

Valid values:

- `"schmux"` (default) — isolated server, recommended
- `"default"` — shared with the user's own tmux sessions (tmux's built-in default socket name)
- Any other string — custom named socket

This is a free-form string, not an enum. tmux's `-L` flag accepts any name. An enum would force us to decide upfront which values are valid; a name lets advanced users pick `"schmux-staging"` without a code change.

### 2. Per-session socket affinity

Add one field to `state.Session`:

```go
type Session struct {
    // ...existing fields...
    TmuxSocket string `json:"tmux_socket,omitempty"`
}
```

Lifecycle:

- **At spawn:** set `TmuxSocket` to the current `cfg.GetTmuxSocketName()`.
- **At restore (daemon restart):** use the session's stored `TmuxSocket` to look up the session on the correct tmux server.
- **Empty value:** backward-compatible — treat as `"default"` (pre-isolation sessions were on the default server). This provides a free migration path from the current `main` branch.

### 3. TmuxServer resolution

Replace the single `*TmuxServer` on `session.Manager` with a resolver pattern:

```go
// serverForSocket returns a TmuxServer targeting the given socket.
// TmuxServer is stateless, so construction is free.
func (m *Manager) serverForSocket(socketName string) *tmux.TmuxServer {
    if m.server == nil {
        return nil // test path: no tmux available
    }
    if socketName == "" {
        socketName = "default"
    }
    return tmux.NewTmuxServer(m.server.Binary(), socketName, m.logger)
}
```

Constructor wiring: `serverForSocket` reads the binary path from `m.server.Binary()` (a public accessor, already exists) and uses `m.logger` (already on the Manager struct). No new fields are added to Manager — `m.server` and `m.logger` are sufficient. The nil guard preserves test compatibility: tests that pass `nil` for the server parameter in `New()` get `nil` back from `serverForSocket`, and existing nil-check patterns in tests continue to work.

The `m.server` field remains for convenience — it's the server for the _current config socket_, used for new spawns and admin queries. But per-session operations resolve their own server from the session's stored `TmuxSocket`.

### 4. Call site migration (complete enumeration)

Every `m.server` and `s.tmuxServer` usage that operates on a specific session must resolve the correct `TmuxServer` from the session's stored `TmuxSocket`. The `m.server` field is only used for new spawns and admin queries where no specific session is in scope.

#### 4a. `session/manager.go` — per-session operations

| Line      | Current code                                         | After                                                          |
| --------- | ---------------------------------------------------- | -------------------------------------------------------------- |
| 864-868   | `m.server.CreateSession(...)`                        | `m.server.CreateSession(...)` (new spawn, use config socket)   |
| 873-878   | `m.server.ConfigureStatusBar(...)`                   | Same (new spawn)                                               |
| 882-887   | `m.server.GetPanePID(...)`                           | Same (new spawn)                                               |
| 892-901   | Session struct literal                               | Add `TmuxSocket: m.server.SocketName()`                        |
| 965-987   | SpawnCommand: same three calls                       | Same pattern as Spawn                                          |
| 993-1000  | SpawnCommand session literal                         | Add `TmuxSocket: m.server.SocketName()`                        |
| 1248-1249 | `m.server.SessionExists(...)` (IsRunning)            | `m.serverForSocket(sess.TmuxSocket).SessionExists(...)`        |
| 1316-1319 | `m.server.CaptureOutput(...)` (Dispose)              | `m.serverForSocket(sess.TmuxSocket).CaptureOutput(...)`        |
| 1333-1335 | `m.server.SessionExists(...)` (Dispose)              | `m.serverForSocket(sess.TmuxSocket).SessionExists(...)`        |
| 1340-1343 | `m.server.KillSession(...)` (Dispose)                | `m.serverForSocket(sess.TmuxSocket).KillSession(...)`          |
| 1445      | `m.server.GetAttachCommand(...)`                     | `m.serverForSocket(sess.TmuxSocket).GetAttachCommand(...)`     |
| 1459      | `m.server.CaptureOutput(...)` (GetOutput)            | `m.serverForSocket(sess.TmuxSocket).CaptureOutput(...)`        |
| 1502      | `m.server.RenameSession(...)`                        | `m.serverForSocket(sess.TmuxSocket).RenameSession(...)`        |
| 1629      | `NewLocalSource(..., m.server, ...)` (ensureTracker) | `NewLocalSource(..., m.serverForSocket(sess.TmuxSocket), ...)` |

The `ensureTrackerFromSession` change at line 1629 is the most critical: without it, trackers for sessions on a non-default socket will attach to the wrong tmux server and retry forever.

#### 4b. `dashboard/websocket.go` — capture/cursor fallbacks

| Line    | Current code                         | After                                                                 |
| ------- | ------------------------------------ | --------------------------------------------------------------------- |
| 300-305 | `s.tmuxServer.CaptureLastLines(...)` | Resolve server from `sess.TmuxSocket`                                 |
| 340-352 | `s.tmuxServer.GetCursorState(...)`   | Resolve server from `sess.TmuxSocket`                                 |
| 873-879 | CR terminal bootstrap capture        | Resolve server from session                                           |
| 982-989 | FM terminal bootstrap capture        | FM sessions use `s.tmuxServer` (correct — FM always on config socket) |

The dashboard `Server` needs a method or helper to resolve the right `TmuxServer` for a given session. Options: expose `session.Manager.ServerForSocket()` publicly, or have the dashboard construct its own via the binary/logger it already holds.

#### 4c. `cmd/schmux/attach.go` — CLI attach command

Line 65 hard-codes `tmux -L schmux`, throwing away the socket from `AttachCmd`.

**Do NOT use `exec.Command("sh", "-c", sess.AttachCmd)`.** `AttachCmd` interpolates `sess.TmuxSession` (derived from user-provided nicknames) and the socket name (user-configurable free-form string) via `fmt.Sprintf`. `sanitizeNickname` only strips `.` and `:` — not shell metacharacters (`;`, `$()`, `` ` ``, `"`, `|`). Passing this through a shell interpreter enables command injection.

Fix: expose a structured method on `session.Manager`:

```go
func (m *Manager) GetAttachArgs(sessionID string) (binary, socketName, sessionName string, err error) {
    sess, found := m.state.GetSession(sessionID)
    if !found {
        return "", "", "", fmt.Errorf("session not found: %s", sessionID)
    }
    server := m.serverForSocket(sess.TmuxSocket)
    return server.Binary(), server.SocketName(), sess.TmuxSession, nil
}
```

The CLI uses this to construct a safe `exec.Command`:

```go
binary, socket, session, err := client.GetAttachArgs(sessionID)
// ...
tmuxCmd := exec.Command(binary, "-L", socket, "attach", "-t", "="+session)
```

No shell interpretation. The socket name and session name are passed as separate OS process arguments, immune to injection. `parseTmuxSession` becomes dead code and should be removed.

#### 4d. Dual-path `if m.server != nil` elimination

All `if m.server != nil { m.server.X() } else { tmux.X() }` guards are removed. With per-session socket affinity, a `TmuxServer` is always available. The nil fallback path to package-level functions is dead code.

### 5. Daemon startup

The daemon constructs the default `TmuxServer` from config and ensures the server is running:

```go
tmuxServer := tmux.NewTmuxServer(tmuxBin, cfg.GetTmuxSocketName(), logger)
_ = tmuxServer.StartServer(ctx)
```

Before restoring sessions, start tmux servers for all sockets that restored sessions live on:

```go
// Derive active sockets from stored sessions
activeSocketSet := map[string]bool{cfg.GetTmuxSocketName(): true}
for _, sess := range st.GetSessions() {
    socket := sess.TmuxSocket
    if socket == "" {
        socket = "default" // pre-isolation sessions
    }
    activeSocketSet[socket] = true
}

// Ensure all active socket servers are running
for socket := range activeSocketSet {
    server := tmux.NewTmuxServer(tmuxBin, socket, nil)
    if err := server.StartServer(ctx); err != nil {
        logger.Warn("failed to start tmux server for socket", "socket", socket, "err", err)
    }
}
```

Then restore sessions using per-session socket resolution:

```go
for _, sess := range st.GetSessions() {
    server := sm.ServerForSocket(sess.TmuxSocket)
    if !server.SessionExists(ctx, sess.TmuxSession) {
        continue
    }
    sm.EnsureTracker(sess.ID)
}
```

This fixes the existing bug in `daemon.go:749` where the package-level `tmux.SessionExists()` targets the wrong server. Without starting the old socket's server first, `SessionExists` silently fails (exit code 1, "no server running") and sessions are skipped.

### 6. Dashboard and floor manager

**Dashboard admin operations** (debug tmux list, environment page) query the configured socket by default. When sessions exist on multiple sockets (mixed-mode state), the debug endpoint should fan out across all active sockets.

The set of active sockets is derived from:

```
active_sockets = { sess.TmuxSocket for sess in state.Sessions } ∪ { cfg.GetTmuxSocketName() }
```

Fan-out note: `ListSessions` on a socket with no running server returns an error (exit code 1, "no server running"), not an empty list. Fan-out must treat this as zero sessions, not propagate the error.

**Floor manager** sessions always use the current config socket. They are ephemeral, not user-visible, and have no migration concern. The FM does not directly interact with user sessions at the tmux level — it sends keys to its own terminal via its own `ControlSource`, and reads user session state from the Go state store, not from tmux.

**Dashboard Server** receives `*TmuxServer` for the configured socket (as today). For session-specific operations (WebSocket capture fallbacks, tell handler, etc.), it resolves the correct server from the session's `TmuxSocket` via `session.Manager.ServerForSocket()`.

**Environment page** (`handlers_environment.go:155-211`): `ShowEnvironment` and `SetEnvironment` operate on the config socket only. In mixed-mode, other sockets' environments are not visible. This is acceptable: all sockets are started by the same daemon process and inherit the same parent environment. `set-environment -g` only affects one server, but new sessions on that socket pick up the change. Document this limitation in the environment page help text.

**Conflict resolution sessions** (`handlers_sync.go:415`): constructs a `LocalSource` with `s.tmuxServer` for ephemeral LLM sessions. These are infrastructure sessions, not agent sessions — they always use the config socket and have no `state.Session` with a `TmuxSocket` field. No change needed.

**WebSocket broadcast**: `TmuxSocket` must be added to `SessionResponseItem` at `handlers_sessions.go:22-48` and mapped from `sess.TmuxSocket` at line 368-392. Without this, the frontend cannot display socket info or render the mixed-socket banner. On the frontend, add `tmux_socket?: string` to the manual `SessionResponse` interface in `types.ts:1-26`.

**Config API contract**: Add `TmuxSocketName` to `ConfigResponse` and `ConfigUpdateRequest` in `internal/api/contracts/config.go`, then regenerate TypeScript types with `go run ./cmd/gen-types`.

### 7. Live migration flow

When a user changes `tmux_socket_name` from `"schmux"` to `"default"`:

```
                     Config change
                     "schmux" → "default"
                          │
          ┌───────────────┴───────────────┐
          │                               │
   Existing sessions              New sessions
   (stay on "schmux")           (created on "default")
          │                               │
          │  ┌──────────────────────┐     │
          └─>│ Drain naturally:     │     │
             │ - User disposes      │     │
             │ - Agent exits        │     │
             │ - Session errors out │     │
             └──────────────────────┘     │
                                          │
               Old socket empties    All sessions
               (no cleanup needed,   on new socket
                tmux server exits
                when last session
                disconnects)
```

No orchestrated migration. No kill-and-respawn. Sessions stay on the socket they were born on. New sessions use the new config. The old socket drains naturally as sessions end.

### 8. Dashboard UX

#### Settings page

Next to the existing `tmux_binary` field:

```
tmux socket name  [ schmux           ]
                  "schmux" = isolated server (recommended)
                  "default" = shared with your tmux sessions

  Takes effect for new sessions.
  Existing sessions continue on their current socket.
```

Free-text input with help text. No dropdown — the value space is open-ended.

#### Mixed-socket banner

When sessions exist on more than one socket, the home page shows:

```
  Socket transition in progress
  3 sessions on "schmux"  ·  2 sessions on "default"
  New sessions use "default"
```

This appears only during the transitional period and disappears once all sessions are on one socket.

#### Session detail

The session metadata panel shows the socket name (next to PID, workspace, agent). The "attach" command already includes the correct `-L` flag via `GetAttachCommand`, so copy-paste commands work regardless of socket.

#### `schmux status` CLI

Currently hard-codes `"schmux"` at `cmd/schmux/main.go:102`. Must read the configured socket name. Options: extend `daemon.Status()` to return socket info, or have the CLI read `config.json` directly (it already knows the schmux dir path).

```
tmux socket: schmux (inspect with `tmux -L schmux ls`)
```

If mixed-mode:

```
tmux socket: default (inspect with `tmux -L default ls`)
  note: 3 sessions still on socket "schmux"
```

### 9. Elimination of package-level tmux functions

The `if m.server != nil { ... } else { ... }` dual-path pattern throughout the branch was a transitional scaffold. With per-session socket affinity, a `TmuxServer` is always available — it just has different socket names for different sessions.

The following package-level functions in `tmux.go` (lines 326-559 of the branch) and their associated globals (`binary`, `pkgLogger`) should be deleted:

| Function             | Line | Rationale                                   |
| -------------------- | ---- | ------------------------------------------- |
| `CreateSession`      | 364  | Replaced by `TmuxServer.CreateSession`      |
| `SessionExists`      | 389  | Replaced by `TmuxServer.SessionExists`      |
| `GetPanePID`         | 399  | Replaced by `TmuxServer.GetPanePID`         |
| `CaptureOutput`      | 426  | Replaced by `TmuxServer.CaptureOutput`      |
| `CaptureLastLines`   | 451  | Replaced by `TmuxServer.CaptureLastLines`   |
| `KillSession`        | 477  | Replaced by `TmuxServer.KillSession`        |
| `ListSessions`       | 490  | Replaced by `TmuxServer.ListSessions`       |
| `SetOption`          | 517  | Internal to `ConfigureStatusBar`            |
| `ConfigureStatusBar` | 528  | Replaced by `TmuxServer.ConfigureStatusBar` |
| `ResizeWindow`       | 536  | Dead code (zero callers)                    |
| `RenameSession`      | 552  | Replaced by `TmuxServer.RenameSession`      |
| `GetCursorState`     | 570  | Replaced by `TmuxServer.GetCursorState`     |
| `ShowEnvironment`    | 330  | Replaced by `TmuxServer.ShowEnvironment`    |
| `SetEnvironment`     | 354  | Replaced by `TmuxServer.SetEnvironment`     |
| `binary` (var)       | 22   | Replaced by `TmuxServer.binary` field       |
| `pkgLogger` (var)    | 17   | Replaced by `TmuxServer.logger` field       |
| `Binary()`           | 25   | Replaced by `TmuxServer.Binary()`           |

Retained at package level (no server interaction):

- `ValidateBinary(path)` — pre-server validation, used before any `TmuxServer` exists
- `StripAnsi`, `IsPromptLine`, `IsSeparatorLine`, `IsChoiceLine`, `IsAgentStatusLine`, `ExtractLatestResponse`, `MaxExtractedLines` — pure utilities
- `CursorState` — data type
- `ansiRegex` — internal to `StripAnsi`

### 10. Upgrade paths

Two upgrade paths exist depending on which branch the user is on:

**From `main` (pre-isolation):** Sessions were on the default tmux server. State has no `TmuxSocket` field, so it unmarshals as `""`. The spec maps `""` to `"default"`, which is correct — these sessions genuinely live on the default server. On first startup after upgrade:

1. Config defaults to `tmux_socket_name: "schmux"`
2. Restored sessions have `TmuxSocket: ""` → resolved as `"default"`
3. `serverForSocket("default")` checks the default tmux server — sessions are found
4. Trackers start with the correct server — control mode attaches to the right socket
5. New spawns go to `"schmux"` — the system is now in mixed-socket mode
6. Sessions drain naturally from `"default"` as agents finish

**From the isolation branch (sessions on `"schmux"` but no `TmuxSocket` in state):** The isolation branch creates sessions on socket `"schmux"` but does not write `TmuxSocket` to state. On upgrade to this spec:

- `TmuxSocket: ""` → mapped to `"default"` → wrong server
- The daemon would look for `"schmux"` sessions on the `"default"` server and fail

**Fix:** Add a migration step in `state.Load()`: if `sess.TmuxSocket == ""` and the config socket is `"schmux"` (the isolation branch default), backfill `TmuxSocket` with `"schmux"`. Alternatively, the isolation branch should always persist `TmuxSocket` to state before this spec lands. The latter is preferred — it makes the migration path unambiguous and keeps `state.Load()` free of heuristics.

### 11. Validation

**Socket name validation:**

- Must be non-empty after applying the default
- Must contain only alphanumeric characters, hyphens, and underscores (tmux socket name constraints)
- Validated at config save time (dashboard settings handler) AND enforced at `serverForSocket` entry (defense in depth against state corruption or manual state edits)

**Startup validation:**

- `TmuxServer.Check()` validates the binary is executable (already exists)
- `TmuxServer.StartServer()` ensures the configured socket's server is running (already exists)
- For restored sessions on a different socket: start that server too, or skip sessions if it fails
- `ValidateReadyToRun` at `daemon.go:120` hardcodes `"schmux"` before config is loaded. This is harmless — `Check()` only validates the binary, not the socket — but should be updated for consistency

### 12. Config change at runtime

Config IS hot-reloaded: `handleConfigUpdate` at `handlers_config.go:257` calls `s.config.Reload()`, and many fields take effect immediately. However, the `TmuxServer` is constructed once at daemon startup (`daemon.go:423`) and never replaced. If the socket name changes in config but `m.server` still uses the old name, new spawns would record the new socket name in `sess.TmuxSocket` but be created on the old socket — silent state corruption.

**Required:** Add `tmux_socket_name` to the `NeedsRestart` condition at `handlers_config.go:756`:

```go
// Before applying updates (alongside oldTmuxBinary at line 266):
oldTmuxSocketName := cfg.TmuxSocketName

// In the NeedsRestart check (line 756):
if !reflect.DeepEqual(oldNetwork, cfg.Network) ||
    !reflect.DeepEqual(oldAccessControl, cfg.AccessControl) ||
    cfg.TmuxBinary != oldTmuxBinary ||
    cfg.TmuxSocketName != oldTmuxSocketName {
    s.state.SetNeedsRestart(true)
}
```

This ensures the dashboard shows the restart banner when the socket name changes, preventing the user from spawning sessions on a mismatched socket.

Future option: if hot-swap is desired, the session manager could accept a `SetDefaultServer(*TmuxServer)` method guarded by its existing mutex. But this is not needed for the initial implementation — `m.mu` is NOT held during Spawn (it only guards the tracker map), so an atomic swap of `m.server` mid-Spawn would cause the three sequential tmux calls (CreateSession, ConfigureStatusBar, GetPanePID) to target different sockets.

## Testing

**Config tests:**

- `GetTmuxSocketName()` returns `"schmux"` when field is empty
- `GetTmuxSocketName()` returns the configured value when set
- Socket name validation rejects invalid characters
- `ConfigResponse` and `ConfigUpdateRequest` include `TmuxSocketName`

**Session state tests:**

- `TmuxSocket` is persisted and restored from JSON
- Empty `TmuxSocket` is treated as `"default"`
- State migration: old state.json without `TmuxSocket` loads cleanly

**Manager tests:**

- `serverForSocket()` returns a `TmuxServer` with the correct socket name
- `serverForSocket("")` returns a `TmuxServer` with socket name `"default"`
- Spawn sets `TmuxSocket` on the created session
- Dispose uses the session's `TmuxSocket`, not the manager's default
- EnsureTracker creates a `LocalSource` with the session's socket-specific server
- Restore loop checks session existence on the correct socket
- IsRunning resolves the correct server for each session

**Integration/E2E tests:**

- Spawn a session, change config socket name, spawn another session: verify sessions are on different sockets
- Dispose a session on the old socket: verify it's killed on the correct server
- Daemon restart with mixed-socket sessions: verify all sessions are restored correctly
- Attach command includes correct `-L` flag for each session's socket
- E2E test helpers (`e2e.go:551, 561, 895, 1019`) must parameterize the socket name — currently hardcoded to `-L schmux`

**Scenario tests:**

- `helpers-terminal.ts` tmux commands (`sendTmuxCommand:41`, `capturePane:61`, `clearTmuxHistory:194`, `getTmuxCursorPosition:297`, `getTmuxCursorVisible:356`) must accept a socket name parameter
- Attach command regex at `helpers-terminal.ts:24` already makes `-L schmux` optional — verify it handles other socket names

**Dashboard tests:**

- Settings page validates socket name
- Mixed-socket banner appears when sessions span multiple sockets
- Session detail shows socket name
- `SessionResponseItem` includes `TmuxSocket` field

## Alternatives considered

| Alternative                               | Verdict  | Key Evidence                                                                                                                                                                                                              |
| ----------------------------------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| TmuxServer pool/cache                     | Rejected | 56-byte struct, ~1/sec call rate. Codebase only caches O(n) lookups (e.g., `config.go:120` repo URL cache). Trivial construction needs no cache.                                                                          |
| LocalSource-only (no resolver on Manager) | Rejected | 11+ Manager call sites need TmuxServer for lifecycle ops (create, kill, capture, rename) that are outside the `ControlSource` streaming boundary. Would expand ControlSource from 8 to 16+ methods.                       |
| CLI flag instead of config field          | Rejected | `TmuxBinary` config field is exact precedent. CLI flag requires changes across 4 files vs 1 field addition. Config field allows dashboard UI.                                                                             |
| Atomic hot-swap (no restart)              | Rejected | Spawn does 3 sequential tmux calls on one socket (CreateSession, ConfigureStatusBar, GetPanePID). Atomic swap mid-Spawn corrupts session state. No codebase precedent for atomic pointer swaps of infrastructure objects. |
| Per-workspace socket                      | Rejected | Contradicts daemon-level singleton tmux pattern in 5+ subsystems. Floor manager is daemon-scoped, not workspace-scoped. Would require per-socket locking across session manager, floor manager, dashboard, and NudgeNik.  |

## Out of scope

- Transparent session relocation between sockets (tmux limitation)
- Multi-daemon support (orthogonal concern, unchanged from isolation spec)
- Hot-swap of default server without daemon restart
- Per-workspace socket configuration (all sessions in a workspace use the same socket; the socket is a daemon-level setting)
- Automatic drain orchestration (no "migrate all" button — sessions drain naturally)
