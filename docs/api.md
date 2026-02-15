# API Contract

This document defines the daemon HTTP API contract. It is intentionally client-agnostic. If behavior changes, update this doc first and treat any divergence as a breaking change.

Base URL: `http://localhost:7337` (or `https://<public_base_url>` when auth is enabled)

Doc-gate policy:

- Any API-affecting code change must update `docs/api.md`. CI enforces this rule.

General conventions:

- JSON requests/responses use `Content-Type: application/json`.
- Many error responses use plain text via `http.Error`; do not assume JSON unless specified.
- CORS: when auth is disabled, requests are allowed from `http://localhost:7337` and `http://127.0.0.1:7337`. When `bind_address` is `0.0.0.0`, any origin is allowed.
- When auth is enabled, CORS is restricted to the derived allowed origins (must include `public_base_url`) and `Access-Control-Allow-Credentials: true` is set.
- When auth is enabled, all `/api/*` and `/ws/*` endpoints require authentication.

## Auth Endpoints

### GET /auth/login

Redirects to GitHub OAuth.

### GET /auth/callback

OAuth callback endpoint. Exchanges the code, creates a session, and redirects to `/`.

### POST /auth/logout

Clears the auth session cookie.

Response:

```json
{ "status": "ok" }
```

### GET /auth/me

Returns the current authenticated user.

Response:

```json
{
  "github_id": 123,
  "login": "octocat",
  "name": "The Octocat",
  "avatar_url": "https://..."
}
```

## Endpoints

### GET /api/healthz

Health check with version information.

Response:

```json
{
  "status": "ok",
  "version": "1.0.0"
}
```

If a newer version is available, the response includes:

```json
{
  "status": "ok",
  "version": "0.9.0",
  "latest_version": "1.0.0",
  "update_available": true
}
```

### POST /api/update

Triggers a self-update to the latest version from GitHub releases.

The update runs synchronously. On success, the daemon shuts down and must be restarted manually.

Response (200):

```json
{
  "status": "ok",
  "message": "Update successful. Restart schmux to use the new version."
}
```

Errors:

- 405: "Method not allowed" (GET requests rejected)
- 409 with JSON: `{"error":"update already in progress"}`
- 500 with JSON: `{"error":"update failed: ..."}` (includes specific error reason)

Note: Dev builds (version "dev") cannot be updated via this endpoint.

### GET /api/hasNudgenik

Returns whether NudgeNik is available based on whether a nudgenik target is configured.

Response:

```json
{ "available": true }
```

### GET /api/askNudgenik/{sessionId}

Ask NudgeNik to analyze the latest agent response for a session.

Response (200):

```json
{
  "state": "...",
  "confidence": "...",
  "evidence": ["..."],
  "summary": "..."
}
```

Errors:

- 400: "No response found in session output"
- 404: "session not found"
- 503: "Nudgenik is disabled. Configure a target in settings." / "Nudgenik target not found" / "Nudgenik target missing required secrets"
- 500: "Failed to ask nudgenik: ..."

### GET /api/sessions

Returns workspaces and their sessions (hierarchical).

Response:

```json
[
  {
    "id": "workspace-id",
    "repo": "repo-url-or-name",
    "branch": "branch",
    "path": "/path/to/workspace",
    "session_count": 1,
    "git_ahead": 0,
    "git_behind": 0,
    "git_lines_added": 0,
    "git_lines_removed": 0,
    "git_files_changed": 0,
    "git_branch_url": "https://github.com/user/repo/tree/branch", // optional, when remote exists
    "sessions": [
      {
        "id": "session-id",
        "target": "target-name",
        "branch": "branch",
        "nickname": "optional",
        "created_at": "YYYY-MM-DDTHH:MM:SS",
        "last_output_at": "YYYY-MM-DDTHH:MM:SS",
        "running": true,
        "attach_cmd": "tmux attach ...",
        "nudge_state": "optional",
        "nudge_summary": "optional"
      }
    ],
    "previews": [
      {
        "id": "prev_ab12cd34",
        "workspace_id": "workspace-id",
        "target_host": "127.0.0.1",
        "target_port": 5173,
        "proxy_port": 51853,
        "url": "http://127.0.0.1:51853",
        "status": "ready"
      }
    ]
  }
]
```

Notes:

- `last_output_at` is an in-memory runtime signal and resets after daemon restart.
- `last_output_at` may be omitted when no activity has been observed since daemon start.

### POST /api/workspaces/scan

Scans workspace directory and reconciles state.

Response:

```json
{
  "added": 0,
  "removed": 0,
  "updated": 0
}
```

Errors:

- 500 with plain text: "Failed to scan workspaces: ..."

### POST /api/workspaces/{workspaceId}/refresh-overlay

Refresh overlay files for a workspace.

Response:

```json
{ "status": "ok" }
```

Errors:

- 400 with JSON: `{"error":"..."}`

### POST /api/workspaces/{workspaceId}/previews

