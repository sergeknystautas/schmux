# IO Workspace Telemetry — Design

## Purpose

Lightweight, toggleable telemetry harness that instruments `exec.Command` calls in the workspace package. Captures timing, byte counts, and exit codes. Writes diagnostic captures to `~/.schmux/diagnostics/` in the same format as the terminal diagnostic system. Produces data that both humans and AI can analyze to identify IO bottlenecks.

## Mirror Target

This system mirrors the terminal desync diagnostic system end-to-end:

| Terminal Desync Diagnostic                                                           | IO Workspace Telemetry                                                                                |
| ------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------- |
| `RefreshTelemetry` — in-memory collector (`internal/workspace/refresh_telemetry.go`) | `IOWorkspaceTelemetry` — in-memory collector                                                          |
| `DiagnosticCapture` + `WriteToDir` (`internal/dashboard/diagnostic.go`)              | `IOWorkspaceDiagnosticCapture` + `WriteToDir`                                                         |
| `StreamMetricsPanel` — live metrics panel with "Diagnose" button                     | `IOWorkspaceMetricsPanel` — live metrics panel with "Capture" button                                  |
| `StreamDiagnostics` — frontend diagnostics class                                     | Frontend stats consumer for IO workspace                                                              |
| `WSStatsMessage` — periodic stats over terminal WebSocket                            | `WSIOWorkspaceStatsMessage` — periodic stats over terminal WebSocket                                  |
| `"diagnostic"` WebSocket message triggers capture                                    | `"io-workspace-diagnostic"` WebSocket message triggers capture                                        |
| Config: `desyncEnabled` checkbox + `desyncTarget` selector in AdvancedTab            | Config: `ioWorkspaceTelemetryEnabled` checkbox + `ioWorkspaceTelemetryTarget` selector in AdvancedTab |
| Capture auto-spawns agent session to analyze when target is set                      | Capture auto-spawns agent session to analyze `meta.json` when target is set                           |

## Architecture

### 1. In-Memory Collector: `IOWorkspaceTelemetry`

Lives in `internal/workspace/`. Same shape as `RefreshTelemetry`:

- Mutex-protected
- Nil-safe (all methods no-op on nil receiver)
- Counters: per-command-type counts (e.g. `git_fetch`, `git_status`, `git_merge_base`)
- Trigger counts: `poller` / `watcher` / `explicit`
- Span aggregates: count, total_ms, max_ms, avg_ms — keyed by command type
- By-trigger span breakdown
- By-workspace span breakdown
- Slow command ring buffer (128 entries, threshold 100ms)
- Full command ring buffer (all recent commands, not just slow ones — mirrors the 256KB ring buffer in `StreamDiagnostics`)

Each slow command entry:

| Field          | Type    | Description                                      |
| -------------- | ------- | ------------------------------------------------ |
| `ts`           | string  | RFC3339Nano                                      |
| `command`      | string  | Full git command (e.g. `git status --porcelain`) |
| `workspace_id` | string  | Workspace ID                                     |
| `working_dir`  | string  | Working directory                                |
| `trigger`      | string  | `poller` / `watcher` / `explicit`                |
| `duration_ms`  | float64 | Execution time                                   |
| `exit_code`    | int     | Process exit code                                |
| `stdout_bytes` | int64   | Bytes on stdout                                  |
| `stderr_bytes` | int64   | Bytes on stderr                                  |

### 2. Diagnostic Capture: `IOWorkspaceDiagnosticCapture`

Mirrors `DiagnosticCapture`. Struct with a `WriteToDir(dir string) error` method.

**Directory**: `~/.schmux/diagnostics/{timestamp}-io-workspace/`

**Files** (mirrors `meta.json` + `ringbuffer-backend.txt` + `screen-tmux.txt`):

- `meta.json` — structured summary:
  - `timestamp`
  - `duration_s` (time since last reset)
  - `workspace_count`, `total_commands`, `total_duration_ms`
  - `counters` (per-command-type counts)
  - `trigger_counts`
  - `span_durations` (aggregates per command type)
  - `by_trigger_spans`
  - `by_workspace_spans`
  - `findings` — automated first-pass analysis (list of strings)
  - `verdict` — summary assessment
- `commands-ringbuffer.txt` — full ring buffer of all recent commands (mirrors `ringbuffer-backend.txt`)
- `slow-commands.txt` — human-readable dump of slow command ring buffer entries
- `by-workspace.txt` — human-readable per-workspace summary

**Automated findings** (computed at capture time, mirrors `findings`/`verdict` in terminal diagnostic):

- Flag if any single command type exceeds N% of total time
- Flag if watcher-triggered commands overlap with poller-triggered commands (duplicate work)
- Flag if any workspace accounts for disproportionate share of total time
- Flag total command count per second (death-by-a-thousand-cuts detection)
- Verdict summarizes the dominant pattern

