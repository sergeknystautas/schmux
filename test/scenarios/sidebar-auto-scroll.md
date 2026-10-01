# Sidebar auto-scroll yields to manual scrolling

A user with many workspaces scrolls the left sidebar to look at other
workspaces. Background updates must not yank the sidebar back to the
workspace they are working in. When they navigate to another workspace, the
sidebar brings it into view again.

## Preconditions

- The daemon is running with one repository and eight workspaces, each with
  one running session (branches `scroll-01` … `scroll-08`)
- The browser viewport is short enough (480px tall) that the sidebar's
  workspace list overflows

## Verifications

- On a session page for `scroll-01`, with alphabetical sort, the active
  workspace row is visible in the sidebar
- For each way a user scrolls the sidebar — mouse wheel, dragging the
  scrollbar, touch drag, and the End key on a focused sidebar row — after
  scrolling, the active workspace row is no longer visible, and renaming the
  `scroll-08` session via the API updates the sidebar (the new nickname
  appears) without the sidebar scrolling
- After a manual scroll, pressing Ctrl+ArrowDown navigates to `scroll-02`,
  and its workspace row scrolls into view in the sidebar
