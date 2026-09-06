# Codex hook payloads

Captured with Codex CLI 0.152.0: a temporary dump-to-file handler per event was
merged into `~/.codex/hooks.json`, one short session was run with
`codex exec --dangerously-bypass-hook-trust` (so the probe handlers ran without
the trust review), and the file was restored from its backup. Values below are
trimmed; `session_id`, paths, and ids are examples. Field names match the Codex
hooks crate (`codex-rs/hooks/src/events/*.rs`).

```json
SessionStart: {"session_id":"s","transcript_path":"...","cwd":"...","hook_event_name":"SessionStart","model":"gpt-5.6-terra","permission_mode":"bypassPermissions","source":"startup"}
UserPromptSubmit: {"session_id":"s","turn_id":"t","cwd":"...","hook_event_name":"UserPromptSubmit","prompt":"..."}
PostToolUse: {"session_id":"s","turn_id":"t","hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"false"},"tool_response":"","tool_use_id":"x"}
Stop: {"session_id":"s","turn_id":"t","hook_event_name":"Stop","stop_hook_active":false,"last_assistant_message":"DONE"}
SessionEnd: {"session_id":"s","transcript_path":"...","cwd":"...","hook_event_name":"SessionEnd","reason":"other"}
```

## What `tool_response` is

For the shell tool (`tool_name` "Bash") `tool_response` is a **string**: the
command's output text, truncated to the model's output budget, and nothing
else. The exit code is not in it; Codex puts "Process exited with code N" in
the header it sends to the model, not in the hook payload
(`codex-rs/core/src/tools/context.rs`, `ExecCommandToolOutput::post_tool_use_response`).
So `capture-failure-codex.sh` can only recognize a failure from the output
text, using the same category patterns as Claude's `capture-failure.sh`; a
command that fails silently (`false`, a non-zero exit with no message) writes
nothing. MCP and other structured tools pass their result JSON, which the
script checks for an `error` field.

`PermissionRequest` does not fire under non-interactive `codex exec`; its fields
are `tool_name` and `tool_input` (`events/permission_request.rs`), confirmed live
only once the hook groups are trusted.

## Trust

After schmux merges its groups into `~/.codex/hooks.json`, a Codex TUI session
must be opened once and the hook review accepted before the new groups run.
The pre-existing capture group keeps its index in `UserPromptSubmit`, so
`resume_id` capture keeps working in the meantime.
