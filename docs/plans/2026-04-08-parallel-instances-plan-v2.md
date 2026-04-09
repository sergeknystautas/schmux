# Plan: Configurable Schmux Directory for Parallel Instances (v2)

**Goal**: Allow multiple schmux instances on the same machine by making `~/.schmux/` configurable via `--config-dir` flag and `SCHMUX_HOME` env var.
**Architecture**: New `internal/schmuxdir/` package with `Set()`/`Get()` (package-level, matches existing `SetLogger()` pattern). Called once at startup in `main.go`, read everywhere via `schmuxdir.Get()`.
**Tech Stack**: Go 1.22+, standard library only.
**Design Doc**: `docs/plans/2026-04-08-parallel-instances-design-v2.md`

## Changes from previous version

1. **daemon.go: all 18 `homeDir` + `.schmux` references enumerated.** The v1 plan incorrectly claimed "all other `schmuxDir` uses within `Run()` are already derived from this local variable." In reality, `Run()` has 13 direct `filepath.Join(homeDir, ".schmux", ...)` calls that bypass the `schmuxDir` local variable (plus 2 calls that pass `homeDir` to downstream functions). Every reference is now listed by line number in Step 4.

2. **Function signatures kept unchanged to eliminate cross-group file conflicts.** The v1 plan had Steps 7, 8, and 9 (Group 4) cascading changes back into `daemon.go` (Group 3) when removing `homeDir` parameters from `floormanager.New()` and `EnsureGlobalHookScripts()`. This revision keeps those function signatures unchanged -- the functions ignore their `homeDir` param internally and use `schmuxdir.Get()` instead. This makes Groups 3 and 4 truly independent with zero file conflicts.

3. **Step 4 (daemon.go) split into three sub-steps.** The v1 plan had a single monolithic step for daemon.go covering 18 references across 4 functions and 1400+ lines. Now split into: (a) package-level functions `ValidateReadyToRun`, `Start`, `Stop`, `Status`; (b) `Run()` first half (before config load, lines 365-680); (c) `Run()` second half (after config load, lines 1087-1417).

4. **Integration test strengthened.** The v1 test only verified `schmuxdir.Get()` returns the custom dir -- trivially weak. The revised test calls actual downstream functions (`config.ConfigExists()`, `lore.LoreStateDir()`, `secretsPath()` equivalent) with a custom schmuxdir and verifies returned paths use the custom dir.

5. **Step 12 removed (redundant).** The v1 Step 12 ("verify `SCHMUX_HOME` propagation in `Start()` and update `printUsage()`") was already fully covered by Steps 2 and 4a.

6. **`cleanEnv()` marked as verified no-op.** The function in `tools/dev-runner/src/lib/cleanEnv.ts` only strips `npm_*`, `INIT_CWD`, `NODE`, and `SCHMUX_PRISTINE_*` vars. `SCHMUX_HOME` passes through unchanged. No code change needed.

7. **Final sweep notes expected acceptable hits.** `internal/e2e/e2e.go` (test infrastructure, uses its own `HomeDir` for isolation) and `internal/state/state.go` (per-workspace `.schmux` directories, not the config dir) are expected remaining references and do not need migration.

## Dependency Groups

| Group | Steps                    | Can Parallelize | Notes                                                    |
| ----- | ------------------------ | --------------- | -------------------------------------------------------- |
| 1     | Step 1                   | No              | Foundation -- `schmuxdir` package                        |
| 2     | Step 2                   | No              | CLI flag parsing in `main.go` (depends on Group 1)       |
| 3     | Steps 3, 4a, 4b, 4c, 5   | Yes             | Core package migrations (config, daemon, cli)            |
| 4     | Steps 6, 7, 8, 9, 10, 11 | Yes             | Leaf package migrations (no file conflicts with Group 3) |
| 5     | Step 12                  | No              | Integration test (depends on all above)                  |

**Key property of Groups 3 and 4**: No file is modified by more than one step. Group 4 steps do NOT touch `daemon.go` because function signatures are kept unchanged (the functions ignore their `homeDir` parameter internally).

---

## Step 1: Create `internal/schmuxdir/` package

