# Flaky Test Report — 2026-04-04

Run: `./test.sh --all --repeat 5` on branch `fix/flaky-tests`
Stable: **3042 tests** passed all 5 runs.
Flaky: **39 tests** failed at least once.

**Follow-up runs (isolated, no cross-suite contention):**

- `./test.sh --e2e --repeat 5` — **48/48 passed all 5 runs, zero flakes.** All E2E failures were contention artifacts.
- `./test.sh --scenarios --repeat 5` — **508/528 stable, 20 genuinely flaky.** ~13 scenario tests that failed under `--all` were contention artifacts; 20 are real.

---

## Contention vs Genuine Flakiness

Tests that failed under `--all` but passed 5/5 in isolation (contention artifacts, no fix needed):

| Test                                                   | `--all` rate |
| ------------------------------------------------------ | ------------ |
| All 7 E2E tests (TestE2ENudge, TestE2EConfigGet, etc.) | 20-40%       |
| `dismiss-conflict-resolution-tab`                      | 60%          |
| `dispose-session`                                      | 50%          |
| `edit-session-nickname`                                | 40%          |
| `resize-scroll-stability:viewport at bottom`           | 40%          |
| `escbuf-gap-replay`                                    | 40%          |
| `configure-remote-access-settings` (3 subtests)        | 33-50%       |
| `remote-auth-browser-flow` (2 subtests)                | 50%          |
| `remote-host-terminal-focus`                           | 33%          |
| `configure-repofeed`                                   | 33%          |
| `lore-page-repo-tabs`                                  | 33%          |
| `terminal-fidelity:ascii lines`                        | 60%          |
| `terminal-fidelity:carriage return`                    | 33%          |
| `terminal-fidelity:cursor survives flood`              | 33%          |
| `terminal-fidelity:utf8 in output flood`               | 50%          |
| `terminal-fidelity:scrollback after alt screen exit`   | 33-50%       |

## Genuinely Flaky Tests (fail even in isolation)

Results from `./test.sh --scenarios --repeat 5`:

| Test                                                                                 | Isolated rate              | Results     |
| ------------------------------------------------------------------------------------ | -------------------------- | ----------- |
| `terminal-streaming.spec.ts:41` — viewport visible on session page                   | 67%                        | ✗✗✓         |
| `terminal-fidelity.spec.ts:493` — bootstrap matches scrollback after reconnect       | 67%                        | ✗✗✓         |
| `timelapse-recording.spec.ts:190` — sidebar does not show Timelapse link             | 50%                        | ✗✓          |
| `view-code-diff.spec.ts:53` — diff page shows file list and viewer                   | 50%                        | ✗✓          |
| `terminal-fidelity.spec.ts:598` — build log then TUI then exit                       | 40%                        | ✗✗✓✓✓       |
| `terminal-fidelity.spec.ts:185` — erase in line                                      | 40%                        | ✗✗✓✓✓       |
| `terminal-fidelity.spec.ts:210` — scroll region                                      | 33%                        | ✗✓✓         |
| `terminal-fidelity.spec.ts:689` — reconnect mid-stream                               | 33%                        | ✗✓✓         |
| `terminal-fidelity.spec.ts:457` — scrollback after bulk output                       | 20-33%                     | ✗✓✓ / ✗✓✓✓✓ |
| `terminal-fidelity.spec.ts:381` — alternate screen roundtrip                         | 25%                        | ✗✓✓✓        |
| `terminal-fidelity.spec.ts:98` — ansi colors preserve text                           | 25%                        | ✗✓✓✓        |
| `spawn-multiple-agents.spec.ts:52` — spawn multiple agents via the UI                | 25%                        | ✗✓✓✓        |
| `spawn-single-session.spec.ts:40` — spawn a single session via the UI                | 25%                        | ✗✓✓✓        |
| `persist-lore-curator-model.spec.ts:56` — curate-on-dispose persists across reload   | 25%                        | ✗✓✓✓        |
| `terminal-fidelity.spec.ts:70` — utf8 box drawing                                    | 20%                        | ✗✓✓✓✓       |
| `typing-latency.spec.ts:58` — idle typing latency                                    | 20%                        | ✗✓✓✓✓       |
| `remote-host-connection-modal.spec.ts:42` — clicking card opens connection modal     | 20%                        | ✗✓✓✓✓       |
| `resize-scroll-stability.spec.ts:110` — followTail remains true through resize cycle | appeared only in isolation | —           |

---

## Root Cause Clusters

### 1. Terminal fidelity — INVESTIGATED, partially fixed

**Fixes applied (commit 192636f0d):** drain writeBuffer before terminal.reset(), poll xterm.js buffer for sentinel instead of separate WebSocket, add afterAll session disposal. These fixed erase-in-line (40%→5%), reconnect-mid-stream (33%→stable), ansi-colors (25%→stable).

