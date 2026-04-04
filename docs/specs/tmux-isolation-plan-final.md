# Plan: tmux Isolation and Control Mode Unification (Final)

**Goal**: Isolate schmux on its own tmux socket (`-L schmux`), consolidate runtime queries onto the control mode client, unify local/remote through the existing `ControlSource` interface, and rename `SessionTracker` to `SessionRuntime`.
**Architecture**: `TmuxServer` struct replaces package-level functions; all runtime queries go through `ControlSource`; admin operations stay as CLI shell-outs on `TmuxServer`. See `docs/specs/tmux-isolation-design-final.md`.
**Tech Stack**: Go, TypeScript/React (dashboard), Playwright (scenario tests)

---

## Changes from previous version

This revision addresses all critical issues and incorporates suggestions from the review (`tmux-isolation-plan-review-1.md`).

**C1 (Step 5a rename file list):** Fixed. Removed `internal/dashboard/websocket_test.go` (zero `SessionTracker` references). Added `internal/dashboard/websocket_helpers.go` (contains `waitForTrackerAttach(tracker *session.SessionTracker, ...)` at line 84) and `internal/dashboard/handlers_sync.go` (contains `session.NewSessionTracker(...)` at line 417).

**C2 (5th type assertion in NewSessionTracker constructor):** Fixed. Added `HealthProbeProvider` optional interface to cover the `switch s := source.(type)` at `tracker.go:132-139` that extracts the health probe from `*LocalSource` and `*RemoteSource`. Step 4 now addresses all five type assertions.

**C3 (RemoteSource file path):** Fixed. Corrected from `internal/remote/remotesource.go` to `internal/session/remotesource.go`. Verified that `RemoteSource.SendTmuxKeyName` uses `s.conn.Client().Execute(ctx, cmd)` -- `Connection.Client()` returns `*controlmode.Client`, which has an `Execute(ctx, cmd)` method at `client.go:154`. The existing `RemoteSource.SendKeys` uses `s.conn.SendKeys(ctx, paneID, keys)` (a higher-level wrapper), but `SendTmuxKeyName` needs the raw `Execute` path since `SendKeys` goes through `ClassifyKeyRuns`.

**C4 (Commit instructions):** Fixed. All commit instructions changed from `git commit -m "..."` to `/commit`, per CLAUDE.md: "ALWAYS use `/commit` to create commits. NEVER run `git commit` directly."

**C5 (Step 2 split):** Fixed. Step 2 split into three substeps: 2a (session lifecycle: `CreateSession`, `KillSession`, `SessionExists`, `ListSessions`, `StartServer`), 2b (spawn-time: `SetOption`, `ConfigureStatusBar`, `GetPanePID`, `GetAttachCommand`), and 2c (query/environment: `CaptureOutput`, `CaptureLastLines`, `GetCursorState`, `RenameSession`, `ShowEnvironment`, `SetEnvironment`).

**C6 (Scenario test helpers-terminal.ts):** Fixed. Step 17 now lists all bare `tmux` calls: `sendTmuxCommand` (lines 41-42), `capturePane` (lines 61-63), `clearTmuxHistory` (line 195), `getTmuxCursorPosition` (line 297), `getTmuxCursorVisible` (line 354), plus the regex parser (line 24).

**C7 (daemon_test.go):** Fixed. Added Step 16b to update `daemon_test.go` which has 10 references to `tmux.TmuxChecker`, 1 reference to `tmux.Checker` (line 75), and a `mockChecker` struct that implements the removed interface.

**C8 (daemon.go SetBinary/SetLogger):** Fixed. Step 6 now explicitly accounts for both `tmux.SetBinary` calls (lines 183 and 410) and `tmux.SetLogger` (line 347), noting that the `TmuxServer` constructor absorbs all three.

**S1 (Remote branch unification opportunity):** Added note to Steps 11 and 12 about the opportunity to eliminate `if sess.RemoteHostID != ""` branching in `handlers_tell.go` and `handlers_capture.go`, since `RemoteSource` implements the same `ControlSource` interface.

**S2 (`./test.sh --quick` checkpoints):** Added `./test.sh --quick` checkpoints every 2-3 steps throughout the plan.

**S3 (NewLocalSource callers):** Step 9 now lists all callers that need updating when the signature changes: `manager.go:1530`, `localsource_test.go:32,37,57,64`, `tracker_test.go:29,162`, `tracker_bench_test.go:31,82`.

**S4 (status.go reference):** Fixed. Step 18 now references `cmd/schmux/main.go` (lines 92-105), not the nonexistent `status.go`.

