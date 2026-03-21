# VCS Backend Abstraction Design

**Date**: 2026-03-20
**Status**: Draft
**Branch**: feature/sapling

## Problem

Schmux is tightly coupled to git for workspace management. All VCS operations (clone, worktree, fetch, status, diff) are direct `git` CLI calls on the `Manager` struct. To support Sapling workspaces (including EdenFS-backed setups), we need a VCS abstraction that lets the same Manager orchestrate workspaces regardless of the underlying version control system.

## Constraint: No environment-specific tools in open source

Schmux is open source. Sapling (`sl`) is also open source and can be referenced directly. However, environment-specific tools for workspace lifecycle (clone, remove, list mounts) vary across deployments. The sapling backend must use **configurable commands** for lifecycle operations so that different environments can plug in their own tooling without forking Schmux. Observability operations (`sl status`, `sl log`, `sl diff`) use `sl` directly since it's the public CLI.

## Scope

**In scope (Tier 1 + Tier 2):**

- Workspace lifecycle: create, dispose, fetch
- Dashboard observability: dirty status, ahead/behind, changed files, branch info
- Config and state model changes
- Web config editor updates

**Out of scope (Tier 3, future work):**

- Linear sync (rebase from/to main)
- Conflict resolution
- Commit graph visualization
- Push-to-branch
- PR/diff checkout

Tier 3 operations remain git-only. The interface can be extended later without breaking the abstraction.

## Approach: Strategy Object on Manager

The Manager keeps its role as the single orchestrator for workspace lifecycle, state management, overlays, session coordination, and reuse logic. VCS-specific operations delegate to a `VCSBackend` interface selected per-repo.

```
Manager
  ├── backends map[string]VCSBackend
  │     "git"     → &GitBackend{}
  │     "sapling" → &SaplingBackend{}
  │
  ├── backendFor(repoURL) VCSBackend
  │     → looks up config.Repo.VCS, returns matching backend
  │     → defaults to "git" if VCS field is empty
  │
  ├── backendForWorkspace(workspaceID) VCSBackend
  │     → looks up state.Workspace.VCS, returns matching backend
  │
  └── All non-VCS logic stays on Manager:
        overlay management, state persistence, session
        coordination, workspace reuse, telemetry wrapping,
        file watching, poll round deduplication
```

### Why not separate Manager implementations?

Duplicates too much VCS-agnostic orchestration logic (reuse checks, state management, overlay handling, session coordination).

### Why not a thin command wrapper (runGit → runVCS)?

The differences between git and sapling aren't just "swap the binary." The workspace model is fundamentally different (worktree vs. eden mount), arguments differ, output formats differ, and some operations (like `IsBranchInUse`) don't even apply to sapling.

## VCSBackend Interface

Lives in `internal/workspace/vcs.go`.

