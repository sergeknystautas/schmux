# Incremental Chat History Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop resending and rereducing an entire chat history on reconnect by resuming from a durable delivery sequence and a cached normalized conversation.

**Architecture:** `conversation.jsonl` remains the append-only source of truth and never stores sequence numbers. Go assigns wire-only sequences while reading and after successful appends; `/ws/chat/{sessionId}?since=N` returns only durable records after `N` plus current queue overlays. The browser caches a normalized durable `Conversation` in module memory, reconnects from its last sequence, and keeps live-only state out of that durable snapshot.

**Tech Stack:** Go, Gorilla WebSocket, React 18, TypeScript, Vitest, React Testing Library.

**Spec:** `docs/superpowers/specs/2026-09-25-incremental-chat-history-design.md`

## Global Constraints

- Run every command from `/Users/sergek/dev/schmux-005`.
- Full verification is exactly `./test.sh`; do not substitute `./test.sh --quick`.
- Frontend iteration uses `./test.sh --quick`; never run `npx vitest` from `assets/dashboard/`.
- Dashboard builds use `go run ./cmd/build-dashboard`; never run npm or Vite directly.
- Sequence numbers are wire metadata only and must not appear in persisted `conversation.jsonl` lines.
- Do not add IndexedDB, localStorage, a sidecar offset index, generic pagination, or a Go transcript reducer.
- API docs must change in the same implementation because `/ws/chat/{sessionId}` changes.
- The user owns git. At each **User commit gate**, stop and ask; never run `git commit` unless explicitly authorized separately.

## Review Focus

- **Future `since`:** a value greater than `last_seq` must return full history with `reset: true`, never an empty delta. Tested in Tasks 1 and 2.
- **Invalid `since`:** malformed numeric input gets the same full-reset behavior as a future value. Tested in Task 2.
- **Filtered Codex records:** records omitted by compaction still consume sequence positions and advance `last_seq`. Tested in Task 2.
- **Held queue overlays:** overlays ride with history but have `seq: 0`, never advance `last_seq`, and never enter the durable cache. Tested in Tasks 1 and 4.
- **Live-only stream state:** rendered state may advance mid-turn, but reconnect starts from the durable cache and drops stale stream-only state. Tested in Task 4.
- **Old daemon rolling reload:** a history frame without `last_seq` must take the cold full-history path. Tested in Task 4.
- **Image preview substitution:** `chatRecordForBrowser` must preserve the durable record sequence. Tested in Task 2.

---

### Task 1: Runtime delivery sequences

**Files:**

- Modify: `internal/chat/record.go`
- Modify: `internal/chat/runtime.go`
- Create: `internal/chat/runtime_sequence_test.go`

**Interfaces:**

- Consumes: existing `Record`, `Log.ReadAll`, `Runtime.appendLocked`, `Runtime.mu`, and `Runtime.held`.
- Produces:

```go
type Subscription struct {
	History []Record
	Live    <-chan Record
	LastSeq uint64
	Reset   bool
}

func (r *Runtime) Subscribe(after uint64) (Subscription, error)
```

`Record` gains:

```go
Seq uint64 `json:"seq,omitempty"`
```

`SequenceRecords(records []Record) uint64` assigns `1..N`.

- [ ] **Step 1: Write failing sequence tests**

Add `internal/chat/runtime_sequence_test.go`:

