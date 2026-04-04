# tmux Isolation and Control Mode Unification (Final)

**Date:** 2026-04-03
**Status:** Design
**Motivated by:** Architecture review finding #7 (tmux isolation and integration model)
**Previous versions:** `tmux-isolation-design.md`, `tmux-isolation-design-v2.md`

---

## Changes from previous version

This revision addresses all critical issues and incorporates suggestions from the second review (`tmux-isolation-design-review-2.md`).

**C1 (SendKeys migration -- semantic correctness):** Fixed. The v2 design incorrectly claimed that `runtime.SendInput("C-u")` and `runtime.SendInput("Enter")` would work via `ClassifyKeyRuns`. They would not -- `ClassifyKeyRuns` operates on raw byte values, not tmux key name strings. `"C-u"` is three printable ASCII characters (67, 45, 117) and would be sent as literal text `C-u` via `send-keys -l`, not as Ctrl+U. The correct approach is option 2 from the review: add a `SendTmuxKeyName(name string)` method to `ControlSource` that issues `send-keys -t %pane KEYNAME` (without `-l`) via control mode. All migration examples updated. See Section 4a.

**C2 (FloorManager admin calls missing):** Fixed. Added all 7 `tmux.*` calls from `floormanager/manager.go` to the caller enumeration in Section 5c. `floormanager.Manager` needs `*TmuxServer` injected at construction (passed from `daemon.go`). See Section 5c.

**C3 (Incomplete function accounting):** Fixed. Complete accounting of all 35 exported symbols (30 functions, 2 types, 1 interface, 1 variable, 1 constant) plus 5 unexported helpers in `internal/tmux/tmux.go`. Dead code explicitly marked as eliminated. See Section 2.

**S1 (CaptureLines escape sequence behavior):** Noted. The control mode `CapturePaneLines` always uses the `-e` flag (escapes included), while `handlers_capture.go` currently calls `tmux.CaptureLastLines(ctx, name, lines, false)` (no escapes). This silently changes the `/api/sessions/{id}/capture` API response to include ANSI escapes. See Section 5b note.

**S2 (DiagnosticCounters and SetTmuxSession type assertions):** Addressed. Added `DiagnosticCounters() map[string]int64` and `SetTmuxSession(name string)` to the type assertion inventory alongside `IsAttached()` and `SyncTrigger()`. All four are handled via interface additions or optional interfaces. See Section 3.

**S3 (handlers_tell.go plumbing):** Noted. `handlers_tell.go` currently only accesses the state store (`s.state.GetSession(sessionID)`) and has no interaction with the session manager. To get a `SessionRuntime`, it needs `s.session.GetTracker(sessionID)` added. See Section 5b note.

**S4 (SetWindowSizeManual dead code):** Eliminated. Zero callers in production code (only `tmux_test.go`). Listed in Section 2e.

**S5 (IsPaneDead dead code):** Eliminated. Zero callers anywhere in the codebase. Listed in Section 2e.

---

## Problem

schmux has two tmux integration problems:

1. **Shared tmux server.** schmux uses the default tmux server, sharing namespace with the user's own sessions. `tmux ls` shows both. `CleanTmuxServerEnv()` mutates the shared server's global environment. A user killing their tmux server kills all schmux sessions.

2. **Split-brain operation.** For each local session, `LocalSource` holds a persistent control mode connection (`tmux -C attach-session`) for streaming output. But many runtime callers -- `handlers_tell.go`, `handlers_capture.go`, `floormanager/injector.go`, `floormanager/manager.go`, `websocket.go` fallbacks -- bypass the tracker and shell out directly to the `tmux` CLI. Two paths compete for tmux server state and duplicate work.

Remote sessions don't have either problem: they use a dedicated `schmux` tmux session and route all queries through the control mode client. Local is the inconsistent path.

## Design

### 1. Socket isolation via `TmuxServer` struct

The `internal/tmux` package transforms from package-level functions with global state into a `TmuxServer` struct:

```go
type TmuxServer struct {
    binary     string
    socketName string   // "schmux"
    logger     *log.Logger
}

func NewTmuxServer(binary, socketName string, logger *log.Logger) *TmuxServer
```

All methods use a private helper that prepends `-L schmux`:

```go
func (s *TmuxServer) cmd(ctx context.Context, args ...string) *exec.Cmd {
    fullArgs := append([]string{"-L", s.socketName}, args...)
    return exec.CommandContext(ctx, s.binary, fullArgs...)
}
```

The socket name `schmux` is hardcoded, not configurable. Multi-daemon is unsupported (see Section 8).

### 2. Complete function accounting

