# Test Coverage Analysis Report

**Date:** 2026-02-23
**Branch:** `fix/coverage`
**Commit:** `960a5bd1`

---

## How to Generate Coverage Reports

Run the test suite with coverage enabled:

```bash
./test.sh --coverage
```

This runs both backend (Go) and frontend (Vitest) test suites and prints structured per-package coverage reports after the test summary. The output includes:

- **Backend**: Per-package table with coverage %, function count, uncovered count, and LoC — sorted by coverage ascending. Packages below 40% coverage with >200 LoC get a "weakest areas" breakdown listing uncovered functions.
- **Frontend**: Per-directory table with statement and function coverage percentages.

Coverage thresholds are color-coded: red (<30%), yellow (30-60%), green (>60%).

You can also run coverage for a single suite:

```bash
./test.sh --coverage --backend    # Backend only
./test.sh --coverage --frontend   # Frontend only
```

The raw Go coverage profile is written to `coverage.out` for further analysis (e.g., `go tool cover -html=coverage.out` for an interactive HTML report).

---

## Overall Numbers

| Suite            | Statement Coverage | Test Count |
| ---------------- | ------------------ | ---------- |
| Backend (Go)     | **34.9%**          | 700        |
| Frontend (React) | **51.0%**          | 363        |
| **Combined**     | —                  | **1,063**  |

---

## Backend Coverage by Package

Sorted by ascending average coverage. "LoC" is non-blank, non-comment lines in non-test `.go` files. "@0%" is the count of functions with zero coverage.

| Package                       | Avg Cov | Funcs | @0% | LoC   |
| ----------------------------- | ------- | ----- | --- | ----- |
| `cmd/build-dashboard`         | 0.0%    | 5     | 5   | 88    |
| `cmd/gen-types`               | 0.0%    | 10    | 10  | 187   |
| `cmd/scan-ansi`               | 0.0%    | 2     | 2   | 71    |
| `internal/assets`             | 0.0%    | 8     | 8   | 193   |
| `internal/benchutil`          | 0.0%    | 2     | 2   | 84    |
| `internal/commitmessage`      | 0.0%    | 1     | 1   | 11    |
| `internal/update`             | 0.0%    | 9     | 9   | 297   |
| `internal/vcs`                | 0.0%    | 23    | 23  | 153   |
| `internal/daemon`             | 23.1%   | 13    | 8   | 885   |
| `internal/oneshot`            | 24.9%   | 13    | 9   | 337   |
| `cmd/schmux`                  | 25.7%   | 79    | 57  | 1,511 |
| `internal/remote`             | 29.2%   | 60    | 38  | 1,422 |
| `internal/dashboard`          | 34.2%   | 240   | 128 | 9,785 |
| `internal/schema`             | 36.7%   | 5     | 3   | 122   |
| `internal/session`            | 38.4%   | 61    | 31  | 1,465 |
| `internal/config`             | 39.3%   | 148   | 81  | 2,112 |
| `internal/github`             | 39.5%   | 19    | 11  | 375   |
| `internal/tunnel`             | 42.8%   | 18    | 7   | 448   |
| `internal/remote/controlmode` | 50.2%   | 44    | 19  | 809   |
| `internal/difftool`           | 51.6%   | 11    | 5   | 249   |
| `internal/tmux`               | 56.1%   | 29    | 9   | 428   |
| `internal/nudgenik`           | 56.5%   | 8     | 3   | 149   |
| `pkg/cli`                     | 56.6%   | 14    | 5   | 414   |
| `internal/preview`            | 56.8%   | 21    | 5   | 499   |
| `internal/branchsuggest`      | 57.6%   | 5     | 2   | 127   |
| `internal/workspace`          | 58.9%   | 158   | 44  | 4,565 |
| `internal/detect`             | 59.3%   | 43    | 11  | 884   |
| `internal/logging`            | 60.0%   | 3     | 1   | 61    |
| `internal/state`              | 60.1%   | 54    | 19  | 723   |
| `internal/telemetry`          | 63.7%   | 8     | 2   | 158   |
| `internal/lore`               | 63.8%   | 39    | 8   | 933   |
| `internal/compound`           | 72.3%   | 35    | 5   | 733   |
| `internal/workspace/ensure`   | 74.2%   | 24    | 4   | 499   |
| `internal/signal`             | 84.1%   | 16    | 1   | 375   |
| `internal/escbuf`             | 85.9%   | 3     | 0   | 85    |
| `internal/conflictresolve`    | 98.6%   | 7     | 0   | 159   |
| `pkg/shellutil`               | 100.0%  | 1     | 0   | 5     |

