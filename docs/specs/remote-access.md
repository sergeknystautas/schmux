# Remote Access

## Overview

Remote Access lets you access your schmux dashboard from your phone (or any device) over the internet, even when your laptop is behind residential NAT. It uses Cloudflare's free quick tunnels (`trycloudflare.com`) to create an ephemeral HTTPS URL — no account, no config, no port forwarding.

Authentication uses a three-step token → nonce → password flow designed specifically for ephemeral tunnel URLs where traditional OAuth callback URLs don't work.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         User's Machine                              │
│                                                                     │
│  ┌─────────────┐     ┌──────────────────┐     ┌────────────────┐    │
│  │ cloudflared │◀───▶│ Dashboard Server │◀───▶│ tmux Sessions  │    │
│  │  (tunnel)   │     │  localhost:7337  │     │  (AI agents)   │    │
│  └──────┬──────┘     └──────────────────┘     └────────────────┘    │
│         │                     ▲                                     │
│         │                     │ direct connection                   │
│         │              ┌──────┴──────┐                              │
│         │              │ Local       │                              │
│         │              │ Browser     │                              │
│         │              └─────────────┘                              │
└─────────┼───────────────────────────────────────────────────────────┘
          │ encrypted tunnel
          │
┌─────────▼───────────────────────────────────────────────────────────┐
│              Cloudflare Edge (TLS termination)                      │
│         https://<random>.trycloudflare.com                          │
└─────────┬───────────────────────────────────────────────────────────┘
          │ HTTPS
          │
┌─────────▼───────────────────────────────────────────────────────────┐
│              Remote Device (phone, tablet, etc.)                    │
│         Browser → password auth → full dashboard access             │
└─────────────────────────────────────────────────────────────────────┘
```

### Components

**`internal/tunnel/manager.go`** — Tunnel lifecycle manager:

- Finds `cloudflared` on PATH, falls back to `~/.schmux/bin/cloudflared`, auto-downloads if configured
- Spawns and supervises the `cloudflared` process
- Parses the ephemeral URL from `cloudflared`'s stderr output via regex
- Exposes tunnel state machine: `off` → `starting` → `connected` (or `error`)
- Kills `cloudflared` on stop, shutdown, or timeout expiry
- Fires `OnStatusChange` callback for WebSocket broadcasts and auth setup

**`internal/tunnel/cloudflared.go`** — Binary management:

- `FindCloudflared(schmuxBinDir)` — searches PATH then `~/.schmux/bin/`
- `EnsureCloudflared(schmuxBinDir)` — finds or downloads the correct platform binary
- `extractTgz()` — extracts macOS `.tgz` archives with decompression bomb protection
- `verifyCloudflaredSignature()` — macOS codesign verification

**`internal/tunnel/notify.go`** — Notification dispatcher:

- Sends auth URL via ntfy.sh and/or custom shell command
- Token leakage prevention: custom commands receive base tunnel URL (no token) via `$SCHMUX_REMOTE_URL` env var

**`internal/dashboard/handlers_remote_auth.go`** — Auth flow handlers:

- Token → nonce exchange, password form rendering, bcrypt validation
- Session cookie issuance, CSRF cookie issuance

**`internal/dashboard/handlers_remote_access.go`** — Management endpoints:

- Start/stop tunnel, status, set password, test notification

**`internal/dashboard/auth.go`** — Auth middleware:

- `withAuth` — checks GitHub OAuth or remote session cookie
- `withAuthAndCSRF` — adds CSRF validation for state-changing endpoints
- `isTrustedRequest` — distinguishes trusted from untrusted requests

---

## User Flow

```
 User                    Dashboard              cloudflared          Cloudflare
  │                         │                       │                    │
  │── schmux remote on ────▶│                       │                    │
  │                         │── spawn ─────────────▶│                    │
  │                         │                       │── connect ────────▶│
  │                         │◀── stderr: URL ───────│                    │
  │                         │                       │                    │
  │                         │── generate token ─────│                    │
  │                         │── generate secret ────│                    │
  │                         │── send notification ──│                    │
  │                         │   (ntfy / command)    │                    │
  │                         │                       │                    │
  │   [phone gets ntfy push with auth URL]          │                    │
  │                         │                       │                    │
  │── open URL on phone ───▶│◀──────────────────────│◀───────────────────│
  │   (token in URL)        │── consume token       │                    │
  │                         │── generate nonce      │                    │
  │◀── redirect to ?nonce= ─│                       │                    │
  │                         │                       │                    │
  │── enter password ──────▶│                       │                    │
  │                         │── bcrypt verify       │                    │
  │                         │── delete nonce        │                    │
  │                         │── set session cookie  │                    │
  │◀── redirect to / ───────│                       │                    │
  │                         │                       │                    │
  │   [full dashboard access with cookie]           │                    │
