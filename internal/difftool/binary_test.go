package difftool

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsBinaryHeuristic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content []byte
		want    bool
	}{
		{
			name:    "text file is not binary",
			content: []byte("hello world\nthis is a text file\n"),
			want:    false,
		},
		{
			name:    "file with null byte is binary",
			content: []byte("hello\x00world"),
			want:    true,
		},
		{
			name:    "null byte at start",
			content: []byte("\x00rest of content"),
			want:    true,
		},
		{
			name:    "empty file is not binary",
			content: []byte{},
			want:    false,
		},
		{
			name:    "file with only newlines",
			content: []byte("\n\n\n"),
			want:    false,
		},
		{
			name:    "file with high bytes but no nulls is not binary",
			content: []byte("café résumé naïve"),
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "testfile")
			if err := os.WriteFile(path, tt.content, 0644); err != nil {
				t.Fatal(err)
			}
			got := isBinaryHeuristic(path)
			if got != tt.want {
				t.Errorf("isBinaryHeuristic() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsBinaryHeuristic_NonexistentFile(t *testing.T) {
	t.Parallel()
	got := isBinaryHeuristic("/nonexistent/file.bin")
	if got {
		t.Error("expected false for nonexistent file")
	}
}

func TestIsBinaryFile(t *testing.T) {
	t.Parallel()
	// filePath is relative to repoDir — the process cwd is elsewhere, so the
	// join inside IsBinaryFile is what makes the file readable at all.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sound.ogg"), []byte("OggS\x00\x02binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("plain text\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !IsBinaryFile(dir, "sound.ogg") {
		t.Error("expected binary for file with null byte")
	}
	if IsBinaryFile(dir, "notes.txt") {
		t.Error("expected not binary for text file")
	}
	if IsBinaryFile(dir, "missing.bin") {
		t.Error("expected not binary for nonexistent file")
	}
}
