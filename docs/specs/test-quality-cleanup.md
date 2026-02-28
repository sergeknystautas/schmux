# Test Quality Cleanup Spec

This document captures all actionable findings from a deep quality analysis of the test suite. Each item includes the exact file, line numbers, what's wrong, and what to do about it.

---

## Phase 1: Remove False Confidence (High Priority)

Tests that pad coverage metrics but would never catch a real regression. These actively harm the suite by creating an illusion of coverage.

### 1.1 Delete or implement `internal/update/update_test.go`

**File**: `internal/update/update_test.go` (lines 7-115)

**Problem**: Both tests (`TestCheckForUpdate` at line 7, `TestSemverComparisonLogic` at line 74) are unconditionally `t.Skip()`'d. They have never executed. The skip messages say "requires HTTP client mocking or refactoring" and "requires refactoring Update() to use injected semver comparison."

**Action**: Either:

- (a) Refactor `CheckForUpdate()` and `Update()` in `internal/update/update.go` to accept interfaces for HTTP client and semver comparison, then implement the tests properly.
- (b) Delete the file entirely. A skipped test is worse than no test because it inflates the test count.

### 1.2 Fix or remove `TestContextCancellation` in tmux

**File**: `internal/tmux/tmux_test.go` (lines 172-272)

**Problem**: Contains 8 subtests (CreateSession, ListSessions, GetPanePID, CaptureOutput, KillSession, SendKeys, HasSession, CaptureLastLines). Every single subtest uses `t.Log()` instead of `t.Error()` when context cancellation is not observed. The comment on line 170 says "tight race" but the result is these tests can literally never fail.

**Action**: Either:

- (a) Use `context.WithTimeout` with a very short deadline (e.g., 1 nanosecond) and assert that the returned error wraps `context.DeadlineExceeded` or `context.Canceled`. This makes cancellation deterministic.
- (b) Remove the entire `TestContextCancellation` function (~100 lines). If context cancellation can't be tested reliably, don't pretend to test it.

### 1.3 Deduplicate telemetry test

**File**: `internal/telemetry/telemetry_test.go` (lines 19-103)

**Problem**: `TestNewSendsEvents` (lines 19-60) and `TestGeneratesInstallIDWhenEmpty` (lines 62-103) are byte-for-byte identical in their implementation. Both create an httptest server, override `posthogEndpoint`, create a client with `New("", nil)`, track "test_event", call `Shutdown()`, and assert `received.count == 1` and `received.id != ""`. The second test claims to test install ID generation when empty but its body is a copy-paste of the first.

**Action**: Either:

- (a) Delete `TestGeneratesInstallIDWhenEmpty` entirely (since `TestNewSendsEvents` already covers the same path).
- (b) Make `TestGeneratesInstallIDWhenEmpty` actually test something different: pass a non-empty install ID to `New("my-existing-id", nil)` and assert the captured payload uses `"my-existing-id"` instead of generating a new one.

### 1.4 Remove zero-assertion `TestNoopTelemetry`

**File**: `internal/telemetry/telemetry_test.go` (lines 12-17)

**Problem**: Zero assertions. The test calls `Track()` and `Shutdown()` on a `NoopTelemetry` and the only expectation is "should not panic" (per the comment). This is a nil-safety smoke test with no verification.

**Action**: Either:

- (a) Delete it — every other test in the file exercises `New()` which implicitly proves the interface works.
- (b) Add a compile-time interface assertion (`var _ Tracker = (*NoopTelemetry)(nil)`) to the production code instead, which is more idiomatic Go and doesn't require a test.

### 1.5 Fix mock-return tests in checker_test.go

**File**: `internal/tmux/checker_test.go` (lines 34-70)

**Problem**: `TestChecker_MissingTmux` (line 34) and `TestChecker_TmuxNoOutput` (line 54) both replace the global `TmuxChecker` with a `mockChecker` that returns a hardcoded error, then assert the error message matches what they just hardcoded. This tests zero application logic — it verifies "if I create a mock returning X, calling it returns X." The file itself acknowledges this is an anti-pattern (comments on lines 28-33).

**Action**: Either:

