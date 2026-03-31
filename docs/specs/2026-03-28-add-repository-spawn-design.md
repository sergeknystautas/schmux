# Add Repository via Spawn

## Problem

The spawn wizard's `+ Create New Repository` option only creates local repos via `git init`. Users cannot clone a remote git URL through the spawn flow — they must manually edit `config.json` first. The name is also misleading since it implies only creating new repos, not adding existing ones.

## Solution

Rename the dropdown option to `+ Add Repository` and make the single text input accept either a new repository name to init a new repo, or a git URL to download a new one.

## UI Changes

- **Dropdown option**: `+ Create New Repository` → `+ Add Repository`
- **Input placeholder**: `Repository name` → `Name or git URL`
- **Helper text**: One line below the input: "Enter a project name to init a new one locally, or paste a git URL to clone an existing repository."
- **Everything else unchanged**: dropdown structure, existing repos listing, agent selection, branch suggestion, engage flow.

## Detection & Routing

The frontend sends the raw value in the `repo` field. The spawn handler (`handlers_spawn.go`) determines the type before calling into the session/workspace layer:

1. **Git URL** — matches `https://`, `http://`, `git@`, `ssh://`, or `git://` patterns:
   - Call `FindRepoByURL` — if found, pass the URL through (existing flow handles it)
   - If not found: generate a name, append to `config.Repos`, save config, then pass the URL through — the existing flow handles cloning via `EnsureRepoBase`
2. **Plain name** — no URL pattern detected:
   - Same as today: creates `local:<name>` repo via `git init` with empty initial commit
   - Appends to config repos

## Name Validation

When the input is a plain name (not a URL), reject:

- `..` (directory traversal)
- `/` (path separator)
- `\` (Windows path separator)
- `:` (collides with `local:` prefix sentinel)

## URL Validation

- Must match a recognized git URL pattern
- Clone failure (bad URL, no access, doesn't exist) returns a clear error from the spawn flow
- The repo entry is added to config before cloning — if the clone fails, the entry remains (consistent with how invalid URLs can be added via the config editor)

## Repo Name for URL-Added Repos

The name is derived from the URL, lowercase, using the last path segment with `.git` stripped. On collision with an existing repo name, the owner/org (truncated to 6 characters) is prepended. If that still collides, a numeric suffix is appended.

**Name generation steps:**

1. Strip `.git` suffix from URL
2. Extract last path segment (repo name) and second-to-last segment (owner), lowercase both
3. Candidate = repo name (e.g., `claude-code`)
4. If candidate collides with an existing name in `config.Repos`:
   - Prepend owner truncated to 6 chars: `anthro-claude-code`
5. If that still collides, append numeric suffix: `anthro-claude-code-2`, `-3`, etc.

**Examples:**

| URL                                       | Existing names                     | Result                |
| ----------------------------------------- | ---------------------------------- | --------------------- |
| `github.com/anthropics/claude-code.git`   | (none)                             | `claude-code`         |
| `github.com/bob/claude-code.git`          | `claude-code`                      | `bob-claude-code`     |
| `github.com/alice/claude-code.git`        | `claude-code`, `alice-claude-code` | `alice-claude-code-2` |
| `github.com/very-long-org-name/utils.git` | `utils`                            | `very-l-utils`        |

**Edge case:** URL with only one path segment (no owner) skips owner disambiguation and goes straight to numeric suffix.

This is a new standalone function. It does not reuse `extractRepoNameFromURL` (which is tech debt being removed).

## Idempotency

- If the user pastes a URL that's already in config, the existing repo is used — no duplicate entry, no re-clone
- The frontend can short-circuit: if the URL matches a repo already in the dropdown, silently switch to that repo instead of sending the new-repo signal
- **Limitation:** URL matching is exact-string (same as all existing repo lookups). `https://github.com/user/repo.git` and `git@github.com:user/repo.git` are treated as different repos. This is consistent with how git itself stores remote URLs. URL normalization is a separate follow-up improvement.

## Backend Changes

### `internal/dashboard/handlers_spawn.go`

Before calling `session.Spawn`, the handler processes `req.Repo`:

1. Detect if the value is a git URL (matches `https://`, `http://`, `git@`, `ssh://`, or `git://`)
2. If URL: call `FindRepoByURL`
   - If found: pass through (done — existing flow handles everything)
   - If not found: generate a name (see "Repo Name for URL-Added Repos"), append `config.Repo{Name, URL, BarePath: name + ".git"}` to `config.Repos`, save config (`EnsureRepoBase` requires a non-empty `BarePath` at clone time)
3. If not a URL: existing behavior (plain name → `local:` prefix)

After registration, the URL falls through to the normal spawn flow. `GetOrCreate` → `create()` → `EnsureRepoBase` handles cloning the bare repo automatically. No new workspace manager methods or changes needed.

### `SpawnPage.tsx`

- Rename option text from `+ Create New Repository` to `+ Add Repository`
- Change input placeholder to "Name or git URL"
- Add helper text below input
- On submit: if value matches URL pattern, send raw in `repo` field (no `local:` prefix). If name, send `local:<name>` as today.
- Before submit: if value matches an existing repo URL in dropdown, switch to that repo instead.

### `internal/workspace/manager.go`

No changes.

## What Doesn't Change

- Config structure (still appending to `repos` array)
- `local:` repo creation flow (git init, empty commit, set user config)
- Branch suggestion, agent selection, engage flow
- Workspace creation for known repos
- The dropdown itself and how existing repos are listed
