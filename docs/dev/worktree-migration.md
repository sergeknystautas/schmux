# Spec: Git Worktree Migration

> **Note:** As of the `source_code_management` config option, worktrees are now optional. Users can choose between "git-worktree" (default, efficient) or "git" (full clones, allows same-branch workspaces). Configure in Settings > Workspace > Source Code Management.

## Overview

Replace full `git clone` per workspace with `git worktree` to reduce disk usage and speed up workspace creation.

## Motivation

Currently, each workspace is a full clone:

- **Disk usage**: Each clone duplicates `.git/objects` (~50-200MB per repo)
- **Creation time**: Network clone for every workspace
- **Fetch overhead**: Each workspace fetches independently

With worktrees:

- **Disk usage**: One worktree base + lightweight worktrees (working files only)
- **Creation time**: Instant local operation
- **Fetch efficiency**: One fetch updates all worktrees

## Directory Structure

### Current

```
~/dev/schmux-workspaces/
├── myrepo-001/           # Full clone (~100MB .git)
│   └── .git/
├── myrepo-002/           # Another full clone (~100MB .git)
│   └── .git/
└── myrepo-003/
    └── .git/
```

### New

```
~/.schmux/repos/                    # Worktree bases (bare clones)
├── myrepo.git/                     # Shared objects store
│   └── worktrees/                  # Git-managed worktree metadata
│       ├── myrepo-001/
│       └── myrepo-002/
└── another-repo.git/

~/dev/schmux-workspaces/            # Unchanged path
├── myrepo-001/                     # Worktree (~0 overhead)
│   └── .git                        # FILE pointing to worktree base
├── myrepo-002/
│   └── .git                        # FILE pointing to worktree base
└── another-repo-001/
```

## State Changes

### `internal/state/state.go`

Add new struct for worktree bases:

```go
// WorktreeBase tracks a bare clone that hosts worktrees
type WorktreeBase struct {
    RepoURL string `json:"repo_url"`  // e.g., "git@github.com:user/repo.git"
    Path    string `json:"path"`      // e.g., "~/.schmux/repos/myrepo.git"
}
```

Add to `State`:

```go
type State struct {
    Workspaces   []Workspace `json:"workspaces"`
    Sessions     []Session   `json:"sessions"`
    WorktreeBases []WorktreeBase  `json:"base_repos"`    // NEW
    // ...
}
```

Add accessors:

```go
func (s *State) GetBaseRepos() []WorktreeBase
func (s *State) AddWorktreeBase(br BaseRepo) error
func (s *State) GetWorktreeBaseByURL(repoURL string) (BaseRepo, bool)
```

### `Workspace` struct

No changes needed—`Path` already points to the working directory.

## Config Changes

### `internal/config/config.go`

Add worktree bases path (optional, defaults to `~/.schmux/repos`):

```go
type Config struct {
    // ...
    BaseReposPath string `json:"base_repos_path,omitempty"`  // NEW
}

func (c *Config) GetWorktreeBasePath() string {
    if c.BaseReposPath == "" {
        homeDir, _ := os.UserHomeDir()
        return filepath.Join(homeDir, ".schmux", "repos")
    }
    return c.BaseReposPath
}
```

## Workspace Manager Changes

### `internal/workspace/manager.go`

#### New: `ensureWorktreeBase()`

Creates or returns existing bare clone for a repo URL:

```go
func (m *Manager) ensureWorktreeBase(ctx context.Context, repoURL string) (string, error) {
    // Check if worktree base already exists in state
    if br, found := m.state.GetWorktreeBaseByURL(repoURL); found {
        // Verify it still exists on disk
        if _, err := os.Stat(br.Path); err == nil {
            return br.Path, nil
        }
        // Worktree base missing on disk, will recreate below
    }

    // Derive worktree base path from repo name
    repoName := extractRepoName(repoURL)  // "myrepo" from "git@github.com:user/myrepo.git"
    baseRepoPath := filepath.Join(m.config.GetWorktreeBasePath(), repoName+".git")

    // Ensure worktree bases directory exists
    if err := os.MkdirAll(m.config.GetWorktreeBasePath(), 0755); err != nil {
        return "", fmt.Errorf("failed to create worktree bases directory: %w", err)
    }

    // Clone as bare repo
    if err := m.cloneBareRepo(ctx, repoURL, baseRepoPath); err != nil {
        return "", err
    }

    // Track in state
    m.state.AddWorktreeBase(state.BaseRepo{RepoURL: repoURL, Path: baseRepoPath})
    m.state.Save()

    return baseRepoPath, nil
}
```

