# Repos

## What it does

Repo entries are the daemon's records of the repositories it can clone and
spawn workspaces from. One manager owns them, they live in `state.json`, and
the browser adds and removes one at a time.

A repo entry is a name, a URL, a bare path, a VCS, its overlay paths, and two
flags. The name is the identity: it becomes the workspace ID prefix, the
bare-repo directory, the per-repo directory under `~/.schmux`, and the `{repo}`
route segment.

## Key files

| File                                               | Purpose                                                                           |
| -------------------------------------------------- | --------------------------------------------------------------------------------- |
| `internal/repo/manager.go`                         | `Manager` — the only writer of repo entries                                       |
| `internal/repo/name.go`                            | `SanitizeRepoName`, `repoNameFromURL` — the naming rule and the derivation        |
| `internal/repo/bare_paths.go`                      | `PopulateBarePaths`, `NormalizeBarePaths` — the bare-path repairs                 |
| `internal/repo/relocate_bare_repo.go`              | `relocateBareRepo` — renames a bare repo and fixes its worktrees' `gitdir:` lines |
| `internal/repo/pending.go`                         | `AddPending` — consumes `pending-repos.jsonl`                                     |
| `internal/repo/errors.go`                          | `ErrNotFound`, `ErrDuplicateName`, `ErrInvalid`                                   |
| `internal/state/repos.go`                          | `state.Repo` and the six store accessors                                          |
| `internal/config/pending_repos.go`                 | the `move_repos_to_pending` migration, the config backup, the pending file        |
| `internal/dashboard/handlers_repos.go`             | `handleRepoAdd`, `handleRepoRemove` — the two endpoints                           |
| `internal/workspace/repo_remove.go`                | `RemoveRepo`, `deleteRepoClones` — the removal rule and the clone deletion        |
| `internal/workspace/vcs_git.go`                    | `EnsureRepoBase` — finds or clones the worktree base at `repos/<bare_path>`       |
| `internal/dashboard/handlers_overlay.go`           | the overlay handlers, which write their fields through the manager                |
| `internal/daemon/daemon.go`                        | `initManagers` builds the manager and wires the pending consumer                  |
| `assets/dashboard/src/routes/ConfigPage.tsx`       | the Settings repo list: one add, one remove, then a refresh                       |
| `assets/dashboard/src/components/AddRepoModal.tsx` | the Add Repository dialog                                                         |
| `assets/dashboard/src/lib/api.ts`                  | `addRepo`, `removeRepo` — the client                                              |

## Architecture decisions

- **One manager owns the entries.** The manager is the only writer; the store's
  accessors are called by it alone. Every write reads the entry, changes one
  field, calls `state.UpdateRepo` under one mutex, and ends in `state.Save()`.
  No operation replaces the list. A list-replacing write would silently drop a
  repo when two writers race, and the one-entry-at-a-time shape also gives each
  write its own error.
- **Repos live in `state.json`, not `config.json`.** Repos are records the daemon
  creates and changes while it runs. `config.json` is the user's settings
  document, and it is replaced wholesale on every reload — a repo registered
  between the read and the write would be lost.
- **The name is derived for git and given for sapling.** A git URL carries the
  repo name; a sapling URL (`sl:my.repo`) does not, so sapling is the only VCS
  whose name comes from the request. The derived name avoids every stored name
  and every bare base on disk that belongs to a different remote
  (`Manager.deriveName` feeds `repoNameFromURL` both).
- **No URL uniqueness.** Two entries may share a URL, as before. A second entry
  with a stored URL takes that URL's `bare_path`. The name is the real identity:
  the bare base, the workspace prefix, and the route segment all come from it.
- **The migration has two halves joined by a file.** `config.Load` runs before
  the state store and the manager exist, so `move_repos_to_pending` cannot add
  anything — it only records what it saw. The daemon takes the entries from
  there later, through `Manager.AddPending`, the API every other caller uses.
  The hand-off is a file and not memory because `schmux start` loads the config
  in the parent process and runs the daemon as a child.
- **The bare-path repairs moved with the entries.** `PopulateBarePaths` and
  `NormalizeBarePaths` walk the repo list, so they moved to `internal/repo` and
  write to state. They were the last code in `internal/config` that owned repo
  entries.
- **A repo with workspaces cannot be removed, so the entry is the only record of
  its clones.** An entry's clones are `repos/<bare_path>` (the worktree base)
  and `query/<bare_path>` (the origin query repo). `state.json` used to keep a
  second list, `base_repos`, because a repo could be removed while its
  workspaces stayed; that list was the only thing left that knew where the
  clone was, so the last dispose could delete it. With removal rejected while a
  workspace uses the repo, every workspace and every git clone has its entry,
  and every lookup (spawn, dispose fallback, sapling's create command) reads
  the location from the entry. `base_repos` is dropped on load without being
  read; clones orphaned by older versions are left on disk.
- **The removal rule lives in `workspace.Manager.RemoveRepo`, not in
  `repo.Manager.Remove`.** GitHub connect calls `repos.Remove` to merge a local
  repo into an existing entry while the local repo's workspaces still point at
  `local:<name>`, then relinks them. A check inside `Remove` would reject that
  merge.
- **Removal holds `repoLock(url)` across the check, the entry removal, and the
  clone deletion.** Spawn (`GetOrCreate`) and dedicated workspaces already hold
  that lock from entry lookup to the workspace landing in state, so a removal
  either sees the new workspace or makes the spawn find no entry. `Scan` takes
  the same lock and repeats the entry lookup before it adds or relinks a
  workspace. Branch spawn takes no lock: its source workspace keeps the repo in
  use for the whole call.
- **Removing a git repo deletes both clones at once; dispose deletes none.** The
  query clone goes too because `ensureOriginQueryRepo` reuses whatever sits at
  `query/<bare_path>` and re-points its origin, and name derivation only checks
  `repos/` for foreign clones, so a later repo with the same name would inherit
  the old remote's query clone. Sapling entries are skipped (the base can be a user-managed
  checkout) and local entries are skipped (their `bare_path` names nothing; the
  history lives in the workspace). Deletion errors are logged and the removal
  still returns `200`.