**Files**: `internal/schmuxdir/schmuxdir.go`, `internal/schmuxdir/schmuxdir_test.go`

### 1a. Write the package

```go
// internal/schmuxdir/schmuxdir.go
package schmuxdir

import (
	"os"
	"path/filepath"
)

var dir string

// Set stores the resolved schmux directory. Called once at startup.
func Set(d string) { dir = d }

// Get returns the schmux directory. Falls back to ~/.schmux if unset.
func Get() string {
	if dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".schmux")
}

// Reset clears the stored directory (for testing only).
func Reset() { dir = "" }
```

### 1b. Write test

```go
// internal/schmuxdir/schmuxdir_test.go
package schmuxdir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetDefault(t *testing.T) {
	Reset()
	got := Get()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".schmux")
	if got != want {
		t.Errorf("Get() = %q, want %q", got, want)
	}
}

func TestSetOverrides(t *testing.T) {
	Reset()
	defer Reset()
	Set("/tmp/my-schmux")
	if got := Get(); got != "/tmp/my-schmux" {
		t.Errorf("Get() = %q, want /tmp/my-schmux", got)
	}
}

func TestResetClearsOverride(t *testing.T) {
	Set("/tmp/override")
	Reset()
	got := Get()
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".schmux")
	if got != want {
		t.Errorf("after Reset(), Get() = %q, want %q", got, want)
	}
}
```

### 1c. Run test

```bash
go test ./internal/schmuxdir/...
```

---

## Step 2: Add `--config-dir` flag parsing and `SCHMUX_HOME` to `main.go`

**File**: `cmd/schmux/main.go`

### 2a. Write implementation

Add a `resolveAndStripConfigDir()` function before `main()`, and call it at the top of `main()` before the command switch. This function:

1. Scans `os.Args` for `--config-dir <path>` or `--config-dir=<path>`
2. Strips the flag (and value) from `os.Args`
3. Falls back to `SCHMUX_HOME` env var
4. Falls back to `~/.schmux`
5. Converts to absolute path
6. Calls `schmuxdir.Set()`
7. Logs if non-default

Add import for `"github.com/sergeknystautas/schmux/internal/schmuxdir"` and `"path/filepath"`.

Call `resolveAndStripConfigDir()` as the first line of `main()`, before `if len(os.Args) < 2`.

```go
// resolveAndStripConfigDir parses --config-dir from os.Args, resolves the
// schmux directory, and stores it via schmuxdir.Set(). The flag is stripped
// from os.Args so subcommands never see it.
func resolveAndStripConfigDir() {
	var configDir string
	var newArgs []string

	args := os.Args
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--config-dir" && i+1 < len(args) {
			configDir = args[i+1]
			i++ // skip the value
			continue
		}
		if strings.HasPrefix(arg, "--config-dir=") {
			configDir = strings.TrimPrefix(arg, "--config-dir=")
			continue
		}
		newArgs = append(newArgs, arg)
	}
	os.Args = newArgs

	if configDir == "" {
		configDir = os.Getenv("SCHMUX_HOME")
	}

	if configDir == "" {
		return // use default ~/.schmux
	}

	// Expand ~ prefix
	if strings.HasPrefix(configDir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			configDir = filepath.Join(home, configDir[2:])
		}
	}

	// Convert to absolute path
	if abs, err := filepath.Abs(configDir); err == nil {
		configDir = abs
	}

	schmuxdir.Set(configDir)
	fmt.Fprintf(os.Stderr, "schmux: using config dir %s\n", configDir)
}
```

Also add `--config-dir` to `printUsage()`:

```
fmt.Println("Global Flags:")
fmt.Println("  --config-dir <path>  Use alternate config directory (default: ~/.schmux)")
fmt.Println("                       Can also set SCHMUX_HOME env var")
```

### 2b. Write test

**File**: `cmd/schmux/main_test.go` (new file or add to existing)

Test that `resolveAndStripConfigDir` correctly parses the flag, strips it from args, and handles the `=` form. Since it mutates global state (`os.Args`, `schmuxdir`), tests should save/restore.

### 2c. Run test

```bash
go test ./cmd/schmux/... -run TestResolveAndStripConfigDir
```

---

## Step 3: Migrate `internal/config/` (config.go + secrets.go)

