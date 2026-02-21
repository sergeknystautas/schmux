package dashboard

import (
	"testing"
)

func TestRingBuffer_WriteAndSnapshot(t *testing.T) {
	rb := NewRingBuffer(16) // 16-byte buffer
	rb.Write([]byte("hello"))
	snap := rb.Snapshot()
	if string(snap) != "hello" {
		t.Errorf("got %q, want %q", string(snap), "hello")
	}
}

func TestRingBuffer_Wraps(t *testing.T) {
	rb := NewRingBuffer(8)
	rb.Write([]byte("abcdefgh")) // fills exactly
	rb.Write([]byte("ij"))       // overwrites first 2 bytes
	snap := rb.Snapshot()
	// should return data in order: "cdefghij"
	if string(snap) != "cdefghij" {
		t.Errorf("got %q, want %q", string(snap), "cdefghij")
	}
}

func TestRingBuffer_Empty(t *testing.T) {
	rb := NewRingBuffer(16)
	snap := rb.Snapshot()
	if len(snap) != 0 {
		t.Errorf("expected empty snapshot, got %d bytes", len(snap))
	}
}

func TestRingBuffer_LargerThanBuffer(t *testing.T) {
	rb := NewRingBuffer(4)
	rb.Write([]byte("abcdefgh")) // write 8 bytes into 4-byte buffer
	snap := rb.Snapshot()
	// should keep only last 4 bytes
	if string(snap) != "efgh" {
		t.Errorf("got %q, want %q", string(snap), "efgh")
	}
}

func TestRingBuffer_MultipleSmallWrites(t *testing.T) {
	rb := NewRingBuffer(8)
	rb.Write([]byte("ab"))
	rb.Write([]byte("cd"))
	rb.Write([]byte("ef"))
	rb.Write([]byte("gh"))
	rb.Write([]byte("ij")) // overwrites "ab"
	snap := rb.Snapshot()
	if string(snap) != "cdefghij" {
		t.Errorf("got %q, want %q", string(snap), "cdefghij")
	}
}
