# Codex chat parity: render what Codex sends, and signal the way Claude does

Status: design for review. Builds on `docs/chat-sessions.md` (commit `4517db974`, the Codex protocol adapter).

## Problem

A Codex chat session renders far less than a Claude one, and the session list barely moves for it. Neither is a limit of the wire; both are the translation stopping short.

Evidence, from a real Codex chat session (`schmux-003-ce9d2352`, 3 turns, 313 output lines) and a real Claude one (`schmux-003-3555d20d`) on this machine, plus two throwaway probes against `codex app-server` 0.152.0:

- **Every Codex tool row says `command`.** The session had 9 `commandExecution` items. Each carries `commandActions`, a typed list: 9 `read` actions (with `name` and `path`), 8 `search` (with `query` and `path`), 4 `unknown`. The reducer ignores the list, names every row `command`, and uses the raw `/bin/zsh -lc "…"` string as the summary. The Claude session's rows were `Bash`, `Read`, `Skill`, `AskUserQuestion`, with the command, path, or skill as the summary.
- **Thinking never shows.** All 9 Codex `reasoning` items had empty `summary` and `content`. The Claude session showed 21 thinking blocks. Codex sends reasoning summaries only when asked: a probe that passed `summary: "auto"` on `turn/start` received `item/reasoning/summaryPartAdded` and `summaryTextDelta` notifications and `item/completed` reasoning items with text (`"**Planning patch application steps**"`).
- **Edits render as JSON.** A `fileChange` item carries `changes: [{path, kind: {type: add|update|delete, move_path}, diff}]` with a unified diff. The reducer names the row `edit` and the summary falls through to a slice of the JSON.
- **Four of the nine rows were schmux's own signaling.** The model's status commands (`printf '{"type":"status",…}' >> "$SCHMUX_EVENTS_FILE"`) render as tool rows. The Claude session had zero such rows, because Claude's status comes from hooks and the model never writes it.
- **The session list.** Claude's events file for its chat session shows `working` (spawn, session start, and each prompt with its text), `needs_input` ("Claude needs your permission to use AskUserQuestion", so the `Notification` hook fires under `-p` with the stdio permission tool), `idle` after each turn, and `completed` at the end, all from hooks. Codex's shows `working` from spawn, `resume_id` from its one hook, and whatever the model chose to write. On a one-line task in a probe it wrote nothing.

The first four are the reducer and the protocol adapter not using what the wire provides. The fifth is the signaling design: `docs/chat-sessions.md` kept Codex on the instruction-file strategy ("status keeps the source each harness has"), which was never at Claude's bar for terminal sessions either.

## Design

### 1. Tool rows from `commandActions`

The Codex reducer names and summarizes a `commandExecution` row from its `commandActions`, not from `item.command`. The vocabulary matches Claude's tool names where the meaning matches, so the page reads the same for both harnesses.

| `commandActions`                         | Row `name` | `input` (what `summarizeTool` shows)                                                                                                   |
| ---------------------------------------- | ---------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| one `read`                               | `Read`     | `{ file_path: action.path ?? action.name }`                                                                                            |
| one `search`                             | `Search`   | `{ pattern: action.query, file_path: action.path }` (summary is the pattern)                                                           |
| one `listFiles`                          | `List`     | `{ file_path: action.path ?? "." }`                                                                                                    |
| one `unknown`                            | `Bash`     | `{ command: action.command }` (the inner command, not the shell wrapper)                                                               |
| several, all `read`/`search`/`listFiles` | `Explore`  | `{ command: targets }` where targets is each action's path, name, or query, comma-joined; the name Codex's own TUI uses for this group |
| several including `unknown`              | `Bash`     | `{ command: item.command }`                                                                                                            |
| none                                     | `Bash`     | `{ command: item.command }`                                                                                                            |

`inputJson` is the full item (`command`, `cwd`, `commandActions`) so the expanded row shows everything; `result` and `state` are unchanged (`aggregatedOutput`, `completed` → done, `failed`/`declined` → error). `summarizeTool` is unchanged: it already prefers `command`, then `file_path`, then `pattern`.

The permission card for `item/commandExecution/requestApproval` uses the same mapping (the request carries `commandActions` too), so a card reads "Read needs permission" or "Bash needs permission" with the same summary as the row it sits under.

### 2. Reasoning summaries

