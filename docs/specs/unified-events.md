# Unified Agent Event System: Design Proposal

## Problem

Schmux has three subsystems that consume real-time information from agents:

1. **Dashboard/UI** — needs agent state (working/completed/error/needs_input), intent, and blockers for real-time display and user notification
2. **Lore (continual learning)** — needs friction data (tool failures, reflections) for curation into instruction file proposals
3. **Floor manager** — needs state transitions across all agents for supervisory orchestration

These subsystems evolved independently, each inventing its own communication channel:

| Channel          | Mechanism                         | Format                | Producer                  | Consumer                 |
| ---------------- | --------------------------------- | --------------------- | ------------------------- | ------------------------ |
| Signal file      | Overwrite `$SCHMUX_STATUS_FILE`   | Multi-line plain text | Hooks + agent prompt      | Dashboard, Floor manager |
| Lore failures    | Append to `.schmux/lore.jsonl`    | JSON line             | `capture-failure.sh` hook | Lore curator             |
| Lore reflections | Append to `.schmux/lore.jsonl`    | JSON line             | Agent (prompt-instructed) | Lore curator, stop-gate  |
| Stop gate        | `stop-gate.sh` reads both files   | Exit code + stderr    | Claude `Stop` hook        | Claude Code runtime      |
| Escalation       | `schmux escalate` CLI → HTTP POST | JSON body             | Floor manager agent       | Dashboard                |
| Lifecycle        | Internal callback → tmux inject   | Plain text            | Session/workspace manager | Floor manager            |

This causes several problems:

- **Two files, overlapping concerns**: The signal file and lore JSONL both carry agent-produced data but use different mechanisms, formats, and file paths.
- **Conflated stop gate**: `stop-gate.sh` enforces both status signaling (dashboard concern) and lore reflection (learning concern) in one script. You can't disable one without the other.
- **Floor manager gets lore hooks**: The hook provisioning doesn't distinguish session roles, so the supervisory floor manager gets failure capture and reflection enforcement meant for coding agents.
- **Claude-only features**: Automatic failure capture only works for Claude Code (requires `PostToolUseFailure` hook). Other agents get prompt instructions but no enforcement or automatic capture.
- **Shared lore file**: Multiple sessions in the same workspace append to a single `.schmux/lore.jsonl`, requiring session ID filtering and creating contention.

## Design: Unified Event File

### Core Concept

Replace the signal file and lore JSONL with a single **per-session event file**:

```
<workspace>/.schmux/events/<session-id>.jsonl
```

Each session gets its own append-only event file. Each line is a typed JSON event. Consumers subscribe to event types they care about.

This is a natural fit because:

- Each session already has a unique ID and its own signal file at `.schmux/signal/<session-id>`
- The event file replaces that signal file at the same granularity
- No concurrent-write contention — only one session writes to each file
- Stop hooks only need to read their own session's file (no grep for `sid`)
- Session cleanup (dispose) can simply delete or archive the file
- The lore curator aggregates across session files when needed, the same way it currently aggregates across workspace lore files

### Event Schema

Every event has a common envelope:

```json
{
  "ts": "2026-02-18T14:30:00Z",
  "type": "<event-type>",
  ...type-specific fields...
}
```

| Field  | Type            | Description              |
| ------ | --------------- | ------------------------ |
| `ts`   | ISO 8601 string | Event timestamp (UTC)    |
| `type` | string          | Event type discriminator |

Note: there is no `sid` field. The session ID is encoded in the file path (`<session-id>.jsonl`). This avoids redundancy and keeps each event line smaller.

#### Event Types

**`status`** — Agent state change (replaces signal file)

```json
{
  "ts": "...",
  "type": "status",
  "state": "working",
  "message": "Refactoring auth module",
  "intent": "Improve module structure",
  "blockers": ""
}
```

| Field      | Description                                                                       |
| ---------- | --------------------------------------------------------------------------------- |
| `state`    | One of: `working`, `completed`, `needs_input`, `needs_testing`, `error`, `rotate` |
| `message`  | Short description (≤100 chars)                                                    |
| `intent`   | What the agent is trying to accomplish (optional)                                 |
| `blockers` | What's preventing progress (optional)                                             |

**`failure`** — Tool failure (replaces lore failure entries)

```json
{
  "ts": "...",
  "type": "failure",
  "tool": "Bash",
  "input": "go build ./...",
  "error": "undefined: Foo",
  "category": "build_failure"
}
```

