# Agent Signaling

Schmux provides a comprehensive system for agents to communicate their status to users in real-time.

## Overview

The agent signaling system has three components:

1. **Direct Signaling** - Agents output bracket-based markers to signal their state
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
│  Agent outputs: --<[schmux:completed:Done]>--                   │
│  Schmux PTY tracker detects signal → updates dashboard          │
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

Agents signal their state by outputting a bracket-based text marker **on its own line** in their response:

```
--<[schmux:state:message]>--
```

**Important:** The signal must be on a separate line by itself. Signals embedded within other text are ignored.

**Examples:**

```
# Signal completion
--<[schmux:completed:Implementation complete, ready for review]>--

# Signal needs input
--<[schmux:needs_input:Waiting for permission to delete files]>--

# Signal error
--<[schmux:error:Build failed with 3 errors]>--

# Signal needs testing
--<[schmux:needs_testing:Please test the new feature]>--

# Clear signal (starting new work)
--<[schmux:working:]>--
```

**Benefits:**

- **Passes through markdown** - Unlike HTML comments, bracket markers are visible in rendered output
- **Looks benign** - If not stripped, the marker looks like an innocuous code annotation
- **Highly unique** - The format is extremely unlikely to appear naturally in agent output

### Valid States

| State           | Meaning                                   | Dashboard Display     |
| --------------- | ----------------------------------------- | --------------------- |
| `completed`     | Task finished successfully                | ✓ Completed           |
| `needs_input`   | Waiting for user authorization/input      | ⚠ Needs Authorization |
| `needs_testing` | Ready for user testing                    | 🧪 Needs User Testing |
| `error`         | Error occurred, needs intervention        | ❌ Error              |
| `working`       | Actively working (clears previous signal) | (clears status)       |

### How Signals Flow

The signal pipeline spans the full stack, from agent terminal output to browser notification sound. Here is the complete data flow with code references:

```
 Agent (in tmux session)
 │
 │  Outputs: --<[schmux:completed:Done]>--
 │
 ▼
 tmux captures output in terminal buffer
 │
 ▼
 ┌──────────────────────────────────────────────────────────┐
 │  SessionTracker.attachAndRead()                          │
 │  internal/session/tracker.go:190                         │
 │                                                          │
 │  PTY read loop reads 8KB chunks from tmux attach-session │
 │  Splits on UTF-8 boundaries (findValidUTF8Boundary)      │
 │  Feeds each chunk to SignalDetector                      │
 └──────────────────────────────────┬───────────────────────┘
                                    │
                                    ▼
 ┌──────────────────────────────────────────────────────────┐
 │  SignalDetector.Feed(chunk)                              │
 │  internal/signal/detector.go:47                          │
 │                                                          │
 │  Appends chunk to internal line buffer                   │
 │  Splits on last newline:                                 │
 │    - Complete lines → parseLines()                       │
 │    - Remainder → buffered for next Feed()                │
 │  If no newline at all → buffer + enforceBufLimit()       │
 └──────────────────────────────────┬───────────────────────┘
                                    │
                                    ▼
 ┌──────────────────────────────────────────────────────────┐
 │  SignalDetector.parseLines(data)                         │
 │  internal/signal/detector.go:96                          │
 │                                                          │
 │  1. StripANSIBytes() - removes ANSI escape sequences     │
 │     internal/signal/signal.go:40                         │
 │  2. parseBracketSignals() - matches regex against data   │
 │     internal/signal/signal.go:173                        │
 │     Regex: ^[prefix]*--<\[schmux:(\w+):([^\]]*)\]>--$    │
 │     internal/signal/signal.go:33                         │
 │  3. For each valid Signal → invoke callback              │
 │  4. If no signals found → near-miss detection            │
 └──────────────────────────────────┬───────────────────────┘
                                    │  callback(Signal)
                                    │  (unless suppressed)
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

#### Flush Ticker (signals without trailing newlines)

Agents may emit a signal as the last thing before going silent (no trailing newline). Without intervention, the signal sits in the detector's line buffer indefinitely. A flush ticker goroutine handles this:

```
 ┌───────────────────────────────────────────────────────────┐
 │  Flush ticker goroutine                                   │
 │  internal/session/tracker.go:244                          │
 │                                                           │
 │  Ticks every 500ms (signal.FlushTimeout)                  │
 │  If SignalDetector.ShouldFlush() == true:                 │
 │    → SignalDetector.Flush()                               │
 │    → Forces parseLines() on buffered data                 │
 │                                                           │
 │  ShouldFlush() returns true when:                         │
 │    - Buffer is non-empty                                  │
 │    - No data received for >= 500ms                        │
 │    internal/signal/detector.go:89                         │
 └───────────────────────────────────────────────────────────┘