- (a) Delete both tests. The existing `TestDefaultChecker_Success` (line 16) already tests the real `defaultChecker` with a skip for missing tmux.
- (b) Refactor the code that _calls_ `TmuxChecker.Check()` (likely in `internal/daemon/`) to be testable, and write tests there that verify the daemon's behavior when the checker returns an error. That's where the real logic lives.

### 1.6 Fix dead-code assertion in run_git_test.go

**File**: `internal/workspace/run_git_test.go` (lines 44-62) — `TestRunGit_CapturesExitCode`

**Problem**: Lines 57-61 contain a conditional branch that does nothing:

```go
if len(snap.AllCommands) > 0 && snap.AllCommands[0].ExitCode == 0 {
    // Running git log in a temp dir that's not a repo should fail
    // But if it somehow succeeds, that's OK too
}
```

The test claims to verify exit code capture but silently accepts any exit code.

**Action**: Assert the expected exit code explicitly. Running `git log` in a non-repo temp dir should produce a non-zero exit code. Assert `snap.AllCommands[0].ExitCode != 0` (or the specific exit code `128`).

### 1.7 Delete skipped placeholder `TestStatus`

**File**: `internal/daemon/daemon_test.go` (lines 17-21)

**Problem**: `TestStatus` is unconditionally skipped: `t.Skip("requires running daemon")`. This is a placeholder.

**Action**: Delete it. The daemon lifecycle is already tested in E2E tests (`TestE2EDaemonLifecycle`).

---

## Phase 2: Remove Low-Value Tests (Medium Priority)

Tests that technically execute but test trivial things (assignment operators, constants, stdlib behavior).

### 2.1 Trivial constructor test

**File**: `internal/session/manager_test.go` (lines 19-43) — `TestNew`

**Problem**: Tests that `New()` returns non-nil and that three fields (`config`, `state`, `workspace`) equal the values passed in. Every other test in the file also calls `New()`, so this provides no incremental coverage.

**Action**: Delete `TestNew`. The 15+ other tests in this file exercise the constructor.

### 2.2 Constant test

**File**: `pkg/cli/daemon_client_test.go` (lines 12-17) — `TestGetDefaultURL`

**Problem**: Tests that `GetDefaultURL()` returns `"http://127.0.0.1:7337"`. This is testing a hardcoded constant.

**Action**: Delete it.

### 2.3 Assignment-operator test

**File**: `pkg/cli/daemon_client_test.go` (lines 19-34) — `TestNewDaemonClient`

**Problem**: Tests that `NewDaemonClient()` sets `BaseURL`, `HTTPClient`, and `Timeout` to the values passed in.

**Action**: Delete it. The other tests in this file (`TestDaemonClient_*`) exercise the client constructor implicitly.

### 2.4 Stdlib atomic test

**File**: `internal/session/tracker_test.go` (lines 24-39) — `TestTrackerCounters_Increment`

**Problem**: Tests that `atomic.Int64.Add(5)` followed by `.Load()` returns 5. This tests Go's standard library, not application logic.

**Action**: Delete it.

### 2.5 Coverage-padding frontend tests

**File**: `assets/dashboard/src/components/TypingPerformance.test.tsx` (entire file, ~24 lines)

**Problem**: 2 trivial tests that render the component and check one text string exists each. No interaction, no data flow, no error states. The component likely has real complexity (histogram rendering, stats calculation) that goes entirely untested.

**Action**: Either:

- (a) Delete the file — these tests catch nothing.
- (b) Replace with meaningful tests: simulate performance data arriving, verify histogram rendering, test how stats update over time, test error states.

**File**: `assets/dashboard/src/lib/utils.test.ts` (lines 150-178, `formatTimestamp` section)

**Problem**: 3 of 4 tests only assert `typeof result === 'string'` and `result.length > 0`. Any non-empty string passes.

**Action**: Assert the actual formatted output against expected strings for known timestamps (e.g., `new Date('2024-01-15T10:30:00Z')` should produce a specific format).

**File**: `assets/dashboard/src/lib/utils.test.ts` (lines 321-337, `nudgeStateEmoji`)

