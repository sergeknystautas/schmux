package signal

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestFileWatcherDetectsWrite(t *testing.T) {
	dir := t.TempDir()
	signalFile := filepath.Join(dir, "signal")

	var got []Signal
	var mu sync.Mutex
	fw, err := NewFileWatcher("test-session", signalFile, func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Stop()

	os.WriteFile(signalFile, []byte("completed Task done\n"), 0644)

	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for signal")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if got[0].State != "completed" {
		t.Errorf("State = %q, want %q", got[0].State, "completed")
	}
	if got[0].Message != "Task done" {
		t.Errorf("Message = %q, want %q", got[0].Message, "Task done")
	}
}

func TestFileWatcherDeduplicates(t *testing.T) {
	dir := t.TempDir()
	signalFile := filepath.Join(dir, "signal")

	var got []Signal
	var mu sync.Mutex
	fw, err := NewFileWatcher("test-session", signalFile, func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Stop()

	os.WriteFile(signalFile, []byte("completed Done\n"), 0644)
	time.Sleep(300 * time.Millisecond)
	os.WriteFile(signalFile, []byte("completed Done\n"), 0644)
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Errorf("expected 1 signal (dedup), got %d", len(got))
	}
}

func TestFileWatcherDifferentSignals(t *testing.T) {
	dir := t.TempDir()
	signalFile := filepath.Join(dir, "signal")

	var got []Signal
	var mu sync.Mutex
	fw, err := NewFileWatcher("test-session", signalFile, func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Stop()

	os.WriteFile(signalFile, []byte("completed Done\n"), 0644)
	time.Sleep(300 * time.Millisecond)
	os.WriteFile(signalFile, []byte("needs_input What next?\n"), 0644)

	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 2 {
			break
		}
		select {
		case <-deadline:
			mu.Lock()
			n := len(got)
			mu.Unlock()
			t.Fatalf("timed out, got %d signals", n)
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if got[0].State != "completed" {
		t.Errorf("first State = %q, want completed", got[0].State)
	}
	if got[1].State != "needs_input" {
		t.Errorf("second State = %q, want needs_input", got[1].State)
	}
}

func TestFileWatcherInvalidContent(t *testing.T) {
	dir := t.TempDir()
	signalFile := filepath.Join(dir, "signal")

	var got []Signal
	var mu sync.Mutex
	fw, err := NewFileWatcher("test-session", signalFile, func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Stop()

	os.WriteFile(signalFile, []byte("banana nonsense\n"), 0644)
	time.Sleep(300 * time.Millisecond)
	os.WriteFile(signalFile, []byte("completed Done\n"), 0644)

	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for signal")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Errorf("expected 1 signal, got %d", len(got))
	}
	if got[0].State != "completed" {
		t.Errorf("State = %q, want completed", got[0].State)
	}
}

func TestFileWatcherReadCurrent(t *testing.T) {
	dir := t.TempDir()
	signalFile := filepath.Join(dir, "signal")

	os.WriteFile(signalFile, []byte("needs_input Waiting\n"), 0644)

	fw, err := NewFileWatcher("test-session", signalFile, func(sig Signal) {})
	if err != nil {
		t.Fatal(err)
	}
	defer fw.Stop()

	sig := fw.ReadCurrent()
	if sig == nil {
		t.Fatal("ReadCurrent returned nil")
	}
	if sig.State != "needs_input" {
		t.Errorf("State = %q, want needs_input", sig.State)
	}
}
