# Dispose a zombie workspace with an empty directory

A workspace whose VCS metadata has been corrupted or removed (e.g., after a
partial `git worktree remove` that cleaned up the bare repo but left the
directory on disk), and whose directory has no other files beyond the VCS
metadata, should still be disposable from the dashboard. Because there is
nothing worth preserving inside the directory, the disposal deletes it.

The user opens the workspace header menu, clicks "Dispose workspace", and
confirms the action. The disposal should succeed — the workspace disappears
from the dashboard and is removed from state, and the directory is deleted
from disk.

## Preconditions

- A workspace exists in state whose directory has no VCS metadata (no `.git`
  file or directory for git workspaces, no `.sl` directory for sapling)
- The workspace directory is otherwise empty

## Verifications

- Confirming the disposal returns a 200 OK response
- GET /api/sessions no longer includes the disposed workspace's sessions
- GET /api/workspaces no longer includes the disposed workspace
- The workspace directory has been removed from disk
