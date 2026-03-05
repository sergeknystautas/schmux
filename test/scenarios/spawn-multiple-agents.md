# Verify multi-session workspace after API spawn

Two agents are spawned via the API on the same task. The test verifies
that both sessions are navigable, each renders a terminal viewport,
and the API confirms both sessions exist with different targets.

## Preconditions

- The daemon is running
- At least one repository is configured
- At least two promptable agents are configured

## Verifications

- Selecting "Multiple agents" from the agent dropdown expands a multi-agent grid
- Two agents can be selected simultaneously
- The form submits successfully
- The user lands on a session detail page
- Both created sessions are navigable and render terminal output
- GET /api/sessions returns sessions for both agents (across one or more workspaces)
- Each session has a different target