### 3. Capture Trigger: WebSocket Message

Mirrors the terminal diagnostic trigger. The capture is triggered by sending a `"io-workspace-diagnostic"` message on the terminal WebSocket (`/ws/terminal/{id}`), same as the terminal diagnostic uses `"diagnostic"`.

On receiving this message:

1. Snapshot the in-memory `IOWorkspaceTelemetry`
2. Compute `findings` and `verdict`
3. Write diagnostic directory via `WriteToDir`
4. Send response back over WebSocket: `{ "type": "io-workspace-diagnostic", "diagDir": "...", "counters": {...}, "findings": [...], "verdict": "..." }`
5. If `ioWorkspaceTelemetryTarget` is configured, auto-spawn an agent session pointed at the `meta.json` for analysis

### 4. Config UI: AdvancedTab Section

Mirrors the "Terminal Desync Diagnostics" section in AdvancedTab:

- **Checkbox**: "Enable IO workspace telemetry" (`ioWorkspaceTelemetryEnabled`)
- **Target selector**: Pick a promptable target for auto-analysis (`ioWorkspaceTelemetryTarget`), with "None (capture only)" option
- **Hint text**: "When enabled, workspace git operations are instrumented with timing telemetry. When a target is selected, a diagnostic capture will automatically spawn an agent session to analyze the captured data."

Config fields in Go:

- `io_workspace_telemetry_enabled *bool` (default `false`)
- `io_workspace_telemetry_target string` (default `""`)

### 5. Dashboard UI: Live Metrics Panel

Mirrors `StreamMetricsPanel`. Shown when IO workspace telemetry is enabled.

**Collapsed view** (pill):

- Total commands count
- Total duration
- Commands/sec

**Expanded view** (dropdown on click):

- Total commands
- Total duration
- Commands/sec
- Per-trigger breakdown (poller / watcher / explicit)
- Top 5 slowest command types with avg/max
- Workspace count

**"Capture" button** (mirrors "Diagnose" button):

- Sends `"io-workspace-diagnostic"` message over the WebSocket
- Receives response with `diagDir`, `counters`, `findings`, `verdict`
- If target is configured, agent session spawns automatically

### 6. Live Stats over WebSocket

Mirrors `WSStatsMessage` sent every 3 seconds on the terminal WebSocket.

- Periodic `WSIOWorkspaceStatsMessage` sent on the terminal WebSocket when telemetry is enabled
- Message type: `"io-workspace-stats"`
- Fields: `total_commands`, `total_duration_ms`, `commands_per_sec`, `trigger_counts`
- Frontend `IOWorkspaceMetricsPanel` consumes these to update live counters

### 7. Command Instrumentation: `runGit`

Thin wrapper in the workspace package:

```go
func (m *Manager) runGit(ctx context.Context, workspaceID string, trigger RefreshTrigger, dir string, args ...string) ([]byte, error)
```

- Calls `exec.CommandContext(ctx, "git", args...)`
- Times execution
- Measures stdout/stderr byte counts
- Records to `IOWorkspaceTelemetry` (if non-nil)
- Returns `(stdout, error)` — same interface as raw exec

Existing `exec.CommandContext(ctx, "git", ...)` call sites in the workspace package get refactored to use `runGit`.

### 8. Daemon Wiring

Same injection pattern as `RefreshTelemetry`:

1. Check `cfg.GetIOWorkspaceTelemetryEnabled()`
2. If enabled, `ioTel := workspace.NewIOWorkspaceTelemetry()`
3. `wm.SetIOWorkspaceTelemetry(ioTel)`
4. Register WebSocket message handler for `"io-workspace-diagnostic"`
5. Start stats ticker (3s interval) for `WSIOWorkspaceStatsMessage` when telemetry is enabled

## Analysis Workflow

Mirrors the terminal desync diagnostic workflow:

1. Toggle "Enable IO workspace telemetry" in AdvancedTab config UI
2. Optionally select a promptable target for auto-analysis
3. Let the system run under normal load
4. Click "Capture" in the live metrics panel (sends WebSocket message)
5. System writes `~/.schmux/diagnostics/{timestamp}-io-workspace/` with `meta.json`, `commands-ringbuffer.txt`, `slow-commands.txt`, `by-workspace.txt`
6. Response comes back over WebSocket with `diagDir`, `findings`, `verdict`
7. If target is configured, agent session auto-spawns to analyze `meta.json`
8. Make changes, repeat from step 3, compare captures

## Scope

- Workspace package only (`internal/workspace/`)
- Git commands only (the dominant IO source)
- No optimization work — telemetry harness only
