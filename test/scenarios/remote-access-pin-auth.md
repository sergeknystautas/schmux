# Remote access password authentication

A user wants to set up password-based authentication for remote access tunnels
so they can securely access their dashboard from a phone or another device.

They set a password via the API endpoint. After setting the password, the config
should reflect that a password hash is configured. The dashboard sidebar
should not show the "Set a password first" warning when a password is set.

They also try to visit the remote auth page without a valid token
(simulating an expired or bogus link) and expect to see an error page
telling them the link is invalid.

## Preconditions

- The daemon is running
- Remote access is not disabled in config

## Verifications

- POST /api/remote-access/set-password with `{"password": "test1234"}` succeeds with `{"ok": true}`
- GET /api/config shows `remote_access.password_hash_set` is true after setting password
- Reloading the dashboard, the "Set a password first" warning is no longer visible
- GET /remote-auth without a token query parameter returns an HTML page containing "Check your notification app"
- GET /remote-auth?token=bogus-token returns an HTML page containing "Invalid or expired link"
