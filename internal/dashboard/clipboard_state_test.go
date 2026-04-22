package dashboard

import (
	"sync"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/session"
)

// capturingBroadcaster collects broadcast calls so tests can assert on them.
// All access is guarded by mu because broadcasts happen from time.AfterFunc
// goroutines.
type capturingBroadcaster struct {
	mu       sync.Mutex
	requests []contracts.ClipboardRequestEvent
	cleared  []contracts.ClipboardClearedEvent
}

func (c *capturingBroadcaster) BroadcastClipboardRequest(ev contracts.ClipboardRequestEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, ev)
}

func (c *capturingBroadcaster) BroadcastClipboardCleared(ev contracts.ClipboardClearedEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleared = append(c.cleared, ev)
}

func (c *capturingBroadcaster) requestsCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.requests)
}

func (c *capturingBroadcaster) clearedCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.cleared)
}

func (c *capturingBroadcaster) lastRequest() contracts.ClipboardRequestEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.requests) == 0 {
		return contracts.ClipboardRequestEvent{}
	}
	return c.requests[len(c.requests)-1]
}

// withTestDebounce shortens the debounce window and TTL for the duration of
// a test, restoring the originals afterwards. Avoids 200ms / 5min waits.
func withTestDebounce(t *testing.T, debounce, ttl time.Duration) {
	t.Helper()
	origDebounce, origTTL := clipboardDebounceWindow, clipboardTTL
	clipboardDebounceWindow = debounce
	clipboardTTL = ttl
	t.Cleanup(func() {
		clipboardDebounceWindow = origDebounce
		clipboardTTL = origTTL
	})
}

// waitFor polls cond every 5ms up to timeout. Returns true if cond became
// true, false on timeout. Used so tests don't depend on a single sleep
// matching the debounce window exactly (slow CI tolerance).
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

// TestClipboardState_DebouncedBroadcast verifies a single onRequest fires
// exactly one broadcast after the debounce window — not before.
func TestClipboardState_DebouncedBroadcast(t *testing.T) {
	withTestDebounce(t, 30*time.Millisecond, 5*time.Second)
	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)

	cs.onRequest(session.ClipboardRequest{SessionID: "s1", Text: "hello", ByteCount: 5})

	// Before the debounce fires (negative assertion — must wait), nothing
	// should have broadcast yet.
	time.Sleep(5 * time.Millisecond)
	if got := b.requestsCount(); got != 0 {
		t.Errorf("got %d broadcasts before debounce; want 0", got)
	}

	// Poll until the debounce fires.
	if !waitFor(time.Second, func() bool { return b.requestsCount() == 1 }) {
		t.Fatalf("got %d broadcasts after debounce window; want 1", b.requestsCount())
	}
	last := b.lastRequest()
	if last.SessionID != "s1" || last.Text != "hello" || last.Type != "clipboardRequest" {
		t.Errorf("unexpected event = %+v", last)
	}
}

// TestClipboardState_DebounceCoalesces verifies two onRequests within the
// debounce window collapse into a single broadcast carrying the second's
// payload.
func TestClipboardState_DebounceCoalesces(t *testing.T) {
	withTestDebounce(t, 50*time.Millisecond, 5*time.Second)
	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)

	cs.onRequest(session.ClipboardRequest{SessionID: "s1", Text: "first", ByteCount: 5})
	time.Sleep(20 * time.Millisecond)
	cs.onRequest(session.ClipboardRequest{SessionID: "s1", Text: "second", ByteCount: 6})

	if !waitFor(time.Second, func() bool { return b.requestsCount() >= 1 }) {
		t.Fatalf("got %d broadcasts; want 1", b.requestsCount())
	}
	if got := b.requestsCount(); got != 1 {
		t.Errorf("got %d broadcasts; want 1 (debounce should coalesce)", got)
	}
	if last := b.lastRequest(); last.Text != "second" {
		t.Errorf("last.Text = %q; want %q (debounce should keep newest payload)", last.Text, "second")
	}
}

// TestClipboardState_TTLClearsPending verifies the TTL timer fires a
// clipboardCleared and removes the entry from pending.
func TestClipboardState_TTLClearsPending(t *testing.T) {
	withTestDebounce(t, 10*time.Millisecond, 60*time.Millisecond)
	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)

	cs.onRequest(session.ClipboardRequest{SessionID: "s1", Text: "ttl-test"})

	// Wait for debounce + TTL to fire (one cleared event from TTL).
	if !waitFor(2*time.Second, func() bool { return b.clearedCount() == 1 }) {
		t.Fatalf("clearedCount = %d, want 1 (TTL should fire)", b.clearedCount())
	}
	cs.mu.Lock()
	_, stillPending := cs.pending["s1"]
	cs.mu.Unlock()
	if stillPending {
		t.Error("pending entry still present after TTL fired")
	}
}