**S5 (Groups 1 and 2 parallelism):** Fixed. Removed the misleading claim. Steps 3-4 modify `tracker.go` which is also touched by Step 5, so running Groups 1 and 2 in parallel would create merge conflicts.

**S6 (Multi-daemon startup guard):** Added to Step 6 as substep 6a-ii, implementing the guard from design Section 8.

---

## Step 1: Create `TmuxServer` struct with `cmd()` helper

**File**: `internal/tmux/tmux.go`

### 1a. Write failing test

**File**: `internal/tmux/tmux_test.go`

```go
func TestTmuxServerCmdPrependsSocket(t *testing.T) {
	srv := NewTmuxServer("tmux", "schmux", nil)
	cmd := srv.cmd(context.Background(), "list-sessions")
	// The args should be: ["-L", "schmux", "list-sessions"]
	want := []string{"-L", "schmux", "list-sessions"}
	got := cmd.Args[1:] // skip binary name
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cmd args = %v, want %v", got, want)
	}
}

func TestTmuxServerBinaryAccessor(t *testing.T) {
	srv := NewTmuxServer("/usr/local/bin/tmux", "schmux", nil)
	if got := srv.Binary(); got != "/usr/local/bin/tmux" {
		t.Errorf("Binary() = %q, want %q", got, "/usr/local/bin/tmux")
	}
}

func TestTmuxServerSocketNameAccessor(t *testing.T) {
	srv := NewTmuxServer("tmux", "test-socket", nil)
	if got := srv.SocketName(); got != "test-socket" {
		t.Errorf("SocketName() = %q, want %q", got, "test-socket")
	}
}
```

### 1b. Run test to verify it fails

```bash
go test ./internal/tmux/ -run TestTmuxServerCmd -v
```

### 1c. Write implementation

**File**: `internal/tmux/tmux.go`

```go
// TmuxServer manages an isolated tmux server accessed via the -L flag.
// All methods prepend "-L <socketName>" to tmux commands automatically.
type TmuxServer struct {
	binary     string
	socketName string
	logger     *log.Logger
}

// NewTmuxServer creates a TmuxServer that talks to a named tmux socket.
func NewTmuxServer(binary, socketName string, logger *log.Logger) *TmuxServer {
	return &TmuxServer{
		binary:     binary,
		socketName: socketName,
		logger:     logger,
	}
}

// Binary returns the tmux binary path.
func (s *TmuxServer) Binary() string { return s.binary }

// SocketName returns the tmux socket name (the -L flag value).
func (s *TmuxServer) SocketName() string { return s.socketName }

// cmd builds an exec.Cmd with -L prepended.
func (s *TmuxServer) cmd(ctx context.Context, args ...string) *exec.Cmd {
	fullArgs := append([]string{"-L", s.socketName}, args...)
	return exec.CommandContext(ctx, s.binary, fullArgs...)
}

// Check verifies the tmux binary is functional.
func (s *TmuxServer) Check() error {
	cmd := exec.Command(s.binary, "-V")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tmux is not installed or not accessible.\n-> %w", err)
	}
	if len(output) == 0 {
		return fmt.Errorf("tmux command produced no output")
	}
	return nil
}
```

### 1d. Run test to verify it passes

```bash
go test ./internal/tmux/ -run TestTmuxServer -v
```

### 1e. Commit

```
/commit
```

---

## Step 2a: Migrate session lifecycle methods to `TmuxServer`

**File**: `internal/tmux/tmux.go`

### 2a-i. Write failing test

**File**: `internal/tmux/tmux_test.go`

```go
func TestTmuxServerStartServerCmd(t *testing.T) {
	srv := NewTmuxServer("tmux", "schmux", nil)
	// Verify the method exists and compiles (integration test would verify behavior)
	_ = srv.StartServer
}
```

### 2a-ii. Write implementation

Add session lifecycle methods to `TmuxServer` that mirror existing package-level functions but use `s.cmd()`:

- `StartServer(ctx) error`
- `CreateSession(ctx, name, dir, command) error` -- no longer calls `CleanTmuxServerEnv()` (isolated server, no pollution to clean)
- `KillSession(ctx, name) error`
- `SessionExists(ctx, name) bool`
- `ListSessions(ctx) ([]string, error)` (or use existing return type)

Each method body is a copy of the existing package-level function, replacing `exec.CommandContext(ctx, binary, ...)` with `s.cmd(ctx, ...)`.

