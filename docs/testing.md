# Testing Guide

## What it does

Testing infrastructure for schmux: Go backend unit tests, React frontend Vitest tests, Docker-based E2E integration tests, and Playwright-based scenario regression tests.

**The `Test authoring rubric` section below is the sole source of test-authoring rules.** Every later section is operational reference. Other docs and skills link here instead of restating rules.

---

## Key files

| File                                            | Purpose                                                                                                |
| ----------------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| `test.sh`                                       | Unified test runner with `--quick`, `--all`, `--e2e`, `--scenarios`, `--race`, `--coverage` flags      |
| `Dockerfile.e2e`                                | Docker container for E2E tests (tmux, Go binary, schmux config)                                        |
| `.agents/skills/commit/SKILL.md`                | Shared Claude/Codex definition-of-done workflow                                                        |
| `.agents/skills/test-rules-review/`             | Read-only rubric review of changed tests (`scan.sh` + judgment); a violations verdict blocks `/commit` |
| `test/scenarios/*.md`                           | Scenario files — plain English descriptions of user goals                                              |
| `test/scenarios/generated/*.spec.ts`            | Generated Playwright tests from scenario files                                                         |
| `test/scenarios/generated/helpers.ts`           | Shared test harness (setup, teardown, API client)                                                      |
| `test/scenarios/generated/playwright.config.ts` | Playwright configuration                                                                               |
| `test/scenarios/check-coverage.sh`              | Checks whether UI/API changes have corresponding scenarios                                             |
| `tools/test-runner/src/cache.ts`                | Cache key computation, load/save/expire, miss logging for Docker suites                                |
| `tools/test-runner/src/self-tests/`             | Self-tests for the runner itself (node:test via tsx); `test.sh` runs them before any suite             |
| `scripts/determinism.sh`                        | Fresh-process sampling harness for non-deterministic backend tests                                     |
| `badcode.sh`                                    | Static analysis + `tsc --noEmit` across all TS trees (pre-commit)                                      |

---

## Test authoring rubric

### Policy rules

**Synchronization**

1. Wait for a semantic state transition, not guessed elapsed time.
2. A deadline is a failure backstop. It must not make the test pass or trigger a repeated correctness assertion.
3. Preserve time-based product claims. Use a fake/injected clock, a completion event, or one observation window whose duration is the claim.
4. Negative claims may use a bounded observation window, with the reason adjacent to the wait.
5. Playwright locator assertions and React Testing Library `findBy*`/`waitFor` are allowed for an eventual UI state. Do not use them to retry a result after the system says the operation is complete.

**Assertion and probing**

6. Opaque external-process readiness may use one centralized condition probe with a deadline, interval, last observation, and failure diagnostics. Do not duplicate probe loops in individual tests.
7. Never retry a behavioral assertion. If terminal equality is valid only after rendering settles, expose and await "render settled," then compare once.
8. Performance measurements run only through manual benchmark commands and never pass or fail PR CI. Functional tests may assert authored timeout configuration or behavior under a fake clock.

**Isolation**

9. Tests own their HOME/config/git repository/tmux socket/ports/processes and clean them up. Ambient developer state must not affect the answer.

**Gate placement**

10. Put the test in the lowest gate that can make the same deterministic assertion.
11. A skipped or unexecuted gate is missing evidence, not a pass.
12. On failure, report the observed value or sequence and preserve available logs/artifacts. Do not widen tolerance, increase retries, or lengthen a settling sleep as "diagnosis."

Rule numbers are stable; reviews and docs cite rules by number.

### Gates

