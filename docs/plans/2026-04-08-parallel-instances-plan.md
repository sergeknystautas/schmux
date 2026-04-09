# Plan: Configurable Schmux Directory for Parallel Instances

**Goal**: Allow multiple schmux instances on the same machine by making `~/.schmux/` configurable via `--config-dir` flag and `SCHMUX_HOME` env var.
**Architecture**: New `internal/schmuxdir/` package with `Set()`/`Get()` (package-level, matches existing `SetLogger()` pattern). Called once at startup in `main.go`, read everywhere via `schmuxdir.Get()`.
**Tech Stack**: Go 1.22+, standard library only.
**Design Doc**: `docs/plans/2026-04-08-parallel-instances-design-v2.md`

## Dependency Groups

| Group | Steps                        | Can Parallelize | Notes                                              |
| ----- | ---------------------------- | --------------- | -------------------------------------------------- |
| 1     | Step 1                       | No              | Foundation — `schmuxdir` package                   |
| 2     | Step 2                       | No              | CLI flag parsing in `main.go` (depends on Group 1) |
| 3     | Steps 3, 4, 5                | Yes             | Independent package migrations                     |
| 4     | Steps 6, 7, 8, 9, 10, 11, 12 | Yes             | Independent package migrations                     |
| 5     | Step 13                      | No              | Integration test (depends on all above)            |

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

### 3a. config.go — Replace hardcoded paths

In `ConfigExists()` (line ~1913): replace `filepath.Join(homeDir, ".schmux", "config.json")` with `filepath.Join(schmuxdir.Get(), "config.json")`. Remove the `os.UserHomeDir()` call.

In `EnsureExists()` (line ~1925): same — use `schmuxdir.Get()` for the config path. Remove the `os.UserHomeDir()` call since `ConfigExists()` no longer needs it.

In `GetWorktreeBasePath()`: replace `filepath.Join(homeDir, ".schmux", "repos")` default with `filepath.Join(schmuxdir.Get(), "repos")`.

In `GetQueryRepoPath()`: replace `filepath.Join(homeDir, ".schmux", "query")` default with `filepath.Join(schmuxdir.Get(), "query")`.

Add import: `"github.com/sergeknystautas/schmux/internal/schmuxdir"`

### 3b. secrets.go — Replace `secretsPath()`

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

## Step 4: Migrate `internal/daemon/daemon.go`

**File**: `internal/daemon/daemon.go`

### 4a. Replace all hardcoded `~/.schmux` paths

Add import: `"github.com/sergeknystautas/schmux/internal/schmuxdir"`

**`ValidateReadyToRun()`** (line ~117): Replace:

```go
schmuxDir := filepath.Join(homeDir, ".schmux")
```

With:

```go
schmuxDir := schmuxdir.Get()
```

Remove the `os.UserHomeDir()` call and `homeDir` variable.

**`Start()`** (line ~163): Same replacement. Additionally, propagate `SCHMUX_HOME` to the child process — after `cmd := exec.Command(...)`, before `cmd.Start()`:

```go
// Propagate config dir to child process
if d := schmuxdir.Get(); d != "" {
	cmd.Env = append(os.Environ(), "SCHMUX_HOME="+d)
}
```

Wait — `Start()` already sets `cmd.Env` or uses the default. Need to check. If `cmd.Env` is nil (inherits parent), just set it. If it's already set, append.

**`Stop()`** (line ~231): Replace:

```go
pidFile := filepath.Join(homeDir, ".schmux", pidFileName)
```

With:

```go
pidFile := filepath.Join(schmuxdir.Get(), pidFileName)
```

Remove `os.UserHomeDir()` call.

**`Status()`** (line ~280): Replace both:

```go
pidFile := filepath.Join(homeDir, ".schmux", pidFileName)
startedFile := filepath.Join(homeDir, ".schmux", "daemon.started")
```

With:

```go
d := schmuxdir.Get()
pidFile := filepath.Join(d, pidFileName)
startedFile := filepath.Join(d, "daemon.started")
```

