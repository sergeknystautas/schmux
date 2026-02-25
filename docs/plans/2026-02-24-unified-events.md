# Unified Agent Event System Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Replace the signal file and lore JSONL with a single per-session append-only event file, giving all consumers (dashboard, floor manager, lore) a typed pub/sub system.

**Architecture:** New `internal/events/` package with typed events (status, failure, reflection, friction), per-session JSONL files at `<workspace>/.schmux/events/<session-id>.jsonl`, fsnotify-based watcher with handler dispatch by event type. Hook scripts centralized at `~/.schmux/hooks/`. Four-phase migration: dual-write, consumer switch, signal file removal, lore.jsonl removal.

**Tech Stack:** Go, fsnotify, JSONL, shell scripts (bash), tmux control mode

**Design doc:** `docs/specs/unified-events.md`

---

## Phase 1: Event Infrastructure + Dual-Write

### Task 1: Event Types Package

Create the core event types that all consumers will use.

**Files:**

- Create: `internal/events/types.go`
- Create: `internal/events/types_test.go`

**Step 1: Write the failing test**

```go
// internal/events/types_test.go
package events

import (
	"encoding/json"
	"testing"
)

func TestStatusEventMarshal(t *testing.T) {
	e := StatusEvent{
		Ts:      "2026-02-18T14:30:00Z",
		Type:    "status",
		State:   "working",
		Message: "Refactoring auth module",
		Intent:  "Improve module structure",
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	json.Unmarshal(data, &parsed)
	if parsed["type"] != "status" {
		t.Errorf("type = %v, want status", parsed["type"])
	}
	if parsed["state"] != "working" {
		t.Errorf("state = %v, want working", parsed["state"])
	}
}

func TestFailureEventMarshal(t *testing.T) {
	e := FailureEvent{
		Ts:       "2026-02-18T14:30:00Z",
		Type:     "failure",
		Tool:     "Bash",
		Input:    "go build ./...",
		Error:    "undefined: Foo",
		Category: "build_failure",
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	json.Unmarshal(data, &parsed)
	if parsed["type"] != "failure" {
		t.Errorf("type = %v, want failure", parsed["type"])
	}
	if parsed["tool"] != "Bash" {
		t.Errorf("tool = %v, want Bash", parsed["tool"])
	}
}

func TestReflectionEventMarshal(t *testing.T) {
	e := ReflectionEvent{
		Ts:   "2026-02-18T14:30:00Z",
		Type: "reflection",
		Text: "When using bare repos, run git fetch before git show",
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	json.Unmarshal(data, &parsed)
	if parsed["type"] != "reflection" {
		t.Errorf("type = %v, want reflection", parsed["type"])
	}
}

func TestFrictionEventMarshal(t *testing.T) {
	e := FrictionEvent{
		Ts:   "2026-02-18T14:30:00Z",
		Type: "friction",
		Text: "The build command is go run ./cmd/build-dashboard",
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	json.Unmarshal(data, &parsed)
	if parsed["type"] != "friction" {
		t.Errorf("type = %v, want friction", parsed["type"])
	}
}

func TestParseRawEvent(t *testing.T) {
	line := `{"ts":"2026-02-18T14:30:00Z","type":"status","state":"working","message":"test"}`
	raw, err := ParseRawEvent([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	if raw.Type != "status" {
		t.Errorf("type = %v, want status", raw.Type)
	}
}

func TestParseRawEventInvalidJSON(t *testing.T) {
	_, err := ParseRawEvent([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/events/...
```

Expected: FAIL — package does not exist.

**Step 3: Write minimal implementation**

```go
// internal/events/types.go
package events

import "encoding/json"

// RawEvent is the common envelope parsed from each JSONL line.
type RawEvent struct {
	Ts   string `json:"ts"`
	Type string `json:"type"`
}

// ParseRawEvent extracts the envelope fields from a JSONL line.
func ParseRawEvent(data []byte) (RawEvent, error) {
	var raw RawEvent
	if err := json.Unmarshal(data, &raw); err != nil {
		return RawEvent{}, err
	}
	return raw, nil
}

// StatusEvent represents an agent state change.
type StatusEvent struct {
	Ts       string `json:"ts"`
	Type     string `json:"type"`
	State    string `json:"state"`
	Message  string `json:"message,omitempty"`
	Intent   string `json:"intent,omitempty"`
	Blockers string `json:"blockers,omitempty"`
}

// ValidStates for status events.
var ValidStates = map[string]bool{
	"working":       true,
	"completed":     true,
	"needs_input":   true,
	"needs_testing": true,
	"error":         true,
	"rotate":        true,
}

// FailureEvent represents a tool failure.
type FailureEvent struct {
	Ts       string `json:"ts"`
	Type     string `json:"type"`
	Tool     string `json:"tool"`
	Input    string `json:"input"`
	Error    string `json:"error"`
	Category string `json:"category"`
}

// ReflectionEvent represents a friction learning.
type ReflectionEvent struct {
	Ts   string `json:"ts"`
	Type string `json:"type"`
	Text string `json:"text"`
}

// FrictionEvent represents an ad-hoc friction note.
type FrictionEvent struct {
	Ts   string `json:"ts"`
	Type string `json:"type"`
	Text string `json:"text"`
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/events/...
```

Expected: PASS

**Step 5: Commit**

Use `/commit` with message: "feat(events): add event types package with status, failure, reflection, friction"

---

### Task 2: Event File I/O

Writer appends JSON lines, reader scans files for events.

**Files:**

- Create: `internal/events/writer.go`
- Create: `internal/events/reader.go`
- Create: `internal/events/io_test.go`

**Step 1: Write the failing tests**

