#!/bin/bash
# Schmux: capture the harness session id as a resume_id event.
# Called by claude's SessionStart/UserPromptSubmit hooks and codex's
# UserPromptSubmit hook. Reads hook JSON from stdin.
set -euo pipefail
[ -n "${SCHMUX_EVENTS_FILE:-}" ] || exit 0
ID=$(jq -r '.session_id // empty' 2>/dev/null || true)
[ -n "$ID" ] || exit 0
TS=$(date -u +%Y-%m-%dT%H:%M:%SZ)
jq -n -c --arg ts "$TS" --arg id "$ID" '{ts:$ts,type:"resume_id",id:$id}' >> "$SCHMUX_EVENTS_FILE"
exit 0
