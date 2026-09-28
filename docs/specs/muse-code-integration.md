# Muse Code integration

Status: draft for future implementation. No integration is implemented or approved for implementation by this document. The user is evaluating Muse in a normal shell before deciding whether to proceed.

Investigation date: 2026-09-28. Tested on macOS with Muse Code `1.4.0 (1.4.0-R4302.1)` and the subscription-selected `meta / muse-spark-1.3-contributor` model. These are observations of one version, not a promise about later releases or model availability.

Companion: [feasibility and evidence report](../../review/muse-code-feasibility.html). That self-contained report preserves the investigation results. This document describes the proposed behavior, implementation boundaries, protocol details, and remaining decisions.

## Decision and scope

Native chat is feasible. A live MSP client successfully exercised streamed replies, context across turns, a user question, approval and execution of a shell command, denial preventing a shell command, interruption, and resume with retained conversational context. A separate storage probe round-tripped six published Muse trace scenarios through schmux's existing Go conversation log.

These probes did not wire Muse into schmux's dashboard. They did not evaluate the model's usefulness on real project tasks. Model quality remains the user's adoption criterion; successful protocol exchanges do not answer that question.

If implementation proceeds, target a local `kind: "chat"` session using `muse serve`. Reuse the existing bridge, persisted conversation record, WebSocket, conversation model, and chat page. Add a Muse protocol implementation and a Muse event reducer. Do not emulate the Codex protocol or parse terminal escape sequences into a conversation.

| Option               | Required work                                                                                                    | Position                                                               |
| -------------------- | ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| Terminal             | Descriptor, launch/resume arguments; separately validate status hooks and resume-ID capture                      | Smallest entry point. Normal Muse use in a shell is already available. |
| Read-only transcript | Item/event interpretation and a history/live source; this checkout has no generic transcript-import session kind | Could be an intermediate milestone of the chat implementation.         |
| Native chat          | Descriptor, MSP client, reducer, control mapping, lifecycle integration                                          | Preferred if Muse proves useful.                                       |

### Initial feature boundary

- Launch the installed Muse binary in the selected local workspace, using its existing authenticated account and configured model unless a model is explicitly selected.
- Send text, render streamed and completed replies, show tool requests/results, answer supported questions and approvals, and stop the foreground turn.
- Keep subsequent sends in schmux's existing dispatch queue until the current turn settles. Do not introduce MSP steering or a second queue UI for the first integration.
- Preserve display history on browser reload, daemon restart, session end, and schmux Restart. Resume the exact Muse conversation on Restart.
- Surface startup, transport, model, control-command, and compatibility failures with enough information to act on them.

Out of initial scope: remote chat, workflow/subagent orchestration controls, model-provider routing through other harnesses, one-shot integration, importing arbitrary TUI logs, transcript import from another application, and model benchmarking. Background Muse activity still needs a safe display even when its controls are not exposed.

Image input, model/effort switching, terminal lifecycle hooks, and advanced approval scopes are available or suggested by the external surfaces, but were not validated end to end. They require an explicit scope decision and appropriate UI behavior; existing shared controls must not silently promise unsupported behavior.

## Evidence baseline

The installed binary's schema export and live `initialize` response agreed on:

```text
schema.version: 1
schema.fingerprint:
sha256:99a7458c70a670dda3dda45512bdd1e270aba156f46a1324515de45dce95a658
sessionDurability: durable
```

| Observation                                                       | Result                                                                                     | Boundary of the claim                                       |
| ----------------------------------------------------------------- | ------------------------------------------------------------------------------------------ | ----------------------------------------------------------- |
| Prompt asks Muse to remember `ORCHID-731` and reply `READY`       | `item/delta`, authoritative `agentMessage`, successful `turn/completed`                    | A short reply; not a long-output stress test                |
| Follow-up asks for the remembered token without repeating it      | Reply `ORCHID-731`                                                                         | Context within one session                                  |
| `request_user_input` asks Blue/Green                              | Answer command selects Blue; settled event reports answered; reply is Blue                 | Single-choice input only                                    |
| Shell approval selects the offered `allow_once` choice            | `bash` finishes with exit code 0; marker file contains exactly `APPROVED`                  | One shell requirement; no persistent grant                  |
| Another shell approval selects offered `abort`                    | Resolution has `policyResult: "deny"`; denied file does not exist after the turn completes | The prompt also instructed the model not to retry           |
| Stop during a model step                                          | Interrupt admitted; turn completes with `terminal: "cancelled"`                            | Not interruption of an executing tool                       |
| Close stdin, start a new host, resume the session                 | Clean exits, inline retained history; a subsequent turn remembers Blue                     | Clean host restart; not crash or pending-approval recovery  |
| Six public conformance scenarios through existing log append/read | 63 persisted server frames retain their JSON structure; 5 deltas intentionally omitted     | Storage compatibility only; no Muse runtime/reducer existed |

