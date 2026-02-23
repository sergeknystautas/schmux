# HTTPS Server via dashboard.sx

## Goal

Enable trusted HTTPS access to schmux over a private network without browser warnings, CA installation, or internet-routed tunnels. This unlocks clipboard API access (copy/paste images) from any device on the LAN.

## Background

The browser Clipboard API (`navigator.clipboard.read()` and `navigator.clipboard.write()`) requires a "secure context":

- HTTPS connection, OR
- localhost / 127.0.0.1

Users accessing schmux via `http://192.168.1.x:7337` cannot paste content. Existing solutions have tradeoffs:

| Solution             | Works?  | Tradeoff                        |
| -------------------- | ------- | ------------------------------- |
| Self-signed cert     | Sort of | Browser warning on every device |
| mkcert               | Yes     | Must install CA on every device |
| Cloudflare tunnel    | Yes     | Routes over internet (latency)  |
| Let's Encrypt for IP | No      | LE doesn't issue certs for IPs  |

## Solution: dashboardsx managed service

**dashboardsx is a managed service that maps private schmux deployments (192.168.x.x, 10.x.x.x, etc.) to publicly verifiable hostnames of the form 'abc12.dashboard.sx'. This allows Let's Encrypt to provide free, automated HTTPS certificates.**

This is similar to dynamic DNS services like DuckDNS or No-IP, but purpose-built for schmux developers with automated Let's Encrypt certificates. This service requires nothing more than authentication with your github account to get this hostname mapped to your local schmux address.

Sint Maarten (the Dutch side of the Caribbean island shared with French Saint-Martin) provides the domain name suffix '.sx'. It was quite cheap to register dashboard.sx.

### The Problem

You're running schmux on your computer at `192.168.1.100:7337`. But when you access `http://192.168.1.100:7337`, the browser blocks clipboard access because it's not a "secure context". You can't copy/paste.

### The Solution

The dashboardsx managed service gives you a public hostname that points to your private IP:

```
47293.dashboard.sx  →  192.168.1.100
```

Now you access `https://47293.dashboard.sx:7337`:

- Browser sees valid HTTPS cert from Let's Encrypt
- DNS resolves to your LAN IP (192.168.1.100)
- Connection stays on your local network (~2ms latency)
- Clipboard works because it's a secure context

### What You Get

| Before                      | After                             |
| --------------------------- | --------------------------------- |
| `http://192.168.1.100:7337` | `https://47293.dashboard.sx:7337` |
| Browser: "Not secure"       | Browser: Secure padlock           |
| Clipboard: Broken           | Clipboard: Works                  |
| Paste images: No            | Paste images: Yes                 |

### What dashboard.sx Manages

1. **DNS Records** - `xxxxx.dashboard.sx` → your LAN IP address
2. **DNS Challenge** - Creates TXT records for Let's Encrypt verification
3. **Code Assignment** - Unique 5-digit codes for each schmux instance
4. **Cleanup** - Inactive registrations are automatically reclaimed

**Note:** SSL certificates are managed locally by schmux, not by dashboard.sx. This keeps private keys on your machine only.

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              User's Network                                 │
│                                                                             │
│   ┌─────────────────────────────┐                                           │
│   │ schmux daemon               │                                           │
│   │ 192.168.1.100:7337 (HTTPS)  │                                           │
│   │                             │                                           │
│   │ instance_key: abc123...     │                                           │
│   │ dashboardsx_code: 47293     │                                           │
│   │                             │                                           │
│   │ [lego library]              │  ← Let's Encrypt client runs here         │
│   │ cert.pem, key.pem           │  ← Private key never leaves machine       │
│   └────────────┬────────────────┘                                           │
│                │                                                            │
│                │ Heartbeat (start + daily)                                  │
│                │ DNS Challenge requests (during cert provisioning)          │
│                ▼                                                            │
└────────────────┼────────────────────────────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                    dashboard.sx Service (fly.io)                            │
│                                                                             │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                        Web UI + API                                 │   │
│   │                                                                     │   │
│   │  /register        →  Start setup flow (stores return_url)           │   │
│   │  /auth/github     →  GitHub OAuth                                   │   │
│   │  /heartbeat       →  Keep registration alive                        │   │
│   │  /dns-challenge   →  Create/delete TXT records for LE               │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
│   ┌─────────────────┐  ┌─────────────────┐                                  │
│   │ Route 53        │  │ PostgreSQL      │                                  │
│   │ (DNS updates)   │  │ (storage)       │                                  │
│   └─────────────────┘  └─────────────────┘                                  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Setup Flow

The registration process follows an OAuth-like flow to link your schmux instance to your GitHub identity:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         User's schmux deployment                            │
│                                                                             │
│   ┌─────────────────────────────----┐                                       │
│   │ schmux daemon                   │                                       │
│   │ http://192.168.1.100:7337       │                                       │
│   │                                 │                                       │
│   │ instance_key: ce89c7we9c78we7c  │                                       │
│   └─────────────────────────────----┘                                       │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
          │
          │ 1. User opens registration URL
          │    https://dashboard.sx/register
          │    ?instance_key=ce89c7we9c78we7c
          │    &ip=192.168.1.100
          │    &return_url=http://192.168.1.100:7337/api/dashboardsx/callback
          │
          ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         dashboard.sx (fly.io)                               │
│                                                                             │
│   Validates return_url:                                                     │
│   - Must match provided IP (192.168.1.100)                                 │
│   - Must be private IP range or localhost                                   │
│                                                                             │
│   Creates server-side session:                                              │
│   - session_id (cookie)                                                     │
│   - instance_key, return_url, ip (stored server-side)                       │
│   - csrf_token (random, for callback verification)                          │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
          │
          │ 2. Redirect to GitHub OAuth
          │
          ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                              GitHub                                         │