| Gate                     | Command                                       | What it proves                                                                                                                                            | Where it runs                                                                                      |
| ------------------------ | --------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| Quick                    | `./test.sh --quick`                           | Backend + frontend unit behavior, no Docker                                                                                                               | CI (`unit.yml`), pre-commit baseline                                                               |
| E2E                      | `./test.sh --e2e`                             | CLI → daemon → tmux → HTTP API in Docker                                                                                                                  | CI (`e2e.yml`)                                                                                     |
| Scenarios                | `./test.sh --scenarios`                       | User-goal regression via Playwright in Docker                                                                                                             | CI (`scenarios.yml`)                                                                               |
| Full                     | `./test.sh` (default)                         | All four suites sequentially                                                                                                                              | Local pre-commit requirement; release verification re-runs quick + e2e + scenarios (`release.yml`) |
| Race                     | `./test.sh --race`                            | Concurrency safety under the race detector                                                                                                                | Local                                                                                              |
| Coverage                 | `./test.sh --coverage`                        | Coverage measurement (never combined with `--repeat`)                                                                                                     | Local                                                                                              |
| Repeat / flake detection | `./test.sh --<suite> --repeat N`              | Flake evidence with completeness enforcement (a test observed fewer than N times marks the suite broken)                                                  | Local diagnostic                                                                                   |
| Determinism sampling     | `./scripts/determinism.sh`                    | Order/scheduling/host sensitivity across fresh-process configurations                                                                                     | Local (CI scheduling is planned, not present)                                                      |
| Benchmarks               | `./test.sh --bench`, `./test.sh --microbench` | Performance measurement: native Go/PTY benchmarks plus the browser typing benchmark in the docker-scenario container profile; never a correctness verdict | Manual only                                                                                        |
| Type/static analysis     | `./badcode.sh`                                | Static analysis plus `tsc --noEmit` across all TS trees (including `test/scenarios/generated`)                                                            | Local pre-commit                                                                                   |

Placement follows rules 10 and 11: each test lives in the cheapest gate that can make its assertion, and a skipped gate is missing evidence, not a pass. Rule 8 keeps benchmarks out of PR verdicts entirely.

Browser benchmark specs are named `*.bench.spec.ts`: the scenario gate's Playwright config ignores that pattern, and only the benchmark config (one worker, zero retries) selects them. `./test.sh --bench` exits nonzero only when a benchmark produced no valid sample — a percentile above the 500 ms typing objective (`test/scenarios/typing-latency.md`) is reported, never a verdict.

### Allowed exceptions

| Exception                              | Relaxes                                    | Required reason                                                  | Required failure backstop                                           |
| -------------------------------------- | ------------------------------------------ | ---------------------------------------------------------------- | ------------------------------------------------------------------- |
| Deadline timer around an awaited event | Rule 2                                     | Why this duration bounds the wait                                | Deadline expiry fails the test with last observed state             |
| Negative observation window            | Rule 1                                     | What absence is being proven, and why this window is long enough | Window expiry is the assertion; timestamp the boundary              |
| Product timing claim                   | Rule 1                                     | The claim being preserved                                        | One observation window whose duration is the claim, or a fake clock |
| Centralized external-process probe     | Rules 1, 7                                 | Why the boundary is opaque (black-box process, OS state)         | Deadline, interval, last observation, and diagnostics on failure    |
| Docker stale-base-image rebuild        | None — listed to prevent misclassification | Dependency setup, not assertion logic                            | Rebuild failure fails the suite                                     |

### Examples by framework