Remove `os.UserHomeDir()` call.

**`Run()` method** (line ~370 onward): Replace:

```go
schmuxDir := filepath.Join(homeDir, ".schmux")
```

With:

```go
schmuxDir := schmuxdir.Get()
```

Remove the `os.UserHomeDir()` call. The `homeDir` variable may still be needed for `EnsureGlobalHookScripts(homeDir)` — check in Step 10.

All other `schmuxDir` uses within `Run()` are already derived from this local variable, so they'll work automatically.

### 4b. Run tests

```bash
go test ./internal/daemon/...
```

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

Note: `pkg/cli` importing `internal/schmuxdir` is valid because they're in the same module.

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

**websocket.go** and **server.go** use `os.Getenv("HOME")` — these need the same treatment but with the different source pattern.

### 6b. Run tests

```bash
go test ./internal/dashboard/...
```

---

## Step 7: Migrate `internal/workspace/overlay.go` and `internal/workspace/ensure/manager.go`

**Files**: `internal/workspace/overlay.go`, `internal/workspace/ensure/manager.go`

### 7a. Replace hardcoded paths

**overlay.go** — `OverlayDir()`: replace `filepath.Join(homeDir, ".schmux", "overlays", repoName)` with `filepath.Join(schmuxdir.Get(), "overlays", repoName)`.

**ensure/manager.go** — `SignalingInstructionsFilePath()`: replace `filepath.Join(homeDir, ".schmux", "signaling.md")` with `filepath.Join(schmuxdir.Get(), "signaling.md")`.

Add import `"github.com/sergeknystautas/schmux/internal/schmuxdir"` to both.

### 7b. Run tests

```bash
go test ./internal/workspace/...
```

---

## Step 8: Migrate `internal/lore/`, `internal/logging/`, `internal/floormanager/`

**Files**: `internal/lore/scratchpad.go`, `internal/logging/logging.go`, `internal/floormanager/manager.go`

### 8a. Replace hardcoded paths

**lore/scratchpad.go** — `LoreStateDir()`: replace `filepath.Join(homeDir, ".schmux", "lore", repoName)` with `filepath.Join(schmuxdir.Get(), "lore", repoName)`.

**logging/logging.go** — startup log path: replace `filepath.Join(homeDir, ".schmux", ...)` with `filepath.Join(schmuxdir.Get(), ...)`.

**floormanager/manager.go** — `New()`: replace `filepath.Join(homeDir, ".schmux", "floor-manager")` with `filepath.Join(schmuxdir.Get(), "floor-manager")`. If `homeDir` param is now unused, remove it from the signature and update the caller in `daemon.go`.

Add import `"github.com/sergeknystautas/schmux/internal/schmuxdir"` to each.

### 8b. Run tests

```bash
go test ./internal/lore/... ./internal/logging/... ./internal/floormanager/...
```

---

## Step 9: Migrate `internal/assets/`, `internal/detect/`

**Files**: `internal/assets/download.go`, `internal/detect/adapter_claude_hooks.go`

### 9a. Replace hardcoded paths

**assets/download.go** — dashboard cache path: replace `filepath.Join(homeDir, ".schmux", "dashboard")` with `filepath.Join(schmuxdir.Get(), "dashboard")`.

**detect/adapter_claude_hooks.go** — `EnsureGlobalHookScripts()`: replace `filepath.Join(homeDir, ".schmux", "hooks")` with `filepath.Join(schmuxdir.Get(), "hooks")`. If the `homeDir` parameter is only used for this join, remove it and update the caller in `daemon.go`.

Add import `"github.com/sergeknystautas/schmux/internal/schmuxdir"` to each.

### 9b. Run tests

```bash
go test ./internal/assets/... ./internal/detect/...
```

---

## Step 10: Migrate `internal/dashboardsx/`, `internal/oneshot/`

**Files**: `internal/dashboardsx/paths.go`, `internal/oneshot/oneshot.go`

