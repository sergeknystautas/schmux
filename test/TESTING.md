# Testing Knowledge Base

Living document updated by `/improve-testing`. Read this before starting a round — it saves re-investigating known issues.

Last updated: 2025-04-09

## Current Baseline

| Metric | Value | Date |
|--------|-------|------|
| Backend coverage | 45.0% | 2025-04-09 |
| Frontend coverage | 48.8% | 2025-04-09 |
| Total tests | 2,703 | 2025-04-09 |
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
- Fixed lsof contention that made repeat runs unusable