// TestClipboardState_ClearMatchesRequestID verifies clear() distinguishes
// fresh vs stale requestIDs.
func TestClipboardState_ClearMatchesRequestID(t *testing.T) {
	withTestDebounce(t, 10*time.Millisecond, 5*time.Second)
	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)

	cs.onRequest(session.ClipboardRequest{SessionID: "s1", Text: "x"})
	if !waitFor(time.Second, func() bool { return b.requestsCount() == 1 }) {
		t.Fatal("debounce did not fire")
	}

	cs.mu.Lock()
	rid := cs.pending["s1"].requestID
	cs.mu.Unlock()

	if ok := cs.clear("s1", "wrong-id"); ok {
		t.Error("clear with wrong requestID should return false")
	}
	if ok := cs.clear("s1", rid); !ok {
		t.Error("clear with matching requestID should return true")
	}
}

// TestClipboardState_ClearUnknownSession returns false without crashing.
func TestClipboardState_ClearUnknownSession(t *testing.T) {
	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)
	if ok := cs.clear("nope", ""); ok {
		t.Error("clear of unknown session should return false")
	}
}

// TestClipboardState_Snapshot rehydrates a reconnecting client.
func TestClipboardState_Snapshot(t *testing.T) {
	withTestDebounce(t, 10*time.Millisecond, 5*time.Second)
	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)
	cs.onRequest(session.ClipboardRequest{SessionID: "s1", Text: "hello", ByteCount: 5})
	if !waitFor(time.Second, func() bool { return b.requestsCount() == 1 }) {
		t.Fatal("debounce did not fire")
	}

	snap := cs.snapshot()
	if len(snap) != 1 {
		t.Fatalf("got %d events, want 1", len(snap))
	}
	if snap[0].SessionID != "s1" || snap[0].Text != "hello" || snap[0].Type != "clipboardRequest" {
		t.Errorf("event = %+v", snap[0])
	}
}

// TestClipboardState_Snapshot_Empty returns an empty (non-nil-friendly) slice.
func TestClipboardState_Snapshot_Empty(t *testing.T) {
	cs := newClipboardState(&capturingBroadcaster{}, nil)
	if snap := cs.snapshot(); len(snap) != 0 {
		t.Errorf("snapshot of empty state = %+v, want empty", snap)
	}
}

// TestClipboardState_CrossSessionDedupSuppressesDuplicate verifies that two
// onRequest calls with the same Text from different SessionIDs within
// clipboardDedupWindow collapse into a single pending entry / single
// broadcast. This protects against the multi-session-on-shared-tmux-socket
// case where every control-mode client receives the same
// %paste-buffer-changed notification.
func TestClipboardState_CrossSessionDedupSuppressesDuplicate(t *testing.T) {
	withTestDebounce(t, 30*time.Millisecond, 5*time.Second)
	origDedup := clipboardDedupWindow
	clipboardDedupWindow = 200 * time.Millisecond
	t.Cleanup(func() { clipboardDedupWindow = origDedup })

	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)

	cs.onRequest(session.ClipboardRequest{SessionID: "s1", Text: "shared", ByteCount: 6})
	// Within the dedup window, this duplicate (different SessionID, same Text)
	// must be dropped silently.
	cs.onRequest(session.ClipboardRequest{SessionID: "s2", Text: "shared", ByteCount: 6})

	cs.mu.Lock()
	pendingCount := len(cs.pending)
	_, hasS1 := cs.pending["s1"]
	_, hasS2 := cs.pending["s2"]
	cs.mu.Unlock()
	if pendingCount != 1 {
		t.Errorf("pending entries = %d, want 1 (dedup should drop s2)", pendingCount)
	}
	if !hasS1 {
		t.Error("expected s1 to retain its pending entry")
	}
	if hasS2 {
		t.Error("expected s2 to be dropped by cross-session dedup")
	}

	// After the debounce fires, exactly ONE broadcast should land.
	if !waitFor(time.Second, func() bool { return b.requestsCount() == 1 }) {
		t.Fatalf("got %d broadcasts; want 1", b.requestsCount())
	}
	// Give any spurious extra broadcast a chance to fire.
	time.Sleep(50 * time.Millisecond)
	if got := b.requestsCount(); got != 1 {
		t.Errorf("got %d broadcasts after dedup; want 1", got)
	}
}