```go
// internal/events/io_test.go
package events

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppendEvent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	err := AppendEvent(path, StatusEvent{
		Ts: "2026-02-18T14:30:00Z", Type: "status",
		State: "working", Message: "doing stuff",
	})
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	if len(data) == 0 {
		t.Fatal("file is empty")
	}
	// Must end with newline
	if data[len(data)-1] != '\n' {
		t.Error("event line does not end with newline")
	}
}

func TestAppendMultipleEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	AppendEvent(path, StatusEvent{Ts: "2026-02-18T14:30:00Z", Type: "status", State: "working"})
	AppendEvent(path, StatusEvent{Ts: "2026-02-18T14:31:00Z", Type: "status", State: "completed"})

	events, err := ReadEvents(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
}

func TestReadLastByType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	AppendEvent(path, StatusEvent{Ts: "2026-02-18T14:30:00Z", Type: "status", State: "working", Message: "first"})
	AppendEvent(path, FailureEvent{Ts: "2026-02-18T14:30:30Z", Type: "failure", Tool: "Bash"})
	AppendEvent(path, StatusEvent{Ts: "2026-02-18T14:31:00Z", Type: "status", State: "completed", Message: "done"})

	raw, data, err := ReadLastByType(path, "status")
	if err != nil {
		t.Fatal(err)
	}
	if raw.Ts != "2026-02-18T14:31:00Z" {
		t.Errorf("ts = %v, want 2026-02-18T14:31:00Z", raw.Ts)
	}
	if data == nil {
		t.Fatal("data is nil")
	}
}

func TestReadLastByTypeNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	AppendEvent(path, StatusEvent{Ts: "2026-02-18T14:30:00Z", Type: "status", State: "working"})

	_, _, err := ReadLastByType(path, "reflection")
	if err == nil {
		t.Error("expected error for missing type")
	}
}

func TestReadEventsWithFilter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	AppendEvent(path, StatusEvent{Ts: "2026-02-18T14:30:00Z", Type: "status", State: "working"})
	AppendEvent(path, FailureEvent{Ts: "2026-02-18T14:30:30Z", Type: "failure", Tool: "Bash"})
	AppendEvent(path, ReflectionEvent{Ts: "2026-02-18T14:31:00Z", Type: "reflection", Text: "test"})

	loreTypes := map[string]bool{"failure": true, "reflection": true, "friction": true}
	events, err := ReadEvents(path, func(raw RawEvent) bool {
		return loreTypes[raw.Type]
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (failure + reflection)", len(events))
	}
}

func TestReadEventsNonexistentFile(t *testing.T) {
	events, err := ReadEvents("/nonexistent/path.jsonl", nil)
	if err != nil {
		t.Fatal("should not error for missing file")
	}
	if len(events) != 0 {
		t.Fatalf("got %d events, want 0", len(events))
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/events/...
```

Expected: FAIL — `AppendEvent`, `ReadEvents`, `ReadLastByType` not defined.

**Step 3: Write minimal implementation**

```go
// internal/events/writer.go
package events

import (
	"encoding/json"
	"os"
	"sync"
)

var writeMu sync.Mutex

// AppendEvent marshals an event to JSON and appends it as a line to the file.
func AppendEvent(path string, event any) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	writeMu.Lock()
	defer writeMu.Unlock()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}
```

```go
// internal/events/reader.go
package events

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// EventLine holds the raw envelope and the original JSON bytes.
type EventLine struct {
	RawEvent
	Data []byte
}

// ReadEvents reads all events from a JSONL file, applying an optional filter.
// Returns empty slice (not error) for nonexistent files.
func ReadEvents(path string, filter func(RawEvent) bool) ([]EventLine, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var events []EventLine
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		raw, err := ParseRawEvent(line)
		if err != nil {
			continue // skip malformed lines
		}
		if filter != nil && !filter(raw) {
			continue
		}
		cp := make([]byte, len(line))
		copy(cp, line)
		events = append(events, EventLine{RawEvent: raw, Data: cp})
	}
	return events, scanner.Err()
}

// ReadLastByType returns the last event of the given type and its raw JSON.
func ReadLastByType(path string, eventType string) (RawEvent, json.RawMessage, error) {
	events, err := ReadEvents(path, func(raw RawEvent) bool {
		return raw.Type == eventType
	})
	if err != nil {
		return RawEvent{}, nil, err
	}
	if len(events) == 0 {
		return RawEvent{}, nil, fmt.Errorf("no event of type %q found", eventType)
	}
	last := events[len(events)-1]
	return last.RawEvent, json.RawMessage(last.Data), nil
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/events/...
```

Expected: PASS

**Step 5: Commit**

Use `/commit` with message: "feat(events): add event file writer and reader with filtering"

---

### Task 3: Event File Provisioning

Create the `.schmux/events/` directory alongside `.schmux/signal/` at session spawn time. Set `SCHMUX_EVENTS_FILE` env var. Update git exclude patterns.

**Files:**

- Modify: `internal/session/manager.go:640-643` (Spawn signal dir), `657-662` (Spawn env), `760-763` (SpawnCommand signal dir), `766-771` (SpawnCommand env), `415-420` (SpawnRemote env), `508-511` (SpawnRemote queued mkdir), `528-532` (SpawnRemote connected mkdir)
- Modify: `internal/workspace/ensure/manager.go:617-621` (excludePatterns)

**Step 1: In `Spawn()`, add events directory creation alongside signal directory**

At `manager.go:640-643`, the code creates `.schmux/signal/`. Add `.schmux/events/` creation right after it:

```go
schmuxDir := filepath.Join(w.Path, ".schmux", "signal")
if err := os.MkdirAll(schmuxDir, 0755); err != nil {
	return "", fmt.Errorf("create signal dir: %w", err)
}
eventsDir := filepath.Join(w.Path, ".schmux", "events")
if err := os.MkdirAll(eventsDir, 0755); err != nil {
	return "", fmt.Errorf("create events dir: %w", err)
}
```

**Step 2: In `Spawn()`, add `SCHMUX_EVENTS_FILE` to env map**

At `manager.go:657-662`, add the new env var:

```go
resolved.Env = mergeEnvMaps(resolved.Env, map[string]string{
	"SCHMUX_ENABLED":      "1",
	"SCHMUX_SESSION_ID":   sessionID,
	"SCHMUX_WORKSPACE_ID": w.ID,
	"SCHMUX_STATUS_FILE":  filepath.Join(w.Path, ".schmux", "signal", sessionID),
	"SCHMUX_EVENTS_FILE":  filepath.Join(w.Path, ".schmux", "events", sessionID+".jsonl"),
})
```

**Step 3: Repeat for `SpawnCommand()`**

Same changes at `manager.go:760-763` (dir creation) and `766-771` (env vars).

