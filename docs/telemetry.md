# Telemetry

schmux has independent telemetry systems: **PostHog telemetry** sends anonymous product usage events to PostHog for understanding how the tool is used; **IO workspace telemetry** instruments local git command execution; and **chat load telemetry** measures chat history delivery, browser rendering, and image loading.

---

## PostHog Telemetry

### What it does

Sends anonymous usage events to PostHog via their HTTP API. PostHog is the default telemetry backend. A second backend, **CommandTelemetry**, can send events to an external command instead (see below). Telemetry is enabled by default with opt-out available. Events are non-blocking (enqueued and sent by a background worker) with at-most-once delivery guarantees.

### Key files

| File                                     | Purpose                                                                         |
| ---------------------------------------- | ------------------------------------------------------------------------------- |
| `internal/telemetry/telemetry.go`        | `Telemetry` interface, `NoopTelemetry` (disabled mode) -- always compiled       |
| `internal/telemetry/posthog.go`          | PostHog `Client` (background worker) -- compiled when `noposthog` tag is absent |
| `internal/telemetry/posthog_disabled.go` | PostHog stubs -- compiled when `noposthog` tag is set                           |
| `internal/telemetry/command.go`          | `CommandTelemetry` -- external command backend, always compiled                 |
| `internal/telemetry/telemetry_test.go`   | Unit tests for PostHog client lifecycle, event queuing, failure handling        |
| `internal/daemon/daemon.go`              | Init telemetry, ensure `installation_id` in config, select backend by priority  |
| `internal/workspace/manager.go`          | Tracks `workspace_created` events                                               |
| `internal/session/manager.go`            | Tracks `session_created` events                                                 |
| `internal/workspace/linear_sync.go`      | Tracks `push_to_main` events                                                    |

### Architecture decisions

- **Why PostHog HTTP API directly instead of the Go SDK:** Avoids adding a dependency. The capture API is a single POST endpoint.
- **Why a bounded queue with a single worker:** The 100-event channel with one goroutine prevents unbounded memory growth and serializes HTTP calls. If the queue fills, the oldest event is dropped. This means `Track()` is always <1ms and never blocks the caller.
- **Why `Telemetry` interface instead of package globals:** Managers receive the interface via constructor injection. This allows `NoopTelemetry` when disabled and straightforward test mocking.
- **Why at-most-once delivery:** No retry on failure. Telemetry is best-effort; retries would add complexity and latency for data that is not critical.
- **Why the API key is hardcoded:** It is a write-only public key that only allows sending events. It is safe to commit to source. All builds (release binaries, local dev, `go install`) send telemetry.
- **Why an external command backend:** `CommandTelemetry` execs a user-configured command per event, writing typed JSON to its stdin. This allows organizations to route telemetry to their own infrastructure without modifying schmux source. The command backend is always compiled (no build tag), so it remains available even when PostHog is excluded via `-tags noposthog`.
- **Why `noposthog` instead of `notelemetry`:** The build tag compiles out only the PostHog client, not the telemetry infrastructure. Core types (`Telemetry` interface, `NoopTelemetry`, `CommandTelemetry`) are always available regardless of build tags.

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

### Gotchas

- Failure logging is rate-limited to 1 message per minute to avoid log spam during network outages.
- Shutdown flushes pending events with a 5-second timeout. Events still in the queue after that are dropped.
- The `installation_id` is generated on first run if missing and persisted in config. It survives upgrades.

### Common modification patterns

- **Add a new event:** Call `telemetry.Track("event_name", map[string]any{...})` at the appropriate callsite. Add the event to the privacy allowlist documentation.
- **Change the PostHog endpoint:** Override `posthogEndpoint` in tests. The default is `https://us.posthog.com/capture/`.

---

## CommandTelemetry (External Command Backend)

### What it does

`CommandTelemetry` sends events to a user-configured external command by writing typed JSON to its stdin. Each `Track()` call spawns the command once, writes the JSON payload, and returns immediately (the child process is reaped asynchronously). This allows organizations to route telemetry to their own infrastructure without modifying schmux source.

### Typed JSON format

Properties are categorized into buckets by Go type:

| Bucket   | Contains                                                          |
| -------- | ----------------------------------------------------------------- |
| `int`    | integers, bools (true=1, false=0), and `time` (Unix timestamp)    |
| `normal` | strings, `event` name, `installation_id`, and any fallback values |
| `double` | float64 values (omitted if empty)                                 |

Example payload written to stdin:

```json
{
  "int": {
    "time": 1712937600
  },
  "normal": {
    "event": "daemon_started",
    "installation_id": "550e8400-e29b-41d4-a716-446655440000",
    "version": "1.2.3"
  }
}
```

The `double` bucket is included only when float64 properties are present.

### Backend priority

The daemon selects the telemetry backend at startup using this priority:

1. **Disabled** -- if `telemetry.enabled` is `false` (or environment kill switch is set), use `NoopTelemetry`.
2. **Command** -- if `telemetry.command` is set, use `CommandTelemetry`.
3. **PostHog** -- if PostHog is compiled in (`-tags noposthog` was NOT used), use the PostHog `Client`.
4. **Noop** -- fallback when PostHog is compiled out and no command is configured.

### Configuration

```json
{
  "telemetry": {
    "enabled": true,
    "command": "my-telemetry-sink"
  },
  "installation_id": "uuid-v4-here"
}
```

| Field               | Default        | Description                                                              |
| ------------------- | -------------- | ------------------------------------------------------------------------ |
| `telemetry.enabled` | `true`         | Set to `false` to disable all tracking.                                  |
| `telemetry.command` | `""`           | External command to exec per event. If set, takes priority over PostHog. |
| `installation_id`   | auto-generated | UUID v4, created on first run, used as PostHog `distinct_id`.            |

Legacy flat field `telemetry_enabled` is migrated automatically to `telemetry.enabled` on load.

### How to opt out

Set `telemetry.enabled` to `false` in `~/.schmux/config.json`:

```json
{
  "telemetry": {
    "enabled": false
  }
}
```

Environment variables `SCHMUX_TELEMETRY_OFF` or `DO_NOT_TRACK` (any non-empty value) also disable telemetry.

### Data retention

Events sent to PostHog are retained according to their standard retention policies. Events sent via CommandTelemetry are handled by whatever infrastructure the configured command routes to. The data is not shared with third parties by schmux itself.

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
| `internal/workspace/run_cmd.go`                      | `runCmd()` -- instrumented wrapper around `exec.CommandContext`, records to telemetry collector. `runGit()` is a thin wrapper that delegates to `runCmd()`  |

### Architecture decisions

- **Why mirror the terminal desync diagnostic system:** Both systems follow the same shape: in-memory collector, diagnostic capture with `WriteToDir()`, live metrics panel in the dashboard, WebSocket message to trigger capture, config toggle + target selector for auto-analysis. This makes both systems predictable for developers who know one.
- **Why nil-safe methods:** All `IOWorkspaceTelemetry` methods are no-ops on nil receiver. This eliminates nil checks at every callsite -- callers pass the collector around and call methods without checking if telemetry is enabled.
- **Why lazy initialization in `runCmd()`:** If the config has telemetry enabled but no collector has been set via `SetIOWorkspaceTelemetry()`, `runCmd()` lazily creates one. This supports hot-reloading the config toggle without a daemon restart.
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
- Command type derivation (inline in `RecordCommand()`) uses the first arg only (e.g., `["status", "--porcelain"]` becomes `git_status`). Subcommands are not distinguished.
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

---

## Client Performance Recording

### What it does

A dev-only, browser-side recorder of what the dashboard's main thread was doing while it felt slow: per-second event loop delay, long tasks, React commits, WebSocket handler time by message type, fetches, terminal throughput, chat load samples, memory, navigation, and errors. The user starts recording from a sidebar pane, sends the recording as a chat attachment to a performance chat spawned in a schmux checkout, and an agent there diagnoses and fixes the cause. The daemon analyzes nothing. See `docs/client-performance.md` for the file format.

### Key files