1. **Go goroutines — channels and callbacks.** Await the completion signal (channel close, `sync.WaitGroup`, callback) with `select` and a deadline. A sleep between poll attempts violates rule 1 even when the loop usually works.
2. **Go timers and debounces — injected clock.** Production code takes a `now func() time.Time` defaulting to `time.Now`; tests advance the clock instead of sleeping. (This is the required pattern. `RateLimiter` in `internal/dashboard/server.go` has one; other packages gain theirs as their tests demand.)
3. **Dashboard state — WebSocket events.** Await the state transition on `/ws/dashboard` (initial snapshot, then events) or `SessionsContext.waitForSession` instead of polling `GET /api/sessions`.
4. **Playwright — locator assertions.** `expect(locator).toBeVisible()` and `expect.poll` for debounced API state await an eventual UI state (rule 5). They are not for retrying a result after the operation reports completion.
5. **React Testing Library — async queries.** `findBy*`/`waitFor` await an eventual UI state (rule 5). Once the system signals completion, assert once (rule 7).
6. **Terminal fidelity — render completion.** Await "render settled" (a prompt-embedded marker parsed and no pending write/render work) via the stream's completion API, then capture tmux and xterm once, compare once. The assert helpers enforce the boundary structurally — a `sentinel` option is required.
7. **External processes — one centralized probe.** Daemon health (`waitForHealthy`) and shell-prompt readiness (`waitForShellPrompt`) are canonical: one helper, deadline, interval, last observation, failure diagnostics (rule 6). Tests call the helper; they never embed their own probe loops. Terminal control-mode readiness has its own canonical wait: `waitForControlModeAttached` (scenario helpers) asserts the session pill's `data-control-mode` attribute, fed by the backend's connect-time `controlMode` snapshot — no probe loop, no fixed delay.
8. **Negative claims — one bounded window.** The dismissed-tab regression is canonical: one `waitForTimeout` whose duration is the claim, with the reason in an adjacent comment (rule 4).

### Failure telemetry

A failing test must be diagnosable from its first occurrence. Report:

- Last observed state and the event sequence that preceded it (terminal convergence diagnostics already capture this).
- Relevant IDs and deadlines: session/workspace IDs, awaited marker, deadline value, timestamped boundaries.
- Daemon logs and terminal captures where the suite produces them.
- Artifact location: Playwright failures land in `test/scenarios/artifacts/`; the determinism harness preserves raw JSON streams and stderr under `.schmux/determinism/`.

Rule 12 bounds this: none of it licenses widening tolerance or lengthening waits as "diagnosis."

---

## Running Tests

```bash
# Recommended: all fast tests (backend + frontend, no Docker)
./test.sh --quick

# All tests (unit + E2E + scenarios)
./test.sh --all

# Unit tests with race detector
./test.sh --race

# Unit tests with coverage report
./test.sh --coverage

# E2E tests only (requires Docker)
./test.sh --e2e

# Scenario tests only (Playwright, requires Docker)
./test.sh --scenarios

# To run the Docker suites inside a fenced schmux session, the repo must enable
# the `docker` fence preset — see docs/fenced-sessions.md.

# Or run Go tests directly
go test ./...
go test -v ./...
go test -cover ./...
go test ./internal/tmux     # Specific package
```

**IMPORTANT:** Never run frontend tests by `cd`-ing into `assets/dashboard/` and invoking `npx vitest run` directly. Frontend tests are included in `./test.sh --quick`. Running vitest from the subdirectory bypasses the project test wrapper and produces unreliable results.

---

## Browser benchmark suite

The browser typing benchmark runs through `./test.sh --bench` in an isolated scenario Docker container and measures end-to-end keystroke round-trip latency (browser keydown → WebSocket → server → tmux → numbered agent acknowledgement → back → xterm render settled) under idle and stressed conditions. A flood frame cannot end a stressed sample because every key waits for its own unique acknowledgement. The suite exits nonzero only when the benchmark cannot produce a valid sample — never because a percentile exceeded the 500 ms product objective. Latency values are measurements, not verdicts.

### Key files