**Files**: `internal/config/config.go`, `internal/config/secrets.go`

### 3a. config.go -- Replace hardcoded paths

In `ConfigExists()` (line ~1913): replace `filepath.Join(homeDir, ".schmux", "config.json")` with `filepath.Join(schmuxdir.Get(), "config.json")`. Remove the `os.UserHomeDir()` call.

In `EnsureExists()` (line ~1925): same -- use `schmuxdir.Get()` for the config path. Remove the `os.UserHomeDir()` call since `ConfigExists()` no longer needs it.

In `GetWorktreeBasePath()`: replace `filepath.Join(homeDir, ".schmux", "repos")` default with `filepath.Join(schmuxdir.Get(), "repos")`.

In `GetQueryRepoPath()`: replace `filepath.Join(homeDir, ".schmux", "query")` default with `filepath.Join(schmuxdir.Get(), "query")`.

Add import: `"github.com/sergeknystautas/schmux/internal/schmuxdir"`

### 3b. secrets.go -- Replace `secretsPath()`

Change `secretsPath()` (line ~34) from:

```go
func secretsPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".schmux", "secrets.json"), nil
}
```

To:

```go
func secretsPath() (string, error) {
	d := schmuxdir.Get()
	if d == "" {
		return "", fmt.Errorf("failed to resolve schmux directory")
	}
	return filepath.Join(d, "secrets.json"), nil
}
```

Add import: `"github.com/sergeknystautas/schmux/internal/schmuxdir"`
Remove unused `"os"` import if no other callers need it.

### 3c. Run tests

```bash
go test ./internal/config/...
```

---

## Step 4a: Migrate `internal/daemon/daemon.go` -- package-level functions

**File**: `internal/daemon/daemon.go`

Add import: `"github.com/sergeknystautas/schmux/internal/schmuxdir"`

This sub-step covers the four package-level functions. Each currently calls `os.UserHomeDir()` independently and constructs `.schmux` paths. Replace all with `schmuxdir.Get()`.

### Complete enumeration of references in package-level functions

**`ValidateReadyToRun()`** -- 1 reference:
| Line | Current code | Replacement |
|------|-------------|-------------|
| 125-130 | `homeDir, err := os.UserHomeDir()` ... `schmuxDir := filepath.Join(homeDir, ".schmux")` | `schmuxDir := schmuxdir.Get()` |

Remove the `os.UserHomeDir()` call and `homeDir` variable. Line 135 (`filepath.Join(schmuxDir, pidFileName)`) already uses the local `schmuxDir` -- no change needed there.

**`Start()`** -- 1 reference + env propagation:
| Line | Current code | Replacement |
|------|-------------|-------------|
| 164-169 | `homeDir, err := os.UserHomeDir()` ... `schmuxDir := filepath.Join(homeDir, ".schmux")` | `schmuxDir := schmuxdir.Get()` |

Remove the `os.UserHomeDir()` call. All subsequent uses in `Start()` (lines 175, 184) already reference the local `schmuxDir` variable.

Additionally, propagate `SCHMUX_HOME` to the child process. After `cmd := exec.Command(...)`, before `cmd.Start()`:

```go
if d := schmuxdir.Get(); d != "" {
	cmd.Env = append(os.Environ(), "SCHMUX_HOME="+d)
}
```

Note: `cmd.Env` is currently nil (inherits parent env). Setting it preserves all inherited vars plus adds `SCHMUX_HOME`.

**`Stop()`** -- 1 reference:
| Line | Current code | Replacement |
|------|-------------|-------------|
| 232-237 | `homeDir, err := os.UserHomeDir()` ... `pidFile := filepath.Join(homeDir, ".schmux", pidFileName)` | `pidFile := filepath.Join(schmuxdir.Get(), pidFileName)` |

Remove the `os.UserHomeDir()` call and `homeDir` variable.

