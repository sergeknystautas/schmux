# Remote host connection modal renders without errors

A user wants to connect to a remote host from the spawn page. They click
a remote flavor card, the connection modal opens with an embedded terminal
for provisioning output, and the terminal initializes without throwing any
runtime errors (e.g., xterm.js addon compatibility issues).

## Preconditions

- The daemon is running
- At least one repository is configured
- At least one remote flavor is configured

## Verifications

- The spawn page loads and shows a "+ New [flavor] host" card with "Provision a new instance"
- Clicking the "+ New host" card opens the connection progress modal
- The modal contains a terminal container (dark background div for xterm)
- No uncaught JavaScript errors occur when the modal opens (catches xterm addon init failures)
- The modal header shows the flavor display name
- The modal header shows a status message (e.g., "Provisioning remote host...")
- The modal can be closed via the close button
