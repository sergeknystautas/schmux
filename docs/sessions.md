# Sessions

## What it does

Sessions are tmux-backed agent execution environments. Each coding agent (Claude Code, Codex, Gemini, etc.) runs interactively in its own tmux session inside a workspace directory, with full terminal access for monitoring and intervention.

## Key files

| File                                                | Purpose                                                       |
| --------------------------------------------------- | ------------------------------------------------------------- |
| `internal/session/manager.go`                       | Session lifecycle: spawn, dispose, buildCommand               |
| `internal/session/tracker.go`                       | Drains ControlSource, output fan-out, OutputLog               |
| `internal/session/controlsource.go`                 | ControlSource interface (input boundary for tracker)          |
| `internal/session/localsource.go`                   | Local tmux control mode source                                |
| `internal/session/remotesource.go`                  | Remote SSH-tunneled source                                    |
| `internal/detect/commands.go`                       | Tool modes (promptable, command, resume) and command building |
| `internal/detect/adapter_claude.go`                 | Claude Code adapter (hooks, resume command)                   |
| `internal/detect/adapter_codex.go`                  | Codex adapter                                                 |
| `internal/detect/adapter_gemini.go`                 | Gemini CLI adapter                                            |
| `internal/workspace/ensure/manager.go`              | Pre-spawn workspace setup (hooks, git exclude)                |
| `assets/dashboard/src/routes/SpawnPage.tsx`         | Spawn wizard UI                                               |
| `assets/dashboard/src/routes/SessionDetailPage.tsx` | Session detail with terminal                                  |
| `internal/tmux/tmux.go`                             | TmuxServer struct, socket isolation, admin/spawn CLI wrappers |
| `internal/state/state.go`                           | Session state including TmuxSocket field                      |
| `cmd/schmux/attach.go`                              | CLI attach command (socket-aware, injection-safe)             |

---

**Problem:** Most agent orchestration focuses on agents talking to each other, batch operations, and strict sandboxes. This makes it hard for _you_ to see what's happening or step in when needed. For long-running agent work, you need a lightweight, local solution where you can observe, review, and interject at any point -- with sessions that persist if you disconnect, preserve history, and can be reviewed after completion.

**Problem:** Even with visibility, there is grunt work -- spinning up sessions, creating workspaces, typing the same prompts. These small tasks steal attention from the actual problem you are trying to solve.

---

## Tmux-Based Sessions

Each coding agent runs interactively in its own tmux session.

- Sessions persist after the agent process exits
- Attach via terminal anytime: `tmux attach -t schmux-<session-id>`
- Full terminal access for debugging or manual intervention

---

## Session Lifecycle

```
provisioning → running → stopped ──→ disposing → (removed from state)
```

### Status values

| Status         | Meaning                                                         |
| -------------- | --------------------------------------------------------------- |
| `provisioning` | Creating the workspace and starting the agent (remote sessions) |
| `running`      | Agent is actively working                                       |
| `stopped`      | Agent has exited; session preserved for review                  |
| `failed`       | Session failed to start or crashed                              |
| `queued`       | Waiting for a slot (e.g., remote host provisioning)             |
| `disposing`    | Teardown in progress; sidebar grays out, clicks disabled        |

All constants live in `internal/state/state.go`.

### Workspace status values

| Status         | Meaning                                   |
| -------------- | ----------------------------------------- |
| `provisioning` | Workspace creation in progress            |
| `running`      | Ready for use                             |
| `failed`       | Creation failed                           |
| `disposing`    | Teardown in progress                      |
| `recyclable`   | Marked for reuse to minimize backup churn |

---

## Disposing Status

The `disposing` status provides immediate visual feedback during teardown. Without it, clicking "Dispose" leaves the item looking normal for several seconds while cleanup runs.

### How it works

