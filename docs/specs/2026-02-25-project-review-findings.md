# Project Review Findings

**Date:** 2026-02-25
**Scope:** Full codebase review (Go backend, React frontend, docs, DX)
**Codebase:** 88K Go lines (281 files), 33K TypeScript lines (148 files), 1,020 commits

---

## Backend

### B1. Decompose `daemon.Run()` (~900 lines)

**File:** `internal/daemon/daemon.go`
**Severity:** Medium
**Type:** Maintainability

The `Run()` method handles config loading, state loading, manager creation, callback wiring, background goroutines, floor manager lifecycle, lore system, overlay compounding, git watcher, subreddit generation, PR discovery, signal handling, and shutdown -- all in one function.

The inline closures are particularly dense:

- `propagator` closure (lines 698-784)
- Lore curator callback (lines 896-1033, ~140 lines)

**Action:** Extract initialization into named phases (e.g., `initManagers()`, `wireCallbacks()`, `startBackgroundTasks()`). Extract inline closures to named methods.

---

### B2. Fix race conditions in session tracker

**File:** `internal/session/tracker.go`
**Severity:** High
**Type:** Correctness

`LastTerminalCols` and `LastTerminalRows` are written without synchronization. These are diagnostic-only fields, but they will fail `go test -race`.

**Action:** Use `atomic.Int32` for `LastTerminalCols`/`LastTerminalRows`, or protect with the existing mutex.

---

### B3. Fix race condition in floor manager lifecycle

**File:** `internal/daemon/daemon.go`
**Severity:** Medium
**Type:** Correctness

The `startFloorManager`/`stopFloorManager` closures (lines 579-627) capture `fm` and `fmInjector` by closure and mutate them without synchronization. If `OnFloorManagerToggle` is called concurrently from the config update handler, these closures race.

**Action:** Add a mutex guarding the floor manager start/stop closures, or serialize config update callbacks.

---

### B4. Prune unbounded maps in workspace manager

**File:** `internal/workspace/manager.go`
**Severity:** Low
**Type:** Memory leak

`repoLocks`, `workspaceGates`, and `lockedWorkspaces` maps grow as workspaces are created but are never pruned when workspaces are disposed. Long-running daemons accumulate dead entries.

**Action:** Delete map entries in `Dispose()` when a workspace is removed.

---

### B5. Deduplicate `Spawn()` / `SpawnCommand()` workspace resolution

**File:** `internal/session/manager.go`
**Severity:** Low
**Type:** Duplication

`Spawn()` (lines 576-725) and `SpawnCommand()` (lines 729-822) share ~50 lines of identical workspace resolution logic (lines 584-603 vs 733-752).

**Action:** Extract shared workspace resolution into a private `resolveWorkspace()` helper.

---

### B6. Add WebSocket handler tests

**File:** `internal/dashboard/websocket.go`
**Severity:** Medium
**Type:** Test coverage

`websocket.go` is ~1,300 lines handling broadcast, connection lifecycle, message routing, and terminal I/O proxying. There's a benchmark test (`websocket_bench_test.go`) but no functional tests.

**Action:** Add tests for connection registration/deregistration, broadcast delivery, terminal message routing, and displacement logic.

---

### B7. Propagate context in workspace `Dispose()`

**File:** `internal/workspace/manager.go`
**Severity:** Low
**Type:** Correctness

`Dispose()` (line 930) creates `context.Background()` instead of accepting a context parameter. This prevents cancellation during daemon shutdown.

**Action:** Accept a `context.Context` parameter in `Dispose()`.

---

### B8. Use `DaemonClient` for `end-shift` command

**File:** `cmd/schmux/main.go`
**Severity:** Low
**Type:** Consistency

The `end-shift` command (lines 184-196) uses raw `http.Client` instead of `cli.NewDaemonClient`, which every other command uses.

**Action:** Refactor to use `cli.NewDaemonClient` for consistency.

---

### B9. Handle `os.WriteFile` errors for debug artifacts

**File:** `internal/daemon/daemon.go`
**Severity:** Low
**Type:** Error handling

Several `os.WriteFile` calls (lines 962, 987, 1001, 1004) for debug artifacts ignore errors silently.

**Action:** Log errors at debug/warn level instead of ignoring them.

---

### B10. Fix `handleApp` error message referencing npm

**File:** `internal/dashboard/server.go`
**Severity:** Low
**Type:** Consistency

`handleApp` (line 82) references `npm install` and `npm run build` in its error message. The project standard is `go run ./cmd/build-dashboard`.

**Action:** Update the error message to reference `go run ./cmd/build-dashboard`.

---

## Frontend

### F1. Decompose `AppShell.tsx` (1,023 lines)

**File:** `assets/dashboard/src/components/AppShell.tsx`
**Severity:** Medium
**Type:** Maintainability

Contains the entire sidebar navigation, workspace listing, session listing, dev mode panel, keyboard shortcuts, and reconnect modals. Violates the documented principle that layout components "don't contain business logic."

**Action:** Extract into `Sidebar.tsx`, `WorkspaceList.tsx`, `NavSessionItem.tsx`, `DevModePanel.tsx`.

---

### F2. Decompose `SessionDetailPage.tsx` (1,083 lines)

