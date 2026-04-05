VERDICT: NEEDS_REVISION

## Summary Assessment

The v2 design correctly addresses all five critical issues from the first review. The scoping decision to cache only Docker-based suites (e2e and scenarios) is the right call, and the flags-that-disable-caching table is comprehensive. However, two remaining issues need resolution before implementation: the `--coverage` flag interaction creates a correctness gap, and the scenario cache key's `**/*.ts` glob misses non-TypeScript files that are copied into the Docker image and directly affect test execution.

## Critical Issues (must fix)

### 1. `--coverage` cached result silently breaks the dual coverage report

The design makes `opts.coverage` part of the cache key (correct), but does not address what happens when a `--coverage` run is served from cache. When e2e or scenarios actually run with `--coverage`, they produce side-effect artifacts:

- **E2E**: writes Go coverage data to `build/covdata-e2e/` (see `e2e.ts:101-105`)
- **Scenarios**: writes Go coverage data to `build/covdata-scenarios/` and frontend coverage to `test/scenarios/artifacts/fe-coverage/` (see `scenarios.ts:124-129` and `entrypoint.sh:50-53`)

These directories are consumed by `main.ts:220-247` to generate the dual coverage comparison report (`compareGoCoverage`, `compareFrontendCoverage`). If the suite is cached, the coverage directories will not be populated (or will contain stale data from a previous run). The `main.ts` code at line 217 checks `results.some(r => r.suite === 'e2e')` -- a cached e2e result would still match this check, causing it to attempt reading `build/covdata-e2e/` which may not exist or may be stale.

The fix: add `--coverage` to the "Flags That Disable Caching" table. Coverage runs explicitly request data artifacts. Alternatively, note in the design that a cached `--coverage` result must skip the dual coverage report, but this is fragile -- it is simpler and safer to just not cache coverage runs.

### 2. Scenario cache key `test/scenarios/**/*.ts` misses non-TS files that affect test behavior

The design expanded the glob from `*.spec.ts` to `**/*.ts` (good), but the Dockerfile.scenarios copies additional non-TypeScript files that directly influence test execution:

- `test/scenarios/generated/entrypoint.sh` -- orchestrates daemon startup, coverage collection, and Playwright invocation. A change here (e.g., adjusting daemon wait logic, adding environment variables, changing how artifacts are copied) directly affects whether tests pass or fail.
- `test/scenarios/generated/package.json` -- defines Playwright and dependency versions. A version bump here changes the test runtime.
- `test/scenarios/generated/tsconfig.json` -- controls TypeScript compilation settings for the test files.

All three are explicitly `COPY`'d in `Dockerfile.scenarios` (lines 32-34). The `package-lock.json` is already handled by the "Content hash of `assets/dashboard/package-lock.json`" entry, but the _scenario_ `package-lock.json` at `test/scenarios/generated/package-lock.json` is not -- this is the lock file for Playwright and test dependencies (installed by the base image via `npm ci`), which is a separate file from the dashboard's lock file.

The fix: either expand the glob to `test/scenarios/generated/**` (all files in the directory), or explicitly list `entrypoint.sh`, `package.json`, `tsconfig.json`, and `package-lock.json` as additional input hashes for the scenario cache key.

## Suggestions (nice to have)

### 1. E2E cache key does not include support scripts copied into the Docker image

The `Dockerfile.e2e` copies `test/mock-remote.sh`, `test/mock-clipboard-agent.sh`, and `scripts/e2e-entrypoint.sh` into the container (lines 22-36). These shell scripts are part of the test infrastructure -- `mock-remote.sh` simulates remote provisioning output that E2E tests assert against, and `mock-clipboard-agent.sh` simulates clipboard interaction. A change to either (e.g., altering the mock output format) could cause E2E test failures without touching any `.go` file.

However, these files change very rarely and any change would likely accompany Go file changes. The `git rev-parse HEAD` component would catch committed changes. The risk is limited to the case where someone modifies only these support scripts in a dirty working tree. Worth noting in the design as a known gap, even if not worth adding to the key.

### 2. Consider whether `--force` should also write the cache on success

Currently the design says `--force` disables caching entirely (neither read nor written). This means after `./test.sh --e2e --force` passes, a subsequent `./test.sh --e2e` will still need to run the tests (cache was not written during the forced run). This is slightly surprising -- the user rebuilt the base image and confirmed tests pass, but the next run doesn't benefit. Consider allowing `--force` to skip cache reads but still write on success. This is minor; the current behavior is safe and conservative.