1. The handler calls `MarkSessionDisposing()` which sets status to `disposing` and saves state.
2. The handler broadcasts via WebSocket. The client sees the item gray out within ~100ms.
3. The handler calls the blocking `Dispose()` teardown.
4. On success: item removed from state; second broadcast reflects removal.
5. On failure: status reverts to previous value, state saved, broadcast ungrays the item.

`MarkSessionDisposing()` is a separate method from `Dispose()` on the session manager. The handler orchestrates the sequence: mark, broadcast, dispose.

### Client behavior

- Sidebar items with `disposing` status get CSS classes that reduce opacity and set `pointer-events: none`.
- Dispose buttons are disabled. Keyboard shortcuts check status before triggering.
- Keyboard navigation (Cmd+Up/Down) skips disposing workspaces.

### Crash recovery

Because `disposing` is persisted via `state.Save()`, daemon restart finds items stuck in this status. On startup, the daemon retries disposal. If retry fails, it reverts to `running` (workspaces) or `stopped` (sessions).

### Idempotency

If an item already has `disposing` status when a dispose request arrives, the handler returns 200 OK (no-op).

---

## ControlSource Interface

`ControlSource` (`internal/session/controlsource.go`) is the input boundary for `SessionRuntime`. It decouples the runtime from transport details so local and remote sessions share the exact same downstream pipeline: OutputLog, sequencing, fan-out, gap detection, WebSocket delivery, and recording.

### Why it exists

Before ControlSource, local and remote sessions had completely separate streaming paths. Local sessions went through SessionRuntime (with OutputLog, sequencing, gap detection, diagnostics) while remote sessions bypassed it entirely. Any feature built on the runtime silently did not work for remote sessions.

### Interface

```go
type ControlSource interface {
    Events() <-chan SourceEvent
    SendKeys(keys string) (controlmode.SendKeysTimings, error)
    CaptureVisible() (string, error)
    CaptureLines(n int) (string, error)
    GetCursorState() (controlmode.CursorState, error)
    Resize(cols, rows int) error
    Close() error
}
```

The source emits `SourceEvent` values with a `Type` discriminator: `SourceOutput`, `SourceGap`, `SourceResize`, or `SourceClosed`.

### Implementations

| Source         | File                               | Wraps                                                |
| -------------- | ---------------------------------- | ---------------------------------------------------- |
| `LocalSource`  | `internal/session/localsource.go`  | tmux control mode (with reconnection, health probes) |
| `RemoteSource` | `internal/session/remotesource.go` | `remote.Connection` (SSH tunnel)                     |

`SessionRuntime` takes a `ControlSource` at construction via `NewSessionRuntime()`. Everything downstream is identical regardless of source type.

---

## Spawning Sessions

### Spawn Modes

The backend supports three spawn modes, toggled via slash commands in the prompt textarea:

| Mode       | Trigger    | Form Fields     | Command Built          |
| ---------- | ---------- | --------------- | ---------------------- |
| Promptable | (default)  | target + prompt | `claude 'the prompt'`  |
| Command    | `/command` | raw command     | user's literal command |
| Resume     | `/resume`  | target only     | `claude --continue`    |

`buildCommand()` in `internal/session/manager.go` has three paths corresponding to these modes. Each tool adapter in `internal/detect/` returns its command parts for all three modes via `BuildCommandParts()`.

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

- `-t, --target`: Which target to run (detected tool, model, or user-defined)
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
- **Slash commands**: Type `/command`, `/resume`, or `/quick` in the textarea to switch modes via autocomplete
  - `/command`: Run a raw shell command instead of a promptable target
  - `/resume`: Resume the agent's last conversation in an existing workspace (requires workspace selection)
  - `/quick`: Run a quick launch preset (workspace mode only; shows dropdown of available quick commands)
- **Parallel target configuration**: Select agents and configure targets in parallel below the prompt
- **AI-powered branch suggestions**: Branch name is auto-generated from your prompt (when creating new workspaces)
- **One-click engage**: The "Engage" button handles branch naming and spawning in sequence

