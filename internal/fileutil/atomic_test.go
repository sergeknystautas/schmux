package fileutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFile_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	data := []byte(`{"key": "value"}`)
	if err := AtomicWriteFile(path, data, 0600); err != nil {
		t.Fatalf("AtomicWriteFile failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("content = %q, want %q", got, data)
	}
}

func TestAtomicWriteFile_Permissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.json")

	if err := AtomicWriteFile(path, []byte("secret"), 0600); err != nil {
		t.Fatalf("AtomicWriteFile failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("permissions = %o, want %o", perm, 0600)
	}
}

func TestAtomicWriteFile_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	if err := AtomicWriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatalf("first write failed: %v", err)
	}

	if err := AtomicWriteFile(path, []byte("new"), 0600); err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != "new" {
		t.Errorf("content = %q, want %q", got, "new")
	}
}

func TestAtomicWriteFile_NoTempFileLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")

	if err := AtomicWriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatalf("AtomicWriteFile failed: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("expected 1 file, got %d: %v", len(entries), names)
	}
}

func TestAtomicWriteFile_InvalidDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent", "subdir", "test.json")

	err := AtomicWriteFile(path, []byte("data"), 0600)
	if err == nil {
		t.Fatal("expected error for nonexistent directory, got nil")
	}
}
