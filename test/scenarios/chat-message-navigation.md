# Navigate a chat transcript with the keyboard

A user reading a long chat transcript wants to scroll back through their own
messages without touching the mouse, without losing their place in the
composer. Each step is measured from where the transcript is scrolled, so a
manual scroll is honored. Stepping past the most recent message returns to the
latest output, and a stepped-to position survives a reload.

## Preconditions

- The isolated scenario daemon serves the real dashboard.
- Dashboard and chat WebSockets replay controlled harness records, following
  the chat-session-activity pattern: a history of twenty user and assistant
  exchanges ending in a long summary turn, long enough that the transcript
  scrolls in a 900px viewport.
- A draft is typed in the composer and the caret is left mid-draft.
- Run in light theme with reduced motion enabled.

## Verifications

- From the bottom, `Control+BracketLeft` puts the most recent user message's
  top at the top of the transcript and the Resume control appears. The
  composer keeps the focus, the draft, and the caret position.
- Two more `Control+BracketLeft` presses put the third-most-recent user
  message at the top of the transcript.
- After scrolling by hand so the tenth user message sits just above the top,
  `Control+BracketLeft` puts the tenth message at the top, and
  `Control+BracketRight` then puts the eleventh at the top.
- After scrolling by hand just past the nineteenth message,
  `Control+BracketRight` puts the twentieth at the top, and one more press
  returns to the very bottom with Resume hidden. The composer still has the
  focus, the draft, and the same caret position.
- From the bottom, stepping back to the third-most-recent message and
  reloading restores the transcript to that message's scroll position, with
  Resume visible.
