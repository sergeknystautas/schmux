# View code changes in a workspace

A user wants to review the uncommitted code changes that an AI agent has made
in a workspace.

They navigate to the diff page for a workspace that has uncommitted changes.
The page should show a file list sidebar on the left with the changed files,
and a diff viewer on the right showing the actual changes for the selected file.

## Preconditions

- The daemon is running with a workspace that has uncommitted git changes
  (at least one modified file with added and removed lines)

## Verifications

- The diff page loads and shows the file list sidebar
- At least one changed file appears in the file list with +/- line counts
- Clicking a file shows its diff in the viewer
- The diff viewer shows added lines (green) and removed lines (red)
- GET /api/diff/:workspaceId returns the file changes with diff content
