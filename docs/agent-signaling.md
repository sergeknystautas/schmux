# Agent Signaling

Schmux provides a comprehensive system for agents to communicate their status to users in real-time.

## Overview

The agent signaling system has three components:

1. **Direct Signaling** - Agents write status events to a JSONL event file
2. **Automatic Provisioning** - Schmux teaches agents about signaling via instruction files
3. **NudgeNik Fallback** - LLM-based classification for agents that don't signal

```
┌─────────────────────────────────────────────────────────────────┐
│                         On Session Spawn                        │
├─────────────────────────────────────────────────────────────────┤
│  1. Workspace obtained                                          │
│  2. Provision: Create .claude/CLAUDE.md (or .codex/, .gemini/)  │
│  3. Inject: SCHMUX_ENABLED=1, SCHMUX_SESSION_ID, etc.           │
│  4. Launch agent in tmux                                        │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      During Session Runtime                     │
├─────────────────────────────────────────────────────────────────┤
│  Agent reads instruction file → learns signaling protocol       │
│  Hooks append JSON event to $SCHMUX_EVENTS_FILE                 │
│  EventWatcher detects new line → publishes to event bus         │
│  Bus subscribers update dashboard state + trigger notifications │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                         Fallback Path                           │
├─────────────────────────────────────────────────────────────────┤
│  If no signal for 5+ minutes:                                   │
│  NudgeNik (LLM) analyzes terminal output → classifies state     │
└─────────────────────────────────────────────────────────────────┘
```

**Key principle**: Agents signal WHAT attention they need. Schmux/dashboard controls HOW to notify the user.

---

## Direct Signaling Protocol

Agents signal their state by appending a JSON event line to the event file provided by schmux:

```bash
printf '{"ts":"%s","type":"status","state":"STATE","message":"MSG"}\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$SCHMUX_EVENTS_FILE"
```

The `SCHMUX_EVENTS_FILE` environment variable contains the path to the session's event file (typically `$WORKSPACE/.schmux/events/<session-id>.jsonl`).

In practice, Claude Code hooks handle this automatically — agents rarely need to write events directly. The hooks are provisioned by schmux during session spawn.

**Examples:**

```bash
# Signal completion (via hook — automatic)
printf '{"ts":"%s","type":"status","state":"completed","message":"Implementation complete"}\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$SCHMUX_EVENTS_FILE"

# Signal needs input
printf '{"ts":"%s","type":"status","state":"needs_input","message":"Waiting for permission"}\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$SCHMUX_EVENTS_FILE"

# Signal error
printf '{"ts":"%s","type":"status","state":"error","message":"Build failed"}\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$SCHMUX_EVENTS_FILE"

# Signal working (clears attention state)
printf '{"ts":"%s","type":"status","state":"working","message":""}\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$SCHMUX_EVENTS_FILE"
```

**Benefits:**

- **Append-only** - No data loss from overwrites; full history preserved
- **JSON structured** - Easy to parse, supports multiple event types
- **No parsing complexity** - No ANSI stripping or regex matching needed
- **Easy to debug** - `tail -1` shows latest event, `cat` shows full history

### Valid States

| State           | Meaning                              | Dashboard Display     |
| --------------- | ------------------------------------ | --------------------- |
| `completed`     | Task finished successfully           | ✓ Completed           |
| `needs_input`   | Waiting for user authorization/input | ⚠ Needs Authorization |
| `needs_testing` | Ready for user testing               | 🧪 Needs User Testing |
| `error`         | Error occurred, needs intervention   | ❌ Error              |
| `working`       | Actively working on a task           | ⏳ Working            |

### How Signals Flow

The signal pipeline spans the full stack, from event file writes to browser notification sound. Here is the complete data flow:

```
 Agent (in tmux session)
 │
 │  Hook appends JSON event to $SCHMUX_EVENTS_FILE
 │
 ▼
 ┌──────────────────────────────────────────────────────────┐
 │  LOCAL SESSIONS:                                         │
 │  EventWatcher                                            │
 │  internal/event/watcher.go                               │
 │                                                          │
 │  fsnotify watcher receives Write event                   │
 │  Reads new lines from event file                         │
 │  Parses JSON event, extracts type + payload              │
 │  Publishes to in-process event bus                       │
 └──────────────────────────────────┬───────────────────────┘
                                    │
 ┌──────────────────────────────────┴───────────────────────┐
 │  REMOTE SESSIONS:                                        │
 │  Watcher pane in tmux (shell script)                     │
 │  internal/signal/remotewatcher.go                        │
 │                                                          │
 │  tail -n0 -f watches event file for new lines            │
 │  Emits: __SCHMUX_SIGNAL__{json}__END__                   │
 │  Received via %output event in control mode              │
 │  Parsed by RemoteSignalWatcher.ProcessOutput()           │
 │  Publishes to event bus                                  │
 └──────────────────────────────────┬───────────────────────┘
                                    │  bus.Publish("agent.status", ...)
                                    ▼
 ┌──────────────────────────────────────────────────────────┐
 │  Event Bus (internal/bus/bus.go)                         │
 │                                                          │
 │  Subscribers:                                            │
 │  - Dashboard broadcaster (updates nudge, broadcasts)     │
 │  - Floor manager injector (feeds status to supervisor)   │
 │  - Escalation consumer                                   │
 └──────────────────────────────────┬───────────────────────┘
                                    │
                                    ▼
 ┌──────────────────────────────────────────────────────────┐
 │  Dashboard Broadcaster                                   │
 │  internal/dashboard/websocket.go                         │
 │                                                          │
 │  1. MapStateToNudge(state) → display string              │
 │  2. If "working": clear nudge                            │
 │  3. Otherwise: serialize nudge JSON, set nudge           │
 │  4. IncrementNudgeSeq (attention states only)            │
 │  5. state.Save() → persist to ~/.schmux/state.json       │
 │  6. go doBroadcast() → immediate WebSocket push          │
 └──────────────────────────────────┬───────────────────────┘
                                    │  JSON via WebSocket
                                    ▼
 ┌──────────────────────────────────────────────────────────┐
 │  Frontend: SessionsContext.tsx                           │
 │                                                          │
 │  Unified notification useEffect:                         │
 │  - Checks nudge_seq changes + escalation changes         │
 │  - If nudge_seq > lastAcked AND isAttentionState():      │
 │    → playAttentionSound() (at most once per cycle)       │
 │    → localStorage.setItem(storageKey, nudge_seq)         │
 └──────────────────────────────────────────────────────────┘
```

#### Daemon Restart Recovery

When the daemon restarts, existing sessions recover by reading the latest event from the event file:

```
 Daemon starts → restores sessions
 │
 ▼
 For local sessions:
 EventWatcher.ReadCurrent() reads latest status event
 internal/event/watcher.go
 │
 │  Scans JSONL file for last "status" type event
 │  Publishes recovered state to bus
 │  Continues watching for new events
 │
 └──────────────────────────────────────────────

 For remote sessions:
 tail -n1 of event file → parse JSON event
 internal/session/manager.go
 │
 │  Initial state recovery reads last line
 │  Publishes recovered state to bus
 │  Watcher pane continues tailing for new lines
 │
 └──────────────────────────────────────────────
```

The event file contains the full history, so the latest status event gives the current state.

#### Nudge Clearing (user interaction)

When the user types in a terminal WebSocket session, the nudge is automatically cleared:

```
 User presses Enter/Tab in terminal
 │
 ▼
 handleTerminalWebSocket / handleRemoteTerminalWebSocket
 internal/dashboard/websocket.go
 │
 │  Detects \r (Enter), \t (Tab), or \x1b[Z (Shift-Tab)
 │  in the input message
 │
 ▼
 state.ClearSessionNudge(sessionID) → returns true if cleared
 │
 ▼
 state.Save() + BroadcastSessions()
```

---

## Environment Variables

Every spawned session receives these environment variables:

| Variable              | Example                                                | Purpose                       |
| --------------------- | ------------------------------------------------------ | ----------------------------- |
| `SCHMUX_ENABLED`      | `1`                                                    | Indicates running in schmux   |
| `SCHMUX_SESSION_ID`   | `myproj-abc-xyz12345`                                  | Unique session identifier     |
| `SCHMUX_WORKSPACE_ID` | `myproj-abc`                                           | Workspace identifier          |
| `SCHMUX_EVENTS_FILE`  | `/path/to/workspace/.schmux/events/<session-id>.jsonl` | Event file path for signaling |

Agents can check `SCHMUX_ENABLED=1` to conditionally enable signaling. The `SCHMUX_EVENTS_FILE` variable provides the path where hooks and agents append JSON event lines.

