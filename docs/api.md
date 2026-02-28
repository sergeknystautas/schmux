# API Contract

This document defines the daemon HTTP API contract. It is intentionally client-agnostic. If behavior changes, update this doc first and treat any divergence as a breaking change.

Base URL: `http://localhost:7337` (or `https://<public_base_url>` when auth is enabled)

Doc-gate policy:

- Any API-affecting code change must update `docs/api.md`. CI enforces this rule.
- Internal refactorings that touch API packages without changing the API surface still bump this file to satisfy the doc gate.

General conventions:

- JSON requests/responses use `Content-Type: application/json`.
- Many error responses use plain text via `http.Error`; do not assume JSON unless specified.
- CORS: when TLS is disabled, requests are allowed from `http://localhost:7337` and `http://127.0.0.1:7337`. When TLS is enabled, the scheme switches to `https`. When `bind_address` is `0.0.0.0`, any origin is allowed. Allowed methods: `GET, POST, DELETE, PUT, PATCH, OPTIONS`.
- When auth is enabled, CORS is restricted to the derived allowed origins (must include `public_base_url`) and `Access-Control-Allow-Credentials: true` is set.
- Resource ID validation: workspace IDs and lore repo names in URL parameters are validated (no path separators, dots, null bytes, max 128 chars). Invalid values return `400 Bad Request`.
- When auth is enabled, all `/api/*` and `/ws/*` endpoints require authentication.
- Trusted request bypass: when `remote_access` is not enabled in config, all requests are considered trusted and bypass tunnel auth checks. When `remote_access` is enabled, only loopback requests without tunnel forwarding headers (`Cf-Connecting-IP`, `X-Forwarded-For`) are trusted.

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

### GET /api/debug/tmux-leak

Dev diagnostics endpoint returning simple tmux counts for the sidebar:

- `tmux_sessions.count` — `tmux list-sessions` line count
- `os_processes.attach_session_process_count` — `ps` count of tmux `attach-session` processes
- `os_processes.tmux_process_count` — `ps` count of tmux-related processes

Errors from `tmux`/`ps` are reported in `tmux_sessions_error` / `ps_error` fields when present.

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
    "repo_name": "optional-configured-name",
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
        "nudge_summary": "optional",
        "persona_id": "optional",
        "persona_icon": "optional",
        "persona_color": "optional",
        "persona_name": "optional"
      }
    ],
    "previews": [
      {
        "id": "prev_ab12cd34",
        "workspace_id": "workspace-id",
        "target_host": "127.0.0.1",
        "target_port": 5173,
        "proxy_port": 53000,
        "status": "ready"
      }
    ]
  }
]
```

Notes:

- `last_output_at` is an in-memory runtime signal and resets after daemon restart.
- `last_output_at` may be omitted when no activity has been observed since daemon start.
- `repo_name` is the configured repo name from `config.json`, populated when the workspace repo URL matches a configured repo. May be empty for workspaces from unconfigured repos.
- `nudge_state` values: `Working`, `Idle`, `Needs Input`, `Needs Attention`, `Needs Feature Clarification`, `Completed`, `Error`. State priority prevents lower-tier states from overwriting higher-tier ones: tier 0 (Working, Idle) < tier 1 (Needs Input, Needs Attention) < tier 2 (Completed, Error). Only `Working` can reset a terminal state (new turn started).
- Unrecognized workspace sub-routes return 404.

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
  "proxy_port": 53000,
  "status": "ready"
}
```

The frontend constructs the preview URL using `window.location.hostname` and `proxy_port`.

Notes:

- `proxy_port` is stable: each workspace gets a fixed port block and the same `(workspace, target_host, target_port)` tuple always maps to the same port, surviving daemon restarts.
- Target host must be loopback only (`127.0.0.1`, `::1`, `localhost`).
- Remote workspaces return 422.
- Preview listeners follow the daemon's `bind_address`: loopback in default mode, `0.0.0.0` in network-access mode.
- `status` is `ready` when upstream is reachable, `degraded` when not.

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
  "resume": false,
  "persona_id": "optional",
  "image_attachments": ["base64-encoded-png", "..."]
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
- `persona_id` is optional. When set, the persona's system prompt is injected into the agent at spawn time (e.g., via `--append-system-prompt-file` for Claude). The persona ID is stored on the session and used to display persona badges in the dashboard.
- `image_attachments` is optional. Array of base64-encoded PNG strings (max 5). Images are decoded and written to `{workspace}/.schmux/attachments/img-{uuid}.png`. Absolute file paths are appended to the prompt so the agent can reference them. Cannot be used with `resume`, `command`, or `remote_flavor_id`.

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

### POST /api/recent-branches/refresh

Fetches the latest branch information from all configured remote repositories and returns updated recent branches.

Response:

```json
{
  "branches": [
    {
      "repo_url": "git@github.com:user/repo.git",
      "repo_name": "repo",
      "branch": "feature-branch",
      "commit_date": "2026-01-28T15:30:00Z",
      "subject": "Add new feature"
    }
  ],
  "fetched_count": 5
}
```

Notes:

- Performs `git fetch` on all origin query repos to get latest branch information
- Returns the same branch list format as `GET /api/recent-branches`
- `fetched_count` indicates how many branches were returned
- Useful for refreshing the branch list when remote changes may have occurred

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

### POST /api/sessions/{sessionId}/tell

Send a message to a session's terminal. The message is prefixed with `[from FM]` server-side and typed into the agent's stdin.

Request:

```json
{
  "message": "focus on the auth module first"
}
```

Response:

```json
{ "status": "ok" }
```

Errors:

- 400: "invalid request body", "message is required"
- 404: "session not found"
- 409: "session is not running"
- 500: "failed to send message: ..."
- 503: "remote manager not available", "remote host not connected"

### GET /api/sessions/{sessionId}/events

Get event history for a session.

Query parameters:

- `type` (optional): Filter by event type (`status`, `failure`, `reflection`, `friction`)
- `last` (optional): Return only the last N events

Response:

```json
[
  {
    "ts": "2024-01-15T14:32:01Z",
    "type": "status",
    "state": "working",
    "message": "Session spawned",
    "intent": "Implement OAuth2 token refresh"
  }
]
```

Errors:

- 404: "session not found", "workspace not found"
- 500: "failed to read events: ..."
- 503: "remote manager not available", "remote host not connected"

### GET /api/sessions/{sessionId}/capture

Capture recent terminal output from a session's tmux pane.

Query parameters:

- `lines` (optional, default: 50): Number of lines to capture

Response:

```json
{
  "session_id": "schmux-001-abc12345",
  "lines": 50,
  "output": "... terminal output ..."
}
```

Errors:

- 404: "session not found"
- 409: "session is not running"
- 500: "failed to capture output: ..."
- 503: "remote manager not available", "remote host not connected"

### GET /api/workspaces/{workspaceId}/inspect

Full VCS state report for a workspace.

Response:

```json
{
  "workspace_id": "schmux-001",
  "repo": "schmux",
  "branch": "feature/oauth-refresh",
  "pushed": true,
  "remote_branch": "origin/feature/oauth-refresh",
  "ahead_main": 5,
  "behind_main": 0,
  "commits": ["a1b2c3d Add token refresh endpoint"],
  "uncommitted": ["M internal/auth/refresh.go"]
}
```

Errors:

- 404: "workspace not found"
- 503: "remote manager not available", "remote host not connected"

### GET /api/branches

Bird's-eye view of all workspaces with branch info, sync status, and session states.

Response:

```json
[
  {
    "workspace_id": "schmux-001",
    "repo": "schmux",
    "branch": "feature/oauth-refresh",
    "ahead_main": 5,
    "behind_main": 0,
    "pushed": true,
    "dirty": true,
    "session_count": 2,
    "session_states": ["working", "needs_input"]
  }
]
```

### POST /api/clipboard-paste

Paste an image from the browser clipboard into a tmux session.

Writes the image to the system clipboard (macOS only via osascript) and sends Ctrl+V (0x16) to the specified tmux session so the terminal application picks up the image.

Request (max 10MB body):

```json
{
  "sessionId": "session-uuid",
  "imageBase64": "iVBORw0KGgoAAAANSUhEUgAA..."
}
```

Response:

```json
{ "status": "ok" }
```

Errors:

