# Flaky Test Report — 2026-04-04

Run: `./test.sh --all --repeat 5` on branch `fix/flaky-tests`
Stable: **3042 tests** passed all 5 runs.
Flaky: **39 tests** failed at least once.

**Follow-up:** `./test.sh --e2e --repeat 5` (E2E only, no contention) — **48/48 passed all 5 runs, zero flakes.** The E2E failures in the full run were caused by resource contention from running all suites simultaneously, not actual test instability. E2E tests can be removed from the flaky list.

---

## Results by Failure Rate

### 80%+ failure — effectively broken

| Test                                 | Suite   | Score | Results | Error                                                                                                 |
| ------------------------------------ | ------- | ----- | ------- | ----------------------------------------------------------------------------------------------------- |
| `TestHandleLoreApplyMergeAutoCommit` | backend | ~80%  | ✗✗✗✗✓   | `git status failed: exit status 128` / `chdir .../workspaces/testrepo-001: no such file or directory` |

### 60% failure

| Test                                                                                              | Suite     | Score | Results |
| ------------------------------------------------------------------------------------------------- | --------- | ----- | ------- |
| `dismiss-conflict-resolution-tab.spec.ts:109` — conflict tab disappears on dismiss and stays gone | scenarios | 60%   | ✗✗✗✓✓   |
| `terminal-fidelity.spec.ts:56` — ascii lines encoding                                             | scenarios | 60%   | ✗✗✗✓✓   |

### 50% failure

