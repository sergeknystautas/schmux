# Agent Signaling

schmux provides a structured event system for agents to communicate their status in real-time. Agents append JSONL events to a per-session file; schmux watches the file and routes events to the dashboard.

## Overview

The signaling system has three components:

1. **Event-based signaling** -- Agents append JSONL events to `$SCHMUX_EVENTS_FILE`
2. **Automatic provisioning** -- schmux teaches agents about signaling via instruction files, CLI flags, or lifecycle hooks
3. **NudgeNik fallback** -- LLM-based classification for agents that do not signal

**Key principle**: Agents signal WHAT attention they need. schmux and the dashboard control HOW to notify the user.

## JSONL event protocol

Events are JSON objects, one per line, appended to a per-session file at `<workspace>/.schmux/events/<session-id>.jsonl`.

### Event types

**`status`** -- Agent state change (primary signaling mechanism):

```json
{
  "ts": "2026-02-18T14:30:00Z",
  "type": "status",
  "state": "completed",
  "message": "Implemented login feature"
}
```

**`failure`** -- Tool failure report:

```json
{ "ts": "...", "type": "failure", "tool": "bash", "input": "npm test", "error": "exit code 1" }
```

**`reflection`** -- Friction learning:

```json
{ "ts": "...", "type": "reflection", "text": "When editing config.go, check the test file too" }
```

**`friction`** -- Ad-hoc friction note:

```json
{ "ts": "...", "type": "friction", "text": "Build takes 45s, slows iteration" }
```

### Valid states

Defined in `internal/events/types.go` (`ValidStates` map):

| State           | Meaning                              |
| --------------- | ------------------------------------ |
| `working`       | Actively working on a task           |
| `completed`     | Task finished successfully           |
| `needs_input`   | Waiting for user authorization/input |
| `needs_testing` | Ready for user testing               |
| `error`         | Error occurred, needs intervention   |
| `rotate`        | Session should be rotated            |

## Data flow

```
Agent appends JSONL → $SCHMUX_EVENTS_FILE
  │
  ├─ LOCAL: EventWatcher (fsnotify on parent dir, 100ms debounce)
  │         internal/events/watcher.go
  │
  ├─ REMOTE: RemoteEventWatcher (tail -f via hidden tmux pane,
  │          sentinel-wrapped: __SCHMUX_SIGNAL__<json>__END__)
  │          internal/events/remotewatcher.go
  │
  ▼
EventHandler.HandleEvent() dispatch by event type
  │
  ▼
DashboardHandler → Server.HandleStatusEvent()
  │
  ├─ mapEventStateToNudge(state)
  ├─ State priority check (tier 0: working, tier 1: blocking, tier 2: terminal)
  ├─ state.UpdateSessionNudge() + Save()
  └─ BroadcastSessions() → WebSocket → browser notification sound
```

### State priority tiers

| Tier | States                       | Meaning            |
| ---- | ---------------------------- | ------------------ |
| 0    | Working                      | Transient activity |
| 1    | Needs Input, Needs Attention | Blocking           |
| 2    | Completed, Error             | Terminal           |

A lower-tier state cannot overwrite a higher-tier state. Exception: `working` always overwrites (new turn started).

## Environment variables

Every spawned session receives:

| Variable              | Purpose                           |
| --------------------- | --------------------------------- |
| `SCHMUX_ENABLED`      | Indicates running in schmux (`1`) |
| `SCHMUX_SESSION_ID`   | Unique session identifier         |
| `SCHMUX_WORKSPACE_ID` | Workspace identifier              |
| `SCHMUX_EVENTS_FILE`  | Per-session JSONL event file path |

## Automatic provisioning

Each tool adapter declares how it receives signaling instructions via `SignalingStrategy()`:

| Strategy                   | How it works                                            | Tools       |
| -------------------------- | ------------------------------------------------------- | ----------- |
| `SignalingHooks`           | Lifecycle hooks write events at start/stop/permission   | Claude Code |
| `SignalingCLIFlag`         | CLI flag points to `~/.schmux/signaling.md`             | Codex       |
| `SignalingInstructionFile` | Signaling block appended to the tool's instruction file | Gemini      |

The instruction block is wrapped in `<!-- SCHMUX:BEGIN -->` / `<!-- SCHMUX:END -->` markers. User content outside the block is preserved.

## Remote event watching

Remote sessions use a hidden tmux watcher pane running `tail -f` on the event file. Output is sentinel-wrapped (`__SCHMUX_SIGNAL__<json>__END__`) and received via tmux control mode. `RemoteEventWatcher.ProcessOutput()` extracts, deduplicates by timestamp, and dispatches to the same handler chain as local events.

The watcher goroutine survives connection drops and auto-reconnects.

## Key files

| File                                      | Purpose                                                 |
| ----------------------------------------- | ------------------------------------------------------- |
| `internal/events/types.go`                | Event structs, `ValidStates` map                        |
| `internal/events/handler.go`              | `EventHandler` interface                                |
| `internal/events/watcher.go`              | `EventWatcher`: fsnotify-based local file watcher       |
| `internal/events/remotewatcher.go`        | `RemoteEventWatcher`: sentinel-based remote processing  |
| `internal/events/dashboardhandler.go`     | Routes status events to dashboard                       |
| `internal/events/monitorhandler.go`       | Forwards all events (dev mode)                          |
| `internal/detect/adapter.go`              | `ToolAdapter` interface, `SignalingStrategy` enum       |
| `internal/detect/adapter_claude_hooks.go` | Claude hooks-based signaling                            |
| `internal/workspace/ensure/manager.go`    | `SignalingInstructions` template, provisioning logic    |
| `internal/session/manager.go`             | Spawn (env vars, event dir), `StartRemoteSignalMonitor` |
| `internal/dashboard/websocket.go`         | `HandleStatusEvent`: nudge update, priority, broadcast  |
| `internal/dashboard/websocket_helpers.go` | `clearNudgeOnInput`: nudge clearing on user interaction |

## Gotchas

- **fsnotify watches the directory, not the file.** The file may not exist when the watcher starts. Directory watching with filename filtering is more robust.
- **100ms debounce.** Coalesces rapid sequential writes into a single read pass.
- **Nudge clearing on user input.** When the user presses Enter, Tab, or bare Escape in a terminal session, the nudge is automatically cleared via `clearNudgeOnInput()`.
- **Daemon restart recovery.** `events.ReadCurrentStatus(path)` reads the last status event from the JSONL file. The watcher sets its initial offset to the current file size, so it only processes new events.
- **Invalid events are silently skipped.** Malformed JSON lines and unrecognized states are logged but not processed.
- **Per-session files, not per-workspace.** Multiple agents in the same workspace do not interleave events.

## Common modification patterns

- **Add a new event type:** Add struct to `types.go`, register a handler in `daemon.go` under the new type key.
- **Add a new valid state:** Add to `ValidStates` in `types.go`, add display mapping in `mapEventStateToNudge()` in `websocket.go`, add tier in `nudgeStateTier()`, update `SignalingInstructions` template.
- **Add support for a new agent:** Create `adapter_<name>.go` implementing `ToolAdapter`, set `SignalingStrategy()`, implement the appropriate provisioning method.

## For agent developers

```bash
if [ -n "$SCHMUX_EVENTS_FILE" ]; then
    # Signal completion
    echo '{"ts":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'","type":"status","state":"completed","message":"Done"}' >> "$SCHMUX_EVENTS_FILE"

    # Signal needs input
    echo '{"ts":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'","type":"status","state":"needs_input","message":"Approve changes?"}' >> "$SCHMUX_EVENTS_FILE"
fi
```

Always use `>>` (append), never `>` (overwrite). The file is append-only.
