# Dispose a zombie workspace whose directory still has files

A workspace whose VCS metadata has been corrupted or removed (e.g., after a
partial `git worktree remove` that cleaned up the bare repo but left the
directory on disk), and whose directory still contains non-VCS files (for
example `.schmux/events` session transcripts), should still be disposable
from the dashboard. Because schmux does not know what the leftover files
represent, it refuses to destroy them: disposal removes the workspace from
schmux's state but the directory is left on disk for manual cleanup.

The user opens the workspace header menu, clicks "Dispose workspace", and
confirms the action. The disposal should succeed — the workspace disappears
from the dashboard and is removed from state — but the directory and its
surviving files remain on disk exactly as they were.

## Preconditions

- A workspace exists in state whose directory has no VCS metadata (no `.git`
  file or directory for git workspaces, no `.sl` directory for sapling)
- The workspace directory contains at least one non-VCS file that is not
  safe for schmux to delete without the user's knowledge

## Verifications

- Confirming the disposal returns a 200 OK response
- GET /api/sessions no longer includes the disposed workspace's sessions
- GET /api/workspaces no longer includes the disposed workspace
- The workspace directory still exists on disk
- The non-VCS files inside the directory are still present and unchanged
