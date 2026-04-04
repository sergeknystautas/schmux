# Sapling workspace VCS support

A user has both git and sapling repositories configured. Sapling workspaces
should show correct status in the dashboard and support the same operations
as git workspaces (diff, stage, discard, commit graph) via the VCS-agnostic CommandBuilder.

## Preconditions

- The daemon is running with one sapling workspace that has uncommitted changes
- Sapling (sl) is installed in the test environment

## Verifications

- The home page shows the sapling workspace with its branch name
- The sapling workspace appears in the workspace list via WebSocket with vcs="sapling"
- GET /api/diff/{saplingWorkspaceId} returns HTTP 200 with file changes
- POST /api/workspaces/{id}/git-commit-stage succeeds for sapling workspaces
- POST /api/workspaces/{id}/git-discard removes untracked files in sapling workspaces
- GET /api/workspaces/{id}/git-graph does not reject sapling workspaces at the VCS type gate (no HTTP 400)
