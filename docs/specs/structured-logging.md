# Structured Logging Migration Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Replace all ad-hoc `fmt.Printf`/`fmt.Println` daemon logging with `charmbracelet/log`, assigning appropriate log levels (debug/info/warn/error) based on context.

**Architecture:** Create a thin `internal/logging` package that initializes a `charmbracelet/log.Logger` and exposes per-subsystem child loggers via `logger.WithPrefix("[workspace]")` etc. Each package receives its logger at construction time (dependency injection). The daemon's `Run()` function creates the root logger and passes subsystem loggers to managers.

**Tech Stack:** `github.com/charmbracelet/log` (latest v0.4.x)

---

## Scope

**In scope:** All `fmt.Printf`/`fmt.Println` calls inside `internal/` packages that act as daemon log output (~230 calls across ~25 files).

**Out of scope:**

- CLI user-facing output in `cmd/schmux/` (`fmt.Fprintf(os.Stderr, ...)`, `fmt.Println(...)` for user messages) — these stay as-is
- `internal/update/update.go` user-facing messages like "Checking for updates..." — these stay as-is since they're CLI output, not daemon logs

## Log Level Guidelines

| Level   | When to use                                                   | Examples                                                                                             |
| ------- | ------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `Debug` | Verbose operational detail useful only when troubleshooting   | "no origin remote, skipping fetch", "no active sibling workspaces to propagate to"                   |
| `Info`  | Normal operational milestones the operator wants to see       | "workspace created", "session spawned", "daemon shutting down"                                       |
| `Warn`  | Non-fatal problems that degrade but don't break functionality | "failed to copy overlay files", "fetch failed before worktree add", all existing "warning:" messages |
| `Error` | Failures that prevent a requested operation from completing   | "spawn error", "dispose error", "failed to propagate file"                                           |

## Design Decisions

1. **Dependency injection, not globals.** Each manager/package receives a `*log.Logger` at construction time. No package-level `var logger` or `log.Default()` usage.
2. **Prefixed child loggers** replace the manual `[workspace]`, `[session]`, etc. bracket prefixes. Use `logger.WithPrefix("workspace")` so the library handles formatting.
3. **Structured key-value fields** for machine-parseable context: `logger.Info("created", "id", w.ID, "path", w.Path)` instead of `fmt.Printf("[workspace] created: id=%s path=%s\n", w.ID, w.Path)`.
4. **Rate-limited logging** in `telemetry.go` and `session/tracker.go` keeps its existing rate-limit logic but calls `logger.Warn`/`logger.Error` instead of `fmt.Printf`.
5. **The `difftool` callback pattern** changes from `func(string, ...interface{})` to accepting a `*log.Logger`.
6. **Default log level:** `log.InfoLevel`. Configurable via `SCHMUX_LOG_LEVEL` env var (values: `debug`, `info`, `warn`, `error`).
7. **Output writer:** `os.Stderr` (standard practice for daemon logs — stdout reserved for structured data/CLI output).

---

## Task 1: Add the dependency and create the logging package

**Files:**

- Modify: `go.mod`
- Create: `internal/logging/logging.go`
- Create: `internal/logging/logging_test.go`

**Step 1: Add the charmbracelet/log dependency**

```bash
cd /Users/stefanomaz/code/workspaces/schmux-003 && go get github.com/charmbracelet/log
```

**Step 2: Write the test for the logging package**

Create `internal/logging/logging_test.go`:

```go
package logging

import (
	"bytes"
	"os"
	"testing"

	"github.com/charmbracelet/log"
)

func TestNew_DefaultLevel(t *testing.T) {
	logger := New()
	if logger.GetLevel() != log.InfoLevel {
		t.Errorf("expected InfoLevel, got %v", logger.GetLevel())
	}
}

func TestNew_EnvOverride(t *testing.T) {
	t.Setenv("SCHMUX_LOG_LEVEL", "debug")
	logger := New()
	if logger.GetLevel() != log.DebugLevel {
		t.Errorf("expected DebugLevel, got %v", logger.GetLevel())
	}
}

func TestNew_InvalidEnv(t *testing.T) {
	t.Setenv("SCHMUX_LOG_LEVEL", "bogus")
	logger := New()
	if logger.GetLevel() != log.InfoLevel {
		t.Errorf("expected InfoLevel fallback, got %v", logger.GetLevel())
	}
}

func TestNew_WritesToStderr(t *testing.T) {
	// Just verify the logger is created without panic
	logger := New()
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestSub_HasPrefix(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewWithOptions(&buf, log.Options{})
	sub := Sub(logger, "workspace")
	sub.Info("test")
	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("workspace")) {
		t.Errorf("expected prefix 'workspace' in output, got: %s", output)
	}
}
```

**Step 3: Run test to verify it fails**

```bash
go test ./internal/logging/...
```

Expected: FAIL — package doesn't exist yet.

**Step 4: Write the logging package**

Create `internal/logging/logging.go`:

```go
// Package logging provides structured logging for the schmux daemon.
package logging

import (
	"os"
	"strings"

	"github.com/charmbracelet/log"
)

// New creates a root logger configured from environment.
// Log level defaults to InfoLevel, overridden by SCHMUX_LOG_LEVEL env var.
// An optional forceColor parameter enables colorized output regardless of terminal detection.
func New(forceColor ...bool) *log.Logger {
	level := log.InfoLevel
	if env := os.Getenv("SCHMUX_LOG_LEVEL"); env != "" {
		parsed, err := log.ParseLevel(strings.ToLower(env))
		if err == nil {
			level = parsed
		}
	}
	logger := log.NewWithOptions(os.Stderr, log.Options{
		Level:           level,
		ReportTimestamp: true,
	})
	return logger
}

// Sub creates a child logger with the given prefix wrapped in brackets.
func Sub(parent *log.Logger, prefix string) *log.Logger {
	return parent.WithPrefix("[" + prefix + "]")
}
```

**Step 5: Run tests to verify they pass**

```bash
go test ./internal/logging/...
```

Expected: PASS

**Step 6: Commit**

```
feat(logging): add charmbracelet/log logging package
```

---

## Task 2: Wire root logger into the daemon and dashboard server

**Files:**

- Modify: `internal/daemon/daemon.go` — create root logger in `Run()`, pass to dashboard server
- Modify: `internal/dashboard/server.go` — accept `*log.Logger`, convert all `fmt.Printf` calls

**Step 1: Add logger field to the Daemon struct and initialize in `Run()`**

In `daemon.go`, add to the `Run()` function, before any existing log output:

```go
import "github.com/charmbracelet/log"
import "github.com/sergeknystautas/schmux/internal/logging"

// At the top of Run():
logger := logging.New()
logger.Info("starting", "version", version.Version, "port", dashboardPort)
```

Store `logger` so it can be passed to subsystems.

**Step 2: Convert all `fmt.Printf` calls in `daemon.go`**

Replace each call using the level assessment from the survey. Examples:

| Before                                                                                                              | After                                                                                                                       |
| ------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `fmt.Printf("[telemetry] warning: failed to save installation ID: %v\n", err)`                                      | `logging.Sub(logger, "telemetry").Warn("failed to save installation ID", "err", err)`                                       |
| `fmt.Println("[telemetry] anonymous usage metrics enabled ...")`                                                    | `logging.Sub(logger, "telemetry").Info("anonymous usage metrics enabled (opt out: set telemetry_enabled=false in config)")` |
| `fmt.Printf("[daemon] loaded %d cached PRs from state\n", len(prs))`                                                | `logger.Info("loaded cached PRs from state", "count", len(prs))`                                                            |
| `fmt.Printf("[compound] rejecting unsafe relPath in propagator %q: %v\n", relPath, err)`                            | `compoundLog.Error("rejecting unsafe relPath in propagator", "path", relPath, "err", err)`                                  |
| `fmt.Printf("[overlay] no active sibling workspaces to propagate %s to (source=%s)\n", relPath, sourceWorkspaceID)` | `overlayLog.Debug("no active sibling workspaces to propagate to", "path", relPath, "source", sourceWorkspaceID)`            |
| `fmt.Printf("[daemon] received signal %v, shutting down\n", sig)`                                                   | `logger.Info("received signal, shutting down", "signal", sig)`                                                              |