| File                                                    | Purpose                                                                                                      |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `test/scenarios/generated/typing-latency.bench.spec.ts` | Browser bench spec; runs only under `--bench`; emits `BENCH_RESULT_JSON:` lines per variant                  |
| `test/scenarios/generated/typing-latency.spec.ts`       | Gate-runnable functional echo scenario; assertions only, no latency thresholds                               |
| `test/scenarios/generated/playwright.bench.config.ts`   | Bench-only Playwright config (one worker, zero retries, 180s timeout)                                        |
| `test/scenarios/generated/playwright.config.ts`         | Default gate config; `testIgnore: '**/*.bench.spec.ts'` makes bench specs structurally invisible to the gate |
| `test/scenarios/generated/entrypoint.sh`                | Branches to the bench config when env `BENCH_BROWSER=1` is set                                               |
| `test/scenarios/typing-latency.md`                      | Scenario source-of-truth; the 500 ms objective lives in its **Performance objective** section, nowhere else  |
| `tools/test-runner/src/suites/bench.ts`                 | Bench orchestrator; native steps + `runBrowserTypingBenchmark`                                               |
| `tools/test-runner/src/bench-collect.ts`                | Pure parser / validator / builder for browser bench results; covered by `bench-collect.test.ts`              |
| `tools/test-runner/src/types.ts`                        | `BrowserBenchResult`, `BrowserBenchEnvironment`, `BrowserBenchReport` types                                  |
| `internal/benchutil/benchutil.go`                       | Go-side `ComputeBenchResult` / `ReportJSON` helpers; native Go/PTY benchmarks emit the same JSON shape       |

### Architecture decisions

- The benchmark runs in an isolated scenario container — its daemon lives inside the container, not the developer's. `bench.ts` orchestrates the container directly through `docker.ts` / `shared.ts` primitives; it never invokes `./test.sh --scenarios` recursively.
- The default Playwright config's `testIgnore` is the structural guarantee: the scenario gate cannot discover `*.bench.spec.ts` on full runs, via `TEST_GREP`, or via repeats. Only the bench config's `testMatch` selects them.
- One worker and zero retries in `playwright.bench.config.ts` keep sample counts clean; retrying a benchmark hides lost samples (rule 12) and distorts percentiles.
- The host-side canonical report is `bench-results/<date>/browser-typing-latency.json`. The spec also writes `/artifacts/browser-typing-latency.json` inside the container for diagnosis; the container's `playwright-report/`, traces, and daemon logs land at `bench-results/<date>/browser/`.
- `cpusPinned: false` is recorded in the report — no `--cpus` flag is set on the container. Baseline comparisons across runs must account for unpinned CPUs.
- Status is nonzero on: unavailable Docker, container failure, zero parsed results, a missing variant, or any variant without exactly 30 samples. Percentile values — even those above 500 ms — are printed and never affect status.

### Gotchas

- Don't put latency thresholds in scenario-gate specs; they regenerate from `test/scenarios/*.md`. The 500 ms objective lives in `test/scenarios/typing-latency.md`'s **Performance objective** section — that is the only documented place.
- Bench specs must end in `*.bench.spec.ts`. Filename drift breaks both the gate exclusion and the bench selection.
- Never set `BENCH_BROWSER=1` in the scenario gate — it switches `entrypoint.sh` to the bench config.
- `parseBenchResultLine` rejects lines whose benchmark name, variant, or non-negative numeric fields don't match the `BrowserTypingLatency` schema. Validation also requires monotonic percentiles and exactly 30 samples per variant.
- `bench-collect.ts` is pure (no I/O); test new behavior directly in `tools/test-runner/src/self-tests/bench-collect.test.ts` rather than threading it through `bench.ts`.
- The browser benchmark relies on the scenario suite's image machinery (`schmux-scenarios-base` + `Dockerfile.scenarios`). A change to that pipeline can affect bench runs.

### Common modification patterns

- To add a new browser benchmark: create `*.bench.spec.ts`, emit one `BENCH_RESULT_JSON:` line per variant with `name`, `variant`, `iterations`, `p50_ms`, `p95_ms`, `p99_ms`, `max_ms`, `mean_ms`, `timestamp`, `nproc`, `userAgent`. Extend `BrowserBenchResult` and `parseBenchResultLine` if you need new fields.
- To add a new variant: extend `REQUIRED_BENCH_VARIANTS` in `bench-collect.ts`, the parser's variant whitelist, and the spec.
- To add a new native Go benchmark that emits JSON: call `benchutil.ComputeBenchResult` and `benchutil.ReportJSON` from your `_bench_test.go`. The shape matches what `parsers.ts` already ingests.
- To change report metadata: edit `BrowserBenchEnvironment` in `types.ts` and the host-side call in `bench.ts`'s `runBrowserTypingBenchmark`.

