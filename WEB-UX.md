# schmux Web UX Spec (v0.5+)
Owner: Web/UX Architecture  
Audience: Any developer touching the dashboard  
Scope: Dashboard UI/UX patterns, component system, and interaction rules

## 0) Product Reality (so we design correctly)
schmux is a **high-signal internal tool**:
- Users are engineers operating multiple agents across workspaces.
- The UI is primarily **observability + orchestration**, not “content browsing”.
- Work is often **long-running**, **multi-step**, and **partially failing**.
- The UI must remain calm under churn (sessions starting/stopping, output streaming).

Primary objects:
- **Repo** → **Workspace (directory)** → **Session (agent run)**

Primary flows:
- Spawn new workspace sessions (bulk, multi-agent counts)
- Spawn into existing workspace (review/subagent)
- Observe sessions (status, attach command, timestamps)
- Open a session detail (live output, controls, dispose)
- Dispose safely (destructive, confirm, show consequences)

## 1) UX Principles (non-negotiable)
1. **Information density without chaos**
   - Default views are compact, scannable, sortable/filterable.
   - Details are on-demand via drill-in, side panel, or expand row.
2. **Status is a first-class UI element**
   - Running/stopped/waiting/error are visually consistent everywhere.
   - Real-time connection state is explicit (connected/reconnecting/offline).
3. **Destructive actions are slow, safe, and obvious**
   - "Dispose" is always clearly destructive; never hidden in ambiguous menus.
   - Confirmations describe *effects*, not just "Are you sure?"
4. **CLI-first, web-secondary**
   - The CLI is the primary interface for power users.
   - The web dashboard is for observability and visual orchestration.
   - Keyboard accessibility is basic (tab navigation, focus states, Esc to close modals).
5. **Accessible by default**
   - Focus states, ARIA, dialog focus-trap, reduced motion, readable contrast.
6. **Calm UI**
   - Avoid layout jump, flashing, and spammy notifications.
   - Background changes do not steal focus.
7. **URLs are idempotent**
   - All routes are bookmarkable and reloadable without losing state.
   - URL changes reflect the current view; refreshing shows the same thing.

## 2) App Structure (the standard layout)

### 2.1 App Shell
All pages use the same shell:
- Top bar: product name, connection indicator, theme toggle
- Left nav: Sessions, Spawn, (future) Diffs, Config, Logs
- Content area: page header (title + primary actions), then body
- Global overlays: Toaster, DialogHost

### 2.2 Routes (canonical)
- `/sessions` (default landing)
- `/sessions/:id` (session detail; terminal/log view)
- `/spawn`
- `/diff/:workspaceId` (git diff viewer)
- `/config` (settings)
- `/tips` (help/tips)

## 3) Visual System (tokens, not random CSS)

### 3.1 Design Tokens
All styling comes from a token set (CSS variables or Tailwind tokens), including:
- Color: surface, text, muted, border, accent, success, warning, danger
- Elevation: shadow-sm/md/lg
- Radius: sm/md/lg
- Spacing scale: 4/8/12/16/24/32…
- Typography scale: body, small, mono, heading
- Motion: duration-fast/normal, easing-standard

No ad-hoc hex codes in feature code.

### 3.2 Themes
- Dark mode is first-class (default to system; persist user choice).
- Contrast targets: WCAG AA for text; status colors must be legible on both themes.

## 4) Interaction Taxonomy (one rulebook)

### 4.1 Notifications (what to use when)
- **Toast**: ephemeral feedback for completed user actions (copy success, “spawn queued”).
  - Auto-dismiss, never blocks.
  - Must not contain critical info needed later.
- **Banner (persistent)**: connection loss, daemon not running, partial outage.
  - Stays until resolved/dismissed.
- **Inline error**: form validation, field-level issues, request failures tied to a component.
- **Dialog**: destructive confirmation, irreversible action, or “must answer now”.

### 4.2 Loading
- Use skeletons for lists/tables; spinners for tiny inline actions.
- Never blank the entire page if only a region is loading.
- “Refresh” should keep prior data visible and show a subtle updating state.

### 4.3 Real-time updates
- Show a connection pill: Connected / Reconnecting / Offline.
- Updates must not:
  - collapse expanded items
  - reorder rows while user is interacting (freeze sort while focused)
  - steal scroll in log views unless “Follow tail” is enabled

### 4.4 Destructive confirmation standard
Dispose patterns:
- Default: confirm dialog with explicit outcome:
  - “Dispose session X (agent: Y)?”
  - Effects: “Stops tracking, may clean workspace, closes stream…”
- For higher-risk (future: delete workspace / reset repo):
  - typed confirmation: user types workspace id

## 5) Component System (reusable, opinionated)
This is the canonical component inventory. New UI is built from these—no bespoke variants.