Every exported symbol in `internal/tmux/tmux.go` is accounted for below. The file contains 30 exported functions, 2 exported types (`Checker` interface, `CursorState` struct), 1 exported variable (`TmuxChecker`), 1 exported constant (`MaxExtractedLines`), and 5 unexported helpers (`extractChoiceLines`, `nestingEnvVars`, `tmuxManagedKeys`, `baselineMu`/`baselineKeys`). Nothing is silently dropped.

#### 2a. `TmuxServer` methods (admin + spawn-time operations)

These are operations that run before a `ControlSource`/tracker exists, or that manage server-level state. They become methods on `TmuxServer` and use `s.cmd()` to get `-L schmux` automatically.

| Function             | Rationale                                                                                                                                                                                               |
| -------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `CreateSession`      | Spawn-time. Creates the tmux session.                                                                                                                                                                   |
| `KillSession`        | Dispose-time. Kills session after tracker is stopped.                                                                                                                                                   |
| `ListSessions`       | Admin query. Used by debug endpoint and health checks.                                                                                                                                                  |
| `SessionExists`      | Dispose-time guard and FM health check.                                                                                                                                                                 |
| `GetAttachCommand`   | Returns attach string. Now includes `-L schmux`.                                                                                                                                                        |
| `ConfigureStatusBar` | Spawn-time. Called immediately after `CreateSession`, before any tracker exists. Calls `SetOption` four times.                                                                                          |
| `SetOption`          | Spawn-time. Used by `ConfigureStatusBar` and `CreateSession` (for `history-limit`).                                                                                                                     |
| `GetPanePID`         | Spawn-time. Called immediately after `CreateSession` to populate session state. No tracker exists yet.                                                                                                  |
| `CaptureOutput`      | Dispose-time (full scrollback). Used by `manager.go` at dispose and `GetOutput()`. Distinct from the control mode visible/lines capture.                                                                |
| `CaptureLastLines`   | CLI-based scrollback capture. Used by `websocket.go` fallbacks and `handlers_capture.go`. Retained as a `TmuxServer` method for fallback paths; primary runtime path is `ControlSource.CaptureLines()`. |
| `GetCursorState`     | CLI-based cursor query. Used by `websocket.go` fallback (line 367). Retained as a `TmuxServer` method for fallback paths; primary runtime path is `ControlSource.GetCursorState()`.                     |
| `RenameSession`      | Admin. Used by `session/manager.go:1407` for nickname updates.                                                                                                                                          |
| `ShowEnvironment`    | Server-level. Used by `/environment` dashboard page.                                                                                                                                                    |
| `SetEnvironment`     | Server-level. Used by `/environment` sync endpoint.                                                                                                                                                     |

The `Binary()` accessor becomes `s.Binary()` method for callers that need the raw binary path (e.g., `localsource.go` building `exec.Command`). A `SocketName()` accessor is also added for `localsource.go` to build the `-L` flag.

The `Check()` method absorbs `defaultChecker.Check()`. The `Checker` interface and `TmuxChecker` global are eliminated (see 2b).

#### 2b. Functions and globals eliminated (no longer needed)

| Symbol                        | Kind                     | Rationale                                                                                                                                     |
| ----------------------------- | ------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `SetBinary()`                 | Function                 | Global setter. Replaced by `TmuxServer` constructor parameter.                                                                                |
| `SetLogger()`                 | Function                 | Global setter. Replaced by `TmuxServer` constructor parameter.                                                                                |
| `Binary()`                    | Function (package-level) | Global accessor. Replaced by `TmuxServer.Binary()` method.                                                                                    |
| `SetBaseline()`               | Function                 | Tracked pollution keys for the shared server's global environment. With an isolated socket, schmux owns the server -- no pollution to track.  |
| `CleanTmuxServerEnv()`        | Function                 | Removed nesting env vars and pollution from the shared server. Unnecessary when schmux owns its server.                                       |
| `nestingEnvVars`              | Variable                 | Only used by `CleanTmuxServerEnv`.                                                                                                            |
| `tmuxManagedKeys`             | Variable                 | Only used by `CleanTmuxServerEnv`.                                                                                                            |
| `baselineMu` / `baselineKeys` | Variables                | Only used by `SetBaseline` / `CleanTmuxServerEnv`.                                                                                            |
| `TmuxChecker`                 | Variable                 | Package-level global. Absorbed into `TmuxServer.Check()` method.                                                                              |
| `NewDefaultChecker()`         | Function                 | Factory for global checker. Absorbed into `TmuxServer.Check()`.                                                                               |
| `Checker`                     | Interface                | Only consumer was `TmuxChecker`. Replaced by `TmuxServer.Check()`.                                                                            |
| `defaultChecker`              | Struct                   | Implementation detail of `Checker`. Absorbed into `TmuxServer`.                                                                               |
| `binary`                      | Variable                 | Package-level global. Replaced by `TmuxServer.binary` field.                                                                                  |
| `pkgLogger`                   | Variable                 | Package-level global. Replaced by `TmuxServer.logger` field.                                                                                  |
| `SendKeys`                    | Function                 | CLI wrapper. Runtime callers migrate to `ControlSource.SendKeys()` (for raw bytes) or `ControlSource.SendTmuxKeyName()` (for tmux key names). |
| `SendLiteral`                 | Function                 | CLI wrapper with `-l` flag. Runtime callers migrate to `ControlSource.SendKeys()` which handles literal text via `ClassifyKeyRuns`.           |

