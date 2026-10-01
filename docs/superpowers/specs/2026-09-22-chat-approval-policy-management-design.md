# Chat Approval Policy Management

**Date:** 2026-09-22
**Status:** Proposed design, not yet implemented

## Problem

Chat approval handling is currently lossy and policy-blind.

Codex app-server can send an ordered `availableDecisions` array with a command
approval request. Those values can be strings or structured objects,
such as `{"acceptWithExecpolicyAmendment":{"execpolicy_amendment":[...]}}`.
The Codex reducer drops that array, `PermissionCard` renders generic
Allow/Deny buttons, and `codexProtocol.Permission` converts the boolean to
only `"accept"` or `"decline"`. Native choices such as
`acceptForSession`, structured exec-policy amendments, and `cancel` cannot be
selected.

Claude stream-json requests carry native permission data, including
`permission_suggestions`, while the same generic card reduces the response to
Allow/Deny and an optional edited input. There is no way to see or change
Claude's current permission mode from the dashboard.

Neither protocol's current approval policy is represented in the conversation
model, even though both report it: Claude emits `permissionMode` on
`system/init` and `system/status`; Codex reports
`thread.approvalPolicy` in its thread response and
`threadSettings.approvalPolicy` in `thread/settings.updated`. Policy is also
confused with launch-time Fence behavior: Fence chooses initial harness
arguments/sandbox settings, but it is not a live approval-policy control.

## Current Behavior

The current approval path is:

1. A protocol reducer inserts a `PendingSegment` from Claude's
   `can_use_tool` request or Codex's `requestApproval` server request.
2. `PermissionCard` calls `onPermission` with
   `(requestId, allow, updatedInput, message)`.
3. `ChatSocket` writes `/ws/chat/{id}` frame type `permission` with that
   boolean plus Claude-specific fields.
4. `handleChatWebSocket` calls `Runtime.AnswerPermission`.
5. The protocol adapter creates the native response line, which
   `sendControlLocked` records before writing to the harness input.

The path already has the right durability and replay shape: control lines and
harness lines are stored verbatim, history replays through protocol reducers,
and request resolution comes from native records. The defect is the boolean
decision model and the missing policy projection in the reducers.

One existing detail matters to pending state: `appendLocked` both persists and
fans out a control record before `appendInput` attempts the harness write. A
`control` record therefore proves intent, not delivery. The runtime's nudge
tracker deliberately ignores an unwritten control during replay. This design
uses the same interpretation for policy UI: a control record is audit intent
and never reconstructs an in-flight mutation.

The chat WebSocket currently returns one generic `{"type":"error","message":
"..."}` frame for an action failure. There is no action correlation. The
shared `SessionSidebar` has no chat-only control slot. `Session.Fence` is
spawn-time state used by the launch adapters; no live policy mutation exists.

## Goals

1. Render the choices supplied by the running harness and return the selected
   native decision without reducing it to a schmux boolean.
2. Add a chat-session sidebar control that:
   - shows the current policy reported by the harness;
   - lists the policies supported by that running harness dialect;
   - changes policy using the harness's native mutation;
   - changes the confirmed value only after native acknowledgement;
   - exposes failures while retaining the last confirmed value.
3. Preserve raw structured policy values, including Codex's granular policy.
4. Keep approval policy and Fence independent.
5. Restore confirmed policy from the durable conversation record after a
   dashboard reconnect or daemon/runtime reconnect to the same harness process.

## Non-Goals

- No terminal-session UI or terminal protocol changes.
- No schmux policy engine, policy normalization, policy persistence, or
  cross-harness policy semantics.
- No Fence state mutation, Fence restart, sandbox mutation, or launch-argument
  rewriting.
- No automatic resolution of an approval that is already pending.
- No generic settings management for model, sandbox, reviewer, MCP overrides,
  or other harness settings.
- No synthetic fallback choices derived from a tool name or request method.

## Verified Native Protocols

The two supported chat protocol identifiers are `claude-stream-json` and
`codex-app-server`; both spellings must match `internal/chat` and the
dashboard reducer registry exactly.

### Claude stream-json

The current Claude Code 2.1.278 stream-json contract was verified with the
Agent SDK declarations and a live headless probe.

Current state is harness-reported by:

```json
{"type":"system","subtype":"init","permissionMode":"default"}
{"type":"system","subtype":"status","permissionMode":"acceptEdits"}
```

The supported native mode union is:

```text
default | acceptEdits | bypassPermissions | plan | dontAsk | auto
```

`manual` remains a launch-option alias for `default`; it is not added as a
separate runtime choice because it is not part of the current emitted runtime
mode union.

Claude can also report effective mode restrictions. A stream-json
`get_settings` control request returns effective settings;
`permissions.disableBypassPermissionsMode` and the settings shape's
`disableAutoMode` fields are `"disable"` when policy removes those choices. A
live 2.1.278 probe confirmed that this
request works before the first user message and returns effective settings in
the standard successful `control_response` envelope.

A mode change is an outbound control request:

```json
{
  "type": "control_request",
  "request_id": "<schmux-request-id>",
  "request": {
    "subtype": "set_permission_mode",
    "mode": "acceptEdits"
  }
}
```

Successful acknowledgement is harness output:

```json
{
  "type": "control_response",
  "response": {
    "subtype": "success",
    "request_id": "<schmux-request-id>",
    "response": { "mode": "acceptEdits" }
  }
}
```

The live probe then emitted `system/status` with
`permissionMode: "acceptEdits"`. An invalid mode produced:

```json
{
  "type": "control_response",
  "response": {
    "subtype": "error",
    "request_id": "<schmux-request-id>",
    "error": "Cannot set permission mode: must be one of acceptEdits, auto, bypassPermissions, default, dontAsk, plan"
  }
}
```

No later `permissionMode` status was emitted after that error.

For `can_use_tool`, the native response contract is a Claude
`PermissionResult`: `behavior: "allow"` with optional `updatedInput` and
`updatedPermissions`, or `behavior: "deny"` with `message`. A request may also
carry `permission_suggestions`; those are native "always allow"-style
permission updates, not Codex-style decision values. They may be rendered only
as choices that produce a valid native `PermissionResult` containing that
exact supplied update in `updatedPermissions`. Suggestions must not be
flattened into schmux rules.

### Codex app-server

The current installed Codex CLI is 0.155.1. Its generated app-server bindings
define:

```ts
type AskForApproval =
  | 'untrusted'
  | 'on-request'
  | {
      granular: {
        sandbox_approval: boolean;
        rules: boolean;
        skill_approval: boolean;
        request_permissions: boolean;
        mcp_elicitations: boolean;
      };
    }
  | 'never';
```

The native mutation is:

```json
{
  "id": <next-client-json-rpc-id>,
  "method": "thread/settings/update",
  "params": {
    "threadId": "<parent-thread-id>",
    "approvalPolicy": "<native AskForApproval value>"
  }
}
```

The method response is an empty object. Confirmation of the value is the
harness notification:

```json
{
  "method": "thread/settings/updated",
  "params": {
    "threadId": "<parent-thread-id>",
    "threadSettings": {
      "approvalPolicy": "<native AskForApproval value>"
    }
  }
}
```

`ThreadSettingsUpdateParams.approvalPolicy` is documented as overriding policy
for subsequent turns. A successful JSON-RPC response alone therefore means the
mutation was accepted on the wire, not that a confirmed value is available;
the reducer waits for `thread/settings/updated`.

Codex also exposes configured policy restrictions through:

```json
{"id": <client-id>, "method": "configRequirements/read"}
```

The response is:

```json
{
  "id": <client-id>,
  "result": {
    "requirements": {
      "allowedApprovalPolicies": ["<native AskForApproval value>"]
    }
  }
}
```

`requirements` is null when no requirements.toml/MDM restrictions are
configured. A non-null `allowedApprovalPolicies` array is the running
harness's ordered allowlist; a null field inside a non-null requirements
object means the schema union is unrestricted. A live Codex 0.155.1 probe with
no local restrictions returned `{"requirements":null}`.

Codex command approvals carry an ordered native array:

```json
"availableDecisions": [
  "accept",
  {"acceptWithExecpolicyAmendment": {"execpolicy_amendment": ["..."]}},
  "cancel"
]
```

The selected array element must be returned as the same native JSON value:

```json
{"id": <server-request-id>, "result": {"decision": <selected-element>}}
```