│                                                                             │
│   User authenticates and authorizes dashboard.sx                            │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
          │
          │ 3. OAuth callback with authorization code
          │
          ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         dashboard.sx (fly.io)                               │
│                                                                             │
│   - Exchanges code for user info (including email)                          │
│   - Assigns random 5-digit code (e.g., 47293)                               │
│   - Creates Route 53 A record: 47293.dashboard.sx → 192.168.1.100           │
│   - Stores registration in database                                         │
│   - Generates one-time callback_token (expires in 5 minutes)               │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
          │
          │ 4. Redirect to stored return_url (from session, not URL param)
          │    http://192.168.1.100:7337/api/dashboardsx/callback
          │        ?callback_token=<one-time-token>
          │
          │    Note: instance_key NOT included in URL (prevents LAN sniffing)
          │
          ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         User's schmux deployment                            │
│                                                                             │
│   schmux daemon receives callback:                                          │
│   - Extracts callback_token from URL                                        │
│   - Calls dashboard.sx to exchange token:                                   │
│     POST https://dashboard.sx/callback/exchange                             │
│     { callback_token: "..." }                                               │
│                                                                             │
│   Response: { instance_key: "...", code: "47293", email: "user@..." }      │
│                                                                             │
│   - Validates returned instance_key matches local stored value              │
│   - Stores code/email in config                                             │
│   - Returns HTML to browser: "Setup received. Return to your terminal."    │
│   - Provisions Let's Encrypt cert in background goroutine (see below)      │
│                                                                             │
│   Meanwhile, the CLI (separate process) is polling:                         │
│     GET /api/dashboardsx/provision-status                                   │
│   CLI displays progress and prints restart instructions when complete.     │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Security properties of the callback:**

- instance_key is never transmitted over the LAN (only callback_token)
- callback_token is one-time use and expires in 5 minutes
- schmux validates the returned instance_key matches its stored value
- The exchange call is over HTTPS to dashboard.sx

### Certificate Provisioning Flow (runs in schmux)

After receiving the code, schmux provisions the certificate locally:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         User's schmux deployment                            │
│                                                                             │
│   1. schmux generates ECDSA P-256 private key locally (never leaves machine)│
│   2. schmux creates CSR (Certificate Signing Request)                       │
│   3. schmux requests challenge token from dashboard.sx:                     │
│      POST https://dashboard.sx/cert-provisioning/start                      │
│      { instance_key: "...", code: "47293" }                                 │
│                                                                             │
│      Response: { challenge_token: "abc123...",                            │
│                 domain: "47293.dashboard.sx", expires_in: 300 }          │
│                                                                             │
│   4. schmux calls Let's Encrypt: "I want a cert for 47293.dashboard.sx"     │
│                                                                             │
│   5. Let's Encrypt: "Create TXT record at                                   │
│      _acme-challenge.47293.dashboard.sx with value 'xyz123'"                │
│                                                                             │
│   6. schmux calls dashboard.sx:                                             │
│      POST https://dashboard.sx/dns-challenge                                │
│      { instance_key: "...", challenge_token: "abc123...", value: "xyz123" }│
│                                                                             │
│      dashboard.sx validates:                                                │
│      - challenge_token is valid and not expired                             │
│      - request source IP matches registered ip_address                      │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
          │
          │ 7. dashboard.sx validates challenge_token, creates TXT record
          │
          ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         User's schmux deployment                            │
│                                                                             │
│   8. schmux verifies TXT record exists by querying the authoritative       │
│      nameservers for dashboard.sx directly (not recursive resolvers,      │
│      which may cache negative responses). DNS query every 5s, 5min timeout│
│                                                                             │
│   9. schmux tells Let's Encrypt: "ready, verify"                            │
│                                                                             │
│   10. Let's Encrypt verifies TXT record, issues cert                        │
│                                                                             │
│   11. schmux has cert + private key (only schmux ever had the key)          │
│                                                                             │
│   12. schmux calls dashboard.sx: DELETE /dns-challenge                      │
│       { instance_key: "...", challenge_token: "abc123..." }                 │
│       (challenge_token is now invalidated)                                  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

**Key security properties:**

- The private key is generated and stored only on the user's machine
- The challenge_token is short-lived (5 min) and single-use
- dashboard.sx validates both instance_key AND challenge_token for DNS operations
- All API calls use HTTPS
- Source IP is validated against registered IP address

### return_url Determination

The `return_url` is constructed from the hostname and port where schmux is running:

| Access Method        | return_url                                           |
| -------------------- | ---------------------------------------------------- |
| `localhost:7337`     | `http://localhost:7337/api/dashboardsx/callback`     |
| `192.168.1.100:7337` | `http://192.168.1.100:7337/api/dashboardsx/callback` |
| `schmux.local:7337`  | `http://schmux.local:7337/api/dashboardsx/callback`  |

The URL is determined by:

1. Using the `Host` header if schmux is being accessed via web
2. Using the selected IP address + port if run from CLI
3. Defaulting to `localhost:7337` if no other source available

### Setup Completion Flow (UX)