---

## Critical Risk Areas — Detailed Breakdown

### 1. `internal/dashboard` — CRITICAL (34.2%, 9,785 LoC, 128 uncovered functions)

This is the largest package in the project and contains the entire HTTP API surface. Over half its functions have zero test coverage.

#### 1a. Authentication (`auth.go`) — 0% coverage, security-sensitive

Every function in the auth flow is untested:

| Function              | Line | Purpose                                       |
| --------------------- | ---- | --------------------------------------------- |
| `authRedirectURI`     | 70   | Builds OAuth redirect URL                     |
| `authCookieSecure`    | 81   | Determines if cookies should be HTTPS-only    |
| `withAuthHandler`     | 125  | Auth middleware wrapping all protected routes |
| `handleAuthLogin`     | 186  | Initiates GitHub OAuth login                  |
| `handleAuthCallback`  | 229  | Processes OAuth callback, sets session        |
| `handleAuthLogout`    | 279  | Clears session cookie                         |
| `handleAuthMe`        | 311  | Returns current user info                     |
| `exchangeGitHubToken` | 327  | Exchanges OAuth code for access token         |
| `fetchGitHubUser`     | 375  | Fetches GitHub user profile                   |
| `setSessionCookie`    | 413  | Writes signed session cookie                  |
| `parseSessionCookie`  | 462  | Reads and verifies session cookie             |
| `signPayload`         | 496  | HMAC signing for cookie integrity             |
| `sessionKey`          | 502  | Derives session encryption key                |
| `setCookie`           | 532  | Low-level cookie writer                       |

**Regression risk:** Any change to cookie format, HMAC signing, OAuth flow, or middleware ordering could silently break authentication. Since auth gates all API access when enabled, a regression here locks users out entirely or — worse — lets unauthenticated requests through.

#### 1b. Git mutation handlers — 0% coverage, data-loss-sensitive

| Function               | Line | Purpose                            |
| ---------------------- | ---- | ---------------------------------- |
| `handleGitCommitStage` | 372  | Stage files and create git commits |
| `handleGitAmend`       | 437  | Amend the most recent commit       |
| `handleGitDiscard`     | 525  | Discard uncommitted changes        |
| `handleGitUncommit`    | 645  | Undo the last commit               |

**Regression risk:** These handlers mutate user git repositories. Incorrect parameter parsing, missing validation, or wrong git command construction could discard user work or corrupt history.

#### 1c. Sync/merge handlers — 0% coverage, data-loss-sensitive

| Function                                     | Line | Purpose                           |
| -------------------------------------------- | ---- | --------------------------------- |
| `handleLinearSyncFromMain`                   | 102  | Rebase workspace branch onto main |
| `runLinearSyncFromMain`                      | 206  | Executes the rebase operation     |
| `handleLinearSyncToMain`                     | 265  | Merge workspace branch into main  |
| `handlePushToBranch`                         | 328  | Push branch to remote             |
| `handleLinearSyncResolveConflict`            | 403  | Interactive conflict resolution   |
| `handleDeleteLinearSyncResolveConflictState` | 602  | Cleanup conflict state            |

Plus 11 functions in `linear_sync_conflict_state.go` (`AddStep`, `UpdateStep`, `Finish`, `SetHash`, `SetTmuxSession`, `MarshalJSON`, etc.).

**Regression risk:** These orchestrate multi-step git rebase/merge operations with conflict tracking. A regression could leave a workspace in a half-rebased state, lose conflict resolution progress, or silently drop commits during sync.

#### 1d. Remote session handlers — 0% coverage

| Function                     | Line | Purpose                            |
| ---------------------------- | ---- | ---------------------------------- |
| `handleRemoteFlavors`        | 47   | List remote environment flavors    |
| `handleGetRemoteFlavors`     | 59   | Get flavor details                 |
| `handleCreateRemoteFlavor`   | 81   | Create new flavor                  |
| `handleRemoteFlavor`         | 141  | CRUD for individual flavor         |
| `handleRemoteHosts`          | 249  | List connected remote hosts        |
| `handleRemoteHostConnect`    | 297  | Initiate SSH connection            |
| `handleRemoteHostReconnect`  | 383  | Reconnect dropped connection       |
| `handleRemoteHostDisconnect` | 458  | Disconnect from remote host        |
| `handleRemoteHostRoute`      | 493  | Route request to remote host       |
| `handleRemoteFlavorStatuses` | 521  | Get flavor connection statuses     |
| `handleRemoteConnectStream`  | 600  | SSE stream for connection progress |

