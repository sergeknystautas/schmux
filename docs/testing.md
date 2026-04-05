# Testing Guide

## What it does

Testing infrastructure for schmux: Go backend unit tests, React frontend Vitest tests, Docker-based E2E integration tests, and Playwright-based scenario regression tests.

---

## Key files

| File                                            | Purpose                                                                                           |
| ----------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| `test.sh`                                       | Unified test runner with `--quick`, `--all`, `--e2e`, `--scenarios`, `--race`, `--coverage` flags |
| `Dockerfile.e2e`                                | Docker container for E2E tests (tmux, Go binary, schmux config)                                   |
| `.claude/commands/commit.md`                    | Definition-of-done enforcement (runs tests before committing)                                     |
| `test/scenarios/*.md`                           | Scenario files — plain English descriptions of user goals                                         |
| `test/scenarios/generated/*.spec.ts`            | Generated Playwright tests from scenario files                                                    |
| `test/scenarios/generated/helpers.ts`           | Shared test harness (setup, teardown, API client)                                                 |
| `test/scenarios/generated/playwright.config.ts` | Playwright configuration                                                                          |
| `test/scenarios/check-coverage.sh`              | Checks whether UI/API changes have corresponding scenarios                                        |
| `tools/test-runner/src/cache.ts`                | Cache key computation, load/save/expire, miss logging for Docker suites                           |

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

# Or run Go tests directly
go test ./...
go test -v ./...
go test -cover ./...
go test ./internal/tmux     # Specific package
```

**IMPORTANT:** Never run frontend tests by `cd`-ing into `assets/dashboard/` and invoking `npx vitest run` directly. Frontend tests are included in `./test.sh --quick`. Running vitest from the subdirectory bypasses the project test wrapper and produces unreliable results.

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

### Cache behavior

- Only passing results are cached (a failed suite must always re-run)
- 7-day TTL guards against stale Docker base images
- Atomic writes (write-to-temp + rename) so Ctrl+C never leaves corrupt cache files
- `--no-cache` deletes `.test-cache/` entirely AND passes `-count=1` to Go (bypasses Go's own cache)
- Corrupt cache JSON is treated as a cache miss — parse error deletes the file and runs normally
- Cache miss logging shows exactly which input changed (e.g., "HEAD changed: abc → def", "dirty files: ...")

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

The `/commit` command (`.claude/commands/commit.md`) enforces a definition of done at the commit boundary. Agents commit via `/commit`; the command runs checks before proceeding.

### Mechanical checks (automated)

1. **Categorize staged files** into behavioral (Go, TypeScript, package files) and non-behavioral (docs, scripts, config).
2. **API docs check** — if any staged file is in `internal/dashboard/`, `internal/config/`, `internal/state/`, `internal/workspace/`, `internal/session/`, or `internal/tmux/`, then `docs/api.md` must also be staged.
3. **Run tests** — `go vet ./...` and `./test.sh --quick` for behavioral changes. Skipped for non-behavioral-only commits.

### Judgment checks (agent self-assessment)

1. **Tests written** — every new function, handler, or component has a corresponding test.
2. **No architecture drift** — uses existing patterns (WebSocket state, SessionsContext, project logging, modal/toast conventions) rather than inventing new ones.
3. **Docs current** — relevant docs updated beyond just `docs/api.md`.

### Design rationale

- **Hard-enforces for agents** via `/commit`; humans can still run `git commit` directly when appropriate.
- **Auditable** — the DoD criteria are readable in `.claude/commands/commit.md`.
- **Future extensibility** — the criteria are structured as configuration-like steps, anticipating a future product feature where per-workspace DoD config lives in `.schmux/config.json`.

---

## Adding Tests

When adding new functionality:

1. Add unit tests in the same package
2. For parsing/validation, use table-driven tests
3. For complex operations, add multiple test cases (happy path, errors, edge cases)
4. For user-facing features, write a scenario file in `test/scenarios/`
5. Run `./test.sh --quick` before committing

---

## See Also

- [Architecture](architecture.md) — Package structure
- [Terminal Pipeline](terminal-pipeline.md) — Terminal streaming architecture
- [E2E Tests](e2e.md) — Detailed E2E test setup
