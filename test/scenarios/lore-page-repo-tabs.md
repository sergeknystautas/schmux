# View lore proposals and entries across repos

A user with multiple repositories configured wants to review lore
proposals and raw entries for each repo from a single page.

They click the "Lore" link in the sidebar and land on a consolidated
lore page. A tab bar at the top shows one tab per configured repo.
The first repo's proposals and entry counts are shown by default.

Clicking a different repo tab reloads the lore data for that repo.
The proposals section and raw entry counts update accordingly.

When only one repo is configured, the tab bar is hidden.

## Preconditions

- The daemon is running
- At least two repositories are configured

## Verifications

- The sidebar shows a single "Lore" link (not one per repo)
- Clicking "Lore" navigates to /lore (no repo name in URL)
- The page shows a "Lore" heading
- A repo tab bar is visible with one tab per configured repo
- The first repo tab is active by default
- The page shows a "Proposals" section
- The page shows a "Raw Entries" toggle
- Clicking a different repo tab changes the active tab styling
- GET /api/lore/{repoName}/proposals responds for each configured repo