**Regression risk:** Remote session management involves SSH connections, tmux control mode, and connection state machines. Regressions could leave zombie SSH connections, fail to detect disconnects, or route terminal I/O to the wrong session.

#### 1e. WebSocket handlers — 0% coverage

| Function                        | Line | Purpose                                |
| ------------------------------- | ---- | -------------------------------------- |
| `isTerminalResponse`            | 37   | Classify terminal output messages      |
| `checkWSOrigin`                 | 95   | WebSocket origin validation (security) |
| `handleCRTerminalWebSocket`     | 629  | Conflict resolution terminal stream    |
| `handleRemoteTerminalWebSocket` | 745  | Remote terminal WebSocket proxy        |
| `handleProvisionWebSocket`      | 927  | Provisioning progress WebSocket        |

**Regression risk:** WebSocket handlers manage real-time terminal streaming. Origin checking is a security boundary. Regressions could break live terminal output, introduce XSS vectors via missing origin validation, or cause goroutine leaks from unclosed connections.

#### 1f. Server lifecycle — 0% coverage

| Function                       | Line | Purpose                                 |
| ------------------------------ | ---- | --------------------------------------- |
| `Start`                        | 352  | Start HTTP server, register routes      |
| `Stop`                         | 504  | Graceful shutdown                       |
| `doBroadcast`                  | 926  | Fan-out WebSocket broadcasts            |
| `broadcastToAllDashboardConns` | 1049 | Send to all dashboard WebSocket clients |
| `cleanup`                      | 1234 | Resource cleanup on shutdown            |

**Regression risk:** Server startup/shutdown ordering affects connection handling, resource leaks, and graceful degradation.

#### 1g. Other untested handler groups

- **Diff handlers** (`handlers_diff.go`): `getFileContent`, `handleFile`, `serveWorkspaceFile`, `fileMatchesGitignore`, `handleRemoteDiff`, `handleDiffExternal`, `handleRemoteDiffExternal` — 0%
- **Lore handlers** (`handlers_lore.go`): All 10 functions — 0%
- **Model handlers** (`handlers_models.go`): `handleModels`, `handleModel`, `validateModelSecrets` — 0%
- **Overlay handlers** (`handlers_overlay.go`): `handleRefreshOverlay`, `handleOverlayScan`, `handleOverlayAdd`, `handleDismissNudge` — 0%
- **PR handlers** (`handlers_pr.go`): `handlePRs`, `handlePRRefresh`, `handlePRCheckout` — 0%
- **Config handlers** (`handlers_config.go`): `handleConfig`, `handleAuthSecrets` — 0%
- **Dev handlers** (`handlers_dev.go`): `handleDevStatus`, `handleDevRebuild`, `pauseViteWatch`, `resumeViteWatch`, `handleDevClearPassword` — 0%
- **Spawn handlers** (`handlers_spawn.go`): `handlePrepareBranchSpawn`, `adaptQuickLaunch`, `handleCheckBranchConflict`, `handleRecentBranches`, `handleRecentBranchesRefresh` — 0%
- **Commit handlers** (`commit.go`): `CommitPrompt`, `handleCommitGenerate`, `parseNumstat` — 0%
- **Static file handlers** (`handlers.go`): `handleApp`, `serveFileIfExists`, `serveAppIndex`, `handleWorkspacesScan`, `shellSplit` — 0%

---

### 2. `internal/session` — HIGH (38.4%, 1,465 LoC, 31 uncovered functions)

This package manages the core lifecycle of agent sessions — spawning, tracking, and disposing tmux sessions.

#### Uncovered critical functions

| Function                   | File:Line       | Purpose                                    |
| -------------------------- | --------------- | ------------------------------------------ |
| `Spawn`                    | manager.go:587  | Create a local tmux session for an agent   |
| `SpawnRemote`              | manager.go:362  | Create a session on a remote host          |
| `SpawnCommand`             | manager.go:728  | Run a one-off command in a session         |
| `ResolveTarget`            | manager.go:824  | Resolve agent target config to binary path |
| `ensureModelSecrets`       | manager.go:1007 | Inject API keys into session environment   |
| `disposeRemoteSession`     | manager.go:1130 | Tear down a remote session                 |
| `nicknameExists`           | manager.go:1261 | Check for session name collisions          |
| `generateUniqueNickname`   | manager.go:1282 | Generate unique session names              |
| `updateTrackerSessionName` | manager.go:1390 | Update tracker after rename                |
| `SetRemoteManager`         | manager.go:96   | Wire up remote manager dependency          |
| `SetSignalCallback`        | manager.go:102  | Register signal handler                    |
| `SetOutputCallback`        | manager.go:108  | Register output handler                    |
| `SetCompoundCallback`      | manager.go:114  | Register compound handler                  |
| `SetLoreCallback`          | manager.go:120  | Register lore handler                      |
| `SetTelemetry`             | manager.go:131  | Wire up telemetry                          |
| `trackSessionCreated`      | manager.go:137  | Emit session-created telemetry event       |
| `StartRemoteSignalMonitor` | manager.go:153  | Poll remote sessions for signal files      |
| `StopRemoteSignalMonitor`  | manager.go:328  | Stop the remote signal poller              |
| `GetRemoteManager`         | manager.go:353  | Accessor                                   |