**Problem**: Tests only check `expect(nudgeStateEmoji['Needs Input']).toBeDefined()`. Changing the emoji value would still pass.

**Action**: Assert the actual emoji values: `expect(nudgeStateEmoji['Needs Input']).toBe('expected-emoji')`.

**File**: `assets/dashboard/src/hooks/useTerminalStream.test.tsx` (lines 41 vs 45)

**Problem**: "does not create stream when sessionId is null" and "...is undefined" are functionally identical tests.

**Action**: Delete one. Or combine into a parameterized `it.each([null, undefined])`.

---

## Phase 3: Extract Shared Test Helpers (Medium Priority)

Duplicated setup code that makes tests harder to maintain and slower to write.

### 3.1 Go: Extract `newTestManager(t)` for session manager tests

**File**: `internal/session/manager_test.go`

**Problem**: The following 5-line boilerplate is repeated ~15 times across the file:

```go
cfg := &config.Config{WorkspacePath: "/tmp/workspaces", ...}
st := state.New("", nil)
statePath := t.TempDir() + "/state.json"
wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
m := New(cfg, st, statePath, wm, nil)
```

**Action**: Add a helper at the top of the file:

```go
func newTestManager(t *testing.T) (*Manager, *config.Config, *state.State) {
    t.Helper()
    cfg := &config.Config{WorkspacePath: "/tmp/workspaces", Repos: []config.Repo{{URL: "test", Path: "/tmp/repo"}}}
    st := state.New("", nil)
    statePath := filepath.Join(t.TempDir(), "state.json")
    wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
    m := New(cfg, st, statePath, wm, nil)
    return m, cfg, st
}
```

Then replace all 15 occurrences. This saves ~60 lines and makes each test focus on what it's actually testing.

### 3.2 Go: Extract `setupPreviewTest(t)` for preview manager tests

**File**: `internal/preview/manager_test.go`

**Problem**: 6 test functions repeat 8 lines of setup (state creation, workspace creation, httptest server, port parsing, manager creation).

**Action**: Add a helper that returns `(m *Manager, st *state.State, ws state.Workspace, upstreamURL string, cleanup func())`.

### 3.3 Go: Extract `createTestRepo(t)` for E2E tests

**File**: `internal/e2e/e2e_test.go`, `e2e_api_test.go`, `e2e_api_coverage_test.go`

**Problem**: Every E2E test function has 10-15 lines of identical git repo setup:

```go
repoDir := filepath.Join(tmpDir, "repo")
os.MkdirAll(repoDir, 0o755)
// git init, git config user.name, git config user.email
// write a file, git add, git commit
// env.AddRepoToConfig(...)
```

The scenario test helpers already have a `createTestRepo()` function, but the Go E2E tests don't.

**Action**: Add to `internal/e2e/e2e.go` (the test harness):

```go
func (e *E2E) CreateTestRepo(t *testing.T, name string) string {
    t.Helper()
    // ... consolidate the git init + commit boilerplate
    // ... call e.AddRepoToConfig()
    // return repoDir
}
```

### 3.4 Go: Replace custom `contains`/`equalSlices` with stdlib

**File**: `internal/oneshot/oneshot_test.go` (lines 451-474)

**Problem**: Contains a complex, bug-prone reimplementation of `strings.Contains` (the `contains` function with `containsMiddle` sub-helper) and a reimplementation of `slices.Equal`.

**Action**: Replace with `strings.Contains()` and `slices.Equal()` from Go standard library. Delete the 24 lines of custom helpers.

**File**: `internal/events/remotewatcher_test.go` (lines 24-35)

**Problem**: Custom `containsStr` and `searchStr` functions — another manual reimplementation of `strings.Contains` using byte-by-byte comparison.

**Action**: Replace with `strings.Contains()`. Delete the 12 lines of custom helpers.

### 3.5 Go: Deduplicate `nudgenikManifest` struct

**Files**:

- `internal/tmux/tmux_test.go` (~lines 340-377)
- `internal/nudgenik/integration_test.go`

**Problem**: Both files define an identical `nudgenikManifest` struct and `loadNudgenikManifest` helper function.

