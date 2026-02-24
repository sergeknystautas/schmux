# Spawn multiple agents on the same task

A user wants to start two AI agents working on the same task so they can
compare approaches.

They navigate to the spawn page, type a task description, select
"Multiple agents" from the bottom of the agent dropdown to expand the
multi-agent grid, select two agents, pick a repository, and submit
the form.

After submitting, they should land on a session detail page for one of the
new sessions. They should also be able to navigate to both created sessions
and see terminal output for each.

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
