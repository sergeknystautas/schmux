# Repofeed: Unified Repo Activity Feed with Cross-Developer Federation

**Date**: 2026-03-07
**Status**: Design
**Branch**: `feature/repo-activity-federation`

## Problem

AI coding agents produce large changes fast. By the time two developers
discover their work overlaps — typically when code lands and creates merge
conflicts — resolving it is expensive. Developers need ambient awareness of
what others are working on _before_ code is committed.

Meanwhile, the existing subreddit system provides LLM-generated summaries of
landed commits. Since commits are shared via git, every developer's schmux
instance independently generates the same view — there is cross-developer
visibility for landed code, just redundant LLM work across instances.

The gap is on the **pre-commit side**: there is no visibility into what other
developers are actively working on before their code lands.

These are two sides of the same coin:

- **Intents** — what is being developed (pre-commit)
- **Landed** — what has finished development (post-commit)

Both deserve the same UX affordance. Neither should require any new action
from developers — the system must be fully automatic.

## Core Concept

The **repofeed** is a unified, per-repo activity feed that shows:

1. **In-progress intents** from other developers (federated via git)
2. **Landed code summaries** from the existing subreddit system (generated locally)

The dashboard presents both in a single chronological view with filter tabs.
Developers glance at it like an inbox. Coordination happens out of band
(Slack, etc.) — the repofeed provides awareness, not judgment.

## Design Principles

- **Zero friction**: Fully automatic. No new actions, no new habits. If it
  feels like a tax, adoption is zero.
- **Awareness, not judgment**: Shows what's happening. Humans decide if it
  matters. No automated overlap detection or alerts.
- **Git as transport**: No new dependencies. Intent federation uses a git
  orphan branch on the shared remote.
- **One feed, two lenses**: Intents and landed entries share one UI. Tabs
  filter by kind, but the default is unified.

## Architecture

```
┌────────────────────── YOUR SCHMUX INSTANCE ──────────────────────┐
│                                                                  │
│  PUBLISHING (automatic, daemon components)                       │
│  ─────────────────────────────────────────                       │
│                                                                  │
│  Sessions ──spawn──▶ Intent Publisher (new)                      │
│  (prompt)            │                                           │
│                      ├─ Summarize prompt (small LLM call)        │
│  Sessions ──dispose  ├─ Derive status from session lifecycle     │
│  /finalize──────────▶├─ Write <username>.json                    │
│                      └─ git push to dev-repofeed branch          │
│                                                                  │
│  Bare clones ───────▶ Subreddit Generator (existing)             │
│  (git log)            │                                          │
│                       └─ LLM-generated post summaries            │
│                          stored in ~/.schmux/subreddit/          │
│                                                                  │
│  CONSUMING (periodic, displayed on dashboard)                    │
│  ────────────────────────────────────────────                    │
│                                                                  │
│  git fetch ──────────▶ Repofeed Consumer (new)                   │
│  (dev-repofeed)        │                                         │
│                        ├─ Reads other devs' JSON files           │
│                        └─ Merges with local subreddit posts      │
│                           into unified feed                      │
│                                                                  │
│  Dashboard /repofeed ◀── Unified view                            │
│  CLI: schmux repofeed ◀─ (intents + landed, filterable)         │
│  FM: queries via CLI ◀──                                         │
└──────────────────────────────────────────────────────────────────┘
```

### Why Two Separate Publishers

**Intent publisher** federates information that is otherwise invisible — only
the developer's schmux instance knows what sessions are running and what
prompts were given. This _must_ be pushed to the git remote.

**Subreddit generator** summarizes commits that are already shared via git.
Every developer's instance can independently generate summaries from the same
commits. This does _not_ need federation — each instance generates its own
local view.

The repofeed consumer merges both sources for display.

## Data Model

### Git Branch: `dev-repofeed` (orphan)

An orphan branch on the shared remote. Each developer owns exactly one file.
Since no two developers write to the same file, merge conflicts are
structurally impossible.

```
dev-repofeed/
├── stefano@example.com.json
├── alice@example.com.json
└── bob@example.com.json
```

Filenames are derived from `git config user.email`, which is unique per
developer and consistent across machines.

#### Developer File Schema

```json
{
  "developer": "stefano@example.com",
  "display_name": "Stefano",
  "updated": "2026-03-07T14:32:00Z",
  "repos": {
    "schmux": {
      "activities": [
        {
          "id": "a1b2c3",
          "intent": "Refactoring session auth to support remote SSH flavors",
          "status": "active",
          "started": "2026-03-07T13:00:00Z",
          "branches": ["feature/remote-auth"],
          "session_count": 2,
          "agents": ["claude", "codex"]
        }
      ]
    },
    "other-project": {
      "activities": []
    }
  }
}
```