#### New: `cloneBareRepo()`

```go
func (m *Manager) cloneBareRepo(ctx context.Context, url, path string) error {
    m.logger.Printf("cloning bare repository: url=%s path=%s", url, path)
    args := []string{"clone", "--bare", url, path}
    cmd := exec.CommandContext(ctx, "git", args...)

    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("git clone --bare failed: %w: %s", err, string(output))
    }

    m.logger.Printf("bare repository cloned: path=%s", path)
    return nil
}
```

#### New: `addWorktree()`

```go
func (m *Manager) addWorktree(ctx context.Context, baseRepoPath, workspacePath, branch string) error {
    m.logger.Printf("adding worktree: base=%s path=%s branch=%s", baseRepoPath, workspacePath, branch)

    // Check if remote branch exists
    remoteBranch := "origin/" + branch
    checkCmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/remotes/"+remoteBranch)
    checkCmd.Dir = baseRepoPath
    remoteBranchExists := checkCmd.Run() == nil

    var args []string
    if remoteBranchExists {
        // Track existing remote branch
        args = []string{"worktree", "add", "--track", "-b", branch, workspacePath, remoteBranch}
    } else {
        // Create new local branch
        args = []string{"worktree", "add", "-b", branch, workspacePath}
    }

    cmd := exec.CommandContext(ctx, "git", args...)
    cmd.Dir = baseRepoPath

    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("git worktree add failed: %w: %s", err, string(output))
    }

    m.logger.Printf("worktree added: path=%s", workspacePath)
    return nil
}
```

#### Branch conflicts

Git worktrees cannot share a branch. When a requested branch is already checked out in another worktree, schmux appends a 3-character suffix to make a unique branch name (e.g., `feature/foo-a1b`) and creates it from the requested branch tip (prefer `origin/<branch>`, fall back to local). The workspace state stores the actual branch name used.

#### New: `removeWorktree()`

```go
func (m *Manager) removeWorktree(ctx context.Context, baseRepoPath, workspacePath string) error {
    m.logger.Printf("removing worktree: base=%s path=%s", baseRepoPath, workspacePath)

    args := []string{"worktree", "remove", "--force", workspacePath}
    cmd := exec.CommandContext(ctx, "git", args...)
    cmd.Dir = baseRepoPath

    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("git worktree remove failed: %w: %s", err, string(output))
    }

    m.logger.Printf("worktree removed: path=%s", workspacePath)
    return nil
}
```

#### Update: `create()`

Replace clone with worktree add:

