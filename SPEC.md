# schmux - Smart Cognitive Hub on tmux

## Specification v0.5

### Overview

A Golang application that orchestrates multiple AI coding agents (Codex, Claude Code with various LLM backends) running in tmux sessions. Provides a web dashboard for spawning, monitoring, and managing agent sessions across git repositories.

### Core Components

1. **Daemon** - Long-running background process (`schmux start` / `schmux stop`)
2. **Web Dashboard** - localhost, no auth, primary UI for managing sessions
3. **Workspace Manager** - Manages cloned repo directories
4. **tmux Integration** - Each agent runs in its own tmux session

---

### Configuration

JSON file, hand-edited for v0.5. Location: `~/.schmux/config.json`

```json
{
  "workspace_path": "~/dev/schmux-workspaces",
  "repos": [
    {
      "name": "myproject",
      "url": "git@github.com:user/myproject.git"
    }
  ],
  "agents": [
    {
      "name": "codex",
      "command": "codex"
    },
    {
      "name": "claude",
      "command": "claude"
    },
    {
      "name": "claude-glm",
      "command": "/path/to/glm-4.7"
    },
    {
      "name": "claude-minimax",
      "command": "/path/to/minimax"
    },
    {
      "name": "claude-kimi",
      "command": "/path/to/kimi-thinking"
    }
  ]
}
```

---

### Workspace Management

- **Single global workspace directory** configured in `workspace_path`
- **Sequential directory naming**: `<repo>-001`, `<repo>-002`, etc.
- **Directory status tracking**: available (clean, no active session) vs in-use
- **Multiple sessions per directory**: A directory can have multiple agents running simultaneously
- **Git operations**:
  - Clone repo if not already present
  - Checkout to specified local branch
  - `git pull --rebase` before starting session (for new directory spawn)
  - If pull/rebase fails → workspace marked unusable (conflicts need manual resolution)
  - Cleanup: `git checkout -- .` to reset state when disposing

---

### Session Lifecycle

**New Directory Spawn (Worker)**
1. User spawns session(s) via web dashboard spawn view
2. schmux creates a new workspace directory for the repo
3. Clones repo, checks out branch, pulls latest
4. Creates tmux session, runs agent command with user's prompt
5. Session tracked in state (process running/stopped)

**Existing Directory Spawn (Reviewer/Subagent)**
1. User spawns session from directory view in dashboard
2. schmux uses existing workspace directory (no git operations)
3. Creates tmux session, runs agent command with user's prompt
4. Session tracked in state, associated with same workspace

**Common**
- User can attach via terminal (`tmux attach -t <session>`)
- tmux session persists after agent process exits (enables resume)
- User disposes session via dashboard when done

---

### Web Dashboard Features

**Dashboard Hierarchy**: Project → Directory → Sessions

**Project/Directory View**
- Organized by project, then by directory
- Each directory shows all sessions (N agents per directory)
- Displays: directory name, branch, session count
- Expand to see individual sessions

**Session List**
- Displays: agent type, process status (running/stopped), created time
- Copy-able attach command for each session
- Dispose button per session
- **Spawn in this directory** button to add more agents

**Spawn View (New Directory)**
- Select git repo (dropdown from pre-registered list)
- Enter branch name
- Enter prompt (textarea)
- Agent quantity selector ("shopping cart" style - pick count per agent type)
- Submit spawns all requested sessions with same prompt in new directories

**Session Detail View**
- Real-time terminal output (scrolling text)
- Session metadata (repo, branch, agent, created time)
- Dispose button

**Connection Monitoring**
- Header pill shows server connection status (Connected/Disconnected)
- Polls `/api/healthz` every 5 seconds

---

### State

JSON file at `~/.schmux/state.json`

```json
{
  "workspaces": [
    {
      "id": "myproject-001",
      "repo": "myproject",
      "branch": "main",
      "path": "/Users/x/dev/schmux-workspaces/myproject-001",
      "usable": true
    }
  ],
  "sessions": [
    {
      "id": "schmux-session-abc123",
      "workspace_id": "myproject-001",
      "agent": "claude-glm",
      "prompt": "fix the auth bug",
      "tmux_session": "schmux-session-abc123",
      "created_at": "2025-01-05T10:30:00Z",
      "pid": 12345
    },
    {
      "id": "schmux-session-def456",
      "workspace_id": "myproject-001",
      "agent": "claude-kimi",
      "prompt": "review the changes",
      "tmux_session": "schmux-session-def456",
      "created_at": "2025-01-05T11:00:00Z",
      "pid": 12346
    }
  ]
}
```

Note: Multiple sessions can reference the same `workspace_id`.

---

### CLI Commands (v0.5)

```
schmux start          # start daemon in background
schmux stop           # stop daemon
schmux status         # show daemon status, web dashboard URL
```

---

### Technical Notes

- **Language**: Go
- **Web server**: Embedded in daemon, serves dashboard
- **Terminal streaming**: Capture tmux pane output, stream to browser via websocket
- **Process tracking**: Get PID from tmux pane (`#{pane_pid}`) at session creation, then check if that PID is running. More reliable than `pgrep` (no false matches, no race conditions).
- **Dependency check**: Verify tmux is installed on startup, error if not found
- **License**: Apache 2.0

---

## Future Scope

### v0.6 - Now we're cooking with gas

- **Cross-agent copy** - Select text from one session's terminal, copy with context to another session in same directory
- **Richer session status** - Beyond just process running/stopped
- **Batch grouping** - Dashboard groups sessions started together with same prompt

### v0.9 - CLI catch up

- **CLI commands for spawning** - `schmux run --repo X --branch Y --agents "claude:3" --prompt "..."`
- **CLI commands for resume** - `schmux resume <session-id>`

### v1.0 - Usable by others

- **Getting started documentation** - Installation guide, tutorials, examples
- **Config management UI** - Web and/or CLI interface instead of hand-editing JSON
- **Completion notification** - Via agent hooks (`--hook` on prompt complete) to distinguish "task complete" vs "waiting for input" vs "running"
- **Pluggable agent configuration** - Easier way to define new LLM endpoints without wrapper scripts

### v1.1 - Power tools
- **Full terminal emulator** - xterm.js in browser with colors, cursor, full interactivity
- **Show repo diffs in browser** - View what changes agents have made to the codebase
- **Saved prompts** - Easily spin up agents to run standard workflow

### v2.0+ - Not in scope (future ideas)

- **Budget tracking** - Track API costs per agent/session
- **Feedback system** - Rate agent outputs, track which agents/backends perform better on different tasks
- **SQLite for state** - More robust storage if JSON becomes limiting
- **Remote branch operations** - Create branches, push, PR creation
- **Search** - Find prompts and responses across current or historical sessions
- **Keyboard-first web** - Command palette and advanced keyboard navigation
