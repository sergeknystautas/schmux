//go:build !nodashboardsx

package dashboardsx

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func TestEnsureInstanceKey(t *testing.T) {
	tmpDir := t.TempDir()
	schmuxdir.Set(tmpDir)
	defer schmuxdir.Set("")
	store := installMemFS(t)

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
	f, ok := store[InstanceKeyPath()]
	if !ok {
		t.Fatal("instance key not written")
	}
	if perm := f.mode.Perm(); perm != 0600 {
		t.Errorf("key file permissions = %o, want 0600", perm)
	}
}