| Function             | File:Line      | Purpose                                  |
| -------------------- | -------------- | ---------------------------------------- |
| `IsAttached`         | tracker.go:80  | Check if user is viewing session         |
| `fanOut`             | tracker.go:168 | Fan-out terminal output to subscribers   |
| `CaptureLastLines`   | tracker.go:221 | Get recent terminal output               |
| `GetCursorState`     | tracker.go:246 | Get terminal cursor position and mode    |
| `GetCursorPosition`  | tracker.go:258 | Get raw cursor coordinates               |
| `DiagnosticCounters` | tracker.go:270 | Debugging stats for terminal tracking    |
| `attachControlMode`  | tracker.go:318 | Attach to tmux control mode for output   |
| `discoverPaneID`     | tracker.go:445 | Find the tmux pane for a session         |
| `shouldLogRetry`     | tracker.go:488 | Rate-limit retry logging                 |
| `waitOrStop`         | tracker.go:498 | Wait with cancellation support           |
| `extractNudgeState`  | tracker.go:513 | Parse nudge signals from terminal output |

**Regression risk:** `Spawn` is the most critical function in the entire application — it's the primary action users take. A regression here means users can't create sessions at all. `ResolveTarget` determines which binary runs, so a bug could launch the wrong agent or fail to find the binary. `ensureModelSecrets` handles API key injection — a regression silently starts sessions without credentials.

---

### 3. `internal/config` — HIGH (39.3%, 2,112 LoC, 81 uncovered functions)

This package manages all user configuration, including secrets.

#### Secrets management — 0% coverage, security-sensitive

| Function                   | File:Line      | Purpose                                 |
| -------------------------- | -------------- | --------------------------------------- |
| `secretsPath`              | secrets.go:32  | Resolve path to secrets file            |
| `LoadSecretsFile`          | secrets.go:41  | Read and decrypt secrets from disk      |
| `SaveSecretsFile`          | secrets.go:115 | Encrypt and write secrets to disk       |
| `SaveModelSecrets`         | secrets.go:144 | Save API keys for a model               |
| `DeleteModelSecrets`       | secrets.go:162 | Remove API keys for a model             |
| `GetModelSecrets`          | secrets.go:183 | Retrieve API keys for a model           |
| `GetProviderSecrets`       | secrets.go:196 | Get secrets by provider name            |
| `GetEffectiveModelSecrets` | secrets.go:220 | Resolve secrets with fallbacks          |
| `DeleteProviderSecrets`    | secrets.go:243 | Remove all secrets for a provider       |
| `GetAuthSecrets`           | secrets.go:264 | Get OAuth/auth secrets                  |
| `SaveGitHubAuthSecrets`    | secrets.go:273 | Save GitHub OAuth credentials           |
| `EnsureSessionSecret`      | secrets.go:287 | Generate session signing key if missing |
| `GetSessionSecret`         | secrets.go:308 | Retrieve session signing key            |

**Regression risk:** Secrets are encrypted on disk. A change to the encryption/decryption path, key derivation, or file format could make existing secrets unreadable (locking users out of all configured models) or — worse — write secrets in plaintext.

#### Config getters — 65 functions at 0% coverage

These are mostly simple accessor functions with default-value logic (e.g., `GetPort`, `GetBindAddress`, `GetTLSEnabled`, `GetAuthEnabled`). While individually low-risk, they collectively define the runtime behavior of every feature. A handful have non-trivial fallback logic:

| Function                  | Line | Why it matters                                     |
| ------------------------- | ---- | -------------------------------------------------- |
| `UseWorktrees`            | 559  | Determines clone strategy (worktree vs full clone) |
| `GetNetworkAccess`        | 1444 | Controls network binding (security)                |
| `GetTLSEnabled`           | 1541 | Enables/disables HTTPS                             |
| `GetAuthEnabled`          | 1548 | Enables/disables authentication                    |
| `IsValidPublicBaseURL`    | 1644 | Validates URL format for OAuth redirects           |
| `EnsureModelSecrets`      | 1687 | Ensures required API keys exist before spawn       |
| `DetectedToolsFromConfig` | 1676 | Discovers available agent binaries                 |
| `EnsureExists`            | 1238 | Creates default config file if missing             |

#### Run targets — partially covered

| Function                  | Line | Coverage | Purpose                                            |
| ------------------------- | ---- | -------- | -------------------------------------------------- |
| `IsModel`                 | 57   | 0%       | Determines if a target is a model or detected tool |
| `splitRunTargets`         | 210  | 0%       | Parses compound target definitions                 |
| `MergeDetectedRunTargets` | 227  | 0%       | Merges auto-detected with configured targets       |

**Regression risk:** Run target resolution determines which agent binary is used for each session. A regression could cause sessions to spawn with the wrong binary or fail to detect installed agents.

---

### 4. `internal/remote` — HIGH (29.2%, 1,422 LoC, 38 uncovered functions)

This package manages SSH connections to remote hosts and proxies tmux commands over them.

#### Connection lifecycle — 0% coverage

| Function             | File:Line         | Purpose                                         |
| -------------------- | ----------------- | ----------------------------------------------- |
| `Connect`            | connection.go:211 | Establish SSH connection and start control mode |
| `Reconnect`          | connection.go:328 | Re-establish dropped connection                 |
| `waitForControlMode` | connection.go:566 | Wait for tmux control mode handshake            |
| `monitorProcess`     | connection.go:671 | Monitor SSH process health                      |
| `Provision`          | connection.go:962 | Provision remote host (install schmux)          |
| `rediscoverSessions` | connection.go:940 | Find existing sessions after reconnect          |

#### Remote tmux client — 0% coverage

| Function             | File:Line     | Purpose                             |
| -------------------- | ------------- | ----------------------------------- |
| `CreateWindow`       | client.go:260 | Create tmux window on remote        |
| `KillWindow`         | client.go:280 | Kill tmux window on remote          |
| `SendKeys`           | client.go:290 | Send keystrokes to remote pane      |
| `SendEnter`          | client.go:403 | Send Enter keystroke                |
| `ListWindows`        | client.go:409 | List tmux windows                   |
| `GetPaneInfo`        | client.go:434 | Get pane dimensions and state       |
| `ResizeWindow`       | client.go:457 | Resize tmux window                  |
| `CapturePaneLines`   | client.go:470 | Capture terminal output             |
| `CapturePaneVisible` | client.go:479 | Capture visible terminal content    |
| `WaitForReady`       | client.go:524 | Wait for remote tmux to be ready    |
| `FindWindowByName`   | client.go:534 | Locate a window by name             |
| `RunCommand`         | client.go:582 | Execute command on remote           |
| `SubscribeOutput`    | client.go:236 | Subscribe to terminal output stream |
| `UnsubscribeOutput`  | client.go:245 | Unsubscribe from terminal output    |
| `tmuxQuote`          | client.go:571 | Escape strings for tmux commands    |

#### Manager — 0% coverage

| Function                     | File:Line      | Purpose                               |
| ---------------------------- | -------------- | ------------------------------------- |
| `StartConnect`               | manager.go:55  | Begin async connection to remote      |
| `Reconnect`                  | manager.go:391 | Reconnect a specific host             |
| `RunCommand`                 | manager.go:486 | Execute command on remote host        |
| `GetConnectionByFlavorID`    | manager.go:496 | Look up connection by flavor          |
| `IsFlavorConnected`          | manager.go:516 | Check if flavor has active connection |
| `Disconnect`                 | manager.go:528 | Disconnect from remote host           |
| `GetActiveConnections`       | manager.go:569 | List all active connections           |
| `handleStatusChange`         | manager.go:582 | React to connection status changes    |
| `GetHostForSession`          | manager.go:647 | Find which host owns a session        |
| `StartReconnect`             | manager.go:657 | Initiate reconnection                 |
| `MarkStaleHostsDisconnected` | manager.go:796 | Clean up stale connections            |
| `GetHostConnectionStatus`    | manager.go:871 | Get connection state                  |
| `reconcileSessions`          | manager.go:884 | Sync session state after reconnect    |

**Regression risk:** Remote sessions involve a complex state machine (connecting → ready → control mode → session management). Regressions in connection lifecycle can leave zombie SSH processes, fail to recover from network interruptions, or lose session state. `reconcileSessions` is particularly dangerous — it runs after reconnect and must correctly match existing remote sessions to local state.