// TestClipboardState_CrossSessionDedupExpiresAfterWindow verifies that two
// onRequest calls with the same Text but separated by more than the dedup
// window produce TWO entries / TWO broadcasts.
func TestClipboardState_CrossSessionDedupExpiresAfterWindow(t *testing.T) {
	withTestDebounce(t, 5*time.Millisecond, 5*time.Second)
	origDedup := clipboardDedupWindow
	clipboardDedupWindow = 30 * time.Millisecond
	t.Cleanup(func() { clipboardDedupWindow = origDedup })

	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)

	cs.onRequest(session.ClipboardRequest{SessionID: "s1", Text: "shared", ByteCount: 6})
	// Wait past the dedup window AND past the debounce so s1 has fired and
	// been cleared (well, debounced — but the dedup window has elapsed).
	time.Sleep(80 * time.Millisecond)
	cs.onRequest(session.ClipboardRequest{SessionID: "s2", Text: "shared", ByteCount: 6})

	if !waitFor(time.Second, func() bool { return b.requestsCount() == 2 }) {
		t.Fatalf("got %d broadcasts; want 2 (dedup window expired)", b.requestsCount())
	}
}

// TestClipboardState_CrossSessionDedupIgnoresSameSession verifies the dedup
// scan skips the requesting session's own entry — same-session re-emits are
// handled by the per-session debounce (TestClipboardState_DebounceCoalesces).
func TestClipboardState_CrossSessionDedupIgnoresSameSession(t *testing.T) {
	withTestDebounce(t, 50*time.Millisecond, 5*time.Second)
	origDedup := clipboardDedupWindow
	clipboardDedupWindow = 200 * time.Millisecond
	t.Cleanup(func() { clipboardDedupWindow = origDedup })

	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)

	cs.onRequest(session.ClipboardRequest{SessionID: "s1", Text: "v1", ByteCount: 2})
	cs.onRequest(session.ClipboardRequest{SessionID: "s1", Text: "v1", ByteCount: 2})

	// Same session re-emit should be coalesced by the debounce path, not the
	// cross-session dedup path. There must still be exactly one pending entry
	// and exactly one broadcast carrying the latest payload.
	cs.mu.Lock()
	pendingCount := len(cs.pending)
	cs.mu.Unlock()
	if pendingCount != 1 {
		t.Errorf("pending = %d, want 1", pendingCount)
	}

	if !waitFor(time.Second, func() bool { return b.requestsCount() >= 1 }) {
		t.Fatalf("debounce did not fire")
	}
	if got := b.requestsCount(); got != 1 {
		t.Errorf("got %d broadcasts; want 1", got)
	}
}

// withTestPromptSuppressionTTL shortens the prompt suppression TTL for the
// duration of a test, restoring the original afterwards. Avoids 60s waits.
func withTestPromptSuppressionTTL(t *testing.T, ttl time.Duration) {
	t.Helper()
	orig := clipboardPromptSuppressionTTL
	clipboardPromptSuppressionTTL = ttl
	t.Cleanup(func() { clipboardPromptSuppressionTTL = orig })
}

// TestClipboardState_RegisterSpawnPrompt_SuppressesMatching verifies that a
// ClipboardRequest whose Text matches a registered spawn prompt is dropped
// silently and the prompt-suppression counter ticks. This is the headline UX
// fix: schmux knows the prompt verbatim because it just typed it into the
// spawn form, so the agent's argv-prompt round-trip should be invisible.
func TestClipboardState_RegisterSpawnPrompt_SuppressesMatching(t *testing.T) {
	withTestDebounce(t, 30*time.Millisecond, 5*time.Second)
	withTestPromptSuppressionTTL(t, 5*time.Second)

	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)

	cs.RegisterSpawnPrompt("hi")
	cs.onRequest(session.ClipboardRequest{SessionID: "s1", Text: "hi", ByteCount: 2})

	// Wait long enough for the debounce to have fired had the request not
	// been suppressed; nothing should have been broadcast.
	time.Sleep(80 * time.Millisecond)
	if got := b.requestsCount(); got != 0 {
		t.Errorf("got %d broadcasts; want 0 (prompt should suppress)", got)
	}
	if got := cs.PromptSuppressionCount(); got != 1 {
		t.Errorf("PromptSuppressionCount = %d; want 1", got)
	}
}