**`Status()`** -- 4 references:
| Line | Current code | Replacement |
|------|-------------|-------------|
| 281 | `homeDir, err := os.UserHomeDir()` | Remove; use `d := schmuxdir.Get()` |
| 286 | `pidFile := filepath.Join(homeDir, ".schmux", pidFileName)` | `pidFile := filepath.Join(d, pidFileName)` |
| 287 | `startedFile := filepath.Join(homeDir, ".schmux", "daemon.started")` | `startedFile := filepath.Join(d, "daemon.started")` |
| 313 | `urlFile := filepath.Join(homeDir, ".schmux", "daemon.url")` | `urlFile := filepath.Join(d, "daemon.url")` |
| 319 | `config.Load(filepath.Join(homeDir, ".schmux", "config.json"))` | `config.Load(filepath.Join(d, "config.json"))` |

### Run tests

```bash
go test ./internal/daemon/...
```

---

## Step 4b: Migrate `internal/daemon/daemon.go` -- `Run()` first half (lines 365-680)

**File**: `internal/daemon/daemon.go`

This sub-step covers the `Run()` method from the `homeDir` declaration through the floor manager setup. The `homeDir` variable is declared at line 365 and used throughout `Run()`.

### Complete enumeration of references (Run, first half)

| Line    | Current code                                                                                    | Replacement                                                       |
| ------- | ----------------------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| 365-370 | `homeDir, err := os.UserHomeDir()` ... `schmuxDir := filepath.Join(homeDir, ".schmux")`         | `schmuxDir := schmuxdir.Get()`                                    |
| 378     | `ensure.EnsureGlobalHookScripts(homeDir)`                                                       | **No change.** Keep passing `homeDir` (see note below).           |
| 523     | `loreInstructionsDir := filepath.Join(homeDir, ".schmux", "instructions")`                      | `loreInstructionsDir := filepath.Join(schmuxDir, "instructions")` |
| 530-531 | `home, _ := os.UserHomeDir()` + `recordingsDir := filepath.Join(home, ".schmux", "recordings")` | `recordingsDir := filepath.Join(schmuxDir, "recordings")`         |
| 680     | `floormanager.New(cfg, sm, tmuxServer, homeDir, fmLog)`                                         | **No change.** Keep passing `homeDir` (see note below).           |

**Note on lines 378 and 680**: The function signatures for `ensure.EnsureGlobalHookScripts(homeDir)` and `floormanager.New(..., homeDir, ...)` are kept unchanged. These functions will be migrated in Steps 7 and 8 to ignore the `homeDir` parameter internally and use `schmuxdir.Get()` instead. This avoids cross-group file conflicts -- daemon.go does not need to change when those functions are migrated.

**Note on `homeDir` variable lifecycle**: After this step, `homeDir` is still needed at line 365 for the two call sites (lines 378 and 680) that pass it to downstream functions. The `os.UserHomeDir()` call at line 365 must be kept. The `schmuxDir` local variable is replaced to use `schmuxdir.Get()`.

Change line 370 from:

```go
schmuxDir := filepath.Join(homeDir, ".schmux")
```

To:

```go
schmuxDir := schmuxdir.Get()
```

### Run tests

```bash
go test ./internal/daemon/...
```

---

## Step 4c: Migrate `internal/daemon/daemon.go` -- `Run()` second half (lines 1076-1406)

**File**: `internal/daemon/daemon.go`

This sub-step covers the remaining `filepath.Join(homeDir, ".schmux", ...)` references in the second half of `Run()`. All are replaced with `schmuxDir` (the local variable already set to `schmuxdir.Get()` in Step 4b).

### Complete enumeration of references (Run, second half)