When spawning into an existing workspace, the page shows workspace context (header + tabs) and auto-navigates to the newly created session after successful spawn.

### Spawn Modes

The spawn page has three modes, determined once on page load:

| Mode        | Source                        | Description                                                                    |
| ----------- | ----------------------------- | ------------------------------------------------------------------------------ |
| `workspace` | URL `?workspace_id=xxx`       | Spawn into existing workspace                                                  |
| `prefilled` | React Router `location.state` | Pre-selected repo/branch with prepared prompt (from home page recent branches) |
| `fresh`     | no params, no state           | New spawn from scratch                                                         |

### Data Sources

The spawn page uses a three-layer persistence model:

**Layer 1: Mode Logic (Entry Point)**

- Highest priority, determined by navigation method
- URL parameters: `workspace_id` for existing workspace spawns
- React Router location state: `repo`, `branch`, `prompt` for prefilled mode
  - Passed via `navigate('/spawn', { state })` from home page
  - Produced by `POST /api/prepare-branch-spawn` (see below)

**Layer 2: Session Storage Draft (Active Draft)**

- Per-tab, survives page refresh within the same tab
- What you're actively typing right now
- Key: `spawn-draft-{workspace_id}` or `spawn-draft-fresh`
- Auto-saved as user types
- **Cleared on successful spawn**
- Fields saved: `prompt`, `spawnMode`, `selectedCommand`, `targetCounts`, `modelSelectionMode`
- Additional fields saved only when key is `fresh`: `repo`, `newRepoName`
- `modelSelectionMode` values: `'single'` (one agent), `'multiple'` (toggle multiple), `'advanced'` (0-10 per agent)

**Layer 3: Local Storage (Long-term Memory)**

- Cross-tab, survives browser close/reopen
- Last successful configuration
- **Never auto-cleared**
- Keys (with `schmux:` prefix):
  - `schmux:spawn-last-repo` — Last repository used
  - `schmux:spawn-last-target-counts` — Last target counts used (e.g. `{'claude-sonnet': 1}`)
  - `schmux:spawn-last-model-selection-mode` — Last model selection mode used (`'single'`, `'multiple'`, or `'advanced'`)
- **Updated on successful spawn** with the values that were actually used
- **Cross-tab sync**: Changes propagate to other tabs via browser `storage` event, taking effect on next page load/navigation

### Form Fields

| Field              | Description                                                                  |
| ------------------ | ---------------------------------------------------------------------------- |
| repo               | Repository URL, or `'__new__'` for new local repo                            |
| branch             | Git branch name                                                              |
| newRepoName        | Name for new local repo (only when repo is `'__new__'`)                      |
| prompt             | Task description for AI agents                                               |
| spawnMode          | `'promptable'`, `'command'`, or `'resume'`                                   |
| selectedCommand    | Which command to run (only when spawnMode is `'command'`)                    |
| targetCounts       | Map of target name to count (e.g. `{'claude-code': 2}`)                      |
| modelSelectionMode | `'single'`, `'multiple'`, or `'advanced'` - controls how agents are selected |
| nickname           | Friendly name for the session (user-provided)                                |

### Model Selection Modes

When `spawnMode` is `'promptable'`, the agent selection UI offers three modes:

| Mode       | Description                       | Agent Behavior                                                      |
| ---------- | --------------------------------- | ------------------------------------------------------------------- |
| `single`   | One agent only                    | Clicking an agent deselects others (radio button behavior)          |
| `multiple` | Multiple agents, one session each | Each agent toggles on/off independently (0 or 1 sessions per agent) |
| `advanced` | Full control                      | Each agent can have 0-10 sessions via +/- counter buttons           |

The mode selector appears as a left column with buttons for each mode. The agent grid appears on the right, arranged in a responsive grid layout (wider columns in advanced mode for the counter controls).

**Default mode:** `'single'`

