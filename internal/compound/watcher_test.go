package compound

import (
	"os"
	"path/filepath"
	"sync"
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
	var mu sync.Mutex
	var gotWorkspaceID, gotRelPath string

	w, err := NewWatcher(100, func(workspaceID, rp string) {
		mu.Lock()
		gotWorkspaceID = workspaceID
		gotRelPath = rp
		mu.Unlock()
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

	mu.Lock()
	wsID := gotWorkspaceID
	rp := gotRelPath
	mu.Unlock()

	if wsID != "ws-001" {
		t.Errorf("callback workspaceID = %q, want %q", wsID, "ws-001")
	}
	if rp != relPath {
		t.Errorf("callback relPath = %q, want %q", rp, relPath)
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

func TestWatcher_DetectsNewFileAtDeclaredPath(t *testing.T) {
	tmpDir := t.TempDir()
	relPath := filepath.Join(".claude", "settings.local.json")
	os.MkdirAll(filepath.Join(tmpDir, ".claude"), 0755)

	var callbackCount atomic.Int32
	var mu2 sync.Mutex
	var gotRelPath string

	w, err := NewWatcher(100, func(workspaceID, rp string) {
		mu2.Lock()
		gotRelPath = rp
		mu2.Unlock()
		callbackCount.Add(1)
	})
	if err != nil {
		t.Fatalf("NewWatcher() error: %v", err)
	}

	manifest := map[string]string{}
	declaredPaths := []string{relPath}
	if err := w.AddWorkspaceWithDeclaredPaths("ws-001", tmpDir, manifest, declaredPaths); err != nil {
		t.Fatalf("AddWorkspaceWithDeclaredPaths() error: %v", err)
	}

	w.Start()
	defer w.Stop()

	os.WriteFile(filepath.Join(tmpDir, relPath), []byte(`{"key": "value"}`), 0644)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if callbackCount.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if callbackCount.Load() == 0 {
		t.Fatal("expected callback for newly created file at declared path")
	}
	mu2.Lock()
	rp := gotRelPath
	mu2.Unlock()
	if rp != relPath {
		t.Errorf("callback relPath = %q, want %q", rp, relPath)
	}
}

func TestWatcher_WatchesParentDirCreation(t *testing.T) {
	tmpDir := t.TempDir()
	relPath := filepath.Join(".newdir", "config.json")

	var callbackCount atomic.Int32

	w, err := NewWatcher(100, func(workspaceID, rp string) {
		callbackCount.Add(1)
	})
	if err != nil {
		t.Fatalf("NewWatcher() error: %v", err)
	}

	manifest := map[string]string{}
	declaredPaths := []string{relPath}
	if err := w.AddWorkspaceWithDeclaredPaths("ws-001", tmpDir, manifest, declaredPaths); err != nil {
		t.Fatalf("AddWorkspaceWithDeclaredPaths() error: %v", err)
	}

	w.Start()
	defer w.Stop()

	// Create the parent directory (should trigger retryPendingDirs)
	os.MkdirAll(filepath.Join(tmpDir, ".newdir"), 0755)
	time.Sleep(200 * time.Millisecond) // let fsnotify detect dir creation

	// Now create the file
	os.WriteFile(filepath.Join(tmpDir, relPath), []byte("content"), 0644)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if callbackCount.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if callbackCount.Load() == 0 {
		t.Fatal("expected callback for file at declared path after parent dir creation")
	}
}
