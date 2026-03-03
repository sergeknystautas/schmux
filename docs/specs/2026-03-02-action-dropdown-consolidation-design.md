# Action Dropdown Consolidation

## Problem

The emerging actions commit (e04523a) replaced the old QuickLaunch dropdown in SessionTabs with an ActionDropdown that only shows emerged actions from the registry. This removed the ability to spawn sessions from configured QuickLaunch presets via the "+" button. Meanwhile, the QuickLaunch tab in config and the Actions tab in lore both exist as separate management surfaces, confusing users.

## Design

The ActionDropdown shows two separate sections with distinct sources, each with a "manage" link:

```
┌──────────────────────────┐
│ Spawn a session...  wizard│
├──────────────────────────┤
│ QUICK LAUNCH      manage │
│ Claude Code              │
│ Codex                    │
├──────────────────────────┤
│ EMERGED           manage │
│ fix-lint-errors     ●●●○ │
│ run-tests           ●●○○ │
└──────────────────────────┘
```

### Data sources

- **Quick Launch section**: Reads from config (`config.quick_launch` global presets + `workspace.quick_launch` per-workspace presets). Uses existing `getQuickLaunchItems()` logic.
- **Emerged section**: Reads from the action registry via `getActions(repoName)`. Shows pinned emerged actions with confidence dots.

### Behaviors

- **Quick Launch section**: Always visible. Shows "No presets configured" when empty. "manage" link → `/config` (Quick Launch tab). Spawns immediately on click (existing QuickLaunch behavior).
- **Emerged section**: Always visible. Shows "No emerged actions yet" when empty. "manage" link → `/lore?repo=...&tab=actions`. Spawns via the existing `handleActionSpawn` logic.
- **Section headers**: Subtle, muted, uppercase labels with "manage" link right-aligned.

### Migration removal

Remove `MigrateQuickLaunchToActions()` from daemon startup. QuickLaunch presets stay in config, emerged actions stay in the registry. No cross-pollination.

## Files to change

1. `assets/dashboard/src/components/ActionDropdown.tsx` — Add QuickLaunch section, section headers with manage links, empty states
2. `assets/dashboard/src/components/ActionDropdown.module.css` — Styles for section headers, manage links, empty states
3. `internal/dashboard/server.go` — Remove `MigrateQuickLaunchToActions()` call
