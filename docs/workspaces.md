# Workspaces

## What it does

Workspaces are isolated working directories on the filesystem where AI agents run. schmux creates, tracks, and manages these directories so multiple agents can work in parallel without stepping on each other.

---

## Key files

| File                                       | Purpose                                                     |
| ------------------------------------------ | ----------------------------------------------------------- |
| `internal/workspace/manager.go`            | Workspace lifecycle: create, dispose, locking, VCS status   |
| `internal/workspace/interfaces.go`         | `WorkspaceManager` interface for testability                |
| `internal/workspace/vcs.go`                | `VCSBackend` interface and VCS-agnostic data types          |
| `internal/workspace/vcs_git.go`            | Git backend (worktree, bare clone, status, fetch)           |
| `internal/workspace/vcs_sapling.go`        | Sapling backend (configurable commands, `sl` observability) |
| `internal/workspace/linear_sync.go`        | Sync-from-main and sync-to-main via cherry-pick             |
| `internal/workspace/overlay.go`            | Overlay file copying                                        |
| `internal/workspace/worktree.go`           | Git worktree creation and management                        |
| `internal/workspace/ensure/manager.go`     | Workspace configuration setup (hooks, git exclude)          |
| `internal/config/normalize_bare_paths.go`  | Startup normalization of non-conforming bare repo dirs      |
| `internal/config/relocate_bare_repo.go`    | Bare repo rename utility with worktree fixup                |
| `internal/preview/manager.go`              | Preview proxy lifecycle for workspace web servers           |
| `internal/dashboard/preview_autodetect.go` | Auto-detect listening ports from terminal output            |

---

## VCS Abstraction

schmux supports both git and Sapling workspaces via a `VCSBackend` strategy-object pattern. VCS-specific operations delegate to the interface while the Manager keeps all VCS-agnostic orchestration (state management, overlays, session coordination, reuse logic).

### The VCSBackend interface

Defined in `internal/workspace/vcs.go`. Two tiers:

- **Tier 1 (Lifecycle):** `EnsureRepoBase`, `CreateWorkspace`, `RemoveWorkspace`, `PruneStale`, `Fetch`, `IsBranchInUse`
- **Tier 2 (Observability):** `GetStatus`, `GetChangedFiles`, `GetDefaultBranch`, `GetCurrentBranch`, `EnsureQueryRepo`, `FetchQueryRepo`, `ListRecentBranches`, `GetBranchLog`
- **Not abstracted (Tier 3):** Linear sync, conflict resolution, commit graph, push-to-branch -- these remain git-only.

### Backend resolution

The Manager holds `backends map[string]VCSBackend` with `"git"` and `"sapling"` entries. `backendFor(repoURL)` uses `config.Repo.VCS` (defaults to `"git"`). `backendForWorkspace(workspaceID)` uses `state.Workspace.VCS` (set at creation, persisted).

### Git backend (`vcs_git.go`)

Uses git worktrees backed by a shared bare clone. `EnsureRepoBase` clones a bare repo if missing. `CreateWorkspace` does `git worktree add`. `IsBranchInUse` checks if a branch is already checked out in another worktree.

### Sapling backend (`vcs_sapling.go`)

Uses configurable command templates for lifecycle and `sl` directly for observability. Lifecycle commands are Go `text/template` strings (defaults use `sl clone` / `rm -rf`). Environments with specialized tooling (e.g., EdenFS) override via `sapling_commands` in config. Key differences: `IsBranchInUse` always returns false, `PruneStale` is a no-op, `Fetch` runs `sl pull` per workspace.

---

## GetOrCreate: Workspace Reuse Tiers

`GetOrCreate` in `manager.go` finds or creates a workspace. Tiers evaluated in order; first match wins.

| Tier                      | What it matches                             | What it does                                                                                    |
| ------------------------- | ------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| **0 -- Recyclable**       | Same repo, status `"recyclable"`            | Verifies directory, checks divergence, `prepare()`, re-copies overlays, promotes to `"running"` |
| **1 -- Same branch**      | Same repo + same branch, no active sessions | `prepare()`, re-copies overlays                                                                 |
| **2 -- Different branch** | Same repo, any branch, no active sessions   | Checks divergence, `prepare()` with new branch                                                  |
| **3 -- Create**           | No match                                    | `EnsureRepoBase`, `Fetch`, `CreateWorkspace`, overlays, state                                   |

