# Action dropdown shows quick launch and emerged sections

A user clicks the "+" button on the session tab bar to spawn a new session.
The dropdown should show two distinct sections: "Quick Launch" (from config
presets) and "Emerged" (from the action registry), each with a "manage" link.

When no quick launch presets are configured and no emerged actions exist,
both sections show their respective empty states.

When quick launch presets are configured, they appear in the Quick Launch
section and can be clicked to spawn a session immediately.

## Preconditions

- The daemon is running with at least one repository configured
- At least one promptable agent is configured
- At least one quick launch preset is configured (matching the agent name)

## Verifications

- Clicking the "+" button on the session tab bar opens a dropdown
- The dropdown shows "Spawn a session..." at the top
- The dropdown shows a "Quick Launch" section header with a "manage" link
- The dropdown shows an "Emerged" section header with a "manage" link
- Quick launch presets from config appear in the Quick Launch section
- The Emerged section shows "No emerged actions yet" when empty
- Clicking a quick launch preset spawns a session and navigates to it
- Clicking the Quick Launch "manage" link navigates to the config page