### 2a-iii. Run test to verify it passes

```bash
go test ./internal/tmux/ -run TestTmuxServer -v
```

### 2a-iv. Commit

```
/commit
```

---

## Step 2b: Migrate spawn-time methods to `TmuxServer`

**File**: `internal/tmux/tmux.go`

### 2b-i. Write implementation

Add spawn-time methods to `TmuxServer`:

- `SetOption(ctx, session, option, value) error`
- `ConfigureStatusBar(ctx, session) error`
- `GetPanePID(ctx, name) (int, error)`
- `GetAttachCommand(name) string` -- returns `tmux -L schmux attach -t "=SESSION"`

### 2b-ii. Write test

**File**: `internal/tmux/tmux_test.go`

```go
func TestTmuxServerGetAttachCommand(t *testing.T) {
	srv := NewTmuxServer("tmux", "schmux", nil)
	got := srv.GetAttachCommand("my-session")
	want := `tmux -L schmux attach -t "=my-session"`
	if got != want {
		t.Errorf("GetAttachCommand() = %q, want %q", got, want)
	}
}
```

### 2b-iii. Run test to verify it passes

```bash
go test ./internal/tmux/ -run TestTmuxServer -v
```

### 2b-iv. Commit

```
/commit
```

---

## Step 2c: Migrate query and environment methods to `TmuxServer`

**File**: `internal/tmux/tmux.go`

### 2c-i. Write implementation

Add query and environment methods to `TmuxServer`:

- `CaptureOutput(ctx, name) (string, error)`
- `CaptureLastLines(ctx, name, lines, escapes) (string, error)`
- `GetCursorState(ctx, name) (CursorState, error)`
- `RenameSession(ctx, oldName, newName) error`
- `ShowEnvironment(ctx) (map[string]string, error)`
- `SetEnvironment(ctx, key, value) error`

### 2c-ii. Run test to verify it passes

```bash
go test ./internal/tmux/ -run TestTmuxServer -v
```

### 2c-iii. Checkpoint

```bash
./test.sh --quick
```

### 2c-iv. Commit

```
/commit
```

---

## Step 3: Add `SendTmuxKeyName` and `IsAttached` to `ControlSource`

**File**: `internal/session/controlsource.go`

### 3a. Write failing test

**File**: `internal/session/tracker_test.go`

```go
func TestControlSourceInterfaceHasSendTmuxKeyName(t *testing.T) {
	// Compile-time interface conformance check
	var _ ControlSource = (*LocalSource)(nil)
}
```

### 3b. Run test to verify it fails

```bash
go test ./internal/session/ -run TestControlSourceInterface -v
```

### 3c. Write implementation

**File**: `internal/session/controlsource.go` -- add two methods to the interface:

```go
type ControlSource interface {
	Events() <-chan SourceEvent
	SendKeys(keys string) (controlmode.SendKeysTimings, error)
	SendTmuxKeyName(name string) error    // NEW
	IsAttached() bool                      // NEW
	CaptureVisible() (string, error)
	CaptureLines(n int) (string, error)
	GetCursorState() (controlmode.CursorState, error)
	Resize(cols, rows int) error
	Close() error
}
```

**File**: `internal/session/localsource.go` -- add implementations:

```go
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

func (s *LocalSource) IsAttached() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cmClient != nil
}
```

**File**: `internal/session/remotesource.go` -- add implementations:

Note: `RemoteSource` lives in the `session` package, not `remote`. The `SendTmuxKeyName` implementation uses `s.conn.Client().Execute(ctx, cmd)` rather than `s.conn.SendKeys()` because `SendKeys` goes through `ClassifyKeyRuns` (which would interpret `"C-u"` as literal characters). `Connection.Client()` (at `connection.go:227`) returns a `*controlmode.Client`, and `Client.Execute()` (at `client.go:154`) sends raw tmux commands.

```go
func (s *RemoteSource) SendTmuxKeyName(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := s.conn.Client()
	if client == nil {
		return fmt.Errorf("not connected")
	}
	cmd := fmt.Sprintf("send-keys -t %s %s", s.windowID, name)
	_, _, err := client.Execute(ctx, cmd)
	return err
}

func (s *RemoteSource) IsAttached() bool {
	return s.conn != nil && s.conn.IsConnected()
}
```

### 3d. Run test to verify it passes

```bash
go test ./internal/session/ -run TestControlSourceInterface -v
```

### 3e. Commit

```
/commit
```

---

## Step 4: Add optional interfaces for type assertion cleanup