```go
func (m *Manager) create(ctx context.Context, repoURL, branch string) (*state.Workspace, error) {
    // Find repo config by URL (unchanged)
    repoConfig, found := m.findRepoByURL(repoURL)
    if !found {
        return nil, fmt.Errorf("repo URL not found in config: %s", repoURL)
    }

    // Find the next available workspace number (unchanged)
    workspaces := m.getWorkspacesForRepo(repoURL)
    nextNum := findNextWorkspaceNumber(workspaces)
    workspaceID := fmt.Sprintf("%s-"+workspaceNumberFormat, repoConfig.Name, nextNum)
    workspacePath := filepath.Join(m.config.GetWorkspacePath(), workspaceID)

    // NEW: Ensure worktree base exists (replaces cloneRepo)
    baseRepoPath, err := m.ensureWorktreeBase(ctx, repoURL)
    if err != nil {
        return nil, fmt.Errorf("failed to ensure worktree base: %w", err)
    }

    // Fetch latest before creating worktree
    m.gitFetch(ctx, baseRepoPath)

    // Clean up if creation fails
    cleanupNeeded := true
    defer func() {
        if cleanupNeeded {
            m.logger.Printf("cleaning up failed workspace: %s", workspacePath)
            // Try worktree remove first, fall back to rm -rf
            if err := m.removeWorktree(ctx, baseRepoPath, workspacePath); err != nil {
                os.RemoveAll(workspacePath)
            }
        }
    }()

    // NEW: Add worktree (replaces cloneRepo)
    if err := m.addWorktree(ctx, baseRepoPath, workspacePath, branch); err != nil {
        return nil, fmt.Errorf("failed to add worktree: %w", err)
    }

    // Copy overlay files (unchanged)
    if err := m.copyOverlayFiles(ctx, repoConfig.Name, workspacePath); err != nil {
        m.logger.Printf("warning: failed to copy overlay files: %v", err)
    }

    // Create workspace state (unchanged)
    w := state.Workspace{
        ID:     workspaceID,
        Repo:   repoURL,
        Branch: branch,
        Path:   workspacePath,
    }

    if err := m.state.AddWorkspace(w); err != nil {
        return nil, fmt.Errorf("failed to add workspace to state: %w", err)
    }
    if err := m.state.Save(); err != nil {
        return nil, fmt.Errorf("failed to save state: %w", err)
    }

    cleanupNeeded = false
    return &w, nil
}
```

#### Update: `Dispose()`

Use worktree remove instead of rm -rf:

```go
func (m *Manager) Dispose(workspaceID string) error {
    w, found := m.state.GetWorkspace(workspaceID)
    if !found {
        return fmt.Errorf("workspace not found: %s", workspaceID)
    }

    m.logger.Printf("disposing workspace: id=%s path=%s", workspaceID, w.Path)

    // Check if workspace has active sessions (unchanged)
    if m.hasActiveSessions(workspaceID) {
        return fmt.Errorf("workspace has active sessions: %s", workspaceID)
    }

    // Check git safety (unchanged)
    ctx := context.Background()
    gitStatus, err := m.checkGitSafety(ctx, workspaceID)
    if err != nil {
        return fmt.Errorf("failed to check git status: %w", err)
    }
    if !gitStatus.Safe {
        return fmt.Errorf("workspace has unsaved changes: %s", gitStatus.Reason)
    }

    // NEW: Determine if this is a worktree or legacy clone
    if isWorktree(w.Path) {
        // Find worktree base and remove worktree
        baseRepoPath, err := m.findWorktreeBaseForWorkspace(w)
        if err != nil {
            m.logger.Printf("warning: could not find worktree base, falling back to rm: %v", err)
            if err := os.RemoveAll(w.Path); err != nil {
                return fmt.Errorf("failed to delete workspace directory: %w", err)
            }
        } else {
            if err := m.removeWorktree(ctx, baseRepoPath, w.Path); err != nil {
                return fmt.Errorf("failed to remove worktree: %w", err)
            }
        }
    } else {
        // Legacy full clone - delete directory
        if err := os.RemoveAll(w.Path); err != nil {
            return fmt.Errorf("failed to delete workspace directory: %w", err)
        }
    }

    // Remove from state (unchanged)
    if err := m.state.RemoveWorkspace(workspaceID); err != nil {
        return fmt.Errorf("failed to remove workspace from state: %w", err)
    }
    if err := m.state.Save(); err != nil {
        return fmt.Errorf("failed to save state: %w", err)
    }

    m.logger.Printf("workspace disposed: id=%s", workspaceID)
    return nil
}
```

#### Update: `gitFetch()` for worktrees

When fetching inside a worktree, resolve to the worktree base:

