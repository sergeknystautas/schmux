#!/bin/bash
# Schmux: gate agent stopping on status update.
# Called by Claude Code Stop hook.
# Reads $SCHMUX_EVENTS_FILE for the last status event.
# Uses JSON decision output to block without "error" labeling.

INPUT=$(cat)

# Prevent infinite loops: if stop_hook_active, just write completed and exit
ACTIVE=$(echo "$INPUT" | jq -r '.stop_hook_active // false')
if [ "$ACTIVE" = "true" ]; then
  [ -n "$SCHMUX_EVENTS_FILE" ] && printf '{"ts":"%s","type":"status","state":"completed","message":""}\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$SCHMUX_EVENTS_FILE" || true
  exit 0
fi

# If not a schmux session, exit cleanly
[ -n "$SCHMUX_EVENTS_FILE" ] || exit 0

# Check for a status event with a meaningful state
if [ -f "$SCHMUX_EVENTS_FILE" ]; then
  LAST_STATE=$(grep '"type":"status"' "$SCHMUX_EVENTS_FILE" | tail -1 | jq -r '.state // ""')
  case "$LAST_STATE" in
    completed|needs_input|needs_testing|error) exit 0 ;;
    working)
      LAST_MSG=$(grep '"type":"status"' "$SCHMUX_EVENTS_FILE" | tail -1 | jq -r '.message // ""')
      [ -n "$LAST_MSG" ] && exit 0 ;;
  esac
fi

jq -n -c '{decision:"block",reason:"Write your status before finishing. Run: printf '\''{{\"ts\":\"%s\",\"type\":\"status\",\"state\":\"STATE\",\"message\":\"what you did\"}\\n}'\'' \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\" >> \"$SCHMUX_EVENTS_FILE\""}' 2>/dev/null \
  || printf '{"decision":"block","reason":"Write your status before finishing."}\n'
exit 0
