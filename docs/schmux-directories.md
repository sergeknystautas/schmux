# Schmux Directories

Schmux uses two types of directories:

1. **Global Directory** - `~/.schmux/` in user's home
2. **Workspace Directory** - `<workspace>/.schmux/` inside each git worktree

---

## Global Directory

`~/.schmux/`

| Path                    | Purpose                                            | Created By                           | Notes                                |
| ----------------------- | -------------------------------------------------- | ------------------------------------ | ------------------------------------ |
| `config.json`           | Main configuration (repos, agents, workspace path) | User or `schmux init`                | Required for daemon                  |
| `state.json`            | Runtime state (workspaces, sessions, PIDs)         | Daemon                               | Auto-managed                         |
| `daemon.pid`            | PID file for running daemon                        | `schmux start`                       | Used to check if daemon is running   |
| `daemon.started`        | Timestamp marker for daemon startup                | `schmux start`                       | Used for health checks               |
| `daemon-startup.log`    | Daemon startup logs                                | Daemon                               | First few seconds of output          |
| `secrets.json`          | Encrypted secrets storage                          | `schmux secret set`                  | Git should NEVER see this            |
| `signaling.md`          | Agent signaling instructions template              | `ensure.SignalingInstructionsFile()` | Injected via CLI flags               |
| `dashboard/`            | Downloaded dashboard assets                        | `internal/assets`                    | For standalone binary                |
| `lore/<repo>/`          | Autolearn state (curated learnings)                | Autolearn curator                    | JSONL state files per repo           |
| `overlays/<repo>/`      | Overlay files synced across workspaces             | Workspace manager                    | .env, config files, etc.             |
| `repos/`                | Bare git clones of repositories                    | Workspace manager                    | Source for worktrees                 |
| `schemas/`              | JSON schemas for oneshot validation                | `internal/oneshot`                   | Generated from Go structs at startup |
| `dev-state.json`        | Dev mode state (current worktree)                  | `dev.sh` wrapper                     | Development only                     |
| `dev-build-status.json` | Dev mode build status                              | `dev.sh` wrapper                     | Development only                     |
| `dev-restart.json`      | Dev mode restart manifest                          | Dashboard                            | Development only                     |

### Code Locations

- **Config loading**: `internal/config/config.go:1206`
- **State management**: `internal/state/state.go`
- **Daemon PID/started**: `internal/daemon/daemon.go:42,100,287-288`
- **Secrets path**: `internal/config/secrets.go:38`
- **Signaling template**: `internal/workspace/ensure/manager.go:256`
- **Overlay directory**: `internal/workspace/overlay.go:24`
- **Autolearn state**: `internal/autolearn/signals.go`

---

## Workspace Directory

`<workspace>/.schmux/`

| Path                        | Purpose                                   | Created By        | Lifecycle                                 |
| --------------------------- | ----------------------------------------- | ----------------- | ----------------------------------------- |
| `events/<session-id>.jsonl` | Agent event JSONL file (status, friction) | Session spawn     | Per-session, env var `SCHMUX_EVENTS_FILE` |
| `lore.jsonl`                | Autolearn scratchpad (friction capture)   | Hooks/agents      | Append-only, pruned after 30 days         |
| `config.json`               | Per-workspace repo config overrides       | Workspace manager | Optional, for namespaced configs          |

### Code Locations

- **Events directory**: `internal/session/manager.go` - Creates `.schmux/events/` and sets `SCHMUX_EVENTS_FILE`
- **Autolearn signals**: `internal/autolearn/signals.go` - Append/read JSONL entries
- **Hook scripts**: `internal/detect/` - `EnsureGlobalHookScripts()` writes to `~/.schmux/hooks/`
- **Workspace config**: `internal/workspace/config.go` - Per-workspace config loading

---

## Hiding Daemon Files from Git Status

### Problem

When schmux creates a workspace, daemon-managed files (signal files, hook scripts, autolearn) appear as untracked in `git status`:

```
Untracked files:
  .schmux/events/
  .schmux/hooks/
```

We can't ignore `.schmux/` entirely because `.schmux/config.json` is a user-managed file that should remain visible.

### Solution: `.git/info/exclude` with Managed Markers

Schmux writes specific exclude patterns to `.git/info/exclude` using managed markers:

```gitignore
# SCHMUX:BEGIN - managed by schmux, do not edit
.schmux/hooks/
.schmux/events/
.opencode/plugins/schmux.ts
.opencode/commands/schmux-*.md
.opencode/commands/commit.md
.claude/skills/schmux-*/
# SCHMUX:END
```

Only daemon-written paths are excluded. User-managed files like `.schmux/config.json` remain visible in `git status`.

**Implementation**: `internal/workspace/ensure/manager.go` — `GitExclude()` and `ensureExcludeEntries()`

### How It Works

- The `# SCHMUX:BEGIN` / `# SCHMUX:END` markers delimit the managed block
- If markers exist, the block is replaced in-place (handles pattern updates)
- If no markers exist, the block is appended
- User entries in `info/exclude` are preserved (before and after the block)
- The operation is idempotent — running it twice produces an identical file

### When It Runs

- **Daemon startup**: `workspace.Manager.EnsureAll()` ensures all existing local workspaces
- **Session spawn**: `ensure.Ensurer.ForSpawn()` ensures hooks, scripts, and git exclude
- **Overlay refresh**: `ensure.Ensurer.ForWorkspace()` ensures hooks, scripts, and git exclude

This means excludes are applied both to existing workspaces (on restart) and new workspaces (on creation).

### Worktree Handling

- **Full clones**: Writes to `<workspace>/.git/info/exclude`
- **Worktrees**: Resolves the shared git directory via `git rev-parse --git-common-dir` and writes to `<bare-repo>/info/exclude`, which covers all worktrees sharing that bare repo

### Why `info/exclude` Over `.gitignore`

| Aspect                      | `info/exclude`                    | `.gitignore`                       |
| --------------------------- | --------------------------------- | ---------------------------------- |
| **Modifies tracked files**  | No                                | Yes — `.gitignore` is tracked      |
| **Risk of upstream commit** | None                              | Could be accidentally committed    |
| **Scope**                   | Per-clone only                    | Shared across all clones           |
| **Daemon control**          | Safe — daemon owns the file       | Risky — user/agent may edit it     |
| **Selective patterns**      | Yes — exclude specific paths only | Same, but visible to collaborators |

---

## Remote Workspaces

Remote workspaces skip git exclude setup — the daemon cannot access the remote filesystem's `.git/info/exclude`. Remote users may need to manually exclude `.schmux/` paths.

---

## Summary

| Directory              | Scope         | Contents                                 | Git Visibility                 |
| ---------------------- | ------------- | ---------------------------------------- | ------------------------------ |
| Global (`~/.schmux/`)  | Global        | Config, state, daemon, secrets, overlays | N/A (outside repos)            |
| Workspace (`.schmux/`) | Per-workspace | Signal files, autolearn, hooks, config   | Auto-excluded via info/exclude |
