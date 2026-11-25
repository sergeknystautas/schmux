# Reap Orphaned Session Processes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Disposing a fenced session also kills the processes its shells orphaned.

**Architecture:** A pure function, `orphanPIDs`, picks orphan PIDs from two `ps` listings. `reaper.orphans` runs `ps` and turns those PIDs into `procIdent` records. `Manager.Dispose` appends them to `fencedProcs`, which `reap` already kills.

**Tech Stack:** Go standard library, `ps`, tmux.

**Spec:** `docs/superpowers/specs/2026-11-24-reap-orphaned-session-processes-design.md`

## Global Constraints

- `ps` columns: `-o pid=,pgid=,stat=,command=`, with `axww` for the plain listing and `axeww` for the one with environments.
- Only the PID, group and command are compared between listings.
- The ID must match exactly. A zombie group leader counts as dead.
- Unfenced and remote disposal do not change. `reap` does not change.
- Log orphans as PIDs only, never `ps` output.
- If the scan fails, log a warning and dispose as today.
- No `git commit`: the user commits with `/commit`.
- Run every command from the repository root.

## Review Focus

- `ps` cannot run during dispose: the session is still disposed. (Task 2)
- Another session has its own orphan: it survives. (Task 2)
- The environment has values with spaces before the ID: the orphan is still found. (Task 1)
- The orphan is itself a zombie: it is not returned. (Task 1)
- The leader is alive in only one listing: the process is kept. (Task 1)

## Verification commands

`ps` cannot run inside the fence, so `go test` there skips every test that calls `requireProcTools`, and a skip is not evidence. **LINUX-GATE** runs the package's tests in Docker, where `ps`, `pgrep` and tmux work. `--init` is required: without it `pgrep -P 1` finds nothing and the tests skip.

```bash
CGO_ENABLED=0 GOOS=linux go test -c -o "$TMPDIR/session.test" ./internal/session
docker run --rm --init -u 1000 -v "$TMPDIR/session.test:/session.test:ro" \
  --entrypoint /session.test schmux-e2e-base:latest -test.v -test.run "<PATTERN>"
```

---

### Task 1: Find a session's orphans

**Files:**

- Modify: `internal/session/reaper.go` (header comment, `enumerate` at 58-73, new functions after it)
- Test: `internal/session/reaper_test.go`

**Interfaces:**

- Consumes: `procIdent`, `procState`, `currentPGID`, `procStartTime` (all in `reaper.go`).
- Produces:
  - `func (r *reaper) orphans(ctx context.Context, sessionID string) ([]procIdent, error)`; no orphans is `nil, nil`
  - `func orphanPIDs(plain, withEnv, sessionID string) []int`
  - `func identify(ctx context.Context, pid int) (procIdent, bool)`
  - test helpers `spawnOrphan`, `spawnOrphanGroup`, `awaitZombie`, `identOf`, and type `orphanOpts` (below)

- [ ] **Step 1: Write the failing parser test** `TestOrphanPIDs`, table-driven. It needs no `ps`, so it also runs inside the fence. Session ID is `ws-1`. Each case gives `plain` and `withEnv` lines (`pid pgid stat command`) and the wanted PIDs.

