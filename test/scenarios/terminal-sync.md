# Terminal sync auto-correction

The sync mechanism periodically sends a tmux screen snapshot to the frontend
so xterm.js can detect and correct any rendering desync caused by bootstrap
race conditions.

## Preconditions

- A session is running with a shell that has produced output
- The terminal WebSocket is connected and bootstrapped

## Verifications

- The server sends periodic `sync` messages over the terminal WebSocket
  containing the tmux visible screen and cursor state
- When xterm.js content matches tmux, no correction occurs
- When xterm.js content is corrupted (desynced), the sync mechanism detects
  the mismatch and replays the tmux screen to correct it
- After correction, the terminal content matches tmux's ground truth
- The frontend sends a `syncResult` message back to the server indicating
  whether a correction was applied
