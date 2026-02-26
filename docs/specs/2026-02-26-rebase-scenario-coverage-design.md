# Rebase Scenario Coverage — Design

## Problem

`LinearSyncFromDefault` (clean sync) and `LinearSyncResolveConflict` (resolve) have observed failures in production: broken repo states, orphaned WIP commits, and lost local changes. The existing test suite covers only utility functions (`truncateString`, `extractConflictHunks`) and one orphan-branch rejection test. No tests exercise the actual rebase operations against real git repository states.

## Goals

1. **Enumerate every conceivable git state** that the two functions can encounter
2. **Build a declarative test harness** that makes scenario setup + invariant assertions reusable
3. **43 tests** covering both functions across 8 dimensions of variation
4. **Inject the LLM dependency** in `LinearSyncResolveConflict` so tests can control conflict resolution outcomes

## Non-goals

- `LinearSyncToDefault` and `PushToBranch` are out of scope
- Dashboard/WebSocket integration testing (covered by scenario tests)
- Performance benchmarking

---

## Scenario Taxonomy

### Dimension 1: Branch relationship (feature ↔ origin/main)

| ID | Scenario | Expected behavior |
|----|----------|-------------------|
| 1a | Already up to date (origin/main is ancestor of HEAD) | Success, count=0 |
| 1b | Strictly behind (HEAD is ancestor of origin/main) | Rebase succeeds |
| 1c | Diverged (both sides have commits since fork) | Rebase local on top of main |
| 1d | Same commit (HEAD == origin/main) | Success, count=0 |
| 1e | No common ancestor (orphan default branch) | Error: "no common ancestor" |

### Dimension 2: Commit counts

| ID | Scenario |
|----|----------|
| 2a | 1 commit on main |
| 2b | Many commits on main (5-10) |
| 2c | 1 local commit |
| 2d | Many local commits (5) |
| 2e | Commits on both sides (N main × M local) |

### Dimension 3: Conflict characteristics

| ID | Scenario |
|----|----------|
| 3a | No conflicts (different files) |
| 3b | Single file conflict, single hunk |
| 3c | Single file conflict, multiple hunks |
| 3d | Multiple files conflict |
| 3e | Conflict on first main commit, rest clean |
| 3f | Conflict on Nth commit (not first) |
| 3g | Conflicts on every commit |
| 3h | File deleted on one side, modified on other |
| 3i | File renamed on one side |
| 3j | Binary file conflict |
| 3k | New file on both sides with same name |

### Dimension 4: Working directory state

| ID | Scenario |
|----|----------|
| 4a | Clean working directory |
| 4b | Staged changes |
| 4c | Unstaged modifications |
| 4d | Untracked files |
| 4e | Mix of staged, unstaged, and untracked |
| 4f | Changes overlapping with incoming conflict |
| 4g | Empty (WIP commit skipped) |

### Dimension 5: WIP commit edge cases

| ID | Scenario |
|----|----------|
| 5a | WIP created, rebase succeeds, WIP unwound |
| 5b | WIP created, rebase fails, WIP unwound |
| 5c | No WIP needed, rebase succeeds |
| 5d | No WIP needed, rebase fails |
| 5e | HEAD already has "WIP:" message (previous leftover) |
| 5f | Pre-commit hook rejects WIP commit |
| 5g | WIP unwind when HEAD message doesn't match |

### Dimension 6: Abort/recovery edge cases

| ID | Scenario |
|----|----------|
| 6a | Rebase abort succeeds |
| 6b | Rebase abort fails (corrupt state) |
| 6c | Rebase already in progress on entry |
| 6d | Context cancelled mid-rebase |
| 6e | Timeout approaching (80% deadline) |

### Dimension 7: Concurrent/locking

| ID | Scenario |
|----|----------|
| 7a | Concurrent sync on same workspace → ErrWorkspaceLocked |
| 7b | Sync while workspace already locked |

### Dimension 8: LLM-specific (LinearSyncResolveConflict only)

| ID | Scenario |
|----|----------|
| 8a | LLM resolves all files, high confidence |
| 8b | LLM resolves all files, low confidence → abort |
| 8c | LLM can't resolve all files → abort |
| 8d | LLM returns error → abort |
| 8e | LLM omits a conflicted file → abort |
| 8f | LLM says "deleted" but file still exists → abort |
| 8g | LLM says "modified" but conflict markers remain → abort |
| 8h | LLM returns unknown action → abort |
| 8i | Multiple commits with conflicts, all resolved |
| 8j | First commit resolves, second doesn't |
| 8k | rebase --continue triggers next conflict |
| 8l | Git auto-resolves (no unmerged files, rebase in progress) |