Environment variables are injected during spawn by `Manager.Spawn()` (`internal/session/manager.go`).

---

## Automatic Provisioning

### How Agents Learn About Signaling

When you spawn a session, schmux automatically creates an instruction file in the workspace that teaches the agent about the signaling protocol.

| Agent       | Instruction File    |
| ----------- | ------------------- |
| Claude Code | `.claude/CLAUDE.md` |
| Codex       | `.codex/AGENTS.md`  |
| Gemini      | `.gemini/GEMINI.md` |

### Provisioning Flow

```
 Manager.Spawn()
 internal/session/manager.go
 │
 ├─ CLI-flag tools (claude, codex):
 │    provision.SupportsSystemPromptFlag(toolName)
 │    internal/provision/provision.go
 │    │
 │    ▼
 │    provision.EnsureSignalingInstructionsFile()
 │    │  Writes SignalingInstructions template to
 │    │  ~/.schmux/signaling.md
 │    │
 │    ▼
 │    buildCommand() injects CLI flag:
 │      Claude: --append-system-prompt-file ~/.schmux/signaling.md
 │      Codex:  -c model_instructions_file=~/.schmux/signaling.md
 │
 └─ File-based tools (gemini, others):
      provision.EnsureAgentInstructions(workspacePath, targetName)
      internal/provision/provision.go
      │
      │  Looks up instruction config:
      │    detect.GetAgentInstructionConfigForTarget(target)
      │    internal/detect/tools.go
      │
      │  Creates/updates instruction file with schmux block
      │  wrapped in <!-- SCHMUX:BEGIN --> / <!-- SCHMUX:END -->
      └──────────────────────────────────────────────────────
```

### What Gets Created

The instruction file contains:

- Explanation of the signaling protocol
- Available states and when to use them
- Code examples for signaling
- Best practices

Content is wrapped in markers for safe updates:

```markdown
<!-- SCHMUX:BEGIN -->

## Schmux Status Signaling

...instructions...

<!-- SCHMUX:END -->
```

### Provisioning Behavior

| Scenario                      | Action                                         |
| ----------------------------- | ---------------------------------------------- |
| File doesn't exist            | Create with signaling instructions             |
| File exists, no schmux block  | Append signaling block                         |
| File exists, has schmux block | Update the block (preserves user content)      |
| Unknown agent type            | No action (signaling still works via env vars) |

### Model Support

Models are mapped to their base tools via `GetBaseToolName()` (`internal/detect/tools.go`):

| Target                                                   | Base Tool | Instruction Path    |
| -------------------------------------------------------- | --------- | ------------------- |
| `claude`, `claude-opus`, `claude-sonnet`, `claude-haiku` | claude    | `.claude/CLAUDE.md` |
| `codex`                                                  | codex     | `.codex/AGENTS.md`  |
| `gemini`                                                 | gemini    | `.gemini/GEMINI.md` |
| Third-party models (kimi, etc.)                          | claude    | `.claude/CLAUDE.md` |

---

## For Agent Developers

### Detecting Schmux Environment

```bash
if [ "$SCHMUX_ENABLED" = "1" ]; then
    # Running in schmux - hooks handle signaling automatically
    # Manual signaling example:
    printf '{"ts":"%s","type":"status","state":"completed","message":"Task done"}\n' \
      "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$SCHMUX_EVENTS_FILE"
fi
```

### Integration Examples

**Bash / AI agents (Claude Code, etc.):**

Hooks handle signaling automatically for Claude Code. For manual signaling:

```bash
printf '{"ts":"%s","type":"status","state":"completed","message":"Feature implemented"}\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$SCHMUX_EVENTS_FILE"
```

**Python:**

```python
import os
import json
from datetime import datetime, timezone

def signal_schmux(state: str, message: str = ""):
    if os.environ.get("SCHMUX_ENABLED") == "1":
        events_file = os.environ.get("SCHMUX_EVENTS_FILE")
        if events_file:
            event = {
                "ts": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
                "type": "status",
                "state": state,
                "message": message,
            }
            with open(events_file, "a") as f:
                f.write(json.dumps(event) + "\n")

# Usage
signal_schmux("completed", "Implementation finished")
signal_schmux("needs_input", "Approve the changes?")
```

**Node.js:**

