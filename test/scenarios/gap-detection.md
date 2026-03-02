# Gap detection and replay

When live terminal frames are dropped (channel backpressure), the frontend
detects the gap via sequence number discontinuity and requests replay from the
server's output log. This recovers lost data without a full re-bootstrap.

## Preconditions

- A session is running with a shell
- The terminal WebSocket is connected and bootstrapped
- The output log is capturing sequenced events on the server

## Verifications

- All binary frames carry an 8-byte big-endian sequence header
- Sequence numbers are monotonically non-decreasing during normal streaming
- The `bootstrapComplete` message is sent after the last bootstrap chunk
- After a massive output flood, the terminal content matches tmux's
  ground truth (gap detection + replay recovers any dropped frames)
- The `stats` message reports `currentSeq` and `logOldestSeq` reflecting
  the output log state