- 400: "method not allowed", "invalid request body", "sessionId and imageBase64 are required", "invalid base64 image data"
- 404: "session not found"
- 500: "failed to process image", "failed to set clipboard: ...", "remote manager not configured", "session tracker not found", "failed to send input"
- 503: "remote host not connected"

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
  "sessions": {
    "dashboard_poll_interval_ms": 0,
    "git_status_poll_interval_ms": 0,
    "git_clone_timeout_ms": 0,
    "git_status_timeout_ms": 0,
    "dispose_grace_period_ms": 0
  },
  "xterm": {
    "query_timeout_ms": 0,
    "operation_timeout_ms": 0
  },
  "network": {
    "bind_address": "127.0.0.1",
    "port": 7337,
    "public_base_url": "https://schmux.local:7337",
    "tls": {
      "cert_path": "/path/to/schmux.local.pem",
      "key_path": "/path/to/schmux.local-key.pem"
    },
    "dashboardsx": {
      "enabled": false,
      "code": "",
      "email": "",
      "ip": "",
      "service_url": ""
    }
  },
  "access_control": {
    "enabled": false,
    "provider": "github",
    "session_ttl_minutes": 1440
  },
  "telemetry_enabled": true,
  "installation_id": "uuid-string",
  "remote_access": {
    "disabled": false,
    "timeout_minutes": 0,
    "password_hash_set": false,
    "notify": {
      "ntfy_topic": "",
      "command": ""
    }
  },
  "floor_manager": {
    "enabled": false,
    "target": "claude",
    "rotation_threshold": 150,
    "debounce_ms": 2000
  },
  "notifications": {
    "sound_disabled": false,
    "confirm_before_close": false,
    "suggest_dispose_after_push": true
  },
  "needs_restart": false
}
```

**TLS behavior**: The server serves HTTPS whenever `network.tls.cert_path` and `network.tls.key_path` are both set, regardless of whether `access_control.enabled` is true. This allows dashboard.sx HTTPS without requiring GitHub auth.

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
  "sessions": {
    "dashboard_poll_interval_ms": 0,
    "git_status_poll_interval_ms": 0,
    "git_clone_timeout_ms": 0,
    "git_status_timeout_ms": 0,
    "dispose_grace_period_ms": 0
  },
  "xterm": {
    "query_timeout_ms": 0,
    "operation_timeout_ms": 0
  },
  "network": {
    "bind_address": "127.0.0.1",
    "port": 7337,
    "public_base_url": "https://schmux.local:7337",
    "tls": {
      "cert_path": "/path/to/schmux.local.pem",
      "key_path": "/path/to/schmux.local-key.pem"
    },
    "dashboardsx": {
      "enabled": false,
      "code": "",
      "ip": "",
      "service_url": ""
    }
  },
  "access_control": {
    "enabled": false,
    "provider": "github",
    "session_ttl_minutes": 1440
  },
  "lore": {
    "enabled": true,
    "llm_target": "",
    "auto_pr": false,
    "curate_on_dispose": "session",
    "curate_debounce_ms": 30000,
    "prune_after_days": 30,
    "instruction_files": ["CLAUDE.md", "AGENTS.md"]
  },
  "remote_access": {
    "disabled": false,
    "timeout_minutes": 30,
    "notify": {
      "ntfy_topic": "my-schmux-topic",
      "command": ""
    }
  },
  "subreddit": {
    "target": "claude",
    "hours": 24
  },
  "notifications": {
    "sound_disabled": false,
    "confirm_before_close": false,
    "suggest_dispose_after_push": true
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

### POST /api/tls/validate

Validates TLS certificate and key paths without modifying configuration. Used by the UI to preview certificate details before saving.

Request:

```json
{
  "cert_path": "/path/to/cert.pem",
  "key_path": "/path/to/key.pem"
}
```

Response (success):

```json
{
  "valid": true,
  "hostname": "schmux.local",
  "expires": "2026-12-25T00:00:00Z"
}
```

Response (error):

```json
{
  "valid": false,
  "error": "Certificate file not found: /path/to/cert.pem"
}
```

Notes:

- Expands `~` to home directory in paths
- Validates that both files exist and are readable
- Validates that the certificate and key match (can be loaded as a pair)
- Extracts hostname from Subject Alternative Names (SAN) or falls back to Common Name (CN)
- Returns expiry date in RFC3339 format

### GET /api/dashboardsx/callback

Handles the OAuth callback from dashboard.sx after the user authenticates with GitHub. The browser is redirected here with a one-time `callback_token`. The handler exchanges it for registration info, provisions a TLS certificate via automated DNS-01 challenge, and updates the config.

Query parameters:

- `callback_token` (required): one-time token from dashboard.sx (expires after 5 minutes)

Response: HTML page confirming setup is complete, with instructions to restart the daemon.

Errors:

- 400 if `callback_token` is missing
- 403 if the returned instance key doesn't match the local one
- 502 if the token exchange or DNS provider setup fails
- 500 if cert provisioning or config save fails

### GET /api/auth/secrets

Returns whether GitHub auth secrets are configured (values are not returned).

Response:

```json
{
  "client_id": "Ov23li...",
  "client_secret_set": true
}
```

Notes:

- `client_id` is the actual value (not a boolean) since it's not a secret - it's visible in GitHub OAuth app settings
- `client_secret_set` indicates whether a secret has been configured (the actual secret value is never returned)

### POST /api/auth/secrets

Saves GitHub auth secrets. Supports partial updates.

Request:

```json
{
  "client_id": "...",
  "client_secret": "..."
}
```

Notes:

- `client_id` is required
- `client_secret` is optional; if omitted or empty, keeps the existing secret
- For initial setup, `client_secret` is required (returns 400 if missing and no secret exists)

Response:

```json
{ "status": "ok" }
```

Errors:

- 400 for missing client_id, or missing client_secret on initial setup (plain text)
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
  "command": "command-name" // optional; name from configured external_diff_commands or built-in commands (e.g. "VS Code")
}
```

Response:

```json
{ "success": true, "message": "Opened 3 files in external diff tool" }
```

Errors:

- 400 with JSON: `{"success":false,"message":"No diff command specified"}` / `{"success":false,"message":"Unknown diff command: ..."}` / `{"success":false,"message":"invalid request: ..."}`
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