Create or reuse a workspace preview proxy.

Request:

```json
{
  "target_host": "127.0.0.1",
  "target_port": 5173
}
```

Response:

```json
{
  "id": "prev_ab12cd34",
  "workspace_id": "schmux-005",
  "target_host": "127.0.0.1",
  "target_port": 5173,
  "proxy_port": 51853,
  "url": "http://127.0.0.1:51853",
  "status": "ready"
}
```

Notes:

- Target host must resolve only to loopback addresses (`127.0.0.1`, `::1`, `localhost`).
- Remote workspaces are blocked in Phase 1 (422).
- In network-access mode (`bind_address=0.0.0.0`), preview creation is local-client only.
- `status` can be `degraded` when upstream target is not yet reachable.

### GET /api/workspaces/{workspaceId}/previews

List known previews for a workspace.

Response: array of preview objects from the create endpoint.

### DELETE /api/workspaces/{workspaceId}/previews/{previewId}

Delete a preview mapping and stop its listener.

Response:

```json
{ "status": "ok" }
```

### POST /api/spawn

Spawn sessions.

Request:

```json
{
  "repo": "repo-url",
  "branch": "branch",
  "prompt": "optional",
  "nickname": "optional",
  "targets": { "target-name": 1 },
  "workspace_id": "optional",
  "resume": false
}
```

Contract (pre-2093ccf):

- When `workspace_id` is empty, `repo` and `branch` are required.
- **`repo` must be a repo URL**, not a repo name. The server passes it directly to workspace creation.
- When `workspace_id` is provided, the spawn is an "existing directory spawn" and **no git operations** are performed.
- `targets` is required and maps target name -> quantity.
- Promptable targets require `prompt`. Command targets must not include `prompt`.
- For non-promptable targets, the server forces `count` to 1.
- If multiple sessions are spawned and `nickname` is provided, nicknames are auto-suffixed globally:
  - `"<nickname> (1)"`, `"<nickname> (2)"`, ...

Resume mode (`resume: true`):

