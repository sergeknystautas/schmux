# Testing Knowledge Base

Living document updated by `/improve-testing`. Read this before starting a round — it saves re-investigating known issues.

Last updated: 2026-04-19 (round 7)

## Current Baseline

| Metric                  | Value                            | Date       |
| ----------------------- | -------------------------------- | ---------- |
| Backend coverage        | 46.7%                            | 2026-04-19 |
| Frontend coverage       | 49.6%                            | 2026-04-19 |
| Total tests             | 3,089 (1x), 3,289 (3x repeat)    | 2026-04-19 |
| Backend suite time      | ~38s (1x), ~1m 51s (3x repeat)   | 2026-04-19 |
| Frontend suite time     | ~7s                              | 2026-04-19 |
| E2E suite time          | ~16s (1x), ~22s (3x repeat)      | 2026-04-19 |
| Scenario suite time     | ~64s (1x), ~1m 35s (3x repeat)   | 2026-04-19 |
| Full suite time         | ~2m 5s (1x), ~3m 56s (3x repeat) | 2026-04-19 |
| Flaky tests (3x repeat) | **0 across all suites**          | 2026-04-19 |

## Known Slow Tests (Not Fixable)

These tests are inherently slow due to real git operations (clone, checkout, worktree creation). They cannot be meaningfully sped up without mocking git, which would reduce their regression-detection value.

| Test                                                       | Duration | Why                                     |
| ---------------------------------------------------------- | -------- | --------------------------------------- |
| TestGetOrCreate_BranchReuse_PurgesConflictingRecyclable    | ~12s     | 5x GetOrCreate with real git repos      |
| TestPushToBranch_RebasedWithExtraOriginCommits_Confirmed   | ~7s      | Real git push/rebase operations         |
| TestGetOrCreate_BranchReuse_PromotesRecyclableStatus       | ~7s      | Multiple workspace lifecycle operations |
| TestGetOrCreate_RecyclableBranchCollision_PurgesAndRetries | ~7s      | Workspace creation + disposal + retry   |
| TestGitGraph_MaxCommits                                    | ~7s      | Generates many real git commits         |

All top-20 slowest backend tests are in `internal/workspace/` and involve real git operations.

After round 6's daemon-stop fix, E2E tests cluster at 1–3s. The slowest one is now `TestE2EGitAmendAndUncommit` (~10s, full git amend + uncommit workflow). Daemon-restart tests dropped from ~20s to <2s because Stop() no longer waits for orphan/zombie process death — it polls the daemon's PID file (removed by the daemon's `defer` on clean shutdown) instead.

## Known Issues (Fixed)

### lsof contention freezes machine during --repeat runs

**Problem**: `preview.LookupPortOwner`, `preview.BuildPortOwnerCache`, and `detectPortsViaLsof` call `lsof` directly. With `--repeat 3`, dozens of parallel test packages invoke `lsof` simultaneously, which on macOS scans the entire process table per call. This froze the machine.

**Fix** (2025-04-09): Made all three functions pluggable via package-level function variables (`LookupPortOwnerFunc`, `BuildPortOwnerCacheFunc`, `detectPortsForPIDFunc`). Dashboard tests swap in lightweight TCP-connect alternatives via `TestMain` in `internal/dashboard/testmain_test.go`. Production code still uses `lsof`.

**Never revert**: `internal/dashboard/testmain_test.go` — removing it re-enables lsof in tests.

### schmuxdir global state poisons HOME-based tests (round 5)

**Problem**: `internal/schmuxdir/schmuxdir.go` uses a package-level `dir` variable. `Get()` falls back to `~/.schmux` only when `dir == ""`. `internal/dashboard/handlers_timelapse_test.go` did `oldDir := schmuxdir.Get()` (which returned a _resolved_ path like `/Users/me/.schmux`) before `Set(tmpHome)`, then restored to `oldDir` — leaving `dir` permanently non-empty for the rest of the test process. Subsequent tests that relied on `t.Setenv("HOME", x)` (e.g. `TestAPIContract_DisposeBlockedByDevMode`) couldn't redirect schmuxdir to their tmp HOME, so `devSourceWorkspacePath()` read from a stale path. Order-dependent: 67% flaky in 3x repeat.

**Fix** (2026-04-19): `handlers_timelapse_test.go` now restores via `schmuxdir.Set("")`, returning the package to fallback mode. `TestAPIContract_DisposeBlockedByDevMode` now explicitly pins `schmuxdir.Set(schmuxDir)` in setup with `Set("")` cleanup, removing the dependency on test order.

**Pattern to avoid**: never store the result of `schmuxdir.Get()` and pass it back to `Set()` as a "restore" — always restore to `""` so HOME-based fallback resumes.

### daemon.Stop() spent ~12s/test polling zombie PIDs (round 6)

**Problem**: `daemon.Stop()` polled `process.Signal(syscall.Signal(0))` to detect daemon death. On Linux/Docker, when a non-child process exits, it can sit in zombie state until the original parent reaps it — and signal 0 still returns success against a zombie. Result: `Stop()` always hit its 10s deadline (returning a "timeout waiting for daemon to stop" error), even when shutdown completed in ~1s. The CLI returned an error, the e2e test logged a warning and continued, healthz polling caught the actual death — total cost was ~10s per stop. Under heavy parallel Docker load, the deadline was sometimes too short and the test failed (`TestE2EMultipleSessionsIsolatedSignals`).

**Fix** (2026-04-19): `Stop()` now polls the daemon's PID file (removed by the daemon's `defer os.Remove(pidFile)` on clean shutdown). This is the only signal that fires reliably on graceful exit and isn't fooled by zombie state. Signal-0 polling is kept as a fallback for crashes. SIGKILL escalates after 15s with a 2s wait. Result: e2e suite went from 52s → 16s (3.2x faster), individual daemon-stop calls from ~10s → <1s.

