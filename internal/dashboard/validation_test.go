package dashboard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCaseSensitiveFileExists(t *testing.T) {
	dir := t.TempDir()

	// Create files with specific casing
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("readme"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Guide.md"), []byte("guide"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"exact match", "README.md", true},
		{"different case", "readme.md", false},
		{"mixed case mismatch", "GUIDE.md", false},
		{"exact match second file", "Guide.md", true},
		{"nonexistent file", "missing.md", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := caseSensitiveFileExists(dir, tt.filename)
			if got != tt.want {
				t.Errorf("caseSensitiveFileExists(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}

	t.Run("nonexistent directory returns false", func(t *testing.T) {
		got := caseSensitiveFileExists("/nonexistent/dir/12345", "file.txt")
		if got {
			t.Error("expected false for nonexistent directory")
		}
	})
}
