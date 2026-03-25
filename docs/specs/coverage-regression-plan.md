# Coverage Plan: High-Regression-Risk Areas

Coverage baseline: 43.5% backend, 42.6% frontend (2,441 tests passing).

This plan targets the five areas where untested code poses the highest risk of
silent behavioral regressions. Each workstream is independent and can be
executed in parallel.

---

## Workstream 1: Daemon Lifecycle (internal/daemon)

**Current state:** 13.5% coverage, 1,545 LoC. `NewDaemon`, `Start`, `Stop`,
`Run`, `Shutdown` are all at 0%. Only `Status` (70%), `ValidateReadyToRun`
(9.5%), `createDevConfigBackup` (80%), and `cleanupOldBackups` (81%) have any
coverage.

**Why it matters:** A regression in daemon lifecycle means schmux won't start,
won't stop, or won't clean up — the product is bricked.

**Constraint:** `Daemon.Run` is a ~1,000-line orchestration function that wires
up config, state, workspace manager, session manager, dashboard server, and
background goroutines. Full integration testing belongs in E2E. The unit test
goal is to cover the testable sub-behaviors.

### Tests to add in `internal/daemon/daemon_test.go`

#### 1.1 ValidateReadyToRun — stale PID file cleanup

Currently only tests the "tmux missing" path. Add:

| Test                                      | Input                                            | Expected                                   |
| ----------------------------------------- | ------------------------------------------------ | ------------------------------------------ |
| `TestValidateReadyToRun_StalePidFile`     | PID file containing a PID of a dead process      | Returns nil (stale file removed)           |
| `TestValidateReadyToRun_RunningPid`       | PID file containing the current test process PID | Returns error containing "already running" |
| `TestValidateReadyToRun_MalformedPidFile` | PID file containing "not-a-number"               | Returns nil (treats as stale)              |
| `TestValidateReadyToRun_NoPidFile`        | No PID file exists                               | Returns nil                                |

**Approach:** Override `os.UserHomeDir` via environment or use a testable
wrapper. `ValidateReadyToRun` currently hardcodes `os.UserHomeDir()` — if
refactoring is needed, extract a `schmuxDir` parameter or use `t.Setenv("HOME",
tmpDir)` to isolate.

#### 1.2 Shutdown idempotency

| Test                          | Input                                  | Expected                                       |
| ----------------------------- | -------------------------------------- | ---------------------------------------------- |
| `TestShutdown_Idempotent`     | Call `Shutdown()` twice on same daemon | No panic, shutdownChan closed exactly once     |
| `TestShutdown_CancelsContext` | Call `Shutdown()`                      | `shutdownCtx.Err()` returns `context.Canceled` |

**Approach:** Direct — `NewDaemon()` + call `Shutdown()`.

#### 1.3 DevRestart

| Test                            | Input                     | Expected                  |
| ------------------------------- | ------------------------- | ------------------------- |
| `TestDevRestart_Idempotent`     | Call `DevRestart()` twice | No panic                  |
| `TestDevRestart_CancelsContext` | Call `DevRestart()`       | `shutdownCtx` is canceled |

#### 1.4 validateSessionAccess — session with inaccessible tmux

Currently only tests the empty-state case.

| Test                                            | Input                                                   | Expected                                   |
| ----------------------------------------------- | ------------------------------------------------------- | ------------------------------------------ |
| `TestValidateSessionAccess_WithOrphanedSession` | State has a session with a tmux name that doesn't exist | Returns error listing the orphaned session |
| `TestValidateSessionAccess_AllAccessible`       | State has sessions, all tmux sessions exist             | Returns nil                                |

**Approach:** Requires either mocking tmux or running with real tmux. Prefer
a mock approach: the function calls `tmux.HasSession` — if it uses an
interface, mock it; if not, skip in CI without tmux and mark as integration.

#### 1.5 startNudgeNikChecker — context cancellation

| Test                                            | Input                      | Expected                                                      |
| ----------------------------------------------- | -------------------------- | ------------------------------------------------------------- |
| `TestStartNudgeNikChecker_StopsOnContextCancel` | Cancel context immediately | Goroutine exits without calling checkInactiveSessionsForNudge |

**Approach:** Pass a pre-canceled context, verify no panics and the function
returns promptly.