```go
type VCSBackend interface {
    // --- Tier 1: Lifecycle ---
    // Lifecycle operations may use configurable commands (see SaplingCommands).

    // EnsureRepoBase creates or locates the shared backing store for a repo.
    //   git:     clones bare repo if missing, returns bare clone path
    //   sapling: runs configured CheckRepoBase command; if not found,
    //            runs CreateRepoBase command
    EnsureRepoBase(ctx context.Context, repoIdentifier, basePath string) (string, error)

    // CreateWorkspace creates a new workspace from the repo base.
    //   git:     git worktree add <destPath> <branch>
    //   sapling: runs configured CreateWorkspace command
    CreateWorkspace(ctx context.Context, repoBasePath, branch, destPath string) error

    // RemoveWorkspace deletes a workspace.
    //   git:     git worktree remove --force <path>
    //   sapling: runs configured RemoveWorkspace command
    RemoveWorkspace(ctx context.Context, workspacePath string) error

    // PruneStale cleans up stale workspace references.
    //   git:     git worktree prune
    //   sapling: no-op (workspace provider manages its own state)
    PruneStale(ctx context.Context, repoBasePath string) error

    // Fetch pulls latest changes from the remote.
    //   git:     git fetch (on bare clone — shared across worktrees)
    //   sapling: sl pull (per workspace)
    Fetch(ctx context.Context, path string) error

    // IsBranchInUse checks if a branch is checked out in another workspace.
    //   git:     git worktree list --porcelain (one branch per worktree)
    //   sapling: always false (workspaces are independent)
    IsBranchInUse(ctx context.Context, repoBasePath, branch string) (bool, error)

    // --- Tier 2: Observability ---
    // These use sl directly (open source).

    // GetStatus returns VCS status for a workspace.
    //   git:     git status --porcelain, rev-list, diff --stat
    //   sapling: sl status, sl log
    GetStatus(ctx context.Context, workspacePath string) (VCSStatus, error)

    // GetChangedFiles returns detailed file change info.
    //   git:     git status --porcelain + git diff --numstat
    //   sapling: sl status + sl diff --stat
    GetChangedFiles(ctx context.Context, workspacePath string) ([]ChangedFile, error)

    // GetDefaultBranch returns the default branch name.
    //   git:     git symbolic-ref refs/remotes/origin/HEAD
    //   sapling: "main" (sapling convention, configurable)
    GetDefaultBranch(ctx context.Context, repoBasePath string) (string, error)

    // GetCurrentBranch returns the current branch/bookmark.
    //   git:     git rev-parse --abbrev-ref HEAD
    //   sapling: sl log -r . -T '{bookmarks}'
    GetCurrentBranch(ctx context.Context, workspacePath string) (string, error)

    // --- Tier 2: Query repos ---

    // EnsureQueryRepo ensures a lightweight query repo exists for branch listing.
    EnsureQueryRepo(ctx context.Context, repoIdentifier, path string) error

    // FetchQueryRepo updates the query repo.
    FetchQueryRepo(ctx context.Context, path string) error

    // ListRecentBranches returns recent branches sorted by commit date.
    ListRecentBranches(ctx context.Context, path string, limit int) ([]RecentBranch, error)

    // GetBranchLog returns commit subjects for a branch relative to default.
    GetBranchLog(ctx context.Context, path, branch string, limit int) ([]string, error)
}
```

### VCS-agnostic data types

```go
type VCSStatus struct {
    Dirty            bool
    CurrentBranch    string
    AheadOfDefault   int
    BehindDefault    int
    LinesAdded       int
    LinesRemoved     int
    FilesChanged     int
    SyncedWithRemote bool
}

type ChangedFile struct {
    Path         string
    Status       string // "added", "modified", "deleted", "untracked"
    LinesAdded   int
    LinesRemoved int
}
```

### Design notes

- Every method takes `ctx` for cancellation (matches existing pattern).
- The interface operates on filesystem paths, not workspace IDs or state objects. It's pure VCS operations — Manager wraps them with state tracking and telemetry.
- `repoIdentifier` is a git clone URL or a sapling repo name (`fbsource`). The VCS field on config disambiguates interpretation.

## Git vs. Sapling: Operation Mapping

| Operation        | Git                                         | Sapling                                                   |
| ---------------- | ------------------------------------------- | --------------------------------------------------------- |
| Ensure repo base | `git clone --bare` + configure refspec      | Configurable: `CheckRepoBase` + `CreateRepoBase` commands |
| Create workspace | `git worktree add <path> <branch>`          | Configurable: `CreateWorkspace` command                   |
| Remove workspace | `git worktree remove --force`               | Configurable: `RemoveWorkspace` command                   |
| Prune stale      | `git worktree prune`                        | no-op                                                     |
| Fetch            | `git fetch` (on bare clone, shared)         | `sl pull` (per workspace)                                 |
| Branch in use?   | `git worktree list --porcelain`             | always false (workspaces are independent)                 |
| Status           | `git status --porcelain`                    | `sl status`                                               |
| Ahead/behind     | `git rev-list`                              | `sl log`                                                  |
| Changed files    | `git diff --numstat`                        | `sl diff --stat`                                          |
| Default branch   | `git symbolic-ref refs/remotes/origin/HEAD` | `"main"` (configurable)                                   |
| Current branch   | `git rev-parse --abbrev-ref HEAD`           | `sl log -r . -T '{bookmarks}'`                            |

The split is intentional: **lifecycle** operations (create/remove workspace, manage repo base) are configurable because they depend on the workspace provider (plain sapling clone, EdenFS, or other tooling). **Observability** operations (status, diff, log, pull) use `sl` directly since it's the standard open source CLI regardless of environment.

## Config Changes

### `config.Repo` — add VCS field

```go
type Repo struct {
    Name         string   `json:"name"`
    URL          string   `json:"url"`           // git clone URL or sapling repo identifier
    BarePath     string   `json:"bare_path"`
    VCS          string   `json:"vcs,omitempty"`  // "git" (default) or "sapling"
    OverlayPaths []string `json:"overlay_paths,omitempty"`
    ...
}
```