**Pattern to avoid**: don't rely on signal 0 to detect death of non-child processes — zombie state defeats it. Use a side-effect of clean shutdown (PID file removal, healthz endpoint refusal) instead.

### inotify exhaustion under fast parallel daemon turnover (round 7)

**Problem**: Round 6's faster daemon shutdown (signal-0 → PID-file polling) accidentally exposed a latent flake. Each daemon opens 4+ fsnotify watchers (config, git, compound, plus one per session). On macOS Docker (8 CPUs), `e2e-entrypoint.sh` set `-test.parallel = nproc * 2 = 16`. With `--repeat 3` running 3 containers in parallel against the host's shared `fs.inotify.max_user_instances = 128`, the upper bound was 16 × 3 × ~5 = 240 instances → "too many open files" failures during daemon startup. 13 of 52 e2e tests went flaky (33–67%). Fast shutdowns let more daemons exist concurrently, which is why round 5's slower-stop baseline didn't trip this.

**Fix** (2026-04-19): Capped `-test.parallel` at 6 in `scripts/e2e-entrypoint.sh` (3 × 6 × ~5 = 90, comfortable headroom under 128). Docker won't allow `--sysctl fs.inotify.*` on the container (not in the namespaced sysctl list), so reducing demand is the only portable knob. Result: 0 flaky e2e tests across 3x repeat, suite stays fast (~22s for 3x = 156 invocations).

**Pattern to avoid**: parallelism caps that scale with `nproc` need an absolute upper bound when the workload uses kernel-shared resources (inotify instances, ephemeral ports, Unix sockets, etc.) — those don't scale with CPU.

## Coverage Gaps (Investigated)

### Not worth testing (skip these)

- `cmd/build-dashboard`, `cmd/build-website`, `cmd/gen-types` — build tools, 0% coverage, thin wrappers around npm/go commands
- `internal/assets` — asset download code, only used at install time
- `internal/commitmessage` — 12 LoC, single function
- Simple getters/setters in config.go (dozens at 0%) — no branching logic
- Code behind `e2e` build tag — already tested via E2E suite
- Session/workspace manager setter methods (SetRemoteManager, SetHooksDir, etc.) — trivial one-liners

### Worth testing (highest value gaps)

- `internal/dashboard` handlers (42.5% coverage, 15,687 LoC) — many HTTP handlers at 0%, but most require full server setup. Pure functions like `isValidSocketName` and `reposEqual` are now covered.
- `internal/session/manager.go` (50.1%, 107 git commits) — frequently changed, `Spawn` at 0%. Needs daemon/tmux to test properly.
- `internal/workspace/manager.go` (66.9%, 109 git commits) — frequently changed, but integration tests already cover the critical paths well.

### Scenario test sleeps are well-structured (don't optimize)

42 Playwright spec files, 191 tests, ~60s per run. 34/42 files use `test.describe.serial` (shared daemon state). Sleep usage investigated:

- **Polling loops** (`sleep(200)` in `for` loops): condition-based polling for API readiness (git diff, remote host connection). Already the correct pattern.
- **Negative assertions** (`waitForTimeout(2000)`): verifying things did NOT happen (e.g., dismissed tab stays gone). Cannot reduce.
- **Timing measurement** (`sleep(10)`, `sleep(50)`): keystroke latency tests measuring real input timing.
- **SSH connection waits** (`sleep(1000)` in loops): remote host tests polling for SSH readiness with 30-attempt limit. 1s interval is appropriate for SSH.

Most sleep-heavy files: `git-operations.spec.ts` (8), `typing-latency.spec.ts` (7), `timelapse-recording.spec.ts` (7) — all are polling loops, not fixed waits.

### E2E sleeps are intentional (don't optimize)

