# Remove detectExistingBarePath

## Problem

`BarePath` is a config field that stores the filesystem directory name for a repo's base (bare clone for git, repo base for Sapling). It exists because the system tolerates non-conforming filesystem layouts — instead of dictating where repo bases live, the code scans the disk to discover whatever directory name happens to exist and conforms to it.

This is an architectural violation: config and filesystem are supposed to agree, and the correct response to disagreement is to fix the filesystem, not embrace the non-conformity.

The canonical rule is simple: a repo's base directory is derived from its Name. For git repos, that's `{name}.git`. For Sapling repos, that's `{name}`. The `BarePath` field is redundant — it's always derivable from Name and VCS type. There is no namespaced path, no legacy path, no detection needed.

## The Violation

`detectExistingBarePath()` and its supporting functions (`findBarePathOnDisk`, `bareRepoMatchesURL`, `parseGitHubURL`, `extractRepoNameFromURL`) exist to scan the filesystem and accept whatever layout they find. `populateBarePaths()` runs on every config load, calling this detection machinery to fill in or correct the `BarePath` field based on what's on disk.

## Solution: Two Phases

### Phase 1: Normalize the filesystem (this branch)

A new function `normalizeBarePaths()` runs during daemon startup, after both config and state are loaded. It enforces the invariant by renaming non-conforming directories and updating all dependent state in one place.

**Call site:** Daemon startup in `internal/daemon/daemon.go`, after config and state are both initialized. Not called on live config reload — avoids racing with active sessions, and after the first startup there's nothing left to normalize anyway.

**Signature:** `normalizeBarePaths(cfg *config.Config, st *state.State)`

**For each git repo where `BarePath != Name + ".git"`** (skip repos with `VCS == "sapling"` — they use different base-path semantics and don't have git worktrees to fix up):

1. Resolve the current bare repo's absolute path on disk (check both `repos/` and `query/` base paths).
2. If it doesn't exist on disk, skip.
3. Compute target path: `{name}.git` under the same base path.
4. If target already exists on disk, log: `"cannot normalize repo %q: target %s already exists — rename one of the repos with duplicate name %q"` and skip.
5. `os.Rename(oldPath, newPath)`.
6. Fix up worktree references: enumerate `newPath/worktrees/*/gitdir`. Each `gitdir` file contains the absolute path to a worktree's `.git` file. Read it, go to that worktree's `.git` file, and rewrite the `gitdir:` line replacing the old bare repo path with the new one.
7. Update the state RepoBase entry (`Path` field) to the new absolute path. Save state.
8. Set `repo.BarePath = Name + ".git"`. Save config.

If a rename fails, log: `"failed to normalize repo %q from %s to %s: %v"` and skip. Next startup retries automatically.

Both `repos/` and `query/` directories are checked — a bare repo may exist in either or both.

**Verified by experiment:** Renaming a bare repo directory and rewriting worktree `.git` files is sufficient. The bare repo's own `worktrees/*/gitdir` files point to the worktrees (not to itself) and do not need updating. Git operations (status, commit, branch, log, new worktree creation) all work immediately after the rename.

#### Repo name uniqueness validation

Add server-side validation that rejects duplicate repo names. Entry points:

- **Config save handler** (`internal/dashboard/handlers_config.go`, the `req.Repos != nil` block around line 270): already validates name and URL are non-empty. Add a check that no two repos in the request share the same name. Return `400 Bad Request` with `"duplicate repo name: %q"`.
- **`CreateLocalRepo`** (`internal/workspace/manager.go`): already sets `BarePath = repoName + ".git"`. Add a check that `FindRepo(repoName)` returns not-found before appending. Return an error if the name is taken.

This prevents the collision scenario that would block normalization.

#### Reusable `relocateBareRepo` function

Extract the rename-and-fixup logic into a standalone function that `normalizeBarePaths()` calls but that also stays as a permanent capability:

```go
// relocateBareRepo moves a bare repo from oldPath to newPath and updates
// all worktree .git files to point to the new location.
// Returns an error if the rename or any fixup fails.
func relocateBareRepo(oldPath, newPath string) error
```

This function:

1. `os.Rename(oldPath, newPath)`
2. Enumerates `newPath/worktrees/*/gitdir`, reads each to find the worktree, rewrites that worktree's `.git` file replacing oldPath with newPath.

It does not touch config or state — the caller handles that. This keeps it reusable for future repo renaming (change Name, compute new BarePath, call `relocateBareRepo`, update config and state).

### Phase 2: Delete the tolerance code (future branch)

Once Phase 1 has been deployed and filesystems are normalized, the `BarePath` field is redundant — it's always derivable from Name and VCS type (`{name}.git` for git, `{name}` for Sapling). Phase 2 deletes the field and everything that existed to populate, detect, or tolerate it:

- `BarePath` field from `config.Repo` and `contracts.Repo` — replaced by deriving from Name and VCS at point of use.
- `normalizeBarePaths()` — nothing left to normalize.
- `detectExistingBarePath()` — no detection needed.
- `findBarePathOnDisk()` — no disk scanning needed.
- `bareRepoMatchesURL()` — no URL-matching verification needed.
- `parseGitHubURL()` — only called by `findBarePathOnDisk`. No other callers.
- `extractRepoNameFromURL()` — only called by `findBarePathOnDisk` and `detectExistingBarePath`. No other callers.
- `populateBarePaths()` — replaced by deriving from Name.
- All tests for the above functions.
- `ResolveBareRepoDir()` — callers just use `filepath.Join(config.GetWorktreeBasePath(), name + ".git")`.

Phase 2 is not designed here — just scoped.

## What Stays

- `relocateBareRepo()` — permanent utility for moving bare repos. Enables future repo renaming.
- Repo name uniqueness validation — permanent guard.
- The convention `{name}.git` is already what `CreateLocalRepo()` uses for new repos. This change just enforces it retroactively.
