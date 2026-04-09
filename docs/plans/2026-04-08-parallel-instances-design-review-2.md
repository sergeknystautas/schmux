VERDICT: NEEDS_REVISION

## Summary Assessment

The v2 design successfully addresses all five critical issues from round 1 and the `schmuxdir` package-level pattern is sound. However, the inventory is still incomplete -- three files with hardcoded `~/.schmux` paths are missing, and one listed file is a false positive. Two of the missing files use `os.Getenv("HOME")` instead of `os.UserHomeDir()`, which is a different code pattern that an implementer searching only for `os.UserHomeDir` would miss.

## Critical Issues (must fix)

### 1. Three files missing from the inventory

The inventory table claims completeness but omits three production files that construct `~/.schmux` paths:

**a) `internal/dashboard/websocket.go` (lines 792, 834)** -- Uses `os.Getenv("HOME")` (not `os.UserHomeDir()`) to build diagnostic output directories:

```go
diagDir := filepath.Join(os.Getenv("HOME"), ".schmux", "diagnostics", ...)
ioDiagDir := filepath.Join(os.Getenv("HOME"), ".schmux", "diagnostics", ...)
```

Without this change, terminal diagnostics from instance 2 would be written to instance 1's `~/.schmux/diagnostics/` directory.

**b) `internal/dashboard/server.go` (line 515)** -- Uses `os.Getenv("HOME")` for a log-message path comparison:

```go
if strings.HasPrefix(path, filepath.Join(os.Getenv("HOME"), ".schmux")) {
```

This is cosmetic (only affects a log line), but it would log "serving from cached assets" incorrectly when the assets are in a non-default schmux dir. More importantly, an implementer doing a codebase sweep for `os.UserHomeDir` would miss this because it uses `os.Getenv("HOME")`.

**c) `internal/workspace/ensure/manager.go` (lines 508-512)** -- `SignalingInstructionsFilePath()` uses `os.UserHomeDir()` + `.schmux`:

```go
func SignalingInstructionsFilePath() string {
    homeDir, err := os.UserHomeDir()
    if err != nil {
        return filepath.Join(".schmux", "signaling.md")
    }
    return filepath.Join(homeDir, ".schmux", "signaling.md")
}
```

Without this change, both instances would write to the same `~/.schmux/signaling.md` file. Since this file contains signaling instructions injected into agent system prompts, the content would be functionally identical between instances, so the cross-talk impact is low. But it violates the design principle that all `~/.schmux` paths go through `schmuxdir.Get()`, and a future change to per-instance signaling would silently fail.

**Recommendation:** Add all three to the inventory table. For the `os.Getenv("HOME")` pattern, add a note in the implementation order warning implementers to search for both `os.UserHomeDir()` and `os.Getenv("HOME")` when sweeping for `.schmux` references.

### 2. `cmd/schmux/spawn.go` is a false positive in the inventory

The inventory lists `cmd/schmux/spawn.go` with "schmux dir reference." A codebase search confirms that `spawn.go` contains zero `.schmux` string references. Its `os.UserHomeDir()` call (line 134) is solely for tilde expansion in user-provided workspace paths (`~/my-project` -> `/Users/x/my-project`), which is unrelated to the schmux config directory. This should be removed from the inventory to avoid wasted implementation effort and to maintain the inventory's credibility as a complete and accurate audit.

## Suggestions (nice to have)

### 1. Explicitly call out the `os.Getenv("HOME")` search pattern

The design's "Complete Inventory" section should note that implementers must search for BOTH `os.UserHomeDir()` and `os.Getenv("HOME")` combined with `.schmux`. The two files that use `os.Getenv("HOME")` (`websocket.go`, `server.go`) prove this is an easy pattern to miss in an audit. A one-line note like "Search for both `os.UserHomeDir` and `os.Getenv("HOME")` when verifying completeness" would prevent regression.

### 2. Clarify the `cleanEnv()` description

The design says: "the fix is: `cleanEnv()` must preserve `SCHMUX_HOME` in the environment it passes to the child process." This implies `cleanEnv()` currently strips it. In fact, `cleanEnv()` only strips `npm_*`, `INIT_CWD`, `NODE`, and two `SCHMUX_PRISTINE_*` vars -- it already preserves `SCHMUX_HOME`. The actual propagation path is: shell env -> `dev.sh` -> `exec npx` (inherits env) -> dev-runner `process.env` -> `cleanEnv()` (preserves non-npm vars) -> daemon process. This works today with no code changes needed. The design should clarify that no `cleanEnv()` modification is required, only verification that the existing passthrough chain works.

### 3. Consider caching the `os.UserHomeDir()` result in `schmuxdir.Get()`

When `dir` is empty (the common case for single-instance usage), `Get()` calls `os.UserHomeDir()` on every invocation. While `os.UserHomeDir()` is cheap (reads `$HOME`), caching the default in `Get()` via a `sync.Once` would eliminate repeated lookups. This is purely a polish concern.

## Verified Claims (things I confirmed are correct)

- **All 5 critical issues from round 1 are addressed.** The design now covers secrets isolation (`secretsPath()`), `Stop()`, CLI subcommand plumbing via `schmuxdir.Get()`, dev mode restart propagation, and the expanded inventory.

- **The `schmuxdir` package-level pattern is sound.** The `SetLogger()` pattern is established in 8+ packages (`config`, `lore`, `detect`, `workspace/ensure`, `update`, `tunnel`, `compound`, `dashboardsx`). Using the same pattern for `schmuxdir.Set()` / `schmuxdir.Get()` is consistent and idiomatic for this codebase. The single-writer (main.go at startup) / many-reader pattern has no concurrency concern.

- **`daemon.go` coverage is comprehensive.** The design's "Run-time paths" catch-all for `daemon.go` covers all 19 `.schmux` references in that file: `ValidateReadyToRun` (line 130), `Start` (line 169), `Stop` (line 237), `Status` (lines 286-287, 313, 319), and `Run` (lines 370, 523, 531, 1087, 1099, 1120, 1127, 1131, 1206, 1392, 1417).

- **`config.go` tilde-expansion calls are correctly excluded.** Lines 1674 and 1817 in `config.go` call `os.UserHomeDir()` but only for expanding `~` in user-provided paths (`WorkspacePath`, `WorktreeBasePath`). They have no `.schmux` join and do not need changes.

- **`handlers_tls.go` and `handlers_config.go` are correctly excluded.** Both have `os.UserHomeDir()` calls but only for tilde expansion in user-provided paths, not for `.schmux` directory access.

- **`internal/detect/agents.go` is correctly excluded.** It stores `os.UserHomeDir` as a function variable for testability but never joins the result with `.schmux`.

- **`cleanEnv()` already preserves `SCHMUX_HOME`.** Verified: it only strips `npm_*`, `INIT_CWD`, `NODE`, `SCHMUX_PRISTINE_PATH`, and `SCHMUX_PRISTINE_NPM_VARS`. All other env vars pass through unchanged.

- **The PID safety net works as described.** `ValidateReadyToRun()` reads the PID file from the schmux dir, so two invocations pointing at the same directory will correctly detect the conflict.

- **Non-changes are correctly identified.** `state.Load()`, `config.Load()`, `workspace.New()`, `session.New()`, and `models.New()` all receive paths or config objects rather than constructing `~/.schmux` paths themselves.

- **`dev.sh` propagates env correctly.** `exec npx` inherits the shell environment, so `SCHMUX_HOME` set in the user's shell will reach the dev-runner process without any shell script changes.