**File**: `internal/session/controlsource.go`

### 4a. Write implementation

Add optional interfaces so `SessionTracker` (soon `SessionRuntime`) can stop type-asserting to `*LocalSource` and `*RemoteSource`:

```go
// SyncTriggerer is implemented by sources that support sync triggers.
type SyncTriggerer interface {
	SyncTrigger() <-chan struct{}
}

// DiagnosticsProvider is implemented by sources that expose transport diagnostics.
type DiagnosticsProvider interface {
	SourceDiagnostics() map[string]int64
}

// SessionRenamer is implemented by sources that support runtime session renames.
type SessionRenamer interface {
	SetTmuxSession(name string)
}

// HealthProbeProvider is implemented by sources that expose a health probe.
type HealthProbeProvider interface {
	GetHealthProbe() *TmuxHealthProbe
}
```

**File**: `internal/session/localsource.go` -- implement:

```go
func (s *LocalSource) SourceDiagnostics() map[string]int64 {
	// Return parser/client-specific counters
	// (extract from existing DiagnosticCounters logic in tracker.go)
}

func (s *LocalSource) GetHealthProbe() *TmuxHealthProbe {
	return s.HealthProbe
}
```

**File**: `internal/session/remotesource.go` -- implement:

```go
func (s *RemoteSource) GetHealthProbe() *TmuxHealthProbe {
	return s.healthProbe
}
```

### 4b. Update `SessionTracker` to use optional interfaces

**File**: `internal/session/tracker.go` -- replace all five type assertions:

```go
// Type assertion 1 -- IsAttached (line 107):
// Before: if ls, ok := t.source.(*LocalSource); ok { ... }
// After:
t.source.IsAttached()

// Type assertion 2 -- SyncTrigger (line 121):
// Before: if ls, ok := t.source.(*LocalSource); ok { ch = ls.SyncTrigger() }
// After:
if st, ok := t.source.(SyncTriggerer); ok { ch = st.SyncTrigger() }

// Type assertion 3 -- HealthProbe extraction in NewSessionTracker (lines 132-139):
// Before:
// switch s := source.(type) {
// case *LocalSource:
//     healthProbe = s.HealthProbe
// case *RemoteSource:
//     healthProbe = s.healthProbe
// default:
//     healthProbe = NewTmuxHealthProbe()
// }
// After:
if hp, ok := source.(HealthProbeProvider); ok {
    healthProbe = hp.GetHealthProbe()
} else {
    healthProbe = NewTmuxHealthProbe()
}

// Type assertion 4 -- SetTmuxSession (line 196):
// Before: if ls, ok := t.source.(*LocalSource); ok { ls.SetTmuxSession(name) }
// After:
if sr, ok := t.source.(SessionRenamer); ok { sr.SetTmuxSession(name) }

// Type assertion 5 -- DiagnosticCounters (line 305):
// Before: if ls, ok := t.source.(*LocalSource); ok { maps.Copy(counters, ls.DiagnosticCounters()) }
// After:
if dp, ok := t.source.(DiagnosticsProvider); ok { maps.Copy(counters, dp.SourceDiagnostics()) }
```

### 4c. Run tests

```bash
go test ./internal/session/ -v
```

### 4d. Checkpoint

```bash
./test.sh --quick
```

### 4e. Commit

```
/commit
```

---

## Step 5: Rename `SessionTracker` to `SessionRuntime`

**Files**: `internal/session/tracker.go`, `internal/session/tracker_test.go`, and all callers

### 5a. Rename in source

Rename the struct, constructor, and all references:

- `SessionTracker` -> `SessionRuntime`
- `NewSessionTracker` -> `NewSessionRuntime`
- `tracker` -> `runtime` in variable names where it is the primary identifier

Callers to update (search for `SessionTracker` and `NewSessionTracker`):

- `internal/session/manager.go`
- `internal/session/tracker_test.go`
- `internal/session/tracker_bench_test.go`
- `internal/dashboard/websocket.go`
- `internal/dashboard/websocket_helpers.go` -- contains `waitForTrackerAttach(tracker *session.SessionTracker, ...)` at line 84
- `internal/dashboard/handlers_sync.go` -- contains `session.NewSessionTracker(...)` at line 417
- `internal/dashboard/server.go` -- contains `crTrackers map[string]*session.SessionTracker` and related methods
- `internal/floormanager/manager.go`
- `internal/daemon/daemon.go`

### 5b. Run tests

```bash
go test ./internal/session/ ./internal/dashboard/ ./internal/daemon/ ./internal/floormanager/ -v
```