---

### 5. `internal/daemon` — HIGH (23.1%, 885 LoC, 8 uncovered functions)

| Function                        | File:Line      | Purpose                                    |
| ------------------------------- | -------------- | ------------------------------------------ |
| `Start`                         | daemon.go:133  | Start the daemon process (background mode) |
| `Stop`                          | daemon.go:181  | Stop the running daemon                    |
| `Status`                        | daemon.go:228  | Check daemon status                        |
| `Run`                           | daemon.go:281  | Run the daemon in foreground               |
| `DevRestart`                    | daemon.go:979  | Hot-restart for development                |
| `startNudgeNikChecker`          | daemon.go:990  | Start periodic nudge checking              |
| `checkInactiveSessionsForNudge` | daemon.go:1015 | Check idle sessions for nudges             |
| `askNudgeNikForSession`         | daemon.go:1071 | Generate nudge for a session               |

**Regression risk:** `Run` is the daemon entry point — it wires together all managers (session, workspace, dashboard, remote, preview). A regression here could prevent the daemon from starting. `Start`/`Stop` manage the daemon lifecycle via PID files and signals — regressions could leave orphan processes or prevent clean shutdown.

---

### 6. `internal/workspace` — MEDIUM-HIGH (58.9%, 4,565 LoC, 44 uncovered functions)

While this package has moderate average coverage, the uncovered areas are disproportionately dangerous.

#### Linear sync — 0% coverage, data-loss-sensitive

| Function                    | File:Line           | Purpose                                     |
| --------------------------- | ------------------- | ------------------------------------------- |
| `LinearSyncToDefault`       | linear_sync.go:225  | Merge workspace branch into default branch  |
| `LinearSyncResolveConflict` | linear_sync.go:463  | Interactive conflict resolution during sync |
| `extractConflictHunks`      | linear_sync.go:946  | Parse conflict markers from files           |
| `truncateOutput`            | linear_sync.go:1009 | Truncate long output for display            |
| `getUnmergedFiles`          | linear_sync.go:1018 | List files with merge conflicts             |
| `getRebaseHead`             | linear_sync.go:1035 | Get current rebase HEAD                     |
| `getRebaseMessage`          | linear_sync.go:1046 | Get commit message being rebased            |
| `rebaseInProgress`          | linear_sync.go:1058 | Check if rebase is in progress              |

**Regression risk:** These functions orchestrate multi-step git rebase/merge operations. A regression in `LinearSyncToDefault` could merge the wrong commits, drop changes, or leave a workspace in an unrecoverable state. `LinearSyncResolveConflict` runs during active conflict resolution — a bug here could silently choose the wrong side of a conflict.

#### Workspace management — 0% coverage

| Function               | File:Line      | Purpose                                     |
| ---------------------- | -------------- | ------------------------------------------- |
| `CreateFromWorkspace`  | manager.go:986 | Clone a workspace from an existing one      |
| `Cleanup`              | manager.go:653 | Remove workspace directory and state        |
| `UpdateGitStatus`      | manager.go:729 | Refresh git status for a workspace          |
| `UpdateAllGitStatus`   | manager.go:810 | Refresh all workspaces' git status          |
| `EnsureWorkspaceDir`   | manager.go:838 | Create workspace directory if missing       |
| `EnsureAllGitExcludes` | manager.go:853 | Set up .git/info/exclude for all workspaces |

#### Origin queries — 0% coverage

| Function                    | File:Line             | Purpose                                  |
| --------------------------- | --------------------- | ---------------------------------------- |
| `EnsureOriginQueries`       | origin_queries.go:18  | Set up bare repo for fast origin queries |
| `setOriginHead`             | origin_queries.go:151 | Update origin HEAD reference             |
| `ensureCorrectOriginURL`    | origin_queries.go:169 | Verify origin URL matches config         |
| `FetchOriginQueries`        | origin_queries.go:214 | Fetch from origin for queries            |
| `GetRecentBranches`         | origin_queries.go:248 | List recently-active branches            |
| `getRecentBranchesFromBare` | origin_queries.go:291 | Branch listing from bare repo            |
| `GetBranchCommitLog`        | origin_queries.go:391 | Get commit log for a branch              |

#### Pull requests and overlays — 0% coverage

