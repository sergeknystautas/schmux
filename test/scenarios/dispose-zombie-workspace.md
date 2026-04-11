# Dispose a zombie workspace

A workspace whose VCS metadata has been corrupted or removed (e.g., after a
partial `git worktree remove` that cleaned up the bare repo but left the
directory on disk) should still be disposable from the dashboard.

The user opens the workspace header menu, clicks "Dispose workspace", and
confirms the action. The disposal should succeed — the workspace disappears
from the dashboard and is removed from state. If the directory was empty, it
is deleted. If the directory still has files, it is left on disk but the
workspace no longer appears in schmux.

## Preconditions

- A workspace exists in state whose directory has no VCS metadata (no `.git`
  file or directory for git workspaces, no `.sl` directory for sapling)

## Verifications

- The workspace appears in the dashboard workspace list
- The "Dispose workspace" button is available in the workspace header
- Confirming the disposal returns a 200 OK response
- GET /api/sessions no longer includes the disposed workspace's sessions
- GET /api/workspaces no longer includes the disposed workspace
- The user is navigated to the home page