---

## Test Harness Design

### rebaseFixture builder

File: `internal/workspace/rebase_fixture_test.go`

```go
type rebaseFixture struct {
    t          *testing.T
    remoteDir  string
    cloneDir   string
    manager    *Manager
    st         *state.State
    wsID       string

    localBranch      string
    hadLocalChanges  bool
    localChangeFiles map[string]string   // filename → content
    untrackedFiles   map[string]string
}

func newRebaseFixture(t *testing.T) *rebaseFixtureBuilder { ... }
```

Builder methods configure the scenario:

```go
builder.
    WithLocalBranch(name string).
    WithRemoteCommits(commits ...testCommit).
    WithLocalCommits(commits ...testCommit).
    WithConflictingRemoteCommit(file, localContent, remoteContent string).
    WithStagedChanges(file, content string).
    WithUnstagedChanges(file, content string).
    WithUntrackedFiles(file, content string).
    WithPreCommitHook(script string).
    WithTimeout(d time.Duration).
    WithMockLLM(fn mockConflictResolver).
    Build() *rebaseFixture
```

### testCommit helper

```go
type testCommit struct {
    Files   map[string]string  // filename → content
    Message string
    Delete  []string           // files to delete in this commit
}

func commit(msg string, files ...string) testCommit { ... }  // "file.txt:content" pairs
```

### Universal invariant assertions

Every test calls `fix.AssertInvariants()` which checks:

1. No `.git/rebase-merge` or `.git/rebase-apply` present
2. HEAD is not detached (`git symbolic-ref HEAD` succeeds)
3. On the expected branch
4. No "WIP:" commit as HEAD
5. `git status` succeeds (repo is not corrupt)

### Scenario-specific assertions

```go
fix.AssertSuccess(result, expectedCount)
fix.AssertConflict(result, hash)
fix.AssertError(err, substring)
fix.AssertLocalChangesPreserved()       // verifies all files from With*Changes still present
fix.AssertCleanWorkingDir()
fix.AssertAncestorOf(ancestor, descendant)
fix.AssertHeadMessage(msg)
fix.AssertStepEmitted(action, status)   // for ResolveConflict progress callbacks
```

---

## LLM Mock Injection

### Production change

Add a `conflictResolver` field to `Manager`:

```go
// ConflictResolverFunc is the signature for LLM-based conflict resolution.
type ConflictResolverFunc func(ctx context.Context, cfg *config.Config, prompt string, workspacePath string) (*conflictresolve.Result, string, error)

type Manager struct {
    // ... existing fields ...
    conflictResolver ConflictResolverFunc
}
```

`New()` sets it to `conflictresolve.Execute`.
`LinearSyncResolveConflict` calls `m.conflictResolver(...)` instead of `conflictresolve.Execute(...)`.

### Test mock

The mock must:
1. Return a `conflictresolve.Result` with controllable AllResolved/Confidence/Files
2. Actually write resolved file contents to disk (since validation reads the files)
3. Optionally delete files (for delete-vs-modify scenarios)

```go
func mockLLMHighConfidence(files map[string]string) ConflictResolverFunc {
    return func(ctx context.Context, cfg *config.Config, prompt, workspacePath string) (*conflictresolve.Result, string, error) {
        fileActions := make(map[string]conflictresolve.FileAction)
        for name, content := range files {
            if content == "" {
                os.Remove(filepath.Join(workspacePath, name))
                fileActions[name] = conflictresolve.FileAction{Action: "deleted"}
            } else {
                os.WriteFile(filepath.Join(workspacePath, name), []byte(content), 0644)
                fileActions[name] = conflictresolve.FileAction{Action: "modified"}
            }
        }
        return &conflictresolve.Result{
            AllResolved: true,
            Confidence:  "high",
            Files:       fileActions,
        }, "", nil
    }
}
```

---

## Complete Test List

### LinearSyncFromDefault (23 tests)

