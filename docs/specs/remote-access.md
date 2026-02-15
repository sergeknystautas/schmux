# Remote Access Design

## Overview

Remote Access lets you access your schmux dashboard from your phone (or any device) over the internet, even when your laptop is behind residential NAT. It uses Cloudflare's free quick tunnels (`trycloudflare.com`) to create an ephemeral HTTPS URL — no account, no config, no port forwarding.

## User Flow

1. User runs `schmux remote on` (or clicks toggle on dashboard)
2. schmux checks for `cloudflared` on PATH, then `~/.schmux/bin/cloudflared`, downloads if neither found
3. schmux spawns `cloudflared tunnel --url localhost:{port}` as a managed subprocess
4. `cloudflared` prints the ephemeral `*.trycloudflare.com` URL to stderr — schmux parses it
5. Dashboard shows the URL as a QR code on a "Remote Access" panel
6. schmux POSTs the URL to the user's configured ntfy.sh topic (and/or runs their custom command)
7. Auth is mandatory — GitHub OAuth with allowlist enforcement
8. User opens URL on phone, authenticates with GitHub, gets the full dashboard
9. User runs `schmux remote off` (or toggles the button) — schmux kills `cloudflared`, tunnel closes

## Architecture

### New Components

**`internal/remote/tunnel.go`** — Tunnel manager:

- Check for `cloudflared` on PATH first, fall back to `~/.schmux/bin/cloudflared`, download if neither exists
- Download correct platform binary on first use (darwin/arm64, darwin/amd64, linux/amd64, etc.)
- Spawn and supervise the `cloudflared` process
- Parse the ephemeral URL from `cloudflared`'s stderr output
- Expose tunnel state (off / starting / connected / error) and current URL
- Kill `cloudflared` on shutdown, toggle-off, or timeout expiry

**`internal/remote/notify.go`** — Notification dispatcher:

- POST tunnel URL to ntfy.sh (`POST https://ntfy.sh/{topic}`)
- Optionally run a user-defined shell command with URL as `$SCHMUX_REMOTE_URL` env var
- Notify on tunnel up and (optionally) on tunnel down

### Dashboard Changes (React)

- "Remote Access" panel — toggle switch, status indicator (off/starting/connected/error), QR code display
- QR code panel only visible on desktop-sized viewports (not shown on the phone that's already connected)
- Persistent banner when remote access is active showing connected device count
- Mobile-responsive layout (see Mobile UI section)

### API Surface

**CLI commands:**

```
schmux remote on       # Start tunnel, display URL, send notification
schmux remote off      # Stop tunnel
schmux remote status   # Show tunnel state, URL, connected devices
```

**HTTP endpoints:**

```
POST /api/remote/on       # Start tunnel (requires auth + non-empty allowlist)
POST /api/remote/off      # Stop tunnel
GET  /api/remote/status   # { state, url, connected_devices }
```

**WebSocket events** (on existing `/ws/dashboard`):

```json
{"type": "remote_status", "data": {"state": "connected", "url": "https://..."}}
{"type": "remote_status", "data": {"state": "off"}}
```

### Config

```json
{
  "remote_access": {
    "disabled": false,
    "timeout_minutes": 0,
    "notify": {
      "ntfy_topic": "my-secret-schmux-topic",
      "command": ""
    }
  }
}
```

- `disabled` — Kill switch. Hides the feature entirely: no toggle on dashboard, CLI commands return error, API returns 403. Default: `false`.
- `timeout_minutes` — Auto-kill tunnel after N minutes. 0 means no timeout. Default: `0`.
- `notify.ntfy_topic` — ntfy.sh topic name for push notifications. Empty means no ntfy notification. Default: `""`.
- `notify.command` — Custom shell command to run when tunnel URL is available. Receives URL via `$SCHMUX_REMOTE_URL` env var. Default: `""`.

## Security Model

1. **Tunnel layer** — Cloudflare provides TLS termination. Traffic between phone and Cloudflare edge is HTTPS. Traffic between Cloudflare and localhost is over the local tunnel (never leaves the machine).

2. **Auth layer** — GitHub OAuth is **mandatory** when remote access is active. `schmux remote on` refuses to start if auth is not configured. No way to expose an unauthenticated dashboard to the internet.

3. **Authorization layer** — `access_control.allowed_users` allowlist must be non-empty. Fail closed: if the allowlist is empty, remote access refuses to start.

4. **Visibility** — Persistent banner on dashboard when remote access is active. Shows connected device count with ability to view connected sessions.

5. **Auto-expiry** — Optional `timeout_minutes` config. For users who don't want to risk leaving the tunnel open accidentally.

6. **Full access** — Authenticated and authorized users get full dashboard access, same as desktop. No artificial feature restrictions — the agents already have full machine access, so restricting terminal input over remote would be security theater.

## Mobile UI

The existing React dashboard adapts to mobile viewports via responsive breakpoints. No separate app, no separate API.

**Layout changes at ~768px and below:**

- Sidebar collapses to bottom navigation bar (sessions, spawn, settings)
- Session cards stack vertically, full-width, larger touch targets
- Terminal view goes full-screen when tapped into a session, with back button to return to list
- On-screen keyboard works for terminal input
- Session status badges are larger and color-coded for at-a-glance scanning

**What we don't build:**

- No native app — browser is sufficient
- No offline / PWA — live connection is required anyway
- No separate mobile API — same endpoints, same WebSocket, same auth

## `cloudflared` Dependency Management

`cloudflared` is a standalone binary (~30MB). schmux manages it transparently:

1. Check if `cloudflared` exists on PATH — if so, use it
2. Check if `~/.schmux/bin/cloudflared` exists — if so, use it
3. Otherwise, download the correct platform binary from Cloudflare's GitHub releases to `~/.schmux/bin/cloudflared`
4. First toggle takes a few seconds for download, then it's cached

This avoids requiring users to install `cloudflared` system-wide while respecting their existing installation if present.

## Notification Flow

When the tunnel comes up:

1. If `notify.ntfy_topic` is set: `POST https://ntfy.sh/{topic}` with body containing the URL and a title like "schmux remote access"
2. If `notify.command` is set: run the command with `SCHMUX_REMOTE_URL` env var
3. Both can be configured simultaneously

This lets users get the URL pushed to their phone even if they're already away from the laptop (e.g., daemon restarted remotely via SSH).

### ntfy Topic Discoverability

The configured ntfy topic is surfaced in three ways so the user can easily subscribe on their phone:

1. **Dashboard** — The Remote Access panel displays the configured ntfy topic name with a clickable "Subscribe" link (`https://ntfy.sh/{topic}`) and a QR code for the ntfy subscription URL. Scan once on your phone to subscribe permanently.

2. **CLI** — `schmux remote status` includes the configured topic name in its output, so it's always one command away.

3. **Test notification on first config** — When the user first sets `ntfy_topic` in their config (or changes it), schmux sends a test notification ("schmux notification test — you're all set!") so they can verify the subscription works before they actually need it.