The original scratch client, raw JSONL, exported schema, and marker file were removed during documentation consolidation. The report retains outcomes and abridged wire evidence. It is not a replay fixture. Capture fresh exchanges against the chosen supported release before writing integration fixtures.

The public corpus inspected was [commit a7c10c5dd3f66be412077d29f9d11111af70317b](https://github.com/meta-models/muse-code-sdk/tree/a7c10c5dd3f66be412077d29f9d11111af70317b/schema/msp/transcripts): `text-run-single-turn`, `approval-round-trip`, `approval-deny-round-trip`, `cancel-mid-turn`, `userinput-answer-round-trip`, and `resume-after-cursor`. Its schema fingerprint differed from the installed binary. Use it for supplemental cases, not as an unquestioned description of the current host.

## Existing schmux boundaries

| Area                      | Existing integration point                                                                                                                                              | Proposed change                                                                                                                                                    |
| ------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Agent registration        | [descriptor.go](../../internal/detect/descriptor.go), [adapter_generic.go](../../internal/detect/adapter_generic.go), [descriptor guide](../dev/adapter-descriptors.md) | Add a Muse descriptor and recognize a new `muse-msp` chat dialect. A YAML override alone cannot implement the dialect.                                             |
| Launch                    | [session/chat.go](../../internal/session/chat.go), [chat/protocol.go](../../internal/chat/protocol.go)                                                                  | Implement `Protocol` in a new `internal/chat/muse.go`; register it in `ProtocolFor`.                                                                               |
| Transport and persistence | [bridge.go](../../internal/chat/bridge.go), [record.go](../../internal/chat/record.go), [runtime.go](../../internal/chat/runtime.go)                                    | Retain the file bridge and ordered record. Add only the control/recovery behavior MSP requires.                                                                    |
| Session status            | [nudge.go](../../internal/chat/nudge.go), [signout.go](../../internal/chat/signout.go), [session/chat.go](../../internal/session/chat.go)                               | Interpret parent turn state, pending controls, usage if supported, and observed authentication failures.                                                           |
| Frontend interpretation   | [types.ts](../../assets/dashboard/src/lib/chat/types.ts), [reducer.ts](../../assets/dashboard/src/lib/chat/reducer.ts)                                                  | Add protocol union/dispatch entries and `muse.ts`; produce the existing conversation and activity shapes.                                                          |
| User actions              | [PermissionCard.tsx](../../assets/dashboard/src/components/chat/PermissionCard.tsx), [websocket_chat.go](../../internal/dashboard/websocket_chat.go)                    | Carry enough Muse identity and choice information for safe decisions and answers. Extend shared contracts only where the existing ones lose necessary information. |
| Restart                   | [handlers_restart.go](../../internal/dashboard/handlers_restart.go), [state.go](../../internal/state/state.go)                                                          | Persist Muse protocol/session identity and retain the existing guard against switching dialects during Restart.                                                    |

The current `Protocol` interface is the right starting point, but it is not the whole change. `runtime.go` has protocol-specific control resolution; `nudge.go` has dialect-specific status interpretation; frontend request-resolution dispatch also needs a Muse entry. Review these call sites rather than assuming registration in `ProtocolFor` is sufficient.

The current Codex implementation serializes dispatch using `Addressable()` while a turn is active or awaiting admission. Follow that current pattern for Muse; older narrative references to mid-turn steering are not the behavior to copy.

## Launch, authentication, and network

Use the installed binary and native account state. Do not embed an API key into the descriptor, command line, or persisted conversation. Successful probes inherited the already authenticated subscription account without introducing an SDK dependency into schmux.

Observed CLI surfaces:

```text
muse [OPTIONS] [PROMPT]
muse resume --last
muse resume <session-ref>
muse serve [OPTIONS]
muse schema generate-json-schema --out DIR
muse schema generate-ts --out DIR
```

`serve` speaks JSON-RPC 2.0 as one UTF-8 JSON object per line over stdin/stdout. Stderr is separate. Its sandbox posture and session durability are launch-time properties; approval mode is selected over MSP. Do not copy terminal auto-approval flags onto `serve` or assume all CLI flags apply to every subcommand.

The existing file bridge can carry these lines. A direct Go implementation avoids adding a Node sidecar. Preserve the wrapper behavior that ends the pane when the host exits and does not leave the input tail running.

Start in the selected workspace. Model/provider selection belongs in the session request, not arbitrary `serve` flags. When no model was supplied, this installation selected `muse-spark-1.3-contributor`; do not hardcode that observation into a lasting default catalog.

The probes demonstrated that normal Muse sandboxing and explicit tool approvals can work inside this fenced session. Whether production should preserve Muse approvals or follow schmux's existing fenced auto-approval convention remains an explicit design decision. Do not silently inherit another harness's bypass flags. Workspace trust/instruction/persona injection also needs a focused probe before choosing descriptor paths or launch flags.

### Network prerequisite proved by failure and retry

Muse inference used `https://api.meta.ai/v1/responses`. The inherited proxy initially rejected the CONNECT tunnel with HTTP 403, which Muse reported as a transport error. Adding a subscription did not fix that denial. After the user allowed `api.meta.ai`, the same minimal protocol exercise completed successfully.

Account for this exact host in the Muse descriptor's fence domains or the model endpoint configuration, following [the fence design](../fenced-sessions.md). `dev.meta.ai` served documentation; it did not authorize `api.meta.ai`. Further login/update destinations need evidence before adding them. Never classify a proxy denial as an expired login merely because a model request failed.

## MSP protocol behavior

The following examples preserve observed field names but replace environment-specific identities with placeholders. They are explanatory templates, not executable fixtures. Generate valid UUIDv7 command IDs and substitute actual server-issued identities.

### Handshake and identifiers

1. Send `initialize` with `clientInfo.name: "schmux"` and a client version.
2. Wait for the response; record the host version, schema fingerprint, and durability profile.
3. Send the `initialized` notification.
4. Start a new session or resume the exact requested session.
5. Open the dispatch gate only after session identity is known and the host can accept a turn.

The local host rejected a client name containing hyphens; it required `^[a-z0-9_]+$`. Request IDs correlate individual JSON-RPC exchanges. Server request IDs are a separate direction of traffic. Session, turn, item, approval, and user-input IDs identify different entities; do not collapse them into one ID namespace.

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"schmux","version":"<version>"}}}
{"jsonrpc":"2.0","method":"initialized"}
{"jsonrpc":"2.0","id":2,"method":"session/start","params":{"commandId":"<uuid-v7>","workspaceRoot":"<absolute-workspace-path>"}}
```

Commands that mutate state require a UUIDv7 `commandId`. Query request IDs are not command idempotency keys. Keep the same command identity when recovering an already submitted operation. In particular, a freshly minted command ID on replay can duplicate a turn or action.

### Send, queue, and stop

```json
{"jsonrpc":"2.0","id":3,"method":"turn/start","params":{"sessionId":"<session>","commandId":"<uuid-v7>","input":[{"type":"text","text":"Hello"}]}}
{"jsonrpc":"2.0","id":4,"method":"turn/interrupt","params":{"sessionId":"<session>","commandId":"<another-uuid-v7>","turnId":"<active-turn>"}}
```

An accepted `turn/start` is admission, not completion. Capture its turn ID and follow the parent `turn/started` and `turn/completed` events. Keep subsequent local messages queued until that turn settles. An interrupt acknowledgement is also admission; wait for the terminal event before marking the turn stopped. The observed cancellation reason was `cancelled during model step`.

Preserve the existing record-before-dispatch behavior so user messages appear immediately and survive reload. The implementation must prove the association between a schmux message and its MSP command survives the record/input crash boundaries. Use the existing dispatch bookkeeping where possible; the exact durable representation is an open implementation detail to resolve before coding recovery.

### Transcript and activity

| MSP event/item                                   | Treatment                                                                                                                                                            |
| ------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `item/delta`                                     | Append a live field delta, correlated by item identity; omit from durable conversation storage only when final/full item recovery preserves content.                 |
| `item/started`, `item/updated`, `item/completed` | Fold full item objects by identity and revision. Accept higher revisions; ignore older/equal ones. Accept completion even without a prior start.                     |
| `agentMessage`                                   | Assistant prose; the completed object's `text` is authoritative.                                                                                                     |
| `userMessage`                                    | Match the local send through command identity; do not duplicate the user message already recorded by schmux.                                                         |
| `reasoning`                                      | Map to the existing thinking surface; account for field-specific deltas. Schema-described, not demonstrated by the tiny reply probes.                                |
| `toolCall`                                       | Name from `tool`, arguments from `args`, result from `visibleOutput`; preserve status, failure information, and references to paged output.                          |
| `reminderChild` and other background items       | Activity/fallback presentation, with child identity distinct from the parent's tool/turn. A cancelled reminder was observed in a successfully completed parent turn. |
| Unknown item kind                                | Render the kind, status, and available fallback text; preserve the record rather than crashing or silently dropping it.                                              |
| `turn/completed`                                 | Close the parent turn using its terminal outcome. Do not derive the parent outcome from arbitrary child events.                                                      |

The same reducer must produce consistent results from live delivery and persisted history. Do not concatenate final text onto already accumulated deltas. A successful answer can arrive before background work settles and before `turn/completed`; displaying the answer must not prematurely change the session to idle.

In the current runtime, a line classified `LiveOnly` is forwarded before the `Observe` path and never reaches that method. Any Muse cursor/addressing design must account for this: do not assume `Observe` sees every view event. Keep a recoverable committed position and validate the chosen gap-recovery strategy against the actual delivery paths.

MSP view cursors, item revisions, JSON-RPC request IDs, and schmux WebSocket record sequence numbers solve different problems. Keep them separate. Gap repair and paged history belong to the Muse event interpretation/recovery path; do not invent sequence correspondence between the two protocols.

### Approvals: receipt, decision, resolution

An `approval/request` is a server JSON-RPC request. Replying with an empty result acknowledges its presentation; it does not approve the action. `approval/requested` can also appear as a view event. Both represent the same approval, and their relative arrival order must not produce duplicate cards.

The live shell request supplied a `subject`, `currentRequirementId`, and `availableChoices` containing `allow_once` and `abort`. Preserve the requirement and choice identity from the server. An approval may need to display without a completed tool item: denied shell probes did not produce an executed-tool result.

```json
{"jsonrpc":"2.0","id":"<server-request-id>","result":{}}
{"jsonrpc":"2.0","id":5,"method":"approval/decide","params":{"sessionId":"<session>","commandId":"<uuid-v7>","approvalId":"<approval>","requirementId":{"approvalId":"<approval>","sourceIndex":0},"choiceId":"allow_once"}}
```

Use the current requirement, not a cached stage number. Select an offered choice; do not synthesize `allow_once` or `abort` when absent. The initial two-button UI may map unambiguously to an offered once-only approval and denial. Other menus/stages require supported presentation or an explicit unsupported-action outcome; never silently broaden the grant.

Track decision submission separately from final resolution. Only an authoritative `approval/resolved` event or a documented conflict response should settle the approval. A send failure must not look like a successful answer; an RPC error must not leave a permanently dismissed card. Stale requirements and already-resolved conflicts need deliberate handling based on the selected release's schema.

This is a material integration gap: schmux currently accepts a boolean in `Protocol.Permission`, and its control-resolution helpers understand Claude/Codex response shapes. Decide where to preserve Muse's server-owned requirement/choice data, then extend the contract only as far as necessary. `Protocol.Observe` can emit receipt replies, but recovery must not resend historical presentation requests into a new connection.

### User questions

The live request used `toolName: "request_user_input"`, one question with `selection.mode: "single"`, and labeled options Blue/Green. The client sent a presentation receipt and then a separate command:

```json
{
  "jsonrpc": "2.0",
  "id": 6,
  "method": "userInput/answer",
  "params": {
    "sessionId": "<session>",
    "commandId": "<uuid-v7>",
    "userInputId": "<question-request>",
    "answers": [{ "questionId": "color", "selectedLabel": "Blue" }]
  }
}
```

The terminal event was `userInput/settled` with `outcome: "answered"` and the selected answer. Use the user-input ID for the request and the question ID for each answer. Preserve server-side settlement through reload, including settlement not initiated by the current browser.

The exported schema also describes `selectedLabels`, `freeText`, and optional notes. Do not flatten these into indistinguishable strings. Confirm how existing question controls represent them before enabling those modes. Cancellation, auto-resolution, and unsupported question shapes need visible, recoverable behavior.

## Persistence and lifecycle

| Boundary                                   | Required behavior                                                                                                                                                                                |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Browser reconnect                          | Rebuild from schmux's persisted record and its existing `since` sequence mechanism; keep one copy of each user message, assistant item, and pending card.                                        |
| Daemon restart while Muse survives in tmux | Rebuild protocol state from retained bridge/record data. Continue tailing the same process; do not perform a fresh handshake or replay admitted turns.                                           |
| Schmux Restart                             | Preserve old display history, mark the old lifetime ended, and use the exact native session ID to resume in a new host. Avoid duplicating history returned by Muse and the seeded schmux record. |
| External/most-recent resume                | Select a workspace-appropriate retained root session using the actual `session/list` contract. This selection and import policy were not probed; resolve them before exposing the option.        |
| Muse exits or is disposed                  | End open display state and disable sends. Clean up the bridge tail according to the existing process lifecycle.                                                                                  |
| Ephemeral host                             | Keep schmux's display history, but do not promise native resume. Communicate or disable unsupported Restart behavior.                                                                            |

The successful host-resume request used `session/resume` with a fresh command ID, the original session ID, and `excludeItems: false`; history was inline. This proved native persistence and context. It did not prove how schmux should seed/import that history. For Restart, using `excludeItems: true` with the existing seeded record is a candidate, subject to proving restoration of pending controls and recovering any missing tail.

The durability field is part of the handshake. The documented contract treats its absence as durable and an unknown value conservatively; verify that rule against the chosen release. A graceful stop/resume is not evidence for process-crash recovery, writer-lease conflicts, or resuming a pending protected write.

## Versioning and failure behavior

Record the tested host version/fingerprint and export the schema for the release used during implementation. Decide the supported compatibility range and mismatch behavior explicitly. The online documentation, public corpus, and installed binary had different fingerprints during this investigation; a hash mismatch alone does not establish which methods changed.

When startup cannot produce an addressable session, show a startup error and retain the unsent message rather than leaving an unexplained busy state. Keep stderr available separately from JSON events. Inspect actual error kinds/messages before adding signed-out matching; no live authentication-expiry fixture was captured.

Frame limits, truncated outputs, gap events, and paged reads are described in MSP. They were not stressed by the probes. Do not assume a resumed response always includes all items or that tool output always fits in one frame. Do not present a partial history as complete when the host reports truncation.

## Open decisions before implementation

1. Does Muse prove useful on the user's real tasks? Integration remains deferred until that decision.
2. Which installed releases and fingerprints will be supported, and how will mismatches surface?
3. What approval and sandbox posture should a fenced Muse chat session use? The live probe preserved approvals; another harness's default is not a decision for Muse.
4. Which existing persisted dispatch fields can preserve command identity and uncertainty across every record/input crash boundary?
5. What minimal control-contract extension preserves offered choices, changing requirements, answer types, and authoritative settlement?
6. Will initial scope expose most-recent/external resume or only new chats and exact-ID Restart? How will returned history and pending controls be merged without duplication?
7. How should Muse load schmux persona/instructions and provide terminal status hooks without conflicting with workspace trust or instruction precedence?
8. Which optional controls, especially images and effort/model selection, will be supported initially, and how will unavailable ones be represented?

## Acceptance and verification

These are future acceptance requirements, not claims that tests already exist. Follow [docs/testing.md](../testing.md), which is the sole test-authoring rubric. Recapture the relevant behavior on the supported Muse release and keep deterministic fixtures free of credentials and environment-specific paths.

| Behavior                | Minimum evidence / appropriate gate                                                                                                                                                                    |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Descriptor and launch   | Quick: strict descriptor parsing, protocol registration, correct new/resume launch parameters, no inappropriate terminal flags.                                                                        |
| Handshake and dispatch  | Quick: fragmented input lines, readiness, two queued user messages, admission errors, stable command association, no send before initialization/session readiness.                                     |
| Text/history            | Quick: live deltas converge to completed text; duplicate/equal/older revisions do not duplicate content; completion without start; seeded history boundaries.                                          |
| Controls                | Quick: receipt never settles a card, request/view duplicates collapse, exact offered choice and requirement used, send/RPC errors recover, final settlements clear the correct card.                   |
| Questions               | Quick: correct question IDs and answer variants; settlement restores consistently after replay; unsupported forms do not silently become another answer type.                                          |
| Lifecycle and status    | Quick: parent success with cancelled child, real turn cancellation, unknown item kinds, host exit, protocol mismatch and ephemeral restart policy.                                                     |
| Recovery                | Quick plus E2E where needed: daemon reattachment sends no duplicate command; exact-ID Restart does not duplicate transcript; crash-boundary and pending-control cases are explicit.                    |
| Full user flow          | Scenarios: spawn Muse chat, send/follow up, see tool output, allow/deny, answer a question, stop, navigate/reload/reconnect, and Restart with history/context.                                         |
| Real host compatibility | Manual version-pinned probes: successful inference and tool execution with the intended fence/workspace/account settings. Preserve failures; do not treat mocked UI scenarios as proof of a real host. |

Wait for semantic events such as `turn/started`, `approval/request`, `userInput/settled`, and `turn/completed`; deadlines only fail stalled work. Inspect denied-file absence after the terminal boundary. Ordinary repository gates should use deterministic hosts/fixtures, not an ambient paid account. Keep user-owned processes and account state outside automated test cleanup.

Implementation completion also requires the repository's prescribed tests, lint/typecheck, test-rules review for changed tests, dashboard style review for changed UI, regenerated API types when Go contracts change, and corresponding [API documentation](../api.md) updates. A passing manual Muse probe is not a substitute for those gates.

## Resuming the investigation

No new paid model calls are needed merely to read or edit this draft. When implementation is chosen:

1. Record `muse --version` and export that binary's schema to a disposable location under `review/`; compare its fingerprint and changed surfaces with this document.
2. Recheck the current schmux protocol, dispatch, control, and restart code rather than relying on old line numbers.
3. Establish the selected sandbox/approval/trust posture, then capture the missing risky cases before committing to the recovery/control design.
4. Implement the smallest complete local chat scope and derive deterministic tests from the supported release's captures.

Reference material inspected during the original investigation:

- [Muse Code overview](https://dev.meta.ai/docs/muse-code), [configuration](https://dev.meta.ai/docs/muse-code/configuration), and [headless/hooks](https://dev.meta.ai/docs/muse-code/extending).
- [MSP wire guide](https://meta-models.github.io/muse-code-sdk/next/guides/msp-wire/), [framing](https://meta-models.github.io/muse-code-sdk/next/guides/msp-wire/framing-and-caps/), and [quickstart](https://meta-models.github.io/muse-code-sdk/next/guides/quickstart/).
- [Item fold](https://meta-models.github.io/muse-code-sdk/next/guides/msp-concepts/fold-model/), [approvals](https://meta-models.github.io/muse-code-sdk/next/guides/msp-concepts/approvals/), and [durability](https://meta-models.github.io/muse-code-sdk/next/guides/msp-concepts/durability-profiles/).
- [Public SDK/corpus repository](https://github.com/meta-models/muse-code-sdk) and the pinned corpus commit above.

The linked external pages may evolve. The versioned local observations and explicit limits in this document are the preserved evidence.