Syncs commits from `origin/main` into the workspace's current branch via iterative rebase.

Request body:

```json
{
  "hash": "abc123..."
}
```

- `hash` is required and must match the current next commit hash to sync from main.

Response (accepted immediately):

```json
{
  "success": true,
  "message": "sync started",
  "in_progress": true
}
```

Errors:

- 400: "workspace ID is required"
- 400 with JSON: `{"success":false,"message":"hash is required"}`
- 404 with JSON: `{"success":false,"message":"workspace {id} not found"}`
- 409 with JSON: `{"success":false,"message":"workspace is locked by another sync operation"}`
- 409 with JSON on hash mismatch: `{"success":false,"message":"hash mismatch: ...","hash":"...","actual_hash":"..."}`
- 500 with JSON: `{"success":false,"message":"Failed to sync from main: ..."}`

Notes:

- Handles both behind and diverged branch states
- Aborts if conflicts are detected during cherry-pick
- Preserves local changes via temporary WIP commit
- Updates workspace git status after sync
- Lock state changes are broadcast in real-time via the `workspace_locked` WebSocket message
- Rebase progress (current/total commits) is streamed via `workspace_locked` messages with `sync_progress`
- This endpoint now returns immediately (HTTP 202) and runs the sync in the background

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

Pushes the workspace's current branch to `origin/{branch}` using `--force-with-lease`, creating the remote branch if necessary.

Request body (optional):

```json
{
  "confirm": true
}
```

Response (success):

```json
{
  "success": true,
  "success_count": 0,
  "branch": "feature-branch"
}
```

Response (needs confirmation):

```json
{
  "success": false,
  "branch": "feature-branch",
  "needs_confirm": true,
  "diverged_commits": ["abc123 other commit", "def456 another commit"]
}
```

Errors:

- 400: "workspace ID is required"
- 404 with JSON: `{"success":false,"message":"workspace {id} not found"}`
- 500 with JSON: `{"success":false,"message":"Failed to push to branch: ..."}`

Notes:

- Uses `--force-with-lease` for safe force-push after rebase
- Fails if local is behind origin (would overwrite newer remote commits)
- If branches have diverged (e.g., after rebase), returns `needs_confirm: true` with list of commits that would be overwritten
- Call again with `confirm: true` to proceed with force-push
- Updates workspace git status after successful push

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
  "main_ahead_next_hash": "abc123...",
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

### GET /api/workspaces/{workspaceId}/git-commit/{commitHash}

Returns detailed information about a specific commit, including file diffs.

Path Parameters:

- `commitHash`: Short (7-char) or full (40-char) commit hash

Response:

```json
{
  "hash": "abc1234567890abcdef...",
  "short_hash": "abc1234",
  "author_name": "John Doe",
  "author_email": "john@example.com",
  "timestamp": "2026-02-12T15:45:00-08:00",
  "message": "Add new feature\n\nThis is the commit body.",
  "parents": ["def5678..."],
  "is_merge": false,
  "files": [
    {
      "old_path": "src/file.ts",
      "new_path": "src/file.ts",
      "old_content": "old file content...",
      "new_content": "new file content...",
      "status": "modified",
      "lines_added": 10,
      "lines_removed": 2,
      "is_binary": false
    }
  ]
}
```

Errors:

- 400 with JSON: `{"error":"invalid path: ..."}` / `{"error":"invalid commit hash: ..."}`
- 404 with JSON: `{"error":"workspace not found: {id}"}` / `{"error":"commit not found: {hash}"}`
- 501 with JSON: `{"error":"commit detail not yet supported for remote workspaces"}`
- 500 with JSON: `{"error":"..."}`

Notes:

- For merge commits, `is_merge` is true and diff is against first parent only
- Binary files have `is_binary: true` with empty `old_content`/`new_content`
- File content is truncated at 1MB per file
- Commit hash is validated for security (hex chars only, 4-40 characters)

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

### GET /api/github/status

Returns the GitHub CLI (`gh`) authentication status. This is the gate for all GitHub features in the UI.

Response:

```json
{
  "available": true,
  "username": "octocat"
}
```

Fields:

- `available` (bool): Whether the `gh` CLI is installed and authenticated
- `username` (string): The authenticated GitHub username (empty if not available)

The status is checked once at daemon startup and broadcast via the dashboard WebSocket.

### GET /api/overlays

Returns overlay information for all repos.

Response:

```json
{
  "overlays": [{ "repo_name": "repo", "path": "/path", "exists": true, "file_count": 0 }]
}
```

## Floor Manager API

### GET /api/floor-manager

Returns the floor manager status.

Response:

```json
{
  "enabled": true,
  "tmux_session": "schmux-floor-manager",
  "running": true,
  "injection_count": 42,
  "rotation_threshold": 150
}
```

Fields:

- `enabled` — whether floor manager is enabled in config
- `tmux_session` — name of the tmux session (empty if not running)
- `running` — whether the tmux session is alive
- `injection_count` — number of signal injections in the current shift
- `rotation_threshold` — configured threshold for forced rotation

### POST /api/floor-manager/end-shift

Signals the floor manager that the current shift rotation is acknowledged. Called by `schmux end-shift` CLI command. The floor manager agent should save its memory to `memory.md` before this is called.

Response:

```json
{ "status": "ok" }
```

Error cases:

- `500` — floor manager not configured

## Lore API

### GET /api/lore/status

Returns the lore system configuration status, including whether the curator is configured and any issues.

Response:

```json
{
  "enabled": true,
  "curator_configured": false,
  "curate_on_dispose": "session",
  "llm_target": "",
  "issues": ["No LLM target configured — curator cannot run. Set lore.llm_target in config."]
}
```

Fields:

- `enabled` (bool): Whether the lore system is enabled
- `curator_configured` (bool): Whether the curator has an LLM executor configured
- `curate_on_dispose` (string): When to auto-curate — `"session"` (every session dispose), `"workspace"` (only when last session for a workspace is disposed), or `"never"`
- `llm_target` (string): Configured LLM target name (may be empty)
- `issues` (string[]): Configuration issues that prevent full functionality

### GET /api/lore/{repo}/proposals

Lists all proposals for a repo.

Response:

```json
{
  "proposals": [{ "id": "...", "repo": "...", "status": "pending", ... }]
}
```

### GET /api/lore/{repo}/proposals/{id}

Returns a single proposal by ID.

### POST /api/lore/{repo}/proposals/{id}/apply

Applies a proposal: creates a worktree, commits changes, pushes the branch.

Request body (optional):

```json
{
  "overrides": { "CLAUDE.md": "modified content" }
}
```

Response:

```json
{
  "status": "applied",
  "branch": "lore/...",
  "pr_url": "https://..."
}
```

### POST /api/lore/{repo}/proposals/{id}/dismiss

Marks a proposal as dismissed.

### GET /api/lore/{repo}/entries

Returns lore entries for a repo, aggregated from all workspaces.

Query parameters:

- `state` — filter by state (`raw`, `proposed`, `applied`, `dismissed`)
- `agent` — filter by agent name
- `type` — filter by entry type
- `limit` — max entries to return

Response:

```json
{
  "entries": [{ "ts": "...", "ws": "...", "agent": "...", "type": "...", "text": "..." }]
}
```

### DELETE /api/lore/{repo}/entries

Clears all raw event entries by truncating per-session event JSONL files for the given repo.

Response:

```json
{
  "status": "cleared",
  "cleared": 3
}
```

### POST /api/lore/{repo}/curate

Triggers manual curation for a repo. Returns immediately with a curation ID; progress events stream via the `/ws/dashboard` WebSocket as `curator_event` messages.

Response:

```json
{
  "id": "cur-myrepo-20260222-153045",
  "status": "started"
}
```

If there are no raw entries to curate:

```json
{
  "id": "",
  "status": "no_raw_entries"
}
```

Errors:

- 503: "lore curator not configured (no LLM target)" or "lore system not enabled"
- 409: "curation already running for {repo}"

### GET /api/lore/{repo}/curations

Lists past curation run logs for a repo.

Response:

```json
{
  "runs": [
    {
      "id": "cur-myrepo-20260222-153045",
      "size_bytes": 12345,
      "created_at": "2026-02-22T15:30:45Z"
    }
  ]
}
```

Notes:

- Each curation run is stored as a subdirectory under `~/.schmux/lore-curator-runs/{repo}/{id}/`
- Each directory contains: `prompt.txt`, `run.sh`, `events.jsonl`, and `output.txt` or `error.txt`
- `size_bytes` reports the size of `events.jsonl` within the directory
- Sorted newest first
- Returns empty `runs` array if no curation runs exist

### GET /api/lore/{repo}/curations/{id}/log

Returns the JSONL event log for a specific curation run (reads `events.jsonl` from the run directory).

Response:

```json
{
  "events": [
    {"type": "system", "subtype": "init", ...},
    {"type": "assistant", "message": {...}, ...},
    {"type": "result", "duration_ms": 15000, ...}
  ]
}
```

Notes:

- Each event is a raw JSON object from the Claude CLI stream-json output
- The curation ID must not contain path separators
- Returns 404 if the log file does not exist

### GET /api/lore/{repo}/curations/active

Returns all active (in-progress) curation runs with their buffered events. Used on page load / WebSocket reconnect to recover state.

Response:

```json
{
  "runs": [
    {
      "id": "cur-myrepo-20260222-153045",
      "repo": "myrepo",
      "started_at": "2026-02-22T15:30:45Z",
      "events": [...],
      "done": false,
      "error": ""
    }
  ]
}
```

## Personas API

Personas are named behavioral profiles (system prompts + visual identity) that shape how agents operate. Each persona is a YAML file with frontmatter metadata and a body containing the system prompt. Five built-in personas are provided on first run.

