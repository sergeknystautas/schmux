#!/bin/bash
# stop-lore-check.sh — gates agent stop on friction reflection in event file.
# Reads from $SCHMUX_EVENTS_FILE (per-session append-only JSONL).
INPUT=$(cat)
ACTIVE=$(echo "$INPUT" | jq -r '.stop_hook_active // false')
[ "$ACTIVE" = "true" ] && exit 0
[ -n "${SCHMUX_EVENTS_FILE:-}" ] || exit 0

if grep -q '"type":"reflection"' "$SCHMUX_EVENTS_FILE" 2>/dev/null; then
  exit 0
fi

printf '{"decision":"block","reason":"Write a friction reflection before finishing. Report what tripped you up: echo '\''{"ts":"...","type":"reflection","text":"When X, do Y instead"}'\'' >> \"$SCHMUX_EVENTS_FILE\""}\n'
exit 0
