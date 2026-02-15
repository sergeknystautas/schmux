# View and manage overlay files across repos

A user with multiple repositories configured wants to view and manage
overlay files for each repo without navigating between separate pages.

They click the "Overlays" link in the sidebar and land on a single
overlay page. A tab bar at the top shows one tab per configured repo.
The first repo's overlay information is shown by default.

Clicking a different repo tab switches the displayed overlay data to
that repo's files. The auto-managed and repo-specific sections update
to reflect the selected repo.

When only one repo is configured, the tab bar is hidden and the page
shows that repo's overlays directly.

## Preconditions

- The daemon is running
- At least two repositories are configured
- Overlay data exists for at least one repo (auto-managed builtin paths)

## Verifications

- The sidebar shows a single "Overlays" link (not one per repo)
- Clicking "Overlays" navigates to /overlays (no repo name in URL)
- The page title says "Overlay Files"
- A repo tab bar is visible with one tab per configured repo
- The first repo tab is active by default
- The page shows an "Auto-managed" section
- Clicking a different repo tab changes the active tab styling
- The overlay content updates when switching tabs
- GET /api/overlays returns overlay info for all configured repos