### GET /api/personas

Returns all personas sorted by name.

Response:

```json
{
  "personas": [
    {
      "id": "security-auditor",
      "name": "Security Auditor",
      "icon": "🔒",
      "color": "#e74c3c",
      "prompt": "You are a security expert...",
      "expectations": "Produce a structured report.",
      "built_in": true
    }
  ]
}
```

### GET /api/personas/{id}

Returns a single persona by ID.

Response: a `Persona` object (same shape as items in the list response).

Errors:

- 400: "invalid persona ID" (must be lowercase alphanumeric + hyphens)
- 404: "persona not found: {id}"

### POST /api/personas

Creates a new persona.

Request:

```json
{
  "id": "my-reviewer",
  "name": "My Reviewer",
  "icon": "👀",
  "color": "#9b59b6",
  "prompt": "You review code carefully...",
  "expectations": "optional"
}
```

Response: the created `Persona` object.

Errors:

- 400: "invalid persona ID" / missing required fields (id, name, icon, color, prompt) / `"create"` is a reserved ID
- 409: "persona already exists: {id}"

### PUT /api/personas/{id}

Updates an existing persona. All fields are optional; only provided fields are changed.

Request:

```json
{
  "name": "Updated Name",
  "icon": "🔍",
  "color": "#2ecc71",
  "prompt": "Updated prompt...",
  "expectations": "Updated expectations"
}
```

Response: the updated `Persona` object.

Errors:

- 400: "invalid persona ID"
- 404: "persona not found: {id}"

### DELETE /api/personas/{id}

Deletes a custom persona, or resets a built-in persona to its default content.

Response: 204 No Content

Errors:

- 400: "invalid persona ID"
- 404: "persona not found: {id}"
- 500: "failed to delete/reset persona: ..."

Notes:

- Built-in personas are restored from embedded defaults rather than permanently deleted
- The `persona_id` field in `SpawnRequest` references these IDs
- Session responses include denormalized `persona_icon`, `persona_color`, and `persona_name` fields (computed at broadcast time from the persona manager, not persisted)

## Remote Access

### GET /remote-auth

Unauthenticated. Renders the password entry page for remote access authentication.

Query Parameters:

- `token` (required): One-time authentication token from the notification URL

Response: HTML page with password entry form.

Notes:

- Returns an error page if token is missing, invalid, or expired
- Returns a lockout page after 5 failed password attempts
- The token is generated when the tunnel connects and included in the notification URL

### POST /remote-auth

Unauthenticated. Validates the password against the bcrypt hash stored in config.

Form Parameters:

- `nonce`: Short-lived nonce (obtained by exchanging the one-time token)
- `password`: User-entered password

On success: Sets `schmux_remote` cookie (HMAC-signed timestamp + User-Agent fingerprint, 12h TTL) and redirects to `/`.

On failure: Re-renders password page with error message and remaining attempts count.

Notes:

- Maximum 5 password attempts per nonce; after that the nonce is invalidated
- On first GET with token, the token is consumed and replaced with a short-lived nonce
- The `schmux_remote` cookie is HttpOnly, Secure, SameSite=Lax, bound to User-Agent

### POST /api/remote-access/set-password

Sets the remote access password. Requires authentication (local dashboard access).

Request:

```json
{
  "password": "my-secret-password"
}
```

Response:

```json
{ "ok": true }
```

Errors:

- 400: "Password cannot be empty" / "Invalid request body"
- 405: "Method not allowed"
- 500: "Failed to hash password" / "Failed to save config: ..."

Notes:

- The password is bcrypt-hashed before storage; plaintext is never persisted
- Stored in `config.json` as `remote_access.password_hash`

### POST /api/remote-access/on

Start a Cloudflare quick tunnel for remote access.

Response (200):

```json
{ "state": "starting" }
```

Errors:

- 403: "remote access is disabled in config"
- 400: "remote access requires a password (run: schmux remote set-password)"
- 405: "Method not allowed"
- 500: "remote access not available" (tunnel manager not initialized)

Notes:

- Requires a password to be set (`remote_access.password_hash` in config)
- On tunnel connect, a one-time auth token is generated and sent via notification
- The tunnel URL is broadcast via WebSocket once connected

### POST /api/remote-access/off

Stop the remote access tunnel.

Response (200):

```json
{ "state": "off" }
```

Errors:

- 405: "Method not allowed"
- 500: "remote access not available" (tunnel manager not initialized)

### GET /api/remote-access/status

Get the current remote access tunnel status.

Response:

```json
{
  "state": "connected",
  "url": "https://abc123.trycloudflare.com",
  "error": ""
}
```

`state` can be: `"off"`, `"starting"`, `"connected"`, or `"error"`.

Errors:

- 405: "Method not allowed"
- 500: "remote access not available" (tunnel manager not initialized)

## Subreddit Digest

### GET /api/subreddit

