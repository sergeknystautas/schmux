# Draggable Session Tabs

## Problem

Session tabs within a workspace render in server-provided order. Users have no way to rearrange them to match their mental model or workflow.

## Solution

Add drag-to-reorder for session tabs using @dnd-kit/sortable. Custom order is stored client-side in localStorage per workspace, so each browser remembers its own arrangement independently of the server.

## Scope

- **Desktop only.** Mobile tabs remain a horizontal scroll with no drag support.
- **Session tabs only.** Accessory tabs (diff, git commit graph, previews, add-session) are not draggable and remain in their fixed positions.
- **Client-side only.** No backend or SessionsContext changes. The reordering is a view-layer sort applied between context data and rendered output.
- **No keyboard reordering in v1.** dnd-kit supports keyboard sensors, but this is out of scope for the initial implementation.

## Design

### Dependency

Add `@dnd-kit/core`, `@dnd-kit/sortable`, and `@dnd-kit/utilities` (provides `arrayMove`) to the dashboard's package.json (~25-30KB gzipped total). These provide the `DndContext`, `SortableContext`, `useSortable` hook, and array helpers.

### State Management

**Storage:** localStorage keyed by workspace ID using the existing `schmux:` prefix convention (e.g. `schmux:tab-order:{workspaceId}`). Value is a JSON array of session IDs representing the user's preferred order.

**Order resolution** is split into two layers:

- **`sortSessionsByTabOrder(workspaceId, sessions)`** — a pure utility function that reads localStorage and returns sessions sorted by stored order, with new sessions appended and disposed ones omitted from the result. It does **not** write to localStorage — pruning the stored order is the hook's responsibility on drag-end. Can be called anywhere (no hook rules). Returns original order when `workspaceId` is undefined.
- **`useTabOrder(workspaceId, sessions)`** — a React hook that wraps `sortSessionsByTabOrder` and adds drag lifecycle state (freeze/snapshot during active drags). Used only in SessionTabs.

Steps (shared by both):

1. Read stored order for the current workspace from localStorage.
2. Sort incoming sessions to match stored order.
3. Sessions not in stored order (newly spawned) are appended at the end.
4. Stored IDs whose sessions no longer exist (disposed) are omitted from the returned array (but the utility does not write back to localStorage).
5. On drag-end (hook only), write the new order (with disposed IDs pruned) to localStorage.

**Failure handling:** If `localStorage.getItem` returns null or unparseable JSON, fall back to original server order silently. If `localStorage.setItem` throws (quota exceeded, unavailable), catch silently — the drag completes visually but the order won't persist across reloads.

**When `workspaceId` is undefined** (the `workspace` prop is optional on SessionTabs), both the function and hook return sessions in their original order. The hook's `reorder` does nothing.

### Component Changes

**Two components consume the custom order:**

**SessionTabs.tsx** — the tab bar (drag source):

- Add a `DndContext` provider inside SessionTabs (this is the only drag surface).
- Wrap the session tab list in a `SortableContext` from dnd-kit.
- Extract each session tab into a `SortableSessionTab` wrapper component that calls `useSortable` and applies drag listeners + transform styles.
- On `onDragEnd`, compute the new order via `arrayMove` and persist to localStorage.
- Configure `PointerSensor` via `useSensors` with an **activation distance of 5px** to distinguish clicks from drags. This is critical because every tab is clickable for navigation and contains interactive controls (dispose button, tooltips) — without the distance threshold, clicks would initiate drags and break both navigation and in-tab actions. Child button clicks with `stopPropagation` work normally since they don't trigger 5px pointer movement.
- **When `isLocked` is true**, disable dragging entirely — either skip rendering the `DndContext`/`SortableContext` or pass an empty sensors array. Locked workspaces already disable tab clicks and force-navigate away, so dragging should be blocked too.
- **Desktop only:** Only configure sensors when not on mobile. Use a viewport width check (matching the existing CSS breakpoint) to conditionally skip the `DndContext` on mobile, ensuring touch interactions remain pure scroll.

**AppShell.tsx** — the sidebar (read-only consumer):

- The sidebar renders sessions per workspace in `.nav-workspace__sessions` via `workspace.sessions?.map()`. This must use the same custom order so the sidebar and tab bar stay consistent.
- AppShell calls `sortSessionsByTabOrder(workspace.id, workspace.sessions)` (the plain utility function, not the hook) inside the workspace `.map()` callback. This avoids a Rules of Hooks violation — hooks cannot be called inside loops. The utility function simply reads localStorage and sorts, which is safe to call anywhere.
- No drag functionality in the sidebar — it just reads the stored order.

**Non-session elements in the sortable area:** The "Spawning..." tab and the "add" button also appear inside `.session-tabs__main`. These are **excluded from the `SortableContext`** — they are rendered outside/after the sortable items list so they don't interfere with drag operations. They remain non-draggable and always appear at the end of the session tabs row.

**New utility:** `sortSessionsByTabOrder(workspaceId, sessions)` — pure function (no side effects), ~15 lines. Reads localStorage, sorts by stored order, appends new sessions, omits disposed from result. Does not write back to localStorage. Used by both the hook and the sidebar.

**New hook:** `useTabOrder(workspaceId, sessions)` — wraps `sortSessionsByTabOrder` and adds:

- A `reorder(activeId, overId)` function for the drag-end handler.
- **Freeze/snapshot during active drag** (see WebSocket section below).
- Used only in SessionTabs.

### CSS Layout: Addressing `display: contents`