### 5.1 Primitives
- `Button` (variants: primary/secondary/ghost/danger; sizes: sm/md)
- `IconButton`
- `Badge` / `StatusPill` (Running/Stopped/Waiting/Error/Unknown)
- `Card` / `Section`
- `Divider`
- `Tabs`
- `Table` (supports empty state, loading state)
- `FormField` (label, hint, error)
- `TextInput`, `Textarea`, `Select`, `Combobox`
- `Dialog` + `ConfirmDialog`
- `Toast` + `Toaster`
- `Banner`
- `Tooltip`
- `DropdownMenu`
- `CopyField` (monospace + copy button + success toast)
- `Skeleton`, `Spinner`

### 5.2 Domain Components
- `WorkspaceList` (filterable, grouped by repo; expandable)
- `WorkspaceRow` (repo, branch, workspace id, session count, quick actions)
- `SessionTable` (agent, status pill, created, actions)
- `SpawnWizard` (see section 6)
- `SessionDetailView`
- `LogViewer` (see section 7)
- `ActivityCenter` (future: long-running tasks + results)

### 5.3 Component rules (enforcement)
- No inline `<style>` in pages.
- No page-local “one-off” modals or toast divs.
- Every new UI must reuse `DialogHost` and `Toaster`.
- Components must define:
  - states: idle/loading/empty/error/success
  - keyboard behavior
  - accessibility notes (aria-label, role, focus)

## 6) Screen Specs

### 6.1 Sessions (default landing)
Goals: fast overview + quick actions.

Page header:
- Title: “Sessions”
- Primary action: “Spawn”
- Secondary: search/filter, refresh

Main region:
- Filter bar:
  - Search: repo/workspace/session/agent
  - Filters: status (Running/Stopped/Waiting/Error), agent, repo
- Workspace list:
  - Grouped by repo, then workspace
  - Expand shows session table
- Session actions per row:
  - Open (session detail)
  - Copy attach command (CopyField / CopyButton)
  - Dispose (danger)

Empty state:
- Friendly instruction + single CTA “Spawn sessions”

### 6.2 Spawn (wizard, not a dumping-ground form)
Spawn is a multi-step “compose” flow.

Steps:
1) Target
   - Repo (combobox)
   - Branch (text)
   - Optional: spawn into existing workspace (if entered from workspace context)
2) Agents
   - Agent list with stepper controls (0–N)
   - Presets: “1 each”, “review squad”, “reset”
3) Prompt
   - Large textarea
   - Optional templates/snippets (future)
4) Review & Spawn
   - Summary of what will be created
   - Spawn button with progress state

Results:
- Structured results panel:
  - Created sessions (links)
  - Failures (agent + reason + suggested next step)
- One CTA: “Back to Sessions”

### 6.3 Session Detail
Layout: split view.
- Left (primary): `LogViewer`
- Right (secondary): metadata + actions
  - workspace id, repo, branch, agent, created time, status
  - Copy attach command
  - Dispose (danger)
  - (future) “Open diff”, “Open workspace”

## 7) LogViewer / Terminal UX (stop treating it like a text div)
`LogViewer` requirements:
- Header bar:
  - connection pill
  - status pill (running/stopped/waiting)
  - actions: Follow tail toggle, Pause stream, Search, Copy selection, Download
- Body:
  - monospace, readable line height
  - does not re-render entire buffer every tick (incremental append or virtualized)
  - preserves scroll position when not following tail
- Empty + error states:
  - “No output yet” vs “Disconnected” vs “Session ended”

Future upgrade path:
- swap implementation to xterm.js without changing page layout or controls

## 8) Content & Microcopy
Rules:
- Buttons are verbs: “Spawn”, “Dispose”, “Open”, “Copy attach cmd”
- Errors include actionable hints:
  - “Git pull failed (conflict). Marked workspace unusable. Resolve conflicts in …”
- Never show raw stack traces in UI; log them, surface a user-safe message.

## 9) Accessibility Checklist (ship blocker)
- Dialogs trap focus; Esc closes unless action is mandatory
- Visible focus outlines
- Proper aria-labels for icon-only buttons
- Color is never the only signal (status pill includes text)
- Reduced motion supported

## 10) Recommended Implementation (so components stay real)
Preferred stack (opinionated, modern, maintainable):
- React + TypeScript + Vite
- Tailwind (or tokenized CSS modules) + a11y-first primitives (Radix/shadcn-style)
- Client routing (or minimal router) + fetch wrappers
- Build outputs to a `dist/` served by Go (future: embed with `//go:embed`)

Acceptable alternative (if you refuse a build step):
- HTMX + server-rendered templates + a small component JS layer  
…but you still must implement the same component inventory + rules above.