**Action**: Either:

- (a) Export the struct from the `nudgenik` package and import it in the tmux test.
- (b) Create a shared `internal/nudgenik/testdata/manifest.go` with exported types.

### 3.6 Frontend: Create shared `test-utils.tsx`

**Problem**: Each route test file independently creates its own `renderWithProviders` / `renderTab` / `renderPage` wrapper:

- `routes/config/WorkspacesTab.test.tsx:27` — `renderTab(overrides)`
- `routes/config/ConfigPage.test.tsx:106` — `renderConfigPage()`
- `routes/SpawnPage.agent-select.test.tsx:155` — `renderSpawnPage()`
- `routes/PersonasListPage.test.tsx:55` — `renderListPage()`
- `contexts/ConfigContext.test.tsx:16` — `wrapper()`
- `contexts/SessionsContext.test.tsx:53` — `makeWrapper()`

**Action**: Create `assets/dashboard/src/test-utils.tsx` with:

```tsx
import { render } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

export function renderWithRouter(ui: React.ReactElement, { route = '/' } = {}) {
  return render(<MemoryRouter initialEntries={[route]}>{ui}</MemoryRouter>);
}

// Add provider wrappers as needed
```

Then update test files to import from `test-utils` instead of reinventing the wheel.

### 3.7 Frontend: Standardize error suppression in tests

**Problem**: Three different patterns across test files:

1. `ErrorBoundary.test.tsx` — saves/restores `console.error` and adds `window.addEventListener('error', ...)`
2. `ModalProvider.test.tsx` — same approach, duplicated
3. `ToastProvider.test.tsx` — `vi.spyOn(console, 'error').mockImplementation(() => {})`

**Action**: Add a `suppressConsoleErrors()` helper to `test-utils.tsx` (or `setupTests.ts`) and use it consistently:

```tsx
export function suppressConsoleErrors() {
  const original = console.error;
  beforeAll(() => {
    console.error = vi.fn();
  });
  afterAll(() => {
    console.error = original;
  });
}
```

### 3.8 Frontend: Deduplicate CSRF header tests

**File**: `assets/dashboard/src/lib/api-csrf.test.ts` (lines 30-73)

**Problem**: 4 tests (`remoteAccessOn`, `remoteAccessOff`, `setRemoteAccessPassword`, `testRemoteAccessNotification`) all repeat an identical CSRF header extraction block:

```ts
const token =
  init.headers instanceof Headers ? init.headers.get('X-CSRF-Token') : init.headers['X-CSRF-Token'];
expect(token).toBe('test-csrf-token');
```

**Action**: Use `it.each` or extract a helper:

```ts
function expectCSRFHeader(init: RequestInit) {
  const token =
    init.headers instanceof Headers
      ? init.headers.get('X-CSRF-Token')
      : init.headers['X-CSRF-Token'];
  expect(token).toBe('test-csrf-token');
}
```

---

## Phase 4: Add Missing High-Value Tests (Medium Priority)

Tests that would actually catch regressions but don't exist yet.

### 4.1 Frontend: WebSocket error handling

**File to modify**: `assets/dashboard/src/hooks/useSessionsWebSocket.test.ts`

**Missing tests**:

- Trigger `ws.onerror` and verify the hook handles it gracefully (reconnect behavior, error state)
- Send malformed JSON via `ws.onmessage` and verify it doesn't crash the app
- Send a non-string/non-JSON payload and verify graceful handling

### 4.2 Frontend: Config tab interaction tests

**Files to modify**: `SessionsTab.test.tsx`, `CodeReviewTab.test.tsx`, `AdvancedTab.test.tsx`, `TargetSelect.test.tsx`

**Problem**: These files have 5-6 render-only tests each but almost no tests that click, toggle, or type. A refactor that breaks an `onChange` handler would pass all existing tests.

**Missing tests for each file**:

- Click a checkbox/toggle and verify the `dispatch` callback is called with the correct action
- Type in a text input and verify the value updates
- For TargetSelect: select an option from the dropdown and verify selection

### 4.3 Frontend: passwordStrength edge cases