All tiers promote the workspace to `WorkspaceStatusRunning` on reuse. The per-repo lock (`repoLock`) protects from concurrent callers claiming the same workspace.

---

## Recyclable Workspaces

### Why they exist

Disposing a workspace deletes thousands of files, then respawning recreates them. Backup software (Time Machine, Backblaze) treats every file event as a sync operation, saturating upload bandwidth even though branches typically differ by single-digit percentages.

### How it works

When `recycle_workspaces` is enabled in config, disposing a workspace keeps the directory on disk and marks it `"recyclable"`. On next spawn for the same repo, it is reused via `git checkout` -- only files that differ between branches are touched.

```json
{ "recycle_workspaces": true }
```

Default: `false`. Toggle in the config editor at `/config`.

### Dispose path

Recycling is checked inside `dispose()` regardless of the `force` parameter. `force` controls whether safety checks are skipped -- it does NOT control recycling. This is deliberate: `handleDisposeWorkspaceAll` (the normal "dispose workspace + sessions" button) uses `DisposeForce`, and if force bypassed recycling, the most common dispose flow would never recycle.

### Purge API

Separate from dispose. Purge is the explicit "I want disk space back" escape hatch.

```
DELETE /api/workspaces/{id}/purge      -- single recyclable
DELETE /api/workspaces/purge?repo=URL  -- all recyclable for a repo
DELETE /api/workspaces/purge           -- all recyclable
```

Only operates on recyclable workspaces; calling on a running workspace returns an error.

### Dashboard behavior

- `buildSessionsResponse` excludes recyclable workspaces from WebSocket broadcasts -- main UI stays clean.
- `GET /api/workspaces/recyclable` provides counts by repo. Dashboard shows a collapsed indicator with "Purge" button.
- `UpdateAllVCSStatus` and `EnsureAll` skip recyclable workspaces.

### Crash recovery

On daemon startup, workspaces stuck in `"disposing"` are retried via `DisposeForce()`. With recycling on and directory present, the normal dispose path marks them `"recyclable"`. With recycling off, the directory is deleted and the workspace removed from state. If the directory is already gone, the workspace is removed from state.

---

## BarePath Normalization

### The canonical rule

A repo's bare repo directory is derived from its `Name`: for git repos it is `{name}.git`, for sapling repos it is `{name}`. The `BarePath` config field is always derivable.

### Why `detectExistingBarePath` was removed

The system previously tolerated non-conforming filesystem layouts, scanning the disk to discover whatever directory name happened to exist. This was an architectural violation: config and filesystem are supposed to agree. The correct response to disagreement is to fix the filesystem.

### How normalization works

`NormalizeBarePaths()` in `internal/config/normalize_bare_paths.go` runs at daemon startup. For each git repo where `BarePath != Name + ".git"`:

1. Finds the current bare repo on disk
2. Calls `RelocateBareRepo(oldPath, newPath)` to rename and fix up worktree references
3. Updates config and state, saves immediately

`RelocateBareRepo` (`internal/config/relocate_bare_repo.go`) renames the directory and rewrites `gitdir:` lines in all worktree `.git` files. It resolves symlinks before string replacement and rolls back the rename if worktree fixup fails.

---

## Filesystem-Based, Not Containerized

Workspaces are working directories on your filesystem, not containers or virtualized environments.

- Workspace directories live in `~/.schmux-workspaces/` by default
- Each repository gets sequential workspace directories: `myproject-001`, `myproject-002`, etc.
- Multiple agents can work in the same workspace simultaneously
- Full access to your real files, tools, and environment
- No container startup overhead or complexity

---

## Workspace Ensure System

The `ensure` package (`internal/workspace/ensure/manager.go`) handles ensuring workspaces have the necessary schmux configuration. It is initialized once at daemon startup with a state reference and passed to the session manager and workspace manager.

### Architecture decisions

