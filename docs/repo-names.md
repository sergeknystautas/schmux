# Repo Names

## What it does

A repo name is reused as a workspace ID prefix, a directory name, a tmux session name, and an API route segment. `repo.SanitizeRepoName` reduces every new name that reaches the repo store (`~/.schmux/state.json`) to a character set that is valid in all of those places, so a repo named `lordbalto.games` becomes `lordbalto-games` and can spawn a session.

The store, the manager that owns it, and the endpoints that write to it are in [repos.md](repos.md).

## Key files

| File                                           | Purpose                                                                  |
| ---------------------------------------------- | ------------------------------------------------------------------------ |
| `internal/repo/name.go`                        | `SanitizeRepoName` — the single place the rule lives                     |
| `internal/repo/name.go`                        | `repoNameFromURL` — derives a name from a git URL, already sanitized     |
| `internal/repo/manager.go`                     | `Manager.Add` — derives the name of a git repo, already sanitized        |
| `internal/dashboard/handlers_repos.go`         | `handleRepoAdd` — sanitizes the sapling name the request carries         |
| `internal/workspace/manager.go`                | `CreateLocalRepo` — sanitizes the typed local-repo name                  |
| `internal/tmux/tmux.go`                        | `ValidateSessionName` — the constraint that forced the rule              |
| `internal/dashboard/validation.go`             | `isValidRepoName` — HTTP-route validator for the `{repo}` path parameter |
| `internal/dashboard/handlers_spawn_entries.go` | `validateSpawnRepo` — route middleware for `/api/spawn/{repo}/…`         |

## The rule

`SanitizeRepoName` replaces every character outside `[A-Za-z0-9_-]` with `-`, cuts the result to 128 characters, and trims leading and trailing `-`.

| Input             | Output                                   |
| ----------------- | ---------------------------------------- |
| `lordbalto.games` | `lordbalto-games`                        |
| `.github`         | `github`                                 |
| `../etc`          | `etc`                                    |
| `foo/bar`         | `foo-bar`                                |
| `my repo:v2`      | `my-repo-v2`                             |
| `...`             | `` (rejected as "repo name is required") |

## Why that character set

The rule lists valid characters rather than invalid ones. The repo name becomes the workspace ID prefix (`internal/workspace/manager.go:705`, `:876`, `:1883`), the workspace ID becomes the session ID prefix, and the session ID becomes the tmux session name. tmux forbids `.` in session names, so `lordbalto.games-001-d0659c47` fails to spawn:

```
failed to create tmux session: invalid session name:
invalid session name "lordbalto.games-001-d0659c47": contains disallowed characters
```

The same name is also a directory name, a `<name>.git` bare-repo directory, a per-repo directory under `~/.schmux`, and a `{repo}` route segment. Each of those has its own, different check (`internal/tmux/tmux.go:21`, `internal/dashboard/validation.go:67`, `internal/dashboard/handlers_spawn_entries.go:47`, `internal/autolearn/signals.go:344`, `internal/autolearn/pending_merge.go:66`). Letters, digits, `-` and `_`, with no leading `-`, at most 128 characters, pass every one of them. Those downstream checks are deliberately not tightened: tightening them would break repos that already exist.

## Where names enter the repo store

| Entry point                            | Location                                               | Rule applied                                 |
| -------------------------------------- | ------------------------------------------------------ | -------------------------------------------- |
| Spawn with a git URL (register if new) | `internal/dashboard/handlers_spawn.go` → `Manager.Add` | always (the name is derived from the URL)    |
| `POST /api/repos`, git or git-clone    | `internal/dashboard/handlers_repos.go` → `Manager.Add` | always (the request's `name` is not read)    |
| `POST /api/repos`, sapling             | `internal/dashboard/handlers_repos.go`                 | the request's `name`, sanitized; empty → 400 |
| `CreateLocalRepo`                      | `internal/workspace/manager.go` → `Manager.Add`        | always                                       |
| GitHub connect                         | `internal/workspace/github_connect.go`                 | reuses the local repo's existing name        |
| Migration from `config.json`           | `internal/repo/pending.go` → `Manager.AddPending`      | none: a stored name moves unchanged          |

Frontend senders need no change: the server derives the name, and the frontend reloads the repo list after each add so it shows the stored name.

## Architecture decisions

- **Sanitize at the entry point, not at the tmux layer.** Sanitizing the session ID before handing it to tmux is a smaller diff, but the session ID and the tmux session name would diverge, and dotted names would still reach workspace IDs and bare paths. The repo name is the cause.
- **One function holds the rule.** `internal/repo` is imported by both `internal/dashboard` and `internal/workspace`, so the call sites share one implementation rather than copies that can drift.
- **Names move unchanged into `state.json`.** The repos moved from `config.json` to `state.json` (see `docs/schmux-directories.md`). The move is a straight storage change: no code path renames a repo, and a pre-existing dotted repo keeps its dotted name until the user removes it and adds it again.
- **Stored names are arbitrary.** `DELETE /api/repos/{repo}` takes the name percent-encoded and unescapes it, so a stored name with a space, `/`, or `%` can still be removed.

## Gotchas

- **A pre-existing dotted repo still cannot spawn.** Its stored name is unchanged, so the workspace ID and session name still contain `.`. The fix is to remove it in the settings page and add it again — a new name is sanitized like any other.
- **Sanitizing can create a collision.** A new repo named `a.b` becomes `a-b`, which may already exist. The request then fails with the existing duplicate-name error, the same as typing `a-b` would.
- **A name with no valid characters fails as "repo name is required"**, not as an invalid-name error. `CreateLocalRepo`'s old `..` / `/` / `\` rejection can no longer fire and was removed; `../etc` is now a valid name that becomes `etc`.
- **`isValidRepoName` still allows dots.** It is an HTTP-route validator, not a store constraint. Names written by the API are reduced to the valid set, but a repo already stored in `state.json` can still hold a dotted name.
