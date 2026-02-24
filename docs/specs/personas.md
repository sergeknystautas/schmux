# Personas: Behavioral Profiles for Agents

## Problem

When spawning agents in schmux, the only behavioral input is the task prompt — _what_ to do. There's no mechanism to shape _how_ the agent approaches work. A security review, a documentation pass, and a QA audit all require fundamentally different perspectives, but today each agent starts as a blank slate.

Users who want multi-perspective coverage (e.g., security + QA + docs review of the same codebase) must manually write detailed system prompt preambles every time, with no way to save, reuse, or visually distinguish these behavioral modes.

## Solution

A **Persona** is a named, reusable behavioral profile that shapes how an agent operates. It carries a system prompt (the behavioral instructions), output expectations (what kind of deliverable to produce), and a visual identity (icon + color) for dashboard recognition.

Personas are:

- **Global** — stored in `~/.schmux/personas/`, available across all projects
- **Orthogonal to model selection** — any persona can be attached to any model at spawn time
- **Optional** — agents can still spawn without a persona (current behavior)
- **Managed via dashboard** — a dedicated `/personas` page for CRUD operations

## Data Model

### Storage

Personas are stored as individual YAML files with frontmatter metadata and a body that _is_ the system prompt:

```
~/.schmux/personas/
├── security-auditor.yaml
├── qa-engineer.yaml
├── docs-writer.yaml
├── design-reviewer.yaml
└── technical-pm.yaml
```

### File Format

```yaml
---
id: security-auditor
name: Security Auditor
icon: "🔒"
color: "#e74c3c"
expectations: |
  Produce a structured report with severity ratings.
  Categorize findings as critical, high, medium, low.
  Suggest concrete fixes for each finding.
built_in: true
---

You are a security expert. Your primary focus is identifying vulnerabilities,
insecure patterns, and potential attack surfaces in code.

When reviewing code, systematically check for:
- Input validation and sanitization
- Authentication and authorization flaws
- Injection vulnerabilities (SQL, command, XSS)
- Sensitive data exposure
- Insecure dependencies

Always explain the risk of each finding and suggest a concrete fix.
```

The frontmatter carries metadata. The body after the closing `---` is the system prompt, written as natural prose. This makes personas easy to author in both a text editor and the dashboard UI.

### Schema

A persona consists of:

| Field          | Type   | Description                                                                           |
| -------------- | ------ | ------------------------------------------------------------------------------------- |
| `id`           | string | URL-safe slug, used as filename and API identifier                                    |
| `name`         | string | Human-readable display name                                                           |
| `icon`         | string | Emoji for visual identification                                                       |
| `color`        | string | Hex color for UI accents                                                              |
| `prompt`       | string | The system prompt (YAML body, not a frontmatter field)                                |
| `expectations` | string | Guidance on deliverable format (reports vs. code changes vs. both)                    |
| `built_in`     | bool   | Whether this persona ships with schmux. Enables "Reset to default" instead of delete. |

Built-in personas are embedded in the binary and written to disk on first run or when missing. Users can modify them freely.

## Built-in Personas

schmux ships with five personas:

| Persona          | Icon | Color     | Focus                                                                                              |
| ---------------- | ---- | --------- | -------------------------------------------------------------------------------------------------- |
| Security Auditor | 🔒   | `#e74c3c` | Vulnerabilities, OWASP top 10, input validation, attack surfaces, insecure dependencies            |
| QA Engineer      | 🧪   | `#2ecc71` | Edge cases, error handling, test coverage gaps, regression risks, boundary conditions              |
| Docs Writer      | 📝   | `#3498db` | Identify drift between documentation and code, fix stale docs, fill gaps, improve clarity          |
| Design Reviewer  | 🎨   | `#9b59b6` | Usability, UI consistency, accessibility, UX patterns, interaction design                          |
| Technical PM     | 📊   | `#f39c12` | Summarize project activity by analyzing commits over time periods, identify trends, track progress |

The Technical PM produces _reports_, not code changes. The Docs Writer cross-references documentation files against the actual codebase and flags inconsistencies.

## Prompt Delivery

The persona's system prompt is injected via the most authoritative mechanism each agent supports, separately from the user's task prompt:

| Agent  | Delivery Mechanism                                                                              |
| ------ | ----------------------------------------------------------------------------------------------- |
| Claude | `--append-system-prompt-file` (dedicated system prompt file, alongside existing signaling file) |
| Codex  | Appended to the `.codex/AGENTS.md` instruction file                                             |
| Gemini | Appended to the `.gemini/GEMINI.md` instruction file                                            |

The user's task prompt remains the final CLI argument — _what_ to do. The persona prompt shapes _how_ to do it. The two are never concatenated; they travel through separate channels.

## Spawn Behavior

- Spawning gains an optional persona selection.
- **Single agent mode**: a persona dropdown below the agent/model selector. Default: none.
- **Multi-agent / advanced mode**: each agent row gets its own persona dropdown, enabling multiple instances of the same model with different personas against the same task prompt.

## Session Display

- Each session carries its persona identity in state.
- Session cards show the persona icon as a badge next to the agent/model indicator.
- Session detail view shows the persona name with the persona's color as an accent.
- Persona name is visible on hover over the badge in list views.

## API

```
GET    /api/personas          — list all personas
GET    /api/personas/{id}     — get single persona
POST   /api/personas          — create persona
PUT    /api/personas/{id}     — update persona
DELETE /api/personas/{id}     — delete (or reset to default if built-in)
```

Spawn and session contracts gain a `persona_id` field.

## Dashboard: Persona Management (`/personas`)

A grid of persona cards, each showing:

- Icon and name
- Color accent bar on the left edge
- Preview of the system prompt (first ~2 lines)
- "Built-in" badge where applicable
- Edit / Delete actions (delete becomes "Reset to default" for built-ins)

Create/edit form: name, icon (emoji input), color (color picker), expectations (textarea), system prompt (large textarea).

Navigation: "Personas" appears in the sidebar, above overlays.

## Design Boundaries

These are explicitly **not** part of the personas design:

- **Per-project personas** — personas are global, not scoped to repositories
- **Persona-to-model binding** — model selection is an independent choice at spawn time
- **Team composition templates** — composing multiple personas into named teams is a separate feature that builds on top of personas
- **Import/export/sharing** — no marketplace or file exchange mechanism
- **Prompt templates** — personas do not suggest or constrain what task the user types