| # | Test | Scenarios | Key assertions |
|---|------|-----------|----------------|
| 1 | `AlreadyUpToDate` | 1a | Success, count=0 |
| 2 | `SameCommit` | 1d | Success, count=0 |
| 3 | `StrictlyBehind_SingleCommit` | 1b+2a | Success, count=1, HEAD advanced |
| 4 | `StrictlyBehind_ManyCommits` | 1b+2b | Success, count=5 |
| 5 | `Diverged_NoConflicts` | 1c+3a | Success, local commits replayed on top |
| 6 | `Diverged_ManyCommitsBothSides` | 1c+2b+2d | Success, all commits present |
| 7 | `PreservesUnstagedChanges` | 4c | Modified file still shows in `git diff` |
| 8 | `PreservesUntrackedFiles` | 4d | Untracked file still exists |
| 9 | `PreservesStagedChanges` | 4b | File still in `git diff --cached` |
| 10 | `PreservesMixedWorkingDir` | 4e | All three types preserved |
| 11 | `CleanDir_NoWipCommit` | 4g | No WIP commit in log |
| 12 | `ConflictOnFirstCommit` | 3e | Conflict result, hash matches, count=0 |
| 13 | `ConflictOnNthCommit` | 3f | Conflict result, count=N-1 |
| 14 | `ConflictSingleFileMultipleHunks` | 3c | Conflict detected, repo clean after |
| 15 | `ConflictMultipleFiles` | 3d | Conflict detected |
| 16 | `ConflictDeleteVsModify` | 3h | Conflict detected |
| 17 | `ConflictNewFileSameName` | 3k | Conflict detected |
| 18 | `ConflictPreservesLocalChanges` | 3b+4e | Conflict + local changes both handled |
| 19 | `OrphanDefaultBranch` | 1e | Error: "no common ancestor" |
| 20 | `TimeoutStopsEarly` | 6e | Success with partial count |
| 21 | `PreCommitHookRejectsWip` | 5f | PreCommitHookError returned |
| 22 | `ConcurrentSyncBlocked` | 7a | ErrWorkspaceLocked |
| 23 | `WorkspaceNotFound` | — | Error: "workspace not found" |

### LinearSyncResolveConflict (20 tests)

| # | Test | Scenarios | Key assertions |
|---|------|-----------|----------------|
| 24 | `AlreadyCaughtUp` | — | Success, "Caught up" |
| 25 | `CleanRebase_NoConflicts` | 3a | Success, 0 resolutions |
| 26 | `SingleConflict_HighConfidence` | 8a | Success, 1 resolution |
| 27 | `MultipleFiles_AllResolved` | 3d+8a | Success, files resolved |
| 28 | `DeletedFile_Resolved` | 3h+8a | Success, deleted file handled |
| 29 | `TwoConflictingCommits_BothResolved` | 8i+8k | Success, 2 resolutions |
| 30 | `FirstResolvesSecondFails` | 8j | Failure, 1 resolution in result |
| 31 | `GitAutoResolves` | 8l | Success via rebase --continue |
| 32 | `LowConfidence_Aborts` | 8b | Failure, repo clean |
| 33 | `NotAllResolved_Aborts` | 8c | Failure, repo clean |
| 34 | `LLMError_Aborts` | 8d | Failure, repo clean |
| 35 | `LLMOmitsFile_Aborts` | 8e | Failure, repo clean |
| 36 | `LLMSaysDeletedButFileExists` | 8f | Failure, repo clean |
| 37 | `ConflictMarkersRemain_Aborts` | 8g | Failure, repo clean |
| 38 | `UnknownAction_Aborts` | 8h | Failure, repo clean |
| 39 | `AbortPreservesLocalChanges` | 8c+4e | Failure + local changes preserved |
| 40 | `AbortNoWipIfCleanDir` | 8c+4a | Failure, no orphaned WIP |
| 41 | `PreCommitHookRejectsWip` | 5f | PreCommitHookError |
| 42 | `ConcurrentBlocked` | 7a | ErrWorkspaceLocked |
| 43 | `NoCommonAncestor` | 1e | Error: "no common ancestor" |

---

## File Layout

```
internal/workspace/
├── rebase_fixture_test.go       # Builder, assertions, mock helpers
├── linear_sync_from_default_test.go  # Tests 1-23
├── linear_sync_resolve_test.go       # Tests 24-43
└── linear_sync.go                    # Production code (add conflictResolver field)
```

## Production Code Change

One change to `linear_sync.go`:
- Add `conflictResolver ConflictResolverFunc` field to Manager
- Default to `conflictresolve.Execute` in `New()`
- Replace direct call in `LinearSyncResolveConflict`

This is a single-line behavioral change (indirect call instead of direct call) with no impact on production behavior.
