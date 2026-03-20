# Agent Signaling

Schmux provides a comprehensive system for agents to communicate their status to users in real-time.

## Overview

The agent signaling system has three components:

1. **Direct Signaling** - Agents write status to a file to signal their state
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
│  Agent writes: echo "completed Done" > $SCHMUX_STATUS_FILE      │
│  Schmux file watcher detects change → updates dashboard         │
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

Agents signal their state by writing to a status file provided by schmux:

```bash
echo "STATE message" > $SCHMUX_STATUS_FILE
```

The `SCHMUX_STATUS_FILE` environment variable contains the path to the status file (typically `$WORKSPACE/.schmux/signal/<session-id>`).

**Examples:**

```bash
# Signal completion
echo "completed Implementation complete, ready for review" > $SCHMUX_STATUS_FILE

# Signal needs input
echo "needs_input Waiting for permission to delete files" > $SCHMUX_STATUS_FILE

# Signal error
echo "error Build failed with 3 errors" > $SCHMUX_STATUS_FILE

# Signal needs testing
echo "needs_testing Please test the new feature" > $SCHMUX_STATUS_FILE

# Clear signal (starting new work)
echo "working" > $SCHMUX_STATUS_FILE
```

**Benefits:**

- **Simple and reliable** - Plain file writes are universally supported
- **No parsing complexity** - No ANSI stripping or regex matching needed
- **Idempotent** - File content represents current state
- **Easy to debug** - Just read the file to see current status

### Valid States

| State           | Meaning                              | Dashboard Display     |
| --------------- | ------------------------------------ | --------------------- |
| `completed`     | Task finished successfully           | ✓ Completed           |
| `needs_input`   | Waiting for user authorization/input | ⚠ Needs Authorization |
| `needs_testing` | Ready for user testing               | 🧪 Needs User Testing |
| `error`         | Error occurred, needs intervention   | ❌ Error              |
| `working`       | Actively working on a task           | ⏳ Working            |

### How Signals Flow

The signal pipeline spans the full stack, from agent file writes to browser notification sound. Here is the complete data flow with code references:

```
 Agent (in tmux session)
 │
 │  Writes: echo "completed Done" > $SCHMUX_STATUS_FILE
 │
 ▼
 ┌──────────────────────────────────────────────────────────┐
 │  LOCAL SESSIONS:                                         │
 │  FileWatcher.watch() goroutine                           │
 │  internal/signal/filewatcher.go:82                       │
 │                                                          │
 │  fsnotify watcher receives Write event                   │
 │  Reads file content                                      │
 │  Compares with last-read content (string comparison)     │
 │  If changed: parses STATE MESSAGE format                 │
 │  Invokes callback with Signal                            │
 └──────────────────────────────────┬───────────────────────┘
                                    │
 ┌──────────────────────────────────┴───────────────────────┐
 │  REMOTE SESSIONS:                                        │
 │  Watcher pane in tmux (shell script)                     │
 │  internal/session/manager.go:1116                        │
 │                                                          │
 │  inotifywait or polling detects file change              │
 │  Emits: __SCHMUX_SIGNAL__completed Done__END__           │
 │  Received via %output event in control mode              │
 │  Parsed by RemoteMonitor.handleControlOutput()           │
 │  internal/session/remote.go:196                          │
 └──────────────────────────────────┬───────────────────────┘
                                    │  callback(Signal)
                                    ▼
 ┌──────────────────────────────────────────────────────────┐
 │  Manager.signalCallback(sessionID, sig)                  │
 │  Closure set in internal/session/manager.go:88           │
 │  Wired in internal/daemon/daemon.go:377                  │
 │                                                          │
 │  Routes to dashboard server:                             │
 └──────────────────────────────────┬───────────────────────┘
                                    │
                                    ▼
 ┌──────────────────────────────────────────────────────────┐
 │  Server.HandleAgentSignal(sessionID, sig)                │
 │  internal/dashboard/websocket.go:308                     │
 │                                                          │
 │  1. MapStateToNudge(sig.State) → display string          │
 │     internal/signal/signal.go:211                        │
 │  2. If "working": clear nudge                            │
 │     state.UpdateSessionNudge(id, "")                     │
 │     internal/state/state.go:379                          │
 │  3. Otherwise: serialize nudge JSON, set nudge           │
 │     state.UpdateSessionNudge(id, payload)                │
 │  4. state.UpdateSessionLastSignal(id, timestamp)         │
 │     internal/state/state.go:340                          │
 │  5. state.IncrementNudgeSeq(id) [non-working only]       │
 │     internal/state/state.go:352                          │
 │  6. state.Save() → persist to ~/.schmux/state.json       │
 │  7. go doBroadcast() → immediate WebSocket push          │
 │     internal/dashboard/server.go:669                     │
 └──────────────────────────────────┬───────────────────────┘
                                    │  JSON via WebSocket
                                    ▼
 ┌──────────────────────────────────────────────────────────┐
 │  Dashboard WebSocket clients                             │
 │  internal/dashboard/server.go:669 (doBroadcast)          │
 │                                                          │
 │  Sends {type:"sessions", workspaces:[...]} to all        │
 │  registered dashboard connections                        │
 └──────────────────────────────────┬───────────────────────┘
                                    │
                                    ▼
 ┌──────────────────────────────────────────────────────────┐
 │  Frontend: SessionsContext.tsx                           │
 │  assets/dashboard/src/contexts/SessionsContext.tsx:73    │
 │                                                          │
 │  useEffect detects nudge_seq change:                     │
 │  - Compares session.nudge_seq vs                         │
 │    localStorage["schmux:ack:{sessionId}"]                │
 │  - If nudge_seq > lastAcked AND isAttentionState():      │
 │    → playAttentionSound()                                │
 │      assets/dashboard/src/lib/notificationSound.ts:50    │
 │    → localStorage.setItem(storageKey, nudge_seq)         │
 └──────────────────────────────────────────────────────────┘
```

