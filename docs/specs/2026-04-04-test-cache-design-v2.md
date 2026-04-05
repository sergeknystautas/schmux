# Test Cache: Skip Unchanged Suites (v2)

**Date:** 2026-04-04
**Status:** Approved

## Changes from previous version

Addresses all critical issues and suggestions from review:

1. **Dropped suite-level caching for backend and frontend.** Go and vitest already
   handle per-package caching natively. Suite-level caching on top added correctness
   surface for marginal speedup. Cache now applies only to e2e and scenarios.
2. **`--run PATTERN` disables caching entirely.** A partial test run must never
   satisfy a full-suite cache check.
3. **Scenario cache key expanded to `test/scenarios/**/\*.ts`.\*\* Catches helpers,
   fixtures, config, and setup files — not just specs.
4. **`--repeat > 1` disables caching.** Repeat mode is for flaky detection; caching
   defeats the purpose.
5. **`--verbose` and `--record-video` bypass the cache.** User expects output or
   artifacts that a cached result cannot provide.
6. **`--force` busts the cache for Docker suites.** Rebuilding base images implies
   intent to re-test.
7. **bench/microbench explicitly excluded.** Benchmarks measure performance, not
   pass/fail; caching them is nonsensical.
8. **Cache version mismatch treated as cache miss.** Mismatched version deletes the
   file and re-runs.
9. **Cache miss logging.** Logs which input changed to aid debugging.

## Problem

`./test.sh --all` runs all four suites (backend, frontend, e2e, scenarios) every
time, even when nothing has changed. Docker-based suites are especially expensive
— building images and spinning up containers takes minutes even when the code is
identical to the last run.

## Solution

Add a suite-level caching layer to the test runner for Docker-based suites only.
Before running e2e or scenarios, compute a hash of the suite's inputs. If the hash
matches a previous passing run, skip the suite and report the cached result.

Backend and frontend are **not** cached at the suite level — Go's built-in test
cache and vitest's built-in caching already handle per-package invalidation
effectively.

## Cache Scope

Only two suites are cached:

- **E2E**: Docker container startup dominates cost. All-or-nothing.
- **Scenarios**: Docker container startup dominates cost. All-or-nothing.

Not cached:

- **Backend**: Go's built-in test cache handles per-package invalidation. No
  suite-level cache needed.
- **Frontend**: Vitest's built-in caching handles per-file invalidation. No
  suite-level cache needed.
- **bench / microbench**: Benchmarks measure performance. Caching a benchmark
  result is meaningless — these suites must always run.

## Cache Key Computation

A new module `tools/test-runner/src/cache.ts` computes keys per suite. Each key
is `sha256(sorted list of component hashes)`.

### Input sets per suite

**E2E:**

- `git rev-parse HEAD`
- Content hash of `go.mod` + `go.sum`
- Sorted hashes of dirty/untracked `.go` files (via `git status --porcelain`)
- Content hash of `Dockerfile.e2e`, `Dockerfile.e2e-base`
- `opts.race` and `opts.coverage` booleans

**Scenarios:**

- `git rev-parse HEAD`
- Content hash of `go.mod` + `go.sum`
- Sorted hashes of dirty/untracked `.go` files
- Content hash of `assets/dashboard/package-lock.json`
- Sorted hashes of dirty/untracked `.ts`, `.tsx`, `.css`, `.json` files under
  `assets/dashboard/`
- Content hash of `Dockerfile.scenarios`, `Dockerfile.scenarios-base`
- Content hash of all `test/scenarios/**/*.ts` files (specs, helpers, fixtures,
  config, setup — not just `*.spec.ts`)
- Content hash of `test/scenarios/generated/package.json`,
  `test/scenarios/generated/package-lock.json`,
  `test/scenarios/generated/tsconfig.json`, and
  `test/scenarios/generated/entrypoint.sh` (all copied into Docker image)
- `opts.race` and `opts.coverage` booleans

### Invalidation triggers

- Changing any source file invalidates the relevant suites
- `go.mod` / `go.sum` changes invalidate e2e and scenarios
- `package-lock.json` changes invalidate scenarios
- Switching branches changes HEAD, invalidating all suites
- Switching back to a clean branch re-validates (same HEAD, no dirty files)
- `--race` or `--coverage` flags are part of the key — a non-race cached result
  won't satisfy a `--race` run

### Performance

Git operations (`rev-parse`, `status --porcelain`, `hash-object`) add ~50-100ms
before test execution. Negligible compared to any suite's runtime.

## Flags That Disable Caching

The following flags bypass the cache entirely (suite always runs):

| Flag                 | Reason                                                         |
| -------------------- | -------------------------------------------------------------- |
| `--no-cache`         | Explicit user intent to skip caching                           |
| `--run PATTERN`      | Partial test run must not satisfy full-suite cache             |
| `--repeat N` (N > 1) | Flaky detection requires real execution                        |
| `--verbose`          | User expects full output; cached result has none               |
| `--coverage`         | Coverage data dirs must be populated for dual coverage reports |
| `--record-video`     | User expects video artifacts; cached result has none           |
| `--force`            | Rebuilds Docker base images; user expects re-test              |