```go
func (m *Manager) gitFetch(ctx context.Context, dir string) error {
    // Resolve to worktree base if this is a worktree
    fetchDir := dir
    if isWorktree(dir) {
        if baseRepo, err := resolveWorktreeBaseFromWorktree(dir); err == nil {
            fetchDir = baseRepo
        }
    }

    args := []string{"fetch"}
    cmd := exec.CommandContext(ctx, "git", args...)
    cmd.Dir = fetchDir

    if output, err := cmd.CombinedOutput(); err != nil {
        return fmt.Errorf("git fetch failed: %w: %s", err, string(output))
    }

    return nil
}
```

## New Helper Functions

```go
// extractRepoName extracts "myrepo" from various URL formats
func extractRepoName(repoURL string) string {
    // Handle: git@github.com:user/myrepo.git
    // Handle: https://github.com/user/myrepo.git
    // Handle: https://github.com/user/myrepo

    // Strip .git suffix
    name := strings.TrimSuffix(repoURL, ".git")

    // Get last path component
    if idx := strings.LastIndex(name, "/"); idx >= 0 {
        name = name[idx+1:]
    }
    if idx := strings.LastIndex(name, ":"); idx >= 0 {
        name = name[idx+1:]
    }

    return name
}

// isWorktree checks if a path is a worktree (has .git file) vs full clone (.git dir)
func isWorktree(path string) bool {
    gitPath := filepath.Join(path, ".git")
    info, err := os.Stat(gitPath)
    if err != nil {
        return false
    }
    return !info.IsDir()  // File = worktree, Dir = full clone
}

// resolveWorktreeBaseFromWorktree reads the .git file to find the worktree base path
func resolveWorktreeBaseFromWorktree(worktreePath string) (string, error) {
    gitFilePath := filepath.Join(worktreePath, ".git")
    content, err := os.ReadFile(gitFilePath)
    if err != nil {
        return "", fmt.Errorf("failed to read .git file: %w", err)
    }

    // Format: "gitdir: /path/to/base.git/worktrees/workspace-name"
    line := strings.TrimSpace(string(content))
    if !strings.HasPrefix(line, "gitdir: ") {
        return "", fmt.Errorf("invalid .git file format")
    }

    gitdir := strings.TrimPrefix(line, "gitdir: ")

    // Strip "/worktrees/xxx" to get worktree base path
    if idx := strings.Index(gitdir, "/worktrees/"); idx >= 0 {
        return gitdir[:idx], nil
    }

    return "", fmt.Errorf("could not parse worktree base from gitdir: %s", gitdir)
}

// findWorktreeBaseForWorkspace finds the worktree base for a workspace
func (m *Manager) findWorktreeBaseForWorkspace(w state.Workspace) (string, error) {
    // First try: resolve from .git file (works for worktrees)
    if isWorktree(w.Path) {
        return resolveWorktreeBaseFromWorktree(w.Path)
    }

    // Not a worktree
    return "", fmt.Errorf("workspace is not a worktree: %s", w.ID)
}
```

## `initLocalRepo()` — No Changes

Local repos (`local:{name}`) continue to use `git init` directly. They don't have a remote to share, so worktrees provide no benefit.

## Migration Strategy

### Existing workspaces

**Approach: Leave in place**

- Existing full clones continue to work unchanged
- `Dispose()` detects worktree vs full clone and acts accordingly
- New workspaces for same repo use worktrees
- Old workspaces age out naturally as users dispose them

This is the safest approach—no data migration required.

### State migration

In `state.go`, handle missing `BaseRepos` field:

```go
func Load(path string) (*State, error) {
    // ... existing load logic ...

    // Initialize BaseRepos if nil (existing state files)
    if st.BaseRepos == nil {
        st.BaseRepos = []WorktreeBase{}
    }

    return &st, nil
}
```

## Edge Cases

### Same branch, multiple workspaces

Git worktrees prevent two worktrees from using the same branch simultaneously.

**Solution**: Block at API level with clear error message.

