# Remote Access Authentication

## Problem

Remote access uses Cloudflare quick tunnels which generate a new random `*.trycloudflare.com` URL each session. GitHub OAuth requires a fixed callback URL registered in advance, making it incompatible with ephemeral tunnel URLs.

## Solution: Token → Nonce → Password Authentication

Three-step authentication using a one-time token (delivered via notification), a short-lived nonce (prevents token from persisting in browser history), and a user-configured password verified with bcrypt.

### Auth Flow

```
┌──────────┐     ┌────────────┐     ┌────────────┐     ┌────────────┐
│  Tunnel   │────▶│  Token URL │────▶│  Password  │────▶│  Dashboard │
│ connects  │     │ (ntfy/cmd) │     │   form     │     │  (cookie)  │
└──────────┘     └────────────┘     └────────────┘     └────────────┘
  generates        one-time           nonce-scoped       HMAC-signed
  token+secret     consumed on        5-min expiry       24h session
                   first visit        5 max attempts
```

1. User starts tunnel from dashboard or CLI (`schmux remote on`)
2. Cloudflared connects → server generates a one-time token (32 bytes, `crypto/rand`, hex-encoded) and a new 32-byte session secret
3. Notification (ntfy.sh and/or custom command) delivers: `https://<random>.trycloudflare.com/remote-auth?token=<token>`
4. User opens link → token is **consumed immediately** (one-time use) → server generates a short-lived nonce (16 bytes, 5-minute TTL) → redirects to `/remote-auth?nonce=<nonce>`
5. User sees password form (the token no longer appears in the URL or browser history)
6. User enters password → server validates nonce + bcrypt-compares password against stored hash
7. On success: nonce is deleted, server sets `schmux_remote` session cookie + `schmux_csrf` cookie, redirects to `/`
8. On failure: failure counter incremented, form re-rendered with error. At 5 failures: all nonces deleted, session locked

**Why three steps instead of two:**

- The token in the notification URL is a secret. If the user's browser history syncs across devices, or if the URL appears in server logs, a direct token→password flow would leave the token exposed. The immediate redirect to a nonce removes the token from the URL bar before the user interacts with the page.
- Nonces are server-side only (never in notifications) and expire in 5 minutes.

---

## Security Architecture

### Layer 1: Transport — Cloudflare TLS

All traffic between the remote device and Cloudflare's edge is HTTPS. Traffic between Cloudflare and the local machine travels through the `cloudflared` tunnel process over a local connection (never leaves the machine).

### Layer 2: Authentication — Token + Password

Neither factor alone grants access:

| Factor   | Source                         | Lifetime            | Purpose                                                      |
| -------- | ------------------------------ | ------------------- | ------------------------------------------------------------ |
| Token    | `crypto/rand`, 32 bytes, hex   | One-time use        | Proves the user received the notification                    |
| Nonce    | `crypto/rand`, 16 bytes, hex   | 5 minutes           | Scopes the password form, keeps token out of browser history |
| Password | User-configured, bcrypt-hashed | Persisted in config | Proves the user knows the password                           |

### Layer 3: Session — HMAC-Signed Cookie

After successful authentication:

- **Cookie name:** `schmux_remote`
- **Value:** `<unix_timestamp>.<HMAC-SHA256(timestamp, session_secret)>`
- **Attributes:** `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`, `MaxAge=86400` (24 hours)
- **Server-side validation:** Verifies HMAC signature, checks timestamp is within 24 hours, requires non-empty session secret

The session secret is regenerated on each tunnel start, which cryptographically invalidates all cookies from previous tunnel sessions.

### Layer 4: CSRF Protection

State-changing endpoints (`/api/remote-access/on`, `/off`, `/set-password`, `/test-notification`) are wrapped with `withAuthAndCSRF`:

- On successful auth, the server sets a `schmux_csrf` cookie (`HttpOnly=false`, `SameSite=Strict`, `Secure=true`)
- The frontend reads this cookie and sends it as the `X-CSRF-Token` header on POST requests
- The server validates that the header value matches the cookie value using `hmac.Equal`
- Local requests (from loopback without forwarding headers) are exempt from CSRF checks

### Layer 5: CORS Origin Restriction

When a tunnel is active, `isAllowedOrigin()` restricts CORS origins to:

1. The tunnel URL itself (`https://<random>.trycloudflare.com`)
2. Localhost (`http://localhost:{port}`, `http://127.0.0.1:{port}`)

All other origins are rejected with 403 Forbidden — even if `network_access` is enabled. This prevents cross-origin requests from malicious sites that might try to use the authenticated session cookie.

When no tunnel is active, normal CORS rules apply (localhost, configured `public_base_url`, or any origin if `network_access` is enabled).

### Layer 6: Rate Limiting

Two independent rate limiters protect the auth endpoint:

**IP-based rate limiter on `/remote-auth` POST:**