**Single mode constraint:** When switching to `single` mode, if multiple agents were previously selected, only the first selected agent remains selected; all others are deselected.

### Field Initialization by Mode

Field resolution follows priority order: **Mode Logic → Session Storage → Local Storage → Default**

**Mode: `workspace`**

| Field              | 1. Mode Logic               | 2. sessionStorage Draft | 3. localStorage                   | 4. Default     |
| ------------------ | --------------------------- | ----------------------- | --------------------------------- | -------------- |
| repo               | `workspace.repo` (locked)   | -                       | -                                 | -              |
| branch             | `workspace.branch` (locked) | -                       | -                                 | -              |
| prompt             | -                           | `prompt`                | -                                 | `""`           |
| spawnMode          | -                           | `spawnMode`             | -                                 | `'promptable'` |
| modelSelectionMode | -                           | `modelSelectionMode`    | `spawn-last-model-selection-mode` | `'single'`     |
| selectedCommand    | -                           | `selectedCommand`       | -                                 | `""`           |
| targetCounts       | -                           | `targetCounts`          | `spawn-last-target-counts`        | `{}`           |
| nickname           | -                           | -                       | -                                 | `""`           |

**Mode: `prefilled`**

| Field              | 1. Mode Logic                    | 2. sessionStorage Draft | 3. localStorage                   | 4. Default     |
| ------------------ | -------------------------------- | ----------------------- | --------------------------------- | -------------- |
| repo               | `location.state.repo` (locked)   | -                       | -                                 | -              |
| branch             | `location.state.branch` (locked) | -                       | -                                 | -              |
| prompt             | `location.state.prompt`          | -                       | -                                 | -              |
| spawnMode          | -                                | `spawnMode`             | -                                 | `'promptable'` |
| modelSelectionMode | -                                | `modelSelectionMode`    | `spawn-last-model-selection-mode` | `'single'`     |
| selectedCommand    | -                                | `selectedCommand`       | -                                 | `""`           |
| targetCounts       | -                                | `targetCounts`          | `spawn-last-target-counts`        | `{}`           |
| nickname           | -                                | -                       | -                                 | `""`           |

**Mode: `fresh`**

| Field              | 1. sessionStorage Draft | 2. localStorage                   | 3. Default     |
| ------------------ | ----------------------- | --------------------------------- | -------------- |
| repo               | `repo`                  | `spawn-last-repo`                 | `""`           |
| branch             | -                       | -                                 | `""`           |
| newRepoName        | `newRepoName`           | -                                 | `""`           |
| prompt             | `prompt`                | -                                 | `""`           |
| spawnMode          | `spawnMode`             | -                                 | `'promptable'` |
| modelSelectionMode | `modelSelectionMode`    | `spawn-last-model-selection-mode` | `'single'`     |
| selectedCommand    | `selectedCommand`       | -                                 | `""`           |
| targetCounts       | `targetCounts`          | `spawn-last-target-counts`        | `{}`           |

### Resume Mode

When `spawnMode` is `'resume'`, the form simplifies to target + repo selection. Resume resumes the agent's most recent conversation in the workspace directory using agent-native resume commands.

**Per-agent resume commands:**

| Agent       | Resume Command        | Notes                                              |
| ----------- | --------------------- | -------------------------------------------------- |
| Claude Code | `claude --continue`   | Resumes last conversation in the working directory |
| Codex       | `codex resume --last` | Resumes last conversation in the working directory |
| Gemini CLI  | `gemini -r latest`    | Resumes last conversation in the working directory |

The backend builds the resume command via `ToolModeResume` in `internal/detect/commands.go`. Each tool adapter returns its resume command parts in `BuildCommandParts()`.

**In `workspace` or `prefilled` mode:**

- Only the Target dropdown is shown (workspace is already determined by URL/state)
- Spawns into the existing workspace with `resume: true`

**In `fresh` mode:**

