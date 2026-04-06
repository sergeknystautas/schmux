# Configure default communication style per agent type

A user wants to set a default communication style that applies automatically
to all sessions for a given agent type. They navigate to the config page,
find the Communication Styles section in the Sessions tab, select a default
style for an agent type, and save.

After saving, new sessions spawned for that agent type should automatically
use the configured default style.

## Preconditions

- The daemon is running
- At least one agent is configured
- At least one communication style exists (built-ins are available)

## Verifications

- The config page Sessions tab shows a "Communication Styles" section
- The section shows a dropdown for each enabled agent type
- Each dropdown lists available styles and a "None" option
- Selecting a style and clicking "Save Changes" succeeds
- GET /api/config returns the updated comm_styles map
- The comm_styles map contains the selected style for the agent type
- Clearing the style (selecting "None") and saving removes the entry from comm_styles
