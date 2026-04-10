package compound

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	hash, err := FileHash(path)
	if err != nil {
		t.Fatalf("FileHash() error = %v", err)
	}

	// SHA-256 of "hello world"
	want := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if hash != want {
		t.Errorf("FileHash() = %s, want %s", hash, want)
	}
}

func TestFileHash_MissingFile(t *testing.T) {
	_, err := FileHash("/nonexistent/file")
	if err == nil {
		t.Error("FileHash() expected error for missing file")
	}
}

func TestFileHash_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatal(err)
	}

	hash, err := FileHash(path)
	if err != nil {
		t.Fatalf("FileHash() error = %v", err)
	}

	// SHA-256 of empty string
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if hash != want {
		t.Errorf("FileHash() = %s, want %s", hash, want)
	}
}
