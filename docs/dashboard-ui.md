# Dashboard UI Subsystems

## What it does

The web dashboard provides real-time monitoring, session spawning, and workspace management through a sidebar-driven layout with collapsible tools navigation, action dropdowns for quick/emerged actions, workspace sorting, a dev-mode event monitor, persona selection in the spawn flow, and image attachment support for spawn prompts.

> **Visual conventions live elsewhere.** This guide covers dashboard UI _behaviors_ and architecture. For the design system — tokens, component primitives, page templates, and the compliance rubric every surface is held to — see [`dashboard-style-guide.md`](dashboard-style-guide.md).

## Key files

| File                                                            | Purpose                                                                                                                   |
| --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `assets/dashboard/src/components/AppShell.tsx`                  | Root layout: sidebar, workspace list, sort toggle, dev-mode panels, ToolsSection placement, `WorkspaceStatusBadge` caller |
| `assets/dashboard/src/components/WorkspaceStatusBadge.tsx`      | Slot between sidebar workspace name and dev button: locked spinner, dirty diff, clean branch ahead/behind + CI chip       |
| `assets/dashboard/src/components/WorkspaceStatusBadge.test.tsx` | Tests for every rendering branch (locked, dirty, clean with pair, clean with chip, no remote, non-git)                    |
| `assets/dashboard/src/components/ToolsSection.tsx`              | Collapsible tools nav (Overlays, Lore, Personas, Repofeed, Timelapse, Remote Hosts, Environment, Tips, Config)            |
| `assets/dashboard/src/components/ToolsSection.test.tsx`         | Tests for toggle behavior, badge rendering, localStorage persistence                                                      |
| `assets/dashboard/src/components/ActionDropdown.tsx`            | Per-workspace "+" dropdown: Quick Launch presets + emerged actions with spawn logic                                       |
| `assets/dashboard/src/components/ActionDropdown.module.css`     | CSS modules for dropdown sections, confidence dots, manage links                                                          |
| `assets/dashboard/src/components/EventMonitor.tsx`              | Sidebar panel: last 5 events, collapsible, color-coded by type                                                            |
| `assets/dashboard/src/routes/EventsPage.tsx`                    | Full-page `/events` view: filterable table with auto-scroll and JSON expansion                                            |
| `assets/dashboard/src/contexts/MonitorContext.tsx`              | React context providing `monitorEvents` and `clearMonitorEvents`                                                          |
| `assets/dashboard/src/routes/SpawnPage.tsx`                     | Spawn wizard: persona dropdown layout, image paste handling, draft persistence                                            |
| `assets/dashboard/src/lib/quicklaunch.ts`                       | Resolves Quick Launch items from global config + per-workspace presets                                                    |
| `internal/events/monitorhandler.go`                             | Backend: `MonitorHandler` forwards all event types via callback (dev-mode only)                                           |
| `internal/dashboard/handlers_events.go`                         | Backend: `GET /api/dev/events/history` scans `.schmux/events/*.jsonl` across workspaces                                   |
| `internal/dashboard/handlers_spawn.go`                          | Backend: `SpawnRequest` struct with `ImageAttachments`, validation, file writing, prompt modification                     |
| `assets/dashboard/src/styles/global.css`                        | Styles for tools-section, event-monitor, workspace sort toggle, spawn form layout, scoped sidebar sync-group CSS          |

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
- **First-match-wins status slot in the sidebar workspace row.** Each row has a single narrow slot between the workspace name and the dev button. It shows: a spinner while the workspace is locked, then `+N -N` for uncommitted lines, then — for a clean git workspace — the branch's ahead/behind versus its remote plus the GitHub build status chip. Otherwise nothing. Three branches share one slot so the row does not fight the dev button for width. `WorkspaceHeader` (the focused-workspace detail view) deliberately shows more — both origin/main and origin/branch comparisons, plus a literal `(fork)` label — because it is a detail surface with room; the sidebar is a scanning surface with one line to spend.

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
- **Status slot class names are reused across scopes.** `WorkspaceStatusBadge` reuses `.app-header__git-pair` and `app-header__ci*` even in the sidebar. Renaming those to neutral shared names would touch `CIStatusChip`, `WorkspaceHeader`, `global.css`, and their tests. Accepted as-is; CSS scoping (`.nav-workspace__sync .app-header__ci-circle` / `.app-header__ci-dot { width: 8px }`) handles the header-vs-sidebar size mismatch instead — the defaults are sized for 0.75rem header text and read oversized against the sidebar's 0.7rem text.
- **No separate `remote_branch_exists` gate on the CI chip.** `ci_status` is documented as absent when there is no remote branch (see `docs/api.md` `WorkspaceResponseItem`), so re-checking it in the component would add a condition that can never change the output.
- **`isGit` gate is preserved on the sync group.** Workspaces whose `vcs` is not git (e.g. sapling) render nothing in the slot even when `lines_added`/`lines_removed` or `remote_unique_commits`/`local_unique_commits` are populated, because the sapling VCS adapter does not populate the remote-unique fields. The sync group inherits the same gate the dirty-diff branch uses.
- **`workspaceLockStates` and `linearSyncResolveConflictStates` stay owned by `AppShell`.** `WorkspaceStatusBadge` only takes the resolved `locked` boolean; it does not subscribe to either map. The badge stays presentational.

