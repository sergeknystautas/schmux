#!/bin/bash
# stop-status-check.sh — gates agent stop on status event in event file.
# Reads from $SCHMUX_EVENTS_FILE (per-session append-only JSONL).
INPUT=$(cat)
ACTIVE=$(echo "$INPUT" | jq -r '.stop_hook_active // false')
[ "$ACTIVE" = "true" ] && exit 0
[ -n "${SCHMUX_EVENTS_FILE:-}" ] || exit 0

if [ -f "$SCHMUX_EVENTS_FILE" ]; then
  # The "schmux: signaling" Stop hook appends an idle heartbeat before this
  # gate runs, so the newest status event is always idle. Skip idle events and
  # judge the last status the agent actually reported. -R/fromjson? keeps a
  # malformed line from aborting the whole scan.
  LAST=$(grep '"type":"status"' "$SCHMUX_EVENTS_FILE" |
    jq -Rc 'fromjson? | select(.state != "idle")' | tail -1)
  LAST_STATE=$(printf '%s' "$LAST" | jq -r '.state // ""' 2>/dev/null)
  case "$LAST_STATE" in
    completed|needs_input|needs_testing|error) exit 0 ;;
    working)
      LAST_MSG=$(printf '%s' "$LAST" | jq -r '.message // ""' 2>/dev/null)
      [ -n "$LAST_MSG" ] && exit 0 ;;
  esac
fi

# Encode with jq: the reason embeds a JSON example whose quotes would break a
# concatenated payload, and Claude Code drops block decisions it cannot parse.
REASON=$(cat <<'EOF'
Write your status before finishing. Use schmux_status to report: echo '{"ts":"...","type":"status","state":"completed","message":"what you did"}' >> "$SCHMUX_EVENTS_FILE"
EOF
)
jq -nc --arg reason "$REASON" '{decision:"block",reason:$reason}'
exit 0
