# Plan: tmux Isolation and Control Mode Unification

**Goal**: Isolate schmux on its own tmux socket (`-L schmux`), consolidate runtime queries onto the control mode client, unify local/remote through the existing `ControlSource` interface, and rename `SessionTracker` to `SessionRuntime`.
**Architecture**: `TmuxServer` struct replaces package-level functions; all runtime queries go through `ControlSource`; admin operations stay as CLI shell-outs on `TmuxServer`. See `docs/specs/tmux-isolation-design-final.md`.
**Tech Stack**: Go, TypeScript/React (dashboard), Playwright (scenario tests)

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

```bash
git commit -m "feat(tmux): add TmuxServer struct with socket-aware cmd() helper"
```

---

## Step 2: Migrate admin methods to `TmuxServer`

**File**: `internal/tmux/tmux.go`

### 2a. Write failing test

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

### 2b. Run test to verify it fails

```bash
go test ./internal/tmux/ -run TestTmuxServerGetAttach -v
```

### 2c. Write implementation

Add methods to `TmuxServer` that mirror existing package-level functions but use `s.cmd()`:

- `StartServer(ctx) error`
- `CreateSession(ctx, name, dir, command) error`
- `KillSession(ctx, name) error`
- `ListSessions(ctx) ([]string, error)` (or use existing return type)
- `SessionExists(ctx, name) bool`
- `GetAttachCommand(name) string`
- `SetOption(ctx, session, option, value) error`
- `ConfigureStatusBar(ctx, session) error`
- `GetPanePID(ctx, name) (int, error)`
- `CaptureOutput(ctx, name) (string, error)`
- `CaptureLastLines(ctx, name, lines, escapes) (string, error)`
- `GetCursorState(ctx, name) (CursorState, error)`
- `RenameSession(ctx, oldName, newName) error`
- `ShowEnvironment(ctx) (map[string]string, error)`
- `SetEnvironment(ctx, key, value) error`

Each method body is a copy of the existing package-level function, replacing `exec.CommandContext(ctx, binary, ...)` with `s.cmd(ctx, ...)`.

The `CreateSession` method no longer calls `CleanTmuxServerEnv()` (isolated server, no pollution to clean).

### 2d. Run test to verify it passes

```bash
go test ./internal/tmux/ -run TestTmuxServer -v
```

### 2e. Commit

