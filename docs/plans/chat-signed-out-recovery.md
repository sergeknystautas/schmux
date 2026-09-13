# Chat Signed-Out Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persisted per-chat-session `signed_out` flag that is set by the session's own failed turn, corrected by the harness's status tool on page focus and failed turns, displayed as banner + composer lock + badges, with a one-click re-auth terminal session.

**Architecture:** One boolean on `state.Session`, mutated by exactly two backend rules (live turn-error matcher sets; status-tool check sets/clears) that both funnel through `UpdateSessionFunc` → `Save` → `BroadcastSessions`. The chat runtime gains a live-only turn-error callback (never fired during record replay). A new `internal/authcheck` package runs `claude auth status --json` / `codex login status` with a timeout. Two new POST endpoints spawn the login terminal (`reauth`) and trigger the check (`auth-check`). Frontend adds a focus-triggered check, a warning banner, a composer lock, and badges.

**Tech Stack:** Go (chi, exec), React + TypeScript (CSS modules + global tokens), Vitest + React Testing Library, generated types via `cmd/gen-types`.

**Spec:** `docs/specs/chat-signed-out-recovery.md` — the plan argues from the spec; executors read both.

## Global Constraints

Copied verbatim from the spec's Design constraints and Project conventions; every task implicitly includes these:

- "No eligibility field persisted at spawn; no predicate on environment contents."
- "No credentials-file inspection."
- "Harness output may set state only through the narrow sign-out matchers above; everything else about harness output is a trigger at most."
- "No interval, no daemon-startup check."
- "The existing nudge/status element in the session UI is untouched."
- "Nothing in this feature reads or writes chat history" — the matcher sees the live turn error only; daemon restart replays no history to derive the flag.
- Scope: "A session is in scope by resolving its persisted target at the moment a rule needs the answer; if the target can't be resolved, the session is in scope (fail toward showing recovery). Remote chat sessions are out."
- "Types are generated (`go run ./cmd/gen-types`), never hand-edited."
- "Dashboard builds only via `go run ./cmd/build-dashboard`; frontend tests only via `./test.sh --quick`; completion requires full `./test.sh`, `./badcode.sh`, `./format.sh`."
- "`docs/api.md` updated for both endpoints; dashboard UI follows `docs/dashboard-style-guide.md`" (no hardcoded palette colors; only tokens from `docs/dashboard-style-guide.md` §tokens).
- Git: the user owns git — commits happen only through the project's `/commit` command, never raw `git commit`. Run everything from the repository root.
- Test commands: `go test ./internal/<pkg>/` during a task; `./test.sh --quick` before any `/commit`; full `./test.sh` only in the final gate task (it needs Docker for E2E).

---

### Task 1: `Session.SignedOut` persisted field

**Files:**

- Modify: `internal/state/state.go:343` (add field next to `Fence`)
- Test: `internal/state/state_chat_signed_out_test.go` (new)

**Interfaces:**

- Consumes: `UpdateSessionFunc(id string, fn func(sess *Session)) bool`, `New(path string, logger *log.Logger) *State`, `Load(path string, logger *log.Logger) (*State, error)`, `Save() error`, `AddSession`, `AddWorkspace` — all existing.
- Produces: `Session.SignedOut bool` with JSON tag `signed_out,omitempty`. Later tasks mutate it only via `UpdateSessionFunc`.

- [ ] **Step 1: Write the failing test**

```go
package state

import (
	"path/filepath"
	"testing"
	"time"
)

// TestSessionSignedOutPersistence: the flag survives a Save/Load round trip,
// and the JSON key is signed_out (acceptance scenario 5).
func TestSessionSignedOutPersistence(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	s := New(statePath, nil)
	if err := s.AddWorkspace(Workspace{ID: "ws-1", Repo: "git@github.com:u/r.git", Branch: "main", Path: t.TempDir()}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	if err := s.AddSession(Session{ID: "chat-1", WorkspaceID: "ws-1", Target: "claude", Kind: SessionKindChat, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	updated := s.UpdateSessionFunc("chat-1", func(sess *Session) { sess.SignedOut = true })
	if !updated {
		t.Fatal("UpdateSessionFunc missed the session")
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(statePath, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := reloaded.GetSession("chat-1")
	if !ok {
		t.Fatal("session missing after reload")
	}
	if !got.SignedOut {
		t.Error("SignedOut did not survive Save/Load")
	}
	if got.IsChat() != true {
		t.Error("IsChat() should hold for a chat-kind session")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/state/ -run TestSessionSignedOutPersistence -v`
Expected: FAIL with `sess.SignedOut undefined` (compile error).

- [ ] **Step 3: Add the field**

In `internal/state/state.go`, inside `type Session struct`, directly under the `Fence` field (line ~343):

```go
	// SignedOut is true when the session's harness login is known absent:
	// set by the session's own failed turn (sign-out statement matcher) or
	// the harness status tool, cleared by the status tool. Chat sessions
	// only. See docs/specs/chat-signed-out-recovery.md.
	SignedOut bool `json:"signed_out,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/state/ -v`
Expected: all PASS (existing tests unaffected — field is `omitempty`).

- [ ] **Step 5: Commit**

Run `/commit` with message `feat(state): persisted signed_out flag on chat sessions`.

---

### Task 2: Broadcast the flag through the session summary

**Files:**

- Modify: `internal/api/contracts/sessions.go:44` (add field after `Kind`)
- Modify: `internal/dashboard/handlers_sessions.go:380` (populate in the summary build)
- Modify (generated): `assets/dashboard/src/lib/types.generated.ts` — via generator only

**Interfaces:**

- Consumes: `Session.SignedOut` from Task 1.
- Produces: `SessionResponseItem.SignedOut bool` / TS `signed_out?: boolean` on the session summary — Tasks 11–13 read `sessionData.signed_out`.

- [ ] **Step 1: Add the contract field**

In `internal/api/contracts/sessions.go`, after the `Kind` field's comment block (line ~44):

```go
	// SignedOut is true when the harness login for this chat session is
	// known absent (chat sessions only).
	SignedOut bool `json:"signed_out,omitempty"`
```

- [ ] **Step 2: Populate it in the summary build**

In `internal/dashboard/handlers_sessions.go`, in the `SessionResponseItem{...}` literal (lines 351–381), add after `Kind: sess.Kind,`:

```go
			SignedOut:       sess.SignedOut,
```

- [ ] **Step 3: Regenerate types and build**

Run from repo root:

```bash
go run ./cmd/gen-types
go build ./...
```

Expected: build passes; `types.generated.ts` now contains `signed_out?: boolean`. Never hand-edit the generated file.

- [ ] **Step 4: Run backend tests**

Run: `go test ./internal/dashboard/ ./internal/api/...`
Expected: PASS.

- [ ] **Step 5: Commit**

Run `/commit` with message `feat(dashboard): broadcast signed_out in session summaries`.

---

### Task 3: Sign-out statement matcher (chat package)

**Files:**

- Create: `internal/chat/signout.go`
- Test: `internal/chat/signout_test.go`

**Interfaces:**

- Produces: `func MatchSignOutStatement(protocol, text string) bool` — used by Task 8 (dashboard turn-error sink). Protocol values are `ProtocolClaude` (`"claude-stream-json"`) and `ProtocolCodex` (`"codex-app-server"`).

- [ ] **Step 1: Write the failing test**

```go
package chat

import "testing"

func TestMatchSignOutStatement(t *testing.T) {
	cases := []struct {
		name     string
		protocol string
		text     string
		want     bool
	}{
		{"claude login prompt", ProtocolClaude, "Invalid API key. Please run /login to authenticate", true},
		{"claude not logged in", ProtocolClaude, "You are not logged in. Run /login first", true},
		{"claude case insensitive", ProtocolClaude, "PLEASE RUN /LOGIN", true},
		{"claude usage limit must not match", ProtocolClaude, "You've hit your usage limit. Usage resets at 5pm", false},
		{"claude tool failure must not match", ProtocolClaude, "Bash command failed with exit code 1", false},
		{"codex not logged in", ProtocolCodex, "Codex is not logged in", true},
		{"codex login required", ProtocolCodex, "login required: run codex login", true},
		{"codex usage limit must not match", ProtocolCodex, "You've used all of your available usage", false},
		{"protocol isolation: claude text on codex protocol", ProtocolCodex, "Please run /login", false},
		{"protocol isolation: codex text on claude protocol", ProtocolClaude, "Codex is not logged in", false},
		{"empty text", ProtocolClaude, "", false},
		{"unknown protocol", "opencode", "Please run /login", false},
	}
	for _, tc := range cases {
		if got := MatchSignOutStatement(tc.protocol, tc.text); got != tc.want {
			t.Errorf("%s: MatchSignOutStatement(%q, %q) = %v, want %v", tc.name, tc.protocol, tc.text, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/chat/ -run TestMatchSignOutStatement -v`