E2E tests contain `time.Sleep` calls that look like optimization targets but are NOT:

- **Negative assertion sleeps** (2s): "wait and verify nothing propagated" — reducing these risks false passes
- **Suppression window waits** (500ms-1.2s): testing that overlay suppression expires correctly — these test actual timing behavior
- **Polling loops** (200ms intervals): already using condition-based waiting with `WaitFor*` helpers — the sleep is between poll attempts, not a fixed wait

## Test Infrastructure Notes

- `./test.sh` delegates to `tools/test-runner/` (TypeScript)
- `--repeat N` runs each test N times via `go test -count=N` (backend) and `--repeat-each=N` (Playwright)
- `--coverage` adds instrumentation overhead — do NOT combine with `--repeat` (inflates timings, causes false flakiness)
- Flaky detection is built into the test runner — `flakyScore > 0` means mixed pass/fail across runs
- E2E and scenario tests run in Docker containers
- Frontend tests use Vitest + React Testing Library

## Improvement History

### Round 7 (2026-04-19)

**Coverage additions:**

- `corsMiddleware` — 5 cases (rejects disallowed origin → 403, allows known origin + sets ACAO, sets credentials when auth enabled, OPTIONS preflight short-circuits to 200 with Methods/Headers, missing Origin header passes through). Coverage: 6.2% → 100%.
- `normalizeOrigin` — 6 cases (https/http/path-stripping/empty/missing-scheme/missing-host). Coverage: 75% → 100%.

**Flakiness fixes:**

- 13 e2e tests were 33–67% flaky under 3x repeat (uncovered after round 6). Root caused to inotify exhaustion — see Known Issues above. Fix: cap test parallelism at 6 in `scripts/e2e-entrypoint.sh`. Verified: 0 flaky across 3x repeat full suite.

**Process learnings:**

- Round 6's e2e speedup (96s → 16s) accidentally exposed an inotify ceiling that the slower stop had been hiding — daemons died fast enough to overlap with their successors' startup. Faster tests amplify latent races.
- Docker won't allow `--sysctl fs.inotify.max_user_instances=N` (not namespaced), and `--ulimit nofile` doesn't help inotify limits. The only portable lever is reducing demand (parallelism cap).

### Round 6 (2026-04-19)

**Coverage additions:**

- `csrfMiddleware` — 4 cases (GET bypass, trusted-local POST bypass, untrusted POST without token → 403, untrusted POST with valid token bypass). Coverage: 16.7% → 100%.
- `authMiddleware` — 5 cases (no-auth bypass, tunnel local-bypass, tunnel non-local + no cookie → 401, valid GitHub cookie → bypass, expired cookie → 401). Coverage: 9.1% → 100%.
- `waitForDaemonExit` — 3 cases (PID file removed, process death, timeout with live process). Coverage: 100%.

**Performance + flakiness fixes (combined):**

- Fixed `daemon.Stop()` — see Known Issues above. **5.9x speedup on E2E suite** (1m 36s → 16.3s) AND eliminated the `TestE2EMultipleSessionsIsolatedSignals` flake. Daemon-restart tests went from 21s → 1.7s; baseline e2e tests from 11s → 1-2s.
- Verified flake fix with 3x repeat of `TestE2EMultipleSessionsIsolatedSignals` — stable.

**Process learnings:**

