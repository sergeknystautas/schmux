# Design: Collapsed Persona Dropdown

**Date:** 2026-02-25
**Status:** Approved

## Goal

Collapse the persona dropdown into the same row as Agent (and Repo when applicable), removing redundant labels for a cleaner, more compact spawn form.

## Current State

- Spawn form uses a 2-column grid layout (label | value)
- In "single agent + fresh mode": Agent and Repo are side-by-side with "Agent" label on left
- Persona appears on its own row below the agent section with "Persona" label on left

## Proposed State

Remove labels entirely. Dropdowns appear in a single flex row with equal widths.

### Layout Matrix

| Scenario                 | Personas? | Layout                                    |
| ------------------------ | --------- | ----------------------------------------- |
| Fresh + Single           | Yes       | `[Agent] [Persona] [Repo]` (33/33/33)     |
| Fresh + Single           | No        | `[Agent] [Repo]` (50/50)                  |
| Workspace + Single       | Yes       | `[Agent] [Persona]` (50/50)               |
| Workspace + Single       | No        | `[Agent]` (100%)                          |
| Remote (no provisioning) | Yes       | `[Agent] [Persona]` (50/50)               |
| Remote (no provisioning) | No        | `[Agent]` (100%)                          |
| Multiple/Advanced        | Yes       | Repo row → Agent grid → Persona row below |
| Multiple/Advanced        | No        | Repo row → Agent grid (no persona row)    |
| Branch input appears     | —         | Full-width row below dropdowns            |

### Visual Examples

**Fresh + Single + Personas:**

```
[Agent ▼]      [Persona ▼]      [Repo ▼]
```

**Fresh + Single + No Personas:**

```
[Agent ▼]                        [Repo ▼]
```

**Workspace + Single + Personas:**

```
[Agent ▼]      [Persona ▼]
```

**Branch input (when shown):**

```
[Agent ▼]      [Persona ▼]      [Repo ▼]
[Branch input - full width ─────────────]
```

**Multiple/Advanced mode:**

```
[Repo ▼]
┌──────────────────────────────────────┐
│ [Single agent] button                │
│                                      │
│ [Agent 1] [Agent 2] [Agent 3] ...    │  ← agent grid
└──────────────────────────────────────┘
[Persona ▼]
```

## Technical Approach

1. Replace 2-column grid with flexbox row for the dropdowns
2. Use `flex: 1` for equal-width columns
3. Conditionally render persona dropdown based on `personas.length > 0`
4. Keep branch input as full-width row below when needed
5. Multiple/Advanced modes keep persona below the agent grid

## Files to Modify

- `assets/dashboard/src/routes/SpawnPage.tsx` — restructure dropdown layout