Expected: FAIL with `undefined: MatchSignOutStatement`.

- [ ] **Step 3: Implement**

`internal/chat/signout.go`:

```go
package chat

import "strings"

// Sign-out statement lists per protocol. A turn error whose text contains
// one of these statements (case-insensitive) is the session reporting that
// its login is gone. Explicit lists only — never fuzzy classification
// (docs/specs/chat-signed-out-recovery.md, State rules / Set).
//
// Seeds are best-effort: acceptance scenarios 1 and 4 observe the real
// harness phrasings and the lists are tightened there. Usage-limit and
// unrelated error texts must never appear here.
var signOutStatements = map[string][]string{
	ProtocolClaude: {
		"please run /login",
		"you are not logged in",
		"session expired",
	},
	ProtocolCodex: {
		"codex is not logged in",
		"login required",
		"log in to chatgpt",
	},
}

// MatchSignOutStatement reports whether a harness turn-error text is a
// sign-out statement for the protocol. Unknown protocols never match.
func MatchSignOutStatement(protocol, text string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, stmt := range signOutStatements[protocol] {
		if strings.Contains(lower, stmt) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/chat/ -run TestMatchSignOutStatement -v`
Expected: PASS.

- [ ] **Step 5: Commit**

Run `/commit` with message `feat(chat): explicit sign-out statement matchers`.

---

### Task 4: Live turn-error event from the chat runtime

**Files:**

- Modify: `internal/chat/nudge.go` — tracker fields + three fire sites + replay gate
- Modify: `internal/chat/runtime.go` — `TurnErrorEvent`, `SetTurnErrorCallback`, replay gating in `replayNudgeLocked`
- Test: `internal/chat/signout_runtime_test.go` (new)

**Interfaces:**

- Consumes: existing `NudgeTracker`, `Runtime`.
- Produces:
  - `type TurnErrorEvent struct { Protocol, Text string }`
  - `type TurnErrorCallback func(TurnErrorEvent)`
  - `func (r *Runtime) SetTurnErrorCallback(cb TurnErrorCallback)` — fires on live turn errors only, never during replay.

Firing sites (the only places a turn ends in error today):

- claude: `observeClaude` `"result"` case (nudge.go ~210–224) — inside `if v.IsError || strings.HasPrefix(v.Subtype, "error")` after `t.errorMsg = claudeErrorMessage(line)`.
- codex: `observeCodex` `"turn/completed"` `Status == "failed"` branch (~424–430) after `t.fail(msg)`.
- codex: `observeCodex` `"error"` `!p.WillRetry` branch (~431–438) after `t.fail(msg)`.

- [ ] **Step 1: Write the failing test**

```go
package chat

import "testing"

// The tracker fires the turn-error hook with the extracted error text for
// both protocols' turn-failure shapes.
func TestNudgeTrackerTurnErrorFires(t *testing.T) {
	cases := []struct {
		name     string
		proto    string
		lines    []string
		wantText string
	}{
		{
			name:  "claude error result",
			proto: ProtocolClaude,
			lines: []string{
				`{"type":"user","message":{"role":"user","content":"hi"}}`,
				`{"type":"assistant","message":{"role":"assistant"}}`,
				`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Invalid API key. Please run /login"}`,
			},
			wantText: "Invalid API key. Please run /login",
		},
		{
			name:  "codex failed turn",
			proto: ProtocolCodex,
			lines: []string{
				`{"method":"turn/started","params":{"threadId":"t1","turn":{"id":"turn1"}}}`,
				`{"method":"turn/completed","params":{"threadId":"t1","turnId":"turn1","turn":{"id":"turn1","status":"failed","error":{"message":"Codex is not logged in"}}}}`,
			},
			wantText: "Codex is not logged in",
		},
		{
			name:  "codex error event no retry",
			proto: ProtocolCodex,
			lines: []string{
				`{"method":"turn/started","params":{"threadId":"t1","turn":{"id":"turn1"}}}`,
				`{"method":"error","params":{"threadId":"t1","turnId":"turn1","error":{"message":"login required"},"willRetry":false}}`,
			},
			wantText: "login required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := NewNudgeTracker(tc.proto)
			var got string
			var fired bool
			tr.onTurnError = func(text string) { got, fired = text, true }
			for _, line := range tc.lines {
				tr.Rec(NewHarness([]byte(line)))
			}
			if !fired {
				t.Fatal("turn-error hook did not fire")
			}
			if got != tc.wantText {
				t.Errorf("hook text = %q, want %q", got, tc.wantText)
			}
		})
	}
}

