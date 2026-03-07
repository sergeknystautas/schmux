# GitHub Features (Not Yet Implemented)

**Status:** Not implemented. These are consolidated design notes from four specs for future reference.

## 1. GitHub Integration (`gh auth status` gate)

**Problem:** GitHub features (PR list, repo links) show for all users even when `gh` CLI isn't installed. Local repos created via `local:name` can't be published to GitHub.

**Proposed design:**
- Run `gh auth status` at daemon startup → `GitHubStatus { Available, Username }`
- Gate all GitHub UI sections on this status
- Add "Publish to GitHub" flow for local repos using `gh repo create`
- Skip remote git checks (fetch, ahead/behind) for `local:` repos

**Source specs:** `github-integration.md`, `github-pr-discovery.md` (PR discovery is already implemented — see `docs/git-features.md`)

## 2. GitHub OAuth Authentication (dashboard login)

**Problem:** The dashboard has no authentication. Anyone with network access can control all sessions.

**Proposed design:**
- GitHub OAuth flow: `/auth/login` → GitHub → `/auth/callback` → session cookie
- Config: `access_control.enabled`, `access_control.provider`, `network.public_base_url`, `network.tls`
- Secrets: `auth.github.client_id`, `auth.github.client_secret` in `~/.schmux/secrets.json`
- All UI/API/WS routes require auth when enabled
- CORS locked to `public_base_url` origin, `Access-Control-Allow-Credentials: true`
- CLI setup command: `schmux auth github` (interactive prompts for URL, TLS paths, OAuth credentials)

**Key decisions from the specs:**
- Auth is independent of `network_access` (can enable auth without network access and vice versa)
- TLS is required when auth is enabled (except `http://localhost`)
- Session TTL configurable via `session_ttl_minutes` (default 1440)
- CLI validates but allows saving with warnings (non-blocking validation)
- v2 CLI adds prerequisite checking and optional `mkcert` certificate generation

**Source specs:** `github-auth-v1.md`, `github-auth-cli.md`, `github-auth-cli-v2.md`