```go
package chat

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSequenceRecordsAssignsOneThroughN(t *testing.T) {
	records := []Record{
		NewUserMessage("one", nil),
		NewHarness([]byte(`{"type":"system","subtype":"init"}`)),
		NewControl([]byte(`{"type":"control_request"}`)),
	}
	if got := SequenceRecords(records); got != 3 {
		t.Fatalf("last sequence = %d, want 3", got)
	}
	for i, rec := range records {
		if rec.Seq != uint64(i+1) {
			t.Fatalf("record %d sequence = %d", i, rec.Seq)
		}
	}
}

func TestRuntimeSubscribeReturnsDurableSuffix(t *testing.T) {
	rt, _ := newTestRuntime(t)
	l, err := OpenLog(rt.paths.Conversation)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range []Record{
		NewUserMessage("one", nil),
		NewHarness([]byte(`{"type":"system","subtype":"init"}`)),
		NewHarness([]byte(`{"type":"assistant"}`)),
	} {
		if err := l.Append(rec); err != nil {
			t.Fatal(err)
		}
	}

	sub, err := rt.Subscribe(1)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Unsubscribe(sub.Live)
	if sub.LastSeq != 3 || sub.Reset || len(sub.History) != 2 {
		t.Fatalf("suffix = %+v", sub)
	}
	if sub.History[0].Seq != 2 || sub.History[1].Seq != 3 {
		t.Fatalf("suffix sequences = %d, %d", sub.History[0].Seq, sub.History[1].Seq)
	}
}

func TestRuntimeSubscribeIncludesHeldOverlayWithoutSequence(t *testing.T) {
	rt, _ := newTestRuntime(t)
	l, _ := OpenLog(rt.paths.Conversation)
	l.Append(NewUserMessage("held", nil))

	rt.mu.Lock()
	rt.held = append(rt.held, Record{ID: "held-id"})
	rt.mu.Unlock()

	sub, err := rt.Subscribe(1)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Unsubscribe(sub.Live)
	if len(sub.History) != 1 || sub.History[0].Type != RecordUserMessageQueue || sub.History[0].Seq != 0 {
		t.Fatalf("overlay history = %+v", sub.History)
	}
	if sub.LastSeq != 1 {
		t.Fatalf("overlay advanced LastSeq: %+v", sub)
	}
}

func TestRuntimeSubscribeFutureAfterResets(t *testing.T) {
	rt, _ := newTestRuntime(t)
	l, _ := OpenLog(rt.paths.Conversation)
	l.Append(NewUserMessage("one", nil))
	l.Append(NewHarness([]byte(`{"type":"assistant"}`)))

	sub, err := rt.Subscribe(3)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Unsubscribe(sub.Live)
	if !sub.Reset || sub.LastSeq != 2 || len(sub.History) != 2 {
		t.Fatalf("future resume = %+v", sub)
	}
}

func TestRuntimeAppendSequencesFanoutButNotLog(t *testing.T) {
	rt, p := newTestRuntime(t)
	l, _ := OpenLog(p.Conversation)
	user := NewUserMessage("one", nil)
	l.Append(user)
	l.Append(NewUserMessageDispatch(user.ID))
	l.Append(NewHarness([]byte(`{"type":"system","subtype":"init"}`)))
	rt.Start()

	sub, err := rt.Subscribe(3)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Unsubscribe(sub.Live)

	next := NewHarness([]byte(`{"type":"assistant"}`))
	rt.mu.Lock()
	err = rt.appendLocked(next)
	rt.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if next.Seq != 4 {
		t.Fatalf("fanout sequence = %d, want 4", next.Seq)
	}
	if got := recv(t, sub.Live); got.Seq != 4 {
		t.Fatalf("live sequence = %d, want 4", got.Seq)
	}

	persisted, err := l.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(persisted[len(persisted)-1])
	if strings.Contains(string(raw), `"seq"`) {
		t.Fatalf("sequence persisted to log: %s", raw)
	}
}
```

- [ ] **Step 2: Run the new tests and verify they fail**

Run: `go test ./internal/chat -run 'TestSequenceRecords|TestRuntimeSubscribe|TestRuntimeAppendSequences' -count=1`

Expected: compile failure on `SequenceRecords`, `Record.Seq`, or `Subscribe(after)`.

- [ ] **Step 3: Implement the runtime sequence boundary**

In `record.go`:

```go
Seq uint64 `json:"seq,omitempty"`
```

```go
func SequenceRecords(records []Record) uint64 {
	for i := range records {
		records[i].Seq = uint64(i + 1)
	}
	return uint64(len(records))
}
```

In `runtime.go`:

```go
nextSeq uint64
```

In `Start`, immediately after `recs, err := r.log.ReadAll()` succeeds:

```go
lastSeq := SequenceRecords(recs)
```

and under the existing mutex before any possible append:

```go
r.nextSeq = lastSeq
```

Replace `appendLocked` with:

```go
func (r *Runtime) appendLocked(rec Record) error {
	if err := r.log.Append(rec); err != nil {
		return err
	}
	r.nextSeq++
	rec.Seq = r.nextSeq
	r.fanOutLocked(rec)
	return nil
}
```

Replace `Subscribe` with a method that locks `r.mu`, reads and sequences durable records, sets `Reset` for `after > lastSeq`, slices `Seq > after`, appends `NewUserMessageQueue(rec.ID, true)` overlays for `r.held`, registers the live channel, and returns the snapshot.

Update every existing `Subscribe()` call site repository-wide to `Subscribe(0)`.

- [ ] **Step 4: Run the focused runtime tests**

Run: `go test ./internal/chat -run 'TestSequenceRecords|TestRuntimeSubscribe|TestRuntimeAppendSequences' -count=1`

Expected: PASS.

- [ ] **Step 5: Run the package gate**

Run: `go test ./internal/chat`

Expected: PASS.

- [ ] **Step 6: User commit gate**

Ask the user to review and commit. Suggested message: `feat(chat): add durable delivery sequences`

---

### Task 2: Chat WebSocket delta frames

**Files:**

- Modify: `internal/dashboard/websocket_chat.go`
- Modify: `internal/dashboard/websocket_chat_test.go`

**Interfaces:**

- Consumes: `chat.Subscription`, `chat.Record.Seq`, `rt.Subscribe(after)`, `chat.SequenceRecords`, `compactCodexHistory`, and `chatRecordForBrowser`.
- Produces history frame fields:

```go
Since   *uint64 `json:"since,omitempty"`
LastSeq uint64  `json:"last_seq"`
Reset   bool    `json:"reset"`
```

and telemetry fields `since`, `last_seq`, `reset`, and `durable_records`.

- [ ] **Step 1: Write failing stopped-session delta tests**

Add to `internal/dashboard/websocket_chat_test.go`:

```go
func TestChatWebSocket_SinceReturnsStoppedSessionSuffix(t *testing.T) {
	srv, _, st := newTestServer(t)
	workspace := t.TempDir()
	st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "r", Branch: "b", Path: workspace})
	st.AddSession(state.Session{ID: "delta", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, Pid: 999999999, CreatedAt: time.Now()})
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	paths := chat.PathsFor(schmuxdir.ChatSessionDir("ws-1", "delta"))
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	log, err := chat.OpenLog(paths.Conversation)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"one", "two", "three"} {
		if err := log.Append(chat.NewUserMessage(text, nil)); err != nil {
			t.Fatal(err)
		}
	}

	ts := httptest.NewServer(chatTestRouter(srv))
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/chat/delta?since=2"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var frame struct {
		Type     string        `json:"type"`
		Since    uint64        `json:"since"`
		LastSeq  uint64        `json:"last_seq"`
		Reset    bool          `json:"reset"`
		Records  []chat.Record `json:"records"`
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatal(err)
	}
	if frame.Type != "history" || frame.Since != 2 || frame.LastSeq != 3 || frame.Reset ||
		len(frame.Records) != 1 || frame.Records[0].Text != "three" || frame.Records[0].Seq != 3 {
		t.Fatalf("delta frame = %+v", frame)
	}

	events := readChatPerformanceEvents(t)
	event := events[len(events)-1]
	if event.Kind != "history" || event.Data["since"] != float64(2) ||
		event.Data["last_seq"] != float64(3) || event.Data["reset"] != false ||
		event.Data["durable_records"] != float64(1) {
		t.Fatalf("delta telemetry = %+v", event)
	}
}

func TestChatWebSocket_InvalidOrFutureSinceResets(t *testing.T) {
	srv, _, st := newTestServer(t)
	workspace := t.TempDir()
	st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "r", Branch: "b", Path: workspace})
	st.AddSession(state.Session{ID: "reset", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, Pid: 999999999, CreatedAt: time.Now()})
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	paths := chat.PathsFor(schmuxdir.ChatSessionDir("ws-1", "reset"))
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	log, _ := chat.OpenLog(paths.Conversation)
	log.Append(chat.NewUserMessage("one", nil))
	log.Append(chat.NewUserMessage("two", nil))

	ts := httptest.NewServer(chatTestRouter(srv))
	defer ts.Close()
	for _, query := range []string{"since=99", "since=nope"} {
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/ws/chat/reset?"+query, nil)
		if err != nil {
			t.Fatal(err)
		}

		var frame struct {
			Type    string        `json:"type"`
			LastSeq uint64        `json:"last_seq"`
			Reset   bool          `json:"reset"`
			Records []chat.Record `json:"records"`
		}
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if err := conn.ReadJSON(&frame); err != nil {
			conn.Close()
			t.Fatal(err)
		}
		conn.Close()
		if !frame.Reset || frame.LastSeq != 2 || len(frame.Records) != 2 {
			t.Fatalf("%s reset frame = %+v", query, frame)
		}
	}
}
```