// Successful results and replayed history never fire the hook.
func TestNudgeTrackerTurnErrorSilent(t *testing.T) {
	t.Run("claude success result does not fire", func(t *testing.T) {
		tr := NewNudgeTracker(ProtocolClaude)
		fired := false
		tr.onTurnError = func(string) { fired = true }
		tr.Rec(NewHarness([]byte(`{"type":"result","subtype":"success","is_error":false,"result":"done"}`)))
		if fired {
			t.Error("hook fired on a successful result")
		}
	})
	t.Run("replaying records do not fire", func(t *testing.T) {
		tr := NewNudgeTracker(ProtocolClaude)
		fired := false
		tr.onTurnError = func(string) { fired = true }
		tr.replaying = true
		tr.Rec(NewHarness([]byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Please run /login"}`)))
		tr.replaying = false
		if fired {
			t.Error("hook fired during replay")
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/chat/ -run 'TestNudgeTrackerTurnError' -v`
Expected: compile FAIL (`tr.onTurnError undefined`, `tr.replaying undefined`).

- [ ] **Step 3: Implement the tracker hook**

In `internal/chat/nudge.go`:

Add to `NudgeTracker` struct (after `activeTurnID string`):

```go
	// onTurnError, when set, receives the extracted text of every live
	// turn-ending error. replaying suppresses it: daemon restart must not
	// re-derive state from history (spec: nothing is rebuilt on load).
	onTurnError func(text string)
	replaying   bool
```

Claude site — in `observeClaude`'s `"result"` case, the existing block is:

```go
			if !t.interrupted {
				if v.IsError || strings.HasPrefix(v.Subtype, "error") {
					t.errorMsg = claudeErrorMessage(line)
				} else if v.Subtype == "success" {
					t.completed = true
				}
			}
```

Change the error branch to fire:

```go
			if !t.interrupted {
				if v.IsError || strings.HasPrefix(v.Subtype, "error") {
					t.errorMsg = claudeErrorMessage(line)
					if !t.replaying && t.onTurnError != nil {
						t.onTurnError(t.errorMsg)
					}
				} else if v.Subtype == "success" {
					t.completed = true
				}
			}
```

Codex sites — in `observeCodex`'s `"turn/completed"` case:

```go
			if p.Turn.Status == "failed" {
				msg := "codex turn failed"
				if p.Turn.Error != nil && p.Turn.Error.Message != "" {
					msg = p.Turn.Error.Message
				}
				t.fail(msg)
				if !t.replaying && t.onTurnError != nil {
					t.onTurnError(msg)
				}
			}
```

and the `"error"` case:

```go
		case "error":
			if !p.WillRetry {
				msg := p.Error.Message
				if msg == "" {
					msg = "codex error"
				}
				t.fail(msg)
				if !t.replaying && t.onTurnError != nil {
					t.onTurnError(msg)
				}
			}
```

- [ ] **Step 4: Implement the runtime callback and replay gate**

In `internal/chat/runtime.go`, next to `NudgeCallback` (line ~37):

```go
// TurnErrorEvent is one live chat turn ending in error. Protocol is the
// chat protocol name; Text is the harness's error text as extracted by the
// nudge tracker. It never fires for replayed history.
type TurnErrorEvent struct {
	Protocol string
	Text     string
}

// TurnErrorCallback receives live turn errors. Like NudgeCallback, it must
// not re-enter the runtime.
type TurnErrorCallback func(TurnErrorEvent)
```

Add a `turnErrorCallback TurnErrorCallback` field to `Runtime`, and after `SetNudgeCallback` (line ~121):

```go
// SetTurnErrorCallback registers the sink for live turn-ending errors.
// Register before Start. Replayed history never fires it.
func (r *Runtime) SetTurnErrorCallback(cb TurnErrorCallback) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turnErrorCallback = cb
	protoName := r.proto.Name()
	r.nudgeTracker.onTurnError = func(text string) {
		if cb != nil {
			cb(TurnErrorEvent{Protocol: protoName, Text: text})
		}
	}
}
```

Gate replay in `replayNudgeLocked` — set the flag around the record loop:

```go
func (r *Runtime) replayNudgeLocked(recs []Record) {
	start := lastSessionEndedIndex(recs) + 1
	current := recs[start:]
	written, err := controlsInInput(r.paths.Input, current)
	if err != nil {
		r.warn("failed to reconcile chat controls", err)
	}
	r.nudgeTracker.replaying = true
	defer func() { r.nudgeTracker.replaying = false }()
	// ... existing loop unchanged ...
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/chat/ -v`
Expected: all PASS including the new tests and every existing nudge/runtime test.

- [ ] **Step 6: Commit**

Run `/commit` with message `feat(chat): live turn-error callback, silent during replay`.

---

### Task 5: Wire the turn-error path through manager and daemon

**Files:**

- Modify: `internal/session/manager.go` — field + setter + re-wire loop + forwarder + `ensureChatRuntime` hookup
- Modify: `internal/daemon/daemon.go:998` — register the sink

**Interfaces:**

- Consumes: `chat.TurnErrorEvent`, `chat.TurnErrorCallback` (Task 4).
- Produces: `func (m *Manager) SetChatTurnErrorCallback(cb func(sessionID string, ev chat.TurnErrorEvent))` — mirrors `SetChatNudgeCallback` (manager.go:233). Consumed by `internal/daemon`.

No unit test of its own: the wiring is three lines of passthrough already covered end-to-end by Task 8's dashboard tests plus the existing manager test suite compiling.

- [ ] **Step 1: Add the field and setter**

In `internal/session/manager.go`, next to `chatNudgeCallback` (line ~65):

```go
	chatTurnErrorCallback func(sessionID string, ev chat.TurnErrorEvent) // live chat turn errors; nil disables the path
```

Next to `SetChatNudgeCallback` (line ~233), mirroring it exactly (`chatRuntimes` is `map[string]*chat.Runtime` keyed by session ID, manager.go:59):

```go
// SetChatTurnErrorCallback registers the sink that receives live chat
// turn-ending errors (nil disables). Wiring happens here and in
// ensureChatRuntime so restored runtimes are covered too.
func (m *Manager) SetChatTurnErrorCallback(cb func(sessionID string, ev chat.TurnErrorEvent)) {
	m.chatTurnErrorCallback = cb
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, rt := range m.chatRuntimes {
		rt.SetTurnErrorCallback(makeChatTurnErrorForwarder(id, cb))
	}
}

// makeChatTurnErrorForwarder closes over the session id and the manager's
// stored callback, so the runtime can invoke it through a
// chat.TurnErrorCallback.
func makeChatTurnErrorForwarder(sessionID string, cb func(sessionID string, ev chat.TurnErrorEvent)) chat.TurnErrorCallback {
	if cb == nil {
		return nil
	}
	return func(ev chat.TurnErrorEvent) {
		cb(sessionID, ev)
	}
}
```

And in `ensureChatRuntime` (line ~2403, where `existing := m.chatRuntimes[sessionID]` guards re-creation), right after the existing `rt.SetNudgeCallback(makeChatNudgeForwarder(sess.ID, ...))` wiring:

```go
		rt.SetTurnErrorCallback(makeChatTurnErrorForwarder(sess.ID, m.chatTurnErrorCallback))
```

- [ ] **Step 2: Register the daemon sink**

In `internal/daemon/daemon.go`, directly after `sm.SetChatNudgeCallback(...)` (line ~997):

```go
	// Live chat turn errors: the matcher may set signed_out (scope-checked
	// in the server), and every turn error triggers the protocol auth check.
	sm.SetChatTurnErrorCallback(server.HandleChatTurnError)
```

- [ ] **Step 3: Build and run the affected tests**

Run: `go build ./... && go test ./internal/session/ ./internal/daemon/`
Expected: build passes; tests PASS (`HandleChatTurnError` lands in Task 8 — until then this line will not compile, so implement Tasks 5 and 8 in the same working session, or add the daemon line as the first step of Task 8. Order chosen: do Task 5's manager changes now, daemon line with Task 8.)

- [ ] **Step 4: Commit (manager wiring only)**

Run `/commit` with message `feat(session): chat turn-error callback passthrough`.

---

### Task 6: Endpoint-routing predicate on the models manager

**Files:**

- Modify: `internal/models/manager.go` — `ResolvedModel.Endpoint` field (~445), set in `ResolveModel` (~481), new `RoutesToEndpoint`
- Test: `internal/models/manager_test.go` (add)

**Interfaces:**

- Consumes: existing `ResolveModel(modelID string) (*ResolvedModel, error)`, `FindModel`, `ResolveToolForModel`, `model.RunnerFor(toolName)` whose `RunnerSpec` has `Endpoint string`.
- Produces: `func (m *Manager) RoutesToEndpoint(target string) bool` — true when the target resolves to a model whose selected runner routes through a non-first-party endpoint (`spec.Endpoint != ""`). Bare tool names and unknown targets return false (unknown = in scope per spec).

- [ ] **Step 1: Write the failing test**

In `internal/models/manager_test.go` (follow the file's existing helper for building a Manager with registry models — reuse whatever constructor the neighboring tests use):

```go
func TestRoutesToEndpoint(t *testing.T) {
	// Build a manager whose registry has one endpoint-routed model and one
	// first-party model, plus a detected bare tool, mirroring the file's
	// existing registry-setup helper.
	m := newTestManagerWithRegistry(t) // existing helper; if none, construct models.New(cfg, tools, "", logger) + SetRegistryModels as other tests do

	if !m.RoutesToEndpoint("routed-model") {
		t.Error("endpoint-routed model should route")
	}
	if m.RoutesToEndpoint("firstparty-model") {
		t.Error("first-party model should not route")
	}
	if m.RoutesToEndpoint("claude") {
		t.Error("bare tool target never routes")
	}
	if m.RoutesToEndpoint("no-such-target") {
		t.Error("unknown target never routes (in scope per spec)")
	}
}
```

Adapt the setup to the file's real helpers: the models must be registered with `detect.Model{ID: ..., Runners: map[...]RunnerSpec{...}}` where the routed model's preferred runner spec has `Endpoint: "https://gateway.example"` and is detected/secret-satisfied, the first-party model's does not. If the existing tests already build such a manager, reuse that code verbatim in this test.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/models/ -run TestRoutesToEndpoint -v`
Expected: FAIL with `m.RoutesToEndpoint undefined`.

- [ ] **Step 3: Implement**

In `internal/models/manager.go`:

```go
// ResolvedModel holds everything needed to spawn a session or run a oneshot
// with a specific model. Returned by ResolveModel.
type ResolvedModel struct {
	Model    detect.Model
	ToolName string
	Command  string
	Env      map[string]string
	// Endpoint is the runner spec's non-first-party endpoint ("" when the
	// model runs against the harness's own login).
	Endpoint string
}
```

In `ResolveModel`, after `spec, _ := model.RunnerFor(toolName)`:

```go
	// Surface the endpoint so scope rules can see routing (signed_out).
	endpoint := spec.Endpoint
```

and include `Endpoint: endpoint,` in the returned `&ResolvedModel{...}` literal.

New method (next to `ResolveTargetToTool`, line ~432):

```go
// RoutesToEndpoint reports whether a target resolves to a model whose
// selected runner routes the harness to a non-first-party endpoint (the
// runner_env.when_endpoint case). Bare tool targets run first-party;
// unknown targets return false so callers treat the session as in scope
// (fail toward showing recovery).
func (m *Manager) RoutesToEndpoint(targetName string) bool {
	model, ok := m.FindModel(targetName)
	if !ok {
		return false
	}
	toolName := m.ResolveToolForModel(model)
	if toolName == "" {
		return false
	}
	spec, _ := model.RunnerFor(toolName)
	return spec.Endpoint != ""
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/models/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

Run `/commit` with message `feat(models): expose endpoint routing per target`.

---

### Task 7: `internal/authcheck` — status tool runner

**Files:**

- Create: `internal/authcheck/authcheck.go`
- Test: `internal/authcheck/authcheck_test.go`

**Interfaces:**

- Produces:
  - `type Result int` with `LoggedIn`, `LoggedOut`, `NoAnswer`
  - `const Timeout = 10 * time.Second` (used by the dashboard caller)
  - `func Run(ctx context.Context, protocol string) (Result, string)` — second return is raw output for logging. Protocol values: `chat.ProtocolClaude`, `chat.ProtocolCodex` (import `internal/chat` for the constants only).

- [ ] **Step 1: Write the failing test** (PATH-stub pattern from `internal/detect/vscode_test.go:163`)

```go
package authcheck

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
)

// stubBin writes an executable script with the given body under dir with
// the given name and prepends dir to PATH.
func stubBin(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write stub %s: %v", name, err)
	}
}

func withStubbedPATH(t *testing.T, dir string) {
	t.Helper()
	orig := os.Getenv("PATH")
	os.Setenv("PATH", dir+string(os.PathListSeparator)+orig)
	t.Cleanup(func() { os.Setenv("PATH", orig) })
}

func TestRunClaude(t *testing.T) {
	dir := t.TempDir()
	withStubbedPATH(t, dir)

	stubBin(t, dir, "claude", `echo '{"loggedIn": true, "authMethod": "oauth_token"}'`)
	if res, _ := Run(context.Background(), chat.ProtocolClaude); res != LoggedIn {
		t.Error("loggedIn true should be LoggedIn")
	}

	stubBin(t, dir, "claude", `echo '{"loggedIn": false}'`)
	if res, _ := Run(context.Background(), chat.ProtocolClaude); res != LoggedOut {
		t.Error("loggedIn false should be LoggedOut")
	}

	stubBin(t, dir, "claude", `echo 'not json at all'`)
	if res, _ := Run(context.Background(), chat.ProtocolClaude); res != NoAnswer {
		t.Error("unparseable output should be NoAnswer")
	}

	stubBin(t, dir, "claude", `sleep 5`)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if res, _ := Run(ctx, chat.ProtocolClaude); res != NoAnswer {
		t.Error("timeout should be NoAnswer")
	}
}

func TestRunCodex(t *testing.T) {
	dir := t.TempDir()
	withStubbedPATH(t, dir)

	stubBin(t, dir, "codex", `echo 'Logged in using ChatGPT'`)
	if res, _ := Run(context.Background(), chat.ProtocolCodex); res != LoggedIn {
		t.Error("ChatGPT marker should be LoggedIn")
	}

	// Logged-out shape is unobserved in the wild: any non-empty completed
	// output without the marker reads as LoggedOut (the command's whole
	// job is to answer); empty output is NoAnswer — never guess.
	stubBin(t, dir, "codex", `echo 'Not logged in'`)
	if res, _ := Run(context.Background(), chat.ProtocolCodex); res != LoggedOut {
		t.Error("answered-without-marker should be LoggedOut")
	}

	stubBin(t, dir, "codex", `exit 1`)
	if res, _ := Run(context.Background(), chat.ProtocolCodex); res != NoAnswer {
		t.Error("no output should be NoAnswer")
	}
}

func TestRunUnknownProtocol(t *testing.T) {
	if res, _ := Run(context.Background(), "opencode"); res != NoAnswer {
		t.Error("unknown protocol should be NoAnswer")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/authcheck/ -v`
Expected: FAIL (`no Go files` / undefined package).

- [ ] **Step 3: Implement**

```go
// Package authcheck runs the harness's own login-status tools and parses
// their answer. It never inspects credential files and never guesses: an
// answer it cannot parse is NoAnswer (docs/specs/chat-signed-out-recovery.md,
// State rules / Correct and clear).
package authcheck

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
)

// Result is the parsed answer of one status-tool run.
type Result int

const (
	NoAnswer Result = iota // timeout, missing binary, unparseable output
	LoggedIn
	LoggedOut
)

// Timeout bounds one status-tool run. The dashboard caller derives its
// context from it.
const Timeout = 10 * time.Second

// Run executes the protocol's status tool and parses the answer. The
// second return is the raw combined output for logging on NoAnswer.
func Run(ctx context.Context, protocol string) (Result, string) {
	var cmd *exec.Cmd
	switch protocol {
	case chat.ProtocolClaude:
		cmd = exec.CommandContext(ctx, "claude", "auth", "status", "--json")
	case chat.ProtocolCodex:
		cmd = exec.CommandContext(ctx, "codex", "login", "status")
	default:
		return NoAnswer, ""
	}
	out, _ := cmd.CombinedOutput()
	raw := string(out)
	switch protocol {
	case chat.ProtocolClaude:
		var v struct {
			LoggedIn *bool `json:"loggedIn"`
		}
		if err := json.Unmarshal(out, &v); err != nil || v.LoggedIn == nil {
			return NoAnswer, raw
		}
		if *v.LoggedIn {
			return LoggedIn, raw
		}
		return LoggedOut, raw
	default:
		if strings.Contains(raw, "Logged in using ChatGPT") {
			return LoggedIn, raw
		}
		if strings.TrimSpace(raw) != "" {
			return LoggedOut, raw
		}
		return NoAnswer, raw
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/authcheck/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

Run `/commit` with message `feat(authcheck): harness login status tool runner`.

---

### Task 8: Dashboard orchestration — apply tool answers, set from turn errors

**Files:**

- Create: `internal/dashboard/authcheck.go`
- Modify: `internal/dashboard/server.go` — `authMu`/`authInFlight` fields on `Server` (near `models` at line ~191)
- Modify: `internal/daemon/daemon.go` — register `sm.SetChatTurnErrorCallback(server.HandleChatTurnError)` (the line prepared in Task 5)
- Test: `internal/dashboard/authcheck_test.go` (new)

**Interfaces:**

- Consumes: `authcheck.Run/Result/Timeout` (Task 7), `chat.MatchSignOutStatement` (Task 3), `chat.TurnErrorEvent` (Task 4), `models.Manager.RoutesToEndpoint` (Task 6), `state.UpdateSessionFunc/GetSessions/GetSession/Save` (existing), `Server.BroadcastSessions`, `Server.models`, `Server.state`.
- Produces (both trusted in-process entry points, mirroring `UpdateChatNudge` in websocket.go:1124):
  - `func (s *Server) RunAuthCheck(protocol string)`
  - `func (s *Server) HandleChatTurnError(sessionID string, ev chat.TurnErrorEvent)`
  - Internal: `func (s *Server) chatSessionInScope(sess state.Session) bool`

- [ ] **Step 1: Write the failing tests**

```go
package dashboard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/state"
)

// newAuthCheckServer builds a Server over a fresh state seeded with the
// scope matrix: local first-party chat (in scope), remote chat (out),
// terminal (out). models resolves no endpoint routing for target "claude".
func newAuthCheckServer(t *testing.T) *Server {
	t.Helper()
	st := state.New(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "git@github.com:u/r.git", Branch: "main", Path: t.TempDir()}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	now := time.Now()
	sessions := []state.Session{
		{ID: "chat-1", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, CreatedAt: now},
		{ID: "chat-remote", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, RemoteHostID: "host-1", CreatedAt: now},
		{ID: "term-1", WorkspaceID: "ws-1", Target: "claude", CreatedAt: now},
	}
	for _, sess := range sessions {
		if err := st.AddSession(sess); err != nil {
			t.Fatalf("AddSession %s: %v", sess.ID, err)
		}
	}
	cfg := &config.Config{}
	return newTestServer(t, st, cfg) // if no such helper exists, construct Server the way neighboring dashboard tests do (fields: state, models, logger)
}

func TestHandleChatTurnError_SetsOnMatchInScopeOnly(t *testing.T) {
	s := newAuthCheckServer(t)

	s.HandleChatTurnError("chat-1", chat.TurnErrorEvent{Protocol: chat.ProtocolClaude, Text: "Invalid API key. Please run /login"})
	sess, _ := s.state.GetSession("chat-1")
	if !sess.SignedOut {
		t.Error("in-scope chat session should be signed out after matching turn error")
	}

	s.HandleChatTurnError("chat-remote", chat.TurnErrorEvent{Protocol: chat.ProtocolClaude, Text: "Please run /login"})
	sess, _ = s.state.GetSession("chat-remote")
	if sess.SignedOut {
		t.Error("remote chat session must never be set")
	}

	s.HandleChatTurnError("chat-1", chat.TurnErrorEvent{Protocol: chat.ProtocolClaude, Text: "You've hit your usage limit"})
	sess, _ = s.state.GetSession("chat-1")
	// The matcher did not set it again, and the async tool check is
	// stubbed away by an empty PATH below; assert the matcher alone.
	_ = sess
}

func TestRunAuthCheck_AppliesToInScopeSessionsOfProtocol(t *testing.T) {
	s := newAuthCheckServer(t)
	// Seed one flag to verify clearing.
	s.state.UpdateSessionFunc("chat-1", func(sess *state.Session) { sess.SignedOut = true })

	dir := t.TempDir()
	orig := os.Getenv("PATH")
	os.Setenv("PATH", dir)
	t.Cleanup(func() { os.Setenv("PATH", orig) })
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\necho '{\"loggedIn\": true}'"), 0o755); err != nil {
		t.Fatalf("stub claude: %v", err)
	}

	s.RunAuthCheck(chat.ProtocolClaude)

	deadline := time.Now().Add(2 * time.Second)
	for {
		sess, _ := s.state.GetSession("chat-1")
		if !sess.SignedOut {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("logged-in answer did not clear the flag")
		}
		time.Sleep(10 * time.Millisecond)
	}
	sess, _ := s.state.GetSession("term-1")
	_ = sess // terminal session untouched by construction (never in scope)
}

func TestRunAuthCheck_NoAnswerChangesNothing(t *testing.T) {
	s := newAuthCheckServer(t)
	s.state.UpdateSessionFunc("chat-1", func(sess *state.Session) { sess.SignedOut = true })

	orig := os.Getenv("PATH")
	os.Setenv("PATH", t.TempDir()) // no binaries: exec fails -> NoAnswer
	t.Cleanup(func() { os.Setenv("PATH", orig) })

	s.RunAuthCheck(chat.ProtocolClaude)

	time.Sleep(100 * time.Millisecond) // NoAnswer returns without goroutine racing long
	sess, _ := s.state.GetSession("chat-1")
	if !sess.SignedOut {
		t.Error("NoAnswer must change nothing")
	}
}
```

Note on `newTestServer`: mirror how existing dashboard tests that need a `*Server` construct it (search `&Server{` in `internal/dashboard/*_test.go`). If they build the struct literal directly with `state`, `models`, `logger` fields, do the same; `models: models.New(cfg, []detect.Tool{{Name: "claude"}}, "", discardLogger())`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dashboard/ -run 'TestHandleChatTurnError|TestRunAuthCheck' -v`
Expected: FAIL with `s.HandleChatTurnError undefined`.

- [ ] **Step 3: Implement**

`internal/dashboard/authcheck.go`:

```go
package dashboard

import (
	"context"
	"sync"

	"github.com/sergeknystautas/schmux/internal/authcheck"
	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/state"
)

// chatSessionInScope reports whether a session participates in signed-out
// recovery: local chat session whose target does not route the harness to
// a non-first-party endpoint. Unresolvable targets are in scope (fail
// toward showing recovery).
func (s *Server) chatSessionInScope(sess state.Session) bool {
	return sess.IsChat() &&
		sess.RemoteHostID == "" &&
		!s.models.RoutesToEndpoint(sess.Target)
}

// applyAuthAnswer sets or clears signed_out on every in-scope chat session
// of the protocol, saves once, and broadcasts once.
func (s *Server) applyAuthAnswer(protocol string, res authcheck.Result) {
	want := res == authcheck.LoggedOut
	changedAny := false
	for _, sess := range s.state.GetSessions() {
		if sess.EffectiveChatProtocol() != protocol || !s.chatSessionInScope(sess) {
			continue
		}
		if sess.SignedOut == want {
			continue
		}
		updated := s.state.UpdateSessionFunc(sess.ID, func(p *state.Session) {
			p.SignedOut = want
		})
		if updated {
			changedAny = true
		}
	}
	if !changedAny {
		return
	}
	if err := s.state.Save(); err != nil {
		logging.Sub(s.logger, "authcheck").Error("failed to save state", "err", err)
		return
	}
	go s.BroadcastSessions()
}

// RunAuthCheck runs the protocol's status tool once and applies the answer
// to every in-scope chat session of that protocol. At most one check per
// protocol is in flight; a trigger arriving during a run is covered by it.
func (s *Server) RunAuthCheck(protocol string) {
	s.authMu.Lock()
	if s.authInFlight == nil {
		s.authInFlight = map[string]bool{}
	}
	if s.authInFlight[protocol] {
		s.authMu.Unlock()
		return
	}
	s.authInFlight[protocol] = true
	s.authMu.Unlock()
	defer func() {
		s.authMu.Lock()
		delete(s.authInFlight, protocol)
		s.authMu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), authcheck.Timeout)
	defer cancel()
	res, raw := authcheck.Run(ctx, protocol)
	if res == authcheck.NoAnswer {
		logging.Sub(s.logger, "authcheck").Warn("status tool gave no answer", "protocol", protocol, "output", raw)
		return
	}
	s.applyAuthAnswer(protocol, res)
}

// HandleChatTurnError is the in-process sink for live chat turn errors.
// A matching sign-out statement sets the flag on the failing session when
// it is in scope; every turn error also triggers the protocol check (the
// error is a trigger, never a decider — this is the retraction path).
func (s *Server) HandleChatTurnError(sessionID string, ev chat.TurnErrorEvent) {
	if chat.MatchSignOutStatement(ev.Protocol, ev.Text) {
		if sess, ok := s.state.GetSession(sessionID); ok && s.chatSessionInScope(sess) && !sess.SignedOut {
			if s.state.UpdateSessionFunc(sessionID, func(p *state.Session) { p.SignedOut = true }) {
				if err := s.state.Save(); err != nil {
					logging.Sub(s.logger, "authcheck").Error("failed to save state", "session", sessionID, "err", err)
				} else {
					go s.BroadcastSessions()
				}
			}
		}
	}
	go s.RunAuthCheck(ev.Protocol)
}
```

Add fields to `Server` in `internal/dashboard/server.go` (near `models`, line ~191):

```go
	authMu       sync.Mutex
	authInFlight map[string]bool // protocol -> check running
```

Add the daemon registration from Task 5 in `internal/daemon/daemon.go` after `sm.SetChatNudgeCallback(...)`:

```go
	sm.SetChatTurnErrorCallback(server.HandleChatTurnError)
```

- [ ] **Step 4: Run tests**

Run: `go build ./... && go test ./internal/dashboard/ ./internal/daemon/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

Run `/commit` with message `feat(dashboard): signed_out set/clear orchestration and turn-error sink`.

---

### Task 9: HTTP endpoints `reauth` and `auth-check` + `docs/api.md`

**Files:**

- Create: `internal/dashboard/handlers_auth.go`
- Modify: `internal/dashboard/server.go:928` — register both routes; wire the `runAuthCheck` callback into `SpawnHandlers`
- Modify: `internal/dashboard/handlers_spawn.go` — `runAuthCheck func(protocol string)` field on `SpawnHandlers`
- Modify: `docs/api.md` — two endpoint sections
- Test: `internal/dashboard/handlers_auth_test.go` (new)

**Interfaces:**

- Consumes: `Server.RunAuthCheck` (Task 8) via a `runAuthCheck` callback field (same pattern as `broadcastSessions`); `session.Manager.Spawn` with `SpawnOptions{WorkspaceID, Command, Nickname}` (existing — `Command` runs as a shell command, manager.go:1287); `SessionResult` (handlers_spawn.go:64); `state.Session.EffectiveChatProtocol()`.
- Produces:
  - `POST /api/sessions/{sessionID}/reauth` → `SessionResult` JSON; 404 unknown, 400 non-chat, 409 remote.
  - `POST /api/sessions/{sessionID}/auth-check` → 204 No Content (response body unused; state changes arrive via the session broadcast); same guards.

- [ ] **Step 1: Write the failing guard tests**

```go
package dashboard

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/state"
)

// newAuthHandlers mirrors newRestartHandler (handlers_restart_test.go:24).
func newAuthHandlers(t *testing.T) *SpawnHandlers {
	t.Helper()
	st := state.New(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "git@github.com:u/r.git", Branch: "main", Path: t.TempDir()}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	now := time.Now()
	sessions := []state.Session{
		{ID: "chat-1", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, CreatedAt: now},
		{ID: "chat-codex", WorkspaceID: "ws-1", Target: "codex", Kind: state.SessionKindChat, ChatProtocol: "codex-app-server", CreatedAt: now},
		{ID: "chat-remote", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, RemoteHostID: "host-1", CreatedAt: now},
		{ID: "term-1", WorkspaceID: "ws-1", Target: "claude", CreatedAt: now},
	}
	for _, sess := range sessions {
		if err := st.AddSession(sess); err != nil {
			t.Fatalf("AddSession %s: %v", sess.ID, err)
		}
	}
	cfg := &config.Config{}
	return &SpawnHandlers{
		logger: discardLogger(),
		state:  st,
		config: cfg,
		models: models.New(cfg, []detect.Tool{{Name: "claude"}, {Name: "codex"}}, "", discardLogger()),
	}
}

func postAuth(t *testing.T, h *SpawnHandlers, suffix string, sessionID string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/api/sessions/{sessionID}/reauth", h.handleReauth)
	r.Post("/api/sessions/{sessionID}/auth-check", h.handleAuthCheck)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/"+suffix, strings.NewReader(""))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestAuthEndpointsGuards(t *testing.T) {
	h := newAuthHandlers(t)

	cases := []struct {
		suffix    string
		sessionID string
		want      int
	}{
		{"reauth", "no-such-session", http.StatusNotFound},
		{"auth-check", "no-such-session", http.StatusNotFound},
		{"reauth", "term-1", http.StatusBadRequest},
		{"auth-check", "term-1", http.StatusBadRequest},
		{"reauth", "chat-remote", http.StatusConflict},
		{"auth-check", "chat-remote", http.StatusConflict},
	}
	for _, tc := range cases {
		if got := postAuth(t, h, tc.suffix, tc.sessionID).Code; got != tc.want {
			t.Errorf("%s %s: status = %d, want %d", tc.suffix, tc.sessionID, got, tc.want)
		}
	}
}

func TestAuthCheckRunsChecker(t *testing.T) {
	h := newAuthHandlers(t)
	protocols := []string{}
	h.runAuthCheck = func(protocol string) { protocols = append(protocols, protocol) }
	postAuth(t, h, "auth-check", "chat-codex")
	if len(protocols) != 1 || protocols[0] != "codex-app-server" {
		t.Errorf("auth-check should run the session's protocol check, got %v", protocols)
	}
}
```

(`newAuthHandlers` mirrors `newRestartHandler` in handlers_restart_test.go — match its `SpawnHandlers` literal fields exactly; if the existing helper sets more fields, keep them.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dashboard/ -run 'TestAuth' -v`
Expected: FAIL (`h.handleReauth undefined` / routes missing).

- [ ] **Step 3: Implement the handlers**

`internal/dashboard/handlers_auth.go`:

```go
package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
)

// reauthCommands maps chat protocol to the login-flow command the reauth
// terminal runs in the chat session's workspace (spec, Recovery UX).
var reauthCommands = map[string]string{
	chat.ProtocolClaude: "claude auth logout || true; claude /login",
	chat.ProtocolCodex:  "codex login",
}

// authGuard applies the shared guards: 404 unknown, 400 non-chat,
// 409 remote. Returns the session when eligible.
func (h *SpawnHandlers) authGuard(w http.ResponseWriter, r *http.Request) (state.Session, bool) {
	sessionID := chi.URLParam(r, "sessionID")
	sess, ok := h.state.GetSession(sessionID)
	if !ok {
		writeJSONError(w, "unknown session", http.StatusNotFound)
		return state.Session{}, false
	}
	if !sess.IsChat() {
		writeJSONError(w, "not a chat session", http.StatusBadRequest)
		return state.Session{}, false
	}
	if sess.RemoteHostID != "" {
		writeJSONError(w, "remote chat sessions authenticate on their host", http.StatusConflict)
		return state.Session{}, false
	}
	return sess, true
}

// handleReauth spawns a terminal session in the chat session's workspace
// running the harness's real login flow, and returns it — the exact
// response shape and encode pattern of the restart handler
// (handlers_restart.go:136).
func (h *SpawnHandlers) handleReauth(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.authGuard(w, r)
	if !ok {
		return
	}
	protocol := sess.EffectiveChatProtocol()
	cmd, found := reauthCommands[protocol]
	if !found {
		writeJSONError(w, "no login flow for chat protocol "+protocol, http.StatusBadRequest)
		return
	}
	newSess, err := h.session.Spawn(r.Context(), session.SpawnOptions{
		WorkspaceID: sess.WorkspaceID,
		Command:     cmd,
		Nickname:    "sign-in",
	})
	if err != nil {
		writeJSONError(w, "failed to spawn login session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := json.NewEncoder(w).Encode(SessionResult{
		SessionID:   newSess.ID,
		WorkspaceID: newSess.WorkspaceID,
		Nickname:    newSess.Nickname,
	}); err != nil {
		h.logger.Error("failed to encode reauth response", "err", err)
	}
}

// handleAuthCheck runs the login-status check for the session's protocol.
// The response body is unused; state changes arrive via the session
// broadcast.
func (h *SpawnHandlers) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.authGuard(w, r)
	if !ok {
		return
	}
	protocol := sess.EffectiveChatProtocol()
	go func() {
		if h.runAuthCheck != nil {
			h.runAuthCheck(protocol)
		}
	}()
	w.WriteHeader(http.StatusNoContent)
}
```

Add the callback field to `SpawnHandlers` (handlers_spawn.go, next to `broadcastSessions`):

```go
	runAuthCheck func(protocol string)
```

Register routes in `internal/dashboard/server.go` next to the restart route (line ~927):

```go
			r.Post("/sessions/{sessionID}/reauth", spawnH.handleReauth)
			r.Post("/sessions/{sessionID}/auth-check", spawnH.handleAuthCheck)
```

Wire the callback where `SpawnHandlers` is constructed (server.go ~440, next to `broadcastSessions: s.BroadcastSessions`):

```go
			runAuthCheck: s.RunAuthCheck,
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS.

- [ ] **Step 5: Document both endpoints in `docs/api.md`**

Add after the restart section (docs/api.md ~835–880), matching its format:

```markdown
### POST /api/sessions/{sessionId}/reauth

Spawns a terminal session in the chat session's workspace running the harness's real login flow — `claude auth logout || true; claude /login` for claude-protocol chat sessions, `codex login` for codex — and returns the spawned session. The dashboard navigates to it; when the user returns to the chat and the page is activated, the auth-check clears the `signed_out` flag. The claude command logs out first so a half-dead credential cannot survive into the login.

Guards: 404 unknown session, 400 non-chat session, 409 remote chat session (its login lives on another host).

Request: no body.

Response: a spawn result for the login terminal session (`session_id`, `workspace_id`, `nickname`).

### POST /api/sessions/{sessionId}/auth-check

Runs the harness login-status check (`claude auth status --json` / `codex login status`) for the chat session's protocol and applies the answer to every in-scope chat session of that protocol: logged in clears `signed_out`, logged out sets it, no answer (timeout/unparseable) changes nothing. At most one check per protocol runs at a time. The page calls this on every activation (load, session-tab switch, refocus, visibility change) — this is how "I signed back in" gets answered, and how a logged-out session is detected before the user types.

Guards: 404 unknown session, 400 non-chat session, 409 remote chat session.

Request: no body. Response: `204 No Content` — the response body is unused; state changes arrive via the session broadcast (`/ws/dashboard`), where each session summary carries `signed_out` (chat sessions only).
```

- [ ] **Step 6: Verify the CI doc check locally**

Run: `./scripts/check-api-docs.sh`
Expected: exit 0.

- [ ] **Step 7: Commit**

Run `/commit` with message `feat(dashboard): reauth and auth-check endpoints`.

---

### Task 10: Frontend API functions

**Files:**

- Modify: `assets/dashboard/src/lib/api.ts:191` (after `analyzeFence`)

**Interfaces:**

- Produces (consumed by Tasks 11–12):
  - `export async function reauthSession(sessionId: string): Promise<SpawnResult>`
  - `export async function authCheck(sessionId: string): Promise<void>`

No separate test — covered by Task 11's hook test and Task 12's interaction tests via module mocks; this task's check is `./test.sh --quick` compiling the new exports.

- [ ] **Step 1: Add the functions** (exact `analyzeFence` shape, api.ts:182-191)

```ts
/**
 * Spawns the harness's login flow as a terminal session in the chat
 * session's workspace. Returns the login terminal session.
 */
export async function reauthSession(sessionId: string): Promise<SpawnResult> {
  const response = await apiFetch(`/api/sessions/${sessionId}/reauth`, {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to start sign-in');
  }
  return response.json();
}

/**
 * Asks the daemon to verify the login state for the session's protocol.
 * The response body is unused; resulting state changes arrive via the
 * session broadcast.
 */
export async function authCheck(sessionId: string): Promise<void> {
  const response = await apiFetch(`/api/sessions/${sessionId}/auth-check`, {
    method: 'POST',
    headers: { ...csrfHeaders() },
  });
  if (!response.ok) {
    await parseErrorResponse(response, 'Failed to check sign-in state');
  }
}
```

- [ ] **Step 2: Typecheck**

Run: `./test.sh --quick`
Expected: PASS (typecheck green, no unused-export failures).

- [ ] **Step 3: Commit**

Run `/commit` with message `feat(dashboard): reauth and auth-check api clients`.

---

### Task 11: Focus trigger hook + ChatSessionPage wiring

**Files:**

- Create: `assets/dashboard/src/hooks/useAuthCheckOnFocus.ts`
- Test: `assets/dashboard/src/hooks/useAuthCheckOnFocus.test.ts` (follow neighboring hook tests, e.g. `useChatSocket`/`useLocalStorage` tests for setup patterns)
- Modify: `assets/dashboard/src/routes/ChatSessionPage.tsx` — call the hook, add the reauth handler

**Interfaces:**

- Consumes: `authCheck`, `reauthSession` (Task 10); `useSessions().waitForSession` (existing, ChatSessionPage.tsx:32).
- Produces: `export function useAuthCheckOnFocus(sessionId: string | undefined, skip: boolean): void` — fires `authCheck` on mount, `window` focus, and `visibilitychange`→visible; never while `skip`. ChatView consumes `onReauth` in Task 12.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useAuthCheckOnFocus } from './useAuthCheckOnFocus';

vi.mock('../lib/api', () => ({
  authCheck: vi.fn().mockResolvedValue(undefined),
}));

import { authCheck } from '../lib/api';

describe('useAuthCheckOnFocus', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(authCheck).mockResolvedValue(undefined);
  });
  afterEach(() => vi.restoreAllMocks());

  it('fires on mount', () => {
    renderHook(() => useAuthCheckOnFocus('s1', false));
    expect(authCheck).toHaveBeenCalledWith('s1');
  });

  it('fires on window focus and on becoming visible', () => {
    renderHook(() => useAuthCheckOnFocus('s1', false));
    expect(authCheck).toHaveBeenCalledTimes(1);
    act(() => {
      window.dispatchEvent(new Event('focus'));
    });
    expect(authCheck).toHaveBeenCalledTimes(2);
    act(() => {
      Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true });
      document.dispatchEvent(new Event('visibilitychange'));
    });
    expect(authCheck).toHaveBeenCalledTimes(3);
  });

  it('never fires while skip is true (remote sessions)', () => {
    renderHook(() => useAuthCheckOnFocus('s1', true));
    act(() => {
      window.dispatchEvent(new Event('focus'));
    });
    expect(authCheck).not.toHaveBeenCalled();
  });

  it('does not fire on hidden visibility changes', () => {
    renderHook(() => useAuthCheckOnFocus('s1', false));
    const before = vi.mocked(authCheck).mock.calls.length;
    act(() => {
      Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true });
      document.dispatchEvent(new Event('visibilitychange'));
    });
    expect(vi.mocked(authCheck).mock.calls.length).toBe(before);
  });

  it('swallows failures (focus checks are best-effort)', async () => {
    vi.mocked(authCheck).mockRejectedValue(new Error('net'));
    renderHook(() => useAuthCheckOnFocus('s1', false));
    await act(async () => {});
    // No unhandled rejection: the hook caught it.
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./test.sh --quick`
Expected: FAIL — module `./useAuthCheckOnFocus` does not exist.

- [ ] **Step 3: Implement the hook**

```ts
import { useEffect } from 'react';
import { authCheck } from '../lib/api';

/**
 * Signed-out recovery focus trigger: every activation of the chat page —
 * load, session-tab switch (remount), window refocus, visibility change —
 * asks the daemon to verify the login for the session's protocol,
 * unconditionally (banner or not). Remote sessions never ask (their login
 * lives on another host). Failures are swallowed: the check is best-effort
 * and state arrives via the session broadcast.
 */
export function useAuthCheckOnFocus(sessionId: string | undefined, skip: boolean): void {
  useEffect(() => {
    if (!sessionId || skip) return;
    const fire = () => {
      void authCheck(sessionId).catch(() => {});
    };
    fire();
    const onVisibility = () => {
      if (document.visibilityState === 'visible') fire();
    };
    window.addEventListener('focus', fire);
    document.addEventListener('visibilitychange', onVisibility);
    return () => {
      window.removeEventListener('focus', fire);
      document.removeEventListener('visibilitychange', onVisibility);
    };
  }, [sessionId, skip]);
}
```

- [ ] **Step 4: Wire into ChatSessionPage**

In `ChatSessionPage.tsx`, after the `useChatSocket` call (~line 99):

```tsx
// Signed-out recovery: any activation of the page asks the daemon to
// verify the protocol login. Remote chat pages never ask.
useAuthCheckOnFocus(sessionId, Boolean(sessionData?.remote_host_id));
```

with the import:

```tsx
import { useAuthCheckOnFocus } from '../hooks/useAuthCheckOnFocus';
```

Add the reauth handler next to `handleAnalyzeFence` (~line 186):

```tsx
const handleReauth = async () => {
  if (!sessionId) return;
  try {
    const result = await reauthSession(sessionId);
    if (result.session_id) {
      await waitForSession(result.session_id);
      navigate(`/sessions/${result.session_id}`);
    }
  } catch (err) {
    alert('Sign-In Failed', `Failed to start sign-in: ${getErrorMessage(err, 'Unknown error')}`);
  }
};
```

extending the existing import from `'../lib/api'` (line 19) with `reauthSession`. Pass `onReauth={handleReauth}` into `ChatView` in Task 12 (add the prop line in this task only if implementing both together).

- [ ] **Step 5: Run tests**

Run: `./test.sh --quick`
Expected: PASS.

- [ ] **Step 6: Commit**

Run `/commit` with message `feat(dashboard): auth-check focus trigger and reauth navigation`.

---

### Task 12: Banner, composer lock, per-protocol copy

**Files:**

- Modify: `assets/dashboard/src/components/chat/ChatView.tsx` — props, banner, composer override
- Modify: `assets/dashboard/src/components/chat/chat.module.css` — banner class (tokens only)
- Modify: `assets/dashboard/src/routes/ChatSessionPage.tsx` — pass props
- Test: `assets/dashboard/src/components/chat/ChatView.test.tsx` (extend)

**Interfaces:**

- Consumes: `sessionData.signed_out`, `sessionData.chat_protocol` (Task 2 summary), `onReauth` (Task 11), Composer `disabled`/`disabledReason` (Composer.tsx:11-12).
- Produces: `ChatViewProps` gains `signedOut: boolean`, `signedOutProtocol: string`, `onReauth: () => void`.

- [ ] **Step 1: Write the failing tests** (extend `ChatView.test.tsx` following its existing render setup)

```tsx
describe('signed-out recovery', () => {
  it('shows the claude banner copy and sign-in button when signed out', () => {
    renderChatView({ signedOut: true, signedOutProtocol: 'claude-stream-json' });
    expect(screen.getByTestId('signed-out-banner')).toHaveTextContent(
      "Claude is signed out — messages won't reach it until you sign in again."
    );
    expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument();
  });

  it('shows the codex banner copy (restart instruction)', () => {
    renderChatView({ signedOut: true, signedOutProtocol: 'codex-app-server' });
    expect(screen.getByTestId('signed-out-banner')).toHaveTextContent(
      'Codex is signed out. After signing in, restart this session.'
    );
  });

  it('disables the composer with a reason placeholder while signed out', () => {
    renderChatView({ signedOut: true, signedOutProtocol: 'claude-stream-json' });
    const textarea = screen.getByTestId('composer-input');
    expect(textarea).toBeDisabled();
    expect(textarea).toHaveAttribute('placeholder', 'Signed out — sign in to continue');
  });

  it('renders no banner and leaves the composer alone when clear', () => {
    renderChatView({ signedOut: false, signedOutProtocol: '' });
    expect(screen.queryByTestId('signed-out-banner')).not.toBeInTheDocument();
  });

  it('navigates via onReauth when the sign-in button is clicked', async () => {
    const onReauth = vi.fn();
    renderChatView({ signedOut: true, signedOutProtocol: 'claude-stream-json', onReauth });
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }));
    expect(onReauth).toHaveBeenCalledTimes(1);
  });
});
```

(`renderChatView` is the file's existing helper — extend it to accept and default the three new props: `signedOut: false`, `signedOutProtocol: ''`, `onReauth: vi.fn()`. Check the composer input's real testid in `Composer.test.tsx` and use that selector.)

- [ ] **Step 2: Run test to verify it fails**

Run: `./test.sh --quick`
Expected: FAIL — no `signed-out-banner` testid.

- [ ] **Step 3: Implement**

In `ChatView.tsx`:

Add to `ChatViewProps`:

```tsx
  /** Signed-out recovery: banner + composer lock while the login is absent. */
  signedOut: boolean;
  signedOutProtocol: string;
  onReauth: () => void;