| File                                                    | Purpose                                                                                         |
| ------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `assets/dashboard/src/lib/clientPerf.ts`                | Recorder core: ring buffers, the 100ms stall rule, file builder, send/stop semantics            |
| `assets/dashboard/src/lib/clientPerfObservers.ts`       | PerformanceObservers, MessageChannel event-loop probe, visibility and error hooks, clock offset |
| `assets/dashboard/src/lib/clientPerfStore.ts`           | IndexedDB persistence; one snapshot per tab                                                     |
| `assets/dashboard/src/lib/transport.ts`                 | Wraps every `createWebSocket` so handlers are timed                                             |
| `assets/dashboard/src/components/ClientPerformance.tsx` | Sidebar pane (Start/Stop recording, open the performance chat)                                  |
| `assets/dashboard/src/components/chat/Composer.tsx`     | `Recording since HH:MM · attach` checkbox; uploads on send                                      |
| `internal/dashboard/handlers_client_performance.go`     | Dev-only `POST /api/client-performance/session`; ensures the chat exists                        |

### Architecture decisions

- **The daemon stores nothing about the chat.** The browser keeps the workspace/session ids and sends them on every call; the endpoint settles workspace and session from them. This keeps the feature dev-only with zero state-file changes.
- **The branch is fixed (`client-performance`).** The agent lands in the same workspace every round, its earlier commits are still there, and the user's own workspaces on the repo are never adopted. A non-schmux checkout is refused.
- **Three switches gate recording:** the daemon's `dev_mode` (applied by `AppShell` from healthz), `client_performance.enabled` from config, and a per-browser `localStorage` switch written only by the recorder, so a config hot-reload or a daemon restarted without `--dev-mode` turns the recorder off.
- **Buffers persist across reloads** (IndexedDB, one record per tab) because the agent may ask the user to reload as a test; the reload itself is recorded.
- **A binary WebSocket frame is counted by `byteLength` under type `binary`, never parsed.**

### Data collected

One JSON file per send: `client-perf-<timestamp>-<browser id>.json` with `build` (version, dev mode, source workspace, vite dev flag), `environment` (machine, unsupported observers, clock offset), `workload` (session/socket counts, config flags), a per-second `timeline` with a `stalls` index array, and detail rings (`longTasks`, `interactions`, `commits`, `websocket`, `fetches`, `terminals`, `chatLoads`, `memory`, `navigation`, `errors`). Every timestamp is daemon time. Caps: timeline 3600 rows; detail rings 100–3600 each.

### Analysis workflow

Open the performance chat from the sidebar pane; the agent reads the attached file. See `docs/client-performance.md`.

### Gotchas

- A production React build leaves `commits` empty: React's `Profiler` only fires in the development build. `--dev-mode` without `--dev-proxy` serves the embedded production bundle; `build.viteDev` says which one the recording came from.
- Two tabs of the same browser record separately: separate IndexedDB keys, separate switches.
- The recorder empties its buffers in exactly three cases: a message carrying the recording was sent, the user pressed Stop, or the config switch went off.

### Common modification patterns

- **Add a new measured source:** add a `recordX` method on `ClientPerfCollector`, call it from the source, and document the buffer in `docs/client-performance.md`.
- **Change the stall rule:** edit `STALL_MS` in `clientPerf.ts`; the pane and the file share it.
- **Change a ring cap:** edit the `new Ring<T>(...)` capacities in `clientPerf.ts` and the caps table in `docs/client-performance.md`.

### Configuration

```json
{
  "client_performance": { "enabled": false, "repo": "", "target": "" }
}
```

| Field                        | Default | Description                                                   |
| ---------------------------- | ------- | ------------------------------------------------------------- |
| `client_performance.enabled` | `false` | Turns on the recorder and the sidebar pane. Hot-reloadable.   |
| `client_performance.repo`    | `""`    | Configured repo the chat works in. Must be a schmux checkout. |
| `client_performance.target`  | `""`    | Target the chat is spawned on. Must have a chat mode.         |

---

## Chat Load Telemetry

The dashboard and daemon append chat history, browser rendering, and image timings to `~/.schmux/diagnostics/chat-performance.jsonl`. The Advanced tab's **Chat Load Profiling** toggle adds detailed breakdowns; baseline timings are recorded regardless of the toggle. Every history frame and browser load carries the same `load_id` for correlation. The file survives daemon restarts and is separate from `daemon-startup.log`.
