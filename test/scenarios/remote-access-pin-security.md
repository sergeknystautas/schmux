# Remote access password security enforcement

A user tries to set weak passwords for remote access and expects the system to
reject them. The server enforces a minimum password length of 6 characters on
both the API endpoint and displays the right error.

After setting a valid password, the user checks that the tunnel timeout has a
safe default value (120 minutes) even when no explicit timeout is configured.

## Preconditions

- The daemon is running
- No password is set initially (fresh config)

## Verifications

- POST /api/remote-access/set-password with `{"password": ""}` returns 400
- POST /api/remote-access/set-password with `{"password": "abc"}` returns 400 with body containing "at least 6"
- POST /api/remote-access/set-password with `{"password": "12345"}` returns 400 (5 chars, still too short)
- POST /api/remote-access/set-password with `{"password": "secure123"}` succeeds with `{"ok": true}`
- GET /api/config shows `remote_access.password_hash_set` is true
- GET /api/config shows `remote_access.timeout_minutes` is 120 (default when not explicitly set)
