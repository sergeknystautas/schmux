# Dismiss completed intent from repofeed

A user sees a completed intent from another developer in the repofeed
incoming section and wants to clear it from their view. They click the
dismiss button and verify it is recorded via the API.

## Preconditions

- The daemon is running
- At least one repository is configured
- Repofeed is enabled in config (repofeed.enabled=true)

## Verifications

- POST /api/repofeed/dismiss with developer and workspace_id returns 200
- POST /api/repofeed/dismiss with missing workspace_id returns 400
- POST /api/repofeed/dismiss without a dismissed store configured returns 503