```

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

#### Scrollback Recovery (daemon restart)

When the daemon restarts, existing sessions may have emitted signals while the daemon was down. The tracker recovers by parsing scrollback with suppression enabled:

```
 Daemon starts → restores trackers for existing sessions
 internal/daemon/daemon.go:382
 │
 ▼
 SessionTracker.attachAndRead() (first attach)
 internal/session/tracker.go:214
 │
 │  1. signalDetector.Suppress(true)
 │  2. tmux.CaptureLastLines(200 lines)
 │  3. signalDetector.Feed(scrollback) + Flush()
 │  4. signalDetector.Suppress(false)
 │
 │  This initializes internal detector state without
 │  re-firing callbacks (which would cause duplicate
 │  notifications for already-seen signals).
 └──────────────────────────────────────────────────
```

### Why Bracket Markers

- **Works for text-based agents**: Any agent that generates text responses can signal
- **Passes through markdown**: Unlike HTML comments, visible in rendered output
- **Highly unique**: Extremely unlikely to appear naturally in agent output
- **Looks benign**: If displayed, appears as an innocuous annotation

---

## Environment Variables

Every spawned session receives these environment variables:

| Variable              | Example               | Purpose                     |
| --------------------- | --------------------- | --------------------------- |
| `SCHMUX_ENABLED`      | `1`                   | Indicates running in schmux |
| `SCHMUX_SESSION_ID`   | `myproj-abc-xyz12345` | Unique session identifier   |
| `SCHMUX_WORKSPACE_ID` | `myproj-abc`          | Workspace identifier        |

Agents can check `SCHMUX_ENABLED=1` to conditionally enable signaling.

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

Models are mapped to their base tools via `GetBaseToolName()` (`internal/detect/tools.go:59`):

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
    echo "--<[schmux:completed:Task done]>--"
fi
```

### Integration Examples

**Bash / AI agents (Claude Code, etc.):**

Output the signal marker on its own line:

```
--<[schmux:completed:Feature implemented successfully]>--
```

Note: The signal must be on a separate line — do not embed it within other text.

**Python:**

```python
import os

def signal_schmux(state: str, message: str = ""):
    if os.environ.get("SCHMUX_ENABLED") == "1":
        # Output the signal marker
        print(f"--<[schmux:{state}:{message}]>--")

# Usage
signal_schmux("completed", "Implementation finished")
signal_schmux("needs_input", "Approve the changes?")
```

**Node.js:**

```javascript
function signalSchmux(state, message = '') {
  if (process.env.SCHMUX_ENABLED === '1') {
    // Output the signal marker
    console.log(`--<[schmux:${state}:${message}]>--`);
  }
}

// Usage
signalSchmux('completed', 'Build successful');
```

### Best Practices

