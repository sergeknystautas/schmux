# Add first workspace and navigate to spawn

A new user clicks "+ Add Repository" on the empty home page. They enter a
repository URL in the smart input, the system validates access, adds the repo
to the config, and navigates directly to the spawn page with the repo and
default branch pre-filled.

## Preconditions

- The daemon is running with no workspaces and no sessions
- At least one agent is detected
- git is available on the system
- A test repository URL is accessible (can be a local bare repo created for the test)

## Verifications

- Clicking "+ Add Repository" opens the add-workspace modal
- The modal shows a "Clone from" label and subtext about isolated copies
- Entering a valid repo URL and submitting triggers a "Checking repository access..." spinner
- After successful access validation, the repo is added to the config
- GET /api/config returns the new repo in the repos array
- The user is navigated to the spawn page (/spawn)
- The spawn page has the repo pre-selected
- The spawn page has a branch pre-selected (detected default branch)
- The spawn page shows at least one agent to select
