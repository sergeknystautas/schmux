# Emergence System Design

## Problem

Users encode procedural knowledge in ad-hoc prompts — quality criteria, ordering
constraints, things to watch out for. They write a 2-paragraph prompt once, forget
half of it next time, write a slightly different version. The system that observes
all variants can synthesize a better procedure than any single prompt.

The current action system attempted this but never gelled. This design replaces it
with a cleaner model: schmux as the **emergence engine** that feeds agents' native
skill systems.

## Core Idea

Schmux observes user prompts across sessions, clusters semantically similar ones,
distills them into reusable skills (procedure + quality criteria), and writes them
into agents' native skill/command systems. The user interacts with emerged skills
through the agent's existing UX — `/commands` in Claude Code, `/commands` in
opencode — not through a parallel schmux-specific system.

The spawn dropdown provides a quick-launch surface for starting sessions with
specific skill intent.

## Design Principles

- **Emergence, not configuration.** The system proposes skills from observed
  behavior. Users review and pin, not define from scratch (though manual creation
  is available for simple cases).
- **Agent-native runtime.** Emerged skills flow into agents' native skill systems.
  Schmux doesn't own an invocation mechanism — agents do.
- **Invisible to git.** Emerged skills are injected into workspaces via agents'
  native skill paths and hidden from git via `.git/info/exclude`. The user never
  sees them in `git status`.
- **Graduated richness.** Shell commands, simple prompt actions, emerged skills,
  and built-in skills coexist in the spawn dropdown. No user needs to understand
  the taxonomy.

## Architecture

### Schmux Is the Emergence Engine, Agents Are the Runtime

```
SCHMUX'S JOB:                          AGENT'S JOB:
  Observe prompts                        Discover available skills
  Cluster patterns                       Match user request to skill
  Distill into skills                    Read and follow procedure
  Propose to user                        Native /command invocation
  Write to agent's native format
  Track lifecycle (internally)
```

Schmux doesn't own a parallel invocation mechanism. It feeds emerged knowledge
into the systems the user already interacts with. The fact that a skill was emerged
by schmux is invisible at invocation time.

### Skill Sources

```
BUILT-IN                    EMERGED                   MANUAL
────────                    ───────                   ──────
Ship with schmux            Observed from prompts     User creates
e.g., /commit               Proposed → user pins      Just name + prompt
Default: enabled             Default: proposed         Default: pinned
User can disable             User pins/dismisses
in config
```

All three coexist in the spawn dropdown with visual differentiation but no
functional hierarchy.

### Storage

```
~/.schmux/
  emergence/
    <repo>/
      spawn-entries.json     ← what shows in the spawn dropdown
      metadata.json          ← emergence tracking (evidence, confidence, decay)

<workspace>/
  .claude/skills/
    schmux-commit/
      SKILL.md               ← built-in skill, written by schmux
    schmux-code-review/
      SKILL.md               ← emerged skill, written by schmux

  .opencode/commands/
    commit.md                ← built-in skill for opencode
    code-review.md           ← emerged skill for opencode
```

- **Skill content** lives in agents' native skill locations within each workspace.
  For Claude Code: `.claude/skills/<name>/SKILL.md`. For opencode:
  `.opencode/commands/<name>.md`. Both are workspace-level.
- **Invisible to git.** Schmux manages `.git/info/exclude` entries to hide
  injected skill files (already does this for other workspace files). The user
  never sees them in `git status`.
- **Written at workspace setup.** Adapters write skills at spawn time, ensuring
  every workspace for a repo gets the same set of pinned skills. Skills are
  also written/removed when the user pins or dismisses proposals.

- **Spawn entries** are the schmux-owned registry of what appears in the spawn
  dropdown. Lightweight: name, type, reference, lifecycle state.

- **Emergence metadata** tracks evidence, confidence, decay. Internal to schmux,
  not user-facing.

### Spawn Entries

The spawn dropdown reads from `spawn-entries.json`:

```json
[
  {
    "id": "b1",
    "name": "Commit workflow",
    "type": "skill",
    "skill_ref": "schmux-commit",
    "source": "built-in",
    "state": "pinned"
  },
  {
    "id": "e1",
    "name": "Deep code review",
    "type": "skill",
    "skill_ref": "schmux-code-review",
    "source": "emerged",
    "state": "pinned",
    "confidence": 0.85
  },
  {
    "id": "m1",
    "name": "Start dev server",
    "type": "command",
    "command": "npm run dev",
    "source": "manual",
    "state": "pinned"
  },
  {
    "id": "m2",
    "name": "Run full test suite",
    "type": "agent",
    "prompt": "Run all tests, fix any failures",
    "target": "claude-code-sonnet",
    "source": "manual",
    "state": "pinned"
  }
]
```

Entry types:

| Type      | Fields                    | Spawn behavior                        |
| --------- | ------------------------- | ------------------------------------- |
| `skill`   | skill_ref                 | Spawns agent; skill via native system |
| `command` | command (shell string)    | Opens tmux, runs command              |
| `agent`   | prompt, target (optional) | Spawns agent with prompt              |
| `shell`   | (none)                    | Opens interactive tmux session        |

### Agent Injection Adapters

Each agent adapter implements skill injection into its native format:

```
AGENT          INJECTION PATH                         DISCOVERY
─────          ──────────────                         ─────────
Claude Code    .claude/skills/<name>/SKILL.md          /slash autocomplete
               (workspace-level, gitignored)           native skill matching

opencode       .opencode/commands/<name>.md            /slash autocomplete
               (workspace-level, gitignored)

Fallback       Catalog in SCHMUX:BEGIN/END block       Agent reads instruction
               of instruction file                     file, best-effort match
```

All agents use workspace-level injection with `.git/info/exclude` management.
This keeps the pattern consistent across agents and naturally scopes skills to
the repo they emerged from.

The adapter interface extension:

```go
type Adapter interface {
    // ...existing methods...
    InjectSkill(workspacePath string, skill SkillModule) error
    RemoveSkill(workspacePath string, skillName string) error
}
```

Claude Code adapter writes to `.claude/skills/` in the workspace.
opencode adapter writes to `.opencode/commands/` in the workspace.
Both are gitignored via `.git/info/exclude` (schmux already manages this).
Fallback adapter adds a catalog entry to the SCHMUX:BEGIN/END block.

### Built-in Skills

Built-in skills ship embedded in the schmux binary (`//go:embed`). At workspace
setup, adapters write them to agents' native skill locations (unless disabled in
config).

```json
// Config
{
  "built_in_skills": {
    "commit": true,
    "review": false
  }
}
```

Built-in skill files carry a source marker in frontmatter so schmux can:

- Update them when schmux upgrades
- Remove them when disabled
- Distinguish them from user-created skills

## Emergence Pipeline

### Signal Capture

No new event types needed. The hooks system already captures user prompts as
`status` events with `intent` fields:

1. **Spawn-time prompt** — written when `session.Spawn()` is called
2. **In-session prompts** — captured via `UserPromptSubmit` Claude Code hook

### Trigger

Emergence runs on session dispose (same as lore), with thresholds:

- **Minimum 3** semantically similar intent signals
- From at least **2 different sessions** (not repeated prompts in one session)
- Spread across at least **2 different days** (not a single-afternoon burst)
- Manual trigger always available from dashboard

Only runs if new intent signals have accumulated since last curation.

### Pipeline

```
PHASE 1: COLLECT
  Read ALL intent signals for repo from event JSONL.
  Not just new ones — full re-cluster each pass.

PHASE 2: CLUSTER + DISTILL
  Single LLM call receives:
    - All intent signals (deduplicated)
    - All existing pinned skills (full content)
    - Repo context (name, language, key tools)

  LLM produces:
    - New skill proposals (clusters not covered by existing skills)
    - Updated skill proposals (existing skills with meaningfully better content)
    - Discarded signals (one-off, not patterns)

PHASE 3: PROPOSE
  New proposals → state: "proposed" in spawn entries
  Updated proposals → shown as "update available" with diff
  Skill files written to staging location until user approves

PHASE 4: PIN (user action)
  User reviews on Lore page → Pin / Dismiss / Edit & Pin
  On pin: skill file written to agent's native location via adapter
  On dismiss: proposal removed
```

### Curator Prompt Design

The curator receives all signals and existing skills in one call. Key instructions:

- **Procedure** is the union of steps across all prompt variants — not the
  intersection. If one prompt mentioned "check for security" and another didn't,
  include it.
- **Quality criteria** are concerns/constraints the user expressed. These encode
  tacit knowledge — the most valuable part.
- **Parameters** are variable parts across prompts ("fix lint in `src/`" vs
  "fix lint in `components/`" → parameter `path`).
- **Triggers** are short phrases for matching. Used for spawn autocomplete and
  agent-native skill discovery.

Curator output schema:

```json
{
  "new_skills": [
    {
      "name": "code-review",
      "description": "Deep code review with security and test focus",
      "triggers": ["review", "code review", "review this PR"],
      "parameters": [{ "name": "scope", "description": "What to review", "default": "current PR" }],
      "procedure": "1. Read PR description...\n2. Check security...",
      "quality_criteria": "- No empty test assertions\n- Error handling covers unhappy path",
      "evidence": ["prompt 1 text", "prompt 3 text"],
      "confidence": 0.85
    }
  ],
  "updated_skills": [
    {
      "name": "code-review",
      "full_content": "...",
      "changes": "Added N+1 query check criterion",
      "new_evidence": ["prompt 12 text"]
    }
  ],
  "discarded_signals": {
    "deploy to staging": "one-time action, not recurring"
  }
}
```

### Module Evolution

Existing skills evolve through periodic re-emergence:

1. Each curation pass re-clusters ALL signals (not just new ones)
2. LLM is aware of existing pinned skills
3. If evidence supports meaningfully different/better content, LLM proposes
   a replacement
4. User sees the diff ("added N+1 query check, rephrased step 3")
5. "Accept update" replaces the skill file, preserves lifecycle stats
6. "Keep current" dismisses the update proposal

### Graduation Path

Manual agentic actions can naturally graduate to emerged skills:

```
Manual action: "Run all tests, fix any failures"
    ↓ (user keeps adding detail in actual prompts)
"Run all tests, fix failures, make sure to check coverage"
"Run tests, fix failures, don't add empty assertions"
    ↓ (emergence detects pattern with richer knowledge)
Proposed skill: test-fix (with procedure, criteria)
    ↓ (user pins)
Emerged skill replaces or supplements the manual action
```

No user action required for this graduation — the emergence system observes and
proposes naturally.

## UI Surfaces

### Spawn Dropdown (Sessions Page)

```
┌───────────────────────────────────────┐
│  Spawn a session...                    │  ← opens full spawn page
│ ─────────────────────────────────────  │
│  ◉ Deep code review                    │  ← emerged, most used
│  ■ Commit workflow                     │  ← built-in
│  ○ Run full test suite                 │  ← manual
│  ◉ Fix lint errors                     │  ← emerged
│  ■ Code review                         │  ← built-in
│  ○ Start dev server                    │  ← manual, least used
│ ─────────────────────────────────────  │
│  + Create action                       │
└───────────────────────────────────────┘

  ■ = built-in    ◉ = emerged    ○ = manual
  Sorted by usage frequency (most used first).
```

### Lore Page — Actions Tab

Proposed skills surface for review:

```
LORE    [Instructions] [Actions]

PROPOSED SKILL                                          NEW
┌──────────────────────────────────────────────────────────┐
│ "Deep code review"                       confidence: 85% │
│                                                          │
│ Emerged from 7 similar prompts:                          │
│   • "review this PR carefully, check security..."        │
│   • "code review the changes, make sure tests..."        │
│                                                          │
│ Procedure:                                               │
│   1. Read PR description and understand intent           │
│   2. Check each changed file for security issues         │
│   3. Verify test coverage for new code paths             │
│                                                          │
│ [Pin]  [Dismiss]  [Edit & Pin]                           │
└──────────────────────────────────────────────────────────┘

UPDATE AVAILABLE                                     UPDATE
┌──────────────────────────────────────────────────────────┐
│ "Deep code review" (pinned Feb 28)                       │
│                                                          │
│ New evidence suggests adding:                            │
│   + Check for N+1 query patterns                         │
│   + Verify error messages are user-facing                │
│                                                          │
│ [View full diff]  [Accept update]  [Keep current]        │
└──────────────────────────────────────────────────────────┘
```

### Spawn Page — Autocomplete

```
Prompt:
┌─────────────────────────────────────────────┐
│ review|                                     │
├─────────────────────────────────────────────┤
│ ▸ Deep code review           (skill, ×12)   │
│ ▸ review the auth changes    (history, Mar 5)│
└─────────────────────────────────────────────┘
```

### Manual Action Creation

```
+ Create action →

  ┌─────────────────────────────────────────┐
  │ Name: ___________________________       │
  │                                         │
  │ Type:  ○ Shell command                  │
  │        ● Agent session                  │
  │                                         │
  │ Prompt: _________________________       │
  │ Target: [claude-code-sonnet ▾]          │
  │                                         │
  │ [Save]                                  │
  └─────────────────────────────────────────┘
```

## Built-in to Emerged Pipeline (Dev Mode)

When developing schmux, emerged skills that prove universally useful can be
promoted to built-ins:

1. Skill emerges from real usage across repos
2. Developer reviews: "this is universally useful"
3. Skill content added to schmux source (`//go:embed`)
4. Added to built-in skill list
5. Ships with next release

The emergence system dog-foods the built-in skill pipeline.

## Migration from Current Actions System

The current action registry (`~/.schmux/actions/`) is replaced:

1. Migrated/manual actions become spawn entries (type: `agent` or `command`)
2. Any emerged actions with skill-level content become skill files
3. Old registry format deprecated
4. Quick Launch config entries already migrated to actions — they carry forward
   as spawn entries

## What's Deliberately Out of Scope

- **Cross-repo skill sharing** — skills are per-repo. If the same pattern emerges
  independently in multiple repos, that validates it. Sharing is a future concern.
- **In-session skill invocation by schmux** — agents handle in-session discovery
  through their native skill systems. Schmux doesn't inject mid-session.
- **Contextual skill ranking** — the spawn dropdown is ranked by usage frequency,
  not by contextual signals (time of day, current branch, recent activity). Simple
  frequency is the v1 ranking signal.
- **Auto-spawn** — the system never fires a skill without user interaction.
- **Skill composition** — skills are standalone units. Composing multiple skills
  into workflows is a future concern.