Codex file-change approvals do not carry `availableDecisions` in the current
request payload, and `item/permissions/requestApproval` has a different
response shape rather than a `decision` field. This design does not synthesize
choices for them based on the method name. They retain the existing
abort/unsupported card until Codex supplies choices or a separate native
response renderer is designed.

## Chosen Approach

Keep each harness payload native and put only the minimum common shape at the
UI boundary.

The alternative would be a normalized policy/decision engine. That is
rejected: Codex's granular object and Claude's permission suggestions do not
map losslessly, and every new harness variant would require changing a central
engine. A launch/restart-only policy control is also rejected because it is
not a live harness protocol operation and would couple policy changes to
Fence/restart behavior.

The small shared UI shape is presentation state, not semantics:

```ts
interface NativePolicyChoice {
  // Exact value to send; never reconstructed by the backend.
  value: unknown;
  label: string;
  description?: string;
  // Protocol-specific detail used only by the chat policy control.
  detail?: unknown;
}

interface ApprovalPolicyState {
  current: unknown | null;
  choices: NativePolicyChoice[] | null;
  canUpdate: boolean;
  error?: string;
}
```

`Conversation` gains one `approvalPolicy` field of this type;
`emptyConversation()` starts with `current: null`, `choices: null`, and
`canUpdate: false`. The field is reducer output, not a separate React store.
`choices: null` means unknown/unavailable; `choices: []` means the harness
explicitly allowed no mutations.

Protocol reducers create this state from native records. Equality for
selecting/highlighting a structured current value uses deep JSON equality;
the stored confirmed value itself remains the harness-reported raw object.

The end-to-end policy flow is:

1. Protocol launch/discovery requests obtain native restrictions and current
   thread/session state.
2. Harness output is recorded verbatim and replayed through the protocol
   reducer, which derives only `current`, `choices`, `canUpdate`, and a native
   mutation error.
3. The chat WebSocket sends the existing history frame and then appended
   records.
4. A sidebar selection sends the raw native policy value to the runtime.
5. The runtime records its attempt and writes the protocol's native mutation
   without blocking the socket reader.
6. A native acknowledgement/error record resolves display state; only the
   acknowledgement changes `current`.

### Implementation surface

- Backend protocol boundary: `internal/chat/protocol.go`, `claude.go`,
  `codex.go`, and `runtime.go`.
- Chat socket: `internal/dashboard/websocket_chat.go`.
- Frontend transport/model/reducers: `assets/dashboard/src/lib/chat/socket.ts`,
  `types.ts`, `claude.ts`, `codex.ts`, and `reducer.ts`.
- Chat page integration: `useChatSocket.ts`, `ChatSessionPage.tsx`, and
  `PermissionCard.tsx`.
- Shared sidebar: `SessionSidebar.tsx` gets only the optional chat-control
  slot; a new chat-only `ApprovalPolicyControl` consumes it.
- The new control's granular Codex form uses the existing chat module styling
  rather than a parallel visual language.
- Documentation: `docs/api.md` for the WebSocket contract and
  `docs/chat-sessions.md` for the reducer/state rules.

The surface is broad because one native value must cross the harness adapter,
runtime, chat WebSocket, reducer, and sidebar. It stays narrowly scoped to
those boundaries: no session-state schema, spawn form, terminal page, Fence
manager, or global settings service is added.

## Approval Decision Flow

### Frontend record model

`PendingSegment` gains a protocol-populated native choice list:

```ts
interface NativeApprovalChoice {
  // Exact native response decision/value passed to the daemon.
  decision: unknown;
  label: string;
  description?: string;
  detail?: unknown;
}
```

For Codex command approval, the reducer copies
`params.availableDecisions ?? []` in order. Each element becomes
`NativeApprovalChoice.decision` exactly as received; labels and descriptions
are display metadata and never participate in the response. An empty or absent
array yields an abort-only card, not an inferred set.

For Claude `can_use_tool`, the reducer creates the fixed native
`PermissionResult` choices from the `can_use_tool` response contract:

- allow once: `{"behavior":"allow","updatedInput":<request input>}`;
- deny once: `{"behavior":"deny","message":"Denied from the schmux chat"}`;
- every supplied suggestion whose own `behavior` is `"allow"` additionally
  produces `{"behavior":"allow","updatedInput":<request
input>,"updatedPermissions":[<exact suggestion>]}`. Suggestions with
  `"ask"` or `"deny"` behavior remain visible as native detail but are not
  selectable because they are permission updates, not valid direct
  `PermissionResult` decisions.