**Step 4: Repeat for `SpawnRemote()`**

At `manager.go:415-420` (env vars), add `SCHMUX_EVENTS_FILE`.

At `manager.go:508-511` and `528-532` (remote mkdir), add `mkdir -p .schmux/events` alongside `mkdir -p .schmux/signal`.

**Step 5: Add `.schmux/events/` to git exclude patterns**

At `ensure/manager.go:617-621`:

```go
var excludePatterns = []string{
	".schmux/signal/",
	".schmux/hooks/",
	".schmux/lore.jsonl",
	".schmux/events/",
}
```

**Step 6: Run tests**

```bash
go test ./internal/session/... ./internal/workspace/...
```

Expected: PASS (no behavior change for existing tests — this is additive).

**Step 7: Commit**

Use `/commit` with message: "feat(events): provision event file directory and env var at session spawn"

---

### Task 4: Centralize Hook Scripts

Move hook scripts from per-workspace `.schmux/hooks/` to central `~/.schmux/hooks/`. Split `stop-gate.sh` into `stop-status-check.sh` and `stop-lore-check.sh`. Rewrite `capture-failure.sh` to write to `$SCHMUX_EVENTS_FILE`.

**Files:**

- Create: `internal/workspace/ensure/hooks/stop-status-check.sh`
- Create: `internal/workspace/ensure/hooks/stop-lore-check.sh`
- Modify: `internal/workspace/ensure/hooks/capture-failure.sh`
- Modify: `internal/workspace/ensure/manager.go:722-738` (LoreHookScripts → EnsureGlobalHookScripts)
- Modify: `internal/daemon/daemon.go` (call EnsureGlobalHookScripts at startup)

**Step 1: Create `stop-status-check.sh`**

Extract the status-checking logic from `stop-gate.sh` (lines 19-27) into a standalone script that reads from `$SCHMUX_EVENTS_FILE`:

```bash
#!/bin/bash
# stop-status-check.sh — gates agent stop on status event in event file.
# Reads from $SCHMUX_EVENTS_FILE (per-session append-only JSONL).
INPUT=$(cat)
ACTIVE=$(echo "$INPUT" | jq -r '.stop_hook_active // false')
[ "$ACTIVE" = "true" ] && exit 0
[ -n "$SCHMUX_EVENTS_FILE" ] || exit 0

if [ -f "$SCHMUX_EVENTS_FILE" ]; then
  LAST_STATE=$(grep '"type":"status"' "$SCHMUX_EVENTS_FILE" | tail -1 | jq -r '.state // ""')
  case "$LAST_STATE" in
    completed|needs_input|needs_testing|error) exit 0 ;;
    working)
      LAST_MSG=$(grep '"type":"status"' "$SCHMUX_EVENTS_FILE" | tail -1 | jq -r '.message // ""')
      [ -n "$LAST_MSG" ] && exit 0 ;;
  esac
fi

printf '{"decision":"block","reason":"Write your status before finishing. Use schmux_status to report: echo '\''{\"ts\":\"...\",\"type\":\"status\",\"state\":\"completed\",\"message\":\"what you did\"}'\'' >> \"$SCHMUX_EVENTS_FILE\""}\n'
exit 0
```

**Step 2: Create `stop-lore-check.sh`**

Extract the reflection-checking logic from `stop-gate.sh` (lines 29-48):

```bash
#!/bin/bash
# stop-lore-check.sh — gates agent stop on friction reflection in event file.
# Reads from $SCHMUX_EVENTS_FILE (per-session append-only JSONL).
INPUT=$(cat)
ACTIVE=$(echo "$INPUT" | jq -r '.stop_hook_active // false')
[ "$ACTIVE" = "true" ] && exit 0
[ -n "$SCHMUX_EVENTS_FILE" ] || exit 0

if grep -q '"type":"reflection"' "$SCHMUX_EVENTS_FILE" 2>/dev/null; then
  exit 0
fi

printf '{"decision":"block","reason":"Write a friction reflection before finishing. Report what tripped you up: echo '\''{\"ts\":\"...\",\"type\":\"reflection\",\"text\":\"When X, do Y instead\"}'\'' >> \"$SCHMUX_EVENTS_FILE\""}\n'
exit 0
```

**Step 3: Rewrite `capture-failure.sh` to write to `$SCHMUX_EVENTS_FILE`**

Change the output from appending a lore entry to `.schmux/lore.jsonl` to appending a failure event to `$SCHMUX_EVENTS_FILE`. Remove the `ws` and `session` fields (derived from file path). Keep the same tool extraction and category classification logic. Change the final `jq` output to match the failure event schema (`type`, `tool`, `input`, `error`, `category`).

The key change is the output section (currently lines 61-79 in capture-failure.sh):

```bash
# Output: append failure event to session event file
[ -n "$SCHMUX_EVENTS_FILE" ] || exit 0

TS=$(date -u +%Y-%m-%dT%H:%M:%SZ)
jq -n -c \
  --arg ts "$TS" \
  --arg tool "$TOOL_NAME" \
  --arg input "$INPUT_SUMMARY" \
  --arg error "$ERROR_MSG" \
  --arg category "$CATEGORY" \
  '{ts: $ts, type: "failure", tool: $tool, input: $input, error: $error, category: $category}' \
  >> "$SCHMUX_EVENTS_FILE"
```

**Step 4: Replace `LoreHookScripts()` with `EnsureGlobalHookScripts()`**

At `ensure/manager.go:722-738`, change the function to write to `~/.schmux/hooks/` instead of `<workspace>/.schmux/hooks/`:

```go
//go:embed hooks/capture-failure.sh
var captureFailureScript []byte

//go:embed hooks/stop-status-check.sh
var stopStatusCheckScript []byte

//go:embed hooks/stop-lore-check.sh
var stopLoreCheckScript []byte

// EnsureGlobalHookScripts writes hook scripts to ~/.schmux/hooks/.
// Called once at daemon startup. Returns the hooks directory path.
func EnsureGlobalHookScripts(homeDir string) (string, error) {
	hooksDir := filepath.Join(homeDir, ".schmux", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return "", err
	}
	scripts := map[string][]byte{
		"capture-failure.sh":   captureFailureScript,
		"stop-status-check.sh": stopStatusCheckScript,
		"stop-lore-check.sh":   stopLoreCheckScript,
	}
	for name, content := range scripts {
		path := filepath.Join(hooksDir, name)
		if err := os.WriteFile(path, content, 0755); err != nil {
			return "", fmt.Errorf("write %s: %w", name, err)
		}
	}
	return hooksDir, nil
}
```