Add a Codex compaction assertion to the existing Codex history test:

```go
	if frame.LastSeq != 4 || len(frame.Records) != 2 {
		t.Fatalf("compaction must preserve raw last sequence: frame=%+v", frame)
	}
```

Extend the frame structs in the existing image-preview test with `LastSeq uint64` and assert that a preview-bearing user record retains its original sequence.

Use this exact assertion:

```go
if frame.LastSeq != 1 || frame.Records[0].Seq != 1 {
	t.Fatalf("preview replacement changed sequence: %+v", frame)
}
```

- [ ] **Step 2: Run the dashboard tests and verify they fail**

Run: `go test ./internal/dashboard -run 'TestChatWebSocket_.*Since|TestChatWebSocket_CodexHistoryOmitsUserMessageEchoes' -count=1`

Expected: FAIL because the daemon currently ignores `since` and omits `last_seq`.

- [ ] **Step 3: Implement parsing, slicing, and frame metadata**

In `handleChatWebSocket`:

```go
var after uint64
requestedDelta := false
invalidSince := false
if rawSince := r.URL.Query().Get("since"); rawSince != "" {
	value, err := strconv.ParseUint(rawSince, 10, 64)
	if err != nil {
		after = 0
		invalidSince = true
	} else {
		after = value
		requestedDelta = true
	}
}
```

Track:

```go
var lastSeq uint64
var reset bool
var durableRecords int
```

For running sessions use `rt.Subscribe(after)`. For stopped sessions, after `ReadAll`, call an unexported helper that sequences records, sets `lastSeq`, detects `after > lastSeq`, and returns the requested durable suffix.
Use this exact helper:

```go
func stoppedChatHistory(
	history []chat.Record,
	after uint64,
	requestedDelta bool,
) (sent []chat.Record, lastSeq uint64, reset bool)
```

Pass `requestedDelta || invalidSince` to it so an invalid value produces a full frame with `reset: true`.

Copy the source profile before compaction, compact only the sent slice, and marshal:

```go
"since": sinceValue,
"last_seq": lastSeq,
"reset": reset,
```

`sinceValue` is `*uint64` so a cold load omits the field.

- [ ] **Step 4: Run focused dashboard tests**

Run: `go test ./internal/dashboard -run 'TestChatWebSocket_.*Since|TestChatWebSocket_CodexHistoryOmitsUserMessageEchoes' -count=1`

Expected: PASS.

- [ ] **Step 5: Run the dashboard package gate**

Run: `go test ./internal/dashboard`

Expected: PASS.

- [ ] **Step 6: User commit gate**

Ask the user to review and commit. Suggested message: `feat(chat): send incremental history frames`

---

### Task 3: Browser socket resume metadata

**Files:**

- Modify: `assets/dashboard/src/lib/chat/types.ts`
- Modify: `assets/dashboard/src/lib/chat/socket.ts`
- Modify: `assets/dashboard/src/lib/chat/socket.test.ts`

**Interfaces:**

- Consumes: existing `ChatSocket`, `ConversationRecord`, and transport.
- Produces:

```ts
export interface ChatHistoryMetadata {
  since?: number;
  lastSeq?: number;
  reset?: boolean;
}

export interface ChatSocketHandlers {
  onHistory(
    protocol: ChatProtocol,
    records: ConversationRecord[],
    timing: ChatHistoryTiming,
    metadata: ChatHistoryMetadata
  ): void;
  onRecord(record: ConversationRecord): void;
  onStatus(status: ChatSocketStatus): void;
  onError?(message: string): void;
}
```