| Function            | File:Line          | Purpose                            |
| ------------------- | ------------------ | ---------------------------------- |
| `CheckoutPR`        | pull_request.go:16 | Check out a pull request by number |
| `fetchPRRef`        | pull_request.go:39 | Fetch PR ref from remote           |
| `EnsureOverlayDir`  | overlay.go:30      | Set up overlay directory           |
| `RefreshOverlay`    | overlay.go:217     | Refresh overlay from source        |
| `EnsureOverlayDirs` | overlay.go:250     | Ensure all overlay dirs exist      |

---

### 7. `cmd/schmux` — MEDIUM-HIGH (25.7%, 1,511 LoC, 57 uncovered functions)

#### GitHub auth setup wizard — 0% coverage

The entire `auth_github.go` file (25 functions, ~800 LoC) is untested. This is a multi-step interactive wizard for configuring GitHub OAuth, TLS certificates, and authentication. Functions include `stepEnableAuth`, `stepHostname`, `stepTLSSetup`, `generateCerts`, `stepGitHubOAuth`, `stepSummaryAndSave`, `validateSetup`, `saveConfig`.

**Regression risk:** While this is a one-time setup flow, a regression makes it impossible for new users to configure authentication. The `generateCerts` and `saveConfig` functions write to disk — bugs could produce invalid certificates or corrupt config.

#### CLI entry point — 0% coverage

`main` (main.go:31) and `printUsage` (main.go:182) are untested. This is expected for CLI entry points but means argument parsing changes aren't tested.

---

### 8. Completely Untested Packages

| Package                  | LoC | Functions | Purpose                      | Regression Risk                                     |
| ------------------------ | --- | --------- | ---------------------------- | --------------------------------------------------- |
| `internal/vcs`           | 153 | 23        | Version control abstractions | **MEDIUM** — changes could break all git operations |
| `internal/update`        | 297 | 9         | Self-update mechanism        | **LOW** — peripheral feature                        |
| `internal/assets`        | 193 | 8         | Asset embedding for binary   | **LOW** — build-time only                           |
| `internal/benchutil`     | 84  | 2         | Benchmark utilities          | **NEGLIGIBLE** — dev tooling                        |
| `internal/commitmessage` | 11  | 1         | Commit message generation    | **LOW** — small scope                               |
| `cmd/build-dashboard`    | 88  | 5         | Dashboard build script       | **NEGLIGIBLE** — build tooling                      |
| `cmd/gen-types`          | 187 | 10        | TypeScript type generator    | **NEGLIGIBLE** — code-gen tooling                   |
| `cmd/scan-ansi`          | 71  | 2         | ANSI escape scanner          | **NEGLIGIBLE** — debug tooling                      |

---

## Frontend Coverage Breakdown

Overall: **51.0% statements, 50.0% branches, 48.6% functions**

### By directory

| Directory        | Stmt Coverage | Notes                          |
| ---------------- | ------------- | ------------------------------ |
| `components/`    | 80.6%         | Generally well-tested          |
| `hooks/`         | 71.6%         | Moderate coverage              |
| `lib/`           | 44.8%         | **Weak** — contains core logic |
| `routes/`        | 44.3%         | **Weak** — route components    |
| `routes/config/` | 47.4%         | **Weak** — config UI           |

### Critical frontend gaps

#### `lib/api.ts` — 5.5% statement coverage (CRITICAL)

This is the main API client used by every component to communicate with the backend. 67 of 73 functions are untested. This file contains:

- All fetch wrappers for every API endpoint
- Request body construction
- Response parsing
- Error handling
- CSRF token management (tested separately in `csrf.test.ts`)

**Regression risk:** Any change to URL construction, request headers, body serialization, or response handling breaks the entire dashboard. Since every UI component depends on this file, a regression here causes cascading failures across the entire frontend.

#### `lib/terminalStream.ts` — 30.0% statement coverage (HIGH)

The terminal rendering pipeline: WebSocket connection management, binary data parsing, xterm.js integration, resize handling. Lines 596-881 are completely uncovered.

**Regression risk:** Terminal output is the core feature of the dashboard. Regressions could cause blank terminals, garbled output, or connection drops.

#### `routes/config/AccessTab.tsx` — 5.0% statement coverage (MEDIUM)

The auth/access configuration UI (lines 96-542 uncovered). Contains forms for password setup, GitHub OAuth configuration, and network access settings.

#### `routes/config/QuickLaunchTab.tsx` — 16.2% statement coverage (MEDIUM)

Quick launch configuration UI. Lines 67-264 uncovered.

#### `routes/config/SessionsTab.tsx` — 27.3% statement coverage (MEDIUM)

Session configuration UI. Lines 151-274 uncovered.

#### `routes/config/AdvancedTab.tsx` — 20.7% statement coverage (MEDIUM)

