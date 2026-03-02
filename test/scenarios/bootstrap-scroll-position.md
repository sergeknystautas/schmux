# Bootstrap renders at correct scroll position

When navigating to a session with substantial output, the terminal should
render at the correct scroll position without visible scrolling artifacts.

## Preconditions

- A session is running that has produced at least 2000 lines of output
- The output log has captured the full history of output events

## Verifications

- When navigating to the session page, the terminal viewport shows the
  cursor position (bottom of output) within 2 seconds
- No visible "scrolling through thousands of lines" animation occurs
- The bootstrap uses the output log replay (not capture-pane) when
  the log has data
- Bootstrap data is sent in chunked frames (~16KB each) with sequence
  headers
- scrollToBottom fires only after the terminal write callback completes,
  not synchronously during write