The `URL` field serves double duty:

- Git: `git@github.com:user/repo.git`
- Sapling: a repo identifier meaningful to the configured commands (e.g., a repo name or path)

The `VCS` field disambiguates interpretation. Defaults to `"git"` for backward compatibility.

`BarePath` is ignored by the sapling backend — the workspace provider manages its own paths. `EnsureRepoBase` discovers the repo base path via the configured check command and stores it in `RepoBase.Path`.

### `config.SaplingCommands` — configurable lifecycle commands

Sapling workspace lifecycle commands vary by environment. Schmux provides sensible defaults using `sl` (open source) but allows overriding for environments with specialized tooling.

```go
type SaplingCommands struct {
    // CreateWorkspace is the command template to create a new workspace.
    // Available variables: {{.RepoIdentifier}}, {{.DestPath}}, {{.Branch}}
    // Default: "sl clone {{.RepoIdentifier}} {{.DestPath}}"
    CreateWorkspace string `json:"create_workspace,omitempty"`

    // RemoveWorkspace is the command template to remove a workspace.
    // Available variables: {{.WorkspacePath}}
    // Default: "rm -rf {{.WorkspacePath}}"
    RemoveWorkspace string `json:"remove_workspace,omitempty"`

    // CheckRepoBase is the command template to check if a repo base exists.
    // Should exit 0 and print the repo base path if it exists, exit non-zero otherwise.
    // Available variables: {{.RepoIdentifier}}
    // Default: "" (no check, always create)
    CheckRepoBase string `json:"check_repo_base,omitempty"`

    // CreateRepoBase is the command template to create the initial repo base.
    // Available variables: {{.RepoIdentifier}}, {{.BasePath}}
    // Default: "sl clone {{.RepoIdentifier}} {{.BasePath}}"
    CreateRepoBase string `json:"create_repo_base,omitempty"`

    // ListWorkspaces is the command template to list existing workspaces.
    // Should output JSON array of paths. Used for scanning/discovery.
    // Available variables: {{.RepoIdentifier}}
    // Default: "" (disabled, rely on state file only)
    ListWorkspaces string `json:"list_workspaces,omitempty"`
}
```

Example configuration for an EdenFS-based environment:

```json
{
  "sapling_commands": {
    "create_workspace": "fbclone {{.RepoIdentifier}} {{.DestPath}}",
    "remove_workspace": "eden rm --yes {{.WorkspacePath}}",
    "check_repo_base": "eden list --json | jq -r 'to_entries[] | select(.key | endswith(\"/{{.RepoIdentifier}}\")) | .key'",
    "create_repo_base": "fbclone {{.RepoIdentifier}} {{.BasePath}}",
    "list_workspaces": "eden list --json | jq '[keys[] | select(contains(\"{{.RepoIdentifier}}\"))]'"
  }
}
```

This keeps all environment-specific tool references in user config, not in source code. The sapling backend in the open source repo only uses `sl` directly (for status, log, diff, pull — the observability tier).

### `config.SourceCodeManagement`

Remains as-is. It controls the git workspace strategy (`git-worktree` vs `git` full clone). Per-repo `VCS` field is orthogonal — you could have git repos using worktrees alongside sapling repos.

### Web config editor

- Repo config form gets a VCS dropdown: `git` | `sapling` (default: `git`)
- When `sapling` is selected, URL field label changes to "Repo Identifier"
- A "Sapling Commands" section in settings allows editing the command templates
- Validation: reject empty command templates for required operations (create/remove)

## State Changes

### `state.Workspace` — add VCS field

```go
type Workspace struct {
    ...
    VCS  string `json:"vcs,omitempty"` // "git" or "sapling", set at creation
    ...
}
```

Stored at creation time so Manager can resolve the backend from state alone, even if config changes.

### `state.WorktreeBase` → `state.RepoBase`

```go
type RepoBase struct {     // renamed from WorktreeBase
    RepoURL string `json:"repo_url"`
    Path    string `json:"path"`
    VCS     string `json:"vcs,omitempty"`
}
```

Requires a state migration: read `worktree_bases` key, write as `repo_bases` with VCS defaulting to `"git"`.

## Manager Integration

### Backend resolution

