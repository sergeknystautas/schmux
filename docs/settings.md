# Settings Page

## What it does

The `/config` route provides a 6-tab settings page for configuring schmux. Features are organized by maturity: core settings are always visible, experimental features require explicit opt-in, and dev-only diagnostics are gated behind the debug UI toggle.

## Tab structure

| Tab          | Purpose                                                                                |
| ------------ | -------------------------------------------------------------------------------------- |
| Workspaces   | Workspace path, repository list, VCS settings                                          |
| Sessions     | Quick Launch, Pastebin, Command Targets, NudgeNik, Notifications, session behavior     |
| Agents       | Task assignments (which model does which job), Model Catalog, User Models              |
| Access       | Authentication, network, TLS, remote access                                            |
| Experimental | Per-feature opt-in toggles with inline config                                          |
| Advanced     | Debug toggle, tmux, polling/timeouts, xterm, sapling, diff tools, dev-only diagnostics |

## Key files

| File                                                         | Purpose                                                                 |
| ------------------------------------------------------------ | ----------------------------------------------------------------------- |
| `assets/dashboard/src/routes/ConfigPage.tsx`                 | Main page — tab navigation, config loading, auto-save wiring            |
| `assets/dashboard/src/routes/config/useConfigForm.ts`        | Form state hook — `ConfigFormState`, reducer, derived values            |
| `assets/dashboard/src/routes/config/useAutoSave.ts`          | Auto-save hook — action allowlist/denylist, debounce, revert-on-failure |
| `assets/dashboard/src/routes/config/buildConfigUpdate.ts`    | Pure function that maps `ConfigFormState` → `ConfigUpdateRequest`       |
| `assets/dashboard/src/routes/config/ExperimentalTab.tsx`     | Experimental tab — renders toggle cards from registry                   |
| `assets/dashboard/src/routes/config/experimentalRegistry.ts` | Static array of experimental features with metadata                     |
| `assets/dashboard/src/routes/config/ConfigPanelProps.ts`     | Shared props type for standalone config panel components                |
| `assets/dashboard/src/routes/config/AgentsTab.tsx`           | Agents tab — task assignments, model catalog                            |
| `assets/dashboard/src/routes/config/CommStylesConfig.tsx`    | Comm styles config panel — default style per tool (experimental)        |
| `assets/dashboard/src/routes/config/SessionsTab.tsx`         | Sessions tab — spawn config, NudgeNik, notifications                    |
| `assets/dashboard/src/routes/config/AdvancedTab.tsx`         | Advanced tab — infrastructure knobs, dev-only sections                  |

## Architecture decisions

- **Immediate persistence (no Save button).** Config auto-saves on every change. Toggles/selects save immediately (300ms debounce for coalescing). Text inputs save on blur, not on keystroke. This eliminated `ConfigSnapshot`, `hasChanges`, `snapshotConfig`, and the wizard-era `stepErrors` infrastructure.

- **Flat config fields, not a nested `Experimental` namespace.** Each experimental feature uses its own top-level config field (e.g., `lore.enabled`, `timelapse.enabled`). This means graduating a feature from Experimental to a core tab doesn't require a config migration — the JSON shape in `~/.schmux/config.json` is stable regardless of where the UI renders the fields.

- **Config panel components are standalone.** Each feature's config UI (e.g., `LoreConfig`, `TimelapseConfig`) accepts `ConfigPanelProps` (`state`, `dispatch`, `models`) and renders its fields as a fragment. It knows nothing about the Experimental system. The ExperimentalTab card provides the enable/disable toggle and conditionally renders the panel — the panel itself has no toggle.

- **Experimental registry is a static array, not metadata-driven.** Features are manually listed in `experimentalRegistry.ts`. This is intentional — there's no dynamic feature discovery. Adding a feature means adding an entry to the array.

- **Build-time vs. config-time feature gating.** Some features (Repofeed, Subreddit) have two gates: a build-time flag (`GET /api/features`, controlled by compile tags) and a config-time toggle (user opt-in). If the build-time feature is unavailable, the Experimental card is hidden entirely. The `buildFeatureKey` field in the registry controls this.

- **Task Assignments above the fold in Agents.** The Model Catalog can be very large (20+ models). Task Assignments (commit message, PR review, branch suggest, conflict resolve targets) are placed first so they're visible without scrolling.

- **Dev-only sections gated by `debugUI`.** Terminal Desync Diagnostics and IO Workspace Telemetry are only useful for schmux developers. They appear at the bottom of the Advanced tab and only render when `debug_ui` is enabled in config.

## Visibility tiers

| Tier                       | Gating mechanism                         | Examples                                              |
| -------------------------- | ---------------------------------------- | ----------------------------------------------------- |
| Always visible             | None                                     | Workspaces, Sessions, Agents, Access                  |
| Experimental               | Per-feature `enabled` bool, user-toggled | Personas, Comm Styles, Lore, Floor Manager, Timelapse |
| Experimental + build-gated | Build-time flag AND `enabled` bool       | Repofeed, Subreddit                                   |
| Dev-only                   | `debug_ui` bool                          | Desync Diagnostics, IO Workspace Telemetry            |

## Auto-save system

The auto-save hook (`useAutoSave`) wraps the raw `useReducer` dispatch. It intercepts actions and decides whether to trigger a save:

- **Action allowlist**: `TOGGLE_MODEL`, `CHANGE_RUNNER`, `ADD_REPO`, `REMOVE_REPO`, and other array mutation actions always trigger save.
- **`SET_FIELD` with denylist**: `SET_FIELD` triggers save unless the field is transient (e.g., `newRepoName`, `passwordInput`, `currentStep`, `saving`, `loading`).
- **Revert-on-failure**: Maintains a `lastSavedConfig` ref. On save failure, dispatches `LOAD_CONFIG` from the ref to revert all local state, plus shows an error toast.
- **Error-only indicator**: The header shows nothing on success (auto-save is invisible). An "Error saving" indicator appears only on failure.

## Graduating a feature from Experimental

1. Remove the feature's entry from `EXPERIMENTAL_FEATURES` in `experimentalRegistry.ts`
2. Render its `<XxxConfig />` component in the destination tab
3. (Optional) Remove the enable/disable toggle if the feature becomes always-on
4. (If removing toggle) Remove the `enabled` field from `ConfigFormState`, `initialState`, `LOAD_CONFIG`

Steps 1-2 are the minimal change. No backend changes. No config migration.

## Gotchas

- **Config panels must not have their own enable toggle.** The ExperimentalTab card provides the toggle. If a panel also has one, the user sees two checkboxes for the same thing.
- **`buildConfigUpdate` sends tmux fields unconditionally.** It used to compare against `originalConfig` to conditionally send `tmux_binary` and `tmux_socket_name`. After removing `originalConfig` for auto-save, these are always sent. The server handles no-ops gracefully.
- **`updateConfig` is imported from `lib/api`, not from ConfigContext.** The auto-save hook calls the API directly — it doesn't go through a context method.
- **Auth secrets are NOT auto-saved.** They use a separate modal flow (`saveAuthSecrets`) because they involve password confirmation. Don't add them to the auto-save allowlist.
- **Dissolved tab slugs fall back to tab 1.** Old URL slugs (`?tab=quicklaunch`, `?tab=codereview`, etc.) are handled by `DISSOLVED_SLUGS` set in ConfigPage — they redirect to the Workspaces tab instead of showing a blank page.

## Design debt

**Harness vs. model separation**: The Model Catalog treats each entry as a (model, harness) tuple. A proper separation into independent entities is deferred. The Agents tab is the natural place to address this when the friction becomes real.
