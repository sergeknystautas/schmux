# Configure Build Monitor settings

A user wants to enable the Build Monitor feature to watch GitHub Actions
workflows for hard failures. They configure it in the Experimental tab,
enable a repo, and verify the settings persist.

## Preconditions

- The daemon is running
- Dashboard auth is enabled
- GitHub OAuth app is configured (Client ID and Client Secret set)
- At least one GitHub identity has been authorized for build access
- At least one GitHub repository is configured

## Verifications

- The Settings page loads and the Experimental tab is accessible
- The Experimental tab shows the Build Monitor feature card with an enable toggle
- Enabling the Build Monitor toggle does not activate any repos by default
- Only GitHub-URL repos appear in the per-repo configuration rows
- Enabling a repo binds the single authorized identity automatically (no extra fields to fill)
- After enabling a repo, saving persists the config
- GET /api/config returns build_monitor with the repo keyed by slug (not display name)
- Reloading /config restores the Build Monitor form state correctly