// TestClipboardState_RegisterSpawnPrompt_SuppressesAcrossSessions verifies
// the workspace-scoped semantics: a prompt registered once suppresses the
// matching ClipboardRequest from EVERY session, not just the spawn source.
// This is the bug fix — tmux's %paste-buffer-changed is server-scoped, so
// every control-mode client on the daemon socket receives the notification
// and would otherwise produce a banner (collapsed by cross-session dedup
// onto the WRONG session, attributing the event to a session that did
// nothing).
func TestClipboardState_RegisterSpawnPrompt_SuppressesAcrossSessions(t *testing.T) {
	withTestDebounce(t, 30*time.Millisecond, 5*time.Second)
	withTestPromptSuppressionTTL(t, 5*time.Second)

	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)

	cs.RegisterSpawnPrompt("hi")
	cs.onRequest(session.ClipboardRequest{SessionID: "s1", Text: "hi", ByteCount: 2})
	cs.onRequest(session.ClipboardRequest{SessionID: "s2", Text: "hi", ByteCount: 2})

	time.Sleep(80 * time.Millisecond)
	if got := b.requestsCount(); got != 0 {
		t.Errorf("got %d broadcasts; want 0 (prompt should suppress for both sessions)", got)
	}
	if got := cs.PromptSuppressionCount(); got != 2 {
		t.Errorf("PromptSuppressionCount = %d; want 2 (once per session)", got)
	}
}

// TestClipboardState_RegisterSpawnPrompt_ExpiresAfterTTL verifies the lazy
// expiry: once the registration TTL elapses, the next matching ClipboardRequest
// proceeds normally (no suppression) and the registry entry is evicted inline.
func TestClipboardState_RegisterSpawnPrompt_ExpiresAfterTTL(t *testing.T) {
	withTestDebounce(t, 10*time.Millisecond, 5*time.Second)
	withTestPromptSuppressionTTL(t, 10*time.Millisecond)

	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)

	cs.RegisterSpawnPrompt("hi")
	// Wait past the TTL so the entry's expiry is in the past.
	time.Sleep(40 * time.Millisecond)

	cs.onRequest(session.ClipboardRequest{SessionID: "s1", Text: "hi", ByteCount: 2})
	if !waitFor(time.Second, func() bool { return b.requestsCount() == 1 }) {
		t.Fatalf("got %d broadcasts; want 1 (TTL should have expired suppression)", b.requestsCount())
	}
	if got := cs.PromptSuppressionCount(); got != 0 {
		t.Errorf("PromptSuppressionCount = %d; want 0 (expired entry should not count)", got)
	}
	// Lazy eviction: after onRequest processed an expired entry, the
	// registry should have shrunk.
	cs.mu.Lock()
	_, present := cs.suppressedPrompts["hi"]
	cs.mu.Unlock()
	if present {
		t.Error("expired suppressedPrompts entry was not lazily evicted")
	}
}

// TestClipboardState_RegisterSpawnPrompt_DoesNotSuppressNonMatching verifies
// the registry is keyed by exact text — unrelated yanks still surface a
// banner.
func TestClipboardState_RegisterSpawnPrompt_DoesNotSuppressNonMatching(t *testing.T) {
	withTestDebounce(t, 10*time.Millisecond, 5*time.Second)
	withTestPromptSuppressionTTL(t, 5*time.Second)

	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)

	cs.RegisterSpawnPrompt("hi")
	cs.onRequest(session.ClipboardRequest{SessionID: "s1", Text: "bye", ByteCount: 3})

	if !waitFor(time.Second, func() bool { return b.requestsCount() == 1 }) {
		t.Fatalf("got %d broadcasts; want 1 (non-matching content should not be suppressed)", b.requestsCount())
	}
	if got := cs.PromptSuppressionCount(); got != 0 {
		t.Errorf("PromptSuppressionCount = %d; want 0 (no match)", got)
	}
}