Create subsystem loggers as local variables where multiple calls share a prefix:

```go
compoundLog := logging.Sub(logger, "compound")
overlayLog := logging.Sub(logger, "overlay")
loreLog := logging.Sub(logger, "lore")
telemetryLog := logging.Sub(logger, "telemetry")
```

**Step 3: Pass logger to `dashboard.NewServer()`**

Add a `*log.Logger` parameter to `NewServer()` and store it on the `Server` struct. Convert all `fmt.Printf` calls in `server.go` to use the logger.

**Step 4: Run tests**

```bash
go test ./internal/daemon/... ./internal/dashboard/...
```

Expected: PASS

**Step 5: Commit**

```
feat(logging): wire structured logging into daemon and dashboard server
```

---

## Task 3: Convert dashboard handler files

**Files:**

- Modify: `internal/dashboard/handlers.go`
- Modify: `internal/dashboard/handlers_dispose.go`
- Modify: `internal/dashboard/handlers_spawn.go`
- Modify: `internal/dashboard/handlers_sync.go`
- Modify: `internal/dashboard/handlers_remote.go`
- Modify: `internal/dashboard/preview_autodetect.go`

**Step 1: Use the server's logger in all handler methods**

Since handlers are methods on `*Server`, they access `s.logger`. Create subsystem loggers at the top of handler functions or as needed:

```go
func (s *Server) handleDispose(w http.ResponseWriter, r *http.Request) {
    sessionLog := logging.Sub(s.logger, "session")
    // ...
    sessionLog.Error("dispose failed", "session_id", sessionID, "err", err)
    // ...
    sessionLog.Info("dispose success", "session_id", sessionID)
}
```

**Step 2: Level assignments for handlers**

Key conversions across handler files:

- `handlers.go`: nudgenik messages — "disabled" → info, "no response" → info, "target not found" → warn, "failed to ask" → error
- `handlers_dispose.go`: dispose error → error, dispose success → info, cleanup warning → warn
- `handlers_spawn.go`: spawn request → info, spawn error → error, spawn success → info, branch suggest error → error, quick-launch parse failures → error, skipping invalid cookbooks → warn
- `handlers_sync.go`: sync operations → info/warn, conflict panic → error (keep the panic behavior, log as error before panicking)
- `handlers_remote.go`: remote operations → info/error
- `preview_autodetect.go`: preview detection → debug/info

**Step 3: Run tests**

```bash
go test ./internal/dashboard/...
```

Expected: PASS

**Step 4: Commit**

```
feat(logging): convert dashboard handlers to structured logging
```

---

## Task 4: Convert dashboard WebSocket file

**Files:**

- Modify: `internal/dashboard/websocket.go`

**Step 1: Convert all `fmt.Printf` calls in websocket.go**

This file has ~22 logging calls with prefixes like `[nudgenik]`, `[terminal]`, `[ws %s]`, `[sync]`, `[diagnostic]`, `[signal]`, `[ws remote %s]`, `[ws provision %s]`.

For parameterized prefixes like `[ws %s]`, use structured fields instead:

```go
// Before:
fmt.Printf("[ws %s] connected\n", sessionID)

// After:
wsLog := logging.Sub(s.logger, "ws")
wsLog.Info("connected", "session_id", sessionID)
```

**Step 2: Run tests**

```bash
go test ./internal/dashboard/...
```

Expected: PASS

**Step 3: Commit**

```
feat(logging): convert websocket logging to structured logging
```

