# API Contract

This document defines the daemon HTTP API contract. It is intentionally client-agnostic. If behavior changes, update this doc first and treat any divergence as a breaking change.

Base URL: `http://localhost:7337`

Doc-gate policy:
- Any API-affecting code change must update `docs/api.md`. CI enforces this rule.

General conventions:
- JSON requests/responses use `Content-Type: application/json`.
- Many error responses use plain text via `http.Error`; do not assume JSON unless specified.
- CORS: requests are allowed from `http://localhost:7337` and `http://127.0.0.1:7337`. When `network_access` is enabled, any origin is allowed.

## Endpoints

### GET /api/healthz
Health check with version information.

Response:
```json
{
  "status":"ok",
  "version":"1.0.0"
}
```

If a newer version is available, the response includes:
```json
{
  "status":"ok",
  "version":"0.9.0",
  "latest_version":"1.0.0",
  "update_available":true
}
```

### POST /api/update
Triggers a self-update to the latest version from GitHub releases.

The update runs synchronously. On success, the daemon shuts down and must be restarted manually.

Response (200):
```json
{
  "status":"ok",
  "message":"Update successful. Restart schmux to use the new version."
}
```

Errors:
- 405: "Method not allowed" (GET requests rejected)
- 409 with JSON: `{"error":"update already in progress"}`
- 500 with JSON: `{"error":"update failed: ..."}` (includes specific error reason)

Note: Dev builds (version "dev") cannot be updated via this endpoint.

### GET /api/hasNudgenik
Returns whether NudgeNik is available (currently always true).

Response:
```json
{"available":true}
```

### GET /api/askNudgenik/{sessionId}
Ask NudgeNik to analyze the latest agent response for a session.

Response (200):
```json
{
  "state":"...",
  "confidence":"...",
  "evidence":["..."],
  "summary":"..."
}
```

Errors:
- 400: "No response found in session output"
- 404: "session not found"
- 503: "Claude agent not found. Please run agent detection first."
- 500: "Failed to ask nudgenik: ..."

### GET /api/sessions
Returns workspaces and their sessions (hierarchical).

Response:
```json
[
  {
    "id":"workspace-id",
    "repo":"repo-url-or-name",
    "branch":"branch",
    "path":"/path/to/workspace",
    "session_count":1,
    "git_dirty":false,
    "git_ahead":0,
    "git_behind":0,
    "sessions":[
      {
        "id":"session-id",
        "target":"target-name",
        "branch":"branch",
        "nickname":"optional",
        "created_at":"YYYY-MM-DDTHH:MM:SS",
        "last_output_at":"YYYY-MM-DDTHH:MM:SS",
        "running":true,
        "attach_cmd":"tmux attach ...",
        "nudge_state":"optional",
        "nudge_summary":"optional"
      }
    ]
  }
]
```

### POST /api/workspaces/scan
Scans workspace directory and reconciles state.

Response:
```json
{
  "added":0,
  "removed":0,
  "updated":0
}
```

Errors:
- 500 with plain text: "Failed to scan workspaces: ..."

### POST /api/workspaces/{workspaceId}/refresh-overlay
Refresh overlay files for a workspace.

Response:
```json
{"status":"ok"}
```

Errors:
- 400 with JSON: `{"error":"..."}`

### POST /api/spawn
Spawn sessions.

