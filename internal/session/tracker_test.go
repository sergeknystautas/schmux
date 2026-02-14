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