When any of these is active, the cache is neither read nor written for affected
suites.

## Cache Storage

A `.test-cache/` directory at the repo root (gitignored). One JSON file per cached
suite:

```
.test-cache/
  e2e.json
  scenarios.json
```

Each file:

```json
{
  "version": 1,
  "timestamp": "2026-04-04T14:30:00Z",
  "cacheKey": "sha256:abc123...",
  "status": "passed",
  "durationMs": 12400,
  "passedTests": ["TestFoo", "TestBar"],
  "skippedTests": ["TestSkipped"],
  "testCount": 847
}
```

### Version handling

If the `version` field does not match the current expected version, treat as a
cache miss: delete the file and re-run the suite. No migration logic.

### Expiry

7-day TTL for both suites. Guards against stale Docker base images (OS package
updates, etc.). After 7 days the suite re-runs even if the code hasn't changed.

### Atomicity

Cache writes use write-to-temp-file + rename. A Ctrl+C mid-run never leaves a
corrupt cache file.

## Runner Integration

Changes to `runner.ts` — before invoking a suite runner:

```
for each suite in [e2e, scenarios]:
  if cacheDisabled(opts):
    run suite normally
    continue

  key = computeCacheKey(suite)
  cached = loadCache(suite)

  if cached AND cached.version == CURRENT_VERSION AND cached.key == key AND not expired:
    emit cached result with stored pass/fail counts
    skip runner
  else:
    logCacheMissReason(suite, cached, key)  // e.g. "HEAD changed: abc→def"
    run suite normally
    on pass: saveCache(suite, key, result)
    on fail: deleteCache(suite)  // never cache failures
```

**Only passing results are cached.** A failed suite always re-runs so you can
see if your fix worked.

### Cache miss logging

When a cache miss occurs, the runner logs which input changed:

```
e2e: cache miss (HEAD changed: abc123 → def456)
scenarios: cache miss (dirty files: test/scenarios/generated/helpers.ts)
e2e: cache miss (no previous cache)
scenarios: cache miss (cache expired, 8d old)
scenarios: cache miss (version mismatch: got 0, expected 1)
```

This aids debugging when a suite unexpectedly re-runs.

### `--no-cache` flag

Extended from its current meaning (which only passes `-count=1` to Go):

- Deletes `.test-cache/` entirely before running
- Still passes `-count=1` to Go (bypasses Go's own per-package cache)

### `SuiteResult` type change

Add an optional `cached?: boolean` field so the UI can distinguish real runs from
cache hits.

## UI

### Progress display

```
 ✓ backend    134 passed  4.2s
 ✓ frontend   134 passed  3.1s
 ✓ e2e        cached (52 passed, 12s ago)
 ● scenarios  building... 8s
```

### Summary banner

```
All tests passed (2 cached, 2 ran)
```

Cached suites appear in the summary with their original test count and the time
since the cached run.

## Edge Cases

| Case                                    | Behavior                                                                        |
| --------------------------------------- | ------------------------------------------------------------------------------- |
| Corrupt cache JSON                      | Parse error → cache miss, delete file, run normally                             |
| Version mismatch                        | Cache miss, delete file, run normally                                           |
| Different branch                        | HEAD is part of key → auto-invalidates                                          |
| `git clean -xfd` removes `.test-cache/` | Fine — full re-run, cache rebuilds                                              |
| Parallel worktrees                      | Each worktree has its own `.test-cache/`, no conflicts                          |
| `--coverage` or `--race`                | Part of the cache key — flag combos don't produce false hits                    |
| `--quick` after full run                | `--quick` only runs backend+frontend (no Docker suites), so cache is irrelevant |
| Ctrl+C during cache write               | Atomic rename — no corrupt files                                                |
| Failed suite                            | Never cached — always re-runs                                                   |
| `--run PATTERN`                         | Cache disabled entirely                                                         |
| `--repeat > 1`                          | Cache disabled entirely                                                         |
| `--verbose`                             | Cache disabled entirely                                                         |
| `--coverage`                            | Cache disabled entirely                                                         |
| `--record-video`                        | Cache disabled entirely                                                         |
| `--force`                               | Cache disabled for Docker suites                                                |
| bench / microbench                      | Never cached                                                                    |

## Files Changed

| File                              | Change                                                      |
| --------------------------------- | ----------------------------------------------------------- |
| `tools/test-runner/src/cache.ts`  | New — cache key computation, load/save/expire, miss logging |
| `tools/test-runner/src/runner.ts` | Check cache before running e2e/scenarios, save after pass   |
| `tools/test-runner/src/types.ts`  | Add `cached?: boolean` to `SuiteResult`                     |
| `tools/test-runner/src/ui.ts`     | Render cached suites, update summary counts                 |
| `tools/test-runner/src/main.ts`   | Extend `--no-cache` to wipe `.test-cache/`                  |
| `.gitignore`                      | Add `.test-cache/`                                          |