`ChatSocket` gains an optional third constructor argument:

```ts
getSince?: () => number | undefined
```

- [ ] **Step 1: Write failing socket tests**

Add to `socket.test.ts`:

Import `ChatHistoryMetadata` from `./socket` alongside the existing `ChatSocketStatus` type import.

```ts
it('appends the latest since value on every connection', () => {
  let since: number | undefined;
  const s = new ChatSocket(
    's1',
    {
      onHistory: () => {},
      onRecord: () => {},
      onStatus: () => {},
    },
    () => since
  );
  since = 7;
  s.connect();
  expect(lastWS().url).toContain('/ws/chat/s1?since=7');
  lastWS().onclose?.({ code: 1006 });
  since = 12;
  vi.advanceTimersByTime(500);
  expect(lastWS().url).toContain('/ws/chat/s1?since=12');
  s.close();
});

it('omits since before a cache entry exists', () => {
  const s = new ChatSocket(
    's1',
    {
      onHistory: () => {},
      onRecord: () => {},
      onStatus: () => {},
    },
    () => undefined
  );
  s.connect();
  expect(lastWS().url).not.toContain('since=');
  s.close();
});

it('passes history metadata to onHistory', () => {
  const seen: ChatHistoryMetadata[] = [];
  const s = new ChatSocket('s1', {
    onHistory: (_p, _r, _t, metadata) => seen.push(metadata),
    onRecord: () => {},
    onStatus: () => {},
  });
  s.connect();
  msg(lastWS(), {
    type: 'history',
    protocol: 'claude-stream-json',
    since: 2,
    last_seq: 3,
    reset: false,
    records,
  });
  expect(seen).toEqual([{ since: 2, lastSeq: 3, reset: false }]);
  s.close();
});
```

- [ ] **Step 2: Run socket tests and verify they fail**

Run: `./test.sh --quick`

Expected: FAIL on the new socket tests or TypeScript compile because `ChatSocket` has no `getSince` and no metadata callback argument.

- [ ] **Step 3: Implement socket resume**

Add `seq?: number` to the base `ConversationRecord` shape, add `ChatHistoryMetadata`, and extend the handler signature.

Build the URL in `open()`:

```ts
const since = this.getSince?.();
const query = since === undefined ? '' : `?since=${since}`;
const wsUrl = `${protocol}//${window.location.host}/ws/chat/${this.sessionId}${query}`;
```

Parse `since`, `last_seq`, and `reset`, convert them to numbers, and pass them as the fourth `onHistory` argument.

- [ ] **Step 4: Run the quick gate**

Run: `./test.sh --quick`

Expected: PASS.

- [ ] **Step 5: User commit gate**

Ask the user to review and commit. Suggested message: `feat(dashboard): request incremental chat history`

---

### Task 4: Durable normalized conversation cache

**Files:**

- Create: `assets/dashboard/src/lib/chat/conversation-cache.ts`
- Create: `assets/dashboard/src/lib/chat/conversation-cache.test.ts`
- Modify: `assets/dashboard/src/hooks/useChatSocket.ts`
- Modify: `assets/dashboard/src/hooks/useChatSocket.test.tsx`

**Interfaces:**

- Consumes: `ChatProtocol`, `Conversation`, `applyRecord`, and Task 3 metadata.
- Produces:

```ts
export interface CachedChatConversation {
  protocol: ChatProtocol;
  lastSeq: number;
  conversation: Conversation;
}

export function getCachedConversation(sessionId: string): CachedChatConversation | undefined;
export function setCachedConversation(sessionId: string, entry: CachedChatConversation): void;
export function clearCachedConversation(sessionId: string): void;
export function resetConversationCacheForTests(): void;
```

- [ ] **Step 1: Write failing cache tests**

Create `conversation-cache.test.ts`:

```ts
import { beforeEach, describe, expect, it } from 'vitest';
import { emptyConversation } from './reducer';
import {
  clearCachedConversation,
  getCachedConversation,
  resetConversationCacheForTests,
  setCachedConversation,
} from './conversation-cache';

