VERDICT: NEEDS_REVISION

## Summary Assessment

The overall approach is sound -- per-suite caching for Docker tests is the right granularity, and the hybrid model with Go's built-in cache is pragmatic. However, the design has several gaps in cache key correctness that would cause false cache hits (skipping tests that should run), and it does not address how `--run` pattern filtering interacts with caching.

## Critical Issues (must fix)

### 1. `--run` pattern not addressed -- will produce false cache hits

The design never mentions `--run PATTERN`. If a user runs `./test.sh --backend --run TestFoo` and it passes, the cache key does not include the run pattern. A subsequent `./test.sh --backend` (full suite) would match the same key and report "cached" even though only `TestFoo` was tested. The `runPattern` must either be part of the cache key or caching must be disabled when `--run` is used.

### 2. Scenario suite cache key misses critical non-spec input files

The design says the scenario key includes "Content hash of all `test/scenarios/**/*.spec.ts` files". But the `test/scenarios/generated/` directory contains essential non-spec TypeScript files that directly affect test execution:

- `helpers.ts` -- shared test utilities imported by every spec
- `helpers-terminal.ts` -- terminal interaction helpers
- `coverage-fixture.ts` -- the test fixture (`import { test, expect } from './coverage-fixture'`) used by every spec
- `global-setup.ts` -- global setup hook
- `playwright.config.ts` -- Playwright configuration

A change to `helpers.ts` or `playwright.config.ts` would not invalidate the scenario cache, causing stale results. The glob should be `test/scenarios/**/*.ts` (all `.ts` files), not just `*.spec.ts`.

### 3. Frontend cache key does not include `vite.config.js` or `tsconfig.json`

The frontend suite key only hashes `package-lock.json` and dirty `.ts/.tsx/.css/.json` files under `assets/dashboard/`. But `vite.config.js` configures the build/test pipeline (test transforms, plugins, aliases) and `tsconfig.json` controls TypeScript compilation. Changes to either can break tests without changing any source file. These must be included in the frontend key.

### 4. `--repeat` not addressed in cache interaction

When `--repeat N` is used (flaky detection mode, N > 1), the purpose is explicitly to find non-deterministic failures. Caching a passing result from a repeat run and serving it on the next invocation defeats the purpose. The design should either include `repeat` in the cache key or (more sensibly) disable caching entirely when `--repeat > 1`.

### 5. Backend suite key uses HEAD but backend runner filters packages with `go list`

The backend runner calls `go list` to enumerate packages and excludes `/e2e$`. The cache key doesn't capture this filtering. More importantly, the backend key uses `git rev-parse HEAD` plus dirty `.go` files, but Go's built-in cache already handles per-package invalidation. The design claims this is a "hybrid" where Go's cache works underneath, but then the suite-level cache sits on top and may skip the entire `go test` invocation. If Go's cache is the real benefit for backend, why add a redundant suite-level cache that could mask issues? The backend suite-level cache adds complexity for marginal benefit since Go's own cache is already effective.

## Suggestions (nice to have)

### 1. Consider skipping suite-level cache for backend/frontend entirely

The design's main value is caching Docker-based suites (E2E, scenarios) where container startup dominates. For backend, Go's built-in test cache already skips unchanged packages. For frontend, vitest also has built-in caching. Adding a suite-level cache on top of these existing per-package caches introduces a correctness surface (all the input-set completeness issues above) for minimal speedup -- the existing caches already handle the common case. Consider implementing suite-level caching only for E2E and scenarios, and leaving backend/frontend to their existing per-package caching.

### 2. `bench` and `microbench` suites are unaddressed

The `SuiteName` type includes `'bench' | 'microbench'` and the design lists exactly four suites. Benchmarks are explicitly time-sensitive and should never be cached (their purpose is measuring performance, not pass/fail), but the design should state this explicitly to avoid a future implementer adding cache support for them.

### 3. `--verbose` and `--record-video` should be considered for cache interaction

When `--verbose` is passed, the user expects to see full output. Serving a "cached" result with no output might be confusing. Similarly, `--record-video` for scenarios requests video artifacts -- a cached result has no videos. The design should specify whether these flags bypass the cache or at minimum note that cached results won't produce artifacts/verbose output.

### 4. `--force` (Docker base image rebuild) is not part of the cache key

The `--force` flag rebuilds Docker base images. If someone runs `./test.sh --e2e --force` and it passes, the cache key is identical to a non-force run. A subsequent `./test.sh --e2e` would serve the cached result, which is technically correct (same code), but a user who explicitly used `--force` might expect that the next default run also re-tests. Not critical, but worth documenting.

### 5. Cache version field could prevent issues during migration

The `"version": 1` field is good, but the design doesn't say what happens when the version doesn't match. Specify: treat as cache miss and delete the file.

### 6. Consider logging which inputs changed on cache miss

When a cache miss occurs, knowing _why_ would be very helpful for debugging. For example: "backend: cache miss (HEAD changed: abc123 -> def456)" or "frontend: cache miss (dirty files: src/App.tsx)". This would cost little to implement and significantly aids debugging.

### 7. `--quick` mode interaction is ambiguous

`--quick` sets `race = false` and `coverage = false`, so the cache key components are clear. But `--quick` also restricts suites to `[backend, frontend]`. If someone runs `./test.sh` (all suites, backend passes and is cached), then runs `./test.sh --quick`, the backend cache from the full run would be served. This is correct behavior, but worth noting explicitly since the CLAUDE.md emphasizes that `--quick` is NOT a substitute for `./test.sh`.

## Verified Claims (things I confirmed are correct)

- **`--no-cache` currently only passes `-count=1` to Go**: Confirmed. In `suites/backend.ts:52-54`, `opts.noCache` adds `-count=1`. No other suite uses `noCache`. The design's plan to extend this flag is backward-compatible.

- **Dockerfiles exist as named**: `Dockerfile.e2e`, `Dockerfile.e2e-base`, `Dockerfile.scenarios`, `Dockerfile.scenarios-base` all exist at repo root.

- **Suite runners are in `tools/test-runner/src/`**: Confirmed. The runner structure matches the design's file-change list (`runner.ts`, `types.ts`, `ui.ts`, `main.ts`).

- **`SuiteResult` type exists and can be extended**: Confirmed in `types.ts:22-33`. Adding `cached?: boolean` is non-breaking.

- **Parallel worktrees have separate working directories**: Confirmed by the worktree-based project structure (path contains `schmux-005`). Each worktree has its own repo root, so `.test-cache/` would be worktree-local.

- **Scenario specs live under `test/scenarios/generated/*.spec.ts`**: Confirmed, 37 spec files.

- **`opts.race` and `opts.coverage` are boolean flags that affect test behavior**: Confirmed. Including them in the cache key is correct.

- **The existing `--force` flag only affects Docker base images, not test caching**: Confirmed in `docker.ts:80` and `e2e.ts:74`. The design correctly keeps these concepts separate.

- **`.test-cache/` is not in `.gitignore` yet**: Confirmed. The design correctly lists adding it.