```

Add above the component:

```tsx
const SIGNED_OUT_COPY: Record<string, string> = {
  'claude-stream-json': "Claude is signed out — messages won't reach it until you sign in again.",
  'codex-app-server': 'Codex is signed out. After signing in, restart this session.',
};
```

Render the banner between the `socketError` banner and `Composer` (~line 124), and lock the composer:

```tsx
{
  signedOut ? (
    <div className={styles.signedOutBanner} role="alert" data-testid="signed-out-banner">
      <span>{SIGNED_OUT_COPY[signedOutProtocol] ?? SIGNED_OUT_COPY['claude-stream-json']}</span>
      <button
        className="btn btn--sm btn--secondary"
        onClick={onReauth}
        data-testid="signed-out-reauth"
      >
        Sign in
      </button>
    </div>
  ) : null;
}
<Composer
  ref={composerRef}
  disabled={signedOut || status !== 'connected'}
  disabledReason={signedOut ? 'Signed out — sign in to continue' : undefined}
  ended={ended}
  onSend={onSend}
  initialDraft={initialDraft}
  onDraftChange={onDraftChange}
  onCaretChange={onCaretChange}
/>;
```

In `chat.module.css` (tokens only — no hardcoded palette values, per `docs/dashboard-style-guide.md`):

```css
.signedOutBanner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.5rem 0.75rem;
  margin: 0 0.5rem;
  background: var(--color-warning-subtle);
  border: 1px solid var(--color-warning);
  border-radius: 4px;
  color: var(--color-text);
  font-size: 0.85rem;
}
```

In `ChatSessionPage.tsx`, pass the props into `ChatView` (after `socketError={socketError}`):

```tsx
              signedOut={Boolean(sessionData.signed_out)}
              signedOutProtocol={sessionData.chat_protocol || 'claude-stream-json'}
              onReauth={handleReauth}
