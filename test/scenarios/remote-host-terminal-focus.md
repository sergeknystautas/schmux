# Remote host provisioning terminal holds focus for YubiKey auth

When provisioning a remote host, the terminal in the connection modal must
hold keyboard focus so that YubiKey OTP output reaches the PTY via WebSocket.
If focus lands on the close button or another element instead, the OTP
characters are lost and SSH auth times out.

## Preconditions

- The daemon is running
- At least one repository is configured
- At least one remote flavor is configured

## Verifications

- Opening the connection modal auto-focuses the xterm terminal (not the close button)
- The xterm helper textarea is the active element after the modal renders
- Clicking the modal header moves focus away from the terminal
- Clicking the modal body re-focuses the terminal
