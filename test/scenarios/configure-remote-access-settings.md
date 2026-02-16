# Configure remote access settings

A user wants to configure remote access from the web dashboard instead
of the CLI. They navigate to the config page, open the Access tab, and
configure the tunnel settings — PIN, ntfy topic, timeout, and notify
command.

## Preconditions

- The daemon is running
- At least one repository is configured

## Verifications

- The config page loads and the "Access" tab is accessible at /config?tab=access
- The Access tab contains three sections: Network, Remote Access, and Authentication
- The Remote Access section has an "Enable remote access" checkbox (checked by default)
- Unchecking "Enable remote access" hides the PIN, timeout, ntfy, and command fields
- Re-checking it shows them again
- The PIN field shows "PIN is configured" or not based on pin_hash_set from GET /api/config
- Typing a PIN reveals the confirm field and a "Set PIN" button
- Entering mismatched PINs shows "PINs do not match" error
- Entering matching PINs (at least 6 chars) and clicking "Set PIN" calls POST /api/remote-access/set-pin
- After setting PIN, the status text changes to "PIN is configured"
- Setting ntfy topic to "test-topic", timeout to 30, and notify command to "echo test" then saving persists via POST /api/config
- GET /api/config after save shows remote_access.notify.ntfy_topic="test-topic", remote_access.timeout_minutes=30, remote_access.notify.command="echo test"
- The Network and Authentication sections are present in the Access tab (moved from Advanced)
- The Advanced tab no longer contains Network or Authentication sections
