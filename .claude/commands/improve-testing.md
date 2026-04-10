---
description: Continuous test improvement loop — increase meaningful coverage, reduce flakiness, and improve performance in prioritized rounds
---

## Overview

A unified test improvement loop that runs three phases in sequence, making one round of improvements per invocation. Each round profiles the current state, picks the highest-impact improvement across all three axes, executes it, and verifies the result.

**Before doing anything, read `test/TESTING.md`.** This is the testing knowledge base — it contains current baselines, known slow tests that can't be fixed, coverage gaps that aren't worth testing, and a history of previous improvement rounds. Use this to avoid re-investigating known issues.

## Phase 0: Baseline

Before any improvements, establish the current state using the test runner's built-in profiling. Check `test/TESTING.md` first — if the baseline is recent (within the last week), you can skip the full profiling run and just verify the numbers are still accurate with a quick `./test.sh --quick`.

Run these two commands separately (do NOT combine them — coverage instrumentation adds overhead that skews timing and can cause false flakiness):

1. **Coverage run**: `./test.sh --coverage --no-cache`
   - Captures per-package and per-function coverage data (uncovered functions listed)

2. **Timing + flakiness run**: `./test.sh --repeat 3 --no-cache`
   - Captures clean per-test durations without coverage overhead
   - Runs each test 3 times — the runner's flaky detection reports tests with mixed pass/fail across runs (with flaky scores and rerun commands)

Present a summary table from the output:

```
┌──────────────────────┬──────────┬───────────┬──────────┐
│ Metric               │ Current  │ Target    │ Status   │
├──────────────────────┼──────────┼───────────┼──────────┤
│ Backend coverage     │ XX.X%    │ —         │          │
│ Frontend coverage    │ XX.X%    │ —         │          │
│ Slowest test         │ XX.Xs    │ <2s       │          │
│ Tests >2s            │ N        │ 0         │          │
│ Flaky tests (N runs) │ N        │ 0         │          │
│ Total suite time     │ XX.Xs    │ —         │          │
└──────────────────────┴──────────┴───────────┴──────────┘
```

Then proceed to Phase 1.

---

## Phase 1: Coverage — Test What Matters

**Goal**: Add tests for uncovered code paths that have the highest regression risk. Not chasing a number — targeting code that is complex, recently changed, or error-prone.

### Procedure

1. Run `./test.sh --coverage` to get per-function coverage data
2. Identify the **highest-value untested code** by scoring each uncovered function:
   - **Complexity weight**: functions with branching logic, error handling, or state mutations score higher than simple getters
   - **Change frequency**: `git log --format=oneline <file> | wc -l` — frequently changed files have more regression risk
   - **Package importance**: `internal/dashboard/`, `internal/session/`, `internal/workspace/` are critical paths; `internal/benchutil/` is not
3. Pick the top 3 uncovered functions by score
4. For each, write a test that:
   - Tests the **happy path** and at least one **error/edge case**
   - Uses existing test patterns from the same package (read a nearby `_test.go` file first)
   - Follows table-driven test style where appropriate
5. Run the new tests to verify they pass
6. Re-run `./test.sh --coverage` to confirm coverage increased for those functions
7. Report what was added and the coverage delta

### Skip criteria

Do NOT add tests for:

- Simple getters/setters with no logic
- Functions that are thin wrappers around stdlib calls
- Code behind build tags (e2e) that's tested via E2E suite
- Generated code

---

## Phase 2: Performance — Make Tests Faster

**Goal**: Reduce test suite duration by fixing the slowest tests without introducing flakiness.

### Procedure

