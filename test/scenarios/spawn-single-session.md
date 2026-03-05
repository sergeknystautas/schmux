# Verify session detail page after API spawn

A session is spawned via the API and the test verifies that the session
detail page renders correctly: terminal viewport visible, sidebar present,
status showing "Running", and the API confirming the session was created
with the correct target.

## Preconditions

- The daemon is running
- At least one repository is configured
- At least one promptable agent is configured (can be a simple echo command for testing)

## Verifications

- The spawn page loads and shows the prompt textarea
- The form submits without error after filling prompt, selecting an agent, and picking a repo
- The user lands on a session detail page (URL matches /sessions/:id)
- The terminal viewport is visible
- The session sidebar shows the correct target name
- The session status shows as "running"
- GET /api/sessions returns one workspace with one session
- The session in the API response has the correct target
