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
- Confirmations describe _effects_, not just "Are you sure?"

### 4. URLs Are Idempotent

- All routes are bookmarkable and reloadable
- URL changes reflect current view; refreshing shows the same thing

### 5. Calm UI

- Avoid layout jump, flashing, and spammy notifications
- Background changes do not steal focus

---

## Pages

Open `http://localhost:7337` after starting the daemon.

### Home (`/`)

Dashboard home page. Workspace list and session overview at a glance.

**Features:**

- Workspace list with session counts and status
- Quick access to active sessions
- Real-time status updates

### Tips (`/tips`)

tmux keyboard shortcuts and quick reference.

**Features:**

- tmux key bindings reference
- Common workflows
- Quick links to other pages

### Session Detail (`/sessions/:id`)

Watch terminal output and manage a session.

**Layout:**

- Left: Live terminal via xterm.js (auto-focused on entry), resizes dynamically to fill available space
- Right: Metadata and actions, plus tabbed interface

**Terminal resizing:** The terminal viewport automatically adjusts its dimensions when the browser window is resized, maintaining proper aspect ratio and content layout.

**Workspace header:**

- Workspace info, branch (clickable when remote exists), ahead/behind counts
- Line changes (+N/-M color-coded)
- Horizontal wrapping tabs for session switching

**Session tabs:**

