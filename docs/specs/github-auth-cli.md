# GitHub Auth CLI Spec

**Goal**
Add an interactive CLI flow to configure GitHub auth and TLS without hand-editing JSON. The CLI writes to `config.json` and `secrets.json`, performs non-blocking validation, and allows saving even when warnings exist.

**Command**

- `schmux auth github`

**Non-Goals (v1)**

- Full OAuth verification flow.
- Org/team allowlists.
- Multiple providers.

---

## Data Sources / Writes

### Config (`config.json`)

Writes under `access_control`:

- `network_access` (prompted; user can keep existing)
- `auth.enabled` (set true)
- `auth.provider` (`github`)
- `auth.public_base_url`
- `auth.session_ttl_minutes`
- `auth.tls.cert_path`
- `auth.tls.key_path`

### Secrets (`~/.schmux/secrets.json`)

Writes:

- `auth.github.client_id`
- `auth.github.client_secret`

Must preserve existing model secrets and existing auth secrets for other providers.

---

## Interactive Flow (CLI)

1. **Network Access**
   - Prompt: `Enable local network access? (y/N)`
   - Default: current config value.

2. **Public Base URL**
   - Prompt for `public_base_url` (required).
   - Must be `https://...` (allow `http://localhost` only).

3. **TLS Cert/Key Paths**
   - Prompt for `tls.cert_path` and `tls.key_path`.
   - Accept absolute or `~` paths.

4. **Session TTL**
   - Prompt for `session_ttl_minutes` (default 1440; use existing if set).

5. **GitHub OAuth Credentials**
   - Prompt for `client_id` and `client_secret` (stored in `secrets.json`).

6. **Validation Summary**
   - Show warnings if any of the checks fail (see below).
   - Prompt: `Save anyway? (y/N)`

7. **Write + Restart Guidance**
   - Write config and secrets.
   - Print restart instructions: `./schmux stop && ./schmux start`.

---

## Validation (Warnings Only)

### Required Presence Checks (warn only)

- `public_base_url` present and parseable URL.
- `tls.cert_path` and `tls.key_path` present.
- `client_id` and `client_secret` present.

### TLS Checks (warn only)

- Files exist and are readable.
- Cert parses successfully.
- Cert SAN (or CN fallback) matches `public_base_url` host.

### Connectivity Checks

- None (no OAuth flow validation in v1).

---

## Web UI Validation (Advanced Config)

Add non-blocking warnings during save:

- Public base URL is missing/invalid or non-https (except localhost).
- TLS cert/key missing or unreadable.
- Cert SAN/CN does not match `public_base_url` host.
- Missing GitHub `client_id`/`client_secret` in `secrets.json` (if UI can detect via API).

Behavior:

- Warnings are shown inline.
- User can still save.
- Save triggers `needs_restart`.

---

## UX Copy (CLI)

- `public_base_url` prompt hint:
  - `Example: https://schmux.local`
- TLS warning copy:
  - `Warning: certificate does not match host schmx.local (SAN/CN mismatch)`
- Save prompt:
  - `Proceed and save anyway? (y/N)`

---

## Implementation Notes (for later)

- Use `~` expansion for file paths.
- Do not overwrite unrelated fields in config.
- Preserve existing secrets structure and merge updates.
- If `auth.enabled` already true, allow reconfiguration.