The original `permission_suggestions` array is retained unchanged as option
detail for history/replay. Claude `AskUserQuestion` requests with
`requires_user_interaction` continue through the existing `answer` path and
are not treated as permission decisions.

The two base choices come from the native `can_use_tool` response contract,
not from `tool_name`; no tool-specific third choice is added.

"Same value" means no field dropping, type coercion, or replacement with a
tool-specific fallback; JSON whitespace is not part of the native contract.
Array order is part of the choice data and must be retained.

`PermissionCard` renders the protocol-prepared choices in order: Codex uses
the request-supplied `availableDecisions`, while Claude uses its response
contract plus qualifying supplied suggestions. Structured choices get
a readable label from native fields where available (for example,
`acceptForSession`, `acceptWithExecpolicyAmendment`, or an amendment summary),
but that label is not sent. A choice with no stable display name uses its JSON
key or compact JSON text; the raw value remains the response. Buttons disable
after a click until the protocol's existing pending-request lifecycle resolves
it or the socket reports an action error.

### Client and daemon API

The boolean permission frame is replaced by a decision-bearing frame:

```json
{
  "type": "permission",
  "request_id": "...",
  "decision": "<native decision value>"
}
```

`decision` is a `json.RawMessage` in Go and `unknown` in TypeScript. It is not
unmarshalled into a union or boolean. The daemon rejects a `permission` frame
whose `decision` is missing or not valid JSON. It does not silently interpret
the old `allow` field.

`Protocol.Permission` becomes:

```go
Permission(requestID string, decision json.RawMessage) ([]byte, error)
```

Claude validates that `decision` is a JSON object and wraps it in its native
`control_response` envelope. Codex validates the numeric server request ID and
wraps the exact value in `{"decision": <value>}`. Neither method inspects the
original tool type to decide what the value means.

The WebSocket error frame gains an optional `action_id` so an immediate
encode/write failure can be attached to the policy action without changing
generic errors:

```json
{ "type": "error", "action_id": "...", "message": "..." }
```

This `action_id` is scoped to `/ws/chat/{id}` client actions and is unrelated
to the existing spawn usage `action_id`. Permission request removal remains
protocol-specific and unchanged: Codex uses its native
`serverRequest/resolved` record, while Claude uses its existing recorded
control-response lifecycle. That approval lifecycle is deliberately not reused
for policy confirmation.

## Policy Control Flow

### Sidebar placement and scope

`SessionSidebar` gains only an optional content slot for chat-specific
controls, placed above Dispose. `ChatSessionPage` passes an
`ApprovalPolicyControl`; terminal pages pass no such control and remain
byte-for-byte unaffected in behavior.

The control renders only when all of these are true:

- the session is `kind: "chat"`;
- the effective protocol is Claude stream-json or Codex app-server;
- the current harness lifetime's persisted history has reported a current
  policy.

Its mutation choices are enabled only when the protocol-specific version gate
below marks this running harness version/dialect as supporting the native
mutation. A
harness too old or unknown for the verified gate, and a stopped chat, render
the reported value read-only with an explanatory disabled state. A stopped
chat accepts no mutation because `/ws/chat/{id}` already rejects actions
after the process has ended.

The control uses the existing metadata-field/select visual primitives and the
design-system disabled, loading, and error states. For Codex, choosing
`granular` reveals only the five boolean fields named by the native schema.
Submitting constructs the exact native object; schmux does not name or
reinterpret those fields.

Implementation follows `docs/dashboard-style-guide.md`, and the changed
dashboard markup must pass the repository's dashboard style check before
completion.

### Native choice catalogs and capability gates

Claude's launch handshake sends one `get_settings` control request before the
first user message. The reducer starts with the six native `PermissionMode`
values and removes `bypassPermissions` when
`effective.permissions.disableBypassPermissionsMode` is `"disable"`. It removes
`auto` when either effective `disableAutoMode` field exposed by the current
SDK settings shape is `"disable"`. The response can precede
`system/init`, so policy extraction must work with no open assistant turn. The
rest of the settings response is consumed silently. A current disabled mode
remains visible as the harness-reported current value but is not offered as a
choice.

