# Spawn a single session

A user wants to start one AI agent working on a task in a fresh workspace.

They navigate to the spawn page, type a task description like
"Add unit tests for the auth module", select one agent, pick a
repository, and submit the form.

After submitting, they should land on a session detail page. The page
should show a terminal viewport and a sidebar with the session's metadata
(nickname, branch, target, status). The terminal should eventually show
some output from the agent process.

Going back to the home page, the workspace should appear in the workspace
list with one session.

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
