# Emerging Actions Design

## Problem

Quick Launch presets require users to predict their future needs upfront. This
is backwards — the system should learn what the user does repeatedly and
propose automations, the same way the lore system learns from agent failures
and proposes instruction file improvements.

## Core Idea

Extend the lore pipeline with a second curator mode that observes user intent
signals (spawn prompts, in-session prompts via Claude Code hooks) and proposes
reusable **actions**. Actions replace static Quick Launch presets with a unified
registry where emerged and manual actions live side by side, subject to the
same lifecycle: usage tracking, confidence scoring, and decay.

## Design Principles

- **User controls the toolbar.** The emerging system proposes, the user decides
  what appears in their dropdown. No algorithmic ranking at display time.
- **Actions are not just agent prompts.** They can be shell commands, interactive
  shells, or agent sessions. The existing QuickLaunch taxonomy (command vs.
  target+prompt) was already right.
- **Simple until proven otherwise.** No contextual scoring, no LLM calls at
  display time, no cross-repo sharing in v1.
- **Every decision is reversible.** Pin, dismiss, demote — no one-way doors.

## Architecture

### Extending Lore, Not Replacing It

The lore system is a general-purpose observe-curate-propose-apply engine. We
add a second curator mode rather than building a parallel system.

```
EXISTING LORE PIPELINE              NEW ACTION PIPELINE
──────────────────────              ───────────────────
Signal types:                       Signal types:
  failure, reflection, friction       status (with intent field)

Curator question:                   Curator question:
  "What rules should agents           "What does this user keep
   follow to avoid repeating           doing that could be
   these mistakes?"                    automated?"

Output:                             Output:
  Instruction file diffs              Action definitions

Trigger:                            Trigger:
  Session/workspace dispose           Threshold (N new intent signals)
                                      or manual

Shared infrastructure:
  - Event JSONL storage
  - Append-only state log
  - Proposal store (file-based)
  - Lore page review UI
  - WebSocket progress streaming
```

### Signal Capture

No new event types needed. The hooks system already captures user prompts:

1. **Spawn-time prompt** — Written as a `status` event with `intent` field
   when `session.Spawn()` is called (manager.go:714-725).

2. **In-session prompts** — Captured via the `UserPromptSubmit` Claude Code
   hook, which writes a `status` event with `intent` field to the session's
   event JSONL file (ensure/manager.go:443-453).

The lore system currently reads only `failure`, `reflection`, and `friction`
events from these files. The action curator reads `status` events with
non-empty `intent` fields.

### Action Curator

A second curator prompt mode in `internal/lore/curator.go`. Receives:

- All `status` events with `intent` fields (deduplicated by semantic
  similarity, not exact match)
- The current action registry (to avoid proposing duplicates)
- Spawn metadata: which target and persona were used with each prompt

Produces:

```json
{
  "proposed_actions": [
    {
      "name": "Fix lint errors",
      "template": "Fix all lint errors in {{path}}",
      "parameters": [{ "name": "path", "default": "src/", "source": "observed" }],
      "learned_defaults": {
        "target": { "value": "claude-code-sonnet", "confidence": 0.8 },
        "persona": { "value": "code-engineer", "confidence": 1.0 }
      },
      "evidence_keys": [
        "fix lint errors in src/",
        "fix all linting issues in src/components",
        "run the linter and fix everything in src/"
      ]
    }
  ],
  "entries_discarded": {
    "deploy to production": "one-time action, not a pattern"
  }
}
```

Key curator responsibilities:

- **Semantic clustering** of prompts (not exact match — "fix lint" and "run
  linter and fix" are the same intent)
- **Parameter extraction** — identify the variable parts of a prompt cluster
  and propose template parameters with observed defaults
- **Per-field confidence** — target and persona are conditional distributions
  given the prompt cluster, scored independently
- **Threshold judgment** — only propose when a pattern is strong enough
  (minimum N occurrences, semantic coherence)

### Composition Model

A task in schmux is composed of layers:

```
  agent harness (claude-code, codex, ...)
  model (sonnet, opus, ...)
  persona (code-engineer, architect, ...)
  prompt (the "what")
```

Emerging actions cluster on the **prompt** layer first. The other layers are
learned as conditional defaults — "when you do X, you usually use Y." Each
layer has independent confidence scoring:

- High confidence (>0.8): shown as the default, auto-selected on action use
- Medium confidence (0.5-0.8): shown as suggestion, user confirms
- Low confidence (<0.5): not shown, user picks manually

Model **version** is deliberately fuzzy — we track "sonnet" not "sonnet-4.5"
because versions are ephemeral.

### Action Registry

Replaces the `quick_launch[]` array in config. Single source of truth for all
actions, regardless of origin.

```
~/.schmux/actions/<repoName>/registry.json
```

```json
{
  "actions": [
    {
      "id": "a1b2c3",
      "name": "Fix lint errors",
      "type": "agent",
      "template": "Fix all lint errors in {{path}}",
      "parameters": [{ "name": "path", "default": "src/" }],
      "target": "claude-code-sonnet",
      "persona": "code-engineer",
      "scope": "repo",

      "source": "emerged",
      "confidence": 0.82,
      "evidence_count": 5,
      "first_seen": "2026-02-15T10:00:00Z",
      "last_used": "2026-02-25T14:30:00Z",
      "use_count": 4,
      "edit_count": 1,
      "state": "pinned",

      "proposed_at": "2026-02-20T12:00:00Z",
      "pinned_at": "2026-02-21T09:00:00Z"
    },
    {
      "id": "d4e5f6",
      "name": "Start dev server",
      "type": "command",
      "command": "npm run dev",
      "scope": "repo",

      "source": "manual",
      "confidence": 1.0,
      "evidence_count": 0,
      "first_seen": "2026-02-26T08:00:00Z",
      "last_used": null,
      "use_count": 0,
      "edit_count": 0,
      "state": "pinned",

      "proposed_at": null,
      "pinned_at": "2026-02-26T08:00:00Z"
    }
  ]
}
```

**Action types:**

| Type      | Fields                                | One-click behavior               |
| --------- | ------------------------------------- | -------------------------------- |
| `agent`   | target, persona, template, parameters | Spawns agent session             |
| `command` | command (shell string)                | Opens tmux session, runs command |
| `shell`   | (none beyond workspace)               | Opens interactive tmux session   |

**Source field** is metadata, not destiny:

| Source     | How it entered                         | Starting confidence  |
| ---------- | -------------------------------------- | -------------------- |
| `emerged`  | Curator proposed, user pinned          | From curator scoring |
| `manual`   | User created via "+ Add action"        | 1.0 (starts trusted) |
| `migrated` | One-time migration from quick_launch[] | 1.0                  |

**States:**

| State       | Meaning                              | Visible in dropdown |
| ----------- | ------------------------------------ | ------------------- |
| `proposed`  | Curator suggested, awaiting review   | No                  |
| `pinned`    | User accepted or manually created    | Yes                 |
| `dismissed` | User rejected or decayed from disuse | No                  |

### Promotion Lifecycle

```
  OBSERVE              PROPOSE              PIN                  DECAY
  ───────              ───────              ───                  ─────
  Intent signals       Action curator       User accepts         Unused actions
  accumulate in        clusters & extracts  proposal or          fade: confidence
  event JSONL files    action definitions   creates manually     drops, eventually
                                                                 auto-dismissed
  (silent)             (Lore page review)   (dropdown + spawn)   (silent)
```

**Usage tracking** — on every spawn, check if the prompt matches a pinned
action template:

- **Used as-is** → increment `use_count`, update `last_used`
- **Used but prompt edited** → increment `edit_count` (template may need
  refinement, fed back to next curation)
- **Not used for 30+ days** → decay confidence, eventually auto-dismiss

Manual actions decay too. If you create "deploy staging" and never use it,
it fades. No special treatment for manual vs. emerged.

### Usage Tracking at Spawn Time

When a session is spawned, the spawn handler checks the action registry for
a matching action (by template similarity or by explicit action ID if the
user clicked a pinned action in the dropdown). If matched:

- Increment `use_count`, set `last_used`
- If the user modified the prompt before spawning, increment `edit_count`
  and store the delta for the next curation cycle

This is cheap — a registry read + write on spawn, no LLM involved.

## UI Surfaces

### Sessions Page [+] Dropdown

The primary quick-access point. User-curated, instant, no computation.

```
┌───────────────────────────────────┐
│  Spawn a session...               │  ← primary, opens spawn page
│ ────────────────────────────────  │
│  Fix lint errors             ●●●○ │  ← pinned actions
│  Run test suite              ●●●● │
│  Start dev server            ▪    │
│  Open shell                  ▪    │
│ ────────────────────────────────  │
│  + Add action                     │
└───────────────────────────────────┘
```

- "Spawn a session..." is the star — bold, first item, opens full spawn page
- Pinned actions below are one-click direct spawn (no intermediate page)
- Agent actions with parameters use learned defaults
- Confidence dots (●) give visual sense of how established an action is
- `▪` marker for non-agent actions (commands, shells)
- "+ Add action" opens action editor for manual creation

### Spawn Page — Prompt Autocomplete

The spawn page gets autocomplete on the prompt textarea, backed by two data
sources:

1. **Pinned action templates** — clustered, parameterized (shown first)
2. **Raw prompt history** — exact past prompts from event files (shown second)

```
  Prompt:
  ┌─────────────────────────────────────────────┐
  │ fix li|                                     │
  ├─────────────────────────────────────────────┤
  │ ▸ fix lint errors in {{path}}       (×7)    │
  │ ▸ fix lint errors in src/           (Feb 25)│
  │ ▸ fix linting issues in components  (Feb 22)│
  └─────────────────────────────────────────────┘
```

Selecting a clustered template fills prompt AND applies learned target/persona
defaults (overridable). Selecting a raw past prompt fills prompt only.

Implementation: prefix + fuzzy matching, client-side. Prompt history loaded
via one API call on page mount. No LLM calls, no server round-trips for
matching.

### Lore Page — Actions Tab

Proposed actions appear as a new tab on the existing Lore page, using the
same review UX pattern as lore proposals:

```
  LORE    [Instructions] [Actions]  [Signals]

  PROPOSED ACTION
  ┌──────────────────────────────────────────────┐
  │ "Fix lint errors"                            │
  │                                              │
  │ Based on 5 similar prompts:                  │
  │   • "fix lint errors in src/"                │
  │   • "fix all linting issues in src/components│
  │   • "run the linter and fix everything"      │
  │   • "fix lint in src/utils"                  │
  │   • "fix eslint errors"                      │
  │                                              │
  │ Learned defaults:                            │
  │   target: sonnet (4/5 times)                 │
  │   persona: code-engineer (5/5 times)         │
  │   path param: "src/" (3/5 times)             │
  │                                              │
  │ [Pin]  [Dismiss]  [Edit & Pin]               │
  └──────────────────────────────────────────────┘
```

"Edit & Pin" allows refining name, template, or defaults before accepting.

## Migration

On first run after upgrade:

1. Read `quick_launch[]` entries from `config.json`
2. Write each as `source: "migrated"`, `state: "pinned"` into the action
   registry
3. Remove `quick_launch` key from config
4. Remove QuickLaunchTab from config UI

Reversible: keep the old config key for one version cycle.

## What's Deliberately Out of Scope

- **Cross-repo action sharing** — actions are per-repo. If "fix lint" emerges
  in 3 repos independently, that validates the pattern. Sharing can come later.
- **Contextual ranking** — the dropdown is user-curated, not algorithmically
  ordered. No LLM calls at display time.
- **Slash commands** — actions don't generate agent-agnostic slash commands
  yet. That's a future layer.
- **Auto-spawn** — the system never fires an action without user interaction.
- **Workflow sequence detection** — observing multi-step UI patterns requires
  client-side instrumentation. Start with prompt signals only.
