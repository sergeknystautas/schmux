# Sessions

**Problem:** Most agent orchestration focuses on agents talking to each other, batch operations, and strict sandboxes. This makes it hard for *you* to see what's happening or step in when needed. For long-running agent work, you need a lightweight, local solution where you can observe, review, and interject at any point—with sessions that persist if you disconnect, preserve history, and can be reviewed after completion.

**Problem:** Even with visibility, there's grunt work—spinning up sessions, creating workspaces, typing the same prompts. These small tasks steal attention from the actual problem you're trying to solve.

---

## Tmux-Based Sessions

Each coding agent runs interactively in its own tmux session.

- Sessions persist after the agent process exits
- Attach via terminal anytime: `tmux attach -t schmux-<session-id>`
- Full terminal access for debugging or manual intervention

---

## Session Lifecycle

```
spawning → running → done → disposed
```

- **Spawning**: Creating the workspace and starting the agent
- **Running**: Agent is actively working
- **Done**: Agent has exited; session preserved for review
- **Disposed**: Session removed from tracking; tmux session deleted

---

## Spawning Sessions

### New Workspace
Creates a fresh git clone with a clean slate:

```bash
schmux spawn -t claude -r myproject -b feature-branch
```

### Existing Workspace
Reuses directory; adds another agent to the mix:

```bash
schmux spawn -t codex -w myproject-001
```

### Options
- `-t, --target`: Which target to run (detected tool, variant, or user-defined)
- `-r, --repo`: Repository name (for new workspace)
- `-b, --branch`: Git branch to checkout
- `-w, --workspace`: Existing workspace ID
- `-n, --nickname`: Optional nickname for easy identification
- `-p, --prompt`: Optional prompt to send

---

## Bulk Operations

Spawn multiple sessions at once:

```bash
schmux spawn -t claude -t codex -t gemini -r myproject -b feature-x
```

Dashboard also supports:
- **Bulk create sessions** across the same or new workspaces
- **On-demand workspace creation** when spawning
- **Nicknames** for easy identification

---

## Spawn Draft Persistence

The dashboard preserves your spawn form state in browser sessionStorage, so you don't lose your prompt if you navigate away or refresh:

- **Per-tab isolation**: Each browser tab maintains its own draft (sessionStorage is per-tab and per-origin)
- **Per-workspace drafts**: Fresh spawns and existing workspace spawns are stored separately
- **Auto-save**: Prompt, mode, targets, and repo selection are saved as you type
- **Auto-clear on success**: Draft is cleared only when at least one session spawns successfully

Storage keys:
- `spawn-draft-fresh` — new workspace spawns
- `spawn-draft-{workspaceId}` — spawning into existing workspace

Drafts survive navigation and page refresh within the same tab, but are cleared when the tab is closed.

---

## Web Spawn Interface

### Prompt-First Single-Page Design

The spawn wizard has been redesigned as a single-page interface that prioritizes your task description:

- **Prompt first**: Large textarea at the top for your task description
- **Parallel target configuration**: Select agents and configure targets in parallel below the prompt
- **AI-powered branch suggestions**: Branch name suggestions based on your prompt (when creating new workspaces)
- **Enter to submit**: Press Enter in the branch or nickname fields to trigger spawn (faster keyboard workflow)

When spawning into an existing workspace, the page shows workspace context (header + tabs) and auto-navigates to the newly created session after successful spawn.

### Inline Spawn Controls

A "+" button in the session tabs bar provides quick access to spawn new sessions:

- **Quick launch presets**: Dropdown with your configured quick launch items for one-click spawning
- **"Custom..." option**: Opens the full spawn wizard for complete control
- **Context-aware**: When in a workspace view, spawning automatically targets that workspace

### Error Display

When spawning fails, error results display the full prompt that was attempted—helpful for understanding what context was sent to the agent and debugging spawn failures.

### Terminal Focus

When entering a session detail view, the terminal automatically receives focus for immediate interaction.

---

## Visibility

Now you've got a dozen concurrent sessions. You don't want to spend your day clicking into each terminal to figure out what's happening. You need to know at a glance: which are still working, which are blocked, which are done, which you've already reviewed, and where to focus your attention next.

### Dashboard Shows

- **Real-time terminal output** via WebSocket
- **Last activity**: When the agent last produced output
- **When you last viewed**: Timestamp of when you last looked at the session
- **NudgeNik status**: Blocked, wants feedback, working, or done

### Status Indicators

- **Running**: Agent is actively working
- **Stopped**: Agent has exited (done)
- **Waiting**: Agent is waiting for input or approval
- **Error**: Session failed to start or crashed

---

## Attach Commands

Each session has a tmux attach command for direct terminal access:

```bash
tmux attach -t schmux-abc123
```

Available from:
- Dashboard: Copy attach command button
- CLI: `schmux attach <session-id>`

---

## Session Persistence

Sessions persist after the agent process exits for review:

- Terminal output is preserved
- Session remains in dashboard
- Mark as done when finished
- Dispose explicitly when no longer needed

---

## Log Rotation

Terminal logs are stored in `~/.schmux/logs/<session-id>.log`. When a log file exceeds the configured size threshold (`xterm.max_log_size_mb`, default 50MB), it's automatically rotated when a new WebSocket connection is established:

- Rotation keeps the last ~1MB of log data (configurable via `xterm.rotated_log_size_mb`)
- Existing WebSocket connections receive a "reconnect" message and must reconnect
- Rotation happens via: stop pipe-pane → truncate to target size → restart pipe-pane

Configure these settings in the web dashboard under **Advanced → Advanced Settings**.

---

## Disposal

Explicitly dispose sessions when you're done with them:

```bash
schmux dispose <session-id>
```

- Removes session from tracking
- Deletes tmux session
- Does NOT delete the workspace (workspaces are managed separately)
- Confirmation required (describes effects)

---

## State

Session state is stored at `~/.schmux/state.json` and managed automatically:

- Session ID, workspace, target, nickname
- Creation time, last activity time
- Status (spawning, running, done, disposed)
- Git status at time of spawning
