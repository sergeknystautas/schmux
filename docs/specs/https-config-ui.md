# HTTPS Config UI

## Problem

HTTPS configuration is buried inside the GitHub Authentication section of the Access tab. You have to enable GitHub auth just to see the TLS cert/key path fields. There's no way to enable HTTPS independently, and nothing tells you that auth requires HTTPS.

## Design

### Access Tab Reorganization

The Access tab gets reorganized into a cascading group (each depends on the previous) plus one independent section:

1. **Network** (existing) — bind address (localhost vs LAN), port
2. **HTTPS** (new section) — enable toggle, cert/key paths via modal, read-only hostname, auto-composed dashboard URL
3. **GitHub Authentication** (restructured) — enable toggle (greyed out until HTTPS is configured), session TTL, OAuth credentials
4. **Remote Access** (moved, independent) — Cloudflare tunnel, password, ntfy — visually separated from the cascade

All sections are always visible. Sections whose prerequisites aren't met are greyed out with a badge explaining why (e.g., "Requires HTTPS").

### HTTPS Section

**Toggle**: "Enable HTTPS" checkbox.

**When disabled**: Just the toggle with description: "Encrypt dashboard traffic with TLS certificates."

**When enabled**:

- **Certificate paths** — displayed read-only. A "Configure" button opens the TLS modal.
- **Certificate hostname** — read-only, extracted from cert (e.g., "schmux.local"). Only shown when cert is configured.
- **Dashboard URL** — read-only, auto-composed from `https://` + cert hostname + port from Network section. Replaces the manual `public_base_url` field.

### TLS Certificate Modal

Single modal with both cert and key path inputs. Server-side validation on submit:

- Two path input fields (cert path, key path), pre-filled with current values if configured
- "Validate" button hits `POST /api/tls/validate`
- Inline validation results: file exists, valid PEM, cert+key match, hostname, expiry
- "Save" only enabled after successful validation

### GitHub Auth Section Changes

- **Toggle greyed out** with "Requires HTTPS" badge when HTTPS is off. When HTTPS is on but certs not configured: "Configure HTTPS certificates first."
- **Dashboard URL** — read-only, inherited from HTTPS section. Shown for context (OAuth callback URL) but not editable.
- **Session TTL** — editable, minutes (existing)
- **GitHub OAuth Credentials** — existing modal flow, unchanged
- **Removed from this section**: cert path, key path, editable dashboard URL field (all moved to HTTPS section)

### Backend Changes

**New endpoint**: `POST /api/tls/validate`

- Input: `{ "cert_path": "...", "key_path": "..." }`
- Validates: files exist, readable, valid PEM, cert+key match, extracts hostname from SAN/CN, checks expiry
- Output: `{ "valid": true, "hostname": "schmux.local", "expires": "2027-01-15T00:00:00Z", "error": "" }`

**Server startup decoupling**:

- Current: `if GetAuthEnabled() -> ListenAndServeTLS()`
- New: `if GetTLSEnabled() -> ListenAndServeTLS()`
- `GetTLSEnabled()` already exists (checks both cert+key paths are non-empty), just needs to replace `GetAuthEnabled()` in server startup
- Auth still requires TLS at the config validation level, but TLS runs independently

**Dashboard URL**: Auto-derived from cert hostname + port during TLS validation. Stored in `public_base_url` as before. No new config fields needed.

### Edge Cases

- **HTTPS enabled, no certs configured** — warning on save, auth toggle stays disabled
- **Auth enabled, user disables HTTPS** — confirmation dialog: "Disabling HTTPS will also disable GitHub Authentication. Continue?" Disables both.
- **Cert expiring soon** — yellow warning if < 30 days from expiry
- **Port changes** — Dashboard URL auto-updates (composed from hostname + port)
- **Existing configured users** — loads seamlessly into new layout, same underlying config fields