### 5c. Commit

```
/commit
```

---

## Step 6: Wire `TmuxServer` into daemon startup

**File**: `internal/daemon/daemon.go`

### 6a. Write implementation

#### 6a-i. Construct `TmuxServer` and replace globals

In `daemon.Run()` (or `daemon.Start()`), construct the `TmuxServer`:

```go
tmuxBinary := cfg.TmuxBinary // from config, replaces tmux.SetBinary(cfg.TmuxBinary)
server := tmux.NewTmuxServer(tmuxBinary, "schmux", logging.Sub(logger, "tmux"))
if err := server.Check(); err != nil {
    return err
}
if err := server.StartServer(ctx); err != nil {
    // log warning, non-fatal
}
```

This replaces the following three globals being removed:

- Line 183: `tmux.SetBinary(cfg.TmuxBinary)` (in `Start()`)
- Line 410: `tmux.SetBinary(cfg.TmuxBinary)` (in `Run()`)
- Line 347: `tmux.SetLogger(logging.Sub(logger, "tmux"))`

The `TmuxServer` constructor absorbs the binary from config and the logger, replacing both `SetBinary` and `SetLogger` call sites.

Replace the existing `exec.Command(tmux.Binary(), "-v", "start-server")` at line 192 with `server.StartServer(ctx)`.

Pass `server` to `session.NewManager(...)` and `floormanager.New(...)`.

Remove the `CleanTmuxServerEnv` call and `SetBaseline` call from startup.

#### 6a-ii. Add multi-daemon startup guard

After `server.StartServer(ctx)`, implement the guard from design Section 8:

```go
// Check for unmanaged sessions (possible second daemon)
existing, err := server.ListSessions(ctx)
if err == nil && len(existing) > 0 {
    // Cross-reference with state store
    managed := stateStore.GetAllSessions()
    managedSet := make(map[string]bool)
    for _, s := range managed {
        managedSet[s.TmuxSession] = true
    }
    for _, tmuxSess := range existing {
        if !managedSet[tmuxSess] {
            logger.Printf("WARNING: Found unmanaged sessions on tmux socket 'schmux'. Another daemon may be running. Multi-daemon is not supported.")
            break
        }
    }
}
```

### 6b. Run tests

```bash
go test ./internal/daemon/ -v
```

### 6c. Checkpoint

```bash
./test.sh --quick
```

### 6d. Commit

```
/commit
```

---

## Step 7: Update `SessionManager` to use `TmuxServer`

**File**: `internal/session/manager.go`

### 7a. Write implementation

Add `*tmux.TmuxServer` field to `Manager`. Update constructor signature.

Replace all `tmux.CreateSession(...)` with `m.server.CreateSession(...)`, and similarly for `KillSession`, `SessionExists`, `GetPanePID`, `ConfigureStatusBar`, `CaptureOutput`, `GetAttachCommand`, `RenameSession`.

See design Section 5c for the complete list of call sites.

### 7b. Run tests

```bash
go test ./internal/session/ -v
```

### 7c. Commit

```
/commit
```

---

## Step 8: Update `FloorManager` to use `TmuxServer`

**File**: `internal/floormanager/manager.go`

### 8a. Write implementation

Add `*tmux.TmuxServer` field to `Manager`. Update `New()` signature:

```go
func New(cfg *config.Config, sm *session.Manager, server *tmux.TmuxServer, homeDir string, logger *log.Logger) *Manager
```

Replace all 7 `tmux.*` calls:

- `tmux.KillSession(ctx, tmuxSess)` -> `m.server.KillSession(ctx, tmuxSess)` (lines 111, 386)
- `tmux.SessionExists(...)` -> `m.server.SessionExists(...)` (lines 137, 197, 244)
- `tmux.CreateSession(...)` -> `m.server.CreateSession(...)` (lines 208, 253)

### 8b. Run tests

```bash
go test ./internal/floormanager/ -v
```

### 8c. Commit

```
/commit
```

---

## Step 9: Update `LocalSource` to use `TmuxServer` for attach command

**File**: `internal/session/localsource.go`

### 9a. Write implementation

Add `*tmux.TmuxServer` field to `LocalSource`. Update `NewLocalSource` to accept it.

In `attach()` method, change:

```go
// Before:
cmd := exec.CommandContext(ctx, tmux.Binary(), "-C", "attach-session", "-t", "="+target)

// After:
cmd := exec.CommandContext(ctx, s.server.Binary(), "-L", s.server.SocketName(), "-C", "attach-session", "-t", "="+target)
```