Returns the cached subreddit digest content. The digest is generated hourly by the daemon using the configured LLM target.

Response (cached content available):

```json
{
  "content": "## r/schmux\n\n**Posted by u/devbot** • 24h ago\n\n### What's new this week...",
  "generated_at": "2024-01-15T10:30:00Z",
  "next_generation_at": "2024-01-15T11:30:00Z",
  "hours": 24,
  "commit_count": 12,
  "enabled": true
}
```

Response (no content yet or generation failed):

```json
{
  "enabled": true
}
```

Response (subreddit disabled):

```json
{
  "enabled": false
}
```

Fields:

| Field                | Type   | Description                                                                 |
| -------------------- | ------ | --------------------------------------------------------------------------- |
| `content`            | string | Markdown-formatted digest styled like a Reddit post (omitted if no content) |
| `generated_at`       | string | ISO 8601 timestamp when digest was generated (omitted if no content)        |
| `next_generation_at` | string | ISO 8601 timestamp when next generation is scheduled (omitted if not known) |
| `hours`              | int    | Lookback period in hours for commit gathering (omitted if no content)       |
| `commit_count`       | int    | Number of commits included in the digest (omitted if no content)            |
| `enabled`            | bool   | Whether subreddit feature is enabled (always present)                       |

The content is markdown-formatted text styled like a Reddit post. Empty content (only `enabled` field) indicates:

- Subreddit digest is disabled (no target configured) - `enabled: false`
- No commits in the lookback period
- Generation hasn't completed yet
- Previous generation failed

Configuration is via the config API (`subreddit.target` and `subreddit.hours` fields).

## WebSocket

### WS /ws/terminal/{sessionId}

Streams terminal output for a session.

Client -> server messages:

```json
{"type":"input","data":"raw-bytes-or-escape-seqs"}
{"type":"resize","data":"{\"cols\":120,\"rows\":30}"}
{"type":"diagnostic"}
{"type":"io-workspace-diagnostic"}
{"type":"syncResult","data":"{\"corrected\":true,\"diffRows\":[22,23,24]}"}
```

The `syncResult` message reports the result of a sync comparison. Sent after receiving a `sync` message from the server. Fields in `data` (JSON string):

- `corrected` (bool): whether xterm.js was reset and replayed
- `diffRows` (int[]): row indices that differed (empty if skipped due to activity guard)

Server -> client messages:

Binary frames contain raw terminal bytes. The first binary frame is the bootstrap snapshot (full screen capture with ANSI escape sequences and cursor positioning). Subsequent binary frames are incremental output from tmux control mode.

Text frames are JSON control messages:

```json
{"type":"displaced","content":"..."}
{"type":"stats","eventsDelivered":100,"eventsDropped":0,"bytesDelivered":50000,"bytesPerSec":1200,"controlModeReconnects":0,"syncChecksSent":5,"syncCorrections":0,"syncSkippedActive":2,"clientFanOutDrops":0,"fanOutDrops":0}
{"type":"controlMode","attached":true}
{"type":"diagnostic","diagDir":"...","counters":{...},"findings":[...],"verdict":"...","tmuxScreen":"..."}
{"type":"io-workspace-stats","totalCommands":42,"totalDurationMs":1234.5,"triggerCounts":{"poller":30,"watcher":12},"counters":{"git_status":20,"git_fetch":10}}
{"type":"io-workspace-diagnostic","diagDir":"...","counters":{...},"findings":[...],"verdict":"..."}
{"type":"sync","screen":"<ANSI-escaped tmux capture>","cursor":{"row":24,"col":3,"visible":true},"forced":false}
```

| Type                      | Description                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| ------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `displaced`               | Connection displaced by another window viewing the same session                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `stats`                   | Periodic pipeline diagnostics (dev mode only, every 3s). Includes sync counters: `syncChecksSent`, `syncCorrections`, `syncSkippedActive`. Includes per-layer drop counters: `eventsDropped` (parser), `clientFanOutDrops` (client fan-out), `fanOutDrops` (tracker fan-out)                                                                                                                                                                                                                                                                                                                                                                                          |
| `controlMode`             | tmux control mode attachment state changed                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `diagnostic`              | Response to a `diagnostic` request with capture data (dev mode only)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `io-workspace-stats`      | Periodic IO workspace telemetry stats (every 3s when io_workspace_telemetry enabled). Includes command counts, total duration, per-trigger and per-command-type breakdowns. Note: watcher-triggered refreshes skip `git fetch` (only local state queries), so `git_fetch` counts reflect poller/explicit triggers only. Origin query fetches, workspace fetches, and workspace git status updates all run concurrently within each poll cycle, with per-cycle caches deduplicating `git fetch` and `git worktree list` calls across workspaces sharing the same bare repo. Default branch detection (`git symbolic-ref`) is throttled to once per 60 seconds per repo |
| `io-workspace-diagnostic` | Response to an `io-workspace-diagnostic` request. Writes capture to `~/.schmux/diagnostics/` and returns counters, findings, verdict, and diagDir                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `sync`                    | Periodic screen snapshot for desync detection. `screen` contains visible-screen-only `capture-pane -e -p` output. `cursor` contains position and visibility. `forced` is true when drops have been detected since the last sync, bypassing the frontend's activity guard. Sent 500ms after bootstrap, then every 10s. The frontend compares plain-text content against its xterm.js buffer and corrects on mismatch                                                                                                                                                                                                                                                   |

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

