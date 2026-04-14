//go:build !norepofeed

package repofeed

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDismissedStore_DismissAndCheck(t *testing.T) {
	dir := t.TempDir()
	ds := &DismissedStore{
		dismissed: make(map[string]bool),
		path:      filepath.Join(dir, "dismissed.json"),
	}

	// Not dismissed initially
	if ds.IsDismissed("alice@example.com", "ws-001") {
		t.Fatal("should not be dismissed initially")
	}

	// Dismiss
	ds.Dismiss("alice@example.com", "ws-001")
	if !ds.IsDismissed("alice@example.com", "ws-001") {
		t.Fatal("should be dismissed after Dismiss")
	}

	// Different developer/workspace not dismissed
	if ds.IsDismissed("bob@example.com", "ws-001") {
		t.Fatal("different developer should not be dismissed")
	}
	if ds.IsDismissed("alice@example.com", "ws-002") {
		t.Fatal("different workspace should not be dismissed")
	}
}

func TestDismissedStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dismissed.json")

	// Write
	ds1 := &DismissedStore{
		dismissed: make(map[string]bool),
		path:      path,
	}
	ds1.Dismiss("alice@example.com", "ws-001")
	ds1.Dismiss("bob@example.com", "ws-002")

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}

	// Load from disk
	ds2 := &DismissedStore{
		dismissed: make(map[string]bool),
		path:      path,
	}
	ds2.load()

	if !ds2.IsDismissed("alice@example.com", "ws-001") {
		t.Error("alice:ws-001 should be dismissed after reload")
	}
	if !ds2.IsDismissed("bob@example.com", "ws-002") {
		t.Error("bob:ws-002 should be dismissed after reload")
	}
	if ds2.IsDismissed("carol@example.com", "ws-003") {
		t.Error("carol:ws-003 should not be dismissed")
	}
}

func TestDismissedStore_KeysAreHashed(t *testing.T) {
	dir := t.TempDir()
	ds := &DismissedStore{
		dismissed: make(map[string]bool),
		path:      filepath.Join(dir, "dismissed.json"),
	}

	ds.Dismiss("alice@example.com", "ws-001")

	// The internal key should be a hex hash, not the raw developer:workspace string
	for key := range ds.dismissed {
		if key == "alice@example.com:ws-001" {
			t.Error("key should be hashed, not raw")
		}
		if len(key) != 16 { // sha256[:8] = 16 hex chars
			t.Errorf("key length = %d, want 16 hex chars", len(key))
		}
	}
}
