# Remote auth lockout after failed PIN attempts

A user visits the remote auth page with a valid token but enters the wrong
PIN repeatedly. After 5 failed attempts, the token is invalidated and the
link is locked — the user sees a lockout message and must restart the
tunnel to get a new link.

This scenario simulates the flow by calling the API directly since we
cannot start a real tunnel in the test environment. It uses the internal
remote-auth endpoint to verify lockout behavior.

## Preconditions

- The daemon is running
- A PIN is set (e.g. "testpin123")

## Verifications

- GET /remote-auth without a token shows "Invalid or expired link"
- GET /remote-auth?token=fake-token shows "Invalid or expired link" (no form shown)
- POST /remote-auth with an invalid token returns HTML containing "Invalid or expired link"
- POST /remote-auth with a valid-looking but wrong token and wrong PIN returns HTML containing "Invalid or expired link"