**Callers of `NewLocalSource` that need updating** when its signature changes:

- `internal/session/manager.go` line 1530
- `internal/session/localsource_test.go` lines 32, 37, 57, 64
- `internal/session/tracker_test.go` lines 29, 162
- `internal/session/tracker_bench_test.go` lines 31, 82

### 9b. Run tests

```bash
go test ./internal/session/ -v
```

### 9c. Checkpoint

```bash
./test.sh --quick
```

### 9d. Commit

```
/commit
```

---

## Step 10: Update dashboard `Server` to use `TmuxServer`

**File**: `internal/dashboard/server.go`

### 10a. Write implementation

Add `*tmux.TmuxServer` field to `Server` struct. Wire from daemon.

### 10b. Migrate dashboard callers

**File**: `internal/dashboard/handlers_debug_tmux.go` -- use `s.tmuxServer.ListSessions(ctx)` instead of shelling out.

**File**: `internal/dashboard/handlers_environment.go` -- use `s.tmuxServer.ShowEnvironment(ctx)` and `s.tmuxServer.SetEnvironment(ctx, key, value)`. Remove `SetBaseline`/`CleanTmuxServerEnv` calls.

**File**: `internal/dashboard/websocket.go` -- update fallback paths:

- `tmux.CaptureLastLines(...)` -> `s.tmuxServer.CaptureLastLines(...)`
- `tmux.GetCursorState(...)` -> `s.tmuxServer.GetCursorState(...)`

### 10c. Run tests

```bash
go test ./internal/dashboard/ -v
```

### 10d. Checkpoint

```bash
./test.sh --quick
```

### 10e. Commit

```
/commit
```

---

## Step 11: Migrate `handlers_tell.go` to use `SessionRuntime`

**File**: `internal/dashboard/handlers_tell.go`

### 11a. Write implementation

Replace direct tmux calls with `SessionRuntime` methods:

```go
// Before:
tmux.SendKeys(ctx, tmuxSession, "C-u")
tmux.SendLiteral(ctx, tmuxSession, text)
tmux.SendKeys(ctx, tmuxSession, "Enter")

// After:
runtime, err := s.session.GetTracker(sessionID)  // returns *SessionRuntime
if err != nil { ... }
runtime.SendTmuxKeyName("C-u")
runtime.SendInput(text)
runtime.SendTmuxKeyName("Enter")
```

**Plumbing note:** This handler currently only accesses `s.state.GetSession(sessionID)` to get the tmux session name. It has no interaction with the session manager. To get a `SessionRuntime`, it needs to call `s.session.GetTracker(sessionID)` -- the session manager is already on the dashboard `Server` struct as `s.session`.

**Remote branch unification opportunity:** This handler has `if sess.RemoteHostID != "" { ... } else { ... }` branching. Since `RemoteSource` implements the same `ControlSource` interface, the remote branch can also use `SessionRuntime`, which would eliminate the branch entirely. Consider unifying both paths through the runtime.

### 11b. Run tests

```bash
go test ./internal/dashboard/ -v
```

### 11c. Commit

```
/commit
```

---

## Step 12: Migrate `handlers_capture.go` to use `SessionRuntime`

**File**: `internal/dashboard/handlers_capture.go`

### 12a. Write implementation

```go
// Before:
tmux.CaptureLastLines(ctx, tmuxSession, lines, false)

// After:
runtime, err := s.session.GetTracker(sessionID)
if err != nil { ... }
output, err := runtime.CaptureLastLines(ctx, lines)
// Note: control mode includes ANSI escapes. Strip if needed:
output = tmux.StripAnsi(output)
```

**Remote branch unification opportunity:** Same as Step 11 -- this handler also has `if sess.RemoteHostID != ""` branching that could be unified through the runtime.

### 12b. Run tests

```bash
go test ./internal/dashboard/ -v
```

### 12c. Checkpoint

```bash
./test.sh --quick
```

### 12d. Commit

```
/commit
```

---

## Step 13: Migrate `floormanager/injector.go` to use `SessionRuntime`

**File**: `internal/floormanager/injector.go`

### 13a. Write implementation

```go
// Before:
tmux.SendKeys(ctx, tmuxSession, "C-u")
tmux.SendLiteral(ctx, tmuxSession, text)
tmux.SendKeys(ctx, tmuxSession, "Enter")

// After:
runtime.SendTmuxKeyName("C-u")
runtime.SendInput(text)
runtime.SendTmuxKeyName("Enter")
```