```

1. User starts tunnel from dashboard or CLI (`schmux remote on`)
2. schmux checks for `cloudflared` on PATH, then `~/.schmux/bin/cloudflared`, auto-downloads if allowed
3. schmux spawns `cloudflared tunnel --url localhost:{port}` as a managed subprocess
4. `cloudflared` prints the ephemeral `*.trycloudflare.com` URL to stderr — schmux parses it
5. Server generates a one-time auth token and a new 32-byte session secret
6. Notification delivers auth URL: `https://<random>.trycloudflare.com/remote-auth?token=<token>`
7. User opens link → token consumed → redirected to nonce-scoped password form
8. User enters password → bcrypt verified → session cookie set → dashboard loads
9. User runs `schmux remote off` → schmux kills `cloudflared`, clears all auth state

---

## Authentication

### Why Not GitHub OAuth?

Cloudflare quick tunnels generate a new random `*.trycloudflare.com` URL each session. GitHub OAuth requires a fixed callback URL registered in advance, making it incompatible with ephemeral tunnel URLs.

### Three-Step Flow: Token → Nonce → Password

```
┌──────────────────────────────────────────────────────────────────────┐
│                        Authentication Flow                           │
│                                                                      │
│  Step 1: TOKEN                Step 2: NONCE             Step 3: PWD  │
│  ┌─────────────┐              ┌─────────────┐          ┌──────────┐  │
│  │ Notification│  consume &   │  Password   │  verify  │ Session  │  │
│  │ URL with    │─────────────▶│  form with  │─────────▶│ cookie   │  │
│  │ ?token=...  │  redirect    │  ?nonce=... │  bcrypt  │ issued   │  │
│  └─────────────┘              └─────────────┘          └──────────┘  │
│                                                                      │
│  One-time use                  5-min TTL                 12h TTL     │
│  32 bytes, hex                 16 bytes, hex             HMAC-signed │
│  crypto/rand                   crypto/rand               per-tunnel  │
│  Proves: received              Prevents: token           secret      │
│  the notification              in browser history                    │
└──────────────────────────────────────────────────────────────────────┘
```

**Why three steps instead of two:** The token in the notification URL is a secret. If the user's browser history syncs across devices, or if the URL appears in server logs, a direct token→password flow would leave the token exposed. The immediate 302 redirect to a nonce removes the token from the URL bar before the user interacts with the page. Nonces are server-side only (never in notifications) and expire in 5 minutes.

**Neither factor alone grants access:** Intercepting the notification URL without the password gives nothing. Knowing the password without a valid nonce gives nothing.

### Session Cookie

After successful authentication:

- **Cookie name:** `schmux_remote`
- **Value:** `<unix_timestamp>.<HMAC-SHA256(timestamp, session_secret)>`
- **Attributes:** `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`, `MaxAge=43200` (12h)
- **Server-side validation:** Verifies HMAC signature with constant-time comparison, checks timestamp is within 12 hours, requires non-empty session secret

The 32-byte session secret is regenerated on each tunnel start. This cryptographically invalidates all cookies from previous tunnel sessions — there is no way for an old cookie to pass HMAC verification against a new secret.

### Auth Middleware

```
                         ┌──────────────────┐
              Request ──▶│  requiresAuth()  │
                         └────────┬─────────┘
                                  │
                    ┌─────────────▼──────────────┐
                    │   Auth required?           │
                    │   (OAuth enabled OR        │
                    │    tunnel active)          │
                    └─────────────┬──────────────┘
                           no     │   yes
                    ┌─────────────┘    │
                    ▼                  ▼
               ┌────────┐    ┌────────────────────┐
               │ ALLOW  │    │ Local request +    │
               └────────┘    │ tunnel-only auth?  │
                             └────────┬───────────┘
                               yes    │    no
                              ┌───────┘    │
                              ▼            ▼
                         ┌────────┐  ┌────────────────┐
                         │ ALLOW  │  │ Has valid      │
                         └────────┘  │ schmux_auth OR │
                                     │ schmux_remote? │
                                     └───────┬────────┘
                                      yes    │    no
                                     ┌───────┘    │
                                     ▼            ▼
                                ┌────────┐  ┌─────────┐
                                │ ALLOW  │  │   401   │
                                └────────┘  └─────────┘
```