| Case                             | `plain`                                                                       | `withEnv`                                                                                                 | want   |
| -------------------------------- | ----------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- | ------ |
| dead leader                      | `20 10 R node x.js`                                                           | `20 10 R node x.js A=1 SCHMUX_SESSION_ID=ws-1 B=2`                                                        | `[20]` |
| ID is last                       | `20 10 R node x.js`                                                           | `20 10 R node x.js SCHMUX_SESSION_ID=ws-1`                                                                | `[20]` |
| spaces in an earlier value       | `20 10 R node x.js`                                                           | `20 10 R node x.js NOTE=a b c SCHMUX_SESSION_ID=ws-1`                                                     | `[20]` |
| padded columns, different STAT   | <code>&nbsp;&nbsp;20&nbsp;&nbsp;&nbsp;10 R+&nbsp;&nbsp;&nbsp;node x.js</code> | <code>&nbsp;&nbsp;20&nbsp;&nbsp;&nbsp;10 S&nbsp;&nbsp;&nbsp;&nbsp;node x.js SCHMUX_SESSION_ID=ws-1</code> | `[20]` |
| zombie leader                    | `10 10 Z <defunct>` and `20 10 R node x.js`                                   | `10 10 Z <defunct>` and `20 10 R node x.js SCHMUX_SESSION_ID=ws-1`                                        | `[20]` |
| living leader                    | `10 10 S sh` and `20 10 R node x.js`                                          | `10 10 S sh` and `20 10 R node x.js SCHMUX_SESSION_ID=ws-1`                                               | none   |
| leader alive in one listing only | `10 10 S sh` and `20 10 R node x.js`                                          | `20 10 R node x.js SCHMUX_SESSION_ID=ws-1`                                                                | none   |
| leads its own group              | `20 20 S node x.js`                                                           | `20 20 S node x.js SCHMUX_SESSION_ID=ws-1`                                                                | none   |
| orphan is a zombie               | `20 10 Z node x.js`                                                           | `20 10 Z node x.js SCHMUX_SESSION_ID=ws-1`                                                                | none   |
| different ID                     | `20 10 R node x.js`                                                           | `20 10 R node x.js SCHMUX_SESSION_ID=ws-2`                                                                | none   |
| longer ID                        | `20 10 R node x.js`                                                           | `20 10 R node x.js SCHMUX_SESSION_ID=ws-1-2`                                                              | none   |
| ID in command only               | `20 10 R node SCHMUX_SESSION_ID=ws-1`                                         | `20 10 R node SCHMUX_SESSION_ID=ws-1 A=1`                                                                 | none   |
| command changed                  | `20 10 R sh -c x`                                                             | `20 10 R node x.js SCHMUX_SESSION_ID=ws-1`                                                                | none   |
| group changed                    | `20 11 R node x.js`                                                           | `20 10 R node x.js SCHMUX_SESSION_ID=ws-1`                                                                | none   |
| in `withEnv` only                | (empty)                                                                       | `20 10 R node x.js SCHMUX_SESSION_ID=ws-1`                                                                | none   |

- [ ] **Step 2: Write the process helpers** in `reaper_test.go`. The orphan is the test binary started again, as in `internal/logging/logging_test.go:27`.

```go
// TestOrphanChild is the orphan's body; it does nothing in a normal run.
func TestOrphanChild(t *testing.T) {
	if os.Getenv("SCHMUX_TEST_ORPHAN") != "1" {
		return
	}
	if os.Getenv("SCHMUX_TEST_ORPHAN_SETPGID") == "1" {
		syscall.Setpgid(0, 0)
	}
	fmt.Println(os.Getpid()) // tells the parent it is running
	time.Sleep(time.Minute)  // lifetime backstop; the test kills it
}

type orphanOpts struct {
	sessionID string // SCHMUX_SESSION_ID in the environment
	arg       string // extra command-line argument
	ownGroup  bool   // the child starts its own group
	zombie    bool   // leave the killed shell unreaped
}

// spawnOrphan starts a shell that starts the test binary, kills only the
// shell, and returns the orphan's PID.
func spawnOrphan(t *testing.T, o orphanOpts) int {
	pid, _ := spawnOrphanGroup(t, o)
	return pid
}

// spawnOrphanGroup also returns the killed shell's PID, the group's leader.
func spawnOrphanGroup(t *testing.T, o orphanOpts) (pid, shell int)
```

`spawnOrphanGroup`, exactly:

- Command: `/bin/sh -c '"$@" & wait' sh <os.Args[0]> -test.run=^TestOrphanChild$ -- <arg>` with `Setpgid: true`.
- Env: `os.Environ()` plus `SCHMUX_TEST_ORPHAN=1`, `SCHMUX_SESSION_ID=<sessionID>`, and `SCHMUX_TEST_ORPHAN_SETPGID=1` when `ownGroup`.
- Stdout: the write end of an `os.Pipe`, closed in the parent after `Start`. Read the first line for the PID, with a 5 s `SetReadDeadline` as the failure backstop. A `StdoutPipe` will not do: `Wait` closes it.
- Then `SIGKILL` the shell's PID only. Call `cmd.Wait()` unless `zombie`; when `zombie`, call it in cleanup.
- Cleanup: `SIGKILL` the shell's group and the orphan's PID.

