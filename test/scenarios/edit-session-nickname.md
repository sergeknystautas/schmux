# Edit a session's nickname

A user wants to give a session a descriptive nickname so they can identify it
easily among multiple sessions.

They navigate to a session's detail page, click the edit button next to the
nickname field in the sidebar, type a new nickname in the prompt dialog, and
confirm.

The nickname should update immediately in the sidebar and in the workspace tabs.

## Preconditions

- The daemon is running with a spawned session

## Verifications

- The nickname area is visible in the session sidebar
- Clicking the edit button opens a prompt dialog
- Typing a new nickname and confirming updates the sidebar display
- The workspace tabs reflect the updated nickname
- PUT /api/sessions-nickname/:id accepts the new nickname
- GET /api/sessions returns the session with the updated nickname