The Codex protocol's `UserMessage` encoder adds `"summary": "auto"` to every `turn/start`. Codex documents the parameter as "override the reasoning summary for this turn and subsequent turns"; sending it on every turn costs nothing and survives a resume. The reducer's reasoning rules are unchanged except that summary parts are joined with a blank line, not concatenated, since each part is a headline sentence. The thinking disclosure then shows for Codex exactly as it does for Claude, and the existing empty-thinking-drops rule covers models or efforts that send nothing.

`auto` rather than `detailed`: Codex's own TUI default. Whether a `model_reasoning_summary` setting in the user's `config.toml` or the request parameter wins was not tested; the request parameter is the one schmux controls, so it is the one sent.

### 3. File changes

A `fileChange` item becomes a tool row named by its changes: one change → `Write` (kind `add`), `Edit` (`update`), `Delete` (`delete`); several → `Edit`. `input` is `{ file_path: paths joined with ", " }` so the summary is the path list; `result` is the diffs joined, one file header per change (`--- path` then the diff), so the expanded row shows the unified diff. The `item/fileChange/requestApproval` card uses the same name and summary, with the `reason` line already shown.

### 4. Nothing runs or blocks invisibly

- **Generic tool row.** Any `item/started` whose type is not handled by a specific rule and is not `userMessage`, `agentMessage`, `reasoning`, `plan`, `contextCompaction`, `enteredReviewMode`, or `exitedReviewMode` becomes a tool row named by the item type (`webSearch`, `dynamicToolCall`, `collabAgentToolCall`, `subAgentActivity`, `imageView`, `imageGeneration`, `sleep`, `functionCallOutput`, `hookPrompt`), `input` = the item minus `id`, `type`, and `status`, `result` = the completed item's `output`, `result`, or `text` field when present, otherwise the completed item as JSON. When a capture shows a kind's real fields, it gets its own rule and fixture; until then it is visible.
- **Generic pending card, answered with a JSON-RPC error.** Any recorded harness line with both `id` and `method` that no specific rule handled is a server request; it becomes a pending card with `toolName` = the method, `input` = the params, `abortOnly: true`, and a single Deny button. Each server request kind has its own result schema (`item/permissions/requestApproval` wants `{permissions, scope}`, `mcpServer/elicitation/request` wants `{action}`, `applyPatchApproval` wants a `ReviewDecision`), so a permission-shaped `{"decision":"decline"}` is wrong for all of them. The one answer that is valid for every request is the JSON-RPC error response: `{"id":N,"error":{"code":-32601,"message":"schmux: unsupported request <method>"}}`. Codex's app-server routes an error response to the pending request's callback as an error (`outgoing_message.rs`, `notify_client_error`: it records the request as aborted and delivers `Err` to the waiting core), the same path its own `cancel_request` takes, so the request ends and the turn continues or fails cleanly instead of waiting forever. The card sends a new client frame `{"type":"abort","request_id":…}`; the WebSocket handler calls a new `Runtime.Abort(requestID)`, which calls `Protocol.Abort(requestID)`; Codex encodes the error response above; Claude returns an error ("no abortable requests") since it has no such request kinds. The reducer's control rule extends to "a `control` line with an `id`, no `method`, and either `result` or `error`" so the card is removed when the abort is recorded. Known kinds that hit this today: `item/permissions/requestApproval`, `item/tool/call`, `mcpServer/elicitation/request`, `applyPatchApproval`, `execCommandApproval`. When one of them turns out to matter, it gets a real card and a schema-correct encoder, with a capture.

### 5. Status from hooks, for Codex as for Claude

Codex's descriptor changes `signaling.strategy` from `cli_flag` to `hooks`. `appendSignalingFlags` becomes a no-op for Codex, so `-c model_instructions_file=~/.schmux/signaling.md` is no longer passed, for terminal and chat sessions alike. Status, resume id, and autolearn capture come from hooks merged into `~/.codex/hooks.json` by the existing `global-json-settings-merge` strategy, which today merges one group and will merge the set below.

