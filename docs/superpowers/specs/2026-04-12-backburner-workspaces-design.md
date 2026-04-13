# Backburner Workspaces

## Problem

As the number of workspaces grows, the sidebar and home page become visually taxing. There's no way to deprioritize workspaces you're not actively focused on without disposing them.

## Solution

A per-workspace "backburner" toggle that dims and sorts backburnered workspaces to the bottom of all workspace lists. Purely visual — backburnered workspaces remain fully functional.

## Feature Gate

Backburner is an experimental feature, disabled by default. Enabled via the Experimental tab in the config UI as a simple checkbox toggle (no config panel). When disabled, all backburner UI and sorting behavior is hidden; existing `backburner` values in state.json persist but are inert.

### Config UI

New entry in `experimentalRegistry.ts`:

- **id**: `backburner`
- **name**: Backburner
- **description**: "Dim and sort workspaces you want to set aside"
- **enabledKey**: `backburnerEnabled`
- **configPanel**: null (no extra configuration)

Auto-saves via the existing `useAutoSave` debounce pipeline (300ms → `POST /api/config`).

## Data Model

### State (`internal/state/state.go`)

Add to `Workspace` struct:

```go
Backburner bool `json:"backburner,omitempty"`
```

Persisted in `~/.schmux/state.json`. Defaults to `false`.

### Config (`internal/config/config.go`)

Add to `Config` struct:

```go
BackburnerEnabled bool `json:"backburner_enabled,omitempty"`
```

### API Contracts (`internal/api/contracts/`)

Add to `ConfigUpdateRequest`:

```go
BackburnerEnabled *bool `json:"backburner_enabled,omitempty"`
```

### TypeScript Types

Add to `WorkspaceResponse` in `assets/dashboard/src/lib/types.ts`:

```typescript
backburner?: boolean;
```

Add `backburnerEnabled` to `ConfigFormState` and `buildConfigUpdate`.

## API

### Toggle Backburner

```
POST /api/workspaces/{id}/backburner
Content-Type: application/json

{ "backburner": true }
```

- Sets `Backburner` on the workspace in state.json
- Broadcasts updated workspace state via dashboard WebSocket
- Returns 404 if `BackburnerEnabled` is false in config
- Returns 404 if workspace not found

## Frontend UI

### Workspace Header Button

Location: `WorkspaceHeader.tsx`, in `app-header__actions` div, before the VS Code button.

**Icon**: Three ascending Z letterforms (zzz/snooze motif):

```svg
<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"
     stroke-linecap="round" stroke-linejoin="round">
  <path d="M2 10h6l-6 8h6"/>
  <path d="M10 5h5l-5 7h5"/>
  <path d="M16 2h5l-5 6h5"/>
</svg>
```

**Toggle states**:

- **Off** (not backburnered): Muted gray icon (`#555`), standard ghost button styling (`btn btn--sm btn--ghost btn--bordered`). Tooltip: "Backburner".
- **On** (backburnered): Purple/lavender icon (`#b8a9e0`), tinted background (`rgba(184,169,224,0.1)`), tinted border (`rgba(184,169,224,0.4)`). Tooltip: "Wake up".

Button only renders when `backburnerEnabled` is true in config.

### Sidebar (`AppShell.tsx`)

- **Dimming**: `opacity: 0.38` on the entire workspace row when backburnered. Affects all content uniformly (text, stats, badges, icons, session tabs).
- **Sorting**: Partition workspaces into two groups — active first, backburnered second. Within each group, apply the user's chosen sort mode (alphabetical or time-based). This wraps the existing sort logic.
- **Interaction**: Fully clickable. Hover, navigation, session tabs all work normally. Opacity stays at 0.38 on hover (no brightening).
- **No separator**: The opacity change itself creates the visual boundary between groups.

### Home Page (`HomePage.tsx`)

Same treatment as sidebar:

- `opacity: 0.38` on backburnered workspace rows
- Sorted to bottom (active first, then backburnered, alphabetical within each group)
- Fully interactive

### No changes to:

- Session tabs or tab ordering
- Anything inside a workspace row beyond opacity
- Workspace detail pages (other than the header button)

## Scope Boundaries

Explicitly not in scope:

- No backburner on sessions (workspace-level only)
- No bulk backburner operations
- No backburner toggle from the sidebar (must navigate into workspace)
- No zzz icon indicator in sidebar rows
- No filtering/hiding of backburnered workspaces
- No keyboard shortcut