Resolution order:

1. **No auth required** → allow
2. **Local request + tunnel-only auth** (no GitHub OAuth, just tunnel active) → allow (local bypass)
3. **GitHub OAuth cookie** (`schmux_auth`) → validate HMAC signature + expiry → allow if valid
4. **Remote session cookie** (`schmux_remote`) → validate HMAC signature + timestamp → allow if valid
5. **Reject** → 401 Unauthorized

The `/remote-auth` endpoint is **excluded from auth middleware** — it handles its own token/nonce-based protection.

---

## Security Model

Nine layers of defense protect against unauthorized remote access:

```
┌─────────────────────────────────────────────────────────────┐
│ Layer 9: Non-Loopback Bind Rejection                        │
│   Tunnel refuses to start on 0.0.0.0                        │
├─────────────────────────────────────────────────────────────┤
│ Layer 8: Binary Verification                                │
│   macOS codesign, download size limits, decompress limits   │
├─────────────────────────────────────────────────────────────┤
│ Layer 7: Local Request Bypass                               │
│   Loopback without Cf-Connecting-IP = always allowed        │
├─────────────────────────────────────────────────────────────┤
│ Layer 6: Rate Limiting                                      │
│   5 req/min per IP + 5 failures per IP = lockout            │
├─────────────────────────────────────────────────────────────┤
│ Layer 5: CORS Origin Restriction                            │
│   Only tunnel URL + localhost when tunnel active            │
├─────────────────────────────────────────────────────────────┤
│ Layer 4: CSRF Protection                                    │
│   X-CSRF-Token header must match schmux_csrf cookie         │
├─────────────────────────────────────────────────────────────┤
│ Layer 3: Session Cookie (HMAC-SHA256)                       │
│   HttpOnly, Secure, SameSite=Lax, 24h, per-tunnel secret    │
├─────────────────────────────────────────────────────────────┤
│ Layer 2: Authentication (Token + Nonce + Password)          │
│   One-time token → 5-min nonce → bcrypt password            │
├─────────────────────────────────────────────────────────────┤
│ Layer 1: Transport (Cloudflare TLS)                         │
│   HTTPS to edge, encrypted tunnel to localhost              │
└─────────────────────────────────────────────────────────────┘
```

### Layer 1: Transport — Cloudflare TLS

All traffic between the remote device and Cloudflare's edge is HTTPS. Traffic between Cloudflare and the local machine travels through the `cloudflared` tunnel process over a local connection (never leaves the machine).

### Layer 2: Authentication — Token + Password

| Factor   | Source                         | Lifetime            | Purpose                                                      |
| -------- | ------------------------------ | ------------------- | ------------------------------------------------------------ |
| Token    | `crypto/rand`, 32 bytes, hex   | One-time use        | Proves the user received the notification                    |
| Nonce    | `crypto/rand`, 16 bytes, hex   | 5 minutes           | Scopes the password form, keeps token out of browser history |
| Password | User-configured, bcrypt-hashed | Persisted in config | Proves the user knows the password                           |

### Layer 3: Session — HMAC-Signed Cookie

- `schmux_remote` cookie: `<timestamp>.<HMAC-SHA256(timestamp, secret)>`
- `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`, `MaxAge=43200`
- Session secret regenerated per tunnel start → old cookies cryptographically invalidated

### Layer 4: CSRF Protection

State-changing endpoints (`/api/remote-access/on`, `/off`, `/set-password`, `/test-notification`) use `withAuthAndCSRF`:

- On auth success, server sets `schmux_csrf` cookie (`HttpOnly=false`, `SameSite=Strict`, `Secure=true`)
- Frontend reads cookie and sends it as `X-CSRF-Token` header on POST requests (`assets/dashboard/src/lib/csrf.ts`)
- Server validates header matches cookie using `hmac.Equal` (constant-time)
- Local requests (loopback without forwarding headers) are CSRF-exempt
- CORS `Access-Control-Allow-Headers` includes `X-CSRF-Token`

