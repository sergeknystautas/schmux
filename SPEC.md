# schmux - Smart Cognitive Hub on tmux

## Specification v0.8

### Overview

A Golang application that orchestrates multiple AI coding agents (Claude Code, Codex, and other LLM-backed agents) running in tmux sessions. Provides a web dashboard for spawning, monitoring, and managing agent sessions across git repositories.

**Core concepts:**
- Run multiple AI agents simultaneously on the same codebase
- Each agent gets its own isolated workspace directory
- Monitor all agents from a web dashboard with live terminal output
- Compare results across agents by viewing git diffs

---

### Configuration

Configuration lives at `~/.schmux/config.json`. You can edit it directly or use the web dashboard's settings page.

```json
{
  "workspace_path": "~/schmux-workspaces",
  "repos": [
    {"name": "myproject", "url": "git@github.com:user/myproject.git"}
  ],
  "agents": [
    {"name": "claude", "command": "claude"},
    {"name": "claude-glm", "command": "/path/to/glm-4.7"}
  ],
  "terminal": {
    "width": 120,
    "height": 40,
    "seed_lines": 100
  }
}
```

**Required settings:**
- `workspace_path` - Where workspace directories are created
- `repos` - Git repositories you want to work with
- `agents` - AI agents to spawn (name + terminal command)
- `terminal` - Terminal dimensions for sessions

**Advanced settings** (optional `internal` section):
- Polling intervals for status updates
- Session tracking timing

---

### State

Application state is stored at `~/.schmux/state.json` and managed automatically. Tracks your workspaces and sessions.

---

### Workspaces

Workspaces are directories where agents do their work.

- Each repo gets sequential directories: `myproject-001`, `myproject-002`, etc.
- Multiple agents can work in the same workspace simultaneously
- Each workspace tracks git status (dirty, ahead, behind)
- Workspaces are created on-demand when you spawn sessions

**Git behavior:**
- New workspaces clone fresh and pull latest
- Existing workspaces skip git operations (safe for concurrent agents)
- Disposing a workspace resets git state (`git checkout -- .`)

---

### Sessions

A session is one agent running in one workspace.

**Spawning:**
- New workspace - Fresh git clone, clean slate
- Existing workspace - Reuse directory, add another agent to the mix
- Provide an optional nickname to easily identify sessions
- Attach via terminal anytime: `tmux attach -t <session>`

**Session lifecycle:**
- Agent runs in a tmux session (persists after process exits)
- Dashboard shows real-time terminal output
- Mark sessions as done when finished (disposes the tmux session)

---

### Web Dashboard

Open `http://localhost:7337` after starting the daemon.

**Pages:**
- **Sessions** (`/`, `/sessions`) - View all sessions grouped by workspace, filter by status or repo, scan for workspace changes
- **Session Detail** (`/sessions/:id`) - Watch terminal output, view diffs, manage session
- **Spawn** (`/spawn`) - Start new sessions with the spawn wizard
- **Diff** (`/diff/:workspaceId`) - View git changes for a workspace
- **Settings** (`/config`) - Configure repos, agents, and workspace path

**Key features:**
- **Spawn wizard** - Multi-step form to pick repo, branch, agents, and prompt
- **Live terminals** - Real-time output from running agents
- **Git diffs** - See what agents changed (side-by-side diff viewer)
- **Filters** - Find sessions by status (running/stopped) or repository
- **Git status** - See at a glance which workspaces have uncommitted changes
- **Connection status** - Indicator shows if dashboard is connected to daemon

**Getting started:**
First-time users see a setup wizard to configure workspace path, repos, and agents.

---

### CLI Commands

```
schmux start          # start daemon in background
schmux stop           # stop daemon
schmux status         # show daemon status, web dashboard URL
schmux daemon-run     # run daemon in foreground (debug)
```

---

## Future Scope

### v0.9 - Richer collaboration

- **Copy between sessions** - Share text/output from one session to another
- **Batch grouping** - See which sessions were started together

### v1.0 - Production polish

- **Completion detection** - Know when agents finish vs waiting for input
- **Easy agent config** - Add new LLMs without wrapper scripts
- **Documentation** - Installation guide, tutorials, examples

### v1.1 - Workflow tools

- **Saved prompts** - Reuse common task prompts
- **Better terminal** - Full interactive terminal in browser

### v1.9 - Descoped

- **CLI spawning** - `schmux run` commands (use web dashboard instead)

### v2.0+ - Future ideas

- Budget tracking, feedback/rating system, search across sessions, remote git operations
