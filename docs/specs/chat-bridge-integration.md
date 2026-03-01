# Chat Bridge Integration

**Date:** 2026-02-28
**Branch:** spec/chat-bridge-integration

## Problem

Teams using schmux on remote infrastructure (EC2, etc.) want visibility into what their agents are doing without SSH-ing into the box or opening the web dashboard. The natural place for this is the team's existing chat platform — Discord or Slack.

Existing projects like `claude-code-discord` spin up independent Claude Code sessions, but what teams actually want is a window into their *running schmux sessions*: status checks, activity summaries, and the ability to relay requests to agents already doing work.

## Goals

1. **Observe** — query session status, get activity summaries, receive alerts
2. **Command** — spawn, dispose, nudge sessions from chat
3. **Converse** — ask questions about what agents are doing, relay instructions to idle sessions
4. **Platform-agnostic** — support Discord and Slack through a shared core
5. **Native to schmux** — the bridge is a schmux session, not an external service

## Non-Goals

- Full terminal streaming to chat (too noisy, ANSI issues)
- Replacing the web dashboard (chat is for quick checks and team coordination)
- Supporting arbitrary chat platforms beyond Discord and Slack initially

## Design

### Key Insight: The Bridge Is a Session

Rather than building an external service, the chat bridge runs as a schmux session — a Claude Code agent in a tmux pane like everything else. This means:

- It shows up in the dashboard alongside other sessions
- It can be monitored, nudged, disposed, and restarted like any session
- It accesses other sessions through schmux's localhost API
- No new infrastructure to deploy or manage

### Architecture

```
Discord/Slack
     ↕
Thin adapter (webhook relay)
     ↕  HTTP POST (messages in/out)
schmux bridge session (Claude Code agent)
     ↕  localhost:7337
schmux API
     ↕
Other sessions (via capture, events, terminal WebSocket)
```

### Three Components

#### 1. Bridge Session (the brain)

A Claude Code session spawned by schmux with a system prompt and tools that give it:

- Access to the schmux API (session list, capture, events, branches, spawn, dispose, nudge)
- Knowledge of channel-to-session mappings
- Instructions for how to behave as a chat bridge (summarize, don't dump raw output, be concise)

The bridge agent handles all intelligence:
- Interpreting natural language requests ("what's the auth session doing?")
- Deciding whether to query status, relay to a session, or spawn something new
- Formatting responses appropriately for chat
- Triaging alerts (which state transitions are worth notifying about)

This could be implemented as:
- A Claude Code session with MCP tools wrapping the schmux API
- A Claude Code session with a `CLAUDE.md` that instructs it to use `curl` against localhost
- A custom agent binary that uses the Anthropic API directly (lighter weight, no Claude Code overhead)

#### 2. Thin Adapter (the relay)

A small process that bridges between chat platform APIs and the bridge session. Per platform:

| Concern | Discord | Slack |
|---|---|---|
| Incoming messages | Interactions / gateway | Events API / slash commands |
| Outgoing messages | REST API (embeds) | Web API (Block Kit) |
| Buttons/actions | Message components | Block Kit interactive |
| Threads | Thread replies | Thread replies |
| Auth | Bot token | Bot token + signing secret |

The adapter is intentionally dumb — it:
- Receives messages/commands from chat, posts them to the bridge session
- Receives responses from the bridge session, formats and posts them to chat
- Handles platform-specific formatting (Discord embeds vs Slack blocks)
- Manages WebSocket/gateway connections to chat platforms

The adapter does NOT interpret messages, make API calls to schmux, or contain business logic.

#### 3. Proactive Monitor (optional, in the bridge session)

The bridge session can optionally hold a WebSocket connection to `/ws/dashboard` and watch for state transitions worth alerting on:

- Session enters `Error` or `Needs Attention` nudge state
- Session completes a task
- All sessions go idle
- A session has been stuck in `Needs Input` for N minutes

Alerts are posted to a configured channel.

### Interaction Model

**Tier 1 — Commands (direct API mapping)**

```
User: /schmux status
Bot:  3 sessions running across 2 workspaces
      ├─ schmux/feature-auth: 2 sessions (1 working, 1 idle)
      └─ schmux/fix-tests: 1 session (working)

User: /schmux spawn schmux feature-auth claude
Bot:  Spawned session auth-3 on schmux/feature-auth

User: /schmux nudge auth-2
Bot:  Nudged auth-2 (was idle for 12 minutes)
```

**Tier 2 — Observation with synthesis**

```
User: What's the auth session working on?
Bot:  auth-1 is implementing OAuth token refresh. Recent activity:
      - Added refresh token rotation in internal/auth/refresh.go
      - Updated token storage to include expiry timestamps
      - Currently writing tests for the refresh flow

User: Give me a status report
Bot:  Status report (3 sessions):
      ✓ auth-1: Implementing OAuth refresh (active, 45 min)
      ⚠ auth-2: Idle since completing login flow (23 min)
      ● test-1: Running test suite (active, 5 min)

      Git: feature-auth is 7 ahead of main, pushed, clean
```

**Tier 3 — Relay to sessions**

```
User: Tell auth-2 to also add rate limiting to the refresh endpoint
Bot:  auth-2 is currently idle. Relaying your request...
      [posts to auth-2's terminal via WebSocket]
Bot:  auth-2 acknowledged — it's planning the rate limiting approach.
      I'll update you when it has something to show.
```

For tier 3, the bridge agent:
1. Checks the target session's `nudge_state`
2. If idle/waiting: injects the message via `/ws/terminal/{id}`
3. Monitors terminal output for a response (capture polling or WebSocket subscribe)
4. Summarizes the response and posts it back to chat
5. If the session is actively working: queues the request or warns the user

### Communication Between Bridge and Adapter

The adapter needs a way to send messages to and receive messages from the bridge session. Options:

**Option A: Terminal I/O via WebSocket**
- Adapter connects to bridge session's `/ws/terminal/{bridge-id}`
- Sends messages as terminal input, reads output
- Cheap but messy — need to parse terminal output for structured responses

**Option B: HTTP sidecar on the bridge session**
- Bridge session runs a small HTTP server (or the adapter does)
- Structured JSON request/response
- Cleaner but adds a process

**Option C: File-based message queue**
- Adapter writes incoming messages to a file in the bridge workspace
- Bridge session watches the file, processes messages, writes responses
- Bridge workspace acts as a mailbox
- Simple, debuggable, works with any agent type

**Option D: schmux API extension**
- Add a `/api/sessions/{id}/message` endpoint to schmux
- Adapter posts to this, bridge session reads from it
- Cleanest but requires schmux API changes

**Recommended: Option C or D.** Option C works today with no schmux changes. Option D is cleaner if we're willing to add a message queue to the API.

## Configuration

```json
{
  "chat_bridge": {
    "enabled": true,
    "platform": "discord",
    "bot_token": "...",
    "channels": {
      "status": "channel-id-for-status-updates",
      "alerts": "channel-id-for-alerts"
    },
    "session_channels": {
      "auth-*": "channel-id-for-auth-work",
      "test-*": "channel-id-for-testing"
    },
    "alerts": {
      "on_error": true,
      "on_needs_input": true,
      "on_completed": true,
      "idle_threshold_minutes": 30
    }
  }
}
```

This could live in `~/.schmux/config.json` alongside existing config, or in a separate `~/.schmux/chat-bridge.json`.

## Implementation Considerations

### Bridge Session Lifecycle

- schmux spawns the bridge session automatically when `chat_bridge.enabled` is true
- If the bridge session dies, schmux restarts it (like a supervised process)
- The bridge session appears in the dashboard with a distinct persona (e.g., a bridge icon)
- Disposing the bridge session disables chat integration until re-enabled

### Security

- Bot tokens stored in config (same security model as other schmux secrets)
- The bridge session has the same localhost API access as any session
- Chat users can only interact with sessions visible to the bridge
- Consider: role mapping (Discord roles → allowed schmux operations)

### Adapter Deployment

The thin adapter needs to be reachable by the chat platform (for webhooks) or maintain an outbound connection (for gateway/WebSocket). Options:

- Run on the same EC2 instance as schmux, expose a port or use ngrok
- Run as a lightweight cloud function that relays to schmux (requires schmux to be reachable)
- Run alongside schmux with a tunnel (cloudflared, tailscale, etc.)

### Cost

The bridge session is a Claude Code session, which means API costs for every interaction. For a team doing occasional status checks and relays, this is negligible. For high-traffic channels, consider:

- Rate limiting chat interactions
- Using a smaller/cheaper model for the bridge agent
- Caching status responses with a short TTL
- Making tier 1 commands (status, list) handled by the adapter directly without hitting the LLM

## Implementation Steps

### Phase 1: Proof of Concept

1. Manually spawn a Claude Code session with a system prompt describing the bridge role
2. Give it MCP tools or curl-based access to the schmux API
3. Write a ~150-line Discord bot that relays messages to/from this session (file-based: Option C)
4. Validate the interaction model works end-to-end

### Phase 2: Native Integration

1. Add `chat_bridge` config section to schmux config
2. Implement bridge session auto-spawn and supervision in the daemon
3. Build the thin adapter as a Go package (`internal/chatbridge/`)
4. Add Discord adapter (`internal/chatbridge/discord/`)
5. Add Slack adapter (`internal/chatbridge/slack/`)
6. Bridge persona in the dashboard

### Phase 3: Polish

1. Proactive monitoring (WebSocket-based alerts)
2. Channel-to-session mapping
3. Role-based access control
4. Tier 3 relay with response tracking
5. Rate limiting and cost controls

## Open Questions

1. **Bridge agent implementation**: Claude Code session with MCP tools vs. custom lightweight agent? MCP tools are more ergonomic but Claude Code has overhead. A custom agent using the Anthropic API directly would be cheaper for simple status queries.

2. **Adapter-to-bridge communication**: File-based queue (works today) vs. schmux API extension (cleaner long-term)? Could start with files and migrate.

3. **Multi-team support**: Should one schmux instance support multiple chat integrations (e.g., Discord for one team, Slack for another)? Or is one bridge per instance sufficient?

4. **Session-to-channel affinity**: Should sessions be mapped to specific channels, or should all interaction go through one channel? Threads could help — one thread per session.

5. **Cost model**: Should tier 1 commands bypass the LLM entirely and be handled by the adapter? This would reduce cost but split the logic between adapter and bridge.