describe('conversation-cache', () => {
  beforeEach(() => {
    resetConversationCacheForTests();
  });

  it('stores entries by session', () => {
    const conversation = emptyConversation();
    setCachedConversation('s1', { protocol: 'claude-stream-json', lastSeq: 4, conversation });
    expect(getCachedConversation('s1')?.conversation).toBe(conversation);
    expect(getCachedConversation('s2')).toBeUndefined();
  });

  it('replaces an entry and clears it explicitly', () => {
    setCachedConversation('s1', {
      protocol: 'claude-stream-json',
      lastSeq: 1,
      conversation: emptyConversation(),
    });
    setCachedConversation('s1', {
      protocol: 'codex-app-server',
      lastSeq: 2,
      conversation: emptyConversation(),
    });
    expect(getCachedConversation('s1')?.lastSeq).toBe(2);
    clearCachedConversation('s1');
    expect(getCachedConversation('s1')).toBeUndefined();
  });
});
```

- [ ] **Step 2: Write failing hook behavior test**

Add to `useChatSocket.test.tsx`:

```ts
it('resumes from cached durable state and keeps overlays out of the cache', async () => {
  resetConversationCacheForTests();
  const { result, unmount } = renderHook(() => useChatSocket('resume', true));
  act(() => {
    lastWS().onopen?.();
    lastWS().onmessage?.({
      data: JSON.stringify({
        type: 'history',
        load_id: 'cold',
        protocol: 'claude-stream-json',
        since: undefined,
        last_seq: 1,
        reset: false,
        records: [
          { seq: 1, ts: 't', type: 'user_message', id: 'u1', text: 'hello' },
          { ts: 't', type: 'user_message_queue', id: 'u1', queued: true },
        ],
      }),
    });
  });
  expect(getCachedConversation('resume')?.lastSeq).toBe(1);
  expect(getCachedConversation('resume')?.conversation.items[0]).toMatchObject({ queued: false });
  expect(result.current.conversation.items[0]).toMatchObject({ queued: true });

  act(() => {
    lastWS().onclose?.({ code: 1006 });
    vi.advanceTimersByTime(500);
  });
  expect(lastWS().url).toContain('/ws/chat/resume?since=1');
  act(() => {
    lastWS().onopen?.();
    lastWS().onmessage?.({
      data: JSON.stringify({
        type: 'history',
        load_id: 'delta',
        protocol: 'claude-stream-json',
        since: 1,
        last_seq: 2,
        reset: false,
        records: [
          { seq: 2, ts: 't', type: 'harness', line: { type: 'result', subtype: 'success' } },
        ],
      }),
    });
  });
  expect(result.current.conversation.phase).toBe('idle');
  expect(getCachedConversation('resume')?.lastSeq).toBe(2);
  unmount();
});

it('treats a legacy history frame as a cold load', () => {
  resetConversationCacheForTests();
  setCachedConversation('legacy', {
    protocol: 'claude-stream-json',
    lastSeq: 9,
    conversation: emptyConversation(),
  });
  const { result, unmount } = renderHook(() => useChatSocket('legacy', true));
  act(() => {
    lastWS().onopen?.();
    lastWS().onmessage?.({
      data: JSON.stringify({
        type: 'history',
        protocol: 'claude-stream-json',
        records: [{ ts: 't', type: 'user_message', id: 'u1', text: 'full' }],
      }),
    });
  });
  expect(JSON.stringify(result.current.conversation.items)).toContain('full');
  expect(getCachedConversation('legacy')).toBeUndefined();
  unmount();
});
```

Import the cache helpers and `emptyConversation` in the test file. Reset the cache in the existing global `beforeEach`.

- [ ] **Step 3: Run frontend tests and verify they fail**

Run: `./test.sh --quick`

Expected: FAIL because the cache module does not exist and the hook does not use it.

- [ ] **Step 4: Implement the cache and reconnect reduction**

Create the cache module as a `Map<string, CachedChatConversation>` with the exact exports above.

In `useChatSocket`, construct `ChatSocket` with:

```ts
() => getCachedConversation(sessionId)?.lastSeq;
```

so automatic reconnect reads the latest value.

In `onHistory`, read the cache at frame time rather than capturing a stale value:

```ts
const cached = getCachedConversation(sessionId);
```

Then derive:

```ts
const cacheHit =
  metadata.lastSeq !== undefined &&
  !metadata.reset &&
  cached?.protocol === protocol &&
  cached.lastSeq === metadata.since;