Request:
```json
{
  "repo":"repo-url",
  "branch":"branch",
  "prompt":"optional",
  "nickname":"optional",
  "targets":{"target-name":1},
  "workspace_id":"optional"
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

Response (array of results):
```json
[
  {
    "session_id":"session-id",
    "workspace_id":"workspace-id",
    "target":"target-name",
    "prompt":"optional",
    "nickname":"optional"
  }
]
```

Errors are per-result:
```json
[
  {
    "target":"target-name",
    "error":"..."
  }
]
```

### POST /api/dispose/{sessionId}
Dispose a session.

Response:
```json
{"status":"ok"}
```

Errors:
- 400: "session ID is required"
- 500: "Failed to dispose session: ..."

### POST /api/dispose-workspace/{workspaceId}
Dispose a workspace.

Response:
```json
{"status":"ok"}
```

Errors:
- 400 with JSON: `{"error":"..."}` (e.g., dirty workspace)

### PUT/PATCH /api/sessions-nickname/{sessionId}
Update a session nickname.

Request:
```json
{"nickname":"new name"}
```

Response:
```json
{"status":"ok"}
```

Errors:
- 409 with JSON: `{"error":"nickname already in use"}`
- 500: "Failed to rename session: ..."

### GET /api/config
Returns the current config.

Response:
```json
{
  "workspace_path":"/path",
  "repos":[{"name":"repo","url":"https://..."}],
  "run_targets":[{"name":"target","type":"promptable","command":"...","source":"user"}],
  "quick_launch":[{"name":"preset","target":"target","prompt":"optional"}],
  "variants":[{"name":"...","enabled":true,"env":{"KEY":"VALUE"}}],
  "nudgenik":{"target":"optional","viewed_buffer_ms":0,"seen_interval_ms":0},
  "terminal":{"width":0,"height":0,"seed_lines":0,"bootstrap_lines":0},
  "sessions":{
    "dashboard_poll_interval_ms":0,
    "git_status_poll_interval_ms":0,
    "git_clone_timeout_ms":0,
    "git_status_timeout_ms":0
  },
  "xterm":{
    "mtime_poll_interval_ms":0,
    "query_timeout_ms":0,
    "operation_timeout_ms":0,
    "max_log_size_mb":0,
    "rotated_log_size_mb":0
  },
  "access_control":{
    "network_access":false
  },
  "needs_restart":false
}
```

### POST/PUT /api/config
Update the config. All fields are optional; omitted fields are unchanged.

Request:
```json
{
  "workspace_path":"/path",
  "repos":[{"name":"repo","url":"https://..."}],
  "run_targets":[{"name":"target","type":"promptable","command":"...","source":"user"}],
  "quick_launch":[{"name":"preset","target":"target","prompt":"optional"}],
  "variants":[{"name":"...","enabled":true,"env":{"KEY":"VALUE"}}],
  "nudgenik":{"target":"optional","viewed_buffer_ms":0,"seen_interval_ms":0},
  "terminal":{"width":120,"height":30,"seed_lines":1000,"bootstrap_lines":200},
  "sessions":{
    "dashboard_poll_interval_ms":0,
    "git_status_poll_interval_ms":0,
    "git_clone_timeout_ms":0,
    "git_status_timeout_ms":0
  },
  "xterm":{
    "mtime_poll_interval_ms":0,
    "query_timeout_ms":0,
    "operation_timeout_ms":0,
    "max_log_size_mb":0,
    "rotated_log_size_mb":0
  },
  "access_control":{
    "network_access":false
  }
}
```

Response:
- 200: `{"status":"ok","message":"Config saved and reloaded. Changes are now in effect."}`
- 200 (warning when workspace_path changes with existing sessions/workspaces):
```json
{
  "warning":"...",
  "session_count":0,
  "workspace_count":0,
  "requires_restart":true
}
```

Errors:
- 400 for validation errors (plain text)
- 500 for save/reload errors (plain text)

### GET /api/detect-tools
Returns detected run targets.

Response:
```json
{
  "tools":[{"name":"tool","command":"...","source":"config"}]
}
```

### GET /api/variants
Lists available variants and whether they are configured.

Response:
```json
{
  "variants":[
    {
      "name":"variant",
      "display_name":"Variant",
      "base_tool":"tool",
      "required_secrets":["KEY"],
      "usage_url":"https://...",
      "configured":true
    }
  ]
}
```

### GET /api/variants/{name}/configured
Response:
```json
{"configured":true}
```

### POST /api/variants/{name}/secrets
Set secrets for a variant.

Request:
```json
{"secrets":{"KEY":"VALUE"}}
```

Response:
```json
{"status":"ok"}
```

Errors:
- 400: missing secrets or invalid payload (plain text)
- 500: "Failed to save secrets: ..."

### DELETE /api/variants/{name}/secrets
Delete secrets for a variant.

Response:
```json
{"status":"ok"}
```

Errors:
- 400: "variant is in use by nudgenik or quick launch"

### GET /api/builtin-quick-launch
Returns built-in quick launch presets.

Response:
```json
[
  {"name":"Preset","target":"target","prompt":"prompt text"}
]
```

### GET /api/diff/{workspaceId}
Returns git diff for a workspace (tracked files + untracked).

Response:
```json
{
  "workspace_id":"workspace-id",
  "repo":"repo",
  "branch":"branch",
  "files":[
    {
      "old_path":"optional",
      "new_path":"file",
      "old_content":"optional",
      "new_content":"optional",
      "status":"added|modified|deleted|renamed|untracked"
    }
  ]
}
```

Errors:
- 404: "workspace not found"
- 400: "workspace ID is required"

### POST /api/open-vscode/{workspaceId}
Opens VS Code in a new window for the workspace.

Response:
```json
{"success":true,"message":"You can now switch to VS Code."}
```

Errors:
- 404 with JSON if workspace not found or directory missing
- 404 with JSON if `code` command not found in PATH
- 500 with JSON on launch failure

### GET /api/overlays
Returns overlay information for all repos.

Response:
```json
{
  "overlays":[
    {"repo_name":"repo","path":"/path","exists":true,"file_count":0}
  ]
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
```

Server -> client messages:
```json
{"type":"full","content":"..."}   // initial full content (with ANSI state)
{"type":"append","content":"..."} // incremental content
{"type":"reconnect","content":"Log rotated, please reconnect"}
```

Errors:
- 400: "session ID is required"
- 410: "session not running"
