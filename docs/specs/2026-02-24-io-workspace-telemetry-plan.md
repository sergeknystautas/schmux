# IO Workspace Telemetry Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Instrument all git exec.Command calls in the workspace package with timing telemetry, mirroring the terminal desync diagnostic system end-to-end.

**Architecture:** In-memory collector (`IOWorkspaceTelemetry`) records every git command. Live stats pushed over the terminal WebSocket. Capture triggered via WebSocket message, writes to `~/.schmux/diagnostics/`, optionally auto-spawns analysis agent. Config toggle + target selector in AdvancedTab.

**Tech Stack:** Go (backend collector, WebSocket handler, diagnostic capture), TypeScript/React (config UI, metrics panel, WebSocket consumer)

**Design doc:** `docs/specs/2026-02-24-io-workspace-telemetry-design.md`

---

### Task 1: In-Memory Collector — `IOWorkspaceTelemetry`

**Files:**

- Create: `internal/workspace/io_workspace_telemetry.go`
- Create: `internal/workspace/io_workspace_telemetry_test.go`

Mirror: `internal/workspace/refresh_telemetry.go` (284 lines)

**Step 1: Write the failing test**

Test file: `internal/workspace/io_workspace_telemetry_test.go`

```go
package workspace

import (
	"testing"
	"time"
)

func TestIOWorkspaceTelemetry_NilSafe(t *testing.T) {
	var tel *IOWorkspaceTelemetry
	// All methods must be safe on nil receiver
	tel.RecordCommand("git", []string{"status"}, "ws-1", "/tmp", RefreshTriggerPoller, 100*time.Millisecond, 0, 500, 0)
	tel.Reset()
	snap := tel.Snapshot(false)
	if snap.TotalCommands != 0 {
		t.Fatalf("expected 0 total commands on nil, got %d", snap.TotalCommands)
	}
}

func TestIOWorkspaceTelemetry_RecordAndSnapshot(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()
	tel.RecordCommand("git", []string{"status", "--porcelain"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 50*time.Millisecond, 0, 200, 0)
	tel.RecordCommand("git", []string{"fetch"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 200*time.Millisecond, 0, 1000, 50)
	tel.RecordCommand("git", []string{"status", "--porcelain"}, "ws-2", "/tmp/ws2", RefreshTriggerWatcher, 30*time.Millisecond, 0, 100, 0)

	snap := tel.Snapshot(false)

	if snap.TotalCommands != 3 {
		t.Fatalf("expected 3 total commands, got %d", snap.TotalCommands)
	}
	if len(snap.Counters) == 0 {
		t.Fatal("expected non-empty counters")
	}
	if snap.Counters["git_status"] != 2 {
		t.Fatalf("expected 2 git_status, got %d", snap.Counters["git_status"])
	}
	if snap.Counters["git_fetch"] != 1 {
		t.Fatalf("expected 1 git_fetch, got %d", snap.Counters["git_fetch"])
	}
	if snap.TriggerCounts["poller"] != 2 {
		t.Fatalf("expected 2 poller triggers, got %d", snap.TriggerCounts["poller"])
	}
	if len(snap.SpanDurations) == 0 {
		t.Fatal("expected non-empty span durations")
	}
}

func TestIOWorkspaceTelemetry_SlowRing(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()
	// Record a command above the slow threshold (100ms)
	tel.RecordCommand("git", []string{"fetch"}, "ws-1", "/tmp", RefreshTriggerPoller, 150*time.Millisecond, 0, 500, 0)
	// Record one below
	tel.RecordCommand("git", []string{"show-ref"}, "ws-1", "/tmp", RefreshTriggerPoller, 5*time.Millisecond, 0, 50, 0)

	snap := tel.Snapshot(false)
	if len(snap.SlowCommands) != 1 {
		t.Fatalf("expected 1 slow command, got %d", len(snap.SlowCommands))
	}
	if snap.SlowCommands[0].Command != "git fetch" {
		t.Fatalf("expected 'git fetch', got %q", snap.SlowCommands[0].Command)
	}
}

func TestIOWorkspaceTelemetry_FullRing(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()
	// Record a command that goes into the full ring
	tel.RecordCommand("git", []string{"status"}, "ws-1", "/tmp", RefreshTriggerPoller, 10*time.Millisecond, 0, 100, 0)

	snap := tel.Snapshot(false)
	if len(snap.AllCommands) == 0 {
		t.Fatal("expected non-empty all commands ring")
	}
}

func TestIOWorkspaceTelemetry_SnapshotReset(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()
	tel.RecordCommand("git", []string{"status"}, "ws-1", "/tmp", RefreshTriggerPoller, 50*time.Millisecond, 0, 200, 0)

	snap := tel.Snapshot(true) // reset=true
	if snap.TotalCommands != 1 {
		t.Fatalf("expected 1, got %d", snap.TotalCommands)
	}

	snap2 := tel.Snapshot(false)
	if snap2.TotalCommands != 0 {
		t.Fatalf("expected 0 after reset, got %d", snap2.TotalCommands)
	}
}

func TestIOWorkspaceTelemetry_ByWorkspace(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()
	tel.RecordCommand("git", []string{"fetch"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 100*time.Millisecond, 0, 500, 0)
	tel.RecordCommand("git", []string{"fetch"}, "ws-2", "/tmp/ws2", RefreshTriggerPoller, 200*time.Millisecond, 0, 500, 0)

	snap := tel.Snapshot(false)
	if len(snap.ByWorkspaceSpans) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(snap.ByWorkspaceSpans))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/workspace/ -run TestIOWorkspaceTelemetry -v`