| Field      | Description                                                                                                               |
| ---------- | ------------------------------------------------------------------------------------------------------------------------- |
| `tool`     | Tool name (Bash, Read, Edit, etc.)                                                                                        |
| `input`    | Summarized tool input (≤300 chars)                                                                                        |
| `error`    | Summarized error message (≤500 chars)                                                                                     |
| `category` | Classification: `not_found`, `permission`, `syntax`, `wrong_command`, `build_failure`, `test_failure`, `timeout`, `other` |

**`reflection`** — Friction learning (replaces lore reflection entries)

```json
{
  "ts": "...",
  "type": "reflection",
  "text": "When using bare repos, run git fetch before git show"
}
```

| Field  | Description                                                                              |
| ------ | ---------------------------------------------------------------------------------------- |
| `text` | Friction statement: "When X, do Y instead" — or `"none"` if nothing tripped the agent up |

**`friction`** — Ad-hoc friction note (replaces lore friction entries, used by non-Claude agents)

```json
{
  "ts": "...",
  "type": "friction",
  "text": "The build dashboard command is go run ./cmd/build-dashboard, not npm run build"
}
```

| Field  | Description                    |
| ------ | ------------------------------ |
| `text` | Free-form friction observation |

### File Lifecycle

- **Created**: When a session spawns. The directory `.schmux/events/` is created alongside `.schmux/signal/` (or replaces it).
- **Written to**: By hooks (for Claude), by agents (for reflections/friction), by hook scripts (for failures). Only the owning session writes to its file.
- **Read by**: Event watcher (daemon), stop hooks, lore curator.
- **Cleaned up**: On session dispose, the daemon can archive or delete the file. Lore-relevant events (failure, reflection, friction) are read by the curator before cleanup.

### Hook Scripts Location

Hook scripts (stop checks, failure capture) live at `~/.schmux/hooks/`, **not** inside workspaces. This avoids polluting worktrees with schmux infrastructure files that would need to be gitignored.

```
~/.schmux/hooks/
  stop-status-check.sh    # gates agent stop on status update
  stop-lore-check.sh      # gates agent stop on friction reflection
  capture-failure.sh      # captures tool failures as events
```

Scripts are written once at daemon startup (or on first use) and shared across all workspaces. Hook commands reference them by absolute path, resolved at hook config generation time:

```json
{
  "type": "command",
  "command": "/Users/you/.schmux/hooks/stop-lore-check.sh"
}
```

This is a change from the current design where scripts are written per-workspace to `<workspace>/.schmux/hooks/` and `<workspace>/.claude/hooks/`. The current approach:

- Writes identical copies to every workspace on every spawn
- Puts files inside the worktree that may not be gitignored
- Requires `$CLAUDE_PROJECT_DIR`-relative paths in hook commands

The new approach:

- Writes scripts once to a central location outside any worktree
- No gitignore concerns — `~/.schmux/` is never inside a git repo
- Hook commands use absolute paths (resolved by `ClaudeHooks()` at config generation time)
- `LoreHookScripts()` becomes `EnsureGlobalHookScripts()` and runs at daemon startup instead of per-spawn

### Environment Variables

| Current               | New                   | Description                       |
| --------------------- | --------------------- | --------------------------------- |
| `SCHMUX_STATUS_FILE`  | `SCHMUX_EVENTS_FILE`  | Path to this session's event file |
| `SCHMUX_SESSION_ID`   | `SCHMUX_SESSION_ID`   | Unchanged — session identifier    |
| `SCHMUX_WORKSPACE_ID` | `SCHMUX_WORKSPACE_ID` | Unchanged — workspace identifier  |

`SCHMUX_EVENTS_FILE` points to `<workspace>/.schmux/events/<session-id>.jsonl`. Each session has its own file. Hooks and agents append to this file.

For backward compatibility during migration, `SCHMUX_STATUS_FILE` can be kept temporarily while consumers transition to reading from `SCHMUX_EVENTS_FILE`.

## Consumer Model

### Event Watcher (replaces `signal.FileWatcher`)

One watcher per session monitors its event file using fsnotify. On file change:

1. Read new lines since the last read position (track file offset)
2. Parse each line as JSON
3. Dispatch to registered event handlers by `type`