---

## Task 5: Convert session package

**Files:**

- Modify: `internal/session/manager.go`
- Modify: `internal/session/tracker.go`

**Step 1: Add `*log.Logger` to `Manager` struct and constructor**

The `session.Manager` (or whatever the struct is called) needs a logger field. Pass a prefixed logger from the daemon: `logging.Sub(logger, "session")`.

**Step 2: Convert `manager.go` logging (~16 calls)**

Level assignments:

- "cannot start remote watcher: no workspace path" → warn
- "failed to create watcher window" → error
- "failed to send watcher script" → error
- "failed to wrap command with hooks provisioning" → warn
- "queued session failed" → error
- "queued session succeeded" → info
- "queued session: session no longer in state" → warn
- All "warning: failed to create .schmux/signal" → warn
- All "warning: failed to provision" → warn

**Step 3: Convert `tracker.go` logging (~3 calls)**

The tracker has rate-limited retry logging (`shouldLogRetry` with `trackerRetryLogInterval`). Keep the rate-limiting logic but replace the `fmt.Printf` inside with `logger.Warn(...)` or `logger.Debug(...)`.

Pass the logger to the tracker at construction time.

**Step 4: Run tests**

```bash
go test ./internal/session/...
```

Expected: PASS

**Step 5: Commit**

```
feat(logging): convert session package to structured logging
```

---

## Task 6: Convert workspace package

**Files:**

- Modify: `internal/workspace/manager.go` (~42 calls)
- Modify: `internal/workspace/git_watcher.go` (~10 calls)

**Step 1: Add `*log.Logger` to workspace `Manager` struct and constructor**

Pass `logging.Sub(logger, "workspace")` from the daemon.

**Step 2: Convert `manager.go` logging**

This is the largest single file (~42 calls). Level assignments:

- **debug:** "no origin remote, skipping fetch", "no origin/%s remote ref, skipping pull", "adding worktree for branch" (verbose operational detail)
- **info:** "created", "reusing existing", "prepared", "disposed", "worktree added", "created from workspace", "using unique branch", etc. (normal operational milestones)
- **warn:** all "warning:" messages, "directory missing, skipping", "cleaning up failed", "failed to get current branch", "failed to update git status"
- **error:** "failed to cleanup local repo" (with error detail)

Use structured fields:

```go
// Before:
fmt.Printf("[workspace] created: id=%s path=%s branch=%s repo=%s\n", w.ID, w.Path, w.Branch, repoURL)

// After:
m.logger.Info("created", "id", w.ID, "path", w.Path, "branch", w.Branch, "repo", repoURL)
```

**Step 3: Convert `git_watcher.go` logging**

Pass the workspace manager's logger (or a sub-logger) to the git watcher. Convert its ~10 calls.

**Step 4: Run tests**

```bash
go test ./internal/workspace/...
```

Expected: PASS

**Step 5: Commit**

```
feat(logging): convert workspace package to structured logging
```

---

## Task 7: Convert remaining internal packages (batch)

**Files:**

- Modify: `internal/state/state.go` (2 calls)
- Modify: `internal/tmux/tmux.go` (1 call)
- Modify: `internal/telemetry/telemetry.go` (~6 calls, rate-limited)
- Modify: `internal/github/discovery.go` (5 calls)
- Modify: `internal/remote/connection.go` (~28 calls)
- Modify: `internal/remote/manager.go` (~20 calls)
- Modify: `internal/compound/compound.go` (8 calls)
- Modify: `internal/compound/watcher.go` (4 calls)
- Modify: `internal/tunnel/manager.go` (2 calls)
- Modify: `internal/signal/filewatcher.go` (2 calls)
- Modify: `internal/preview/manager.go` (3 calls)
- Modify: `internal/assets/download.go` (2 calls)
- Modify: `internal/lore/proposals.go` (1 call)
- Modify: `internal/lore/scratchpad.go` (1 call)

