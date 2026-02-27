# Dual Coverage Tracking: Unit vs Integration

## Goal

Track which lines of code are covered by unit tests only, integration tests only, both, or neither. The primary signal is **confidence mapping**: lines covered only by unit tests are higher regression risk because unit tests can miss interaction bugs.

## Current State

| Suite                         | Coverage? | Mechanism                                              |
| ----------------------------- | --------- | ------------------------------------------------------ |
| Backend unit (Go)             | Yes       | `go test -coverprofile=coverage.out -covermode=atomic` |
| Frontend unit (Vitest)        | Yes       | V8 provider, text reporter to stdout                   |
| E2E (Docker/Go)               | No        | Binary compiled with `-c`, no coverage flags           |
| Scenarios (Docker/Playwright) | No        | Black-box, no instrumentation                          |

No merging or comparison happens today.

## Design

### Go Backend Integration Coverage

**Binary instrumentation**: Use `go build -cover` (Go 1.20+) to compile the schmux binary with coverage instrumentation. Set `GOCOVERDIR` environment variable so the binary writes coverage profiles on graceful exit.

Both test environments send SIGTERM and the daemon handles it with a full graceful shutdown, so coverage data flushes reliably.

**E2E tests**: Build a coverage-instrumented schmux binary alongside the test binary. The e2e test code starts/stops the daemon via `schmux stop` (SIGTERM). After tests complete, extract `GOCOVERDIR` contents from the Docker container.

**Scenario tests**: Build coverage-instrumented binary in the scenario Docker image. The entrypoint runs `schmux daemon-run` with `GOCOVERDIR` set. Add `wait $DAEMON_PID` after the existing `kill $DAEMON_PID` to ensure coverage flushes before exit. Extract coverage data from container.

**Profile conversion and comparison**:

1. Convert binary `covdata` to text coverprofile format via `go tool covdata textfmt`
2. Merge e2e + scenario profiles into a single "integration" profile
3. Parse both unit and integration profiles into statement-level coverage sets
4. Classify each statement block as unit-only, integration-only, both, or neither
5. Aggregate per-package

### React Frontend Integration Coverage

**Unit side**: Add `'json'` reporter to Vitest coverage config in `vite.config.js` so it writes `coverage/coverage-final.json` in Istanbul format (alongside existing text output).

**Integration side**: Use `vite-plugin-istanbul` to instrument the production build when `VITE_COVERAGE=true`. During Playwright scenario tests, extract `window.__coverage__` after each test via a fixture/afterEach hook, write per-test JSON files. Use `nyc merge` to combine into a single `coverage-scenarios.json`.

**Comparison**: Both Vitest JSON reporter and Istanbul instrumentation produce Istanbul-format JSON. Parse both, classify statements per-directory, print comparison table.

### Output Format

Gated behind the existing `--coverage` flag. Example output:

```
Go Coverage Comparison (unit vs integration)
─────────────────────────────────────────────────────────────────────
Package                          Unit   Integ   Both  Neither  Total
─────────────────────────────────────────────────────────────────────
internal/dashboard                12%     31%    18%     39%    482
internal/session                   8%     22%    35%     35%    310
...
─────────────────────────────────────────────────────────────────────
Total                              9%     17%    14%     60%   3841

Frontend Coverage Comparison (unit vs integration)
─────────────────────────────────────────────────────────────────────
Directory                        Unit   Integ   Both  Neither  Total
─────────────────────────────────────────────────────────────────────
src/components                    22%     18%    26%     34%    312
...
─────────────────────────────────────────────────────────────────────
Total                             24%     18%    21%     37%    722
```

Columns sum to 100% per row. "Total" column is statement count.

## Implementation Steps

### Step 1: Go e2e binary with coverage

- Modify `tools/test-runner/src/suites/e2e.ts` to compile schmux with `go build -cover`
- Modify `scripts/e2e-entrypoint.sh` to set `GOCOVERDIR`
- Mount/extract coverage directory from Docker container

### Step 2: Go scenario binary with coverage

- Same changes in `tools/test-runner/src/suites/scenarios.ts` and `test/scenarios/generated/entrypoint.sh`
- Add `wait $DAEMON_PID` after `kill` to ensure flush

### Step 3: Go profile merge and compare

- Add coverprofile parser in `tools/test-runner/src/coverage.ts`
- Convert covdata to text via `go tool covdata textfmt`
- Merge e2e + scenario into "integration" profile
- Compare against unit `coverage.out`, print per-package table

### Step 4: Frontend JSON reporter for Vitest

- Add `'json'` to coverage reporters in `vite.config.js`

### Step 5: Frontend Istanbul instrumentation for scenarios

- Add `vite-plugin-istanbul` dev dependency
- Configure activation on `VITE_COVERAGE=true`
- Modify scenario Dockerfile to build with that env var

### Step 6: Frontend Playwright coverage extraction

- Add fixture/afterEach to extract `window.__coverage__`
- Add `nyc merge` step in entrypoint
- Extract from Docker

### Step 7: Frontend profile compare

- Parse Istanbul JSON in `tools/test-runner/src/coverage.ts`
- Compare Vitest vs Playwright coverage per-directory
- Print frontend comparison table

### Step 8: Wire into test runner

- Gate all changes behind `--coverage` flag
- No behavioral change when flag is absent