#### 2c. Package-level functions retained (pre-server or pure utility)

| Symbol                           | Kind                  | Rationale                                                                                                                                                   |
| -------------------------------- | --------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ValidateBinary(path)`           | Function              | Pre-server validation. Used by `handlers_config.go` to validate a user-provided binary path before saving config. Must work before any `TmuxServer` exists. |
| `StripAnsi(text)`                | Function              | Pure utility. No tmux server interaction.                                                                                                                   |
| `IsAgentStatusLine(text)`        | Function              | Pure utility. No tmux server interaction.                                                                                                                   |
| `ExtractLatestResponse(lines)`   | Function              | Pure utility. Terminal output parsing for `nudgenik`. No tmux server interaction.                                                                           |
| `IsSeparatorLine(text)`          | Function              | Pure utility. Internal to `ExtractLatestResponse`.                                                                                                          |
| `IsPromptLine(text)`             | Function              | Pure utility. Internal to `ExtractLatestResponse`.                                                                                                          |
| `IsChoiceLine(text)`             | Function              | Pure utility. Internal to `ExtractLatestResponse`.                                                                                                          |
| `extractChoiceLines(lines, idx)` | Function (unexported) | Internal to `ExtractLatestResponse`.                                                                                                                        |
| `MaxExtractedLines`              | Constant              | Used by `ExtractLatestResponse`.                                                                                                                            |
| `CursorState`                    | Struct                | Returned by `GetCursorState`. Stays as a tmux package type.                                                                                                 |
| `ansiRegex`                      | Variable (unexported) | Used by `StripAnsi`.                                                                                                                                        |

#### 2d. Functions that move to control mode (via `SessionRuntime` -> `ControlSource`)

These are runtime queries that currently shell out to the tmux CLI but should go through the existing control mode connection. The `ControlSource` interface already has methods for all of these except `SendTmuxKeyName` (new, see Section 4a).

| CLI function                                             | ControlSource method              | Already implemented                                                                             |
| -------------------------------------------------------- | --------------------------------- | ----------------------------------------------------------------------------------------------- |
| `SendKeys` (with tmux key names like `"C-u"`, `"Enter"`) | `SendTmuxKeyName(name)`           | **New** -- issues `send-keys -t %pane KEYNAME` (without `-l`) via control mode. See Section 4a. |
| `SendLiteral`                                            | `SendKeys(text)`                  | Yes -- `client.SendKeys()` classifies raw bytes and uses `-l` for literal runs internally.      |
| `CaptureLastLines`                                       | `CaptureLines(n)`                 | Yes -- `LocalSource.CaptureLines()` delegates to `client.CapturePaneLines()`.                   |
| `CapturePane` (visible)                                  | `CaptureVisible()`                | Yes -- `LocalSource.CaptureVisible()` delegates to `client.CapturePaneVisible()`.               |
| `GetCursorState`                                         | `GetCursorState()`                | Yes -- `LocalSource.GetCursorState()` delegates to `client.GetCursorState()`.                   |
| `GetCursorPosition`                                      | `GetCursorState()` (extract x, y) | Yes.                                                                                            |
| `ResizeWindow`                                           | `Resize(cols, rows)`              | Yes -- `LocalSource.Resize()` delegates to `client.ResizeWindow()`.                             |

#### 2e. Dead code eliminated

These functions have zero callers in production code (only referenced in `tmux_test.go`). They are removed entirely.

| Function                            | Evidence                                                                                                                                                                                |
| ----------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `IsPaneDead`                        | Zero callers anywhere in the codebase (confirmed via grep).                                                                                                                             |
| `SetWindowSizeManual`               | Zero callers outside `tmux_test.go`.                                                                                                                                                    |
| `GetWindowSize`                     | Zero callers outside `tmux_test.go`.                                                                                                                                                    |
| `ResizeWindow` (package-level)      | Zero callers outside `tmux_test.go`. Runtime resize goes through `ControlSource.Resize()` -> `client.ResizeWindow()`.                                                                   |
| `GetCursorPosition` (package-level) | Zero callers outside `tmux_test.go` and `tracker.go` (which delegates to `ControlSource.GetCursorState()` already). The tracker method stays; the package-level function is eliminated. |

### 3. `SessionRuntime` (renamed from `SessionTracker`)

`SessionRuntime` is a **rename** of `SessionTracker`. It preserves every existing responsibility:

- **Sequenced output log** (`OutputLog`) for replay-based WebSocket bootstrap and gap recovery
- **Fan-out** to WebSocket subscribers (`SubscribeOutput` / `UnsubscribeOutput`)
- **Activity tracking** (debounced `UpdateSessionLastOutput` to state store)
- **Event watcher** integration (file-based event system for the unified event bus)
- **Timelapse recording** (`RecorderFactory` / `gapCh`)
- **Health probes** (`TmuxHealthProbe`)
- **Diagnostic counters** (`TrackerCounters` -- events delivered, bytes, reconnects, fan-out drops, WS stats)
- **Terminal size tracking** (`LastTerminalCols` / `LastTerminalRows`)
- **Output callback** (preview autodetect)

The struct continues to hold a `ControlSource` internally and delegate transport operations to it. The public API stays the same:

```go
type SessionRuntime struct {
    sessionID      string
    source         ControlSource          // local or remote transport
    state          state.StateStore
    eventWatcher   *events.EventWatcher
    outputCallback func([]byte)
    logger         *log.Logger

    // Output log, fan-out subscribers, diagnostic counters, etc.
    // (all existing SessionTracker fields preserved)
    outputLog      *OutputLog
    subs           []chan SequencedOutput
    Counters       TrackerCounters
    HealthProbe    *TmuxHealthProbe
    // ... (everything else from SessionTracker)
}

