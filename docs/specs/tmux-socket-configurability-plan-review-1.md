VERDICT: NEEDS_REVISION

## Summary Assessment

The plan is well-structured with clear dependency groups, but has several critical issues: a circular dependency between Steps 2 and 4, five missed call sites that will cause compilation failures after Step 12, an underspecified CLI attach approach, and missing coverage for the spec's mixed-socket banner UI component.

## Critical Issues (must fix)

### 1. Circular dependency between Step 2 and Step 4

Step 2 (Group 1) adds `TmuxSocket: m.server.SocketName()` to session struct literals in `session/manager.go`, but `TmuxSocket` is not added to `state.Session` until Step 4 (Group 2). The plan acknowledges this at the end of Step 2 ("This requires the `TmuxSocket` field on `state.Session` (Step 4). If landing this as a prerequisite before the full spec, add the field first.") but the dependency group table declares Steps 1-2 as Group 1 and Steps 3-4 as Group 2, with Group 2 depending on Group 1. This means Step 2 cannot compile without Step 4.

**Fix:** Move Step 4 into Group 1, or merge Steps 2 and 4 into a single step, or reorder so Step 4 comes before Step 2.

### 2. Five missed call sites that will break compilation at Step 12

The plan's Step 12 deletes all package-level tmux functions, but Steps 6-8 do not cover all callers. After deletion, these call sites will fail to compile:

- **`internal/dashboard/handlers_environment.go:158`** -- `tmux.ShowEnvironment(r.Context())` in the `else` branch
- **`internal/dashboard/handlers_environment.go:207`** -- `setEnvFn := tmux.SetEnvironment` as default assignment
- **`internal/dashboard/handlers_debug_tmux.go:56`** -- `tmux.Binary()` in the `else` branch of `collectTmuxSessionCount()`
- **`internal/session/localsource.go:206`** -- `tmux.Binary()` in the `else` branch of the control-mode attach command
- **`internal/floormanager/injector_test.go:164`** -- `tmux.CaptureOutput(ctx, sessName)`

The spec's Section 4 explicitly lists `handlers_environment.go:164` and `handlers_environment.go:212` as call sites requiring migration, and `handlers_debug_tmux.go:47` as well. The plan covers `handlers_debug_tmux.go` partially in Step 8's scope but never addresses it explicitly. None of these five files appear in the plan at all.

Additionally, `internal/session/tracker_bench_test.go` uses `tmux.CreateSession` and `tmux.KillSession` at lines 26, 40, 63, 77, 90, and 113. And `internal/floormanager/injector_test.go` uses `tmux.KillSession` at lines 111 and 113, and `tmux.CreateSession` at line 119. These test files will also fail to compile after Step 12.

**Fix:** Add a step (or expand Step 8 or Step 12) to migrate:

- `handlers_environment.go` dual-path to use `s.tmuxServer` directly (same as the existing non-nil branch)
- `handlers_debug_tmux.go` dual-path to use `s.tmuxServer` directly
- `localsource.go` dual-path to use `s.server` directly
- `injector_test.go` and `tracker_bench_test.go` to use `tmux.NewTmuxServer(...)` instances

### 3. Step 11 (CLI attach) is not actionable -- proposes multiple approaches without committing

Step 11a presents three different approaches for fixing `attach.go` without selecting one:

1. Parse `AttachCmd` string structurally
2. Use `sess.Binary`, `sess.Socket`, `sess.TmuxSession` fields from the API response
3. Use `sess.TmuxSocket` and `sess.TmuxSession` from the session response added in Step 13

Approach 3 depends on Step 13, which is in Group 7 (after Group 5 which includes Step 11). This creates a forward dependency. Furthermore, the CLI communicates with the daemon via HTTP API (not Go method calls), so `session.Manager.GetAttachArgs()` from Step 6h is not directly callable. The CLI would need either: (a) a new API endpoint, or (b) the fields added to the session response in Step 13.

**Fix:** Commit to one approach. The cleanest is: add `tmux_socket` and `tmux_session` fields to the session API response (Step 13a) and move that into Group 4 or earlier. Then Step 11 can use those fields. Alternatively, add a dedicated `/api/sessions/{id}/attach-args` endpoint.