| Test                                                                                             | Suite     | Score | Results |
| ------------------------------------------------------------------------------------------------ | --------- | ----- | ------- |
| `remote-host-connection-modal.spec.ts:42` — clicking card opens connection modal                 | scenarios | 50%   | ✗✓      |
| `configure-remote-access-settings.spec.ts:208` — Advanced tab no longer contains Network or Auth | scenarios | 50%   | ✗✓      |
| `remote-auth-browser-flow.spec.ts:65` — correct password grants dashboard access                 | scenarios | 50%   | ✗✓      |
| `spawn-single-session.spec.ts:40` — spawn a single session via the UI (retry #1)                 | scenarios | 50%   | ✗✓      |
| `terminal-fidelity.spec.ts:56` — ascii lines (retry #1)                                          | scenarios | 50%   | ✗✓      |
| `configure-remote-access-settings.spec.ts:122` — setting PIN via dashboard succeeds (retry #1)   | scenarios | 50%   | ✗✓      |
| `escbuf-gap-replay.spec.ts:70` — ANSI-heavy output produces no phantom gaps (retry #1)           | scenarios | 50%   | ✗✓      |
| `remote-auth-browser-flow.spec.ts:28` — token URL redirects to nonce (retry #1)                  | scenarios | 50%   | ✗✓      |
| `terminal-fidelity.spec.ts:632` — utf8 in output flood (retry #1)                                | scenarios | 50%   | ✗✓      |
| `dispose-session.spec.ts:41` — dispose session via the UI                                        | scenarios | 50%   | ✗✗✓✓    |
| `terminal-fidelity.spec.ts:475` — scrollback preserved after alt screen exit (retry #1)          | scenarios | 50%   | ✗✓      |

### 40% failure

| Test                                                                                   | Suite     | Score | Results |
| -------------------------------------------------------------------------------------- | --------- | ----- | ------- |
| `TestE2ENudgeClearOnTerminalInput`                                                     | e2e       | 40%   | ✗✗✓✓✓   |
| `TestE2EConfigGet`                                                                     | e2e       | 40%   | ✗✗✓✓✓   |
| `TestE2ESaplingDiffAndDiscard`                                                         | e2e       | 40%   | ✗✗✓✓✓   |
| `escbuf-gap-replay.spec.ts:70` — ANSI-heavy output produces no phantom gaps            | scenarios | 40%   | ✗✗✓✓✓   |
| `edit-session-nickname.spec.ts:44` — edit session nickname via the UI                  | scenarios | 40%   | ✗✗✓✓✓   |
| `resize-scroll-stability.spec.ts:49` — viewport stays at bottom after container resize | scenarios | 40%   | ✗✗✓✓✓   |
| `spawn-single-session.spec.ts:40` — spawn a single session via the UI                  | scenarios | 40%   | ✗✗✓✓✓   |

### 33% failure

| Test                                                                                            | Suite     | Score | Results |
| ----------------------------------------------------------------------------------------------- | --------- | ----- | ------- |
| `configure-remote-access-settings.spec.ts:163` — saving remote access settings persists via API | scenarios | 33%   | ✗✓✓     |
| `remote-host-terminal-focus.spec.ts:42` — terminal receives focus on modal open                 | scenarios | 33%   | ✗✓✓     |
| `terminal-fidelity.spec.ts:475` — scrollback preserved after alt screen exit                    | scenarios | 33%   | ✗✓✓     |
| `terminal-fidelity.spec.ts:493` — bootstrap matches scrollback after reconnect                  | scenarios | 33%   | ✗✓✓     |
| `terminal-fidelity.spec.ts:737` — cursor position survives background output flood              | scenarios | 33%   | ✗✓✓     |
| `configure-repofeed.spec.ts:68` — Repofeed tab is accessible on config page                     | scenarios | 33%   | ✗✓✓     |
| `lore-page-repo-tabs.spec.ts:36` — navigates to /lore via sidebar                               | scenarios | 33%   | ✗✓✓     |
| `terminal-fidelity.spec.ts:457` — scrollback after bulk output (retry #1)                       | scenarios | 33%   | ✗✓✓     |
| `terminal-fidelity.spec.ts:157` — carriage return overwrites                                    | scenarios | 33%   | ✗✓✓     |

### 20-25% failure

| Test                                                                               | Suite     | Score | Results |
| ---------------------------------------------------------------------------------- | --------- | ----- | ------- |
| `terminal-fidelity.spec.ts:381` — alternate screen roundtrip                       | scenarios | 25%   | ✗✓✓✓    |
| `terminal-fidelity.spec.ts:457` — scrollback after bulk output                     | scenarios | 25%   | ✗✓✓✓    |
| `typing-latency.spec.ts:58` — idle typing latency                                  | scenarios | 25%   | ✗✓✓✓    |
| `persist-lore-curator-model.spec.ts:56` — curate-on-dispose persists across reload | scenarios | 25%   | ✗✓✓✓    |
| `terminal-fidelity.spec.ts:210` — scroll region                                    | scenarios | 25%   | ✗✓✓✓    |
| `TestNormalizeBarePaths_NormalizesQueryDir`                                        | backend   | 20%   | ✗✓✓✓✓   |
| `TestE2ECaptureSession`                                                            | e2e       | 20%   | ✗✓✓✓✓   |
| `TestE2EGitAmendAndUncommit`                                                       | e2e       | 20%   | ✗✓✓✓✓   |
| `TestE2EInspectWorkspace`                                                          | e2e       | 20%   | ✗✓✓✓✓   |
| `TestE2EOverlayFileDeletion`                                                       | e2e       | 20%   | ✗✓✓✓✓   |
| `terminal-fidelity.spec.ts:598` — build log then TUI then exit                     | scenarios | 20%   | ✗✓✓✓✓   |

---

## Root Cause Clusters

### 1. Terminal fidelity timing races (~15 tests, scenarios)

**Tests:** Most `terminal-fidelity.spec.ts` subtests (encoding, cursor movement, scrollback, alternate screen, compounding).

**Symptom:** Terminal content assertions fire before tmux output has fully rendered in xterm.js.

**Root cause:** Tests send terminal commands and immediately assert on content. The pipeline is: tmux capture -> WebSocket -> xterm.js render. Any delay in this chain causes a snapshot mismatch.

**Proposed fix:**

- Replace fixed `waitForTimeout()` with polling assertions that retry until content matches (Playwright's `expect(locator).toContainText()` with auto-retry, or a custom `waitForTerminalContent()` helper).
- Add a "content settled" helper that waits for terminal content to stop changing for N ms before snapshotting.
- For scrollback tests, wait for the xterm.js buffer to reach the expected line count.

### 2. UI interaction races (~8 tests, scenarios)

**Tests:** spawn-single-session, dispose-session, edit-session-nickname, dismiss-conflict-resolution-tab, resize-scroll-stability, lore-page-repo-tabs.

**Symptom:** UI state hasn't updated when the assertion fires — elements not yet visible/hidden, WebSocket broadcasts not yet processed.

**Root cause:** Tests click buttons and immediately check for state changes that depend on async WebSocket updates arriving and React re-rendering.

**Proposed fix:**

- Wait for specific DOM changes driven by WebSocket updates rather than fixed delays.
- Use Playwright's built-in auto-retrying `expect()` for visibility/content checks.
- For dispose/dismiss tests, use `waitForSelector` with `state: 'detached'` or `'hidden'`.

### 3. Remote/auth flow timing (~5 tests, scenarios)

**Tests:** remote-host-connection-modal, remote-auth-browser-flow, configure-remote-access-settings, remote-host-terminal-focus.

**Symptom:** Modals, forms, or auth redirects not ready when interaction begins.

**Root cause:** Modal transition animations, form hydration, or mock SSH setup hasn't completed.

**Proposed fix:**

- Wait for modal to be fully visible (`state: 'visible'`) before clicking form elements.
- Wait for form fields to be enabled/editable before filling them.
- Add readiness checks for mock SSH connections.

### 4. E2E Docker environment races — NOT ACTUALLY FLAKY

**Tests:** TestE2ENudgeClearOnTerminalInput, TestE2EConfigGet, TestE2ESaplingDiffAndDiscard, TestE2ECaptureSession, TestE2EGitAmendAndUncommit, TestE2EInspectWorkspace, TestE2EOverlayFileDeletion.

**Result:** `./test.sh --e2e --repeat 5` (isolated, no contention) passed 48/48 all 5 runs. The failures in `--all` were caused by resource contention from running all suites simultaneously. **No fix needed.**

### ~~5. `TestHandleLoreApplyMergeAutoCommit` — nearly broken (backend)~~ FIXED

**Symptom:** `git status failed: exit status 128` and `chdir ... no such file or directory`. Failed ~80% of runs.

**Root cause:** Two issues:

1. The handler at `handlers_lore.go:583` fires a goroutine to `DisposeForce` the workspace after push. The test assertions verified `git status`/`git log` on `ws.Path`, which was being deleted by the goroutine.
2. Git background `gc --auto` processes kept files open in `t.TempDir()`, causing cleanup failures (which mark the test FAIL in Go 1.21+).

**Fix applied:**

- Added `backgroundWG sync.WaitGroup` to `Server` struct, tracked in the dispose goroutine, waited in `CloseForTest()`.
- Changed test assertions to verify the commit message against the remote repo (which persists) instead of the disposed workspace.
- Disabled git auto-gc (`gc.auto=0`, `gc.autoDetach=false`) in test git repos.

### ~~6. `TestNormalizeBarePaths_NormalizesQueryDir` — unit test flake (backend)~~ FIXED

**Symptom:** `TempDir RemoveAll cleanup: unlinkat ... directory not empty`. Failed ~20% of runs.

**Root cause:** `createBareRepoWithWorktrees` runs `git clone --bare` which can trigger background `git gc --auto`. The background gc process writes pack files, causing `t.TempDir()` cleanup to fail with `directory not empty` (which marks the test FAIL in Go 1.21+).

**Fix applied:** Disabled git auto-gc (`gc.auto=0`, `gc.autoDetach=false`) in all repos created by `createBareRepoWithWorktrees` (remote, working copy, and bare clone).

---

## Prioritized Fix Plan (remaining)

| Priority | Cluster                                         | Impact                              | Effort                                     |
| -------- | ----------------------------------------------- | ----------------------------------- | ------------------------------------------ |
| ~~1~~    | ~~`TestHandleLoreApplyMergeAutoCommit`~~        | ~~FIXED~~                           |                                            |
| 2        | Terminal fidelity polling pattern               | Stabilizes ~15 tests at once        | Medium — build shared helper, update tests |
| 3        | UI interaction waits                            | Stabilizes ~8 tests                 | Medium — switch to auto-retry assertions   |
| ~~4~~    | ~~`TestNormalizeBarePaths_NormalizesQueryDir`~~ | ~~FIXED~~                           |                                            |
| ~~5~~    | ~~E2E Docker timing~~                           | ~~Not flaky (contention artifact)~~ |                                            |
| 6        | Remote/auth flow timing                         | Stabilizes ~5 tests                 | Low-Medium — better wait conditions        |