Keep `LoreHookScripts()` for backward compat during migration — it still writes the old scripts per-workspace. It will be removed in phase 3.

**Step 5: Call `EnsureGlobalHookScripts()` from daemon startup**

In `daemon.go`, after config loading but before session tracker creation, call:

```go
hooksDir, err := ensure.EnsureGlobalHookScripts(homeDir)
if err != nil {
    log.Printf("warning: failed to write global hook scripts: %v", err)
}
```

Store `hooksDir` for passing to `buildClaudeHooksMap()` in task 5.

**Step 6: Run tests**

```bash
go test ./internal/workspace/ensure/... ./internal/daemon/...
```

Expected: PASS

**Step 7: Commit**

Use `/commit` with message: "feat(events): centralize hook scripts and split stop-gate into status + lore checks"

---

### Task 5: Update Hook Configuration for Dual-Write

Update `buildClaudeHooksMap()` to accept `hooksDir` parameter and dual-write to both signal file and event file. Update `SignalingInstructions` for the event protocol.

**Files:**

- Modify: `internal/workspace/ensure/manager.go:378-390` (signal helpers), `393-463` (buildClaudeHooksMap), `97-154` (SignalingInstructions), `509-588` (ClaudeHooks)

**Step 1: Add event-writing helper functions**

Alongside existing `signalCommand()` and `signalCommandWithContext()`, add helpers that append JSON events to `$SCHMUX_EVENTS_FILE`:

```go
func statusEventCommand(state, messageExpr string) string {
	return fmt.Sprintf(
		`[ -n "$SCHMUX_EVENTS_FILE" ] && printf '{"ts":"%%s","type":"status","state":"%s","message":"%%s"}\n' "$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)" "%s" >> "$SCHMUX_EVENTS_FILE" || true`,
		state, messageExpr,
	)
}

func statusEventWithIntentCommand(state, jqField string) string {
	return fmt.Sprintf(
		`[ -n "$SCHMUX_EVENTS_FILE" ] && { MSG=$(jq -r ".%s // empty" 2>/dev/null | tr -d "\n" | cut -c1-100 || true); printf '{"ts":"%%s","type":"status","state":"%s","message":"%%s","intent":"%%s"}\n' "$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)" "${MSG}" "${MSG}" >> "$SCHMUX_EVENTS_FILE"; } || true`,
		jqField, state,
	)
}

func statusEventWithBlockersCommand(state, jqField string) string {
	return fmt.Sprintf(
		`[ -n "$SCHMUX_EVENTS_FILE" ] && { MSG=$(jq -r ".%s // empty" 2>/dev/null | tr -d "\n" | cut -c1-100 || true); printf '{"ts":"%%s","type":"status","state":"%s","message":"%%s","blockers":"%%s"}\n' "$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)" "${MSG}" "${MSG}" >> "$SCHMUX_EVENTS_FILE"; } || true`,
		jqField, state,
	)
}
```

**Step 2: Update `buildClaudeHooksMap()` to dual-write and accept `hooksDir`**

Change the signature to `buildClaudeHooksMap(hooksDir string)`. Each hook command writes to **both** the signal file (existing `signalCommand`) **and** the event file (new `statusEventCommand`), joined with `&&`. Stop hooks reference scripts at `hooksDir` by absolute path instead of `$CLAUDE_PROJECT_DIR/.schmux/hooks/`.

The function should produce commands like:

```bash
# SessionStart: dual-write
[ -n "$SCHMUX_STATUS_FILE" ] && echo "working" > "$SCHMUX_STATUS_FILE" || true; [ -n "$SCHMUX_EVENTS_FILE" ] && printf '...' >> "$SCHMUX_EVENTS_FILE" || true
```

```bash
# Stop: absolute paths to centralized scripts
[ -f "/Users/you/.schmux/hooks/stop-status-check.sh" ] && "/Users/you/.schmux/hooks/stop-status-check.sh" || true
```

**Step 3: Update `ClaudeHooks()` to accept and pass through `hooksDir`**

Change `ClaudeHooks(workspacePath string)` to `ClaudeHooks(workspacePath, hooksDir string)`. Pass `hooksDir` to `buildClaudeHooksMap(hooksDir)`.

**Step 4: Update all callers of `ClaudeHooks()`**

Search for calls to `ClaudeHooks` in `ensureWorkspace()` and `WrapCommandWithHooks()`. Pass the `hooksDir` through. The `Ensurer` struct or `ensureWorkspace()` function needs access to `hooksDir` — add it as a field on `Ensurer` or a parameter.

**Step 5: Update `SignalingInstructions`**

At `ensure/manager.go:97-154`, update the const to teach agents the event protocol:

- Replace `echo "STATE message" > $SCHMUX_STATUS_FILE` with `echo '{"ts":"...","type":"status","state":"STATE","message":"..."}' >> "$SCHMUX_EVENTS_FILE"`
- Keep the `$SCHMUX_STATUS_FILE` instructions during dual-write phase (agents write to both)
- Add reflection event instructions

**Step 6: Run tests**

```bash
go test ./internal/workspace/ensure/... ./internal/session/... ./internal/daemon/...
```

Expected: PASS

**Step 7: Commit**

Use `/commit` with message: "feat(events): dual-write hooks to signal file + event file, centralized hook paths"

---

## Phase 2: Event Watcher + Pub/Sub

### Task 6: EventWatcher (Local)

Create the local event watcher with handler interface and typed dispatch.

**Files:**

- Create: `internal/events/handler.go`
- Create: `internal/events/watcher.go`
- Create: `internal/events/watcher_test.go`

**Step 1: Write the failing tests**

