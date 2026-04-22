package session

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestInputEchoBuffer_AppendRecordsData verifies that appendInput records
// bytes plus a timestamp, and that matchesRecent finds them while inside the
// configured window.
func TestInputEchoBuffer_AppendRecordsData(t *testing.T) {
	var b inputEchoBuffer
	now := time.Now()
	payload := "MARKER-test-12345"
	b.appendInput([]byte(payload), now)

	b.mu.Lock()
	got := len(b.entries)
	bytes := b.totalBytes
	b.mu.Unlock()

	if got != 1 {
		t.Fatalf("entries=%d, want 1", got)
	}
	if bytes != len(payload) {
		t.Errorf("totalBytes=%d, want %d", bytes, len(payload))
	}
	if !b.matchesRecent(payload, now.Add(time.Millisecond), inputEchoWindow) {
		t.Error("matchesRecent should find the just-appended payload within the window")
	}
}

// TestInputEchoBuffer_MatchesRecentTrue covers the fast-match case.
func TestInputEchoBuffer_MatchesRecentTrue(t *testing.T) {
	var b inputEchoBuffer
	now := time.Now()
	b.appendInput([]byte("MARKER-test-12345"), now)

	if !b.matchesRecent("MARKER-test-12345", now.Add(time.Second), inputEchoWindow) {
		t.Error("expected exact match within window")
	}
}

// TestInputEchoBuffer_MatchesRecentSubstring verifies substring matching:
// the OSC 52 payload may be a subset of a longer SendInput chunk (e.g.
// "claude \"MARKER\"\n" → OSC 52 with "MARKER").
func TestInputEchoBuffer_MatchesRecentSubstring(t *testing.T) {
	var b inputEchoBuffer
	now := time.Now()
	b.appendInput([]byte(`claude "MARKER-payload-abc123"`+"\n"), now)

	if !b.matchesRecent("MARKER-payload-abc123", now.Add(time.Second), inputEchoWindow) {
		t.Error("expected substring match against longer recorded chunk")
	}
}

// TestInputEchoBuffer_MatchesRecentFalse_ContentDiffers verifies the
// straightforward non-match: an OSC 52 the user did NOT type should fall
// through (banner fires).
func TestInputEchoBuffer_MatchesRecentFalse_ContentDiffers(t *testing.T) {
	var b inputEchoBuffer
	now := time.Now()
	b.appendInput([]byte("hello-world-foo"), now)

	if b.matchesRecent("entirely-different-bytes-here", now.Add(time.Second), inputEchoWindow) {
		t.Error("non-matching content should not be suppressed")
	}
}

// TestInputEchoBuffer_MatchesRecentFalse_TooOld verifies that entries
// outside the window are no longer eligible to suppress.
func TestInputEchoBuffer_MatchesRecentFalse_TooOld(t *testing.T) {
	var b inputEchoBuffer
	old := time.Now()
	b.appendInput([]byte("MARKER-payload-abc"), old)

	now := old.Add(2 * inputEchoWindow)
	if b.matchesRecent("MARKER-payload-abc", now, inputEchoWindow) {
		t.Error("entry older than window should not suppress")
	}
}

// TestInputEchoBuffer_TrimsExpiredEntries verifies the trim path discards
// stale entries on a subsequent call. Asserts both that the matching call
// returns false AND that the buffer's entries slice has shrunk.
func TestInputEchoBuffer_TrimsExpiredEntries(t *testing.T) {
	var b inputEchoBuffer
	old := time.Now()
	b.appendInput([]byte("very-old-entry-bytes"), old)

	// Second append far in the future triggers trim of the first entry.
	future := old.Add(2 * inputEchoWindow)
	b.appendInput([]byte("fresh-entry-bytes"), future)

	b.mu.Lock()
	entries := len(b.entries)
	bytes := b.totalBytes
	b.mu.Unlock()

	if entries != 1 {
		t.Fatalf("entries after trim=%d, want 1", entries)
	}
	if bytes != len("fresh-entry-bytes") {
		t.Errorf("totalBytes after trim=%d, want %d", bytes, len("fresh-entry-bytes"))
	}

	// The fresh entry should still match.
	if !b.matchesRecent("fresh-entry-bytes", future.Add(time.Millisecond), inputEchoWindow) {
		t.Error("fresh entry should still match after trim")
	}
}

// TestInputEchoBuffer_RejectsShortNeedle verifies that needles shorter than
// inputEchoMinLen are not suppressed even if they would textually match.
// Protects against accidental matches on tiny content.
func TestInputEchoBuffer_RejectsShortNeedle(t *testing.T) {
	var b inputEchoBuffer
	now := time.Now()
	b.appendInput([]byte("ok"), now)

	if b.matchesRecent("ok", now.Add(time.Millisecond), inputEchoWindow) {
		t.Errorf("needle %q is below min-len threshold (%d) and should not suppress", "ok", inputEchoMinLen)
	}
}

