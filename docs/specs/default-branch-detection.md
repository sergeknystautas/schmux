# Default Branch Detection Spec

**Goal**
Eliminate hardcoded `"main"` branch assumptions throughout the codebase. The system should detect and use the actual default branch (main, master, develop, etc.) for each repository.

**Key Behaviors**
- Default branch is detected once per repository using `git symbolic-ref refs/remotes/origin/HEAD`
- Detection happens in **query repos** (`~/.schmux/query`) on daemon startup
- Detected value is cached in-memory in the Manager (not persisted to state.json)
- In-memory cache is refreshed when origin query repos are fetched
- Exposed via API (`/api/config`) for frontend consumption
- Used everywhere `origin/main` is currently hardcoded
- **Fails fast** if detection fails - no silent fallback to "main" after trying common defaults
- **Omitted** from API response for repos where detection hasn't completed yet

---

## Problem Statement

Current code has many hardcoded references to `"main"` as the default branch:

| File | Line(s) | Issue |
|------|---------|-------|
| `internal/workspace/worktree.go` | 116 | Creates new branches from `origin/main` - breaks on non-main repos |
| `internal/workspace/git.go` | 205-210 | Compares status against `origin/main` - incorrect ahead/behind counts |
| `internal/workspace/origin_queries.go` | 214 | Fallback when git detection fails |
| `cmd/schmux/spawn.go` | 46-47 | CLI default flag value |
| `assets/dashboard/src/routes/SpawnPage.tsx` | 587, 596, 599-600, 624 | Frontend fallbacks |

When a repository uses `master`, `develop`, or another name as its default branch:
1. New worktree creation fails (tries to create from non-existent `origin/main`)
2. Git status shows incorrect ahead/behind counts
3. Frontend spawn wizard defaults to wrong branch

---

## Two Types of Repos

The codebase has TWO separate repo systems - this is a common source of confusion:

| Property | Worktree Bases | Query Repos |
|----------|---------------|-------------|
| Path method | `GetWorktreeBasePath()` | `GetQueryRepoPath()` |
| Default path | `~/.schmux/repos` | `~/.schmux/query` |
| Configurable | Yes (`base_repos_path` in config) | No (hardcoded) |
| Purpose | Host worktrees | Branch queries (recent branches, commit logs) |
| Created by | `ensureWorktreeBase()` | `EnsureOriginQueries()` |
| Tracked in state | Yes (`WorktreeBase` struct, `base_repos` field) | No |
| Created when | First workspace spawn needed | Daemon startup |
| Lifetime | Long-lived, deleted explicitly | Refreshed/updated periodically |

**Why two systems?**
- Worktree bases host worktrees and must be long-lived
- Query repos are for fast branch lookups without affecting worktrees
- They're separate clones to avoid conflicts

**For this spec:**
- **Query repos** (`~/.schmux/query`) own the default branch detection - they're for querying metadata
- **Worktree bases** (`~/.schmux/repos`) use the cached value from the Manager for worktree operations
- No state.json storage - only in-memory cache (refreshed on daemon startup and periodic fetch)

---

## Data Model

### In-Memory Cache Only (no state.json changes)

No persistent storage changes needed. Default branch is:
- Detected from origin query repos (`~/.schmux/bare`) on daemon startup
- Cached in-memory in the Manager (`map[repoURL]defaultBranch`)
- Refreshed when origin query repos are fetched
- Available via API for frontend consumption

### Go Types
```go
// internal/workspace/manager.go
type Manager struct {
    // ... existing fields
    defaultBranchCache   map[string]string  // repoURL -> defaultBranch (NEW)
    defaultBranchCacheMu sync.RWMutex
}
```

No changes needed to `state.BaseRepo` or `state.json`.

---

## Backend Changes

### Detection Function (`internal/workspace/origin_queries.go`)

Enhance existing `getDefaultBranch` to cache results and improve error handling:

```go
// getDefaultBranch detects the default branch for a bare repo (origin query repo).
func (m *Manager) getDefaultBranch(ctx context.Context, queryRepoPath string) string {
    cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "refs/remotes/origin/HEAD")
    cmd.Dir = queryRepoPath
    output, err := cmd.Output()
    if err == nil {
        // Output is like "refs/remotes/origin/main"
        ref := strings.TrimSpace(string(output))
        return strings.TrimPrefix(ref, "refs/remotes/origin/")
    }

    // Fallback: try common defaults (main, master, develop)
    for _, candidate := range []string{"main", "master", "develop"} {
        if m.branchExists(ctx, queryRepoPath, candidate) {
            return candidate
        }
    }

    return "" // Signal failure - caller should handle error
}

// branchExists checks if a branch exists in the bare repo.
func (m *Manager) branchExists(ctx context.Context, queryRepoPath, branch string) bool {
    ref := "refs/remotes/origin/" + branch
    cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", ref)
    cmd.Dir = queryRepoPath
    return cmd.Run() == nil
}
```

