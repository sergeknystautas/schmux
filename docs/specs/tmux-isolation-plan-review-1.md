VERDICT: NEEDS_REVISION

## Summary Assessment

The plan covers the core design well and most file paths and line references are accurate, but it has several critical gaps: missing files in the rename step, an uncovered type assertion, incomplete scenario test updates, incorrect commit instructions, and Step 2 is far too large for a single "bite-sized" task.

## Critical Issues (must fix)

### C1. Step 5a: Rename list is incomplete -- missing 2 files, includes 1 wrong file

The plan lists files containing `SessionTracker` references to rename:

- `internal/dashboard/websocket_test.go` -- **wrong.** This file has zero `SessionTracker` references (confirmed via grep). Remove it.
- `internal/dashboard/websocket_helpers.go` -- **missing.** Contains `waitForTrackerAttach(tracker *session.SessionTracker, ...)` at line 84.
- `internal/dashboard/handlers_sync.go` -- **missing.** Contains `session.NewSessionTracker(...)` at line 417.

These omissions will cause compile failures if the rename is applied without updating these files.

### C2. Step 4: Misses the 5th type assertion -- HealthProbe extraction in `NewSessionTracker` constructor

The plan identifies 4 type assertions to `*LocalSource` (lines 107, 121, 196, 305). There is a 5th: the `switch s := source.(type)` at lines 132-139 in `NewSessionTracker()`, which type-switches on both `*LocalSource` and `*RemoteSource` to extract the health probe. This needs either an optional `HealthProbeProvider` interface or another approach. Without addressing it, the constructor retains a concrete type assertion that breaks the abstraction the plan is trying to establish.

### C3. Step 3: RemoteSource file path is wrong

Step 3c gives the implementation location as `internal/remote/remotesource.go`. The actual file is `internal/session/remotesource.go`. The `RemoteSource` struct lives in the `session` package, not the `remote` package. Additionally, the sample code references `s.conn.Client().Execute(...)` but the actual `RemoteSource` uses `s.conn.SendKeys(...)` for key operations (see `remotesource.go` line 43). The implementer will need to verify how `Execute` is exposed on the connection.

### C4. All commit steps use `git commit` -- CLAUDE.md requires `/commit`

Every step's commit instruction is `git commit -m "..."`. CLAUDE.md explicitly states: "ALWAYS use `/commit` to create commits. NEVER run `git commit` directly." The `/commit` command enforces definition-of-done checks including `./test.sh` and API docs verification. All 19 commit instructions must be changed to `/commit`.

### C5. Step 2 is not bite-sized -- migrating 15 methods is 20+ minutes of work

Step 2 ("Migrate admin methods to TmuxServer") asks to copy and modify 15 methods from package-level functions to struct methods in a 714-line file with 36 functions. This includes `StartServer`, `CreateSession`, `KillSession`, `ListSessions`, `SessionExists`, `GetAttachCommand`, `SetOption`, `ConfigureStatusBar`, `GetPanePID`, `CaptureOutput`, `CaptureLastLines`, `GetCursorState`, `RenameSession`, `ShowEnvironment`, `SetEnvironment`. Each method needs body modification (replacing `exec.CommandContext` with `s.cmd()`), plus tests. This is at least 4-5x larger than a "2-5 minute" task. Split into 2-3 substeps (e.g., session lifecycle methods, capture/query methods, environment methods).

### C6. Step 17: Scenario test `helpers-terminal.ts` update is incomplete

The plan says to update the regex at line 24 of `helpers-terminal.ts`. But the file has **8+ additional bare `tmux` shell-outs** that all need `-L schmux`:

- Line 41: `tmux send-keys -t ...`
- Line 42: `tmux send-keys -t ... Enter`
- Line 61-63: `tmux capture-pane -p -t ...`
- Line 195: `tmux clear-history -t ...`
- Line 297: `tmux display-message -p -t ...`
- Line 354: `tmux display-message -p -t ...`

These are the functions `sendTmuxCommand`, `capturePane`, `clearTmuxHistory`, `getTmuxCursorPosition`, and `getTmuxCursorVisible`. Without updating them, every scenario test that interacts with tmux will fail against the isolated socket.

### C7. Step 16: `daemon_test.go` will break -- not accounted for

Step 16 removes `TmuxChecker`, `Checker` interface, and `NewDefaultChecker`. But `internal/daemon/daemon_test.go` uses `tmux.TmuxChecker` 10 times and `tmux.Checker` once (line 75: `mockChecker` implements `tmux.Checker`). The test file needs to be rewritten to work with `TmuxServer.Check()` instead. This is not mentioned anywhere in the plan.

### C8. Step 6: Misses second `tmux.SetBinary` call site in `daemon.go`

Step 6 mentions replacing line 192 (`exec.Command(tmux.Binary(), "-v", "start-server")`). But `daemon.go` also has:

- Line 183: `tmux.SetBinary(cfg.TmuxBinary)` (in `Start()`)
- Line 347: `tmux.SetLogger(logging.Sub(logger, "tmux"))`
- Line 410: `tmux.SetBinary(cfg.TmuxBinary)` (in `Run()`)