```
Manager.backendFor(repoURL string) VCSBackend:
    repo := findRepoByURL(repoURL)
    vcs := repo.VCS  // defaults to "git" if empty
    return m.backends[vcs]

Manager.backendForWorkspace(workspaceID string) VCSBackend:
    ws := m.state.GetWorkspace(workspaceID)
    return m.backends[ws.VCS]
```

### GetOrCreate flow

```
GetOrCreate(ctx, repoURL, branch):
    vcs := m.backendFor(repoURL)

    // Try reuse (VCS-agnostic)
    if ws := findReusableWorkspace(repoURL, branch); ws != nil:
        return ws

    // Create
    basePath := vcs.EnsureRepoBase(ctx, repoURL, ...)
    vcs.Fetch(ctx, basePath)

    if vcs needs unique branch (git only):
        branch = vcs.ensureUniqueBranch(...)

    vcs.CreateWorkspace(ctx, basePath, branch, destPath)

    // VCS-agnostic: overlay, state, watchers
    applyOverlay(destPath)
    state.AddWorkspace(ws)  // ws.VCS set from config
    return ws
```

### dispose flow

```
dispose(ctx, workspaceID):
    vcs := m.backendForWorkspace(workspaceID)

    // Safety checks (VCS-agnostic)
    checkActiveSessions(workspaceID)

    // VCS-specific safety
    status := vcs.GetStatus(ctx, ws.Path)
    if status.Dirty || status.AheadOfDefault > 0:
        return ErrUnsafeDispose

    // Remove
    vcs.RemoveWorkspace(ctx, ws.Path)
    vcs.PruneStale(ctx, basePath)

    // VCS-agnostic cleanup
    state.RemoveWorkspace(workspaceID)
```

### Poll round (updateAllVCSStatus)

```
updateAllVCSStatus(ctx):
    round := newPollRound()
    for _, ws := range allWorkspaces:
        vcs := m.backendForWorkspace(ws.ID)

        // Deduplicate fetch per repo base (same optimization as today)
        if !round.hasFetched(ws.repoBase):
            vcs.Fetch(ctx, ws.repoBase)
            round.markFetched(ws.repoBase)

        status := vcs.GetStatus(ctx, ws.Path)
        mapStatusToWorkspace(ws, status)
```

### Telemetry

Telemetry recording stays in Manager. The current `runGit()` records command name, args, duration, exit code. Options:

1. Backends accept a `runCmd` function that Manager provides (wrapping exec with telemetry)
2. Backends return timing info and Manager records it

Option 1 is simpler — backends call `m.runCmd("git", args...)` or `m.runCmd("sl", args...)` and Manager handles the rest. This is essentially what `runGit` does today, generalized to any binary.

## File Organization

### New files

```
internal/workspace/
    vcs.go              — VCSBackend interface, VCSStatus, ChangedFile
    vcs_git.go          — GitBackend (extracted from git.go + worktree.go)
    vcs_git_test.go
    vcs_sapling.go      — SaplingBackend
    vcs_sapling_test.go
```

### Existing file changes

```
git.go           — shrinks: VCS ops move to vcs_git.go.
                   ValidateBranchName, isWorktree, resolveWorktreeBase stay.
worktree.go      — contents move to vcs_git.go. File deleted.
run_git.go       — generalized to runCmd(binary, args...).
                   Becomes the shared command runner for all backends.
git_poll_round.go — stays on Manager, calls backend.Fetch() instead of
                    runGit("fetch") directly. Renamed to vcs_poll_round.go.
git_watcher.go   — stays on Manager unchanged. File watching is filesystem-
                    level, not VCS-specific.
origin_queries.go — query repo methods move to respective backends.
manager.go       — gains backends map, backendFor(), backendForWorkspace().
                   GetOrCreate/dispose/updateGitStatus refactored to
                   delegate VCS ops to backend.
interfaces.go    — WorkspaceManager stays as-is (it's the external contract).
                   Method names like UpdateGitStatus renamed to
                   UpdateVCSStatus.
```

## Implementation Phases

### Phase 1: Extract GitBackend (refactor, no new behavior)

- Define `VCSBackend` interface in `vcs.go`
- Create `GitBackend` struct in `vcs_git.go`
- Move git operations from Manager methods into GitBackend
- Manager gets `backends` map with single `"git"` entry
- Generalize `runGit()` to `runCmd()` accepting a binary name
- Rename `WorktreeBase` → `RepoBase` with state migration
- Rename `UpdateGitStatus` → `UpdateVCSStatus` on the interface
- All existing tests must pass — behavior is unchanged