**Step 1: Add `*log.Logger` to each package's manager/primary struct**

Each package that logs needs its struct updated to accept a `*log.Logger` at construction. The daemon's `Run()` wires these up:

```go
stateLog := logging.Sub(logger, "state")
tmuxLog := logging.Sub(logger, "tmux")
telemetryLog := logging.Sub(logger, "telemetry")
githubLog := logging.Sub(logger, "github")
remoteLog := logging.Sub(logger, "remote")
compoundLog := logging.Sub(logger, "compound")
tunnelLog := logging.Sub(logger, "remote-access")
signalLog := logging.Sub(logger, "signal")
previewLog := logging.Sub(logger, "preview")
loreLog := logging.Sub(logger, "lore")
```

**Step 2: Convert the `difftool` callback pattern**

Change `SweepAndScheduleTempDirs` signature from:

```go
func SweepAndScheduleTempDirs(cleanupAfter time.Duration, logger func(string, ...interface{})) (deleted, scheduled int)
```

to:

```go
func SweepAndScheduleTempDirs(cleanupAfter time.Duration, logger *log.Logger) (deleted, scheduled int)
```

Update the call site in `daemon.go` accordingly.

**Step 3: Convert telemetry rate-limited logging**

Keep the `shouldLogFailure` / `failureLogInterval` mechanism but replace the underlying `fmt.Printf` in `logFailure()`:

```go
func (c *Client) logFailure(msg string, keyvals ...interface{}) {
    c.mu.Lock()
    defer c.mu.Unlock()
    now := time.Now()
    if now.Sub(c.lastFailureLog) < failureLogInterval {
        return
    }
    c.lastFailureLog = now
    c.logger.Warn(msg, keyvals...)
}
```

The `logFailure` signature changes from `(format string, args ...interface{})` to `(msg string, keyvals ...interface{})`.

**Step 4: Run all tests**

```bash
./test.sh
```

Expected: PASS

**Step 5: Commit**

```
feat(logging): convert all remaining internal packages to structured logging
```

---

## Task 8: Clean up and verify

**Files:**

- Modify: `internal/daemon/daemon.go` — remove any remaining `fmt` import if unused
- All converted files — verify no `fmt.Printf` logging remains

**Step 1: Search for remaining fmt.Printf logging calls**

```bash
grep -rn 'fmt\.Printf.*\[' internal/ | grep -v '_test.go' | grep -v 'Sprintf'
grep -rn 'fmt\.Println.*\[' internal/ | grep -v '_test.go'
```

Expected: No results (all converted).

**Step 2: Check that `fmt` is only imported where still needed**

Run the build to catch any unused imports:

```bash
go build ./...
```

Expected: Clean build, no errors.

**Step 3: Run the full test suite**

```bash
./test.sh --all
```

Expected: All tests pass.

**Step 4: Manual smoke test**

```bash
SCHMUX_LOG_LEVEL=debug ./schmux daemon-run
```

Verify:

- Logs appear on stderr with timestamps and prefixes
- Debug-level messages appear with `SCHMUX_LOG_LEVEL=debug`
- Only info+ messages appear without the env var
- Log output uses charmbracelet/log formatting (colorized levels, timestamps)

**Step 5: Commit**

```
chore(logging): clean up unused fmt imports after logging migration
```

---

## Summary

| Task                   | Files         | Estimated log calls |
| ---------------------- | ------------- | ------------------- |
| 1. Logging package     | 2 new         | 0 (infrastructure)  |
| 2. Daemon + server     | 2             | ~50                 |
| 3. Dashboard handlers  | 6             | ~40                 |
| 4. Dashboard websocket | 1             | ~22                 |
| 5. Session package     | 2             | ~19                 |
| 6. Workspace package   | 2             | ~52                 |
| 7. Remaining packages  | 14            | ~65                 |
| 8. Cleanup + verify    | all           | 0 (verification)    |
| **Total**              | **~29 files** | **~230 calls**      |
