package session

import (
	"strings"
	"sync"
	"time"
)

// inputEchoWindow is how long sent-input bytes remain eligible to suppress a
// matching ClipboardRequest. Tuned for Claude-Code-style argv-prompt
// round-trips: the user types `claude "MARKER"`, schmux sends those bytes
// into the pane, and Claude Code reads its own argv and emits OSC 52 with the
// same payload within ~10–500 ms. 5 s is generous for a slow-starting TUI
// without holding past the point a real yank would have happened.
//
// inputEchoMinLen guards against accidental matches on very short content
// (e.g. user types "ok", a TUI happens to OSC 52 "ok" — we want the banner).
//
// inputEchoCapacity bounds the per-session ring buffer. 16 KiB is plenty for
// any reasonable typed input including a long pasted prompt; older entries
// are trimmed lazily on every appendInput / matchesRecent call.
//
// Both window and capacity are package-level vars so tests can override them
// without sleeping for full real-world durations. (Same pattern as
// clipboardDebounceWindow in dashboard/clipboard_state.go.)
var (
	inputEchoWindow   = 5 * time.Second
	inputEchoMinLen   = 8
	inputEchoCapacity = 16 * 1024
)

// inputEchoEntry is one chunk of bytes recorded by SendInput, paired with the
// timestamp at which it was sent. Substring matching against req.Text uses
// the entry's bytes; the timestamp drives both eligibility and trimming.
type inputEchoEntry struct {
	data   []byte
	sentAt time.Time
}

// inputEchoBuffer is a per-session record of recently-sent input bytes. It
// supports the "TUI echoed back what the user just typed" suppression
// heuristic in fanOut and the SourcePasteBuffer handler.
//
// Why per-chunk entries (vs one rolling byte slice): chunk boundaries
// preserve substring matching against the original SendInput payload — if
// the user types "MARKER" as a single SendInput, the entry holds "MARKER"
// and a TUI echoing "MARKER" matches exactly. A flat byte slice would
// require us to re-slice on every match, and trimming would be more
// expensive (we'd have to walk to find a chunk boundary).
//
// Memory bound: totalBytes is tracked so we can evict oldest entries when
// the ring exceeds inputEchoCapacity. Combined with the time-based trim,
// this ensures the buffer stays small even under sustained input.
type inputEchoBuffer struct {
	mu         sync.Mutex
	entries    []inputEchoEntry
	totalBytes int
}

// appendInput records one chunk of bytes sent to the pane via SendInput.
// Trims any entries older than inputEchoWindow (relative to t) and evicts
// oldest entries if total bytes exceeds inputEchoCapacity. Empty chunks are
// ignored — they cost a slot for no signal.
func (b *inputEchoBuffer) appendInput(data []byte, t time.Time) {
	if len(data) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	// Copy so the caller can mutate / reuse its slice without disturbing the
	// recorded bytes. SendInput hands us a string-derived []byte already, but
	// being defensive here is cheap and removes a class of subtle bugs.
	cp := make([]byte, len(data))
	copy(cp, data)
	b.entries = append(b.entries, inputEchoEntry{data: cp, sentAt: t})
	b.totalBytes += len(cp)

	b.trimLocked(t)
}

// matchesRecent reports whether needle is found as a substring of any
// recorded entry whose sentAt is within window of now. Trims expired
// entries opportunistically before searching so the search itself is fast
// even when SendInput is idle (a TUI emitting a stream of OSC 52 still
// triggers the trim).
//
// Returns false for needles shorter than inputEchoMinLen — short content
// might match by accident and we'd rather show a spurious banner than
// silently swallow a legitimate short yank.
func (b *inputEchoBuffer) matchesRecent(needle string, now time.Time, window time.Duration) bool {
	if len(needle) < inputEchoMinLen {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	b.trimLocked(now)

	cutoff := now.Add(-window)
	for _, entry := range b.entries {
		if entry.sentAt.Before(cutoff) {
			continue // belt-and-suspenders against trim slack
		}
		if strings.Contains(string(entry.data), needle) {
			return true
		}
	}
	return false
}

// trimLocked drops entries older than inputEchoWindow and, if the buffer
// still exceeds inputEchoCapacity, drops oldest entries until it doesn't.
// Caller must hold b.mu.
func (b *inputEchoBuffer) trimLocked(now time.Time) {
	cutoff := now.Add(-inputEchoWindow)

	// Drop expired entries from the front.
	drop := 0
	for drop < len(b.entries) && b.entries[drop].sentAt.Before(cutoff) {
		b.totalBytes -= len(b.entries[drop].data)
		drop++
	}
	if drop > 0 {
		// Slide the surviving entries down. Allocate a fresh slice when most
		// of the buffer was dropped so the underlying array can be GC'd; a
		// long-lived large buffer otherwise keeps stale data referenced.
		if drop >= len(b.entries)/2 {
			b.entries = append([]inputEchoEntry(nil), b.entries[drop:]...)
		} else {
			b.entries = b.entries[drop:]
		}
	}

	// Capacity overflow: evict oldest entries until under cap.
	for b.totalBytes > inputEchoCapacity && len(b.entries) > 0 {
		b.totalBytes -= len(b.entries[0].data)
		b.entries = b.entries[1:]
	}
}
