#!/bin/bash
# Schmux lore: capture tool failures as structured lore entries.
# Called by Claude Code PostToolUseFailure hook.
# Reads JSON from stdin with: tool_name, tool_input, error, is_interrupt

set -euo pipefail

INPUT=$(cat)

# Skip user interrupts — not real failures
IS_INTERRUPT=$(echo "$INPUT" | jq -r '.is_interrupt // false')
[ "$IS_INTERRUPT" = "true" ] && exit 0

TOOL=$(echo "$INPUT" | jq -r '.tool_name // "unknown"')
ERROR=$(echo "$INPUT" | jq -r '.error // ""' | head -c 500)

# Skip empty errors
[ -z "$ERROR" ] && exit 0

# Extract input summary based on tool type
case "$TOOL" in
  Bash)
    INPUT_SUMMARY=$(echo "$INPUT" | jq -r '.tool_input.command // ""' | head -c 300)
    ;;
  Read|Edit|Write|Glob)
    INPUT_SUMMARY=$(echo "$INPUT" | jq -r '.tool_input.file_path // .tool_input.pattern // ""' | head -c 300)
    ;;
  Grep)
    INPUT_SUMMARY=$(echo "$INPUT" | jq -r '.tool_input.pattern // ""' | head -c 300)
    ;;
  *)
    INPUT_SUMMARY=$(echo "$INPUT" | jq -r '.tool_input' | head -c 200)
    ;;
esac

# Classify error category
CATEGORY="other"
case "$ERROR" in
  *"No such file"*|*"not found"*|*"does not exist"*|*"ENOENT"*)
    CATEGORY="not_found" ;;
  *"permission denied"*|*"EACCES"*|*"Permission denied"*)
    CATEGORY="permission" ;;
  *"syntax error"*|*"SyntaxError"*|*"parse error"*|*"unexpected token"*)
    CATEGORY="syntax" ;;
  *"command not found"*|*"Missing script"*|*"not recognized"*)
    CATEGORY="wrong_command" ;;
  *"build failed"*|*"compilation"*|*"cannot find module"*|*"undefined:"*|*"does not compile"*)
    CATEGORY="build_failure" ;;
  *"FAIL"*|*"assertion"*|*"expected"*|*"test failed"*)
    CATEGORY="test_failure" ;;
  *"timeout"*|*"timed out"*|*"deadline exceeded"*)
    CATEGORY="timeout" ;;
esac

# Read workspace and session IDs from env (set by schmux at session spawn)
WS_ID="${SCHMUX_WORKSPACE_ID:-unknown}"
SESSION_ID="${SCHMUX_SESSION_ID:-unknown}"

# Build and append the lore entry
# Use CLAUDE_PROJECT_DIR for absolute path; fall back to relative if not set
if [ -n "$CLAUDE_PROJECT_DIR" ]; then
  LORE_FILE="$CLAUDE_PROJECT_DIR/.schmux/lore.jsonl"
else
  LORE_FILE=".schmux/lore.jsonl"
fi
mkdir -p "$(dirname "$LORE_FILE")"

TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

jq -n -c \
  --arg ts "$TIMESTAMP" \
  --arg ws "$WS_ID" \
  --arg session "$SESSION_ID" \
  --arg tool "$TOOL" \
  --arg input_summary "$INPUT_SUMMARY" \
  --arg error_summary "$ERROR" \
  --arg category "$CATEGORY" \
  '{ts: $ts, ws: $ws, session: $session, agent: "claude-code", type: "failure", tool: $tool, input_summary: $input_summary, error_summary: $error_summary, category: $category}' \
  >> "$LORE_FILE"

exit 0
