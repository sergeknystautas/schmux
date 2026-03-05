# Command spawn triggers immediate WebSocket broadcast

A user spawns a command-based session (e.g., a "shell" quick launch that
runs `zsh`) from the home page and expects to land on the session detail
page within a few seconds.

Previously, command-based spawns did not trigger a WebSocket broadcast,
so the UI had to wait for the next background poll cycle (10+ seconds)
before the session appeared — making it feel unresponsive.

## Preconditions

- The daemon is running
- At least one repository is configured
- A workspace exists with an active session (so the home page shows a workspace card)

## Verifications

- Spawn a command session via the API with a workspace_id
- The session appears in the dashboard WebSocket feed within 3 seconds (not waiting for poll)
- GET /api/sessions confirms the command session exists with target "command"
- The session is running