// TestInputEchoBuffer_CapacityEvictsOldest verifies that pumping bytes far
// past inputEchoCapacity triggers eviction from the front. The newest entry
// should still match; the oldest should not.
func TestInputEchoBuffer_CapacityEvictsOldest(t *testing.T) {
	var b inputEchoBuffer
	base := time.Now()

	oldestNeedle := "OLDEST-MARKER-entry-aaa"
	b.appendInput([]byte(oldestNeedle), base)

	// Push enough bytes to exceed capacity. Use chunks within the time
	// window so trim doesn't remove them by age.
	chunk := make([]byte, 1024)
	for i := range chunk {
		chunk[i] = byte('A' + (i % 26))
	}
	for i := 0; i < (inputEchoCapacity/1024)+2; i++ {
		b.appendInput(chunk, base.Add(time.Duration(i+1)*time.Millisecond))
	}

	newestNeedle := "NEWEST-MARKER-entry-zzz"
	tNew := base.Add(time.Second)
	b.appendInput([]byte(newestNeedle), tNew)

	b.mu.Lock()
	bytes := b.totalBytes
	b.mu.Unlock()

	if bytes > inputEchoCapacity {
		t.Errorf("totalBytes=%d exceeds capacity=%d", bytes, inputEchoCapacity)
	}
	if b.matchesRecent(oldestNeedle, tNew.Add(time.Millisecond), inputEchoWindow) {
		t.Error("oldest entry should have been evicted by capacity overflow")
	}
	if !b.matchesRecent(newestNeedle, tNew.Add(time.Millisecond), inputEchoWindow) {
		t.Error("newest entry should still match after eviction")
	}
}

// TestInputEchoBuffer_IgnoresEmpty verifies that empty appends are dropped.
func TestInputEchoBuffer_IgnoresEmpty(t *testing.T) {
	var b inputEchoBuffer
	b.appendInput(nil, time.Now())
	b.appendInput([]byte{}, time.Now())

	b.mu.Lock()
	got := len(b.entries)
	b.mu.Unlock()
	if got != 0 {
		t.Errorf("entries=%d, want 0 after empty appends", got)
	}
}

// TestInputEchoBuffer_DefensiveCopy verifies that mutating the input slice
// after appendInput does not corrupt the recorded data — the buffer must
// own its bytes.
func TestInputEchoBuffer_DefensiveCopy(t *testing.T) {
	var b inputEchoBuffer
	now := time.Now()
	original := []byte("MARKER-defensive-copy")
	b.appendInput(original, now)

	// Scribble over the caller's slice.
	for i := range original {
		original[i] = '!'
	}

	if !b.matchesRecent("MARKER-defensive-copy", now.Add(time.Millisecond), inputEchoWindow) {
		t.Error("recorded bytes should not be affected by caller mutating its slice")
	}
}

// TestInputEchoBuffer_ConcurrentAppendAndMatch is a -race check: spawn many
// goroutines appending and matching simultaneously. We don't assert on
// outcome (the interleaving is nondeterministic); the goal is that go test
// -race doesn't trip.
func TestInputEchoBuffer_ConcurrentAppendAndMatch(t *testing.T) {
	var b inputEchoBuffer
	const goroutines = 20
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				payload := fmt.Sprintf("APPEND-%d-%d-padding-padding", i, j)
				b.appendInput([]byte(payload), time.Now())
			}
		}(i)
	}

	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				needle := fmt.Sprintf("APPEND-%d-%d-padding-padding", i, j)
				_ = b.matchesRecent(needle, time.Now(), inputEchoWindow)
			}
		}(i)
	}

	wg.Wait()
}

// TestInputEchoBuffer_WindowOverride verifies that callers can override the
// window per-call, which is what fanOut would do if we ever introduced a
// shorter check window. Matches today's behavior of always using the
// package var.
func TestInputEchoBuffer_WindowOverride(t *testing.T) {
	var b inputEchoBuffer
	now := time.Now()
	b.appendInput([]byte("MARKER-narrow-window"), now)

	// 1 ns window — should not match.
	if b.matchesRecent("MARKER-narrow-window", now.Add(time.Second), 1*time.Nanosecond) {
		t.Error("entry should be outside a 1ns window when checked 1s later")
	}
	// 10 s window — should match.
	if !b.matchesRecent("MARKER-narrow-window", now.Add(time.Second), 10*time.Second) {
		t.Error("entry should be inside a 10s window when checked 1s later")
	}
}