Codex hooks, from the Codex source (`codex-rs/hooks/src/events/*.rs`, `schema.rs`, `config/src/hook_config.rs`, 0.152.0): the `hooks.json` keys are `SessionStart`, `SessionEnd`, `UserPromptSubmit`, `PermissionRequest`, `PreToolUse`, `PostToolUse`, `Stop`, `Interrupt`, `SubagentStart`, `SubagentStop`, `PreCompact`, `PostCompact`, each a list of matcher groups `{matcher?, hooks: [{type: "command", command, timeout?, statusMessage?}]}`, the same shape Claude uses. Payloads arrive on stdin as snake_case JSON with `session_id`, `turn_id`, `cwd`, `hook_event_name`, `model`, `permission_mode`, plus per event: `UserPromptSubmit` `prompt`; `PermissionRequest` `tool_name`, `tool_input`; `PostToolUse` `tool_name`, `tool_use_id`, `tool_input`, `tool_response`; `Stop` `stop_hook_active`, `last_assistant_message`. A `Stop` or `UserPromptSubmit` hook that prints `{"decision":"block","reason":"…"}` blocks, exactly as Claude's does.

The Codex hook map, mirroring `buildClaudeHooksMap`:

| Event                              | Group                                                                         | Writes                                                                                                                             |
| ---------------------------------- | ----------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `SessionStart`                     | status                                                                        | `working`                                                                                                                          |
| `SessionStart`, `UserPromptSubmit` | capture-session                                                               | `resume_id` from `session_id` (today's `UserPromptSubmit` group, plus `SessionStart` as Claude has)                                |
| `UserPromptSubmit`                 | status                                                                        | `working` with `prompt` as message and intent, floor-manager prefix detected as for Claude                                         |
| `PermissionRequest`                | status                                                                        | `needs_input` with `tool_name` and the command or path from `tool_input` as the message                                            |
| `Stop`                             | status heartbeat, then `stop-status-check.sh`, then `stop-autolearn-check.sh` | `idle`; the gates block until the model has reported and reflected, same scripts, same reason text (`stop_hook_active` is honored) |
| `PostToolUse`                      | `capture-failure-codex.sh`                                                    | `failure` when `tool_response` indicates one                                                                                       |
| `SessionEnd`                       | status                                                                        | `completed`                                                                                                                        |

`capture-failure.sh` reads Claude's `PostToolUseFailure` payload (`error`, `is_interrupt`). Codex has no failure-only event; its `PostToolUse` fires for every tool with `tool_response`. A Codex variant of the script decides failure from the response: for shell tools an `exit_code` other than 0, for others an `error` key; it emits the same `failure` event shape (`tool`, `input`, `error`, `category`) so autolearn needs no change. The exact `tool_response` fields per tool are read from the first capture during implementation; the script is written against that capture and tested with it.

What changes for the model: it no longer receives the signaling paragraph, and learns the status vocabulary the way Claude does, from the Stop gate's reason text if it ever tries to finish without a status on record. In practice the gate is satisfied by the hook-written states (`stop-status-check.sh` accepts a `working` with a message, which `UserPromptSubmit` writes every turn, and `needs_input`), which is why the Claude session ran no status commands at all. Codex gets the same: the status-signaling rows disappear from the chat. The "Web Preview Registration" and "Friction Capture" sections of the signaling file are no longer given to Codex; Claude never had them (friction is enforced by the autolearn gate instead, previews rely on terminal auto-detection, which chat sessions of either harness do not get; that gap is not this spec's).

Trust. Codex runs a user hook only after it is trusted; `hooks/list` reports `trustStatus` and `currentHash` per handler, and a newly merged group is untrusted until the user accepts it in a Codex TUI session, which the merge strategy's comment already describes. The new groups therefore run only after one such acceptance, and the first slice documents that step (in `docs/agent-signaling.md` and the spawn log line for Codex sessions). If Codex's `hooks.state` entries in `config.toml` (`enabled`, `trusted_hash`) can be written by schmux with a hash it can compute, that removes the step; it is verified during implementation, not assumed.

### 6. Answering a card clears "Needs Input"

`PermissionRequest` writes `needs_input`, a tier-1 state. Under the priority rules only `working` (tier 0, the exception), another tier-1 state, or a tier-2 state can replace it; the `idle` heartbeat at `Stop` cannot. Terminal sessions already have the answer: the terminal WebSocket calls `clearNudgeOnInput` when the user presses Enter, Tab, or Escape, which clears the persisted nudge, saves, and broadcasts, and the next hook-written state takes over. Claude chat sessions have the same gap today, because nothing on the chat socket clears the nudge.

The chat WebSocket handler does what the terminal one does: on a `permission`, `answer`, `abort`, or `send` frame it calls `ClearSessionNudge` and, when that cleared something, saves and broadcasts, exactly as `clearNudgeOnInput` does. Answering a card is the chat's Enter. Not a `PreToolUse` hook writing `working` (it would fire on every tool call and re-trigger the nudge sequence), and not a change to the tiers.