### Layer 5: CORS Origin Restriction

When a tunnel is active, `isAllowedOrigin()` restricts CORS origins to:

1. The tunnel URL itself (`https://<random>.trycloudflare.com`)
2. Localhost (`http://localhost:{port}`, `http://127.0.0.1:{port}`)

All other origins are rejected with 403 Forbidden — even if `network_access` is enabled. This prevents cross-origin requests from malicious sites that might try to use the authenticated session cookie.

### Layer 6: Rate Limiting

**IP-based rate limiter on `/remote-auth` POST:**

- 5 requests per minute per IP address
- Uses `Cf-Connecting-IP` header (when tunnel is active and request comes from loopback) to rate-limit by actual remote IP, not cloudflared's loopback address
- Returns 429 with `Retry-After: 60` header

**Per-IP lockout (per tunnel session):**

- After 5 failed password attempts per IP (`maxPasswordAttempts`), all nonces are deleted and the session is locked for that IP
- Lockout counter resets only when the tunnel restarts
- Per-IP tracking uses `map[string]int` with a cap of 1000 tracked IPs (`maxFailureIPs`)

### Layer 7: Trusted Request Bypass

Trusted requests bypass tunnel-only auth. When remote access is not enabled, all requests are trusted (the LAN is the trust boundary). When remote access is enabled, only genuine loopback requests are trusted.

**Detection logic (`isTrustedRequest`):**

```
                  ┌────────────────────────┐
    Request ─────▶│ remote_access enabled? │
                  └───────────┬────────────┘
                        no    │    yes
                  ┌───────────┘    │
                  ▼                ▼
              TRUSTED    ┌────────────────────────┐
                         │ RemoteAddr is loopback? │
                         └───────────┬─────────────┘
                               no    │    yes
                         ┌───────────┘    │
                         ▼                ▼
                     NOT TRUSTED  ┌────────────────────┐
                                  │ Tunnel active AND  │
                                  │ Cf-Connecting-IP   │
                                  │ or X-Forwarded-For │
                                  │ header present?    │
                                  └────────┬───────────┘
                                    yes    │    no
                                  ┌────────┘    │
                                  ▼             ▼
                             NOT TRUSTED     TRUSTED
                             (tunneled)    (genuine)
```

Applied consistently to: HTTP API handlers, dashboard WebSocket, terminal WebSocket, provision WebSocket, and CSRF validation.

### Layer 8: Binary Verification

When `cloudflared` is auto-downloaded:

- **macOS:** Verified via `codesign -v --deep` — checks signing by Cloudflare Inc. (Team ID: `68WVV388M8`). Unsigned or tampered binaries are rejected.
- **Linux:** No signature verification available (logged as warning). `AllowAutoDownload` defaults to `false`.
- **Download size limit:** HTTP response body limited to 200MB via `io.LimitReader`
- **Decompression bomb protection:** tar entries rejected if `header.Size` exceeds 200MB, plus `io.LimitReader` on the tar reader enforces the limit at runtime

### Layer 9: Non-Loopback Bind Rejection

`Manager.Start()` rejects tunnel start when the server is bound to a non-loopback address (e.g., `0.0.0.0`). This prevents exposing an unauthenticated listener on the LAN that would bypass the cloudflared proxy chain.

---

## Server State

```go
// On the Server struct (internal/dashboard/server.go):
remoteToken         string                  // current one-time token (empty = consumed or no tunnel)
remoteTokenFailures map[string]int          // failed password attempts per IP for current tunnel session
remoteTokenMu       sync.Mutex              // protects all remote* fields
remoteSessionSecret []byte                  // 32-byte HMAC key for signing cookies, regenerated per tunnel
remoteTunnelURL     string                  // current tunnel URL (for CORS validation)
remoteNonces        map[string]*remoteNonce // active nonces (token → nonce exchange results)
```

### State Lifecycle

