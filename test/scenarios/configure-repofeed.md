# Configure repofeed settings

A user wants to enable the repofeed system so that cross-developer
intent federation is active. They configure it via the API and
verify the settings round-trip correctly, then check the repofeed
API returns an empty list.

## Preconditions

- The daemon is running
- At least one repository is configured

## Verifications

- POST /api/config with repofeed.enabled=true is accepted
- GET /api/config returns the repofeed settings with correct values
- GET /api/repofeed returns an empty repos list (no other developers active)
- The config page loads and the Repofeed tab is accessible
- The Repofeed tab shows the "Enable repofeed" checkbox