### In-Memory Cache (`internal/workspace/manager.go`)

```go
type Manager struct {
    // ... existing fields
    defaultBranchCache   map[string]string  // repoURL -> defaultBranch (NEW)
    defaultBranchCacheMu sync.RWMutex
}

// GetDefaultBranch returns the cached default branch for a repo URL.
// Returns error if the origin query repo hasn't been created yet or detection failed.
func (m *Manager) GetDefaultBranch(ctx context.Context, repoURL string) (string, error) {
    // Check in-memory cache first
    m.defaultBranchCacheMu.RLock()
    if branch, ok := m.defaultBranchCache[repoURL]; ok {
        m.defaultBranchCacheMu.RUnlock()
        return branch, nil
    }
    m.defaultBranchCacheMu.RUnlock()

    // Not in cache - try to detect from origin query repo
    queryRepoDir := m.config.GetQueryRepoPath()
    if queryRepoDir == "" {
        return "", fmt.Errorf("bare repos path not configured")
    }

    repoName := extractRepoName(repoURL)
    queryRepoPath := filepath.Join(queryRepoDir, repoName+".git")

    // Check if origin query repo exists
    if _, err := os.Stat(queryRepoPath); os.IsNotExist(err) {
        return "", fmt.Errorf("origin query repo not found for %s (has daemon started?)", repoURL)
    }

    // Detect from git
    branch := m.getDefaultBranch(ctx, queryRepoPath)
    if branch == "" {
        return "", fmt.Errorf("failed to detect default branch for %s", repoURL)
    }

    // Cache the result
    m.setDefaultBranch(repoURL, branch)
    return branch, nil
}

// setDefaultBranch caches the default branch in memory.
func (m *Manager) setDefaultBranch(repoURL, branch string) {
    m.defaultBranchCacheMu.Lock()
    defer m.defaultBranchCacheMu.Unlock()
    if m.defaultBranchCache == nil {
        m.defaultBranchCache = make(map[string]string)
    }
    m.defaultBranchCache[repoURL] = branch
}
```

### Update: EnsureOriginQueries (`internal/workspace/origin_queries.go`)

Populate the cache when origin query repos are created or fetched:

```go
// EnsureOriginQueries ensures origin query repos exist for all configured repos.
func (m *Manager) EnsureOriginQueries(ctx context.Context) error {
    // ... existing directory and repo setup code ...

    for _, repo := range m.config.GetRepos() {
        repoName := extractRepoName(repo.URL)
        queryRepoPath := filepath.Join(queryRepoDir, repoName+".git")

        // Skip if already exists
        if _, err := os.Stat(queryRepoPath); err == nil {
            continue
        }

        // Clone as bare repo for origin queries
        if err := m.cloneOriginQueryRepo(ctx, repo.URL, queryRepoPath); err != nil {
            continue  // existing error handling
        }

        // NEW: Detect and cache default branch after cloning
        defaultBranch := m.getDefaultBranch(ctx, queryRepoPath)
        if defaultBranch != "" {
            m.setDefaultBranch(repo.URL, defaultBranch)
        }
    }

    return nil
}

// FetchOriginQueries fetches updates for all origin query repos.
func (m *Manager) FetchOriginQueries(ctx context.Context) {
    // ... existing fetch code ...

    for _, repo := range m.config.GetRepos() {
        repoName := extractRepoName(repo.URL)
        queryRepoPath := filepath.Join(queryRepoDir, repoName+".git")

        // ... fetch existing repo ...

        // NEW: Refresh default branch cache after fetch
        defaultBranch := m.getDefaultBranch(ctx, queryRepoPath)
        if defaultBranch != "" {
            m.setDefaultBranch(repo.URL, defaultBranch)
        }
    }
}
```

### Fix: addWorktree (`internal/workspace/worktree.go`)

Line 115-116 - change from hardcoded `origin/main`:

```go
// Before (line 116):
args = []string{"worktree", "add", "-b", branch, workspacePath, "origin/main"}

// After:
defaultBranch, err := m.GetDefaultBranch(ctx, repoURL)
if err != nil {
    return "", fmt.Errorf("failed to get default branch: %w", err)
}
args = []string{"worktree", "add", "-b", branch, workspacePath, "origin/"+defaultBranch}
```

Note: `addWorktree` has access to `repoURL` via the `create` function that calls it. Need to pass `repoURL` as a parameter.

### Fix: gitStatus (`internal/workspace/git.go`)

Lines 202-210 - use cached default branch from workspace's repo URL:

