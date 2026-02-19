package session

import (
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/state"
)

func TestSessionTrackerAttachDetach(t *testing.T) {
	st := state.New("")
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil)

	ch1 := tracker.AttachWebSocket()
	if ch1 == nil {
		t.Fatal("expected first channel")
	}

	ch2 := tracker.AttachWebSocket()
	if ch2 == nil {
		t.Fatal("expected second channel")
	}
	if ch1 == ch2 {
		t.Fatal("expected replacement channel")
	}

	select {
	case _, ok := <-ch1:
		if ok {
			t.Fatal("expected replaced channel to be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected replaced channel close signal")
	}

	tracker.DetachWebSocket(ch2)
	select {
	case _, ok := <-ch2:
		if ok {
			t.Fatal("expected detached channel to be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected detached channel close signal")
	}
}

func TestSessionTrackerInputResizeWithoutPTY(t *testing.T) {
	st := state.New("")
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil)

	if err := tracker.SendInput("abc"); err == nil {
		t.Fatal("expected error when PTY is not attached")
	}
	err := tracker.Resize(80, 24)
	if err == nil {
		t.Fatal("expected error when PTY is not attached")
	}
}

func TestFindValidUTF8Boundary(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected int
	}{
		{
			name:     "empty slice",
			input:    []byte{},
			expected: 0,
		},
		{
			name:     "ASCII only",
			input:    []byte("hello world"),
			expected: 11,
		},
		{
			name:     "valid 2-byte UTF-8 (é = C3 A9)",
			input:    []byte("café"),
			expected: 5, // c-a-f-é(2 bytes)
		},
		{
			name:     "valid 3-byte UTF-8 (中 = E4 B8 AD)",
			input:    []byte("中文"),
			expected: 6, // 2 chars × 3 bytes
		},
		{
			name:     "valid 4-byte UTF-8 (😀 = F0 9F 98 80)",
			input:    []byte("😀"),
			expected: 4,
		},
		{
			name:     "incomplete 2-byte at end (missing continuation)",
			input:    []byte{0x48, 0x69, 0xC3}, // "Hi" + first byte of é
			expected: 2,                        // only "Hi"
		},
		{
			name:     "incomplete 3-byte at end (1 of 3 bytes)",
			input:    []byte{0x48, 0x69, 0xE4}, // "Hi" + first byte of 中
			expected: 2,
		},
		{
			name:     "incomplete 3-byte at end (2 of 3 bytes)",
			input:    []byte{0x48, 0x69, 0xE4, 0xB8}, // "Hi" + first 2 bytes of 中
			expected: 2,
		},
		{
			name:     "incomplete 4-byte at end (1 of 4 bytes)",
			input:    []byte{0x48, 0x69, 0xF0}, // "Hi" + first byte of emoji
			expected: 2,
		},
		{
			name:     "incomplete 4-byte at end (2 of 4 bytes)",
			input:    []byte{0x48, 0x69, 0xF0, 0x9F}, // "Hi" + first 2 bytes of emoji
			expected: 2,
		},
		{
			name:     "incomplete 4-byte at end (3 of 4 bytes)",
			input:    []byte{0x48, 0x69, 0xF0, 0x9F, 0x98}, // "Hi" + first 3 bytes of emoji
			expected: 2,
		},
		{
			name:     "complete character followed by incomplete",
			input:    []byte{0xE4, 0xB8, 0xAD, 0xE6}, // 中 + first byte of another 3-byte
			expected: 3,
		},
		{
			name:     "mixed ASCII and multi-byte with incomplete at end",
			input:    append([]byte("Hello 中文 "), []byte{0xF0, 0x9F}...), // trailing incomplete emoji
			expected: 13,                                                 // "Hello 中文 " = 6 + 3 + 3 + 1
		},
		{
			name:     "ANSI escape sequences are valid ASCII",
			input:    []byte("\x1b[31mred\x1b[0m"),
			expected: 12,
		},
		{
			name:     "terminal output with UTF-8 and ANSI",
			input:    []byte("\x1b[32m✓\x1b[0m test"),
			expected: 17, // ESC[32m(5) + ✓(3) + ESC[0m(4) + " test"(5)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findValidUTF8Boundary(tt.input)
			if result != tt.expected {
				t.Errorf("findValidUTF8Boundary(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsMeaningfulTerminalChunk(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			name: "pure ANSI escape sequences",
			data: []byte("\x1b[31m\x1b[0m\x1b[1;34m"),
			want: false,
		},
		{
			name: "printable text",
			data: []byte("hello world"),
			want: true,
		},
		{
			name: "mixed ANSI and text",
			data: []byte("\x1b[32mSuccess\x1b[0m"),
			want: true,
		},
		{
			name: "tmux DA response (CSI ?)",
			data: []byte("\x1b[?1;2c"),
			want: false,
		},
		{
			name: "tmux DA response (CSI >)",
			data: []byte("\x1b[>0;136;0c"),
			want: false,
		},
		{
			name: "OSC 10 foreground query",
			data: []byte("\x1b]10;rgb:ffff/ffff/ffff\x1b\\"),
			want: false,
		},
		{
			name: "OSC 11 background query",
			data: []byte("\x1b]11;rgb:0000/0000/0000\x1b\\"),
			want: false,
		},
		{
			name: "whitespace only",
			data: []byte("   \t\n  "),
			want: false,
		},
		{
			name: "empty",
			data: []byte{},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isMeaningfulTerminalChunk(tt.data)
			if got != tt.want {
				t.Errorf("isMeaningfulTerminalChunk(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

// TestSendInputFallbackComment documents that SendInput falls back to tmux
// send-keys when the PTY is not attached. A full integration test of the
// successful fallback path requires a running tmux server.
func TestSendInputFallbackComment(t *testing.T) {
	st := state.New("")
	tracker := NewSessionTracker("s1", "tmux-nonexistent", st, "", nil, nil)

	// Without a PTY or tmux, SendInput returns an error from the tmux fallback.
	// This documents the fallback behavior; verifying successful delivery
	// requires an integration test with a live tmux session.
	err := tracker.SendInput("test input")
	if err == nil {
		t.Fatal("expected error from SendInput without PTY or tmux")
	}
}

func TestSendCoalesced_ImmediateSend(t *testing.T) {
	// When the channel has capacity, chunks are sent immediately with no buffering.
	ch := make(chan []byte, 4)
	data := []byte("hello")

	coalesce := sendCoalesced(ch, data, nil)
	if coalesce != nil {
		t.Fatal("expected nil coalesce buffer after successful send")
	}

	select {
	case got := <-ch:
		if string(got) != "hello" {
			t.Fatalf("got %q, want %q", got, "hello")
		}
	default:
		t.Fatal("expected chunk in channel")
	}
}

func TestSendCoalesced_BuffersWhenFull(t *testing.T) {
	// When the channel is full, the chunk is buffered (not dropped).
	ch := make(chan []byte, 1)
	ch <- []byte("filler") // fill the channel

	coalesce := sendCoalesced(ch, []byte("buffered"), nil)
	if coalesce == nil {
		t.Fatal("expected non-nil coalesce buffer when channel is full")
	}
	if string(coalesce) != "buffered" {
		t.Fatalf("coalesce = %q, want %q", coalesce, "buffered")
	}
}

func TestSendCoalesced_MergesBufferedData(t *testing.T) {
	// Previously buffered data is merged with the new chunk and sent together.
	ch := make(chan []byte, 4)
	prev := []byte("first-")

	coalesce := sendCoalesced(ch, []byte("second"), prev)
	if coalesce != nil {
		t.Fatal("expected nil coalesce after successful merged send")
	}

	got := <-ch
	if string(got) != "first-second" {
		t.Fatalf("got %q, want %q", got, "first-second")
	}
}

func TestSendCoalesced_NoDataLostUnderBackpressure(t *testing.T) {
	// Simulates rapid output: multiple chunks arrive while channel is full.
	// All data must be preserved — none dropped.
	ch := make(chan []byte, 1)
	ch <- []byte("blocking") // fill the channel

	// Three chunks arrive while channel is full
	var coalesce []byte
	coalesce = sendCoalesced(ch, []byte("chunk1-"), coalesce)
	coalesce = sendCoalesced(ch, []byte("chunk2-"), coalesce)
	coalesce = sendCoalesced(ch, []byte("chunk3"), coalesce)

	if coalesce == nil {
		t.Fatal("expected buffered coalesce data")
	}
	if string(coalesce) != "chunk1-chunk2-chunk3" {
		t.Fatalf("coalesce = %q, want %q", coalesce, "chunk1-chunk2-chunk3")
	}

	// Drain the blocking item to make room
	<-ch

	// Next send should flush the merged data
	coalesce = sendCoalesced(ch, []byte("-chunk4"), coalesce)
	if coalesce != nil {
		t.Fatal("expected nil coalesce after flush")
	}

	got := <-ch
	if string(got) != "chunk1-chunk2-chunk3-chunk4" {
		t.Fatalf("flushed = %q, want %q", got, "chunk1-chunk2-chunk3-chunk4")
	}
}

func TestSendCoalesced_BackpressureOnLargePayload(t *testing.T) {
	// When coalesced data exceeds 1MB, sendCoalesced blocks until the
	// channel drains, applying backpressure to the producer.
	ch := make(chan []byte, 1)

	// Create a >1MB payload
	large := make([]byte, 1<<20+1)
	for i := range large {
		large[i] = 'A'
	}

	done := make(chan struct{})
	var coalesce []byte
	go func() {
		coalesce = sendCoalesced(ch, large, nil)
		close(done)
	}()

	// The send should block because it's >1MB — read from channel to unblock
	got := <-ch
	<-done

	if coalesce != nil {
		t.Fatal("expected nil coalesce after blocking send")
	}
	if len(got) != 1<<20+1 {
		t.Fatalf("got %d bytes, want %d", len(got), 1<<20+1)
	}
}
