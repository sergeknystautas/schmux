# API Contract

This document defines the daemon HTTP API contract. It is intentionally client-agnostic. If behavior changes, update this doc first and treat any divergence as a breaking change.

Base URL: `http://localhost:7337` (or `https://<public_base_url>` when auth is enabled)

Doc-gate policy:

- Any API-affecting code change must update `docs/api.md`. CI enforces this rule.
- Internal refactorings that touch API packages without changing the API surface still bump this file to satisfy the doc gate.
- VCS subprocess execution sets `GIT_TERMINAL_PROMPT=0` and uses process-group kill to prevent credential-prompt hangs and orphaned child processes.

General conventions:

- JSON requests/responses use `Content-Type: application/json`.
- Many error responses use plain text via `http.Error`; do not assume JSON unless specified.
- CORS: when TLS is disabled, requests are allowed from `http://localhost:7337` and `http://127.0.0.1:7337`. When TLS is enabled, the scheme switches to `https`. When `dashboard_hostname` is set and resolves to a local network interface, the derived URL (`scheme://dashboard_hostname:port`) is also allowed; non-local hostnames are silently ignored so the same binary works on both devservers and laptops. When `bind_address` is `0.0.0.0`, any origin is allowed. Allowed methods: `GET, POST, DELETE, PUT, PATCH, OPTIONS`.
- Dual-stack loopback: when `bind_address` is `127.0.0.1` (the default), the server also listens on `[::1]` (IPv6 loopback) on a best-effort basis. This ensures the dashboard is reachable via IPv6 localhost (e.g., on devservers where proxies connect via `[::1]`).
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

### GET /api/features

Reports which optional modules are available in this build. Used by the dashboard to hide UI panels for excluded modules.

Response:

```json
{
  "tunnel": true,
  "github": true,
  "telemetry": true,
  "update": true,
  "dashboardsx": true,
  "model_registry": true,
  "repofeed": true,
  "subreddit": true
}
```