### 3. The `cacheDisabled(opts)` pseudocode should be explicit about which flags apply per suite

The design has a single `cacheDisabled(opts)` function in the runner integration pseudocode, but `--record-video` only applies to scenarios (E2E tests don't produce video artifacts). Similarly, `--force` affects both Docker suites but not identically. Making the check per-suite in the pseudocode would prevent an implementer from accidentally disabling e2e caching when `--record-video` is passed even though it's irrelevant to e2e.

### 4. Cache file location and the pre-build step ordering deserve a note

In the current `runner.ts`, when suites run in parallel, build prerequisites (`buildLocalArtifacts`, `buildDashboard`) execute before suite runners (lines 80-93). The design should clarify that cache checks happen _before_ these build steps. If e2e is cached, the runner should skip both the build prerequisites and the suite runner -- otherwise the user still pays the cross-compilation cost for a cached suite, significantly reducing the benefit of caching.

### 5. Atomic rename on Node.js requires same-filesystem guarantee

The design correctly specifies write-to-temp-file + rename for atomicity. In Node.js, `fs.renameSync` fails across filesystem boundaries. Since both the temp file and `.test-cache/` are in the repo root, this will work in practice. But the implementation should ensure the temp file is created in `.test-cache/` (same directory as the target), not in `os.tmpdir()` which may be on a different mount.

## Verified Claims (things I confirmed are correct)

- **`--no-cache` currently only passes `-count=1` to Go backend**: Confirmed. `noCache` appears only in `backend.ts:52` where it adds `-count=1`. No other suite references `opts.noCache`. The design's plan to extend this flag is backward-compatible.

- **All four Dockerfiles exist as named**: `Dockerfile.e2e`, `Dockerfile.e2e-base`, `Dockerfile.scenarios`, `Dockerfile.scenarios-base` all exist at repo root.

- **`.test-cache/` is not yet in `.gitignore`**: Confirmed. The `.gitignore` does not contain `.test-cache/`. The design correctly lists adding it.

- **`SuiteResult` type can be extended with `cached?: boolean`**: Confirmed. The type at `types.ts:22-33` is a plain interface. Adding an optional field is non-breaking.

- **Scenario test directory contains non-spec TypeScript files**: Confirmed. `test/scenarios/generated/` contains `helpers.ts`, `helpers-terminal.ts`, `coverage-fixture.ts`, `global-setup.ts`, `playwright.config.ts` in addition to `*.spec.ts` files. The v2 design's expansion to `**/*.ts` correctly captures all of these.

- **bench and microbench suites are explicitly excluded**: Confirmed. The design states these are never cached because benchmarks measure performance.

- **`--run PATTERN` disables caching entirely**: Confirmed. This was the first review's critical issue #1 and is addressed in v2.

- **`--repeat > 1` disables caching entirely**: Confirmed. This was the first review's critical issue #4 and is addressed in v2.

- **`--verbose` and `--record-video` bypass the cache**: Confirmed. Both are in the flags-that-disable-caching table.

- **`--force` busts the cache for Docker suites**: Confirmed. This was the first review's suggestion #4 and is now in the flags table.

- **Cache version mismatch is handled**: Confirmed. The design specifies "delete the file and re-run the suite, no migration logic."

- **Cache miss logging is specified**: Confirmed. The design includes example log messages showing which input changed.

- **Parallel worktrees have separate `.test-cache/`**: Confirmed. Each worktree has its own repo root (verified by the `schmux-005` path prefix), so `.test-cache/` is naturally isolated.

- **E2E tests are Go tests compiled into a binary**: Confirmed. `shared.ts:67-69` compiles `./internal/e2e` with `-c -o build/e2e-test`. All Go source contributes to this binary, so `git rev-parse HEAD` + dirty `.go` files is a correct (if conservative) approximation.

- **Scenario entrypoint handles environment variables for coverage, grep, repeat, and video**: Confirmed by reading `entrypoint.sh`. The variables `GOCOVERDIR`, `TEST_GREP`, `TEST_REPEAT`, and `RECORD_VIDEO` are all checked.

- **`--quick` runs only backend + frontend**: Confirmed at `main.ts:123-124`. Docker suites never run under `--quick`, so the cache is irrelevant.
