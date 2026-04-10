package compound

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatcher_DetectsNewFileAtDeclaredPath(t *testing.T) {
	tmpDir := t.TempDir()
	relPath := filepath.Join(".claude", "settings.local.json")
	os.MkdirAll(filepath.Join(tmpDir, ".claude"), 0755)

	var callbackCount atomic.Int32
	var mu2 sync.Mutex
	var gotRelPath string

	w, err := NewWatcher(100, 5*time.Second, func(workspaceID, rp string) {
		mu2.Lock()
		gotRelPath = rp
		mu2.Unlock()
		callbackCount.Add(1)
	}, nil)
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

	w, err := NewWatcher(100, 5*time.Second, func(workspaceID, rp string) {
		callbackCount.Add(1)
	}, nil)
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
