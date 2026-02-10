# Agent Signaling

Schmux provides a comprehensive system for agents to communicate their status to users in real-time.

## Overview

The agent signaling system has three components:

1. **Direct Signaling** - Agents output OSC escape sequences to signal their state
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
│  Schmux WebSocket detects signal → updates dashboard            │
│  Signal stripped from terminal output (invisible to user)       │
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

Schmux supports two signaling mechanisms:

1. **Bracket-based markers** (recommended) - Text markers that agents output in their responses
2. **OSC 777 escape sequences** - Terminal escape codes for agents with direct stdout access

### Bracket-Based Markers (Recommended)

For agents that generate text responses (like Claude Code, Codex, etc.), output the bracket marker **on its own line** in your response:

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
- **Invisible to user** - Markers are stripped before showing to user in the dashboard
- **Looks benign** - If not stripped, the marker looks like an innocuous code annotation
- **Highly unique** - The format is extremely unlikely to appear naturally in agent output

### OSC 777 Format

For agents with direct terminal control, use the standard OSC 777 notification format:

```
ESC ] 777 ; notify ; <state> ; <message> BEL
\x1b]777;notify;<state>;<message>\x07
```

The `notify` keyword is standard OSC 777. The "title" field contains the state, and "body" contains the optional message.

**Examples:**

```bash
# Signal completion
printf '\x1b]777;notify;completed;Implementation complete, ready for review\x07'

# Signal needs input
printf '\x1b]777;notify;needs_input;Waiting for permission to delete files\x07'

# Signal error
printf '\x1b]777;notify;error;Build failed with 3 errors\x07'

# Signal needs testing
printf '\x1b]777;notify;needs_testing;Please test the new feature\x07'

# Clear signal (starting new work)
printf '\x1b]777;notify;working;\x07'
```

**Benefits:**

- **Standard format** - Already supported by terminals (VSCode, rxvt-unicode)
- **May trigger native notifications** - Terminals that support OSC 777 could show desktop notifications
- **Interoperable** - Other tools could produce compatible signals

### Valid States

| State           | Meaning                                   | Dashboard Display     |
| --------------- | ----------------------------------------- | --------------------- |
| `completed`     | Task finished successfully                | ✓ Completed           |
| `needs_input`   | Waiting for user authorization/input      | ⚠ Needs Authorization |
| `needs_testing` | Ready for user testing                    | 🧪 Needs User Testing |
| `error`         | Error occurred, needs intervention        | ❌ Error              |
| `working`       | Actively working (clears previous signal) | (clears status)       |

### How Signals Flow

1. Agent outputs signal (bracket marker or OSC 777 sequence)
2. tmux captures output via pipe-pane to log file
3. Schmux WebSocket reads log file, detects signal
4. Signal is parsed and validated (must be a valid schmux state)
5. Session nudge state is updated
6. Signal is stripped from output before sending to browser terminal
7. Dashboard broadcasts update to all connected clients

### Benefits of Dual-Format Support

- **Bracket-based**: Works for any agent that generates text responses, passes through markdown
- **OSC 777**: Works for agents with direct terminal access
- **Invisible**: Both formats are stripped from terminal output before display
- **Standard**: OSC 777 is recognized by many terminals
- **Flexible**: Choose the format that works best for your agent

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
    # Bracket-based (recommended for text-based agents):
    echo "--<[schmux:completed:Task done]>--"

    # Or OSC 777 (for terminal control):
    printf '\x1b]777;notify;completed;Task done\x07'
fi
```

### Integration Examples

**Bash (for AI agents like Claude Code):**

Output the signal marker on its own line in your response:

```
--<[schmux:completed:Feature implemented successfully]>--
```

Note: The signal must be on a separate line - do not embed it within other text.

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
  signal/           # OSC 777 parsing
    signal.go       # ParseSignals, ExtractAndStripSignals
    signal_test.go

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

**Signal Detection** (`internal/signal/signal.go`):

```go
// Parse signals from terminal output (both bracket-based and OSC 777)
signals := signal.ParseSignals(data)

// Extract signals and return cleaned data
signals, cleanData := signal.ExtractAndStripSignals(data)
```

The parser supports:

- Bracket-based markers: `--<[schmux:state:message]>--`
- OSC 777 escape sequences: `\x1b]777;notify;state;message\x07`

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
2. In the terminal, run either:
   - Bracket-based: `echo "--<[schmux:completed:Test signal]>--"`
   - OSC 777: `printf '\x1b]777;notify;completed;Test signal\x07'`
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

Only signals with valid schmux states are processed and stripped. Other content that looks similar passes through unchanged:

- **OSC 777 with invalid states**: Other OSC 777 notifications (from other tools) pass through
- **Bracket markers with invalid states**: Markers like `--<[schmux:invalid_state:msg]>--` are preserved

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
3. **Agent-agnostic**: Protocol works for any agent that can output to stdout
4. **Graceful fallback**: NudgeNik handles agents that don't signal
5. **Invisible**: Signals are stripped from terminal output
6. **Standard format**: Uses OSC 777, a recognized terminal escape sequence
