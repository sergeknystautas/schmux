# Testing Knowledge Base

Living document updated by `/improve-testing`. Read this before starting a round — it saves re-investigating known issues.

Last updated: 2025-04-09 (round 4)

## Current Baseline

| Metric | Value | Date |
|--------|-------|------|
| Backend coverage | 45.0% | 2025-04-09 |
| Frontend coverage | 48.8% | 2025-04-09 |
| Total tests | 2,905 (with 3x repeat) | 2025-04-09 |
| Backend suite time | ~50s (1x), ~86s (3x repeat) | 2025-04-09 |
| Frontend suite time | ~6s | 2025-04-09 |
| E2E suite time | ~50s | 2025-04-09 |
| Scenario suite time | ~60s | 2025-04-09 |
| Full suite time | ~3m | 2025-04-09 |
| Flaky tests (3x repeat) | 0 | 2025-04-09 |

## Known Slow Tests (Not Fixable)

These tests are inherently slow due to real git operations (clone, checkout, worktree creation). They cannot be meaningfully sped up without mocking git, which would reduce their regression-detection value.

| Test | Duration | Why |
|------|----------|-----|
| TestGetOrCreate_BranchReuse_PurgesConflictingRecyclable | ~12s | 5x GetOrCreate with real git repos |
| TestPushToBranch_RebasedWithExtraOriginCommits_Confirmed | ~7s | Real git push/rebase operations |
| TestGetOrCreate_BranchReuse_PromotesRecyclableStatus | ~7s | Multiple workspace lifecycle operations |
| TestGetOrCreate_RecyclableBranchCollision_PurgesAndRetries | ~7s | Workspace creation + disposal + retry |
| TestGitGraph_MaxCommits | ~7s | Generates many real git commits |

All top-20 slowest backend tests are in `internal/workspace/` and involve real git operations.

The top-4 slowest E2E tests (~20s each) involve daemon restart cycles:
- `TestE2EOverlayDaemonRestart` — start, spawn, stop, restart, verify overlay state survived
- `TestE2ERemoteStatePersistence` — start, spawn remote, stop, restart, verify remote state
- `TestE2ESignalDaemonRestart` — start, spawn, signal, stop, restart, verify signal state
- `TestE2EGitAmendAndUncommit` — full git amend + uncommit workflow

The remaining E2E tests cluster at 10-13s — this is the baseline cost of daemon start + workspace creation + spawn in Docker.

## Known Issues (Fixed)

### lsof contention freezes machine during --repeat runs

**Problem**: `preview.LookupPortOwner`, `preview.BuildPortOwnerCache`, and `detectPortsViaLsof` call `lsof` directly. With `--repeat 3`, dozens of parallel test packages invoke `lsof` simultaneously, which on macOS scans the entire process table per call. This froze the machine.

**Fix** (2025-04-09): Made all three functions pluggable via package-level function variables (`LookupPortOwnerFunc`, `BuildPortOwnerCacheFunc`, `detectPortsForPIDFunc`). Dashboard tests swap in lightweight TCP-connect alternatives via `TestMain` in `internal/dashboard/testmain_test.go`. Production code still uses `lsof`.

**Never revert**: `internal/dashboard/testmain_test.go` — removing it re-enables lsof in tests.

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
