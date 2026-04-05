# Plan: Test Cache for Docker Suites

**Goal**: Skip e2e and scenario test suites when their inputs haven't changed since the last passing run, saving minutes of Docker build/run time.
**Architecture**: New `cache.ts` module in the test runner computes per-suite hash keys from git state + file content + flags. Cache files stored in `.test-cache/` (gitignored). Only e2e and scenarios are cached; backend/frontend rely on Go/vitest built-in caching.
**Tech Stack**: TypeScript (test runner), Node.js crypto (sha256), git CLI

---

## Step 1: Add `cached` field to `SuiteResult` type

**File**: `tools/test-runner/src/types.ts`

### 1a. Write implementation

Add `cached?: boolean` to the `SuiteResult` interface and `cachedTimestamp?: string` for UI display:

```typescript
// In SuiteResult interface, after the output field:
  cached?: boolean;
  cachedTimestamp?: string; // ISO timestamp of the cached run
```

### 1b. Verify

No test needed — type-only change. Verify with `npx --prefix tools/test-runner tsc --noEmit`.

---

## Step 2: Create `cache.ts` — cache key computation

**File**: `tools/test-runner/src/cache.ts` (new)

### 2a. Write implementation

Create the module with these exports:

```typescript
import { createHash } from 'node:crypto';
import {
  readFileSync,
  writeFileSync,
  mkdirSync,
  unlinkSync,
  renameSync,
  existsSync,
} from 'node:fs';
import { resolve, relative } from 'node:path';
import { exec, projectRoot } from './exec.js';
import type { SuiteName, Options, SuiteResult } from './types.js';

const CACHE_VERSION = 1;
const CACHE_DIR = '.test-cache';
const TTL_MS = 7 * 24 * 60 * 60 * 1000; // 7 days

interface CacheEntry {
  version: number;
  timestamp: string;
  cacheKey: string;
  status: 'passed';
  durationMs: number;
  passedTests: string[];
  skippedTests: string[];
  testCount: number;
}

// --- Public API ---

export function isCacheable(suite: SuiteName): boolean {
  return suite === 'e2e' || suite === 'scenarios';
}

export function isCacheDisabled(opts: Options): boolean {
  return (
    opts.noCache ||
    opts.runPattern !== null ||
    opts.repeat > 1 ||
    opts.verbose ||
    opts.coverage ||
    opts.recordVideo ||
    opts.force
  );
}

export async function computeCacheKey(suite: SuiteName, opts: Options): Promise<string> {
  // ... (implementation in step 2a body below)
}

export function checkCache(suite: SuiteName): {
  hit: boolean;
  result?: CacheEntry;
  missReason: string;
} {
  // ... (implementation below)
}

export function saveCache(suite: SuiteName, key: string, result: SuiteResult): void {
  // ... (atomic write-to-temp + rename)
}

export function deleteCache(suite: SuiteName): void {
  // ...
}

export function wipeCacheDir(): void {
  // Delete entire .test-cache/ directory
}
```

**`computeCacheKey` implementation details:**

1. Run `git rev-parse HEAD` and `git status --porcelain` (two exec calls, can run in parallel)
2. For **e2e**: hash HEAD + `go.mod` + `go.sum` + dirty `.go` files + `Dockerfile.e2e` + `Dockerfile.e2e-base` + `opts.race` + `opts.coverage`
3. For **scenarios**: hash everything from e2e + `assets/dashboard/package-lock.json` + dirty `.ts/.tsx/.css/.json` under `assets/dashboard/` + `Dockerfile.scenarios` + `Dockerfile.scenarios-base` + all `test/scenarios/**/*.ts` + `test/scenarios/generated/package.json` + `test/scenarios/generated/package-lock.json` + `test/scenarios/generated/tsconfig.json` + `test/scenarios/generated/entrypoint.sh`

Hash computation: sha256 of sorted list of `label:hash` strings.

**`checkCache` implementation details:**

1. Read `.test-cache/{suite}.json`
2. On parse error → return miss with reason "corrupt cache file"
3. On version mismatch → return miss with reason "version mismatch: got X, expected Y"
4. On expired (timestamp + TTL < now) → return miss with reason "cache expired, Xd old"
5. On key mismatch → return miss with reason "inputs changed"
6. Otherwise → return hit

**`saveCache` implementation details:**

1. `mkdirSync(.test-cache, { recursive: true })`
2. Write JSON to `.test-cache/{suite}.json.tmp`
3. `renameSync` to `.test-cache/{suite}.json`

### 2b. Verify

```bash
npx --prefix tools/test-runner tsc --noEmit
```

---

## Step 3: Integrate cache into runner

**File**: `tools/test-runner/src/runner.ts`

### 3a. Write implementation

Import cache functions and wrap suite execution:

```typescript
import {
  isCacheable,
  isCacheDisabled,
  computeCacheKey,
  checkCache,
  saveCache,
  deleteCache,
} from './cache.js';
```

In both `runSerial` and `runParallel`, before calling the suite runner, add cache logic:

