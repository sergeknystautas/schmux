# Return to a chat session where you left off

A user reading earlier in a long chat transcript switches to another tab in
the workspace and comes back to the same place. A user following the tail
comes back to the new bottom.

## Preconditions

- The isolated scenario daemon serves the real dashboard.
- Dashboard and chat WebSockets replay controlled harness records, following
  the chat-session-activity pattern: a history of twenty user and assistant
  exchanges ending in a long summary turn, long enough that the transcript
  scrolls in a 900px viewport, plus two further exchanges that arrive only
  after the user has switched to another tab while following the tail.
- The workspace has a diff tab to switch to.
- Run in light theme with reduced motion enabled.

## Verifications

- After scrolling the transcript so that it is detached (Resume visible) at a
  recorded scrollTop, opening the diff tab and returning shows the transcript
  at that same scrollTop and Resume visible.
- Pressing Resume after returning pins the transcript to the bottom and hides
  Resume.
- With the transcript at the bottom and Resume hidden, switching to the diff
  tab, delivering the two live exchanges, and returning shows the last
  exchange in view, pinned to the bottom, with Resume hidden.
- Reloading the page while detached at a recorded scrollTop returns to that
  scrollTop with Resume visible.