- Switch between multiple sessions in the same workspace
- Terminal viewer area connects visually to tabs
- Shows "Stopped" instead of time for stopped sessions
- Pastebin button (icon) next to the spawn "+" button -- sends saved text clips to the active terminal (see [Pastebin](#pastebin))

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
- Open Preview (prompts for target port, opens a local ephemeral proxy URL)

**Preview notes:**

- Previews are ephemeral — not persisted, auto-detected from running dev servers
- Tabs appear as `web:<port>` in the workspace tab bar
- Auto-cleaned when upstream server dies or session is disposed
- Clicking preview tab opens in-app; use modifier key (Cmd/Ctrl/Shift) for external browser
- Remote-host workspaces are not supported yet
- When running with `bind_address=0.0.0.0`, preview is only available to local clients on the daemon host

**Keyboard shortcuts (dashboard):**

- `Cmd+K` (or `Ctrl+K`) to enter keyboard mode
- `1-9` jump to session by index (1 = first)
- `K` then `1-9` jump to workspace by index (left nav order)
- `Cmd+Left/Right` cycle through workspace tabs (sessions, previews, diff, git)
- `Cmd+Up/Down` cycle through workspaces
- `W` dispose session (session detail only)
- `Shift+W` dispose workspace (workspace only)
- `V` open workspace in VS Code (workspace only)
- `D` go to diff page (workspace only)
- `G` go to git graph (workspace only)
- `N` spawn new session (context-aware)
- `Shift+N` spawn new session (always general)
- `H` go to home
- `?` show keyboard shortcuts help
- `Esc` cancel keyboard mode

### Spawn (`/spawn`)

Single-page wizard to start new sessions. Prompt-first design for faster workflow.

**Layout:**

- **Prompt first**: Large textarea for task description at top
- **Parallel configuration**: Repo/branch selection and target configuration below
- **AI branch suggestions**: Branch name suggestions based on prompt (when creating new workspace)
- **Enter to submit**: Press Enter in branch/nickname fields to spawn

**Add Repository (inline):**

The repo dropdown includes `+ Add Repository`. It accepts either a plain project name (to `git init` a new local repo) or a git URL (to clone a remote repo). See [Add Repository via Spawn](#add-repository-via-spawn).

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

Configure repos, run targets, models, workspace path, and pastebin entries.

**Edit mode:**

- Sticky header with "Save Changes" button (persistent while editing)
- Compact step navigation for quick section switching
- Distinction from first-run wizard: guided onboarding uses original header/footer navigation

**Features:**

- Repository management
- Run target configuration (edit modals for user-defined targets)
- Quick launch item editing (prompts for promptable targets, commands for command targets)
- Model secrets (for third-party models)
- Workspace overlay status
- Access control (network access + optional GitHub auth)
- Pastebin management (add/remove text entries for quick terminal pasting)

### Preview (`/preview/:workspaceId/:previewId`)

In-app iframe for workspace dev server previews. Proxied through the daemon's preview manager.

### Git Graph (`/commits/:workspaceId`)

Interactive commit graph for a workspace's git history.

### Conflict Resolution (`/resolve-conflict/:workspaceId/:tabId`)

Linear sync conflict resolution progress view for a persisted resolve-conflict tab.

### Remote Settings (`/settings/remote`)

Remote flavor configuration for SSH-based remote workspaces.

### Environment (`/environment`)

Compare system shell environment against the tmux server environment. Shows which variables are in sync, differ, or exist only on one side. Sync buttons push individual system values to the tmux server so new sessions pick up changes without restarting.

### Event Monitor (`/events`, dev mode only)

Real-time event monitor for the unified events system. Shows all events (status, failure, reflection, friction) from active sessions. Sidebar panel shows the last 5 events; full page view at `/events` provides a filterable table with type and session filters, auto-scroll, and expandable rows for raw JSON inspection.

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
- PastebinDropdown (quick-paste from config entries)

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

---

## Pastebin

A built-in quick-paste feature for sending saved text clips to a terminal session.

Users save text entries (commands, code snippets, boilerplate) once, then paste them into any active session with a single click. Clicking an entry sends its content to the active terminal via WebSocket (`sendInput`) and closes the dropdown.

**UI components:**

- **Button** (`SessionTabs.tsx`): Icon button using the Pastebin SVG logo. Enabled when a session is selected; dimmed otherwise.
- **Dropdown** (`PastebinDropdown.tsx`): Reuses `ActionDropdown` CSS. Renders entries alphabetically (first line, truncated to 40 chars), plus a "manage" link to Settings. Closes on click-outside only (no Escape handler).
- **Config tab** (`PastebinTab.tsx`): Tab slug `pastebin` in the Settings page. Lists entries as multi-line textareas with add/remove controls.

**Storage:** Flat `string[]` in `~/.schmux/config.json` under `"pastebin"`. No name field, no struct wrapper -- each entry is the literal text to paste.

**Why config, not state:** Pastebin entries are user-defined presets, not runtime artifacts. They survive daemon restarts, are shared across all sessions, and are edited through the same Settings page as repos and targets.

---

## Add Repository via Spawn

The spawn wizard accepts git URLs directly in the repository input, eliminating the need to edit `config.json` before cloning.

**Detection logic** (`isGitURL` in `internal/dashboard/repo_name.go`): matches `https://`, `http://`, `ssh://`, `git://` prefixes (with path after host) and `git@` prefix (with `:` after host). Everything else is a plain repo name.

**Routing:**

1. **Git URL** -- `FindRepoByURL` checks for duplicates. If not found, a name is generated, a new repo entry is appended to config, and the normal spawn flow clones via `EnsureRepoBase`.
2. **Plain name** -- creates `local:<name>` repo via `git init`.

**Name generation** (`repoNameFromURL` in `internal/dashboard/repo_name.go`):

1. Strip `.git` suffix, extract last path segment (repo name) and owner, lowercase both
2. Candidate = repo name (e.g., `claude-code`)
3. If collision: prepend owner truncated to 6 chars (`anthro-claude-code`)
4. If still collision: append numeric suffix (`anthro-claude-code-2`)

**Why inline in spawn:** The user's intent at spawn time is "work on this repo" -- forcing a config detour breaks flow. The repo entry persists for future spawns.

**Gotchas:**

- Config entry is registered before cloning starts. A failed clone leaves the entry in config.
- URL matching is exact-string. `https://...` and `git@...` for the same repo are treated as different entries.
- Plain name validation rejects `:` to prevent collision with the `local:` prefix sentinel.