Codex does have a runtime source. Its launch handshake additionally sends
`configRequirements/read` once with a uniquely allocated JSON-RPC ID; the
adapter restores that ID during `Rebuild`. The reducer publishes exactly the
returned `requirements.allowedApprovalPolicies` array when present. When
`requirements` or `allowedApprovalPolicies` is null, it publishes the four
native `AskForApproval` variants from the current schema. An empty allowlist
publishes no mutation choices. A current policy outside the allowlist remains
visible as the harness-reported current value but is not added back as a
choice.

Mutation support is still gated on the verified Claude `claude_code_version`
and Codex `thread.cliVersion` using semantic-version comparison (Claude
2.1.278 and Codex 0.155.1 are the verified minima). A harness that reports an
older or unknown dialect, a Claude settings-read error, or a Codex
requirements-read error can still show a harness-reported current value
read-only with an explanatory disabled state. Lowering either gate later
requires a captured compatibility fixture, not an assumption.

The gate may be conservative for recent intermediate versions. That is
deliberate: claiming an unsupported mutation is a worse failure than requiring
a restart on an unverified harness.

### WebSocket action

A policy change uses a new chat-only client frame:

```json
{
  "type": "set_approval_policy",
  "action_id": "...",
  "policy": "<native policy value>"
}
```

`policy` remains raw JSON end to end. The dashboard generates `action_id`; it
is only for correlating immediate action errors and is not sent to the
harness.

`Protocol` gains one narrow encoder:

```go
SetApprovalPolicy(policy json.RawMessage) ([]byte, error)
```

Claude emits only `set_permission_mode`. Codex requires `Addressable()` and
emits only `threadId` plus `approvalPolicy`. The method never sends sandbox,
CWD, model, reviewer, permissions, Fence, or other settings.

Claude allocates a unique control request ID with the existing record-ID
utility and prefixes it for diagnostics (for example, `policy-...`). Codex
uses its existing monotonic JSON-RPC client ID allocator so daemon/runtime
rebuild cannot reuse an outstanding ID.

The runtime keeps the existing intent-before-side-effect `sendControlLocked`
order. A policy `control` record can therefore remain in history even when its
harness-input write fails; that record is an audit of the attempted native
mutation, not proof of delivery and not pending policy state.

The initiating browser keeps its local updating state until it receives either
an immediate `action_id` error or a correlated native acknowledgement/error
record. Encode, record-append, and input-write failures return immediately to
that browser with the previous confirmed value. A reconnect discards the local
updating state. The runtime does not persist a separate desired policy in
`state.json`.

`useChatSocket` owns that transient action state, not the conversation
reducer: `setApprovalPolicy(policy)` records `actionId` and the requested raw
value; a broadcast outbound policy control record supplies its native request
ID; an immediate action error or correlated native acknowledgement/error
resolves it. `ApprovalPolicyControl` renders that state as "updating" and
disables another submission from the same browser. A local 30-second
acknowledgement timeout resolves it with "No acknowledgement from the harness"
while retaining the confirmed value; a late native acknowledgement or error
still wins when it arrives. The timeout is UI recovery, not a protocol
cancellation. Another browser that did not initiate the mutation remains
usable; the native acknowledgement remains the final arbiter.

The sidebar shows a transient action error while one is active; otherwise it
shows the reducer's durable native mutation error. This keeps delivery
failures and harness rejections separately owned without merging them into a
schmux policy state machine.

## State Ownership

There are deliberately two durable reducer states plus one local state:

- **Confirmed policy**: derived by the protocol reducer from the most recent
  harness-reported record. It is never copied into schmux session state.
- **Native mutation error**: derived from a correlated harness error response,
  retained for display while leaving the confirmed value unchanged.
- **Transient action state**: the initiating browser connection's
  `action_id`, click/disable state, and immediate write error. It is not
  durable and does not survive a reload.

Codex confirmation rules:

1. An outbound `thread/settings/update` never changes `current` by itself.
2. A JSON-RPC error with the same client ID records a native mutation error.
3. An empty successful response records nothing confirmed.
4. A parent-thread response carrying `thread.approvalPolicy`, or a
   `thread/settings/updated` notification carrying
   `threadSettings.approvalPolicy`, replaces `current` exactly and clears the
   native error.
