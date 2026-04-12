# Configure repofeed settings

A user wants to enable the repofeed system so that cross-developer
intent federation is active. They configure it via the API and
verify the settings round-trip correctly, then check the repofeed
API returns an empty list.

Repofeed is now an experimental feature on the Experimental tab.
The user navigates to Settings > Experimental, finds the Repofeed
card, and enables it via the experimental toggle.

## Preconditions

- The daemon is running
- At least one repository is configured

## Verifications

- POST /api/config with repofeed.enabled=true is accepted
- GET /api/config returns the repofeed settings with correct values
- GET /api/repofeed returns an empty repos list (no other developers active)
- The Settings page loads and the Experimental tab is accessible
- The Experimental tab shows the Repofeed feature card with an enable toggle
