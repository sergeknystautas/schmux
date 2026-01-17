# CLI

**Problem:** Some tasks are faster from a terminal; others benefit from visual UI. Tools that force you into one interface create friction when the other would be better for the job.

The CLI is for **speed and scripting** — quick commands from the terminal with composable operations and JSON output for automation.

The web dashboard is for **observability and orchestration** — visual monitoring, real-time terminal streaming, and interactive session management.

---

## Quick Reference

```bash
# Daemon Management
schmux start              # Start daemon in background
schmux stop               # Stop daemon
schmux status             # Show daemon status and dashboard URL
schmux daemon-run         # Run daemon in foreground (debugging)

# Session Management
schmux spawn -t <target> [flags]          # Spawn a new session
schmux list [--json]                     # List all sessions
schmux attach <session-id>                # Attach to a session
schmux dispose <session-id>               # Dispose a session

# Workspace Management
schmux refresh-overlay <workspace-id>     # Refresh overlay files for a workspace

# Help
schmux help                               # Show help message
```

**Common spawn patterns:**
```bash
# In current workspace (auto-detected)
schmux spawn -t claude -p "do a code review"

# In specific workspace
schmux spawn -w . -t kimi-thinking -p "do a code review"

# In new workspace
schmux spawn -r schmux -t glm-4.7 -p "implement X"

# With branch
schmux spawn -r schmux -b feature-x -t codex -p "implement X"
```

---

## Daemon Commands

### `schmux start`

Start the schmux daemon in the background.

```bash
schmux start
```

The daemon serves the web dashboard at `http://localhost:7337` and handles session spawning via the HTTP API.

---

### `schmux stop`

Stop the running schmux daemon.

```bash
schmux stop
```

---

### `schmux status`

Show daemon status and dashboard URL.

```bash
schmux status
```

**Output:**
```
schmux daemon is running
Dashboard: http://localhost:7337
```

Exits with code 1 if the daemon is not running.

---

### `schmux daemon-run`

Run the daemon in the foreground (for debugging).

```bash
schmux daemon-run
```

Useful for seeing debug output directly in the terminal.

---

## Session Commands

### `schmux spawn`

Spawn a new run target session.

**Syntax:**
```bash
schmux spawn -t <target> [flags]
```

**Required Flags:**
| Flag | Description |
|------|-------------|
| `-t, --target` | Run target name (user target, detected tool, or variant) |

**Optional Flags:**
| Flag | Description |
|------|-------------|
| `-p, --prompt` | Prompt for promptable targets (required if target is promptable) |
| `-w, --workspace` | Workspace path (e.g., `.` for current dir, or `~/ws/myproject-001`) |
| `-r, --repo` | Repo name from config (creates new workspace) |
| `-b, --branch` | Git branch (default: `main`) |
| `-n, --nickname` | Optional session nickname |
| `--json` | JSON output for scripting |

**Workspace Resolution (in order of precedence):**

1. **If `-w` is specified** → Use that workspace (repo is inferred)
2. **If `-r` is specified** → Create/find workspace for that repo
3. **If neither** → Auto-detect if current directory is a workspace, assume `-w .`

**Examples:**

```bash
# Spawn in current workspace (simplest - no flags needed if in a workspace)
schmux spawn -t claude -p "Please do a code review"

# Explicit current workspace
schmux spawn -w . -t kimi-thinking -p "do a code review"

# Spawn in new workspace
schmux spawn -r schmux -t glm-4.7 -p "Please do a code review"

# With specific branch
schmux spawn -r schmux -b feature-x -t codex -p "implement this feature"

# With nickname
schmux spawn -t glm-4.7 -n "reviewer" -p "check this PR"

# Spawn a command target (no prompt)
schmux spawn -t zsh -n "shell"

# JSON output for scripting
schmux spawn -t glm-4.7 -p "fix bug" --json
```

**Output:**
```
Spawn results:
  [glm-4.7] Session: schmux-001-abc12345
        Workspace: schmux-001
        Attach: schmux attach schmux-001-abc12345
```

---

### `schmux list`

List all sessions (grouped by workspace).