```go
// internal/events/watcher_test.go
package events

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type testHandler struct {
	mu     sync.Mutex
	events []RawEvent
	data   [][]byte
}

func (h *testHandler) HandleEvent(ctx context.Context, sessionID string, raw RawEvent, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, raw)
	cp := make([]byte, len(data))
	copy(cp, data)
	h.data = append(h.data, cp)
}

func (h *testHandler) getEvents() []RawEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	cp := make([]RawEvent, len(h.events))
	copy(cp, h.events)
	return cp
}

func TestEventWatcherDispatch(t *testing.T) {
	dir := t.TempDir()
	eventsDir := filepath.Join(dir, ".schmux", "events")
	os.MkdirAll(eventsDir, 0755)
	path := filepath.Join(eventsDir, "test-session.jsonl")

	statusHandler := &testHandler{}
	failureHandler := &testHandler{}

	w, err := NewEventWatcher(path, "test-session", map[string][]EventHandler{
		"status":  {statusHandler},
		"failure": {failureHandler},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Write a status event
	AppendEvent(path, StatusEvent{
		Ts: "2026-02-18T14:30:00Z", Type: "status",
		State: "working", Message: "test",
	})

	// Wait for dispatch
	deadline := time.After(2 * time.Second)
	for {
		if len(statusHandler.getEvents()) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for status event dispatch")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	if len(failureHandler.getEvents()) != 0 {
		t.Error("failure handler should not have received events")
	}
}

func TestEventWatcherReadCurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	// Pre-populate file
	AppendEvent(path, StatusEvent{Ts: "2026-02-18T14:30:00Z", Type: "status", State: "working", Message: "first"})
	AppendEvent(path, StatusEvent{Ts: "2026-02-18T14:31:00Z", Type: "status", State: "completed", Message: "done"})

	status, err := ReadCurrentStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "completed" {
		t.Errorf("state = %v, want completed", status.State)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/events/...
```

Expected: FAIL — `NewEventWatcher`, `EventHandler`, `ReadCurrentStatus` not defined.

**Step 3: Write minimal implementation**

```go
// internal/events/handler.go
package events

import "context"

// EventHandler processes events dispatched by an EventWatcher.
type EventHandler interface {
	HandleEvent(ctx context.Context, sessionID string, raw RawEvent, data []byte)
}
```

```go
// internal/events/watcher.go
package events

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// EventWatcher monitors a per-session event file and dispatches events to handlers.
type EventWatcher struct {
	path      string
	sessionID string
	offset    int64
	handlers  map[string][]EventHandler
	ctx       context.Context
	cancel    context.CancelFunc
	fsw       *fsnotify.Watcher
	mu        sync.Mutex
}

// NewEventWatcher creates a watcher for the given event file.
// handlers maps event type strings to slices of handlers.
func NewEventWatcher(path, sessionID string, handlers map[string][]EventHandler) (*EventWatcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(path)
	if err := fsw.Add(dir); err != nil {
		fsw.Close()
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	w := &EventWatcher{
		path:      path,
		sessionID: sessionID,
		handlers:  handlers,
		ctx:       ctx,
		cancel:    cancel,
		fsw:       fsw,
	}

	// Set initial offset to current file size (skip existing content)
	if info, err := os.Stat(path); err == nil {
		w.offset = info.Size()
	}

	go w.run()
	return w, nil
}

func (w *EventWatcher) run() {
	fileName := filepath.Base(w.path)
	var debounce *time.Timer

	for {
		select {
		case <-w.ctx.Done():
			if debounce != nil {
				debounce.Stop()
			}
			return
		case event, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if filepath.Base(event.Name) != fileName {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(100*time.Millisecond, func() {
				w.processNewLines()
			})
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}

func (w *EventWatcher) processNewLines() {
	w.mu.Lock()
	defer w.mu.Unlock()

	f, err := os.Open(w.path)
	if err != nil {
		return
	}
	defer f.Close()

	if _, err := f.Seek(w.offset, io.SeekStart); err != nil {
		return
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		raw, err := ParseRawEvent(line)
		if err != nil {
			continue
		}
		handlers, ok := w.handlers[raw.Type]
		if !ok || len(handlers) == 0 {
			continue
		}
		dataCopy := make([]byte, len(line))
		copy(dataCopy, line)
		for _, h := range handlers {
			h.HandleEvent(w.ctx, w.sessionID, raw, dataCopy)
		}
	}

	pos, err := f.Seek(0, io.SeekCurrent)
	if err == nil {
		w.offset = pos
	}
}

// Stop shuts down the watcher.
func (w *EventWatcher) Stop() {
	w.cancel()
	w.fsw.Close()
}

// ReadCurrentStatus reads the event file and returns the latest status event.
// Used for daemon restart recovery. Does not fire handlers.
func ReadCurrentStatus(path string) (*StatusEvent, error) {
	raw, data, err := ReadLastByType(path, "status")
	if err != nil {
		return nil, err
	}
	_ = raw
	var status StatusEvent
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, err
	}
	return &status, nil
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/events/...
```

Expected: PASS

**Step 5: Commit**

Use `/commit` with message: "feat(events): add local EventWatcher with fsnotify and typed handler dispatch"

---

### Task 7: RemoteEventWatcher

Create the remote event watcher with `tail -f` script and JSON-based ProcessOutput.

**Files:**

- Create: `internal/events/remotewatcher.go`
- Create: `internal/events/remotewatcher_test.go`

**Step 1: Write the failing tests**

