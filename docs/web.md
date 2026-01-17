# Web Dashboard

**Problem:** Some tasks are faster from a terminal; others benefit from visual UI. Tools that force you into one interface create friction when the other would be better for the job.

---

## Dashboard Purpose

The web dashboard is for **observability and orchestration**:

- See all your sessions and workspaces at a glance
- Monitor real-time terminal output
- Spawn and manage sessions visually
- Compare results across agents via git diffs

The CLI is for **speed and scripting**:

- Quick commands from the terminal
- Scriptable operations
- JSON output for automation

---

## UX Principles

### 1. Information Density Without Chaos
- Default views are compact, scannable, sortable/filterable
- Details are on-demand via drill-in

### 2. Status Is First-Class
- Running/stopped/waiting/error visually consistent everywhere
- Real-time connection state is explicit

### 3. Destructive Actions Are Slow
- "Dispose" is always clearly destructive
- Confirmations describe *effects*, not just "Are you sure?"

### 4. URLs Are Idempotent
- All routes are bookmarkable and reloadable
- URL changes reflect current view; refreshing shows the same thing

### 5. Calm UI
- Avoid layout jump, flashing, and spammy notifications
- Background changes do not steal focus

---

## Pages

Open `http://localhost:7337` after starting the daemon.

### Sessions (`/`, `/sessions`)
Default landing page. View all sessions grouped by workspace, filter by status or repo.

**Features:**
- Filter by status (Running/Stopped/Waiting/Error), agent, or repo
- Search across sessions
- Grouped by workspace (expand to see sessions)
- Quick actions: Open, copy attach command, dispose

### Session Detail (`/sessions/:id`)
Watch terminal output and manage a session.

**Layout:**
- Left: Live terminal via xterm.js
- Right: Metadata (workspace, repo, branch, agent, status) and actions

**Actions:**
- Copy attach command
- Dispose session
- (Future) Open diff, open workspace in VS Code

### Spawn (`/spawn`)
Multi-step wizard to start new sessions.

**Steps:**
1. Target: Repo, branch, optional existing workspace
2. Agents: Select agents with stepper controls (0–N)
3. Prompt: Large textarea for the task
4. Review & Spawn: Summary of what will be created

**Results panel:**
- Created sessions (with links)
- Failures (agent + reason + suggested next step)
- "Back to Sessions" CTA

### Diff (`/diff/:workspaceId`)
View git changes for a workspace.

**Features:**
- Side-by-side diff viewer
- See what agents changed
- Compare across multiple workspaces

### Settings (`/config`)
Configure repos, run targets, variants, and workspace path.

**Features:**
- Repository management
- Run target configuration
- Variant secrets
- Workspace overlay status

---

## Real-Time Updates

### Connection Status
Always-visible pill: Connected / Reconnecting / Offline

### Update Behavior
- Show connection indicator
- Do not collapse expanded items
- Do not reorder rows while user is interacting
- Preserve scroll position in log views (unless "Follow tail" is enabled)

---

## Design System

### Design Tokens
All styling uses CSS custom properties:

```css
:root {
  --color-surface: #ffffff;
  --color-text: #1a1a1a;
  --color-accent: #0066cc;
  --spacing-md: 12px;
  --radius-md: 6px;
}
```

### Dark Mode
First-class support via `[data-theme="dark"]` attribute. Persists to localStorage.

### Accessibility
- Focus states visible
- Dialogs trap focus; Esc closes
- ARIA labels for icon-only buttons
- Color is never the only signal (status includes text)

---

## Component Inventory

### Primitives
- Button, IconButton, Badge, StatusPill, Card, Divider, Tabs
- Table, FormField, TextInput, Textarea, Select, Combobox
- Dialog, ConfirmDialog, Toast, Toaster, Banner, Tooltip
- CopyField, Skeleton, Spinner

### Domain Components
- WorkspaceList (filterable, grouped by repo)
- WorkspaceRow (repo, branch, session count, quick actions)
- SessionTable (agent, status, created, actions)
- SpawnWizard (multi-step form)
- SessionDetailView (terminal + metadata)
- LogViewer (xterm.js wrapper)

---

## Notifications

- **Toast**: Ephemeral feedback for completed actions (auto-dismiss)
- **Banner**: Persistent for connection loss, daemon not running
- **Inline error**: Form validation, field-level issues
- **Dialog**: Destructive confirmation, irreversible action

---

## Destructive Actions

Dispose patterns:

**Default**: Confirm dialog with explicit outcome
```
Dispose session X (agent: Y)?
Effects: Stops tracking, closes stream, tmux session deleted
```

**Higher-risk** (future: delete workspace): Typed confirmation
```
Type workspace ID to confirm: myproject-001
```
