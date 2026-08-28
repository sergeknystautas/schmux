#!/bin/bash
# stop-autolearn-check.sh — gates agent stop on friction reflection in event file.
# Reads from $SCHMUX_EVENTS_FILE (per-session append-only JSONL).
INPUT=$(cat)
ACTIVE=$(echo "$INPUT" | jq -r '.stop_hook_active // false')
[ "$ACTIVE" = "true" ] && exit 0
[ -n "${SCHMUX_EVENTS_FILE:-}" ] || exit 0

# Autolearn can be toggled at runtime; honor the current config on every
# invocation instead of only at hook install time. A missing or unreadable
# config means enabled (preserve previous behavior).
CONFIG_FILE="${SCHMUX_CONFIG_FILE:-$HOME/.schmux/config.json}"
if [ -f "$CONFIG_FILE" ]; then
  ENABLED=$(jq -r '(.autolearn // .lore // {}).enabled != false' "$CONFIG_FILE" 2>/dev/null || echo true)
  [ "$ENABLED" = "false" ] && exit 0
fi

if grep -q '"type":"reflection"' "$SCHMUX_EVENTS_FILE" 2>/dev/null; then
  exit 0
fi

# The reason text contains a JSON example, so it must be encoded by jq rather
# than concatenated into the payload — otherwise its quotes terminate the
# reason string early and Claude Code discards the block decision.
REASON=$(cat <<'EOF'
Write a friction reflection before finishing. Report what tripped you up: echo '{"ts":"...","type":"reflection","text":"When X, do Y instead"}' >> "$SCHMUX_EVENTS_FILE"
EOF
)
jq -nc --arg reason "$REASON" '{decision:"block",reason:$reason}'
exit 0
