# Dispose a session

A user wants to stop and remove an AI agent session that is no longer needed.

They navigate to a running session's detail page, click the "Dispose Session"
button in the sidebar, and confirm the action in the confirmation dialog.

After confirming, they should be navigated away from the disposed session. The page
redirects to the workspace's natural landing page (spawn page, since this test's
workspace has no other sessions and no git changes). The session should no longer
appear in the workspace tabs or the API.

## Preconditions

- The daemon is running with a spawned session

## Verifications

- The "Dispose Session" button is visible in the sidebar
- Clicking it shows a confirmation dialog
- Confirming the dialog navigates away from the session to the spawn page for
  the disposed session's workspace
- The disposed session no longer appears in the workspace tabs
- GET /api/sessions no longer includes the disposed session
