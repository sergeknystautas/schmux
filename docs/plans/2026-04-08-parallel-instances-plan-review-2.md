VERDICT: NEEDS_REVISION

## Summary Assessment

The v2 plan addresses all four critical issues from round 1 and is substantially improved. However, two new issues emerged: the integration test will not compile due to a function signature mismatch, and all Step 4c line numbers are systematically wrong (off by 11), which could lead to missed or misapplied edits.

## Critical Issues (must fix)

### 1. Integration test (Step 12) will not compile

The test code calls:

```go
loreDir := lore.LoreStateDir("test-repo")
```

But the actual function signature (verified at `internal/lore/scratchpad.go` line 651) is:

```go
func LoreStateDir(repoName string) (string, error)
```

This is a two-return-value function. The test must be:

```go
loreDir, err := lore.LoreStateDir("test-repo")
if err != nil {
    t.Fatalf("LoreStateDir() returned error: %v", err)
}
```

Additionally, the `ConfigExists()` assertion is too weak. Line 626-628 uses `t.Log` instead of `t.Errorf`, so the test passes even if `ConfigExists()` returns true (meaning the migration was never applied and it still checks `~/.schmux`). Change:

```go
if exists {
    t.Log("config.ConfigExists() returned true -- expected false for empty temp dir")
}
```

To:

```go
if exists {
    t.Errorf("config.ConfigExists() returned true for empty temp dir -- likely still checking ~/.schmux instead of custom dir")
}
```

### 2. Step 4c line numbers are all wrong (off by 11)

Every line number in the Step 4c table and the corresponding summary table rows 13-20 are off by exactly 11 lines. The actual locations are:

| Plan claims | Actual line | Content               |
| ----------- | ----------- | --------------------- |
| 1087        | 1076        | `emergenceBaseDir`    |
| 1099        | 1088        | `actionBaseDir`       |
| 1120        | 1109        | `loreProposalDir`     |
| 1127        | 1116        | `loreInstructionsDir` |
| 1131        | 1120        | `lorePendingMergeDir` |
| 1206        | 1195        | `lore-curator-runs`   |
| 1392        | 1381        | `subredditDir`        |
| 1417        | 1406        | `oldCachePath`        |

The plan header (line 14) says "Step 4c covers lines 1087-1417" -- the actual range is 1076-1406. An implementer using the plan's line numbers to navigate would land in the wrong places and could miss references or apply edits to the wrong lines. The 8 references in Step 4c are real and correctly enumerated by content, but every line number must be corrected.

Row 12 in the summary table (`floormanager.New`) is also off by 1 (plan says 680, actual is 681).

## Suggestions (nice to have)

### 1. Group 3 parallelization is ambiguous for Steps 4a/4b/4c

The dependency group table says Group 3 (Steps 3, 4a, 4b, 4c, 5) has "Can Parallelize: Yes." Steps 3 and 5 touch different files, so they can run in parallel. But Steps 4a, 4b, and 4c all modify `internal/daemon/daemon.go`. If an agent attempts to run all three sub-steps in parallel, they will conflict on the same file. The table should either mark 4a/4b/4c as sequential within the group, or note that the "Yes" applies to Steps 3 and 5 relative to the 4x sub-steps, while 4a/4b/4c are sequential among themselves.

### 2. `SCHMUX_HOME` propagation could produce duplicate env vars

Step 4a proposes:

```go
if d := schmuxdir.Get(); d != "" {
    cmd.Env = append(os.Environ(), "SCHMUX_HOME="+d)
}
```

If the parent process already has `SCHMUX_HOME` in its environment (e.g., user ran `SCHMUX_HOME=foo schmux start`), `os.Environ()` already contains `SCHMUX_HOME=foo`, and `append` adds a second `SCHMUX_HOME=<d>`. While Go's `exec.Cmd` on Unix typically uses the last occurrence, this is an implementation detail. A cleaner approach would filter out any existing `SCHMUX_HOME` before appending, or use a helper that replaces rather than appends. Not critical since it works in practice, but worth noting.

### 3. Plan header says "18" but summary table has 20