**Diagnostic instrumentation added (commit 3d0ff3ee7):** On assertion failure, writes detailed diagnostic files to `/tmp/terminal-diagnostics/` including convergence log (STUCK vs CONVERGING), stream state, and full tmux/xterm captures at first and last retry.

**Root cause for scrollback-reconnect (67-90% failure): tmux pane size mismatch.**
Diagnostic data shows STUCK pattern (25 rows differ from retry 0 through 150, never converges). Content is byte-for-byte identical but shifted by 1 row. After `page.reload()`, xterm.js fits to 56 rows and sends resize, but the tmux pane remains at 57 rows. `resize-window -y 56` sets the window height but the pane height stays 57 (likely due to tmux status bar: window=57 → status_bar=1 → pane=56, but the initial pane was created at 57 before status bar existed). The bootstrap `CaptureLastLines(5000)` captures 57 lines → xterm.js (56 rows) puts 1 line into scrollback → `baseY: 1` → viewport offset.

**Attempted fixes that didn't work:**
- Visible-pane capture (`CapturePane`) instead of `CaptureLastLines`: tmux pane is still 57 rows, so visible capture returns 57 lines too
- Client-side `terminal.clear()` after bootstrap write: async timing — clear runs but baseY doesn't update in time
- Client-side CSI 3J after bootstrap: clears the overflow line but also removes the first visible content line
- Trimming bootstrap to N rows: breaks ANSI escape sequences that span across lines

**Next steps to fix:** The underlying issue is that `resize-window` doesn't resize the pane to the requested height. Need to investigate whether tmux's `resize-pane` command or disabling the status bar (`set -g status off`) in session creation resolves the 1-row offset.

### 2. UI interaction / page readiness races (genuinely flaky)

**Tests:** `terminal-streaming:viewport visible` (67%), `timelapse-recording:disabled hides UI` (50%), `view-code-diff` (50%), `spawn-single-session` (25%), `spawn-multiple-agents` (25%), `persist-lore-curator-model` (25%), `resize-scroll-stability:followTail` (isolated only), `remote-host-connection-modal` (20%).

**Symptom:** UI state hasn't updated when the assertion fires — elements not yet visible/hidden, page not fully loaded.

**Root cause (hypothesis — needs code investigation):** Tests interact with elements or assert on state before async operations (WebSocket updates, React re-renders, page navigations) have completed.

**Proposed fix:**

- Wait for specific DOM changes driven by WebSocket updates rather than fixed delays.
- Use Playwright's built-in auto-retrying `expect()` for visibility/content checks.

### 3. Typing latency benchmark (genuinely flaky, 20%)

**Test:** `typing-latency.spec.ts:58` — idle typing latency.

**Likely cause:** Performance benchmark with tight threshold; Docker resource variability causes occasional threshold breach.

### 4. Contention-only failures — NO FIX NEEDED

The following tests only fail when running under `--all` (alongside backend + E2E):

- All 7 E2E tests — stable 48/48 in isolation
- ~13 scenario tests (dismiss-conflict-resolution-tab, dispose-session, edit-session-nickname, escbuf-gap-replay, configure-remote-access-settings, remote-auth-browser-flow, remote-host-terminal-focus, configure-repofeed, lore-page-repo-tabs, resize-scroll-stability:viewport, terminal-fidelity:ascii lines, terminal-fidelity:carriage return, terminal-fidelity:cursor survives flood, terminal-fidelity:utf8 in output flood, terminal-fidelity:scrollback after alt screen exit)

These pass reliably when their suite runs in isolation. The failures are caused by CPU/memory contention from running all suites simultaneously.

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

| Priority | Cluster                                       | Tests  | Isolated rate           | Effort                                             |
| -------- | --------------------------------------------- | ------ | ----------------------- | -------------------------------------------------- |
| 1        | Terminal fidelity timing                      | 10     | 20-67%                  | Medium — build shared polling helper, update tests |
| 2        | UI interaction / page readiness               | 8      | 20-67%                  | Medium — auto-retry assertions, wait for DOM state |
| 3        | Typing latency threshold                      | 1      | 20%                     | Low — widen threshold or add retry tolerance       |
| ~~4~~    | ~~Contention-only~~                           | ~~20~~ | ~~stable in isolation~~ | ~~No fix needed~~                                  |
| ~~5~~    | ~~TestHandleLoreApplyMergeAutoCommit~~        | ~~1~~  | ~~FIXED~~               |                                                    |
| ~~6~~    | ~~TestNormalizeBarePaths_NormalizesQueryDir~~ | ~~1~~  | ~~FIXED~~               |                                                    |