## Common modification patterns

- **To add a new tool link to the sidebar**: add an entry to the `menuItems` array in `ToolsSection.tsx`. Provide `to`, `label`, `icon`, and optionally `badge`/`hidden`/`disabled`. Add styles for the route if needed.
- **To add a new section to ActionDropdown**: follow the Quick Launch / Emerged pattern — add a separator, section header with `sectionLabel` + `manageLink`, item list, and empty state. Use CSS module classes from `ActionDropdown.module.css`.
- **To add a new event type color**: update `eventDotColor()` in `EventMonitor.tsx` and `typeBadgeClass()` in `EventsPage.tsx`. Add the new type to the `EVENT_TYPES` array in `EventsPage.tsx`.
- **To add a new workspace sort mode**: add the mode to the `WorkspaceSortMode` type in `AppShell.tsx`, add a sort branch in the `sortedWorkspaces` useMemo, and add a button to the `.nav-sort-toggle` UI.
- **To change persona dropdown placement**: edit the flex layout in `SpawnPage.tsx`. Search for `agent-persona-row` (workspace mode) or the `spawn-agent-row` flex container (fresh mode). The persona select is conditionally rendered based on `personas.length > 0`.
- **To add new spawn attachment types**: extend `SpawnDraft` in `SpawnPage.tsx`, add fields to `SpawnRequest` in both `handlers_spawn.go` (Go) and `types.ts` (TypeScript), add validation rules in `handleSpawnPost`, and handle file writing before prompt assembly.
- **To change the event ring buffer size**: update the capacity in `SessionsProvider` (where `monitorEvents` is managed) and the `maxEvents` constant in `handlers_events.go`.
- **To change the event monitor sidebar display count**: update the slice in `EventMonitor.tsx` (currently `monitorEvents.slice(-5)`).
- **To add a new state to the sidebar workspace status slot:** Add a branch to `WorkspaceStatusBadge.tsx` in the existing order (locked → dirty → clean), extend `WorkspaceStatusBadge.test.tsx` with cases for the new branch plus the "priority over what came before" rule, and add any scoped CSS to `global.css` near the existing `.nav-workspace__sync` block. The slot's class is `.nav-workspace__changes`; the narrow-viewport rule that hides it also hides the new content (intended).

---

## Session Connection Pill (Terminal Control-Mode)

The session detail page renders a small status pill in the terminal header that reports whether the live terminal stream is delivering real-time output. It reflects two independent state sources: the terminal WebSocket's connect status (`wsStatus`) and the backend's tmux control-mode attachment (`controlMode`).

### Key files

| File                                                                 | Purpose                                                                                                                                                             |
| -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `assets/dashboard/src/routes/SessionDetailPage.tsx`                  | Tri-state `controlMode` (`unknown`/`attached`/`detached`), pill derivation, observability attributes (`data-testid="session-connection-pill"`, `data-control-mode`) |
| `internal/dashboard/websocket.go` (`controlModeMonitor`)             | Emits `controlMode` once on connect as an initial snapshot, then on each transition                                                                                 |
| `test/scenarios/generated/helpers.ts` (`waitForControlModeAttached`) | Playwright helper that asserts `data-control-mode="attached"`                                                                                                       |

### Architecture decisions

- **Tri-state, not boolean.** `controlMode` is `'unknown' | 'attached' | 'detached'`. The boolean lied when a client connected after the tracker had already attached (no transition would fire), so the optimistic `useState(true)` produced a phantom "Live" before the backend had spoken. The initial render is now `unknown`; the backend's snapshot resolves it.
- **Snapshot on connect.** `controlModeMonitor` reads `tracker.IsAttached()` first and emits the result once before entering the 1 s tick loop. The wire contract change is the only behavior change needed on the backend; the frontend just consumes the new first message.
- **Reset on reconnect.** A new WebSocket connection sets `controlMode` back to `unknown`. The snapshot arrives within milliseconds, but the page does not assume any prior state. Without this, a brief reconnect during a working session would flash a stale "Live" until the next transition fired.
- **Pill text depends on both axes.** The pill renders `Live` only when `wsStatus === 'connected' && controlMode === 'attached'`. `Stalled` fires only when `wsStatus === 'connected' && controlMode === 'detached'` (control mode dropped while the socket is up). `Connecting…` covers both `'disconnected'` and `'connected' && 'unknown'`. The two axes are not redundant — a connected socket and a stalled control mode are different problems.