| Line | Current code                                                                             | Replacement                                                                     |
| ---- | ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| 1076 | `emergenceBaseDir := filepath.Join(homeDir, ".schmux", "emergence")`                     | `emergenceBaseDir := filepath.Join(schmuxDir, "emergence")`                     |
| 1088 | `actionBaseDir := filepath.Join(homeDir, ".schmux", "actions")`                          | `actionBaseDir := filepath.Join(schmuxDir, "actions")`                          |
| 1109 | `loreProposalDir := filepath.Join(homeDir, ".schmux", "lore-proposals")`                 | `loreProposalDir := filepath.Join(schmuxDir, "lore-proposals")`                 |
| 1116 | `loreInstructionsDir := filepath.Join(homeDir, ".schmux", "instructions")`               | `loreInstructionsDir := filepath.Join(schmuxDir, "instructions")`               |
| 1120 | `lorePendingMergeDir := filepath.Join(homeDir, ".schmux", "lore-pending-merges")`        | `lorePendingMergeDir := filepath.Join(schmuxDir, "lore-pending-merges")`        |
| 1195 | `runDir := filepath.Join(homeDir, ".schmux", "lore-curator-runs", repoName, curationID)` | `runDir := filepath.Join(schmuxDir, "lore-curator-runs", repoName, curationID)` |
| 1381 | `subredditDir := filepath.Join(homeDir, ".schmux", "subreddit")`                         | `subredditDir := filepath.Join(schmuxDir, "subreddit")`                         |
| 1406 | `oldCachePath := filepath.Join(homeDir, ".schmux", "subreddit.json")`                    | `oldCachePath := filepath.Join(schmuxDir, "subreddit.json")`                    |

That is 8 replacements. Each changes `filepath.Join(homeDir, ".schmux", ...)` to `filepath.Join(schmuxDir, ...)`, using the `schmuxDir` local variable that was set to `schmuxdir.Get()` in Step 4b.

### Run tests

```bash
go test ./internal/daemon/...
```

---

## Summary of all daemon.go references

For verification, here is the complete list of all 18 `homeDir` + `.schmux` references in `daemon.go` and how each is handled:

| #   | Line | Function             | Pattern                                                                           | Handled in                                        |
| --- | ---- | -------------------- | --------------------------------------------------------------------------------- | ------------------------------------------------- |
| 1   | 130  | `ValidateReadyToRun` | `filepath.Join(homeDir, ".schmux")`                                               | Step 4a                                           |
| 2   | 169  | `Start`              | `filepath.Join(homeDir, ".schmux")`                                               | Step 4a                                           |
| 3   | 237  | `Stop`               | `filepath.Join(homeDir, ".schmux", pidFileName)`                                  | Step 4a                                           |
| 4   | 286  | `Status`             | `filepath.Join(homeDir, ".schmux", pidFileName)`                                  | Step 4a                                           |
| 5   | 287  | `Status`             | `filepath.Join(homeDir, ".schmux", "daemon.started")`                             | Step 4a                                           |
| 6   | 313  | `Status`             | `filepath.Join(homeDir, ".schmux", "daemon.url")`                                 | Step 4a                                           |
| 7   | 319  | `Status`             | `filepath.Join(homeDir, ".schmux", "config.json")`                                | Step 4a                                           |
| 8   | 370  | `Run`                | `filepath.Join(homeDir, ".schmux")`                                               | Step 4b                                           |
| 9   | 378  | `Run`                | `ensure.EnsureGlobalHookScripts(homeDir)` (passes homeDir)                        | Step 4b (kept unchanged; Step 7 fixes internally) |
| 10  | 523  | `Run`                | `filepath.Join(homeDir, ".schmux", "instructions")`                               | Step 4b                                           |
| 11  | 531  | `Run`                | `filepath.Join(home, ".schmux", "recordings")` (note: uses `home`, not `homeDir`) | Step 4b                                           |
| 12  | 681  | `Run`                | `floormanager.New(..., homeDir, ...)` (passes homeDir)                            | Step 4b (kept unchanged; Step 8 fixes internally) |
| 13  | 1076 | `Run`                | `filepath.Join(homeDir, ".schmux", "emergence")`                                  | Step 4c                                           |
| 14  | 1088 | `Run`                | `filepath.Join(homeDir, ".schmux", "actions")`                                    | Step 4c                                           |
| 15  | 1109 | `Run`                | `filepath.Join(homeDir, ".schmux", "lore-proposals")`                             | Step 4c                                           |
| 16  | 1116 | `Run`                | `filepath.Join(homeDir, ".schmux", "instructions")`                               | Step 4c                                           |
| 17  | 1120 | `Run`                | `filepath.Join(homeDir, ".schmux", "lore-pending-merges")`                        | Step 4c                                           |
| 18  | 1195 | `Run`                | `filepath.Join(homeDir, ".schmux", "lore-curator-runs", ...)`                     | Step 4c                                           |
| 19  | 1381 | `Run`                | `filepath.Join(homeDir, ".schmux", "subreddit")`                                  | Step 4c                                           |
| 20  | 1406 | `Run`                | `filepath.Join(homeDir, ".schmux", "subreddit.json")`                             | Step 4c                                           |

