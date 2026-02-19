# Multi-User Sessions for Shared Schmux Instances

**Status**: Speculative / Stubbed
**Author**: Aaron (farra)
**Context**: Jam & Tea shared development workflow

## Problem

Schmux is currently single-user. When deployed on a shared VM (e.g., `jambot-dev-1:7337` accessed via Tailscale), multiple developers can open the dashboard and interact with sessions, but there is no concept of user identity. No one knows who spawned a session, who's currently viewing it, or who last interacted with it. This makes the "shared vibe coding" workflow — where developer A starts a session and developer B picks it up later — functional but opaque.

### Use Case: Jam & Tea Shared Development

Jam & Tea is a small startup (2-5 developers) building a Godot-based game and several other projects (Rust, JavaScript). The team uses agentic coding extensively. The desired workflow:

1. Deploy schmux to `http://jambot-dev-1:7337` on a dev-node-infra EC2 VM
2. Create a workspace for "project scone" (a Godot/C#/.NET game)
3. Developer A starts a Claude session on scone, works for a while, pauses
4. Developer B opens the dashboard, sees A's session, communicates with A out-of-band, then continues work in that same session
5. Both developers can see session history — who started it, who interacted last

This is a high-trust environment (everyone is on the same Tailscale network, same small team). The goal is visibility and handoff, not access control or isolation.

### What Exists Today

- **GitHub OAuth**: Gates dashboard access (single-user, on/off). Implemented in `pkg/cli/daemon_client.go`.
- **Remote tunnel + PIN auth**: Cloudflared tunnel with PIN-based access for remote dashboard exposure. Differentiates local vs. remote but not between users.
- **No session ownership**: Sessions have an `id`, `target`, `workspace`, `status` — but no `created_by`, `last_active_user`, or similar fields.
- **Chaplin spec** (unmerged `feature/orchestration` branch): Describes a full federation/coordination service with user management, per-project chat, and activity recordings. Much larger scope than what's needed here.

## Design Principles

1. **Lightweight** — This is not chaplin. No federation, no coordination service, no chat. Just user identity on a single shared instance.
2. **High trust** — All authenticated users can see and interact with all sessions. No per-session permissions.
3. **Opt-in** — Single-user deployments should work exactly as they do today with zero configuration.
4. **Identity via existing auth** — Leverage GitHub OAuth (already implemented) to identify users. No new auth system.

## Proposed Design

### User Identity

When GitHub OAuth is enabled, the authenticated GitHub user becomes the session identity. The dashboard already gates access via OAuth; we extend it to track *who* is behind each action.

```go
// internal/state/state.go
type UserInfo struct {
    GitHubLogin  string `json:"github_login"`
    DisplayName  string `json:"display_name"`
    AvatarURL    string `json:"avatar_url"`
}
```

### Session Ownership

Extend the session model with user tracking:

```go
type Session struct {
    // ... existing fields
    CreatedBy      *UserInfo `json:"created_by,omitempty"`
    LastActiveUser *UserInfo `json:"last_active_user,omitempty"`
    LastActiveAt   time.Time `json:"last_active_at,omitempty"`
}
```

- `CreatedBy` is set once at spawn time
- `LastActiveUser` updates when someone sends input via the terminal WebSocket
- `LastActiveAt` updates on any user interaction (input, nudge, dispose)

### Dashboard Changes

**Session list / sidebar**:
- Show avatar + initials badge on each session card
- If `last_active_user` differs from `created_by`, show both (small "started by A, last active B")
- Sessions without user info (pre-upgrade or OAuth disabled) display as they do today

**Terminal view**:
- Small user presence indicator showing who else has this terminal open (via WebSocket connection tracking)
- Optional: "A is also viewing this session" banner

**No new pages or modals needed.** This is purely additive UI on existing views.

### API Changes

**GET /api/sessions** — extend response:

```json
{
  "sessions": [
    {
      "id": "claude-abc123",
      "status": "running",
      "created_by": {
        "github_login": "farra",
        "display_name": "Aaron",
        "avatar_url": "https://github.com/farra.png"
      },
      "last_active_user": {
        "github_login": "cofounder",
        "display_name": "Pat",
        "avatar_url": "https://github.com/cofounder.png"
      },
      "last_active_at": "2026-02-18T14:30:00Z"
    }
  ]
}
```

**WebSocket /ws/terminal/{id}** — extract user identity from the OAuth session/cookie on connection. Track active viewers per session for presence indicators.

**POST /api/spawn** — attach `created_by` from the authenticated user context.

### Extracting User Identity

The GitHub OAuth flow already authenticates users and has access to their profile. The implementation needs to:

1. Store the authenticated user's `UserInfo` in the HTTP session/cookie (may already be partially done for OAuth gating)
2. Pass `UserInfo` through to API handlers via request context
3. Attach it to state mutations (spawn, input, nudge, dispose)

If OAuth is disabled, `UserInfo` is nil and everything works as today.

## What This Does NOT Cover

- **Per-session permissions** — Any authenticated user can interact with any session. High-trust model.
- **Chat between users** — Use Slack, Discord, or voice. Chaplin covers this if ever needed.
- **Federation** — This is single-instance only. Multiple schmux instances remain independent.
- **Concurrent editing conflicts** — Two users can type into the same terminal simultaneously. tmux handles this naturally (both inputs go to the same pane). This is a feature, not a bug, for pair programming.
- **User management UI** — No admin panel for managing users. If you have GitHub OAuth access, you're in.

## Implementation Scope

This is a small change set:

- Extend `state.Session` with 3 new fields
- Extract `UserInfo` from OAuth context in API handlers (middleware)
- Pass user info through spawn, input, nudge, dispose code paths
- Frontend: avatar badges on session cards, presence indicator on terminal view
- No new endpoints, no new auth flows, no database

### Estimated Complexity

- Backend: ~200-300 lines of Go across 4-5 files
- Frontend: ~100-150 lines of React across 2-3 components
- Tests: standard unit tests for state changes, one integration test for the spawn→list flow

## Relationship to Other Specs

- **Chaplin** (`multiplayer-orchestration.md`, unmerged): This spec is a minimal subset of chaplin's user model. If chaplin is ever built, this work feeds directly into it — the `UserInfo` type and session ownership model would be reused.
- **Multi-worker architecture** (`multi-worker-architecture.md`): Orthogonal. Multi-user is about identity; multi-worker is about execution topology. They compose cleanly.
- **Environment init hooks** (`environment-init-hooks-farra.md`): Complementary. Init hooks solve "what tools are available"; multi-user solves "who is using them". Together they enable the full shared development workflow.

## Architectural Position

This spec fills a specific gap in the schmux remote platform design. Three layers are needed for shared remote development:

| Layer | Concern | Solution |
|-------|---------|----------|
| **Workspace runtime** | Per-workspace environment activation, IDE connection | `environment-init-hooks-farra.md` (`shell_init`) |
| **User identity** | Who spawned/touched each session on a shared instance | **This spec** |
| **Federation / coordination** | Cross-instance awareness, group chat, floor manager | Chaplin (unmerged spec, future) |

This spec is the "missing glue" between the workspace layer and any future federation work. It can be built independently of both — environment hooks don't need user identity, and chaplin doesn't need to exist for user identity to be useful. But all three compose cleanly when present.