### Gotchas

- **Do not derive pill text from `wsStatus` alone.** A connected socket is not enough; `attached: true` proves control-mode protocol sync and the `%paste-buffer-changed` channel are armed, which is what makes `tmux set-buffer` and similar operations reach the daemon.
- **`TerminalStream.onControlModeChange` keeps its `(attached: boolean) => void` signature.** The wire message carries a boolean; `unknown` is the absence of a message and stays as page state, not stream state.
- **The stalled-output tooltip fires only on `detached`.** While `unknown`, the tooltip says "awaiting control-mode state" — it does not claim the stream is stalled before it has heard from the backend.
- **`data-control-mode` is a test observability attribute, not a styling hook.** Style changes should go through `connection-pill--*` classes in `global.css`. Tests and helpers read the attribute directly.

### Common modification patterns

- **To add a new pill state**: extend the `controlMode` union and the `wsPillText`/`wsPillClass` ternaries in `SessionDetailPage.tsx`, plus the Vitest cases in `SessionDetailPage.test.tsx` (the `connection pill control-mode states` describe block is the template).
- **To change what counts as "ready"**: edit the `Live` predicate in `wsPillText`. Any change to what "Live" means must be paired with a snapshot assertion in the Vitest block.
- **To add a new readiness helper for the dashboard**: extend `waitForControlModeAttached` in `helpers.ts` with the same Playwright-locator-assertion pattern; do not embed a `waitForTimeout` or polling loop.

---

## Workspace Keyboard Navigation Preference

Cmd+Up and Cmd+Down move the focused workspace up/down the sidebar. A single preference (`ui.skip_empty_workspaces`, default true) gates whether empty workspaces are skipped. When the preference is on (the default), the shortcut jumps past workspaces without sessions; when off, it visits every workspace including empties. The toggle lives in the Advanced tab under Sidebar → Navigation, immediately above the existing Debug Panels list.

### Key files

| File                                                                                  | Purpose                                                                                                                                                                                   |
| ------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `assets/dashboard/src/lib/navigation.ts` (`findNextWorkspace`)                        | Pure helper: returns the next non-disposing index in `direction` (`1` or `-1`), gated by `skipEmptySessions`. `disposing` workspaces are always skipped.                                  |
| `assets/dashboard/src/lib/navigation.test.ts`                                         | Both skip modes (default-on and the no-skip neighbor case) plus the disposing-always-skipped rule                                                                                         |
| `assets/dashboard/src/components/AppShell.tsx` (Cmd+Arrow handler)                    | Reads `config?.ui?.skip_empty_workspaces ?? true`, passes it to the helper; `skipEmptyWorkspaces` is in the `useCallback` dep list; the freeze snapshot is unchanged                      |
| `assets/dashboard/src/routes/config/AdvancedTab.tsx`                                  | Renders the toggle under Sidebar → Navigation (the section title is "Sidebar", the inner group label is "Navigation"); the section title for the panel list became "Debug Panels"         |
| `assets/dashboard/src/routes/config/buildConfigUpdate.ts`                             | Maps form state to `UIConfigUpdate.SkipEmptyWorkspaces` for the wire payload                                                                                                              |
| `internal/api/contracts/config.go` (`UIConfig`, `UIConfigUpdate`, `UIConfigResponse`) | Storage: `*bool` (nil means unset). Update wire: `*bool` + `omitempty` so partial updates omit the field. Response wire: `bool` without `omitempty` so explicit `false` always serializes |
| `internal/config/config.go` (`GetSkipEmptyWorkspaces`)                                | Resolves unset (`*bool == nil`) to `true`                                                                                                                                                 |
| `internal/dashboard/handlers_config.go` (`handleConfigUpdate`)                        | Preserves other `UIConfig` fields when a partial update arrives; only updates `SkipEmptyWorkspaces` when the request supplies it                                                          |
| `internal/config/config_test.go` (`TestGetSkipEmptyWorkspaces`)                       | Default-true / explicit-true / explicit-false                                                                                                                                             |
| `internal/dashboard/api_contract_test.go` (persistence + round-trip)                  | Update keeps an explicit `false` after a write→read cycle, omitting the field preserves the prior value, the response body contains the literal field                                     |

### Architecture decisions