// Existing delegating methods -- same signatures, same behavior:
func (r *SessionRuntime) SendInput(data string) (controlmode.SendKeysTimings, error)
func (r *SessionRuntime) SendTmuxKeyName(name string) error  // NEW -- delegates to ControlSource.SendTmuxKeyName()
func (r *SessionRuntime) Resize(cols, rows int) error
func (r *SessionRuntime) CaptureLastLines(ctx context.Context, lines int) (string, error)
func (r *SessionRuntime) CapturePane(ctx context.Context) (string, error)
func (r *SessionRuntime) GetCursorState(ctx context.Context) (controlmode.CursorState, error)
func (r *SessionRuntime) GetCursorPosition(ctx context.Context) (x, y int, err error)

// Application-layer methods (not on ControlSource):
func (r *SessionRuntime) Start()
func (r *SessionRuntime) Stop()
func (r *SessionRuntime) SubscribeOutput() <-chan SequencedOutput
func (r *SessionRuntime) UnsubscribeOutput(ch <-chan SequencedOutput)
func (r *SessionRuntime) OutputLog() *OutputLog
func (r *SessionRuntime) DiagnosticCounters() map[string]int64
func (r *SessionRuntime) IsAttached() bool
func (r *SessionRuntime) Source() ControlSource
func (r *SessionRuntime) SetTmuxSession(name string)
```

**Type assertion inventory.** The current `SessionTracker` has four type assertions to `*LocalSource` that break the `ControlSource` abstraction. All four are addressed:

1. **`IsAttached()` (line 107):** Add `IsAttached() bool` to the `ControlSource` interface. `LocalSource` already has this method. `RemoteSource` returns `true` while its connection is alive.

2. **`SyncTrigger()` (line 120):** Add an optional `SyncTriggerer` interface:

   ```go
   type SyncTriggerer interface {
       SyncTrigger() <-chan struct{}
   }
   ```

   The runtime checks `if st, ok := r.source.(SyncTriggerer); ok { ... }`. Only `LocalSource` implements it.

3. **`DiagnosticCounters()` (line 305):** Currently type-asserts to `*LocalSource` to extract parser/client-specific counters (`DroppedOutputs`, `DroppedFanOut`). Add an optional `DiagnosticsProvider` interface:

   ```go
   type DiagnosticsProvider interface {
       SourceDiagnostics() map[string]int64
   }
   ```

   `LocalSource` implements it by returning parser and client counters. `RemoteSource` can implement it to return connection-specific metrics. The runtime merges source diagnostics into its own counter map without type-asserting.

4. **`SetTmuxSession()` (line 196):** Currently type-asserts to `*LocalSource` to update the target session name (used after `RenameSession`). Add an optional `SessionRenamer` interface:
   ```go
   type SessionRenamer interface {
       SetTmuxSession(name string)
   }
   ```
   The runtime checks `if sr, ok := r.source.(SessionRenamer); ok { sr.SetTmuxSession(name) }`. Only `LocalSource` implements it. `RemoteSource` does not support runtime renames.

### 4. `ControlSource` interface -- adopted as-is, plus two additions

The existing `ControlSource` interface at `internal/session/controlsource.go` is adopted with two additions:

```go
type ControlSource interface {
    Events() <-chan SourceEvent
    SendKeys(keys string) (controlmode.SendKeysTimings, error)
    SendTmuxKeyName(name string) error                         // NEW
    IsAttached() bool                                           // NEW
    CaptureVisible() (string, error)
    CaptureLines(n int) (string, error)
    GetCursorState() (controlmode.CursorState, error)
    Resize(cols, rows int) error
    Close() error
}
```

#### 4a. `SendTmuxKeyName` -- why it is needed and how it works

The existing `ControlSource.SendKeys(keys string)` method passes raw bytes through `ClassifyKeyRuns`, which classifies by byte value: printable ASCII (32-126) becomes literal runs sent with `send-keys -l`, control characters (0x01-0x1a) become `C-a` through `C-z`, `\r`/`\n` becomes `Enter`, etc.

Callers like `handlers_tell.go`, `floormanager/injector.go`, and `floormanager/manager.go` currently pass **tmux key name strings** to `tmux.SendKeys()`:

```go
tmux.SendKeys(ctx, tmuxSession, "C-u")    // tmux interprets "C-u" as Ctrl+U
tmux.SendKeys(ctx, tmuxSession, "Enter")  // tmux interprets "Enter" as carriage return
```

These are tmux key name arguments, not raw bytes. The tmux CLI's `send-keys` command (without `-l`) parses these names via its internal key name table. `ClassifyKeyRuns` does NOT parse tmux key names -- it classifies by raw byte values. Passing `"C-u"` to `ControlSource.SendKeys()` would produce a literal run `{Text: "C-u", Literal: true}`, typing the characters C, -, u into the terminal.

`SendTmuxKeyName(name string)` solves this by issuing `send-keys -t %pane KEYNAME` (without `-l`) directly via control mode, bypassing `ClassifyKeyRuns` entirely:

```go
// LocalSource implementation
func (s *LocalSource) SendTmuxKeyName(name string) error {
    s.mu.RLock()
    client := s.cmClient
    paneID := s.paneID
    s.mu.RUnlock()
    if client == nil {
        return fmt.Errorf("not attached")
    }
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    cmd := fmt.Sprintf("send-keys -t %s %s", paneID, name)
    _, _, err := client.Execute(ctx, cmd)
    return err
}