All three are globals being removed. The plan should explicitly note that the `TmuxServer` constructor absorbs the binary from config and the logger, replacing both `SetBinary` and `SetLogger` call sites.

## Suggestions (nice to have)

### S1. Step 11/12: handlers_tell and handlers_capture also have remote branches

Both handlers have `if sess.RemoteHostID != "" { ... } else { ... }` branching. The plan only migrates the local `else` branch to use `SessionRuntime`. Consider noting that the remote branch could also use `SessionRuntime` (since `RemoteSource` implements the same `ControlSource` interface), which would further unify the code and eliminate the branch entirely. Not required for correctness, but it is a missed opportunity.

### S2. Test commands should end with `./test.sh --quick` not just `go test ./internal/...`

Most steps use `go test ./internal/tmux/ -v` or similar as the verification command. While these are fine for fast iteration during development, CLAUDE.md says `./test.sh --quick` is the minimum for cross-package verification. Consider noting that at least every 2-3 steps should run `./test.sh --quick` as a checkpoint, not just the final Step 19.

### S3. Step 9 changes `NewLocalSource` signature but doesn't list all callers

Step 9 adds `*tmux.TmuxServer` to `NewLocalSource`. The callers that need updating:

- `internal/session/manager.go` line 1530
- `internal/session/localsource_test.go` lines 32, 37, 57, 64
- `internal/session/tracker_test.go` lines 29, 162
- `internal/session/tracker_bench_test.go` lines 31, 82

The plan should list these so the implementer doesn't have to discover them.

### S4. Step 18 references `cmd/schmux/status.go` which does not exist

The status command lives in `cmd/schmux/main.go` (lines 92-105), not a separate `status.go` file. The step header should reference the correct file.

### S5. Dependency group 2 (Steps 3-4) is not truly independent of Group 1

The plan says Groups 1 and 2 can run in parallel. Step 3 adds methods to `ControlSource` and implementations in `LocalSource`/`RemoteSource`. While these don't depend on `TmuxServer` code, Step 4b modifies `tracker.go` which is also touched by Step 5 (rename). If Groups 1 and 2 actually run in parallel branches, they will have merge conflicts in the same files. The parallelism claim is misleading.

### S6. No task for the multi-daemon guard described in the design

Design Section 8 describes a startup guard: "On startup, the daemon checks `TmuxServer.ListSessions()`. If sessions exist that are not in the daemon's state store, it logs a warning." This is not reflected in any plan step. It should be added to Step 6 or as a new step.

## Verified Claims (things I confirmed are correct)

- **File paths exist:** `internal/tmux/tmux.go`, `internal/session/controlsource.go`, `internal/session/localsource.go`, `internal/session/remotesource.go`, `internal/session/tracker.go`, `internal/session/manager.go`, `internal/floormanager/manager.go`, `internal/floormanager/injector.go`, `internal/dashboard/handlers_tell.go`, `internal/dashboard/handlers_capture.go`, `internal/dashboard/handlers_debug_tmux.go`, `internal/dashboard/handlers_environment.go`, `internal/dashboard/server.go`, `internal/dashboard/websocket.go`, `internal/e2e/e2e.go`, `internal/e2e/e2e_test.go`, `cmd/schmux/attach.go`, `test/scenarios/generated/helpers-terminal.ts` all exist.
- **Line references verified correct:** `handlers_tell.go` lines 64-69, `handlers_capture.go` line 60, `floormanager/injector.go` lines 115-124, `floormanager/manager.go` lines 346-350, `websocket.go` lines 331/367/994/1099, `HomePage.tsx` lines 689/1367, `tmux-tab.tsx` line 101, `helpers-terminal.ts` line 24 regex, `e2e.go` lines 551/560/894/1019, `e2e_test.go` lines 104/109.
- **ControlSource interface:** Current interface at `controlsource.go` matches what the plan describes (7 methods, no `SendTmuxKeyName` or `IsAttached` -- those are the additions).
- **Type assertions in tracker.go:** Confirmed 5 type assertions at lines 107, 121, 132-136, 196, 305 (plan only accounts for 4).
- **`GetTracker` method:** Confirmed at `manager.go` line 1487-1488 returning `*SessionTracker`.
- **Session manager field name:** Confirmed as `s.session` on the dashboard `Server` struct (line 112 of `server.go`).
- **`tmux.Binary()` callers:** Confirmed at `localsource.go:187`, `daemon.go:192`, `handlers_debug_tmux.go:47`, `attach.go:65`.
- **`tmux.SetBaseline` caller:** Confirmed at `handlers_environment.go:151`.
- **`injector_test.go` tmux calls:** Confirmed at lines 123 (`tmux.SendLiteral`) and 141 (`tmux.CaptureOutput`).
- **`floormanager.New()` signature:** Currently takes `(cfg, sm, homeDir, logger)` -- plan correctly adds `*TmuxServer`.
- **No `status.go` file exists:** Status command is inline in `main.go` at line 92.