- First attempt bumped SIGTERM polling to 20s + 2s SIGKILL fallback. Made every test +12s slower because zombie process state defeats `signal(0)` polling — the 20s deadline always fired.
- Second attempt switched to PID-file polling (the daemon's clean-shutdown defer removes it). Worked perfectly because it observes the actual completion event, not a heuristic.

### Round 5 (2026-04-19)

**Coverage additions:**

- `parseSessionCookie` — 7 test cases for HMAC session cookie parsing (Go). Covers valid cookies, missing separator, empty value, bad base64 in payload/signature, wrong-key signature mismatch, malformed JSON payload, and expired sessions. Coverage: 0% → 95.2%.
- `authCookieSecure` — 4 table cases for HTTPS detection from `PublicBaseURL` (https/http/empty/malformed). Coverage: 0% → 100%.
- `EntryKey` — 7 cases for autolearn entry key formatting (failure with/without tool, text-based types, empty type fallback). Coverage: 40% → 100%.
- `expandHome` — 7 cases for `~/` expansion (absolute, relative, empty, inner tilde, no-HOME fallback). Coverage: 40% → 100%.

**Flakiness fixes:**

- `TestAPIContract_DisposeBlockedByDevMode` — root caused to `schmuxdir` global state leaking from `TestHandleTimelapseList_*` tests. Fixed both call sites (see Known Issues above). Verified stable across 5x repeat.

**Performance findings:**

- All top-20 slowest tests still in `internal/workspace/` (real git operations) — not optimizable without weakening regression detection.
- E2E daemon-restart tests still cluster at ~20s; remaining E2E tests at ~10–13s baseline (Docker + daemon + workspace).
- Backend suite improved from ~50s → ~38s since round 4 (~24% faster), likely from accumulated optimizations and fewer redundant init paths.

**Outstanding flakes (not addressed this round):**

- `TestE2EMultipleSessionsIsolatedSignals` — fails ~33% under 3x parallel docker load with "timeout waiting for daemon to stop". Daemon's internal Stop() has 10s polling deadline; under heavy CPU contention, shutdown sequence (sm.Stop + remoteManager.DisconnectAll + tunnelMgr.Stop + server.Stop) can exceed it. Needs a follow-up: bump deadline to 20s + add SIGKILL fallback after timeout.
- `escbuf-gap-replay.spec.ts:163:3` — 33% flaky under 3x repeat. Stress test floods 5000 colored lines and asserts terminal matches tmux + `gapReplayWritten == 0`. Inherently sensitive to Docker scheduling jitter; needs investigation into whether assertion is over-strict or replay logic actually races.

### Round 1 (2025-04-09)

**Coverage additions:**

- `CurationTracker` lifecycle tests (Start, AddEvent, Complete, Active, Recent) — `internal/dashboard/curation_state_test.go`
- `isValidSocketName` input validation with security edge cases — `internal/dashboard/handlers_config_test.go`
- `reposEqual` config comparison — `internal/dashboard/handlers_config_test.go`

**Performance findings:**

- No actionable slow tests — all top-20 are workspace integration tests doing real git work
- Dashboard tests are all <1s individually; suite time is from quantity (200+ tests)

**Flakiness findings:**

- Zero flaky tests in 3x repeat run (1594 backend tests, all consistent)

### Round 2 (2025-04-09)

**Full suite validation (all 4 suites, 3x repeat):**

- 2,905 tests across frontend (844), backend (1600), E2E (49), scenarios (412)
- Zero flaky tests — all consistent across 3 runs
- Total time: 4m 34s

**Coverage additions:**

- `CopyResolveConflicts`, `copyConflictDiffs`, `copyStringSlice` deep copy tests with mutation isolation — `internal/state/copy_test.go`
- `HasTextOutput`, `IsAllDigits` model registry helper tests — `internal/models/registry_helpers_test.go`
- `cloneNetwork`, `cloneAccessControl` deep copy tests with TLS pointer independence — `internal/dashboard/handlers_config_test.go`

**Performance findings:**

- Top 4 slowest E2E tests (~20s) are daemon restart tests — inherently 2x daemon lifecycle
- Remaining E2E tests cluster at 10-13s — baseline Docker + daemon + workspace cost
- E2E sleeps investigated: all are negative assertions or timing behavior tests, not optimization targets

**Flakiness findings:**

- Zero flaky tests across all 4 suites in 3x repeat run (2,905 tests total)

### Round 4 (2025-04-09)

**Coverage additions:**

- `ClassifyKeyRuns` — 36 test cases for keyboard input parser (Go). Covers ASCII, Enter/Tab/Backspace, all arrow keys, control chars, Meta combos, F1-F4, PageUp/Down, Home/End, BTab, Delete, Insert, unknown CSI skip, bare escape, UTF-8, pre-allocated dst reuse. Coverage: 53% → ~95%.
- `validateCompoundConfig` / `validateNudgenikConfig` — 9 test cases for config validators (Go). Both are currently stubs (always nil) — tests document this and will catch regressions.
- `getQuickLaunchItems` — 7 test cases for quicklaunch deduplication/scoping (TS). Covers global/workspace scope ordering, deduplication across scopes, whitespace trimming/filtering.
- `constants.ts` — investigated, skipped (only exports string constants, no functions)

### Round 3 (2025-04-09)

**Frontend coverage additions (4 new test files, 33 test cases):**

- `passwordStrength.test.ts` — 9 tests: weak/ok/strong classification, edge cases (empty, repeated chars, sequential digits, mixed alphanumeric)
- `tmuxHealth.test.ts` — 5 tests: histogram computation (bucket counts, edge cases, null for insufficient data)
- `screenDiff.test.ts` — 9 tests: terminal desync detection (identical screens, ANSI stripping, different row counts, diff text format)
- `notificationSound.test.ts` — 10 tests: nudge state → sound mapping (attention/completion/null)

**Frontend coverage targets investigated but skipped:**

- `accessoryTabOrder.ts` — uses localStorage, needs jsdom mocking for minimal value
- `api.ts` (9.8% coverage) — HTTP client wrappers, tested via integration/scenario tests
- `terminalStream.ts` (45%) — WebSocket streaming, requires complex mock setup
- React components with low coverage — require rendering infrastructure, better tested via scenarios
- Fixed lsof contention that made repeat runs unusable
