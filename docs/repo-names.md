# Repo Names

## What it does

A repo name is reused as a workspace ID prefix, a directory name, a tmux session name, and an API route segment. `config.SanitizeRepoName` reduces every name that reaches `~/.schmux/config.json` to a character set that is valid in all of those places, so a repo named `lordbalto.games` becomes `lordbalto-games` and can spawn a session.

## Key files

| File                                           | Purpose                                                                  |
| ---------------------------------------------- | ------------------------------------------------------------------------ |
| `internal/config/repo_mutators.go`             | `SanitizeRepoName` — the single place the rule lives                     |
| `internal/dashboard/repo_name.go`              | `repoNameFromURL` — derives a name from a git URL, already sanitized     |
| `internal/dashboard/handlers_config.go`        | `handleConfigUpdate` — sanitizes new and renamed names on a config save  |
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

## Where names enter config

| Entry point                                        | Location                                                    | Rule applied                               |
| -------------------------------------------------- | ----------------------------------------------------------- | ------------------------------------------ |
| Spawn with a git URL (register if new)             | `internal/dashboard/handlers_spawn.go` → `repoNameFromURL`  | always                                     |
| `handleConfigUpdate`, new git repo                 | `internal/dashboard/handlers_config.go` → `repoNameFromURL` | always (the request's `name` is discarded) |
| `handleConfigUpdate`, existing URL or sapling repo | `internal/dashboard/handlers_config.go`                     | only when the name changed                 |
| `CreateLocalRepo`                                  | `internal/workspace/manager.go`                             | always                                     |
| GitHub connect                                     | `internal/workspace/github_connect.go`                      | reuses the local repo's existing name      |

Frontend senders need no change: the server overrides the name it receives, and the frontend reloads config after saving so it shows the stored name.

## Architecture decisions

- **Sanitize at the entry point, not at the tmux layer.** Sanitizing the session ID before handing it to tmux is a smaller diff, but the session ID and the tmux session name would diverge, and dotted names would still reach workspace IDs and bare paths. The repo name is the cause.
- **One function holds the rule.** `internal/config` is imported by both `internal/dashboard` and `internal/workspace`, so the three call sites share one implementation rather than three copies that can drift.
- **No migration.** No code path renames a repo already in `config.json`, and nothing touches `state.json`. A pre-existing dotted repo keeps its dotted name until the user renames it.
- **Unchanged names are stored exactly as sent.** The settings page re-sends every repo on each save. Sanitizing unconditionally would silently rename a pre-existing dotted repo on the next unrelated settings save.

## Gotchas

- **A pre-existing dotted repo still cannot spawn.** Its stored name is unchanged, so the workspace ID and session name still contain `.`. The fix is to rename it in the settings page (or edit `config.json`) to the dash form — the rename path is sanitized like any other new name.
- **Sanitizing can create a collision.** A new repo named `a.b` becomes `a-b`, which may already exist. The request then fails with the existing duplicate-name error, the same as typing `a-b` would.
- **A name with no valid characters fails as "repo name is required"**, not as an invalid-name error. `CreateLocalRepo`'s old `..` / `/` / `\` rejection can no longer fire and was removed; `../etc` is now a valid name that becomes `etc`.
- **`isValidRepoName` still allows dots.** It is an HTTP-route validator, not a config-schema constraint. Names written by the API are reduced to the valid set, but a repo already stored in `config.json` can still hold a dotted name.
