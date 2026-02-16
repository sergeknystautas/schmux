# Remote access PIN authentication

A user wants to set up PIN-based authentication for remote access tunnels
so they can securely access their dashboard from a phone or another device.

First they set a PIN via the API endpoint. After setting the PIN, the
config should reflect that a PIN hash is configured. The dashboard sidebar
should no longer show the "Set a PIN first" warning.

They also try to visit the remote auth page without a valid token
(simulating an expired or bogus link) and expect to see an error page
telling them the link is invalid.

Finally, they try to start a tunnel without a PIN set (before setting one)
and expect the API to reject the request with an error about needing a PIN.

## Preconditions

- The daemon is running
- Remote access is not disabled in config
- No PIN is set initially (fresh config)

## Verifications

- GET /api/config shows `remote_access.pin_hash_set` is false initially
- The dashboard home page shows the Remote Access panel with a "Set a PIN" link
- POST /api/remote-access/set-pin with `{"pin": "test1234"}` succeeds with `{"ok": true}`
- GET /api/config now shows `remote_access.pin_hash_set` is true
- Reloading the dashboard, the "Set a PIN first" warning is no longer visible
- GET /remote-auth without a token query parameter returns an HTML page containing "Invalid or expired link"
- GET /remote-auth?token=bogus-token returns an HTML page containing "Invalid or expired link"
- POST /api/remote-access/on without a PIN set returns 400 with an error mentioning "PIN"