**Estimated new coverage:** ~25 → 35% on daemon package (targeting
ValidateReadyToRun, Shutdown, DevRestart, validateSessionAccess).

---

## Workstream 2: Session Spawn & Dispose (internal/session)

**Current state:** 51.6% coverage, 2,005 LoC. The tested code is all
accessors/queries (`GetSession`, `GetAllSessions`, `IsRunning`, `buildCommand`,
nickname helpers). The core mutation paths — `Spawn`, `SpawnCommand`,
`SpawnRemote`, `Dispose`, `Stop`, `ResolveTarget`, `resolveWorkspace` — are all
at 0%.

**Why it matters:** Spawn and Dispose are the two most important user
operations. A regression in Spawn means agents can't start. A regression in
Dispose means orphaned tmux sessions and leaked disk space.

### Tests to add in `internal/session/manager_test.go`

#### 2.1 ResolveTarget

`ResolveTarget` maps a target name to a `ResolvedTarget` struct. It checks
config run_targets, detected tools, and model manager. This is pure logic
with no tmux dependency.

| Test                              | Input                                                              | Expected                                                                                        |
| --------------------------------- | ------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------- |
| `TestResolveTarget_UserRunTarget` | Config has `RunTarget{Name: "lint", Command: "golangci-lint run"}` | Returns `ResolvedTarget{Kind: TargetKindUser, Command: "golangci-lint run", Promptable: false}` |
| `TestResolveTarget_DetectedTool`  | No config match, detected tools include "claude"                   | Returns `ResolvedTarget{Kind: TargetKindDetected, Command: "claude", Promptable: true}`         |
| `TestResolveTarget_NotFound`      | Target name "nonexistent", no config or detected match             | Returns error containing "not found"                                                            |
| `TestResolveTarget_ModelTarget`   | Model manager has "claude-sonnet-4-6" registered                   | Returns `ResolvedTarget{Kind: TargetKindModel}` with correct env vars                           |

**Approach:** Create manager via `newTestManager`, configure `config.RunTargets`
and/or set a model manager with test data. `ResolveTarget` doesn't need tmux.

#### 2.2 resolveWorkspace

`resolveWorkspace` takes a repo+branch and calls workspace manager's
`GetOrCreate`. It's a thin delegation but validates the workspace result.

| Test                                  | Input                                                        | Expected                                   |
| ------------------------------------- | ------------------------------------------------------------ | ------------------------------------------ |
| `TestResolveWorkspace_Success`        | Valid repo URL + branch, workspace manager returns workspace | Returns workspace with correct ID and path |
| `TestResolveWorkspace_WorkspaceError` | Workspace manager's GetOrCreate returns error                | Returns error, no session created          |

**Approach:** Requires either a real workspace manager (with a real git repo in
`t.TempDir()`) or extracting `WorkspaceManager` as an interface on the session
side. The existing tests use real `workspace.New()` — follow that pattern with
a temp git repo.

#### 2.3 Dispose — happy path with stopped session

The current test only checks "nonexistent session returns error." Add:

| Test                           | Input                                                | Expected                                          |
| ------------------------------ | ---------------------------------------------------- | ------------------------------------------------- |
| `TestDispose_StoppedSession`   | Session in state with Status="stopped", no real tmux | Session removed from state, tracker stopped       |
| `TestDispose_CleansUpTracker`  | Session with active tracker                          | Tracker's Stop() called, tracker removed from map |
| `TestDispose_RemovesOutputLog` | Session with output log file                         | Log file deleted                                  |

**Approach:** Add a session to state, optionally create a tracker via
`GetTracker`, then call `Dispose`. Since tmux isn't running, the tmux kill
step will be a no-op (the function handles missing tmux sessions gracefully).
Verify state mutation and file cleanup.

#### 2.4 MarkSessionDisposing + RevertSessionStatus roundtrip

`RevertSessionStatus` is at 0%. It's the rollback path when dispose fails
mid-way.

| Test                                         | Input                                   | Expected                                |
| -------------------------------------------- | --------------------------------------- | --------------------------------------- |
| `TestRevertSessionStatus_RestoresOriginal`   | Session marked disposing from "running" | After revert, status is "running" again |
| `TestRevertSessionStatus_NonexistentSession` | Nonexistent session ID                  | No panic, no error (silent no-op)       |

