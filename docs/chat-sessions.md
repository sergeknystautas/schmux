# Chat Sessions

## What it does

A chat-kind session replaces the terminal with a structured conversation view on the dashboard. The user types a message, sees it appear immediately, watches the agent stream a prose answer with subordinate tool calls, and can interrupt or answer inline permission and question requests. The conversation survives reloads, daemon restarts, and Restart (which seeds the new session's record from the old one). Claude Code and Codex support chat; each speaks its own wire protocol behind the same bridge, record, socket, and page.

## Key files

| File                                                          | Purpose                                                                                                                     |
| ------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `internal/chat/record.go`                                     | Record types, file-backed `Log` (append + read), `CopyLog` for restart seeding                                              |
| `internal/chat/bridge.go`                                     | `Paths`, `Ensure`, `PipelineCommand` (the `tail` background-job wrapper), `AppendInput`                                     |
| `internal/chat/protocol.go`                                   | `Protocol` interface (`Launch`, `LiveOnly`, `ResumeID`, `Observe`, `Rebuild`, encoders) + `ProtocolFor`                     |
| `internal/chat/claude.go`                                     | Claude stream-json implementation; `Launch`, `LiveOnly`, encoders                                                           |
| `internal/chat/codex.go`                                      | Codex app-server implementation; handshake, addressing state, encoders, `Rebuild`                                           |
| `internal/chat/runtime.go`                                    | Per-session runtime: holds a `Protocol`; tail output → record + fan-out, send/interrupt/answer/abort                        |
| `internal/chat/testdata/codex/`                               | Trimmed bridge captures for the Codex `Protocol` round-trip                                                                 |
| `internal/session/chat.go`                                    | Chat command building, file prep, seeding; `ErrChatSession` gate from `GetTracker`                                          |
| `internal/session/manager.go`                                 | Holds chat runtime in place of terminal runtime for chat sessions; calls `End` on dispose only                              |
| `internal/schmuxdir/schmuxdir.go`                             | `ChatSessionDir` path: `~/.schmux/chat/<workspaceID>/<sessionID>/`                                                          |
| `internal/dashboard/websocket_chat.go`                        | `/ws/chat/{id}`: history (with `protocol`) then live records; client frames incl. `answer` and `abort`                      |
| `internal/dashboard/handlers_spawn.go`                        | Rejects `kind: "chat"` for remote, command targets, harnesses without a chat mode                                           |
| `internal/dashboard/handlers_sessions.go`                     | `kind` on `SessionResponseItem`; chat sessions use the same tmux name and pid as terminal ones                              |
| `internal/dashboard/handlers_config.go`                       | `chat_sessions` flag on `GET`/`POST /api/config`                                                                            |
| `internal/detect/descriptor.go`                               | `Chat *ModeDesc` on descriptor with `protocol`; "chat" in `validCapabilities`                                               |
| `internal/detect/adapter.go`                                  | `ChatArgs(model, resume, resumeID)`, `ChatProtocol()` on `ToolAdapter`                                                      |
| `internal/detect/descriptors/claude.yaml`                     | Claude's `chat:` mode with `protocol: claude-stream-json`                                                                   |
| `internal/detect/descriptors/codex.yaml`                      | Codex's `chat:` mode with `protocol: codex-app-server` and `app-server` base args                                           |
| `internal/state/state.go`                                     | `Kind` and `ChatProtocol` fields on `Session`; `EffectiveChatProtocol()`                                                    |
| `internal/dashboard/handlers_restart.go`                      | Rejects Restart when the resolved descriptor's protocol differs from the session's                                          |
| `internal/config/config.go`                                   | `ChatSessions` field on config                                                                                              |
| `assets/dashboard/src/routes/ChatSessionPage.tsx`             | Chat route at `/sessions/{id}`, selected when session kind is "chat"                                                        |
| `assets/dashboard/src/routes/SessionPage.tsx`                 | Terminal page (now uses shared sidebar); the chat replaces the terminal pane on this route                                  |
| `assets/dashboard/src/components/chat/ChatView.tsx`           | Top-level chat layout: transcript, status row, composer                                                                     |
| `assets/dashboard/src/components/chat/ChatTranscript.tsx`     | Renders the list of user messages and assistant turns                                                                       |
| `assets/dashboard/src/components/chat/AssistantTurnView.tsx`  | Renders one assistant turn's segments (prose, tool calls, thinking, cards, user segments)                                   |
| `assets/dashboard/src/components/chat/Composer.tsx`           | Textarea + Attach + Send; focus, draft persistence, Enter/Shift+Enter, image paste                                          |
| `assets/dashboard/src/components/chat/ToolCallRow.tsx`        | Compact mono row with summary; expands to full input/result/sub-calls                                                       |
| `assets/dashboard/src/components/chat/PermissionCard.tsx`     | Inline answerable card for `can_use_tool`/`requestApproval` requests                                                        |
| `assets/dashboard/src/components/chat/QuestionCard.tsx`       | Interactive card for AskUserQuestion and `requestUserInput` (toggle-off, empty submit, option descriptions)                 |
| `assets/dashboard/src/components/chat/AnsweredQuestion.tsx`   | Read-only answered card: disabled options with chosen still primary, right-aligned user bubble showing the submitted answer |
| `assets/dashboard/src/components/chat/ThinkingDisclosure.tsx` | Collapsed thinking block; visible only when content exists                                                                  |
| `assets/dashboard/src/components/chat/UserMessageBubble.tsx`  | Right-aligned user message; used both for top-level user items and steer segments                                           |
| `assets/dashboard/src/components/SessionSidebar.tsx`          | Sidebar shared by terminal and chat pages (no attach command or iTerm2 link for chat sessions)                              |
| `assets/dashboard/src/hooks/useChatSocket.ts`                 | WebSocket lifecycle, batching per animation frame, follow-tail scrolling                                                    |
| `assets/dashboard/src/hooks/useSessionActions.ts`             | Dispose / Restart / nickname for chat sessions                                                                              |
| `assets/dashboard/src/lib/chat/reducer.ts`                    | Page-side dispatcher and shared turn helpers; protocol modules live in `claude.ts`/`codex.ts`                               |
| `assets/dashboard/src/lib/chat/claude.ts`                     | Claude stream-json reducer (moved from `reducer.ts`)                                                                        |
| `assets/dashboard/src/lib/chat/codex.ts`                      | Codex reducer; commandActions, reasoning, file changes, generic rows and cards                                              |
| `assets/dashboard/src/lib/chat/socket.ts`                     | Client side of `/ws/chat/{id}`; threads the protocol from the history frame                                                 |
| `assets/dashboard/src/lib/chat/types.ts`                      | Wire types for record / frame / model; `ChatProtocol`, `Question.id`, `UserSegment`                                         |
| `assets/dashboard/src/lib/chat-draft.ts`                      | Per-sessionStorage composer drafts (the same mechanism the spawn wizard uses)                                               |
| `assets/dashboard/src/lib/chat-answers.ts`                    | Per-sessionStorage question-card answer drafts; cleared delivery-authoritatively                                            |
| `assets/dashboard/src/lib/chat-focus.ts`                      | Per-sessionStorage focus record (composer caret or question target); restored on return                                     |
| `assets/dashboard/src/lib/chat/__fixtures__/claude/`          | Claude probe-cut JSONL fixtures, used by `claude.test.ts`                                                                   |
| `assets/dashboard/src/lib/chat/__fixtures__/codex/`           | Codex probe-cut JSONL fixtures, used by `codex.test.ts`                                                                     |
| `internal/detect/hooks_codex_json.go`                         | Codex hook map, Claude-shaped, merged into `~/.codex/hooks.json`                                                            |
| `internal/detect/hooks/capture-failure-codex.sh`              | Codex PostToolUse failure capture for autolearn                                                                             |
| `internal/chat/signout.go`                                    | Per-protocol sign-out statement lists; `MatchSignOutStatement`                                                              |
| `internal/chat/auth.go`                                       | Shared Codex account-response interpretation for message delivery and session status                                        |
| `internal/authcheck/authcheck.go`                             | Runs `claude auth status --json` / `codex login status` with a timeout; `LoggedIn`/`LoggedOut`/`NoAnswer`                   |
| `internal/dashboard/authcheck.go`                             | `RunAuthCheck` (single-flight per protocol), `applyAuthAnswer`, `HandleChatTurnError`                                       |
| `internal/dashboard/handlers_auth.go`                         | `POST /sessions/{id}/reauth` (spawns the login terminal) and `POST /sessions/{id}/auth-check`                               |
| `assets/dashboard/src/hooks/useAuthCheckOnFocus.ts`           | Fires the auth check on chat page load, refocus, and visibility change                                                      |

## Architecture decisions

- **The conversation record is schmux's source of truth.** A `user_message` is appended to the record _before_ it is written to the harness's input file, so the page shows it before Claude answers, and a reload, daemon restart, or Restart (which copies the old record into the new session's directory) all reproduce the same history. Harness-emitted `user` records are never the source of a user message — only the conversation record is.

- **Four record types, written in four places.** `user_message` (when the user sends), `control` (interrupt or answer, wrapping the line sent verbatim), `harness` (every output line _except_ `stream_event`), `session` with `event: "ended"` (written from the dispose path before the harness is killed, and always written after a Restart seed). Anything else comes out of the harness and is consumed by the reducer without being shown.

- **Stream events are forwarded live, never recorded.** In a typical session most output lines are `stream_event` deltas averaging ~356 bytes; recording them would dominate the file (the durable `assistant` record per block already carries the complete text). On reconnect the runtime resumes the output tail after the number of recordable lines already in the record; deltas in between are simply not re-forwarded, and a page that reconnects reloads history anyway.

- **The `session` ended record exists so a cut-off turn closes.** Dispose and Restart end a turn without a `result`. Without the ended record, a seeded history after Restart ends in an open turn: the next message shows as queued, Claude's reply lands in the old turn above it, and the Stop button never leaves; Codex rebuild can also mistake every seeded user message for one still waiting to be sent. The deterministic Restart-seed boundary closes the old lifetime, keeps the copied turns as display history, and prevents them from being reissued while the harness resumes its native conversation.

- **The `control` record exists so "stopped" is decided by schmux, not by parsing text.** The harness's interrupt ack is `control_response` `{"still_queued":[]}` followed by a `user` record whose content is the literal string `[Request interrupted by user]` and then a `result` with `is_error: true`. There is no interrupt-specific subtype, so the only reliable signal is that schmux itself recorded the interrupt control line during the turn. Same idea for permission/question answers: a reload between answering and the tool result arriving does not re-show the card because the answer is recorded.

- **The page's reducer is the only thing that interprets the record for transcript rendering.** Transcript display still lives in the dashboard reducer. What moved to the backend is the bookkeeping for the `Session.Nudge` waiting-for field: the chat runtime now derives `state` and `summary` from the same record, on a separate code path from the page. The runtime keeps the bookkeeping required to know whether a turn is open, which requests are pending, an interrupt has been issued, or work is queued between turns. It does not store the transcript or anything else the reducer reads. The transcript reducer continues to live in `assets/dashboard/src/lib/chat/reducer.ts`; the fixtures and rules that prove it are kept together.

- **Subagent activity folds into the parent tool row.** Any harness record whose `parent_tool_use_id` is non-null belongs to the subagent run by the tool call with that id; only `assistant` records with `tool_use` blocks, `user` records with `tool_result` blocks, and (regardless of parent) `can_use_tool` requests add visible content. Everything else is consumed silently. The subagent's Bash call would otherwise render as the main agent's own — that is the failure mode the rule prevents.

- **Chat sessions have no terminal runtime.** `internal/session/chat.go` defines `ErrChatSession`, returned from `GetTracker` for chat sessions. Terminal socket, capture, tell, clipboard injection, and conflict-resolution key injection all obtain the tracker first and therefore fail for chat sessions without per-handler checks. That one fact is the gate.

- **The pane must exit when `claude` exits.** A plain `tail -f in.jsonl | claude` does not do that: `tail` only learns its reader is gone on its next write, so the pane (and the pane pid that `IsRunning` checks) stays alive until the next user input. The bridge therefore runs `tail` as a background job of the pane's shell, writes its pid to `tail.pid`, and `kill`s it when `claude` returns — so the pane closes, `IsRunning` goes false, and the chat socket answers 410, which is what disables the composer.

- **One mutex guards the whole step: append, fan-out, subscribe, encode, held queue, input append.** Every append and fan-out happens under `Runtime.mu`, and a new subscriber reads the file and registers its channel under the same mutex. A subscriber sees each record exactly once, either in history or live — there is no gap window and no duplicate. The same lock covers the protocol encode (which for Codex allocates a request id), the held-queue mutation, and the input-file append, so the input order always equals the record order and `Protocol` implementations need no locks of their own. The page batches incoming records once per animation frame; user messages and assistant turns are memoized on their item object so a burst of deltas costs one render of the changing turn.

- **Chat mode is declared per-descriptor.** Descriptors gain a `chat:` block with `base_args` and a required `protocol` naming the wire dialect (`claude-stream-json` or `codex-app-server`). A descriptor with a `chat:` mode reports `"chat"` in `Capabilities()`, which is how `GET /api/config`'s `runners[tool].capabilities` tells the wizard which harnesses can chat. The spawn handler rejects `kind: "chat"` when no selected target has chat capability, and rejects it for remote spawns and command targets. Claude and Codex declare chat modes today.

- **Resume mode resumes the workspace's most recent conversation, per harness.** The wizard's `/resume` sends `resume: true` with `kind: "chat"` when the Chat toggle is on, and the same resume rules apply as for a terminal spawn (no prompt, no images). Claude's chat block declares `resume_args: ['--continue']`, which `ChatArgs(model, resume, resumeID)` appends when `resume` is set without an id (an id always wins: Restart resumes exactly its own conversation). Codex has no argv for it: `Launch` replaces the handshake's `thread/start` with `thread/list` (id 4: `cwd`, `limit: 1`, `sortKey: updatedAt`, `sourceKinds: cli, vscode, appServer`), and `Observe` answers the list response with the id-3 `thread/resume` of the newest thread (`excludeTurns: true`), or a plain `thread/start` when the workspace has none. The runtime writes those follow-up lines to the input file like the handshake, unrecorded. The pending thread params live only on the protocol instance `Launch` populated, so a restarted runtime replaying the output never re-issues the request. In both harnesses the conversation record starts empty: the resumed history lives in the harness, and the page shows the turns from this session on.

- **The protocol is one `Protocol` interface per harness.** The conversation record is verbatim per session; Go never interprets it. The only things the daemon does differently per harness are launch (argv and a handshake written to the input file before tmux starts), encode (the line for each user action), and observe (live-only lines, the resume id, and for Codex the addressing ids the next request needs). `internal/chat/protocol.go` defines the interface, `claude.go` and `codex.go` implement it, and the runtime holds one instance per session. Every component above the protocol — the bridge, the record, the WebSocket, the page — is shared. The persisted `state.Session.ChatProtocol` is what the runtime, the reducer, and the Restart guard read; they never re-resolve the descriptor (model targets and `~/.schmux/adapters/` overrides can change the answer between spawn and a later daemon restart). A chat session with an empty `ChatProtocol` is one spawned before the field existed, when `claude-stream-json` was the only protocol; `EffectiveChatProtocol` returns that value. Restart rejects when the resolved descriptor's protocol differs (`restart would switch the harness protocol`), because the seeded record holds the old dialect's lines and the resume id belongs to the old harness.

- **Codex needs addressing state that Claude does not.** `turn/start`, `turn/interrupt`, and the responses to server requests carry ids that the next request must use. The Codex `Protocol` keeps four pieces of state, learned live by `Observe` and rebuilt after a daemon restart by `Rebuild` (replay `Observe` over `out.jsonl`, scan `in.jsonl` for already-allocated request ids and `clientUserMessageId`s, then derive held sends): `threadID` from the `thread/start` response, `loggedIn` from the `account/read` response, `activeTurn` from the latest `turn/started` without a later `turn/completed` for the same id, and `nextID` from the maximum `id` in the input file plus one (4 on a fresh session). The instance is per-runtime (one per session) and the runtime serializes every encode and observe under `Runtime.mu`; implementations hold no locks of their own.

- **Nudge ownership follows the session kind; other hooks still follow the harness.** Terminal sessions retain hook/agent status and NudgeNik classification. For chat sessions the status Stop gate is short-circuited by the per-process `SCHMUX_SESSION_KIND=chat` env var: the status script returns silently for chat, the headless chat runtime owns `Nudge` directly, and the server-side `IsChat()` guard in `HandleStatusEvent` and `checkInactiveSessionsForNudge` keeps already-running chat sessions from being overwritten by a delayed status event. Both harnesses retain their other hooks: Claude's `settings.local.json` map and Codex's global `~/.codex/hooks.json` map (`SessionStart`, `UserPromptSubmit`, `PermissionRequest`, `Stop`, `PostToolUse`, `SessionEnd`). `buildChatCommand` does not inject an instruction file for Codex. `thread/status/changed` is recorded but not mapped: its vocabulary is a poorer subset of the states the session list and nudges need.

- **Steer is honest, not queued.** Codex folds a `turn/start` submitted while a turn is open into the running turn as a `userMessage` item; the page shows the message at the point it was sent, no queued label. Claude queues and runs after the open turn ends, with a "queued" label until the harness echoes the message back. The reducers differ in one rule for that reason; the `user` segment kind is shared so the page renders both through `UserMessageBubble` without branching. Reversible later by a runtime rule if the difference turns out to matter to users.

- **The chat-sessions flag is a single switch.** `chat_sessions` (bool) in config, exposed on `GET /api/config`, settable through `POST /api/config`, and toggled on the Settings Advanced tab next to `debug_ui`. The spawn wizard shows the Chat checkbox only when the flag is on and every selected target resolves to a runner whose capabilities include `chat`. The server enforces the same rule.

- **Per-sessionStorage drafts and focus, keyed by session id.** `chat-draft.ts`, `chat-answers.ts`, and `chat-focus.ts` all use the same `sessionStorage` pattern (key `chat-{draft|answers|focus}-{sessionId}`). Drafts survive switching to another session tab and back, but never cross sessions in the same tab, never reach another browser tab, and never persist past reload. Keys are session ids; workspaced never appears in the key, so two sessions in the same workspace are isolated too. `ChatSessionPage` is the sole place these stores are read and written, so removing or replacing the persistence is a single page change.

- **Clearing question answers is delivery-authoritative, not click-authoritative.** `QuestionCard` only sends the answer frame on Submit; the card itself is removed from the reducer only when the harness's resolution record arrives (`control_response` / JSON-RPC response / cancel). The shared boundary is exposed as `resolvesRequest(protocol, record)` in `lib/chat/reducer.ts` — one per protocol module, mirroring exactly the records that `removePending` matches in the reducer. `useChatSocket` runs every record (history and live) through it and fires `onRequestResolved(requestId)`; `ChatSessionPage` clears the answer draft at that same boundary. Clearing at click would destroy in-progress answers on a failed write; clearing at observed resolution reuses the same success boundary that already removes the card. No backend change is needed because the resolution record is already on the wire.

- **`AnsweredSegment` is a separate segment kind, not a flag on `PendingSegment`.** Five production sites filter `kind === 'pending'` to mean _awaiting input_ (`findAgentToolIdForRequest`, `clearAgentPendingInput`, `removePending`, activity-selector). A distinct `kind: 'answered'` keeps those sites correct untouched. `resolvePending` in `lib/chat/reducer.ts` converts a pending question segment to answered instead of dropping it; permission pendings (no questions) drop as today. Idempotent: once converted, no pending segment exists to find. Interrupts and `control_cancel_request` still drop the segment outright — a cancelled question was never answered.

- **Single-select re-click clears the option; empty submit is a real answer.** `QuestionCard.toggle()` treats single-select like multi-select on re-click, so misclicks are recoverable. The Submit button is never disabled: a question with no selection and no Other text submits an empty array, which `internal/chat` joins into an empty answer string. "None of the above" is a legitimate answer choice, not a dead end.

- **The question card fills the turn width via an opt-in modifier.** The base `.card` caps at 480px, which fits short, centered permission cards but wastes width on the question card on a wide screen. Question cards apply `.cardQuestion` alongside, setting `max-width: none`. The options grid (`repeat(auto-fill, minmax(min(100%, 18rem), 1fr))`) divides the card width evenly and collapses to one column on narrow viewports instead of overflowing. Permission cards keep the cap — their text is short and the cap keeps them focused. If a new card type needs full width, add a similar modifier; do not silently raise the base.

- **Question tool rows summarize with the question text, not the input JSON.** `summarizeTool()` recognizes `AskUserQuestion` and returns the first question's text (joined with `; `). The always-visible result line (`chat-tool-result`) is suppressed for question rows — the answered card carries the answer. Raw input/result JSON still appear in the expanded details for opt-in debug. Codex's `requestUserInput` produces no tool row today (it creates only a pending segment), so no codex-side row changes were needed.

- **"Answered" means submission was recorded, not consumed.** The daemon appends the control record before writing the harness input (`sendControlLocked`), so a record proves the answer was recorded. On input-write failure the live fan-out never fires — the card stays interactive and the draft survives — but the record replays on the next history load and renders as answered while the backend still waits. Daemon restart reconciles (`Rebuild` → held → `flushHeldLocked` re-delivers recorded-but-unwritten controls), so the disagreement self-heals rather than persisting. Making delivery itself the boundary would require Go changes whose failure inversion (delivered-but-unrecorded) is worse; out of scope. The same mismatch is inherited from the old `removePending` path — today's behavior loses the question entirely on this path, the new one shows it.

- **`ChatSessionPage` is the sole programmatic focuser.** `Composer` no longer focuses itself on enable — that effect would have fired at `connected`, before the history frame, and its `onFocus` would have captured an empty focus record that clobbered the saved question target. Restore happens once `useChatSocket`'s `historyLoaded` flips true (the history frame has been applied; pending cards are in the DOM). `historyLoaded` resets to false on `disconnected` because `ChatSocket` reconnects internally and the connection effect does not re-run, so a flag that did not reset would survive a reconnect and skip the re-restore. The `ChatTranscript` handle exposes `focusQuestionTarget(requestId, questionId, kind, label?, position?)` — it queries its own rendered tree for `[data-chat-question-target]` and `[data-question-id]` / `[data-option-label]` dataset fields, returning `false` so the page can fall back to the composer at end-of-text. The fallback is the only way a stale record (a question target that resolved between saves) is recovered from.

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

- **The answer-draft and focus stores have no explicit clear path.** `chat-answers.ts` clears per `requestId` only via `useChatSocket`'s `onRequestResolved` callback; `chat-focus.ts` is never cleared. Staleness is handled by restore fallbacks in `ChatSessionPage`: a focus record pointing at a question field that no longer exists falls back to the composer at end-of-text, and an answer draft for a request that resolved is harmless because the card that would have consumed it is gone. Do not add a generic clear button or migration step — the lifecycle is the resolution event.

- **`historyLoaded` in `useChatSocket` is required, not optional.** `connected` fires on WebSocket open, but the conversation — and therefore the pending question cards — arrives in the history frame after open. Without `historyLoaded`, a question card that _will_ render is indistinguishable from one that is _gone_, and a saved focus record would either miss its target or land on a field that exists for the wrong reasons. The flag is set true in `onHistory` (empty history counts) and false on session change and on `disconnected`.

- **`resolvesRequest` must mirror each protocol's reducer exactly.** A drift between the records that remove a pending segment in the reducer and the records that `resolvesRequest` reports would either clear an answer before its card is gone (visible as in-progress answers that vanish during navigation) or leave it stale (the restore then shows answers for a card that no longer exists). The two tests live next to each other: `claude.test.ts` / `codex.test.ts` for the reducer and a matching describe block for `resolvesRequest`. Add a new resolution-record shape in both places.

- **`focusQuestionTarget` matches dataset fields, not CSS selectors.** Question ids are user-supplied question text, so the matches are `[data-chat-question-target][data-request-id="…"][data-question-id="…"]` against the `data-question-id` field. If you add a new focusable on a question card, give it `data-chat-question-target` and the right dataset fields or restore won't reach it.

- **The `QuestionCard` `stateRef` write inside `setSelected`/`setOther` is intentional.** State updaters must be pure, so the ref write happens alongside (not inside) the updater; the value used by `report` is read synchronously from the same closure, not from the React state. StrictMode double-invocation is fine because the writes are idempotent. If you split the toggle logic, preserve this: do not read state inside `report`.

- **The chat runtime publishes the waiting-for value directly to `Session.Nudge`.** It owns the field for chat sessions: tracking a turn's open/closed state, pending permission and question requests, the user's interrupt intent, queued follow-up work, and the most recent result. The runtime does not clear the field on a successful send or answer the way the chat socket used to (`clearChatNudge`); instead the tracker recomputes the value from the latest record + user action, and the runtime publishes the new value when it differs from the previous one. An answer write that fails (the input file append errors) leaves the pending request pending. A successfully written interrupt records intent only: pending requests and the Working indicator remain until the harness confirms the turn ended. Claude’s error-shaped interruption result becomes Idle, not Error, when paired with that intent. A rejected interrupt changes nothing. The dashboard's `Server.UpdateChatNudge` writes the JSON with `source: "headless"`, increments `NudgeSeq` only when the payload actually changes, saves, and broadcasts. Identical snapshots — including an unchanged restored snapshot after a daemon restart — do not increment the sequence.

- **Unknown Codex server requests are answered with a JSON-RPC error via the `abort` frame; a new request kind that matters gets a real card and a schema-correct encoder.** Until answered, they produce Needs Input with the method name. Structured questions remain pending regardless of Codex’s `isBlocking` flag; the captured nonblocking question still requires a response. The first unanswered question is shown verbatim, with `(+N more)` counting additional questions and approval/unsupported requests. Numeric server request ID zero is valid; unrelated replies to client requests never resolve a wait.

- **Nudge replay follows the current lifetime and successful input writes.** Records before the most recent session-ended marker are history only. Control records are reconciled against the input file by ordered occurrence and applied at their original record position, so failed answers/interrupts do not become successful after a daemon restart. Only the final snapshot is published. Claude’s consumed-message echoes distinguish the active message from queued follow-ups (including identical text), preventing Done between queued turns. Child execution and telemetry cannot finish or restart a root turn; unrecoverable harness errors produce Error, while retry notifications and tool failures alone do not.

- **A fresh or replacement session is Idle.** Ordinary readiness is not a request for attention: before its first turn, its Nudge is Idle with an empty summary. Needs Input is reserved for an outstanding question, approval, or other request requiring a response. Restarting a session preserves conversation history but never copies the previous process's Working, pending requests, or Done state. Restoring the daemon around the same surviving process instead rebuilds that process's current state.

- **Headless activity drives the existing `last_output_at` clock.** Accepted user messages, successful control writes, and harness output (including live-only deltas) update the session's in-memory `LastOutputAt`, which feeds elapsed idle time and workspace recent-activity sorting. Stream updates are rate-limited to 500ms with a trailing flush; Nudge changes flush immediately so Idle/Done retain the final event time. Activity alone never saves state or increments `NudgeSeq`. A fresh session starts at its creation time; daemon restoration reconstructs the latest durable current-lifetime record timestamp rather than marking the session active now. Live-only deltas remain unpersisted, so replay recovers the latest durable timestamp, not necessarily the final streamed token time.

- **`--permission-prompt-tool stdio` is the only way permission prompts reach the page.** Under the fence, the fence's existing meaning (sandbox plus skip approvals) applies unchanged: the harness's auto-approve args are appended, no `can_use_tool` requests arrive, and `AskUserQuestion` is unavailable to the model. That is the same trade-off a fenced terminal session makes today.

- **Hooks fire under `-p` and are echoed as `system/hook_started` and `system/hook_response`.** Hook events stay where hooks write them, in the workspace's `.schmux/events/`. The runtime starts a hooks event watcher on the same events file with the same handlers a terminal session gets. The first `system/init` of every turn carries the harness `session_id`; the runtime passes it to the same idempotent `UpdateSessionResumeID` the hook path already uses, so Restart is available as soon as the first turn starts. Codex's `UserPromptSubmit` hook writes the same `resume_id`; the runtime independently captures the thread id from the `thread/start` response (or the `thread/started` notification) and applies it through the same `UpdateSessionResumeID`. Two sources, both required to agree; the precedence rule is the existing one and is not changed (last write wins, so a harness that forks on resume is tracked).
- **Codex hooks run only after a one-time trust accept per machine.** Codex gates user hooks behind a trust check (`hooks/list` reports `trustStatus` and `currentHash`); a freshly merged `~/.codex/hooks.json` group is untrusted until the user accepts it in a Codex TUI session, which the merge strategy's comment already describes. The first Codex chat session on a new machine will therefore not see hook-driven status until the user accepts the merged group once. Writing the trusted hash directly via `hooks.state` in `~/.codex/config.toml` is verified during implementation as a way to remove the manual step; do not assume it works without checking.

- **Held sends are derived, not persisted.** Codex's `turn/start` needs the thread id and a request id neither of which is known until the handshake is answered. The runtime records the `user_message` immediately, calls `protocol.UserMessage`, and on `ErrNotAddressable` holds the record until the protocol becomes addressable. The id is allocated at flush time, not at hold time. After a daemon restart, `protocol.Rebuild` replays `Observe` over the output file, scans the input file for `clientUserMessageId`s already written, and appends to `held` every `user_message` record after the last `session` record whose id is missing. Claude is always addressable, so its `held` is always empty.

- **`Runtime.mu` covers the whole step.** Record append, fan-out, the protocol encode (which reads and advances `nextID` and `activeTurn`), the held-queue mutation, and the input-file append all happen under the same lock. The Codex encoder and `interrupt` allocate a request id and write the line under that lock, so two WebSocket clients sending at once cannot produce duplicate ids or a torn held queue. `Protocol` implementations hold no locks of their own.

- **Codex `thread/resume` passes `excludeTurns: true`.** The record already holds the history (Restart seeds it from the old conversation), and Codex deprecates full hydration with a `deprecationNotice`. Resume id last-write-wins; the hook path and the thread response both must agree on the value, but the precedence is the existing one, not a new mechanism.

- **Chat sessions never get a terminal runtime, a timelapse recorder, or a dispose-time pane capture.** If you add one, the chat session path needs an explicit opt-out — the chat session kind is the only "in" check.

- **Slash commands typed into the composer are sent as ordinary messages and the harness expands them itself.** Built-ins whose interactive output is a panel (`/usage`) come back as plain text from the harness; the page shows that text with its line breaks intact. Prose paragraphs preserve single newlines. Composer-side slash-command completion is out of scope.

- **`.cardQuestion` is opt-in.** The base `.card` 480px cap is right for permission cards. The question card must apply `.cardQuestion` alongside, but permission cards must NOT. If a new card type needs full width, add a similar modifier; do not raise the base. The two card components share the same `questionOptions` / `questionOption` / `questionOptionDescription` classes so a layout change can land in `chat.module.css` once and reach both.

- **The Submit button is never disabled.** Disabling it would block the legitimate "none of the above" path. If a future requirement needs completeness gating, gate the answer text the user submits, not the button — empty submissions are real answers and a UI that hides them is a UX regression.

- **`chat-tool-result` is intentionally absent for `AskUserQuestion` rows.** The answered card carries the answer. The expanded details still show raw input/result JSON for debug; that path is untouched.

- **An `AnsweredSegment` derived from an abort-path `serverRequest/resolved` (Codex) has `answers: {}`.** It renders the read-only card without a user bubble. This is expected — the question was answered (no longer pending) but the answer text never reached the wire. The same shape arises from any future protocol whose resolution record arrives without an echo.

## Common modification patterns

- **To add a chat capability to a harness.** Add a `chat:` block to its descriptor with a `protocol` and `base_args`. Implement a `Protocol` in `internal/chat` and register it in `ProtocolFor`. Add a page reducer module in `assets/dashboard/src/lib/chat` and register it in `reducer.ts`'s dispatcher. Add a hook map for its `hooks.strategy`, so status does not depend on the model. Cut fixtures from a probe.

- **To add a new record type:** Define a `RecordType` constant and a `New<Type>` constructor in `internal/chat/record.go`. Extend the reducer in `assets/dashboard/src/lib/chat/reducer.ts` to handle it (with a fixture). The runtime forwards any output line; if the new type comes from the harness, decide whether it should be appended (`Harness`) or forwarded live only (current `stream_event` pattern). If it is schmux-initiated (like a future "rename" or "attach"), add the corresponding `NewControl` path in `runtime.go` and the WebSocket frame in `internal/dashboard/websocket_chat.go`.

- **To add a new client frame (chat WebSocket):** Extend `chatClientFrame` in `internal/dashboard/websocket_chat.go` and the switch over `f.Type`. Add a corresponding `Runtime` method (`Send`, `Interrupt`, `Permission`, `Answer` are the current four). Each runtime method appends its record to the conversation first, fans it out, and then writes the line to the input file — keep that order.

- **To add a chat capability to a harness:** Add a `chat:` block to its descriptor with `base_args`, `resume_args` (resume most recent), and `resume_id_args`. Implement `ChatArgs` in the adapter. Add `"chat"` to `Capabilities()`. The spawn handler will accept `kind: "chat"` for that harness automatically; the wizard will show the Chat checkbox when it is the selected tool.

- **To add a new chat-related config flag:** Mirror `ChatSessions` in `internal/config/config.go`, `ConfigResponse`/`ConfigUpdateRequest` in `internal/api/contracts/config.go`, regenerate `assets/dashboard/src/lib/types.generated.ts` via `go run ./cmd/gen-types`, expose it on `GET/POST /api/config`, and add a control in `assets/dashboard/src/routes/config/AdvancedTab.tsx`. Server-side gating is in `internal/dashboard/handlers_spawn.go`.

- **To change the reducer's behavior:** Edit `assets/dashboard/src/lib/chat/reducer.ts` (or the protocol-specific module under `claude.ts` / `codex.ts`). Add a fixture in `assets/dashboard/src/lib/chat/__fixtures__/{claude,codex}/` that reproduces the input. The existing fixtures are cut from `review/claude-chat-probe/probe{2,3,4,5}.out.jsonl` (Claude) and `review/codex-chat-probe/` (Codex); new harness behaviors need a new probe cut and the fixtures vendored under the matching subdirectory.

- **To add a new segment to an assistant turn (e.g. file diffs):** Extend the segment types in `assets/dashboard/src/lib/chat/types.ts`, add a renderer in `assets/dashboard/src/components/chat/AssistantTurnView.tsx`, and update the reducer to produce it from the relevant record type. The terminal session's `.markdown-preview-content` stylesheet (with an `--inline` modifier) is the established Markdown surface; reuse it.

- **To add a new live-only forwarded event type (e.g. progress):** Do not append it; fan it out as a synthetic `Record` with `Type: RecordHarness` only if the reducer needs it, or with a new type if the reducer needs to distinguish it. Add the dispatch in `runtime.go`'s output-tail loop and the reducer handling.

- **To add a sidebar field that chat sessions should also see:** Extend `SessionSidebar` (used by both pages); for chat-only fields, branch on `session.kind === "chat"` inside the sidebar, the way the attach-command and iTerm2 link are hidden today.

- **To add a new kind of focus target to chat sessions.** Add the variant to `ChatFocus` in `lib/chat-focus.ts`. Add the `data-*` dataset fields on the element in `QuestionCard.tsx` (or wherever the focusable is rendered). Extend the query loop in `ChatTranscript`'s `focusQuestionTarget`. Add the restore branch in `ChatSessionPage`'s restore effect. Do not add a separate "is this focus target available?" probe — `focusQuestionTarget` already returns `false` when no element matches, and that is the fallback signal.

- **To add a new client-side draft to a chat session.** Mirror `chat-answers.ts` / `chat-focus.ts`: a `sessionStorage` lib keyed by session id, with a `load<Thing>` / `save<Thing>` pair and corrupt-JSON tolerance. Wire it from `ChatSessionPage` only. If the draft must be cleared on a server-observed event, expose another `resolves<Thing>` from `lib/chat/reducer.ts` and consume it via `useChatSocket`'s callback — never clear on click.

- **To add a new server-initiated request type that has a card.** Extend `resolvesRequest` in the relevant protocol module with the new resolution record shape, mirroring `removePending` in the reducer. The answer-draft `clearChatAnswers` boundary and the focus-fallback on resolution both depend on this hook firing — leave it missing and stale answer drafts will be restored onto an unrelated later request.

- **To add a new answer wire format.** Parse it in `claudeAnswersFromResponse` (Claude) or `codexAnswersFromResult` (Codex) and pass the result to `resolvePending` from the protocol's reducer module. If a future protocol does not echo answers at all, leave the parameter undefined — `resolvePending` defaults to `{}` and the card will render without a bubble. Both helpers normalize into `{ questionId: "label1, label2, ..." }`.

- **To change the answered card layout.** Edit `AnsweredQuestion.tsx` (the read-only view). `QuestionCard.tsx` stays interactive-only. The two share CSS classes (`questionOptions`, `questionOption`, `questionOptionDescription`), so a layout change can land in `chat.module.css` once and reach both views.

- **To suppress the tool-result line for another question-type tool.** Add the tool name to the `isQuestion` check in `ToolCallRow.tsx` alongside `AskUserQuestion`, and add the question-text branch in `summarizeTool()`. The answered card below the row carries the answer; the JSON result line would be noise.

- **To widen another card type beyond `.card`'s 480px.** Add a CSS modifier like `.cardQuestion` (max-width: none) and apply it alongside `.card` in the component. Do not raise the base — the cap is right for permission cards.

## Signed-out recovery

Chat harnesses authenticate with a first-party login. When it is lost, the
session looks healthy until a message fails. `signed_out` is a persisted
boolean on `state.Session`, broadcast as `signed_out` on the session
summary, and it alone drives the banner above the composer, the composer
lock, and the "Signed out" line on the session tab and in the sidebar.
The browser does not interpret `account/read` or synthesize authentication
errors while rebuilding the transcript. The backend owns the login decision;
historical messages remain visible regardless of current login state.

### How the flag moves

- **Set by the session's own failure.** A live Codex startup `account/read`
  response explicitly reporting a missing required login, or a live turn error whose
  text contains a sign-out statement for its protocol (`internal/chat/signout.go`)
  sets the flag on that session. A local first-party Claude result with the
  structured `api_error_status: 401` signal is stronger: schmux runs
  `claude auth logout` to remove the credential Anthropic rejected, then sets
  every in-scope Claude chat to signed out because the credential is
  HOME-global. The chat runtime's turn-error callback fires for live records
  only; record replay after a daemon restart never derives the flag.
- **Corrected by the harness's status tool.** `internal/authcheck` runs
  `claude auth status --json` (`loggedIn`) or `codex login status`
  (a successful `Logged in using …` response, or explicit
  `Not logged in`) and the answer applies to every in-scope chat
  session of that protocol at once, because login state is HOME-global.
  Timeout or unparseable output changes nothing and logs the raw output.
  One run per protocol is in flight at a time.
- **Two triggers, both event-driven.** Any activation of a chat page
  (`useAuthCheckOnFocus`) and any failed turn (`HandleChatTurnError`). There
  is no interval and no daemon-startup check. Ordinary failed turns run the
  status tool. Explicit login failures set the flag without immediately
  checking a potentially stale CLI credential; a first-party Claude 401
  also invalidates the rejected credential. Out-of-scope failures do not
  launch global login checks. The auth-check and reauth endpoints reject
  provider-routed sessions, just as they reject remote sessions.
- **Scope is resolved when a rule fires, not stamped at spawn.** Local chat
  sessions whose target does not route the harness to a non-first-party
  endpoint (`models.Manager.RoutesToEndpoint`). Unresolvable targets are in
  scope; remote sessions never are. Nothing is persisted at spawn and no
  environment contents are inspected: provider secrets ride in every target
  resolution, so any env-based test would be false on a configured machine.

### Sign-in terminal

`POST /api/sessions/{id}/reauth` spawns a plain command session in the chat
session's workspace and the page navigates to it. The command is
`claude auth logout || true; claude /login` for claude and `codex login` for
codex. The logout runs first so a half-dead credential cannot survive into
the login. When the user returns to the chat, the focus check clears the flag.

### Gotchas

- **Use the REPL `/login`, not `claude auth login`.** As of Claude Code
  2.1.270 the standalone subcommand's "Paste code here if prompted" prompt
  reads the pasted code without echoing a character and exits on a bad code,
  so in the dashboard it looks like a terminal that ignores keystrokes and
  then dies. The REPL dialog echoes and stays up. `TestReauthClaudeUsesREPLLogin`
  pins this.
- **The REPL `/logout` resets Claude's first-run onboarding; `claude auth logout` does not.**
  Signing out with `/logout` to test recovery makes the next `claude /login`
  walk through the theme picker before the login-method selector, and then
  show the `/login` dialog a second time after login completes. Token expiry
  and `claude auth logout` leave the onboarding flag alone, so the production
  path goes straight to the login-method selector and OAuth. Test with
  `claude auth logout`.
- **The sign-in terminal does not end itself.** `claude /login` is the full
  REPL; after the OAuth callback it stays running until disposed or `/exit`.
  Disposing it from the logged-in answer of `RunAuthCheck` is the natural hook
  if this becomes a problem; it is not built.
- **Claude reports a failed turn twice.** An assistant text block carries the
  error ("Not logged in · Please run /login"), then the error result carries
  the same text. `endTurn` in `lib/chat/claude.ts` drops a final prose segment
  that equals the error text so the transcript shows it once, as the red
  turn-end line. Do not "fix" the duplicate in the backend: the record is
  verbatim.
- **`claude auth status` does not validate the cached credential.** A revoked
  OAuth credential can still produce `loggedIn: true`. The observed revoked
  token shape is an assistant record with `error: "authentication_failed"`
  followed by an error result with `api_error_status: 401`. The 401 path must
  clear Claude's cache before any later status check is allowed to call the
  session logged in.
- **Broadcast after spawning the login session.** `handleReauth` calls
  `broadcastSessions` like `handleSpawnPost` does. Without it the page's
  `waitForSession` sits until an unrelated broadcast or its 8s timeout before
  navigating.
- **Codex may not error the turn when signed out mid-session.** A codex
  runtime that failed its launch-time account check holds sent messages
  until a restart resumes the conversation; the banner copy says to restart.
  Whether a mid-session codex sign-out surfaces as a turn error or a silent
  hold was never observed; the focus check catches the silent case.
- **Logged-out output shapes were never observed in the wild.** The parsers
  treat `loggedIn: false` as logged out and anything unparseable as no
  answer. Never make them guess.
- **Codex account responses have one backend parser.**
  When CodeX is configured with a third-party provider (Z.ai GLM via codex),
  `account/read` returns `{"account":null,"requiresOpenaiAuth":false}` —
  Codex is ready and needs no OpenAI auth. `internal/chat/auth.go` supplies
  the same answer to the protocol's message-delivery gate and the Nudge
  tracker, so the session is addressable once the thread id exists and
  remains Idle until work starts. A null account without
  `requiresOpenaiAuth`, or with the field `true`, stays in today's logged-out
  reading. Missing/malformed responses and RPC failures do not establish
  login state; delivery remains blocked while unknown, and RPC errors
  retain their actual error text. Provider scope is also enforced by the
  signed-out recovery path.

### Modifying it

- **To add a sign-out phrasing:** append to the protocol's list in
  `internal/chat/signout.go` and add a case to `signout_test.go`. Explicit
  substrings only, never fuzzy classification; usage-limit texts must never
  match.
- **To add a protocol:** add its status tool and parser to
  `internal/authcheck/authcheck.go`, its statements to `signout.go`, its
  login command to `reauthCommands` in `handlers_auth.go`, and its banner copy
  in `ChatView.tsx`.

## Activity area

The activity area sits between the transcript and composer. It shows the current
foreground phase, independently running workers, pending input, and an optional
collapsed list of plan steps currently in progress. It displays three operations
initially; Show all reveals the rest. Agents and explicit background tasks appear
immediately. Ordinary foreground actions only appear after ten seconds, with a
known launch and a description or command. Coordination calls stay hidden.
Completed, failed, stopped, and unavailable operations leave this panel immediately;
their outcomes remain in the transcript.

### Identity and lifecycle

- A top-level Claude task notification received while idle both updates the task
  outcome and opens an assistant-only turn for the follow-up reply. The subsequent
  assistant/result records render and close that turn normally. Child notifications
  update activity without opening a parent turn; an existing turn is reused.
- `lib/chat/activity.ts` owns session-level operations. Each has a harness
  `(namespace, id)` identity and an explicit nullable `toolId` link to its
  originating transcript tool. Task and agent IDs are not assumed to equal tool IDs.
- Claude launch tools, task-start events, background snapshots, and async launch
  results can arrive in different orders. `upsertClaudeTask` merges their
  observed aliases into one operation and one entry in display order, retaining
  terminal evidence and the original launch link.
- Ordinary Claude tools are derived from the same observed tool segments as the
  transcript; top-level `tool_progress` heartbeats update their duration and
  last-observed time. When `heartbeat: true`, `parent_tool_use_id` identifies
  the command; the heartbeat's own `tool_use_id` is an event ID, not a worker.
  A launch result does not finish an independently tracked
  background task. Codex ordinary tool items use their observed item IDs.
- Activity times come from the durable record's `ts`, with reported harness
  timings retained separately. The row labels update age as “Updated … ago”
  and sampled execution time as “Last reported runtime”; neither is presented as
  a continuously measured runtime. Hooks appear after one second while active.
- Retry status clears on observed recovery, turn completion, or a new accepted
  message. Only in-progress plan steps appear; pending and completed steps stay
  in the transcript.
- Ending a process marks unresolved work as status unavailable. Replacement
  history does not display that old work as active; the next user message starts
  an empty activity lifetime. At the ended marker, each linked transcript tool
  retains a frozen outcome so resetting live activity cannot erase its history.
  Ordinary turn completion preserves independent
  background operations.
- A pending child question marks its owning agent as needing input through the
  explicit tool link. Answering or canceling one request only restores running
  status once that agent has no remaining requests.

### Rendering and navigation

Stop sits at the right edge of the activity header while a turn is running and
connected. It interrupts the current turn, like Escape. The header stays visible
while the task list scrolls; Stop is also available when the turn has no task rows.

`activity-selector.ts` derives the full visible row set and validates tool links
against the transcript. Rows show the launch description, command, latest activity,
and reported runtime directly. `ChatActivity` expands usage and output-file metadata. It does not read output files. Only
linked rows offer Jump to transcript.

`ToolCallRow` and `AssistantTurnView` use the same `operationForTool` lookup.
A late terminal notification updates the originating closed turn's status and
result; the original launch response remains in its expanded details. Memoization
compares the relevant operation references as well as interactive props.

Jump scrolls and focuses the linked tool and suspends following until Resume is
selected. It preserves the composer draft. A single timer updates the activity
area while connected with authoritative history. Reconnect retains the last
observed rows with a stale-status explanation and freezes the clock until
replacement history arrives.

Activity and composer occupy their own non-shrinking layout rows below the
scrollable transcript; activity has a bounded height and scrolls internally.
The transcript observes both viewport and content size changes so activity
expansion, completion, composer growth, and window resizing preserve bottom-following.
Resize-induced scroll events do not count as the user scrolling away. When the
user reads earlier content, resizing preserves that position until Resume.

### Evidence and validation

- Captured Claude records live in `lib/chat/__fixtures__/claude/activity-*.jsonl`.
  Tests preserve their task/tool IDs and event order. The helper in
  `__fixtures__/activity.ts` adds deterministic durable timestamps.
- `activity-heartbeats.jsonl` preserves a foreground Bash launch, task start,
  three distinct heartbeat IDs, and completion from the reported session, with
  original durable timestamps. Its regression checks one worker throughout and
  immediate removal on completion, including after history replay.
- The supplied background and agent cuts contain notifications before the final
  parent result. Tests explicitly delay a notification to exercise completion
  after a closed parent turn; this variant is synthetic, not a claim about the
  captured order.
- `AssistantTurnView.test.tsx` verifies late rendered outcomes and preservation
  of launch responses. `ChatView.test.tsx` verifies actual focus and scroll
  through captured links. Selector tests assert worker rows throughout alias
  convergence, rather than counting inherently unique object keys.
- `useChatSocket.test.tsx` exercises disconnect, replacement durable history,
  live-only events, buffered records, a nonempty live tail, and row removal.
  `activity-lifecycle.test.ts` covers ordinary tools, heartbeats, phase recovery,
  process boundaries, acknowledged failures, hooks, and multiple child questions.
- `test/scenarios/chat-session-activity.md` and its Playwright test exercise the
  production page in both themes with controlled WebSocket delivery: expansion,
  distinct worker identities, focus/navigation, draft retention, and a late result.

### Codex live verification

A successful Codex 0.153.4 app-server probe on 2026-09-09 (configured model:
`gpt-5.6-terra`) captured one child calculation alongside the parent's work.
`__fixtures__/codex/activity-live.jsonl` preserves selected notifications in wire
order, including their original IDs and timestamps; only the home path is
sanitized. The probe used schmux's stable initialization handshake with a
read-only scratch directory and the existing login.

The child launch and completion arrive as `subAgentActivity` items keyed by
`agentThreadId`. An `item/completed` with `kind: "started"` completes the launch;
the child remains running. A later item with `kind: "completed"` closes that
assignment. The same connection carries the child's own turn and message events.
Those update the child activity row; they must not close the parent's transcript,
insert child prose as the parent's answer, or change Stop's parent-turn target.
The launch item links the row to an Agent entry in the transcript.

The capture also verifies synchronous hook start/finish notifications (their
`startedAt` and `completedAt` are seconds, converted to milliseconds), MCP
startup `starting` → `ready`, and a `collabAgentToolCall` wait with empty receiver
and state lists. Reducer and selector tests replay this capture; a backend
regression verifies that child turn events preserve the parent's interrupt target.
A synthetic new child turn verifies that a follow-up reopens the child's
assignment clock while the closed parent transcript stays closed.

Plan updates and collaboration calls carrying populated `agentsStates` remain
schema-tested only. The successful probe emitted no `turn/plan/updated`; the
model reported that `update_plan` was unavailable. This is no longer a network
blocker. Follow-up assignments, interrupted/failed children, and child input
requests still need focused live captures before claiming those paths verified.
