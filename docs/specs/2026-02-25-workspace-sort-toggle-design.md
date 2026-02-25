# Workspace Sort Toggle Design

## Overview

Add a sort toggle to the Workspaces header in the sidebar, allowing users to switch between alphabetical and time-based sorting. The preference is stored in localStorage and sorting is performed client-side.

## UI Changes

**Header row in sidebar:**

```
Workspaces (21)                    [abc | 12:00]
```

- Count in parentheses after "Workspaces"
- Segmented control pill, right-aligned with session activity times
- Active segment highlighted
- "abc" = alphabetical sort (current behavior)
- "12:00" = time sort (most recent session activity first)

## Data Flow

1. **Server** - Remove sorting logic from backend, send workspaces as-is
2. **Client** - Receives workspaces via WebSocket, applies sort based on localStorage
3. **localStorage key** - `schmux-workspace-sort` with value `"alpha"` or `"time"`
4. **Default** - If no preference stored, default to `"alpha"` (current behavior)

## Sorting Logic (Client-Side)

**Alphabetical (`abc`):** Same as current backend - sort by repo name, then branch name

**Time (`12:00`):**

- For each workspace, find the most recent `last_output_at` from all its sessions
- Sort workspaces by this timestamp, descending (most recent first)
- Workspaces with no sessions sort to the bottom

## Files to Change

1. `internal/dashboard/handlers_sessions.go` - Remove workspace sorting (let client handle it)
2. `assets/dashboard/src/components/AppShell.tsx` - Add count + toggle UI, implement client-side sorting
3. `assets/dashboard/src/styles/global.css` - Styles for segmented control

## Implementation Notes

- The segmented control should match existing UI patterns in the app
- Consider reusing any existing toggle/segmented control components if available
- The workspace count is simply `workspaces.length`