| Event                                        | Token               | Failures    | Secret            | Nonces          |
| -------------------------------------------- | ------------------- | ----------- | ----------------- | --------------- |
| Tunnel connects                              | Generated (32B hex) | Reset to 0  | Regenerated (32B) | Cleared         |
| Token consumed (GET with valid token)        | Cleared to ""       | Unchanged   | Unchanged         | New nonce added |
| Successful auth (POST with correct password) | Unchanged           | Unchanged   | Unchanged         | Nonce deleted   |
| Failed auth attempt                          | Unchanged           | Incremented | Unchanged         | Unchanged       |
| 5th failure (lockout)                        | Unchanged           | 5           | Unchanged         | All deleted     |
| Tunnel stops (`ClearRemoteAuth`)             | Cleared             | Reset to 0  | Cleared to nil    | Cleared         |
| Password changed while tunnel active         | Unchanged           | Unchanged   | Regenerated       | Unchanged       |

Password change regenerates the session secret, which immediately invalidates all existing remote session cookies. Users must re-authenticate through the full token → nonce → password flow.

---

## API

### Auth Endpoints (Unauthenticated)

**`GET /remote-auth`** — Three behaviors based on query params:

| Query            | Behavior                                                                                    |
| ---------------- | ------------------------------------------------------------------------------------------- |
| `?token=<token>` | Validates and consumes token (one-time), generates nonce, 302 redirects to `?nonce=<nonce>` |
| `?nonce=<nonce>` | Validates nonce (exists, < 5 min old, not locked out), renders password form                |
| (no params)      | Renders instructions page ("check your notification app")                                   |

**`POST /remote-auth`** — Rate-limited password verification:

- Body: `application/x-www-form-urlencoded` with `nonce` and `password` fields
- Body size: capped by `http.MaxBytesReader`
- Rate limit: 5 req/min per IP (429 if exceeded)
- On success: nonce deleted, `schmux_remote` + `schmux_csrf` cookies set, 302 to `/`
- On failure: counter incremented, form re-rendered with error and remaining attempts
- On lockout (5 failures): nonces deleted, locked-out message
- Concurrency: lock released during bcrypt, re-acquired with double-check

### Management Endpoints (Authenticated + CSRF)

| Endpoint                               | Method | Auth              | Description                                       |
| -------------------------------------- | ------ | ----------------- | ------------------------------------------------- |
| `/api/remote-access/status`            | GET    | `withAuth`        | Current tunnel state, URL, config                 |
| `/api/remote-access/on`                | POST   | `withAuthAndCSRF` | Start tunnel (requires password configured)       |
| `/api/remote-access/off`               | POST   | `withAuthAndCSRF` | Stop tunnel, clear all auth state                 |
| `/api/remote-access/set-password`      | POST   | `withAuthAndCSRF` | Set password (bcrypt hash to config, min 8 chars) |
| `/api/remote-access/test-notification` | POST   | `withAuthAndCSRF` | Send test notification to configured channels     |

### WebSocket Events

On `/ws/dashboard`:

```json
{"type": "tunnel_status", "data": {"state": "connected", "url": "https://..."}}
{"type": "tunnel_status", "data": {"state": "off"}}
{"type": "tunnel_status", "data": {"state": "error", "error": "cloudflared exited unexpectedly"}}
```

---

## Password Entry Page

Self-contained HTML served by the Go backend (`renderPasswordPage`). No React dependency — the page must work before the user is authenticated to load the SPA.

- Inline CSS only (no external stylesheets or JavaScript)
- Dark/light mode via `prefers-color-scheme` media query
- Standard `<form method="POST">` to `/remote-auth` with hidden nonce field
- XSS prevention: nonce and error message values escaped via `html.EscapeString()`
- Shows remaining attempts count when failures > 0
- Locked-out state renders error message without form

---

## Notification

When the tunnel connects, `HandleTunnelConnected` generates the auth token, builds the auth URL, and dispatches notifications:

```
                     ┌───────────────────────┐
                     │ HandleTunnelConnected │
                     │   tunnelURL           │
                     └──────────┬────────────┘
                                │
                    ┌───────────▼───────────┐
                    │ Generate token (32B)  │
                    │ Generate secret (32B) │
                    │ Build auth URL        │
                    └───────────┬───────────┘
                                │
              ┌─────────────────┼─────────────────┐
              ▼                                   ▼
    ┌─────────────────┐                 ┌───────────────────┐
    │ ntfy.sh POST    │                 │ Custom command    │
    │ Full auth URL   │                 │ sh -c <command>   │
    │ (with token)    │                 │ $SCHMUX_REMOTE_URL│
    └─────────────────┘                 │ = base URL only   │
                                        │ (NO token)        │
                                        └───────────────────┘
```