Total: 20 references (7 in package-level functions, 13 in `Run()`). Line 1863 is only a comment in `createDevConfigBackup()` and does not contain a hardcoded path (the function accepts `schmuxDir` as a parameter).

---

## Step 5: Migrate `pkg/cli/daemon_client.go`

**File**: `pkg/cli/daemon_client.go`

### 5a. Replace hardcoded path in `ResolveURL()`

Change (line ~46-48):

```go
home, err := os.UserHomeDir()
if err == nil {
	data, err := os.ReadFile(filepath.Join(home, ".schmux", "daemon.url"))
```

To:

```go
d := schmuxdir.Get()
if d != "" {
	data, err := os.ReadFile(filepath.Join(d, "daemon.url"))
```

Add import: `"github.com/sergeknystautas/schmux/internal/schmuxdir"`

Note: `pkg/cli` importing `internal/schmuxdir` is valid because they are in the same module.

### 5b. Run tests

```bash
go test ./pkg/cli/...
```

---

## Step 6: Migrate `internal/dashboard/` handler files

**Files**: `internal/dashboard/handlers_dev.go`, `handlers_subreddit.go`, `handlers_timelapse.go`, `handlers_usermodels.go`, `handlers_lore.go`, `websocket.go`, `server.go`

### 6a. Replace all hardcoded paths

For each file, add import `"github.com/sergeknystautas/schmux/internal/schmuxdir"` and replace every `filepath.Join(homeDir, ".schmux", ...)` or `filepath.Join(os.Getenv("HOME"), ".schmux", ...)` with `filepath.Join(schmuxdir.Get(), ...)`.

Remove the `os.UserHomeDir()` / `os.Getenv("HOME")` calls that were only used for the `.schmux` join.

**websocket.go** and **server.go** use `os.Getenv("HOME")` -- these need the same treatment but with the different source pattern.

### 6b. Run tests

```bash
go test ./internal/dashboard/...
```

---

## Step 7: Migrate `internal/workspace/overlay.go` and `internal/workspace/ensure/manager.go`

**Files**: `internal/workspace/overlay.go`, `internal/workspace/ensure/manager.go`

### 7a. Replace hardcoded paths

**overlay.go** -- `OverlayDir()`: replace `filepath.Join(homeDir, ".schmux", "overlays", repoName)` with `filepath.Join(schmuxdir.Get(), "overlays", repoName)`.

**ensure/manager.go** -- `SignalingInstructionsFilePath()`: replace `filepath.Join(homeDir, ".schmux", "signaling.md")` with `filepath.Join(schmuxdir.Get(), "signaling.md")`.

**ensure/manager.go** -- `EnsureGlobalHookScripts(homeDir string)`: **Keep the function signature unchanged.** Internally, the function delegates to `detect.EnsureGlobalHookScripts(homeDir)` which will be migrated in Step 9 to ignore its `homeDir` param. No change needed in this wrapper beyond what Step 9 handles.

Add import `"github.com/sergeknystautas/schmux/internal/schmuxdir"` to both.

**This step does NOT touch `daemon.go`.** The `ensure.EnsureGlobalHookScripts(homeDir)` call on daemon.go line 378 remains unchanged.

### 7b. Run tests

```bash
go test ./internal/workspace/...
```

---

## Step 8: Migrate `internal/lore/`, `internal/logging/`, `internal/floormanager/`

**Files**: `internal/lore/scratchpad.go`, `internal/logging/logging.go`, `internal/floormanager/manager.go`

### 8a. Replace hardcoded paths

**lore/scratchpad.go** -- `LoreStateDir()`: replace `filepath.Join(homeDir, ".schmux", "lore", repoName)` with `filepath.Join(schmuxdir.Get(), "lore", repoName)`.

**logging/logging.go** -- startup log path: replace `filepath.Join(homeDir, ".schmux", ...)` with `filepath.Join(schmuxdir.Get(), ...)`.

