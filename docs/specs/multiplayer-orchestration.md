# Chaplin: Work Coordination for Multi-Operator Software Factories

**chaplin** is the coordination layer for schmux — the little tramp who navigates the chaos of the factory floor, keeping things moving despite the madness.

## Problem

schmux multiplies individual productivity — one person can drive many agents simultaneously. But when two people operate against the same codebase, without coordination they risk merge conflicts, duplicated effort, and stale context. Like cars on a road: individually fast, collectively gridlocked without traffic management.

The goal is smooth work flow between two or more peers, reducing alignment friction without introducing rigid process.

## Design Principles

- **AI-native**: Information is written in English, consumed by both humans and LLM agents. Only model rigidly what software needs to operate (IDs, permissions, state enums). Everything else is freeform text.
- **Symmetric peers**: No hierarchy. Both operators have equal power and reach.
- **Intent over code**: Agents write code. Humans coordinate on goals, requirements, and constraints. The system facilitates high-level alignment review.
- **Influence through conversation, not control**: Operators interact through chat and observation, not by taking over each other's sessions.
- **YAGNI**: No workflow engine, no approval gates, no automated task decomposition, no multi-project dependencies.

## Architecture

Three components:

```
schmux-A  <──WebSocket──>  chaplin  <──WebSocket──>  schmux-B
   |                          |                          |
Dashboard-A            Read-only Web UI            Dashboard-B
```

### chaplin (new service)

A standalone lightweight service deployable to the cloud or co-located with schmux.

Responsibilities:

- Store projects, activities, and users
- Sync state in real-time via WebSocket to connected schmux instances
- Relay terminal streams on-demand (lazy pull)
- Route intervention requests between instances
- Optional read-only web UI for observers
- Persist to SQLite (single-file, no external DB dependency)

### schmux (extended)

The existing agent orchestrator, extended with:

- A coordination client that connects to chaplin
- User identity (who is operating this instance)
- Every workspace maps to an activity, every activity to a project
- Publishes session/workspace metadata and git stats to chaplin
- Serves terminal streams to chaplin on request (lazy pull)
- Uploads activity recordings when archival is enabled (push)

### Dashboard (extended)

- Sidebar becomes the floor view: projects, activities, status, blockers across all visible users
- Main pane becomes a per-project group chat: humans + floor manager agents
- Drill-down from activity to linked sessions to terminal view (if execution sharing is on)

## Deployment Scenarios

**Scenario 1 — Centralized**: One schmux + chaplin on a cloud host. Multiple users access via browser. Shared agent credentials and token costs. Requires user authentication (schmux already has GitHub OAuth, extended to differentiate users).

**Scenario 2 — Federated**: Each person runs schmux locally with their own credentials. chaplin is deployed separately in the cloud. Each schmux instance connects to it.

Scenario 2 is the general case. Scenario 1 is Scenario 2 with both services co-located and auth added.

## Data Model

Only three structured enums. Everything else is freeform text.

### Project

```
id
owner_id
visibility: private | read-only | read-write
share_execution: bool
archive_recordings: bool
content: text    # name, goals, motivation, constraints — freeform
created_at
updated_at
```

### Activity

```
id
project_id
owner_id
status: active | blocked | completed | abandoned
content: text    # description, plan, blockers, approach — freeform
links: [workspace_ids, session_ids, branch_names]
created_at
updated_at
```

### User

```
id
name
instance_id
connected: bool
last_seen_at
```

**Key mapping**: Every schmux workspace is an activity. Every activity belongs to a project. No orphan sessions.

### Permissions

Per-project, set by owner:

- **Private**: Only the owner sees the project and its activities.
- **Read-only**: Others can see intent, plan, status, blockers. If `share_execution` is on, they can also view session metadata and live terminal output.
- **Read-write**: Others can also spawn new sessions into the project's activities.

Privacy is split into two layers:

- **Intent & plan** (lightweight, low-bandwidth): shared whenever visibility is read-only or read-write.
- **Execution** (session details, terminal streams): opt-in via `share_execution` toggle. Can be changed at any time.

## Coordination Protocol

### 1. State Sync (always on)

Persistent WebSocket between each schmux instance and chaplin. Small JSON messages for project/activity CRUD and deltas.