```typescript
// For each suite:
if (isCacheable(suite) && !isCacheDisabled(opts)) {
  const key = await computeCacheKey(suite, opts);
  const { hit, result: cached, missReason } = checkCache(suite);

  if (hit && cached && cached.cacheKey === key) {
    // Cache hit — emit cached result
    const cachedResult: SuiteResult = {
      suite,
      status: 'passed',
      durationMs: cached.durationMs,
      passedTests: cached.passedTests,
      failedTests: [],
      skippedTests: cached.skippedTests,
      testDurations: {},
      output: '',
      cached: true,
      cachedTimestamp: cached.timestamp,
    };
    // display.finish(cachedResult) or display.onEvent with status
    return cachedResult; // skip runner
  }

  // Log miss reason
  console.log(`  ${suite}: cache miss (${missReason})`);
}

// Run suite normally...
// After completion:
if (isCacheable(suite) && !isCacheDisabled(opts)) {
  if (result.status === 'passed') {
    saveCache(suite, key, result);
  } else {
    deleteCache(suite);
  }
}
```

The key change is wrapping the existing suite execution in both `runSerial` and `runParallel` with this cache check/save logic. Extract to a helper function to avoid duplication.

### 3b. Verify

```bash
npx --prefix tools/test-runner tsc --noEmit
```

---

## Step 4: Extend `--no-cache` to wipe cache directory

**File**: `tools/test-runner/src/main.ts`

### 4a. Write implementation

In `main()`, after parsing args but before `runSuites`:

```typescript
import { wipeCacheDir } from './cache.js';

// In main(), before printHeader():
if (opts.noCache) {
  wipeCacheDir();
}
```

### 4b. Verify

```bash
npx --prefix tools/test-runner tsc --noEmit
```

---

## Step 5: Update UI for cached results

**File**: `tools/test-runner/src/ui.ts`

### 5a. Write implementation

**In `ParallelDisplay.finish()`**: When `result.cached` is true, set status to `'passed'` and show cached indicator in `lastActivity`:

```typescript
if (result.cached) {
  const ago = result.cachedTimestamp ? formatTimeAgo(new Date(result.cachedTimestamp)) : '';
  state.lastActivity = `cached (${state.passed} passed${ago ? `, ${ago}` : ''})`;
}
```

Add a `formatTimeAgo` helper that converts a timestamp to "12s ago", "3m ago", "2h ago", "1d ago".

**In `printSummary()`**: Show cached indicator per suite row and update the banner:

```typescript
// Per-row: if result.cached, show "cached" instead of icon
const icon = r.cached
  ? chalk.cyan('↺')
  : r.status === 'passed'
    ? chalk.green('✓')
    : /* ... existing logic */;

// In tests column for cached suites:
if (r.cached) {
  const ago = r.cachedTimestamp ? `, ${formatTimeAgo(new Date(r.cachedTimestamp))}` : '';
  tests = chalk.cyan(`cached (${r.passedTests.length} passed${ago})`);
}
```

**In `printFinalBanner()`**: Add cached count parameter:

```typescript
export function printFinalBanner(allPassed: boolean, hasBroken = false, cachedCount = 0): void {
  if (allPassed) {
    const suffix = cachedCount > 0
      ? ` (${cachedCount} cached, ${/* total - cached */} ran)`
      : '';
    console.log(chalk.green(` All tests passed!${suffix}`));
  }
  // ... rest unchanged
}
```

Update `main.ts` to pass `cachedCount` to `printFinalBanner`.

### 5b. Verify

```bash
npx --prefix tools/test-runner tsc --noEmit
```

---

## Step 6: Add `.test-cache/` to `.gitignore`

**File**: `.gitignore`

### 6a. Write implementation

Add under the "Build cache" section:

```
# Test result cache
.test-cache/
```

### 6b. Verify

```bash
git status  # .test-cache/ should not appear as untracked
```

---

## Step 7: End-to-end verification

### 7a. Type check

```bash
npx --prefix tools/test-runner tsc --noEmit
```

### 7b. Run quick tests to verify nothing is broken

```bash
./test.sh --quick
```

### 7c. Manual smoke test

Run `./test.sh --backend` twice. Second run should NOT be cached (backend is not a cacheable suite — it should run normally both times, relying on Go's built-in cache).

### 7d. Verify cache-disable flags

Check that the `isCacheDisabled` function correctly returns `true` for all bypass flags by reading the code and tracing the logic.

---

## Task Dependencies

| Group | Steps                                       | Can Parallelize                                           | Files Touched                   |
| ----- | ------------------------------------------- | --------------------------------------------------------- | ------------------------------- |
| 1     | Step 1 (types), Step 6 (gitignore)          | Yes (independent)                                         | `types.ts`, `.gitignore`        |
| 2     | Step 2 (cache.ts)                           | No (depends on types from Step 1)                         | `cache.ts` (new)                |
| 3     | Step 3 (runner), Step 4 (main), Step 5 (ui) | Yes (all import from cache.ts, independent of each other) | `runner.ts`, `main.ts`, `ui.ts` |
| 4     | Step 7 (verification)                       | No (depends on all)                                       | none                            |