**floormanager/manager.go** -- `New()`: **Keep the function signature unchanged** (`func New(cfg *config.Config, sm *session.Manager, server *tmux.TmuxServer, homeDir string, logger *log.Logger) *Manager`). Internally, change the `workDir` computation on line 65 from `filepath.Join(homeDir, ".schmux", "floor-manager")` to `filepath.Join(schmuxdir.Get(), "floor-manager")`. The `homeDir` parameter is now unused but kept to avoid cascading changes to `daemon.go`.

Add import `"github.com/sergeknystautas/schmux/internal/schmuxdir"` to each.

**This step does NOT touch `daemon.go`.** The `floormanager.New(cfg, sm, tmuxServer, homeDir, fmLog)` call on daemon.go line 680 remains unchanged.

### 8b. Run tests

```bash
go test ./internal/lore/... ./internal/logging/... ./internal/floormanager/...
```

---

## Step 9: Migrate `internal/assets/`, `internal/detect/`

**Files**: `internal/assets/download.go`, `internal/detect/adapter_claude_hooks.go`

### 9a. Replace hardcoded paths

**assets/download.go** -- dashboard cache path: replace `filepath.Join(homeDir, ".schmux", "dashboard")` with `filepath.Join(schmuxdir.Get(), "dashboard")`.

**detect/adapter_claude_hooks.go** -- `EnsureGlobalHookScripts(homeDir string)`: **Keep the function signature unchanged.** Internally, change line 382 from `hooksDir := filepath.Join(homeDir, ".schmux", "hooks")` to `hooksDir := filepath.Join(schmuxdir.Get(), "hooks")`. The `homeDir` parameter is now unused but kept to avoid cascading changes through the `ensure` wrapper and `daemon.go`.

Add import `"github.com/sergeknystautas/schmux/internal/schmuxdir"` to each.

**This step does NOT touch `daemon.go` or `ensure/manager.go`.** The call chain `daemon.go -> ensure.EnsureGlobalHookScripts(homeDir) -> detect.EnsureGlobalHookScripts(homeDir)` remains structurally unchanged. The `homeDir` parameter flows through but is ignored at the leaf.

### 9b. Run tests

```bash
go test ./internal/assets/... ./internal/detect/...
```

---

## Step 10: Migrate `internal/dashboardsx/`, `internal/oneshot/`

**Files**: `internal/dashboardsx/paths.go`, `internal/oneshot/oneshot.go`

### 10a. Replace hardcoded paths

**dashboardsx/paths.go** -- `Dir()` and related functions: replace `filepath.Join(homeDir, ".schmux", dirName)` with `filepath.Join(schmuxdir.Get(), dirName)`.

**oneshot/oneshot.go** -- schemas dir: replace `filepath.Join(homeDir, ".schmux", "schemas", ...)` with `filepath.Join(schmuxdir.Get(), "schemas", ...)`.

Add import `"github.com/sergeknystautas/schmux/internal/schmuxdir"` to each.

### 10b. Run tests

```bash
go test ./internal/dashboardsx/... ./internal/oneshot/...
```

---

## Step 11: Migrate `cmd/schmux/` files

**Files**: `cmd/schmux/timelapse.go`, `cmd/schmux/auth_github.go`, `cmd/schmux/dashboardsx.go`

### 11a. Replace hardcoded paths

**timelapse.go** -- recordings dir: replace `filepath.Join(home, ".schmux", "recordings")` with `filepath.Join(schmuxdir.Get(), "recordings")`.

**auth_github.go** -- TLS cert/key paths and config path: replace all `filepath.Join(cmd.homeDir, ".schmux", ...)` with `filepath.Join(schmuxdir.Get(), ...)`. The `homeDir` field on the command struct may become unused -- remove if so.

**dashboardsx.go** -- config loading: replace `filepath.Join(homeDir, ".schmux", "config.json")` with `filepath.Join(schmuxdir.Get(), "config.json")`.

Add import `"github.com/sergeknystautas/schmux/internal/schmuxdir"` to each.

### 11b. Run tests

```bash
go test ./cmd/schmux/...
```

---

## Step 12: Integration test -- verify downstream functions use custom dir

**File**: `internal/schmuxdir/schmuxdir_integration_test.go` (new file)