Workspace lock state (sent in real-time, not debounced, when lock state changes or sync progress updates):

```json
{
  "type": "workspace_locked",
  "workspace_id": "workspace-id",
  "locked": true,
  "sync_progress": { "current": 5, "total": 496 }
}
```

Unlock with sync completion metadata (sent when `linear-sync-from-main` finishes):

```json
{
  "type": "workspace_locked",
  "workspace_id": "workspace-id",
  "locked": false,
  "sync_result": {
    "success": true,
    "success_count": 23,
    "branch": "main",
    "conflicting_hash": ""
  }
}
```

- `locked: true` sent when a sync operation acquires the workspace lock
- `locked: false` sent when the lock is released
- `sync_progress` is optional; included during `linear-sync-from-main` rebase with current/total commit counts
- `sync_result` is optional; included on unlock after `linear-sync-from-main` completes

GitHub CLI status (sent on connect and when status changes):

```json
{
  "type": "github_status",
  "github_status": {
    "available": true,
    "username": "octocat"
  }
}
```

Notes:

- Sessions updates use trailing debounce (100ms) to coalesce rapid changes into single broadcasts

Curator event (sent per streaming event during lore curation):

```json
{
  "type": "curator_event",
  "repo": "myrepo",
  "timestamp": "2026-02-22T15:30:46Z",
  "event_type": "assistant",
  "subtype": "",
  "raw": { ... }
}
```

- `event_type` values include: `system`, `assistant`, `user`, `result`, `error`, `server_error`, `overloaded_error`, `curator_done`, `curator_error`
- `raw` contains the full stream-json event from Claude CLI
- Error events (`error` or any type ending in `_error` like `server_error`, `overloaded_error`) indicate LLM API errors; `raw` may contain `{"error":{"type":"...","message":"..."}}` (wrapped) or `{"type":"server_error","detail":"...","status_code":500}` (direct)
- Terminal events (`curator_done`, `curator_error`) include `proposal_id`/`file_count` or `error` in `raw`

Curator state (sent on WebSocket connect to recover active and recently completed curations):

```json
{
  "type": "curator_state",
  "runs": [
    {
      "id": "cur-myrepo-20260222-153045",
      "repo": "myrepo",
      "started_at": "2026-02-22T15:30:45Z",
      "completed_at": "2026-02-22T15:32:10Z",
      "events": [...],
      "done": false,
      "error": ""
    }
  ]
}
```

- Active (in-progress) runs are always sent on connect
- Recently completed runs (within 60s) are also sent, so reconnecting clients learn about completions/errors that happened while disconnected
- `completed_at` is set when the run finishes (omitted if still in progress)

- `workspace_locked` messages are sent immediately (not debounced)
- No client-to-server messages expected; the connection is kept alive by reading

Remote access status update:

```json
{
  "type": "remote_access_status",
  "data": {
    "state": "connected",
    "url": "https://abc123.trycloudflare.com",
    "error": ""
  }
}
```

Event monitor (dev mode only, broadcasts all unified events):

```json
{
  "type": "event",
  "session_id": "ws1-abc123",
  "event": {
    "ts": "2026-02-24T10:00:00Z",
    "type": "status",
    "state": "working",
    "message": "Running tests"
  }
}
```

- Only sent when dev mode is active
- `event.type` values: `status`, `failure`, `reflection`, `friction`
- `event` contains the raw event JSON from the unified events system

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
      "provision_command": "setup.sh",
      "hostname_regex": "dev-\\d+\\.example\\.com",
      "vscode_command_template": "code --remote ssh-remote+{hostname}"
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

### GET /api/dev/events/history

Returns the most recent 200 events from all workspace event files, sorted chronologically. Used to bootstrap the event monitor on page load.

Response:

```json
[
  {
    "session_id": "ws1-abc123",
    "event": { "ts": "2026-02-24T10:00:00Z", "type": "status", "state": "working" },
    "ts": "2026-02-24T10:00:00Z"
  }
]
```

- Scans `<workspace>/.schmux/events/*.jsonl` across all active workspaces
- Session ID is derived from the filename (without `.jsonl` extension)
- Returns at most 200 events, sorted oldest-first

<!-- Test coverage: config getters, dashboard dispose/nickname guards, compound suppression, injector HandleEvent, secrets file migration, nudge summary parsing, curator response parsing, shell argument splitting -->
