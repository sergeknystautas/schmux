# Remote auth browser flow

A user accesses their schmux dashboard remotely through a cloudflare tunnel.
They receive an auth URL (via ntfy notification), open it in a browser,
enter their password, and gain access to the dashboard. This scenario
simulates the tunnel using the dev API and drives the entire flow through
a real browser.

## Preconditions

- The daemon is running in dev mode
- A password "scenariotest123" is set via the API
- A simulated tunnel is activated via POST /api/dev/simulate-tunnel

## Verifications

- POST /api/dev/simulate-tunnel returns a JSON response with non-empty `token` and `url` fields
- Navigating to /remote-auth?token={token} redirects to /remote-auth?nonce={nonce}
- The nonce page shows a password form with a password input field
- The password form has a submit button
- Submitting with wrong password shows "Incorrect password" error text
- The password input is cleared after failed attempt and the form is still visible
- Submitting with correct password "scenariotest123" redirects to /
- After successful auth, the dashboard loads and shows the session list (or empty state)
- The browser has a "schmux_remote" cookie set
- Navigating to /api/healthz returns 200 (authenticated API access works)
- After stopping the simulated tunnel via POST /api/dev/simulate-tunnel-stop, the remote cookie no longer grants API access from a fresh browser context (cookie is server-invalidated)
