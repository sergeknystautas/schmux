# Dashboard UI Subsystems

## What it does

The web dashboard provides real-time monitoring, session spawning, and workspace management through a sidebar-driven layout with collapsible tools navigation, action dropdowns for quick/emerged actions, workspace sorting, a dev-mode event monitor, persona selection in the spawn flow, and image attachment support for spawn prompts.

## Key files

| File                                                        | Purpose                                                                                               |
| ----------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `assets/dashboard/src/components/AppShell.tsx`              | Root layout: sidebar, workspace list, sort toggle, dev-mode panels, ToolsSection placement            |
| `assets/dashboard/src/components/ToolsSection.tsx`          | Collapsible tools nav (Overlays, Lore, Personas, Remote Hosts, Tips, Config)                          |
| `assets/dashboard/src/components/ToolsSection.test.tsx`     | Tests for toggle behavior, badge rendering, localStorage persistence                                  |
| `assets/dashboard/src/components/ActionDropdown.tsx`        | Per-workspace "+" dropdown: Quick Launch presets + emerged actions with spawn logic                   |
| `assets/dashboard/src/components/ActionDropdown.module.css` | CSS modules for dropdown sections, confidence dots, manage links                                      |
| `assets/dashboard/src/components/EventMonitor.tsx`          | Sidebar panel: last 5 events, collapsible, color-coded by type                                        |
| `assets/dashboard/src/routes/EventsPage.tsx`                | Full-page `/events` view: filterable table with auto-scroll and JSON expansion                        |
| `assets/dashboard/src/contexts/MonitorContext.tsx`          | React context providing `monitorEvents` and `clearMonitorEvents`                                      |
| `assets/dashboard/src/routes/SpawnPage.tsx`                 | Spawn wizard: persona dropdown layout, image paste handling, draft persistence                        |
| `assets/dashboard/src/lib/quicklaunch.ts`                   | Resolves Quick Launch items from global config + per-workspace presets                                |
| `internal/events/monitorhandler.go`                         | Backend: `MonitorHandler` forwards all event types via callback (dev-mode only)                       |
| `internal/dashboard/handlers_events.go`                     | Backend: `GET /api/dev/events/history` scans `.schmux/events/*.jsonl` across workspaces               |
| `internal/dashboard/handlers_spawn.go`                      | Backend: `SpawnRequest` struct with `ImageAttachments`, validation, file writing, prompt modification |
| `assets/dashboard/src/styles/global.css`                    | Styles for tools-section, event-monitor, workspace sort toggle, spawn form layout                     |

## Architecture decisions

- **ToolsSection replaced MoreMenu** (which replaced individual nav links). A collapsible section was chosen over a popover because it preserves discoverability while letting power users reclaim vertical space. MoreMenu.tsx no longer exists.
- **Collapsed/expanded state is persisted in localStorage** (`schmux-tools-collapsed`), not server state. This follows the pattern used by workspace sort (`schmux-workspace-sort`) and sidebar collapse (`schmux-nav-collapsed`).
- **Workspace sorting is client-side only.** The backend sends workspaces unsorted; the client applies alphabetical or time-based sort via `useMemo` in AppShell. This avoids coupling sort preferences to the API contract.
- **Time sort uses `last_output_at`** from sessions, not workspace creation time. Workspaces with no sessions sort to the bottom. A frozen snapshot prevents reordering during Cmd+Up/Down keyboard navigation.
- **ActionDropdown has two data sources that stay separate.** Quick Launch items come from config (`config.quick_launch` + `workspace.quick_launch`). Emerged actions come from the action registry via `useActions(repoName)`. They are not merged or migrated into each other.
- **Event monitoring is dev-mode only**, gated at both the backend (MonitorHandler only registered in dev mode, `/api/dev/events/history` only mounted in dev mode) and frontend (EventMonitor only rendered when `isDevMode` is true).
- **Events use a ring buffer (200 cap)** in the frontend, fed by `"event"` messages on the existing `/ws/dashboard` WebSocket. No separate WebSocket connection.
- **Persona dropdown is inline, not a separate form row.** In single-agent mode, the persona `<select>` sits in the same flex row as Agent (and Repo for fresh spawns). In multiple/advanced mode, it appears as a full-width row below the agent grid.
- **Image attachments flow through the prompt, not SpawnOptions.** The handler decodes base64, writes PNGs to `{workspace}/.schmux/attachments/`, and appends absolute paths to the prompt string. The session manager receives a normal prompt and is unaware of images.
- **50MB body limit on spawn endpoint** (vs default 1MB) to accommodate base64-encoded image payloads. Enforced via `http.MaxBytesReader` in `handleSpawnPost`.