Expected: FAIL — types not defined

**Step 3: Write implementation**

Create `internal/workspace/io_workspace_telemetry.go`. Mirror `refresh_telemetry.go` exactly — same ring buffer, same aggregation, same nil-safety pattern. Key differences:

- Records full git commands instead of named spans
- Has both a slow command ring (128 entries, >=100ms) and a full command ring (512 entries, all commands)
- Extracts command type from args (e.g. `["status", "--porcelain"]` → `"git_status"`)
- Tracks per-workspace aggregates
- `RecordCommand(bin string, args []string, workspaceID, workingDir string, trigger RefreshTrigger, duration time.Duration, exitCode int, stdoutBytes, stderrBytes int64)`

Snapshot type: `IOWorkspaceTelemetrySnapshot` with fields:

- `SnapshotAt`, `TotalCommands`, `TotalDurationMS`
- `Counters` (map[string]int64 — per command type)
- `TriggerCounts` (map[string]int64)
- `SpanDurations` (map[string]WorkspaceRefreshDurationStats — reuse existing type)
- `ByTriggerSpans`, `ByWorkspaceSpans`
- `SlowCommands` ([]IOWorkspaceCommandEntry — slow ring snapshot)
- `AllCommands` ([]IOWorkspaceCommandEntry — full ring snapshot)
- `RingCapacity`, `SlowRingCapacity`, `SlowThresholdMS`

