# Configure a new repository

A user setting up schmux for the first time wants to add a git repository
to the configuration so they can spawn sessions against it.

They navigate to the Settings page, go to the Workspaces tab, fill in the
repository name and URL in the add form, and click "Add". Adding a repo
triggers an auto-save (no Save button). After the auto-save completes, the
repository appears in the config API.

## Preconditions

- The daemon is running
- A local git repository exists at a known path

## Verifications

- The Settings page loads with the Workspaces tab active by default
- The manual add form is behind a disclosure ("Or add manually...") that must be opened first
- The add form accepts a name and URL
- Clicking "Add" adds the repo to the list and auto-saves
- Wait briefly for auto-save to complete
- POST /api/config accepts the updated config
- GET /api/config shows the new repo in the repos list
