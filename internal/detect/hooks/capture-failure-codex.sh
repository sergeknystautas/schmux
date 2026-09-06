#!/bin/bash
# Schmux events: capture Codex PostToolUse failures as structured events.
# Codex fires PostToolUse for every tool, success or not. For the shell tool
# (tool_name "Bash") tool_response is the command's output text and nothing
# else: no exit code (it goes to the model, not the hook), so a silently
# failing command is indistinguishable from a successful one here. Failure is
# therefore inferred from the output text using the same categories Claude's
# capture-failure.sh assigns; a command whose output matches none of them
# writes nothing. Object responses (MCP tools) are checked for an error field.
# See README-codex-payloads.md.

set -euo pipefail

INPUT=$(cat)

# Autolearn can be toggled at runtime; honor the current config on every
# invocation instead of only at hook install time. A missing or unreadable
# config means enabled (preserve previous behavior).
CONFIG_FILE="${SCHMUX_CONFIG_FILE:-$HOME/.schmux/config.json}"
if [ -f "$CONFIG_FILE" ]; then
  ENABLED=$(jq -r '(.autolearn // .lore // {}).enabled != false' "$CONFIG_FILE" 2>/dev/null || echo true)
  [ "$ENABLED" = "false" ] && exit 0
fi

TOOL=$(printf '%s' "$INPUT" | jq -r '.tool_name // "unknown"')
KIND=$(printf '%s' "$INPUT" | jq -r '.tool_response | type' 2>/dev/null || echo null)

case "$KIND" in
  string)
    # Shell output text. Failure is whatever the category patterns recognize.
    ERROR=$(printf '%s' "$INPUT" | jq -r '.tool_response' | head -c 500)
    ;;
  object)
    # MCP and other structured tools: an error field, or a non-zero exit code
    # when a tool happens to report one.
    FAILED=$(printf '%s' "$INPUT" | jq -r '
      ((.tool_response.exit_code // 0) != 0) or
      (((.tool_response.error // "") | tostring) != "") or
      (.tool_response.isError == true)' 2>/dev/null || echo false)
    [ "$FAILED" = "true" ] || exit 0
    ERROR=$(printf '%s' "$INPUT" | jq -r '.tool_response.error // .tool_response.stderr // .tool_response.output // (.tool_response | tostring)' | head -c 500)
    [ -n "$ERROR" ] || ERROR="tool response indicated failure"
    ;;
  *)
    exit 0
    ;;
esac

INPUT_SUMMARY=$(printf '%s' "$INPUT" | jq -r '.tool_input.command // .tool_input.path // .tool_input.file_path // (.tool_input | tostring)' | head -c 300)

CATEGORY="other"
case "$ERROR" in
  *"No such file"*|*"not found"*|*"does not exist"*|*"ENOENT"*) CATEGORY="not_found" ;;
  *"permission denied"*|*"EACCES"*|*"Permission denied"*) CATEGORY="permission" ;;
  *"syntax error"*|*"SyntaxError"*|*"parse error"*|*"unexpected token"*) CATEGORY="syntax" ;;
  *"command not found"*|*"Missing script"*|*"not recognized"*) CATEGORY="wrong_command" ;;
  *"build failed"*|*"compilation"*|*"cannot find module"*|*"undefined:"*|*"does not compile"*) CATEGORY="build_failure" ;;
  *"FAIL"*|*"assertion"*|*"expected"*|*"test failed"*) CATEGORY="test_failure" ;;
  *"timeout"*|*"timed out"*|*"deadline exceeded"*) CATEGORY="timeout" ;;
esac

# Shell output with no recognizable error is not evidence of failure.
if [ "$KIND" = "string" ] && [ "$CATEGORY" = "other" ]; then
  exit 0
fi

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
