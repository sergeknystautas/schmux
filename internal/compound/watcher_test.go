package compound

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatcher_DetectsFileChange(t *testing.T) {
	tmpDir := t.TempDir()

	// Create the file to watch
	relPath := "config.yaml"
	absPath := filepath.Join(tmpDir, relPath)
	if err := os.WriteFile(absPath, []byte("initial"), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	var callbackCount atomic.Int32
	var gotWorkspaceID, gotRelPath string

	w, err := NewWatcher(100, func(workspaceID, rp string) {
		gotWorkspaceID = workspaceID
		gotRelPath = rp
		callbackCount.Add(1)
	})
	if err != nil {
		t.Fatalf("NewWatcher() error: %v", err)
	}

	manifest := map[string]string{relPath: "abc123"}
	if err := w.AddWorkspace("ws-001", tmpDir, manifest); err != nil {
		t.Fatalf("AddWorkspace() error: %v", err)
	}

	w.Start()
	defer w.Stop()

	// Modify the watched file
	if err := os.WriteFile(absPath, []byte("modified"), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	// Poll until callback fires
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if callbackCount.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	count := callbackCount.Load()
	if count == 0 {
		t.Fatal("expected callback to fire after file change, got 0 calls")
	}

	if gotWorkspaceID != "ws-001" {
		t.Errorf("callback workspaceID = %q, want %q", gotWorkspaceID, "ws-001")
	}
	if gotRelPath != relPath {
		t.Errorf("callback relPath = %q, want %q", gotRelPath, relPath)
	}
}

func TestWatcher_DebouncesBurstWrites(t *testing.T) {
	tmpDir := t.TempDir()

	relPath := "config.yaml"
	absPath := filepath.Join(tmpDir, relPath)
	if err := os.WriteFile(absPath, []byte("initial"), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	var callbackCount atomic.Int32

	w, err := NewWatcher(200, func(workspaceID, rp string) {
		callbackCount.Add(1)
	})
	if err != nil {
		t.Fatalf("NewWatcher() error: %v", err)
	}

	manifest := map[string]string{relPath: "abc123"}
	if err := w.AddWorkspace("ws-001", tmpDir, manifest); err != nil {
		t.Fatalf("AddWorkspace() error: %v", err)
	}

	w.Start()
	defer w.Stop()

	// Fire 5 rapid writes — should debounce into 1-2 callbacks
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(absPath, []byte("burst write"), 0644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Wait for debounce to settle (200ms debounce + margin)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if callbackCount.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Wait a bit more to let any additional callbacks fire
	time.Sleep(300 * time.Millisecond)

	count := callbackCount.Load()
	if count == 0 {
		t.Fatal("expected at least 1 callback after debounce, got 0")
	}
	if count > 2 {
		// With 200ms debounce and ~100ms total event spread, we expect 1 callback.
		// Allow up to 2 for timing variance, but 5 means no debounce.
		t.Errorf("expected debounce to collapse events into 1-2 callbacks, got %d", count)
	}
}

func TestWatcher_IgnoresSuppressedFiles(t *testing.T) {
	tmpDir := t.TempDir()

	relPath := "config.yaml"
	absPath := filepath.Join(tmpDir, relPath)
	if err := os.WriteFile(absPath, []byte("initial"), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	var callbackCount atomic.Int32

	w, err := NewWatcher(100, func(workspaceID, rp string) {
		callbackCount.Add(1)
	})
	if err != nil {
		t.Fatalf("NewWatcher() error: %v", err)
	}

	manifest := map[string]string{relPath: "abc123"}
	if err := w.AddWorkspace("ws-001", tmpDir, manifest); err != nil {
		t.Fatalf("AddWorkspace() error: %v", err)
	}

	w.Start()
	defer w.Stop()

	// Suppress the file before modifying it
	w.Suppress("ws-001", relPath)

	// Modify the watched file
	if err := os.WriteFile(absPath, []byte("suppressed write"), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	// Wait long enough for debounce to fire if it were going to
	time.Sleep(500 * time.Millisecond)

	count := callbackCount.Load()
	if count != 0 {
		t.Errorf("expected 0 callbacks for suppressed file, got %d", count)
	}
}

func TestWatcher_RemoveWorkspace(t *testing.T) {
	tmpDir := t.TempDir()

	relPath := "config.yaml"
	absPath := filepath.Join(tmpDir, relPath)
	if err := os.WriteFile(absPath, []byte("initial"), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	var callbackCount atomic.Int32

	w, err := NewWatcher(100, func(workspaceID, rp string) {
		callbackCount.Add(1)
	})
	if err != nil {
		t.Fatalf("NewWatcher() error: %v", err)
	}

	manifest := map[string]string{relPath: "abc123"}
	if err := w.AddWorkspace("ws-001", tmpDir, manifest); err != nil {
		t.Fatalf("AddWorkspace() error: %v", err)
	}

	w.Start()
	defer w.Stop()

	// Remove the workspace before modifying the file
	w.RemoveWorkspace("ws-001")

	// Modify the file after workspace removal
	if err := os.WriteFile(absPath, []byte("after removal"), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	// Wait long enough for any debounce to fire if it were going to
	time.Sleep(500 * time.Millisecond)

	count := callbackCount.Load()
	if count != 0 {
		t.Errorf("expected 0 callbacks after workspace removal, got %d", count)
	}
}
