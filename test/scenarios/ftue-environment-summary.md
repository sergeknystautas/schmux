# First-time home page shows detected environment

A new user opens the dashboard for the first time with no workspaces configured.
Instead of being redirected to a setup wizard, they land on the home page and see
what tools were detected on their system — agents, version control, and tmux —
displayed as badges. The page also shows a prominent call-to-action to add their
first workspace.

## Preconditions

- The daemon is running with no workspaces and no sessions
- git is available on the system

## Verifications

- The home page loads without redirecting to /config
- The environment summary section is visible with detected tool badges
- A VCS badge is shown (e.g., "git")
- The "+ Add Repository" call-to-action button is visible
- The branches section is NOT shown (hidden when no workspaces)
- The tmux attach tip is NOT shown (hidden when no workspaces)
- GET /api/detection-summary returns status "ready" with non-empty vcs array