- 5 requests per minute per IP address
- Uses `Cf-Connecting-IP` header (when tunnel is active and request is from loopback) to rate-limit by the actual remote IP, not cloudflared's loopback address
- Returns 429 with `Retry-After: 60` header

**Global lockout (per tunnel session):**

- After 5 failed password attempts, all nonces are deleted and the session is locked
- Requires generating a new auth URL (restarting the tunnel or having the server generate a new token)
- The lockout counter resets when the tunnel restarts

### Layer 7: Local Request Bypass

Local requests (from loopback addresses without Cloudflare forwarding headers) bypass tunnel-only auth. The local user always has unrestricted access to the dashboard — the tunnel auth exists to protect remote access, not local access.

**Detection logic (`isLocalRequest`):**

1. Check `RemoteAddr` is a loopback IP (`127.0.0.1`, `::1`)
2. If tunnel is active AND request has `Cf-Connecting-IP` or `X-Forwarded-For` headers → **not local** (it's a remote request proxied through cloudflared)
3. If tunnel is active AND no forwarding headers → **local** (genuine local browser request)

This applies consistently to:

- HTTP API handlers (`withAuth` middleware)
- Dashboard WebSocket (`/ws/dashboard`)
- Terminal WebSocket (`/ws/terminal/{id}`)
- Provision WebSocket (`/ws/provision/{id}`)
- CSRF validation (local requests are CSRF-exempt)

### Layer 8: Binary Verification

When `cloudflared` is auto-downloaded:

- **macOS:** Verified via `codesign -v --deep` — checks the binary is signed by Cloudflare Inc. (Apple Developer Team ID: `68WVV388M8`). Unsigned or tampered binaries are rejected.
- **Linux:** No signature verification available (logged as a warning). `AllowAutoDownload` defaults to `false`, requiring explicit opt-in.
- **Download size limit:** HTTP response body is limited to 200MB via `io.LimitReader`
- **Decompression bomb protection:** tar entries are rejected if `header.Size` exceeds 200MB, and extraction uses `io.LimitReader` around the tar entry to enforce the limit at runtime regardless of header claims

### Layer 9: Non-Loopback Bind Rejection

`tunnel.Manager.Start()` rejects tunnel start when the dashboard server is bound to a non-loopback address (e.g., `0.0.0.0`). This prevents exposing an unauthenticated listener on the LAN that would bypass the cloudflared proxy chain.

---

## Server State

```go
remoteToken         string                  // current one-time token (empty = consumed or no tunnel)
remoteTokenFailures int                     // failed password attempts for current tunnel session
remoteTokenMu       sync.Mutex              // protects all remote* fields
remoteSessionSecret []byte                  // 32-byte HMAC key for signing cookies, regenerated per tunnel
remoteTunnelURL     string                  // current tunnel URL (for CORS validation)
remoteNonces        map[string]*remoteNonce // active nonces (token → nonce exchange results)
```

### State lifecycle

| Event                                        | Token               | Failures    | Secret            | Nonces          |
| -------------------------------------------- | ------------------- | ----------- | ----------------- | --------------- |
| Tunnel connects                              | Generated (32B hex) | Reset to 0  | Regenerated (32B) | Cleared         |
| Token consumed (GET with valid token)        | Cleared to ""       | Unchanged   | Unchanged         | New nonce added |
| Successful auth (POST with correct password) | Unchanged           | Unchanged   | Unchanged         | Nonce deleted   |
| Failed auth attempt                          | Unchanged           | Incremented | Unchanged         | Unchanged       |
| 5th failure (lockout)                        | Unchanged           | 5           | Unchanged         | All deleted     |
| Tunnel stops (`ClearRemoteAuth`)             | Cleared             | Reset to 0  | Cleared to nil    | Cleared         |
| Password changed while tunnel active         | Unchanged           | Unchanged   | Regenerated       | Unchanged       |

Password change regenerates the session secret, which immediately invalidates all existing remote session cookies. Users must re-authenticate through the full token→nonce→password flow.

---

## Endpoints

### `GET /remote-auth` — Unauthenticated

Three cases based on query parameters:

| Query            | Behavior                                                                                |
| ---------------- | --------------------------------------------------------------------------------------- |
| `?token=<token>` | Validates token, consumes it (one-time), generates nonce, redirects to `?nonce=<nonce>` |
| `?nonce=<nonce>` | Validates nonce (exists, not expired, not locked out), renders password form            |
| (no params)      | Renders instructions page ("check your notification app")                               |

### `POST /remote-auth` — Unauthenticated, Rate-Limited

- **Body:** `application/x-www-form-urlencoded` with `nonce` and `password` fields
- **Body size limit:** `http.MaxBytesReader` with `maxBodySize`
- **Rate limit:** 5 requests/minute per IP (429 if exceeded)
- **Validation:** Nonce must exist and not be expired (5 min). Password bcrypt-compared against stored hash.
- **On success:** Nonce deleted, `schmux_remote` + `schmux_csrf` cookies set, redirect to `/`
- **On failure:** Failure counter incremented, form re-rendered with error and remaining attempts
- **On lockout (5 failures):** Nonce deleted, locked-out message displayed
- **Concurrency:** Lock released during expensive bcrypt comparison, re-acquired with double-check on nonce existence afterward

### `POST /api/remote-access/set-password` — Authenticated + CSRF

- **Body:** `{"password": "<password>"}` (minimum 6 characters)
- **Storage:** bcrypt hash written to `~/.schmux/config.json` (`remote_access.password_hash`)
- **Side effect:** If tunnel is active, session secret is regenerated (invalidates existing remote cookies)
- **API response:** `{"ok": true}`
- **Note:** Plaintext password never touches disk

### `POST /api/remote-access/on` — Authenticated + CSRF

Starts the cloudflared tunnel. Requires password to be configured.

### `POST /api/remote-access/off` — Authenticated + CSRF

Stops the tunnel and calls `ClearRemoteAuth()` to wipe all remote auth state.

### `GET /api/remote-access/status` — Authenticated

Returns current tunnel state (off/starting/connected/error), URL, and configuration.

---

## Auth Middleware

The `withAuth` middleware resolves credentials in order:

1. **No auth required** (`requiresAuth()` returns false) → allow
2. **Local request + tunnel-only auth** (no GitHub OAuth, just tunnel active) → allow (local bypass)
3. **GitHub OAuth cookie** (`schmux_auth`) → validate HMAC signature + expiry → allow if valid
4. **Remote session cookie** (`schmux_remote`) → validate HMAC signature + timestamp → allow if valid
5. **Reject** → 401 Unauthorized

`requiresAuth()` returns true when either:

- GitHub OAuth is enabled (`auth.enabled` in config), OR
- A tunnel is active (session secret exists)

The `/remote-auth` endpoint is **excluded from auth middleware** — it handles its own token/nonce-based protection.

---

## Password Entry Page

Self-contained HTML served by the Go backend (`renderPasswordPage`). No React dependency — the page must work before the user is authenticated to load the SPA.

**Properties:**

- Inline CSS (no external stylesheets or JavaScript)
- Dark/light mode via `prefers-color-scheme` media query
- Standard `<form method="POST">` to `/remote-auth`
- XSS prevention: nonce and error message values are escaped via `html.EscapeString()`
- Shows remaining attempts count when failures > 0
- Locked-out state shows error without form

---

## Notification

When the tunnel connects, `HandleTunnelConnected` sends the full auth URL (with token) via:

1. **ntfy.sh** — `POST https://ntfy.sh/{topic}` with title "schmux remote access" and body containing the auth URL
2. **Custom command** — Executed via `sh -c <command>` with `SCHMUX_REMOTE_URL` env var set to the **base tunnel URL** (without token, to prevent token leakage to arbitrary shell commands)

Both can be configured simultaneously.

---

## Config

```json
{
  "remote_access": {
    "enabled": true,
    "timeout_minutes": 0,
    "password_hash": "$2a$10$...",
    "allow_auto_download": false,
    "notify": {
      "ntfy_topic": "my-secret-topic",
      "command": ""
    }
  }
}
```

| Field                 | Default | Description                                                                  |
| --------------------- | ------- | ---------------------------------------------------------------------------- |
| `enabled`             | `true`  | Kill switch. When false, tunnel start is rejected with 403.                  |
| `timeout_minutes`     | `0`     | Auto-kill tunnel after N minutes. 0 = no timeout.                            |
| `password_hash`       | `""`    | bcrypt hash of the user's password. Never exposed via API.                   |
| `allow_auto_download` | `false` | Whether to auto-download cloudflared if not found. Requires explicit opt-in. |
| `notify.ntfy_topic`   | `""`    | ntfy.sh topic for push notifications.                                        |
| `notify.command`      | `""`    | Shell command to run when tunnel URL is available.                           |

The API (`GET /api/config`) exposes `password_hash_set: bool` — never the hash itself.

---

## Test Coverage

The remote access auth system has comprehensive test coverage across multiple files:

- **`handlers_remote_auth_test.go`** — Unit tests for cookie validation, nonce lifecycle, token consumption, rate limiting, XSS escaping, password validation, session invalidation, local bypass
- **`tunnel_e2e_test.go`** — End-to-end security tests: full auth flow, CSRF attacks from malicious origins, CORS validation, nonce reuse, cookie replay across tunnel sessions, brute force lockout, rate limiting by IP, WebSocket auth for tunneled/local/authenticated requests, local access unaffected by tunnel, password change revoking sessions
- **`handlers_remote_access_test.go`** — Remote access on/off/status endpoint tests
- **`cloudflared_test.go`** — Binary verification, download URL generation, decompression bomb protection
- **`csrf.test.ts` / `api-csrf.test.ts`** — Frontend CSRF token reading and header inclusion
