# Terminal scroll position survives nudge-triggered resize

When a status event fires (e.g., Claude asks a question), the session tab
bar may grow (nudge text appears in row2), shrinking the terminal container.
The terminal viewport must stay at the bottom — followTail must not be
falsely disabled by the resize-induced scroll events.

## Preconditions

- A session is running with a shell agent
- The terminal WebSocket is connected and bootstrapped
- The terminal has produced enough output to have scrollback

## Verifications

- After generating substantial output, the viewport is at the bottom
  (viewportY >= baseY)
- When the terminal container is resized (simulating the nudge-triggered
  layout change), the viewport remains at the bottom within 500ms
- followTail stays true after the resize — it is not falsely disabled
  by scroll events from the buffer adjustment
- The terminal content matches tmux after the resize settles