This is the riskiest phase (many method moves) but purely mechanical.

### Phase 2: Config + state plumbing

- Add `VCS` field to `config.Repo` and `state.Workspace`
- Add `VCS` field to `state.RepoBase`
- Update web config editor: VCS dropdown on repo form
- Validation: reject unknown VCS values
- `backendFor()` / `backendForWorkspace()` resolution logic
- Still git-only in practice — just threading the field through

### Phase 3: SaplingBackend implementation

- Implement each `VCSBackend` method for sapling:
  - `EnsureRepoBase`: execute configured `CheckRepoBase` command; if not found, run `CreateRepoBase`
  - `CreateWorkspace`: execute configured `CreateWorkspace` command template
  - `RemoveWorkspace`: execute configured `RemoveWorkspace` command template
  - `Fetch`: `sl pull --cwd <path>` (uses sl directly)
  - `GetStatus`: parse `sl status` and `sl log` output (uses sl directly)
  - `GetChangedFiles`: parse `sl status` + `sl diff --stat` (uses sl directly)
  - `GetDefaultBranch`: configurable, default `"main"`
  - `GetCurrentBranch`: `sl log -r . -T '{bookmarks}'` (uses sl directly)
- Command template rendering with Go `text/template`
- Register `"sapling"` in the backends map
- Unit tests with mocked command execution
- Add `SaplingCommands` config section with defaults and web editor UI

### Phase 4: Integration validation

- Test on a machine with sapling installed
- Validate: daemon creates workspace via configured commands, spawns tmux session, agent can `sl status` and read/write files
- Test with pre-existing repo base (discover-first path via `CheckRepoBase`)
- Test on fresh machine (creates repo base via `CreateRepoBase`)
- Test disposal via configured `RemoveWorkspace` command
- Validate mount namespace: tmux sessions spawned by daemon can access workspaces created by environment-specific tools

## Risks and Mitigations

### Mount namespace / sandbox isolation

Some agent sandboxes use private mount namespaces. FUSE mounts created by environment-specific tools from inside the sandbox may be invisible. **Mitigation**: Schmux daemon runs outside the sandbox; validate in Phase 4 that tmux sessions spawned by the daemon can access workspaces created by the configured commands.

### State migration (WorktreeBase → RepoBase)

One-way door for existing users. **Mitigation**: Schmux already has state migration machinery. Add a migration that reads the old field name, writes the new one, defaults VCS to `"git"`.

### Sapling command output parsing

`sl status`, `sl log` output formats may differ from git. **Mitigation**: Write parsing logic against actual sapling output, not assumptions. Test with real `sl`.

### Command template security

Configured commands are executed via shell. **Mitigation**: Only the daemon (controlled by the user who owns the config file) executes these commands. Template variables are validated/escaped before interpolation. The web config editor validates command syntax.

### Sapling availability

Not all machines have `sl`. **Mitigation**: `SaplingBackend` checks for `sl` in PATH at initialization. Returns a clear error if unavailable. Config validation warns if VCS is `"sapling"` but `sl` isn't detected. Lifecycle commands are validated separately (they may reference environment-specific tools the user has installed).

## Decisions Log

| Decision              | Choice                                     | Rationale                                                           |
| --------------------- | ------------------------------------------ | ------------------------------------------------------------------- |
| Abstraction pattern   | Strategy object on Manager                 | Avoids duplicating orchestration logic; cleaner than conditionals   |
| Scope                 | Tier 1+2 only                              | Tier 3 is complex and two-way-door; can extend later                |
| VCS selection         | Per-repo config field                      | Supports mixed git + sapling from one instance                      |
| Repo base for sapling | Discover-first, create if needed           | Configured CheckRepoBase command handles detection                  |
| Naming                | RepoBase (not BackingStore)                | Concise, close to existing WorktreeBase, VCS-neutral                |
| BarePath for sapling  | Ignored; workspace provider manages paths  | Don't fight the environment's conventions                           |
| Telemetry             | Shared runCmd in Manager                   | Backends use Manager-provided command runner; consistent telemetry  |
| Open source boundary  | `sl` in code, lifecycle commands in config | Sapling is open source; environment-specific tools are configurable |
