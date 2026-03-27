# Xterm title updates propagate to dashboard tabs

When a terminal program (e.g., Claude Code) sets the xterm window title via
OSC 0/2 escape sequences, the schmux dashboard should reflect that title in
the session tab — falling back from nickname to xterm_title to target name.

## Preconditions

- The daemon is running
- At least one repository is configured
- A workspace exists with a running session

## Verifications

- PUT /api/sessions-xterm-title/{sessionID} with a title updates the session state
- The updated title appears in the GET /api/sessions response as xterm_title
- A second PUT with the same title returns 200 (idempotent)
- PUT with an empty title clears the xterm_title field
- PUT with a missing session ID returns 400
- Nickname takes priority: if both nickname and xterm_title are set, the API returns both but the nickname is the display name