```

(The `|| 'claude-stream-json'` mirrors `EffectiveChatProtocol`'s pre-field default, state.go:357-368.)

- [ ] **Step 4: Run tests**

Run: `./test.sh --quick`
Expected: PASS.

- [ ] **Step 5: Commit**

Run `/commit` with message `feat(dashboard): signed-out banner, composer lock, sign-in action`.

---

### Task 13: Badges — session tab and app sidebar

**Files:**

- Modify: `assets/dashboard/src/components/SessionTabs.tsx:122` — badge in the tab row
- Modify: `assets/dashboard/src/components/AppShell.tsx:919-929` — badge in the nav session item
- Modify: `assets/dashboard/src/styles/global.css` — one shared badge class (tokens only)
- Test: extend `assets/dashboard/src/components/SessionTabs.test.tsx` and `AppShell.test.tsx`

**Interfaces:**

- Consumes: `sess.signed_out` on the session summary (Task 2).
- Produces: `<span className="session-badge--signed-out">Signed out</span>` rendered in both surfaces when the flag is set.

- [ ] **Step 1: Write the failing tests** (extend both files' existing suites; seed sessions the way their current tests do)

```tsx
it('shows a Signed out badge for a signed-out chat session', () => {
  renderSessionTabs({ sessions: [chatSession({ id: 's1', signed_out: true })] });
  expect(screen.getByText('Signed out')).toBeInTheDocument();
});