---

## Unit Test Conventions

### Framework

Standard Go `testing` package with `*_test.go` files and `TestXxx` naming.

### Table-Driven Unit Tests

Prefer table-driven tests for parsing and state transitions:

```go
func TestParseStatus(t *testing.T) {
    tests := []struct {
        name   string
        input  string
        want   Status
    }{
        {"running", "running", StatusRunning},
        {"stopped", "stopped", StatusStopped},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := ParseStatus(tt.input)
            if got != tt.want {
                t.Errorf("ParseStatus() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

### Unit Test Data

Test fixtures live in `testdata/` directories next to the code they test.

Example: `internal/tmux/testdata/` contains tmux session captures for testing terminal parsing.

---

## Package-Specific Notes

### `internal/tmux`

Tests use captured tmux output stored in `testdata/`. To update captures:

```bash
# In test directory
tmux new-session -d -s test-capture "your command"
tmux capture-pane -t test-capture -p > testdata/capture.txt
tmux kill-session -t test-capture
```

### `internal/dashboard`

Tests use a mock server. No external dependencies required.

### `internal/workspace`

Tests use temporary directories for workspace operations. Cleaned up automatically.

---

## Test Cache (Docker Suites)

E2E and scenario test suites are cached to skip re-running when inputs haven't changed. Cache files are stored in `.test-cache/` (gitignored), one JSON file per suite.

### Why only e2e and scenarios are cached

Go's built-in test cache handles per-package invalidation natively. Vitest's built-in caching handles per-file invalidation. Adding suite-level caching on top of those would add correctness surface area for marginal speedup.

### Cache key composition

The cache key includes: `git rev-parse HEAD`, dirty file hashes (`git status --porcelain` + `git hash-object`), the suite name, and flags (`--race`, `--coverage`). Switching branches auto-invalidates; switching back to a clean branch re-validates.

### Flags that disable caching

| Flag             | Reason                                                          |
| ---------------- | --------------------------------------------------------------- |
| `--run PATTERN`  | Partial test run must never satisfy a full-suite cache check    |
| `--repeat > 1`   | Repeat mode is for flaky detection; caching defeats the purpose |
| `--verbose`      | User expects output that a cached result cannot provide         |
| `--record-video` | User expects artifacts that a cached result cannot provide      |
| `--force`        | Rebuilding base images implies intent to re-test                |
| `--coverage`     | Coverage data dirs must be populated for dual coverage reports  |

### Repeat behavior

`--repeat N` runs each test N times via `go test -count=N` (backend),
`--repeat-each=N` (Playwright), and N independent `vitest run` processes
(frontend). Frontend results are aggregated from Vitest's JSON reporter;
any frontend test with fewer than N observed outcomes marks the suite
`broken` (INSUFFICIENT EVIDENCE) rather than reporting a clean verdict.

### Cache behavior

- Only passing results are cached (a failed suite must always re-run)
- 7-day TTL guards against stale Docker base images
- Atomic writes (write-to-temp + rename) so Ctrl+C never leaves corrupt cache files
- `--no-cache` deletes `.test-cache/` entirely AND passes `-count=1` to Go (bypasses Go's own cache)
- Corrupt cache JSON is treated as a cache miss — parse error deletes the file and runs normally
- Cache miss logging shows exactly which input changed (e.g., "HEAD changed: abc → def", "dirty files: ...")
- A stale-base-image rebuild in the Docker runner is a dependency-setup retry, not an assertion retry; it may remain as-is

---

## End-to-End (E2E) Testing

E2E tests validate the full system: CLI -> daemon -> tmux -> HTTP API.

### Running E2E Tests

**In Docker (recommended):**

```bash
# Build and run E2E tests in Docker
docker build -f Dockerfile.e2e -t schmux-e2e .
docker run --rm schmux-e2e

