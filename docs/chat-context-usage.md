# Chat Context Usage

## What it does

Surfaces the live tokens-in-context reported by Claude and Codex chat harnesses in the session sidebar, prefixed to the model's known context window maximum. The sidebar reads `272K / 1000K tokens` instead of `1000K tokens` once the harness has reported a value.

## Key files

| File                                                      | Purpose                                                                                       |
| --------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `internal/usage/context.go`                               | `ParseClaudeContextTokens`, `ParseCodexContextTokens` — protocol-specific line parsers        |
| `internal/usage/context_test.go`                          | Table-driven parser tests against real harness line shapes                                    |
| `internal/usage/parse_test.go`                            | Fixture-based integration tests using captured `out.jsonl` lines                              |
| `internal/dashboard/chat_context.go`                      | `chatContextUsage` — mutex-guarded `sessionID → tokens` map                                   |
| `internal/dashboard/chat_context_test.go`                 | `set`/`get` behavior, broadcast-on-change, unknown-session drop                               |
| `internal/api/contracts/sessions.go`                      | `ContextTokens int` field on `SessionResponseItem` (regenerated to `types.generated.ts`)      |
| `internal/dashboard/handlers_sessions.go`                 | `chatContextTokens` callback on `SessionHandlers`; fills the field in `buildSessionsResponse` |
| `internal/dashboard/server.go`                            | `chatContext` field; `observeChatUsage` writes the map and triggers `BroadcastSessions`       |
| `internal/chat/runtime.go`                                | Forwards every harness line to `usageCallback` — same sink as plan-usage                      |
| `internal/session/manager.go`                             | `SetChatUsageCallback` rewires new and restored runtimes                                      |
| `assets/dashboard/src/lib/types.ts`                       | Hand-written `context_tokens?: number` on `SessionResponse` (regen does not touch this)       |
| `assets/dashboard/src/components/SessionSidebar.tsx`      | `formatContextWindow` — prefixes live usage when `context_tokens > 0`                         |
| `assets/dashboard/src/components/SessionSidebar.test.tsx` | Display tests for with/without usage and missing context window                               |

## Architecture decisions

- **`message_delta` is the only reliable Claude source.** The durable `assistant` line carries real values for Anthropic models but reports zero usage for third-party models served through the Claude CLI (e.g., `glm-5.3`). `message_delta` carries real values for both. `message_delta` is live-only — it is never recorded to the transcript, which is why the value disappears on daemon restart and the sidebar falls back to showing the maximum alone until the session's next model call.
- **`message_start` is not a source.** It reports a separate `input_tokens` field without cache numbers and is not what the spec needs.
- **Subagent events are ignored.** Lines with a non-null `parent_tool_use_id` describe a different context and must not overwrite the root session's value.
- **Input tokens only.** Output tokens (including reasoning/thinking tokens) are excluded because Claude and Codex differ on whether they stay in the window. Input is comparable across both.
- **Codex: `last.inputTokens` already includes cached input.** A real line showed `inputTokens: 18088` with `cachedInputTokens: 12160` as a subset, and `totalTokens: 19015 = 18088 + 927 output`. Using `last.inputTokens` (not `last.totalTokens`) avoids double-counting.
- **Memory only.** No persistence, no replay from `out.jsonl`, no migration. A daemon restart starts empty. Rebuilding from recorded history only works for Anthropic models under the Claude protocol, so it is intentionally out of scope.
- **Replaces values, never history.** A later report overwrites the stored value. A zero-usage report returns `tokens > 0 = false` and does not call `set`, so a zero can never overwrite a real value. Compaction or a model switch shrinks the context — that is a change, and `set` reports it as such so the sidebar updates.
- **No new plumbing.** The context parser runs inside the existing `Server.observeChatUsage` callback sink that already receives every harness line for plan-usage tracking. The context parser must run **before** the plan-usage parser because `observeChatUsage` returns early when a line carries no plan quota today.
- **Broadcast on change only.** `chatContextUsage.set` returns whether the value changed; `observeChatUsage` calls `BroadcastSessions` (debounced 100 ms) only on change. Identical reports are dropped to avoid redundant broadcasts.
- **Terminal sessions stay hidden.** They have no token source, and the row already hides when `model.context_window` is missing. Terminal sessions never carry `context_tokens`, so their sidebar is unaffected.

## Gotchas

- `message_delta` lines must have `parent_tool_use_id == null` and a non-nil `usage` field with at least one of `input_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens` non-zero. Any other shape returns `false` and is ignored — including the zero-usage `assistant` line, `message_start`, and subagent events.
- The contract field is `ContextTokens int` with `json:"context_tokens,omitempty"` so Go zero-value sessions (terminal, or chat sessions before the first report) serialize the field as omitted, not as `0`. The frontend reads `context_tokens?: number` and the sidebar treats `undefined` and `0` the same way (shows maximum alone).
- `SessionResponse` in `assets/dashboard/src/lib/types.ts` is **hand-written**; regenerating via `go run ./cmd/gen-types` does not touch it. Adding a new contract field means updating both `internal/api/contracts/sessions.go` and the hand-written `SessionResponse`. `SessionWithWorkspace` is generated and updates automatically.
- Disposed sessions leave one int behind in the map until the daemon restarts. The map is keyed by session ID and never pruned on dispose. This is accepted — the memory cost is bounded by the number of distinct session IDs ever created in the daemon's lifetime, and the value will be skipped on `buildSessionsResponse` because the session is no longer present.
- The sidebar row is gated on `model.context_window` (the maximum from the models.dev registry), not on `context_tokens`. If the registry has no context window for the target, the row stays hidden even when a live count is known. The live count without a maximum would be unactionable.
- `BroadcastSessions` is debounced 100 ms; a flurry of harness lines in quick succession produces one WebSocket message, not many.
- The parser returns `tokens > 0` as the `ok` value — a line with all three cache fields `null` parses successfully but yields `0`, which is intentionally treated as "no value to record."

## Common modification patterns

- **To add a new chat protocol (e.g., a future Kimi chat mode):** write `ParseKimiContextTokens` in `internal/usage/context.go` mirroring the existing parsers, add a branch in `Server.observeChatUsage` keyed off `session.EffectiveChatProtocol()`, and add a table-driven case in `internal/usage/context_test.go`.
- **To change the context-token math (e.g., exclude cache writes):** edit the existing parser, update the matching fixture test in `internal/usage/parse_test.go`, and verify the table-driven tests in `context_test.go` still pass with the new expected values.
- **To change the sidebar display (e.g., show percentage instead of raw tokens):** edit `formatContextWindow` in `SessionSidebar.tsx` and update the three display tests in `SessionSidebar.test.tsx`. The `data-testid="session-context-window"` selector is the stable hook — keep it on the same element the tests query.
- **To surface context usage elsewhere (e.g., a new sidebar panel):** add the field to the relevant response type and component; do not introduce a new socket. Use `/ws/dashboard` like every other session-level state — `BroadcastSessions` already carries `context_tokens` because it flows through `buildSessionsResponse`.
- **To persist across daemon restarts:** out of scope by design. If you change this, expect to handle protocol asymmetry — only the Claude protocol records `assistant` lines with reliable usage, and only for Anthropic models.
