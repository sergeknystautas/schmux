# Remote access onboarding

A user wants to set up remote access from scratch. They navigate to the
config page, set a password, generate a secure ntfy topic, send a test
notification, and then start the tunnel. The flow should work end-to-end
without requiring a daemon restart.

## Preconditions

- The daemon is running
- At least one repository is configured
- No remote access password has been set yet (password_hash_set is false)

## Verifications

- GET /api/config shows remote_access.password_hash_set is false initially
- The config page Access tab shows the "Set a password first" warning in the Remote Access section
- The "Start" button in the sidebar remote access panel is present
- POST /api/remote-access/set-password with `{"password": "mypassword123"}` succeeds with `{"ok": true}`
- GET /api/config after setting password shows remote_access.password_hash_set is true
- Reloading the dashboard, the "Set a password first" warning is no longer visible
- Local API requests still work after setting password — GET /api/config returns 200 (not 401)
- The config page Access tab has an ntfy topic input field
- The "Generate secure topic" button is visible
- Clicking "Generate secure topic" populates the ntfy topic input with a value matching /^schmux-[0-9a-f]{32}$/
- A QR code (SVG element) appears after generating the topic
- The "Send test notification" button is disabled when no ntfy topic is configured (empty input)
- After generating a topic and saving config (POST /api/config with remote_access.notify.ntfy_topic set), the "Send test notification" button is enabled
- POST /api/remote-access/test-notification returns 200 when ntfy topic is configured
- POST /api/remote-access/test-notification returns 400 when ntfy topic is not configured
- The password strength indicator appears when typing a password of 6+ characters
- Short passwords (< 6 chars) are rejected by POST /api/remote-access/set-password with 400
