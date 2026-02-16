# Remote access PIN security enforcement

A user tries to set weak PINs for remote access and expects the system to
reject them. The server enforces a minimum PIN length of 6 characters on
both the API endpoint and displays the right error.

After setting a valid PIN, the user checks that the tunnel timeout has a
safe default value (120 minutes) even when no explicit timeout is configured.

## Preconditions

- The daemon is running
- No PIN is set initially (fresh config)

## Verifications

- POST /api/remote-access/set-pin with `{"pin": ""}` returns 400
- POST /api/remote-access/set-pin with `{"pin": "abc"}` returns 400 with body containing "at least 6"
- POST /api/remote-access/set-pin with `{"pin": "12345"}` returns 400 (5 chars, still too short)
- POST /api/remote-access/set-pin with `{"pin": "secure123"}` succeeds with `{"ok": true}`
- GET /api/config shows `remote_access.pin_hash_set` is true
- GET /api/config shows `remote_access.timeout_minutes` is 120 (default when not explicitly set)
