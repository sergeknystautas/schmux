# Tools Section: Collapsible Sidebar Navigation

## Problem

The sidebar has a "More" popover hiding six feature links (Overlays, Lore, Personas, Remote Hosts, Tips, Config). This hurts discoverability — users don't know these features exist unless they click a nondescript "More" button. But showing them permanently consumes vertical space needed for the workspace list.

## Solution

Replace the "More" popover with a collapsible **Tools section** at the bottom of the sidebar. It starts expanded (vertical list with labels) so new users discover features naturally. Users can collapse it into a compact horizontal icon bar when they want more workspace room.

## Design

### Expanded State (default)

- Header row: `▾` chevron on the left + "Tools" label, same muted style as "Workspaces" header
- Below: vertical stack of links, each with icon + label + optional badge
- Active route gets accent highlight
- Clicking a link navigates; section stays expanded

### Collapsed State (user-toggled)

- Single row: `▸` chevron on the left, then icons spaced across remaining width
- No "Tools" label — maximize horizontal space for icons
- Badges render as small colored dots on icon top-right corner (red for attention-needed, muted for informational)
- Tooltip on hover shows label (e.g., "Overlays (3 unread)")
- Active icon gets accent color
- Clicking an icon navigates; clicking the chevron re-expands

### Whole-sidebar collapse (48px mode)

- Tools section hides entirely (consistent with current behavior)

### Persistence

- Collapsed/expanded state stored in localStorage key `schmux-tools-collapsed`

### Transitions

- Instant swap between states, no animation

## Implementation

### New: `assets/dashboard/src/components/ToolsSection.tsx`

Replaces MoreMenu. Contains collapsible section with both expanded/collapsed rendering. Reads state from localStorage. Takes `navCollapsed` prop. Houses same link definitions from MoreMenu (routes, icons, badges, visibility conditions).

### Modify: `assets/dashboard/src/components/AppShell.tsx`

Swap `<MoreMenu>` for `<ToolsSection>`. Remove MoreMenu import. Pass `navCollapsed` prop.

### Modify: `assets/dashboard/src/styles/global.css`

Remove `.more-menu__*` styles. Add `.tools-section` styles:

- Header row (chevron + label)
- Expanded vertical list (reuse more-menu item styling patterns)
- Collapsed horizontal icon row (flexbox, gap)
- Badge dots on icons
- Tooltip on hover
- Hide rules under `.app-shell--collapsed`

### Delete: `MoreMenu.tsx` and `MoreMenu.test.tsx`

Fully replaced by ToolsSection.

### New: `assets/dashboard/src/components/ToolsSection.test.tsx`

Port relevant tests from MoreMenu (navigation, badges). Add tests for toggle behavior and localStorage persistence.
