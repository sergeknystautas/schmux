# Remote Access Authentication

## Problem

Remote access uses Cloudflare quick tunnels which generate a new random `*.trycloudflare.com` URL each session. GitHub OAuth requires a fixed callback URL registered in advance, making it incompatible with ephemeral tunnel URLs.

## Solution: Token + PIN Authentication

Two-factor authentication using a one-time token (delivered via notification) and a user-configured PIN/passphrase.

### Auth Flow

1. User starts tunnel from dashboard or CLI
2. Cloudflared connects → server generates a one-time token
3. Notification (ntfy/command) delivers: `https://<random>.trycloudflare.com/remote-auth?token=<token>`
4. User opens link on phone → sees PIN entry form
5. User enters their PIN → server validates token + PIN
6. On success: server sets `schmux_remote` session cookie, redirects to dashboard
7. On failure: error message, 5 max attempts per token before invalidation

The token alone is not sufficient — intercepting the notification URL without the PIN grants no access.

---

## Preconditions

The tunnel manager currently requires `AuthEnabled` and `AllowedUsersSet`. These are replaced by a single precondition: **PIN must be configured**.

**ManagerConfig changes:**

```go
type ManagerConfig struct {
    Disabled       bool
    PinHashSet     bool   // replaces AuthEnabled + AllowedUsersSet
    Port           int
    SchmuxBinDir   string
    TimeoutMinutes int
    OnStatusChange func(TunnelStatus)
}
```

**Start() precondition:**

```go
if !m.config.PinHashSet {
    return fmt.Errorf("remote access requires a PIN (run: schmux remote set-pin)")
}
```

**PIN storage:** The PIN is stored as a bcrypt hash in config (`remote_access.pin_hash`). Plaintext never touches disk.

**Setting the PIN:**

```bash
schmux remote set-pin
# Prompts for PIN interactively, writes bcrypt hash to config
```

Or from the dashboard settings panel (POST to `/api/remote-access/set-pin`).

---

## Server-Side Auth Components

### New state on Server struct

```go
remoteToken         string        // current one-time token (empty = no active auth session)
remoteTokenFailures int           // failed PIN attempts for current token
remoteTokenMu       sync.Mutex    // protects token + failures
remoteSessionSecret []byte        // HMAC key for signing remote cookies, regenerated per tunnel
```

### New endpoints

**`GET /remote-auth?token=<token>`** — Unauthenticated

- Validates token matches current `remoteToken`
- Returns self-contained HTML PIN entry form
- If token invalid or expired: returns error page

**`POST /remote-auth`** — Unauthenticated

- Form fields: `token`, `pin`
- Validates token, then bcrypt-compares PIN against stored hash
- On success: sets `schmux_remote` cookie (HMAC-signed timestamp), redirects to `/`
- On failure: increments `remoteTokenFailures`, re-renders form with error
- At 5 failures: invalidates token (`remoteToken = ""`), shows "locked out" message

**`POST /api/remote-access/set-pin`** — Authenticated (normal auth required)

- Body: `{"pin": "my-secret-phrase"}`
- Bcrypt-hashes the PIN, writes to config file
- Returns `{"ok": true}`

### Token lifecycle

- Generated when tunnel connects (in `HandleTunnelConnected`)
- Invalidated on: successful auth, 5 failed attempts, tunnel stop
- Only one token active at a time (new tunnel = new token)
- 32 bytes, crypto/rand, hex-encoded

---

## Auth Middleware Integration

The existing auth middleware resolves credentials in order:

1. **Auth disabled** → allow (existing behavior)
2. **GitHub OAuth session** → allow (existing behavior)
3. **Remote session cookie** → validate HMAC signature + timestamp, allow if valid
4. **Reject** → 401

The `/remote-auth` endpoint is **excluded from auth middleware** (it handles its own token-based protection).

### Remote cookie details

- Name: `schmux_remote`
- Value: HMAC-SHA256 signed timestamp
- `remoteSessionSecret` is regenerated each time the tunnel starts, which invalidates all previous remote cookies when the tunnel stops and restarts
- Cookie attributes: `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/`

---

## PIN Entry Page

Self-contained HTML served by the Go backend. No React dependency — the page must work before the user is authenticated to load the SPA.

**Properties:**

- Inline CSS (no external stylesheets)
- Dark/light mode via `prefers-color-scheme` media query
- Standard `<form>` POST to `/remote-auth`
- Error message displayed inline when PIN is wrong
- Shows remaining attempts ("2 attempts remaining")
- Locked-out state when token is invalidated
- Embedded as a Go `const` or template string in the handler file

---

## Notification URL Change

Currently, the daemon wires an `OnStatusChange` callback that sends notifications directly. This changes so the server owns the notification flow, since it generates the token.

**New method on Server:**

```go
func (s *Server) HandleTunnelConnected(tunnelURL string) {
    // 1. Generate one-time token (32 bytes, crypto/rand, hex)
    // 2. Reset failure counter
    // 3. Regenerate remoteSessionSecret
    // 4. Build auth URL: tunnelURL + "/remote-auth?token=" + token
    // 5. Send notification with auth URL (via notify dispatcher)
}
```

**OnStatusChange callback update:**

```go
OnStatusChange: func(status tunnel.TunnelStatus) {
    server.BroadcastTunnelStatus(status)
    if status.State == tunnel.StateConnected && status.URL != "" {
        server.HandleTunnelConnected(status.URL)
    }
}
```

The notification message contains the full auth URL (not just the tunnel URL).

---

## CLI and Config Changes

### CLI

New command:

```
schmux remote set-pin    Set PIN for remote access authentication
```

Prompts interactively for PIN (with confirmation), bcrypt-hashes it, writes to config via daemon API (`POST /api/remote-access/set-pin`).

### Config

**`~/.schmux/config.json` additions:**

```json
{
  "remote_access": {
    "pin_hash": "$2a$10$..."
  }
}
```

**Config Go struct:**

```go
// In internal/config/config.go
type RemoteAccessConfig struct {
    Disabled       bool   `json:"disabled,omitempty"`
    TimeoutMinutes int    `json:"timeout_minutes,omitempty"`
    PinHash        string `json:"pin_hash,omitempty"`
    Notify         RemoteAccessNotifyConfig `json:"notify,omitempty"`
}

func (c *Config) GetRemoteAccessPinHash() string { ... }
```

### API Contract

**Response (GET /api/config):**

```json
{
  "remote_access": {
    "disabled": false,
    "timeout_minutes": 60,
    "pin_hash_set": true,
    "notify": { ... }
  }
}
```

The API exposes `pin_hash_set` (boolean) — never the hash itself.

**Contract struct:**

```go
type RemoteAccess struct {
    Disabled       bool               `json:"disabled"`
    TimeoutMinutes int                `json:"timeout_minutes"`
    PinHashSet     bool               `json:"pin_hash_set"`
    Notify         RemoteAccessNotify `json:"notify"`
}
```

### Dashboard

The Remote Access panel shows:

- PIN status: "Configured" or "Not set — run `schmux remote set-pin`"
- If PIN not set, the Start button is disabled with explanation
