# GitHub Auth v1 Spec

**Goal**

- Add first-party authentication to the dashboard/API using GitHub OAuth.
- Auth is independent of `network_access`.
- If auth is enabled, all UI/API/WS access requires authentication.
- Keep structure extensible for future providers (OIDC), but only GitHub in v1.

**Non-Goals (v1)**

- Org/team/user allowlists.
- Multiple providers.
- Password or local accounts.
- CLI auth flow.

---

## Configuration & Secrets

### Config (`config.json`)

New fields under `access_control`:

- `auth.enabled` (bool, default false)
- `auth.provider` (string, default `github`)
- `auth.public_base_url` (string, required when auth enabled)
- `auth.session_ttl_minutes` (int, default 1440)
- `auth.tls` (object, required when auth enabled)
  - `auth.tls.cert_path` (string, required)
  - `auth.tls.key_path` (string, required)

Example:

```json
{
  "access_control": {
    "network_access": false,
    "auth": {
      "enabled": true,
      "provider": "github",
      "public_base_url": "https://schmux.local",
      "session_ttl_minutes": 1440,
      "tls": {
        "cert_path": "/path/to/schmux.local.pem",
        "key_path": "/path/to/schmux.local-key.pem"
      }
    }
  }
}
```

### Secrets (`~/.schmux/secrets.json`)

Extend format to allow auth credentials while remaining backward compatible.

Preferred structure:

```json
{
  "models": {
    "anthropic": { "ANTHROPIC_AUTH_TOKEN": "..." }
  },
  "auth": {
    "github": {
      "client_id": "...",
      "client_secret": "..."
    }
  }
}
```

Legacy structure (must still work):

```json
{
  "anthropic": { "ANTHROPIC_AUTH_TOKEN": "..." }
}
```

### Validation Rules

When auth is enabled:

- `public_base_url` is required.
- `public_base_url` must be `https://...` (allow `http://localhost` only).
- TLS config is required and must point to readable cert/key files.
- GitHub `client_id` and `client_secret` must exist in secrets.
- Failure to meet these blocks daemon start with a clear error.

---

## Auth Flow

### Endpoints

- `GET /auth/login`
  - Redirects to GitHub OAuth authorize endpoint.
  - Uses `state`.
- `GET /auth/callback`
  - Exchanges code for access token.
  - Fetches GitHub user profile (`/user`).
  - Creates session and sets cookie.
- `POST /auth/logout`
  - Clears session cookie.
- `GET /auth/me`
  - Returns current user info when authenticated.

### Session

- Cookie-based session (signed/encrypted).
- Cookie attributes:
  - `HttpOnly`
  - `SameSite=Lax`
  - `Secure` when `public_base_url` is https
- TTL from `session_ttl_minutes`.

### User Model (minimal)

- GitHub `id`, `login`, `name`, `avatar_url`.

---

## Access Enforcement

### UI

- If auth enabled and unauthenticated: redirect to `/auth/login`.
- `/` and all SPA routes are protected server-side.

### API

- All `/api/*` routes require auth.

### WebSocket

- `/ws/*` requires auth cookie.
- WebSocket origin checks must allow only the derived allowed origins (must include `public_base_url`).

### CORS

When auth is enabled:

- `Access-Control-Allow-Origin` must be explicit (no `*`).
- `Access-Control-Allow-Credentials: true`.
- Allowed origins are derived from `public_base_url` (must include it).

---

## Dashboard UI (Advanced Config Tab)

### Access Control section

- Existing: Network Access toggle.
- New: Authentication
  - Enable auth toggle
  - Provider (locked to GitHub for v1)
  - Public Base URL (text input)
  - Allowed Origins (multi input)
  - Session TTL (minutes)

### Secrets UI

- Reuse existing "Model secrets" pattern for auth secrets.
- Add “Auth secrets (GitHub)” modal/input:
  - `client_id`
  - `client_secret`

### Restart behavior

- Any auth change requires daemon restart.

---

## Documentation Updates

- `docs/api.md`: authentication, auth endpoints, cookie requirements, CORS changes.
- `docs/web.md`: login required when auth enabled.
- `docs/dev/README.md`: configuration + secrets.json format + mkcert/tls setup guidance.

---

## Implementation Plan (high-level)

1. Extend config schema + validation for `access_control.auth`.
2. Extend `secrets.json` loader to support `auth` while keeping legacy format.
3. Add auth handlers (GitHub OAuth flow, session cookies).
4. Add auth middleware for UI/API/WS.
5. Update CORS + WebSocket origin checks to use derived allowed origins (must include `public_base_url`).
6. Add UI controls and secrets modal in Config page.
7. Update docs and API contract.