**File to modify**: `assets/dashboard/src/lib/passwordStrength.test.ts`

**Missing test cases** (add to the existing `it.each`):

- Empty string `""`
- Single character `"a"`
- Whitespace-only `"   "`
- Unicode: `"pässwörd123!"`
- Very long string (1000+ chars)

### 4.4 Frontend: ModalProvider concurrent modals

**File to modify**: `assets/dashboard/src/components/ModalProvider.test.tsx`

**Missing test**: Call `prompt()` while another modal is already open. Verify the behavior is defined (either queuing, replacing, or rejecting the second call).

---

## Phase 5: Test Infrastructure Improvements (Low-Medium Priority)

### 5.1 Replace hard `time.Sleep` calls in E2E with polling

**File**: `internal/e2e/e2e_test.go`

**Problem**: ~15 instances of `time.Sleep(600 * time.Millisecond)` for debounce waits and `time.Sleep(2 * time.Second)` for workspace setup. These are inherently fragile — if server timing changes or the machine is under load, tests become flaky.

**Action**: Where possible, replace with `PollUntil()` (which already exists in the E2E harness). For debounce waits, poll for the expected WebSocket message instead of sleeping for the debounce duration.

Specific locations:

- Lines 794, 816, 898, 942, 962, 984, 998, 1014, 1126: `time.Sleep(600ms)` — debounce waits
- `e2e_api_coverage_test.go` lines 52, 164, 304, 387: `time.Sleep(2s)` — workspace setup
- `e2e_api_test.go` line 251: `time.Sleep(200ms)` — filesystem sync

### 5.2 Scenario config contamination

**File**: `test/scenarios/helpers/global-setup.ts`

**Problem**: `global-setup.ts` disposes all sessions before the suite starts but does NOT reset config. Tests like `configure-remote-access-settings.spec.ts` mutate the config, which can affect subsequent tests in the shared-daemon model.

**Action**: Add a config reset step to `global-setup.ts` that restores a known-good baseline config before each spec file. Alternatively, each spec that mutates config should save/restore it in `beforeAll`/`afterAll`.

### 5.3 Update outdated E2E documentation

**File**: `docs/dev/e2e.md`

**Problem**: Still says "Phase 2: WebSocket coverage (deferred)" and "pipe-pane fails in Docker" — but WebSocket tests are already implemented and working.

**Action**: Update the doc to reflect the current state of E2E coverage.

---

## Appendix: Exemplary Tests (Do Not Touch)

These are the highest-quality tests in the suite, useful as reference patterns:

| File                                                                   | Why It's Good                                                                                  |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `internal/compound/merge_test.go` (515 lines)                          | Table-driven, edge cases (binary files, nil executor, empty response), well-factored helpers   |
| `internal/workspace/giturl_test.go` (199 lines)                        | Clean table-driven tests covering GitHub/GitLab/Bitbucket + edge cases                         |
| `internal/workspace/io_workspace_telemetry_test.go` (270 lines)        | Ring buffer overflow, reset behavior, workspace-level breakdowns                               |
| `internal/session/manager_test.go` `TestBuildCommand` (lines 347-681)  | `shouldContain`/`shouldNotContain` patterns covering many command construction scenarios       |
| `internal/remote/controlmode/parser_test.go` (229 lines)               | Parsing, escaping, high-throughput (10K commands), drop counters                               |
| `internal/oneshot/oneshot_test.go` (554 lines)                         | Comprehensive table-driven for command building, output parsing, streaming events              |
| `assets/dashboard/src/lib/gitGraphLayout.test.ts` (977 lines)          | 35 tests: empty graphs, single/multi-branch, merges, truncated, disconnected, 50-commit chains |
| `assets/dashboard/src/lib/previewKeepAlive.test.ts` (427 lines)        | LRU eviction, dynamic module imports for clean state isolation                                 |
| `assets/dashboard/src/routes/config/useConfigForm.test.ts` (655 lines) | Thorough reducer testing: all action types, derived values, change detection                   |
| `test/scenarios/generated/terminal-fidelity.spec.ts`                   | Compares tmux capture-pane with xterm.js buffer line-by-line with retry logic                  |
