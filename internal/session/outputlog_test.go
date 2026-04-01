package session

import (
	"testing"
	"time"
)

func TestOutputLog_AppendAndReplay(t *testing.T) {
	log := NewOutputLog(100) // 100 entries max

	log.Append([]byte("hello"))
	log.Append([]byte("world"))

	entries := log.ReplayFrom(0)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Seq != 0 || string(entries[0].Data) != "hello" {
		t.Errorf("entry 0: seq=%d data=%q", entries[0].Seq, entries[0].Data)
	}
	if entries[1].Seq != 1 || string(entries[1].Data) != "world" {
		t.Errorf("entry 1: seq=%d data=%q", entries[1].Seq, entries[1].Data)
	}
}

func TestOutputLog_ReplayFromMid(t *testing.T) {
	log := NewOutputLog(100)
	for i := 0; i < 10; i++ {
		log.Append([]byte{byte('a' + i)})
	}

	entries := log.ReplayFrom(7)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries (seq 7,8,9), got %d", len(entries))
	}
	if entries[0].Seq != 7 {
		t.Errorf("first entry seq=%d, want 7", entries[0].Seq)
	}
}

func TestOutputLog_ReplayFromCurrentSeq(t *testing.T) {
	log := NewOutputLog(100)
	log.Append([]byte("a"))
	log.Append([]byte("b"))

	// Requesting from the current seq (nothing new) returns empty
	entries := log.ReplayFrom(2)
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestOutputLog_Wraparound(t *testing.T) {
	log := NewOutputLog(4) // tiny buffer
	for i := 0; i < 10; i++ {
		log.Append([]byte{byte('0' + i)})
	}

	// Only last 4 entries should be available (seq 6,7,8,9)
	entries := log.ReplayFrom(6)
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
	if entries[0].Seq != 6 || string(entries[0].Data) != "6" {
		t.Errorf("entry 0: seq=%d data=%q", entries[0].Seq, entries[0].Data)
	}

	// Requesting evicted data returns nil (gap unrecoverable)
	entries = log.ReplayFrom(0)
	if entries != nil {
		t.Fatalf("expected nil for evicted data, got %d entries", len(entries))
	}
}

func TestOutputLog_CurrentSeq(t *testing.T) {
	log := NewOutputLog(100)
	if log.CurrentSeq() != 0 {
		t.Errorf("initial seq=%d, want 0", log.CurrentSeq())
	}
	log.Append([]byte("a"))
	if log.CurrentSeq() != 1 {
		t.Errorf("after 1 append seq=%d, want 1", log.CurrentSeq())
	}
}

func TestOutputLog_OldestSeq(t *testing.T) {
	log := NewOutputLog(4)
	// Empty log
	if log.OldestSeq() != 0 {
		t.Errorf("empty log oldest=%d, want 0", log.OldestSeq())
	}
	for i := 0; i < 10; i++ {
		log.Append([]byte{byte('0' + i)})
	}
	// After wraparound, oldest should be 6
	if log.OldestSeq() != 6 {
		t.Errorf("oldest=%d, want 6", log.OldestSeq())
	}
}

func TestOutputLog_ReplayAll(t *testing.T) {
	log := NewOutputLog(100)
	log.Append([]byte("a"))
	log.Append([]byte("b"))
	log.Append([]byte("c"))

	entries := log.ReplayAll()
	if len(entries) != 3 {
		t.Fatalf("expected 3, got %d", len(entries))
	}
}

func TestOutputLog_ConcurrentAppendReplay(t *testing.T) {
	log := NewOutputLog(1000)
	done := make(chan struct{})

	// Writer
	go func() {
		for i := 0; i < 500; i++ {
			log.Append([]byte("data"))
		}
		close(done)
	}()

	// Reader (concurrent)
	for i := 0; i < 100; i++ {
		_ = log.ReplayFrom(0)
		_ = log.CurrentSeq()
	}
	<-done
}

func TestOutputLog_TotalBytes(t *testing.T) {
	log := NewOutputLog(100)
	log.Append([]byte("hello")) // 5 bytes
	log.Append([]byte("world")) // 5 bytes

	if log.TotalBytes() != 10 {
		t.Errorf("total bytes=%d, want 10", log.TotalBytes())
	}
}

func TestOutputLog_ZeroCapacity(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for zero capacity, got none")
		}
	}()
	log := NewOutputLog(0)
	log.Append([]byte("boom"))
}

