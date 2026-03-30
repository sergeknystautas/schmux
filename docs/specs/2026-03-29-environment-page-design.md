# Environment Page

## Problem

When shell profile changes (`.zshrc`, `.zprofile`), the tmux server retains its original environment. New sessions spawned by schmux inherit stale values. The only current fix is killing the tmux server, which destroys all running sessions.

## Solution

A new dashboard page that compares the current system environment against the tmux server environment, letting you sync individual variables so new sessions pick up the changes.

## How It Works

On page load, the backend:

1. Spawns a fresh login shell (`env -i HOME=$HOME USER=$USER SHELL=$SHELL TERM=xterm-256color $SHELL -l -c env`) to capture the current system environment (10-second timeout; uses `$SHELL` to respect the user's configured login shell)
2. Reads the tmux server environment (`tmux show-environment -g`)
3. Filters out blocked keys
4. Compares the two and returns a list of keys with their status

The comparison runs once when the page is opened. It does not poll or auto-refresh.

## Page Layout

**Navigation:** After Remote Hosts, before Tips. Icon from https://www.svgrepo.com/svg/376036/environment. Label: "Environment".

**Main table** with three columns:

| Key        | Status      | Action |
| ---------- | ----------- | ------ |
| PATH       | differs     | [Sync] |
| GOPATH     | in sync     | --     |
| NVM_DIR    | system only | [Sync] |
| LEGACY_VAR | tmux only   | --     |

**Statuses:**

- **in sync** -- key exists in both, values match
- **differs** -- key exists in both, values don't match
- **system only** -- exists in fresh shell but not in tmux server
- **tmux only** -- exists in tmux server but not in fresh shell

**Actions:**

- Sync button for "differs" and "system only" -- sets tmux server env to the system value
- No action for "in sync" or "tmux only"

No values are shown anywhere. Keys and status only.

**Below the table:** informational section listing which keys are blocked and why.

## Blocked Keys

Hardcoded blocklist of keys excluded from the table:

- **tmux-internal:** TMUX, TMUX_PANE
- **session-transient:** SHLVL, PWD, OLDPWD, \_, COLUMNS, LINES, TMPDIR
- **terminal-specific:** TERM_SESSION_ID, ITERM_SESSION_ID, WINDOWID, STY, WINDOW, COLORTERM, COLOR, TERM_PROGRAM, TERM_PROGRAM_VERSION, TERMINFO
- **terminal-emulator:** GHOSTTY*\* (prefix match), ITERM*\* (prefix match)
- **macOS session/system:** LaunchInstanceID, SECURITYSESSIONID, XPC_FLAGS, XPC_SERVICE_NAME, **CFBundleIdentifier, **CF_USER_TEXT_ENCODING, OSLogRateLimit, COMMAND_MODE
- **npm pollution:** npm\_\* (prefix match), INIT_CWD, NODE
- **session-specific:** SSH_AUTH_SOCK, EDITOR

## API

**`GET /api/environment`**

Returns the comparison. Response:

```json
{
  "vars": [
    { "key": "PATH", "status": "differs" },
    { "key": "GOPATH", "status": "in_sync" },
    { "key": "NVM_DIR", "status": "system_only" },
    { "key": "LEGACY_VAR", "status": "tmux_only" }
  ],
  "blocked": [
    "TMUX",
    "TMUX_PANE",
    "SHLVL",
    "PWD",
    "OLDPWD",
    "_",
    "COLUMNS",
    "LINES",
    "TERM_SESSION_ID",
    "ITERM_SESSION_ID",
    "WINDOWID",
    "STY",
    "WINDOW"
  ]
}
```

**`POST /api/environment/sync`**

Request: `{ "key": "PATH" }`

Spawns a fresh login shell to get the current value for that key, then calls `tmux set-environment -g KEY VALUE`. Returns 204 on success.

After a successful sync, the frontend re-calls `GET /api/environment` to update the table.

## Frontend

Single route component at `/environment`. Follows the existing admin page pattern (RemoteSettingsPage):

- `useEffect` on mount calls `GET /api/environment`
- Loading spinner while waiting
- Table renders the response
- Sync buttons call `POST /api/environment/sync`, then reload the table
- Below the table, blocked keys section