```
-> project.create { content, visibility, share_execution }
-> project.update { id, content?, visibility?, share_execution? }
-> activity.create { project_id, content, links }
-> activity.update { id, content?, status?, links? }
<- sync { projects, activities }         # full state on reconnect
<- delta { entity, id, changes }         # incremental updates
```

### 2. Execution Visibility (lazy pull, on-demand)

Terminal streams are NOT continuously synced. When User A clicks into User B's session:

```
-> execution.request { activity_id, session_id }
<- execution.metadata { session_id, target, status, workspace, branch, git_stats }

-> terminal.subscribe { session_id }
<- terminal.stream { session_id, data }  # continuous while subscribed
-> terminal.unsubscribe { session_id }
```

chaplin relays between instances. Stream exists only while someone is watching. Zero cost otherwise.

### 3. Intervention (read-write projects only)

Spawning a new session into a shared activity:

```
-> session.spawn { activity_id, target, prompt }
<- session.spawned { session_id, workspace_id }
```

chaplin forwards the request to the schmux instance that owns the workspace. No direct terminal takeover — influence happens through the group chat.

## Activity Recordings (Flight Recorder)

Two modalities for reviewing completed activities:

**Push (opt-in archival):** When `archive_recordings` is enabled on a project, completed activities upload a recording bundle to chaplin:

- Terminal recordings (asciinema-style: character stream + timestamps, compresses well)
- Agent prompts and human inputs
- Git snapshots (branch state at start/end, diff stats)
- Activity content as it evolved

**Pull (always available):** When the source schmux instance is online, another user can request recordings on-demand through chaplin. No central storage needed.

| Scenario                                 | Modality                |
| ---------------------------------------- | ----------------------- |
| Colleague offline, review their work     | Push (must be archived) |
| Colleague online, peek at recent work    | Pull                    |
| Long-term archival / onboarding          | Push                    |
| Privacy-sensitive, owner controls access | Pull (stays local)      |

Retention policy: configurable per project. Auto-expire after N days, or mark specific activities as "worth preserving."

## Floor Manager Agent

Each user can run a floor manager agent per project. It participates in the project's group chat.

**Capabilities:**

- Answers questions about its user's work (status, progress, what happened)
- Detects overlaps between activities (comparing branch diffs across activities for file-level conflicts)
- Surfaces blockers proactively
- Suggests plans and work breakdowns
- Can spawn sessions on behalf of its user when instructed through chat

**Three operating modes (per-user, configurable):**

| Mode          | Behavior                                                                                                       | Token cost                     |
| ------------- | -------------------------------------------------------------------------------------------------------------- | ------------------------------ |
| **Silent**    | Disabled. Zero cost. Manual coordination only.                                                                 | None                           |
| **On-demand** | Activates when @ mentioned or asked a question.                                                                | Low — only when invoked        |
| **Proactive** | Watches for high-signal events (overlaps, blockers, completions) and chimes in. Configurable polling interval. | Higher — continuous monitoring |

The floor manager's context is: project content, activity content, session metadata, git stats, and terminal output (for its user's sessions only).

## User Experience

### Sidebar (Floor View)

All visible projects, grouped by owner. Each project shows:

- Name (from content)
- Its activities with status indicators (active/blocked/completed)
- Blocker summaries
- Whether they need something from you

### Main Pane (Per-Project Group Chat)

When you select a project, the main pane shows a group chat:

- Both humans can write messages
- Both floor managers participate (based on their mode)
- Messages can reference activities, sessions, branches
- Coordination happens conversationally — intent alignment, blocker resolution, work partitioning

### Activity Detail

Clicking an activity shows:

- Its freeform content (intent, plan)
- Linked sessions and their status
- Git stats (files changed, diff size, ahead/behind)
- If execution sharing is on: live terminal view (lazy pull)
- If archived: playback of completed session recordings

### Overlap Detection

The system compares branch diffs across activities and surfaces warnings:

- "Activity X and Activity Y both modify `internal/session/manager.go`"
- Shown in the floor view sidebar and mentioned by the floor manager in chat (if proactive)

## What This Does NOT Include

- No session takeover (influence through chat, not terminal control)
- No automated task decomposition or assignment
- No workflow engine or approval gates
- No multi-project dependencies
- No persistent chat history beyond chaplin's storage
- No role-based access beyond owner + visibility
- No CRDTs or offline-first — assumes instances are online
