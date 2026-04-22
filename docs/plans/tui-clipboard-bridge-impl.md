# Plan: TUI → Browser Clipboard Bridge (OSC 52, server-extracted, user-in-loop)

**Goal:** When a TUI inside a schmux tmux session writes to the clipboard via OSC 52, the dashboard surfaces a sanitized confirmation prompt; the user clicks Approve to commit the write to their browser system clipboard.

**Source-of-truth design:** [`docs/specs/tui-clipboard-bridge.md`](../specs/tui-clipboard-bridge.md). Read it first. This plan executes that design.

**Architecture:** Server-side OSC 52 extraction at `SessionRuntime.fanOut` (the single byte chokepoint) before bytes hit `outputLog.Append`. Daemon owns canonical pending-clipboard state per session, broadcasts over `/ws/dashboard`, debounces 200ms in the broadcast layer. Frontend banner + per-session badge. Approve does `navigator.clipboard.writeText` then POSTs an ack with `requestId` for staleness detection.

**Tech stack:** Go 1.x backend, React 18 + TypeScript + Vite frontend (xterm.js for terminals), Vitest + React Testing Library, Playwright scenarios under Docker.

---

## Critical reading before starting

Before any task, read in order:

1. `docs/specs/tui-clipboard-bridge.md` (entire spec, source of truth)
2. `CLAUDE.md` — especially the warnings on `./test.sh` vs `./test.sh --quick`, `go run ./cmd/build-dashboard` vs npm, `/commit` for commits
3. `internal/session/tracker.go:222-246` (the `fanOut` chokepoint)
4. `internal/dashboard/websocket.go:558-563, 920, 1013` (the zero-length-frame patterns and the two handlers we'll patch)
5. `internal/remote/connection.go:643-755` (the `waitForControlMode` post-handshake block)
6. `internal/daemon/daemon.go:213-230, 578, 1003-1011` (StartServer call sites)

**Test commands** for every step:

- Backend Go unit: `./test.sh --quick` (faster) or `go test ./internal/<package>/...` for targeted runs.
- Full backend + frontend: `./test.sh --quick`.
- Pre-commit gate (always before the final commit of each step group): `./test.sh` (NOT `--quick`).
- Scenarios (Docker): `./test.sh --scenarios`.
- TypeScript types regen after Go contract changes: `go run ./cmd/gen-types`.
- Dashboard rebuild after frontend changes (only if exercising the embedded binary, NOT during dev): `go run ./cmd/build-dashboard`.

**Commit policy:** use `/commit` (NOT `git commit`) — it runs `./test.sh` + `./badcode.sh` + the doc gates.

---

## Task dependency graph

| Group | Steps                   | Depends on                    | Notes                                                                                                                                                                                                                                  |
| ----- | ----------------------- | ----------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A     | 1, 2                    | (none)                        | Independent: `SetServerOption` on `*tmux.TmuxServer` and on `*controlmode.Client`, different files. Parallelizable.                                                                                                                    |
| B     | 3, 4, 5                 | A (Steps 1–2 for the helpers) | Step 5 (CR/FM zero-length fix) is independent of Steps 3 and 4 (different files); all three can be parallel.                                                                                                                           |
| C     | 6                       | A + B                         | Wires the helpers into daemon startup. Single sequential step touching `daemon.go` and `connection.go`.                                                                                                                                |
| D     | 7–15                    | (none)                        | Self-contained `osc52Extractor` package + tests. Could in principle land before any of A/B/C since it's pure Go with no daemon coupling, but bundling here keeps the PR sequence coherent. Each sub-step is sequential within Group D. |
| E     | 16a–16d, 17, 18, 19, 20 | C + D                         | Wires extractor into `SessionRuntime`, contracts, dashboard server state, ack endpoint, WS-reconnect snapshot. Sequential.                                                                                                             |
| F     | 22, 23, 24              | E (broadcast + endpoint)      | Frontend layers. Step 22 (context) blocks 23 (banner) which blocks 24 (sidebar).                                                                                                                                                       |
| G     | 26                      | F                             | Manual verification.                                                                                                                                                                                                                   |
| H     | 27                      | G                             | Playwright scenarios.                                                                                                                                                                                                                  |

---

## Group A — Server-option helpers (local + remote)

### Step 1: Add `SetServerOption` to `*tmux.TmuxServer`

**Files:**

- Modify: `internal/tmux/tmux.go`
- Modify: `internal/tmux/tmux_test.go`

#### 1a. Read the existing `SetOption` precedent

Read `internal/tmux/tmux.go:195-206` (the `SetOption` method) and `internal/tmux/tmux_test.go:218-226` (the existing test pattern for option-builder).

#### 1b. Write failing test

Mirrors the existing `TestTmuxServerSetOptionArgs` pattern at `internal/tmux/tmux_test.go:221-229`. Add:

```go
func TestTmuxServerSetServerOptionArgs(t *testing.T) {
    srv := NewTmuxServer("/usr/bin/tmux", "test-sock", nil)
    // Mirror the SetServerOption implementation: set-option -s <opt> <val>
    cmd := srv.cmd(context.Background(), "set-option", "-s", "set-clipboard", "external")
    want := []string{"-L", "test-sock", "set-option", "-s", "set-clipboard", "external"}
    if got := cmd.Args[1:]; !reflect.DeepEqual(got, want) {
        t.Errorf("args = %v, want %v", got, want)
    }
}
```

This test mirrors the existing precedent (it asserts `cmd()` builds the right args, since `SetServerOption` itself runs the command which we can't easily fake here). Run: `go test ./internal/tmux/ -run TestTmuxServerSetServerOptionArgs` — passes immediately. (The functional behavior of `SetServerOption` is implicitly covered when it's wired in Step 6 / Group H scenario tests.)

#### 1c. Implement `SetServerOption`

Add to `internal/tmux/tmux.go` immediately below `SetOption`:

```go
// SetServerOption sets a tmux server-scope option (set-option -s).
// Server-scope options are global to a tmux server and are not associated
// with any session. Used for options like set-clipboard and terminal-features
// that apply to the whole server.
func (s *TmuxServer) SetServerOption(ctx context.Context, option, value string) error {
    cmd := s.cmd(ctx, "set-option", "-s", option, value)
    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("failed to set server option %s: %w: %s", option, err, string(output))
    }
    return nil
}
```

#### 1d. Verify

```bash
go test ./internal/tmux/...
```

#### 1e. Commit (group with Step 2 below)

---

### Step 2: Add `SetServerOption` to `*controlmode.Client`

**Files:**

- Modify: `internal/remote/controlmode/client.go`
- Modify: `internal/remote/controlmode/client_test.go`

#### 2a. Read the existing `SetOption` at `internal/remote/controlmode/client.go:570-574`

Verbatim:

```go
func (c *Client) SetOption(ctx context.Context, option, value string) error {
    _, _, err := c.Execute(ctx, fmt.Sprintf("set-option %s %s", option, value))
    return err
}
```

`Execute` returns `(string, time.Duration, error)` (stdout, latency, err) — verified at `internal/remote/controlmode/client.go:173`. No stderr return. No scope flag in `SetOption` either — defaults to session-scope, which is correct for the existing `window-size manual` use but wrong for server-scope options like `set-clipboard`.

#### 2b. Add a test capture seam

`internal/remote/controlmode/client_test.go` does not have a precedent for capturing `Execute` invocations against a fake. Two options:

- **(preferred)** Add a small `executor` interface so `SetOption` and `SetServerOption` route through it; in production `*Client` is its own executor (calls `c.Execute`); in tests, supply a recording fake. This is a minimal refactor: one interface and a one-line method-receiver change.
- **(fallback)** Skip the unit test and rely on the integration-level test added at Step 6 (which exercises the helper from `applyRemoteTmuxDefaults` end-to-end via a fake Client) plus Group H scenarios.

Pick option 1 unless the executor refactor turns out to disrupt other call sites. Inspect `c.Execute` users in `controlmode/client.go` first; if widespread, fall back to option 2.

If option 1: define

```go
type cmdExecutor interface {
    Execute(ctx context.Context, cmd string) (string, time.Duration, error)
}
```

in a test helper file or production file (your call), and have `SetServerOption` call it.

#### 2c. Write failing test (assuming option 1)

```go
import "time"

type recordingExec struct { cmds []string }
func (r *recordingExec) Execute(_ context.Context, cmd string) (string, time.Duration, error) {
    r.cmds = append(r.cmds, cmd)
    return "", 0, nil
}

func TestClientSetServerOption(t *testing.T) {
    rec := &recordingExec{}
    if err := setServerOptionVia(rec, context.Background(), "set-clipboard", "external"); err != nil {
        t.Fatal(err)
    }
    want := []string{"set-option -s set-clipboard external"}
    if !reflect.DeepEqual(rec.cmds, want) {
        t.Errorf("cmds = %v, want %v", rec.cmds, want)
    }
}
```

where `setServerOptionVia` is a small unexported helper that the public `SetServerOption` method delegates to. Keeps the testable seam tiny.

#### 2d. Implement `SetServerOption`

Add to `internal/remote/controlmode/client.go` next to `SetOption`:

```go
// SetServerOption sets a tmux server-scope option (set-option -s).
// See *tmux.TmuxServer.SetServerOption for rationale.
func (c *Client) SetServerOption(ctx context.Context, option, value string) error {
    return setServerOptionVia(c, ctx, option, value)
}

func setServerOptionVia(e cmdExecutor, ctx context.Context, option, value string) error {
    _, _, err := e.Execute(ctx, fmt.Sprintf("set-option -s %s %s", option, value))
    return err
}
```

#### 2e. Verify

```bash
go test ./internal/remote/controlmode/...
```

#### 2f. Commit Steps 1+2 together

```bash
/commit
```

Suggested message:

```
feat(tmux): add SetServerOption for server-scope tmux options

Both *tmux.TmuxServer and *controlmode.Client gain a SetServerOption
method that issues `set-option -s`. Required for set-clipboard and
terminal-features which are server-scoped, not session-scoped.

Pure addition, no behavior change yet.
```

---

## Group B — Apply-defaults helpers, CR/FM zero-length fix, extractor scaffolding

### Step 3: `ApplyTmuxServerDefaults` helper + interface seam

**Files:**

- Create: `internal/tmux/defaults.go`
- Create: `internal/tmux/defaults_test.go`

#### 3a. Define interface and write failing test

`internal/tmux/defaults_test.go`:

```go
package tmux

import (
    "context"
    "testing"
)

type fakeOptionSetter struct {
    calls [][2]string // [opt, value]
}

func (f *fakeOptionSetter) SetServerOption(_ context.Context, opt, value string) error {
    f.calls = append(f.calls, [2]string{opt, value})
    return nil
}

func TestApplyTmuxServerDefaultsSetsExpectedOptions(t *testing.T) {
    f := &fakeOptionSetter{}
    ApplyTmuxServerDefaults(context.Background(), f, nil) // nil logger ok
    want := [][2]string{
        {"set-clipboard", "external"},
        {"terminal-features", "*:clipboard"},
    }
    if len(f.calls) != len(want) {
        t.Fatalf("got %d calls, want %d", len(f.calls), len(want))
    }
    for i, c := range f.calls {
        if c != want[i] {
            t.Errorf("call %d = %v, want %v", i, c, want[i])
        }
    }
}
```

Run: `go test ./internal/tmux/ -run TestApplyTmuxServerDefaultsSetsExpectedOptions` — expect compile error.

#### 3b. Implement helper

`internal/tmux/defaults.go`:

```go
package tmux

import (
    "context"

    "github.com/charmbracelet/log"
)

// tmuxServerOptionSetter is the minimal surface ApplyTmuxServerDefaults needs.
// *TmuxServer satisfies it. Carved out as an interface so daemon-startup code
// can be tested with a fake recorder.
type tmuxServerOptionSetter interface {
    SetServerOption(ctx context.Context, option, value string) error
}

// ApplyTmuxServerDefaults sets server-scope options every schmux-owned tmux
// server should have:
//   - set-clipboard external: forward OSC 52 from inner panes out to the daemon
//     PTY without keeping a tmux-internal copy of every yanked secret.
//   - terminal-features '*:clipboard': bypass tmux's outer-terminal Ms-capability
//     check so OSC 52 forwarding works regardless of the daemon's TERM.
//
// Errors are logged and swallowed — these options are belt-and-braces; failure
// must not prevent server startup. Pre-existing tmux servers schmux did not
// start will still receive these options for as long as they live; they may
// drop the options if killed and restarted outside schmux's control.
func ApplyTmuxServerDefaults(ctx context.Context, srv tmuxServerOptionSetter, logger *log.Logger) {
    options := [][2]string{
        {"set-clipboard", "external"},
        {"terminal-features", "*:clipboard"},
    }
    for _, opt := range options {
        if err := srv.SetServerOption(ctx, opt[0], opt[1]); err != nil {
            if logger != nil {
                logger.Warn("ApplyTmuxServerDefaults: failed to set option",
                    "option", opt[0], "value", opt[1], "err", err)
            }
        }
    }
}
```

#### 3c. Verify

```bash
go test ./internal/tmux/...
```

---

### Step 4: `applyRemoteTmuxDefaults` helper

**Files:**

- Create: `internal/remote/defaults.go`
- Create: `internal/remote/defaults_test.go`

**Important:** `*controlmode.Client` does NOT have `SetGlobalOption` or `Setenv` methods. The current behavior at `internal/remote/connection.go:736-746` uses `c.client.SetOption(ctx, "window-size", "manual")` (session-scope, no `-g`) and `c.client.Execute(ctx, "setenv -g DISPLAY :99")` (raw command). Mirror that exactly to avoid changing semantics — only the new `set-clipboard external` and `terminal-features *:clipboard` are added via the new `SetServerOption`.

#### 4a. Failing test

```go
package remote

import (
    "context"
    "reflect"
    "testing"
    "time"
)

type fakeRemoteClient struct {
    setOptCalls       [][2]string // SetOption (session-scope)
    setServerOptCalls [][2]string // SetServerOption (server-scope)
    execCalls         []string    // raw Execute commands
}

func (f *fakeRemoteClient) SetOption(_ context.Context, opt, val string) error {
    f.setOptCalls = append(f.setOptCalls, [2]string{opt, val})
    return nil
}
func (f *fakeRemoteClient) SetServerOption(_ context.Context, opt, val string) error {
    f.setServerOptCalls = append(f.setServerOptCalls, [2]string{opt, val})
    return nil
}
func (f *fakeRemoteClient) Execute(_ context.Context, cmd string) (string, time.Duration, error) {
    f.execCalls = append(f.execCalls, cmd)
    return "", 0, nil
}

func TestApplyRemoteTmuxDefaults(t *testing.T) {
    f := &fakeRemoteClient{}
    applyRemoteTmuxDefaults(context.Background(), f, nil)

    wantServer := [][2]string{
        {"set-clipboard", "external"},
        {"terminal-features", "*:clipboard"},
    }
    if !reflect.DeepEqual(f.setServerOptCalls, wantServer) {
        t.Errorf("server-option calls = %v, want %v", f.setServerOptCalls, wantServer)
    }
    wantSession := [][2]string{{"window-size", "manual"}}
    if !reflect.DeepEqual(f.setOptCalls, wantSession) {
        t.Errorf("session-option calls = %v, want %v", f.setOptCalls, wantSession)
    }
    wantExec := []string{"setenv -g DISPLAY :99"}
    if !reflect.DeepEqual(f.execCalls, wantExec) {
        t.Errorf("execute calls = %v, want %v", f.execCalls, wantExec)
    }
}
```

#### 4b. Implement helper

`internal/remote/defaults.go`:

```go
package remote

import (
    "context"

    "github.com/charmbracelet/log"
)

// remoteTmuxClient is the minimal surface applyRemoteTmuxDefaults needs.
// *controlmode.Client satisfies it (SetOption and Execute already exist;
// SetServerOption is added in Step 2).
type remoteTmuxClient interface {
    SetOption(ctx context.Context, option, value string) error
    SetServerOption(ctx context.Context, option, value string) error
    Execute(ctx context.Context, cmd string) (string, time.Duration, error)
}

// applyRemoteTmuxDefaults applies all options every remote tmux server should
// have. Called from waitForControlMode (which itself is called from both
// connect() and Reconnect(), so the options are re-applied if the remote tmux
// server is restarted).
//
// Replaces the inline option block previously at
// internal/remote/connection.go:736-746. Behavior preservation:
//   - window-size manual: session-scope (matches existing SetOption call, no -g).
//   - DISPLAY :99: raw setenv -g (matches existing Execute).
//   - set-clipboard / terminal-features: NEW; server-scope via SetServerOption.
func applyRemoteTmuxDefaults(ctx context.Context, c remoteTmuxClient, logger *log.Logger) {
    serverOpts := [][2]string{
        {"set-clipboard", "external"},
        {"terminal-features", "*:clipboard"},
    }
    for _, o := range serverOpts {
        if err := c.SetServerOption(ctx, o[0], o[1]); err != nil && logger != nil {
            logger.Warn("applyRemoteTmuxDefaults: server option", "opt", o[0], "err", err)
        }
    }
    if err := c.SetOption(ctx, "window-size", "manual"); err != nil && logger != nil {
        logger.Warn("applyRemoteTmuxDefaults: window-size", "err", err)
    }
    if _, _, err := c.Execute(ctx, "setenv -g DISPLAY :99"); err != nil && logger != nil {
        logger.Warn("applyRemoteTmuxDefaults: DISPLAY", "err", err)
    }
}
```

#### 4c. Verify

```bash
go test ./internal/remote/...
```

---

### Step 5: CR/FM zero-length-frame fix in dashboard websocket

**Files:**

- Modify: `internal/dashboard/websocket.go`
- Modify: `internal/dashboard/websocket_test.go` (or new file if needed)

#### 5a. Read the precedent

Read `internal/dashboard/websocket.go:540-580` (main handler, especially the comment at lines 558-562 explaining why empty frames must be sent). Read `:910-935` (CR handler) and `:1003-1025` (FM handler) — both have `if len(event.Data) == 0 { continue }` that we need to replace.

#### 5b. Read the real precedent

Read the main handler at `internal/dashboard/websocket.go:549-567` carefully. The shape:

```go
if len(event.Data) > 0 {
    send, hb, so := escbuf.SplitClean(escScratch, escHoldback, []byte(event.Data))
    escHoldback = hb
    escScratch = so
    // ... ringBuf write ...
    // Always send a frame to preserve sequence continuity, even when
    // escbuf holds back the entire event (send is empty). Without this,
    // the skipped seq creates a phantom gap on the frontend...
    frameBuf = appendSequencedFrame(frameBuf, event.Seq, send)
    if err := conn.WriteMessage(websocket.BinaryMessage, frameBuf); err != nil {
        tracker.Counters.WsWriteErrors.Add(1)
        return
    }
}
```

Note the comment at `:558-562` explaining why the frame is sent even when `send` is empty.

The CR handler at `:910-936` and FM handler at `:1003-1025` use the same `escbuf.SplitClean` + `appendSequencedFrame` + `conn.WriteMessage(websocket.BinaryMessage, ...)` pattern, BUT each gates frame-emission on `len(send) > 0` (CR) or `len(event.Data) == 0` early-skip (both). They also need `lastSeq = event.Seq` updates so the deferred-flush at handler exit (`:915-917`) carries the right seq.

The fix is to emit a frame on every event including zero-length ones — same comment as the main handler, same code shape.

#### 5c. Implement: CR handler

In `internal/dashboard/websocket.go:920` replace:

```go
if len(event.Data) == 0 {
    continue
}
lastSeq = event.Seq
send, hb, so := escbuf.SplitClean(escScratch, escHoldback, []byte(event.Data))
escHoldback = hb
escScratch = so
if len(send) > 0 {
    frameBuf = appendSequencedFrame(frameBuf, event.Seq, send)
    if err := conn.WriteMessage(websocket.BinaryMessage, frameBuf); err != nil {
        return
    }
}
```

with:

```go
lastSeq = event.Seq
send, hb, so := escbuf.SplitClean(escScratch, escHoldback, []byte(event.Data))
escHoldback = hb
escScratch = so
// Always send a frame to preserve sequence continuity, even when escbuf
// holds back all bytes or the upstream event was zero-length (e.g. an
// OSC 52 sequence the daemon-side extractor consumed in full). Mirrors
// the main handler at websocket.go:558-562; see comment there.
frameBuf = appendSequencedFrame(frameBuf, event.Seq, send)
if err := conn.WriteMessage(websocket.BinaryMessage, frameBuf); err != nil {
    return
}
```

#### 5d. Implement: FM handler

Mirror the same change at the FM handler (`:1003-1025`). Read the existing block first to pick up any FM-specific differences (e.g., its handling of `controlChan` or `tracker.Counters.WsWriteErrors`), preserve those, change only the zero-length skip → unconditional emit.

#### 5e. Tests

`internal/dashboard/websocket_test.go` does NOT currently have a harness for the CR/FM streaming loops, so a fully-isolated unit test is not in scope here without a substantial refactor. Two pragmatic options:

- **(preferred)** Cover via the Group H scenario tests: a session that writes only OSC 52 produces zero-length stripped events; the scenario test exercises `daemon → CR/FM stream → xterm.js` end-to-end. If the frontend's gap detector fires (because zero-length frames were skipped), the scenario will surface a duplicate-write or cursor-jump symptom and fail.
- **(supplementary)** Add a small static check: a Go test that greps the CR/FM handler bodies for the string `if len(event.Data) == 0` and fails if found. Brittle but cheap regression guard:

```go
func TestCRFMHandlersDoNotSkipZeroLengthEvents(t *testing.T) {
    src, err := os.ReadFile("websocket.go")
    if err != nil { t.Fatal(err) }
    if bytes.Contains(src, []byte("if len(event.Data) == 0 {")) {
        t.Error("CR/FM handler still contains zero-length skip; this re-introduces the phantom-gap bug")
    }
}
```

This catches a regression without needing a real harness. Acceptable for v1.

Pick (preferred + supplementary). If the executor decides to build a real CR/FM harness later, it's its own follow-up.

#### 5f. Verify all dashboard tests still pass

```bash
go test ./internal/dashboard/...
```

#### 5g. Commit Group B together

```bash
/commit
```

Suggested message:

```
feat(tmux,dashboard): server-option defaults helpers + CR/FM zero-length frame fix

- Add ApplyTmuxServerDefaults (local) and applyRemoteTmuxDefaults (remote)
  to centralize "every schmux-owned tmux server gets these options".
  Not yet wired in.
- Fix CR/FM websocket handlers to forward zero-length frames instead of
  skipping, matching the main handler's existing precedent. Without this,
  any zero-byte event triggers a phantom gap on the frontend; latent bug
  exposed by the upcoming OSC 52 extractor (which routinely produces
  empty post-strip output for clean nvim yanks).
```

---

## Group C — Wire helpers + TERM env into daemon startup

### Step 6: Wire defaults at all server-start sites + set TERM

**Files:**

- Modify: `internal/daemon/daemon.go`
- Modify: `internal/remote/connection.go`
- Modify: `internal/daemon/daemon_test.go` (light test for default-socket wiring)

#### 6a. Local: default-socket explicit start

The default-socket `tmuxServer` is constructed in `initConfigAndState` and returned via `daemonInit` (`internal/daemon/daemon.go:578`). It's unpacked in `Run()` at the line near `tmuxServer := di.tmuxServer` (~`daemon.go:389` area — verify exact line by reading `Run()` from line 376 down).

Insert immediately after `tmuxServer` is unpacked, BEFORE any code path that may spawn a tmux child:

```go
// Ensure the default-socket tmux server is running and has our server-scope
// option defaults. StartServer is idempotent; under `schmux start` the parent
// shim already started it (daemon.go:217), under `daemon-run` (dev mode) it
// has not been started yet.
if err := tmuxServer.StartServer(d.shutdownCtx); err != nil {
    logger.Warn("StartServer for default socket failed", "err", err)
}
tmux.ApplyTmuxServerDefaults(d.shutdownCtx, tmuxServer, logger)
```

Note: `ApplyTmuxServerDefaults` from Step 3 is package-private (`internal/tmux`). Either export it as `ApplyTmuxServerDefaults` (preferred) or move the wiring code into the `internal/tmux` package and have the daemon call a wrapper. **Pick: export it** — pure addition, no leak.

Adjust Step 3 so the helper is named `ApplyTmuxServerDefaults` (capital A) from the start. Update the Step 3 test to use the exported name.

(Variable names — `tmuxServer`, `logger`, `d.shutdownCtx` — verified in nearby code; adapt if locals differ.)

#### 6b. Local: pair with restored-socket loop at daemon.go:1003-1011

Read the existing loop:

```go
for socket := range activeSocketSet {
    if socket == cfg.GetTmuxSocketName() {
        continue
    }
    srv := tmux.NewTmuxServer(tmuxBin, socket, nil)
    if err := srv.StartServer(d.shutdownCtx); err != nil {
        logger.Warn("failed to start tmux server for socket", "socket", socket, "err", err)
    }
}
```

Add immediately after `StartServer`:

```go
tmux.ApplyTmuxServerDefaults(d.shutdownCtx, srv, logger)
```

#### 6c. Local: TERM environment

Read `internal/daemon/daemon.go:226-236` (the daemon fork in `Start()`).

Modify the forked command to ensure `TERM=xterm-256color` is in `cmd.Env`:

```go
cmd.Env = append(os.Environ(), "SCHMUX_HOME="+schmuxHome)
// Ensure TERM is set so tmux's outer-terminal capability check passes for
// OSC 52 forwarding even when the parent (launchd, cron, daemon-run, ...)
// has no TERM. Override only if not already set so we don't downgrade a
// richer TERM.
if os.Getenv("TERM") == "" {
    cmd.Env = append(cmd.Env, "TERM=xterm-256color")
}
```

In `Run()`, also set `os.Setenv("TERM", "xterm-256color")` if `os.Getenv("TERM") == ""` so that tmux children spawned by the daemon process inherit it. Place near the top of `Run()`, before any `tmux` subprocess can be spawned.

#### 6d. Remote: replace inline block with helper call

Read `internal/remote/connection.go:706-755` (the post-handshake block inside `waitForControlMode`). The existing block uses `c.client.SetOption(...)` and `c.client.Execute(...)` — `c` is `*Connection` and `c.client` is `*controlmode.Client`.

Replace lines 736-746 (the existing `c.client.SetOption(ctx, "window-size", "manual")` + `c.client.Execute(ctx, "setenv -g DISPLAY :99")` lines) with one call:

```go
applyRemoteTmuxDefaults(ctx, c.client, c.logger)
```

(Verify the actual logger field name on `*Connection` — likely `logger` or `c.log` — by reading nearby code at lines 710-735. Adapt if it differs.)

#### 6e. Optional: small test for wiring (or defer to E2E)

If a daemon-construction test seam exists, add a test that drives `Run()` (or its early init segment) with a fake `tmuxServerOptionSetter` and asserts `ApplyTmuxServerDefaults` was called. If creating the seam is more disruptive than the spec's "(b) E2E fallback" budget allows, skip and rely on Group H's scenario tests to catch regression.

#### 6f. Verify

```bash
go test ./...
```

If running locally with a real daemon:

```bash
go build ./cmd/schmux
./schmux daemon-run --foreground &
DAEMON=$!
sleep 1
tmux -L "$(grep tmuxSocketName ~/.schmux/config.json | head -1 | awk -F'"' '{print $4}')" show-options -s set-clipboard
# expect: set-clipboard external
kill $DAEMON
```

#### 6g. Commit Group C

```bash
/commit
```

Suggested message:

```
feat(daemon,remote): apply server-scope tmux defaults at all start sites

Wire ApplyTmuxServerDefaults at default-socket start + restored-session
socket loop in daemon.Run(). Wire applyRemoteTmuxDefaults inside
waitForControlMode (replacing the inline option block; both connect()
and Reconnect() benefit).

Set TERM=xterm-256color in the daemon's env when unset so tmux's outer-
terminal capability check passes regardless of how the daemon was launched.

Local sessions now have OSC 52 forwarded by tmux. The daemon does not yet
extract OSC 52 — bytes flow into xterm.js as raw escape sequences (ignored
by xterm.js, harmless interim state).
```

---

## Group D — OSC 52 extractor package (TDD, granular)

### Step 7: Define `ClipboardRequest` type and fixture helpers

**Files:**

- Create: `internal/session/osc52.go`
- Create: `internal/session/osc52_test.go`

#### 7a. Skeleton

`internal/session/osc52.go`:

```go
package session

import (
    "encoding/base64"
    "regexp"
    "time"

    "github.com/google/uuid"
)

// ClipboardRequest is one OSC 52 write extracted from a session's byte stream.
// Sent over SessionRuntime.clipboardCh; consumed by the dashboard server.
type ClipboardRequest struct {
    SessionID            string
    RequestID            string // UUID; new per emit
    Text                 string // post-defang
    ByteCount            int    // pre-defang decoded length
    StrippedControlChars int
    Timestamp            time.Time
}

// pcValidationRe enforces OSC 52 selection-char syntax. Empty Pc is allowed
// and means c+s; non-empty must consist of the documented selectors.
var pcValidationRe = regexp.MustCompile(`^[cpsqb0-7]+$`)

// maxOSC52DecodedSize bounds a single clipboard payload at 64 KiB.
const maxOSC52DecodedSize = 64 * 1024

// maxOSC52CarrySize bounds the carry buffer at the same 64 KiB; if a TUI
// emits an OSC 52 prefix without ever terminating, we eventually flush
// the carry to output to avoid unbounded growth.
const maxOSC52CarrySize = 64 * 1024
```

(No tests yet — this is type scaffolding; tests come step by step below.)

---

### Step 8: Plain-bytes pass-through

#### 8a. Failing test

`internal/session/osc52_test.go`:

```go
package session

import (
    "bytes"
    "testing"
)

func TestExtractor_PlainBytesPassThrough(t *testing.T) {
    e := newOSC52Extractor("sess-1")
    out, reqs := e.process([]byte("hello world"))
    if !bytes.Equal(out, []byte("hello world")) {
        t.Errorf("output = %q, want %q", out, "hello world")
    }
    if len(reqs) != 0 {
        t.Errorf("got %d requests, want 0", len(reqs))
    }
}
```

#### 8b. Minimal implementation

```go
type osc52Extractor struct {
    sessionID string
    carry     []byte
}

func newOSC52Extractor(sessionID string) *osc52Extractor {
    return &osc52Extractor{sessionID: sessionID}
}

// process consumes one chunk of session bytes. Returns:
//   - out: the bytes to forward to outputLog/subscribers, with OSC 52 stripped.
//   - reqs: zero or more ClipboardRequests extracted from this chunk (and any
//           that completed across the carry boundary).
func (e *osc52Extractor) process(input []byte) (out []byte, reqs []ClipboardRequest) {
    // For now: trivial passthrough. Subsequent steps add OSC 52 handling.
    return input, nil
}
```

Run: `go test ./internal/session/ -run TestExtractor_PlainBytesPassThrough` — passes.

---

### Step 9: Single-event OSC 52 with BEL terminator

#### 9a. Failing test

```go
func TestExtractor_SingleEventBEL(t *testing.T) {
    e := newOSC52Extractor("sess-1")
    in := []byte("\x1b]52;c;aGVsbG8=\x07") // base64("hello")
    out, reqs := e.process(in)
    if len(out) != 0 {
        t.Errorf("output = %q, want empty", out)
    }
    if len(reqs) != 1 {
        t.Fatalf("got %d requests, want 1", len(reqs))
    }
    r := reqs[0]
    if r.Text != "hello" || r.ByteCount != 5 || r.StrippedControlChars != 0 {
        t.Errorf("req = %+v, want Text=hello ByteCount=5 Stripped=0", r)
    }
    if r.SessionID != "sess-1" || r.RequestID == "" {
        t.Errorf("metadata wrong: %+v", r)
    }
}
```

#### 9b. Implement minimal OSC 52 detection

Replace `process` with the algorithm described in `docs/specs/tui-clipboard-bridge.md` §2 "Algorithm". For this step, only handle BEL-terminated OSC 52, no carry, no defang yet. Return-on-each-emit pattern:

```go
func (e *osc52Extractor) process(input []byte) (out []byte, reqs []ClipboardRequest) {
    out = make([]byte, 0, len(input))
    i := 0
    for i < len(input) {
        if hasOSC52Prefix(input[i:]) {
            // find BEL or ST in input[i+5:]
            end, termLen := findOSC52Terminator(input[i+5:])
            if end >= 0 {
                payload := input[i+5 : i+5+end]
                if req, ok := e.extractRequest(payload); ok {
                    reqs = append(reqs, req)
                }
                i = i + 5 + end + termLen
                continue
            }
            // Unterminated — carry path comes in step 11.
            // For now, emit nothing (defensive); tests for this case are step 11.
            return out, reqs
        }
        out = append(out, input[i])
        i++
    }
    return out, reqs
}

func hasOSC52Prefix(b []byte) bool {
    return len(b) >= 5 && b[0] == 0x1b && b[1] == ']' && b[2] == '5' && b[3] == '2' && b[4] == ';'
}

// findOSC52Terminator looks for BEL (0x07) or ST (ESC \). Returns the index of
// the terminator in b and its length, or (-1, 0) if not found.
func findOSC52Terminator(b []byte) (int, int) {
    for i := 0; i < len(b); i++ {
        if b[i] == 0x07 {
            return i, 1
        }
        if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
            return i, 2
        }
    }
    return -1, 0
}

func (e *osc52Extractor) extractRequest(payload []byte) (ClipboardRequest, bool) {
    // Split Pc;Pd
    semi := bytes.IndexByte(payload, ';')
    if semi < 0 {
        return ClipboardRequest{}, false
    }
    pc := payload[:semi]
    pd := payload[semi+1:]
    if len(pc) > 0 && !pcValidationRe.Match(pc) {
        return ClipboardRequest{}, false
    }
    if len(pd) == 0 || (len(pd) == 1 && pd[0] == '?') {
        return ClipboardRequest{}, false
    }
    decoded, err := base64.StdEncoding.DecodeString(string(pd))
    if err != nil {
        return ClipboardRequest{}, false
    }
    if len(decoded) > maxOSC52DecodedSize {
        return ClipboardRequest{}, false
    }
    // Defang comes in step 13. For now: accept bytes verbatim as text.
    return ClipboardRequest{
        SessionID: e.sessionID,
        RequestID: uuid.New().String(),
        Text:      string(decoded),
        ByteCount: len(decoded),
        Timestamp: time.Now(),
    }, true
}
```

Add the missing `bytes` import to `osc52.go`.

Run: `go test ./internal/session/ -run TestExtractor_SingleEventBEL` — passes.

---

### Step 10: ST terminator + surrounding bytes (regression coverage)

These tests are expected to PASS against the Step 9 implementation. Treat them as regression coverage for cases the algorithm should already handle, NOT as a fresh TDD cycle. If any fails, it indicates a Step 9 bug; fix the algorithm.

#### 10a. Tests

```go
func TestExtractor_ST_Terminator(t *testing.T) {
    e := newOSC52Extractor("s")
    out, reqs := e.process([]byte("\x1b]52;c;aGVsbG8=\x1b\\"))
    if len(out) != 0 || len(reqs) != 1 || reqs[0].Text != "hello" {
        t.Errorf("out=%q reqs=%+v", out, reqs)
    }
}

func TestExtractor_BeforeAndAfter(t *testing.T) {
    e := newOSC52Extractor("s")
    out, reqs := e.process([]byte("before\x1b]52;c;aGVsbG8=\x07after"))
    if string(out) != "beforeafter" {
        t.Errorf("out=%q want beforeafter", out)
    }
    if len(reqs) != 1 || reqs[0].Text != "hello" {
        t.Errorf("reqs=%+v", reqs)
    }
}

func TestExtractor_TwoAdjacent(t *testing.T) {
    e := newOSC52Extractor("s")
    in := []byte("\x1b]52;c;YQ==\x07\x1b]52;c;Yg==\x07") // a, b
    out, reqs := e.process(in)
    if len(out) != 0 || len(reqs) != 2 {
        t.Errorf("out=%q reqs=%+v", out, reqs)
    }
    if reqs[0].Text != "a" || reqs[1].Text != "b" {
        t.Errorf("texts=%q,%q", reqs[0].Text, reqs[1].Text)
    }
}
```

These should pass on the existing implementation from Step 9. Run them; if any fails, fix algorithm.

---

### Step 11: Cross-event carry with narrow OSC 52 prefix table

#### 11a. Failing tests

```go
func TestExtractor_CrossEventSplit(t *testing.T) {
    e := newOSC52Extractor("s")
    out1, reqs1 := e.process([]byte("\x1b]52;c;aGVsb"))
    if len(out1) != 0 || len(reqs1) != 0 {
        t.Errorf("event 1: out=%q reqs=%+v", out1, reqs1)
    }
    out2, reqs2 := e.process([]byte("G8=\x07"))
    if len(out2) != 0 {
        t.Errorf("event 2 out=%q want empty", out2)
    }
    if len(reqs2) != 1 || reqs2[0].Text != "hello" {
        t.Errorf("event 2 reqs=%+v", reqs2)
    }
}

func TestExtractor_CrossEventWithSurrounding(t *testing.T) {
    e := newOSC52Extractor("s")
    out1, _ := e.process([]byte("before\x1b]52;c;aGVsb"))
    out2, reqs := e.process([]byte("G8=\x07after"))
    if string(out1) != "before" || string(out2) != "after" {
        t.Errorf("out1=%q out2=%q", out1, out2)
    }
    if len(reqs) != 1 || reqs[0].Text != "hello" {
        t.Errorf("reqs=%+v", reqs)
    }
}

func TestExtractor_OtherOSCNotCarried(t *testing.T) {
    // Title OSC must NOT be held back across event boundaries; only OSC 52
    // partial prefixes carry.
    e := newOSC52Extractor("s")
    out, reqs := e.process([]byte("\x1b]0;new title\x07"))
    if string(out) != "\x1b]0;new title\x07" {
        t.Errorf("title OSC altered: out=%q", out)
    }
    if len(reqs) != 0 {
        t.Errorf("reqs=%+v", reqs)
    }
}

func TestExtractor_LoneEscNotHeld(t *testing.T) {
    // A lone trailing ESC is NOT part of the OSC 52 prefix table; it should
    // either be flushed or held briefly. Document chosen behavior.
    e := newOSC52Extractor("s")
    out, _ := e.process([]byte("foo\x1b"))
    // Expect the ESC to flush through (\x1b on its own is a meaningful key,
    // not part of OSC 52 prefix).
    if string(out) != "foo\x1b" {
        t.Errorf("out=%q want foo\\x1b", out)
    }
}
```

#### 11b. Implement carry buffer with narrow OSC 52 prefix table

In `osc52.go`, modify `process` to:

1. Prepend `e.carry` to the input.
2. Reset `e.carry = nil`.
3. When the scanner sees an unterminated OSC 52 (ran off the end of input mid-sequence), set `e.carry = input[i:]` and return.
4. When the scanner sees trailing bytes that match `\x1b]`, `\x1b]5`, `\x1b]52`, `\x1b]52;` (OSC 52 prefix candidates) AT THE END of input, hold them as carry. Anything else flushes (including lone `\x1b` and other OSC starts like `\x1b]0`).

Pseudocode for the trailing check after the main loop:

```go
// After the loop exits with i == len(input), check if `out` ends with one of
// the carry-worthy prefixes. If so, peel those bytes off into carry.
//
// Only carry these: \x1b]  \x1b]5  \x1b]52  \x1b]52;
// Do NOT carry: \x1b alone (it's a meaningful key), \x1b]0, \x1b]1, \x1b]9, etc.
```

Actually the cleanest implementation is: when `hasOSC52Prefix` matches at position i but there's no terminator before end-of-input, set `carry = input[i:]` and return without appending those bytes to `out`. Additionally, when the trailing bytes of input form a partial OSC-52-prefix match (`\x1b]`, `\x1b]5`, `\x1b]52`, `\x1b]52;`), do the same. Implement the partial-prefix check as a small helper:

```go
// osc52PartialPrefix returns true if b is one of:
//   \x1b]  \x1b]5  \x1b]52  \x1b]52;
// (not \x1b alone, not other OSC starts)
func osc52PartialPrefix(b []byte) bool {
    switch len(b) {
    case 2:
        return b[0] == 0x1b && b[1] == ']'
    case 3:
        return b[0] == 0x1b && b[1] == ']' && b[2] == '5'
    case 4:
        return b[0] == 0x1b && b[1] == ']' && b[2] == '5' && b[3] == '2'
    case 5:
        return b[0] == 0x1b && b[1] == ']' && b[2] == '5' && b[3] == '2' && b[4] == ';'
    }
    return false
}
```

In `process`, after the main loop, scan the tail of `out` for a 2-5 byte trailing OSC-52-partial-prefix. If found, peel it off into `e.carry`.

Add carry overflow guard: if `len(e.carry) > maxOSC52CarrySize`, flush `e.carry` to `out` and reset.

Run all `TestExtractor_*` tests:

```bash
go test ./internal/session/ -run TestExtractor
```

---

### Step 12: Validation rejections (invalid Pc, read query, oversize) (regression coverage)

These tests should PASS against the Step 9 implementation (which already validates Pc, rejects `?`, and enforces the size cap). Regression coverage, not fresh TDD. If any fails, the Step 9 implementation has a bug.

#### 12a. Tests

```go
func TestExtractor_EmptyPcAccepted(t *testing.T) {
    e := newOSC52Extractor("s")
    out, reqs := e.process([]byte("\x1b]52;;aGVsbG8=\x07"))
    if len(out) != 0 || len(reqs) != 1 || reqs[0].Text != "hello" {
        t.Errorf("out=%q reqs=%+v", out, reqs)
    }
}

func TestExtractor_InvalidPcRejected(t *testing.T) {
    e := newOSC52Extractor("s")
    out, reqs := e.process([]byte("\x1b]52;xyz;aGVsbG8=\x07"))
    if len(out) != 0 || len(reqs) != 0 {
        t.Errorf("out=%q reqs=%+v", out, reqs)
    }
}

func TestExtractor_ReadQueryRejected(t *testing.T) {
    e := newOSC52Extractor("s")
    _, reqs := e.process([]byte("\x1b]52;c;?\x07"))
    if len(reqs) != 0 {
        t.Errorf("reqs=%+v", reqs)
    }
}

func TestExtractor_OversizeRejected(t *testing.T) {
    e := newOSC52Extractor("s")
    big := bytes.Repeat([]byte{'a'}, maxOSC52DecodedSize+1)
    encoded := base64.StdEncoding.EncodeToString(big)
    in := append([]byte("\x1b]52;c;"), encoded...)
    in = append(in, 0x07)
    _, reqs := e.process(in)
    if len(reqs) != 0 {
        t.Errorf("reqs=%+v", reqs)
    }
}
```

These should pass against the Step 9 implementation (which already handles all four cases). If any fails, fix.

---

### Step 13: Byte-level defang

#### 13a. Failing test

```go
func TestExtractor_Defang(t *testing.T) {
    // Bytes: a \n b \x1b c \x07 d \x00 e
    // Expect: a \n b c d e (\n preserved; \x1b, \x07, \x00 stripped)
    payload := []byte{'a', '\n', 'b', 0x1b, 'c', 0x07, 'd', 0x00, 'e'}
    encoded := base64.StdEncoding.EncodeToString(payload)
    in := append([]byte("\x1b]52;c;"), encoded...)
    in = append(in, 0x07)
    e := newOSC52Extractor("s")
    _, reqs := e.process(in)
    if len(reqs) != 1 {
        t.Fatalf("reqs=%+v", reqs)
    }
    r := reqs[0]
    if r.Text != "a\nbcde" {
        t.Errorf("Text=%q want %q", r.Text, "a\nbcde")
    }
    if r.StrippedControlChars != 3 {
        t.Errorf("Stripped=%d want 3", r.StrippedControlChars)
    }
    if r.ByteCount != len(payload) {
        t.Errorf("ByteCount=%d want %d", r.ByteCount, len(payload))
    }
}
```

#### 13b. Implement defang in `extractRequest`

After `base64.DecodeString` and the size check:

```go
// Byte-level defang: strip C0 controls except \n (0x0a) and \t (0x09), plus DEL (0x7f).
// UTF-8 lead/continuation bytes are >= 0x80 and unaffected.
defanged := make([]byte, 0, len(decoded))
stripped := 0
for _, b := range decoded {
    if (b < 0x20 && b != '\n' && b != '\t') || b == 0x7f {
        stripped++
        continue
    }
    defanged = append(defanged, b)
}
return ClipboardRequest{
    SessionID:            e.sessionID,
    RequestID:            uuid.New().String(),
    Text:                 string(defanged),
    ByteCount:            len(decoded),
    StrippedControlChars: stripped,
    Timestamp:            time.Now(),
}, true
```

Run all `TestExtractor_*` tests.

---

### Step 14: Carry overflow failsafe + lone-byte UTF-8

#### 14a. Failing tests

```go
func TestExtractor_CarryOverflowFlushes(t *testing.T) {
    // Send an OSC 52 prefix followed by 64+ KiB without terminator.
    e := newOSC52Extractor("s")
    open := []byte("\x1b]52;c;")
    junk := bytes.Repeat([]byte{'A'}, maxOSC52CarrySize+100)
    _, reqs := e.process(append(open, junk...))
    if len(reqs) != 0 {
        t.Errorf("expected no requests, got %+v", reqs)
    }
    // Carry should be drained (flushed to output or discarded — pin behavior).
    // Subsequent normal bytes should pass through cleanly:
    out, _ := e.process([]byte("hello"))
    if string(out) != "hello" {
        t.Errorf("after overflow, out=%q want hello", out)
    }
}

func TestExtractor_LoneByte0x80(t *testing.T) {
    // base64 of [0x80] = "gA=="
    e := newOSC52Extractor("s")
    _, reqs := e.process([]byte("\x1b]52;c;gA==\x07"))
    if len(reqs) != 1 {
        t.Fatalf("reqs=%+v", reqs)
    }
    // Go's string([]byte{0x80}) preserves the byte; over JSON it becomes
    // a U+FFFD substitution byte sequence. Pin chosen behavior here.
    // For now: assert ByteCount=1 and Text length is exactly 1 rune of
    // U+FFFD when re-decoded as UTF-8, OR the raw byte. Document actual.
    if reqs[0].ByteCount != 1 {
        t.Errorf("ByteCount=%d want 1", reqs[0].ByteCount)
    }
}
```

#### 14b. Verify overflow already handled by Step 11 carry guard; tighten if not

Step 11 added `if len(e.carry) > maxOSC52CarrySize { flush; reset }`. Confirm the test passes; if not, add the guard now.

---

### Step 15: Commit Group D

```bash
go test ./internal/session/ -run TestExtractor
/commit
```

Suggested commit:

```
feat(session): osc52Extractor — strip OSC 52 from byte stream

Pure package addition. Recognizes BEL- and ST-terminated OSC 52 sequences,
validates Pc and Pd, decodes base64, applies byte-level defang (strip C0
controls except \n, \t; strip DEL), enforces 64 KiB cap, carries cross-event
partial sequences with a narrow prefix table (only OSC 52 partials are held
back; title/CSI/other OSCs flush immediately), and bounds carry at 64 KiB
with failsafe flush.

Not yet wired into fanOut.
```

---

## Group E — Wire extractor into SessionRuntime + dashboard server

### Step 16: Wire extractor into `SessionRuntime` (split into four sub-steps)

**Files:**

- Modify: `internal/session/tracker.go`
- Modify: `internal/session/tracker_test.go`

The struct in question is `TrackerCounters` (`internal/session/tracker.go:48-55`) — NOT `Counters`. Reachable via the `Counters` field on `SessionRuntime`.

**Test SessionRuntime construction:** the codebase pattern is direct `NewSessionRuntime("s1", source, st, "", nil, nil, nil)` calls (see `internal/session/tracker_test.go:20, 31, 169, 408, 480` for examples). There is no `newTestSessionRuntime` helper. The dashboard subscriber wiring (Step 18c) lives in `manager.go` / `server.go` startup — NOT in `NewSessionRuntime`. Tests that drop directly into `sr.clipboardCh` (e.g., `TestFanOut_DropsOnFullChannel`) therefore won't race with a subscriber. Either inline the constructor in each test OR add a small `newTestSessionRuntime(t *testing.T) *SessionRuntime` helper at the top of `tracker_test.go` that wraps the existing constructor pattern. Pick: **add a helper** to keep test code clean.

#### Step 16a: Add `ClipboardDrops` to `TrackerCounters`

##### 16a.0. (One-time) add the test helper

At the top of `internal/session/tracker_test.go` (or in a new `tracker_testutil_test.go`), add:

```go
// newTestSessionRuntime constructs a SessionRuntime suitable for unit tests.
// It does NOT wire the dashboard subscriber (that lives in manager.go /
// server.go startup); tests that interact with sr.clipboardCh directly are
// safe from races with a real consumer.
func newTestSessionRuntime(t *testing.T) *SessionRuntime {
    t.Helper()
    src := newFakeSource() // existing test fixture; check tracker_test.go for the canonical helper
    st := &state.Session{ID: "s1"} // adapt to actual state.Session shape
    return NewSessionRuntime("s1", src, st, "", nil, nil, nil)
}
```

Verify the actual signature of `NewSessionRuntime` and the existing fake source pattern by reading `internal/session/tracker_test.go:20-50`. If a fake source helper doesn't exist, write a minimal one that satisfies the `controlmode.Source` interface.

##### 16a.i. Failing test

```go
func TestTrackerCountersHasClipboardDrops(t *testing.T) {
    var c TrackerCounters
    c.ClipboardDrops.Add(1)
    if c.ClipboardDrops.Load() != 1 {
        t.Errorf("ClipboardDrops not present or not atomic")
    }
}
```

##### 16a.ii. Implement

In `internal/session/tracker.go:48-55`, add `ClipboardDrops atomic.Int64` to `TrackerCounters` alongside `FanOutDrops`. Update any `DiagnosticCounters` JSON-export struct (search for usages around line 305-308) to include the new field.

##### 16a.iii. Verify

```bash
go test ./internal/session/ -run TestTrackerCountersHasClipboardDrops
```

#### Step 16b: Add `clipboardCh` + extractor field with constructor wiring

##### 16b.i. Failing test

```go
func TestSessionRuntime_HasClipboardChannel(t *testing.T) {
    sr := newTestSessionRuntime(t) // existing test helper or minimal constructor
    if sr.clipboardCh == nil {
        t.Fatal("clipboardCh not initialized")
    }
    if cap(sr.clipboardCh) != 1 {
        t.Errorf("clipboardCh capacity = %d, want 1 (drop-on-overflow pattern)", cap(sr.clipboardCh))
    }
    if sr.extractor == nil {
        t.Fatal("extractor not initialized")
    }
}
```

##### 16b.ii. Implement

Add to `SessionRuntime` struct:

```go
clipboardCh chan ClipboardRequest
extractor   *osc52Extractor
```

In `SessionRuntime` constructor, initialize after `sr.ID` is set:

```go
sr.clipboardCh = make(chan ClipboardRequest, 1)
sr.extractor = newOSC52Extractor(sr.ID)
```

##### 16b.iii. Verify

```bash
go test ./internal/session/ -run TestSessionRuntime_HasClipboardChannel
```

#### Step 16c: Wire extractor into `fanOut`

##### 16c.i. Failing tests

Two cases:

```go
func TestFanOut_StripsOSC52FromOutputLog(t *testing.T) {
    sr := newTestSessionRuntime(t)
    sr.fanOut(controlmode.OutputEvent{Data: "\x1b]52;c;aGVsbG8=\x07"})
    // outputLog should have an entry with empty data (the entire event was OSC 52).
    // outputlog.go exposes ReplayAll() and ReplayFrom(uint64) — use ReplayAll().
    entries := sr.outputLog.ReplayAll()
    if len(entries) != 1 || len(entries[0].Data) != 0 {
        t.Errorf("expected one zero-length entry, got %+v", entries)
    }
}

func TestFanOut_EmitsClipboardRequest(t *testing.T) {
    sr := newTestSessionRuntime(t)
    sr.fanOut(controlmode.OutputEvent{Data: "\x1b]52;c;aGVsbG8=\x07"})
    select {
    case req := <-sr.clipboardCh:
        if req.Text != "hello" {
            t.Errorf("Text=%q want hello", req.Text)
        }
    case <-time.After(100 * time.Millisecond):
        t.Fatal("expected ClipboardRequest, got none")
    }
}

func TestFanOut_DropsOnFullChannel(t *testing.T) {
    sr := newTestSessionRuntime(t)
    // Fill channel
    sr.clipboardCh <- ClipboardRequest{Text: "first"}
    // Send another via fanOut — should drop
    sr.fanOut(controlmode.OutputEvent{Data: "\x1b]52;c;Yg==\x07"}) // "b"
    if sr.Counters.ClipboardDrops.Load() != 1 {
        t.Errorf("ClipboardDrops = %d, want 1", sr.Counters.ClipboardDrops.Load())
    }
}
```

##### 16c.ii. Implement

Modify `fanOut` (`internal/session/tracker.go:222`):

```go
func (t *SessionRuntime) fanOut(event controlmode.OutputEvent) {
    t.Counters.EventsDelivered.Add(1)
    t.Counters.BytesDelivered.Add(int64(len(event.Data)))

    // Server-side OSC 52 extraction — strip from byte stream, emit
    // ClipboardRequests. Single goroutine here; no lock needed.
    stripped, reqs := t.extractor.process([]byte(event.Data))
    for _, req := range reqs {
        select {
        case t.clipboardCh <- req:
        default:
            t.Counters.ClipboardDrops.Add(1)
        }
    }

    // Note: stripped may be empty when the event was entirely OSC 52.
    // We still call Append to consume a seq — keeps subscribers' gap
    // detection contiguous (CR/FM zero-length frame fix in Step 5
    // ensures empty frames also propagate).
    seq := t.outputLog.Append(stripped)

    seqEvent := SequencedOutput{
        OutputEvent: controlmode.OutputEvent{Data: string(stripped)},
        Seq:         seq,
    }
    // ... existing fan-out to subs (unchanged) ...
}
```

##### 16c.iii. Verify

```bash
go test ./internal/session/...
```

All `TestExtractor_*`, the new `TestFanOut_*`, and existing `TestTrackerOutputLog_*` tests should pass.

#### Step 16d: Dispose ordering for `clipboardCh`

##### 16d.i. Read existing Stop()

`internal/session/tracker.go:165-185`. Note the existing 5-second timeout on `<-doneCh` at lines 182-183.

##### 16d.ii. Failing test

```go
func TestStop_ClosesClipboardChannel(t *testing.T) {
    sr := newTestSessionRuntime(t)
    sr.Start() // existing
    sr.Stop()
    select {
    case _, ok := <-sr.clipboardCh:
        if ok {
            t.Error("clipboardCh delivered after Stop; expected closed")
        }
    case <-time.After(time.Second):
        t.Fatal("clipboardCh not closed within 1s of Stop")
    }
}
```

##### 16d.iii. Implement

In `Stop()`, after the existing `<-t.doneCh` wait (and inside the same defer/cleanup path that closes subscriber channels), add:

```go
close(t.clipboardCh)
```

**Race caveat to document in code comment:** if `<-doneCh` times out (5s, line 182-183) because `run()` is stuck, `close(t.clipboardCh)` runs anyway and a still-live `fanOut` could panic with "send on closed channel". This is the same class of race that already exists for the subscriber channels at lines 173-178; we inherit it explicitly. Note in a comment, do not try to fix in this plan.

##### 16d.iv. Verify

```bash
go test ./internal/session/...
```

---

### Step 17: Dashboard contract types

**Files:**

- Create: `internal/api/contracts/clipboard.go` (no existing `dashboard.go`; existing files include `sessions.go`, `spawn.go`, `preview.go`, etc. — clipboard gets its own file for clarity)
- Run: `go run ./cmd/gen-types`

#### 17a. Add types

Create `internal/api/contracts/clipboard.go`:

```go
// ClipboardRequestEvent is broadcast on /ws/dashboard when a TUI emits OSC 52.
type ClipboardRequestEvent struct {
    Type                 string `json:"type"` // "clipboardRequest"
    SessionID            string `json:"sessionId"`
    RequestID            string `json:"requestId"`
    Text                 string `json:"text"`
    ByteCount            int    `json:"byteCount"`
    StrippedControlChars int    `json:"strippedControlChars"`
}

// ClipboardClearedEvent is broadcast when a pending clipboard request is
// cleared (approve, reject, dispose, or TTL).
type ClipboardClearedEvent struct {
    Type      string `json:"type"` // "clipboardCleared"
    SessionID string `json:"sessionId"`
    RequestID string `json:"requestId"` // empty for non-ack-driven clears
}

// ClipboardAckRequest is the body of POST /api/sessions/{id}/clipboard.
type ClipboardAckRequest struct {
    Action    string `json:"action"`    // "approve" | "reject"
    RequestID string `json:"requestId"`
}

// ClipboardAckResponse is the response.
type ClipboardAckResponse struct {
    Status string `json:"status"` // "ok" | "stale"
}
```

#### 17b. Register the new types in `cmd/gen-types/main.go`

`cmd/gen-types/main.go` enumerates root types explicitly in a `rootTypes` slice (`cmd/gen-types/main.go:26-82`). Adding a new contracts file does NOT auto-register — gen-types runs and silently produces no diff for unregistered types. Add to the slice:

```go
reflect.TypeOf(contracts.ClipboardRequestEvent{}),
reflect.TypeOf(contracts.ClipboardClearedEvent{}),
reflect.TypeOf(contracts.ClipboardAckRequest{}),
reflect.TypeOf(contracts.ClipboardAckResponse{}),
```

#### 17c. Regenerate TS types

```bash
go run ./cmd/gen-types
```

Verify `assets/dashboard/src/lib/types.generated.ts` now contains the four new types. If it doesn't, the `rootTypes` registration is missing.

---

### Step 18: Dashboard server pendingClipboard map + subscriber

**Files:**

- Modify: `internal/dashboard/server.go` (route registration + new `BroadcastClipboardRequest`/`BroadcastClipboardCleared` methods)
- Create: `internal/dashboard/clipboard_state.go` (the new state holder)
- Create: `internal/dashboard/clipboard_state_test.go`

**File-naming note:** `internal/dashboard/clipboard.go` already exists (image-paste handler from spec context). Do NOT create another `clipboard.go`. The state holder goes in `clipboard_state.go`; the ack HTTP handler in Step 19 goes in `clipboard_ack.go`.

#### 18a. Failing test: subscriber populates map and broadcasts after debounce

Write a test that:

1. Constructs a minimal dashboard state holder (whatever struct holds the broadcast machinery).
2. Sends a `ClipboardRequest` on a fake `clipboardCh`.
3. After 250 ms, asserts a broadcast was emitted with the request's contents.
4. Sends two requests within 100 ms and asserts only ONE broadcast (the second).

#### 18b. Implement

The dashboard server's broadcast pattern is per-purpose helpers calling `s.broadcastToAllDashboardConns([]byte)`. See `BroadcastCuratorEvent` at `internal/dashboard/server.go:1722-1736` and `BroadcastEvent` at `:1740` for precedent. We mirror that — no generic broadcaster.

`internal/dashboard/clipboard_state.go`:

```go
package dashboard

import (
    "encoding/json"
    "sync"
    "time"

    "github.com/charmbracelet/log"
    "github.com/google/uuid"

    "github.com/sergeknystautas/schmux/internal/api/contracts"
    "github.com/sergeknystautas/schmux/internal/session"
)

const (
    clipboardDebounceWindow = 200 * time.Millisecond
    clipboardTTL            = 5 * time.Minute
)

type pendingEntry struct {
    req       session.ClipboardRequest
    requestID string
    debounce  *time.Timer
    ttl       *time.Timer
}

// clipboardBroadcaster is the minimal Server surface clipboardState needs.
// Avoids a circular dep with *Server in tests.
type clipboardBroadcaster interface {
    BroadcastClipboardRequest(ev contracts.ClipboardRequestEvent)
    BroadcastClipboardCleared(ev contracts.ClipboardClearedEvent)
}

type clipboardState struct {
    mu          sync.Mutex
    pending     map[string]*pendingEntry // key: sessionID
    broadcaster clipboardBroadcaster
    logger      *log.Logger
}

func newClipboardState(b clipboardBroadcaster, logger *log.Logger) *clipboardState {
    return &clipboardState{
        pending:     map[string]*pendingEntry{},
        broadcaster: b,
        logger:      logger,
    }
}

// onRequest is called from a per-session subscriber goroutine when the
// extractor emits a ClipboardRequest.
func (cs *clipboardState) onRequest(req session.ClipboardRequest) {
    cs.mu.Lock()
    entry, ok := cs.pending[req.SessionID]
    if ok && entry.debounce != nil {
        entry.debounce.Stop()
    }
    if !ok {
        entry = &pendingEntry{}
        cs.pending[req.SessionID] = entry
    }
    entry.req = req
    entry.requestID = uuid.New().String()
    sid := req.SessionID
    entry.debounce = time.AfterFunc(clipboardDebounceWindow, func() {
        cs.fireBroadcast(sid)
    })
    cs.mu.Unlock()
}

func (cs *clipboardState) fireBroadcast(sessionID string) {
    cs.mu.Lock()
    entry, ok := cs.pending[sessionID]
    if !ok {
        cs.mu.Unlock()
        return
    }
    if entry.ttl != nil {
        entry.ttl.Stop()
    }
    entry.ttl = time.AfterFunc(clipboardTTL, func() {
        cs.clear(sessionID, "")
    })
    ev := contracts.ClipboardRequestEvent{
        Type:                 "clipboardRequest",
        SessionID:            sessionID,
        RequestID:            entry.requestID,
        Text:                 entry.req.Text,
        ByteCount:            entry.req.ByteCount,
        StrippedControlChars: entry.req.StrippedControlChars,
    }
    cs.mu.Unlock()
    cs.broadcaster.BroadcastClipboardRequest(ev)
}

// clear removes a pending entry. Returns true if a matching entry was
// actually cleared (caller can use this to distinguish "ok" vs "stale" for
// the HTTP ack response).
func (cs *clipboardState) clear(sessionID, requestID string) bool {
    cs.mu.Lock()
    entry, ok := cs.pending[sessionID]
    if !ok {
        cs.mu.Unlock()
        return false
    }
    if requestID != "" && entry.requestID != requestID {
        cs.mu.Unlock()
        return false // stale
    }
    if entry.debounce != nil {
        entry.debounce.Stop()
    }
    if entry.ttl != nil {
        entry.ttl.Stop()
    }
    delete(cs.pending, sessionID)
    cs.mu.Unlock()
    cs.broadcaster.BroadcastClipboardCleared(contracts.ClipboardClearedEvent{
        Type:      "clipboardCleared",
        SessionID: sessionID,
        RequestID: requestID,
    })
    return true
}

func (cs *clipboardState) snapshot() []contracts.ClipboardRequestEvent {
    cs.mu.Lock()
    defer cs.mu.Unlock()
    out := make([]contracts.ClipboardRequestEvent, 0, len(cs.pending))
    for sid, entry := range cs.pending {
        out = append(out, contracts.ClipboardRequestEvent{
            Type:                 "clipboardRequest",
            SessionID:            sid,
            RequestID:            entry.requestID,
            Text:                 entry.req.Text,
            ByteCount:            entry.req.ByteCount,
            StrippedControlChars: entry.req.StrippedControlChars,
        })
    }
    return out
}
```

Add the broadcast helpers to `*Server` in `internal/dashboard/server.go` near `BroadcastCuratorEvent` (~line 1722). The codebase has two broadcast envelope conventions:

- **Nested**: `BroadcastCuratorEvent` (`server.go:1722-1736`) wraps the payload in a `{Type, Event}` struct → `{"type":"curator_event","event":{...}}`.
- **Flat**: `BroadcastCatalogUpdated` and `BroadcastConfigUpdated` (`server.go:432-447`) marshal the event directly with an inline `Type string \`json:"type"\``field →`{"type":"catalog_updated", ...}`.

We follow the **flat** precedent (matches our `ClipboardRequestEvent`/`ClipboardClearedEvent` shape, which already includes a `Type` field):

```go
func (s *Server) BroadcastClipboardRequest(ev contracts.ClipboardRequestEvent) {
    payload, err := json.Marshal(ev)
    if err != nil {
        s.logger.Error("BroadcastClipboardRequest: marshal", "err", err)
        return
    }
    s.broadcastToAllDashboardConns(payload)
}

func (s *Server) BroadcastClipboardCleared(ev contracts.ClipboardClearedEvent) {
    payload, err := json.Marshal(ev)
    if err != nil {
        s.logger.Error("BroadcastClipboardCleared: marshal", "err", err)
        return
    }
    s.broadcastToAllDashboardConns(payload)
}
```

In `*Server` struct (or its constructor), add `clipboardState *clipboardState` and initialize: `s.clipboardState = newClipboardState(s, s.logger)` (Server itself satisfies `clipboardBroadcaster`).

#### 18c. Wire one subscriber goroutine per session

The natural hook is wherever the dashboard server already attaches per-tracker callbacks. Verified: `internal/dashboard/server.go:370` calls `s.session.SetOutputCallback(s.handleSessionOutputChunk)` — that's the "tracker comes alive" pattern. Find the equivalent per-tracker registration site (likely a method on the session manager that takes a newly-created `*SessionRuntime`) and spawn the subscriber goroutine there.

For each new `*SessionRuntime`:

```go
go func(tracker *session.SessionRuntime) {
    for req := range tracker.ClipboardCh() {
        s.clipboardState.onRequest(req)
    }
    s.clipboardState.clear(tracker.ID, "") // channel closed → session dispose
}(tracker)
```

Notes:

- Add a `ClipboardCh() <-chan ClipboardRequest` accessor on `*SessionRuntime` if `clipboardCh` is unexported (in Step 16b it was added unexported). Read-only return type prevents external code from closing the channel — only `Stop()` should close it (Step 16d).
- The subscriber's loop exits naturally when `Stop()` closes `clipboardCh`. The trailing `clear` ensures any pending entry is broadcast as cleared so banners drop on session dispose.
- If sessions are restored on daemon startup (`daemon.go:1014-1028`), wire the subscriber for each restored tracker too. Verify by reading the existing tracker-startup loop.

If the registration site isn't immediately obvious, grep for `EnsureTracker` or `NewSessionRuntime` callers to find the construction sites that need the subscriber wiring.

#### 18d. Verify

```bash
go test ./internal/dashboard/... ./internal/session/...
```

---

### Step 19: HTTP endpoint POST /api/sessions/{id}/clipboard

**Files:**

- Create: `internal/dashboard/clipboard_ack.go` (NOT `clipboard.go` — exists already for image-paste)
- Create: `internal/dashboard/clipboard_ack_test.go`
- Modify: `internal/dashboard/server.go` (route registration; chi router)

The codebase uses `github.com/go-chi/chi/v5` (`go.mod:37`). Routes are mounted inside an `r.Route("/api", func(r chi.Router) { ... })` block at `internal/dashboard/server.go:661` — routes inside use bare paths (NOT prefixed with `/api`). Existing session POST convention uses `{sessionID}` not `{id}` (see `internal/dashboard/server.go:860-864`):

```go
r.Post("/sessions/{sessionID}/dispose", wsH.handleDispose)
r.Post("/sessions/{sessionID}/tell", s.handleTellSession)
```

Mirror that convention. Extract URL param via `chi.URLParam(r, "sessionID")`.

#### 19a. Failing tests

`internal/dashboard/clipboard_ack_test.go`:

```go
package dashboard

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/go-chi/chi/v5"

    "github.com/sergeknystautas/schmux/internal/api/contracts"
    "github.com/sergeknystautas/schmux/internal/session"
)

type capturingBroadcaster struct {
    requests []contracts.ClipboardRequestEvent
    cleared  []contracts.ClipboardClearedEvent
}

func (c *capturingBroadcaster) BroadcastClipboardRequest(ev contracts.ClipboardRequestEvent) {
    c.requests = append(c.requests, ev)
}
func (c *capturingBroadcaster) BroadcastClipboardCleared(ev contracts.ClipboardClearedEvent) {
    c.cleared = append(c.cleared, ev)
}

// helper: build a pre-populated state with one entry
func stateWithEntry(t *testing.T, sid, text string) (*clipboardState, *capturingBroadcaster, string /*requestID*/) {
    b := &capturingBroadcaster{}
    cs := newClipboardState(b, nil)
    cs.onRequest(session.ClipboardRequest{SessionID: sid, Text: text, ByteCount: len(text)})
    // Force the debounce so the entry is "broadcast-stage" with a known requestID.
    cs.fireBroadcast(sid)
    cs.mu.Lock()
    rid := cs.pending[sid].requestID
    cs.mu.Unlock()
    return cs, b, rid
}

// helper: dispatch a request through chi so URLParam works.
// Mirror production: routes inside r.Route("/api", ...), so the test
// router mounts the handler at /api/sessions/{sessionID}/clipboard.
func dispatch(t *testing.T, h http.HandlerFunc, sid, body string) *httptest.ResponseRecorder {
    r := chi.NewRouter()
    r.Route("/api", func(r chi.Router) {
        r.Post("/sessions/{sessionID}/clipboard", h)
    })
    req := httptest.NewRequest("POST", "/api/sessions/"+sid+"/clipboard", bytes.NewBufferString(body))
    req.Header.Set("Content-Type", "application/json")
    rec := httptest.NewRecorder()
    r.ServeHTTP(rec, req)
    return rec
}

func TestClipboardAck_Approve_OK(t *testing.T) {
    cs, b, rid := stateWithEntry(t, "s1", "hello")
    body, _ := json.Marshal(contracts.ClipboardAckRequest{Action: "approve", RequestID: rid})
    rec := dispatch(t, makeClipboardAckHandler(cs), "s1", string(body))
    if rec.Code != 200 {
        t.Fatalf("status = %d, want 200", rec.Code)
    }
    var resp contracts.ClipboardAckResponse
    json.NewDecoder(rec.Body).Decode(&resp)
    if resp.Status != "ok" {
        t.Errorf("status = %q, want ok", resp.Status)
    }
    if len(b.cleared) != 1 || b.cleared[0].SessionID != "s1" {
        t.Errorf("cleared = %+v", b.cleared)
    }
}

func TestClipboardAck_StaleRequestID(t *testing.T) {
    cs, b, _ := stateWithEntry(t, "s1", "hello")
    body, _ := json.Marshal(contracts.ClipboardAckRequest{Action: "approve", RequestID: "wrong-id"})
    rec := dispatch(t, makeClipboardAckHandler(cs), "s1", string(body))
    var resp contracts.ClipboardAckResponse
    json.NewDecoder(rec.Body).Decode(&resp)
    if resp.Status != "stale" {
        t.Errorf("status = %q, want stale", resp.Status)
    }
    if len(b.cleared) != 0 {
        t.Errorf("expected no clear broadcast, got %+v", b.cleared)
    }
}

func TestClipboardAck_UnknownSession(t *testing.T) {
    b := &capturingBroadcaster{}
    cs := newClipboardState(b, nil)
    body, _ := json.Marshal(contracts.ClipboardAckRequest{Action: "approve", RequestID: "any"})
    rec := dispatch(t, makeClipboardAckHandler(cs), "no-such-session", string(body))
    var resp contracts.ClipboardAckResponse
    json.NewDecoder(rec.Body).Decode(&resp)
    if resp.Status != "stale" {
        t.Errorf("status = %q, want stale", resp.Status)
    }
}

func TestClipboardAck_RejectInvalidAction(t *testing.T) {
    cs, _, _ := stateWithEntry(t, "s1", "hello")
    body, _ := json.Marshal(map[string]string{"action": "evil", "requestId": "x"})
    rec := dispatch(t, makeClipboardAckHandler(cs), "s1", string(body))
    if rec.Code != 400 {
        t.Errorf("status = %d, want 400", rec.Code)
    }
}
```

Note: tests use `makeClipboardAckHandler(cs *clipboardState) http.HandlerFunc` factory rather than depending on `*Server` directly — keeps tests light. The factory closes over the state and returns the handler.

#### 19b. Implement handler

`internal/dashboard/clipboard_ack.go`:

```go
package dashboard

import (
    "encoding/json"
    "net/http"

    "github.com/go-chi/chi/v5"

    "github.com/sergeknystautas/schmux/internal/api/contracts"
)

func makeClipboardAckHandler(cs *clipboardState) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        sessionID := chi.URLParam(r, "sessionID")
        var req contracts.ClipboardAckRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            writeJSONError(w, "invalid body", http.StatusBadRequest)
            return
        }
        if req.Action != "approve" && req.Action != "reject" {
            writeJSONError(w, "invalid action", http.StatusBadRequest)
            return
        }
        cleared := cs.clear(sessionID, req.RequestID)
        status := "ok"
        if !cleared {
            status = "stale"
        }
        writeJSON(w, contracts.ClipboardAckResponse{Status: status})
    }
}
```

(`writeJSONError` and `writeJSON` already exist; see `internal/dashboard/clipboard.go` for usage examples.)

#### 19c. Register route

In `internal/dashboard/server.go` inside the `r.Route("/api", ...)` block, alongside the existing session POSTs at lines 860-864:

```go
r.Post("/sessions/{sessionID}/clipboard", makeClipboardAckHandler(s.clipboardState))
```

Verify `s.clipboardState` is initialized in the Server constructor (Step 18).

#### 19d. Update `docs/api.md`

CLAUDE.md's CI gate (`scripts/check-api-docs.sh`) requires `docs/api.md` to be updated when `internal/dashboard/`, `internal/session/`, or `internal/api/contracts/` change. Add to `docs/api.md`:

1. **HTTP endpoint** — under the existing session-routes section, document `POST /api/sessions/{sessionID}/clipboard` with the request body (`{action, requestId}`) and response (`{status: "ok" | "stale"}`). Mirror the format of `/api/clipboard-paste` documentation around `docs/api.md:823`.
2. **WebSocket events** — under the `/ws/dashboard` event-types section, document `clipboardRequest` (with all fields from `ClipboardRequestEvent`) and `clipboardCleared` (`ClipboardClearedEvent`). Note that the daemon emits `clipboardRequest` per OSC 52 emit (after a 200ms debounce) and `clipboardCleared` on Approve/Reject/TTL/dispose. Note WS reconnect rehydrates active requests via the standard initial-state burst.

#### 19c. Verify

```bash
go test ./internal/dashboard/...
```

---

### Step 20: WS-reconnect snapshot of pending clipboards

**Files:**

- Modify: `internal/dashboard/server.go` (`handleDashboardWebSocket`, function starts ~line 1758)

Insertion point: AFTER the curation snapshot block at `internal/dashboard/server.go:1822-1834` (so all "currently active" snapshots are grouped). The variable in scope is `conn` (a `*wsConn`), and `*wsConn` exposes only `WriteMessage(messageType int, data []byte) error` (no `WriteJSON`). The existing snapshot pattern marshals JSON manually then calls `conn.WriteMessage(websocket.TextMessage, payload)`.

#### 20a. Failing test

Test `clipboardState.snapshot()` directly (it returns `[]contracts.ClipboardRequestEvent` — already pure):

```go
func TestClipboardState_Snapshot(t *testing.T) {
    cs, _, _ := stateWithEntry(t, "s1", "hello")
    snap := cs.snapshot()
    if len(snap) != 1 {
        t.Fatalf("got %d events, want 1", len(snap))
    }
    if snap[0].SessionID != "s1" || snap[0].Text != "hello" || snap[0].Type != "clipboardRequest" {
        t.Errorf("event = %+v", snap[0])
    }
}

func TestClipboardState_Snapshot_Empty(t *testing.T) {
    cs := newClipboardState(&capturingBroadcaster{}, nil)
    if snap := cs.snapshot(); len(snap) != 0 {
        t.Errorf("snapshot of empty state = %+v, want empty", snap)
    }
}
```

The integration (snapshot → wire) is covered by the Group H scenario test that connects a WS client mid-pending and asserts the banner appears. Building a unit harness for `handleDashboardWebSocket` is out of scope; the snapshot logic itself is trivial (loop + marshal + WriteMessage) and the failure mode (missing entries on reconnect) is exactly what the scenario surfaces.

#### 20b. Implement: snapshot send in `handleDashboardWebSocket`

In `internal/dashboard/server.go`, immediately AFTER the curation snapshot block (after line 1834), add:

```go
// Send active clipboard requests so reconnecting clients see current state.
// Mirrors the curation pattern above.
for _, ev := range s.clipboardState.snapshot() {
    payload, err := json.Marshal(ev)
    if err != nil {
        s.logger.Error("snapshot clipboard: marshal", "err", err)
        continue
    }
    if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
        return
    }
}
```

(`conn` is the `*wsConn` in scope; `s.logger` is the existing field. Pattern matches `:1822-1834` exactly.)

#### 20c. Verify

```bash
go test ./internal/dashboard/...
```

---

### Step 21: Commit Group E

```bash
/commit
```

Suggested message:

```
feat(dashboard): pendingClipboard state, debounce, TTL, ack endpoint, WS snapshot

Wire osc52Extractor into SessionRuntime.fanOut. Add per-session clipboardCh,
ClipboardDrops counter, deterministic close-after-run-exit dispose ordering.

Dashboard server gains a pendingClipboard map with 200ms debounce + 5min TTL,
broadcasts clipboardRequest/clipboardCleared on /ws/dashboard, accepts
POST /api/sessions/{id}/clipboard with requestId for stale detection, and
snapshots pending state to new WS clients.

Frontend not yet wired — banner UI is the next step.
```

---

## Group F — Frontend

### Step 22: SessionsContext / new ClipboardContext

**Files:**

- Modify or create: `assets/dashboard/src/lib/SessionsContext.tsx` (or new `ClipboardContext.tsx`)
- Test: alongside in `*.test.tsx`

#### 22a. Failing test

Add tests using the existing context-test pattern (whatever it is in this codebase) covering:

- Receive `clipboardRequest` event → `pendingClipboard[sid]` populated.
- Receive `clipboardCleared` event → cleared.
- New `clipboardRequest` for same session → replaces previous.
- WS reconnect with snapshot containing clipboardRequests → context rehydrated.
- WS reconnect with empty snapshot but a pre-existing local entry → entry cleared (snapshot is source of truth).

#### 22b. Implement

Decide: extend `SessionsContext` (if not too crowded) or create `ClipboardContext`. Read the current `SessionsContext` — if it already handles many event types cleanly, extend; if it's getting large, create a new context. Document the choice in the PR.

State shape:

```ts
type ClipboardRequest = {
  requestId: string;
  text: string;
  byteCount: number;
  strippedControlChars: number;
};
type PendingClipboard = Record<string /* sessionId */, ClipboardRequest | undefined>;
```

Reducer / event handlers for `clipboardRequest`, `clipboardCleared`. WS reconnect handler clears all entries before applying initial snapshot.

#### 22c. Verify

```bash
./test.sh --quick
```

---

### Step 23: ClipboardBanner component

**Files:**

- Create: `assets/dashboard/src/components/ClipboardBanner.tsx`
- Create: `assets/dashboard/src/components/ClipboardBanner.test.tsx`

#### 23a. Failing tests

```ts
import { render, screen, fireEvent } from '@testing-library/react';
import { ClipboardBanner } from './ClipboardBanner';

describe('ClipboardBanner', () => {
  it('renders text, byte count, and stripped count', () => {
    render(<ClipboardBanner sessionId="s1" request={{
      requestId: 'r1', text: 'hello', byteCount: 5, strippedControlChars: 0,
    }} />);
    expect(screen.getByText(/hello/)).toBeInTheDocument();
    expect(screen.getByText(/5 bytes/)).toBeInTheDocument();
  });

  it('shows stripped count when > 0', () => {
    render(<ClipboardBanner sessionId="s1" request={{
      requestId: 'r1', text: 'a', byteCount: 1, strippedControlChars: 3,
    }} />);
    expect(screen.getByText(/3 control characters/)).toBeInTheDocument();
  });

  it('Reject calls API and clears (mocked)', async () => {
    // ...
  });

  it('Approve happy path: writeText then API', async () => {
    // mock navigator.clipboard.writeText to resolve, fetch to resolve
    // assert order: writeText called before fetch; clear called after both
  });

  it('Approve writeText failure: no API call, error shown', async () => {
    // mock writeText to reject; assert no fetch; assert error visible
  });

  it('In-flight lock: ignores inbound clipboardRequest event during click', async () => {
    // ...
  });

  it('Banner truncates preview > 4 KiB visually but full text passes to writeText', async () => {
    // ...
  });
});
```

#### 23b. Implement banner

```tsx
import { useState, useRef } from 'react';

interface Props {
  sessionId: string;
  request: ClipboardRequest;
  onCleared: () => void; // local optimistic clear
}

const PREVIEW_LIMIT = 4096;

export function ClipboardBanner({ sessionId, request, onCleared }: Props) {
  const [error, setError] = useState<string | null>(null);
  const [inFlight, setInFlight] = useState(false);
  // Snapshot the text at mount (and on requestId change) so a mid-click
  // replacement doesn't change what we write.
  const textRef = useRef(request.text);
  textRef.current = request.text;

  const truncated =
    request.text.length > PREVIEW_LIMIT ? request.text.slice(0, PREVIEW_LIMIT) : request.text;
  const overflowBytes = request.text.length - truncated.length;

  async function approve() {
    if (inFlight) return;
    setInFlight(true);
    setError(null);
    const text = textRef.current; // snapshot
    try {
      await navigator.clipboard.writeText(text);
    } catch (e) {
      setError('Browser blocked clipboard write — try clicking Approve again.');
      setInFlight(false);
      return;
    }
    try {
      await fetch(`/api/sessions/${sessionId}/clipboard`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' /* + csrfHeaders() */ },
        body: JSON.stringify({ action: 'approve', requestId: request.requestId }),
      });
    } finally {
      onCleared();
      setInFlight(false);
    }
  }

  async function reject() {
    if (inFlight) return;
    setInFlight(true);
    try {
      await fetch(`/api/sessions/${sessionId}/clipboard`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' /* + csrfHeaders() */ },
        body: JSON.stringify({ action: 'reject', requestId: request.requestId }),
      });
    } finally {
      onCleared();
      setInFlight(false);
    }
  }

  return (
    <div className="clipboard-banner" role="alert">
      <div className="clipboard-banner__title">
        TUI wants to copy to your clipboard ({request.byteCount} bytes)
      </div>
      <pre className="clipboard-banner__preview">
        {truncated}
        {overflowBytes > 0 && <span>… ({overflowBytes} more bytes)</span>}
      </pre>
      {request.strippedControlChars > 0 && (
        <div className="clipboard-banner__note">
          {request.strippedControlChars} control characters were stripped.
        </div>
      )}
      {error && <div className="clipboard-banner__error">{error}</div>}
      <div className="clipboard-banner__actions">
        <button onClick={reject} disabled={inFlight}>
          Reject
        </button>
        <button onClick={approve} disabled={inFlight}>
          Approve
        </button>
      </div>
    </div>
  );
}
```

Add styles in the project's CSS approach. CLAUDE.md flags `assets/dashboard/src/styles/global.css` as too large to read in full. Heuristic: grep `global.css` for an existing banner / paste / toast class (e.g., `clipboard-paste`, `notification`, `toast-`) and follow whatever convention is in place (BEM-style `.block__element--modifier` or similar). If the project uses CSS modules elsewhere (check whether any other component imports a `.module.css`), prefer that for new components.

The "in-flight lock that ignores inbound clipboardRequest" is enforced at the parent (SessionDetailPage / context selector) by NOT re-rendering the banner with a new request while `inFlight` is true. Document this in a comment; the parent must read `inFlight` from a ref or state and gate re-renders accordingly. If implementing this gate is too entangled with the parent, add a parent-side hook `usePendingClipboardWithLock(sessionId)` that returns the request and skips updates while a click is in flight.

#### 23c. Verify

```bash
./test.sh --quick
```

---

### Step 24: Mount banner in SessionDetailPage; sidebar badge

**Files:**

- Modify: `assets/dashboard/src/routes/SessionDetailPage.tsx`
- Modify: `assets/dashboard/src/components/AppShell.tsx` (sidebar badge)

#### 24a. Mount banner

In `SessionDetailPage`, read `pendingClipboard[currentSessionId]` from context, render `<ClipboardBanner>` above the terminal area when present.

Test: render `SessionDetailPage` with a populated pending request → banner visible.

#### 24b. Sidebar badge

Identify session-row markup in `AppShell.tsx` (~lines 918-986 per round-2 review). Add a small visual indicator (a dot, a tiny clipboard icon, an outlined "1") when `pendingClipboard[session.id]` is present.

If extracting a `SessionRow` component is needed for cleanliness, make it a separate small commit BEFORE this step. Otherwise, thread the `hasPendingClipboard` prop inline.

#### 24c. Verify

```bash
./test.sh --quick
```

---

### Step 25: Commit Group F

```bash
/commit
```

Suggested message:

```
feat(dashboard-ui): clipboard banner + sidebar badge for pending OSC 52

ClipboardContext (or extended SessionsContext) consumes clipboardRequest /
clipboardCleared events from /ws/dashboard. ClipboardBanner renders the
sanitized preview with byte count and stripped-control-chars hint;
Approve does navigator.clipboard.writeText then POSTs ack with requestId
for stale detection; in-flight lock prevents mid-click banner replacement
from confusing the user about what was copied. WS reconnect rehydrates
state from the daemon snapshot, source-of-truth.

Sidebar shows a dot on session rows with pending clipboard so the user
can find the prompt from any route.
```

---

## Group G — Manual verification

### Step 26: Smoke test against real TUIs

#### 26a. Build and run

```bash
go build ./cmd/schmux
go run ./cmd/build-dashboard  # only if you want to test the embedded binary
./schmux daemon-run --foreground
```

Open dashboard at http://localhost:7337.

#### 26b. nvim case

In a session, run `nvim`. In nvim, `:set clipboard=unnamedplus` then visually select text and press `y`. Observe banner appears in dashboard. Click Approve. Switch to another app and paste — text should appear.

#### 26c. tmux copy-mode

In a session, press the tmux copy-mode prefix, select text via mouse drag or keyboard motion, press Enter. Observe banner. Approve. Paste elsewhere.

#### 26d. lazygit (if installed)

Open lazygit in a session, copy a commit hash with `c`. Observe banner. Approve. Paste.

#### 26e. Multi-tab

Open two browser tabs on the dashboard, both viewing the same session. Trigger one OSC 52. Both tabs show banner. Approve in tab A. Tab B's banner clears via broadcast.

#### 26f. WS reconnect

While a banner is up, kill network briefly (toggle wifi or block port 7337). When it reconnects, banner should still be present (snapshot rehydrates).

#### 26g. Daemon restart

While a banner is up, stop and restart the daemon. WS reconnects, no snapshot entry → banner clears.

If anything misbehaves, file specific repro in this plan as a follow-up step. Do NOT proceed to scenarios until all manual checks pass.

---

## Group H — Scenario tests

### Step 27: Add Playwright scenarios

**Files:**

- Create: `test/scenarios/tui-clipboard-write.md`

Use the `scenario` skill / existing scenarios as a template.

Scenario contents (high level):

```markdown
# Scenario: TUI writes to clipboard via OSC 52, user approves

## Setup

- Spawn a session.
- Wait for terminal ready.

## Action 1: TUI emits OSC 52

- In the session terminal, run:
  printf '\e]52;c;%s\a' "$(printf hello | base64)"

## Assertion 1: Banner appears

- Within 2s, an element with role="alert" containing "hello" is visible.

## Action 2: User clicks Approve

- Grant clipboard-read permission to the test page.
- Click the Approve button.

## Assertion 2: Clipboard contains "hello"

- navigator.clipboard.readText() returns "hello".

## Cleanup

- Dispose session.
```

Generate Playwright test from the scenario per the existing scenario workflow.

```bash
./test.sh --scenarios
```

#### 27b. Pastejacking case

Add a second scenario emitting an OSC 52 payload that base64-decodes to a string containing `\n`, `rm -rf ~`. Assert preview shows it on two lines (newline preserved); clipboard after Approve contains the literal text bytes (no shell expansion — verify by reading clipboard text and comparing).

#### 27c. Defang case

Add a third scenario where the payload contains an ANSI escape `\e[31m`. Assert preview shows the escape stripped and the "N control characters were stripped" note appears. Approve, then assert clipboard text does NOT contain `\e`.

#### 27d. Multi-tab scenario

Open two browser contexts, both navigate to the same session. Trigger one OSC 52. Both show banner. Approve in context A. Within 1s, context B's banner is gone (verify via DOM absence).

#### 27e. Verify all scenarios pass

```bash
./test.sh --scenarios
```

#### 27f. Final full test sweep

```bash
./test.sh
```

#### 27g. Commit

```bash
/commit
```

Suggested message:

```
test: scenarios for TUI clipboard bridge — happy path, pastejacking, defang, multi-tab

End-to-end coverage via Playwright/Docker. Pastejacking and defang
scenarios verify the user-in-loop + sanitized-preview defenses work
against the documented attack class.
```

---

## End-to-end verification checklist

Before declaring the feature complete, all of the following must be true:

- [ ] `./test.sh` (full, NOT --quick) passes.
- [ ] `./badcode.sh` passes.
- [ ] `./test.sh --scenarios` passes.
- [ ] All four manual cases (nvim, tmux copy-mode, lazygit if available, multi-tab + WS reconnect + daemon restart) work as designed.
- [ ] `docs/api.md` updated to document `POST /api/sessions/{id}/clipboard` and the new WS event types `clipboardRequest`/`clipboardCleared` (CI gate enforces this for changes touching `internal/dashboard/`).
- [ ] `docs/specs/tui-clipboard-bridge.md` left in place — `/finalize` will consolidate it into a subsystem guide later.
- [ ] This plan file (`docs/plans/tui-clipboard-bridge-impl.md`) deleted in the final commit per the project convention (plans are temporary).

---

## Notes for the implementer

- **Read `docs/specs/tui-clipboard-bridge.md` before each group, not just at the start.** Several decisions (defang at byte-level, narrow-prefix carry, in-flight lock, snapshot-as-source-of-truth) are subtle.
- **Keep commits small** — each group above is one logical commit. If a group grows, split.
- **`./test.sh --quick` is NOT enough.** Run `./test.sh` before every `/commit`. The CLAUDE.md warning is explicit.
- **Never edit `types.generated.ts`.** Modify Go contracts then `go run ./cmd/gen-types`.
- **Worktree boundary:** this plan executes in `/Users/stefanomaz/code/workspaces/schmux-002/`. Do not touch other worktrees.
- **If a step's exact line numbers or function names have shifted** (e.g., a refactor lands meanwhile), find the equivalent location and adapt — the spec describes intent, line numbers are best-effort references.