The "Changes from previous version" section (line 10) says "all 18 `homeDir` + `.schmux` references enumerated" but the summary table at line 417 says "Total: 20 references." The 20 count is correct (18 `filepath.Join` calls + 2 pass-through calls to `ensure.EnsureGlobalHookScripts` and `floormanager.New`). The header should say 20, not 18.

### 4. `createDevConfigBackup` comment at line 1852 mentions `~/.schmux/backups/`

The plan notes this at line 417 as "only a comment" and does not change it. The Go comment at line 1852 says "in the ~/.schmux/backups/ directory." After migration, this comment becomes misleading because the actual path uses `schmuxDir` (the parameter). Consider updating the comment to say "in the schmuxDir/backups/ directory" or similar. This is cosmetic but avoids confusion.

## Verified Claims (things I confirmed are correct)

1. **Round 1 Critical Issue #1 (daemon.go references) -- ADDRESSED.** The v1 plan falsely claimed `Run()` paths "derive from the `schmuxDir` local variable." The v2 plan correctly enumerates all 18 `filepath.Join` calls and 2 pass-through calls across 4 functions. The content descriptions match actual code. Every reference in daemon.go is accounted for.

2. **Round 1 Critical Issue #2 (file conflicts between groups) -- ADDRESSED.** The v2 plan keeps function signatures unchanged for `floormanager.New()`, `ensure.EnsureGlobalHookScripts()`, and `detect.EnsureGlobalHookScripts()`. Verified: Steps 7, 8, and 9 (Group 4) modify the internals of these functions but do NOT touch `daemon.go`. The call sites on daemon.go lines 378 and 681 remain `ensure.EnsureGlobalHookScripts(homeDir)` and `floormanager.New(cfg, sm, tmuxServer, homeDir, fmLog)` unchanged. Groups 3 and 4 have zero file conflicts.

3. **Round 1 Critical Issue #3 (Step 4 too large) -- ADDRESSED.** Step 4 is split into 4a (package-level functions, 7 references), 4b (Run first half, 5 references), and 4c (Run second half, 8 references). Each sub-step is a manageable size.

4. **Round 1 Critical Issue #4 (weak integration test) -- PARTIALLY ADDRESSED.** The v2 test calls real downstream functions (`config.ConfigExists()`, `lore.LoreStateDir()`) rather than just testing `schmuxdir.Get()`. However, the test has the compilation error and weak assertion noted above.

5. **`homeDir` variable lifecycle is correct.** After migration, `homeDir` is still needed in `Run()` for the two pass-through call sites (lines 378 and 681). The `os.UserHomeDir()` call at line 365 must be kept. The plan correctly states this.

6. **`cleanEnv()` is a verified no-op.** The function at `tools/dev-runner/src/lib/cleanEnv.ts` only strips `npm_*`, `INIT_CWD`, `NODE`, and `SCHMUX_PRISTINE_*` vars. `SCHMUX_HOME` passes through untouched. Verified by reading the full source.

7. **`createDevConfigBackup()` is a verified no-op.** The function at line 1854 accepts `schmuxDir` as a parameter and uses it correctly. No hardcoded `.schmux` path in the function body.

8. **Excluded files are correctly excluded.** `internal/e2e/e2e.go` uses its own `e.HomeDir` for test isolation. `internal/state/state.go` and `internal/workspace/config.go` reference per-workspace `.schmux` directories (e.g., `filepath.Join(workspacePath, ".schmux")`), not the global config dir.

9. **Module path is correct.** `go.mod` declares `module github.com/sergeknystautas/schmux`. The `pkg/cli` package can import `internal/schmuxdir` because they are in the same module -- Go's `internal/` visibility restriction only blocks external modules.

10. **`cmd.Env` is nil in current `Start()`.** Verified by reading lines 207-213. No existing `cmd.Env` assignment exists. Setting it to `append(os.Environ(), ...)` preserves all inherited environment variables.

11. **Step 4a line numbers for package-level functions are correct.** Lines 125-130 (`ValidateReadyToRun`), 164-169 (`Start`), 232-237 (`Stop`), 281-319 (`Status`) all match actual code exactly.

12. **Step 4b line numbers are correct within 1.** Lines 365-370 (schmuxDir declaration), 378 (EnsureGlobalHookScripts), 523 (instructions), 530-531 (recordings) all match. Line 680 (floormanager.New) is actually 681, off by 1.