On desktop, `.session-tabs__main` currently uses `display: contents`, which causes it to not generate a layout box — its children participate directly in the parent `.session-tabs` flex container. dnd-kit requires a real container box for measuring geometry and hit detection.

**Fix:** Replace `display: contents` on `.session-tabs__main` with `display: flex; gap: var(--spacing-xs)`. The parent `.session-tabs` keeps its `flex-wrap: wrap` and `gap`. This means session tabs and accessory tabs will no longer share a single flex row — `.session-tabs__main` becomes its own flex line. The spacer (`.session-tabs__spacer`) may need adjustment or removal since the session tabs and accessory tabs are now in separate flex children of the parent.

This is a layout change that needs careful verification. The mobile layout already uses `display: flex` on `.session-tabs__main`, so mobile is unaffected.

**Layout acceptance criteria:**

- Desktop: session tabs and accessory tabs remain visually on the same row when total width fits the viewport.
- Desktop: tabs wrap correctly to multiple rows when they overflow.
- Desktop: accessory tabs (diff, git, previews) remain right-aligned (spacer behavior preserved or replaced equivalently).
- Mobile: layout is visually unchanged from current behavior (horizontal scroll, accessories below).
- No visual regressions with 1 session, many sessions (wrapping), or 0 sessions (empty workspace).

### Drag Styles

Minimal additions:

- `.session-tab--dragging`: reduced opacity on the source tab during drag.
- Transform/transition styles are applied inline by dnd-kit's `useSortable`.

No changes to the mobile layout or accessory tab styles.

### WebSocket Updates During Drag

The dashboard receives real-time session updates via WebSocket. If an update arrives mid-drag (e.g., a session's status changes or a new session spawns), the `sessions` array reference changes, triggering a re-render that could disrupt the drag.

**Strategy:** The `useTabOrder` hook tracks whether a drag is active (set on drag-start, cleared on drag-end). While a drag is active, the hook **snapshots the sorted sessions list and returns the frozen snapshot** instead of re-sorting from the live data. On drag-end, it reconciles: applies the reorder to the snapshot, persists to localStorage, then releases the freeze so the next render picks up any changes that arrived during the drag.

**Edge case — dragged or target session disposed mid-drag:** If `activeId` or `overId` from the drag-end event references a session that no longer exists in the live sessions list, the reorder is discarded. The freeze releases and the UI falls back to the current live state with stored order applied. This is deterministic: any drag referencing a stale session ID is a no-op.

### Behavior Details

- **New sessions** appear at the end of the custom order (consistent with server default).
- **Disposed sessions** are removed from stored order; remaining tabs close the gap.
- **No drag affordance** (grip handle, etc.) — tabs are grabbed anywhere on the tab surface, with visual feedback only during the drag.
- **Animated reorder:** dnd-kit provides the sliding animation of sibling tabs during drag.
- **Dashboard is client-rendered only** (Vite SPA, no SSR), so reading localStorage on mount has no hydration concerns.
- **No cross-tab sync in v1.** If a user reorders tabs in one browser tab, other open tabs will not reflect the change until their next navigation or re-render. This is acceptable for v1 since multi-tab usage of the same dashboard is uncommon.
- **Same-tab consistency:** After a reorder, SessionTabs writes to localStorage and immediately uses the new order. AppShell (sidebar) reads localStorage on each render but won't re-render from the localStorage write alone — the next WebSocket update (which arrives frequently) triggers an AppShell re-render that picks up the new order. The sub-second gap is acceptable for v1.

## Testing

Unit tests (Vitest + React Testing Library):

- `useTabOrder` hook tests (tested in isolation, calling `reorder()` directly — avoids the difficulty of simulating pointer drags in jsdom):
  - Returns sessions in stored order when localStorage has a saved order.
  - Appends new sessions not in stored order at the end.
  - Prunes disposed sessions from stored order.
  - `reorder()` updates localStorage with new order.
  - Returns original order when workspaceId is undefined.
  - Freezes session list during active drag, reconciles on drag-end.
- SessionTabs component tests:
  - Accessory tabs are not wrapped in sortable context.
  - "Spawning..." tab and "add" button are not draggable.
  - DndContext is not rendered when `isLocked` is true.
  - DndContext is not rendered on mobile viewport.
- AppShell sidebar tests:
  - Sidebar session list respects stored custom order.

No E2E/scenario tests — drag interactions are flaky in Playwright, and the ordering logic is well-covered by unit tests on the hook.

## Files Changed

- `assets/dashboard/package.json` — add @dnd-kit/core, @dnd-kit/sortable, @dnd-kit/utilities
- `assets/dashboard/src/lib/tabOrder.ts` — new utility function (sortSessionsByTabOrder)
- `assets/dashboard/src/hooks/useTabOrder.ts` — new hook (wraps utility, adds drag state)
- `assets/dashboard/src/components/SessionTabs.tsx` — integrate dnd-kit, extract SortableSessionTab
- `assets/dashboard/src/components/AppShell.tsx` — use sortSessionsByTabOrder for sidebar session ordering
- `assets/dashboard/src/styles/global.css` — replace `display: contents` on `.session-tabs__main`, add `.session-tab--dragging`
- `assets/dashboard/src/lib/tabOrder.test.ts` — new tests for utility function
- `assets/dashboard/src/hooks/useTabOrder.test.ts` — new tests for hook
- `assets/dashboard/src/components/SessionTabs.test.tsx` — new/updated tests
- `assets/dashboard/src/components/AppShell.test.tsx` — new/updated tests for sidebar ordering
- `docs/react.md` — document client-side tab ordering behavior and rationale
