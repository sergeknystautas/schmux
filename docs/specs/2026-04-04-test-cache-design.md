# Test Cache: Skip Unchanged Suites

**Date:** 2026-04-04
**Status:** Approved

## Problem

`./test.sh --all` runs all four suites (backend, frontend, e2e, scenarios) every
time, even when nothing has changed. Docker-based suites are especially expensive
— building images and spinning up containers takes minutes even when the code is
identical to the last run.

## Solution

Add a suite-level caching layer to the test runner. Before running a suite,
compute a hash of its inputs. If the hash matches a previous passing run, skip
the suite and report the cached result.

## Cache Granularity

**Hybrid approach** — per-suite for Docker tests, per-package for local tests:

- **Backend/Frontend**: cached at the suite level by the test runner. Go's
  built-in test cache also handles per-package caching underneath, so even on a
  cache miss the Go runner may skip unchanged packages.
- **E2E/Scenarios**: cached at the suite level. These are all-or-nothing because
  Docker container startup dominates the cost.

## Cache Key Computation

A new module `tools/test-runner/src/cache.ts` computes keys per suite. Each key
is `sha256(sorted list of component hashes)`.

### Input sets per suite

**Backend:**

- `git rev-parse HEAD`
- Content hash of `go.mod` + `go.sum`
- Sorted hashes of dirty/untracked `.go` files (via `git status --porcelain`)
- `opts.race` and `opts.coverage` booleans

**Frontend:**

- `git rev-parse HEAD`
- Content hash of `assets/dashboard/package-lock.json`
- Sorted hashes of dirty/untracked `.ts`, `.tsx`, `.css`, `.json` files under
  `assets/dashboard/`
- `opts.race` and `opts.coverage` booleans

**E2E:**

- Everything from backend
- Content hash of `Dockerfile.e2e`, `Dockerfile.e2e-base`

**Scenarios:**

- Everything from backend + frontend
- Content hash of `Dockerfile.scenarios`, `Dockerfile.scenarios-base`
- Content hash of all `test/scenarios/**/*.spec.ts` files (even clean ones —
  not covered by backend or frontend keys)

### Invalidation triggers

- Changing any source file invalidates the relevant suites
- `go.mod` / `go.sum` changes invalidate backend, e2e, and scenarios
- `package-lock.json` changes invalidate frontend and scenarios
- Switching branches changes HEAD, invalidating all suites
- Switching back to a clean branch re-validates (same HEAD, no dirty files)
- `--race` or `--coverage` flags are part of the key — a non-race cached result
  won't satisfy a `--race` run

### Performance

Git operations (`rev-parse`, `status --porcelain`, `hash-object`) add ~50-100ms
before test execution. Negligible compared to any suite's runtime.

## Cache Storage

A `.test-cache/` directory at the repo root (gitignored). One JSON file per suite:

```
.test-cache/
  backend.json
  frontend.json
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

### Expiry

- **Backend/Frontend**: no expiry. Invalidated only by content changes.
- **E2E/Scenarios**: 7-day TTL. Guards against stale Docker base images (OS
  package updates, etc.). After 7 days the suite re-runs even if the code hasn't
  changed.

### Atomicity

Cache writes use write-to-temp-file + rename. A Ctrl+C mid-run never leaves a
corrupt cache file.

## Runner Integration

Changes to `runner.ts` — before invoking a suite runner:

```
for each suite in opts.suites:
  key = computeCacheKey(suite)
  cached = loadCache(suite)

  if cached AND cached.key == key AND not expired AND not opts.noCache:
    emit cached result with stored pass/fail counts
    skip runner
  else:
    run suite normally
    on pass: saveCache(suite, key, result)
    on fail: deleteCache(suite)  // never cache failures
```

**Only passing results are cached.** A failed suite always re-runs so you can
see if your fix worked.

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
 ✓ backend    cached (847 passed, 12s ago)
 ✓ frontend   cached (134 passed, 12s ago)
 ● e2e        running...  23s
 ● scenarios  building... 8s
```

### Summary banner

```
All tests passed (2 cached, 2 ran)
```

Cached suites appear in the summary with their original test count and the time
since the cached run.

## Edge Cases

| Case                                    | Behavior                                                     |
| --------------------------------------- | ------------------------------------------------------------ |
| Corrupt cache JSON                      | Parse error → cache miss, delete file, run normally          |
| Different branch                        | HEAD is part of key → auto-invalidates                       |
| `git clean -xfd` removes `.test-cache/` | Fine — full re-run, cache rebuilds                           |
| Parallel worktrees                      | Each worktree has its own `.test-cache/`, no conflicts       |
| `--coverage` or `--race`                | Part of the cache key — flag combos don't produce false hits |
| Ctrl+C during cache write               | Atomic rename — no corrupt files                             |
| Failed suite                            | Never cached — always re-runs                                |

## Files Changed

| File                              | Change                                        |
| --------------------------------- | --------------------------------------------- |
| `tools/test-runner/src/cache.ts`  | New — cache key computation, load/save/expire |
| `tools/test-runner/src/runner.ts` | Check cache before running, save after pass   |
| `tools/test-runner/src/types.ts`  | Add `cached?: boolean` to `SuiteResult`       |
| `tools/test-runner/src/ui.ts`     | Render cached suites, update summary counts   |
| `tools/test-runner/src/main.ts`   | Extend `--no-cache` to wipe `.test-cache/`    |
| `.gitignore`                      | Add `.test-cache/`                            |
