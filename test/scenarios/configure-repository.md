# Configure a new repository

A user setting up schmux for the first time wants to add a git repository
to the configuration so they can spawn sessions against it.

They navigate to the config page, go to the Workspaces tab, fill in the
repository name and URL in the add form, click "Add Repository", and then
save the configuration.

After saving, the repository should appear in the spawn page's repository
dropdown.

## Preconditions

- The daemon is running
- A local git repository exists at a known path

## Verifications

- The config page loads with the Workspaces tab
- The "Add Repository" form accepts a name and URL
- Clicking "Add Repository" adds the repo to the list
- The "Save Changes" button appears after making changes
- Clicking "Save Changes" succeeds (no error toast)
- POST /api/config accepts the updated config
- Navigating to /spawn shows the new repo in the repository dropdown
