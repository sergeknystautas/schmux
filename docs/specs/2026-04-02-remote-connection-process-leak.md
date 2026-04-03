# Remote Connection Process Leak

**Date**: 2026-04-02
**Branch**: fix/remote-host-typing
**Status**: Investigation complete, fix not started
**Symptom**: Two identical `tmux -CC new-session -A -s schmux` processes on the remote host after reconnection or daemon restart.

```
$ ps xa | grep tmux
 243095 pts/0    Ss+    0:00 tmux -CC new-session -A -s schmux
 243179 ?        Ss     0:00 tmux -CC new-session -A -s schmux
```

Both processes are control mode clients attached to the same tmux session "schmux". This causes duplicate `%output` events reaching the control mode parser, which can corrupt terminal output or degrade performance.

## Root Cause

`Connection.Reconnect()` overwrites `c.cmd` and `c.pty` with new values **without killing or closing the old ones**. The old SSH process becomes unreachable — nobody can kill it because the reference is gone.

**File**: `internal/remote/connection.go`, lines 355-479

```go
// Line ~400: overwrites c.cmd — old process is now orphaned
c.cmd = exec.Command(args[0], args[1:]...)

// Lines ~407-411: starts new PTY, overwrites c.pty/c.stdin/c.stdout
ptmx, err := pty.StartWithSize(c.cmd, &pty.Winsize{Rows: 24, Cols: 80})
c.pty = ptmx
c.stdin = ptmx
c.stdout = ptmx
```

The old `c.cmd.Process` is now unreachable. The old `monitorProcess()` goroutine is still blocked on `c.cmd.Wait()` for the **old** cmd value (captured before overwrite), but when it finishes and calls `c.Close()`, that method acts on `c.cmd` which now points to the **new** process — killing the new connection instead of cleaning up the old one.

## Contributing Factors

There are two reconnection code paths with distinct bugs. They share the process leak symptom but have different mechanisms.

### Path 1: `StartReconnect()` — same Connection object reuse

`StartReconnect()` (manager.go:588-732) closes the old `Connection` and replaces it in the map (lines 652-656), then calls `conn.Reconnect()` on the **same new `Connection` object**. However, `Connection.Reconnect()` itself does not clean up its own prior state, so if `Reconnect()` is ever called on a `Connection` that already has a running process (e.g., a retry path), the overwrite/race bugs below apply.

### Path 2: `Manager.Reconnect()` — new Connection object, ordering bug

`Manager.Reconnect()` (manager.go:284-367) creates a **brand new `Connection`** via `NewConnection(cfg)` (line 323) and calls `Reconnect()` on it. The old `Connection` stored in `m.connections[hostID]` is closed afterward (lines 344-347). This means two SSH processes are alive simultaneously during the reconnection window. Even when `existing.Close()` fires, it kills the local SSH process but the remote tmux client survives (tmux is designed to survive SSH drops).

```go
// Line 331: starts new SSH process on NEW Connection object
if err := conn.Reconnect(ctx, host.Hostname); err != nil { ... }

// Lines 344-348: only THEN closes OLD Connection object
if existing, exists := m.connections[hostID]; exists {
    existing.Close()
}
```

No aliasing race on `c.cmd` between old and new objects here — the bug is purely the window where both are alive.

### Factor 1: `Reconnect()` overwrites cmd/pty without cleanup (PRIMARY)

**File**: `internal/remote/connection.go:400-411`

No `c.cmd.Process.Kill()` or `c.pty.Close()` before overwriting. The old SSH process is orphaned on both the local and remote host.

### Factor 2: `monitorProcess()` races with `Reconnect()` on `c.cmd` reference — guaranteed wrong-kill

**File**: `internal/remote/connection.go:759-793`

`monitorProcess()` calls `c.cmd.Wait()` on the old process (Go captures the pointer at call time). When the old process exits, `monitorProcess()` calls `c.Close()`. But `c.Close()` uses `c.cmd.Process.Kill()` (line 747-748), and `c.cmd` now points to the **new** process (overwritten by `Reconnect()`).

Critically, `Close()` uses `sync.Once` (`closeOnce`), which makes this worse than a race — it's a guaranteed kill of the new connection:

1. `Reconnect()` overwrites `c.cmd` with new process
2. Old `monitorProcess()` finishes `Wait()` on old process
3. Old `monitorProcess()` calls `c.Close()` — `closeOnce` fires, kills `c.cmd` (now the **new** process), sets `closed = true`
4. New `monitorProcess()` eventually calls `c.Close()` — `closeOnce` already fired, **nothing happens**
5. New process is dead, no cleanup runs for it

This is not "could kill the wrong process" — it **will** kill the new connection the moment the old SSH process exits.

### Factor 3: `controlPipeWriter` leaked on reconnect

**File**: `internal/remote/connection.go:443-444`

`Reconnect()` creates a new `io.Pipe()` and overwrites `c.controlPipeWriter` without closing the old one. The old pipe and its reader goroutine leak.

### Factor 4: `parseProvisioningOutput` goroutine leaked on reconnect

**File**: `internal/remote/connection.go:450`

`Reconnect()` launches `go c.parseProvisioningOutput(c.pty)`. The old `parseProvisioningOutput` goroutine from the previous connection is still running, reading from the old PTY fd. When the old PTY is eventually closed it will get an error and exit, but during the window where both are alive, two goroutines may be writing to `c.controlPipeWriter` (old goroutine to the orphaned pipe, new goroutine to the new pipe). This is a goroutine leak, not a correctness issue, but it adds to the resource accumulation.