```go
type EventHandler interface {
    HandleEvent(ctx context.Context, sessionID string, event Event)
}

type EventWatcher struct {
    path      string
    sessionID string
    offset    int64
    handlers  map[string][]EventHandler // type -> handlers
}
```

On daemon restart, the watcher scans the file to reconstruct current state (latest `status` event).

This is the same granularity as the current `signal.FileWatcher` (one per session), so there's no change in watcher count. The difference is that each watcher now handles all event types instead of just status.

### Consumer: Dashboard (status events)

Subscribes to `type: "status"`. On each status event:

- Update in-memory session state (nudge, intent, blockers)
- Broadcast to WebSocket clients

This replaces `server.HandleAgentSignal()`.

### Consumer: Floor Manager Injector (status events, filtered)

Subscribes to `type: "status"`. Filters:

- Skip events from the floor manager's own session
- Skip `working` → `working` transitions (no-op state changes)
- Only inject `error`, `needs_input`, `needs_testing`, `completed` transitions

Formats and injects `[SIGNAL]` messages into the floor manager's tmux terminal. Same behavior as today, but driven by the unified event stream instead of the signal callback.

### Consumer: Lore Curator (failure + reflection + friction events)

On session dispose, reads the session's event file and extracts `failure`, `reflection`, and `friction` events. The curator aggregates events from all session files across workspaces for a repo (scanning `.schmux/events/*.jsonl` in each workspace).

This replaces the current `ReadEntriesMulti()` call that scans workspace lore files. The per-session file structure maps naturally: each file's name gives the session ID, and the workspace is derived from the file's parent path.

### Consumer: Stop Hooks (status + reflection checks)

Two separate hook scripts, each reading from `$SCHMUX_EVENTS_FILE`:

#### `stop-status-check.sh` — Status enforcement

```bash
#!/bin/bash
INPUT=$(cat)
ACTIVE=$(echo "$INPUT" | jq -r '.stop_hook_active // false')
[ "$ACTIVE" = "true" ] && exit 0
[ -n "$SCHMUX_EVENTS_FILE" ] || exit 0

# Check for a status event with a meaningful state
if [ -f "$SCHMUX_EVENTS_FILE" ]; then
  LAST_STATE=$(grep '"type":"status"' "$SCHMUX_EVENTS_FILE" | tail -1 | jq -r '.state // ""')
  case "$LAST_STATE" in
    completed|needs_input|needs_testing|error) exit 0 ;;
    working)
      LAST_MSG=$(grep '"type":"status"' "$SCHMUX_EVENTS_FILE" | tail -1 | jq -r '.message // ""')
      [ -n "$LAST_MSG" ] && exit 0 ;;
  esac
fi

printf '{"decision":"block","reason":"Write your status before finishing."}\n'
exit 0
```

#### `stop-lore-check.sh` — Reflection enforcement

```bash
#!/bin/bash
INPUT=$(cat)
ACTIVE=$(echo "$INPUT" | jq -r '.stop_hook_active // false')
[ "$ACTIVE" = "true" ] && exit 0
[ -n "$SCHMUX_EVENTS_FILE" ] || exit 0

if grep -q '"type":"reflection"' "$SCHMUX_EVENTS_FILE" 2>/dev/null; then
  exit 0
fi

printf '{"decision":"block","reason":"Write a friction reflection before finishing."}\n'
exit 0
```

Note how much simpler these are compared to the per-workspace version: no need to filter by session ID. Each session's event file contains only that session's events.

Splitting these into two hooks means:

- The floor manager gets `stop-status-check.sh` only (no lore enforcement)
- Normal coding agents get both
- Lore can be disabled per-session without affecting status signaling

## Hook Configuration

### Claude Code Hooks

The `buildClaudeHooksMap()` function takes a `hooksDir` parameter (the resolved absolute path to `~/.schmux/hooks/`) and produces hooks that write to `$SCHMUX_EVENTS_FILE`:

```go
func buildClaudeHooksMap(hooksDir string) map[string][]claudeHookMatcherGroup {
    return map[string][]claudeHookMatcherGroup{
        "SessionStart": {
            {Hooks: []claudeHookHandler{{
                Type:    "command",
                Command: statusEventCommand("working", ""),
            }}},
        },
        "SessionEnd": {
            {Hooks: []claudeHookHandler{{
                Type:    "command",
                Command: statusEventCommand("completed", ""),
            }}},
        },
        "UserPromptSubmit": {
            {Hooks: []claudeHookHandler{{
                Type:    "command",
                Command: statusEventWithIntentCommand("working", "prompt"),
            }}},
        },
        "Stop": {
            // Two separate hooks: status check + lore check
            // Paths resolved at config generation time via hooksDir variable
            // File-existence guards ensure graceful no-op if scripts are missing
            {Hooks: []claudeHookHandler{{
                Type:    "command",
                Command: fmt.Sprintf(`[ -f "%s/stop-status-check.sh" ] && "%s/stop-status-check.sh" || true`, hooksDir, hooksDir),
            }}},
            {Hooks: []claudeHookHandler{{
                Type:    "command",
                Command: fmt.Sprintf(`[ -f "%s/stop-lore-check.sh" ] && "%s/stop-lore-check.sh" || true`, hooksDir, hooksDir),
            }}},
        },
        "Notification": {
            {Matcher: "permission_prompt|elicitation_dialog",
             Hooks: []claudeHookHandler{{
                Type:    "command",
                Command: statusEventWithBlockersCommand("needs_input", "message"),
            }}},
        },
        "PostToolUseFailure": {
            {Hooks: []claudeHookHandler{{
                Type:    "command",
                Command: fmt.Sprintf(`[ -f "%s/capture-failure.sh" ] && "%s/capture-failure.sh" || true`, hooksDir, hooksDir),
            }}},
        },
    }
}
```

The helper functions generate shell commands that append JSON to `$SCHMUX_EVENTS_FILE`:

```go
func statusEventCommand(state, messageExpr string) string {
    return fmt.Sprintf(
        `[ -n "$SCHMUX_EVENTS_FILE" ] && printf '{"ts":"%%s","type":"status","state":"%s","message":"%%s"}\n' "$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)" "%s" >> "$SCHMUX_EVENTS_FILE" || true`,
        state, messageExpr,
    )
}
```

### Non-Claude Agents (Prompt Instructions)

The `SignalingInstructions` constant is updated to teach agents to write events:

```markdown
## Schmux Event Reporting

This workspace is managed by schmux. Report events to help monitor your progress.

### How to Report

Append a JSON line to the file at `$SCHMUX_EVENTS_FILE`. Each line must be valid JSON.

### Status Events

Report your status when it changes:

\`\`\`bash
echo '{"ts":"<ISO8601>","type":"status","state":"STATE","message":"description"}' >> "$SCHMUX_EVENTS_FILE"
\`\`\`

States: `completed`, `needs_input`, `needs_testing`, `error`, `working`

### Friction Reflections

When finishing, report what tripped you up:

\`\`\`bash
echo '{"ts":"<ISO8601>","type":"reflection","text":"When X, do Y instead"}' >> "$SCHMUX_EVENTS_FILE"
\`\`\`
```

This gives all agents the same protocol regardless of whether they have hook support.

## Floor Manager Integration

The floor manager session is provisioned differently:

1. **Status hooks**: Yes — the floor manager needs status signaling for dashboard display and self-rotation (`rotate` state)
2. **Failure capture hook**: No — the floor manager doesn't do coding work; its failures are noise
3. **Reflection enforcement hook**: No — the floor manager is a supervisor, not a learner
4. **Lore check stop hook**: No — only the status check stop hook is installed

The provisioning logic in `session/manager.go` uses `opts.IsFloorManager` to select which hooks to install. Since hook scripts now live at `~/.schmux/hooks/` (a central location), the distinction is made at **hook configuration time**, not script deployment time. The `buildClaudeHooksMap()` function accepts a flag to exclude lore hooks:

```go
if ensure.SupportsHooks(baseTool) {
    // Floor manager: signaling hooks only (no lore enforcement)
    // Normal sessions: signaling + lore hooks
    ensure.ClaudeHooks(w.Path, !opts.IsFloorManager)
}
```

`ClaudeHooks()` generates different `.claude/settings.local.json` content depending on whether lore hooks are included. The hook scripts themselves don't need to be deployed per-workspace — they already exist at `~/.schmux/hooks/`.

The floor manager's event file is at `~/.schmux/floor-manager/.schmux/events/<session-id>.jsonl`. Since this path isn't inside a registered workspace, lore curation never reads it.

## Lore System Changes

The lore system's data source changes from `.schmux/lore.jsonl` to `.schmux/events/<session-id>.jsonl` files. The curator reads only `failure`, `reflection`, and `friction` event types. The `lore.Entry` struct maps from the event schema:

| Event Field | Entry Field                  |
| ----------- | ---------------------------- |
| `ts`        | `Timestamp`                  |
| file path   | `Session` (from filename)    |
| file path   | `Workspace` (from parent)    |
| `type`      | `Type`                       |
| `text`      | `Text` (reflection/friction) |
| `tool`      | `Tool` (failure)             |
| `input`     | `InputSummary` (failure)     |
| `error`     | `ErrorSummary` (failure)     |
| `category`  | `Category` (failure)         |

Both the workspace ID and session ID are derived from the file path rather than stored in each event line. This keeps events compact and avoids redundancy.

State-change records (`proposed`, `applied`, `dismissed`) continue to live in the central state file at `~/.schmux/lore/<repo>/state.jsonl`, separate from session event files. This keeps the session files as pure append-only agent output.

### Aggregation

The curator collects events by scanning all workspaces for a repo:

```
for each workspace where workspace.Repo == targetRepo:
    glob <workspace.Path>/.schmux/events/*.jsonl
    for each file:
        sessionID = filename without .jsonl extension
        read lines, filter by type (failure, reflection, friction)
        tag each entry with sessionID and workspaceID
```

This replaces `ReadEntriesMulti()` with a similar pattern but at session-file granularity.

## Event Watcher Architecture

```
  Session A                    Session B
  .schmux/events/a.jsonl       .schmux/events/b.jsonl
        │                            │
        │ fsnotify                    │ fsnotify
        ▼                            ▼
  ┌──────────────┐            ┌──────────────┐
  │ EventWatcher │            │ EventWatcher │
  │  (session A) │            │  (session B) │
  └──────┬───────┘            └──────┬───────┘
         │                           │
         └───────────┬───────────────┘
                     │ dispatch by type
      ┌──────────────┼──────────────────┐
      ▼              ▼                  ▼
┌───────────┐ ┌────────────────┐ ┌────────────────┐
│ Dashboard │ │ Floor Manager  │ │  Lore System   │
│ Handler   │ │  Injector      │ │  (on dispose)  │
│           │ │                │ │                │
│ status    │ │ status events  │ │ failure,       │
│ events    │ │ → filter       │ │ reflection,    │
│ → WS      │ │ → inject to   │ │ friction       │
│ broadcast │ │   tmux         │ │ → curator      │
└───────────┘ └────────────────┘ └────────────────┘
```

## Remote Event Monitoring

For remote sessions, `fsnotify` is unavailable — the event file lives on the remote host. The remote event watcher reuses the existing infrastructure (hidden tmux watcher pane, sentinel markers, control mode output parsing) but adapts from the overwrite model to append-only streaming.

### Watcher Script

The current `WatcherScript()` uses `cat` to read the entire signal file on each change and compares to a `LAST` variable for dedup. This doesn't work for append-only JSONL — the file grows over the session's lifetime and re-reading it on every append would reprocess all events.

The new script uses `tail -f` to stream new lines as they're appended:

```bash
EVENTS_FILE='<path>'
if [ -f "$EVENTS_FILE" ]; then
  # Emit existing lines for recovery
  while IFS= read -r line; do
    echo "__SCHMUX_SIGNAL__${line}__END__"
  done < "$EVENTS_FILE"
fi
# Stream new lines as they appear
tail -f -n 0 "$EVENTS_FILE" 2>/dev/null | while IFS= read -r line; do
  echo "__SCHMUX_SIGNAL__${line}__END__"
done
```

Each JSONL line gets its own sentinel wrapper. `tail -f -n 0` starts from the current end of the file, avoiding duplicate processing of lines already emitted in the recovery phase. If `tail -f` is unavailable (rare embedded systems), the script falls back to polling with line-count tracking.

### Sentinel Granularity

One sentinel-wrapped message per JSONL line. This maps directly to the existing `%output` event model in control mode — each sentinel emission becomes one `OutputEvent` that `ProcessOutput` extracts.

### ProcessOutput Changes

`RemoteEventWatcher.ProcessOutput()` parses JSON instead of plain text. The sentinel extraction logic (`ParseSentinelOutput`) is unchanged — it still finds content between `__SCHMUX_SIGNAL__` and `__END__` markers.

Deduplication changes from string-equality on the full file content to tracking the timestamp of the last processed event. Since events are append-only with monotonic timestamps, any event with a timestamp ≤ the last seen timestamp is skipped.

