# schmux - Smart Cognitive Hub on tmux

## Specification v1.0

**Source of truth:** If anything here conflicts with [docs/PHILOSOPHY.md](docs/PHILOSOPHY.md), the philosophy doc wins.

### Overview

A Golang application that orchestrates multiple run targets (detected tools like Claude, Codex, Gemini, plus user-defined commands) running in tmux sessions. Provides a web dashboard for spawning, monitoring, and managing target sessions across git repositories.

**Core concepts:**
- Run multiple run targets simultaneously on the same codebase
- Each target runs in a real filesystem workspace directory (git-based, non-containerized)
- Monitor all targets from a web dashboard with live terminal output
- Compare results across targets by viewing git diffs

---

### Configuration

Configuration lives at `~/.schmux/config.json`. You can edit it directly or use the web dashboard's settings page. Secrets can be placed in `~/.schmux/secrets.json` for local-only values.

```json
{
  "workspace_path": "~/schmux-workspaces",
  "repos": [
    {"name": "myproject", "url": "git@github.com:user/myproject.git"}
  ],
  "run_targets": [
    {"name": "glm-4.7-cli", "type": "promptable", "command": "/path/to/glm-4.7"},
    {"name": "zsh", "type": "command", "command": "zsh"}
  ],
  "quick_launch": [
    {"name": "Review: Kimi", "target": "kimi-thinking", "prompt": "Please review these changes."}
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
- `run_targets` - User-supplied run targets (promptable or command)
- `quick_launch` - Saved presets over run targets, detected tools, or variants
- `terminal` - Terminal dimensions for sessions

**Advanced settings** (optional `internal` section):
- Polling intervals for status updates
- Session tracking timing

---

### State

Application state is stored at `~/.schmux/state.json` and managed automatically. Tracks your workspaces and sessions.

---

### Workspaces

Workspaces are directories where targets do their work.

- Each repo gets sequential directories: `myproject-001`, `myproject-002`, etc.
- Multiple targets can work in the same workspace simultaneously
- Each workspace tracks git status (dirty, ahead, behind)
- Workspaces are created on-demand when you spawn sessions

**Git behavior:**
- New workspaces clone fresh and pull latest
- Existing workspaces skip git operations (safe for concurrent targets)
- Disposal is blocked if a workspace has uncommitted or unpushed changes; no automatic git reset

---

### Sessions

A session is one target running in one workspace.

**Spawning:**
- New workspace - Fresh git clone, clean slate
- Existing workspace - Reuse directory, add another target to the mix
- Provide an optional nickname to easily identify sessions
- Attach via terminal anytime: `tmux attach -t <session>`

**Session lifecycle:**
- Target runs in a tmux session (persists after process exits)
- Dashboard shows real-time terminal output
- Mark sessions as done when finished; tmux sessions are disposed explicitly

---

### Web Dashboard

Open `http://localhost:7337` after starting the daemon.

**Pages:**
- **Sessions** (`/`, `/sessions`) - View all sessions grouped by workspace, filter by status or repo, scan for workspace changes
- **Session Detail** (`/sessions/:id`) - Watch terminal output, view diffs, manage session
- **Spawn** (`/spawn`) - Start new sessions with the spawn wizard
- **Diff** (`/diff/:workspaceId`) - View git changes for a workspace
- **Settings** (`/config`) - Configure repos, run targets, variants, and workspace path

**Key features:**
- **Spawn wizard** - Multi-step form to pick repo, branch, targets, and prompt
- **Live terminals** - Real-time output from running targets
- **Git diffs** - See what targets changed (side-by-side diff viewer)
- **Filters** - Find sessions by status (running/stopped) or repository
- **Git status** - See at a glance which workspaces have uncommitted changes
- **Connection status** - Indicator shows if dashboard is connected to daemon

**Getting started:**
First-time users see a setup wizard to configure workspace path, repos, and run targets.

---

### CLI Commands

For full syntax, flags, and examples, see [docs/cli.md](docs/cli.md).

```bash
# Daemon Management
schmux start              # Start daemon in background
schmux stop               # Stop daemon
schmux status             # Show daemon status and dashboard URL
schmux daemon-run         # Run daemon in foreground (debugging)

# Session Management
schmux spawn -a <target> [flags]          # Spawn a new session
schmux list [--json]                     # List all sessions
schmux attach <session-id>                # Attach to a session
schmux dispose <session-id>               # Dispose a session

# Workspace Management
schmux refresh-overlay <workspace-id>     # Refresh overlay files for a workspace

# Help
schmux help                               # Show help message
```

---

## Future Scope

### v1.1 - Production polish

- **Documentation** - Installation guide, tutorials, examples

### v1.2 - Workflow tools

- **Copy between sessions** - Share text/output from one session to another
- **Batch grouping** - See which sessions were started together

### v2.0+ - Future ideas

- Budget tracking, feedback/rating system, search across sessions, remote git operations