5. A notification with a different `threadId` is ignored.

Claude confirmation rules:

1. An outbound `set_permission_mode` never changes `current` by itself.
2. An error `control_response` with the same request ID records a native
   mutation error.
3. A success `control_response` with `response.mode` confirms that raw mode
   and clears the native error.
4. A later `system/init` or `system/status` `permissionMode` also replaces
   the confirmed value; harness report order wins.

For errors, the control keeps showing the previous confirmed policy, shows
the native error text, and re-enables after the user acknowledges it. The
error is not written into `state.json`.

## Reconnect and Replay

The existing history frame remains the source of rebuild state. Protocol
reducers replay all records in order and derive the final current policy and
last relevant native mutation error. Outbound policy control records are audit
events and are skipped for pending-state reconstruction.

Browser WebSocket reconnect:

- `historyLoaded` is invalidated on disconnect as today.
- The new history replaces the whole conversation, including policy state.
- Local button pending/`action_id` state is discarded.
- If the native acknowledgement arrived while disconnected, replay shows the
  confirmed value.
- If no native acknowledgement/error arrived, replay shows the last confirmed
  value with no pending mutation. This is true even when an attempted control
  record exists, because that record does not prove the harness-input write
  succeeded.

Daemon/runtime reconnect to the same harness:

- The existing bridge/input/output files are replayed.
- Codex `Rebuild` continues to restore thread ID and advance `nextID` past
  every written client request, including policy updates.
- Claude's stateless protocol reconstructs request correlation from recorded
  control lines.
- No old policy request is automatically reissued.

Harness/process restart is a new harness lifetime, not a reconnect. The new
init/thread report is authoritative. Restart continues to use the spawn-time
Fence/default launch settings and does not attempt to replay a live policy
change as a durable schmux preference.

## Active Turns, Fence, and Independence

A policy mutation is allowed while a turn is active unless the native dialect
or adapter cannot address it. It does not interrupt the turn and does not
alter Fence state.

An already pending approval remains pending and must be answered with one of
its supplied native decisions. The policy control never emits an approval
response, and the approval card never emits a policy mutation.

For Codex, the confirmed policy affects subsequent turns as documented by its
native settings contract. For Claude, the acknowledgement updates the current
session mode, but the design still makes no claim about retroactively changing
an already issued approval request. That request's native decision contract
remains authoritative.

Fence independence is structural:

- changing Claude policy writes only `set_permission_mode`;
- changing Codex policy writes only `thread/settings/update.approvalPolicy`;
- neither action reads or writes `Session.Fence`, Fence launch state, sandbox
  policy, or Fence monitor state;
- changing Fence remains a launch/restart concern and never emits a policy
  control line;
- tests assert both directions of non-contamination.

## Compatibility

- Old dashboard + new daemon: an old boolean `permission` frame receives an
  explicit action error because `decision` is absent. It is not interpreted as
  accept/decline.
- New dashboard + old daemon: `set_approval_policy` receives the existing
  unknown-frame error. The control's disabled/error state must degrade
  cleanly; unknown/missing policy records must not crash reducers.
- Existing history remains readable. Old conversations have no policy state
  and old approvals have no native choice list; those requests render
  read-only/abort-only rather than being upgraded by inference.
- Existing question and interrupt frames remain unchanged.
- API documentation for `/ws/chat/{id}` must be updated in the same change
  because the client and server frames change, and its `control` record
  description must say the record is an attempted schmux send rather than
  proof that the harness input write succeeded.
- Structured native values are compared by deep equality, not string identity,
  so key order and formatting cannot hide a confirmed match.

Rolling daemon/dashboard upgrades are not promised policy-mutation
compatibility across mixed versions. They are promised not to send a malformed
or silently downgraded native decision.

## Tests

Run the full repository gate before completion (`./test.sh`), with focused
iteration first. Add or update tests as follows; every test must follow
`docs/testing.md`.

### Go protocol and runtime

- Claude `Permission` wraps an exact structured `PermissionResult` unchanged.
- Codex `Permission` round-trips every fixture `availableDecisions` element,
  including a structured amendment, JSON-semantically unchanged.