Command entry type: `IOWorkspaceCommandEntry` with fields matching the design doc table.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/workspace/ -run TestIOWorkspaceTelemetry -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/workspace/io_workspace_telemetry.go internal/workspace/io_workspace_telemetry_test.go
```

Message: `feat(telemetry): add IOWorkspaceTelemetry in-memory collector`

---

### Task 2: Diagnostic Capture — `IOWorkspaceDiagnosticCapture`

**Files:**

- Create: `internal/workspace/io_workspace_diagnostic.go`
- Create: `internal/workspace/io_workspace_diagnostic_test.go`

Mirror: `internal/dashboard/diagnostic.go` (69 lines)

**Step 1: Write the failing test**

```go
package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIOWorkspaceDiagnosticCapture_WriteToDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "test-io-workspace")

	tel := NewIOWorkspaceTelemetry()
	tel.RecordCommand("git", []string{"fetch"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 200*time.Millisecond, 0, 1000, 50)
	tel.RecordCommand("git", []string{"status", "--porcelain"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 50*time.Millisecond, 0, 200, 0)

	snap := tel.Snapshot(false)
	diag := NewIOWorkspaceDiagnosticCapture(snap, time.Now())
	if err := diag.WriteToDir(dir); err != nil {
		t.Fatalf("WriteToDir failed: %v", err)
	}

	// meta.json must exist and be valid JSON
	metaBytes, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("parse meta.json: %v", err)
	}
	if meta["totalCommands"] == nil {
		t.Fatal("meta.json missing totalCommands")
	}
	if meta["findings"] == nil {
		t.Fatal("meta.json missing findings")
	}
	if meta["verdict"] == nil {
		t.Fatal("meta.json missing verdict")
	}

	// commands-ringbuffer.txt must exist
	if _, err := os.Stat(filepath.Join(dir, "commands-ringbuffer.txt")); err != nil {
		t.Fatalf("commands-ringbuffer.txt missing: %v", err)
	}

	// slow-commands.txt must exist
	if _, err := os.Stat(filepath.Join(dir, "slow-commands.txt")); err != nil {
		t.Fatalf("slow-commands.txt missing: %v", err)
	}

	// by-workspace.txt must exist
	if _, err := os.Stat(filepath.Join(dir, "by-workspace.txt")); err != nil {
		t.Fatalf("by-workspace.txt missing: %v", err)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/workspace/ -run TestIOWorkspaceDiagnosticCapture -v`
Expected: FAIL

**Step 3: Write implementation**

Create `internal/workspace/io_workspace_diagnostic.go`:

- `IOWorkspaceDiagnosticCapture` struct holding the snapshot + timestamp
- `NewIOWorkspaceDiagnosticCapture(snap IOWorkspaceTelemetrySnapshot, ts time.Time) *IOWorkspaceDiagnosticCapture`
- `WriteToDir(dir string) error` — mirrors `DiagnosticCapture.WriteToDir` at `internal/dashboard/diagnostic.go:38-69`
  - `os.MkdirAll(dir, 0o755)`
  - Write `meta.json` — snapshot data + `findings` + `verdict` (computed by `computeFindings()`)
  - Write `commands-ringbuffer.txt` — human-readable dump of `AllCommands` ring
  - Write `slow-commands.txt` — human-readable dump of `SlowCommands` ring
  - Write `by-workspace.txt` — per-workspace summary
- `computeFindings(snap)` — returns `([]string, string)` (findings, verdict)
  - Flag command types exceeding 50% of total time
  - Flag watcher/poller overlap
  - Flag disproportionate workspace time
  - Flag commands/sec rate

**Step 4: Run test to verify it passes**

Run: `go test ./internal/workspace/ -run TestIOWorkspaceDiagnosticCapture -v`
Expected: PASS

**Step 5: Commit**

Message: `feat(telemetry): add IOWorkspaceDiagnosticCapture with WriteToDir`

---

### Task 3: Command Instrumentation — `runGit` wrapper

**Files:**

- Modify: `internal/workspace/manager.go` (Manager struct at line 32, add `ioTelemetry` field)
- Create: `internal/workspace/run_git.go`
- Create: `internal/workspace/run_git_test.go`

**Step 1: Write the failing test**

```go
package workspace

import (
	"context"
	"os/exec"
	"testing"
)

func TestRunGit_RecordsTelemetry(t *testing.T) {
	// Create a Manager with IOWorkspaceTelemetry wired
	tel := NewIOWorkspaceTelemetry()
	m := &Manager{}
	m.SetIOWorkspaceTelemetry(tel)

	// Run a real git command that should succeed
	ctx := context.Background()
	_, err := m.runGit(ctx, "ws-test", RefreshTriggerExplicit, t.TempDir(), "version")
	if err != nil {
		// git might not be available in test env, skip
		if _, lookErr := exec.LookPath("git"); lookErr != nil {
			t.Skip("git not available")
		}
		t.Fatalf("runGit failed: %v", err)
	}

	snap := tel.Snapshot(false)
	if snap.TotalCommands != 1 {
		t.Fatalf("expected 1 command recorded, got %d", snap.TotalCommands)
	}
	if snap.Counters["git_version"] != 1 {
		t.Fatalf("expected git_version counter = 1, got %d", snap.Counters["git_version"])
	}
}

func TestRunGit_NilTelemetry(t *testing.T) {
	// runGit must work fine when telemetry is nil
	m := &Manager{}

	ctx := context.Background()
	_, err := m.runGit(ctx, "ws-test", RefreshTriggerExplicit, t.TempDir(), "version")
	if err != nil {
		if _, lookErr := exec.LookPath("git"); lookErr != nil {
			t.Skip("git not available")
		}
		t.Fatalf("runGit failed: %v", err)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/workspace/ -run TestRunGit -v`
Expected: FAIL

**Step 3: Write implementation**

Create `internal/workspace/run_git.go`:

```go
package workspace

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// runGit executes a git command and records telemetry if enabled.
// This is the instrumented replacement for raw exec.CommandContext(ctx, "git", args...).
func (m *Manager) runGit(ctx context.Context, workspaceID string, trigger RefreshTrigger, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	start := time.Now()
	out, err := cmd.CombinedOutput()
	duration := time.Since(start)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	if m.ioTelemetry != nil {
		m.ioTelemetry.RecordCommand("git", args, workspaceID, dir, trigger, duration, exitCode, int64(len(out)), 0)
	}

	return out, err
}
```

Add to Manager struct in `manager.go:32`:

```go
ioTelemetry *IOWorkspaceTelemetry
```

Add setter method:

```go
func (m *Manager) SetIOWorkspaceTelemetry(tel *IOWorkspaceTelemetry) {
	m.ioTelemetry = tel
}
```

Add snapshot method (for the WebSocket handler to call):

```go
func (m *Manager) IOWorkspaceTelemetrySnapshot(reset bool) IOWorkspaceTelemetrySnapshot {
	return m.ioTelemetry.Snapshot(reset)
}
```

**Note:** `runGit` uses `CombinedOutput` for simplicity. Some existing call sites use separate stdout/stderr — adjust `runGit` to support both patterns (a `runGitSeparate` variant may be needed, or use `cmd.Output()` and capture stderr length from `ExitError.Stderr`).

**Step 4: Run test to verify it passes**

Run: `go test ./internal/workspace/ -run TestRunGit -v`
Expected: PASS

**Step 5: Commit**

Message: `feat(telemetry): add runGit instrumented command wrapper`

---

### Task 4: Refactor call sites to use `runGit`

**Files:**

- Modify: `internal/workspace/git.go` — bulk of exec.CommandContext calls (lines 95, 127, 134, 141, 148, 161, 182, 192, 202, 214, 222, 238, 251, 265, 294, 322, 337, 359, 385, 404, 432, 438, 450, 468, 493, 583)
- Modify: `internal/workspace/origin_queries.go` — lines 94, 101, 127, 138, 152, 161, 170, 181, 193, 298, 364, 373, 419
- Modify: `internal/workspace/worktree.go` — lines 21, 29, 106, 112, 123, 128, 153, 197, 204, 215, 221, 230, 250, 275, 292, 314, 321, 327, 334, 341, 356

**This is a mechanical refactoring task.** Each `exec.CommandContext(ctx, "git", args...)` in the workspace package gets replaced with `m.runGit(ctx, workspaceID, trigger, dir, args...)`.

**Step 1: Refactor `git.go` call sites**

For each function, identify the workspaceID and trigger from context. Functions called from the poller loop use `RefreshTriggerPoller`. Functions called from the watcher use `RefreshTriggerWatcher`. Functions called from API handlers use `RefreshTriggerExplicit`.

Some functions are standalone (not on Manager). These either:

- Get refactored to accept `*Manager` or an `ioTelemetry` parameter
- Or stay as-is with no telemetry (if they're rarely called or not in the hot path)

Focus on the hot path first: `gitStatusWithTrigger`, `UpdateGitStatus`, `fetchOrigin`.

**Step 2: Run full test suite**

Run: `go test ./internal/workspace/ -v`
Expected: PASS — behavior unchanged, just instrumented

**Step 3: Refactor `origin_queries.go` call sites**

Same pattern. The `EnsureOriginQueries` and `FetchOriginQueries` functions are called from the poller loop.

**Step 4: Refactor `worktree.go` call sites**

Same pattern. Many of these are one-time operations (workspace creation) so less critical, but instrument them anyway for completeness.

**Step 5: Run full test suite again**

Run: `go test ./internal/workspace/ -v`
Expected: PASS

**Step 6: Commit**

Message: `refactor(workspace): replace raw exec.Command with instrumented runGit`

---

### Task 5: Config fields

**Files:**

- Modify: `internal/config/config.go` — add fields near desync (line 87), add getter methods near desync getters (lines 873-891)
- Modify: `internal/api/contracts/config.go` — add types near `Desync` (line 140)
- Modify: `internal/config/config_test.go` — add test for new getters

Mirror: `Desync` / `DesyncConfig` pattern exactly.

**Step 1: Write failing test**

In `internal/config/config_test.go`, add:

```go
func TestIOWorkspaceTelemetryDefaults(t *testing.T) {
	cfg := &Config{}
	if cfg.GetIOWorkspaceTelemetryEnabled() {
		t.Fatal("expected default false")
	}
	if cfg.GetIOWorkspaceTelemetryTarget() != "" {
		t.Fatal("expected default empty target")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestIOWorkspaceTelemetry -v`
Expected: FAIL

**Step 3: Write implementation**

In `internal/config/config.go`:

Add struct near line 87 (next to `Desync`):

```go
IOWorkspaceTelemetry *IOWorkspaceTelemetryConfig `json:"io_workspace_telemetry,omitempty"`
```

Add config struct near `DesyncConfig` (line 215):

```go
type IOWorkspaceTelemetryConfig struct {
	Enabled *bool  `json:"enabled,omitempty"`
	Target  string `json:"target,omitempty"`
}
```

Add getter methods near desync getters (line 873):

```go
func (c *Config) GetIOWorkspaceTelemetryEnabled() bool {
	if c == nil || c.IOWorkspaceTelemetry == nil || c.IOWorkspaceTelemetry.Enabled == nil {
		return false
	}
	return *c.IOWorkspaceTelemetry.Enabled
}

func (c *Config) GetIOWorkspaceTelemetryTarget() string {
	if c == nil || c.IOWorkspaceTelemetry == nil {
		return ""
	}
	return c.IOWorkspaceTelemetry.Target
}
```

In `internal/api/contracts/config.go`, near `Desync` (line 140):

```go
type IOWorkspaceTelemetry struct {
	Enabled bool   `json:"enabled"`
	Target  string `json:"target"`
}

type IOWorkspaceTelemetryUpdate struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Target  *string `json:"target,omitempty"`
}
```

Add fields to `ConfigResponse` and `ConfigUpdateRequest` (mirror how `Desync`/`DesyncUpdate` are included).

**Step 4: Regenerate TypeScript types**

Run: `go run ./cmd/gen-types`

**Step 5: Run tests**

Run: `go test ./internal/config/ -run TestIOWorkspaceTelemetry -v`
Expected: PASS

**Step 6: Commit**

Message: `feat(config): add io_workspace_telemetry config fields`

---

### Task 6: Daemon wiring

**Files:**

- Modify: `internal/daemon/daemon.go` — near RefreshTelemetry wiring and poller loop (lines 868-896)
- Modify: `internal/workspace/manager.go` — already has `SetIOWorkspaceTelemetry` from Task 3

Mirror: how `RefreshTelemetry` is created and wired in the daemon.

**Step 1: Wire IOWorkspaceTelemetry in daemon**

In `daemon.go`, after workspace manager creation and before the poller loop:

```go
if cfg.GetIOWorkspaceTelemetryEnabled() {
	ioTel := workspace.NewIOWorkspaceTelemetry()
	wm.SetIOWorkspaceTelemetry(ioTel)
}
```

**Step 2: Build and verify**

Run: `go build ./cmd/schmux`
Expected: compiles

**Step 3: Commit**

Message: `feat(daemon): wire IOWorkspaceTelemetry when config enabled`

---

### Task 7: WebSocket handler — capture trigger and stats ticker

**Files:**

- Modify: `internal/dashboard/websocket.go` — add `"io-workspace-diagnostic"` case near `"diagnostic"` case (line 566), add stats ticker near existing stats ticker (line 482)
- Modify: `internal/dashboard/server.go` — ensure workspace manager is accessible from WebSocket handler

Mirror: the `"diagnostic"` case (lines 566-622) and the `statsTickerC` case (lines 482-505) in `websocket.go`.

**Step 1: Add `"io-workspace-diagnostic"` WebSocket message handler**

In `websocket.go`, after the `case "diagnostic":` block (line 622), add:

```go
case "io-workspace-diagnostic":
	ioProvider, ok := s.workspace.(ioWorkspaceTelemetryProvider)
	if !ok {
		break
	}
	snap := ioProvider.IOWorkspaceTelemetrySnapshot(false)
	diag := workspace.NewIOWorkspaceDiagnosticCapture(snap, time.Now())
	diagDir := filepath.Join(os.Getenv("HOME"), ".schmux", "diagnostics",
		fmt.Sprintf("%s-io-workspace", time.Now().Format("2006-01-02T15-04-05")))
	if err := diag.WriteToDir(diagDir); err != nil {
		logging.Sub(s.logger, "io-workspace-diagnostic").Error("write failed", "err", err)
		break
	}
	resp := map[string]interface{}{
		"type":     "io-workspace-diagnostic",
		"diagDir":  diagDir,
		"counters": snap.Counters,
		"findings": diag.Findings,
		"verdict":  diag.Verdict,
	}
	data, _ := json.Marshal(resp)
	conn.WriteMessage(websocket.TextMessage, data)
```

Add the provider interface (near `workspaceRefreshTelemetryProvider` in `handlers_workspace_refresh_telemetry.go`):

```go
type ioWorkspaceTelemetryProvider interface {
	IOWorkspaceTelemetrySnapshot(reset bool) workspace.IOWorkspaceTelemetrySnapshot
}
```

**Step 2: Add IO workspace stats to the stats ticker**

In the `<-statsTickerC:` case (line 482), after sending `WSStatsMessage`, also send IO workspace stats if telemetry is enabled:

```go
if ioProvider, ok := s.workspace.(ioWorkspaceTelemetryProvider); ok {
	ioSnap := ioProvider.IOWorkspaceTelemetrySnapshot(false)
	ioStatsMsg := map[string]interface{}{
		"type":            "io-workspace-stats",
		"totalCommands":   ioSnap.TotalCommands,
		"totalDurationMs": ioSnap.TotalDurationMS,
		"triggerCounts":   ioSnap.TriggerCounts,
		"counters":        ioSnap.Counters,
	}
	data, _ := json.Marshal(ioStatsMsg)
	conn.WriteMessage(websocket.TextMessage, data)
}
```

**Step 3: Build and verify**

Run: `go build ./cmd/schmux`
Expected: compiles

**Step 4: Commit**

Message: `feat(websocket): add io-workspace-diagnostic capture and stats broadcast`

---

### Task 8: Config UI — AdvancedTab section

**Files:**

- Modify: `assets/dashboard/src/routes/config/AdvancedTab.tsx` — add section after "Terminal Desync Diagnostics" (line 276)
- Modify: `assets/dashboard/src/routes/config/AdvancedTab.test.tsx` — add props
- Modify: `assets/dashboard/src/routes/config/useConfigForm.ts` — add state fields near desync (lines 172-173, 304-305, 535-536, 631-632)
- Modify: `assets/dashboard/src/routes/ConfigPage.tsx` — wire new fields

Mirror: the "Terminal Desync Diagnostics" section exactly (lines 230-276 of AdvancedTab.tsx).

**Step 1: Add state fields to `useConfigForm.ts`**

Near `desyncEnabled`/`desyncTarget` in all locations:

- `ConfigSnapshot` (line 59-60): add `ioWorkspaceTelemetryEnabled: boolean; ioWorkspaceTelemetryTarget: string;`
- `ConfigFormState` (line 172-173): add same
- `initialState` (line 304-305): add `ioWorkspaceTelemetryEnabled: false, ioWorkspaceTelemetryTarget: '',`
- `hasChanges` (line 535-536): add comparison
- Snapshot return (line 631-632): add mapping

**Step 2: Add UI section to AdvancedTab**

After the "Terminal Desync Diagnostics" section (line 276), add:

```tsx
<div className="settings-section">
  <div className="settings-section__header">
    <h3 className="settings-section__title">IO Workspace Telemetry</h3>
  </div>
  <div className="settings-section__body">
    <div className="form-group">
      <label
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--spacing-xs)',
          cursor: 'pointer',
        }}
      >
        <input
          type="checkbox"
          checked={ioWorkspaceTelemetryEnabled}
          onChange={(e) => setField('ioWorkspaceTelemetryEnabled', e.target.checked)}
        />
        Enable IO workspace telemetry
      </label>
      <p className="form-group__hint">
        When enabled, workspace git operations are instrumented with timing telemetry.
      </p>
    </div>
    <div className="form-group">
      <label className="form-group__label">Target</label>
      <TargetSelect
        value={ioWorkspaceTelemetryTarget}
        onChange={(v) => setField('ioWorkspaceTelemetryTarget', v)}
        disabled={!ioWorkspaceTelemetryEnabled}
        includeDisabledOption={false}
        includeNoneOption="None (capture only)"
        detectedTargets={detectedTargets}
        models={models}
        promptableTargets={promptableTargets}
      />
      <p className="form-group__hint">
        When a target is selected, a diagnostic capture will automatically spawn an agent session to
        analyze the captured data.
      </p>
    </div>
  </div>
</div>
```

**Step 3: Wire in ConfigPage.tsx**

Mirror how `desyncEnabled`/`desyncTarget` are mapped from `data.desync?.enabled` to form state and back to `ConfigUpdateRequest`.

**Step 4: Add props to AdvancedTab**

Add `ioWorkspaceTelemetryEnabled` and `ioWorkspaceTelemetryTarget` to `AdvancedTabProps`.

**Step 5: Run frontend tests**

Run: `./test.sh` (includes vitest)
Expected: PASS (update test fixtures as needed for new props)

**Step 6: Commit**

Message: `feat(dashboard): add IO workspace telemetry toggle in AdvancedTab`

---

### Task 9: Frontend — Live Metrics Panel

**Files:**

- Create: `assets/dashboard/src/components/IOWorkspaceMetricsPanel.tsx`
- Create: `assets/dashboard/src/components/IOWorkspaceMetricsPanel.test.tsx`
- Modify: `assets/dashboard/src/routes/SessionDetailPage.tsx` — render panel near `StreamMetricsPanel` (line 626)
- Modify: `assets/dashboard/src/lib/terminalStream.ts` — handle `"io-workspace-stats"` and `"io-workspace-diagnostic"` messages

Mirror: `StreamMetricsPanel` (lines 1-435) and the diagnostic flow in `SessionDetailPage.tsx` (lines 213-266).

**Step 1: Create `IOWorkspaceMetricsPanel`**

Mirror `StreamMetricsPanel` structure:

- Props: `stats` (from WebSocket `io-workspace-stats` messages), `onCapture` callback
- Collapsed pill: total commands, total duration, commands/sec
- Expanded dropdown: full breakdown
- "Capture" button that calls `onCapture`

**Step 2: Add WebSocket message handling in `terminalStream.ts`**

Near the existing `case 'stats':` (line 554) and `case 'diagnostic':` (line 558):

```typescript
case 'io-workspace-stats':
  this.latestIOWorkspaceStats = msg;
  this.onIOWorkspaceStatsUpdate?.(msg);
  break;
case 'io-workspace-diagnostic':
  this.onIOWorkspaceDiagnosticResponse?.(msg);
  break;
```

Add `sendIOWorkspaceDiagnostic()` method (mirrors `sendDiagnostic()`):

```typescript
sendIOWorkspaceDiagnostic(): void {
  if (this.ws?.readyState === WebSocket.OPEN) {
    this.ws.send(JSON.stringify({ type: 'io-workspace-diagnostic' }));
  }
}
```

**Step 3: Render in SessionDetailPage**

Near where `StreamMetricsPanel` is rendered (line 626):

```tsx
{
  config.io_workspace_telemetry?.enabled && (
    <IOWorkspaceMetricsPanel
      stats={ioWorkspaceStats}
      onCapture={() => terminalStreamRef.current?.sendIOWorkspaceDiagnostic()}
    />
  );
}
```

Add the auto-spawn agent flow (mirrors lines 213-266) for `io-workspace-diagnostic` response — same pattern: get target from config, find workspace, build prompt pointing at `diagDir`, spawn session.

**Step 4: Run frontend tests**

Run: `./test.sh`
Expected: PASS

**Step 5: Commit**

Message: `feat(dashboard): add IOWorkspaceMetricsPanel with live stats and capture`

---

### Task 10: Build dashboard and integration test

**Files:**

- None new — integration verification

**Step 1: Build dashboard**

Run: `go run ./cmd/build-dashboard`
Expected: builds successfully

**Step 2: Build Go binary**

Run: `go build ./cmd/schmux`
Expected: compiles

**Step 3: Run full test suite**

Run: `./test.sh`
Expected: PASS

**Step 4: Manual smoke test**

1. Start daemon with `io_workspace_telemetry.enabled: true` in config
2. Open dashboard, go to a session
3. Verify `IOWorkspaceMetricsPanel` appears
4. Verify live stats update every 3 seconds
5. Click "Capture"
6. Verify diagnostic directory created in `~/.schmux/diagnostics/`
7. Verify `meta.json` contains expected data

**Step 5: Commit**

Message: `chore: verify io-workspace telemetry end-to-end`