**File:** `assets/dashboard/src/routes/SessionDetailPage.tsx`
**Severity:** Medium
**Type:** Maintainability

Contains terminal management, diagnostic capture, reconnect logic, selection mode, and the entire sidebar metadata display.

**Action:** Extract into `SessionTerminal.tsx` and `SessionSidebar.tsx`.

---

### F3. Split `SessionsContext` (18+ state fields, 30-entry useMemo)

**File:** `assets/dashboard/src/contexts/SessionsContext.tsx`
**Severity:** Medium
**Type:** Performance / Maintainability

Manages workspaces, sessions, sync conflicts, workspace locks, sync results, overlay events, remote access status, curator events, monitor events, pending navigation, and sound notifications. The `useMemo` dependency array has 30 entries. Any change re-renders all consumers.

**Action:** Split into focused contexts: `WorkspaceContext`, `NotificationContext`, `NavigationContext`.

---

### F4. Add context and WebSocket hook tests

**Files:** `assets/dashboard/src/contexts/*.tsx`, `assets/dashboard/src/hooks/useSessionsWebSocket.ts`
**Severity:** Medium
**Type:** Test coverage

All 5 context providers have zero dedicated tests. `useSessionsWebSocket` (the most complex hook with reconnection logic, message parsing, and state management) is also untested.

**Action:** Add tests for `SessionsContext`, `ConfigContext`, and `useSessionsWebSocket` at minimum. These are the highest-risk untested code in the frontend.

---

### F5. Fix `FileDiff` type duplication

**Files:** `assets/dashboard/src/lib/types.ts` (line 213), `assets/dashboard/src/lib/types.generated.ts` (line 112)
**Severity:** Low
**Type:** Maintenance risk

`FileDiff` is defined in both files. The manual definition shadows the generated one, but they could drift.

**Action:** Remove the manual `FileDiff` from `types.ts` and re-export from `types.generated.ts` if needed.

---

### F6. Centralize inline SVG icons

**Files:** `AppShell.tsx`, `SessionDetailPage.tsx`, `HomePage.tsx`
**Severity:** Low
**Type:** Duplication

SVG icons are copy-pasted across multiple files. `Icons.tsx` exists but only exports 2 icons.

**Action:** Move all inline SVG icon components into `Icons.tsx` and import from there.

---

### F7. Fix `useLocalStorage` stale closure risk

**File:** `assets/dashboard/src/hooks/useLocalStorage.ts` (line 47)
**Severity:** Low
**Type:** Correctness

`setValue` includes `storedValue` in its dependency array, causing it to be recreated on every state change. When `value` is a function, there's a stale closure risk.

**Action:** Use the callback form of `setStoredValue`:

```ts
setStoredValue((prev) => (value instanceof Function ? value(prev) : value));
```

---

### F8. Remove `(msg as any)` casts in terminal stream

**File:** `assets/dashboard/src/lib/terminalStream.ts` (lines 669, 672)
**Severity:** Low
**Type:** Type safety

Two `as any` casts bypass TypeScript checking.

**Action:** Add proper type narrowing or define a discriminated union for the message types.

---

### F9. Replace regex route matching with React Router

**File:** `assets/dashboard/src/components/AppShell.tsx` (lines 263-268)
**Severity:** Low
**Type:** Fragility

Manual regex extracts workspace IDs from `location.pathname` instead of using React Router's `useMatch` or route parameters. This duplicates route knowledge.

**Action:** Use `useMatch('/diff/:workspaceId')` or similar.

---

### F10. Reduce inline styles in `AppShell.tsx`

**File:** `assets/dashboard/src/components/AppShell.tsx` (lines 762-774, 815-825, 845-848)
**Severity:** Low
**Type:** Consistency

Hardcoded `style={{ marginRight: '6px', fontSize: '0.65rem', padding: '1px 6px' }}` bypasses the design token system.

**Action:** Move to CSS classes or CSS module, using existing design tokens where applicable.

---

## Documentation / DX

### D1. Prune or mark completed design specs

**Directory:** `docs/specs/`
**Severity:** Low
**Type:** Documentation

50+ design specs include plans for features that have shipped. There's no indication of which specs are historical vs. aspirational.

**Action:** Add a `Status: Implemented` / `Status: Active` / `Status: Superseded` header to each spec, or move completed specs to a `docs/specs/archive/` directory.

---

### D2. Add coverage enforcement to CI

**Files:** `.github/workflows/unit.yml`, `tools/test-runner/`
**Severity:** Low
**Type:** Process

Coverage reporting infrastructure exists (`./test.sh --coverage`) but is not gate-enforced in CI. Coverage can silently regress.

**Action:** Add a minimum coverage threshold check to the unit test CI workflow, at least for critical packages (`session`, `workspace`, `dashboard`, `daemon`).

---

### D3. Consider adding ESLint

**Directory:** `assets/dashboard/`
**Severity:** Low
**Type:** Quality

TypeScript strict mode and Prettier handle formatting and type errors, but neither catches logic errors or enforces React-specific best practices (exhaustive deps, rules of hooks beyond what the compiler checks).

**Action:** Add `eslint` with `@typescript-eslint` and `eslint-plugin-react-hooks` for the `react-hooks/exhaustive-deps` rule at minimum.