- **`EnsureRepoBase` clones only when `repos/<bare_path>` is missing.** An
  existing base is reused only when its `remote.origin.url` equals the entry's
  URL; otherwise the spawn fails rather than land a workspace on the wrong
  remote.
- **The config's other repo keys stay in the config.** `subreddit.repos`,
  `repofeed.repos`, and `build_monitor.repos` are settings keyed by repo, not
  repo entries.

## Gotchas

- **A binary from before this change shows no repos.** The migration takes them
  out of `config.json`; the backup `config.json.<timestamp>.bak` holds the old
  list.
- **`POST /api/config` ignores a `repos` field.** The field is not part of the
  config any more, so every entry stays as it was and the response is still
  `200`. `GET /api/config` still returns `repos`, filled from the manager.
- **The pending file is the only hand-off.** `config` writes it (the migration
  and the build defaults); `Manager.AddPending` reads it and removes it. A crash
  between the pending write and the config save leaves the repos in both
  places — the next load writes them again and `AddPending` skips the ones
  already stored, because the name is taken.
- **`AddPending` removes the file only after `state.Save()` succeeds.** A failed
  save leaves the file, and the next run adds the entries again.
- **`Reload` runs only the `move_repos_to_pending` registry entry.** The rest of
  the registry is not idempotent and would rewrite the file on every reload.
- **A name is sanitized where it is typed**, not on save: the sapling name in
  `POST /api/repos` and the local-repo name in `CreateLocalRepo`. A name that
  came from `config.json` keeps its dots. See [repo-names.md](repo-names.md).
- **Stored names are arbitrary.** `DELETE /api/repos/{repo}` takes the name
  percent-encoded and the handler unescapes it, so a name with a space, `/`, or
  `%` can still be removed.
- **`NormalizeBarePaths` runs at startup only**; `PopulateBarePaths` runs at
  startup and after every config reload.
- **An operation failure is a dialog, not a toast.** The Settings list reports a
  failed add, remove, or refresh through `useModal().alert()`; the message has
  to stay readable and copyable.
- **Remove is disabled while a workspace uses the repo.** The Settings row reads
  "In use by N workspace(s)", counted from `useSessions()` by matching
  `workspace.repo` to `repo.url`. A workspace that appears after the page
  rendered gets the server's `400` (naming the workspace IDs) in the "Remove
  Repo Failed" dialog.
- **A test that removes a repo must dispose its workspaces first.** The scenario
  harness's `seedRepos` (`test/scenarios/generated/helpers.ts`) runs
  `dispose-all` on every workspace before it deletes the previous spec's repos;
  without it, every spec after one that spawned fails in setup with
  `Failed to remove repo …: 400`.

## Common modification patterns

- **To add a field to a repo entry:** add it to `state.Repo`
  (`internal/state/repos.go`), then `go run ./cmd/gen-types`, then read it in
  the manager and the handlers. The JSON names are the ones `config.json` used,
  so entries written by an older binary keep working.
- **To add a write:** add a method to `Manager` that reads the entry, changes
  one field, calls `state.UpdateRepo`, and ends in `state.Save()` under `m.mu`.
  Callers use the manager; never write the store directly.
- **To add a reader:** call `List`, `Get`, or `GetByURL` on the manager. Do not
  read the store directly, and do not add a `config.Repos` accessor back.
- **To add a path that creates or relinks a workspace:** hold
  `m.repoLock(url)` from the entry lookup until the workspace is in state, or
  repeat the lookup under the lock before the write, as `Scan` does. Otherwise
  a removal in between lands a workspace whose repo has no entry.
- **To change what removal deletes:** `deleteRepoClones` in
  `internal/workspace/repo_remove.go`; the tests are in `repo_remove_test.go`
  and the `TestAPIContract_RepoRemove*` contract tests.
- **To change how a name is derived:** `internal/repo/name.go` only.
  `Manager.deriveName` passes it the stored names plus
  `config.ConflictingBaseNames(url)`.
- **To change the API:** `internal/dashboard/handlers_repos.go` for the repo
  endpoints and `docs/api.md` for the contract (CI-enforced), then
  `go run ./cmd/gen-types` for the contracts.
- **To change the browser flow:** `assets/dashboard/src/routes/ConfigPage.tsx`
  for the Settings list and `assets/dashboard/src/lib/api.ts` for the client.
  Each add or remove is one request; the list is then refreshed from
  `GET /api/config`.
- **To change the migration:** `internal/config/pending_repos.go`. It must stay
  a raw-JSON edit that records what it saw — it cannot add entries, because the
  manager does not exist when the config loads.