**Fields:**

| Field                        | Type     | Description                                                        |
| ---------------------------- | -------- | ------------------------------------------------------------------ |
| `developer`                  | string   | Developer's `git config user.email` (stable key, used as filename) |
| `display_name`               | string   | Developer's `git config user.name` (for dashboard display)         |
| `updated`                    | ISO 8601 | Last time this file was pushed                                     |
| `repos`                      | map      | Keyed by repo slug                                                 |
| `repos[].activities`         | array    | Active/recent intents for this repo                                |
| `activities[].id`            | string   | Stable ID (hash of prompt + timestamp)                             |
| `activities[].intent`        | string   | 1-2 sentence summary of what the developer is doing                |
| `activities[].status`        | enum     | `active` · `inactive` · `completed`                                |
| `activities[].started`       | ISO 8601 | When the first session for this activity was spawned               |
| `activities[].branches`      | string[] | Git branches associated with this work                             |
| `activities[].session_count` | int      | Number of schmux sessions working on this                          |
| `activities[].agents`        | string[] | Agent types in use (e.g., `["claude", "codex"]`)                   |

### Local Subreddit Data (unchanged)

The existing subreddit system continues to store posts in
`~/.schmux/subreddit/{repo-slug}.json` with the current schema (`Post` with
`id`, `title`, `content`, `upvotes`, `created_at`, etc.). No changes needed.

### API Endpoints

Two endpoints, split by scope:

#### `GET /api/repofeed` — Repo list with summary counts

```json
{
  "repos": [
    {
      "name": "schmux",
      "slug": "schmux",
      "active_intents": 2,
      "landed_count": 5
    },
    {
      "name": "other-project",
      "slug": "other-project",
      "active_intents": 0,
      "landed_count": 3
    }
  ],
  "last_fetch": "2026-03-07T14:30:00Z"
}
```

Lightweight. Used by the sidebar badge and home page repo summaries.

#### `GET /api/repofeed/{slug}` — Full feed for one repo

```json
{
  "name": "schmux",
  "slug": "schmux",
  "intents": [
    {
      "developer": "alice@example.com",
      "display_name": "Alice",
      "intent": "Refactoring config validation",
      "status": "active",
      "started": "2026-03-07T13:00:00Z",
      "branches": ["feature/remote-config"],
      "session_count": 2,
      "agents": ["claude", "codex"]
    }
  ],
  "landed": [
    {
      "id": "post-1709712000-1",
      "title": "New workspace switching UX",
      "content": "The dashboard now supports...",
      "upvotes": 3,
      "created_at": "2026-03-06T10:00:00Z"
    }
  ],
  "last_fetch": "2026-03-07T14:30:00Z"
}
```

Used by the `/repofeed` page when a repo tab is selected.

## Intent Publisher

### Component: `internal/repofeed/publisher.go`

A daemon component that implements `events.EventHandler`. Registered in the
event pipeline alongside the FM Injector and DashboardHandler.

**Listens for:**

| Event                       | Action                                                                             |
| --------------------------- | ---------------------------------------------------------------------------------- |
| Session spawn (with prompt) | Create new activity entry. LLM call to summarize prompt if longer than ~100 chars. |
| Session status change       | Update `session_count`. Derive `active`/`inactive` from live session state.        |
| Session dispose             | Decrement `session_count`. Mark `inactive` if no sessions remain.                  |
| Finalize signal             | Mark activity `completed`.                                                         |

**LLM summarization:**

Only triggered when the spawn prompt exceeds ~100 characters. Uses a minimal
system prompt:

```
Summarize this developer's task in 1-2 sentences. Focus on intent
(what they're trying to accomplish), not implementation details.
```

Short prompts (e.g., "fix the auth bug") are used verbatim. This keeps LLM
costs near zero — most prompts are already concise.

**Git publishing:**

Uses git plumbing commands to write directly to the orphan branch without
touching any workspace's working directory:

1. Build JSON content in memory
2. `git hash-object -w --stdin` → blob SHA
3. Build tree with all developer files (fetch existing tree first)
4. `git commit-tree` with parent from current `dev-repofeed` HEAD
5. `git update-ref refs/heads/dev-repofeed <commit>`
6. `git push origin dev-repofeed`

Publishes on every state change, debounced (e.g., 30s window to batch rapid
changes). Uses one of the workspace bare clones as the git context — the
publisher does not need its own clone.

**Orphan branch bootstrap:**

On first publish, if `dev-repofeed` doesn't exist on the remote:

1. Create orphan branch: `git checkout --orphan dev-repofeed`
2. Write initial `<username>.json`
3. Commit and push

This is a one-time operation per repo. Subsequent developers' first push
fetches the existing branch and adds their file.

### Activity Lifecycle

