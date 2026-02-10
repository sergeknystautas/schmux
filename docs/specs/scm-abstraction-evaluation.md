# SCM Abstraction Evaluation for Schmux

## Executive Summary

This document evaluates what it would take to abstract the Source Control Management (SCM) layer in schmux. The goal is to identify all git-specific code and define the abstraction boundaries needed to support git-compatible tools (Jujutsu, Sapling) without being locked to a single implementation.

**Current State**: Git-specific code is scattered across 15+ files with ~3,500 lines. The `source_code_management` config field currently only controls workspace strategy (`git-worktree` vs `git`), not the SCM tool itself.

**Key Finding**: The git integration is deeply embedded. Abstraction requires identifying operation boundaries and defining interfaces that capture the semantic operations (fetch, status, rebase) rather than git commands.

---

## Table of Contents

1. [Git Code Inventory](#git-code-inventory)
2. [Abstraction Boundaries](#abstraction-boundaries)
3. [Interface Design](#interface-design)
4. [Refactoring Steps](#refactoring-steps)
5. [Git-Specific Remaining](#git-specific-remaining)
6. [Type Normalization](#type-normalization)

---

## Git Code Inventory

### Backend Files (Go)

| File                                   | Lines | Git Operations                                        | Abstraction Complexity                |
| -------------------------------------- | ----- | ----------------------------------------------------- | ------------------------------------- |
| `internal/workspace/git.go`            | 368   | fetch, checkout, pull, status, clean, safety checks   | **Medium** - direct command execution |
| `internal/workspace/git_graph.go`      | 480   | commit graph with topological sort, branch membership | **High** - git-specific DAG model     |
| `internal/workspace/git_watcher.go`    | 376   | watching `.git/`, `refs/`, `logs/` directories        | **High** - git metadata paths         |
| `internal/workspace/worktree.go`       | 356   | worktree add/remove/prune, branch conflict resolution | **High** - git worktree feature       |
| `internal/workspace/linear_sync.go`    | 465   | iterative rebase, conflict detection/resolution       | **High** - git rebase semantics       |
| `internal/workspace/origin_queries.go` | 386   | bare clone queries, recent branches, commit logs      | **Medium** - query-repo pattern       |
| `internal/workspace/giturl.go`         | 99    | build web URLs for GitHub/GitLab/Bitbucket            | **Low** - web URL construction        |
| `internal/workspace/manager.go`        | 671   | coordinates git ops for workspace lifecycle           | **Medium** - orchestration layer      |
| `internal/dashboard/handlers.go`       | ~100  | API endpoints for git status, diff, graph             | **Low** - HTTP layer                  |

### Frontend Files (TypeScript)

| File                                                | Lines | Purpose                    | Abstraction Need                         |
| --------------------------------------------------- | ----- | -------------------------- | ---------------------------------------- |
| `assets/dashboard/src/components/GitHistoryDAG.tsx` | 272   | Commit graph visualization | **Low** - consumes API, already abstract |
| `assets/dashboard/src/lib/api.ts`                   | ~50   | API client calls           | **None** - just HTTP calls               |
| `assets/dashboard/src/lib/types.generated.ts`       | ~40   | TypeScript types           | **None** - generated from Go types       |

### Test Files

| File                                     | Lines | Purpose                       |
| ---------------------------------------- | ----- | ----------------------------- |
| `internal/workspace/git_test.go`         | -     | Unit tests for git operations |
| `internal/workspace/git_graph_test.go`   | -     | Unit tests for graph logic    |
| `internal/workspace/git_watcher_test.go` | -     | Unit tests for file watching  |

---

## Abstraction Boundaries

### Boundary 1: Command Execution vs Semantic Operations

**Current Pattern** (everywhere):

```go
cmd := exec.CommandContext(ctx, "git", "fetch", "origin")
cmd.Dir = dir
output, err := cmd.CombinedOutput()
```

**Abstraction Target**:

```go
type SCMProvider interface {
    Fetch(ctx context.Context, dir string) error
}
```

All direct `exec.CommandContext(ctx, "git", ...)` calls need to move into a `GitProvider` implementation.

### Boundary 2: Git-Specific Data Structures

**Current**: Functions return git-specific formats:

- `git status --porcelain` output parsing
- `git log --format=%H|%h|%s|%an|%aI|%P` parsing
- `refs/heads/`, `refs/remotes/`, `refs/tags/` paths

**Abstraction Target**: Define domain types:

```go
type Status struct {
    Dirty         bool
    Ahead         int
    Behind        int
    LinesAdded    int
    LinesRemoved  int
    FilesChanged  int
    ModifiedFiles []FileStatus
    UntrackedFiles []string
}

type Commit struct {
    Hash      string
    ShortHash string
    Message   string
    Author    string
    Timestamp time.Time
    Parents   []string
}
```

### Boundary 3: Workspace Strategy vs SCM Type

**Current**: `source_code_management` field mixes two concerns:

- SCM tool: git (only option currently)
- Workspace strategy: worktree vs full clone

**Abstraction Target**: Separate concerns:

- `scm_type`: "git" (could add "jj", "sl" later)
- `workspace_strategy`: "worktree", "clone", or SCM-specific strategy

### Boundary 4: Git Graph Algorithms

**Current**: `git_graph.go` contains git-specific logic:

- `git merge-base` for fork point
- `git rev-list --left-right --count` for ahead/behind
- `git log --topo-order` for traversal
- Branch membership walking via parent pointers

**Abstraction Target**: Define graph operations that any DAG-based SCM can implement:

```go
type CommitGraph interface {
    GetNodes(opts GraphOptions) ([]Commit, error)
    GetMergeBase(commit1, commit2 string) (string, error)
    GetBranchCommits(branch string) ([]Commit, error)
    IsAncestor(ancestor, descendant string) (bool, error)
}
```

### Boundary 5: File System Watching

**Current**: `git_watcher.go` hardcodes:

- `.git` directory path
- `refs/` and `logs/` subdirectories
- `.git` file parsing for worktrees

**Abstraction Target**: Provider reports what to watch:

```go
type MetadataWatcher interface {
    MetadataDir(path string) string
    WatchPaths(path string) []string
    OnMetadataChange(ctx context.Context, path string) (Trigger, error)
}
```

### Boundary 6: Conflict Resolution

**Current**: `linear_sync.go` assumes git conflict model:

- `git rebase` stops on conflict
- `git diff --name-only --diff-filter=U` lists conflicts
- `git add` + `git rebase --continue` resolves

**Abstraction Target**: Generalize conflict workflow:

```go
type RebaseOperation interface {
    StartRebase(ctx context.Context, target string) error
    HasConflict(ctx context.Context) (bool, error)
    GetConflictedFiles(ctx context.Context) ([]string, error)
    MarkResolved(ctx context.Context, files []string) error
    ContinueRebase(ctx context.Context) error
    AbortRebase(ctx context.Context) error
}
```

---

## Interface Design

### Core SCM Interface

```go
// internal/scm/provider.go
package scm

import "context"

// Provider is the main interface for SCM operations.
// All methods take a directory path and operate on that repository.
type Provider interface {
    // Identity
    Name() string // "git", "jj", "sl"

    // Repository lifecycle
    Clone(ctx context.Context, url, destPath string) error
    Init(ctx context.Context, path, defaultBranch string) error
    Fetch(ctx context.Context, path string) error

    // Branch operations
    CurrentBranch(ctx context.Context, path string) (string, error)
    CreateBranch(ctx context.Context, path, branch, startPoint string) error
    DeleteBranch(ctx context.Context, path, branch string) error
    CheckoutBranch(ctx context.Context, path, branch string) error
    ListBranches(ctx context.Context, path string) ([]Branch, error)
    DefaultBranch(ctx context.Context, path string) (string, error)

    // Working copy state
    Status(ctx context.Context, path string) (*Status, error)
    DiscardChanges(ctx context.Context, path string) error
    Clean(ctx context.Context, path string) error

    // Commit operations
    StageAll(ctx context.Context, path string) error
    Commit(ctx context.Context, path, message string) error
    Push(ctx context.Context, path, branch string) error
    Pull(ctx context.Context, path, branch string) error

    // History
    Log(ctx context.Context, path string, opts LogOptions) ([]Commit, error)
    GetCommit(ctx context.Context, path, hash string) (*Commit, error)

    // Graph operations
    GetMergeBase(ctx context.Context, path, ref1, ref2 string) (string, error)
    IsAncestor(ctx context.Context, path, ancestor, descendant string) (bool, error)
    ResolveRef(ctx context.Context, path, ref string) (string, error)

    // Rebase operations
    Rebase(ctx context.Context, path, target string) error
    RebaseAbort(ctx context.Context, path string) error
    RebaseContinue(ctx context.Context, path string) error
    ConflictedFiles(ctx context.Context, path string) ([]string, error)

    // Metadata (for file watching)
    MetadataDir(path string) string
    WatchPaths(path string) []string
}

// Branch represents a branch in the repository.
type Branch struct {
    Name   string
    Head   string  // commit hash
    IsMain bool    // true if default branch
    IsRemote bool // true if remote tracking branch
}

// FileStatus represents a single file's status.
type FileStatus struct {
    Path   string
    Status ChangeType // Added, Modified, Deleted, etc.
}

type ChangeType string

const (
    ChangeTypeAdded    ChangeType = "added"
    ChangeTypeModified ChangeType = "modified"
    ChangeTypeDeleted  ChangeType = "deleted"
    ChangeTypeRenamed  ChangeType = "renamed"
)

// Status represents the working copy state.
type Status struct {
    Dirty           bool
    Ahead           int
    Behind          int
    LinesAdded      int
    LinesRemoved    int
    FilesChanged    int
    ModifiedFiles   []FileStatus
    UntrackedFiles  []string
    CurrentBranch   string
}

// Commit represents a single commit.
type Commit struct {
    Hash      string
    ShortHash string
    Message   string
    Subject   string // first line of message
    Author    string
    Timestamp time.Time
    Parents   []string
}

// LogOptions controls what commits are returned.
type LogOptions struct {
    Limit       int
    Branch      string // if set, only commits on this branch
    NotBranch   string // exclude commits from this branch
    Reverse     bool   // true for oldest first
    SinceMergeBase string // if set, only commits since merge-base with this ref
}
```

### Workspace Strategy Interface

```go
// internal/scm/strategy.go
package scm

import "context"

// Strategy defines how workspace directories are created and managed.
// This is separate from Provider because different SCMs have different
// working copy models (git worktrees vs jj's single copy).
type Strategy interface {
    // Name identifies the strategy (e.g., "worktree", "clone", "single")
    Name() string

    // CompatibleWith returns true if this strategy works with the given SCM provider
    CompatibleWith(provider Provider) bool

    // PrepareWorkspace ensures a workspace exists at path for the given branch.
    // Returns the actual path (may differ for worktrees).
    PrepareWorkspace(ctx context.Context, basePath, repoURL, branch string, provider Provider) (path string, err error)

    // DisposeWorkspace cleans up a workspace directory.
    DisposeWorkspace(ctx context.Context, path string, provider Provider) error

    // CanReuse returns true if an existing workspace can be reused for a different branch.
    CanReuse(existingPath string, newBranch string, provider Provider) bool
}
```

### Graph Query Interface

```go
// internal/scm/graph.go
package scm

// GraphQuery handles commit graph queries, potentially using a separate
// query repository (like git bare clones) to avoid checking out workspaces.
type GraphQuery interface {
    // GetRecentBranches returns recently updated branches.
    GetRecentBranches(ctx context.Context, repoURL string, limit int) ([]Branch, error)

    // GetBranchLog returns commit messages for a branch.
    GetBranchLog(ctx context.Context, repoURL, branch string, limit int) ([]string, error)

    // GetCommitGraph returns the full graph for visualization.
    GetCommitGraph(ctx context.Context, repoURL string, opts GraphOptions) (*Graph, error)
}

// Graph represents a commit graph.
type Graph struct {
    Nodes    []GraphCommit
    Branches map[string]GraphBranch
}

// GraphCommit is a commit with annotation for display.
type GraphCommit struct {
    Commit
    Branches     []string
    IsHead       []string // branches where this is HEAD
    WorkspaceIDs []string // workspaces at this commit
}

// GraphBranch represents a branch in the graph.
type GraphBranch struct {
    Name         string
    Head         string // commit hash
    IsMain       bool
    WorkspaceIDs []string // workspaces on this branch
}

// GraphOptions controls graph construction.
type GraphOptions struct {
    MaxCommits  int
    ContextSize int  // how many commits to show below fork point
}
```

### Web URL Builder Interface

```go
// internal/scm/url.go
package scm

// URLBuilder constructs web URLs for repositories.
type URLBuilder interface {
    // BranchURL returns a web URL for viewing a branch.
    BranchURL(repoURL, branch string) string

    // CommitURL returns a web URL for viewing a commit.
    CommitURL(repoURL, hash string) string

    // DiffURL returns a web URL for viewing a diff.
    DiffURL(repoURL, branch, baseBranch string) string
}
```

---

## Refactoring Steps

### Step 1: Create Interface Package (Low Risk)

Create `internal/scm/` with:

- `provider.go` - Provider interface
- `strategy.go` - Strategy interface
- `graph.go` - GraphQuery interface
- `url.go` - URLBuilder interface
- `types.go` - Shared types (Commit, Status, Branch, etc.)

**No code changes yet** - just define the contracts.

### Step 2: Create GitProvider (Low Risk)

Create `internal/scm/git/provider.go`:

- Move functions from `git.go` into methods on `GitProvider` struct
- Implement all Provider interface methods
- Add `NewGitProvider()` constructor

**Files to modify**: Just create new file, no changes to existing code yet.

### Step 3: Wire GitProvider into Manager (Medium Risk)

Modify `internal/workspace/manager.go`:

- Add `Provider` field to Manager struct
- Update `New()` to accept a Provider
- Replace direct `m.gitFetch()` calls with `m.provider.Fetch()`
- Update all git method calls to use provider interface

**Files to modify**:

- `manager.go` - update Manager to use Provider
- Keep `git.go` initially (it will become unused)

### Step 4: Migrate Worktree Operations (High Risk)

The worktree code is git-specific. Options:

1. Create `GitWorktreeStrategy` implementing Strategy interface
2. Move `worktree.go` code into the strategy
3. Update Manager to use Strategy instead of direct worktree calls

**Files to modify**:

- Create `internal/scm/git/worktree_strategy.go`
- `manager.go` - use Strategy interface
- Keep `worktree.go` initially

### Step 5: Migrate Graph Operations (High Risk)

The graph code has git-specific logic (merge-base, rev-list). Steps:

1. Create `GitGraphQuery` implementing GraphQuery interface
2. Move `git_graph.go` code into the query implementation
3. Update API handlers to use GraphQuery interface
4. Normalize graph types to use shared Commit types

**Files to modify**:

- Create `internal/scm/git/graph_query.go`
- `internal/dashboard/handlers.go` - use GraphQuery
- Keep `git_graph.go` initially

### Step 6: Migrate Watcher (Medium Risk)

The watcher assumes `.git` directory structure. Steps:

1. Add `WatchPaths()` method to Provider interface
2. Update `GitWatcher` to call `provider.WatchPaths()` instead of hardcoding
3. Move git-specific path resolution into GitProvider

**Files to modify**:

- `git_watcher.go` - use provider.WatchPaths()

### Step 7: Migrate Linear Sync (High Risk)

The rebase logic is git-specific. Options:

1. Keep as-is initially (it's a high-level workflow built on Provider ops)
2. Extract into a `GitSyncStrategy` that knows about git rebase
3. Or abstract into Provider.Rebase\* methods (already in interface)

**Files to modify**:

- `linear_sync.go` - use provider.Rebase\* methods

### Step 8: Migrate Origin Queries (Medium Risk)

The query repo pattern is git-specific but could generalize. Steps:

1. Create `GitQueryProvider` wrapping bare clones
2. Implement GraphQuery interface
3. Update Manager to use QueryProvider

**Files to modify**:

- Create `internal/scm/git/query_provider.go`
- `origin_queries.go` - move code into QueryProvider

### Step 9: Update Config (Low Risk)

Add separate fields:

```go
type Config struct {
    // Existing
    SourceCodeManagement string // "git-worktree" or "git"

    // New: separate SCM type from workspace strategy
    SCMType         string // "git"
    WorkspaceStrategy string // "worktree", "clone", or "auto"
}
```

Migration path:

- Old `source_code_management: "git-worktree"` → `scm_type: "git"`, `workspace_strategy: "worktree"`
- Old `source_code_management: "git"` → `scm_type: "git"`, `workspace_strategy: "clone"`

**Files to modify**:

- `config/config.go` - add new fields, add migration

### Step 10: Delete Old Files (Validation)

Once all code uses interfaces:

- Delete `git.go`
- Delete `worktree.go`
- Delete `git_graph.go`
- Delete `git_watcher.go`
- Delete `origin_queries.go`
- Delete `giturl.go` (or keep for GitURLBuilder)

### Step 11: Update Tests (Validation)

Update all test files to use:

- `GitProvider` instead of direct git commands
- Mock Provider for testing Manager logic
- Strategy interface tests

---

## Git-Specific Remaining

Even after abstraction, some code will remain git-specific:

### 1. Git Command Output Parsing

The porcelain formats (`--porcelain`, `--format=%H|%h|...`) are git-specific. These live inside `GitProvider` and don't need to abstract further.

### 2. Worktree Metadata Format

The `.git` file format (`gitdir: <path>`) and worktree directory structure are git-specific. Lives in `GitWorktreeStrategy`.

### 3. Ref Naming

`refs/heads/`, `refs/remotes/origin/`, `refs/tags/` paths are git-specific. Internally, `GitProvider` uses these but returns normalized `Branch` types.

### 4. Rebase Conflict Markers

Git uses `<<<<<<<`, `=======`, `>>>>>>>` markers. Other tools use different formats. This is handled inside `GitProvider.RebaseContinue()`.

### 5. Web URL Patterns

GitHub/GitLab/Bitbucket URL structures assume git. `GitURLBuilder` handles this. JJ/Sapling would need their own URL builders (or could reuse if they use same hosting).

---

## Type Normalization

### Before (Git-Specific)

```go
// Direct parsing of git output
func (m *Manager) gitStatus(...) (dirty bool, ahead int, behind int, ...) {
    // Parse git status --porcelain
    // Parse git rev-list --left-right --count
    // Parse git diff --numstat
}
```

### After (Normalized)

```go
// Provider returns domain type
func (p *GitProvider) Status(ctx context.Context, path string) (*scm.Status, error) {
    // Parse git-specific formats
    // Return normalized Status struct
}

// Caller uses normalized type
status, _ := provider.Status(ctx, path)
if status.Dirty {
    // ...
}
```

### Shared Types in `internal/scm/types.go`

```go
package scm

import "time"

type Commit struct {
    Hash      string
    ShortHash string
    Message   string
    Subject   string
    Author    string
    Timestamp time.Time
    Parents   []string
}

type Branch struct {
    Name     string
    Head     string
    IsMain   bool
    IsRemote bool
}

type Status struct {
    Dirty          bool
    Ahead          int
    Behind         int
    LinesAdded     int
    LinesRemoved   int
    FilesChanged   int
    ModifiedFiles  []FileStatus
    UntrackedFiles []string
    CurrentBranch  string
}

type FileStatus struct {
    Path   string
    Status ChangeType
}

type ChangeType string

const (
    Added    ChangeType = "added"
    Modified ChangeType = "modified"
    Deleted  ChangeType = "deleted"
    Renamed  ChangeType = "renamed"
)
```

---

## Risk Assessment

| Step                      | Risk       | Reason                       |
| ------------------------- | ---------- | ---------------------------- |
| 1. Create interfaces      | Low        | No code changes              |
| 2. Create GitProvider     | Low        | New file, no changes         |
| 3. Wire into Manager      | Medium     | Changes core orchestration   |
| 4. Migrate worktree ops   | High       | Complex git-specific feature |
| 5. Migrate graph ops      | High       | Custom DAG algorithms        |
| 6. Migrate watcher        | Medium     | File watching is tricky      |
| 7. Migrate linear sync    | High       | Complex multi-step workflow  |
| 8. Migrate origin queries | Medium     | Well-contained code          |
| 9. Update config          | Low        | Backward compatible          |
| 10. Delete old files      | Validation | Ensures nothing was missed   |
| 11. Update tests          | Validation | Maintains coverage           |

---

## Testing Strategy

### Unit Tests

Each `GitProvider` method should have tests:

- Use test fixtures (real git repos in testdata)
- Mock exec.Command for error cases
- Test output parsing

### Integration Tests

Test Manager with real Provider:

- Spawn workspace from real git repo
- Run sync operations
- Verify state changes

### Mock Provider for Manager Tests

Create `MockProvider` for testing Manager logic without git:

```go
type MockProvider struct {
    // Track calls
    FetchCalls []string
    StatusReturns map[string]*Status
    // ...
}
```

---

## Summary

**Total Scope**:

- 15+ files with ~3,500 lines of git-specific code
- 11 refactoring steps
- Estimated 4-6 weeks for complete abstraction

**Key Abstractions**:

1. `Provider` interface - core SCM operations
2. `Strategy` interface - workspace creation (worktree vs clone)
3. `GraphQuery` interface - commit graph queries
4. `URLBuilder` interface - web URL construction

**Benefits**:

- Testable (can mock Provider)
- Extensible (can add JJ/Sapling providers)
- Clear boundaries (git code isolated to one package)

**Risks**:

- Complex areas: worktrees, graph algorithms, rebase workflow
- Need thorough testing to maintain parity
- Config migration for existing users