**Security note:** Custom commands receive only the base tunnel URL (without token) via the `$SCHMUX_REMOTE_URL` environment variable. The auth URL containing the one-time token is sent only via ntfy.sh. This prevents token leakage to arbitrary command environments, stdout, or stderr.

Both notification methods can be configured simultaneously.

---

## `cloudflared` Dependency Management

`cloudflared` is a standalone binary (~30MB). schmux manages it transparently:

```
FindCloudflared(schmuxBinDir)
  1. exec.LookPath("cloudflared") → found? use it
  2. Check ~/.schmux/bin/cloudflared → exists? use it
  3. Error: not found

EnsureCloudflared(schmuxBinDir)
  1-2. Same as FindCloudflared
  3. Download from github.com/cloudflare/cloudflared/releases/latest
     → macOS: .tgz archive → extractTgz() → codesign verify
     → Linux: raw binary → direct write
  4. Cache at ~/.schmux/bin/cloudflared
```

Download URLs by platform:

- `darwin/arm64`: `cloudflared-darwin-arm64.tgz`
- `darwin/amd64`: `cloudflared-darwin-amd64.tgz`
- `linux/amd64`: `cloudflared-linux-amd64` (raw binary)
- `linux/arm64`: `cloudflared-linux-arm64` (raw binary)

`AllowAutoDownload` defaults to `false` — users must explicitly opt in before schmux will download binaries.

---

## Configuration

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

| Field                 | Default | Description                                                       |
| --------------------- | ------- | ----------------------------------------------------------------- |
| `enabled`             | `true`  | Kill switch. When false, tunnel start is rejected with 403.       |
| `timeout_minutes`     | `0`     | Auto-kill tunnel after N minutes. 0 = no timeout.                 |
| `password_hash`       | `""`    | bcrypt hash of the user's password. Plaintext never touches disk. |
| `allow_auto_download` | `false` | Whether to auto-download cloudflared if not found.                |
| `notify.ntfy_topic`   | `""`    | ntfy.sh topic for push notifications.                             |
| `notify.command`      | `""`    | Shell command run with `$SCHMUX_REMOTE_URL` env var.              |

The API (`GET /api/config`) exposes `password_hash_set: bool` — never the hash itself.

---

## CLI

```
schmux remote on             Start tunnel, send notification
schmux remote off            Stop tunnel, clear auth state
schmux remote status         Show tunnel state, URL
schmux remote set-password   Set password (interactive prompt, bcrypt hash to config)
```

---

## Mobile UI

The existing React dashboard adapts to mobile viewports via responsive breakpoints. No separate app, no separate API.

**Layout changes at ~768px and below:**

- Sidebar collapses to bottom navigation bar
- Session cards stack vertically, full-width, larger touch targets
- Terminal view goes full-screen with back button
- On-screen keyboard works for terminal input

**Not built:** No native app. No PWA/offline. No separate mobile API.

---

## Test Coverage

| Test File                        | Scope                                                                                                                                                                                                              |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `handlers_remote_auth_test.go`   | Cookie validation, nonce lifecycle, token consumption, rate limiting, XSS escaping, password validation, session invalidation, local bypass                                                                        |
| `tunnel_e2e_test.go`             | Full auth flow, CSRF attacks, CORS validation, nonce reuse, cookie replay across tunnel sessions, brute force lockout, rate limiting by IP, WebSocket auth, local access during tunnel, password change revocation |
| `handlers_remote_access_test.go` | On/off/status endpoints, test server setup                                                                                                                                                                         |
| `cloudflared_test.go`            | Binary verification, download URLs, decompression bomb protection, normal extraction                                                                                                                               |
| `manager_test.go`                | Tunnel state machine, non-loopback bind rejection, auto-download disabled rejection                                                                                                                                |
| `notify_test.go`                 | ntfy.sh notification, notify config                                                                                                                                                                                |
| `csrf.test.ts`                   | Frontend CSRF cookie reading, edge cases (missing, empty, base64, multiple cookies)                                                                                                                                |
| `api-csrf.test.ts`               | All 4 remote access API functions send `X-CSRF-Token` header                                                                                                                                                       |