// TestClipboardState_RegisterSpawnPrompt_EmptyIsNoop verifies the documented
// contract: registering "" leaves the registry untouched. Without this
// guard, a spawn handler that happens to receive req.Prompt == "" would
// inadvertently arm suppression to swallow the next OSC 52 of "".
func TestClipboardState_RegisterSpawnPrompt_EmptyIsNoop(t *testing.T) {
	withTestDebounce(t, 10*time.Millisecond, 5*time.Second)
	withTestPromptSuppressionTTL(t, 5*time.Second)

	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)

	cs.RegisterSpawnPrompt("")
	cs.mu.Lock()
	if _, present := cs.suppressedPrompts[""]; present {
		cs.mu.Unlock()
		t.Fatal(`RegisterSpawnPrompt("") populated the registry; want no-op`)
	}
	cs.mu.Unlock()

	cs.onRequest(session.ClipboardRequest{SessionID: "s1", Text: "", ByteCount: 0})
	if !waitFor(time.Second, func() bool { return b.requestsCount() == 1 }) {
		t.Fatalf("got %d broadcasts; want 1 (empty registration must not suppress)", b.requestsCount())
	}
	if got := cs.PromptSuppressionCount(); got != 0 {
		t.Errorf("PromptSuppressionCount = %d; want 0", got)
	}
}

// TestClipboardState_RegisterSpawnPrompt_IdempotentRefreshesTTL verifies
// that re-registering an existing prompt resets its expiry. Spawning the
// same prompt twice in quick succession should keep suppression active for
// the FULL TTL after the second registration, not the first.
func TestClipboardState_RegisterSpawnPrompt_IdempotentRefreshesTTL(t *testing.T) {
	withTestDebounce(t, 10*time.Millisecond, 5*time.Second)
	withTestPromptSuppressionTTL(t, 30*time.Millisecond)

	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)

	cs.RegisterSpawnPrompt("hi") // t = 0
	time.Sleep(15 * time.Millisecond)
	cs.RegisterSpawnPrompt("hi") // t = 15ms — refreshes TTL to t+30ms = 45ms

	// At t = 35ms the original TTL would have expired, but the refreshed
	// entry is still active (expires at t = 45ms).
	time.Sleep(20 * time.Millisecond) // t = 35ms
	cs.onRequest(session.ClipboardRequest{SessionID: "s1", Text: "hi", ByteCount: 2})
	time.Sleep(60 * time.Millisecond)
	if got := b.requestsCount(); got != 0 {
		t.Errorf("got %d broadcasts at t=35ms; want 0 (refreshed TTL should still be active)", got)
	}
	if got := cs.PromptSuppressionCount(); got != 1 {
		t.Errorf("PromptSuppressionCount at t=35ms = %d; want 1", got)
	}

	// After the refreshed TTL has fully elapsed, suppression no longer
	// applies. We waited 60ms above, so the registration (last set at
	// t=15ms with a 30ms TTL, expiring at t=45ms) is now expired.
	cs.onRequest(session.ClipboardRequest{SessionID: "s2", Text: "hi", ByteCount: 2})
	if !waitFor(time.Second, func() bool { return b.requestsCount() == 1 }) {
		t.Fatalf("got %d broadcasts after TTL fully elapsed; want 1", b.requestsCount())
	}
}

// TestClipboardState_DebounceStaleCallbackDoesNotDoubleBroadcast exercises
// the stale-debounce-callback race. With a zero-length debounce window the
// first callback may be in flight (or queued waiting on cs.mu) when the
// second onRequest arrives. The generation-counter guard in fireBroadcast
// must drop the stale callback so only ONE broadcast is emitted, carrying
// the second (latest) payload.
func TestClipboardState_DebounceStaleCallbackDoesNotDoubleBroadcast(t *testing.T) {
	withTestDebounce(t, 0, 5*time.Second)
	b := &capturingBroadcaster{}
	cs := newClipboardState(b, nil)

	// Tight burst — first debounce callback may be racing with the second
	// onRequest's re-arm.
	cs.onRequest(session.ClipboardRequest{SessionID: "s", Text: "first", ByteCount: 5})
	cs.onRequest(session.ClipboardRequest{SessionID: "s", Text: "second", ByteCount: 6})

	// Wait until at least one broadcast has been delivered.
	if !waitFor(time.Second, func() bool { return b.requestsCount() >= 1 }) {
		t.Fatalf("got %d broadcasts; want at least 1", b.requestsCount())
	}
	// Give any stale callback time to (incorrectly) fire.
	time.Sleep(50 * time.Millisecond)

	if got := b.requestsCount(); got != 1 {
		t.Errorf("expected 1 broadcast, got %d: %+v", got, b.requests)
	}
	if last := b.lastRequest(); last.Text != "second" {
		t.Errorf("expected the latest text, got %q", last.Text)
	}
}