```go
// Before (line 205):
revListCmd := exec.CommandContext(ctx, "git", "rev-list", "--left-right", "--count", "HEAD...origin/main")

// After:
// Use cached default branch from Manager (workspace.Repo contains the repo URL)
defaultBranch := "main" // fallback only
if db, err := m.GetDefaultBranch(ctx, w.Repo); err == nil {
    defaultBranch = db
}
revListCmd := exec.CommandContext(ctx, "git", "rev-list", "--left-right", "--count", "HEAD...origin/"+defaultBranch)
```

This avoids running a git command on every poll. The cache is populated when origin query repos are created on daemon startup.

Also update error message (line 210) to use `defaultBranch` variable.

### Fix: LinearSync Endpoints (`internal/dashboard/handlers.go`)

**`linear-sync-from-default`** (formerly `linear-sync-from-main`):

```go
// Before:
syncCmd := exec.CommandContext(ctx, "git", "log", "--oneline", "--reverse", "origin/main..HEAD")

// After:
defaultBranch, err := m.workspaceManager.GetDefaultBranch(ctx, workspace.Repo)
if err != nil {
    return fail("failed to get default branch: %w", err)
}
syncCmd := exec.CommandContext(ctx, "git", "log", "--oneline", "--reverse", "origin/"+defaultBranch+"..HEAD")
```

**`linear-sync-to-default`** (formerly `linear-sync-to-main`):

```go
// Before:
if gitBehind > 0 {
    return fail("workspace is behind main")
}

// After:
defaultBranch, err := m.workspaceManager.GetDefaultBranch(ctx, workspace.Repo)
if err != nil {
    return fail("failed to get default branch: %w", err)
}
if gitBehind > 0 {
    return fail("workspace is behind %s", defaultBranch)
}
```

And update the push command:
```go
// Before:
pushCmd := exec.CommandContext(ctx, "git", "push", "origin", "HEAD:main")

// After:
pushCmd := exec.CommandContext(ctx, "git", "push", "origin", "HEAD:"+defaultBranch)
```

**API docs (`docs/api.md`)** - rename endpoints:
- `POST /api/workspaces/{workspaceId}/linear-sync-from-main` → `linear-sync-from-default`
- `POST /api/workspaces/{workspaceId}/linear-sync-to-main` → `linear-sync-to-default`

Response messages should also be dynamic:
- "Synced 3 commits from main into feature-branch" → "Synced 3 commits from {default_branch} into {branch}"
- "Pushed 2 commits to main" → "Pushed 2 commits to {default_branch}"

### Usage: getRecentBranchesFromBare (`internal/workspace/origin_queries.go`)

Update to use cached default branch instead of calling `getDefaultBranch` again:

```go
// getRecentBranchesFromBare queries a bare clone for recent branches.
func (m *Manager) getRecentBranchesFromBare(ctx context.Context, queryRepoPath, repoName, repoURL string, limit int) ([]RecentBranch, error) {
    // Get default branch from cache (populated by EnsureOriginQueries)
    defaultBranch := "main" // fallback
    if db, err := m.GetDefaultBranch(ctx, repoURL); err == nil {
        defaultBranch = db
    }

    // Skip the default branch (main/master/etc)
    if branch == defaultBranch {
        continue
    }

    // ... rest of function
}
```

Similarly update `GetBranchCommitLog` to use cached default branch.

---

## API Changes

### GET /api/config Response

Extend `RepoWithConfig` to include `default_branch`:

```go
// internal/api/contracts/config.go
type RepoWithConfig struct {
    Name          string      `json:"name"`
    URL           string      `json:"url"`
    DefaultBranch string      `json:"default_branch,omitempty"` // NEW - omitted if not detected
    Config        *RepoConfig `json:"config,omitempty"`
}
```

**Response example**:
```json
{
  "repos": [
    {
      "name": "myrepo",
      "url": "git@github.com:user/myrepo.git",
      "default_branch": "develop",  // detected from origin query repo
      "config": {...}
    },
    {
      "name": "other",
      "url": "git@github.com:user/other.git",
      // no default_branch field - not yet detected (origin query repo not ready)
      "config": {...}
    }
  ],
  // ... other fields
}
```

**Note**: The `default_branch` field is only included when the origin query repo exists and the default branch was successfully detected. For repos where detection hasn't completed, the field is omitted entirely (not null, not empty string - the key is absent).

---

## Frontend Changes

### TypeScript Types (`contracts.ts`)

After running `go run ./cmd/gen-types`:

```typescript
export interface Repo {
    name: string;
    url: string;
}

export interface RepoWithConfig {
    name: string;
    url: string;
    default_branch?: string;  // NEW - optional, omitted if not detected
    config?: RepoConfig;
}
```

### SpawnPage.tsx

Replace hardcoded `"main"` fallbacks with detected value:

```typescript
// Get default branch from config
const getDefaultBranch = (repoUrl: string): string => {
    const repo = config.repos.find(r => r.url === repoUrl);
    // If default_branch is undefined (repo not cloned yet), use "main" as UI placeholder
    // The actual spawn will fail if the base repo doesn't exist, which is correct behavior
    return repo?.default_branch || "main";
};

// Line 587 - when no branch suggest target
if (!branchSuggestTarget) {
    const defaultBranch = getDefaultBranch(repoUrl);
    setBranch(defaultBranch);
    setScreen('review');
    return;
}

// Lines 596, 599-600 - when branch suggestion fails
setBranch(result.branch || getDefaultBranch(repoUrl));
// ...
toastError(`Branch suggestion failed. Using "${getDefaultBranch(repoUrl)}".`);
setBranch(getDefaultBranch(repoUrl));

// Line 624 - actual branch for spawn
const actualBranch = inExistingWorkspace ? branch : (branch || getDefaultBranch(repoUrl));
```

### CLI (`cmd/schmux/spawn.go`)

Lines 46-47 - the CLI default is less critical since users typically specify branches explicitly. Could:
1. Leave as-is (main is a reasonable default)
2. Query config for repo's actual default if `-r` is provided

Option 2 (if desired):

```go
// If repo specified via flag, try to get its default branch
if repoFlag != "" {
    if cfgRepo, found := cmd.findRepo(repoFlag, cfg); found {
        // Could query daemon for default branch here
        // For now, leave as "main" - user can override with -b
    }
}
fs.StringVar(&branchFlag, "b", "main", "Git branch")
```

---

## Implementation Checklist

### Manager & Cache
- [ ] Add `defaultBranchCache` map and mutex to `Manager` struct
- [ ] Implement `GetDefaultBranch(ctx, repoURL)` with in-memory cache lookup and on-demand detection
- [ ] Implement `setDefaultBranch(repoURL, branch)` helper
- [ ] Implement `branchExists(ctx, queryRepoPath, branch)` helper for fallback

### Origin Queries
- [ ] Update `getDefaultBranch` in `origin_queries.go` to return empty string on failure (not "main")
- [ ] Add `branchExists` helper function to `origin_queries.go`
- [ ] Update `EnsureOriginQueries` to populate cache after cloning each origin query repo
- [ ] Update `FetchOriginQueries` to refresh cache after fetching each origin query repo

### Worktree & Git Operations
- [ ] Update `addWorktree` signature to accept `repoURL`, use `GetDefaultBranch` instead of hardcoded `origin/main`
- [ ] Update `gitStatus` to use cached `GetDefaultBranch` from workspace's repo URL
- [ ] Update error message in `gitStatus` to use detected branch name

### API & Contracts
- [ ] Add `DefaultBranch` field to `contracts.RepoWithConfig`
- [ ] Update `/api/config` handler to populate `default_branch` from cache (omit if not detected)
- [ ] Run `go run ./cmd/gen-types` to regenerate TypeScript types

### LinearSync Endpoints
- [ ] Fix `linear-sync-from-main` to use detected default branch (rename to `linear-sync-from-default`)
- [ ] Fix `linear-sync-to-main` to use detected default branch (rename to `linear-sync-to-default`)
- [ ] Update response messages to use dynamic branch names

### Frontend
- [ ] Update `SpawnPage.tsx` to use `default_branch` from config instead of hardcoded `"main"`

### Documentation
- [ ] Update `docs/api.md`:
  - Document `default_branch` field in `/api/config` response
  - Rename `linear-sync-from-main` → `linear-sync-from-default`
  - Rename `linear-sync-to-main` → `linear-sync-to-default`
  - Update response examples to show dynamic branch names
- [ ] Update `docs/workspaces.md` - change "main" references to "default branch"
- [ ] Update user-facing messages (e.g., "Caught up to main" → "Caught up to {branch}")
- [ ] Update comments that reference `origin/main` to use `origin/{default_branch}` terminology

### Tests
- [ ] Write unit tests for `GetDefaultBranch` cache behavior
- [ ] Write unit tests for `branchExists` helper
- [ ] Add E2E test for spawning with non-main default branch
- [ ] Add E2E test for LinearSync endpoints with non-main default branch

---

## Testing Strategy

### Unit Tests

```go
func TestGetDefaultBranch(t *testing.T) {
    // Test symbolic-ref detection
    // Test fallback to common defaults
    // Test in-memory cache
    // Test state.json persistence
}

func TestTryCommonDefaults(t *testing.T) {
    // Test main exists
    // Test master exists when main doesn't
    // Test develop exists when neither main nor master
    // Test fallback to "main" when none exist
}
```

### E2E Test

Create a test repository with `master` as default branch, verify:
1. Workspace creation succeeds
2. Git status shows correct ahead/behind against `origin/master`
3. Spawn wizard pre-fills `master` as default
