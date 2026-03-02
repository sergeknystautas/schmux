# Scrollback survives sync correction

After a sync correction fires, scrollback history should remain intact.
Only the differing viewport rows are surgically corrected.

## Preconditions

- A session is running that has produced at least 500 lines of output
  (enough to fill scrollback beyond the visible viewport)
- The terminal WebSocket is connected and bootstrapped
- The user has scrolled through or can scroll through the terminal history

## Verifications

- When a sync correction fires (content mismatch detected), only the
  corrupted viewport rows are overwritten
- terminal.reset() is never called from the sync path
- Scrollback lines above the viewport remain accessible and unchanged
- The user can scroll up and see all 500 lines of historical output
  after the correction