### 10a. Replace hardcoded paths

**dashboardsx/paths.go** — `Dir()` and related functions: replace `filepath.Join(homeDir, ".schmux", dirName)` with `filepath.Join(schmuxdir.Get(), dirName)`.

**oneshot/oneshot.go** — schemas dir: replace `filepath.Join(homeDir, ".schmux", "schemas", ...)` with `filepath.Join(schmuxdir.Get(), "schemas", ...)`.

Add import `"github.com/sergeknystautas/schmux/internal/schmuxdir"` to each.

### 10b. Run tests

```bash
go test ./internal/dashboardsx/... ./internal/oneshot/...
```

---

## Step 11: Migrate `cmd/schmux/` files

**Files**: `cmd/schmux/timelapse.go`, `cmd/schmux/auth_github.go`, `cmd/schmux/dashboardsx.go`

### 11a. Replace hardcoded paths

**timelapse.go** — recordings dir: replace `filepath.Join(home, ".schmux", "recordings")` with `filepath.Join(schmuxdir.Get(), "recordings")`.

**auth_github.go** — TLS cert/key paths and config path: replace all `filepath.Join(cmd.homeDir, ".schmux", ...)` with `filepath.Join(schmuxdir.Get(), ...)`. The `homeDir` field on the command struct may become unused — remove if so.

**dashboardsx.go** — config loading: replace `filepath.Join(homeDir, ".schmux", "config.json")` with `filepath.Join(schmuxdir.Get(), "config.json")`.

Add import `"github.com/sergeknystautas/schmux/internal/schmuxdir"` to each.

### 11b. Run tests

```bash
go test ./cmd/schmux/...
```

---

## Step 12: Update `printUsage()` in main.go and verify `SCHMUX_HOME` propagation in `Start()`

**File**: `cmd/schmux/main.go`

### 12a. Implementation

Verify that `daemon.Start()` properly propagates `SCHMUX_HOME` to the child process (done in Step 4). Add usage text for the `--config-dir` flag to `printUsage()`.

### 12b. Manual verification

```bash
# Build
go build ./cmd/schmux

# Test help shows the flag
./schmux help | grep config-dir

# Test flag parsing
./schmux --config-dir /tmp/test-schmux status 2>&1 | grep "using config dir"
```

---

## Step 13: Integration test — verify no cross-talk

**File**: `internal/schmuxdir/schmuxdir_test.go` (add integration-level test) or a new test file.

### 13a. Write verification test

This test verifies that with `schmuxdir.Set()` pointing to a temp directory, none of the path-constructing functions return paths under `~/.schmux/`:

```go
func TestNoCrossTalkWithCustomDir(t *testing.T) {
	tmpDir := t.TempDir()
	Set(tmpDir)
	defer Reset()

	got := Get()
	if got != tmpDir {
		t.Fatalf("Get() = %q, want %q", got, tmpDir)
	}

	// Verify the custom dir is not under ~/.schmux
	home, _ := os.UserHomeDir()
	defaultDir := filepath.Join(home, ".schmux")
	if got == defaultDir {
		t.Fatal("custom dir should differ from default")
	}
}
```

### 13b. Run full test suite

```bash
go test ./...
```

### 13c. Run the project test suite

```bash
./test.sh --quick
```

---

## Step 14: Final sweep — grep for any remaining hardcoded references

### 14a. Verify no `.schmux` hardcoded paths remain in production code

```bash
# Search for os.UserHomeDir followed by .schmux join (production Go files only)
grep -rn '\.schmux' --include='*.go' | grep -v '_test.go' | grep -v vendor/ | grep -v schmuxdir.go

# Search for os.Getenv("HOME") + .schmux pattern
grep -rn 'Getenv.*HOME.*schmux\|schmux.*Getenv.*HOME' --include='*.go' | grep -v '_test.go'
```

Any remaining hits in production code (excluding `schmuxdir.go` itself and test files) need to be migrated.

### 14b. Build the binary

```bash
go build ./cmd/schmux
```