- Target dropdown + Repo dropdown are shown
- Creates a new workspace using the repo's default branch
- Spawns with `resume: true` (agent runs its resume command, e.g., `claude --continue`)

**Validation requirements:**

- A target must be selected (`targetCounts` has at least one non-zero entry)
- In fresh mode: a repo must be selected

**On successful resume spawn:**

- `spawn-last-repo` is updated in localStorage
- Draft is cleared as usual

### Prepare Branch Spawn

When the user clicks a recent branch on the home page:

1. Home page calls `POST /api/prepare-branch-spawn` with `{ repo, branch }`
2. Server does all work in one round-trip:
   - Runs `git log --oneline main..{branch}` on the bare clone to get commit messages
   - Builds a standardized branch review prompt
3. Returns `{ repo, branch, prompt }`
4. Home page navigates to `/spawn` via `navigate('/spawn', { state: result })`
5. Spawn page detects `location.state` → enters prefilled mode

**Branch review prompt** instructs the agent to:

1. Read markdown/spec files in repo root and docs/ for project context and goals
2. Review commit history on the branch
3. Understand the scope of changes
4. Identify what's completed, in progress, and remaining
5. Summarize findings, then ask what to work on next

The user can edit the prompt before engaging. Branch is pre-filled from the selection.

### On Successful Spawn

When at least one session spawns successfully:

**Cleared:**

- sessionStorage draft (all fields including `prompt`, `spawnMode`, `selectedCommand`, `targetCounts`, `modelSelectionMode`, `repo`, `newRepoName`)

**Updated (write-back to localStorage):**

- `spawn-last-repo` ← actual repo used (normalized; `local:name` if new repo) — for promptable, command, and resume modes
- `spawn-last-target-counts` ← actual target counts used (only non-zero entries) — only for promptable mode
- `spawn-last-model-selection-mode` ← actual model selection mode used — only for promptable mode

**Never Cleared:**

- localStorage values persist indefinitely

### Branch Suggestion

Called during the "Engage" flow (inside `handleEngage`) when ALL of these are true:

- Mode is `fresh`
- `spawnMode` is `'promptable'`
- `prompt` is not empty
- `branchSuggestTarget` is configured

The Engage button shows "Naming branch..." during this phase. On success, `branch` is set from the API response and passed directly to spawn.

**Failure handling:** If branch suggestion fails, the UI prompts you to enter a branch name manually instead of silently defaulting to the repository's default branch. This ensures you're always in control of the branch naming.

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

## Terminal Activity

Session activity (`last_output_at`) is tracked in-memory while the daemon is running.

- Values reset on daemon restart
- Activity updates only when new meaningful terminal output arrives

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
- Status (`provisioning`, `running`, `stopped`, `failed`, `queued`, `disposing`)
- Git status at time of spawning

---

## Architecture Decisions

