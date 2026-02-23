# Spawn UI Redesign

Date: 2026-02-23

## Problem

The spawn UI is clumsy with many options. Three specific pain points:

1. **Agent selection has an unnecessary intermediary** — a mode selector dropdown (Single / Multiple / Advanced) before you can even pick an agent
2. **Repository takes its own row** — wastes space when it could share a row with the agent dropdown
3. **Slash commands require too many clicks** — `/command`, `/quick`, and `/resume` switch the form to a new mode that still requires clicking "Engage"

## Approach

Surgical refactor of `SpawnPage.tsx` rendering logic. Same state management (`modelSelectionMode`, `targetCounts`, `spawnMode`, draft persistence), new UI. Smallest diff, least risk.

## Design

### 1. Unified Agent Dropdown

**Current**: Mode selector dropdown (Single / Multiple / Advanced) → then a separate agent dropdown or grid.

**New**: A single `<select>` dropdown containing:

- All promptable agents (e.g., "Claude Code", "Codex", "Aider")
- Separator
- "Multiple agents"
- "Advanced"

**Single agent mode** (default): User picks an agent from the dropdown. Done.

**Transition to multi/advanced**: When user selects "Multiple agents" or "Advanced":

1. The dropdown disappears entirely
2. Replaced by the corresponding grid (toggle buttons for multi, +/- counters for advanced)
3. A **"Single agent" button** appears to go back (styled like other spawn form buttons)
4. Clicking "Single agent" collapses the grid back to the dropdown, keeping the first selected agent or clearing to default

**State**: `modelSelectionMode` (`'single' | 'multiple' | 'advanced'`) and `targetCounts` stay as-is. The mode selector `<select>` is removed; transitions happen via the unified dropdown options and the "Single agent" button.

### 2. Agent + Repo Same Row

**Current**: Agent selection and repo dropdown each take a full-width row.

**New**: Conditional layout based on agent selection mode.

**Single agent mode (fresh spawn)**:

```
[ Agent dropdown ▼ ]  [ Repository dropdown ▼ ]
```

Side-by-side in a flex row. Agent ~50-60% width, repo takes the rest.

**Multi/Advanced mode (fresh spawn)**:

```
[ Repository dropdown ▼ ]         ← full-width row
[Single agent]                     ← button to go back
[ agent grid / counters ]          ← grid below
```

Repo moves to its own full-width row when multi/advanced expands.

**Workspace mode**: No repo dropdown (already in a workspace). Agent selection takes full width. No layout change.

**"Create New Repository"**: When repo is `__new__`, the name input appears on its own row below, same as today.

### 3. Slash Commands Auto-Engage

**Current**: Selecting a slash command from autocomplete switches `spawnMode`, shows a new form state, user clicks "Engage."

**New**: Selecting a slash command from autocomplete immediately submits the form.

- **`/resume`** — submits immediately as a resume spawn with the currently selected agent
- **`/command_name`** (e.g., `/deploy`) — submits immediately as that command
- **`/quick <name>`** (e.g., `/quick dev:web`) — submits immediately as that quick launch preset

**Quick launch expansion**: The bare `/quick` entry is removed from autocomplete. Instead, all quick launch items are expanded as individual entries (`/quick dev:web`, `/quick test:unit`, etc.). Typing `/quick` filters to show them. Only available in workspace mode (same as today).

**Validation**: Existing form validation and toast errors apply as normal. E.g., `/resume` with no agent selected shows a toast error.

**Prompt text**: The `/` prefix text is removed from the textarea (same as today). Any other text in the prompt is discarded — no warning.

## Out of Scope

- Component extraction / refactoring of `SpawnPage.tsx` into smaller files
- Changes to the spawn API contract
- Changes to state management (draft persistence, waterfall defaults)
- Changes to branch suggestion or naming flow
- Changes to remote host spawning