#### Daemon Restart Recovery

When the daemon restarts, existing sessions recover by simply reading the current status file:

```
 Daemon starts → restores sessions
 internal/daemon/daemon.go:382
 │
 ▼
 For local sessions:
 FileWatcher.watch() reads current file content
 internal/signal/filewatcher.go:82
 │
 │  If file exists and has content:
 │    Parse and invoke callback once
 │    Continue watching for changes
 │
 └──────────────────────────────────────────────

 For remote sessions:
 Watcher pane script reads current file
 internal/session/manager.go:1116
 │
 │  Initial check() call reads file content
 │  Emits current state if file exists
 │  Continues watching for changes
 │
 └──────────────────────────────────────────────
```

The file content represents the current state, so no scrollback parsing is needed.

#### Nudge Clearing (user interaction)

When the user types in a terminal WebSocket session, the nudge is automatically cleared:

```
 User presses Enter/Tab in terminal
 │
 ▼
 handleTerminalWebSocket / handleRemoteTerminalWebSocket
 internal/dashboard/websocket.go:259 (local) / :510 (remote)
 │
 │  Detects \r (Enter), \t (Tab), or \x1b[Z (Shift-Tab)
 │  in the input message
 │
 ▼
 state.ClearSessionNudge(sessionID) → returns true if cleared
 internal/state/state.go:393
 │
 ▼
 state.Save() + BroadcastSessions()
 internal/dashboard/server.go:616
```

---

## Environment Variables

Every spawned session receives these environment variables:

| Variable              | Example                                          | Purpose                        |
| --------------------- | ------------------------------------------------ | ------------------------------ |
| `SCHMUX_ENABLED`      | `1`                                              | Indicates running in schmux    |
| `SCHMUX_SESSION_ID`   | `myproj-abc-xyz12345`                            | Unique session identifier      |
| `SCHMUX_WORKSPACE_ID` | `myproj-abc`                                     | Workspace identifier           |
| `SCHMUX_STATUS_FILE`  | `/path/to/workspace/.schmux/signal/<session-id>` | Status file path for signaling |

Agents can check `SCHMUX_ENABLED=1` to conditionally enable signaling. The `SCHMUX_STATUS_FILE` variable provides the path where agents should write status updates.