const durable = records.filter((record) => (record.seq ?? 0) > 0);
const overlays = records.filter((record) => (record.seq ?? 0) === 0);
```

For a hit, start from `cached.conversation`; otherwise start from `emptyConversation()`. Reduce only `durable` on a hit. Save the durable result before applying overlays to the rendered conversation. For a legacy frame (`lastSeq === undefined`), clear the cache entry.

Do not change the cache in `onRecord`.

- [ ] **Step 5: Run the frontend gate**

Run: `./test.sh --quick`

Expected: PASS.

- [ ] **Step 6: User commit gate**

Ask the user to review and commit. Suggested message: `feat(dashboard): resume normalized chat conversations`

---

### Task 5: Telemetry and API documentation

**Files:**

- Modify: `assets/dashboard/src/lib/chat/loadTelemetry.ts`
- Modify: `assets/dashboard/src/lib/chat/loadTelemetry.test.ts`
- Modify: `docs/api.md`
- Modify: `docs/chat-sessions.md`

**Interfaces:**

- Consumes: Task 2 daemon fields and Task 4 cache result.
- Produces browser sample fields:

```ts
cacheHit?: boolean;
since?: number;
lastSeq?: number;
durableRecords?: number;
```

- [ ] **Step 1: Extend the browser telemetry test**

In `loadTelemetry.test.ts`, assert that a sample containing the new fields uploads them unchanged:

```ts
expect(sample).toMatchObject({
  cacheHit: true,
  since: 4,
  lastSeq: 5,
  durableRecords: 1,
});
```

Add the same fields to a `useChatSocket` load-sample assertion on a delta reconnect.

- [ ] **Step 2: Run and verify the telemetry change**

Run: `./test.sh --quick`

Expected: PASS before documentation edits and PASS after them; telemetry behavior must not depend on docs.

- [ ] **Step 3: Document the wire contract**

In `docs/api.md` under `WS /ws/chat/{sessionId}`:

```markdown
Optional query parameter `since` requests durable records whose delivery `seq` is greater than `since`. A cold connect omits it. The response always includes `last_seq`; `reset` is true only when schmux cannot satisfy the requested delta and returns full history. `seq` is delivery metadata, is not persisted, and is absent from live-only stream events and queue overlays.
```

Update the frame example with `since`, `last_seq`, `reset`, and a record `seq`. Update telemetry text with `durable_records`.

In `docs/chat-sessions.md`, describe the browser durable cache, stale live-only state being dropped on reconnect, and cold-load behavior.

- [ ] **Step 4: Run the API documentation gate**

Run: `./scripts/check-api-docs.sh`

Expected: PASS.

- [ ] **Step 5: User commit gate**

Ask the user to review and commit. Suggested message: `docs(chat): document incremental history loads`

---

### Task 6: Full verification and test review

**Files:**

- No new source files. Runs final gates over all changes from Tasks 1-5.

**Interfaces:**

- Consumes all prior tasks.
- Produces evidence that the implementation is complete.

- [ ] **Step 1: Format everything**

Run: `./format.sh`

Expected: `FORMAT_RESULT=PASS`.

- [ ] **Step 2: Run the complete test suite**

Run: `./test.sh`

Expected: backend, frontend, and scenario summary all pass with no skipped task-specific gate.

- [ ] **Step 3: Review tests against the sole rubric**

Use the `test-rules-review` skill over every new or changed test. Fix every finding before claiming completion.

- [ ] **Step 4: Check the diff**

Run: `git diff --check`

Expected: no output.

- [ ] **Step 5: Check the two success measurements**

With chat-load profiling enabled, reconnect to a large unchanged chat session and inspect the new `history` and `browser_load` pair:

```text
durable_records == 0
frame bytes near metadata-only size
cacheHit == true
reduceMs < 50
```

Then force a cold reload and confirm the transcript matches the incremental reconnect render.

- [ ] **Step 6: User commit gate**

Ask the user to review the complete diff and commit. Suggested message: `feat(chat): resume conversations incrementally`
