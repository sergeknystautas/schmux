#!/bin/bash
# Schmux: gate agent stopping on friction reflection.
# Called by Claude Code Stop hook.
# Reads $SCHMUX_EVENTS_FILE for a reflection event.
# Uses JSON decision output to block without "error" labeling.

INPUT=$(cat)

# Prevent infinite loops: if stop_hook_active, exit cleanly
ACTIVE=$(echo "$INPUT" | jq -r '.stop_hook_active // false')
[ "$ACTIVE" = "true" ] && exit 0

# If not a schmux session, exit cleanly
[ -n "$SCHMUX_EVENTS_FILE" ] || exit 0

if grep -q '"type":"reflection"' "$SCHMUX_EVENTS_FILE" 2>/dev/null; then
  exit 0
fi

jq -n -c '{decision:"block",reason:"Write a friction reflection before finishing. Run: printf '\''{{\"ts\":\"%s\",\"type\":\"reflection\",\"text\":\"When <trigger>, do <correction> instead\"}\\n}'\'' \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\" >> \"$SCHMUX_EVENTS_FILE\" — or use text \"none\" if nothing tripped you up."}' 2>/dev/null \
  || printf '{"decision":"block","reason":"Write a friction reflection before finishing."}\n'
exit 0
