# Sapling workspace VCS guards

A user has both git and sapling repositories configured. Sapling workspaces
should show correct status in the dashboard without errors, and git-only
features (diff viewer, commit graph) should return clean "not available"
responses rather than 500 errors.

## Preconditions

- The daemon is running with one sapling workspace
- Sapling (sl) is installed in the test environment

## Verifications

- The home page shows the sapling workspace with its branch name
- The sapling workspace appears in the workspace list via WebSocket
- GET /api/diff/{saplingWorkspaceId} returns HTTP 400 (not 500)
- GET /api/workspaces/{id}/git-graph returns HTTP 400 for sapling workspaces
- The sapling workspace status updates without producing git errors