The injector receives its `SessionRuntime` reference at construction or via the floor manager.

### 13b. Run tests

```bash
go test ./internal/floormanager/ -v
```

### 13c. Commit

```
/commit
```

---

## Step 14: Migrate `floormanager/manager.go` SendKeys calls

**File**: `internal/floormanager/manager.go`

### 14a. Write implementation

Lines 346-350 (shift message sending):

```go
// Before:
tmux.SendKeys(ctx, tmuxSess, "C-u")
tmux.SendLiteral(ctx, tmuxSess, shiftMsg)
tmux.SendKeys(ctx, tmuxSess, "Enter")

// After:
m.runtime.SendTmuxKeyName("C-u")
m.runtime.SendInput(shiftMsg)
m.runtime.SendTmuxKeyName("Enter")
```

### 14b. Run tests

```bash
go test ./internal/floormanager/ -v
```

### 14c. Checkpoint

```bash
./test.sh --quick
```

### 14d. Commit

```
/commit
```

---

## Step 15: Update attach commands and frontend

### 15a. Go side

**File**: `internal/tmux/tmux.go` -- `TmuxServer.GetAttachCommand` returns `tmux -L schmux attach -t "=SESSION"`.

**File**: `cmd/schmux/attach.go` -- update attach command builder and parser to include `-L schmux`.

### 15b. Frontend

**File**: `assets/dashboard/src/routes/HomePage.tsx` -- update help text at lines 689, 1367.

**File**: `assets/dashboard/src/routes/tips-page/tmux-tab.tsx` -- update line 101.

### 15c. Run tests

```bash
go test ./cmd/schmux/ -run TestAttach -v
./test.sh --quick
```

### 15d. Commit

```
/commit
```

---

## Step 16: Remove eliminated package-level functions and globals

### 16a. Remove from `internal/tmux/tmux.go`

Remove:

- Package-level `binary` var, `SetBinary()`, `Binary()` (package-level), `SetLogger()`
- `TmuxChecker` global, `NewDefaultChecker()`, `Checker` interface, `defaultChecker` struct
- `SetBaseline()`, `CleanTmuxServerEnv()`, `baselineMu`, `baselineKeys`, `nestingEnvVars`, `tmuxManagedKeys`
- `SendKeys()`, `SendLiteral()` (package-level)
- Dead code: `IsPaneDead`, `SetWindowSizeManual`, `GetWindowSize`, `ResizeWindow` (package-level), `GetCursorPosition` (package-level)

Keep: `ValidateBinary`, `StripAnsi`, `IsAgentStatusLine`, `ExtractLatestResponse` and its helpers, `CursorState` struct, `MaxExtractedLines`.

### 16b. Update `daemon_test.go`

**File**: `internal/daemon/daemon_test.go`

This file has 10 references to `tmux.TmuxChecker`, 1 reference to `tmux.Checker` (line 75), and a `mockChecker` struct that implements the removed `tmux.Checker` interface. All must be rewritten to work with `TmuxServer.Check()` instead.

Current pattern (to be replaced):

```go
// Line 75: mockChecker implements tmux.Checker
type mockChecker struct{ err error }
func (m *mockChecker) Check() error { return m.err }

// Lines 95-96, 113-115, 145-147, 173-175, 198-200 (5 test functions):
original := tmux.TmuxChecker
defer func() { tmux.TmuxChecker = original }()
tmux.TmuxChecker = &mockChecker{err: ...}
```

Replace with: inject a `*TmuxServer` into the daemon (or mock it via an interface) so tests control the Check behavior without global state.

### 16c. Verify no remaining callers of removed functions

```bash
# Should return zero results for each:
grep -rn 'tmux\.SetBinary\|tmux\.SetLogger\|tmux\.TmuxChecker\|tmux\.SetBaseline\|tmux\.CleanTmuxServerEnv\|tmux\.SendKeys\|tmux\.SendLiteral\|tmux\.IsPaneDead' internal/ cmd/
```

### 16d. Run full test suite

```bash
./test.sh --quick
```

### 16e. Commit

```
/commit
```

---

## Step 17: Update E2E tests

**File**: `internal/e2e/e2e.go`

### 17a. Update Go E2E helpers

Update all bare `tmux` commands to include `-L schmux`:

- Lines 551, 560: `exec.CommandContext(ctx, "tmux", "send-keys", ...)` -- add `-L`, `"schmux"` after `"tmux"`
- Lines 894, 1019: `exec.CommandContext(ctx, "tmux", "ls")` -- add `-L`, `"schmux"`

