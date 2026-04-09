VERDICT: NEEDS_REVISION

## Summary Assessment

The plan has the right overall architecture and covers most of the inventory, but Step 4 (daemon.go) contains a critical factual error that would cause 10+ hardcoded paths inside `Run()` to be missed, and the dependency groups have file conflicts that would break parallel execution.

## Critical Issues (must fix)

### 1. Step 4 claims `Run()` paths derive from `schmuxDir` -- they do not

The plan states: "All other `schmuxDir` uses within `Run()` are already derived from this local variable, so they'll work automatically." This is **false**. Within `daemon.go`'s `Run()` method, at least 10 references use `filepath.Join(homeDir, ".schmux", ...)` directly, bypassing the `schmuxDir` variable:

- Line 378: `ensure.EnsureGlobalHookScripts(homeDir)` (passes `homeDir` to another function)
- Line 523: `filepath.Join(homeDir, ".schmux", "instructions")`
- Line 680: `floormanager.New(cfg, sm, tmuxServer, homeDir, fmLog)` (passes `homeDir`)
- Line 1087: `filepath.Join(homeDir, ".schmux", "emergence")`
- Line 1099: `filepath.Join(homeDir, ".schmux", "actions")`
- Line 1120: `filepath.Join(homeDir, ".schmux", "lore-proposals")`
- Line 1127: `filepath.Join(homeDir, ".schmux", "instructions")` (second occurrence)
- Line 1131: `filepath.Join(homeDir, ".schmux", "lore-pending-merges")`
- Line 1206: `filepath.Join(homeDir, ".schmux", "lore-curator-runs", ...)`
- Line 1392: `filepath.Join(homeDir, ".schmux", "subreddit")`
- Line 1417: `filepath.Join(homeDir, ".schmux", "subreddit.json")`

Simply changing line 370 (`schmuxDir := filepath.Join(homeDir, ".schmux")`) to `schmuxDir := schmuxdir.Get()` will NOT fix these. Each must be individually migrated to use `schmuxdir.Get()` (or the local `schmuxDir` variable). The plan must enumerate every reference in `daemon.go` and either convert it to use the `schmuxDir` local variable or `schmuxdir.Get()` directly.

This is the most impactful issue: without fixing it, ~10 subsystem directories (emergence, lore, subreddit, floor-manager, instructions) would still write to `~/.schmux/` regardless of `--config-dir`, defeating the isolation goal.

### 2. File conflicts between dependency groups

The plan puts Steps 3, 4, 5 in Group 3 (parallel) and Steps 6-12 in Group 4 (parallel). But multiple Group 4 steps require changes to `daemon.go`, which is assigned to Step 4 (Group 3):

- **Step 8** (floormanager): If `floormanager.New()` drops its `homeDir` parameter, the call on `daemon.go` line 680 must be updated. But `daemon.go` belongs to Step 4 (Group 3, already completed).
- **Step 9** (detect): If `detect.EnsureGlobalHookScripts()` drops its `homeDir` parameter, the delegation wrapper in `ensure/manager.go` must change (Step 7), and the call from `daemon.go` line 378 must also change.
- **Step 7** (workspace/ensure): Same cascade -- the `ensure.EnsureGlobalHookScripts()` wrapper call in `daemon.go` must be updated.

These cross-group dependencies mean `daemon.go` is touched by Steps 4, 7, 8, and 9, but Steps 7-9 are supposed to run in parallel in Group 4 after Step 4 is "done." The fix is either: (a) absorb ALL `daemon.go` changes into Step 4, including signature updates for downstream functions, or (b) keep the `homeDir` parameters but ignore them internally (use `schmuxdir.Get()` inside the functions and leave the signatures alone for now).

### 3. Step 4 is not "bite-sized" (2-5 minutes)

With 17 `filepath.Join(homeDir, ".schmux", ...)` occurrences in `daemon.go`, plus signature cascading from Steps 7-9, Step 4 is significantly larger than 2-5 minutes. Even just replacing the direct paths (without signature changes) involves modifying 17 lines across a 1400+ line file, verifying each replacement, and handling the `homeDir` variable lifecycle (some lines may still need `homeDir` for non-.schmux purposes).

### 4. Step 13 integration test is trivially weak

The "integration test" in Step 13 only verifies that `schmuxdir.Get()` returns the custom dir -- it does not test that any downstream functions (config loading, secrets, lore, emergence, etc.) actually use the custom dir. The plan says "verify no cross-talk" but the test does nothing to check cross-talk. At minimum, the test should call functions like `config.ConfigExists()`, `secretsPath()`, or `LoreStateDir()` with a custom schmuxdir and verify the returned paths use the custom dir, not `~/.schmux/`.

## Suggestions (nice to have)

### 1. Consider keeping function signatures unchanged

Instead of removing `homeDir` parameters from `floormanager.New()`, `EnsureGlobalHookScripts()`, etc., consider keeping the signatures but making the functions ignore the parameter and use `schmuxdir.Get()` internally. This avoids cascading signature changes across package boundaries and reduces the chance of merge conflicts. The parameters can be deprecated and removed in a follow-up.

### 2. dev-runner cleanEnv() does not need a code change