- **Three shapes for one preference.** `UIConfig` (storage, `*bool`, nil = unset), `UIConfigUpdate` (request, `*bool` + `omitempty` so partial updates can omit the field), `UIConfigResponse` (response, `bool`, no `omitempty`, so an explicit `false` always serializes). The client-side `config?.ui?.skip_empty_workspaces ?? true` fallback is a defense-in-depth line, not the source of truth — under the response contract the field is always present.
- **Default-on, opt-out.** Unset config means the historical behavior, so existing users see no change. The toggle lives under Sidebar → Navigation in Advanced rather than a separate "Keyboard" section because users searching for "make Cmd+Arrow visit every workspace" look in the sidebar section.
- **`disposing` is always skipped.** The helper checks `status !== 'disposing'` independently of the flag. A disposing workspace has nothing useful to navigate to regardless of session count.
- **Handler preserves other `UIConfig` fields on a UI-only update.** `nextUI := cfg.UI; nextUI.Panels = req.UI.Panels; if req.UI.SkipEmptyWorkspaces != nil { v := *req.UI.SkipEmptyWorkspaces; nextUI.SkipEmptyWorkspaces = &v }; cfg.UI = nextUI`. A request with `{"ui":{"skip_empty_workspaces":false}}` updates only `SkipEmptyWorkspaces` and leaves `Panels` intact.
- **Helper renamed, not overloaded.** The previous `findNextWorkspaceWithSessions` became `findNextWorkspace(workspaces, currentIndex, direction, skipEmptySessions)`. Adding a second helper would have meant parallel implementations of the disposing-skip rule.

### Gotchas

- **The `?? true` fallback must agree with `GetSkipEmptyWorkspaces`.** Both default to true. If one flips to false, the client and the dashboard disagree for users with explicit `true`; if the server default ever changes, the client default must follow in the same commit.
- **Disposing workspaces are skipped even when the preference is off.** `findNextWorkspace` checks `status !== 'disposing'` independently of `skipEmptySessions`.
- **The Cmd+Arrow handler's `useCallback` deps list includes `skipEmptyWorkspaces`.** Without it, a stale closure uses the original preference after the user toggles it. The freeze snapshot mechanism (see Gotcha above) is independent and already documented.
- **Adding a new UI preference touches seven places simultaneously.** Missing any of them yields a toggle that renders correctly but never persists: `UIConfig` (storage), `UIConfigUpdate` (request wire), `UIConfigResponse` (response wire), `GetSkipEmptyWorkspaces`-style getter, `ConfigHandlers.handleConfigUpdate` (preserve-other-fields merge), `AdvancedTab.tsx` (render), `buildConfigUpdate.ts` (emit), `useConfigForm.ts` (form-state field), plus `go run ./cmd/gen-types` to refresh the generated types.
- **The persistence test relies on the absence of `omitempty` in the response shape.** Adding `omitempty` to `UIConfigResponse.SkipEmptyWorkspaces` would make an explicit `false` silently absent, and the client's `?? true` fallback would quietly override the user's choice on every page load.
- **`navigation.ts` exports more than `findNextWorkspace`.** `navigateToWorkspace` (conflict/diff/spawn routing), `findWorkspaceBySessionPrefix` (prefix-match recovery for dead sessions), `usePendingNavigation`, `currentLocationKey`, `locationUnchangedSince`, and `useLocationKeyTracker` live in the same file. Touching one requires reading the others; their behavior is independent but their exports share a module.

### Common modification patterns

- **To change the default behavior**: edit the `if c.UI.SkipEmptyWorkspaces == nil { return true }` branch in `internal/config/config.go` and the `?? true` fallback in `AppShell.tsx`. Both must agree in the same commit.
- **To add another UI preference**: mirror the three-shape pattern. Extend `UIConfig` / `UIConfigUpdate` / `UIConfigResponse` in `internal/api/contracts/config.go`; regenerate via `go run ./cmd/gen-types`; add a `*Config` getter that resolves nil to the chosen default; merge into `cfg.UI` in `handleConfigUpdate` (preserve-other-fields copy); render the toggle in `AdvancedTab.tsx`; add the dispatch field to the form-state shape and reducer (`SET_FIELD`) in `useConfigForm.ts`; emit the field in `buildConfigUpdate.ts`; cover with `config_test.go` (getter cases), `api_contract_test.go` (persistence + round-trip + omit-preserves-prior-value), `buildConfigUpdate.test.ts` (emission), and an `AdvancedTab.test.tsx` case (render + dispatch).
- **To debug a navigation that lands on the wrong workspace**: temporarily log `frozen.map((w, i) => [i, w.id, w.status, w.sessions?.length])` inside the Cmd+Arrow handler in `AppShell.tsx`. The frozen snapshot is the source of truth at navigation time, not `workspaces`.
