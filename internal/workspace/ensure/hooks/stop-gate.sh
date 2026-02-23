#!/bin/bash
set -euo pipefail
# Schmux lore: gate agent stopping on status update + friction reflection.
# Called by Claude Code Stop hook.
# Combines schmux status signaling with lore reflection requirement.

INPUT=$(cat)

# Prevent infinite loops: if stop_hook_active, just signal completed and exit
ACTIVE=$(echo "$INPUT" | jq -r '.stop_hook_active // false')
if [ "$ACTIVE" = "true" ]; then
  [ -n "${SCHMUX_STATUS_FILE:-}" ] && echo "completed" > "$SCHMUX_STATUS_FILE" || true
  exit 0
fi

# If not a schmux session, exit cleanly
[ -n "${SCHMUX_STATUS_FILE:-}" ] || exit 0

# Check 1: schmux status file updated
STATUS_OK=false
if [ -f "$SCHMUX_STATUS_FILE" ]; then
  CONTENT=$(cat "$SCHMUX_STATUS_FILE" 2>/dev/null || true)
  # Status file must contain something other than just "working" (the SessionStart default)
  case "$CONTENT" in
    completed*|needs_input*|needs_testing*|error*|working\ *) STATUS_OK=true ;;
  esac
fi

# Check 2: reflection entry exists in lore.jsonl from THIS session
# Use CLAUDE_PROJECT_DIR for absolute path; fall back to relative if not set
if [ -n "${CLAUDE_PROJECT_DIR:-}" ]; then
  LORE_FILE="$CLAUDE_PROJECT_DIR/.schmux/lore.jsonl"
else
  LORE_FILE=".schmux/lore.jsonl"
fi
SESSION_ID="${SCHMUX_SESSION_ID:-}"
REFLECTION_OK=false
if [ -f "$LORE_FILE" ] && [ -n "$SESSION_ID" ]; then
  # Check for a reflection entry that belongs to this session
  if grep "\"session\":\"$SESSION_ID\"" "$LORE_FILE" 2>/dev/null | grep -q "\"type\":\"reflection\"" 2>/dev/null; then
    REFLECTION_OK=true
  fi
elif [ -f "$LORE_FILE" ] && [ -z "$SESSION_ID" ]; then
  # Fallback: no session ID available, accept any reflection (backwards compat)
  if grep -q '"type":"reflection"' "$LORE_FILE" 2>/dev/null || grep -q '"type": "reflection"' "$LORE_FILE" 2>/dev/null; then
    REFLECTION_OK=true
  fi
fi

# If both are satisfied, exit cleanly
if [ "$STATUS_OK" = "true" ] && [ "$REFLECTION_OK" = "true" ]; then
  exit 0
fi

# Build error message for missing items
MSG=""
if [ "$STATUS_OK" != "true" ]; then
  MSG="1. Status: printf \"working <summary>\\nintent: <your current goal>\\n\" > \"\$SCHMUX_STATUS_FILE\""
fi
if [ "$REFLECTION_OK" != "true" ]; then
  WS_ID="${SCHMUX_WORKSPACE_ID:-unknown}"
  [ -n "$MSG" ] && MSG="$MSG
"
  MSG="${MSG}2. Lore: Append a friction reflection to .schmux/lore.jsonl — what tripped you up this session.
{\"ts\":\"<ISO8601>\",\"ws\":\"$WS_ID\",\"session\":\"${SESSION_ID:-unknown}\",\"agent\":\"claude-code\",\"type\":\"reflection\",\"text\":\"When <trigger>, do <correction> instead\"}
If nothing tripped you up, use {\"ts\":\"<ISO8601>\",\"ws\":\"$WS_ID\",\"session\":\"${SESSION_ID:-unknown}\",\"agent\":\"claude-code\",\"type\":\"reflection\",\"text\":\"none\"}"
fi

echo "Update your schmux status and write a friction reflection before finishing.
$MSG" >&2
exit 2