Also add `func awaitZombie(t *testing.T, pid int)` by extracting the probe loop of `TestAliveValidated_ZombieIsExited` (129-147) and using it there, and `func identOf(t *testing.T, pid int) procIdent`, which calls `identify` and fails the test if it returns false.

- [ ] **Step 3: Write the failing process tests.** Each calls `requireProcTools` first. Build session IDs as `fmt.Sprintf("<name>-%d", time.Now().UnixNano())` so that no other process can match.
  - `TestOrphans_FoundAndReaped`: one orphan. `orphans` returns exactly its PID, with a non-empty `Start` and `PGID` equal to the shell's PID. After `reap` with a 100 ms grace, `err == nil` and `aliveValidated` is false.
  - `TestOrphans_ZombieLeader`: skip unless Linux, with the skip message used in `TestAliveValidated_ZombieIsExited`. Spawn with `zombie`, then `awaitZombie(shell)`. `orphans` returns the orphan's PID, and `reap` kills it.
  - `TestOrphans_Ignores`: one matching orphan as a positive control, plus four others: `ownGroup` with the same ID; a different ID; the ID plus `-2`; and a different ID in the environment with `arg` set to `SCHMUX_SESSION_ID=<id>`. One `orphans` call returns the control's PID only.

- [ ] **Step 4: Run them to verify they fail**

Run: `go test ./internal/session -run 'Orphan'`
Expected: build failure, `undefined: orphanPIDs`.

- [ ] **Step 5: Implement in `reaper.go`**
  - `identify`: move the body of `enumerate`'s loop (60-72) into it; false when the process has exited or is a zombie. `enumerate` calls it.
  - `orphanPIDs`: parse each line into PID, PGID, STAT and the rest, trimming only leading spaces from the rest. A process qualifies when all of these hold:
    - It is in both listings with the same PGID and command, and is not a zombie in either.
    - The `withEnv` command is the plain command followed by a space, and one of `strings.Fields` of what remains equals `SCHMUX_SESSION_ID=<sessionID>`.
    - Its PGID is not the PID of a non-zombie process in either listing.
  - `orphans`: run `ps axww` and `ps axeww` with the pinned columns, wrap either error with which listing failed, and call `identify` on each PID. Skip PIDs it rejects.
  - Header comment: say that the reaper also finds processes orphaned by a dead shell, by session ID and dead group leader.

- [ ] **Step 6: Verify**

Run: `go test ./internal/session -run 'TestOrphanPIDs' -v`
Expected: PASS for all 15 cases.

Run: LINUX-GATE with `TestOrphan|TestEnumerate|TestAlive|TestReap`
Expected: `PASS`, with no `--- SKIP` line.

---

### Task 2: Kill orphans on dispose

**Files:**

- Modify: `internal/session/manager.go:1926` (after the enumeration block, before `if sessionExists`)
- Modify: `docs/api.md:793`
- Test: `internal/session/manager_test.go`

**Interfaces:**

- Consumes: `(*reaper).orphans`, `spawnOrphan`, `orphanOpts`, `identOf`, `requireProcTools` from Task 1.
- Produces: nothing new.

- [ ] **Step 1: Write the failing tests.** Construct managers as `TestDispose_FencedWithoutTmuxTakesSkipPath` does (1888), with `cfg.Sessions = &config.SessionsConfig{DisposeGracePeriodMs: 100}` and `m.reaper.poll = 20 * time.Millisecond`.

  `TestDispose_Orphans`, calling `requireProcTools`:

