# Chat Sessions

## What it does

A chat-kind session replaces the terminal with a structured conversation view on the dashboard. The user types a message, sees it appear immediately, watches the agent stream a prose answer with subordinate tool calls, and can interrupt or answer inline permission and question requests. The conversation survives reloads, daemon restarts, and Restart (which seeds the new session's record from the old one). Claude Code and Codex support chat; each speaks its own wire protocol behind the same bridge, record, socket, and page.

## Key files

| File                                                          | Purpose                                                                                                 |
| ------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `internal/chat/record.go`                                     | Record types, file-backed `Log` (append + read), `CopyLog` for restart seeding                          |
| `internal/chat/bridge.go`                                     | `Paths`, `Ensure`, `PipelineCommand` (the `tail` background-job wrapper), `AppendInput`                 |
| `internal/chat/protocol.go`                                   | `Protocol` interface (`Launch`, `LiveOnly`, `ResumeID`, `Observe`, `Rebuild`, encoders) + `ProtocolFor` |
| `internal/chat/claude.go`                                     | Claude stream-json implementation; `Launch`, `LiveOnly`, encoders                                       |
| `internal/chat/codex.go`                                      | Codex app-server implementation; handshake, addressing state, encoders, `Rebuild`                       |
| `internal/chat/runtime.go`                                    | Per-session runtime: holds a `Protocol`; tail output → record + fan-out, send/interrupt/answer/abort    |
| `internal/chat/testdata/codex/`                               | Trimmed bridge captures for the Codex `Protocol` round-trip                                             |
| `internal/session/chat.go`                                    | Chat command building, file prep, seeding; `ErrChatSession` gate from `GetTracker`                      |
| `internal/session/manager.go`                                 | Holds chat runtime in place of terminal runtime for chat sessions; calls `End` on dispose only          |
| `internal/schmuxdir/schmuxdir.go`                             | `ChatSessionDir` path: `~/.schmux/chat/<workspaceID>/<sessionID>/`                                      |
| `internal/dashboard/websocket_chat.go`                        | `/ws/chat/{id}`: history (with `protocol`) then live records; client frames incl. `answer` and `abort`  |
| `internal/dashboard/handlers_spawn.go`                        | Rejects `kind: "chat"` for remote, command targets, resume path, harnesses without a chat mode          |
| `internal/dashboard/handlers_sessions.go`                     | `kind` on `SessionResponseItem`; chat sessions use the same tmux name and pid as terminal ones          |
| `internal/dashboard/handlers_config.go`                       | `chat_sessions` flag on `GET`/`POST /api/config`                                                        |
| `internal/detect/descriptor.go`                               | `Chat *ModeDesc` on descriptor with `protocol`; "chat" in `validCapabilities`                           |
| `internal/detect/adapter.go`                                  | `ChatArgs(model, resumeID)`, `ChatProtocol()` on `ToolAdapter`                                          |
| `internal/detect/descriptors/claude.yaml`                     | Claude's `chat:` mode with `protocol: claude-stream-json`                                               |
| `internal/detect/descriptors/codex.yaml`                      | Codex's `chat:` mode with `protocol: codex-app-server` and `app-server` base args                       |
| `internal/state/state.go`                                     | `Kind` and `ChatProtocol` fields on `Session`; `EffectiveChatProtocol()`                                |
| `internal/dashboard/handlers_restart.go`                      | Rejects Restart when the resolved descriptor's protocol differs from the session's                      |
| `internal/config/config.go`                                   | `ChatSessions` field on config                                                                          |
| `assets/dashboard/src/routes/ChatSessionPage.tsx`             | Chat route at `/sessions/{id}`, selected when session kind is "chat"                                    |
| `assets/dashboard/src/routes/SessionPage.tsx`                 | Terminal page (now uses shared sidebar); the chat replaces the terminal pane on this route              |
| `assets/dashboard/src/components/chat/ChatView.tsx`           | Top-level chat layout: transcript, status row, composer                                                 |
| `assets/dashboard/src/components/chat/ChatTranscript.tsx`     | Renders the list of user messages and assistant turns                                                   |
| `assets/dashboard/src/components/chat/AssistantTurnView.tsx`  | Renders one assistant turn's segments (prose, tool calls, thinking, cards, user segments)               |
| `assets/dashboard/src/components/chat/Composer.tsx`           | Textarea + Attach + Send; focus, draft persistence, Enter/Shift+Enter, image paste                      |
| `assets/dashboard/src/components/chat/ToolCallRow.tsx`        | Compact mono row with summary; expands to full input/result/sub-calls                                   |
| `assets/dashboard/src/components/chat/PermissionCard.tsx`     | Inline answerable card for `can_use_tool`/`requestApproval` requests                                    |
| `assets/dashboard/src/components/chat/QuestionCard.tsx`       | Inline answerable card for AskUserQuestion and `requestUserInput`                                       |
| `assets/dashboard/src/components/chat/ThinkingDisclosure.tsx` | Collapsed thinking block; visible only when content exists                                              |
| `assets/dashboard/src/components/chat/UserMessageBubble.tsx`  | Right-aligned user message; used both for top-level user items and steer segments                       |
| `assets/dashboard/src/components/SessionSidebar.tsx`          | Sidebar shared by terminal and chat pages (no attach command or iTerm2 link for chat sessions)          |
| `assets/dashboard/src/hooks/useChatSocket.ts`                 | WebSocket lifecycle, batching per animation frame, follow-tail scrolling                                |
| `assets/dashboard/src/hooks/useSessionActions.ts`             | Dispose / Restart / nickname for chat sessions                                                          |
| `assets/dashboard/src/lib/chat/reducer.ts`                    | Page-side dispatcher and shared turn helpers; protocol modules live in `claude.ts`/`codex.ts`           |
| `assets/dashboard/src/lib/chat/claude.ts`                     | Claude stream-json reducer (moved from `reducer.ts`)                                                    |
| `assets/dashboard/src/lib/chat/codex.ts`                      | Codex reducer; commandActions, reasoning, file changes, generic rows and cards                          |
| `assets/dashboard/src/lib/chat/socket.ts`                     | Client side of `/ws/chat/{id}`; threads the protocol from the history frame                             |
| `assets/dashboard/src/lib/chat/types.ts`                      | Wire types for record / frame / model; `ChatProtocol`, `Question.id`, `UserSegment`                     |
| `assets/dashboard/src/lib/chat-draft.ts`                      | Per-sessionStorage composer drafts (the same mechanism the spawn wizard uses)                           |
| `assets/dashboard/src/lib/chat/__fixtures__/claude/`          | Claude probe-cut JSONL fixtures, used by `claude.test.ts`                                               |
| `assets/dashboard/src/lib/chat/__fixtures__/codex/`           | Codex probe-cut JSONL fixtures, used by `codex.test.ts`                                                 |
| `internal/detect/hooks_codex_json.go`                         | Codex hook map, Claude-shaped, merged into `~/.codex/hooks.json`                                        |
| `internal/detect/hooks/capture-failure-codex.sh`              | Codex PostToolUse failure capture for autolearn                                                         |

## Architecture decisions

- **The conversation record is schmux's source of truth.** A `user_message` is appended to the record _before_ it is written to the harness's input file, so the page shows it before Claude answers, and a reload, daemon restart, or Restart (which copies the old record into the new session's directory) all reproduce the same history. Harness-emitted `user` records are never the source of a user message — only the conversation record is.

- **Four record types, written in four places.** `user_message` (when the user sends), `control` (interrupt or answer, wrapping the line sent verbatim), `harness` (every output line _except_ `stream_event`), `session` with `event: "ended"` (written from the dispose path before the harness is killed). Anything else comes out of the harness and is consumed by the reducer without being shown.

- **Stream events are forwarded live, never recorded.** In a typical session most output lines are `stream_event` deltas averaging ~356 bytes; recording them would dominate the file (the durable `assistant` record per block already carries the complete text). On reconnect the runtime resumes the output tail after the number of recordable lines already in the record; deltas in between are simply not re-forwarded, and a page that reconnects reloads history anyway.

- **The `session` ended record exists so a cut-off turn closes.** Dispose and Restart end a turn without a `result`. Without the ended record, a seeded history after Restart ends in an open turn: the next message shows as queued, Claude's reply lands in the old turn above it, and the Stop button never leaves. The ended record closes the turn where it was cut and clears queued flags.

- **The `control` record exists so "stopped" is decided by schmux, not by parsing text.** The harness's interrupt ack is `control_response` `{"still_queued":[]}` followed by a `user` record whose content is the literal string `[Request interrupted by user]` and then a `result` with `is_error: true`. There is no interrupt-specific subtype, so the only reliable signal is that schmux itself recorded the interrupt control line during the turn. Same idea for permission/question answers: a reload between answering and the tool result arriving does not re-show the card because the answer is recorded.

- **The page's reducer is the only thing that interprets the record.** The daemon keeps no turn state. It does not know whether a turn is open or which requests are pending; it appends what the user does, appends what the harness says, and forwards both. All rules — the four record types, subagent folding via `parent_tool_use_id`, queued-message replay clearing, `control_cancel_request` removing pending cards, the empty-thinking-block-renders-nothing rule — live in `assets/dashboard/src/lib/chat/reducer.ts`. The rules and the fixtures that prove them are kept together; new harness behaviors must come with new fixtures.

- **Subagent activity folds into the parent tool row.** Any harness record whose `parent_tool_use_id` is non-null belongs to the subagent run by the tool call with that id; only `assistant` records with `tool_use` blocks, `user` records with `tool_result` blocks, and (regardless of parent) `can_use_tool` requests add visible content. Everything else is consumed silently. The subagent's Bash call would otherwise render as the main agent's own — that is the failure mode the rule prevents.

- **Chat sessions have no terminal runtime.** `internal/session/chat.go` defines `ErrChatSession`, returned from `GetTracker` for chat sessions. Terminal socket, capture, tell, clipboard injection, and conflict-resolution key injection all obtain the tracker first and therefore fail for chat sessions without per-handler checks. That one fact is the gate.

- **The pane must exit when `claude` exits.** A plain `tail -f in.jsonl | claude` does not do that: `tail` only learns its reader is gone on its next write, so the pane (and the pane pid that `IsRunning` checks) stays alive until the next user input. The bridge therefore runs `tail` as a background job of the pane's shell, writes its pid to `tail.pid`, and `kill`s it when `claude` returns — so the pane closes, `IsRunning` goes false, and the chat socket answers 410, which is what disables the composer.

- **One mutex guards the whole step: append, fan-out, subscribe, encode, held queue, input append.** Every append and fan-out happens under `Runtime.mu`, and a new subscriber reads the file and registers its channel under the same mutex. A subscriber sees each record exactly once, either in history or live — there is no gap window and no duplicate. The same lock covers the protocol encode (which for Codex allocates a request id), the held-queue mutation, and the input-file append, so the input order always equals the record order and `Protocol` implementations need no locks of their own. The page batches incoming records once per animation frame; user messages and assistant turns are memoized on their item object so a burst of deltas costs one render of the changing turn.

- **Chat mode is declared per-descriptor.** Descriptors gain a `chat:` block with `base_args` and a required `protocol` naming the wire dialect (`claude-stream-json` or `codex-app-server`). A descriptor with a `chat:` mode reports `"chat"` in `Capabilities()`, which is how `GET /api/config`'s `runners[tool].capabilities` tells the wizard which harnesses can chat. The spawn handler rejects `kind: "chat"` when no selected target has chat capability, and rejects it for remote spawns, command targets, and the wizard's resume path. Claude and Codex declare chat modes today.

- **The protocol is one `Protocol` interface per harness.** The conversation record is verbatim per session; Go never interprets it. The only things the daemon does differently per harness are launch (argv and a handshake written to the input file before tmux starts), encode (the line for each user action), and observe (live-only lines, the resume id, and for Codex the addressing ids the next request needs). `internal/chat/protocol.go` defines the interface, `claude.go` and `codex.go` implement it, and the runtime holds one instance per session. Every component above the protocol — the bridge, the record, the WebSocket, the page — is shared. The persisted `state.Session.ChatProtocol` is what the runtime, the reducer, and the Restart guard read; they never re-resolve the descriptor (model targets and `~/.schmux/adapters/` overrides can change the answer between spawn and a later daemon restart). A chat session with an empty `ChatProtocol` is one spawned before the field existed, when `claude-stream-json` was the only protocol; `EffectiveChatProtocol` returns that value. Restart rejects when the resolved descriptor's protocol differs (`restart would switch the harness protocol`), because the seeded record holds the old dialect's lines and the resume id belongs to the old harness.

- **Codex needs addressing state that Claude does not.** `turn/start`, `turn/interrupt`, and the responses to server requests carry ids that the next request must use. The Codex `Protocol` keeps four pieces of state, learned live by `Observe` and rebuilt after a daemon restart by `Rebuild` (replay `Observe` over `out.jsonl`, scan `in.jsonl` for already-allocated request ids and `clientUserMessageId`s, then derive held sends): `threadID` from the `thread/start` response, `loggedIn` from the `account/read` response, `activeTurn` from the latest `turn/started` without a later `turn/completed` for the same id, and `nextID` from the maximum `id` in the input file plus one (4 on a fresh session). The instance is per-runtime (one per session) and the runtime serializes every encode and observe under `Runtime.mu`; implementations hold no locks of their own.

- **Status, hooks, and resume id follow the harness, not the chat kind.** A chat session changes the transport from terminal to bridge, not who reports status. Both harnesses signal through hooks: Claude's `settings.local.json` map and Codex's global `~/.codex/hooks.json` map (`SessionStart`, `UserPromptSubmit`, `PermissionRequest`, `Stop`, `PostToolUse`, `SessionEnd`). `buildChatCommand` does not inject an instruction file for Codex. `thread/status/changed` is recorded but not mapped: its vocabulary is a poorer subset of the states the session list and nudges need.

- **Steer is honest, not queued.** Codex folds a `turn/start` submitted while a turn is open into the running turn as a `userMessage` item; the page shows the message at the point it was sent, no queued label. Claude queues and runs after the open turn ends, with a "queued" label until the harness echoes the message back. The reducers differ in one rule for that reason; the `user` segment kind is shared so the page renders both through `UserMessageBubble` without branching. Reversible later by a runtime rule if the difference turns out to matter to users.

- **The chat-sessions flag is a single switch.** `chat_sessions` (bool) in config, exposed on `GET /api/config`, settable through `POST /api/config`, and toggled on the Settings Advanced tab next to `debug_ui`. The spawn wizard shows the Chat checkbox only when the flag is on and every selected target resolves to a runner whose capabilities include `chat`. The server enforces the same rule.

## Gotchas

- **`GetTracker` is the gate for chat sessions, not `IsChat()`.** Every terminal-only code path (capture, tell, clipboard injection, attach-command rendering, conflict-resolution key injection) goes through `GetTracker`. Adding a new terminal operation? Do not branch on `IsChat()` — obtain the tracker and rely on `ErrChatSession` so the gate stays in one place.

- **Stream events are not records.** They are forwarded to subscribers live but never appended. Adding a new live-only output type (e.g. a future progress event) requires deciding on both sides: runtime must fan it out without appending, and the reducer must consume it without producing anything visible. Look at how `stream_event` is handled for the pattern.

- **The reducer is the only source of truth for what the user sees.** The backend stores nothing turn-shaped. A behavior change to "what the user sees while Claude is running" is a reducer change, with a fixture proving it. If the fixture cannot reproduce the input the harness emits, the probe (`review/claude-chat-probe/` and `review/codex-chat-probe/`) is where new captures are cut from; the vendored fixtures under `assets/dashboard/src/lib/chat/__fixtures__/{claude,codex}/` are the compatibility contract.

- **The `session` ended record must be the last line of a disposed session's record.** `End` is called from the dispose path (`stopTracker`), never from daemon shutdown. A daemon restart is not an end: the harness keeps running in tmux and no record is written. Adding a daemon-shutdown hook that records "ended" will break Restart: the new session's seeded history will start with `event: "ended"` and the reducer will treat every open turn as already stopped.

- **The conversation record survives dispose but is not referenced from `state.json`.** It lives under the schmux home (`~/.schmux/chat/<workspaceID>/<sessionID>/`), not the workspace, with the same lifetime policy as fence launch dirs: nothing in the repo, nothing removed on dispose. Restart's seeding step (`CopyLog` in `internal/chat/record.go`) is the only thing that creates a new file from an old one.

- **Image records can be large.** `Log.ReadAll` uses a 64 MiB scanner buffer. A message with up to five inline PNGs runs near the dashboard's chat-WebSocket read limit (`chatWSReadLimit`, 32 MiB). Both are deliberate; do not lower them without thinking through the largest fixture.

- **The chat page reuses the terminal page's `WorkspaceHeader`, `SessionTabs`, `.session-detail` grid, and `.log-viewer` wrapper.** It does not invent new layout. If the terminal page changes its layout primitives, the chat page gets the change for free — but a change that affects only chat-specific concerns (e.g. composer placement) must be made in both `ChatSessionPage` and `SessionPage` if it could apply to terminal sessions.

- **The chat page uses the shared `SessionSidebar` extracted from the terminal page.** The sidebar omits the attach command and the iTerm2 link for chat sessions. If a new field appears in the sidebar, decide whether chat sessions need it; chat sessions have no terminal to attach to.

- **Composer drafts are stored in `sessionStorage`, not `localStorage`.** The same mechanism the spawn wizard uses. Drafts survive switching to another session and back, per session, for the life of the tab; sending clears it; another session's draft never appears in this one. Lowering the persistence scope to memory loses the switch-and-back behavior.

- **Any chat action clears the nudge the way terminal input does (`clearChatNudge`).** Sending, answering a card, aborting a request, or interrupting all count, so a "Needs Input" state does not outlive the card that announced it.

- **Unknown Codex server requests are answered with a JSON-RPC error via the `abort` frame; a new request kind that matters gets a real card and a schema-correct encoder.**

- **`--permission-prompt-tool stdio` is the only way permission prompts reach the page.** Under the fence, the fence's existing meaning (sandbox plus skip approvals) applies unchanged: the harness's auto-approve args are appended, no `can_use_tool` requests arrive, and `AskUserQuestion` is unavailable to the model. That is the same trade-off a fenced terminal session makes today.

- **Hooks fire under `-p` and are echoed as `system/hook_started` and `system/hook_response`.** Hook events stay where hooks write them, in the workspace's `.schmux/events/`. The runtime starts a hooks event watcher on the same events file with the same handlers a terminal session gets. The first `system/init` of every turn carries the harness `session_id`; the runtime passes it to the same idempotent `UpdateSessionResumeID` the hook path already uses, so Restart is available as soon as the first turn starts. Codex's `UserPromptSubmit` hook writes the same `resume_id`; the runtime independently captures the thread id from the `thread/start` response (or the `thread/started` notification) and applies it through the same `UpdateSessionResumeID`. Two sources, both required to agree; the precedence rule is the existing one and is not changed (last write wins, so a harness that forks on resume is tracked).

- **Held sends are derived, not persisted.** Codex's `turn/start` needs the thread id and a request id neither of which is known until the handshake is answered. The runtime records the `user_message` immediately, calls `protocol.UserMessage`, and on `ErrNotAddressable` holds the record until the protocol becomes addressable. The id is allocated at flush time, not at hold time. After a daemon restart, `protocol.Rebuild` replays `Observe` over the output file, scans the input file for `clientUserMessageId`s already written, and appends to `held` every `user_message` record after the last `session` record whose id is missing. Claude is always addressable, so its `held` is always empty.

- **`Runtime.mu` covers the whole step.** Record append, fan-out, the protocol encode (which reads and advances `nextID` and `activeTurn`), the held-queue mutation, and the input-file append all happen under the same lock. The Codex encoder and `interrupt` allocate a request id and write the line under that lock, so two WebSocket clients sending at once cannot produce duplicate ids or a torn held queue. `Protocol` implementations hold no locks of their own.

- **Codex `thread/resume` passes `excludeTurns: true`.** The record already holds the history (Restart seeds it from the old conversation), and Codex deprecates full hydration with a `deprecationNotice`. Resume id last-write-wins; the hook path and the thread response both must agree on the value, but the precedence is the existing one, not a new mechanism.

- **Chat sessions never get a terminal runtime, a timelapse recorder, or a dispose-time pane capture.** If you add one, the chat session path needs an explicit opt-out — the chat session kind is the only "in" check.

- **Slash commands typed into the composer are sent as ordinary messages and the harness expands them itself.** Built-ins whose interactive output is a panel (`/usage`) come back as plain text from the harness; the page shows that text with its line breaks intact. Prose paragraphs preserve single newlines. Composer-side slash-command completion is out of scope.

## Common modification patterns

- **To add a chat capability to a harness.** Add a `chat:` block to its descriptor with a `protocol` and `base_args`. Implement a `Protocol` in `internal/chat` and register it in `ProtocolFor`. Add a page reducer module in `assets/dashboard/src/lib/chat` and register it in `reducer.ts`'s dispatcher. Add a hook map for its `hooks.strategy`, so status does not depend on the model. Cut fixtures from a probe.

- **To add a new record type:** Define a `RecordType` constant and a `New<Type>` constructor in `internal/chat/record.go`. Extend the reducer in `assets/dashboard/src/lib/chat/reducer.ts` to handle it (with a fixture). The runtime forwards any output line; if the new type comes from the harness, decide whether it should be appended (`Harness`) or forwarded live only (current `stream_event` pattern). If it is schmux-initiated (like a future "rename" or "attach"), add the corresponding `NewControl` path in `runtime.go` and the WebSocket frame in `internal/dashboard/websocket_chat.go`.

- **To add a new client frame (chat WebSocket):** Extend `chatClientFrame` in `internal/dashboard/websocket_chat.go` and the switch over `f.Type`. Add a corresponding `Runtime` method (`Send`, `Interrupt`, `Permission`, `Answer` are the current four). Each runtime method appends its record to the conversation first, fans it out, and then writes the line to the input file — keep that order.

- **To add a chat capability to a harness:** Add a `chat:` block to its descriptor with `base_args` and `resume_id_args`. Implement `ChatArgs` in the adapter. Add `"chat"` to `Capabilities()`. The spawn handler will accept `kind: "chat"` for that harness automatically; the wizard will show the Chat checkbox when it is the selected tool.

- **To add a new chat-related config flag:** Mirror `ChatSessions` in `internal/config/config.go`, `ConfigResponse`/`ConfigUpdateRequest` in `internal/api/contracts/config.go`, regenerate `assets/dashboard/src/lib/types.generated.ts` via `go run ./cmd/gen-types`, expose it on `GET/POST /api/config`, and add a control in `assets/dashboard/src/routes/config/AdvancedTab.tsx`. Server-side gating is in `internal/dashboard/handlers_spawn.go`.

- **To change the reducer's behavior:** Edit `assets/dashboard/src/lib/chat/reducer.ts` (or the protocol-specific module under `claude.ts` / `codex.ts`). Add a fixture in `assets/dashboard/src/lib/chat/__fixtures__/{claude,codex}/` that reproduces the input. The existing fixtures are cut from `review/claude-chat-probe/probe{2,3,4,5}.out.jsonl` (Claude) and `review/codex-chat-probe/` (Codex); new harness behaviors need a new probe cut and the fixtures vendored under the matching subdirectory.

- **To add a new segment to an assistant turn (e.g. file diffs):** Extend the segment types in `assets/dashboard/src/lib/chat/types.ts`, add a renderer in `assets/dashboard/src/components/chat/AssistantTurnView.tsx`, and update the reducer to produce it from the relevant record type. The terminal session's `.markdown-preview-content` stylesheet (with an `--inline` modifier) is the established Markdown surface; reuse it.

- **To add a new live-only forwarded event type (e.g. progress):** Do not append it; fan it out as a synthetic `Record` with `Type: RecordHarness` only if the reducer needs it, or with a new type if the reducer needs to distinguish it. Add the dispatch in `runtime.go`'s output-tail loop and the reducer handling.

- **To add a sidebar field that chat sessions should also see:** Extend `SessionSidebar` (used by both pages); for chat-only fields, branch on `session.kind === "chat"` inside the sidebar, the way the attach-command and iTerm2 link are hidden today.