it('shows no badge when the flag is clear', () => {
  renderSessionTabs({ sessions: [chatSession({ id: 's1', signed_out: false })] });
  expect(screen.queryByText('Signed out')).not.toBeInTheDocument();
});
```

and the equivalent pair in `AppShell.test.tsx` against its session-list rendering.

- [ ] **Step 2: Run tests to verify they fail**

Run: `./test.sh --quick`
Expected: FAIL — no `Signed out` text.

- [ ] **Step 3: Implement**

`SessionTabs.tsx` — in the tab render, next to the row2 nudge preview (line ~122):

```tsx
{
  sess.signed_out && <span className="session-badge--signed-out">Signed out</span>;
}
{
  nudgePreviewElement && <div className="session-tab__row2">{nudgePreviewElement}</div>;
}
```

`AppShell.tsx` — in the nav-session item, before the existing nudge preview element construction site (line ~919), render the same span inside the item's meta row (pick the exact insertion point by reading the item's JSX — the row that renders `nudgePreviewElement`):

```tsx
{
  sess.signed_out && <span className="session-badge--signed-out">Signed out</span>;
}
```

`global.css` (tokens only):

```css
.session-badge--signed-out {
  display: inline-block;
  padding: 0.05rem 0.4rem;
  border-radius: 3px;
  font-size: 0.7rem;
  background: var(--color-warning-subtle);
  color: var(--color-text-muted);
  border: 1px solid var(--color-warning);
  white-space: nowrap;
}
```

- [ ] **Step 4: Run tests**

Run: `./test.sh --quick`
Expected: PASS.

- [ ] **Step 5: Commit**

Run `/commit` with message `feat(dashboard): signed-out badges on session tab and sidebar`.

---

### Task 14: Gates, style compliance, acceptance scenarios

**Files:**

- No new code files. Produces: `review/chat-signed-out-recovery-acceptance.html` — the acceptance run report (HTML per output conventions, in `./review/`).

**Interfaces:**

- Consumes: everything above.

- [ ] **Step 1: Dashboard style compliance**

Run the `schmux-dashboard-style-check` skill against the changed UI (banner, badge, composer states). Fix any token violations it flags. The nudge/status element must be untouched — verify by inspection that no nudge rendering path changed.

- [ ] **Step 2: Full gates**

Run from repo root:

```bash
./format.sh
./test.sh
./badcode.sh
```

Expected: all three green. `./test.sh` (not `--quick`) — it includes typecheck and E2E (Docker). Do not substitute a faster alternative.

- [ ] **Step 3: Run the acceptance scenarios**

Perform each scenario from the spec's Acceptance section on a machine with provider secrets configured, and record the outcome (pass/fail/observed mechanism) in `review/chat-signed-out-recovery-acceptance.html`:

1. anthropic provider secret stored; sign out of claude externally; send a message in a claude chat session → banner + composer lock immediately.
2. Click sign-in → login terminal spawns in the chat session's workspace; page navigates to it.
3. Complete login, return, refocus → banner and lock clear via the focus check, and the next message gets a reply.
4. Repeat 1–3 for codex, noting which mechanism set the banner (turn error vs focus check — unobserved until now; record it). After re-login and before restart, a sent message is held; restarting delivers it in the resumed conversation.
5. With the banner up, restart the daemon and reload → banner persists; refocus after restoring login clears it.
6. Sessions spawned before the feature (no new persisted fields) behave identically in 1–5.
7. A usage-limit error leaves no standing banner.
8. A session routed through `ANTHROPIC_BASE_URL` never shows the banner.
9. Remote chat sessions never show it.
10. An open idle chat page produces no periodic requests beyond the two resident WebSockets.
11. The existing session status/nudge element behaves exactly as before.

If scenario 4 reveals the codex turn holds rather than errors (banner arrives only via focus check), tighten the codex matcher list from the observed error text (or record that the matcher had nothing to match — that is a valid outcome, the focus check carries it). Tightening the lists is the one expected code change from this step; re-run the affected unit tests and `/commit` with message `feat(chat): observed sign-out statements from acceptance run`.

- [ ] **Step 4: Report**

Summarize results to the user with the acceptance report path. The spec's own words apply: "unit-green alone is not completion evidence."

---

## Self-Review (performed at plan time)

**Spec coverage:** Problem → banner/lock/badge (Tasks 12–13), one-click re-auth (Tasks 9, 11–12). Concept → persisted bool (Task 1), session broadcast (Task 2), no history reads (Task 4 replay gate). State rules → matcher set (Tasks 3–5, 8), tool correct/clear (Tasks 7–8), in-flight dedup (Task 8), two triggers (Tasks 8–9, 11), no interval/startup check (nothing scheduled anywhere; acceptance 10 verifies). Scope rules → `chatSessionInScope` + `RoutesToEndpoint` (Tasks 6, 8; remote guard in handlers Task 9). Recovery UX copy verbatim (Task 12). Endpoints + api.md (Task 9). Design constraints → no spawn-time eligibility field (none added), no env predicate (none), no credentials-file inspection (authcheck only runs the tools), nudge untouched (no nudge files modified). Acceptance scenarios (Task 14). Testing requirements → matcher tests (T3), tool-checker stubs (T7), scope tests incl. provider-secrets case (T8 — `newAuthCheckServer` uses bare-tool targets which is the provider-secrets-equivalent first-party case; the literal "config with provider secrets present" scenario is covered by acceptance 1), persistence (T1), trigger tests (T8–T9, T11), frontend tests (T11–T13).

**Placeholder scan:** the two `newTestServer`/`newTestManagerWithRegistry` notes instruct reusing existing test helpers verbatim with search pointers — every other code block is complete.

**Type consistency:** `SignedOut`/`signed_out` (Go/JSON/TS), `chat.TurnErrorEvent{Protocol, Text}`, `SetChatTurnErrorCallback(func(sessionID string, ev chat.TurnErrorEvent))`, `RunAuthCheck(protocol string)`, `RoutesToEndpoint(target string) bool`, `authcheck.Run(ctx, protocol) (Result, string)`, `useAuthCheckOnFocus(sessionId, skip)`, `reauthSession`/`authCheck` — consistent across tasks.
