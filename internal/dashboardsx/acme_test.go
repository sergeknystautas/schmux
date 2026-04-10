//go:build !nodashboardsx

package dashboardsx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func TestLoadOrCreateAccount(t *testing.T) {
	// Use a temporary directory
	tmpDir := t.TempDir()
	schmuxdir.Set(tmpDir)
	defer schmuxdir.Set("")

	// Create dashboardsx dir
	if err := os.MkdirAll(filepath.Join(tmpDir, "dashboardsx"), 0700); err != nil {
		t.Fatal(err)
	}

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
	accountPath := ACMEAccountPath()
	info, err := os.Stat(accountPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("account key permissions = %o, want 0600", perm)
	}
}

func TestSaveCert(t *testing.T) {
	tmpDir := t.TempDir()
	schmuxdir.Set(tmpDir)
	defer schmuxdir.Set("")

	if err := os.MkdirAll(filepath.Join(tmpDir, "dashboardsx"), 0700); err != nil {
		t.Fatal(err)
	}

	certData := []byte("fake-cert-data")
	keyData := []byte("fake-key-data")

	if err := SaveCert(certData, keyData); err != nil {
		t.Fatalf("SaveCert() error: %v", err)
	}

	// Verify files exist and have correct permissions
	certPath := CertPath()
	keyPath := KeyPath()

	for _, path := range []string{certPath, keyPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("file %s not found: %v", path, err)
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Errorf("file %s permissions = %o, want 0600", path, perm)
		}
	}

	// Verify content
	data, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "fake-cert-data" {
		t.Errorf("cert content = %q, want %q", string(data), "fake-cert-data")
	}
}
