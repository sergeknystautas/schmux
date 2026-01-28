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

### Tips (`/`, `/tips`)
Default landing page. tmux keyboard shortcuts and quick reference.

**Features:**
- tmux key bindings reference
- Common workflows
- Quick links to other pages

### Session Detail (`/sessions/:id`)
Watch terminal output and manage a session.

**Layout:**
- Left: Live terminal via xterm.js (auto-focused on entry)
- Right: Metadata and actions, plus tabbed interface

**Workspace header:**
- Workspace info, branch (clickable when remote exists), ahead/behind counts
- Line changes (+N/-M color-coded)
- Horizontal wrapping tabs for session switching

**Session tabs:**
- Switch between multiple sessions in the same workspace
- Terminal viewer area connects visually to tabs
- Shows "Stopped" instead of time for stopped sessions

**Diff tab:**
- "X files +Y/-Z" tab appears when workspace has changes
- Integrated diff view with same header structure
- Resizable file list sidebar (localStorage persistence)
- Filename prominently displayed with directory path in smaller text
- Per-file lines added/removed instead of status badge

**Actions:**
- Copy attach command
- Dispose session
- Open diff, open workspace in VS Code

### Spawn (`/spawn`)
Single-page wizard to start new sessions. Prompt-first design for faster workflow.

**Layout:**
- **Prompt first**: Large textarea for task description at top
- **Parallel configuration**: Repo/branch selection and target configuration below
- **AI branch suggestions**: Branch name suggestions based on prompt (when creating new workspace)
- **Enter to submit**: Press Enter in branch/nickname fields to spawn

**When spawning into existing workspace:**
- Shows workspace context (header + tabs)
- Auto-navigates to new session after successful spawn

**Quick launch (inline):**
- "+" button in session tabs bar opens dropdown
- Quick launch presets for one-click spawning
- "Custom..." option opens full spawn wizard

**Results panel:**
- Created sessions (with links)
- Failures (agent + reason + full prompt attempted)
- "Back to Sessions" CTA

### Diff (`/diff/:workspaceId`)
View git changes for a workspace.

**Features:**
- Side-by-side diff viewer
- See what agents changed
- Compare across multiple workspaces

### Settings (`/config`)
Configure repos, run targets, variants, and workspace path.

**Edit mode:**
- Sticky header with "Save Changes" button (persistent while editing)
- Compact step navigation for quick section switching
- Distinction from first-run wizard: guided onboarding uses original header/footer navigation

**Features:**
- Repository management
- Run target configuration (edit modals for user-defined targets)
- Quick launch item editing (prompts for promptable targets, commands for command targets)
- Variant secrets
- Workspace overlay status
- Access control (network access + optional GitHub auth)

### Authentication (Optional)
When enabled, the dashboard requires GitHub login and runs over HTTPS. Configure this under **Settings → Advanced → Access Control** or via `schmux auth github`.

Notes:
- `public_base_url` is the canonical URL used for OAuth callbacks and derived CORS origins.
- TLS cert/key paths must be configured for the daemon to start with auth enabled.
 - Callback URL must be `https://<public_base_url>/auth/callback`.

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
