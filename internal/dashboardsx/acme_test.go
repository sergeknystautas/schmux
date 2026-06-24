//go:build !nodashboardsx

package dashboardsx

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func TestLoadOrCreateAccount(t *testing.T) {
	tmpDir := t.TempDir()
	schmuxdir.Set(tmpDir)
	defer schmuxdir.Set("")
	store := installMemFS(t)

	// First call creates a new key
	reg1, key1, err := LoadOrCreateAccount("test@example.com")
	if err != nil {
		t.Fatalf("LoadOrCreateAccount() error: %v", err)
	}
	if reg1 != nil {
		t.Error("expected nil registration for new account")
	}
	if key1 == nil {
		t.Fatal("expected non-nil key")
	}

	// Second call loads the same key
	reg2, key2, err := LoadOrCreateAccount("test@example.com")
	if err != nil {
		t.Fatalf("LoadOrCreateAccount() second call error: %v", err)
	}
	if reg2 != nil {
		t.Error("expected nil registration for loaded account")
	}
	if key2 == nil {
		t.Fatal("expected non-nil key on second call")
	}

	// Verify account key file permissions
	f, ok := store[ACMEAccountPath()]
	if !ok {
		t.Fatal("account key not written")
	}
	if perm := f.mode.Perm(); perm != 0600 {
		t.Errorf("account key permissions = %o, want 0600", perm)
	}
}

func TestSaveCert(t *testing.T) {
	tmpDir := t.TempDir()
	schmuxdir.Set(tmpDir)
	defer schmuxdir.Set("")
	store := installMemFS(t)

	if err := SaveCert([]byte("fake-cert-data"), []byte("fake-key-data")); err != nil {
		t.Fatalf("SaveCert() error: %v", err)
	}

	// Verify files written with correct permissions
	for _, path := range []string{CertPath(), KeyPath()} {
		f, ok := store[path]
		if !ok {
			t.Fatalf("file %s not written", path)
		}
		if perm := f.mode.Perm(); perm != 0600 {
			t.Errorf("file %s permissions = %o, want 0600", path, perm)
		}
	}

	// Verify content
	if got := string(store[CertPath()].data); got != "fake-cert-data" {
		t.Errorf("cert content = %q, want %q", got, "fake-cert-data")
	}
}