### 4. Missing mixed-socket banner UI component (spec Section 8)

The spec explicitly requires a "Socket transition in progress" banner on the home page when sessions span multiple sockets (Section 8, "Mixed-socket banner"). The plan's Step 13 mentions adding `TmuxSocket` to the session response and the config form, but has no sub-step for implementing the mixed-socket banner component itself. This is a user-visible spec requirement with no corresponding task.

**Fix:** Add a sub-step to Step 13 (or a new step) for the React mixed-socket banner component, including the logic to detect multiple active sockets from the sessions list.

### 5. Step 9d is underspecified -- "Find where ConfigResponse is built"

Step 9d says "Find where `ConfigResponse` is built (in `handleGetConfig`) and add `resp.TmuxSocketName = cfg.GetTmuxSocketName()`". But the actual function is `handleConfigGet` (not `handleGetConfig`), and the response is constructed as a struct literal at `handlers_config.go:92`, not built incrementally with field assignments. The instruction should say: add the field to the struct literal after `TmuxBinary` at approximately line 152 of `handlers_config.go`.

**Fix:** Correct the function name to `handleConfigGet` and specify that the field should be added to the struct literal at `handlers_config.go:152` (after `TmuxBinary: s.config.TmuxBinary`), using `TmuxSocketName: cfg.GetTmuxSocketName()`.

## Suggestions (nice to have)

### 1. Step 7 may break floormanager tests that construct Manager with nil server

The `TestRunning_NonexistentTmux` test at `manager_test.go:326` constructs a bare `&Manager{}` (nil server) and calls `Running()`, which currently falls through to `tmux.SessionExists()`. After Step 7 removes the fallback, `m.server.SessionExists()` will nil-pointer panic. The test at line 333 has a comment confirming it relies on `tmux.SessionExists`.

Similarly, `TestRunning_NoSession` at line 318 constructs `&Manager{}` but that test returns early because `sess == ""`. So it is safe.

**Fix suggestion:** Step 7 should either keep a nil guard (`if m.server != nil { return m.server.SessionExists(...) }; return false`) or the test should construct a Manager with a non-nil server.

### 2. Step 10b config loading pattern could be simplified

Step 10b suggests loading the config twice (once for `TmuxBinary`, once for `TmuxSocketName`), then corrects itself with "Extend it". The actual code at `daemon.go:183` loads config once and checks `cfg.TmuxBinary`. The plan should just show the single extended `if` block, not both approaches.

### 3. Step 14a (E2E helpers) is vague about parameterization

The plan says "For robustness, parameterize" the 4 hardcoded `-L schmux` sites in `e2e.go`, but then just sets `socketName := "schmux"` as a constant. This is no-op refactoring. Either properly parameterize by reading from the daemon's config, or acknowledge that E2E tests always use the default and skip this step.

### 4. Step 3 config getter should acquire mutex

The plan's `GetTmuxSocketName()` implementation acquires `c.mu.RLock()`, following the `GetPort` pattern. This is correct but the test at Step 3a creates a raw `&Config{}` without initialization, which means the mutex is zero-valued. This works (zero-value `sync.RWMutex` is valid) but should be noted.

### 5. Step 6h `GetAttachArgs` has limited utility

The `GetAttachArgs` method added in Step 6h is a Go-level method on `session.Manager`. Since the CLI communicates via HTTP, this method is only usable server-side. It would need to be exposed as an API endpoint to be useful for `attach.go`. The plan doesn't wire this to any HTTP handler.

### 6. Step 10c multi-socket startup may re-start the config socket

The code in Step 10c iterates `activeSocketSet` and has a `continue` for the config socket, but only after calling `StartServer` for it. The `continue` check correctly skips the config socket since it's already started above. However, the log structure is slightly misleading -- the nil logger passed to non-config socket servers means errors are logged via `logger.Warn` at a different level than the config socket's server.

### 7. Plan does not address the `ValidateReadyToRun` hardcoded socket

