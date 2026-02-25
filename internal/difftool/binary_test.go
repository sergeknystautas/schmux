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