The plan (Step 8a, final paragraph of design doc v2) suggests that `cleanEnv()` in `tools/dev-runner/src/lib/cleanEnv.ts` "must preserve `SCHMUX_HOME`." It already does -- `cleanEnv()` only strips `npm_*` vars, `INIT_CWD`, `NODE`, and `SCHMUX_PRISTINE_*` vars. `SCHMUX_HOME` passes through unchanged. The plan should explicitly state that this is a **verified no-op** rather than listing it as a task. The file path in the design is also wrong (`src/App.tsx` vs the actual `src/lib/cleanEnv.ts`).

### 3. Step 12 is redundant

Step 12 says "Verify that `daemon.Start()` properly propagates `SCHMUX_HOME` to the child process (done in Step 4). Add usage text for the `--config-dir` flag to `printUsage()`." The verification part is already done in Step 4. The `printUsage()` update is already described in Step 2. This step can be removed or merged.

### 4. Step 14 grep commands are wrong syntax

Step 14a uses `grep -rn` commands, but CLAUDE.md says to use the `Grep` tool, not shell grep. More importantly, the grep pattern `'\.schmux' --include='*.go' | grep -v '_test.go'` is fine for manual use but the step should clarify: remaining hits in `internal/e2e/e2e.go` are test infrastructure and acceptable, `internal/state/state.go` references are per-workspace `.schmux` directories (not the config dir), and `internal/workspace/config.go` references are workspace-level `.schmux` dirs (also not the config dir).

### 5. Missing TDD cycle for Steps 3-12

The CLAUDE.md says each task should have a "failing test -> implement -> verify" TDD cycle. Steps 3-12 all have "Run tests" substeps, but they only run existing tests. There are no new tests that would fail before the migration and pass after. If a migration is missed, existing tests would still pass (they typically use temp dirs set up independently). Consider adding at least one test per package that calls a path-returning function with `schmuxdir.Set("/custom")` and asserts the result starts with `/custom`.

## Verified Claims (things you confirmed are correct)

1. **File paths exist.** All source files referenced in the plan exist in the codebase at the specified paths: `internal/config/config.go`, `internal/config/secrets.go`, `internal/daemon/daemon.go`, `internal/dashboard/handlers_dev.go`, `internal/dashboard/handlers_subreddit.go`, `internal/dashboard/handlers_timelapse.go`, `internal/dashboard/handlers_usermodels.go`, `internal/dashboard/handlers_lore.go`, `internal/dashboard/websocket.go`, `internal/dashboard/server.go`, `internal/workspace/overlay.go`, `internal/workspace/ensure/manager.go`, `internal/lore/scratchpad.go`, `internal/logging/logging.go`, `internal/floormanager/manager.go`, `internal/assets/download.go`, `internal/detect/adapter_claude_hooks.go`, `internal/dashboardsx/paths.go`, `internal/oneshot/oneshot.go`, `cmd/schmux/timelapse.go`, `cmd/schmux/auth_github.go`, `cmd/schmux/dashboardsx.go`, `pkg/cli/daemon_client.go`, `cmd/schmux/main.go`, `cmd/schmux/main_test.go` (exists).

2. **Module path is correct.** The import path `github.com/sergeknystautas/schmux/internal/schmuxdir` matches `go.mod` module declaration.

3. **`internal/schmuxdir/` does not yet exist.** Confirmed -- this is a new package as stated.

4. **`SetLogger()` pattern exists.** Confirmed in 10+ packages (`internal/config/`, `internal/lore/`, `internal/detect/`, `internal/dashboardsx/`, `internal/compound/`, `internal/update/`, `internal/tunnel/`, `internal/workspace/ensure/`). The `schmuxdir.Set()` / `schmuxdir.Get()` pattern is consistent with existing codebase conventions.

5. **Test commands are correct.** `go test ./internal/schmuxdir/...`, `go test ./internal/config/...`, etc. are valid Go test invocations. `./test.sh --quick` and `./test.sh` match CLAUDE.md requirements.

6. **`cleanEnv()` already preserves `SCHMUX_HOME`.** The function in `tools/dev-runner/src/lib/cleanEnv.ts` only strips `npm_*`, `INIT_CWD`, `NODE`, and `SCHMUX_PRISTINE_*` vars. `SCHMUX_HOME` passes through untouched.

7. **Files using `os.UserHomeDir()` for non-.schmux purposes are correctly excluded.** `cmd/schmux/spawn.go`, `internal/dashboard/handlers_config.go`, `internal/dashboard/handlers_tls.go`, `internal/dashboard/handlers_environment.go` all use `os.UserHomeDir()` / `os.Getenv("HOME")` for `~` expansion, not for `.schmux` paths.

8. **`internal/state/state.go` and `internal/workspace/config.go` references are per-workspace `.schmux` dirs, not the config dir.** The plan correctly does not include them in the migration scope.

9. **Go version (1.22+/1.24) supports all constructs used.** No compatibility issues.

10. **`cmd.Env` is nil in current `Start()`.** Setting it to `append(os.Environ(), "SCHMUX_HOME="+d)` is correct -- this preserves all inherited env vars plus adds the new one.