- **Socket isolation**: schmux uses a dedicated tmux socket (`-L schmux`) so it does not share namespace with the user's own tmux sessions. A user killing their tmux server no longer kills schmux sessions, and `tmux ls` no longer shows schmux sessions.
- **TmuxServer struct replaces package-level globals**: The `internal/tmux` package uses a `TmuxServer` struct instead of package-level functions with global state. TmuxServer is stateless and cheap to construct (56 bytes, no connections, no lifecycle) — a pool/cache was explicitly rejected.
- **Per-session socket affinity**: Each session records its `TmuxSocket` at spawn time. Changing the config socket only affects new sessions; existing sessions stay on their birth socket and drain naturally.
- **Hard-cut migration on upgrade**: Sessions on the old tmux server are orphaned on upgrade. Sessions are ephemeral and cheap to re-create, so migration logic was not warranted.
- **Config socket change requires daemon restart**: Atomic hot-swap was rejected because Spawn does 3 sequential tmux calls that would target different sockets if the server swapped mid-operation.
- **SendKeys vs SendTmuxKeyName**: `SendKeys(rawBytes)` classifies by byte value (for WebSocket terminal I/O). `SendTmuxKeyName(name)` sends tmux key names like `"C-u"` without `-l` (for programmatic callers). Passing `"C-u"` to `SendKeys` would type literal characters `C`, `-`, `u`.
- **Conversation state is not persisted by schmux.** The `Session` struct stores ID, workspace, target, tmux session name, etc., but nothing about the agent's conversation state. Each agent stores its own conversation data (e.g., Claude Code in its data directory). Resume (`/resume`) simply invokes the agent's native resume command.
- **Agent-specific signaling is a session-level concern.** `SignalingInstructions` and `AgentInstructions` are written per-session in `session/manager.go`, not as workspace-level setup. They configure prompt injection for the specific agent being spawned.
- **No specific conversation resume.** `/resume` resumes the most recent conversation in the workspace directory, not a specific past conversation. No conversation IDs are tracked.
- **ControlSource unifies local and remote streaming.** SessionRuntime consumes a pluggable `ControlSource` interface rather than hardcoding transport logic. Any feature built on the runtime (OutputLog, sequencing, gap detection, recording, diagnostics) works identically for local and remote sessions.
- **Disposing status is set in the manager, not the handler.** This ensures all disposal callers (HTTP, CLI, automation) get consistent status transitions. Handlers only handle broadcasting.

---

## Common Modification Patterns

- **To add a new spawn mode**: add a `ToolMode` constant in `internal/detect/commands.go`, handle it in each tool adapter's `BuildCommandParts()`, and add the UI mode in `SpawnPage.tsx`.
- **To add a new agent**: create an adapter file `internal/detect/adapter_<name>.go` implementing the `ToolAdapter` interface, register it via `init()`.
- **To change session lifecycle states**: update the status constants in `internal/state/state.go`, handle the new status in the session manager's `Dispose()` method, and update the sidebar CSS/logic.
- **To add a new ControlSource**: implement the `ControlSource` interface in `internal/session/`, pass the new source to `NewSessionRuntime()`. No changes to the runtime, OutputLog, fan-out, or WebSocket handlers are needed.

## Gotchas

- **Attach command must not use shell interpolation**: The attach command interpolates user-provided nicknames and socket names. Use structured `exec.Command(binary, "-L", socket, "attach", ...)` — never `exec.Command("sh", "-c", attachCmd)` which enables command injection.
- **CaptureLines always includes ANSI escapes**: Control mode `CapturePaneLines` uses the `-e` flag. Callers needing plain text must post-process with `tmux.StripAnsi()`.
- **ListSessions on a stopped tmux server returns error, not empty list**: Fan-out across active sockets must treat exit code 1 ("no server running") as zero sessions, not propagate the error.
- **Empty TmuxSocket maps to `"default"`**: Backward compatibility for pre-isolation sessions that had no socket field in state.
- **Multi-daemon is unsupported**: Socket name is shared. If two daemons run simultaneously, they see each other's sessions. A startup guard logs a warning if unmanaged sessions are found.
- **Resume without prior conversation**: when Claude Code's `--continue` finds no prior conversation in the directory, it starts fresh. There is no warning to the user.
- **Agent instructions are git-excluded**: the ensure system writes `.schmux/hooks/` and `.schmux/events/` paths to `.git/info/exclude` so they do not pollute git status.
- **Disposing is persisted**: because `disposing` is saved to `state.json`, a daemon crash during teardown leaves items stuck. The daemon retries on startup, but if retry fails the item reverts to `stopped`/`running` and logs a warning.
- **Pre-existing workspaces have no status**: workspaces created before the status field was added have an empty `Status`. The client treats empty the same as `running`.
- **SourceEvent.Data is string, not []byte**: matches `controlmode.OutputEvent.Data`. Conversion to `[]byte` happens at the `OutputLog.Append()` boundary.
- **BroadcastSessions has 100ms debounce**: the disposing transition relies on this. The delay is imperceptible after a confirmation dialog.
