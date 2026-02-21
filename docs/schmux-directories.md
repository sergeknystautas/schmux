# Schmux Directories

Schmux uses two types of directories:

1. **Global Directory** - `~/.schmux/` in user's home
2. **Workspace Directory** - `<workspace>/.schmux/` inside each git worktree

---

## Global Directory

`~/.schmux/`

| Path                      | Purpose                                                | Created By                           | Notes                              |
| ------------------------- | ------------------------------------------------------ | ------------------------------------ | ---------------------------------- |
| `config.json`             | Main configuration (repos, agents, workspace path)     | User or `schmux init`                | Required for daemon                |
| `state.json`              | Runtime state (workspaces, sessions, PIDs)             | Daemon                               | Auto-managed                       |
| `daemon.pid`              | PID file for running daemon                            | `schmux start`                       | Used to check if daemon is running |
| `daemon.started`          | Timestamp marker for daemon startup                    | `schmux start`                       | Used for health checks             |
| `daemon-startup.log`      | Daemon startup logs                                    | Daemon                               | First few seconds of output        |
| `secrets.json`            | Encrypted secrets storage                              | `schmux secret set`                  | Git should NEVER see this          |
| `signaling.md`            | Agent signaling instructions template                  | `ensure.SignalingInstructionsFile()` | Injected via CLI flags             |
| `dashboard/`              | Downloaded dashboard assets                            | `internal/assets`                    | For standalone binary              |
| `lore/`                   | Central lore state directory                           | `internal/lore`                      | See below                          |
| `lore/<repo>/state.jsonl` | Lore state-change records (proposed/applied/dismissed) | Curator                              | Aggregated from all workspaces     |
| `lore-proposals/`         | Curated lore proposals awaiting review                 | Curator                              | JSON files per proposal            |
| `overlays/<repo>/`        | Overlay files synced across workspaces                 | Workspace manager                    | .env, config files, etc.           |
| `repos/`                  | Bare git clones of repositories                        | Workspace manager                    | Source for worktrees               |
| `schemas/`                | JSON schemas for oneshot validation                    | `internal/oneshot`                   | Auto-downloaded                    |
| `dev-state.json`          | Dev mode state (current worktree)                      | `dev.sh` wrapper                     | Development only                   |
| `dev-build-status.json`   | Dev mode build status                                  | `dev.sh` wrapper                     | Development only                   |
| `dev-restart.json`        | Dev mode restart manifest                              | Dashboard                            | Development only                   |

### Code Locations

- **Config loading**: `internal/config/config.go:1206`
- **State management**: `internal/state/state.go`
- **Daemon PID/started**: `internal/daemon/daemon.go:42,100,287-288`
- **Secrets path**: `internal/config/secrets.go:38`
- **Signaling template**: `internal/workspace/ensure/manager.go:256`
- **Overlay directory**: `internal/workspace/overlay.go:24`
- **Lore state**: `internal/lore/scratchpad.go:522`

---

## Workspace Directory

`<workspace>/.schmux/`

| Path                       | Purpose                             | Created By                 | Lifecycle                         |
| -------------------------- | ----------------------------------- | -------------------------- | --------------------------------- |
| `signal/<session-id>`      | Agent status signaling file         | Session spawn              | Per-session, not auto-cleaned     |
| `lore.jsonl`               | Lore scratchpad (friction capture)  | Hooks/agents               | Append-only, pruned after 30 days |
| `hooks/capture-failure.sh` | PostToolUseFailure hook script      | `ensure.LoreHookScripts()` | Re-created on each spawn          |
| `hooks/stop-gate.sh`       | Stop hook for session gating        | `ensure.LoreHookScripts()` | Re-created on each spawn          |
| `config.json`              | Per-workspace repo config overrides | Workspace manager          | Optional, for namespaced configs  |

### Code Locations

- **Signal directory**: `internal/session/manager.go:626-647` - Creates `.schmux/signal/` and sets `SCHMUX_STATUS_FILE`
- **Lore scratchpad**: `internal/lore/scratchpad.go` - Append/read JSONL entries
- **Hook scripts**: `internal/workspace/ensure/manager.go:540-557` - `LoreHookScripts()` writes embedded scripts
- **Remote signal**: `internal/session/manager.go:492-517` - Creates `.schmux/signal/` on remote hosts
- **Workspace config**: `internal/workspace/config.go:22-25` - Per-workspace config loading

---

## The Gitignore Problem

### Issue

When schmux creates a workspace in a user's repo that doesn't have `.schmux/` in `.gitignore`, these temp files appear as untracked:

```
Untracked files:
  .schmux/
```

This causes problems:

1. **Pull failures** - "would be overwritten" errors
2. **Accidental commits** - temp files pushed to remote
3. **Noisy git status** - clutter in output
4. **Merge confusion** - conflicts with team members

### Solution: Auto-add to `.gitignore`

The recommended fix is to automatically add `.schmux/` to the workspace's `.gitignore` when `ensure.Workspace()` is called during session spawn.

**Implementation location**: `internal/workspace/ensure/manager.go`

```go
const gitignoreEntry = "\n# Schmux temp files (signaling, lore, hooks)\n.schmux/\n"

func EnsureGitignore(workspacePath string) error {
    gitignorePath := filepath.Join(workspacePath, ".gitignore")

    content, err := os.ReadFile(gitignorePath)
    if err != nil && !os.IsNotExist(err) {
        return err
    }

    if strings.Contains(string(content), ".schmux/") {
        return nil // Already has entry
    }

    f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        return err
    }
    defer f.Close()

    if len(content) > 0 && !bytes.HasSuffix(content, []byte("\n")) {
        f.WriteString("\n")
    }
    _, err = f.WriteString(gitignoreEntry)
    return err
}
```

Call from `Workspace()`:

```go
func Workspace(workspacePath string) error {
    if err := EnsureGitignore(workspacePath); err != nil {
        fmt.Printf("[ensure] warning: failed to update .gitignore: %v\n", err)
    }
    // ... existing code
}
```

### Why This Approach

| Aspect                | Rationale                                             |
| --------------------- | ----------------------------------------------------- |
| **Standard practice** | IDEs, language tools, etc. all do this                |
| **Non-breaking**      | User can remove the entry if they want to commit lore |
| **Self-documenting**  | User sees the comment and understands why it's there  |
| **Minimal intrusion** | Only appends if entry doesn't exist                   |

### Alternative Options

| Option                  | Pros                         | Cons                                     |
| ----------------------- | ---------------------------- | ---------------------------------------- |
| `.git/info/exclude`     | Doesn't modify tracked files | Per-clone only, not visible              |
| Store outside workspace | Zero pollution               | Major refactor, breaks agent file access |
| Warn only               | User control                 | Annoying, requires manual fix            |

---

## Remote Workspaces

For remote workspaces, we can't directly modify `.gitignore`. Options:

1. **Prepend command**: Add `mkdir -p .schmux/signal && echo '.schmux/' >> .gitignore || true` to remote wrapper
2. **Accept limitation**: Remote users may need to manually gitignore

---

## Summary

| Directory              | Scope         | Contents                                 | Gitignore                |
| ---------------------- | ------------- | ---------------------------------------- | ------------------------ |
| Global (`~/.schmux/`)  | Global        | Config, state, daemon, secrets, overlays | N/A (outside repos)      |
| Workspace (`.schmux/`) | Per-workspace | Signal files, lore, hooks                | Auto-add to `.gitignore` |