The setup flow spans three surfaces: the CLI terminal, the browser, and the daemon process. Certificate provisioning takes 10-60 seconds (DNS verification + Let's Encrypt issuance), during which the user needs clear feedback.

**Design decisions:**

1. **CLI is the primary communication channel.** The browser is only a vehicle for the OAuth redirect. Once the callback lands on the daemon, the browser shows a brief message directing the user back to the terminal.

2. **CLI polls the daemon for provisioning status.** After opening the browser, the CLI polls `GET /api/dashboardsx/provision-status` every 2 seconds. This endpoint returns the current stage of provisioning.

3. **User manually restarts the daemon.** After cert provisioning completes, the CLI prints instructions to restart. The daemon cannot hot-swap from HTTP to HTTPS — the `ListenAndServe` call is blocking and must be restarted.

**Provision status states:**

| Status          | Meaning                                        | CLI message                                                |
| --------------- | ---------------------------------------------- | ---------------------------------------------------------- |
| `""` (empty)    | No provisioning in progress                    | "Waiting for you to authenticate in the browser..."        |
| `registered`    | Callback received, token exchanged             | "Registered as {domain}. Provisioning certificate..."      |
| `starting`      | Requesting challenge token from dashboard.sx   | "Requesting challenge token from dashboard.sx..."          |
| `acme_account`  | Loading/creating ACME account key              | "Loading ACME account..."                                  |
| `acme_client`   | Creating lego ACME client                      | "Creating ACME client (Let's Encrypt production)..."       |
| `acme_register` | Registering with Let's Encrypt                 | "Registering with Let's Encrypt..."                        |
| `cert_request`  | Calling Certificate.Obtain (DNS challenge)     | "Requesting certificate for {domain} (DNS challenge)..."   |
| `dns_create`    | Creating TXT record via dashboard.sx API       | "Creating DNS TXT record for \_acme-challenge.{domain}..." |
| `dns_verify`    | TXT record created, lego verifying propagation | "TXT record created. Verifying DNS propagation..."         |
| `dns_cleanup`   | LE verified, cleaning up TXT record            | "DNS verified. Cleaning up TXT record..."                  |
| `cert_save`     | Certificate received, saving to disk           | "Certificate received. Saving..."                          |
| `complete`      | Cert saved, config updated                     | "Certificate provisioned for {domain}!"                    |
| `error`         | Something failed                               | "Error: {message}"                                         |

Additionally, the `Client.OnLog` callback injects HTTP-level detail (request URL, status code, response fields) into the message field at any status, so the CLI can display protocol-level diagnostics.

**Status endpoint:**

```
GET /api/dashboardsx/provision-status

Response: { "status": "provisioning", "domain": "47293.dashboard.sx", "message": "..." }
```

This endpoint is unauthenticated (needed before HTTPS is configured). Status is held in-memory on the daemon — it resets on daemon restart.

**Detailed flow:**

```
CLI (terminal)                    Browser                         Daemon
─────────────                     ───────                         ──────
1. schmux dashboardsx setup
   - ensure instance key
   - detect IP, build URL
   - open browser ──────────────► https://dashboard.sx/register?...
   - print "Waiting for
     authentication..."
   - poll /provision-status       (GitHub OAuth happens)
     every 2s
                                  ◄──── redirect to callback ────
                                  GET /callback?callback_token=T
                                                                  2. Exchange token (fast)
                                                                     Validate instance key
                                                                     Set status → "registered"
                                  ◄─────────────────────────────── 3. Return HTML:
                                                                     "Setup received. Return to
                                                                      your terminal for progress.
                                                                      You can close this tab."
                                                                  4. Background goroutine:
                                                                     Status → "provisioning"
                                                                     Create DNS TXT record
                                                                     Verify via authoritative NS
                                                                     Let's Encrypt issues cert
                                                                     Save cert, update config
                                                                     Status → "complete"

5. CLI sees "complete"
   - print success + domain
   - print: "Restart the daemon
     to enable HTTPS:"
   - print: schmux stop &&
     schmux start
   - print HTTPS URL
   - exit
```

**Browser HTML page**: Minimal, not the full dashboard. The user should return to the CLI — the browser's job is done. Page says: "Setup received. Return to your terminal for progress. You can close this tab." No polling, no progress bar, no JavaScript.

**Error handling**: If the background goroutine fails (DNS timeout, LE rejection, etc.), status becomes `"error"` with a descriptive message. The CLI prints the error and exits with a non-zero code.

### Ongoing: Heartbeat

```
schmux daemon:
  - On startup: POST /heartbeat { instance_key, code }
  - Every 24 hours: POST /heartbeat { instance_key, code }

dashboard.sx:
  - Updates last_heartbeat timestamp
  - If no heartbeat for 7 days → mark as inactive
  - If inactive for 30 days → reclaim code, delete DNS record
```

### Ongoing: Certificate Renewal

Certificates are valid for 90 days. schmux handles renewal automatically:

```
schmux daemon:
  - Checks cert expiry daily
  - If cert expires in < 30 days: trigger renewal flow
  - Renewal = same flow as initial provisioning (steps 1-11 above)
  - New cert and new private key generated on each renewal
```

---

## Components

### 1. dashboard.sx Service (fly.io)

**Tech stack:**

- Go web server
- PostgreSQL (user accounts, registrations)
- Route 53 (DNS management)

**Endpoints:**

| Endpoint                   | Method | Description                                           |
| -------------------------- | ------ | ----------------------------------------------------- |
| `/register`                | GET    | Show registration page, start setup flow              |
| `/auth/github`             | GET    | Redirect to GitHub OAuth                              |
| `/auth/github/callback`    | GET    | GitHub OAuth callback, assign code                    |
| `/callback/exchange`       | POST   | Exchange callback_token for registration info (HTTPS) |
| `/heartbeat`               | POST   | Keep registration alive                               |
| `/cert-provisioning/start` | POST   | Get short-lived challenge_token for cert provisioning |
| `/dns-challenge`           | POST   | Create TXT record for ACME challenge                  |
| `/dns-challenge`           | DELETE | Remove TXT record, invalidate challenge_token         |

**Database schema:**

```sql
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    github_id INTEGER UNIQUE NOT NULL,
    github_login VARCHAR(255),
    email VARCHAR(255) NOT NULL,  -- Used for Let's Encrypt account
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE registrations (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    instance_key_hash VARCHAR(64) UNIQUE NOT NULL, -- SHA-256 hash of instance_key
    instance_key TEXT,                    -- Original key, stored temporarily for callback exchange, then cleared
    code VARCHAR(5) UNIQUE NOT NULL,
    ip_address VARCHAR(45) NOT NULL,
    port INTEGER DEFAULT 7337,
    callback_token TEXT,                  -- One-time token for OAuth callback exchange (5-min expiry)
    callback_token_expires TIMESTAMPTZ,
    challenge_token TEXT,                 -- Short-lived token for cert provisioning (5-min expiry, single-use)
    challenge_token_expires TIMESTAMPTZ,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    last_heartbeat_at TIMESTAMP DEFAULT NOW(),
    status VARCHAR(20) DEFAULT 'active'  -- active, inactive, reclaimed
    -- Note: no cert_pem or key_pem - those live only in schmux
);

CREATE INDEX idx_registrations_code ON registrations(code);
CREATE INDEX idx_registrations_status ON registrations(status);
CREATE UNIQUE INDEX idx_registrations_challenge_token_unique ON registrations(challenge_token)
    WHERE challenge_token IS NOT NULL;

CREATE TABLE dns_challenges (
    id SERIAL PRIMARY KEY,
    registration_id INTEGER REFERENCES registrations(id),
    challenge_token TEXT UNIQUE NOT NULL,
    challenge_value TEXT NOT NULL,
    domain TEXT NOT NULL,                 -- e.g., _acme-challenge.47293.dashboard.sx
    status VARCHAR(20) DEFAULT 'pending', -- pending, verified, expired, cleaned
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    verified_at TIMESTAMP,
    cleaned_at TIMESTAMP
);

CREATE INDEX idx_dns_challenges_created ON dns_challenges(created_at);
CREATE INDEX idx_dns_challenges_status ON dns_challenges(status);

-- Required by SCS session manager for server-side OAuth sessions
CREATE TABLE sessions (
    token TEXT PRIMARY KEY,
    data BYTEA NOT NULL,
    expiry TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_sessions_expiry ON sessions (expiry);
```

**Challenge cleanup (runs hourly):**

Challenges older than 1 hour with status 'pending' are automatically marked 'expired', their TXT records are deleted from Route 53, and then marked 'cleaned'. Status lifecycle: `pending` → `verified` (if LE validates) or `expired` (if timeout) → `cleaned` (after TXT record deletion). This prevents orphaned DNS records from failed provisioning attempts.

### 2. schmux Client

**New files:**

- `internal/dashboardsx/client.go` - API client for dashboard.sx (with `OnLog` callback for HTTP observability)
- `internal/dashboardsx/acme.go` - Let's Encrypt provisioning via lego, `ServiceDNSProvider`
- `internal/dashboardsx/heartbeat.go` - Background heartbeat loop
- `internal/dashboardsx/renewal.go` - Automatic certificate renewal
- `internal/dashboardsx/status.go` - Status introspection (cert expiry, config)
- `internal/dashboardsx/instance_key.go` - Instance key generation and persistence
- `internal/dashboardsx/ip.go` - Private IP detection
- `internal/dashboardsx/paths.go` - File path management for `~/.schmux/dashboardsx/`
- `internal/dashboard/handlers_dashboardsx.go` - Daemon HTTP handlers (callback, provision status)
- `cmd/schmux/dashboardsx.go` - CLI commands

**Dependencies:**

- [lego](https://go-acme.github.io/lego/) - Let's Encrypt client library (Go)

**Config (`~/.schmux/config.json`):**

```json
{
  "network": {
    "dashboardsx": {
      "enabled": true,
      "code": "47293",
      "ip": "192.168.1.100",
      "service_url": "https://dashboard.sx"
    }
  }
}
```

**Instance key stored in:** `~/.schmux/dashboardsx/instance.key`

**Certificates stored in:**

- `~/.schmux/dashboardsx/cert.pem`
- `~/.schmux/dashboardsx/key.pem` (private key - never leaves this machine)
- `~/.schmux/dashboardsx/acme-account.key` (ACME account ECDSA P-256 private key, PEM format)

**CLI commands:**

```bash
# Start setup flow (opens browser for GitHub OAuth)
schmux dashboardsx setup

# Show current status
schmux dashboardsx status

# Disable (keeps registration, just stops HTTPS)
schmux dashboardsx disable

# Manually renew certificate
schmux dashboardsx renew-cert
```

**Daemon integration:**

1. On startup, if `dashboardsx.enabled = true`:
   - Load cert/key from `~/.schmux/dashboardsx/`
   - If cert missing or expired, provision new one via Let's Encrypt
   - Start HTTPS server on port 7337
   - Send heartbeat to dashboard.sx
   - Start heartbeat goroutine (every 24h)
   - Start cert renewal checker (daily)

2. Callback endpoint for setup:

   ```
   GET /api/dashboardsx/callback?callback_token=...
   ```

3. Provision status endpoint (for CLI polling):
   ```
   GET /api/dashboardsx/provision-status
   Response: { "status": "...", "domain": "...", "message": "..." }
   ```

### 3. Let's Encrypt Integration (in schmux)

Using [lego](https://go-acme.github.io/lego/) library with custom DNS provider:

```go
// Custom DNS provider that calls dashboard.sx API
type ServiceDNSProvider struct {
    client         *Client
    challengeToken string // from /cert-provisioning/start
    OnStatus       StatusFunc
}

func (p *ServiceDNSProvider) Present(domain, token, keyAuth string) error {
    // IMPORTANT: The TXT record value must be base64url(SHA256(keyAuth)),
    // NOT the raw keyAuth. Use dns01.GetRecord() to compute the correct value.
    fqdn, value := dns01.GetRecord(domain, keyAuth)

    // Call dashboard.sx to create TXT record with the hashed value
    err := p.client.DNSChallengeCreate(p.challengeToken, value)
    return err
}

func (p *ServiceDNSProvider) CleanUp(domain, token, keyAuth string) error {
    return p.client.DNSChallengeDelete(p.challengeToken)
}
```

**Critical: DNS-01 challenge value hashing.** Lego's `Present()` receives the raw `keyAuth` string, but the ACME DNS-01 spec requires the TXT record to contain `base64url(SHA256(keyAuth))`. Use `dns01.GetRecord(domain, keyAuth)` from lego to compute the correct FQDN and value. Passing the raw `keyAuth` as the TXT value will cause Let's Encrypt validation to fail because it will find the wrong value at the authoritative nameservers.

**Certificate provisioning:**

```go
func (cm *CertManager) ProvisionCert(code string, userEmail string) error {
    // Generate new ECDSA P-256 private key locally (fresh key on each provision/renewal)
    privKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

    // Create lego client with custom DNS provider
    client, _ := lego.NewClient(&lego.Config{
        User: lego.User{
            Email: userEmail,  // User's GitHub email from registration
        },
        Key: privKey,
    })

    client.Challenge.SetDNS01Provider(&dashboardsxProvider{...})

    // Request certificate
    cert, err := client.Certificate.Obtain(certificate.ObtainRequest{
        Domains: []string{code + ".dashboard.sx"},
    })

    // Save cert and key locally with restrictive permissions
    os.WriteFile("cert.pem", cert.Certificate, 0600)
    os.WriteFile("key.pem", cert.PrivateKey, 0600)

    return nil
}
```

**DNS propagation verification:**

- schmux verifies TXT record exists before telling Let's Encrypt to check
- **Critical: query authoritative nameservers directly, not recursive resolvers.** Recursive resolvers (e.g. 8.8.8.8) cache negative responses — after Route 53 creates the TXT record, recursive resolvers continue returning "not found" until the negative TTL expires. Querying the authoritative NS for dashboard.sx (the Route 53 nameservers) returns the record immediately.
- Authoritative nameservers for dashboard.sx: `ns-1174.awsdns-18.org`, `ns-1727.awsdns-23.co.uk`, `ns-357.awsdns-44.com`, `ns-751.awsdns-29.net`
- These are hardcoded — dashboard.sx's NS delegation does not change between runs
- Poll every 5 seconds, timeout after 5 minutes
- If timeout, fail with clear error message

---

## Security

### Private Keys

- **Never leave the user's machine** - generated and stored only in `~/.schmux/dashboardsx/key.pem`
- **Algorithm**: ECDSA P-256 (not RSA 2048 - better for 2026)
- **Generated using `crypto/rand`** (not `math/rand`)
- **File permissions**: `0600` (owner read/write only)
- **Directory permissions**: `~/.schmux/dashboardsx/` is `0700`
- dashboard.sx never sees private keys or certificates
- Certificate provisioning happens entirely within schmux

### Instance Key

- Random 32-byte value, hex-encoded (64 chars)
- Generated using `crypto/rand`
- Stored locally in `~/.schmux/dashboardsx/instance.key` with permissions `0600`
- **Never transmitted over unencrypted connections**
- Used to authenticate API calls to dashboard.sx (heartbeat, DNS challenge)
- dashboard.sx stores only the **SHA-256 hash** (`instance_key_hash`) — the original value is stored temporarily during OAuth setup and cleared after `/callback/exchange`

### Callback Security (Preventing LAN Sniffing)

The callback flow is designed to prevent instance_key exposure on the LAN:

1. **One-time callback_token**: Instead of including instance_key in the callback URL, dashboard.sx generates a single-use token that expires in 5 minutes.

2. **Token exchange over HTTPS**: schmux exchanges the callback_token for the registration info via HTTPS to dashboard.sx. The instance_key is returned in this HTTPS response, never exposed on the LAN.

3. **Instance key validation**: schmux validates the returned instance_key matches its stored value.

4. **return_url validation**:
   - Must match the IP address provided in the registration
   - Must be a private IP range (192.168.x.x, 10.x.x.x, 172.16-31.x.x) or localhost
   - Cannot redirect to arbitrary public URLs

### DNS Challenge Authentication

The `/dns-challenge` endpoint requires:

1. **instance_key** - proves caller owns this registration (validated via SHA-256 hash lookup)
2. **challenge_token** - short-lived token (5 min) issued when cert provisioning starts
   - Generated by dashboard.sx when schmux calls `/cert-provisioning/start`
   - Stored in the `registrations` table (not a separate table)
   - Bound to the specific registration via `instance_key_hash`
   - **Single-use** — validated and cleared in an atomic database operation during `/dns-challenge` POST
   - Has a unique constraint to prevent collisions
3. **Source IP validation** - request must come from the registered IP address (skipped for RFC1918 private IPs, since schmux registers from private networks)

This prevents an attacker with only the instance_key from creating arbitrary TXT records.

### Callback Token Lifecycle

The callback_token bridges the OAuth redirect back to schmux:

1. Generated after GitHub OAuth callback (random 32 bytes, hex-encoded, 5-min expiry)
2. Passed in the redirect URL to schmux's `/api/dashboardsx/callback`
3. Exchanged via `POST /callback/exchange` — returns `instance_key`, `code`, `email`
4. **Both `callback_token` and `instance_key` are cleared from the database after exchange** (single-use, for security — instance_key only existed temporarily to be returned in this response)

### Registration Idempotency

When a user retries registration with an existing `instance_key` (e.g., setup was interrupted, IP changed, or re-running on the same machine):

| Scenario                                                                 | Behavior                                                                                                          |
| ------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------- |
| **Flow interrupted** — registration exists, same user, active status     | Return existing registration with fresh `callback_token`, update IP if changed                                    |
| **IP changed** — same as above                                           | Update IP, update DNS A record, refresh `callback_token`                                                          |
| **Reclaimed** — registration exists but status is 'reclaimed'            | Reject. Redirect to `return_url?error=instance_key_reclaimed`. schmux generates a new instance_key automatically. |
| **Different user** — instance_key registered to different GitHub account | Reject. Show generic error page (do NOT reveal which account owns it, do NOT redirect to return_url).             |

### TLS Requirements

**dashboard.sx:**

- Serves HTTPS only (no HTTP)
- TLS 1.2+ required, TLS 1.3 preferred
- Modern cipher suites only (no RC4, 3DES, etc.)
- HSTS header with long max-age

**schmux HTTPS server:**

- TLS 1.2+ required
- ECDSA certificates (from Let's Encrypt)
- OCSP stapling enabled
- HTTP→HTTPS redirect on port 7337 (once HTTPS is set up)

### Source IP Validation

Authenticated endpoints validate source IP, with an exception for RFC1918 private addresses (since schmux registers from private networks and NAT makes source IP matching unreliable):

| Endpoint                   | Source IP Check                                                        |
| -------------------------- | ---------------------------------------------------------------------- |
| `/heartbeat`               | Must match registered ip_address (skipped if registered IP is RFC1918) |
| `/dns-challenge`           | Must match registered ip_address (skipped if registered IP is RFC1918) |
| `/cert-provisioning/start` | Must match registered ip_address (skipped if registered IP is RFC1918) |
| `/callback/exchange`       | No IP check (token-based auth)                                         |

For public IP registrations, this prevents an attacker with a stolen instance_key from making API calls from the internet.

### Rate Limits

| Endpoint                             | Limit     | Scope              |
| ------------------------------------ | --------- | ------------------ |
| `/register`                          | 10/hour   | Per IP address     |
| `/callback/exchange`                 | 5/minute  | Per IP address     |
| `/cert-provisioning/start`           | 10/hour   | Per IP address     |
| `/dns-challenge`                     | 10/hour   | Per IP address     |
| `/heartbeat`                         | 1/hour    | Per registration   |
| `/api/dashboardsx/callback` (schmux) | 10/minute | Per source IP      |
| Registrations                        | 5 max     | Per GitHub account |

**Rate limit headers returned on all rate-limited endpoints:**

- `X-RateLimit-Limit`
- `X-RateLimit-Remaining`
- `X-RateLimit-Reset`
- `Retry-After` (on 429 responses)

### Revocation and Incident Response

**If instance_key is compromised:**

1. User runs `schmux dashboardsx reclaim`
2. Old registration is immediately invalidated
3. DNS A record is deleted immediately
4. Certificate is revoked via Let's Encrypt ACME revocation API
5. User can register again with new instance_key
6. **Holdoff period**: Code cannot be reused for 24 hours (prevents race conditions)

**Emergency kill-switch:**

Dashboard.sx operators can immediately:

- Delete any DNS record
- Revoke any certificate via ACME
- Invalidate any instance_key

### Code Assignment

- **Random, not sequential** - prevents enumeration and registration order leakage
- **Cryptographically random** - uses `crypto/rand`
- **Collision handling** - if random code already exists, try again (max 10 attempts)
- **Exhaustion plan**: If >80% of codes used, expand to 6 digits
- **Holdoff on reuse**: After reclamation, code cannot be reassigned for 24 hours

### IP Address Validation (DNS Rebinding Prevention)

Registered IP addresses are validated to prevent DNS rebinding attacks:

**Allowed ranges:**

- 192.168.0.0/16 (RFC 1918)
- 10.0.0.0/8 (RFC 1918)
- 172.16.0.0/12 (RFC 1918)

**Blocked addresses:**

- 127.0.0.0/8 (loopback)
- 169.254.0.0/16 (link-local / cloud metadata)
- 0.0.0.0/0 (unspecified)
- Any public IP address

### Email Privacy

User's GitHub email is used for Let's Encrypt account and appears in certificate transparency logs.

**Mitigation options:**

- User can use a GitHub email alias
- Future: support separate `cert_email` field for privacy-conscious users

### Audit Logging

All security-relevant events are logged with sensitive fields redacted:

| Event                  | Logged (Redacted)                                      |
| ---------------------- | ------------------------------------------------------ |
| Registration created   | github_id, code[0:2]\*\*\*, ip, timestamp              |
| DNS challenge created  | registration_id, challenge_token[0:4]\*\*\*, timestamp |
| DNS challenge verified | registration_id, timestamp                             |
| Heartbeat received     | registration_id, source_ip, timestamp                  |
| Key regenerated        | registration_id, timestamp                             |
| Registration reclaimed | registration_id, reason, timestamp                     |
| Failed auth attempt    | endpoint, source_ip, timestamp                         |

**Redaction policy:**

- instance_key never logged
- challenge_token truncated
- Full values only in debug mode (off by default)

Logs retained for 90 days.

### Monitoring and Alerting

Dashboard.sx monitors for suspicious activity:

| Alert Condition                  | Threshold                 | Action                      |
| -------------------------------- | ------------------------- | --------------------------- |
| Failed DNS challenges            | 5 in 1 hour               | Rate limit, log warning     |
| Rapid registrations              | 3 from same IP in 1 hour  | Rate limit, flag for review |
| Instance key reuse attempts      | Any                       | Log, alert, reject          |
| Heartbeat from wrong IP          | Any                       | Log warning, reject         |
| Multiple registrations same code | Any (should never happen) | Alert, investigate          |

### Challenge Cleanup

Challenges expire after 1 hour. Cleanup runs **hourly** (not daily) to prevent orphaned TXT records from interfering with new provisioning attempts.

### Heartbeat Jitter

Heartbeats use randomized timing to prevent surveillance:

```
base_interval: 24 hours
jitter: ±2 hours (random)
actual_interval: 22-26 hours (randomized each cycle)
```

### Trust Boundaries

| Trust Assumption                  | Risk if Violated                                                  |
| --------------------------------- | ----------------------------------------------------------------- |
| schmux binary is not compromised  | Compromised binary could exfiltrate private keys                  |
| dashboard.sx is not malicious     | Malicious service could serve wrong DNS records                   |
| User's machine is not compromised | Attacker could read instance_key and cert/key files               |
| GitHub OAuth is secure            | Account takeover would allow registration hijacking               |
| LAN is not being sniffed          | Callback_token could be captured (but 5-min expiry limits damage) |

The security model assumes schmux and the user's machine are trusted. The dashboard.sx service is semi-trusted (it handles DNS but never sees private keys).

### Subdomain Takeover Prevention

When a code is reclaimed and reassigned to a new user, the old instance_key is **immediately invalidated**:

1. **Atomic invalidation**: When code is reassigned, the old registration's instance_key is marked invalid in the database before the new registration is created.

2. **Challenge token binding**: Challenge tokens are cryptographically bound to the specific registration ID, not just the code. Old challenge tokens from the previous registration cannot create TXT records for the new registration.

3. **Source IP binding**: The old instance_key's source IP no longer matches the new registration's IP, so even if an attacker has the old key, they cannot pass source IP validation.

This prevents a previous owner from responding to DNS challenges after their code is reassigned.

### Instance Key Uniqueness and Binding

**Instance keys are globally unique and bound to a single registration:**

1. **Single-use**: Each instance_key can only be associated with one registration. Attempting to register with an instance_key that's already in use returns an error.

2. **No sharing**: If a user copies their `~/.schmux/dashboardsx/` directory to another machine, only one can successfully send heartbeats (the one with the source IP matching the registered IP). The other will be rejected.

3. **No rotation mechanism**: The instance_key is static for the lifetime of the registration. If compromised, the user must reclaim and re-register (which generates a new key).

**Why not passphrase-derived or TPM-backed?**

- Passphrase-derived keys require user interaction on every daemon restart, breaking the "start and forget" model.
- TPM integration varies wildly across platforms (Windows TPM, Linux tpm2-tss, macOS Secure Enclave) and adds significant complexity.
- The threat model assumes if the machine is compromised, the attacker already has access to the LAN where schmux runs.

### DNS Rebinding Tradeoffs

**This service intentionally maps public hostnames to private IP addresses.** This is the core functionality, but it has security implications:

1. **Browser same-origin policy still applies**: The browser treats `https://47293.dashboard.sx:7337` as a distinct origin. Cookies, localStorage, and JavaScript are isolated per-subdomain.

2. **SameSite cookie recommendation**: schmux should set `SameSite=Lax` or `SameSite=Strict` on any cookies it issues to prevent cross-site request forgery.

3. **CORS restrictions**: schmux should not allow arbitrary `Origin` headers. The dashboard API should only allow CORS from its own origin.

4. **Not a general-purpose DNS rebinding attack**: An attacker cannot register `47293.dashboard.sx` pointing to `192.168.1.1` and attack internal services because:
   - Registration requires GitHub OAuth (identity verification)
   - Only one registration per code
   - Source IP must match during cert provisioning
   - The attacker would need to control a machine on the victim's LAN

5. **Mitigation for internal services**: If you run internal services that should never be accessed via dashboard.sx hostnames, configure them to validate the `Host` header and reject requests for `*.dashboard.sx`.

### Certificate Transparency Privacy Warning

**Let's Encrypt certificates are logged to public Certificate Transparency (CT) logs.** Users should be aware that:

1. **Internal topology exposure**: Anyone searching CT logs for `*.dashboard.sx` can see:
   - That you use schmux
   - Your internal IP address (via DNS resolution, not in the cert itself)
   - The approximate timing of your certificate issuance/renewal

2. **Mitigation options**:
   - Use a GitHub email alias (not your primary email) for registration
   - Be aware that your internal IP structure becomes partially public
   - Enterprise users should evaluate whether this exposure is acceptable

3. **Future consideration**: Support a separate `cert_email` field allowing users to specify a throwaway email for the ACME account, independent of their GitHub email.

### Heartbeat IP Change Handling

While source IP validation prevents external attackers from sending heartbeats, IP changes within the LAN need special handling:

1. **IP change detection**: If a heartbeat comes from a different IP than the registered IP:
   - Log a warning (with both old and new IP redacted)
   - Require a **re-authentication flow** before updating the registered IP
   - Send an email notification to the user

2. **Re-authentication for IP changes**:
   - User must click a link in the email or re-run `schmux dashboardsx setup`
   - This triggers a new OAuth flow to verify the user still controls the GitHub account
   - On success, the registered IP is updated

3. **Rate limit**: IP change requests are limited to 1 per 24 hours per registration.

**Rationale**: An attacker who compromises the instance_key but is on a different network cannot change the DNS record to point to their IP without re-authenticating via GitHub.

### GitHub Account Recovery

If a user's GitHub account is compromised, the attacker gains control of all registrations associated with that account.

**Recovery options:**

1. **Revoke via GitHub re-auth**: User can re-run `schmux dashboardsx setup` on a new machine, authenticate with GitHub (after recovering the account), and the new registration will invalidate the old one (same code = old registration invalidated).

2. **dashboard.sx UI**: Users can log in to `https://dashboard.sx/` with GitHub to view and revoke active registrations.

3. **Email recovery**: If GitHub account is recovered, user can contact dashboard.sx support with proof of GitHub account ownership to revoke registrations.

### ACME Account Handling

1. **Account key vs certificate key**: The ACME account key (used to authenticate with Let's Encrypt) is separate from the certificate private key. The account key is generated once and stored locally.

2. **Email changes**: If the user changes their GitHub email between renewals:
   - Let's Encrypt sends expiry warnings to the old email
   - Renewal will still work (email is not validated during renewal)
   - User can update their email by running `schmux dashboardsx setup` again

3. **Account compromise**: If an attacker gains access to the ACME account key:
   - They can only request certificates for domains they can prove ownership of (via DNS challenge)
   - They cannot revoke existing certificates without the certificate private key
   - Mitigation: Regenerate the ACME account key by deleting `~/.schmux/dashboardsx/acme-account.key`

### Database Security

1. **Instance key storage**: Instance keys are stored as **SHA-256 hashes**, not plaintext. During authentication, the submitted instance_key is hashed and compared.

2. **GitHub OAuth tokens**: Only the `read:user` and `user:email` scopes are requested. Tokens are stored encrypted at rest using AES-256-GCM with a key derived from a master secret stored in a cloud secrets manager.

3. **No sensitive data in backups**: The database backup process excludes the `instance_key_hash` column (not needed for recovery) and encrypts OAuth tokens before export.

### Port Limitation

DNS A records do not include port numbers. This service assumes schmux runs on port **7337**.

**Implications:**

- All users must use port 7337 for HTTPS access
- If another service is using port 7337, it must be stopped or reconfigured
- The URL format is always `https://xxxxx.dashboard.sx:7337` (port is explicit)

**Future consideration**: Support SRV records for port flexibility, though this would require browser support that may not exist.

---

## Reclamation Policy

| Status    | Condition                | Action                                                                                     |
| --------- | ------------------------ | ------------------------------------------------------------------------------------------ |
| active    | Heartbeat within 7 days  | DNS record exists                                                                          |
| inactive  | No heartbeat for 7+ days | Marked inactive, warning email sent                                                        |
| reclaimed | Inactive for 30+ days    | DNS record deleted, cert revoked, instance_key invalidated, code held for 24h before reuse |

**When a code is reassigned after reclamation:**

1. Old instance_key is invalidated (can no longer authenticate)
2. Old challenge tokens are invalidated
3. New registration gets a new instance_key
4. 24-hour holdoff prevents race conditions

---

## Implementation Status

All core phases are complete and working in production:

- **schmux client**: CLI setup, ACME/lego integration, heartbeat, auto-renewal, granular status reporting
- **dashboard.sx service**: GitHub OAuth, Route 53 A/TXT record management, challenge token lifecycle, background cleanup jobs
- **End-to-end flow**: `schmux dashboardsx setup` → browser OAuth → callback → cert provisioning → HTTPS serving

### Not Yet Implemented

| Feature                                  | Spec Reference                 | Status          |
| ---------------------------------------- | ------------------------------ | --------------- |
| IP change re-authentication flow         | "Heartbeat IP Change Handling" | Not implemented |
| Warning email on inactive registration   | "Reclamation Policy"           | Not implemented |
| Dashboard UI for revoking registrations  | "GitHub Account Recovery"      | Not implemented |
| `schmux dashboardsx reclaim` CLI command | "CLI commands"                 | Not implemented |

---

## Open Questions

1. **Port configuration**: Fixed at 7337 or configurable? (Current design: fixed)
2. **Multiple IPs**: What if machine has multiple IPs? (Current design: user selects during setup)
3. **IPv6 support**: Include AAAA records? (Future consideration)

## Decisions Made

1. **ACME account**: Per-instance Let's Encrypt account, using user's GitHub email
2. **Private key handling**: Generate new private key on each provision/renewal (more secure)
3. **DNS propagation**: Query authoritative nameservers directly (not recursive resolvers). Route 53 NS for dashboard.sx are hardcoded. Poll every 5 seconds, 5 minute timeout.
4. **Challenge cleanup**: Hourly job cleans expired challenges (marks expired, deletes TXT records from Route 53, marks cleaned). Separate hourly jobs handle code hold releases and registration reclamation.
5. **Setup UX: CLI is primary**. Browser shows "check your terminal" after callback. CLI polls daemon for provision status and shows progress. No JavaScript polling in the browser.
6. **Daemon restart**: User restarts manually. CLI prints instructions after cert provisioning completes. Daemon cannot hot-swap HTTP→HTTPS.
7. **Provision status granularity**: Every step of the post-browser provisioning flow reports status to the CLI via polling. The `Client` logs every HTTP call to dashboard.sx (URL, status code, response). `ProvisionCert` reports each stage (account load, LE registration, cert request, save). The DNS provider reports TXT create/verify/cleanup. If it breaks, the CLI shows exactly which call failed.

---

## Testing

### Unit Tests

- schmux: instance key generation, callback validation, heartbeat logic
- schmux: certificate provisioning flow (mock Let's Encrypt)
- dashboard.sx: code assignment, DNS record creation
- dashboard.sx: DNS challenge lifecycle

### Integration Tests

- Full setup flow (mock GitHub, mock Let's Encrypt)
- Certificate provisioning with real DNS challenge API
- Heartbeat cycle
- Reclamation flow
- Challenge expiration and cleanup

### Manual Testing

- Complete setup from fresh schmux install
- Verify certificate is issued and stored locally
- Access `https://xxxxx.dashboard.sx:7337` from another device
- Verify clipboard works
- Test certificate renewal
- Test reclamation after 30+ days inactive