```go
// internal/events/remotewatcher_test.go
package events

import (
	"context"
	"sync"
	"testing"
)

func TestRemoteWatcherScript(t *testing.T) {
	script := RemoteWatcherScript("/workspace/.schmux/events/test.jsonl")
	if script == "" {
		t.Fatal("empty script")
	}
	// Must contain tail -f
	if !containsStr(script, "tail -f") {
		t.Error("script should use tail -f")
	}
	// Must contain sentinel markers
	if !containsStr(script, "__SCHMUX_SIGNAL__") {
		t.Error("script should use sentinel markers")
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

type remoteTestHandler struct {
	mu     sync.Mutex
	events []RawEvent
}

func (h *remoteTestHandler) HandleEvent(ctx context.Context, sessionID string, raw RawEvent, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, raw)
}

func (h *remoteTestHandler) getEvents() []RawEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	cp := make([]RawEvent, len(h.events))
	copy(cp, h.events)
	return cp
}

func TestRemoteEventWatcherProcessOutput(t *testing.T) {
	handler := &remoteTestHandler{}
	w := NewRemoteEventWatcher("test-session", map[string][]EventHandler{
		"status": {handler},
	})

	w.ProcessOutput(`__SCHMUX_SIGNAL__{"ts":"2026-02-18T14:30:00Z","type":"status","state":"working","message":"test"}__END__`)

	events := handler.getEvents()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Type != "status" {
		t.Errorf("type = %v, want status", events[0].Type)
	}
}

func TestRemoteEventWatcherDedup(t *testing.T) {
	handler := &remoteTestHandler{}
	w := NewRemoteEventWatcher("test-session", map[string][]EventHandler{
		"status": {handler},
	})

	line := `__SCHMUX_SIGNAL__{"ts":"2026-02-18T14:30:00Z","type":"status","state":"working","message":"test"}__END__`
	w.ProcessOutput(line)
	w.ProcessOutput(line) // duplicate

	events := handler.getEvents()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (dedup)", len(events))
	}
}

func TestRemoteEventWatcherNoSentinel(t *testing.T) {
	handler := &remoteTestHandler{}
	w := NewRemoteEventWatcher("test-session", map[string][]EventHandler{
		"status": {handler},
	})

	w.ProcessOutput("some random output without sentinels")

	events := handler.getEvents()
	if len(events) != 0 {
		t.Fatalf("got %d events, want 0", len(events))
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/events/...
```

Expected: FAIL — `RemoteWatcherScript`, `NewRemoteEventWatcher` not defined.

**Step 3: Write minimal implementation**

```go
// internal/events/remotewatcher.go
package events

import (
	"context"
	"strings"
	"sync"
)

const (
	sentinelStart = "__SCHMUX_SIGNAL__"
	sentinelEnd   = "__END__"
)

// RemoteWatcherScript generates the shell script to run on the remote host.
// It streams new JSONL lines from the event file, wrapped in sentinel markers.
func RemoteWatcherScript(eventsFilePath string) string {
	// Use shellutil.Quote equivalent — for now, single-quote the path
	return `EVENTS_FILE='` + eventsFilePath + `'; ` +
		`if [ -f "$EVENTS_FILE" ]; then ` +
		`while IFS= read -r line; do echo "` + sentinelStart + `${line}` + sentinelEnd + `"; done < "$EVENTS_FILE"; ` +
		`fi; ` +
		`touch "$EVENTS_FILE"; ` +
		`tail -f -n 0 "$EVENTS_FILE" 2>/dev/null | while IFS= read -r line; do ` +
		`echo "` + sentinelStart + `${line}` + sentinelEnd + `"; ` +
		`done`
}

// RemoteEventWatcher processes sentinel-wrapped event output from a remote host.
type RemoteEventWatcher struct {
	sessionID string
	handlers  map[string][]EventHandler
	mu        sync.Mutex
	lastTs    string // for deduplication
}

// NewRemoteEventWatcher creates a remote event watcher.
func NewRemoteEventWatcher(sessionID string, handlers map[string][]EventHandler) *RemoteEventWatcher {
	return &RemoteEventWatcher{
		sessionID: sessionID,
		handlers:  handlers,
	}
}

// ProcessOutput extracts sentinel-wrapped events from control mode output.
func (w *RemoteEventWatcher) ProcessOutput(data string) {
	content := ParseSentinelContent(data)
	if content == "" {
		return
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}

	raw, err := ParseRawEvent([]byte(content))
	if err != nil {
		return
	}

	// Dedup by timestamp
	w.mu.Lock()
	if raw.Ts != "" && raw.Ts == w.lastTs {
		w.mu.Unlock()
		return
	}
	if raw.Ts != "" {
		w.lastTs = raw.Ts
	}
	w.mu.Unlock()

	handlers, ok := w.handlers[raw.Type]
	if !ok || len(handlers) == 0 {
		return
	}

	dataCopy := []byte(content)
	for _, h := range handlers {
		h.HandleEvent(context.Background(), w.sessionID, raw, dataCopy)
	}
}

// ParseSentinelContent extracts content between sentinel markers.
func ParseSentinelContent(data string) string {
	start := strings.Index(data, sentinelStart)
	if start == -1 {
		return ""
	}
	start += len(sentinelStart)
	end := strings.LastIndex(data, sentinelEnd)
	if end == -1 || end <= start {
		return ""
	}
	return data[start:end]
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/events/...
```

Expected: PASS

**Step 5: Commit**

Use `/commit` with message: "feat(events): add RemoteEventWatcher with tail-f script and sentinel parsing"

---

### Task 8: Dashboard Event Handler

Create a handler that bridges event dispatch to the existing `HandleAgentSignal` logic.

**Files:**

- Create: `internal/events/dashboardhandler.go`
- Create: `internal/events/dashboardhandler_test.go`

**Step 1: Write the failing tests**

Test that the handler converts a status event into the right callback invocation. Use a mock callback.

```go
// internal/events/dashboardhandler_test.go
package events

import (
	"context"
	"sync"
	"testing"
)

func TestDashboardHandlerStatusEvent(t *testing.T) {
	var mu sync.Mutex
	var capturedID string
	var capturedState string
	var capturedMessage string

	handler := NewDashboardHandler(func(sessionID, state, message, intent, blockers string) {
		mu.Lock()
		defer mu.Unlock()
		capturedID = sessionID
		capturedState = state
		capturedMessage = message
	})

	data := []byte(`{"ts":"2026-02-18T14:30:00Z","type":"status","state":"completed","message":"done"}`)
	raw := RawEvent{Ts: "2026-02-18T14:30:00Z", Type: "status"}

	handler.HandleEvent(context.Background(), "session-1", raw, data)

	mu.Lock()
	defer mu.Unlock()
	if capturedID != "session-1" {
		t.Errorf("sessionID = %v, want session-1", capturedID)
	}
	if capturedState != "completed" {
		t.Errorf("state = %v, want completed", capturedState)
	}
	if capturedMessage != "done" {
		t.Errorf("message = %v, want done", capturedMessage)
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/events/...
```

Expected: FAIL — `NewDashboardHandler` not defined.

**Step 3: Write minimal implementation**