| Case               | `Fence` | tmux  | Orphan after `Dispose`    |
| ------------------ | ------- | ----- | ------------------------- |
| fenced, tmux alive | true    | alive | dead                      |
| fenced, tmux gone  | true    | gone  | dead                      |
| unfenced           | false   | gone  | alive, and no signal sent |

- In every case `Dispose` returns nil and the session is gone from state.
- Every case also spawns an orphan with a different session ID; it is alive afterwards.
- Take `identOf` before `Dispose`, and check `aliveValidated` once after it returns. `Dispose` is synchronous, so do not retry.
- Wrap `m.reaper.signal` to record its calls; the unfenced case asserts there were none.
- The tmux-alive case passes its own server to `New`, as at 1024: `tmux.NewTmuxServer("tmux", fmt.Sprintf("orphan-%d", time.Now().UnixNano()), nil)`, pane command `sleep 600`, and the session's `TmuxSocket` set to that socket name. Assert `SessionExists` before `Dispose` and not after. Kill the session in cleanup.

`TestDispose_OrphanScanFailureStillDisposes`: no `requireProcTools`. `t.Setenv("PATH", t.TempDir())`, fenced, tmux gone, logger writing to a `bytes.Buffer`. `Dispose` returns nil, the session is gone, and the log contains `fenced dispose: orphan scan failed`.

- [ ] **Step 2: Run to verify they fail**

Run: LINUX-GATE with `TestDispose_Orphan`
Expected: FAIL in both fenced cases (orphan alive) and in the scan-failure test (no warning). The unfenced case passes.

- [ ] **Step 3: Implement in `Dispose`.** When `sess.Fence`, whatever `sessionExists` and `enumerate` gave, call `m.reaper.orphans(ctx, sessionID)`.
  - Error: `m.logger.Warn("fenced dispose: orphan scan failed", "session", sessionID, "err", err)` and continue.
  - Orphans found: `m.logger.Info("fenced dispose: found orphans", "session", sessionID, "pids", pids)`, and append them to `fencedProcs`.

- [ ] **Step 4: Update `docs/api.md:793`.**
  - Replace "against the enumerated processes only" with "against those processes and members of their process groups".
  - After the first sentence, add: "Disposal also looks for processes orphaned by the session's shells: those with the session's `SCHMUX_SESSION_ID` in their environment whose process-group leader is dead. They are terminated the same way, even when the tmux session is already gone. This includes `nohup` servers that the session's shells started."
  - Replace "after no live tree process remains" with "after no live process from the tree or the orphan scan remains".
  - Replace "Well-behaved fenced sessions and all unfenced sessions dispose as fast as before." with "Unfenced sessions dispose as fast as before. Fenced sessions take about 0.1 s longer for the orphan scan, and wait out the grace period when there are orphans that do not exit on their own."
  - Replace "for a stuck fenced session" with "for a stuck fenced session or one with orphans".

- [ ] **Step 5: Verify**

Run: LINUX-GATE with `TestDispose|TestOrphan|TestEnumerate|TestAlive|TestReap`
Expected: `PASS`, with no `--- SKIP` line.

---

### Task 3: Gates

**Files:** none, apart from fixes the gates require.

- [ ] **Step 1:** `./format.sh`, then `go vet ./...`. Expected: exit 0 from both.
- [ ] **Step 2:** `./test.sh`. Expected: every suite passes. Inside the fence the `ps` tests skip here, which is why LINUX-GATE is separate.
- [ ] **Step 3:** `./badcode.sh`. Expected: exit 0.
- [ ] **Step 4:** Run the `test-rules-review` skill. Expected: a compliant verdict.
- [ ] **Step 5:** Ask the user to run this unfenced on macOS. Expected: `PASS`, with `TestOrphans_ZombieLeader` and `TestAliveValidated_ZombieIsExited` as the only skips.

```bash
go test ./internal/session -count=1 -v -run 'TestDispose|TestOrphan|TestEnumerate|TestAlive|TestReap'
```

- [ ] **Step 6:** Ask the user to run `/finalize`, which removes the spec and this plan. Then `ls` both paths. Expected: `No such file or directory` for each.
