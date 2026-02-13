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

1. Agent outputs bracket marker on its own line
2. Schmux PTY attachment reads terminal output
3. ANSI escape sequences are stripped, signal regex is matched
4. Signal is parsed and validated (must be a valid schmux state)
5. Session nudge state is updated
6. Dashboard broadcasts update to all connected clients

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

---

## Automatic Provisioning

### How Agents Learn About Signaling

When you spawn a session, schmux automatically creates an instruction file in the workspace that teaches the agent about the signaling protocol.

| Agent       | Instruction File    |
| ----------- | ------------------- |
| Claude Code | `.claude/CLAUDE.md` |
| Codex       | `.codex/AGENTS.md`  |
| Gemini      | `.gemini/GEMINI.md` |

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

Models are mapped to their base tools:

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

### Source Distinction

The API indicates the signal source:

```json
{
  "state": "Completed",
  "summary": "Implementation finished",
  "source": "agent"
}
```

- Direct signals: `source: "agent"`
- NudgeNik classification: `source: "llm"`

---

## Implementation Details

### Package Structure

```
internal/
  signal/           # Signal parsing (bracket-based markers)
    signal.go       # ANSI stripping, bracket regex, parseBracketSignals
    detector.go     # Line accumulation, flush ticker, near-miss detection
    signal_test.go
    detector_test.go

  provision/        # Agent instruction provisioning
    provision.go    # EnsureAgentInstructions, RemoveAgentInstructions
    provision_test.go

  detect/
    tools.go        # AgentInstructionConfig, GetInstructionPathForTarget

  dashboard/
    websocket.go    # handleAgentSignal (processes signals)

  session/
    manager.go      # Calls provision.EnsureAgentInstructions on spawn

  daemon/
    daemon.go       # Skips NudgeNik for recent signals

  state/
    state.go        # LastSignalAt field on Session
```

### Key Functions

**Signal Detection** (`internal/signal/`):

```go
// Low-level: parse signals from ANSI-stripped data
signals := signal.parseBracketSignals(cleanData, now)

// Line accumulator with chunked-read support
detector := signal.NewSignalDetector(sessionID, callback)
detector.Feed(chunk)   // accumulates lines, parses on newline
detector.Flush()       // force-parse buffered data
detector.ShouldFlush() // true if buffered data has aged past FlushTimeout
```

The parser supports bracket-based markers: `--<[schmux:state:message]>--`

**Provisioning** (`internal/provision/provision.go`):

```go
// Ensure instruction file exists with signaling docs
provision.EnsureAgentInstructions(workspacePath, targetName)

// Check if already provisioned
provision.HasSignalingInstructions(workspacePath, targetName)

// Remove schmux block (cleanup)
provision.RemoveAgentInstructions(workspacePath, targetName)
```

**Instruction Config** (`internal/detect/tools.go`):

```go
// Get instruction path for any target (tool or model)
path := detect.GetInstructionPathForTarget("claude-opus")
// Returns: ".claude/CLAUDE.md"
```

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