1. **Signal on its own line** - signals embedded in text are ignored
2. **Signal completion** when you finish the user's request
3. **Signal needs_input** when waiting for user decisions (don't just ask in text)
4. **Signal error** for failures that block progress
5. **Signal working** when starting a new task to clear old status
6. Keep messages concise (under 100 characters)
7. Do not use `]` in the message — it terminates the marker early

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
  signal/               # Signal parsing and detection
    signal.go           # Signal struct, regex, ANSI stripping, state validation
    detector.go         # Line-buffered detector with flush ticker
    signal_test.go
    detector_test.go

  provision/            # Agent instruction provisioning
    provision.go        # File-based and CLI-flag provisioning
    provision_test.go

  detect/               # Agent/tool detection
    tools.go            # Instruction configs, target-to-tool mapping

  session/              # Session lifecycle and PTY tracking
    tracker.go          # PTY read loop, signal detection wiring
    manager.go          # Spawn, signal callback, remote monitors

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

**`signal.SignalDetector`** (`internal/signal/detector.go:16`)

```go
type SignalDetector struct {
    sessionID        string
    callback         func(Signal)       // Invoked for each parsed signal
    nearMissCallback func(string)       // Invoked for malformed signals
    suppressed       atomic.Bool        // Disables callback during scrollback
    mu               sync.Mutex
    buf              []byte             // Partial line buffer
    stripBuf         []byte             // Reusable ANSI strip buffer
    lastData         time.Time          // For ShouldFlush() staleness check
}
```

**`state.Session`** signal-related fields (`internal/state/state.go:90-96`)

```go
LastSignalAt time.Time `json:"last_signal_at,omitempty"` // Last direct agent signal
NudgeSeq     uint64    `json:"nudge_seq,omitempty"`      // Monotonic notification dedup counter
Nudge        string    `json:"nudge,omitempty"`           // JSON-serialized nudgenik.Result
```

`NudgeSeq` is only incremented by direct agent signals (non-"working"), not by NudgeNik polls or manual clears. This prevents spurious frontend notifications.

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

| Function                         | Location        | Purpose                                                                                                                                     |
| -------------------------------- | --------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `StripANSIBytes(dst, data)`      | `signal.go:40`  | State-machine ANSI escape stripper. Converts cursor-forward to spaces, cursor-down to newlines, strips all other CSI/OSC/DCS/APC sequences. |
| `parseBracketSignals(data, now)` | `signal.go:173` | Matches `bracketPattern` regex against ANSI-stripped data. Returns `[]Signal` for valid states only.                                        |
| `IsValidState(state)`            | `signal.go:167` | Checks state against `ValidStates` map.                                                                                                     |
| `MapStateToNudge(state)`         | `signal.go:211` | Maps raw states to display strings (e.g., `"needs_input"` → `"Needs Authorization"`).                                                       |

#### Signal Detection (`internal/signal/`)

| Function                    | Location          | Purpose                                                             |
| --------------------------- | ----------------- | ------------------------------------------------------------------- |
| `NewSignalDetector(id, cb)` | `detector.go:28`  | Constructor. Created per-session by tracker or remote monitor.      |
| `Feed(data)`                | `detector.go:47`  | Ingests PTY chunks. Accumulates lines, parses on newline.           |
| `Flush()`                   | `detector.go:77`  | Force-parses buffered data. Used by flush ticker and on disconnect. |
| `ShouldFlush()`             | `detector.go:89`  | True if buffer is non-empty and stale (>= 500ms since last data).   |
| `Suppress(bool)`            | `detector.go:43`  | Enables/disables callback suppression (for scrollback parsing).     |
| `parseLines(data)`          | `detector.go:96`  | Internal: strips ANSI, matches regex, invokes callbacks.            |
| `enforceBufLimit()`         | `detector.go:119` | Trims buffer to 4096 bytes max, discarding oldest bytes.            |

#### Session Tracking (`internal/session/`)

| Function                                 | Location          | Purpose                                                                                       |
| ---------------------------------------- | ----------------- | --------------------------------------------------------------------------------------------- |
| `NewSessionTracker(id, tmux, state, cb)` | `tracker.go:62`   | Creates tracker with signal detector. Sets near-miss callback.                                |
| `attachAndRead()`                        | `tracker.go:190`  | Core I/O loop: scrollback recovery, PTY attach, flush ticker, 8KB read loop feeding detector. |
| `run()`                                  | `tracker.go:162`  | Outer loop: calls `attachAndRead()` with retry on disconnect.                                 |
| `SetSignalCallback(cb)`                  | `manager.go:88`   | Sets the manager-level callback. Must be called before tracker creation.                      |
| `ensureTrackerFromSession(sess)`         | `manager.go:1248` | Creates or returns existing tracker. Wraps callback with session ID binding.                  |
| `Spawn(...)`                             | `manager.go:414`  | Full spawn flow: workspace, provisioning, env vars, tmux, tracker.                            |
| `StartRemoteSignalMonitor(sess)`         | `manager.go:96`   | Creates detector + goroutine for remote sessions via SSH/ET output channel.                   |

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
 tracker.go:62 — NewSessionTracker receives per-session callback:
 │
 │  signalDetector = NewSignalDetector(sessionID, signalCB)
 │
 ▼
 detector.go:28 — Stores as d.callback, invoked from parseLines()
```

This wiring MUST happen before any tracker creation (`daemon.go:376` comment). If `SetSignalCallback` is called after trackers exist, those trackers will have a nil callback and silently drop signals.

---

## Troubleshooting

### Verify Signaling Works

1. Spawn a session in schmux
2. In the terminal, run: `echo "--<[schmux:completed:Test signal]>--"`
3. Check the dashboard - the session should show a completion status

### Check Environment Variables

In a schmux session:

```bash
echo $SCHMUX_ENABLED        # Should be "1"
echo $SCHMUX_SESSION_ID     # Should show session ID
echo $SCHMUX_WORKSPACE_ID   # Should show workspace ID
```

### Check Instruction File Was Created

```bash
ls -la .claude/CLAUDE.md    # For Claude Code sessions
cat .claude/CLAUDE.md       # Should contain SCHMUX:BEGIN marker
```

### Why Isn't My Agent Signaling?

1. **Agent doesn't read instruction files** - Some agents may not read from the expected location
2. **Agent ignores instructions** - The agent may not follow the signaling protocol
3. **Signaling works, display doesn't** - Check browser console for WebSocket errors

### Invalid Signals Are Preserved

Only signals with valid schmux states are processed. Other content that looks similar passes through unchanged:

- **Bracket markers with invalid states**: Markers like `--<[schmux:invalid_state:msg]>--` are ignored

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
3. **Agent-agnostic**: Protocol works for any agent that can output text to stdout
4. **Graceful fallback**: NudgeNik handles agents that don't signal
