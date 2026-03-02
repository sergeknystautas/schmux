package dashboard

import (
	"fmt"
	"strings"
	"testing"
	"time"
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

func TestRingBuffer_TimestampMarker(t *testing.T) {
	rb := NewRingBuffer(1024)
	// Simulate the call-site pattern: timestamp marker then data
	ts := []byte(fmt.Sprintf("\n--- %s ---\n", time.Now().Format("15:04:05.000000")))
	rb.Write(ts)
	rb.Write([]byte("hello world"))
	snap := rb.Snapshot()
	snapStr := string(snap)
	if !strings.Contains(snapStr, "---") {
		t.Errorf("snapshot should contain timestamp marker '---', got %q", snapStr)
	}
	if !strings.Contains(snapStr, "hello world") {
		t.Errorf("snapshot should contain data 'hello world', got %q", snapStr)
	}
}