### Factor 5: `c.client` (controlmode.Client) not cleaned up in `Reconnect()`

`waitForControlMode()` sets `c.client`. On reconnect, the old `c.client` is overwritten without `Close()`. If `controlmode.Client` holds resources (goroutines, channels), those leak. Should be closed before overwriting.

### Factor 6: Daemon restart doesn't clean up remote tmux clients

**File**: `internal/remote/manager.go:740-774`

`MarkStaleHostsDisconnected()` only updates state — it does not SSH into the remote host to kill orphaned tmux clients. After a daemon restart, old tmux control mode clients accumulate on the remote host.

This is a **separate issue** from the process lifecycle bugs above. The daemon starts cold without SSH credentials (reconnection requires interactive auth like Yubikey touch), so reliable remote cleanup from `MarkStaleHostsDisconnected()` is not feasible. Tracked separately — see "Out of Scope" below.

## Impact

- **Guaranteed disconnect after reconnect**: Via the `closeOnce` + `monitorProcess` race (Factor 2), the old `monitorProcess` goroutine will kill the new connection the moment the old SSH process exits. This makes `StartReconnect()` unreliable when the old process is still alive.
- **Duplicate output events**: Two control mode clients on the same tmux session means every `%output` line is delivered twice. The parser may handle this gracefully (dedup by sequence), but it doubles the fan-out load and could cause subtle corruption.
- **Stale process accumulation**: Each reconnection or daemon restart can leave another orphaned tmux client. Over time, the remote host accumulates idle tmux clients consuming resources.
- **Goroutine/resource leaks**: Leaked `parseProvisioningOutput` goroutines, `controlPipeWriter` pipes, and `controlmode.Client` instances accumulate per reconnection.

## Fix Plan

### Step 1: Core lifecycle fix (required for correctness)

All items here are necessary together — omitting any one leaves a race or leak that breaks reconnection.

**a) `Reconnect()` cleanup** — At the top of `Reconnect()`, before creating the new command, kill and close the old process and resources:

```go
// Kill old process before starting new one
if c.cmd != nil && c.cmd.Process != nil {
    c.cmd.Process.Kill()
    c.cmd.Wait() // reap to avoid zombie
}
if c.pty != nil {
    c.pty.Close()
}
if c.controlPipeWriter != nil {
    c.controlPipeWriter.Close()
}
if c.client != nil {
    c.client.Close()
    c.client = nil
}
```

**b) Capture cmd locally in `monitorProcess()`** — Pass the `*exec.Cmd` as an argument instead of reading `c.cmd` from the struct, so the goroutine always waits on and cleans up the correct process. Without this, the cleanup in (a) creates a tighter race window but doesn't eliminate the wrong-kill:

```go
func (c *Connection) monitorProcess(cmd *exec.Cmd) {
    cmd.Wait()
    // cleanup logic that doesn't touch c.cmd
}
```

`Close()` must also be updated to not kill `c.cmd` directly — the process kill should happen through the `monitorProcess` path or be scoped to the correct cmd reference.

**c) `Manager.Reconnect()` ordering** — Close old connection before calling `conn.Reconnect()`, matching the `StartReconnect()` pattern:

```go
// Close existing connection BEFORE starting new one
m.mu.Lock()
if existing, exists := m.connections[hostID]; exists {
    existing.Close()
}
m.mu.Unlock()

// Now reconnect
if err := conn.Reconnect(ctx, host.Hostname); err != nil { ... }

// Store new connection
m.mu.Lock()
m.connections[hostID] = conn
m.mu.Unlock()
```

### Step 2: Hardening (separate commit)

These are defensive improvements that prevent future regressions but are not required to fix the active bug.

- **Reset `closeOnce` in `Reconnect()`**: After cleanup, reset the `sync.Once` so the new connection's `Close()` path works correctly. (Alternatively, `Reconnect()` could return a new `*Connection` rather than mutating in place — this sidesteps the `closeOnce` problem entirely.)
- **Guard `parseProvisioningOutput` lifecycle**: Ensure the old goroutine has exited (via the old PTY closing) before launching the new one, or use a cancellation mechanism.

## Out of Scope

- **Remote tmux cleanup on daemon restart** (Factor 6): Requires SSH with interactive auth, fundamentally different from local process lifecycle. Track as a separate issue. A possible future approach: when a user explicitly reconnects, the reconnection flow could kill stale tmux clients as part of its setup.

## Files to Change

| File                            | Change                                                                |
| ------------------------------- | --------------------------------------------------------------------- |
| `internal/remote/connection.go` | `Reconnect()`: kill/close old cmd/pty/pipe/client before overwriting  |
| `internal/remote/connection.go` | `monitorProcess()`: accept `*exec.Cmd` parameter; don't touch `c.cmd` |
| `internal/remote/connection.go` | `Close()`: decouple process kill from `c.cmd` field                   |
| `internal/remote/manager.go`    | `Manager.Reconnect()`: close old connection before starting new one   |

## How to Reproduce

1. Connect to a remote host via the dashboard
2. Trigger a reconnection (kill the SSH process, or restart the daemon)
3. SSH into the remote host manually and run `ps xa | grep tmux`
4. Observe two (or more) `tmux -CC new-session -A -s schmux` processes

## Related

The remote typing profiling work (same branch) will make this bug more visible — the new health probes add `Execute()` calls on the shared `controlmode.Client`, and duplicate output from two tmux clients would inflate `mutexWait` measurements.
