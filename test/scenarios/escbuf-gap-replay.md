# Escbuf holdback & gap replay

When `escbuf.SplitClean` holds back the entire event (partial ANSI escape at
the frame boundary), the backend now sends an empty-data frame to preserve
sequence number continuity. Gap replay sends individual per-entry frames
instead of concatenated chunks, enabling correct per-seq dedup on the
frontend. These tests validate both fixes end-to-end.

## Preconditions

- A session is running with a shell
- The terminal WebSocket is connected and bootstrapped
- `window.__schmuxStream` is exposed (dev/test mode) with diagnostics enabled
- The output log is capturing sequenced events on the server

## Verifications

- ANSI-heavy output produces no phantom gaps (empty-data frames preserve seq continuity)
- Sequence numbers are strictly contiguous during ANSI output (no seq gaps despite escbuf holdback)
- After a massive ANSI-colored flood, gap replay frames deduplicate correctly (`gapReplayWritten === 0`) and terminal content matches tmux ground truth
- Rapid cursor-positioning output during a flood does not cause desync (the original cursor-jump bug scenario); visible screen and cursor position match tmux
