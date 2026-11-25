# Reap Orphaned Session Processes on Dispose

**Status:** draft for review.

## Summary

Disposing a fenced session kills the process tree under its tmux pane. A process whose
parent shell has died is no longer in that tree, so it keeps running after the session is
gone.

This adds one scan to dispose. It looks for processes that carry the session's
`SCHMUX_SESSION_ID` in their environment and were orphaned by their shell, and adds them
to the list dispose already kills. How dispose kills things does not change.

## Background

On 2026-11-23 an agent in a fenced session ran a script with an infinite loop six times,
then tried to clean up with `pkill -f bach-humanize`. That matched the shells the script
was started from, and not node. The shells died, and six node processes ran at about 100%
CPU for 11 hours.

The loop and the `pkill` were the agent's bad code. schmux's part is that disposing the
session would have left the processes running: their parent was PID 1, and dispose finds
processes by walking down from the pane (`internal/session/reaper.go:47`).

## Goals

- Disposing a fenced session kills the processes its shells orphaned, apart from the known
  gaps below.
- This holds even when the tmux session is already gone at dispose time. Today that case
  kills nothing (`internal/session/manager.go:1912`).

## Non-goals

- Stopping a runaway process while its session is open. The session in the incident was
  still open, so this would have helped only once it was disposed.
- Changing unfenced or remote sessions.
- Cleaning up after sessions that were already disposed.

## Must not happen

- Dispose kills another session's process.
- Dispose kills a machine-wide daemon that started its own process group. The scan cannot
  protect one that stayed in its shell's group: it looks like any other orphan.

## How a process is identified

schmux starts each agent with `SCHMUX_SESSION_ID=<id>` (`manager.go:1123`, `1324`), and
every process the agent starts inherits it. Losing a parent does not remove it.

A process is killed when both are true:

1. **Its environment has this session's ID.** `ps axeww` prints a process's command
   followed by its environment, and `ps axww` prints the command alone. The scan runs both
   and searches only what `axeww` adds, so an ID that appears in a command line does not
   count. Both print only `-o pid=,pgid=,stat=,command=`: the default columns include CPU
   time, which changes between the listings for a busy process. Only the PID, group and
   command are compared. The ID must match exactly. The two listings are taken one after the other, so a
   process can appear in only one of them, or with a different command. The scan skips it.
2. **Its process-group leader is dead.** The leader is the process whose PID is the group's
   ID. A zombie leader counts as dead, as zombies already do in
   `internal/session/reaper.go:88`. An orphaned process is still in its dead shell's group. A
   daemon that started its own group is its own living leader.

The second condition is there because the ID alone is not enough. When the spike ran, a
machine-wide Codex daemon was running with the ID of the session that started it. From a
throwaway spike on macOS:

```
kill  pid 50825  group 50821   shell killed, node left running
kill  pid 50828  group 50822   nohup node & disown
keep  pid 50830  group 50830   child that started its own group
keep  pid 39291  group 39291   codex app-server daemon
```

The same cases ran on Linux in the `schmux-e2e-base` image as root (procps-ng 4.0.2), with
one more process that had the ID in its command line only. The result matches the table:
the first two kill, the third and fourth keep. The extra process was not found. They have
not run on Linux as another user.

## Changes

- **`internal/session/reaper.go`**: add `orphans(ctx, sessionID)`, which runs the scan and
  returns `procIdent` records like `enumerate` does, start times included. `reap` already
  uses those to avoid killing a reused PID.
- **`internal/session/manager.go`, `Dispose`**: for a fenced session, call `orphans` before
  killing tmux and append the result to `fencedProcs`. `reap` already ignores a PID that is
  listed twice. Do this whether or not tmux is alive or `enumerate` succeeded. If the scan
  fails, log a warning and dispose as today. Log what it found as PIDs only, as the
  enumerated tree is logged today (`internal/session/manager.go:1924`): the scan's output
  carries the environment of every process.
- **`docs/api.md:793`**: it says only the enumerated tree is signaled, that success means
  no live tree process remains, and that well-behaved fenced sessions dispose as fast as
  before. Add the orphan scan and its 0.1 s.

## What users will notice

- A `nohup` server, meaning one that an agent's shell started with `nohup ... & disown`,
  dies when the fenced session is disposed or restarted. Today it survives, although dispose
  deletes its preview (`internal/dashboard/handlers_dispose.go:65`). This is accepted (see
  Decisions).
- Dispose takes about 0.1 s longer (the scan took 105 ms in the macOS spike). When there
  are orphans it takes the grace period (5 s by default) plus up to about 5 s, unless they
  exit on their own first.

## Known gaps

- macOS hides the environment of its own binaries, such as `head` and `zsh`. They are
  killed only along with a process in the same group whose environment can be read, because
  `reap` signals the whole group (`internal/session/reaper.go:290`).
- A process started with a cleared environment is not found.
- With tmux already gone, a process whose group leader is still alive is not found. Under
  the real fence the agent sits in its `sh`'s group (macOS spike: agent 24919 in group 24916,
  led by its `sh`), so a live `sh` keeps it out of the scan.

## Approaches

- **Match the ID and require a dead group leader.** Chosen. It keeps a daemon that started
  its own group. Its cost is that `nohup` servers die with the session (see Decisions).
- **Match the ID alone.** Simpler, but kills the Codex daemon.
- **Record process groups while the session runs.** Finds the hidden processes, but needs
  polling and saved state, and misses anything orphaned between polls.

## Tests

In `internal/session/reaper_test.go`:

- A child left by a killed shell is found, and `reap` kills it.
- On Linux, the same when the killed shell is still a zombie.
- Not found: a child that started its own group; a different session's ID; a longer ID
  that begins with this one; the ID in the command line only.

In `internal/session/manager_test.go`:

- Disposing a fenced session kills its orphan, with tmux alive and with tmux gone.
- Disposing an unfenced session leaves its orphan running.
- The tmux-alive case creates its own tmux socket.

In both files:

- The orphan is the test binary started again. On macOS `head` and `zsh` showed no
  environment, while node and a Go binary did, so it must not be one of macOS's own
  binaries.
- Both call `requireProcTools`, so they skip when `ps` cannot run, as inside the fence.
  A skipped run is not evidence: results come from CI or an unfenced run.

## Decisions

1. **`nohup` servers die with the session.** Accepted on 2026-11-24. The scan cannot tell
   them from accidental orphans, and dispose already removes their preview.
2. **Unfenced sessions are left alone.** Unfenced disposal was left alone when the reaper
   was added (`0451223ed`) and is a tested contract
   (`internal/e2e/e2e_api_test.go:1113`). The incident session was fenced.
