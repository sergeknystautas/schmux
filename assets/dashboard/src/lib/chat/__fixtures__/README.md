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

`claude/activity-{checklist,background,agent}.jsonl`: cut from `review/chat-session-activity-probe/claude/*-fixture.jsonl`
(Claude Code 2.1.266). The Claude captures cover the activity/checklist
fixture surfaces: `activity-checklist.jsonl` (TaskCreate / TaskUpdate structured
results via the `tool_use_result` envelope), `activity-background.jsonl`
(background task lifecycle: snapshot → task_started → snapshot removal →
task_updated → task_notification, with the notification arriving before the
parent `result:success`), `activity-agent.jsonl` (async Agent launch with
`isAsync: true`, `status: "async_launched"`, `agentId` equal to the task id).
Each cut preserves ID relationships and event order; timestamps are uniform
and unrelated to the original capture (use the activity state, not real time,
in tests).

`codex/activity-live.jsonl`: successful 2026-09-09 app-server probe with Codex
0.153.4 and the configured `gpt-5.6-terra` model. Selected notifications retain
their wire order, IDs, and original `emittedAtMs` timestamps. The user home path
is replaced with `/home/probe`. Contains one child's `subAgentActivity` launch
and completion, interleaved parent/child turns and final messages, synchronous
hooks, MCP startup, and an empty-receiver `collabAgentToolCall` wait. The test
adds the application's user record; the harness events are not fabricated.
No plan-update event was emitted (the model reported `update_plan` unavailable).
Populated collaboration snapshots and plan updates remain schema-tested only.

`claude/activity-heartbeats.jsonl`: selected durable records from the reported
bach-godot-004 session on 2026-09-10 (UTC). Retains original IDs and timestamps,
launch input, three `heartbeat: true` events with distinct event IDs and a shared
parent tool ID, and the terminal notification. Unrelated envelope fields are
omitted. The test adds a synthetic user message to open the turn.
