# Chat reducer fixtures

`claude/`: cut from `review/claude-chat-probe/probe{2,3,4,5}.out.jsonl` (Claude Code 2.1.261).
`codex/`: cut from `review/codex-chat-probe/` (Codex CLI 0.152.0, app-server protocol).
`codex/actions.out.jsonl`: real session capture containing `commandActions`.
`codex/edits.out.jsonl`: probe capture containing reasoning summaries and `fileChange` items.
`<scenario>.out.jsonl` is what the harness wrote; `<scenario>.in.jsonl` is what the daemon
wrote (handshake, turns, answers). `item/agentMessage/delta` lines are trimmed to four per
item; the reducer takes the final text from `item/completed`, so streamed text in tests is
partial by design.

Unverified against a live harness (rules specified from the schema, covered by hand-written
records in `codex.test.ts`): `mcpToolCall`, `turn/completed` with `failed`, reconnect mid-item,
the logged-in `account/read` shape.