- Either `workspace_id` (existing workspace) or `repo`+`branch` (create new workspace) must be provided.
- `prompt` must be empty (resume uses agent's resume command, not a prompt).
- The agent's resume command is used instead of a prompt (e.g., `claude --continue`, `codex resume --last`).

Response (array of results):

```json
[
  {
    "session_id": "session-id",
    "workspace_id": "workspace-id",
    "target": "target-name",
    "prompt": "optional",
    "nickname": "optional"
  }
]
```

Errors are per-result:

```json
[
  {
    "target": "target-name",
    "error": "..."
  }
]
```

Global errors (HTTP status codes):

- 409 Conflict: Branch already in use by another workspace (worktree mode only). Message: `branch_conflict: branch "X" is already in use by workspace "Y"`

### POST /api/check-branch-conflict

Check if a branch is already in use by an existing workspace. Used by the UI to validate before spawn in worktree mode.

Request:

```json
{
  "repo": "git@github.com:user/repo.git",
  "branch": "main"
}
```

Response:

```json
{
  "conflict": false
}
```

Or if conflict exists:

```json
{
  "conflict": true,
  "workspace_id": "repo-001"
}
```

Notes:

- Only relevant when `source_code_management` is `"git-worktree"` (the default)
- When `source_code_management` is `"git"`, always returns `{"conflict": false}`

### GET /api/recent-branches

Returns recent branches across all repos, sorted by commit date (most recent first).

Query Parameters:

- `limit` (optional): Maximum number of branches to return (default: 10)

Response:

```json
[
  {
    "repo_url": "git@github.com:user/repo.git",
    "repo_name": "repo",
    "branch": "feature-branch",
    "commit_date": "2026-01-28T15:30:00Z",
    "subject": "Add new feature"
  }
]
```

Notes:

- Uses bare clones to query branch information without worktree checkouts
- Returns branches from all configured repos
- Excludes `main` branch by default

### POST /api/suggest-branch

AI-powered branch name and nickname suggestion from a prompt.

Request:

```json
{
  "prompt": "Add dark mode support to the dashboard"
}
```

Response:

```json
{
  "branch": "add-dark-mode-support",
  "nickname": "Add dark mode support"
}
```

Errors:

- 400 with JSON: `{"error":"Failed to generate branch suggestion: ..."}` (empty prompt, invalid branch/response)
- 404 with JSON: `{"error":"Failed to generate branch suggestion: ..."}` (target not found)
- 503 with JSON: `{"error":"Branch suggestion is not configured"}` (disabled)
- 500 with JSON: `{"error":"Failed to generate branch suggestion: ..."}`

Notes:

- Requires `branch_suggest.target` to be configured
- The target generates both a git-compatible branch name and a human-readable nickname

### POST /api/prepare-branch-spawn

Prepares spawn data for an existing branch. Used when clicking a recent branch on the home page.

Request:

```json
{
  "repo_name": "repo",
  "branch": "feature-branch"
}
```

Response:

```json
{
  "repo": "repo",
  "branch": "feature-branch",
  "prompt": "Review the current state of this branch and prepare to resume work.\n\n...",
  "nickname": "Add new feature"
}
```

Process:

1. Runs `git log --oneline main..{branch}` on the bare clone to get commit messages
2. Passes commit messages to the branch suggestion target to generate a nickname (if configured)
3. Builds a standardized branch review prompt with commit history
4. Returns all data needed to populate the spawn form

Notes:

- Non-fatal errors (e.g., branch suggestion failure) still return a response with empty nickname
- The prompt instructs the agent to review project context, understand changes, and prepare to resume work

### POST /api/sessions/{sessionId}/dispose

Dispose a session.

Response:

```json
{ "status": "ok" }
```

Errors:

- 400: "session ID is required"
- 500: "Failed to dispose session: ..."

### POST /api/workspaces/{workspaceId}/dispose

Dispose a workspace (fails if workspace has active sessions).

Response:

```json
{ "status": "ok" }
```

Errors:

- 400 with JSON: `{"error":"..."}` (e.g., dirty workspace, active sessions)

### POST /api/workspaces/{workspaceId}/dispose-all

Dispose a workspace and all its sessions.

Disposes all sessions in the workspace first, then disposes the workspace itself.

Response:

```json
{ "status": "ok", "sessions_disposed": 3 }
```

Errors:

- 400 with JSON: `{"error":"..."}` (e.g., dirty workspace)

### PUT/PATCH /api/sessions-nickname/{sessionId}

Update a session nickname.

Request:

```json
{ "nickname": "new name" }
```

Response:

```json
{ "status": "ok" }
```

Errors:

- 409 with JSON: `{"error":"nickname already in use"}`
- 500: "Failed to rename session: ..."

### GET /api/config

Returns the current config.

Response:

```json
{
  "workspace_path": "/path",
  "source_code_management": "git-worktree",
  "repos": [{ "name": "repo", "url": "https://..." }],
  "run_targets": [{ "name": "target", "type": "promptable", "command": "...", "source": "user" }],
  "quick_launch": [{ "name": "preset", "target": "target", "prompt": "optional" }],
  "models": [
    {
      "id": "claude-sonnet",
      "display_name": "Claude Sonnet 4.5",
      "base_tool": "claude",
      "provider": "anthropic",
      "category": "native",
      "required_secrets": [],
      "usage_url": "",
      "configured": true
    }
  ],
  "nudgenik": { "target": "optional", "viewed_buffer_ms": 0, "seen_interval_ms": 0 },
  "terminal": { "width": 0, "height": 0, "seed_lines": 0, "bootstrap_lines": 0 },
  "sessions": {
    "dashboard_poll_interval_ms": 0,
    "git_status_poll_interval_ms": 0,
    "git_clone_timeout_ms": 0,
    "git_status_timeout_ms": 0
  },
  "xterm": {
    "mtime_poll_interval_ms": 0,
    "query_timeout_ms": 0,
    "operation_timeout_ms": 0,
    "max_log_size_mb": 0,
    "rotated_log_size_mb": 0
  },
  "network": {
    "bind_address": "127.0.0.1",
    "port": 7337,
    "public_base_url": "https://schmux.local:7337",
    "tls": {
      "cert_path": "/path/to/schmux.local.pem",
      "key_path": "/path/to/schmux.local-key.pem"
    }
  },
  "access_control": {
    "enabled": false,
    "provider": "github",
    "session_ttl_minutes": 1440
  },
  "needs_restart": false
}
```

### POST/PUT /api/config

Update the config. All fields are optional; omitted fields are unchanged.

Request:

```json
{
  "workspace_path": "/path",
  "source_code_management": "git-worktree",
  "repos": [{ "name": "repo", "url": "https://..." }],
  "run_targets": [{ "name": "target", "type": "promptable", "command": "...", "source": "user" }],
  "quick_launch": [{ "name": "preset", "target": "target", "prompt": "optional" }],
  "models": [
    {
      "id": "claude-sonnet",
      "display_name": "Claude Sonnet 4.5",
      "base_tool": "claude",
      "provider": "anthropic",
      "category": "native",
      "required_secrets": [],
      "usage_url": "",
      "configured": true
    }
  ],
  "nudgenik": { "target": "optional", "viewed_buffer_ms": 0, "seen_interval_ms": 0 },
  "terminal": { "width": 120, "height": 30, "seed_lines": 1000, "bootstrap_lines": 200 },
  "sessions": {
    "dashboard_poll_interval_ms": 0,
    "git_status_poll_interval_ms": 0,
    "git_clone_timeout_ms": 0,
    "git_status_timeout_ms": 0
  },
  "xterm": {
    "mtime_poll_interval_ms": 0,
    "query_timeout_ms": 0,
    "operation_timeout_ms": 0,
    "max_log_size_mb": 0,
    "rotated_log_size_mb": 0
  },
  "network": {
    "bind_address": "127.0.0.1",
    "port": 7337,
    "public_base_url": "https://schmux.local:7337",
    "tls": {
      "cert_path": "/path/to/schmux.local.pem",
      "key_path": "/path/to/schmux.local-key.pem"
    }
  },
  "access_control": {
    "enabled": false,
    "provider": "github",
    "session_ttl_minutes": 1440
  }
}
```

Response:

- 200: `{"status":"ok","message":"Config saved and reloaded. Changes are now in effect.","warnings":["optional warnings"]}`
- 200 (warning when workspace_path changes with existing sessions/workspaces):

```json
{
  "warning": "...",
  "session_count": 0,
  "workspace_count": 0,
  "requires_restart": true,
  "warnings": ["optional warnings"]
}
```

Errors:

- 400 for validation errors (plain text)
- 500 for save/reload errors (plain text)

### GET /api/auth/secrets

Returns whether GitHub auth secrets are configured (values are not returned).

Response:

```json
{
  "client_id_set": true,
  "client_secret_set": true
}
```

### POST /api/auth/secrets

Saves GitHub auth secrets.

Request:

```json
{
  "client_id": "...",
  "client_secret": "..."
}
```

Response:

```json
{ "status": "ok" }
```

Errors:

- 400 for missing secrets (plain text)
- 500 for save errors (plain text)

### GET /api/detect-tools

Returns detected run targets.

Response:

```json
{
  "tools": [{ "name": "tool", "command": "...", "source": "config" }]
}
```

### GET /api/models

Lists available models and whether they are configured (provider-scoped secrets apply).

Response:

```json
{
  "models": [
    {
      "id": "claude-sonnet",
      "display_name": "claude sonnet 4.5",
      "base_tool": "claude",
      "provider": "anthropic",
      "category": "native",
      "required_secrets": [],
      "usage_url": "",
      "configured": true
    },
    {
      "id": "kimi-thinking",
      "display_name": "kimi k2 thinking",
      "base_tool": "claude",
      "provider": "moonshot",
      "category": "third-party",
      "required_secrets": ["ANTHROPIC_AUTH_TOKEN"],
      "usage_url": "https://platform.moonshot.ai/console/account",
      "configured": false
    },
    {
      "id": "kimi-k2.5",
      "display_name": "kimi k2.5",
      "base_tool": "claude",
      "provider": "moonshot",
      "category": "third-party",
      "required_secrets": ["ANTHROPIC_AUTH_TOKEN"],
      "usage_url": "https://platform.moonshot.ai/console/account",
      "configured": false
    }
  ]
}
```

### GET /api/models/{id}/configured

Response:

```json
{ "configured": true }
```

### POST /api/models/{id}/secrets

Set secrets for a third-party model (shared across all models for that provider).

Request:

```json
{ "secrets": { "KEY": "VALUE" } }
```

Response:

```json
{ "status": "ok" }
```

Errors:

- 400: missing secrets or invalid payload (plain text)
- 500: "Failed to save secrets: ..."

### DELETE /api/models/{id}/secrets

Delete secrets for a third-party model (clears provider secrets).

Response:

```json
{ "status": "ok" }
```

Errors:

- 400: "model is in use by nudgenik or quick launch"

### GET /api/builtin-quick-launch

Returns built-in quick launch presets.

Response:

```json
[{ "name": "Preset", "target": "target", "prompt": "prompt text" }]
```

### GET /api/diff/{workspaceId}

Returns git diff for a workspace (tracked files + untracked).

Response:

```json
{
  "workspace_id": "workspace-id",
  "repo": "repo",
  "branch": "branch",
  "files": [
    {
      "old_path": "optional",
      "new_path": "file",
      "old_content": "optional",
      "new_content": "optional",
      "status": "added|modified|deleted|renamed|untracked"
    }
  ]
}
```

Errors:

- 404: "workspace not found"
- 400: "workspace ID is required"

### POST /api/diff-external/{workspaceId}

Launches an external diff tool for all changed files in a workspace.

Request:

```json
{
  "command": "command-name" // optional; name from configured external_diff_commands, or a raw command string
}
```

Response:

```json
{ "success": true, "message": "Opened 3 files in external diff tool" }
```

Errors:

- 400 with JSON: `{"success":false,"message":"No diff command specified"}` / `{"success":false,"message":"invalid request: ..."}`
- 404 with JSON: `{"success":false,"message":"workspace {id} not found"}` / `{"success":false,"message":"workspace directory does not exist"}`
- 200 with JSON: `{"success":false,"message":"No changes to diff"}` / `{"success":false,"message":"No modified or deleted files to diff"}`

### POST /api/open-vscode/{workspaceId}

Opens VS Code in a new window for the workspace.

Response:

```json
{ "success": true, "message": "You can now switch to VS Code." }
```

Errors:

- 404 with JSON if workspace not found or directory missing
- 404 with JSON if `code` command not found in PATH
- 500 with JSON on launch failure

### POST /api/workspaces/{workspaceId}/linear-sync-from-main

Syncs commits from `origin/main` into the workspace's current branch via iterative cherry-pick.

Response:

```json
{
  "success": true,
  "message": "Synced 3 commits from main into feature-branch"
}
```

Errors:

- 400: "workspace ID is required"
- 404 with JSON: `{"success":false,"message":"workspace {id} not found"}`
- 500 with JSON: `{"success":false,"message":"Failed to sync from main: ..."}`

Notes:

- Handles both behind and diverged branch states
- Aborts if conflicts are detected during cherry-pick
- Preserves local changes via temporary WIP commit
- Updates workspace git status after sync

### POST /api/workspaces/{workspaceId}/linear-sync-to-main

Pushes the workspace's branch commits directly to `origin/main` via fast-forward.

Response:

```json
{
  "success": true,
  "message": "Pushed 2 commits to main"
}
```

Errors:

- 400: "workspace ID is required"
- 404 with JSON: `{"success":false,"message":"workspace {id} not found"}`
- 409 with JSON: `{"success":false,"message":"workspace has uncommitted changes"}` or `"workspace is behind main"`
- 500 with JSON: `{"success":false,"message":"Failed to sync to main: ..."}`

Notes:

- Requires clean workspace state (no uncommitted changes, not behind main)
- Fast-forward only—no merge commits
- Updates workspace git status after sync
- Supports both on-main and feature-branch workflows

### POST /api/workspaces/{workspaceId}/push-to-branch

Pushes the workspace's current branch commits to `origin/{branch}`, creating the remote branch if necessary.

Response:

```json
{
  "success": true,
  "success_count": 2,
  "branch": "feature-branch"
}
```

Errors:

- 400: "workspace ID is required"
- 404 with JSON: `{"success":false,"message":"workspace {id} not found"}`
- 500 with JSON: `{"success":false,"message":"Failed to push to branch: ..."}`

Notes:

- Fails if the remote branch has commits that the local branch does not have
- Updates workspace git status after push

### GET /api/workspaces/{workspaceId}/git-graph

Returns the git commit graph for a workspace, including branch topology and dirty state.

Query Parameters:

- `max_total` (optional): Maximum total commits to display (default: 200). Also accepts `max_commits` for backward compatibility.
- `main_context` (optional): Number of commits on main before fork point (default: 5). Also accepts `context` for backward compatibility.

Response:

```json
{
  "repo": "repo-url",
  "nodes": [
    {
      "hash": "abc123...",
      "short_hash": "abc123",
      "message": "Add feature",
      "author": "user",
      "timestamp": "2025-01-15T10:00:00Z",
      "parents": ["def456..."],
      "branches": ["feature-branch"],
      "is_head": ["feature-branch"],
      "workspace_ids": ["schmux-001"]
    }
  ],
  "branches": {
    "feature-branch": {
      "head": "abc123...",
      "is_main": false,
      "workspace_ids": ["schmux-001"]
    }
  },
  "main_ahead_count": 3,
  "dirty_state": {
    "files_changed": 2,
    "lines_added": 10,
    "lines_removed": 5
  }
}
```

Errors:

- 400: "workspace ID is required"
- 404 with JSON: `{"error":"workspace not found: {id}"}`
- 500 with JSON: `{"error":"..."}`

Notes:

- `dirty_state` is only included when there are uncommitted changes
- Delegates to remote handler for remote workspaces

### POST /api/workspaces/{workspaceId}/git-commit-stage

Stages the specified files (runs `git add` for each file).

Request:

```json
{
  "files": ["path/to/file1.go", "path/to/file2.go"]
}
```

Response:

```json
{ "success": true, "message": "Files staged" }
```

Errors:

- 400 with JSON: `{"error":"workspace ID is required"}` / `{"error":"invalid request body"}` / `{"error":"invalid file path: \"...\""}`
- 404 with JSON: `{"error":"workspace not found"}`
- 500 with JSON: `{"error":"git add failed: ..."}`

Notes:

- File paths must be relative and cannot contain path traversal (`..`)
- Updates workspace git status and broadcasts after staging

### POST /api/workspaces/{workspaceId}/git-amend

Stages the specified files and amends the last commit (`git commit --amend --no-edit`).

Request:

```json
{
  "files": ["path/to/file1.go"]
}
```

Response:

```json
{ "success": true, "message": "Commit amended" }
```

Errors:

- 400 with JSON: `{"error":"workspace ID is required"}` / `{"error":"No commits to amend"}` / `{"error":"at least one file is required"}` / `{"error":"invalid file path: \"...\""}`
- 404 with JSON: `{"error":"workspace not found"}`
- 500 with JSON: `{"error":"git add failed: ..."}` / `{"error":"git commit --amend failed: ..."}`

Notes:

- Requires at least one unpushed commit (`git_ahead > 0`)
- At least one file must be specified
- Updates workspace git status and broadcasts after amend

### POST /api/workspaces/{workspaceId}/git-discard

Discards local changes. If `files` are specified, only those files are discarded. If `files` is empty or body is omitted, all changes are discarded.

Request (optional):

```json
{
  "files": ["path/to/file.go"]
}
```

Response:

```json
{ "success": true, "message": "Changes discarded" }
```

Errors:

- 400 with JSON: `{"error":"workspace ID is required"}` / `{"error":"invalid request body"}` / `{"error":"invalid file path: \"...\""}`
- 404 with JSON: `{"error":"workspace not found"}`
- 500 with JSON: `{"error":"git clean failed: ..."}` / `{"error":"git checkout failed: ..."}`

Notes:

- Per-file discard tries `git checkout HEAD -- {file}` first, then `git rm --cached` + working tree removal, then `git clean -f` as a last resort
- Discard-all runs `git clean -fd` followed by `git checkout -- .`
- Updates workspace git status and broadcasts after discard

### POST /api/workspaces/{workspaceId}/git-uncommit

Resets the HEAD commit, keeping changes as unstaged (`git reset HEAD~1`). Requires a `hash` parameter to verify we are uncommitting the expected commit.

Request:

```json
{
  "hash": "abc123def456..."
}
```

Response:

```json
{ "success": true, "message": "Commit undone, changes are now unstaged" }
```

Errors:

- 400 with JSON: `{"error":"workspace ID is required"}` / `{"error":"No commits to uncommit"}` / `{"error":"hash is required"}`
- 404 with JSON: `{"error":"workspace not found"}`
- 409 with JSON: `{"error":"HEAD has changed, please refresh and try again"}`
- 500 with JSON: `{"error":"failed to get current HEAD"}` / `{"error":"git reset failed: ..."}`

Notes:

- Requires at least one unpushed commit (`git_ahead > 0`)
- The `hash` must match the current HEAD to prevent accidental uncommit of a different commit
- Updates workspace git status and broadcasts after uncommit

### POST /api/workspaces/{workspaceId}/linear-sync-resolve-conflict

Starts an asynchronous conflict resolution for a workspace. Returns immediately with 202; progress is streamed via the `/ws/dashboard` WebSocket.

Response (202):

```json
{
  "started": true,
  "workspace_id": "workspace-id"
}
```

Errors:

- 400: "workspace ID is required"
- 404 with JSON: `{"started":false,"message":"workspace {id} not found"}`
- 409 with JSON: `{"started":false,"message":"operation already in progress"}`

Notes:

- Progress steps are broadcast as `linear_sync_resolve_conflict` messages on the `/ws/dashboard` WebSocket
- Auto-clears completed/failed state on new request
- Clears `conflict_on_branch` on successful resolution
- Pauses Vite file watching during resolution to avoid transform errors

### DELETE /api/workspaces/{workspaceId}/linear-sync-resolve-conflict-state

Dismisses a completed or failed conflict resolution state.

Response (200): empty body

Errors:

- 400: "workspace ID is required"
- 404: no conflict resolution state found
- 409 with JSON: `{"message":"operation still in progress"}`

### GET /api/prs

Returns cached GitHub pull requests from the last discovery run.

Response:

```json
{
  "prs": [
    {
      "number": 42,
      "title": "Add feature X",
      "body": "...",
      "state": "open",
      "repo_name": "schmux",
      "repo_url": "git@github.com:user/schmux.git",
      "source_branch": "feature-x",
      "target_branch": "main",
      "author": "someone",
      "created_at": "2025-01-15T10:00:00Z",
      "html_url": "https://github.com/user/schmux/pull/42",
      "is_fork": false
    }
  ],
  "last_fetched_at": "2025-01-15T12:00:00Z",
  "error": ""
}
```

Notes:

- PR discovery only runs when `pr_review.target` is configured in your config
- Automatic polling is enabled only when PR discovery is needed (e.g., after config change or manual refresh)
- On daemon startup, PRs are discovered if the target is configured
- Only public GitHub repos are queried (unauthenticated API, 60 req/hour limit)
- Limited to 5 open PRs per repo

### POST /api/prs/refresh

Re-runs PR discovery against GitHub. Same response shape as GET /api/prs with additional fields:

Response:

```json
{
  "prs": [...],
  "fetched_count": 3,
  "error": "",
  "retry_after_sec": null
}
```

Notes:

- `retry_after_sec` is set when rate limited by GitHub

### POST /api/prs/checkout

Creates a workspace from a PR ref and launches a review session.

Request:

```json
{
  "repo_url": "git@github.com:user/repo.git",
  "pr_number": 42
}
```

Response:

```json
{
  "workspace_id": "repo-001",
  "session_id": "abc123"
}
```

Process:

1. Looks up PR metadata from discovery cache
2. Fetches `refs/pull/{number}/head` into the bare clone
3. Creates workspace on branch `pr/{number}` (or `pr/{fork-owner}/{number}` for forks)
4. Launches session using `pr_review.target` with PR context as prompt
5. Returns workspace and session IDs for navigation

Errors:

- 400: "repo_url and pr_number are required"
- 404: "PR #N not found for URL" (PR not in discovery cache)
- 400: "No pr_review target configured"
- 500: "Failed to checkout PR: ..." or "Workspace created but session launch failed: ..."

### GET /api/overlays

Returns overlay information for all repos.

Response:

```json
{
  "overlays": [{ "repo_name": "repo", "path": "/path", "exists": true, "file_count": 0 }]
}
```

## WebSocket

### WS /ws/terminal/{sessionId}

Streams terminal output for a session.

Client -> server messages:

```json
{"type":"pause","data":""}
{"type":"resume","data":""}
{"type":"input","data":"raw-bytes-or-escape-seqs"}
{"type":"resize","data":"{\"cols\":120,\"rows\":30}"}
```

Server -> client messages:

```json
{"type":"full","content":"..."}       // initial full content (with ANSI state)
{"type":"append","content":"..."}     // incremental content
{"type":"displaced","content":"..."}  // connection displaced by another window
{"type":"reconnect","content":"Log rotated, please reconnect"}
```

Errors:

- 400: "session ID is required"
- 410: "session not running"

### WS /ws/dashboard

Real-time dashboard state updates via WebSocket. Sends the full workspace/session state on connect, then pushes updates whenever state changes.

Server -> client messages:

Sessions update:

```json
{
  "type": "sessions",
  "workspaces": [...]
}
```

The `workspaces` array has the same shape as the `GET /api/sessions` response.

Conflict resolution progress (sent as separate messages when active):

```json
{
  "type": "linear_sync_resolve_conflict",
  "workspace_id": "workspace-id",
  "status": "in_progress",
  "hash": "",
  "started_at": "2025-01-15T10:00:00Z",
  "finished_at": "",
  "message": "",
  "steps": [
    {
      "action": "cherry_pick",
      "status": "in_progress",
      "message": "...",
      "at": "2025-01-15T10:00:01Z",
      "local_commit": "abc123",
      "local_commit_message": "Add feature",
      "files": ["file.go"],
      "confidence": "high",
      "summary": "..."
    }
  ],
  "resolutions": []
}
```

Notes:

- Uses trailing debounce (100ms) to coalesce rapid changes into single broadcasts
- No client-to-server messages expected; the connection is kept alive by reading

### WS /ws/provision/{provisionId}

Streams PTY I/O for remote host provisioning. Provides interactive terminal access during remote host setup.

Client -> server messages:

```json
{"type":"input","data":"raw-bytes"}
{"type":"resize","data":"{\"cols\":120,\"rows\":30}"}
```

Also accepts binary WebSocket messages as direct PTY input.

Server -> client messages: binary WebSocket messages (raw PTY output).

Errors:

- 400: "provision ID is required" / "invalid provision ID format"
- 404: "remote host connection not found"
- 503: "remote workspace support not enabled" / "provisioning terminal not available"

## Remote Workspace API

### GET /api/config/remote-flavors

Returns all configured remote flavors.

Response:

```json
[
  {
    "id": "flavor-id",
    "flavor": "devserver",
    "display_name": "Dev Server",
    "vcs": "git",
    "workspace_path": "/home/user/workspaces",
    "connect_command": "ssh {hostname}",
    "reconnect_command": "ssh {hostname}",
    "provision_command": "setup.sh",
    "hostname_regex": "dev-.*",
    "vscode_command_template": "code --remote ssh-remote+{hostname} {path}"
  }
]
```

### POST /api/config/remote-flavors

Creates a new remote flavor.

Request:

```json
{
  "flavor": "devserver",
  "display_name": "Dev Server",
  "vcs": "git",
  "workspace_path": "/home/user/workspaces",
  "connect_command": "ssh {hostname}",
  "reconnect_command": "ssh {hostname}",
  "provision_command": "setup.sh",
  "hostname_regex": "dev-.*",
  "vscode_command_template": "code --remote ssh-remote+{hostname} {path}"
}
```

Response: the created `RemoteFlavorResponse` object (same shape as GET items).

Errors:

- 400: "Invalid request body" or validation error (plain text)
- 500: "Failed to save config"

### GET /api/config/remote-flavors/{id}

Returns a single remote flavor by ID.

Response: a `RemoteFlavorResponse` object.

Errors:

- 400: "Flavor ID required"
- 404: "Flavor not found"

### PUT /api/config/remote-flavors/{id}

Updates an existing remote flavor. The `flavor` field is immutable.

Request:

```json
{
  "display_name": "Updated Name",
  "vcs": "git",
  "workspace_path": "/home/user/workspaces",
  "connect_command": "ssh {hostname}",
  "reconnect_command": "ssh {hostname}",
  "provision_command": "setup.sh",
  "hostname_regex": "dev-.*",
  "vscode_command_template": "code --remote ssh-remote+{hostname} {path}"
}
```

Response: the updated `RemoteFlavorResponse` object.

Errors:

- 400: "Invalid request body" or validation error (plain text)
- 404: "Flavor not found"
- 500: "Failed to save config"

### DELETE /api/config/remote-flavors/{id}

Deletes a remote flavor.

Response: 204 No Content

Errors:

- 404: error message (plain text)
- 500: "Failed to save config"

### GET /api/remote/hosts

Returns all remote hosts with their connection status.

Response:

```json
[
  {
    "id": "remote-abc123",
    "flavor_id": "flavor-id",
    "display_name": "Dev Server",
    "hostname": "dev-001.example.com",
    "uuid": "...",
    "status": "connected",
    "provisioned": true,
    "vcs": "git",
    "connected_at": "2025-01-15T10:00:00Z",
    "expires_at": "2025-01-16T10:00:00Z",
    "provisioning_session_id": ""
  }
]
```

Notes:

- `display_name` and `vcs` are resolved from the flavor configuration
- `provisioning_session_id` is set when a provisioning terminal is active (for WebSocket connection)

### POST /api/remote/hosts/connect

Starts a connection to a remote host asynchronously. Returns immediately; poll `/api/remote/hosts` for status updates.

Request:

```json
{
  "flavor_id": "flavor-id"
}
```

Response (200, if already connected): a `RemoteHostResponse` object with current connection state.

Response (202, if connecting):

```json
{
  "flavor_id": "flavor-id",
  "display_name": "Dev Server",
  "status": "provisioning",
  "vcs": "git",
  "provisioning_session_id": "provision-remote-abc123"
}
```

Errors:

- 400: "Invalid request body" / "flavor_id is required"
- 404: "Flavor not found: {id}"
- 429: "Rate limit exceeded. Max 3 connection attempts per minute."
- 500: "Failed to start connection: ..."
- 503: "Remote workspace support not enabled"

### POST /api/remote/hosts/{id}/reconnect

Starts reconnection to an existing remote host asynchronously. Returns a provisioning session ID for interactive auth via WebSocket.

Response (202):

```json
{
  "id": "remote-abc123",
  "flavor_id": "flavor-id",
  "display_name": "Dev Server",
  "hostname": "dev-001.example.com",
  "status": "reconnecting",
  "vcs": "git",
  "provisioning_session_id": "provision-remote-abc123"
}
```

Errors:

- 400: "Invalid path"
- 404: "Host not found"
- 500: "Failed to start reconnection: ..."
- 503: "Remote workspace support not enabled"

### DELETE /api/remote/hosts/{id}

Disconnects and removes a remote host.

Response: 204 No Content

Errors:

- 400: "Host ID required"
- 500: "Failed to update host: ..." / "Failed to save state"

### GET /api/remote/flavor-statuses

Returns all flavors with their real-time connection status.

Response:

```json
[
  {
    "flavor": {
      "id": "flavor-id",
      "flavor": "devserver",
      "display_name": "Dev Server",
      "vcs": "git",
      "workspace_path": "/home/user/workspaces",
      "connect_command": "ssh {hostname}",
      "reconnect_command": "ssh {hostname}",
      "provision_command": "setup.sh"
    },
    "connected": true,
    "status": "connected",
    "hostname": "dev-001.example.com",
    "host_id": "remote-abc123"
  }
]
```

Notes:

- `status` can be `"provisioning"`, `"connecting"`, `"connected"`, or `"disconnected"`
- Uses real-time connection status from the remote manager when available; falls back to persisted state

## Dev Mode Endpoints

These endpoints are only registered when the daemon is started with `--dev-mode` (via `./dev.sh`).

### GET /api/dev/status

Returns the current dev mode state.

Response:

```json
{
  "active": true,
  "source_workspace": "/path/to/current/worktree",
  "last_build": {
    "success": true,
    "workspace_path": "/path/to/worktree",
    "error": "",
    "at": "2025-01-15T10:30:00Z"
  }
}
```

### POST /api/dev/rebuild

Triggers a dev mode rebuild/restart for a workspace. The daemon writes a restart manifest, responds, then exits with code 42. The wrapper script (`dev.sh`) reads the manifest and rebuilds/restarts accordingly.

Request:

```json
{
  "workspace_id": "schmux-003",
  "type": "frontend|backend|both"
}
```

Response:

```json
{ "status": "rebuilding" }
```

Errors:

- 400: missing workspace_id, invalid type
- 404: workspace not found

### GET /api/healthz (dev mode extension)

When dev mode is active, the healthz response includes an additional field:

```json
{
  "status": "ok",
  "version": "dev",
  "dev_mode": true
}
```