Advanced settings UI. Lines 112-533 uncovered.

#### `routes/config/ConfigModals.tsx` — 25.0% statement coverage (MEDIUM)

Modal dialogs for config changes. Lines 43-202 uncovered.

#### `components/GitProgress.tsx` — 61.6% statement coverage (LOW-MEDIUM)

Git operation progress display. Lines 289-328 uncovered — likely edge cases in progress rendering.

#### `hooks/useFocusTrap.ts` — 53.7% statement coverage (LOW)

Focus trap hook for modals. Lines 40-75 uncovered.

#### `lib/utils.ts` — 60.0% statement coverage (LOW)

General utility functions. Lines 26-74 uncovered.

---

## Risk Prioritization Matrix

Combining coverage gap size, codebase size, and criticality to user workflows:

| Priority | Area                                                | Coverage | LoC    | Why                                                |
| -------- | --------------------------------------------------- | -------- | ------ | -------------------------------------------------- |
| **P0**   | Dashboard API handlers (`internal/dashboard`)       | 34%      | 9,785  | Entire HTTP API surface; auth, git mutations, sync |
| **P0**   | Session spawn/dispose (`internal/session`)          | 38%      | 1,465  | Core user workflow — creating sessions             |
| **P0**   | Linear sync (`internal/workspace/linear_sync.go`)   | 0%       | ~500   | Git rebase/merge — data loss risk                  |
| **P1**   | Config secrets (`internal/config/secrets.go`)       | 0%       | ~300   | API key management — security sensitive            |
| **P1**   | Frontend API client (`lib/api.ts`)                  | 5.5%     | ~1,000 | Every UI component depends on this                 |
| **P1**   | Remote connections (`internal/remote`)              | 29%      | 1,422  | SSH lifecycle, state machine complexity            |
| **P1**   | Daemon lifecycle (`internal/daemon`)                | 23%      | 885    | Startup/shutdown — affects everything              |
| **P2**   | Config getters (`internal/config`)                  | 39%      | 2,112  | Runtime behavior defaults                          |
| **P2**   | Terminal stream (`lib/terminalStream.ts`)           | 30%      | ~900   | Core dashboard feature                             |
| **P2**   | CLI commands (`cmd/schmux`)                         | 26%      | 1,511  | User-facing CLI interface                          |
| **P2**   | Remote control mode (`internal/remote/controlmode`) | 50%      | 809    | tmux protocol parser                               |
| **P3**   | Config UI tabs (frontend)                           | 5-27%    | ~1,500 | Settings pages                                     |
| **P3**   | VCS abstractions (`internal/vcs`)                   | 0%       | 153    | Used by other packages                             |
| **P3**   | Update mechanism (`internal/update`)                | 0%       | 297    | Peripheral feature                                 |

---

## Recommendations

### Highest-value tests to write (ordered by risk reduction per effort)

1. **HTTP handler integration tests for `internal/dashboard`** — Use `httptest.NewServer` with a real `Server` instance (or mock managers). Focus on:
   - Spawn/dispose handlers (happy path + error cases)
   - Git mutation handlers (commit, amend, discard, uncommit)
   - Linear sync handlers (sync from main, push to branch)
   - Auth middleware (authenticated vs unauthenticated requests)

2. **`session.Spawn` unit test** — Mock the tmux and workspace managers, verify that Spawn correctly constructs the tmux command, injects environment variables, and updates state.

3. **`config/secrets.go` round-trip test** — Write secrets, read them back, verify encryption/decryption. Test edge cases: missing file, corrupt file, wrong key.

4. **Frontend `api.ts` tests** — Mock `fetch` globally, test that each API function sends the correct method/URL/body and handles success/error responses. A single test file could cover 50+ functions.

5. **`workspace.LinearSyncToDefault` integration test** — Create a temp git repo, make divergent commits, run LinearSyncToDefault, verify the merge result. Test conflict detection.

6. **Remote `Connect`/`Reconnect` tests** — Mock SSH client, verify state machine transitions (connecting → ready → control mode → error → reconnecting).

7. **Daemon `Run` smoke test** — Verify that `Run` wires up all managers and starts the HTTP server. Can use a short-lived context for quick shutdown.

### Structural improvements

- **Add coverage thresholds to CI** — Fail the build if total coverage drops below 34.9% (current) to prevent further regression. Gradually increase as tests are added.
- **Separate handler tests from unit tests** — Dashboard handler tests will be slower (they need httptest servers). Keep them in a separate test tag so developers can run fast unit tests locally and handler tests in CI.