```bash
git commit -m "feat(tmux): migrate admin methods to TmuxServer"
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

**File**: `internal/session/controlsource.go` — add two methods to the interface:

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

**File**: `internal/session/localsource.go` — add implementations:

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

**File**: `internal/remote/remotesource.go` (or equivalent) — add implementations:

```go
func (s *RemoteSource) SendTmuxKeyName(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := fmt.Sprintf("send-keys -t %s %s", s.windowID, name)
	_, _, err := s.conn.Client().Execute(ctx, cmd)
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

```bash
git commit -m "feat(session): add SendTmuxKeyName and IsAttached to ControlSource"
```

---

## Step 4: Add optional interfaces for type assertion cleanup

**File**: `internal/session/controlsource.go`

### 4a. Write implementation

Add optional interfaces so `SessionTracker` (soon `SessionRuntime`) can stop type-asserting to `*LocalSource`:

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
```

**File**: `internal/session/localsource.go` — implement `SourceDiagnostics()`:

```go
func (s *LocalSource) SourceDiagnostics() map[string]int64 {
	// Return parser/client-specific counters
	// (extract from existing DiagnosticCounters logic in tracker.go)
}
```

### 4b. Update `SessionTracker` to use optional interfaces

**File**: `internal/session/tracker.go` — replace all four type assertions:

```go
// Before:
if ls, ok := t.source.(*LocalSource); ok { ... }

// After (IsAttached):
t.source.IsAttached()

// After (SyncTrigger):
if st, ok := t.source.(SyncTriggerer); ok { ch = st.SyncTrigger() }

// After (DiagnosticCounters):
if dp, ok := t.source.(DiagnosticsProvider); ok { maps.Copy(counters, dp.SourceDiagnostics()) }

// After (SetTmuxSession):
if sr, ok := t.source.(SessionRenamer); ok { sr.SetTmuxSession(name) }
```

### 4c. Run tests

```bash
go test ./internal/session/ -v
```

### 4d. Commit

```bash
git commit -m "refactor(session): replace LocalSource type assertions with optional interfaces"
```

---

## Step 5: Rename `SessionTracker` to `SessionRuntime`

**Files**: `internal/session/tracker.go`, `internal/session/tracker_test.go`, and all callers

### 5a. Rename in source

Rename the struct, constructor, and all references:

- `SessionTracker` → `SessionRuntime`
- `NewSessionTracker` → `NewSessionRuntime`
- `tracker` → `runtime` in variable names where it's the primary identifier

Callers to update (search for `SessionTracker` and `NewSessionTracker`):

- `internal/session/manager.go`
- `internal/session/tracker_test.go`
- `internal/session/tracker_bench_test.go`
- `internal/dashboard/websocket.go`
- `internal/dashboard/websocket_test.go`
- `internal/dashboard/server.go`
- `internal/floormanager/manager.go`
- `internal/daemon/daemon.go`

### 5b. Run tests

```bash
go test ./internal/session/ ./internal/dashboard/ ./internal/daemon/ ./internal/floormanager/ -v
```

### 5c. Commit

```bash
git commit -m "refactor(session): rename SessionTracker to SessionRuntime"
```

---

## Step 6: Wire `TmuxServer` into daemon startup

**File**: `internal/daemon/daemon.go`

### 6a. Write implementation

In `daemon.Run()` (or `daemon.Start()`), construct the `TmuxServer`:

```go
tmuxBinary := tmux.Binary() // or from config
server := tmux.NewTmuxServer(tmuxBinary, "schmux", logger)
if err := server.Check(); err != nil {
    return err
}
if err := server.StartServer(ctx); err != nil {
    // log warning, non-fatal
}
```

Replace the existing `exec.Command(tmux.Binary(), "-v", "start-server")` at line 192.

Pass `server` to `session.NewManager(...)` and `floormanager.New(...)`.

Remove the `CleanTmuxServerEnv` call and `SetBaseline` call from startup.

### 6b. Run tests

```bash
go test ./internal/daemon/ -v
```

### 6c. Commit

```bash
git commit -m "feat(daemon): construct TmuxServer at startup, wire through subsystems"
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

```bash
git commit -m "refactor(session): update Manager to use TmuxServer for all tmux operations"
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

- `tmux.KillSession(ctx, tmuxSess)` → `m.server.KillSession(ctx, tmuxSess)` (lines 111, 386)
- `tmux.SessionExists(...)` → `m.server.SessionExists(...)` (lines 137, 197, 244)
- `tmux.CreateSession(...)` → `m.server.CreateSession(...)` (lines 208, 253)

### 8b. Run tests

```bash
go test ./internal/floormanager/ -v
```

### 8c. Commit

```bash
git commit -m "refactor(floormanager): use TmuxServer for tmux operations"
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

### 9b. Run tests

```bash
go test ./internal/session/ -v
```

### 9c. Commit

```bash
git commit -m "feat(session): LocalSource uses TmuxServer for socket-aware control mode attach"
```

---

## Step 10: Update dashboard `Server` to use `TmuxServer`

**File**: `internal/dashboard/server.go`

### 10a. Write implementation

Add `*tmux.TmuxServer` field to `Server` struct. Wire from daemon.

### 10b. Migrate dashboard callers

**File**: `internal/dashboard/handlers_debug_tmux.go` — use `s.tmuxServer.ListSessions(ctx)` instead of shelling out.

**File**: `internal/dashboard/handlers_environment.go` — use `s.tmuxServer.ShowEnvironment(ctx)` and `s.tmuxServer.SetEnvironment(ctx, key, value)`. Remove `SetBaseline`/`CleanTmuxServerEnv` calls.

**File**: `internal/dashboard/websocket.go` — update fallback paths:

- `tmux.CaptureLastLines(...)` → `s.tmuxServer.CaptureLastLines(...)`
- `tmux.GetCursorState(...)` → `s.tmuxServer.GetCursorState(...)`

### 10c. Run tests

```bash
go test ./internal/dashboard/ -v
```

### 10d. Commit

```bash
git commit -m "refactor(dashboard): use TmuxServer for all tmux operations"
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

Add `SendTmuxKeyName` delegating method to `SessionRuntime` if not already added.

### 11b. Run tests

```bash
go test ./internal/dashboard/ -v
```

### 11c. Commit

```bash
git commit -m "refactor(dashboard): handlers_tell uses SessionRuntime instead of tmux CLI"
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

### 12b. Run tests

```bash
go test ./internal/dashboard/ -v
```

### 12c. Commit

```bash
git commit -m "refactor(dashboard): handlers_capture uses SessionRuntime instead of tmux CLI"
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

```bash
git commit -m "refactor(floormanager): injector uses SessionRuntime instead of tmux CLI"
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

### 14c. Commit

```bash
git commit -m "refactor(floormanager): manager uses SessionRuntime for SendKeys"
```

---

## Step 15: Update attach commands and frontend

### 15a. Go side

**File**: `internal/tmux/tmux.go` — `TmuxServer.GetAttachCommand` returns `tmux -L schmux attach -t "=SESSION"`.

**File**: `cmd/schmux/attach.go` — update attach command builder and parser to include `-L schmux`.

### 15b. Frontend

**File**: `assets/dashboard/src/routes/HomePage.tsx` — update help text at lines 689, 1367.

**File**: `assets/dashboard/src/routes/tips-page/tmux-tab.tsx` — update line 101.

### 15c. Run tests

```bash
go test ./cmd/schmux/ -run TestAttach -v
./test.sh --quick
```

### 15d. Commit

```bash
git commit -m "feat: update attach commands with -L schmux flag"
```

---

## Step 16: Remove eliminated package-level functions and globals

**File**: `internal/tmux/tmux.go`

### 16a. Write implementation

Remove:

- Package-level `binary` var, `SetBinary()`, `Binary()` (package-level), `SetLogger()`
- `TmuxChecker` global, `NewDefaultChecker()`, `Checker` interface, `defaultChecker` struct
- `SetBaseline()`, `CleanTmuxServerEnv()`, `baselineMu`, `baselineKeys`, `nestingEnvVars`, `tmuxManagedKeys`
- `SendKeys()`, `SendLiteral()` (package-level)
- Dead code: `IsPaneDead`, `SetWindowSizeManual`, `GetWindowSize`, `ResizeWindow` (package-level), `GetCursorPosition` (package-level)

Keep: `ValidateBinary`, `StripAnsi`, `IsAgentStatusLine`, `ExtractLatestResponse` and its helpers, `CursorState` struct, `MaxExtractedLines`.

### 16b. Verify no remaining callers of removed functions

```bash
# Should return zero results for each:
grep -rn 'tmux\.SetBinary\|tmux\.SetLogger\|tmux\.TmuxChecker\|tmux\.SetBaseline\|tmux\.CleanTmuxServerEnv\|tmux\.SendKeys\|tmux\.SendLiteral\|tmux\.IsPaneDead' internal/ cmd/
```

### 16c. Run full test suite

```bash
go test ./...
```

### 16d. Commit

```bash
git commit -m "chore(tmux): remove eliminated package-level functions and globals"
```

---

## Step 17: Update E2E tests

**File**: `internal/e2e/e2e.go`

### 17a. Write implementation

Update all bare `tmux` commands to include `-L schmux`:

- Lines 551, 560: `exec.CommandContext(ctx, "tmux", "send-keys", ...)` → add `-L schmux` after `"tmux"`
- Lines 894, 1019: `exec.CommandContext(ctx, "tmux", "ls")` → add `-L schmux`

**File**: `internal/e2e/e2e_test.go`:

- Lines 104, 109: `exec.Command("tmux", "start-server")` and `"new-session"` → add `-L schmux`

**File**: `internal/floormanager/injector_test.go`:

- Migrate `tmux.SendLiteral()` and `tmux.CaptureOutput()` to `TmuxServer` methods.

**File**: `test/scenarios/generated/helpers-terminal.ts`:

- Update regex at line 24 to handle `-L schmux` in attach command.

### 17b. Run E2E tests

```bash
./test.sh --e2e
```

### 17c. Commit

```bash
git commit -m "test: update E2E and scenario tests for tmux socket isolation"
```

---

## Step 18: Update `schmux status` output

**File**: `cmd/schmux/status.go` (or equivalent)

### 18a. Write implementation

Add tmux socket info to status output: "tmux socket: schmux (inspect with `tmux -L schmux ls`)"

### 18b. Commit

```bash
git commit -m "feat(cli): show tmux socket name in status output"
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
6. Copy attach command, run it — verify it connects
7. Check `/environment` page works
8. Stop daemon: `./schmux stop`

### 19c. Commit

```bash
git commit -m "chore: final verification of tmux isolation"
```

---

## Task Dependencies

| Group | Steps       | Can Parallelize              | Notes                                       |
| ----- | ----------- | ---------------------------- | ------------------------------------------- |
| 1     | Steps 1-2   | No (sequential)              | Foundation: `TmuxServer` struct             |
| 2     | Steps 3-4   | Yes (independent of Group 1) | `ControlSource` interface changes           |
| 3     | Step 5      | No (depends on Group 2)      | Rename `SessionTracker` → `SessionRuntime`  |
| 4     | Steps 6-10  | No (sequential, wiring)      | Wire `TmuxServer` through system            |
| 5     | Steps 11-14 | Yes (independent callers)    | Migrate runtime callers to `SessionRuntime` |
| 6     | Steps 15-16 | Yes (independent)            | Attach commands + cleanup                   |
| 7     | Steps 17-18 | Yes (independent)            | E2E tests + status output                   |
| 8     | Step 19     | No (final)                   | End-to-end verification                     |

Groups 1 and 2 can run in parallel. Groups 5, 6, and 7 can run in parallel after Group 4 completes.