### Recovery on Reconnect

When the connection drops and reconnects, `StartRemoteSignalMonitor` creates a new watcher pane that runs the full script (recovery phase + `tail -f`). The Go side deduplicates against the last processed event timestamp, so re-emitted events from the recovery phase are safely skipped.

This replaces the current approach of running `cat .schmux/signal/<sessionID>` as a separate recovery command. The recovery is now built into the watcher script itself.

### Infrastructure Reuse

The watcher pane lifecycle, reconnection loop, `SubscribeOutput` channel, and stop/cleanup logic in `StartRemoteSignalMonitor` / `StopRemoteSignalMonitor` remain structurally identical. The changes are confined to:

- `WatcherScript()` — `tail -f` replaces `cat` + comparison loop
- `ProcessOutput()` — JSON parsing replaces `ParseSignalFile()`
- Deduplication — timestamp-based replaces string-equality

## Migration Path

### Phase 1: Add event file alongside signal file

- Hooks write to both `$SCHMUX_STATUS_FILE` (overwrite) and `$SCHMUX_EVENTS_FILE` (append)
- Dashboard reads from signal file (unchanged)
- Lore reads from event file (new) with fallback to lore.jsonl
- Floor manager reads from signal callback (unchanged)

### Phase 2: Switch consumers to event file

- Event watcher replaces signal file watcher (same per-session granularity, just reading from events file instead)
- Dashboard reads from event watcher
- Floor manager injector subscribes to event watcher
- Signal file becomes write-only (still written for backward compat)

### Phase 3: Remove signal file

- Drop `$SCHMUX_STATUS_FILE` environment variable
- Remove per-session signal file creation and `.schmux/signal/` directory
- Remove `signal.FileWatcher` / `signal.ParseSignalFile`
- Event watcher is the single source of truth

### Phase 4: Remove `.schmux/lore.jsonl`

- Stop hooks read from event file
- Capture-failure writes to event file
- Lore curator reads from event files
- Remove lore JSONL file path references

## Benefits

1. **Single source of truth**: One file per session contains all agent-produced events
2. **Consumer independence**: Dashboard, floor manager, and lore each subscribe to the event types they need
3. **Clean separation of concerns**: Status check and lore check are separate hooks, independently configurable
4. **Floor manager correctness**: Floor manager gets status hooks only, no lore interference
5. **Agent parity**: All agents use the same event protocol (JSON append). Mechanism differs (hook vs prompt instruction) but the data contract is identical.
6. **No concurrent writes**: Each session owns its file exclusively — no contention, no need for `sid` filtering
7. **Simpler stop hooks**: Stop hooks read the session's own file without filtering by session ID
8. **Natural cleanup**: Session dispose can archive or delete the event file
9. **Extensibility**: New consumers (e.g., cost tracking, performance monitoring) subscribe to event types without modifying the agent communication protocol
10. **Simpler lore integration**: Lore reads from event files filtered by type, aggregating across sessions/workspaces as needed

## Daemon Restart Recovery

The event watcher does not persist its file offset. On daemon restart, each session's watcher recovers state and starts fresh:

1. **State recovery**: `ReadCurrent()` scans the event file for the last `"type":"status"` event (reads all lines, keeps the latest). Fires that single event to handlers so the dashboard gets the correct nudge state. Sets `offset` to the end of the file.

2. **Events appended during downtime**: Status events are covered by step 1 (latest wins). Lore events (failure, reflection, friction) don't need real-time dispatch — they're batch-read on session dispose by the curator. Floor manager injection only matters for live transitions; replaying stale signals from a downtime window would confuse the agent.

3. **Orphaned event files**: Sessions in state that no longer have a running tmux process are detected by existing session reconciliation logic. Their event files remain on disk for lore to read on the next curation pass. Files for sessions not in state at all can be cleaned up.

4. **Remote sessions**: The `tail -f` watcher script emits existing lines on startup (recovery phase). The Go-side dedup by timestamp skips events already processed before the crash. On a fresh restart with no prior timestamp, the recovery scan handles it the same way as local: extract latest status, skip the rest.

## Open Questions

1. **Event file retention**: When a session is disposed, should its event file be deleted immediately (after lore reads it), archived to a different location, or left in place until pruned by age? Leaving files in place is simplest but accumulates disk usage.