```javascript
const fs = require('fs');

function signalSchmux(state, message = '') {
  if (process.env.SCHMUX_ENABLED === '1') {
    const eventsFile = process.env.SCHMUX_EVENTS_FILE;
    if (eventsFile) {
      const event = JSON.stringify({
        ts: new Date().toISOString().replace(/\.\d{3}Z$/, 'Z'),
        type: 'status',
        state,
        message,
      });
      fs.appendFileSync(eventsFile, event + '\n');
    }
  }
}

// Usage
signalSchmux('completed', 'Build successful');
```

### Best Practices

1. **Signal completion** when you finish the user's request
2. **Signal needs_input** when waiting for user decisions (don't just ask in text)
3. **Signal error** for failures that block progress
4. **Signal working** when starting a new task to clear old status
5. Keep messages concise (under 100 characters)
6. Always check `SCHMUX_ENABLED` before signaling
7. Use the `SCHMUX_EVENTS_FILE` environment variable, don't hardcode paths

---

## NudgeNik Integration

### Fallback Behavior

NudgeNik provides LLM-based state classification as a fallback:

| Scenario                        | What Happens                     |
| ------------------------------- | -------------------------------- |
| Agent signals directly          | NudgeNik skipped (saves compute) |
| No signal for 5+ minutes        | NudgeNik analyzes output         |
| Agent doesn't support signaling | NudgeNik handles classification  |

### NudgeNik Polling Architecture

```
 startNudgeNikChecker() goroutine
 internal/daemon/daemon.go
 │
 │  Waits 10s on startup, then polls every 15s
 │
 ▼
 checkInactiveSessionsForNudge()
 │
 │  For each session, skip if:
 │    1. Already has a nudge (sess.Nudge != "")
 │    2. LastSignalAt < 5 minutes ago ← direct signal threshold
 │    3. Session not running
 │    4. LastOutputAt < 15s ago (still active)
 │
 │  Otherwise:
 │    nudgenik.AskForSession(ctx, cfg, sess)
 │    internal/nudgenik/nudgenik.go
 │    │
 │    │  1. Captures last 100 lines from tmux
 │    │  2. Extracts latest agent response
 │    │  3. Sends to LLM with classification prompt
 │    │  4. Parses JSON result → nudgenik.Result
 │    │
 │    ▼
 │  Publishes result to event bus
 │  Bus subscriber updates state + broadcasts
 └──────────────────────────────────────
```

### Source Distinction

The API indicates the signal source:

```json
{
  "state": "Completed",
  "summary": "Implementation finished",
  "source": "agent"
}
```

- Direct signals: `source: "agent"` — set by dashboard broadcaster bus subscriber
- NudgeNik classification: `source: "llm"` — set by NudgeNik bus publisher

---

## Implementation Details

### Package Structure

```
internal/
  bus/                  # In-process event bus (pub/sub)
    bus.go              # Bus struct, Subscribe/Publish, typed event payloads

  event/                # Event file watching and parsing
    watcher.go          # EventWatcher: fsnotify-based JSONL file watcher
    event.go            # Event struct, ParseEvent, ToSignal conversion

  signal/               # Signal types and remote watching
    signal.go           # Signal struct, state validation, state-to-display mapping
    remotewatcher.go    # Remote signal watching via tmux watcher pane
    signal_test.go

  provision/            # Agent instruction provisioning
    provision.go        # File-based and CLI-flag provisioning
    provision_test.go

  detect/               # Agent/tool detection
    tools.go            # Instruction configs, target-to-tool mapping

  session/              # Session lifecycle and signal monitoring
    tracker.go          # Session tracking for local sessions
    manager.go          # Spawn, event bus wiring, remote monitors

  dashboard/            # HTTP API and WebSocket handlers
    websocket.go        # Bus subscriber for agent status, terminal WebSocket
    server.go           # BroadcastSessions, doBroadcast, connection management

  state/                # Persistent state management
    state.go            # Session fields, atomic nudge/seq operations
    interfaces.go       # StateStore interface

  nudgenik/             # LLM-based state classification fallback
    nudgenik.go         # AskForSession, prompt building, result parsing

  daemon/               # Top-level orchestration
    daemon.go           # Wires bus subscribers, starts NudgeNik checker
```

### Key Types

**`signal.Signal`** (`internal/signal/signal.go`)

```go
type Signal struct {
    State     string    // needs_input, needs_testing, completed, error, working
    Message   string    // Optional message from the agent
    Timestamp time.Time // When the signal was detected
}
```

**`bus.Event`** (`internal/bus/bus.go`)

```go
type Event struct {
    Type      string      // e.g., "agent.status", "agent.lore", "session.created"
    SessionID string      // Which session produced this event
    Payload   interface{} // Typed payload (AgentStatusPayload, etc.)
    Seq       uint64      // Monotonic sequence number (set by bus)
}
```

**`state.Session`** signal-related fields (`internal/state/state.go`)

```go
LastSignalAt time.Time `json:"last_signal_at,omitempty"` // Last direct agent signal
NudgeSeq     uint64    `json:"nudge_seq,omitempty"`      // Monotonic notification dedup counter
Nudge        string    `json:"nudge,omitempty"`           // JSON-serialized nudgenik.Result
```

`NudgeSeq` is incremented only for **attention states** (`completed`, `needs_input`, `needs_testing`, `error`). The `working` state does not increment `NudgeSeq` because it is a clear operation — incrementing would desync the frontend ack counter.

**`nudgenik.Result`** (`internal/nudgenik/nudgenik.go`)

```go
type Result struct {
    State      string   `json:"state"`
    Confidence string   `json:"confidence,omitempty"`
    Evidence   []string `json:"evidence,omitempty"`
    Summary    string   `json:"summary"`
    Source     string   `json:"source,omitempty"` // "agent" or "llm"
}
```

Shared type between direct signals and NudgeNik responses. `Source` distinguishes origin.

**`detect.AgentInstructionConfig`** (`internal/detect/tools.go`)

```go
type AgentInstructionConfig struct {
    InstructionDir  string // e.g., ".claude", ".codex"
    InstructionFile string // e.g., "CLAUDE.md", "AGENTS.md"
}
```

### Key Functions

#### Signal Parsing (`internal/signal/`)

| Function                 | Location    | Purpose                                                                               |
| ------------------------ | ----------- | ------------------------------------------------------------------------------------- |
| `IsValidState(state)`    | `signal.go` | Checks state against `ValidStates` map.                                               |
| `MapStateToNudge(state)` | `signal.go` | Maps raw states to display strings (e.g., `"needs_input"` → `"Needs Authorization"`). |

#### Event Watching (`internal/event/`)

| Function                        | Location     | Purpose                                                      |
| ------------------------------- | ------------ | ------------------------------------------------------------ |
| `NewEventWatcher(id, path, cb)` | `watcher.go` | Constructor. Created per local session.                      |
| `Start()`                       | `watcher.go` | Starts fsnotify watcher and monitoring goroutine.            |
| `Stop()`                        | `watcher.go` | Stops watcher and cleanup.                                   |
| `ReadCurrent()`                 | `watcher.go` | Reads current state from existing event file (for recovery). |

#### Event Bus (`internal/bus/`)

| Function                    | Location | Purpose                                       |
| --------------------------- | -------- | --------------------------------------------- |
| `New()`                     | `bus.go` | Creates a new event bus.                      |
| `Subscribe(handler, types)` | `bus.go` | Registers a handler for specific event types. |
| `Publish(event)`            | `bus.go` | Dispatches event to all matching subscribers. |

#### Session Tracking (`internal/session/`)

| Function                                   | Location     | Purpose                                                                     |
| ------------------------------------------ | ------------ | --------------------------------------------------------------------------- |
| `NewSessionTracker(id, tmux, state, ...) ` | `tracker.go` | Creates tracker with event watcher for local sessions.                      |
| `run()`                                    | `tracker.go` | Main loop: starts event watcher, waits for session exit.                    |
| `ensureTrackerFromSession(sess)`           | `manager.go` | Creates or returns existing tracker. Publishes events to bus.               |
| `Spawn(...)`                               | `manager.go` | Full spawn flow: workspace, provisioning, env vars, tmux, tracker.          |
| `StartRemoteSignalMonitor(sess)`           | `manager.go` | Creates watcher pane for remote sessions, monitors events via control mode. |

#### State Management (`internal/state/`)

| Function                         | Location   | Purpose                                                               |
| -------------------------------- | ---------- | --------------------------------------------------------------------- |
| `UpdateSessionNudge(id, nudge)`  | `state.go` | Atomically sets the nudge field.                                      |
| `ClearSessionNudge(id)`          | `state.go` | Atomically clears nudge if non-empty. Returns whether it was cleared. |
| `IncrementNudgeSeq(id)`          | `state.go` | Atomically increments and returns new NudgeSeq.                       |
| `GetNudgeSeq(id)`                | `state.go` | Returns current NudgeSeq without incrementing.                        |
| `UpdateSessionLastSignal(id, t)` | `state.go` | Sets LastSignalAt timestamp.                                          |
| `UpdateSessionLastOutput(id, t)` | `state.go` | Sets LastOutputAt timestamp (for NudgeNik inactivity check).          |

#### Dashboard (`internal/dashboard/`)

| Function                                    | Location       | Purpose                                                            |
| ------------------------------------------- | -------------- | ------------------------------------------------------------------ |
| `handleAgentStatusEvent(event)`             | `websocket.go` | Bus subscriber: updates nudge, seq, saves, broadcasts immediately. |
| `handleTerminalWebSocket(w, r)`             | `websocket.go` | Local terminal WebSocket: PTY I/O, nudge clearing on user input.   |
| `handleRemoteTerminalWebSocket(w, r, sess)` | `websocket.go` | Remote terminal WebSocket: SSH/ET output, nudge clearing.          |
| `BroadcastSessions()`                       | `server.go`    | Debounced broadcast (500ms trailing timer).                        |
| `doBroadcast()`                             | `server.go`    | Immediate broadcast to all dashboard WebSocket connections.        |

#### Provisioning (`internal/provision/`)

| Function                                 | Location       | Purpose                                                 |
| ---------------------------------------- | -------------- | ------------------------------------------------------- |
| `EnsureAgentInstructions(path, target)`  | `provision.go` | Creates/updates instruction file with schmux block.     |
| `EnsureSignalingInstructionsFile()`      | `provision.go` | Writes `~/.schmux/signaling.md` for CLI-flag injection. |
| `SupportsSystemPromptFlag(tool)`         | `provision.go` | True for claude, codex (use CLI flag instead of file).  |
| `HasSignalingInstructions(path, target)` | `provision.go` | Checks if instruction file already has schmux markers.  |
| `RemoveAgentInstructions(path, target)`  | `provision.go` | Removes schmux block from instruction file.             |

#### Frontend Notification (`assets/dashboard/src/`)

| Function                         | Location               | Purpose                                                                    |
| -------------------------------- | ---------------------- | -------------------------------------------------------------------------- |
| `warmupAudioContext()`           | `notificationSound.ts` | Registers one-time user gesture listener to resume suspended AudioContext. |
| `playAttentionSound()`           | `notificationSound.ts` | Two-tone sine wave (880Hz A5 + 660Hz E5, ~300ms).                          |
| `isAttentionState(state)`        | `notificationSound.ts` | True for "Needs Authorization" and "Error".                                |
| Unified notification `useEffect` | `SessionsContext.tsx`  | Checks nudge_seq + escalation changes, plays at most one sound per cycle.  |

### Broadcast: Immediate vs Debounced

The system uses two broadcast paths:

```
 Bus subscriber (agent.status)     Other updates (git status, NudgeNik, etc.)
 │                                 │
 ▼                                 ▼
 go doBroadcast()                  BroadcastSessions()
 (immediate)                       (debounced, 500ms trailing timer)
 │                                 │
 │                                 ▼
 │                               broadcastLoop() goroutine
 │                               internal/dashboard/server.go
 │                               │  Waits for timer to fire
 │                               │
 ▼                               ▼
 doBroadcast()                   doBroadcast()
 │
 ▼
 Writes JSON to all registered dashboard WebSocket connections
```

Direct agent signals bypass the debounce timer to ensure instant delivery to the frontend. All other state changes (NudgeNik results, git status updates, user nudge clears) go through the debounced path to avoid flooding clients.

### NudgeSeq and Frontend Notification Dedup

```
 Backend:                              Frontend:
 ┌─────────────────────┐               ┌──────────────────────────────┐
 │ Bus subscriber      │               │ SessionsContext useEffect    │
 │ (agent.status)      │               │ (unified notification)       │
 │                     │               │                              │
 │ "working" signal:   │   WebSocket   │ For each session:            │
 │   NudgeSeq unchanged│ ──────────>   │   nudge_seq from server      │
 │   Nudge cleared     │               │   lastAcked from localStorage│
 │                     │               │                              │
 │ Attention signals:  │               │ If nudge_seq > lastAcked     │
 │   NudgeSeq++        │               │ AND isAttentionState():      │
 │   Nudge set to JSON │               │   → playAttentionSound()     │
 │                     │               │   → update localStorage      │
 └─────────────────────┘               │                              │
                                       │ Also checks escalation:      │
 "working" does NOT increment          │   If new escalation string:  │
 NudgeSeq because it is a clear        │   → playAttentionSound()     │
 operation. Only attention states       │   → showBrowserNotification()│
 (completed, needs_input,              │                              │
 needs_testing, error) increment it.   │ At most one sound per cycle. │
                                       └──────────────────────────────┘
```

### Event Bus Wiring

The event bus is created at daemon startup and subscribers are registered before any sessions are restored:

```
 daemon.go — Creates bus and wires subscribers:
 │
 ├─ Dashboard broadcaster subscriber ("agent.status"):
 │    Updates nudge state, increments NudgeSeq, broadcasts
 │
 ├─ Floor manager injector subscriber ("agent.status"):
 │    Feeds agent status changes to the floor manager session
 │
 ├─ Floor manager lifecycle subscriber:
 │    ("session.created", "session.disposed",
 │     "workspace.created", "workspace.deleted")
 │    Feeds lifecycle events to the floor manager session
 │
 ├─ Escalation subscriber ("escalation.set", "escalation.cleared"):
 │    Updates escalation state in session store
 │
 └─ NudgeNik subscriber ("nudgenik.result"):
     Updates nudge state from LLM classification
```

Producers publish to the bus:

- **EventWatcher** → `agent.status`, `agent.lore` events
- **RemoteSignalWatcher** → `agent.status` events
- **Session/Workspace managers** → lifecycle events
- **NudgeNik checker** → `nudgenik.result` events
- **Escalation API** → `escalation.set`, `escalation.cleared` events

---

## Troubleshooting

### Verify Signaling Works

1. Spawn a session in schmux
2. In the terminal, run: `printf '{"ts":"%s","type":"status","state":"completed","message":"Test"}\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$SCHMUX_EVENTS_FILE"`
3. Check the dashboard - the session should show a completion status

### Check Environment Variables

In a schmux session:

```bash
echo $SCHMUX_ENABLED        # Should be "1"
echo $SCHMUX_SESSION_ID     # Should show session ID
echo $SCHMUX_WORKSPACE_ID   # Should show workspace ID
echo $SCHMUX_EVENTS_FILE    # Should show path to event file
```

### Check Event File

```bash
ls -la $SCHMUX_EVENTS_FILE  # Should exist in .schmux/events/ directory
tail -1 $SCHMUX_EVENTS_FILE # Shows latest event (if agent has signaled)
cat $SCHMUX_EVENTS_FILE     # Shows full event history
```

### Check Instruction File Was Created

```bash
ls -la .claude/CLAUDE.md    # For Claude Code sessions
cat .claude/CLAUDE.md       # Should contain SCHMUX:BEGIN marker
```

### Why Isn't My Agent Signaling?

1. **Agent doesn't read instruction files** - Some agents may not read from the expected location
2. **Agent ignores instructions** - The agent may not follow the signaling protocol
3. **Event file not writable** - Check `.schmux/events/` directory permissions
4. **Signaling works, display doesn't** - Check browser console for WebSocket errors

### Invalid Signals

Only signals with valid schmux states are processed. Invalid states are logged but ignored.

Valid states: `needs_input`, `needs_testing`, `completed`, `error`, `working`

---

## Adding Support for New Agents

To add signaling support for a new agent:

1. **Add instruction config** in `internal/detect/tools.go`:

   ```go
   var agentInstructionConfigs = map[string]AgentInstructionConfig{
       // ...existing...
       "newagent": {InstructionDir: ".newagent", InstructionFile: "INSTRUCTIONS.md"},
   }
   ```

2. **Add detector** in `internal/detect/agents.go` (if not already detected)

3. **Test**: Spawn a session with the new agent, verify instruction file is created

---

## Design Principles

1. **Non-destructive**: Never modify user's existing instruction content
2. **Automatic**: No manual setup required - works out of the box
3. **Agent-agnostic**: Protocol works for any agent that can append JSON to a file
4. **Graceful fallback**: NudgeNik handles agents that don't signal
5. **Single source of truth**: One event file per session, one event bus for routing
6. **Append-only events**: Full history preserved, no data loss from overwrites
