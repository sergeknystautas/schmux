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

## Web Spawn Interface

### Prompt-First Single-Page Design

The spawn wizard is a single-page interface that prioritizes your task description:

- **Prompt first**: Large textarea at the top for your task description
- **Parallel target configuration**: Select agents and configure targets in parallel below the prompt
- **AI-powered branch suggestions**: Branch name suggestions based on your prompt (when creating new workspaces)
- **Enter to submit**: Press Enter in the branch or nickname fields to trigger spawn (faster keyboard workflow)

When spawning into an existing workspace, the page shows workspace context (header + tabs) and auto-navigates to the newly created session after successful spawn.

### Spawn Modes

The spawn page has three modes, determined once on page load:

| Mode | Source | Description |
|------|--------|-------------|
| `workspace` | URL `?workspace_id=xxx` | Spawn into existing workspace |
| `prefilled` | React Router `location.state` | Pre-selected repo/branch with prepared prompt and nickname (from home page recent branches) |
| `fresh` | no params, no state | New spawn from scratch |

### Data Sources

**URL Parameters:**
- `workspace_id` — ID of existing workspace to spawn into

**React Router Location State (prefilled mode):**
- Passed via `navigate('/spawn', { state })` from the home page
- Contains: `repo`, `branch`, `prompt`, `nickname`
- Produced by `POST /api/prepare-branch-spawn` (see below)

**Session Storage Draft (keyed):**
- Persisted form state, survives page refresh within browser session
- Key: `spawn-draft-{workspace_id}` or `spawn-draft-fresh`
- Per-tab isolation (sessionStorage is per-tab and per-origin)
- Auto-saved as user types; auto-cleared when at least one session spawns successfully
- Fields saved: `prompt`, `spawnMode`, `selectedCommand`
- Additional fields saved only when key is `fresh`: `repo`, `newRepoName`

**Session Storage Target Counts (shared):**
- Stored under key `spawn-target-counts`, shared across all spawn modes
- Remembers how many of each target the user last configured (e.g. `{'claude-code': 2}`)

### Form Fields

| Field | Description |
|-------|-------------|
| repo | Repository URL, or `'__new__'` for new local repo |
| branch | Git branch name |
| newRepoName | Name for new local repo (only when repo is `'__new__'`) |
| prompt | Task description for AI agents |
| spawnMode | `'promptable'` or `'command'` |
| selectedCommand | Which command to run (only when spawnMode is `'command'`) |
| targetCounts | Map of target name to count (e.g. `{'claude-code': 2}`) |
| nickname | Friendly name for the session |

### Field Initialization by Mode

**Mode: `workspace`**

| Field | Source |
|-------|--------|
| repo | workspace.repo (locked) |
| branch | workspace.branch (locked) |
| prompt | session storage draft |
| spawnMode | session storage draft, default `'promptable'` |
| selectedCommand | session storage draft |
| targetCounts | shared session storage `spawn-target-counts` |
| nickname | empty string |

**Mode: `prefilled`**

| Field | Source |
|-------|--------|
| repo | location state `repo` (locked) |
| branch | location state `branch` (locked) |
| prompt | location state `prompt` (editable; standardized branch review prompt) |
| spawnMode | session storage draft, default `'promptable'` |
| selectedCommand | session storage draft |
| targetCounts | shared session storage `spawn-target-counts` |
| nickname | location state `nickname` (editable; generated from commit messages) |

**Mode: `fresh`**

| Field | Source |
|-------|--------|
| repo | session storage draft `repo` |
| branch | empty string |
| newRepoName | session storage draft |
| prompt | session storage draft |
| spawnMode | session storage draft, default `'promptable'` |
| selectedCommand | session storage draft |
| targetCounts | shared session storage `spawn-target-counts` |
| nickname | empty string |

### Prepare Branch Spawn

When the user clicks a recent branch on the home page:

1. Home page calls `POST /api/prepare-branch-spawn` with `{ repo, branch }`
2. Server does all work in one round-trip:
   - Runs `git log --oneline main..{branch}` on the bare clone to get commit messages
   - Passes commit messages to the branch suggestion target for a nickname
   - Builds a standardized branch review prompt
3. Returns `{ repo, branch, prompt, nickname }`
4. Home page navigates to `/spawn` via `navigate('/spawn', { state: result })`
5. Spawn page detects `location.state` → enters prefilled mode

**Branch review prompt** instructs the agent to:
1. Read markdown/spec files in repo root and docs/ for project context and goals
2. Review commit history on the branch
3. Understand the scope of changes
4. Identify what's completed, in progress, and remaining
5. Summarize findings, then ask what to work on next

The user can edit both the prompt and nickname before spawning.

### Branch Suggestion

Called in `handleNext` when ALL of these are true:
- Mode is `fresh`
- `spawnMode` is `'promptable'`
- `prompt` is not empty
- `branchSuggestTarget` is configured

On success, sets both `branch` and `nickname` from the API response. Otherwise, branch defaults to `'main'`.

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