At `daemon.go:120`, `ValidateReadyToRun()` hardcodes `tmux.NewTmuxServer("tmux", "schmux", nil)`. The spec (Section 11) notes this is harmless since `Check()` only validates the binary, but suggests updating for consistency. The plan does not include this cleanup.

### 8. `handlers_capture.go` uses `s.session.GetTracker()` which is socket-safe

I verified that `handlers_capture.go` uses the `SessionRuntime` path (via `GetTracker`), not direct tmux calls. No changes needed there. Noting for completeness since it interacts with sessions.

## Verified Claims (things you confirmed are correct)

1. **daemon.go:749 bug is real.** Confirmed: line 749 calls `tmux.SessionExists(timeoutCtx, sess.TmuxSession)` (package-level, no socket), while `tmuxServer` is constructed at line 423 with socket `"schmux"`. This is a genuine pre-existing bug.

2. **`TmuxServer` construction is correct.** `tmux.NewTmuxServer(binary, socketName, logger)` signature matches at `tmux.go:69`. `Binary()` and `SocketName()` accessors exist at lines 74 and 77.

3. **Line numbers for session struct literals are accurate.** Spawn at line 892 and SpawnCommand at line 993 match the actual code.

4. **Line numbers for session/manager.go call sites mostly accurate.** IsRunning (1246-1249), Dispose capture (1316-1319), Dispose exists/kill (1332-1342), GetAttachCommand (1445-1449), GetOutput (1459-1462), RenameSession (1501-1504), ensureTrackerFromSession (1629) all verified against actual code.

5. **ConfigResponse struct placement at contracts/config.go:179.** `TmuxBinary` field confirmed at line 179. The plan's placement after it is correct.

6. **ConfigUpdateRequest placement at contracts/config.go:320.** `TmuxBinary` field confirmed at line 320.

7. **handlers_config.go NeedsRestart check at line 756.** Confirmed the exact line and the existing condition string.

8. **daemon.go Start() config load at line 183.** Confirmed the pattern exists and can be extended.

9. **Floormanager dual-path sites confirmed.** Lines 114-117, 146, 207-211, 224-228, 265-271, 280-284, 424-427 all verified.

10. **`state.Session` struct has no `TmuxSocket` field yet.** Confirmed at `state.go:320-343` -- the field does not exist, so Step 4 correctly identifies this as new.

11. **`handleConfigGet` is the correct function name** (not `handleGetConfig` as the plan says in Step 9d), confirmed at `handlers_config.go:50`.

12. **Test commands match CLAUDE.md conventions.** `go test ./internal/...`, `go build ./cmd/schmux`, and `./test.sh` are all correct per the project conventions.

13. **`parseTmuxSession` at attach.go:73-119 will become dead code** after the attach command is restructured. Confirmed the function exists and is only used at line 49.

14. **E2E helpers at e2e.go:551, 560, 894, 1019** are confirmed to hardcode `-L schmux`.

15. **Scenario test helpers at helpers-terminal.ts** hardcode `-L schmux` at lines 41, 42, 61, 63, 195, 298, 358 (7 sites, not 5 as the plan says -- lines 41 and 42 are separate commands, and lines 61 and 63 are separate paths). The regex at line 24 already makes `-L schmux` optional.

16. **Step 11 does NOT reintroduce `exec.Command("sh", "-c", ...)`.** All three proposed approaches use structured `exec.Command` with separate arguments. The spec's warning about shell injection is respected.

17. **The spec's Section 10 (upgrade paths) is addressed.** Step 2 handles the from-isolation-branch path by persisting `TmuxSocket` before the configurability work lands. Step 10d handles the from-main path by resolving empty `TmuxSocket` as `"default"`. However, the plan does not address the alternative approach mentioned in the spec (migration step in `state.Load()`) -- it relies entirely on the "persist early" approach via Step 2, which is the spec's preferred option.

18. **`handlers_sync.go:415` correctly uses `s.tmuxServer`.** This is for conflict resolution sessions which are infrastructure-level (always on config socket). No change needed, matching the spec's analysis.