```go
// internal/events/dashboardhandler.go
package events

import (
	"context"
	"encoding/json"
)

// StatusCallback is called when a status event is received.
type StatusCallback func(sessionID, state, message, intent, blockers string)

// DashboardHandler dispatches status events to the dashboard.
type DashboardHandler struct {
	callback StatusCallback
}

// NewDashboardHandler creates a handler that forwards status events.
func NewDashboardHandler(callback StatusCallback) *DashboardHandler {
	return &DashboardHandler{callback: callback}
}

func (h *DashboardHandler) HandleEvent(ctx context.Context, sessionID string, raw RawEvent, data []byte) {
	if raw.Type != "status" {
		return
	}
	var status StatusEvent
	if err := json.Unmarshal(data, &status); err != nil {
		return
	}
	h.callback(sessionID, status.State, status.Message, status.Intent, status.Blockers)
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/events/...
```

Expected: PASS

**Step 5: Commit**

Use `/commit` with message: "feat(events): add DashboardHandler bridging status events to dashboard callback"

---

### Task 9: Wire Event System Into Session Tracker + Daemon

Replace the signal callback wiring with event handler registration. Update session tracker to create EventWatcher. Update daemon to register handlers.

**Files:**

- Modify: `internal/session/tracker.go:46-77` (struct), `89-110` (NewSessionTracker)
- Modify: `internal/session/manager.go:104-106` (SetSignalCallback → SetEventHandlers), `1323-1381` (ensureTrackerFromSession), `155-327` (StartRemoteSignalMonitor)
- Modify: `internal/daemon/daemon.go:527-529` (wiring)
- Modify: `internal/dashboard/websocket.go:710-743` (HandleAgentSignal — adapt to accept event fields)

**Step 1: Add `SetEventHandlers` to session manager**

At `manager.go:104-106`, alongside the existing `SetSignalCallback`, add:

```go
func (m *Manager) SetEventHandlers(handlers map[string][]events.EventHandler) {
	m.eventHandlers = handlers
}
```

Add `eventHandlers map[string][]events.EventHandler` field to the Manager struct. Keep `SetSignalCallback` during migration.

**Step 2: Update `SessionTracker` to hold an `EventWatcher`**

At `tracker.go:46-77`, add `eventWatcher *events.EventWatcher` field. In `NewSessionTracker`, accept `eventFilePath string` and `eventHandlers map[string][]events.EventHandler`. If `eventFilePath` is non-empty, create an `EventWatcher`.

**Step 3: Update `ensureTrackerFromSession` to pass event file path and handlers**

At `manager.go:1323-1381`, construct `eventFilePath` from `workspace.Path + ".schmux/events/" + sessionID + ".jsonl"`. Pass `eventHandlers` to `NewSessionTracker`. After creation, call `ReadCurrentStatus(eventFilePath)` for recovery (same pattern as the existing `fw.ReadCurrent()` call).

**Step 4: Update `StartRemoteSignalMonitor` to use `RemoteEventWatcher`**

At `manager.go:155-327`, alongside the existing signal watcher, create a `RemoteEventWatcher` with the event handlers. Update `WatcherScript()` call to use `events.RemoteWatcherScript()`. Update `ProcessOutput` calls to use the new watcher.

Keep the existing signal watcher running in parallel during dual-write phase.

**Step 5: Update daemon wiring**

At `daemon.go:527-529`, create the dashboard handler and register it:

```go
dashHandler := events.NewDashboardHandler(func(sessionID, state, message, intent, blockers string) {
	server.HandleAgentSignal(sessionID, state, message, intent, blockers)
})

sm.SetEventHandlers(map[string][]events.EventHandler{
	"status": {dashHandler},
})

// Keep old signal callback during migration
sm.SetSignalCallback(func(sessionID string, sig schmuxsignal.Signal) {
	server.HandleAgentSignal(sessionID, sig)
})
```

**Step 6: Adapt `HandleAgentSignal` to accept event fields**

At `websocket.go:710-743`, the function currently takes a `signal.Signal`. Add an overload or refactor to accept the individual fields (state, message, intent, blockers) so it can be called from both the old signal callback and the new event handler. Or create `HandleStatusEvent(sessionID, state, message, intent, blockers string)` and have both paths call it.

**Step 7: Run tests**

```bash
go test ./internal/session/... ./internal/daemon/... ./internal/dashboard/... ./internal/events/...
```

Expected: PASS

**Step 8: Commit**

Use `/commit` with message: "feat(events): wire event watcher and dashboard handler into session tracker and daemon"

---

## Phase 3: Remove Signal File

### Task 10: Remove Signal File System

Remove `SCHMUX_STATUS_FILE`, signal directory creation, dual-write from hooks, and delete `internal/signal/` package.

**Files:**

- Modify: `internal/session/manager.go` — remove `SCHMUX_STATUS_FILE` from env maps in `Spawn()` (:657-662), `SpawnCommand()` (:766-771), `SpawnRemote()` (:415-420). Remove `.schmux/signal` mkdir in `Spawn()` (:640-643), `SpawnCommand()` (:760-763), `SpawnRemote()` (:508-511, :528-532). Remove `SetSignalCallback` (:104-106) and all references to `signalCallback`.
- Modify: `internal/session/tracker.go` — remove `fileWatcher` field, `signal.NewFileWatcher` call, signal-related imports.
- Modify: `internal/workspace/ensure/manager.go` — remove `signalCommand()` (:378-380), `signalCommandWithContext()` (:385-390). Remove dual-write from `buildClaudeHooksMap` (only event writes remain). Remove `.schmux/signal/` from `excludePatterns` (:617-621). Remove `LoreHookScripts()` (:722-738) and the old `stop-gate.sh` embed.
- Modify: `internal/daemon/daemon.go` — remove `sm.SetSignalCallback(...)` (:527-529). Remove `signal` import.
- Modify: `internal/dashboard/websocket.go` — remove old `HandleAgentSignal(sessionID, signal.Signal)` overload if still present.
- Delete: `internal/signal/signal.go`, `internal/signal/filewatcher.go`, `internal/signal/remotewatcher.go`, and all test files in `internal/signal/`.

**Step 1: Remove `SCHMUX_STATUS_FILE` from env maps**