- **Ensurer is a stateful service, not a bag of functions.** It holds a `state.StateStore` reference and looks up sessions internally. Callers just say "ensure this workspace" without making decisions about what is needed.
- **Two entry points for different contexts:**
  - `ForSpawn(workspaceID, currentTarget)` — called during session spawn; includes the about-to-be-spawned target since it is not in state yet.
  - `ForWorkspace(workspaceID)` — called from daemon startup and overlay refresh; uses sessions already in state.
- **Agent hook detection lives inside ensure.** `workspaceHookTools()` scans sessions in state to determine which agents support hooks, then only sets up hooks for those tools. Non-Claude workspaces do not get Claude hooks.
- **One code path.** Spawn, overlay refresh, and daemon startup all call the same internal `ensureWorkspace()` function. Adding a new ensure step means adding it in one place.

### What ensure does

1. **Agent hooks** — Sets up tool-specific hooks (e.g., Claude hooks, lore scripts) via the detect adapter system, only for agents that support them.
2. **Tool commands** — Sets up tool-specific commands (e.g., `/commit` for OpenCode).
3. **Git exclude** — Writes `.git/info/exclude` entries for schmux-managed paths (`.schmux/hooks/`, `.schmux/events/`, etc.) so they do not appear in `git status`.

### Agent-specific signaling

Agent instructions (`SignalingInstructions`, `AgentInstructions`) stay in `ensure/manager.go` but are called from `internal/session/manager.go`. These are session-level concerns (what prompt injection to use for a specific agent), not workspace-level configuration.

---

## Workspace Locking

The workspace Manager coordinates concurrent git operations via per-workspace locking.

### Problem solved

Sync operations (`LinearSyncFromDefault`, `LinearSyncResolveConflict`) perform multi-step git sequences (fetch, rebase, commit, reset) that must run atomically. The git-watcher fires `UpdateGitStatus` on filesystem events, which can run `git fetch` mid-rebase, causing spurious failures.

### Design

All locking lives in the workspace Manager. There is no dashboard callback.

- **`lockedWorkspaces map[string]bool`** — tracks which workspaces are locked for sync operations.
- **`workspaceGates map[string]*sync.RWMutex`** — per-workspace gate that coordinates git status vs. sync operations.

### Lock semantics

- **Exclusive**: only one sync operation holds the lock per workspace at a time.
- **Fail-fast for competing syncs**: `LockWorkspace()` returns false immediately if another sync already holds the lock.
- **Waits for git status**: `LockWorkspace()` acquires a full write lock on the gate, blocking until any in-flight `UpdateGitStatus` on that workspace completes.
- **Git status bails early**: `UpdateGitStatus()` checks `IsWorkspaceLocked()` and returns `ErrWorkspaceLocked` if the workspace is locked. It also holds an RLock on the gate while running.

### Visual lockdown

Lock state is communicated via a dedicated `workspace_locked` WebSocket message type, sent in real-time (not debounced) when lock state changes. Both sync operations get the same tab lockdown in the frontend.

- The `workspace_locked` message is separate from the debounced `sessions` broadcast to avoid clobber and timing races.
- Frontend stores lock state in a separate `Record<string, WorkspaceLockState>`, not merged into the `workspaces` array.
- `ErrWorkspaceLocked` returns HTTP 409 Conflict.

### Scope

Locking covers `LinearSyncFromDefault` and `LinearSyncResolveConflict` — the two multi-step rebase flows. Single-command operations (`LinearSyncToDefault`, `PushToBranch`) are not locked but can be added later if needed.

---

## Workspace Overlays

Local-only files (`.env`, configs, secrets) that should not be in git can be automatically copied into each workspace via the overlay system.

### Storage

Overlay files are stored in `~/.schmux/overlays/<repo-name>/` where `<repo-name>` matches the name from your repos config.

Example structure:

```
~/.schmux/overlays/
├── myproject/
│   ├── .env                 # Copied to workspace root
│   └── config/
│       └── local.json      # Copied to workspace/config/local.json
```

### Behavior

- Files are copied after workspace creation, preserving directory structure
- Each file must be covered by `.gitignore` (enforced for safety)
- Use `schmux refresh-overlay <workspace-id>` to reapply overlay files to existing workspaces
- Overlay files overwrite existing workspace files

### Safety Check

The overlay system enforces that files are truly local-only by checking `.gitignore` coverage:

```bash
git check-ignore -q <path>
```

If a file is NOT matched by `.gitignore`, the copy is skipped with a warning. This prevents accidentally shadowing tracked repository files.

---

## Preview Proxy

Workspace web servers (Vite, Next.js, etc.) are automatically detected and proxied through stable per-workspace port blocks.

### How it works

1. **Terminal output scanning** detects `localhost:XXXX` patterns in session output (`internal/dashboard/preview_autodetect.go`).
2. **Process port verification** confirms the session's PID is actually listening on detected ports via `lsof` (macOS) or `ss` (Linux).
3. **Stable port allocation** — each workspace gets a fixed block of 10 ports starting from a configurable base (default 53000). Ports are deterministic and survive daemon restarts.
4. **Reverse proxy** via `httputil.ReverseProxy` with WebSocket upgrade support. No path rewriting.

### Key properties

- **Idempotent**: re-requesting preview for same `(workspace, target host, target port)` returns the same stable port.
- **Browser storage persists**: cookies and localStorage survive because the proxy port never changes for a given workspace slot.
- **Loopback-only targets**: proxy targets must resolve to `127.0.0.1`, `::1`, or `localhost`.
- **Remote workspaces unsupported**: returns an explicit unsupported error.
- **Cleanup**: previews are removed when workspaces are disposed. No idle timeout.

### Configuration

```json
{
  "network": {
    "preview_port_base": 53000,
    "preview_port_block_size": 10
  }
}
```

These are write-once: changing them after workspaces have been allocated invalidates existing port assignments.

---

## Git Status Visualization

The dashboard shows workspace git status at a glance:

- **Dirty indicator**: Uncommitted changes present
- **Branch name**: Current branch (e.g., `main`, `feature/x`) — clickable link to remote when available
- **Ahead/Behind**: Commits ahead or behind origin
- **Line changes**: Color-coded indicators showing uncommitted line additions (+N in green) and deletions (-M in red)

### Clickable Branch Links

When a branch has a remote tracking branch, the branch name in the workspace table appears as a clickable link that opens the branch in the web UI (GitHub, GitLab, Bitbucket, or generic git hosts). Supports both SSH (`git@host:user/repo`) and HTTPS URL formats, with proper URL encoding for special characters.

### Line Change Tracking

The workspace table displays uncommitted line additions and deletions calculated via `git diff --numstat HEAD`, covering both staged and unstaged modifications:

- **+N** (green): Lines added
- **-M** (red): Lines removed

---

## Diff Viewer

### Built-in Diff Viewer

View what changed in a workspace with the built-in diff viewer:

- Side-by-side git diffs
- See what agents changed across multiple workspaces
- Access via dashboard or `schmux diff` commands

### External Diff Tool Integration

Launch workspace changes in your preferred diff tool (VS Code, Kaleidoscope, or any custom tool) directly from the web dashboard.

Configure named commands in `~/.schmux/config.json` under `external_diff_commands` using template placeholders:

```json
{
  "external_diff_commands": [
    { "name": "VS Code", "command": "code --diff {old_file} {new_file}" },
    { "name": "Kaleidoscope", "command": "ksdiff {old_file} {new_file}" }
  ]
}
```

Available placeholders:

- `{old_file}`: Original file version
- `{new_file}`: Modified file version
- `{file}`: Current file (for single-file tools)

The dashboard displays a DiffDropdown UI component on workspace rows with your configured commands. Temp files are automatically cleaned up via scheduled sweeping.

---

## VS Code Integration

Launch a VS Code window directly in any workspace:

- Dashboard: "Open in VS Code" button on workspace
- CLI: `schmux code <workspace-id>`

---

## Safety Checks

schmux prevents accidental data loss:

- Cannot dispose workspaces with uncommitted changes
- Cannot dispose workspaces with unpushed commits
- Explicit confirmation required for disposal

---

## Git Behavior

### Branch Names

schmux supports standard git branch naming conventions:

**Valid characters:**

- Alphanumeric characters (a-z, A-Z, 0-9)
- Hyphens (-), underscores (\_), periods (.), and forward slashes (/) for hierarchical names
- Examples: `feature-branch`, `feature/subfeature`, `bugfix_123`, `release.v1.0`

**Constraints:**