## Gotchas

- **ToolsSection hides entirely when `navCollapsed` is true** (the 48px sidebar mode). A separate `<ToolsSection disableCollapse>` instance renders in `.tools-section--mobile-only` for mobile viewports.
- **Badge semantics differ between expanded and collapsed ToolsSection.** Expanded shows numeric count text. Collapsed shows a colored dot (red for danger, muted for informational) with the count only in the tooltip.
- **ActionDropdown has two spawn code paths.** Quick Launch items call `spawnSessions()` directly with `quick_launch_name`. Emerged actions fill template parameters, resolve learned targets, and may redirect to `/spawn` if the action has unfilled parameters.
- **Image attachments are rejected with 400** when combined with `resume: true`, `command` mode, or `remote_flavor_id`. The frontend silently ignores pastes at the 5-image cap, but the backend enforces max 5 with an error response.
- **Image attachment files persist for workspace lifetime** in `.schmux/attachments/`. There is no active cleanup mechanism.
- **SpawnDraft (including image attachments) persists in sessionStorage**, keyed by workspace ID. This survives page navigation within the tab but not tab close.
- **EventsPage fetches history on mount** from `/api/dev/events/history`, then deduplicates against live WebSocket events by `ts + session_id`. If the endpoint is unavailable (non-dev mode), it silently returns an empty array.
- **Auto-scroll in EventsPage pauses when the user scrolls up** (threshold: 40px from bottom). A "Jump to latest" button appears to re-enable it.
- **Workspace sort toggle freeze**: when navigating workspaces with Cmd+Arrow, `navSnapshotRef` freezes the current sort order for 2 seconds to prevent the list from reshuffling mid-navigation (especially relevant in time-sort mode).

## Common modification patterns

- **To add a new tool link to the sidebar**: add an entry to the `menuItems` array in `ToolsSection.tsx`. Provide `to`, `label`, `icon`, and optionally `badge`/`hidden`/`disabled`. Add styles for the route if needed.
- **To add a new section to ActionDropdown**: follow the Quick Launch / Emerged pattern — add a separator, section header with `sectionLabel` + `manageLink`, item list, and empty state. Use CSS module classes from `ActionDropdown.module.css`.
- **To add a new event type color**: update `eventDotColor()` in `EventMonitor.tsx` and `typeBadgeClass()` in `EventsPage.tsx`. Add the new type to the `EVENT_TYPES` array in `EventsPage.tsx`.
- **To add a new workspace sort mode**: add the mode to the `WorkspaceSortMode` type in `AppShell.tsx`, add a sort branch in the `sortedWorkspaces` useMemo, and add a button to the `.nav-sort-toggle` UI.
- **To change persona dropdown placement**: edit the flex layout in `SpawnPage.tsx`. Search for `agent-persona-row` (workspace mode) or the `spawn-agent-row` flex container (fresh mode). The persona select is conditionally rendered based on `personas.length > 0`.
- **To add new spawn attachment types**: extend `SpawnDraft` in `SpawnPage.tsx`, add fields to `SpawnRequest` in both `handlers_spawn.go` (Go) and `types.ts` (TypeScript), add validation rules in `handleSpawnPost`, and handle file writing before prompt assembly.
- **To change the event ring buffer size**: update the capacity in `SessionsProvider` (where `monitorEvents` is managed) and the `maxEvents` constant in `handlers_events.go`.
- **To change the event monitor sidebar display count**: update the slice in `EventMonitor.tsx` (currently `monitorEvents.slice(-5)`).