Environment variables are injected during spawn by `Manager.Spawn()` (`internal/session/manager.go:414`).

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
 internal/session/manager.go:414
 │
 ├─ CLI-flag tools (claude, codex):
 │    provision.SupportsSystemPromptFlag(toolName)
 │    internal/provision/provision.go:218
 │    │
 │    ▼
 │    provision.EnsureSignalingInstructionsFile()
 │    internal/provision/provision.go:239
 │    │  Writes SignalingInstructions template to
 │    │  ~/.schmux/signaling.md
 │    │
 │    ▼
 │    buildCommand() injects CLI flag:
 │    internal/session/manager.go:704
 │      Claude: --append-system-prompt-file ~/.schmux/signaling.md
 │      Codex:  -c model_instructions_file=~/.schmux/signaling.md
 │
 └─ File-based tools (gemini, others):
      provision.EnsureAgentInstructions(workspacePath, targetName)
      internal/provision/provision.go:77
      │
      │  Looks up instruction config:
      │    detect.GetAgentInstructionConfigForTarget(target)
      │    internal/detect/tools.go:84
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

Models are mapped to their base tools via `models.Manager.ResolveTargetToTool()` (`internal/models/manager.go`):

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
    # Running in schmux - use signaling
    echo "completed Task done" > "$SCHMUX_STATUS_FILE"
fi
```

### Integration Examples

**Bash / AI agents (Claude Code, etc.):**

Write status to the file:

```bash
echo "completed Feature implemented successfully" > "$SCHMUX_STATUS_FILE"
```

**Python:**

```python
import os

def signal_schmux(state: str, message: str = ""):
    if os.environ.get("SCHMUX_ENABLED") == "1":
        status_file = os.environ.get("SCHMUX_STATUS_FILE")
        if status_file:
            with open(status_file, "w") as f:
                if message:
                    f.write(f"{state} {message}\n")
                else:
                    f.write(f"{state}\n")

# Usage
signal_schmux("completed", "Implementation finished")
signal_schmux("needs_input", "Approve the changes?")
```

**Node.js:**

```javascript
const fs = require('fs');