- Cannot begin or end with a separator (/ - . \_)
- Cannot contain consecutive separators (//, --, \_\_, .., etc.)
- Maximum length follows git conventions (typically 256 characters)

**Automatic handling:**

- If you request a branch name that's already in use, schmux appends a unique suffix (e.g., `feature-x7k`)
- Branch names with invalid characters are rejected with a helpful error message

### Source Code Management

schmux supports two modes for creating workspace directories, configurable in **Settings > Workspace > Source Code Management**:

| Mode                       | Description                               | Branch Handling                               |
| -------------------------- | ----------------------------------------- | --------------------------------------------- |
| **Git Worktree** (default) | Efficient disk usage, shares repo history | Each branch can only be used by one workspace |
| **Git (Full Clone)**       | Independent clones                        | Multiple workspaces can use the same branch   |

### Git Worktree Mode

When using worktrees (the default):

1. **First workspace for a repo**: Creates a bare clone in `~/.schmux/repos/<repo>.git`
2. **Additional workspaces**: Uses `git worktree add` from the bare clone (instant, no network)

**Worktree constraint**: Git only allows one worktree per branch. If you request a branch that's already checked out by another worktree, schmux will automatically create a unique branch name by appending a 3-character suffix (e.g., `feature/foo-x7k`) and create it from the requested branch's tip.

```
Requested "feature/foo" is already in use by workspace "myrepo-001".
Using "feature/foo-x7k" for this workspace.
```

**Why Worktrees?**

- Disk efficient: git objects shared across all workspaces for a repo
- Fast creation: no network clone for additional workspaces
- Tool compatible: VS Code, git CLI, and agents work normally

### Git (Full Clone) Mode

When using full clones:

- Each workspace is a complete, independent git clone
- Multiple workspaces can work on the same branch
- No branch conflict restrictions
- Uses more disk space (no shared objects)

### Existing Workspaces

Regardless of mode, spawning into an existing workspace:

- Skips git operations (safe for concurrent agents)
- Reuses the directory for additional sessions

### Disposal

- Blocked if workspace has uncommitted or unpushed changes
- Uses `git worktree remove` for worktrees, `rm -rf` for full clones
- No automatic git reset — you are in control

---

## Git Workflow Sync

schmux provides bidirectional linear synchronization for clean git history without merge commits.

### Sync from Main

Brings commits from `origin/main` into your current branch via iterative cherry-pick:

- **Handles both behind and diverged states**: Works whether your branch is behind or has diverged from main
- **Conflict detection**: Aborts if conflicts are detected during cherry-pick
- **Preserves local changes**: Creates a temporary WIP commit before syncing, resets after success or abort
- **Access**: Dropdown menu on the git status indicator (behind | ahead) in workspace header
- **Disabled when**: Already caught up to main

This replaces the previous "rebase ff main" action.

### Sync to Main

Pushes your branch commits directly to main via fast-forward:

- **Validation**: Requires clean workspace state (no uncommitted changes, not behind main)
- **Fast-forward only**: No merge commits, maintains linear history
- **Two workflow styles**:
  - **On-main workflow**: Push directly when workspace is already on main branch
  - **Feature branch workflow**: Set upstream to main, sync locally after push
- **Access**: Dropdown menu on git status indicator in workspace header
- **Disabled when**: Workspace has uncommitted changes or is behind main

Both actions are available from the dashboard workspace header git status dropdown.

---

## Workspace Configuration

Workspaces can have their own configuration files that extend the global config with workspace-specific settings.

### Location

Place a `.schmux/config.json` file inside any workspace directory:

```
~/schmux-workspaces/myproject-001/
├── .schmux/
│   └── config.json    # Workspace-specific config
├── src/
└── ...
```

### Supported Settings

Currently, workspace configs support:

| Setting        | Description                                          |
| -------------- | ---------------------------------------------------- |
| `quick_launch` | Quick launch presets specific to this workspace/repo |

### Quick Launch

Define quick launch presets that only appear for this repository:

```json
{
  "quick_launch": [
    {
      "name": "Run Tests",
      "command": "npm test"
    },
    {
      "name": "Fix Tests",
      "target": "claude",
      "prompt": "Run the test suite and fix any failures"
    }
  ]
}
```

#### Schema

| Field     | Type   | Description                                        |
| --------- | ------ | -------------------------------------------------- |
| `name`    | string | Display name (required)                            |
| `command` | string | Shell command to run directly                      |
| `target`  | string | Run target (claude, codex, model, or user-defined) |
| `prompt`  | string | Prompt to send to the target                       |

#### Rules

- **Shell command**: Set `command` to run a shell command directly
- **AI agent**: Set `target` and `prompt` to spawn an agent with a prompt
- **Either/or**: Use `command` OR `target`+`prompt`, not both

#### Merge Behavior

Workspace quick launch items are merged with global quick launch items:

- Items with the same name: workspace version takes precedence
- Items with unique names: both appear in the spawn dropdown

### Config File Watching

The daemon monitors workspace config files and reloads them automatically:

- **On startup**: All workspace configs are loaded
- **On change**: Config is reloaded when the file's modification time changes
- **Parse errors**: Logged once per change (not spammed on every poll cycle)
- **Success**: Logged when config is successfully loaded after a change

Example log output:

```
[workspace] loaded config from /path/to/workspace/.schmux/config.json
[workspace] warning: failed to parse /path/to/workspace/.schmux/config.json: invalid character...
```

### Use Cases

- **Project-specific prompts**: "Run tests", "Build docs", "Deploy to staging"
- **Team presets**: Check workspace config into git so the whole team gets the same quick launch options
- **Repo-specific targets**: Different repos may use different agents or workflows

---

## Common Modification Patterns

- **To add a new VCS backend**: implement `VCSBackend` in a new `vcs_*.go` file, register in the Manager's `backends` map.
- **To add a new VCS operation**: add to `VCSBackend` in `vcs.go`, implement in `vcs_git.go` and `vcs_sapling.go`.
- **To add a new ensure step**: add to `ensureWorkspace()` in `internal/workspace/ensure/manager.go`. All callers pick it up automatically.
- **To add a new git exclude pattern**: add to `excludePatterns` in `internal/workspace/ensure/manager.go`.
- **To change workspace locking scope**: add `LockWorkspace`/`UnlockWorkspace` calls around the operation.
- **To add a new preview detection pattern**: update the regex in `internal/dashboard/preview_autodetect.go`.
- **To change the canonical bare path convention**: update `NormalizeBarePaths` in `normalize_bare_paths.go` and `CreateLocalRepo` in `manager.go`.

## Gotchas

- **`force` does not bypass recycling.** `DisposeForce` skips safety checks. It does NOT skip recycling. The escape hatch for actual file deletion is `Purge`.
- **`prepare()` runs `git clean -fd`.** When a recyclable workspace is reused, untracked files (build artifacts, `node_modules`) are deleted. Still orders of magnitude less churn than full directory recreation.
- **Recyclable workspaces hold branch reservations.** A recyclable git worktree keeps its branch checked out. If `create()` collides, `purgeRecyclableWithBranch` deletes the conflicting workspace and retries.
- **Workspace locking is separate from dashboard conflict resolution UI state.** The Manager owns `lockedWorkspaces` for concurrency control; the dashboard maintains `linearSyncResolveConflictStates` for frontend display. Independent concerns.
- **`repoLock` in `LinearSyncResolveConflict` is a different lock.** Repo-level mutex for serializing concurrent resolve-conflict calls. Different purpose from workspace lock.
- **Git-watcher behavior is unchanged by locking.** It still fires events and calls `UpdateGitStatus`. The lock makes `UpdateGitStatus` bail early.
- **Preview port config is write-once.** Changing `preview_port_base` or `preview_port_block_size` after allocation invalidates existing preview URLs.
- **Overlay safety check uses `.gitignore`.** If a file is not covered by `.gitignore`, the copy is skipped with a warning.
- **`NormalizeBarePaths` only runs at startup.** Not on live config reload, to avoid racing with active sessions.
- **`RelocateBareRepo` resolves symlinks.** Git writes symlink-resolved absolute paths into worktree `.git` files. The utility must resolve symlinks or the string replacement silently fails.
- **Sapling `IsBranchInUse` always returns false.** Sapling workspaces are independent -- no branch reservation constraint.