**Approach:** Use existing `MarkSessionDisposing` + `RevertSessionStatus`.

**Estimated new coverage:** ~52 → 65% on session package (targeting
ResolveTarget, resolveWorkspace, Dispose happy path, RevertSessionStatus).

---

## Workstream 3: Dashboard Dispose & Spawn Handlers (internal/dashboard)

**Current state:** `handleDispose` at 10.8%, `handleDisposeWorkspaceAll` at
13.3%, `handleSpawnPost` at 69.7%. All workspace preview handlers at 0%. All
emergence CRUD handlers at 0%.

**Why it matters:** These are the HTTP API endpoints users interact with. The
dispose handler has the highest blast radius — it deletes sessions and
workspaces. The spawn handler is the entry point for creating agents.

### Tests to add

#### 3.1 handleDispose — beyond guards

The existing test only checks the "missing session ID" guard. Add to
`internal/dashboard/handlers_dispose_test.go`:

| Test                                   | Input                                                 | Expected                        |
| -------------------------------------- | ----------------------------------------------------- | ------------------------------- |
| `TestHandleDispose_NonexistentSession` | DELETE with valid-format but nonexistent session ID   | 404 with error message          |
| `TestHandleDispose_SuccessfulDispose`  | DELETE with session ID that exists in state (stopped) | 200, session removed from state |
| `TestHandleDispose_AlreadyDisposing`   | DELETE with session in "disposing" status             | 200 (idempotent) or 409         |

**Approach:** Use `newTestServer`, add workspace + session to state, call
handler directly via `httptest.NewRecorder`. The session manager's `Dispose`
will attempt tmux cleanup (which silently fails on missing sessions — safe
in tests).

#### 3.2 handleDisposeWorkspace — beyond guards

| Test                                              | Input                                | Expected                                                    |
| ------------------------------------------------- | ------------------------------------ | ----------------------------------------------------------- |
| `TestHandleDisposeWorkspace_NonexistentWorkspace` | DELETE with unknown workspace ID     | 404                                                         |
| `TestHandleDisposeWorkspace_WithActiveSessions`   | Workspace with running sessions      | 409 or sessions disposed first, depending on implementation |
| `TestHandleDisposeWorkspace_EmptyWorkspace`       | Workspace with no sessions, real dir | 200, workspace removed from state                           |

**Approach:** Use `newTestServer`. For the "real dir" case, create a temp dir
as the workspace path so disposal can actually remove it.

#### 3.3 handleDisposeWorkspaceAll — beyond guards

| Test                                                 | Input                             | Expected                                 |
| ---------------------------------------------------- | --------------------------------- | ---------------------------------------- |
| `TestHandleDisposeWorkspaceAll_DisposesAllSessions`  | Workspace with 2 stopped sessions | 200, both sessions and workspace removed |
| `TestHandleDisposeWorkspaceAll_NonexistentWorkspace` | Unknown workspace ID              | 404                                      |

#### 3.4 handleSpawnPost — gap in 69.7% coverage

The existing tests cover validation errors. Find uncovered branches by
inspecting the handler. Likely gaps:

| Test                                    | Input                                            | Expected                                    |
| --------------------------------------- | ------------------------------------------------ | ------------------------------------------- |
| `TestHandleSpawnPost_QuickLaunchByName` | Body with `quick_launch: "name"`                 | Resolves quick launch, spawns session       |
| `TestHandleSpawnPost_ResumeWithTargets` | Body with `resume: true, targets: {"claude": 1}` | Validates resume is compatible with targets |
| `TestHandleSpawnPost_MultipleTargets`   | Body with `targets: {"claude": 2, "codex": 1}`   | Spawns 3 sessions across 2 targets          |

**Approach:** Use `postSpawnJSON` helper already in the test file. These will
fail at the workspace creation step (no real git repos), so assert the
validation succeeds and the error comes from workspace/spawn, not from
input validation.

#### 3.5 Workspace preview handlers