function signalSchmux(state, message = '') {
  if (process.env.SCHMUX_ENABLED === '1') {
    const statusFile = process.env.SCHMUX_STATUS_FILE;
    if (statusFile) {
      const content = message ? `${state} ${message}\n` : `${state}\n`;
      fs.writeFileSync(statusFile, content);
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
7. Use the `SCHMUX_STATUS_FILE` environment variable, don't hardcode paths

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
 internal/daemon/daemon.go:573
 │
 │  Waits 10s on startup, then polls every 15s
 │
 ▼
 checkInactiveSessionsForNudge()
 internal/daemon/daemon.go:598
 │
 │  For each session, skip if:
 │    1. Already has a nudge (sess.Nudge != "")
 │    2. LastSignalAt < 5 minutes ago ← direct signal threshold
 │    3. Session not running
 │    4. LastOutputAt < 15s ago (still active)
 │
 │  Otherwise:
 │    nudgenik.AskForSession(ctx, cfg, sess)
 │    internal/nudgenik/nudgenik.go:83
 │    │
 │    │  1. Captures last 100 lines from tmux
 │    │  2. Extracts latest agent response
 │    │  3. Sends to LLM with classification prompt
 │    │  4. Parses JSON result → nudgenik.Result
 │    │     internal/nudgenik/nudgenik.go:151
 │    │
 │    ▼
 │  Saves nudge to session state
 │  Calls BroadcastSessions() (debounced)
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

- Direct signals: `source: "agent"` — set by `HandleAgentSignal` (`websocket.go:308`)
- NudgeNik classification: `source: "llm"` — set by `askNudgeNikForSession` (`daemon.go`)

---

## Implementation Details

### Package Structure

```
internal/
  signal/               # Signal parsing and file watching
    signal.go           # Signal struct, state validation, state-to-display mapping
    filewatcher.go      # File-based signal watching for local sessions
    signal_test.go
    filewatcher_test.go

  provision/            # Agent instruction provisioning
    provision.go        # File-based and CLI-flag provisioning
    provision_test.go

  detect/               # Agent/tool detection
    tools.go            # Instruction configs, target-to-tool mapping

  session/              # Session lifecycle and signal monitoring
    tracker.go          # Session tracking for local sessions
    manager.go          # Spawn, signal callback, remote monitors
    remote.go           # Remote session signal monitoring via watcher pane

  dashboard/            # HTTP API and WebSocket handlers
    websocket.go        # HandleAgentSignal, terminal WebSocket, nudge clearing
    server.go           # BroadcastSessions, doBroadcast, connection management

  state/                # Persistent state management
    state.go            # Session fields, atomic nudge/seq operations
    interfaces.go       # StateStore interface

  nudgenik/             # LLM-based state classification fallback
    nudgenik.go         # AskForSession, prompt building, result parsing

  daemon/               # Top-level orchestration
    daemon.go           # Wires signal callback, starts NudgeNik checker
```

### Key Types

**`signal.Signal`** (`internal/signal/signal.go:21`)

```go
type Signal struct {
    State     string    // needs_input, needs_testing, completed, error, working
    Message   string    // Optional message from the agent
    Timestamp time.Time // When the signal was detected
}
```

**`signal.FileWatcher`** (`internal/signal/filewatcher.go:21`)

```go
type FileWatcher struct {
    sessionID    string
    filePath     string
    callback     func(Signal)
    watcher      *fsnotify.Watcher
    lastContent  string          // For deduplication
    mu           sync.Mutex
}
```

**`state.Session`** signal-related fields (`internal/state/state.go:90-96`)

```go
LastSignalAt time.Time `json:"last_signal_at,omitempty"` // Last direct agent signal
NudgeSeq     uint64    `json:"nudge_seq,omitempty"`      // Monotonic notification dedup counter
Nudge        string    `json:"nudge,omitempty"`           // JSON-serialized nudgenik.Result
```

`NudgeSeq` is incremented by all direct agent signals, but not by NudgeNik polls or manual clears. This prevents spurious frontend notifications.

**`nudgenik.Result`** (`internal/nudgenik/nudgenik.go:74`)

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

**`detect.AgentInstructionConfig`** (`internal/detect/tools.go:24`)

```go
type AgentInstructionConfig struct {
    InstructionDir  string // e.g., ".claude", ".codex"
    InstructionFile string // e.g., "CLAUDE.md", "AGENTS.md"
}
```

### Key Functions

#### Signal Parsing (`internal/signal/`)

| Function                 | Location       | Purpose                                                                               |
| ------------------------ | -------------- | ------------------------------------------------------------------------------------- |
| `ParseSignalFile(data)`  | `signal.go:40` | Parses "STATE MESSAGE" format from file content. Returns Signal or error.             |
| `IsValidState(state)`    | `signal.go:67` | Checks state against `ValidStates` map.                                               |
| `MapStateToNudge(state)` | `signal.go:85` | Maps raw states to display strings (e.g., `"needs_input"` → `"Needs Authorization"`). |

#### File Watching (`internal/signal/`)

| Function                       | Location            | Purpose                                                      |
| ------------------------------ | ------------------- | ------------------------------------------------------------ |
| `NewFileWatcher(id, path, cb)` | `filewatcher.go:36` | Constructor. Created per local session.                      |
| `Start()`                      | `filewatcher.go:58` | Starts fsnotify watcher and monitoring goroutine.            |
| `Stop()`                       | `filewatcher.go:77` | Stops watcher and cleanup.                                   |
| `watch()`                      | `filewatcher.go:82` | Internal goroutine: reads file on changes, invokes callback. |

#### Session Tracking (`internal/session/`)

| Function                                 | Location          | Purpose                                                                              |
| ---------------------------------------- | ----------------- | ------------------------------------------------------------------------------------ |
| `NewSessionTracker(id, tmux, state, cb)` | `tracker.go:62`   | Creates tracker with file watcher for local sessions.                                |
| `run()`                                  | `tracker.go:162`  | Main loop: starts file watcher, waits for session exit.                              |
| `SetSignalCallback(cb)`                  | `manager.go:88`   | Sets the manager-level callback. Must be called before tracker creation.             |
| `ensureTrackerFromSession(sess)`         | `manager.go:1248` | Creates or returns existing tracker. Wraps callback with session ID binding.         |
| `Spawn(...)`                             | `manager.go:414`  | Full spawn flow: workspace, provisioning, env vars, tmux, tracker.                   |
| `StartRemoteSignalMonitor(sess)`         | `manager.go:96`   | Creates watcher pane for remote sessions, monitors %output events for signals.       |
| `createSignalWatcherPane(sess)`          | `manager.go:1116` | Creates hidden tmux pane that watches status file and emits sentinel-wrapped output. |

#### State Management (`internal/state/`)

| Function                         | Location       | Purpose                                                               |
| -------------------------------- | -------------- | --------------------------------------------------------------------- |
| `UpdateSessionNudge(id, nudge)`  | `state.go:379` | Atomically sets the nudge field.                                      |
| `ClearSessionNudge(id)`          | `state.go:393` | Atomically clears nudge if non-empty. Returns whether it was cleared. |
| `IncrementNudgeSeq(id)`          | `state.go:352` | Atomically increments and returns new NudgeSeq.                       |
| `GetNudgeSeq(id)`                | `state.go:365` | Returns current NudgeSeq without incrementing.                        |
| `UpdateSessionLastSignal(id, t)` | `state.go:340` | Sets LastSignalAt timestamp.                                          |
| `UpdateSessionLastOutput(id, t)` | `state.go:327` | Sets LastOutputAt timestamp (for NudgeNik inactivity check).          |

#### Dashboard (`internal/dashboard/`)

| Function                                    | Location           | Purpose                                                                        |
| ------------------------------------------- | ------------------ | ------------------------------------------------------------------------------ |
| `HandleAgentSignal(id, sig)`                | `websocket.go:308` | Central signal handler: updates nudge, seq, saves, broadcasts immediately.     |
| `handleTerminalWebSocket(w, r)`             | `websocket.go:75`  | Local terminal WebSocket: PTY I/O, nudge clearing on user input.               |
| `handleRemoteTerminalWebSocket(w, r, sess)` | `websocket.go:360` | Remote terminal WebSocket: SSH/ET output, nudge clearing.                      |
| `BroadcastSessions()`                       | `server.go:616`    | Debounced broadcast (500ms trailing timer). Used by NudgeNik, git status, etc. |
| `doBroadcast()`                             | `server.go:669`    | Immediate broadcast to all dashboard WebSocket connections.                    |

#### Provisioning (`internal/provision/`)

| Function                                 | Location           | Purpose                                                 |
| ---------------------------------------- | ------------------ | ------------------------------------------------------- |
| `EnsureAgentInstructions(path, target)`  | `provision.go:77`  | Creates/updates instruction file with schmux block.     |
| `EnsureSignalingInstructionsFile()`      | `provision.go:239` | Writes `~/.schmux/signaling.md` for CLI-flag injection. |
| `SupportsSystemPromptFlag(tool)`         | `provision.go:218` | True for claude, codex (use CLI flag instead of file).  |
| `HasSignalingInstructions(path, target)` | `provision.go:252` | Checks if instruction file already has schmux markers.  |
| `RemoveAgentInstructions(path, target)`  | `provision.go:164` | Removes schmux block from instruction file.             |

#### Frontend Notification (`assets/dashboard/src/`)

| Function                    | Location                  | Purpose                                                                        |
| --------------------------- | ------------------------- | ------------------------------------------------------------------------------ |
| `warmupAudioContext()`      | `notificationSound.ts:19` | Registers one-time user gesture listener to resume suspended AudioContext.     |
| `playAttentionSound()`      | `notificationSound.ts:50` | Two-tone sine wave (880Hz A5 + 660Hz E5, ~300ms).                              |
| `isAttentionState(state)`   | `notificationSound.ts:97` | True for "Needs Authorization" and "Error".                                    |
| Nudge detection `useEffect` | `SessionsContext.tsx:73`  | Compares `nudge_seq` vs `localStorage["schmux:ack:{id}"]`, plays sound if new. |

### Broadcast: Immediate vs Debounced

The system uses two broadcast paths:

```
 HandleAgentSignal          Other updates (git status, NudgeNik, etc.)
 │                          │
 ▼                          ▼
 go doBroadcast()           BroadcastSessions()
 (immediate)                (debounced, 500ms trailing timer)
 │                          │
 │                          ▼
 │                        broadcastLoop() goroutine
 │                        internal/dashboard/server.go:644
 │                        │  Waits for timer to fire
 │                        │
 ▼                        ▼
 doBroadcast()            doBroadcast()
 internal/dashboard/server.go:669
 │
 ▼
 Writes JSON to all registered dashboard WebSocket connections
```

Direct agent signals bypass the debounce timer to ensure instant delivery to the frontend. All other state changes (NudgeNik results, git status updates, user nudge clears) go through the debounced path to avoid flooding clients.

### NudgeSeq and Frontend Notification Dedup

```
 Backend:                              Frontend:
 ┌─────────────────────┐               ┌──────────────────────────────┐
 │ HandleAgentSignal   │               │ SessionsContext useEffect    │
 │                     │               │                              │
 │ "working" signal:   │   WebSocket   │ For each session:            │
 │   NudgeSeq unchanged│ ──────────>   │   nudge_seq from server      │
 │   Nudge cleared     │               │   lastAcked from localStorage│
 │                     │               │                              │
 │ Other signals:      │               │ If nudge_seq > lastAcked     │
 │   NudgeSeq++        │               │ AND isAttentionState():      │
 │   Nudge set to JSON │               │   → playAttentionSound()     │
 │                     │               │   → update localStorage      │
 └─────────────────────┘               └──────────────────────────────┘

 "working" does NOT increment NudgeSeq because it is a clear
 operation. Incrementing would cause the frontend to see
 nudge_seq > lastAcked but with no attention state, which
 would desync the ack counter.
```

### Signal Callback Wiring Chain

The signal callback is wired at daemon startup and flows through three layers:

```
 daemon.go:377 — Sets Manager.signalCallback:
 │
 │  sm.SetSignalCallback(func(sessionID, sig) {
 │      server.HandleAgentSignal(sessionID, sig)
 │  })
 │
 ▼
 manager.go:1248 — ensureTrackerFromSession wraps with session ID:
 │
 │  signalCB := func(sig Signal) {
 │      m.signalCallback(sess.ID, sig)
 │  }
 │
 ▼
 tracker.go:62 — NewSessionTracker creates FileWatcher:
 │
 │  fileWatcher = NewFileWatcher(sessionID, signalFilePath, signalCB)
 │
 ▼
 filewatcher.go:82 — watch() goroutine reads file on fsnotify events, invokes callback
```

This wiring MUST happen before any tracker creation (`daemon.go:376` comment). If `SetSignalCallback` is called after trackers exist, those trackers will have a nil callback and silently drop signals.

---

## Troubleshooting

### Verify Signaling Works

1. Spawn a session in schmux
2. In the terminal, run: `echo "completed Test signal" > $SCHMUX_STATUS_FILE`
3. Check the dashboard - the session should show a completion status

### Check Environment Variables

In a schmux session:

```bash
echo $SCHMUX_ENABLED        # Should be "1"
echo $SCHMUX_SESSION_ID     # Should show session ID
echo $SCHMUX_WORKSPACE_ID   # Should show workspace ID
echo $SCHMUX_STATUS_FILE    # Should show path to status file
```

### Check Status File

```bash
ls -la $SCHMUX_STATUS_FILE  # Should exist in .schmux directory
cat $SCHMUX_STATUS_FILE     # Shows current status (if agent has signaled)
```

### Check Instruction File Was Created

```bash
ls -la .claude/CLAUDE.md    # For Claude Code sessions
cat .claude/CLAUDE.md       # Should contain SCHMUX:BEGIN marker
```

### Why Isn't My Agent Signaling?

1. **Agent doesn't read instruction files** - Some agents may not read from the expected location
2. **Agent ignores instructions** - The agent may not follow the signaling protocol
3. **Status file not writable** - Check `.schmux/` directory permissions
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
3. **Agent-agnostic**: Protocol works for any agent that can write to a file
4. **Graceful fallback**: NudgeNik handles agents that don't signal
5. **Simple and reliable**: Plain file writes instead of terminal output parsing
