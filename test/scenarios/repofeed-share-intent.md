# Share workspace intent via repofeed

A user has a workspace and wants to share what they're working on with
their team. They navigate to the repofeed page, see their workspace
in the Outgoing section (private by default), toggle sharing on, and
verify the workspace is now marked as shared via both the UI and API.

## Preconditions

- The daemon is running
- At least one repository is configured
- Repofeed is enabled in config (repofeed.enabled=true)
- At least one workspace exists with a running session

## Verifications

- The repofeed page loads and shows an "Outgoing" section heading
- The outgoing section lists the active workspace with a "Share activity" button
- The workspace shows a lock icon indicating it is private
- Clicking the "Share activity" button calls POST /api/workspaces/{id}/share-intent with {"share": true}
- After sharing, GET /api/sessions shows the workspace with intent_shared=true
- The repofeed page shows an "Incoming" section heading below the outgoing section
- Clicking "Stop sharing activity" calls POST /api/workspaces/{id}/share-intent with {"share": false}
- After unsharing, GET /api/sessions shows the workspace with intent_shared absent or false
