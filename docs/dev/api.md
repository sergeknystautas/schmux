# API Reference

HTTP API and WebSocket protocol for the schmux dashboard.

---

## HTTP API

Base URL: `http://localhost:7337`

All responses are JSON unless otherwise specified.

---

## Sessions

### GET /api/sessions

List all sessions.

**Response:**
```json
[
  {
    "id": "abc123",
    "workspace_id": "myproject-001",
    "target": "claude",
    "nickname": "",
    "status": "running",
    "created_at": "2025-01-15T10:30:00Z",
    "last_activity": "2025-01-15T10:35:00Z"
  }
]
```

### POST /api/sessions

Spawn new sessions.

**Request:**
```json
{
  "workspace_id": "myproject-001",
  "targets": ["claude", "codex"],
  "prompt": "Implement feature X",
  "nickname": "feature-x"
}
```

**Response:**
```json
{
  "sessions": ["abc123", "def456"],
  "failures": []
}
```

### DELETE /api/sessions/:id

Dispose a session.

**Response:** `204 No Content`

---

## Workspaces

### GET /api/workspaces

List all workspaces.

**Response:**
```json
[
  {
    "id": "myproject-001",
    "repo": "myproject",
    "branch": "main",
    "git_dirty": true,
    "git_ahead": 2,
    "git_behind": 0
  }
]
```

### POST /api/workspaces

Create a new workspace.

**Request:**
```json
{
  "repo": "myproject",
  "branch": "feature-x"
}
```

**Response:**
```json
{
  "id": "myproject-001"
}
```

### POST /api/workspaces/:id/refresh-overlay

Refresh overlay files for a workspace.

**Response:** `200 OK` or `400 Bad Request` (if active sessions exist)

---

## Config

### GET /api/config

Get daemon configuration.

**Response:**
```json
{
  "workspace_path": "~/schmux-workspaces",
  "repos": [...],
  "run_targets": [...],
  "quick_launch": [...]
}
```

### PUT /api/config

Update daemon configuration.

**Request:** Same as GET response

**Response:** `200 OK`

---

## Built-in Commands

### GET /api/builtin-commands

Get built-in command templates.

**Response:**
```json
[
  {
    "name": "code review - local",
    "prompt": "please use the code review ai plugin to evaluate the local changes"
  }
]
```

---

## WebSocket Protocol

### Terminal Stream

Connect to: `ws://localhost:7337/ws/terminal/:sessionId`

**Messages from server:**
```json
{
  "type": "output",
  "data": "terminal output here"
}
```

**Messages from client:**
```json
{
  "type": "resize",
  "width": 120,
  "height": 40
}
```

**Connection close:**
- Server closes when session is disposed
- Client can close at any time (reconnect supported)

---

## Error Responses

All endpoints may return error responses:

**400 Bad Request**
```json
{
  "error": "Invalid request: workspace not found"
}
```

**500 Internal Server Error**
```json
{
  "error": "Failed to spawn session"
}
```

---

## CORS

CORS is enabled for all origins in development. In production, configure allowed origins via environment variable.

---

## See Also

- [Architecture](architecture.md) — Overall system architecture
- [React Architecture](react.md) — Frontend API integration patterns