```go
func (m *Manager) addWorktree(ctx context.Context, baseRepoPath, workspacePath, branch string) error {
    // Check if branch is already checked out in another worktree
    listCmd := exec.CommandContext(ctx, "git", "worktree", "list", "--porcelain")
    listCmd.Dir = baseRepoPath
    output, _ := listCmd.CombinedOutput()

    if strings.Contains(string(output), "branch refs/heads/"+branch) {
        return fmt.Errorf("branch %q is already checked out in another workspace", branch)
    }

    // ... proceed with add ...
}
```

Most use cases want different branches anyway. If same-branch is needed, users can create a new branch.

### Detached HEAD (checkout by SHA or tag)

Worktrees support detached HEAD:

```go
// git worktree add --detach <path> <commit>
args := []string{"worktree", "add", "--detach", workspacePath, commitOrTag}
```

This is a future enhancement—current code assumes branch names.

### Orphaned worktrees

If workspace directory is deleted externally (not via `Dispose()`):

```go
// Prune orphaned worktree metadata
func (m *Manager) pruneWorktrees(ctx context.Context, baseRepoPath string) error {
    cmd := exec.CommandContext(ctx, "git", "worktree", "prune")
    cmd.Dir = baseRepoPath
    return cmd.Run()
}
```

Call on daemon startup for each worktree base.

### Worktree base deletion

If `~/.schmux/repos/myrepo.git` is deleted while worktrees exist:

- Worktrees become invalid (`.git` file points to missing path)
- Detection: Check if resolved worktree base path exists
- Recovery: Log error, allow re-creating worktree base on next workspace create

## Files Changed

| File                            | Change                                                |
| ------------------------------- | ----------------------------------------------------- |
| `internal/state/state.go`       | Add `BaseRepo` struct, `BaseRepos` field, accessors   |
| `internal/config/config.go`     | Add `BaseReposPath` field and `GetWorktreeBasePath()` |
| `internal/workspace/manager.go` | Major changes: worktree operations, helper functions  |

## Files Unchanged

| File                             | Reason                                        |
| -------------------------------- | --------------------------------------------- |
| `internal/session/manager.go`    | Sessions don't care about git internals       |
| `internal/dashboard/handlers.go` | API unchanged                                 |
| `assets/dashboard/**`            | Workspace model unchanged from UI perspective |
| `cmd/schmux/**`                  | No new CLI commands needed                    |

## Testing

### Unit tests

- `extractRepoName()` with various URL formats
- `isWorktree()` detection
- `resolveWorktreeBaseFromWorktree()` parsing

### Integration tests

Add to existing workspace tests:

```go
func TestWorktreeCreate(t *testing.T) {
    // Create workspace → should create worktree base + worktree
    // Verify .git is a file, not directory
    // Verify worktree base exists at expected path
}

func TestWorktreeDispose(t *testing.T) {
    // Dispose worktree workspace
    // Verify worktree removed but worktree base remains
}

func TestLegacyCloneDispose(t *testing.T) {
    // Create legacy clone (manually or via test setup)
    // Dispose should use rm -rf, not worktree remove
}

func TestSameBranchBlocked(t *testing.T) {
    // Create workspace on branch X
    // Try to create another on branch X → expect error
}
```

### E2E tests

Full workflow with worktrees via Docker:

1. Start daemon
2. Spawn session (creates worktree base + worktree)
3. Verify git operations work in worktree
4. Dispose session
5. Verify cleanup

## Rollback Plan

If issues arise after deployment:

1. **Feature flag**: Add `use_worktrees: false` to config to disable
2. **Fallback**: When disabled, `create()` uses `cloneRepo()` as before
3. **Mixed state**: Existing worktrees continue to work (git is robust)
4. **Full rollback**: Remove worktree bases, existing workspaces still work

## Future Enhancements

1. **Worktree base garbage collection**: Remove worktree bases with no remaining worktrees
2. **Shared fetch daemon**: Background job to fetch all worktree bases periodically
3. **Detached HEAD support**: Checkout by SHA or tag
4. **`--force` option**: Allow same branch in multiple workspaces (power user)
