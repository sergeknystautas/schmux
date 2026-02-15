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

func TestIsBinary(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name    string
		content []byte
		want    bool
	}{
		{
			name:    "text file",
			content: []byte("hello world\nline 2\n"),
			want:    false,
		},
		{
			name:    "binary with null byte",
			content: append([]byte("hello"), 0, 'w', 'o', 'r', 'l', 'd'),
			want:    true,
		},
		{
			name:    "empty file",
			content: []byte{},
			want:    false,
		},
		{
			name:    "json file",
			content: []byte(`{"key": "value"}`),
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.name+".bin")
			if err := os.WriteFile(path, tt.content, 0644); err != nil {
				t.Fatal(err)
			}
			if got := IsBinary(path); got != tt.want {
				t.Errorf("IsBinary() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsBinary_MissingFile(t *testing.T) {
	// Missing/unreadable files are treated as binary to prevent text merge on unknown data
	if !IsBinary("/nonexistent") {
		t.Error("IsBinary() = false for missing file, want true")
	}
}