In `Spawn()`, `SpawnCommand()`, and `SpawnRemote()`, remove the `"SCHMUX_STATUS_FILE"` line from the env map.

**Step 2: Remove `.schmux/signal` directory creation**

Remove the `os.MkdirAll(schmuxDir, 0755)` blocks for `.schmux/signal/` and the `mkdir -p .schmux/signal` remote commands.

**Step 3: Remove dual-write from hooks**

In `buildClaudeHooksMap()`, remove the `signalCommand()` calls. Each hook should only write to `$SCHMUX_EVENTS_FILE`. Remove `signalCommand()` and `signalCommandWithContext()` helper functions.

**Step 4: Remove `SetSignalCallback` and `signalCallback` from session manager**

Remove the field, the setter, and all call sites in `ensureTrackerFromSession`, `StartRemoteSignalMonitor`, etc.

**Step 5: Remove `fileWatcher` from SessionTracker**

Remove the `signal.FileWatcher` field and its creation in `NewSessionTracker`. Remove the signal file path parameter.

**Step 6: Remove `LoreHookScripts()` and old `stop-gate.sh` embed**

The old per-workspace hook deployment is no longer needed. `EnsureGlobalHookScripts()` is the sole mechanism.

**Step 7: Remove `.schmux/signal/` from git exclude patterns**

**Step 8: Delete `internal/signal/` package**

Remove all files: `signal.go`, `filewatcher.go`, `remotewatcher.go`, and their tests. Remove the `signal` import from all consuming packages.

**Step 9: Update `SignalingInstructions` to remove `$SCHMUX_STATUS_FILE` references**

Only `$SCHMUX_EVENTS_FILE` instructions remain.

**Step 10: Run full test suite**

```bash
./test.sh --quick
```

Expected: PASS. Any test importing `internal/signal` will need updating — search for `"internal/signal"` imports.

**Step 11: Commit**

Use `/commit` with message: "feat(events): remove signal file system — event file is sole source of truth"

---

## Phase 4: Remove `.schmux/lore.jsonl`

### Task 11: Migrate Lore Reader to Event Files

Update the lore system to read failure/reflection/friction events from per-session event files instead of the per-workspace `lore.jsonl`.

**Files:**

- Modify: `internal/lore/scratchpad.go:102-136` (ReadEntries), `489-522` (ReadEntriesMulti), `165-169` (AppendEntry)
- Modify: `internal/dashboard/handlers_lore.go:79-86` (getLoreReadPaths)
- Modify: `internal/workspace/ensure/manager.go:617-621` (excludePatterns — remove `.schmux/lore.jsonl`)

**Step 1: Create `ReadEntriesFromEvents` function**

Add a new function to `scratchpad.go` that reads lore entries from event files:

```go
// ReadEntriesFromEvents reads lore-relevant events from per-session event files.
// It scans <workspace>/.schmux/events/*.jsonl for failure, reflection, friction events.
func ReadEntriesFromEvents(workspacePath, workspaceID string, filter func(Entry) bool) ([]Entry, error) {
	pattern := filepath.Join(workspacePath, ".schmux", "events", "*.jsonl")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	loreTypes := map[string]bool{"failure": true, "reflection": true, "friction": true}
	var entries []Entry

	for _, f := range files {
		sessionID := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		eventLines, err := events.ReadEvents(f, func(raw events.RawEvent) bool {
			return loreTypes[raw.Type]
		})
		if err != nil {
			continue
		}
		for _, el := range eventLines {
			entry := eventLineToEntry(el, sessionID, workspaceID)
			if filter != nil && !filter(entry) {
				continue
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}
```

The `eventLineToEntry` helper maps event fields to `lore.Entry` per the mapping table in the design doc.

**Step 2: Update `getLoreReadPaths` to return event directories**

At `handlers_lore.go:79-86`, change from collecting `<workspace>/.schmux/lore.jsonl` paths to collecting `<workspace>/.schmux/events/` directories. Or refactor to pass workspace paths directly and let the reader glob event files.

**Step 3: Update `ReadEntriesMulti` callers**

Replace calls to `ReadEntriesMulti(paths, filter)` with calls to `ReadEntriesFromEvents(...)` for each workspace, then merge results. Keep the central state file (`~/.schmux/lore/<repo>/state.jsonl`) as a separate read — state-change records don't move.

**Step 4: Remove `AppendEntry` if no Go callers remain**

Check for Go code that calls `AppendEntry`. If all lore writing is now done by shell scripts (hooks) writing to event files, remove the function. If any remain, redirect them to `events.AppendEvent`.

**Step 5: Remove `.schmux/lore.jsonl` from git exclude patterns**

At `ensure/manager.go:617-621`, remove the `.schmux/lore.jsonl` entry.

**Step 6: Run full test suite**

```bash
./test.sh --quick
```

Expected: PASS. Lore tests will need updates — any test that writes to `lore.jsonl` needs to write event files instead.

**Step 7: Commit**

Use `/commit` with message: "feat(events): migrate lore reader to event files, remove lore.jsonl"

---

## Summary

| Task | Phase | Description                                                        |
| ---- | ----- | ------------------------------------------------------------------ |
| 1    | 1     | Event types package (status, failure, reflection, friction)        |
| 2    | 1     | Event file I/O (writer + reader)                                   |
| 3    | 1     | Event file provisioning (directory, env var, git exclude)          |
| 4    | 1     | Centralize hook scripts (split stop-gate, rewrite capture-failure) |
| 5    | 1     | Dual-write hooks + updated SignalingInstructions                   |
| 6    | 2     | Local EventWatcher with fsnotify + handler dispatch                |
| 7    | 2     | RemoteEventWatcher with tail-f + sentinel parsing                  |
| 8    | 2     | DashboardHandler bridging events to nudge system                   |
| 9    | 2     | Wire event system into session tracker + daemon                    |
| 10   | 3     | Remove signal file system entirely                                 |
| 11   | 4     | Migrate lore reader to event files, remove lore.jsonl              |

Each task ends with a commit. Tasks 1-5 are safe to land independently (additive, no behavior change for existing consumers). Tasks 6-9 switch consumers to the new system. Task 10 deletes the old signal system. Task 11 deletes the old lore file.