1. Use the timing data from Phase 0's `./test.sh` output (the "Slowest tests" table and per-test durations). If Phase 0 was skipped, run `./test.sh --no-cache` and read the slowest tests from the output.
2. For each of the top 5 slowest tests (across ALL suites — backend, frontend, E2E, scenarios):
   - Read the test to diagnose WHY it's slow. Common root causes by suite:

   **Backend unit tests:**
   - `time.Sleep` → replace with condition-based polling
   - Heavy setup repeated per-test → extract to `TestMain` or shared fixture
   - Real I/O → use in-memory alternatives where possible
   - Large timeout waits → reduce timeout and poll for condition

   **E2E tests** (Go tests running in Docker):
   - Excessive wait times for daemon startup → tighten health-check polling intervals
   - Redundant daemon start/stop cycles → share daemon across related tests
   - Unnecessary sleeps between operations → poll for expected state instead
   - Slow WebSocket connection setup → reuse connections where possible
   - Large timeout constants → profile actual times and set tighter bounds

   **Scenario tests** (Playwright running in Docker):
   - `waitForTimeout` calls → replace with `waitForSelector` or `expect(...).toBeVisible()`
   - Redundant page navigation → reuse page state across related steps
   - Slow element selectors → use more specific selectors (data-testid)
   - Unnecessary screenshot/video capture → disable for non-debugging runs
   - Docker image build time → optimize Dockerfile layers and caching

   **Frontend unit tests** (Vitest):
   - Heavy component renders → mock expensive children
   - Repeated context provider setup → use shared test wrappers
   - `waitFor` with long timeouts → tighten to actual expected timing

   - Apply the fix
   - Verify reliability: use `./test.sh --<suite> --run <TestName> --repeat 3` and confirm it's faster

3. Report before/after timings for each fixed test

### Skip criteria

Do NOT optimize:

- Tests that are already <1s (unit) or <5s (E2E/scenario)
- Tests where the sleep/wait is testing actual timing behavior

---

## Phase 3: Flakiness — Eliminate Unreliable Tests

**Goal**: Find and fix tests that intermittently fail, eroding trust in the suite.

### Procedure

1. Use flaky data from Phase 0 (which ran `--repeat 3`). If more signal is needed, run `./test.sh --backend --repeat 5 --no-cache` for a deeper scan. The test runner automatically detects flaky tests (mixed pass/fail across runs) and reports them with flaky scores and rerun commands.
2. Review the flaky report output — tests with `flakyScore > 0` are flaky
3. If no flaky tests found, check git history: `git log --all --oneline --grep="flak\|retry\|intermittent"` for previously-known flaky tests and verify they're stable with `./test.sh --backend --run <TestName> --repeat 5`
4. For each flaky test, diagnose the root cause:
   - **Timing dependency**: test assumes operation completes within N ms → add condition polling
   - **Shared state**: test depends on global state from another test → isolate with `t.Parallel()` guards or dedicated fixtures
   - **Port conflicts**: test binds to a fixed port → use port 0 for auto-assignment
   - **Race condition**: test has concurrent goroutines without proper synchronization → add `sync.WaitGroup` or channels
5. Apply fixes and verify with `./test.sh --backend --run <TestName> --repeat 5` to confirm the fix holds
6. Report which tests were flaky, root cause, and fix applied

---

## Loop Behavior

After completing all three phases:

1. **Update `test/TESTING.md`** with everything learned this round:
   - Update the baseline table with new numbers
   - Add any newly discovered slow tests to "Known Slow Tests"
   - Add any investigated coverage gaps to "Coverage Gaps"
   - Add any fixed issues to "Known Issues (Fixed)"
   - Append a new entry to "Improvement History" summarizing what was done
   - Update the "Last updated" date
2. Present a **round summary** comparing before/after for each metric
3. Ask: **"Run another round?"**
   - If yes: go back to Phase 0 (re-baseline with improvements applied)
   - If no: commit all improvements with `/commit`

Each round should take 10-20 minutes and produce a measurable improvement. The loop naturally converges — early rounds have big wins, later rounds have diminishing returns.

---

## Rules

- **Always verify**: never claim an improvement without measuring before AND after
- **Never weaken tests**: coverage additions must test real behavior, not just exercise lines. Performance fixes must not reduce assertion strength. Flakiness fixes must not add blanket retries.
- **One thing at a time**: fix one test, verify, move on. Don't batch multiple speculative changes.
- **Use existing patterns**: read nearby test files before writing new tests. Match the style.
- **Run `./test.sh --quick` after each change** to ensure nothing broke.