### 7. Docs

`docs/chat-sessions.md`: the status decision bullet, the key-files rows for `codex.ts` and `hooks_codex_json.go`, the fixture README. `docs/agent-signaling.md`: Codex moves from the `SignalingCLIFlag` row to `SignalingHooks`, and the one-time hook trust step is documented; the valid-states table gains `idle` (which Claude's Stop hook writes and the dashboard maps) and loses `rotate` (nothing emits or consumes it). `events.ValidStates`, the map that table cites, has no reader in the code and disagrees with it in both directions; this spec deletes it and makes the doc table the reference. `docs/api.md`: nothing changes on the wire.

## Scope

Page: `lib/chat/codex.ts` (rules 1, 3, 4 and summary joining), `codex.test.ts` and new fixtures cut from the probe captures (`commandActions` variants, reasoning summaries, `fileChange` add and update); `types.ts` (`PendingSegment.abortOnly`), `PermissionCard` (Deny-only mode sending `abort`), `socket.ts` and `useChatSocket.ts` (the `abort` frame). Go: `internal/chat/protocol.go` and both implementations (`Abort`), `internal/chat/runtime.go` (`Abort`), `internal/dashboard/websocket_chat.go` (`abort` frame; nudge clear on `send`, `permission`, `answer`, `abort`), `internal/chat/codex.go` (`summary` on `turn/start`), `internal/detect/descriptors/codex.yaml` (`signaling.strategy: hooks`), `internal/detect/hooks_codex_json.go` (the hook map, replacing the single group; the merge logic is reused), `internal/detect/hooks/capture-failure-codex.sh` (new), `internal/detect/adapter_claude_hooks.go` (the status-command builders become shared helpers), `internal/events/types.go` (`ValidStates` deleted). Docs as in section 6.

## What we are deliberately not doing

- Mapping `thread/status/changed` into status; hooks give Codex the same source Claude has.
- Rendering `turn/diff/updated` (the turn's aggregate diff) or plan items; this mode exposes no plan tool.
- Multiple rows per `commandExecution`; one row per item keeps output and result together.
- Filtering the model's status commands out of the transcript; with hooks there are no more of them than Claude has.
- Keeping the signaling instruction file for Codex alongside hooks. Two sources of the same status would double-report.

## Risks and unverified items

- **Hook payload field names** are read from the Codex source, not a capture; the first implementation step captures one payload per event and the scripts are tested against those.
- **`tool_response` shape** for the failure script, as above.
- **Trust review** is a one-time user step per machine until the `hooks.state` write is verified.
- **`summary: "auto"` on every turn** is documented as persisting; if a Codex release rejects it on a resumed thread, the encoder drops it after the first turn.
- **Unknown item kinds** render generically until a capture shows their fields; the generic row may show more JSON than wanted for a while. That is the point.

## Tests

- `codex.test.ts`: one case per row of the `commandActions` table from fixtures; reasoning summary streamed and completed; `fileChange` add, update, and multiple; generic row for an unseen item type; generic pending card for an unseen server request, removed by a recorded `{id, error}` control line; permission card name and summary for a `read` approval.
- `internal/chat`: `turn/start` carries `summary: "auto"`; Codex `Abort` encodes the JSON-RPC error response byte for byte; Claude `Abort` errors.
- `internal/dashboard`: an `abort` frame reaches the runtime; each of `send`, `permission`, `answer`, `abort` clears a set nudge and broadcasts once; a frame with nothing to clear does not save.
- `internal/detect`: the Codex hook map merges every group, keeps user groups byte-for-byte, replaces stale schmux groups, and is idempotent; `SignalingStrategy()` for Codex is hooks; `buildChatCommand` for Codex carries no `model_instructions_file`.
- Hook scripts: `stop-status-check.sh` and `stop-autolearn-check.sh` against captured Codex `Stop` payloads; `capture-failure-codex.sh` against captured `PostToolUse` payloads for a failed and a successful command.
- Manual: a Codex chat session shows Read, Search, Bash, Edit rows with real summaries and thinking; the session list moves through working, needs input, idle, completed without the model writing status.

## Implementation order

1. Reducer and protocol rendering changes (sections 1 to 4) with fixtures from the probe captures. Independently testable; ships value on its own.
2. Hook payload captures (one short session with a temporary dump-to-file hook per event), then the hook map, the failure script, the descriptor change, docs.
