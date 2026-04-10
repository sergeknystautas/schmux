//go:build !nodashboardsx

package dashboardsx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func TestEnsureInstanceKey(t *testing.T) {
	// Use a temporary directory
	tmpDir := t.TempDir()
	schmuxdir.Set(tmpDir)
	defer schmuxdir.Set("")

	// Create dashboardsx dir
	if err := os.MkdirAll(filepath.Join(tmpDir, "dashboardsx"), 0700); err != nil {
		t.Fatal(err)
	}

	// Generate key
	key1, err := EnsureInstanceKey()
	if err != nil {
		t.Fatalf("EnsureInstanceKey() error: %v", err)
	}
	if len(key1) != 64 {
		t.Errorf("key length = %d, want 64", len(key1))
	}

	// Idempotent: second call returns same key
	key2, err := EnsureInstanceKey()
	if err != nil {
		t.Fatalf("EnsureInstanceKey() second call error: %v", err)
	}
	if key1 != key2 {
		t.Errorf("key changed on second call: %s != %s", key1, key2)
	}

	// Verify file permissions
	keyPath := InstanceKeyPath()
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("key file permissions = %o, want 0600", perm)
	}
}
