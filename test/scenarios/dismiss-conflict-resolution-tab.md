# Dismiss conflict resolution tab after completion

A user has a workspace where conflict resolution has completed (succeeded or
failed). The conflict resolution tab should disappear once dismissed and must
not reappear from subsequent WebSocket broadcasts.

The user navigates to a session page where a conflict resolution tab is visible
(the resolution is no longer in progress). They click the dismiss button on the
tab. The tab should disappear immediately and should not come back, even though
the backend may still broadcast the stale state before the DELETE request is
fully processed.

## Preconditions

- The daemon is running with a spawned session in a workspace
- A conflict resolution state exists for that workspace (status "done" or "failed")

## Verifications

- The conflict resolution tab is visible on the session page
- Clicking the tab's dismiss (X) button removes the tab
- After dismissal, the tab does not reappear on subsequent WebSocket broadcasts
- The DELETE API for the conflict resolution state returns success
- The session tabs still show the session tab and other accessory tabs normally
