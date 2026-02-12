# Spawn multiple agents on the same task

A user wants to start two AI agents working on the same task so they can
compare approaches.

They navigate to the spawn page, type a task description, switch to
"Multiple" agent selection mode, select two agents, pick a repository,
and submit the form.

After submitting, they should land on a session detail page. The workspace
tabs should show two session tabs (one per agent). Clicking each tab should
show a different session with its own terminal output.

## Preconditions

- The daemon is running
- At least one repository is configured
- At least two promptable agents are configured

## Verifications

- The agent selection mode can be switched to "Multiple"
- Two agents can be selected simultaneously
- The form submits successfully
- The user lands on a session detail page
- The workspace tabs show two session tabs
- Clicking each tab navigates to a different session (URL changes)
- GET /api/sessions returns one workspace with two sessions
- Each session has a different target