# Or with artifact capture on failure
docker run --rm -v $(pwd)/artifacts:/home/e2e/internal/e2e/testdata/failures schmux-e2e
```

**Locally (requires schmux binary in PATH):**

```bash
# Build schmux first
go build -o schmux ./cmd/schmux

# Run E2E tests
go test -v ./internal/e2e
```

### What E2E Tests Validate

- Daemon lifecycle (start/stop/health endpoint)
- Workspace creation from local git repos
- Session spawning with unique nicknames
- Naming consistency across CLI, tmux, and API
- Session disposal and cleanup

### E2E Test Isolation

E2E tests run in Docker containers. The container provides all isolation:

- Container's `~/.schmux/` is isolated from host
- Container's port 7337 is isolated
- Container's tmux server is isolated

For full details, see `docs/e2e.md`.

---

## Scenario Testing

Scenario tests are a regression testing system where plain English scenario descriptions are the source of truth and Playwright test code is generated from them.

### Architecture

A scenario is something a user wants to accomplish. A scenario regression is when the user can no longer accomplish that goal. Two testing layers run the same scenarios:

| Layer              | What it checks       | How                                                       |
| ------------------ | -------------------- | --------------------------------------------------------- |
| API assertions     | Backend correctness  | HTTP/WebSocket calls with exact value checks              |
| Browser assertions | Frontend correctness | Playwright drives headless Chromium against the dashboard |

Both layers are in the same generated test file. If the API layer passes but the browser layer fails, it is a frontend problem. If the API layer fails, the backend is broken.

### Directory structure

```
test/scenarios/
├── spawn-single-session.md           # Human/agent-authored scenario (source of truth)
├── view-code-diff.md
├── dispose-session.md
├── ...
├── check-coverage.sh                 # Coverage check script
└── generated/
    ├── helpers.ts                     # Shared test harness
    ├── helpers-terminal.ts            # Terminal-specific helpers
    ├── playwright.config.ts           # Playwright configuration
    ├── spawn-single-session.spec.ts   # Generated Playwright test
    ├── view-code-diff.spec.ts
    └── ...
```

### Scenario files

Plain English markdown files. Each describes a user goal, steps, preconditions, and success criteria:

```markdown
# Spawn a session with two agents

A user wants to start two AI agents working on the same task.

## Preconditions

- The daemon is running with at least one repository configured

## Verifications

- The spawn form accepts the input and submits without error
- The home page shows the workspace with both sessions
- GET /api/sessions returns two sessions under the same workspace
```

The `## Verifications` section mixes UI checks and API checks naturally. The generator separates them into Playwright assertions and HTTP assertions.

### Generated tests

Generated test files live in `test/scenarios/generated/` and are committed to the repo. The generator reads scenario files, reads relevant UI code (React components, route definitions, API handlers), and produces Playwright test files. Generated files are regenerated entirely each time (no incremental mode).

### Authoring workflow

1. Implement a feature or fix a bug.
2. Write a scenario file in `test/scenarios/` describing the user-facing behavior.
3. Run the generator to produce the Playwright test.
4. Review both the scenario and generated test, then commit.
5. CI runs the generated tests deterministically on every PR.
6. When UI changes break a test, regenerate from the unchanged scenario file.

### Coverage check

`test/scenarios/check-coverage.sh` checks whether changed files touch UI routes or API handlers without a corresponding scenario update. It nudges but does not block.

---

## Definition of Done

The `/commit` workflow (`.agents/skills/commit/SKILL.md`) enforces a definition of done at the commit boundary. Codex discovers it as a project skill; `.claude/commands/commit.md` is Claude Code's thin slash-command entry point.

### Mechanical checks (automated)