- Both reject missing/invalid decision JSON; Codex rejects non-numeric request
  IDs.
- Claude `SetApprovalPolicy` emits the exact `set_permission_mode` request and
  no Fence-related fields.
- Claude launch includes one `get_settings` request before the first user
  message.
- Codex `SetApprovalPolicy` emits exact `thread/settings/update` params, only
  `threadId` and raw `approvalPolicy`, and returns `ErrNotAddressable`
  before thread/account readiness.
- Codex launch includes one `configRequirements/read` request with a unique,
  rebuildable JSON-RPC ID.
- Codex `Rebuild` advances `nextID` beyond recorded requirements-read and
  policy requests and is idempotent when replayed.
- Runtime policy-control ordering: encode failure records/writes nothing;
  intent append failure writes nothing to the harness; input write failure
  returns an `action_id`-correlated error and leaves the prior confirmed
  policy. An attempted control record alone must not mutate policy state.
- Fence independence with fake protocols: policy mutation does not call Fence
  launch/restart paths or alter sandbox/Fence fields.

### Frontend reducers

- Codex command approval preserves the native choice array, order, strings,
  and structured objects.
- Missing Codex choices produce abort-only rather than tool-derived choices.
- Claude preserves `permission_suggestions` and creates only valid native
  `PermissionResult` option values.
- Unknown approval values are retained without crashing.
- Claude removes `bypassPermissions` and `auto` only when the corresponding
  effective settings field is `"disable"`, and retains a disabled current mode
  as read-only display state.
- A failed or missing Claude settings read leaves current policy visible but
  read-only and does not create a native mutation error.
- Codex derives initial current policy from the parent-thread response and
  later policy from parent-thread settings notifications; it ignores other
  threads.
- Codex publishes `allowedApprovalPolicies` exactly when supplied, falls back
  to the schema union on null requirements, and publishes no choices for an
  empty allowlist.
- A failed or missing Codex requirements read leaves current policy visible
  but read-only and does not create a native mutation error.
- Codex success response alone does not confirm; notification does.
- Claude success response confirms; error leaves the prior value and exposes
  native error text.
- An outbound policy control record alone never creates pending policy state
  during live updates or history replay.
- Full history replay restores the final current policy and final native
  mutation error, plus the supported choices from Claude settings or Codex
  requirements.
- Old conversations without policy/choice fields reduce to the existing model.

### WebSocket

- History still includes records verbatim and selects the effective protocol.
- New permission action reaches `Runtime.AnswerPermission` with exact raw JSON.
- Missing `decision` and old boolean-only frames return explicit errors.
- New policy action reaches `Runtime.SetApprovalPolicy` with exact raw JSON.
- Immediate errors preserve `action_id`; generic errors continue to work.
- Stopped sessions do not execute actions.

### UI

- The chat sidebar shows current, supported choices, updating state, confirmed
  update, native error, and prior value on error.
- The local acknowledgement timeout restores the confirmed value with an
  error, and a late native acknowledgement supersedes that timeout error.
- Structured Codex granular policy round-trips all five native booleans.
- A current policy outside the Codex allowlist remains displayed while only
  allowed values are offered.
- Clicking one policy choice disables duplicate submissions until
  acknowledgement/error.
- Reconnect history replaces policy state without stale local optimism.
- Terminal session pages render no policy control.
- A running but unverified harness dialect and a stopped chat render policy
  read-only.
- Changing policy leaves Fence status and restart metadata unchanged; Fence
  action leaves policy state unchanged.
- PermissionCard renders native decision labels in supplied order and returns
  the exact selected value to the socket callback.
- Question cards continue using the answer path.

### Protocol fixtures

Add captured or minimal schema-accurate fixtures for:

- Claude `set_permission_mode` success, status confirmation, and native error;
- Claude `get_settings` success with mode restrictions enabled and disabled;
- Claude approval with at least one structured permission suggestion;
- Codex command approval with strings plus a structured amendment;
- Codex settings update response plus confirming notification;
- Codex `configRequirements/read` with null requirements and, separately, a
  non-null `allowedApprovalPolicies` array;
- an old conversation with no policy fields.

The Claude live captures used for this design are throwaway `/tmp` probes;
committed fixtures must be minimized and checked against the SDK declarations.