| Test                                | Input                           | Expected      |
| ----------------------------------- | ------------------------------- | ------------- |
| `TestValidateWorkspaceID_Valid`     | Well-formed workspace ID        | Returns nil   |
| `TestValidateWorkspaceID_Empty`     | Empty string                    | Returns error |
| `TestValidateWorkspaceID_Traversal` | ID containing `../`             | Returns error |
| `TestIsValidResourceID_Injection`   | ID containing semicolons, pipes | Returns false |

**Approach:** Direct function calls — these are pure validation functions.

**Estimated new coverage:** ~40 → 50% on dashboard package for handler files
(targeting dispose, spawn, workspace validation).

---

## Workstream 4: WebSocket Terminal Handlers (internal/dashboard)

**Current state:** `handleTerminalWebSocket` at 3.1%. All other WS handlers
(FM terminal, CR terminal, remote terminal, provision) at 0%.
`HandleStatusEvent` at 76.7%, helper functions well-tested.

**Why it matters:** WebSocket is the primary real-time interaction surface. Any
regression silently breaks the terminal experience with no visible HTTP error.

### Tests to add in `internal/dashboard/websocket_test.go`

#### 4.1 checkWSOrigin

This is a pure validation function (0% covered) that gates all WebSocket
upgrades.

| Test                                     | Input                            | Expected                                                                 |
| ---------------------------------------- | -------------------------------- | ------------------------------------------------------------------------ |
| `TestCheckWSOrigin_AllowsLocalhost`      | Origin: `http://localhost:7337`  | Returns true                                                             |
| `TestCheckWSOrigin_RejectsUnknown`       | Origin: `http://evil.com`        | Returns false                                                            |
| `TestCheckWSOrigin_AllowsConfiguredHost` | Origin matches `public_base_url` | Returns true                                                             |
| `TestCheckWSOrigin_EmptyOrigin`          | No Origin header                 | Returns true (browser-less clients) or false (depends on implementation) |

**Approach:** Create a `Server` with test config, call `checkWSOrigin` with
a fabricated `*http.Request`.

#### 4.2 handleTerminalWebSocket — connection lifecycle

Testing real WebSocket handlers requires an `httptest.Server` with a real
upgrader. The existing test infrastructure already imports `gorilla/websocket`
(see `server_test.go`).

| Test                                      | Input                                                    | Expected                                          |
| ----------------------------------------- | -------------------------------------------------------- | ------------------------------------------------- |
| `TestTerminalWS_RejectsInvalidSessionID`  | Connect to `/ws/terminal/nonexistent`                    | WebSocket close message with error                |
| `TestTerminalWS_SendsInitialOutput`       | Connect to valid session with existing output in tracker | First message contains buffered output            |
| `TestTerminalWS_ClientDisconnectCleansUp` | Connect then close client side                           | Server-side subscriber removed, no goroutine leak |

**Approach:** Use `httptest.NewServer` with the chi router. Add a session to
state, optionally create a tracker. Connect via `websocket.Dial`. These tests
require more setup than HTTP handler tests but are high-value.

#### 4.3 buildSyncMessage (100% covered — verify it stays covered)

Already well-tested. No action needed, but flag as a regression canary — if
this breaks, WebSocket sync is broken.

**Estimated new coverage:** 3% → ~15-20% on websocket.go (targeting
checkWSOrigin and basic connection lifecycle). Full WS coverage requires
tmux or mocked trackers.

---

## Workstream 5: FloorManager Spawn/Monitor Loop (internal/floormanager)

**Current state:** 62.8% coverage, 738 LoC. All tested code is in
injector logic and `writeInstructionFiles`. The entire management loop —
`Start`, `Stop`, `spawn`, `spawnResume`, `monitor`, `checkAndRestart`,
`HandleRotation`, `buildFMCommand` — is at 0%.

**Why it matters:** FloorManager automatically restarts sessions that exit and
handles shift rotations. A regression means dead agents don't get restarted,
or rotations silently fail.

### Tests to add in `internal/floormanager/manager_test.go`

#### 5.1 buildFMCommand — pure logic

Builds the shell command for spawning a floor-managed session.

| Test                             | Input                                  | Expected                                                 |
| -------------------------------- | -------------------------------------- | -------------------------------------------------------- |
| `TestBuildFMCommand_BasicTarget` | Target "claude", workspace dir, prompt | Command string contains `claude`, prompt, workspace path |
| `TestBuildFMCommand_WithEnvVars` | Target with env map                    | Command string has env prefix                            |
| `TestBuildFMCommand_EmptyPrompt` | Promptable target, empty prompt        | Returns error                                            |