### 12a. Write integration test

This test verifies that with `schmuxdir.Set()` pointing to a temp directory, downstream path-constructing functions return paths under the custom dir, not `~/.schmux/`:

```go
package schmuxdir_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/lore"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func TestDownstreamFunctionsUseCustomDir(t *testing.T) {
	tmpDir := t.TempDir()
	schmuxdir.Set(tmpDir)
	defer schmuxdir.Reset()

	home, _ := os.UserHomeDir()
	defaultDir := filepath.Join(home, ".schmux")

	// Verify schmuxdir.Get() returns custom dir
	if got := schmuxdir.Get(); got != tmpDir {
		t.Fatalf("schmuxdir.Get() = %q, want %q", got, tmpDir)
	}

	// Verify config.ConfigExists() checks under custom dir, not ~/.schmux
	// (It will return false since the temp dir has no config.json, but
	// the important thing is that it does NOT check ~/.schmux)
	// We verify indirectly: if ConfigExists() used ~/.schmux and a config
	// exists there, it would return true. With our custom dir, it returns false.
	exists := config.ConfigExists()
	if exists {
		t.Errorf("config.ConfigExists() returned true for empty temp dir -- likely still checking ~/.schmux instead of custom dir")
	}

	// Verify lore.LoreStateDir() returns path under custom dir
	loreDir, err := lore.LoreStateDir("test-repo")
	if err != nil {
		t.Fatalf("lore.LoreStateDir() returned error: %v", err)
	}
	if !strings.HasPrefix(loreDir, tmpDir) {
		t.Errorf("lore.LoreStateDir() = %q, want prefix %q", loreDir, tmpDir)
	}
	if strings.HasPrefix(loreDir, defaultDir) {
		t.Errorf("lore.LoreStateDir() = %q, must NOT start with default %q", loreDir, defaultDir)
	}

	// Verify the expected path structure
	wantLore := filepath.Join(tmpDir, "lore", "test-repo")
	if loreDir != wantLore {
		t.Errorf("lore.LoreStateDir() = %q, want %q", loreDir, wantLore)
	}
}
```

### 12b. Run full test suite

```bash
go test ./...
```

### 12c. Run the project test suite

```bash
./test.sh --quick
```

---

## Step 13: Final sweep -- grep for any remaining hardcoded references

### 13a. Verify no `.schmux` hardcoded paths remain in production code

Use the Grep tool to search for remaining `os.UserHomeDir` + `.schmux` patterns in production Go files (excluding test files, `schmuxdir.go` itself, and vendor/).

**Expected acceptable remaining hits:**

- **`internal/e2e/e2e.go`**: Test infrastructure that constructs an isolated `HomeDir` for E2E tests. Uses its own `e.HomeDir` variable, not the config dir. This is correct and does not need migration.
- **`internal/state/state.go`**: References to `.schmux` are per-workspace hidden directories (e.g., `workspace/.schmux/state.json`), not the global config dir. These are structurally different and do not need migration.
- **`internal/workspace/config.go`**: Same as `state.go` -- per-workspace `.schmux` dirs.

Any other remaining hits in production code need to be migrated.

### 13b. Build the binary

```bash
go build ./cmd/schmux
```

---

## Verified No-Ops

These items were investigated and confirmed to require no code changes:

1. **`tools/dev-runner/src/lib/cleanEnv.ts`**: The `cleanEnv()` function only strips `npm_*`, `INIT_CWD`, `NODE`, and `SCHMUX_PRISTINE_*` vars. `SCHMUX_HOME` passes through unchanged. Verified by reading the source -- no conditional or pattern-based deletion would affect `SCHMUX_HOME`.

2. **`createDevConfigBackup()` in daemon.go (line 1863)**: The comment mentions `~/.schmux/backups/` but the function accepts `schmuxDir` as a parameter and uses it correctly. No hardcoded path.

3. **`state.Load()` and `config.Load()`**: Already accept path parameters. No hardcoded `.schmux` paths.

4. **`workspace.New()` and `session.New()`**: Do not hardcode `~/.schmux` -- they receive config/state objects from the daemon.

5. **`models.New()`**: Already takes a `schmuxDir` field and uses it for cache paths.
