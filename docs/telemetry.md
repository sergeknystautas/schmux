# Telemetry

schmux has two independent telemetry systems: **PostHog telemetry** sends anonymous product usage events to PostHog for understanding how the tool is used, and **IO workspace telemetry** instruments local git command execution for diagnosing performance bottlenecks.

---

## PostHog Telemetry

### What it does

Sends anonymous usage events to PostHog via their HTTP API. Telemetry is enabled by default with opt-out available. Events are non-blocking (enqueued and sent by a background worker) with at-most-once delivery guarantees.

### Key files

| File                                   | Purpose                                                                                              |
| -------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `internal/telemetry/telemetry.go`      | PostHog client: `Telemetry` interface, `Client` (background worker), `NoopTelemetry` (disabled mode) |
| `internal/telemetry/telemetry_test.go` | Unit tests for client lifecycle, event queuing, failure handling                                     |
| `internal/daemon/daemon.go`            | Init telemetry, ensure `installation_id` in config                                                   |
| `internal/workspace/manager.go`        | Tracks `workspace_created` events                                                                    |
| `internal/session/manager.go`          | Tracks `session_created` events                                                                      |
| `internal/workspace/linear_sync.go`    | Tracks `push_to_main` events                                                                         |

### Architecture decisions

- **Why PostHog HTTP API directly instead of the Go SDK:** Avoids adding a dependency. The capture API is a single POST endpoint.
- **Why a bounded queue with a single worker:** The 100-event channel with one goroutine prevents unbounded memory growth and serializes HTTP calls. If the queue fills, the oldest event is dropped. This means `Track()` is always <1ms and never blocks the caller.
- **Why `Telemetry` interface instead of package globals:** Managers receive the interface via constructor injection. This allows `NoopTelemetry` when disabled and straightforward test mocking.
- **Why at-most-once delivery:** No retry on failure. Telemetry is best-effort; retries would add complexity and latency for data that is not critical.
- **Why the API key is hardcoded:** It is a write-only public key that only allows sending events. It is safe to commit to source. All builds (release binaries, local dev, `go install`) send telemetry.

### Events tracked

| Event               | When                             | Properties                                 |
| ------------------- | -------------------------------- | ------------------------------------------ |
| `daemon_started`    | Daemon starts                    | `version`                                  |
| `workspace_created` | Any workspace creation path      | `workspace_id`, `repo_host`, `branch`      |
| `session_created`   | Any session spawn path           | `session_id`, `workspace_id`, `target`     |
| `push_to_main`      | `LinearSyncToDefault()` succeeds | `workspace_id`, `branch`, `default_branch` |

### Privacy guarantees

Only these properties are sent. No repository names, URLs, file paths, code content, prompt content, or personally identifying information.

| Property         | Source                  | Example                  |
| ---------------- | ----------------------- | ------------------------ |
| `version`        | Binary version          | `1.2.3`                  |
| `workspace_id`   | Workspace.ID            | `myproject-001`          |
| `session_id`     | Session.ID              | `myproject-001-a1b2c3d4` |
| `repo_host`      | Extracted from repo URL | `github.com`             |
| `branch`         | Workspace.Branch        | `feature/xyz`            |
| `target`         | Session target/agent    | `claude`                 |
| `default_branch` | Git default branch      | `main`                   |

Each installation is assigned a random UUID (`installation_id`) stored in `~/.schmux/config.json`. This ID is not linked to any personal information.

### How to opt out

Set `telemetry_enabled` to `false` in `~/.schmux/config.json`:

```json
{
  "telemetry_enabled": false
}
```

Environment variables `SCHMUX_TELEMETRY_OFF` or `DO_NOT_TRACK` (any non-empty value) also disable telemetry.

### Gotchas

- Failure logging is rate-limited to 1 message per minute to avoid log spam during network outages.
- Shutdown flushes pending events with a 5-second timeout. Events still in the queue after that are dropped.
- The `installation_id` is generated on first run if missing and persisted in config. It survives upgrades.

### Common modification patterns

- **Add a new event:** Call `telemetry.Track("event_name", map[string]any{...})` at the appropriate callsite. Add the event to the privacy allowlist documentation.
- **Change the PostHog endpoint:** Override `posthogEndpoint` in tests. The default is `https://us.posthog.com/capture/`.

### Configuration

```json
{
  "telemetry_enabled": true,
  "installation_id": "uuid-v4-here"
}
```

| Field               | Default        | Description                                                   |
| ------------------- | -------------- | ------------------------------------------------------------- |
| `telemetry_enabled` | `true`         | Set to `false` to disable all tracking.                       |
| `installation_id`   | auto-generated | UUID v4, created on first run, used as PostHog `distinct_id`. |

### Data retention

Events are sent to PostHog and retained according to their standard retention policies. The data is not shared with third parties.

---

## IO Workspace Telemetry

### What it does

A toggleable diagnostic harness that instruments `exec.Command` calls for git operations in the workspace package. Captures timing, byte counts, and exit codes. Writes diagnostic captures to `~/.schmux/diagnostics/` for both human and AI analysis. Mirrors the terminal desync diagnostic system in structure and workflow.

### Key files