#### 5.2 buildFMResumeCommand — pure logic

| Test                              | Input         | Expected                         |
| --------------------------------- | ------------- | -------------------------------- |
| `TestBuildFMResumeCommand_Claude` | Tool "claude" | Command contains `--continue`    |
| `TestBuildFMResumeCommand_Codex`  | Tool "codex"  | Command contains `resume --last` |

#### 5.3 resolveSessionName

At 40% — partially covered. Fill gaps:

| Test                                           | Input                                     | Expected                         |
| ---------------------------------------------- | ----------------------------------------- | -------------------------------- |
| `TestResolveSessionName_ConflictAppendsSuffix` | Name conflicts with existing tmux session | Returns name with numeric suffix |
| `TestResolveSessionName_EmptyPrefix`           | Empty prefix                              | Returns a generated name         |

#### 5.4 resolveTarget — pure logic

| Test                                    | Input                          | Expected                |
| --------------------------------------- | ------------------------------ | ----------------------- |
| `TestResolveTarget_FindsConfigTarget`   | Config has matching run target | Returns resolved target |
| `TestResolveTarget_FallsBackToDetected` | No config match, tool detected | Returns detected target |
| `TestResolveTarget_NothingFound`        | No match anywhere              | Returns error           |

#### 5.5 checkAndRestart — requires tmux mock or integration

| Test                                        | Input                                        | Expected                                     |
| ------------------------------------------- | -------------------------------------------- | -------------------------------------------- |
| `TestCheckAndRestart_DeadSessionRespawns`   | Session marked running but tmux session gone | Respawn triggered, restart count incremented |
| `TestCheckAndRestart_RunningSessionSkipped` | Session still running in tmux                | No respawn, no state change                  |

**Approach:** If tmux is available (`tmuxAvailable()` helper already exists),
run as integration test. Otherwise skip.

**Estimated new coverage:** 63% → 75-80% on floormanager (targeting build\*
functions, resolveTarget, resolveSessionName).

---

## Execution Order

These workstreams are independent. Priority by regression risk × effort ratio:

1. **Workstream 2 (Session Spawn/Dispose)** — Highest value. `ResolveTarget`
   and `Dispose` happy path are pure logic, immediately testable with existing
   `newTestManager` helper.

2. **Workstream 3 (Dashboard Dispose/Spawn handlers)** — Second highest value.
   `newTestServer` helper already provides full infrastructure. Workspace
   validation functions are trivial to test.

3. **Workstream 5 (FloorManager)** — `buildFMCommand` and `resolveTarget` are
   pure functions, easy wins.

4. **Workstream 1 (Daemon)** — `Shutdown`/`DevRestart` idempotency tests are
   trivial. `ValidateReadyToRun` with stale PID needs HOME override.

5. **Workstream 4 (WebSocket)** — Highest setup cost (real httptest.Server +
   WebSocket dial), but `checkWSOrigin` alone is worth it as a quick win.

## Expected Impact

| Workstream | Package                 | Before | After (est.) |
| ---------- | ----------------------- | ------ | ------------ |
| 1          | internal/daemon         | 13.5%  | ~30%         |
| 2          | internal/session        | 51.6%  | ~65%         |
| 3          | internal/dashboard      | 40.4%  | ~48%         |
| 4          | internal/dashboard (ws) | 3.1%   | ~15%         |
| 5          | internal/floormanager   | 62.8%  | ~78%         |

Total backend coverage estimate: 43.5% → ~50%.

## What This Plan Does NOT Cover

- **`Daemon.Run`** (1,000+ lines of orchestration): Properly covered by E2E
  tests. Unit-testing it would require mocking 10+ subsystems.
- **`internal/remote/connection.go`** (Connect/Reconnect at 0%): Requires SSH
  session mocking. Better covered by E2E remote tests.
- **`internal/workspace/linear_sync.go`** (LinearSync at 0%): Requires real
  git repos with conflict states. Already has E2E coverage for the happy path.
- **Frontend route components** (23.1%): Separate workstream, React-specific.