**File**: `internal/e2e/e2e_test.go`:

- Lines 104, 109: `exec.Command("tmux", "start-server")` and `"new-session"` -- add `-L`, `"schmux"`

**File**: `internal/floormanager/injector_test.go`:

- Migrate `tmux.SendLiteral()` and `tmux.CaptureOutput()` to `TmuxServer` methods.

### 17b. Update scenario test `helpers-terminal.ts`

**File**: `test/scenarios/generated/helpers-terminal.ts`

Update ALL bare `tmux` calls to include `-L schmux`. The following functions contain bare `tmux` shell-outs:

| Function                     | Lines | Current command                                                               | Change                                                      |
| ---------------------------- | ----- | ----------------------------------------------------------------------------- | ----------------------------------------------------------- |
| `getTmuxSessionName` (regex) | 24    | `sess.attach_cmd.match(/tmux attach -t "=(.+)"/)`                             | Update regex to `match(/tmux -L schmux attach -t "=(.+)"/)` |
| `sendTmuxCommand`            | 41    | `` `tmux send-keys -t '${tmuxSession}' -l ${shellQuote(command)}` ``          | Add `-L schmux` after `tmux`                                |
| `sendTmuxCommand`            | 42    | `` `tmux send-keys -t '${tmuxSession}' Enter` ``                              | Add `-L schmux` after `tmux`                                |
| `capturePane`                | 61    | `` `tmux capture-pane -p -t '${tmuxSession}'` ``                              | Add `-L schmux` after `tmux`                                |
| `capturePane`                | 63    | `` `tmux capture-pane -p -t '${tmuxSession}' -S -${...}` ``                   | Add `-L schmux` after `tmux`                                |
| `clearTmuxHistory`           | 195   | `` `tmux clear-history -t '${tmuxSession}'` ``                                | Add `-L schmux` after `tmux`                                |
| `getTmuxCursorPosition`      | 297   | `` `tmux display-message -p -t '${tmuxSession}' '#{cursor_x} #{cursor_y}'` `` | Add `-L schmux` after `tmux`                                |
| `getTmuxCursorVisible`       | 354   | `` `tmux display-message -p -t '${tmuxSession}' '#{cursor_flag}'` ``          | Add `-L schmux` after `tmux`                                |

### 17c. Run E2E tests

```bash
./test.sh --e2e
```

### 17d. Commit

```
/commit
```

---

## Step 18: Update `schmux status` output

**File**: `cmd/schmux/main.go` (lines 92-105, the `"status"` case)

### 18a. Write implementation

Add tmux socket info to status output: "tmux socket: schmux (inspect with `tmux -L schmux ls`)"

### 18b. Commit

```
/commit
```

---

## Step 19: End-to-end verification

### 19a. Run the full test suite

```bash
./test.sh
```

### 19b. Manual verification

1. Build: `go build ./cmd/schmux`
2. Start daemon: `./schmux start`
3. Verify isolation: `tmux ls` shows NO schmux sessions; `tmux -L schmux ls` shows them
4. Spawn a session via dashboard, verify terminal works
5. Verify attach command in session detail includes `-L schmux`
6. Copy attach command, run it -- verify it connects
7. Check `/environment` page works
8. Stop daemon: `./schmux stop`

### 19c. Commit

```
/commit
```

---

## Task Dependencies

| Group | Steps               | Can Parallelize                     | Notes                                       |
| ----- | ------------------- | ----------------------------------- | ------------------------------------------- |
| 1     | Steps 1, 2a, 2b, 2c | No (sequential)                     | Foundation: `TmuxServer` struct             |
| 2     | Steps 3-4           | No (sequential, depends on Group 1) | `ControlSource` interface changes           |
| 3     | Step 5              | No (depends on Group 2)             | Rename `SessionTracker` -> `SessionRuntime` |
| 4     | Steps 6-10          | No (sequential, wiring)             | Wire `TmuxServer` through system            |
| 5     | Steps 11-14         | Yes (independent callers)           | Migrate runtime callers to `SessionRuntime` |
| 6     | Steps 15-16         | Yes (independent)                   | Attach commands + cleanup                   |
| 7     | Steps 17-18         | Yes (independent)                   | E2E tests + status output                   |
| 8     | Step 19             | No (final)                          | End-to-end verification                     |

All groups are sequential from Group 1 through Group 4. Groups 5, 6, and 7 can run in parallel after Group 4 completes.