// RemoteSource implementation -- identical pattern via its connection's client
func (s *RemoteSource) SendTmuxKeyName(name string) error {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    cmd := fmt.Sprintf("send-keys -t %s %s", s.windowID, name)
    _, _, err := s.conn.Client().Execute(ctx, cmd)
    return err
}
```

This preserves the semantic distinction:

- `SendKeys(rawBytes)` -- for terminal I/O from WebSocket (raw bytes, classified by `ClassifyKeyRuns`)
- `SendTmuxKeyName(name)` -- for programmatic callers that pass tmux key names like `"C-u"`, `"Enter"`, `"Tab"`

**`LocalSource` change for socket isolation:** The `attach()` method currently builds the tmux command as:

```go
cmd := exec.CommandContext(ctx, tmux.Binary(), "-C", "attach-session", "-t", "="+target)
```

This changes to use the `TmuxServer` to get the binary path and prepend `-L schmux`:

```go
cmd := exec.CommandContext(ctx, s.server.Binary(), "-L", s.server.SocketName(), "-C", "attach-session", "-t", "="+target)
```

`LocalSource` receives a `*TmuxServer` reference at construction (added to `NewLocalSource` parameters). It uses it only for building the control mode attach command.

**`RemoteSource`:** Unchanged. Already works entirely through its SSH-based control mode client.

### 5. Complete caller migration

Every caller that currently uses the `tmux` package directly is listed below with its concrete migration path.

#### 5a. Callers that already go through the tracker (no change needed)

| Caller                      | Current path                                                                                        | Notes            |
| --------------------------- | --------------------------------------------------------------------------------------------------- | ---------------- |
| `websocket.go` primary path | `tracker.CaptureLastLines()`, `tracker.GetCursorState()`, `tracker.SendInput()`, `tracker.Resize()` | Already correct. |

#### 5b. Callers that bypass the tracker -- migrate to `SessionRuntime`

These callers currently shell out to `tmux.SendKeys()`, `tmux.SendLiteral()`, `tmux.CaptureLastLines()`, or `tmux.GetCursorState()` directly. They migrate to use `SessionRuntime` methods instead.

| Caller                                            | Current code                                                                                                                             | After                                                                                                                                                                                                                                                                                                                                                                                                                             |
| ------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **`handlers_tell.go`** (lines 64-69)              | `tmux.SendKeys(ctx, tmuxSession, "C-u")` then `tmux.SendLiteral(ctx, tmuxSession, text)` then `tmux.SendKeys(ctx, tmuxSession, "Enter")` | `runtime.SendTmuxKeyName("C-u")` then `runtime.SendInput(text)` then `runtime.SendTmuxKeyName("Enter")`. `SendTmuxKeyName` issues `send-keys -t %pane C-u` (without `-l`) via control mode. `SendInput` delegates to `ControlSource.SendKeys()`, which classifies the literal text through `ClassifyKeyRuns` and sends with `-l`.                                                                                                 |
| **`handlers_capture.go`** (line 60)               | `tmux.CaptureLastLines(ctx, tmuxSession, lines, false)`                                                                                  | `runtime.CaptureLastLines(ctx, lines)`. Delegates to `ControlSource.CaptureLines()`. **Note:** The control mode `CapturePaneLines` always includes ANSI escape sequences (`-e` flag), while the current CLI call passes `false` (no escapes). This changes the `/api/sessions/{id}/capture` endpoint response to include ANSI escapes. If plain-text output is required, the handler should post-process with `tmux.StripAnsi()`. |
| **`floormanager/injector.go`** (lines 115-124)    | `tmux.SendKeys(ctx, tmuxSession, "C-u")` then `tmux.SendLiteral(ctx, tmuxSession, text)` then `tmux.SendKeys(ctx, tmuxSession, "Enter")` | Same pattern as `handlers_tell.go`: `runtime.SendTmuxKeyName("C-u")` then `runtime.SendInput(text)` then `runtime.SendTmuxKeyName("Enter")`. The injector receives its `SessionRuntime` reference at construction.                                                                                                                                                                                                                |
| **`floormanager/manager.go`** (lines 346-350)     | `tmux.SendKeys(ctx, tmuxSess, "C-u")` then `tmux.SendLiteral(ctx, tmuxSess, shiftMsg)` then `tmux.SendKeys(ctx, tmuxSess, "Enter")`      | Same pattern: `runtime.SendTmuxKeyName("C-u")` then `runtime.SendInput(shiftMsg)` then `runtime.SendTmuxKeyName("Enter")`. The floor manager accesses the runtime through `m.tracker` (which is renamed to `m.runtime`).                                                                                                                                                                                                          |
| **`websocket.go`** fallback (line 331)            | `tmux.CaptureLastLines(capCtx, sess.TmuxSession, ...)` when tracker capture fails                                                        | Route through `TmuxServer.CaptureLastLines()` (not the package-level function). The `TmuxServer` reference is available on the `Server` struct.                                                                                                                                                                                                                                                                                   |
| **`websocket.go`** cursor fallback (line 367)     | `tmux.GetCursorState(curCtx2, sess.TmuxSession)`                                                                                         | Route through `TmuxServer.GetCursorState()`.                                                                                                                                                                                                                                                                                                                                                                                      |
| **`websocket.go`** FM bootstrap (lines 994, 1099) | `tmux.CaptureLastLines(capCtx, tmuxName, ...)`                                                                                           | Route through `TmuxServer.CaptureLastLines()`.                                                                                                                                                                                                                                                                                                                                                                                    |

**Plumbing note for `handlers_tell.go`:** This handler currently only accesses `s.state.GetSession(sessionID)` to get the tmux session name. It has no interaction with the session manager. To get a `SessionRuntime`, it needs to call `s.session.GetTracker(sessionID)` -- the session manager is already on the dashboard `Server` struct as `s.session`. This is a new dependency for this handler.

#### 5c. Callers that use `TmuxServer` methods (admin/spawn/dispose)

These callers use tmux admin operations that must remain as CLI calls because no `ControlSource` exists at the time they run.

| Caller                                     | Current code                                                    | After                                                                                                           |
| ------------------------------------------ | --------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| **`session/manager.go`** (line 827)        | `tmux.CreateSession(ctx, tmuxSession, w.Path, command)`         | `server.CreateSession(ctx, tmuxSession, w.Path, command)`                                                       |
| **`session/manager.go`** (lines 832, 919)  | `tmux.ConfigureStatusBar(ctx, tmuxSession)`                     | `server.ConfigureStatusBar(ctx, tmuxSession)`                                                                   |
| **`session/manager.go`** (lines 835, 922)  | `tmux.GetPanePID(ctx, tmuxSession)`                             | `server.GetPanePID(ctx, tmuxSession)`                                                                           |
| **`session/manager.go`** (line 1246)       | `tmux.CaptureOutput(captureCtx, sess.TmuxSession)`              | `server.CaptureOutput(captureCtx, sess.TmuxSession)`                                                            |
| **`session/manager.go`** (lines 1257-1258) | `tmux.SessionExists()` / `tmux.KillSession()`                   | `server.SessionExists()` / `server.KillSession()`                                                               |
| **`session/manager.go`** (line 1369)       | `tmux.CaptureOutput(ctx, sess.TmuxSession)` (GetOutput)         | `server.CaptureOutput(ctx, sess.TmuxSession)`                                                                   |
| **`session/manager.go`** (line 1359)       | `tmux.GetAttachCommand(sess.TmuxSession)`                       | `server.GetAttachCommand(sess.TmuxSession)`                                                                     |
| **`session/manager.go`** (line 1407)       | `tmux.RenameSession(ctx, oldTmuxName, newTmuxName)`             | `server.RenameSession(ctx, oldTmuxName, newTmuxName)`                                                           |
| **`floormanager/manager.go`** (line 111)   | `tmux.KillSession(ctx, tmuxSess)`                               | `server.KillSession(ctx, tmuxSess)`                                                                             |
| **`floormanager/manager.go`** (line 137)   | `tmux.SessionExists(context.Background(), sess)`                | `server.SessionExists(context.Background(), sess)`                                                              |
| **`floormanager/manager.go`** (line 197)   | `tmux.SessionExists(ctx, m.sessionName)`                        | `server.SessionExists(ctx, m.sessionName)`                                                                      |
| **`floormanager/manager.go`** (line 208)   | `tmux.CreateSession(ctx, m.sessionName, m.workDir, command)`    | `server.CreateSession(ctx, m.sessionName, m.workDir, command)`                                                  |
| **`floormanager/manager.go`** (line 244)   | `tmux.SessionExists(ctx, m.sessionName)`                        | `server.SessionExists(ctx, m.sessionName)`                                                                      |
| **`floormanager/manager.go`** (line 253)   | `tmux.CreateSession(ctx, m.sessionName, m.workDir, command)`    | `server.CreateSession(ctx, m.sessionName, m.workDir, command)`                                                  |
| **`floormanager/manager.go`** (line 386)   | `tmux.KillSession(ctx, tmuxSess)`                               | `server.KillSession(ctx, tmuxSess)`                                                                             |
| **`daemon.go`** (line 192)                 | `exec.Command(tmux.Binary(), "-v", "start-server")`             | `server.StartServer(ctx)` or `exec.Command(server.Binary(), "-L", "schmux", "start-server")`                    |
| **`handlers_debug_tmux.go`** (line 47)     | `exec.CommandContext(ctx, tmux.Binary(), "list-sessions", ...)` | `server.ListSessions(ctx)`                                                                                      |
| **`handlers_environment.go`** (line 164)   | `tmux.ShowEnvironment(r.Context())`                             | `server.ShowEnvironment(r.Context())`                                                                           |
| **`handlers_environment.go`** (line 212)   | `tmux.SetEnvironment(r.Context(), req.Key, value)`              | `server.SetEnvironment(r.Context(), req.Key, value)`                                                            |
| **`handlers_config.go`** (line 728)        | `tmux.ValidateBinary(path)`                                     | `tmux.ValidateBinary(path)` (stays as package-level function)                                                   |
| **`cmd/schmux/attach.go`** (line 65)       | `exec.Command(tmux.Binary(), "attach", "-t", tmuxSession)`      | `exec.Command(server.Binary(), "-L", "schmux", "attach", "-t", tmuxSession)` or use `server.GetAttachCommand()` |

**FloorManager wiring:** `floormanager.Manager` needs a `*TmuxServer` field injected at construction. The `New()` function signature changes:

```go
func New(cfg *config.Config, sm *session.Manager, server *tmux.TmuxServer, homeDir string, logger *log.Logger) *Manager
```

The `*TmuxServer` is passed from `daemon.go` where the server is constructed.

### 6. Attach command updates

Local session attach commands change from `tmux attach -t "=SESSION"` to `tmux -L schmux attach -t "=SESSION"`. Affected locations:

- `tmux.GetAttachCommand()` -- becomes `TmuxServer.GetAttachCommand()`
- Frontend help text in `HomePage.tsx` (lines 689, 1367) and `tmux-tab.tsx` (line 101)
- Scenario test helper `helpers-terminal.ts` regex parser
- CLI attach command in `cmd/schmux/attach.go`

Remote session attach commands are unaffected -- they attach to the remote host's tmux server.

`schmux status` should display the socket name: "tmux socket: schmux (inspect with `tmux -L schmux ls`)"

### 7. Migration

Hard cut. On upgrade, existing sessions on the default tmux server are orphaned. No migration logic. Sessions are ephemeral and cheap to re-create.

The daemon logs a message on first startup if the schmux socket has no sessions: "Using isolated tmux socket 'schmux'. Sessions on the default tmux server are no longer managed."

### 8. Multi-daemon limitation

The socket name `schmux` is hardcoded and not configurable. If two schmux daemons run simultaneously, they share the same tmux server and see each other's sessions. This is unsupported.

**Guard:** On startup, the daemon checks `TmuxServer.ListSessions()`. If sessions exist that are not in the daemon's state store, it logs a warning: "Found unmanaged sessions on tmux socket 'schmux'. Another daemon may be running. Multi-daemon is not supported."

No socket discriminator (PID hash, config hash) is added. There is no current use case for multi-daemon, and adding one would complicate E2E test isolation (which uses `TMUX_TMPDIR` to separate sockets).

### 9. Environment page

The `/environment` dashboard page stays. With an isolated socket it becomes more useful, not less -- users can inspect and manage what environment variables the schmux tmux server has without seeing (or polluting) their personal tmux server.

`ShowEnvironment` and `SetEnvironment` move to `TmuxServer` methods. They use `s.cmd()` to get `-L schmux`.

`SetBaseline` and `CleanTmuxServerEnv` are eliminated. With an isolated server, there is no shared environment to clean. The `updateBaseline()` call in `handlers_environment.go` and the `CleanTmuxServerEnv()` call in `CreateSession` are both removed.

## Testing

**`TmuxServer` unit tests:** Verify `cmd()` prepends `-L schmux`. Test `CreateSession`/`KillSession`/`ListSessions` on the isolated socket. Test `ConfigureStatusBar`, `GetPanePID`, `CaptureOutput`, `RenameSession`, `ShowEnvironment`, `SetEnvironment` all route through the correct socket.

**`ControlSource` conformance tests:** Shared test suite both `LocalSource` and `RemoteSource` must pass. Covers `SendKeys` (literal text via raw bytes), `SendTmuxKeyName` (tmux key names like `"C-u"`, `"Enter"`), `CaptureVisible`, `CaptureLines`, `GetCursorState`, `Resize`, `IsAttached` through control mode.

**`SessionRuntime` tests:** Renamed from existing `tracker_test.go`. Same test coverage, updated type names. Verify delegation to `ControlSource`, output log sequencing, fan-out, activity tracking, diagnostic counters.

**Attach command tests:** `TmuxServer.GetAttachCommand()` format includes `-L schmux`. Scenario test regex parser updated.

**E2E tests:** Will **not** pass without changes. The E2E test helpers use `TMUX_TMPDIR` to isolate the default socket's directory, but shell out to bare `tmux send-keys`, `tmux ls`, etc. without `-L schmux`. These hit the default socket (which has no sessions), not the schmux socket. Concrete files needing updates:

- `internal/e2e/e2e.go` lines 551-561: `exec.CommandContext(ctx, "tmux", "send-keys", ...)` -- add `-L schmux`
- `internal/e2e/e2e.go` lines 894, 1019: `exec.CommandContext(ctx, "tmux", "ls")` -- add `-L schmux`
- `internal/e2e/e2e_test.go` lines 104-109: `exec.Command("tmux", "start-server")` and `"new-session"` -- add `-L schmux`
- `internal/floormanager/injector_test.go` lines 123, 141: `tmux.SendLiteral()` and `tmux.CaptureOutput()` -- migrate to `TmuxServer` methods

**Scenario tests (Playwright):** `helpers-terminal.ts` line 24 regex for parsing attach commands needs updating for the `-L schmux` prefix.

## Out of scope

- Persistent control mode admin connection (option B -- rejected as over-engineered)
- Socket name configurability (no use case, complicates test isolation)
- Migration logic for existing sessions
- Multi-daemon support
- Fuzz tests, WebSocket protocol hardening (separate review findings)
