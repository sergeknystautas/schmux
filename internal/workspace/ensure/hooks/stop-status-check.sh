#!/bin/bash
# stop-status-check.sh — gates agent stop on status event in event file.
# Reads from $SCHMUX_EVENTS_FILE (per-session append-only JSONL).
INPUT=$(cat)
ACTIVE=$(echo "$INPUT" | jq -r '.stop_hook_active // false')
[ "$ACTIVE" = "true" ] && exit 0
[ -n "${SCHMUX_EVENTS_FILE:-}" ] || exit 0

if [ -f "$SCHMUX_EVENTS_FILE" ]; then
  LAST_STATE=$(grep '"type":"status"' "$SCHMUX_EVENTS_FILE" | tail -1 | jq -r '.state // ""')
  case "$LAST_STATE" in
    completed|needs_input|needs_testing|error) exit 0 ;;
    working)
      LAST_MSG=$(grep '"type":"status"' "$SCHMUX_EVENTS_FILE" | tail -1 | jq -r '.message // ""')
      [ -n "$LAST_MSG" ] && exit 0 ;;
  esac
fi

printf '{"decision":"block","reason":"Write your status before finishing. Use schmux_status to report: echo '\''{"ts":"...","type":"status","state":"completed","message":"what you did"}'\'' >> \"$SCHMUX_EVENTS_FILE\""}\n'
exit 0