```
Session spawned with prompt
        │
        ▼
   ┌─────────┐     all sessions      ┌──────────┐
   │  active  │────disposed/stopped──▶│ inactive  │
   │          │◀────session resumed───│           │
   └────┬─────┘                       └─────┬─────┘
        │                                   │
        │         /finalize                 │  /finalize
        └──────────┬────────────────────────┘
                   ▼
             ┌───────────┐
             │ completed  │──▶ pruned after 48h
             └───────────┘
```

- **active**: At least one session in the activity's session list is running
- **inactive**: All sessions stopped or disposed; `updated` timestamp goes stale
- **completed**: `/finalize` was called; entry stays visible for 48h then pruned

No timers, no TTLs for active/inactive — purely derived from session state.
The 48h retention for completed entries lets other developers see "alice
finished the auth refactor" before it drops off.

## Repofeed Consumer

### Component: `internal/repofeed/consumer.go`

A background goroutine that periodically fetches the `dev-repofeed` branch
from each configured repo's remote.

**Fetch cycle** (every 60s, configurable):

1. For each repo with active workspaces:
   - `git fetch origin dev-repofeed` (against bare clone)
   - Read all `*.json` files from the fetched branch
   - Skip own file (don't show your own intents back to you)
   - Parse and store in memory
2. Merge with local subreddit posts (from `~/.schmux/subreddit/`)
3. Expose via `/api/repofeed`
4. Broadcast `repofeed_updated` via WebSocket

**No processing on the read side.** The consumer just fetches, parses, and
serves. No LLM calls, no overlap detection, no filtering. The dashboard
renders the raw feed.

## Dashboard UI

### Route: `/repofeed`

A new route in the dashboard, accessible from the sidebar with an unread badge.

```
┌─────────────────────────────────────────────────────┐
│  Repo Activity                                      │
│                                                     │
│  [repo-tab: schmux]  [repo-tab: other-project]      │
│                                                     │
│  Filter: [All] [In Progress] [Landed]               │
│                                                     │
│  ● alice · 2h ago · in progress                     │
│    Refactoring config validation to support          │
│    remote flavor schemas                             │
│    branches: feature/remote-config                   │
│                                                     │
│  ▲3 bob · 5h ago · landed                           │
│    Added E2E tests for workspace cloning             │
│    Covers the full spawn-to-dispose lifecycle with   │
│    Docker-based integration tests.                   │
│                                                     │
│  ● stefano · 6h ago · in progress                   │
│    Remote auth for session spawning                  │
│    branches: feature/remote-auth                     │
│                                                     │
│  ▲1 carol · 1d ago · landed                         │
│    Fixed dashboard WebSocket reconnection            │
│    The reconnect logic now preserves scroll...       │
│                                                     │
│  ○ dave · inactive 2d                               │
│    Migrating dashboard to new routing conventions    │
│    branches: refactor/dashboard-routes               │
└─────────────────────────────────────────────────────┘

●  active intent     ○  inactive intent     ▲N  landed (upvotes)
```

**Key behaviors:**

- **Tabs per repo**: Same pattern as existing subreddit UI
- **Filter chips**: All / In Progress / Landed — default is All
- **Chronological**: Most recent first, mixing both kinds
- **Visual distinction**: Intent entries show status dot (●/○) and branches;
  landed entries show upvote count (▲N) and expanded summary
- **Unread badge**: Sidebar shows count of new entries since last visit
  (stored in localStorage)
- **WebSocket update**: Listens for `repofeed_updated` to re-fetch

### Migration from Subreddit UI

The existing "r/schmux" card on the home page is replaced by a per-repo
activity summary integrated into each repo's workspace section.

### Home Page Integration

The repofeed surfaces contextually on the home page, alongside each repo's
workspaces. This is more useful than a separate widget because you see team
activity exactly where you're looking at your own work.

```
┌─────────────────────────────────────────────────┐
│  schmux                                         │
│  2 workspaces · 3 sessions                      │
│                                                 │
│  Also active:                                   │
│  ● alice · 2 sessions (claude, codex)           │
│    feature/remote-config                        │
│  ● bob · 1 session (claude)                     │
│    test/workspace-e2e                           │
│  ○ carol · inactive 1d                          │
│    refactor/dashboard-routes                    │
│  View full repofeed →                           │
│                                                 │
│  ├── workspace-001 (feature/remote-auth)        │
│  └── workspace-003 (main)                       │
│                                                 │
│  other-project                                  │
│  1 workspace · 1 session                        │
│  No other activity                              │
│  └── workspace-002 (feature/new-api)            │
└─────────────────────────────────────────────────┘
```

Per developer: status dot (●/○), name, session count, agent types, branch
names. "View full repofeed →" links to `/repofeed` filtered to that repo,
where you also see landed entries.

## CLI

### `schmux repofeed [--repo <slug>] [--kind intents|landed] [--json]`

Reads the same data as the API endpoint. Supports `--json` for FM consumption.

Default output (human-readable):

```
schmux (3 intents, 5 landed)

  ● alice (active, 2h ago)
    Refactoring config validation
    branches: feature/remote-config

  ▲3 bob (5h ago)
    Added E2E tests for workspace cloning

  ● you (active, 6h ago)
    Remote auth for session spawning
    branches: feature/remote-auth
```

The floor manager's CLAUDE.md is updated to include `repofeed` in its
available commands. When the operator asks "is anyone else working on
config?", the FM runs `schmux repofeed --json`, reads the output, and answers
conversationally.

## Configuration

Extends the existing config under a new `repofeed` key:

```json
{
  "repofeed": {
    "enabled": true,
    "publish_interval_seconds": 30,
    "fetch_interval_seconds": 60,
    "completed_retention_hours": 48,
    "repos": {
      "schmux": true,
      "other-project": false
    }
  }
}
```

| Field                       | Default    | Description                                |
| --------------------------- | ---------- | ------------------------------------------ |
| `enabled`                   | `false`    | Master toggle for the repofeed             |
| `publish_interval_seconds`  | `30`       | Debounce window for publishing changes     |
| `fetch_interval_seconds`    | `60`       | How often to fetch other developers' files |
| `completed_retention_hours` | `48`       | How long completed entries stay visible    |
| `repos`                     | all `true` | Per-repo toggle                            |

Developer identity is derived automatically from `git config user.email`
(stable key / filename) and `git config user.name` (display name). No manual
configuration needed.

The existing `subreddit` config remains unchanged. The subreddit generator
continues to run independently — the repofeed consumer simply reads its
output.

## Integration Points

### Subreddit (existing)

No changes to the subreddit package. The repofeed consumer reads subreddit
data from `~/.schmux/subreddit/{slug}.json` as a data source. The subreddit
continues to generate and store posts independently.

### Floor Manager

The FM gains `schmux repofeed` as an available command. No other changes. The
FM does not receive injected signals about the repofeed — it queries on
demand when the operator asks.

### NudgeNik

No integration. NudgeNik classifies individual session states. The repofeed
operates at the intent level (across sessions).

### /finalize

The `/finalize` command emits an event that the intent publisher listens for.
When finalize runs, the publisher marks the corresponding activity as
`completed`. No changes to `/finalize` itself — it just needs to emit a
recognizable event.

## Package Structure

```
internal/repofeed/
├── publisher.go      # EventHandler impl, LLM summary, git plumbing
├── consumer.go       # Periodic fetch, parse, merge with subreddit
├── types.go          # DeveloperFile, Activity, RepofeedEntry
├── git.go            # Orphan branch operations (plumbing commands)
└── publisher_test.go
└── consumer_test.go

internal/dashboard/
├── handlers_repofeed.go   # GET /api/repofeed, GET /api/repofeed/{slug}, WebSocket broadcast

assets/dashboard/src/
├── routes/RepofeedPage.tsx
├── hooks/useRepofeed.ts
```

## What This Design Is NOT

- **Not an alert system**: No push notifications, no interruptions. You look
  when you want to.
- **Not overlap detection**: No automated comparison of intents. Humans
  glance and decide.
- **Not a coordination protocol**: No replies, annotations, or approval
  gates. Intervention happens out of band (Slack, etc.).
- **Not a replacement for git**: Git remains the source of truth for code.
  The repofeed is metadata _about_ development activity.

## Resolved Design Decisions

1. **Identity**: Use `git config user.email` as the stable key (filename on
   the orphan branch). Use `git config user.name` as the display name in the
   JSON payload and dashboard. Email is unique per developer and consistent
   across machines.

2. **Multi-remote repos**: Always push to `origin`. Configurable per-repo if
   a team uses a non-standard remote layout.

3. **Branch cleanup**: No automated cleanup. Developer files on
   `dev-repofeed` are inert when stale (a few KB each). The dashboard hides
   entries with `updated` older than 30 days. Manual cleanup via
   `git rm <email>.json` on the orphan branch if ever needed.

4. **Push conflicts**: Fetch-rebase-push with retry (max 3 attempts). Since
   no two developers edit the same file, the rebase always auto-resolves.
   Standard optimistic concurrency — implementation detail, not a design
   choice.

5. **Home page integration**: Per-repo activity summary shown alongside each
   repo's workspaces on the home page. Per developer: status dot, name,
   session count, agent types, branch names. "View full repofeed →" links to
   `/repofeed` filtered to that repo. The full `/repofeed` page shows both
   intents and landed entries with filter tabs.