1. **Categorize staged files** into behavioral (Go, TypeScript, package files) and non-behavioral (docs, scripts, config).
2. **API docs check** — if any staged file is in `internal/dashboard/`, `internal/config/`, `internal/state/`, `internal/workspace/`, `internal/session/`, or `internal/tmux/`, then `docs/api.md` must also be staged.
3. **Run tests and static analysis** — `go vet ./...`, `./test.sh`, and `./badcode.sh` for behavioral changes. Skipped for non-behavioral-only commits.

### Judgment checks (agent self-assessment)

1. **Tests written** — every new function, handler, or component has a corresponding test.
2. **No architecture drift** — uses existing patterns (WebSocket state, SessionsContext, project logging, modal/toast conventions) rather than inventing new ones.
3. **Docs current** — relevant docs updated beyond just `docs/api.md`.
4. **Rubric review** — changed tests pass the `test-rules-review` skill before completion; a violations verdict blocks the commit

### Design rationale

- **Hard-enforces for agents** via `/commit`; humans can still run `git commit` directly when appropriate.
- **Auditable** — the DoD criteria are readable in `.agents/skills/commit/SKILL.md`.
- **Future extensibility** — the criteria are structured as configuration-like steps, anticipating a future product feature where per-workspace DoD config lives in `.schmux/config.json`.

---

## Adding Tests

When adding new functionality:

1. Follow the test authoring rubric above — synchronization, gate placement, exceptions, and failure telemetry
2. Add unit tests in the same package
3. For parsing/validation, use table-driven tests
4. For complex operations, add multiple test cases (happy path, errors, edge cases)
5. For user-facing features, write a scenario file in `test/scenarios/`
6. Run `./test.sh --quick` before committing

---

## Test Reliability

### Contention vs. genuine flakiness

When running many Docker containers in parallel on a single host, terminal-pipeline tests fail 10-55% of the time due to CPU contention. These same tests pass reliably in isolation (0% failure rate over 20 runs). CI runs a single container per test suite, so contention failures do not affect real CI reliability.

To distinguish genuine flakiness from contention artifacts, run the suspect test in isolation multiple times:

```bash
./test.sh --scenarios --run "test name" --repeat 20
```

### Terminal fidelity test patterns

Terminal tests are timing-sensitive because the rendering pipeline (tmux capture -> WebSocket -> xterm.js buffer) involves multiple async stages. Key reliability patterns:

- **Use `resetAndSettle()` in `openTerminal`.** It drains any submitted xterm write before resetting (xterm's write queue survives `reset()`), cancels the armed write-flush rAF, and waits for the pipeline to clean. Never mutate `TerminalStream` private fields from tests.
- **Await full-pipeline delivery through the rendered xterm.js buffer, not just WebSocket delivery.** The landed pattern is `sendTmuxCommandWithSentinel` (prompt-embedded marker that cannot match echoed input) → the helper's completion wait → single comparison (rubric rule 7). The old sentinel poll and the assert retry loops are gone.
- **Dispose sessions in `afterAll`.** Accumulated sessions overload the daemon. Each `describe.serial` block should dispose its sessions when finished.
- **Treat absence as proven only over a bounded negative window.** A session appearing "missing" in the first `/ws/dashboard` broadcast may be stale initial state; wait for 2+ broadcasts before treating a session as gone (rubric rule 4 — the window's duration is the claim).

### Git auto-gc interference

Git's automatic garbage collection (`git gc --auto`) can cause intermittent failures in tests that create many commits. Disable it in test repos:

```go
exec.Command("git", "-C", repoDir, "config", "gc.auto", "0").Run()
```

## See Also

- [Architecture](architecture.md) — Package structure
- [Terminal Pipeline](terminal-pipeline.md) — Terminal streaming architecture
- [E2E Tests](e2e.md) — Detailed E2E test setup
- [Finding non-deterministic tests](dev/determinism.md) — Fresh-process sampling harness
