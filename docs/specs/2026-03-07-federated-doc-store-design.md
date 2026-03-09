# Federated Document Store Design

## Overview

A per-repo document store that uses a special orphan git branch (`schmux`) as the storage and synchronization layer. Any schmux installation with access to the same remote repository can read and write documents, making the store federated across machines.

## Storage

Documents live on an orphan branch named `schmux` in the repo's bare clone (`~/.schmux/repos/<bare-path>`). A persistent worktree is checked out at `~/.schmux/docs/<bare-path-stem>/` (e.g., `~/.schmux/docs/schmux/` for bare path `schmux.git`).

Layout on the branch:

```
<schema>/<filename>.md
```

Example:

```
notes/architecture.md
tasks/refactor-auth.md
notes/meeting-2026-03-07.md
```

## Interface

Package: `internal/docstore/`

```go
type Store interface {
    Get(ctx context.Context, repoURL, schema, filename string) (string, error)
    Remove(ctx context.Context, repoURL, schema, filename string) error
    Put(ctx context.Context, repoURL, schema, filename, data string) error
    List(ctx context.Context, repoURL, schema string) ([]string, error)
}
```

All methods take `repoURL` as the repo identifier. The store resolves it to the bare clone path via the state store's `GetWorktreeBaseByURL()`.

- `Get` — returns the markdown content as a string; error if not found.
- `Put` — creates or updates. Commits, pulls, and pushes.
- `Remove` — deletes the file. Commits, pulls, and pushes.
- `List` — returns filenames (without `.md` extension) for the given schema.

Filenames must not contain path separators or special characters. Schema names follow the same constraint.

## Write Flow (Put / Remove)

1. Resolve `repoURL` → bare clone path → docstore worktree path.
2. Ensure the worktree exists (bootstrap if first access — see below).
3. Write the file to `<worktree>/<schema>/<filename>.md` (or delete it for Remove).
4. `git add <schema>/<filename>.md` (or `git rm`).
5. `git commit -m "docstore: <put|remove> <schema>/<filename>"`.
6. `git pull --rebase origin schmux` — incorporate any remote changes.
7. `git push --force-with-lease origin schmux`.
8. If any step fails, return the error. The caller decides whether to retry.

## Read Flow (Get / List)

Read directly from the local worktree on disk. No network call. Freshness is maintained by the periodic poller.

## Branch Bootstrap

On first access to a repo's docstore (worktree doesn't exist):

1. In the bare clone, check if `schmux` branch exists locally or at `origin/schmux`.
2. If remote exists: `git worktree add <docs-path> schmux` (tracks remote).
3. If neither exists: create orphan branch.
   - `git worktree add --detach <docs-path>`
   - In the worktree: `git checkout --orphan schmux`
   - `git rm -rf .` (clean any files)
   - `git commit --allow-empty -m "docstore: initialize"`
   - `git push origin schmux`
4. Record the docstore worktree path in state for fast subsequent access.

## Poller Integration

In the existing `UpdateAllGitStatus` polling loop, for each repo that has a docstore worktree:

1. `git fetch origin schmux` (in the bare clone — deduplicated by the existing `gitFetchPollRound` mechanism).
2. Fast-forward the local `schmux` branch to `origin/schmux` if it's a fast-forward (same pattern used for default branch in `updateLocalDefaultBranch`).
3. In the docstore worktree: `git reset --hard origin/schmux` to pick up remote changes (safe because the worktree has no local uncommitted state between writes).

## Concurrency

- A per-repo `sync.Mutex` in the docstore serializes local writes. Multiple concurrent `Put`/`Remove` calls to the same repo queue up.
- Reads (`Get`/`List`) do not acquire the mutex — they just read from disk.
- If `push --force-with-lease` fails because the remote moved, the error is returned to the caller. This is the expected behavior for a federated store — the caller retries if desired.

## Validation

- Schema: lowercase alphanumeric, hyphens, underscores. No path separators.
- Filename: same constraints. The `.md` extension is added/stripped by the store.
- Data: arbitrary string (markdown content).

## Dependencies

- `internal/state` — to resolve `repoURL` → `WorktreeBase` path.
- Git CLI — all git operations via `exec.CommandContext`, same pattern as `workspace/run_git.go`.

The docstore has no dependency on the workspace manager. The daemon wires them together.

## File Structure

```
internal/docstore/
    store.go        — Store interface, implementation, bootstrap logic
    store_test.go   — Unit tests
```
