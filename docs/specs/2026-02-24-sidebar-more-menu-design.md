# Sidebar More Menu Button Design

**Date:** 2026-02-24
**Status:** Approved

## Goal

Consolidate 5 navigation items into a single "More ↑" button to reclaim vertical space in the sidebar for the workspaces list and dev-mode content.

## Current State

Bottom of sidebar structure:

1. Workspaces list (scrollable)
2. TypingPerformance (dev mode only)
3. nav-links section containing:
   - Overlays (with unread badge)
   - Lore (with pending count badge)
   - Remote Hosts
   - Divider
   - Tips and Config (horizontal row)
4. RemoteAccessPanel (if enabled in config)

## Proposed State

Bottom of sidebar structure:

1. Workspaces list (scrollable) — gains vertical space
2. TypingPerformance (dev mode only) — unchanged
3. RemoteAccessPanel (if enabled) — moved up
4. **More ↑ button** — new, consolidates all 5 nav items

## More Button Specification

### Button

- **Label:** "More"
- **Icon:** Up arrow (↑) indicating upward expansion
- **Location:** Absolute bottom of sidebar
- **Style:** Consistent with existing nav-link styling

### Dropdown Menu

- **Trigger:** Click to open
- **Position:** Opens above the button (pop-up style)
- **Close behavior:** Click outside, escape key, or item selection

### Menu Items (in order)

1. **Overlays** — icon + label + unread badge (if count > 0)
2. **Lore** — icon + label + pending count badge (if count > 0)
3. **Remote Hosts** — icon + label
4. **Tips** — icon + label
5. **Config** — icon + label

### Visual Details

- Dropdown styled consistently with existing sidebar
- Active item highlighted when on that page
- Badges preserved and visible
- Icons preserved from current implementation

## Implementation Notes

- Remove `.nav-links` section from AppShell
- Add new `MoreMenu` component with dropdown state
- Preserve NavLink behavior (active state, navigation)
- Preserve badge logic (overlayUnreadCount, totalLorePending)
- Move RemoteAccessPanel above MoreMenu
- Ensure keyboard accessibility (Tab, Enter, Escape)