// --- Boundary tests ---

func TestOutputLog_ReplayFromBoundaryAtOldestSeq(t *testing.T) {
	log := NewOutputLog(5) // cap=5
	// Append 10 entries → oldest=5, newest=9
	for i := 0; i < 10; i++ {
		log.Append([]byte{byte('0' + i)})
	}

	// ReplayFrom(5) returns entries 5-9 (all 5 entries)
	entries := log.ReplayFrom(5)
	if len(entries) != 5 {
		t.Fatalf("ReplayFrom(5): expected 5 entries, got %d", len(entries))
	}
	if entries[0].Seq != 5 || entries[4].Seq != 9 {
		t.Errorf("ReplayFrom(5): first=%d last=%d, want 5..9", entries[0].Seq, entries[4].Seq)
	}

	// ReplayFrom(4) returns nil (evicted)
	entries = log.ReplayFrom(4)
	if entries != nil {
		t.Errorf("ReplayFrom(4): expected nil (evicted), got %d entries", len(entries))
	}

	// ReplayFrom(10) returns empty slice (nothing new)
	entries = log.ReplayFrom(10)
	if entries == nil || len(entries) != 0 {
		t.Errorf("ReplayFrom(10): expected empty slice, got %v", entries)
	}
}

func TestOutputLog_EmptyLogBootstrap(t *testing.T) {
	log := NewOutputLog(100)

	// First append returns seq=0
	seq := log.Append([]byte("first"))
	if seq != 0 {
		t.Errorf("first Append returned seq=%d, want 0", seq)
	}

	// CurrentSeq returns 1 (next seq to assign)
	if log.CurrentSeq() != 1 {
		t.Errorf("CurrentSeq=%d after first append, want 1", log.CurrentSeq())
	}

	// Verify no duplicate-0 issue: ReplayFrom(0) returns exactly 1 entry
	entries := log.ReplayFrom(0)
	if len(entries) != 1 {
		t.Fatalf("ReplayFrom(0): expected 1 entry, got %d", len(entries))
	}
	if entries[0].Seq != 0 {
		t.Errorf("entry seq=%d, want 0", entries[0].Seq)
	}
}

func TestOutputLog_ReplayFromZeroOnEmptyLog(t *testing.T) {
	log := NewOutputLog(100)

	// ReplayFrom(0) on empty log returns empty slice (not nil)
	entries := log.ReplayFrom(0)
	if entries == nil {
		t.Fatal("ReplayFrom(0) on empty log returned nil, want empty slice")
	}
	if len(entries) != 0 {
		t.Fatalf("ReplayFrom(0) on empty log returned %d entries, want 0", len(entries))
	}

	// ReplayFrom(1) on empty log returns nil (requested data doesn't exist)
	entries = log.ReplayFrom(1)
	if entries != nil {
		t.Errorf("ReplayFrom(1) on empty log returned %d entries, want nil", len(entries))
	}
}

func TestOutputLogNotify_WaitForNew(t *testing.T) {
	ol := NewOutputLog(100)

	// WaitForNew should return true immediately if data already exists
	ol.Append([]byte("existing"))
	stopCh := make(chan struct{})
	if !ol.WaitForNew(0, stopCh) {
		t.Error("WaitForNew should return true when data already exists")
	}

	// WaitForNew should block until Append is called
	done := make(chan bool, 1)
	go func() {
		done <- ol.WaitForNew(1, stopCh)
	}()

	// Append should wake the waiter
	ol.Append([]byte("new data"))

	select {
	case result := <-done:
		if !result {
			t.Error("WaitForNew should return true after Append")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForNew did not return within timeout")
	}
}

func TestOutputLogNotify_WaitForNew_StopCh(t *testing.T) {
	ol := NewOutputLog(100)
	stopCh := make(chan struct{})

	done := make(chan bool, 1)
	go func() {
		done <- ol.WaitForNew(0, stopCh)
	}()

	// Close stopCh should wake the waiter and return false
	close(stopCh)

	select {
	case result := <-done:
		if result {
			t.Error("WaitForNew should return false when stopCh is closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForNew did not return within timeout after stopCh closed")
	}
}