| File                                                 | Purpose                                                                                                                                                     |
| ---------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/workspace/io_workspace_telemetry.go`       | In-memory collector: mutex-protected counters, per-command/trigger/workspace aggregates, slow and full command ring buffers                                 |
| `internal/workspace/io_workspace_telemetry_test.go`  | Unit tests for recording, snapshots, ring buffer behavior                                                                                                   |
| `internal/workspace/io_workspace_diagnostic.go`      | Diagnostic capture: `WriteToDir()` produces `meta.json`, `commands-ringbuffer.txt`, `slow-commands.txt`, `by-workspace.txt`; automated findings and verdict |
| `internal/workspace/io_workspace_diagnostic_test.go` | Tests for diagnostic file output and automated findings                                                                                                     |
| `internal/workspace/run_git.go`                      | `runGit()` -- instrumented wrapper around `exec.CommandContext(ctx, "git", ...)`, records to telemetry collector                                            |

### Architecture decisions

- **Why mirror the terminal desync diagnostic system:** Both systems follow the same shape: in-memory collector, diagnostic capture with `WriteToDir()`, live metrics panel in the dashboard, WebSocket message to trigger capture, config toggle + target selector for auto-analysis. This makes both systems predictable for developers who know one.
- **Why nil-safe methods:** All `IOWorkspaceTelemetry` methods are no-ops on nil receiver. This eliminates nil checks at every callsite -- callers pass the collector around and call methods without checking if telemetry is enabled.
- **Why lazy initialization in `runGit()`:** If the config has telemetry enabled but no collector has been set via `SetIOWorkspaceTelemetry()`, `runGit()` lazily creates one. This supports hot-reloading the config toggle without a daemon restart.
- **Why two ring buffers:** The slow ring (128 entries, threshold >= 100ms) captures only slow commands for focused analysis. The full ring (512 entries) captures all recent commands for context.

### Data collected

Each recorded command captures:

| Field          | Type    | Description                                       |
| -------------- | ------- | ------------------------------------------------- |
| `ts`           | string  | RFC3339Nano timestamp                             |
| `command`      | string  | Full git command (e.g., `git status --porcelain`) |
| `workspace_id` | string  | Workspace ID                                      |
| `working_dir`  | string  | Working directory                                 |
| `trigger`      | string  | `poller`, `watcher`, or `explicit`                |
| `duration_ms`  | float64 | Execution time in milliseconds                    |
| `exit_code`    | int     | Process exit code                                 |
| `stdout_bytes` | int64   | Bytes on stdout                                   |
| `stderr_bytes` | int64   | Bytes on stderr                                   |

Aggregate statistics are maintained per command type (e.g., `git_status`, `git_fetch`), per trigger, and per workspace.

### Diagnostic capture

Triggered by sending an `"io-workspace-diagnostic"` message on the terminal WebSocket. Produces a directory under `~/.schmux/diagnostics/{timestamp}-io-workspace/` containing:

- **`meta.json`** -- Structured summary: timestamp, total commands, total duration, counters, trigger counts, span durations, by-trigger/by-workspace breakdowns, automated findings, verdict
- **`commands-ringbuffer.txt`** -- Human-readable dump of all recent commands from the full ring buffer
- **`slow-commands.txt`** -- Human-readable dump of slow commands (>= 100ms) from the slow ring buffer
- **`by-workspace.txt`** -- Per-workspace summary with top 5 slowest command types

### Automated findings

Computed at capture time:

- Flags if any single command type exceeds 50% of total time
- Flags if watcher-triggered and poller-triggered commands overlap (duplicate work)
- Flags if any workspace accounts for a disproportionate share of total time
- Reports the command rate (commands/sec) and flags if it exceeds 10/sec
- Verdict summarizes the dominant pattern

### Analysis workflow

1. Toggle "Enable IO workspace telemetry" in the AdvancedTab of config UI
2. Optionally select a promptable target for auto-analysis
3. Let the system run under normal load
4. Click "Capture" in the live metrics panel (sends WebSocket message)
5. System writes diagnostic directory, returns findings and verdict over WebSocket
6. If a target is configured, an agent session auto-spawns to analyze `meta.json`
7. Make changes, repeat, compare captures

### Gotchas

- The `runGit()` wrapper suppresses git watcher events for the duration of each command to prevent the watcher from triggering redundant refreshes caused by schmux's own git operations.
- `extractCommandType()` derives the key from the first arg only (e.g., `["status", "--porcelain"]` becomes `git_status`). Subcommands are not distinguished.
- The lazy initialization path in `runGit()` uses a package-level mutex (`ioTelemetryMu`) separate from the Manager's fields to avoid holding the Manager lock during telemetry creation.
- Diagnostic captures do not reset the telemetry by default. Pass `reset: true` to `Snapshot()` to clear counters after capture.

### Common modification patterns

- **Instrument a new command type:** Existing git commands in the workspace package already go through `runGit()`. If you add a new `exec.CommandContext(ctx, "git", ...)` call, replace it with `m.runGit(ctx, workspaceID, trigger, dir, args...)`.
- **Add a new finding rule:** Edit `computeFindings()` in `internal/workspace/io_workspace_diagnostic.go`.
- **Change ring buffer sizes or slow threshold:** Modify the constants `ioSlowRingCapacity` (128), `ioFullRingCapacity` (512), and `ioSlowThresholdMS` (100.0) in `internal/workspace/io_workspace_telemetry.go`.
- **Add a new aggregate dimension:** Add a new map field to `IOWorkspaceTelemetry`, update `RecordCommand()` and `Snapshot()`, and include it in `IOWorkspaceDiagnosticCapture.WriteToDir()`.

### Configuration

```json
{
  "io_workspace_telemetry_enabled": false,
  "io_workspace_telemetry_target": ""
}
```

| Field                            | Default | Description                                                    |
| -------------------------------- | ------- | -------------------------------------------------------------- |
| `io_workspace_telemetry_enabled` | `false` | Enable/disable git command instrumentation. Hot-reloadable.    |
| `io_workspace_telemetry_target`  | `""`    | Promptable target for auto-analysis. Empty means capture only. |
