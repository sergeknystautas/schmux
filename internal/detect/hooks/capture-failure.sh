#!/bin/bash
# Schmux events: capture tool failures as structured failure events.
# Called by Claude Code PostToolUseFailure hook.
# Reads JSON from stdin with: tool_name, tool_input, error, is_interrupt
# Writes failure event to $SCHMUX_EVENTS_FILE (per-session append-only JSONL).

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

# Output: append failure event to session event file
[ -n "${SCHMUX_EVENTS_FILE:-}" ] || exit 0

TS=$(date -u +%Y-%m-%dT%H:%M:%SZ)
jq -n -c \
  --arg ts "$TS" \
  --arg tool "$TOOL" \
  --arg input "$INPUT_SUMMARY" \
  --arg error "$ERROR" \
  --arg category "$CATEGORY" \
  '{ts: $ts, type: "failure", tool: $tool, input: $input, error: $error, category: $category}' \
  >> "$SCHMUX_EVENTS_FILE"

exit 0