**Syntax:**
```bash
schmux list [--json]
```

**Examples:**

```bash
# List sessions
schmux list

# JSON output
schmux list --json
```

**Output:**
```
Sessions:

schmux-001 (main) [dirty]
  [schmux-001-abc12345] glm-4.7 - running
  [schmux-001-def67890] claude - stopped

myproject-002 (feature-x) [ahead 3]
  [myproject-002-xyz789] codex - running
```

---

### `schmux attach`

Attach to a running session with tmux.

**Syntax:**
```bash
schmux attach <session-id>
```

**Example:**
```bash
schmux attach schmux-001-abc12345
```

This is equivalent to running `tmux attach -t <session-id>` directly, but uses the schmux session ID for convenience.

---

### `schmux dispose`

Dispose (delete) a session.

**Syntax:**
```bash
schmux dispose <session-id>
```

**Example:**
```bash
schmux dispose schmux-001-abc12345
```

**Output:**
```
Dispose session schmux-001-abc12345? [y/N] y
Session schmux-001-abc12345 disposed.
```

---

## Workspace Commands

### `schmux refresh-overlay`

Refresh (reapply) overlay files to a workspace.

**Syntax:**
```bash
schmux refresh-overlay <workspace-id>
```

Overlays allow you to copy local-only files (like `.env` files) to workspaces automatically. Files are stored in `~/.schmux/overlays/<repo-name>/` and are only copied if covered by `.gitignore`.

**Example:**
```bash
schmux refresh-overlay myproject-001
```

**Output:**
```
Refreshing overlay for workspace myproject-001 (myproject)
Overlay refreshed successfully for workspace myproject-001
```

**Errors:**
- `workspace has active sessions: <id>` - Cannot refresh while sessions are running
- `workspace not found: <id>` - Workspace ID doesn't exist

**When to use:**
- After updating files in an overlay directory
- After adding new files to an overlay directory
- After a workspace was created before overlays were set up

---

## Common Workflows

### Starting Fresh

```bash
# Start the daemon
schmux start

# Spawn a new session in a fresh workspace
schmux spawn -r myproject -t claude -p "Add user authentication"

# Check status
schmux list
```

### Working in Current Workspace

```bash
# Navigate to workspace (or already be in one from another session)
cd ~/schmux-workspaces/myproject-001

# Spawn another session in this workspace
schmux spawn -t glm-4.7 -p "Review the changes"

# List all sessions
schmux list
```

### Scripting with JSON

```bash
# Spawn and get JSON output
schmux spawn -t glm-4.7 -p "fix bug" --json > result.json

# Get session ID with jq
SESSION_ID=$(schmux spawn -t glm-4.7 -p "fix bug" --json | jq -r '.[0].session_id')

# List all sessions as JSON
schmux list --json

# Attach to the session
schmux attach $SESSION_ID
```

---

## Exit Codes

- `0` - Success
- `1` - Error (daemon not running, invalid arguments, command failed)

---

## Configuration

The CLI reads configuration from `~/.schmux/config.json`. See [targets.md](targets.md) for run target configuration and [PHILOSOPHY.md](PHILOSOPHY.md) for product principles.

Run targets can be referenced by name in the `-t` flag. Detected tools and variants are also valid targets.

**Example config:**
```json
{
  "workspace_path": "~/schmux-workspaces",
  "repos": [
    {"name": "schmux", "url": "git@github.com:user/schmux.git"}
  ],
  "run_targets": [
    {"name": "glm-4.7-cli", "type": "promptable", "command": "/path/to/glm-4.7"},
    {"name": "zsh", "type": "command", "command": "zsh"}
  ],
  "quick_launch": [
    {"name": "Review: Kimi", "target": "kimi-thinking", "prompt": "Please review these changes."}
  ]
}
```

---

## When to Use CLI vs Web

**Use the CLI when:**
- You're already in a terminal
- You need quick, one-off operations
- You're scripting or automating
- You want JSON output for processing

**Use the web dashboard when:**
- You need to monitor many sessions at once
- You want real-time terminal output
- You're comparing results across agents
- You prefer visual interaction