When a module is excluded via build tags (e.g. `-tags notunnel,nogithub,notelemetry,noupdate,nodashboardsx,nomodelregistry,norepofeed,nosubreddit`), its field is `false`.

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
    "ahead": 0,
    "behind": 0,
    "lines_added": 0,
    "lines_removed": 0,
    "files_changed": 0,
    "git_branch_url": "https://github.com/user/repo/tree/branch", // optional, when remote exists
    "sessions": [
      {
        "id": "session-id",
        "target": "target-name",
        "branch": "branch",
        "nickname": "optional",
        "xterm_title": "optional",
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
    ],
    "tabs": [
      {
        "id": "tab-uuid",
        "kind": "markdown",
        "label": "README.md",
        "route": "/diff/{workspaceId}/md/README.md",
        "closable": true,
        "meta": { "filepath": "README.md" },
        "created_at": "2025-01-15T10:00:00Z"
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
- Workspace `status` field: `provisioning` (being created), `running` (ready), `failed` (creation failed), `disposing` (being torn down). Omitted for pre-existing workspaces (treat as `running`).
- Session `status` field includes `disposing` during teardown. Dispose endpoints return 200 OK if the item is already in `disposing` status (idempotent).
- Workspace `tabs` array contains Tab objects with fields: `id` (UUID), `kind` (tab type), `label`, `route`, `closable`, `meta` (type-specific metadata), and `created_at`.
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

### GET /api/workspaces/{workspaceId}/previews

List known previews for a workspace. Previews are auto-detected when a session's
terminal output contains an HTTP URL and the port is confirmed to be listening.
Detected ports are validated with an HTTP HEAD probe before a preview is created;
non-HTTP listeners (e.g. IPC, gRPC) are filtered out. The daemon's own
listening port is excluded to prevent self-detection during dev mode. Servers
launched via `nohup`/`disown` (outside the session's PID tree) are detected if
they write a PID file to `.superpowers/brainstorm/*/state/server.pid` in the
workspace.

Response: array of preview objects from the create endpoint.

### POST /api/workspaces/{workspaceId}/previews

Create a preview proxy for a local port.

Request body:

| Field             | Type   | Required | Description                        |
| ----------------- | ------ | -------- | ---------------------------------- |
| target_port       | int    | yes      | Port to proxy (1-65535)            |
| target_host       | string | no       | Loopback host (default: 127.0.0.1) |
| source_session_id | string | no       | Bind lifecycle to a session        |

Response: `201 Created` with preview object on creation, `200 OK` on dedup (exact host+port match).

Errors: 400 (bad input), 404 (workspace not found), 409 (cap reached), 422 (port not listening)

### DELETE /api/workspaces/{workspaceId}/previews/{previewId}

Delete a preview mapping and stop its listener.

Response:

```json
{ "status": "ok" }
```

### POST /api/workspaces/{workspaceId}/tabs

Create a workspace tab. Only certain kinds (`markdown`, `commit`) are allowed for client creation. Server-managed kinds (`diff`, `git`, `preview`, `resolve-conflict`) are created automatically.

Request:

```json
{
  "kind": "markdown",
  "label": "README.md",
  "route": "/diff/{workspaceId}/md/README.md",
  "closable": true,
  "meta": { "filepath": "README.md" }
}
```

Response: `200 OK`

```json
{ "id": "generated-uuid", "status": "ok" }
```

Errors:

- 400: "invalid request body", "unsupported tab kind"
- 404: "workspace not found"

### DELETE /api/workspaces/{workspaceId}/tabs/{tabId}

Close a workspace tab. Only closable tabs can be deleted. For preview tabs, cascades to preview proxy teardown.

Response: `200 OK`

```json
{ "status": "ok" }
```

Errors:

- 400: "tab is not closable"
- 404: "workspace not found", "tab not found"

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
  "command": "optional",
  "workspace_id": "optional",
  "resume": false,
  "persona_id": "optional",
  "action_id": "optional",
  "image_attachments": ["base64-encoded-png", "..."]
}
```

Contract (pre-2093ccf):

- When `workspace_id` is empty, `repo` and `branch` are required.
- **`repo` must be a repo URL**, not a repo name. If the URL is not yet in config, the server auto-registers it (generates a name, sets `bare_path`, saves config) before proceeding with workspace creation.
- When `workspace_id` is provided, the spawn is an "existing directory spawn" and **no git operations** are performed.
- Either `targets` or `command` is required (not both). `targets` maps target name -> quantity. `command` is a raw shell command string (used by quick launch presets like "shell").
- Target names are resolved in order: (1) model IDs and aliases (e.g., "opus", "claude-sonnet-4-6"), (2) user-defined run targets from config, (3) builtin tool names ("claude", "codex", "gemini", "opencode") as a fallback when the tool binary isn't detected locally (useful for remote sessions where the tool is on the remote host). Default models (selecting an agent with no specific model) use bare tool names as IDs: "claude", "codex", "gemini", "opencode".
- Promptable targets require `prompt`. Command targets must not include `prompt`.
- For non-promptable targets, the server forces `count` to 1.
- If multiple sessions are spawned and `nickname` is provided, nicknames are auto-suffixed globally:
  - `"<nickname> (1)"`, `"<nickname> (2)"`, ...
- `persona_id` is optional. When set, the persona's system prompt is injected into the agent at spawn time (e.g., via `--append-system-prompt-file` for Claude). The persona ID is stored on the session and used to display persona badges in the dashboard.
- `image_attachments` is optional. Array of base64-encoded PNG strings (max 5). Images are decoded and written to `{workspace}/.schmux/attachments/img-{uuid}.png`. Absolute file paths are appended to the prompt so the agent can reference them. Cannot be used with `resume`, `command`, or `remote_flavor_id`.
- `action_id` is optional. When set, usage is recorded against the matching spawn entry in the emergence store. When absent and a prompt exactly matches a pinned spawn entry's prompt, usage is recorded automatically.

Resume mode (`resume: true`):

- Either `workspace_id` (existing workspace) or `repo`+`branch` (create new workspace) must be provided.
- `prompt` must be empty (resume uses agent's resume command, not a prompt).
- The agent's resume command is used instead of a prompt (e.g., `claude --continue`, `codex resume --last`, `opencode --continue`).

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

Both target-based and command-based spawns trigger an immediate WebSocket broadcast on `/ws/dashboard` so clients can detect the new session without waiting for the next poll cycle.

Environment cleanup: before creating a tmux session, the server removes pollution from the tmux server's global environment. This includes agent nesting-detection variables (e.g., `CLAUDECODE`) and any variable not present in the system baseline — a snapshot of the fresh login shell environment captured at daemon startup (and refreshed on `GET /api/environment`). Variables like `npm_config_prefix` that leak into the tmux server from processes like `npx`/`dev.sh` are stripped so new sessions inherit clean state. Keys managed by tmux itself (`TMUX`, `TMUX_PANE`) are preserved.

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
- Returns branches from configured remote repos; local `local:{name}` repos are skipped
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
- Skips local `local:{name}` repos because they do not have an origin remote
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

Dispose a session. Sets the session status to `disposing` and broadcasts immediately for visual feedback before starting teardown. Returns 200 OK if the session is already disposing (idempotent). Reverts status on failure.

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

**Local sessions:** Writes the image to the system clipboard (macOS only via osascript) and sends Ctrl+V (0x16) to the tmux session so the terminal application picks up the image.

**Remote sessions:** Transfers the image to the remote host via base64 (through `RunCommand`), then tries two approaches in order: (1) sets the remote X11 clipboard via `xclip` and sends Ctrl+V, or (2) if xclip is unavailable, leaves the file on disk and types the file path into the agent's input. Max 2MB for remote transfers. The response includes `method` ("clipboard" or "file") and `file_path` (set when file fallback is used).

The frontend also intercepts Ctrl+V (`\x16`) keystrokes: when the browser clipboard contains an image, it calls this API instead of forwarding the raw keystroke. Drag-and-drop of image files onto the terminal is also supported.

Request (max 10MB body):

```json
{
  "sessionId": "session-uuid",
  "imageBase64": "iVBORw0KGgoAAAANSUhEUgAA..."
}
```

Response:

```json
{ "status": "ok", "method": "clipboard", "file_path": "" }
{ "status": "ok", "method": "file", "file_path": "/tmp/schmux-clipboard-abc123.png" }
```

Errors:

- 400: "method not allowed", "invalid request body", "sessionId and imageBase64 are required", "invalid base64 image data"
- 404: "session not found"
- 500: "failed to process image", "failed to set clipboard: ...", "remote manager not configured", "session tracker not found", "failed to send input", "failed to paste image on remote host: ..."
- 503: "remote host not connected", "Remote host is still being provisioned..."

### POST /api/workspaces/{workspaceId}/dispose

Dispose a workspace (fails if workspace has active sessions). Sets workspace status to `disposing` and broadcasts immediately for visual feedback before starting teardown. Returns 200 OK if already disposing (idempotent). Reverts status on failure. Disposal runs with an independent server-side timeout and will complete even if the client disconnects.

Response:

```json
{ "status": "ok" }
```

Errors:

- 400 with JSON: `{"error":"..."}` (e.g., dirty workspace, active sessions)

### POST /api/workspaces/{workspaceId}/dispose-all

Dispose a workspace and all its sessions.

Sets workspace and all session statuses to `disposing` and broadcasts immediately before starting teardown. Returns 200 OK if already disposing (idempotent). Reverts workspace status on failure. Disposes all sessions concurrently first, then disposes the workspace itself. Both phases run with independent server-side timeouts and will complete even if the client disconnects.

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

### PUT /api/sessions-xterm-title/{sessionId}

Update a session's xterm title (reported by the frontend when xterm.js detects an OSC 0/2 title change). The title is in-memory only and not persisted across daemon restarts. A broadcast is sent only when the title actually changes.

Request:

```json
{ "title": "Working on feature X" }
```

Response:

```json
{ "status": "ok" }
```

### GET /api/config

Returns the current config. On first run, the daemon creates `~/.schmux/config.json` with defaults automatically (no interactive prompt).

Response:

```json
{
  "workspace_path": "/path",
  "source_code_management": "git-worktree",
  "repos": [{ "name": "repo", "url": "https://...", "vcs": "sapling" }],
  "run_targets": [{ "name": "target", "type": "promptable", "command": "...", "source": "user" }],
  "quick_launch": [
    {
      "name": "preset",
      "target": "target (required if no command)",
      "prompt": "optional",
      "command": "optional (required if no target)",
      "persona_id": "optional"
    }
  ],
  "pastebin": ["text to paste 1", "text to paste 2"],
  "runners": {
    "claude": { "available": true, "capabilities": ["interactive", "oneshot", "streaming"] },
    "opencode": { "available": true, "capabilities": ["interactive", "oneshot"] }
  },
  "models": [
    {
      "id": "claude-sonnet-4-6",
      "display_name": "Claude Sonnet 4.6",
      "provider": "anthropic",
      "configured": true,
      "runners": ["claude", "opencode"]
    }
  ],
  "enabled_models": { "claude-sonnet-4-6": "claude" },
  "nudgenik": { "target": "optional", "viewed_buffer_ms": 0, "seen_interval_ms": 0 },
  "compound": { "target": "", "debounce_ms": 2000, "enabled": true, "suppression_ttl_ms": 5000 },
  "sessions": {
    "dashboard_poll_interval_ms": 0,
    "git_status_poll_interval_ms": 0,
    "git_clone_timeout_ms": 0,
    "git_status_timeout_ms": 0,
    "dispose_grace_period_ms": 0
  },
  "xterm": {
    "query_timeout_ms": 0,
    "operation_timeout_ms": 0,
    "use_webgl": true,
    "sync_check_enabled": false
  },
  "network": {
    "bind_address": "127.0.0.1",
    "port": 7337,
    "public_base_url": "https://schmux.local:7337",
    "dashboard_hostname": "",
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
  "sapling_commands": {
    "create_workspace": "fbclone {{.RepoIdentifier}} {{.DestPath}}",
    "remove_workspace": "rm -rf {{.WorkspacePath}}",
    "create_repo_base": "fbclone {{.RepoIdentifier}} {{.BasePath}}",
    "check_repo_base": ""
  },
  "tmux_binary": "/opt/homebrew/bin/tmux",
  "system_capabilities": {
    "iterm2_available": true
  },
  "needs_restart": false,
  "dashboard_sx_status": {
    "last_heartbeat_time": "2026-04-01T12:00:00Z",
    "last_heartbeat_status": 200,
    "cert_domain": "12540.dashboard.sx",
    "cert_expires_at": "2026-07-01T00:00:00Z"
  }
}
```

Repos with `"vcs": "sapling"` use the sapling backend instead of git. The `vcs` field can be `""` (default, git worktree), `"git-clone"`, or `"sapling"`. The `sapling_commands` section configures command templates for sapling workspace lifecycle using Go `text/template` syntax.

**`tmux_binary`**: Path to a custom tmux binary. When empty or omitted, the system default from `$PATH` is used. The path is validated on save (must exist, be executable, and output a recognized tmux version string). Changing this field flags `needs_restart`.

**TLS behavior**: The server serves HTTPS whenever `network.tls.cert_path` and `network.tls.key_path` are both set, regardless of whether `access_control.enabled` is true. This allows dashboard.sx HTTPS without requiring GitHub auth.

**`dashboard_sx_status`** (object, optional): Dashboard.sx heartbeat and certificate status. Only present when `network.dashboardsx.enabled` is true and at least one heartbeat has been attempted. All fields are omitted when empty/zero:

- `last_heartbeat_time` (string) — ISO 8601 timestamp of last heartbeat attempt
- `last_heartbeat_status` (number) — HTTP status code from last heartbeat (0 for network errors)
- `last_heartbeat_error` (string) — Error message if heartbeat was non-200
- `cert_domain` (string) — Certificate common name (e.g. `"12540.dashboard.sx"`)
- `cert_expires_at` (string) — ISO 8601 timestamp of certificate expiry

### POST/PUT /api/config

Update the config. All fields are optional; omitted fields are unchanged.

Request:

```json
{
  "workspace_path": "/path",
  "source_code_management": "git-worktree",
  "repos": [{ "name": "repo", "url": "https://...", "vcs": "sapling" }],
  "run_targets": [{ "name": "target", "type": "promptable", "command": "...", "source": "user" }],
  "quick_launch": [
    {
      "name": "preset",
      "target": "target (required if no command)",
      "prompt": "optional",
      "command": "optional (required if no target)",
      "persona_id": "optional"
    }
  ],
  "pastebin": ["text to paste 1", "text to paste 2"],
  "enabled_models": { "claude-sonnet-4-6": "claude" },
  "nudgenik": { "target": "optional", "viewed_buffer_ms": 0, "seen_interval_ms": 0 },
  "compound": { "target": "", "debounce_ms": 2000, "enabled": true, "suppression_ttl_ms": 5000 },
  "sessions": {
    "dashboard_poll_interval_ms": 0,
    "git_status_poll_interval_ms": 0,
    "git_clone_timeout_ms": 0,
    "git_status_timeout_ms": 0,
    "dispose_grace_period_ms": 0
  },
  "xterm": {
    "query_timeout_ms": 0,
    "operation_timeout_ms": 0,
    "use_webgl": true,
    "sync_check_enabled": false
  },
  "network": {
    "bind_address": "127.0.0.1",
    "port": 7337,
    "public_base_url": "https://schmux.local:7337",
    "dashboard_hostname": "",
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
  "repofeed": {
    "enabled": true,
    "publish_interval_seconds": 30,
    "fetch_interval_seconds": 60,
    "completed_retention_hours": 48,
    "repos": { "my-repo": true }
  },
  "notifications": {
    "sound_disabled": false,
    "confirm_before_close": false,
    "suggest_dispose_after_push": true
  },
  "tmux_binary": "/opt/homebrew/bin/tmux"
}
```

The `tmux_binary` field is validated on save: the path must exist, be executable, and `<path> -V` must output a recognized tmux version string. An empty string clears the override. Invalid paths return 400.

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

- 400 for validation errors (plain text). Config validation checks structural integrity (non-empty names, no duplicate repo names, non-empty targets) but does not validate whether referenced targets exist at save time — target resolution happens at spawn time.
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

Lists available models and whether they are configured. Each model includes a `runners` list of tool names that can run it; tool-level details (availability, capabilities) are in the top-level `runners` map on the config response. Model catalog, availability, enablement, and resolution are owned by the internal model manager (`internal/models`). Model IDs are vendor-defined (e.g., `claude-sonnet-4-6`). Legacy IDs (`claude-sonnet`, `sonnet`, etc.) are automatically migrated on load.

Response:

```json
{
  "models": [
    {
      "id": "claude-sonnet-4-6",
      "display_name": "Claude Sonnet 4.6",
      "provider": "anthropic",
      "configured": true,
      "runners": ["claude", "opencode"]
    },
    {
      "id": "kimi-thinking",
      "display_name": "kimi k2 thinking",
      "provider": "moonshot",
      "configured": false,
      "runners": ["claude", "opencode"],
      "required_secrets": ["ANTHROPIC_AUTH_TOKEN"]
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

### GET /api/user-models

Returns user-defined models. These are custom models defined by the user that override registry and built-in models.

Response:

```json
{
  "models": [
    {
      "id": "my-model",
      "display_name": "My Model",
      "provider": "custom",
      "runners": ["claude"],
      "command": "claude --dangerously-skip-permissions",
      "required_env": ["API_KEY"]
    }
  ]
}
```

### PUT /api/user-models

Saves user-defined models. Validates that runner names are valid detected tools.

Request:

```json
{
  "models": [
    {
      "id": "my-model",
      "display_name": "My Model",
      "provider": "custom",
      "runners": ["claude"],
      "command": "claude --dangerously-skip-permissions",
      "required_env": ["API_KEY"]
    }
  ]
}
```

Response:

```json
{ "status": "ok" }
```

Errors:

- 400: validation error (plain text) - e.g., invalid runner name, missing required fields

### GET /api/builtin-quick-launch

Returns built-in quick launch presets.

Response:

```json
[{ "name": "Preset", "target": "target", "prompt": "prompt text" }]
```

### GET /api/diff/{workspaceId}

Returns git diff for a workspace (tracked files + untracked).
Returns 400 for non-git workspaces (e.g., sapling).

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

### GET /api/file/{workspaceId}/{filepath}

Serves a raw file from a workspace directory. Supports image files (`.png`, `.jpg`, `.jpeg`, `.webp`, `.gif`) and markdown files (`.md`, `.mdx`). Verifies case-sensitive filename match on case-insensitive filesystems (macOS APFS).

Path:

- `{workspaceId}` — workspace identifier
- `{filepath}` — URL-encoded relative file path within the workspace

Security:

- Path traversal is blocked
- `.gitignore` patterns are respected
- Only allowed file extensions are served
- Directories cannot be served

Response: Raw file content with appropriate `Content-Type` header.

Errors:

- 400: `"workspace ID is required"` / `"invalid path format"`
- 403: `"file type not allowed"` / `"invalid file path"` / `"cannot serve directory"` / `"file is ignored by git"`
- 404: `"workspace not found"` / `"file not found"`

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

Opens VS Code for the workspace. Supports two modes:

**Default mode** (local client): Executes the `code` command on the server to open VS Code locally.

**URI mode** (`?mode=uri`, remote client): Returns a `vscode://` URI for opening VS Code from a remote browser via the Remote-SSH extension. Also detects VS Code Server processes on the host.

Query parameters:

- `mode=uri` — Return a VS Code Remote URI instead of executing a local command. Use when the browser is on a different machine than the schmux server.

Response (default mode):

```json
{ "success": true, "message": "You can now switch to VS Code." }
```

Response (URI mode):

```json
{
  "success": true,
  "message": "Open the VS Code URI to connect remotely.",
  "vscode_uri": "vscode://vscode-remote/ssh-remote+hostname/path/to/workspace",
  "server_info": {
    "hostname": "dev-server.local",
    "web_server_url": "http://dev-server.local:8000",
    "has_vscode_server": true,
    "tunnel_running": false
  }
}
```

The `server_info` field is only present when VS Code Server processes are detected. Fields:

- `hostname` — Server hostname
- `web_server_url` — URL if `code serve-web` is running (direct browser access)
- `has_vscode_server` — `true` if `~/.vscode-server/` exists (SSH Remote was used before)
- `tunnel_running` — `true` if `code tunnel` is running

Errors:

- 404 with JSON if workspace not found or directory missing
- 404 with JSON if `code` command not found in PATH (default mode only)
- 500 with JSON on launch failure or hostname resolution failure

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
Returns 400 for non-git workspaces (e.g., sapling).

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
Returns 400 for non-git workspaces.

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
Returns 400 for non-git workspaces.

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

- Requires at least one unpushed commit (`ahead > 0`)
- At least one file must be specified
- Updates workspace git status and broadcasts after amend

### POST /api/workspaces/{workspaceId}/git-discard

Discards local changes. If `files` are specified, only those files are discarded. If `files` is empty or body is omitted, all changes are discarded.
Returns 400 for non-git workspaces.

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
Returns 400 for non-git workspaces.

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

- Requires at least one unpushed commit (`ahead > 0`)
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
  "proposals": [
    {
      "id": "prop-20260304-153045-ab12",
      "repo": "myrepo",
      "created_at": "2026-03-04T15:30:45Z",
      "status": "pending",
      "rules": [
        {
          "id": "r1",
          "text": "Always use go run ./cmd/build-dashboard instead of npm run build",
          "category": "build",
          "suggested_layer": "repo_public",
          "chosen_layer": null,
          "status": "pending",
          "source_entries": ["2026-03-04T10:00:00Z"],
          "merged_at": null
        }
      ],
      "discarded": ["2026-03-04T08:00:00Z"]
    }
  ]
}
```

Proposal fields:

- `id` (string): Unique proposal identifier
- `repo` (string): Repository name
- `created_at` (string): ISO 8601 creation timestamp
- `status` (string): `"pending"`, `"applied"`, or `"dismissed"`
- `rules` (array): Discrete rules extracted by the curator
- `discarded` (string[]): Entry keys discarded during extraction

Rule fields:

- `id` (string): Rule identifier within the proposal (e.g., `"r1"`)
- `text` (string): The rule text (editable by the user)
- `category` (string): Rule category (e.g., `"build"`, `"testing"`)
- `suggested_layer` (string): AI-suggested layer — `"repo_public"`, `"repo_private"`, or `"cross_repo_private"`
- `chosen_layer` (string|null): User-overridden layer, or null to use suggested
- `status` (string): `"pending"`, `"approved"`, or `"dismissed"`
- `source_entries` (string[]): Entry keys that led to this rule
- `merged_at` (string|null): ISO 8601 timestamp when the rule was merged, or null

### GET /api/lore/{repo}/proposals/{id}

Returns a single proposal by ID. Response shape is the same as a single element from the proposals list above.

### POST /api/lore/{repo}/proposals/{id}/dismiss

Marks a proposal as dismissed.

### POST /api/lore/{repo}/proposals/{proposalID}/rules/{ruleID}

Updates a specific rule within a proposal (approve, dismiss, edit text, or reroute to a different layer).

Request:

```json
{
  "status": "approved",
  "text": "edited rule text (optional)",
  "chosen_layer": "repo_private"
}
```

Fields:

- `status` (string, optional): `"approved"` or `"dismissed"`. Omit to update only text/layer.
- `text` (string, optional): Edited rule text. Only updates if provided.
- `chosen_layer` (string, optional): Layer override — `"repo_public"`, `"repo_private"`, or `"cross_repo_private"`. Overrides the AI-suggested layer.

Response: the updated Proposal object (same shape as `GET /api/lore/{repo}/proposals/{id}`).

Errors:

- 400: invalid status or chosen_layer
- 404: proposal or rule not found

### POST /api/lore/{repo}/proposals/{proposalID}/merge

Triggers phase 3 merge: groups approved rules by their effective layer, reads current content for each layer, calls the merge LLM per layer, and returns previews for user review before applying.

Response:

```json
{
  "previews": [
    {
      "layer": "repo_public",
      "current_content": "existing instruction file content",
      "merged_content": "new merged content from LLM",
      "summary": "description of changes made"
    }
  ]
}
```

Errors:

- 400: no approved rules to merge
- 404: proposal not found
- 503: lore curator not configured

### POST /api/lore/{repo}/proposals/{proposalID}/apply-merge

Applies reviewed merge results to their target layers. For `repo_public`, creates a dedicated `schmux/lore` workspace and writes the merged instruction file as an unstaged change (no commit, no push). The user reviews and commits manually. For `repo_private` and `cross_repo_private`, writes directly to the instruction store.

Request:

```json
{
  "merges": [
    { "layer": "repo_public", "content": "final merged content" },
    { "layer": "repo_private", "content": "private instructions" }
  ]
}
```

Response:

```json
{
  "results": [
    {
      "layer": "repo_public",
      "status": "applied",
      "workspace_id": "ws-abc123"
    },
    { "layer": "repo_private", "status": "applied" }
  ]
}
```

After applying, approved rules are marked with `merged_at` timestamps and the proposal status is set to `applied`.

For `repo_public`, if a `schmux/lore` workspace already exists but has uncommitted changes or commits ahead of `origin/main`, the request is rejected with 409 Conflict.

Errors:

- 400: no merges provided, invalid layer
- 404: proposal or repo not found
- 409: lore workspace has pending changes (public layer only)
- 503: instruction store not configured (for private/global layers)

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

Triggers manual curation for a repo. Returns immediately with a curation ID; progress events stream via the `/ws/dashboard` WebSocket as `curator_event` messages. On successful completion, all raw entries included in the curation are marked as "proposed" and excluded from future curation runs. Extracted rules are deduplicated against existing pending and previously dismissed proposals — rules that match (case-insensitive, whitespace-normalized) are silently dropped.

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
- The curator LLM call does not use JSON schema constrained decoding; the prompt instructs the LLM to output JSON directly, and the response is parsed server-side

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

### Instruction Store

Private instruction layers are stored at `~/.schmux/instructions/`:

- `cross-repo-private.md` — cross-repo private instructions (applied to all repos)
- `repos/<repo>/private.md` — repo-specific private instructions

At agent spawn time, assembled instructions (global + repo-private) are injected into the workspace instruction file within the `<!-- SCHMUX:BEGIN/END -->` markers alongside signaling instructions. These are never committed to the repo.

## Emergence API

Emergence is the skill discovery system that replaces the Actions registry. Spawn entries represent reusable task templates with lifecycle tracking (proposed → pinned or dismissed). Repo names are validated (no path separators, dots, null bytes, max 128 chars).

Pinned skill entries are automatically injected into workspaces at spawn time via the ensure package, in addition to being injected when the pin action is triggered.

### GET /api/emergence/{repo}/entries

Returns pinned spawn entries for a repo (used by the spawn dropdown).

Response:

```json
{
  "entries": [
    {
      "id": "se-abcdef12",
      "name": "Run tests",
      "description": "Run the project test suite with verbose output",
      "type": "command",
      "source": "manual",
      "state": "pinned",
      "use_count": 5,
      "command": "go test ./..."
    }
  ]
}
```

### GET /api/emergence/{repo}/entries/all

Returns all spawn entries for a repo (proposed, pinned, and dismissed).

Response: same shape as `GET /api/emergence/{repo}/entries` with entries in all states. For skill-type entries, the `description` field is enriched from emergence metadata if not already set, and a `metadata` object is included with `skill_content`, `confidence`, `evidence_count`, `evidence`, `emerged_at`, and `last_curated`.

### POST /api/emergence/{repo}/entries

Creates a new manual spawn entry (state=pinned, source=manual).

Request:

```json
{
  "name": "Run tests",
  "type": "command",
  "command": "go test ./..."
}
```

Required fields: `name`, `type`. Optional: `command`, `prompt`, `target`, `skill_ref`.

Response: `201 Created` with the created SpawnEntry object.

### PUT /api/emergence/{repo}/entries/{id}

Updates an existing spawn entry. All fields are optional (patch semantics).

Request:

```json
{
  "name": "Updated name",
  "command": "go test -v ./..."
}
```

Response: the updated SpawnEntry object.

Errors:

- 404: entry not found

### DELETE /api/emergence/{repo}/entries/{id}

Hard-deletes a spawn entry.

Response:

```json
{ "status": "deleted" }
```

Errors:

- 404: entry not found

### POST /api/emergence/{repo}/entries/{id}/pin

Transitions a proposed entry to pinned state. If the entry has a skill reference with emergence metadata, the skill is injected into all workspaces for the repo.

Response:

```json
{ "status": "pinned" }
```

Errors:

- 404: entry not found

### POST /api/emergence/{repo}/entries/{id}/dismiss

Transitions an entry to dismissed state.

Response:

```json
{ "status": "dismissed" }
```

Errors:

- 404: entry not found

### POST /api/emergence/{repo}/entries/{id}/use

Records a usage of a spawn entry (increments use_count).

Response:

```json
{ "status": "recorded" }
```

Errors:

- 404: entry not found

### POST /api/emergence/{repo}/curate

Triggers emergence curation: collects intent signals from workspace event files, sends them to the LLM, and creates proposed spawn entries for discovered skills. Skills that match already-proposed or pinned entries are deduplicated and not re-proposed.

Response: `202 Accepted`

```json
{ "status": "started" }
```

Returns `{ "status": "no_signals" }` with 200 if no intent signals are found.

Errors:

- 503: emergence system or LLM target not configured

### GET /api/emergence/{repo}/prompt-history

Returns prompt autocomplete data from workspace event files. Extracts unique prompts from status events with non-empty intent fields, deduplicated and sorted by last_seen descending.

Response:

```json
{
  "prompts": [
    {
      "text": "Fix the failing tests in the auth module",
      "last_seen": "2026-02-27T14:00:00Z",
      "count": 3
    }
  ]
}
```

Returns at most 50 entries.

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

Returns subreddit posts organized by repository. Posts are generated per-repo with create/update semantics and importance scoring (upvotes 0-5).

Response (enabled with posts):

```json
{
  "enabled": true,
  "repos": [
    {
      "name": "my-repo",
      "slug": "my-repo",
      "posts": [
        {
          "id": "post-1709712000",
          "title": "New workspace switching",
          "content": "Great new feature for switching workspaces...",
          "upvotes": 3,
          "created_at": "2024-03-06T10:00:00Z",
          "updated_at": "2024-03-06T12:30:00Z",
          "revision": 2
        }
      ]
    }
  ],
  "next_generation_at": "2024-03-06T11:30:00Z"
}
```

Response (enabled but no posts yet):

```json
{
  "enabled": true,
  "repos": []
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
| `enabled`            | bool   | Whether subreddit feature is enabled (always present)                       |
| `repos`              | array  | Array of repo objects with posts (omitted if disabled)                      |
| `next_generation_at` | string | ISO 8601 timestamp when next generation is scheduled (omitted if not known) |

Repo object fields:

| Field   | Type   | Description                         |
| ------- | ------ | ----------------------------------- |
| `name`  | string | Repository name                     |
| `slug`  | string | URL-safe slug for the repo          |
| `posts` | array  | Array of post objects for this repo |

Post object fields:

| Field        | Type   | Description                                                |
| ------------ | ------ | ---------------------------------------------------------- |
| `id`         | string | Unique post identifier                                     |
| `title`      | string | Post title (headline)                                      |
| `content`    | string | Markdown-formatted post body                               |
| `upvotes`    | int    | Importance score 0-5 (logarithmic scale)                   |
| `created_at` | string | ISO 8601 timestamp when post was created                   |
| `updated_at` | string | ISO 8601 timestamp when post was last updated              |
| `revision`   | int    | Revision number (1 = original, 2+ = updated at least once) |

Configuration is via the config API:

- `subreddit.target` - LLM target for generation
- `subreddit.interval` - Polling interval in minutes (default: 30)
- `subreddit.checking_range` - Hours to look back for commits (default: 48)
- `subreddit.max_posts` - Maximum posts per repo (default: 30)
- `subreddit.max_age` - Maximum post age in days (default: 14)
- `subreddit.repos` - Map of repo slugs to enabled/disabled status

### GET /api/repofeed

Returns repofeed activity organized by repository. Shows what other developers are working on across repos via cross-developer intent federation.

Response:

```json
{
  "repos": [
    {
      "name": "my-repo",
      "slug": "my-repo",
      "active_intents": 2,
      "landed_count": 0
    }
  ],
  "last_fetch": "2026-03-07T10:00:00Z"
}
```

Response (no activity):

```json
{
  "repos": []
}
```

Fields:

| Field        | Type   | Description                                        |
| ------------ | ------ | -------------------------------------------------- |
| `repos`      | array  | Array of repo summary objects                      |
| `last_fetch` | string | ISO 8601 timestamp of last fetch (omitted if none) |

Repo summary fields:

| Field            | Type   | Description                        |
| ---------------- | ------ | ---------------------------------- |
| `name`           | string | Repository name                    |
| `slug`           | string | URL-safe slug for the repo         |
| `active_intents` | int    | Number of currently active intents |
| `landed_count`   | int    | Number of landed (completed) items |

### GET /api/repofeed/{slug}

Returns full intent details for a specific repository.

Response:

```json
{
  "name": "my-repo",
  "slug": "my-repo",
  "intents": [
    {
      "developer": "alice@example.com",
      "display_name": "Alice",
      "intent": "Adding user authentication",
      "status": "active",
      "started": "2026-03-07T09:00:00Z",
      "branches": ["feature/auth"],
      "session_count": 2,
      "agents": ["claude-code"]
    }
  ],
  "landed": [],
  "last_fetch": "2026-03-07T10:00:00Z"
}
```

Intent entry fields:

| Field           | Type     | Description                                           |
| --------------- | -------- | ----------------------------------------------------- |
| `developer`     | string   | Developer email address                               |
| `display_name`  | string   | Developer display name                                |
| `intent`        | string   | Description of what the developer is working on       |
| `status`        | string   | Activity status: `active`, `inactive`, or `completed` |
| `started`       | string   | ISO 8601 timestamp when activity started              |
| `branches`      | string[] | Git branches associated with this activity            |
| `session_count` | int      | Number of active sessions for this activity           |
| `agents`        | string[] | Agent types being used                                |

Errors:

- 400: "missing slug"

Configuration is via the config API:

- `repofeed.enabled` - Whether repofeed is enabled (default: false)
- `repofeed.publish_interval_seconds` - How often to publish local state (default: 30)
- `repofeed.fetch_interval_seconds` - How often to fetch remote state (default: 60)
- `repofeed.completed_retention_hours` - How long to keep completed activities (default: 48)
- `repofeed.repos` - Map of repo slugs to enabled/disabled status

## WebSocket

### WS /ws/terminal/{sessionId}

Streams terminal output for a session.

Client -> server messages:

Input is sent as **binary WebSocket frames** (raw keystroke bytes, no JSON wrapper) to avoid serialization overhead on the hot path. Control messages are sent as JSON text frames:

```
Binary frame: raw keystroke bytes (e.g., "ls -la\r", arrow keys as ANSI escapes)
```

```json
{"type":"resize","data":"{\"cols\":120,\"rows\":30}"}
{"type":"diagnostic"}
{"type":"io-workspace-diagnostic"}
{"type":"syncResult","data":"{\"corrected\":true,\"diffRows\":[22,23,24]}"}
{"type":"gap","data":"{\"fromSeq\":\"42\"}"}
```

The `syncResult` message reports the result of a sync comparison. Sent after receiving a `sync` message from the server. Fields in `data` (JSON string):

- `corrected` (bool): whether xterm.js rows were surgically corrected
- `diffRows` (int[]): row indices that differed (empty if skipped due to activity guard)

The `gap` message requests replay of missing output log entries. Sent when the client detects a sequence number gap in received binary frames. Gap requests are debounced: only one gap request is sent until the gap is filled by sequential data. The server replies with individual per-entry frames (one frame per log entry, each tagged with its original sequence number) so the client can deduplicate by sequence number. Fields in `data` (JSON string):

- `fromSeq` (string): the first missing sequence number (stringified uint64)

Server -> client messages:

Binary frames contain sequenced terminal bytes. Each binary frame has an 8-byte big-endian uint64 sequence header followed by terminal data (which may be empty for seq-continuity frames). The first binary frame is the bootstrap snapshot (full screen capture with ANSI escape sequences and cursor positioning). Subsequent binary frames are incremental output from tmux control mode. The server always emits a frame for every sequence number, even when escape-sequence buffering holds back the entire event, to preserve sequence continuity. Frame encoding and escape-sequence buffering use caller-owned reusable buffers to amortize allocation to zero on the hot path. Sequence numbers enable gap detection: if the client sees a gap, it sends a `gap` message to request replay of missing entries from the server's output log. The output log requires a positive capacity (panics on zero). The client coalesces scroll-to-bottom calls via `requestAnimationFrame` to avoid forced layout reflows when processing burst output.

Text frames are JSON control messages:

```json
{"type":"displaced","content":"..."}
{"type":"bootstrapComplete"}
{"type":"stats","eventsDelivered":100,"eventsDropped":0,"bytesDelivered":50000,"bytesPerSec":1200,"controlModeReconnects":0,"syncChecksSent":0,"syncCorrections":0,"syncSkippedActive":0,"syncDisabled":true,"clientFanOutDrops":0,"fanOutDrops":0,"currentSeq":100,"logOldestSeq":0,"logTotalBytes":50000,"inputLatency":{"dispatchP50":0.1,"dispatchP99":0.3,"sendKeysP50":5.0,"sendKeysP99":12.0,"echoP50":2.0,"echoP99":8.0,"frameSendP50":0.05,"frameSendP99":0.2,"sampleCount":100,"outputChDepthP50":0,"outputChDepthP99":3,"echoDataLenP50":64,"echoDataLenP99":512},"tmuxHealth":{"samples":[120,135,110],"p50_us":120,"p99_us":200,"max_rtt_us":250,"count":51,"errors":0,"last_us":135,"uptime_s":255}}
{"type":"inputEcho","serverMs":7.5,"dispatchMs":0.1,"sendKeysMs":5.0,"echoMs":2.0,"frameSendMs":0.4}
{"type":"controlMode","attached":true}
{"type":"diagnostic","diagDir":"...","counters":{...},"findings":[...],"verdict":"...","tmuxScreen":"..."}
{"type":"io-workspace-stats","totalCommands":42,"totalDurationMs":1234.5,"triggerCounts":{"poller":30,"watcher":12},"counters":{"git_status":20,"git_fetch":10}}
{"type":"io-workspace-diagnostic","diagDir":"...","counters":{...},"findings":[...],"verdict":"..."}
{"type":"sync","screen":"<ANSI-escaped tmux capture>","cursor":{"row":24,"col":3,"visible":true},"forced":false}
```

| Type                      | Description                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| ------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `displaced`               | Connection displaced by another window viewing the same session                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `bootstrapComplete`       | Signals that bootstrap is complete and the client should enable gap detection. Sent after bootstrap binary frames and cursor restoration                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `stats`                   | Periodic pipeline diagnostics (dev mode only, every 2-3s). Includes sync counters: `syncChecksSent`, `syncCorrections`, `syncSkippedActive`, `syncDisabled`. Includes per-layer drop counters: `eventsDropped` (parser), `clientFanOutDrops` (client fan-out), `fanOutDrops` (tracker fan-out). Includes output log counters: `currentSeq`, `logOldestSeq`, `logTotalBytes`. Includes `inputLatency` (omitted when empty): P50/P99 for 4 server-side timing segments (dispatch, sendKeys, echo, frameSend) plus context fields (outputChDepth, echoDataLen) from a 200-sample ring buffer. Includes `tmuxHealth` (omitted when no samples): raw RTT probe samples in microseconds from a 720-sample ring buffer (5s interval), with P50/P99/max percentiles. The dashboard renders this as a histogram in the Tmux diagnostic widget. Remote sessions emit a simplified `stats` message containing only `inputLatency` (every 2s) |
| `inputEcho`               | Per-keystroke server-side latency breakdown (dev mode only). Sent immediately after the echo frame for the keystroke. `serverMs` is the total (dispatch + sendKeys + echo + frameSend). `dispatchMs`, `sendKeysMs`, `echoMs`, `frameSendMs` are the individual segment durations for this specific keystroke, enabling paired per-keystroke breakdown on the frontend. Emitted for both local and remote sessions                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `controlMode`             | tmux control mode attachment state changed                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `diagnostic`              | Response to a `diagnostic` request with capture data (dev mode only). Diagnostic directory includes: `meta.json` (counters, cursor state, automated findings), `screen-tmux.txt`, `screen-xterm.txt`, `screen-diff.txt` (ANSI-stripped comparison), `ringbuffer-backend.txt`, `ringbuffer-frontend.txt` (timestamped), `gap-stats.json` (gap detection telemetry), `cursor-xterm.json`, `tmux-health.json` (RTT probe time series in microseconds)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `io-workspace-stats`      | Periodic IO workspace telemetry stats (every 3s when io_workspace_telemetry enabled). Includes command counts, total duration, per-trigger and per-command-type breakdowns. Note: watcher-triggered refreshes skip `git fetch` (only local state queries), so `git_fetch` counts reflect poller/explicit triggers only. Origin query fetches, workspace fetches, and workspace git status updates all run concurrently within each poll cycle, with per-cycle caches deduplicating `git fetch` and `git worktree list` calls across workspaces sharing the same bare repo. Default branch detection (`git symbolic-ref`) is throttled to once per 60 seconds per repo                                                                                                                                                                                                                                                             |
| `io-workspace-diagnostic` | Response to an `io-workspace-diagnostic` request. Writes capture to `~/.schmux/diagnostics/` and returns counters, findings, verdict, and diagDir                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `sync`                    | Periodic defense-in-depth screen snapshot for desync detection (currently **disabled** — investigating whether it introduces color artifacts). When enabled: `screen` contains visible-screen-only `capture-pane -e -p` output. `cursor` contains position and visibility. `forced` is true when drops have been detected since the last sync. Gap detection + replay is the primary consistency mechanism; sync is a safety net only. The frontend compares plain-text content against its xterm.js buffer and surgically corrects differing rows on mismatch                                                                                                                                                                                                                                                                                                                                                                    |

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

Repofeed update notification:

```json
{
  "type": "repofeed_updated"
}
```

- Sent when the repofeed consumer fetches new data from remote developer files
- Clients should re-fetch `/api/repofeed` to get updated activity data

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

Updates an existing remote flavor. All fields except `id` are mutable. If `flavor` is omitted or empty, the existing value is preserved.

Request:

```json
{
  "flavor": "new-flavor-name",
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

Starts a connection to a remote host asynchronously. Every call creates a new host instance (multiple hosts per flavor are allowed). Returns immediately; poll `/api/remote/hosts` for status updates.

Request:

```json
{
  "flavor_id": "flavor-id"
}
```

Response (202):

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

Disconnects a remote host. With `?dismiss=true`, also removes all associated sessions, workspaces, and the host record from state.

Query parameters:

- `dismiss` (optional, boolean): When `true`, removes all sessions and workspaces associated with the host, deletes the host from state, and disconnects. When `false` (default), only disconnects.

Response: 204 No Content

Errors:

- 400: "Host ID required"
- 500: "Failed to update host: ..." / "Failed to save state"

### GET /api/remote/flavor-statuses

Returns all flavors with the status of all their hosts. Each flavor contains a `hosts` array with one entry per provisioned host instance.

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
    "hosts": [
      {
        "host_id": "remote-abc123",
        "hostname": "dev-001.example.com",
        "status": "connected",
        "connected": true
      },
      {
        "host_id": "remote-def456",
        "hostname": "dev-002.example.com",
        "status": "provisioning",
        "connected": false
      }
    ]
  }
]
```

Notes:

- Each host's `status` can be `"provisioning"`, `"connecting"`, `"connected"`, or `"disconnected"`
- `connected` is `true` when `status` is `"connected"`
- `hosts` may be empty (no hosts provisioned) or contain multiple entries (multi-instance)
- Uses real-time connection status from the remote manager when available; falls back to persisted state

## Environment API

Compare the system shell environment against the tmux server environment, and sync individual variables.

### GET /api/environment

Compare the current system environment (from a fresh login shell) against the tmux server's global environment.

The backend spawns `env -i HOME=$HOME USER=$USER SHELL=$SHELL TERM=xterm-256color $SHELL -l -i -c env` with a 10-second timeout, reads `tmux show-environment -g`, filters blocked keys, and returns the comparison.

Response:

```json
{
  "vars": [
    { "key": "PATH", "status": "differs" },
    { "key": "GOPATH", "status": "in_sync" },
    { "key": "NVM_DIR", "status": "system_only" },
    { "key": "LEGACY_VAR", "status": "tmux_only" }
  ],
  "blocked": ["TMUX", "TMUX_PANE", "SHLVL", "PWD", "OLDPWD", "_", "COLUMNS", "LINES"]
}
```

Status values:

- `in_sync` — key exists in both, values match
- `differs` — key exists in both, values differ
- `system_only` — exists in fresh shell but not in tmux server
- `tmux_only` — exists in tmux server but not in fresh shell

Blocked keys are excluded from comparison (tmux-internal, session-transient, terminal-specific). Prefix matches: `GHOSTTY_*`, `ITERM_*`, `npm_*`.

### POST /api/environment/sync

Sync a single environment variable from the system to the tmux server.

Request:

```json
{ "key": "PATH" }
```

Spawns a fresh login shell to get the current value, then calls `tmux set-environment -g KEY VALUE`. Returns 204 on success.

Errors:

- 400 if `key` is empty, blocked, or tmux-only (not present in system environment)
- 500 if the login shell or tmux command fails

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

### POST /api/dev/diagnostic-append

Receives frontend diagnostic artifacts and writes them to an existing diagnostic directory created by the WebSocket diagnostic handler. Dev mode only.

**Request body:**

```json
{
  "diagDir": "/path/to/.schmux/diagnostics/2026-03-19T...",
  "xtermScreen": "...",
  "screenDiff": "...",
  "ringBufferFrontend": "...",
  "gapStats": "{...}",
  "cursorXterm": "{...}",
  "scrollEvents": "[{...}, ...]",
  "scrollStats": "{...}",
  "wsEvents": "[{...}, ...]",
  "lifecycleEvents": "[{...}, ...]",
  "writeRaceStats": "{...}",
  "slowReactRenders": "[{...}, ...]"
}
```

All fields are optional strings except `diagDir` (required). Files are written best-effort (write errors are ignored).

**Files written to `diagDir`:**

- `screen-xterm.txt` — xterm.js visible viewport text
- `screen-diff.txt` — diff between tmux and xterm screens
- `ringbuffer-frontend.txt` — raw frame ring buffer
- `gap-stats.json` — gap detection counters
- `cursor-xterm.json` — xterm cursor position
- `scroll-events.json` — scroll state transition ring buffer (last 100 events)
- `scroll-stats.json` — scroll diagnostic counters (followLostCount, scrollSuppressedCount, scrollCoalesceHits, resizeCount, lastResizeTs, recreationCount)
- `ws-events.json` — WebSocket connection lifecycle events
- `lifecycle-events.json` — frontend terminal lifecycle event timeline
- `write-race-stats.json` — xterm.js write/render performance telemetry (parse timing, viewport sync counts, render duration, main thread stalls, buffer switches)
- `slow-react-renders.json` — React renders exceeding 50ms (SessionDetailPage profiler)

**Response:** `200 OK` (empty body)

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

---

## Timelapse Recording

### GET /api/timelapse

List all timelapse recordings in `~/.schmux/recordings/`. Returns `RecordingInfo[]` sorted newest-first.

### POST /api/timelapse/{recordingId}/export

Start async export to asciicast v2 (.cast). Returns `202 Accepted` or `200 OK` if cached.

### GET /api/timelapse/{recordingId}/download

Download exported .cast file. Returns `404` if not yet exported.

### DELETE /api/timelapse/{recordingId}

Delete recording and cached export. Returns `204`.

<!-- Test coverage: config getters, dashboard dispose/nickname guards, compound suppression, injector HandleEvent, secrets file migration, nudge summary parsing, curator response parsing, shell argument splitting, conflict state management, persona ID validation, model secrets validation, target-in-use checks, session manager initialization, tracker counters concurrency, embedded dashboard asset serving, timelapse recording, export, storage -->
