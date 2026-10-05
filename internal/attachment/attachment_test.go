package attachment

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
)

func TestValidName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"data.csv", true},
		{"résumé data.bin", true},
		{"", false},
		{".", false},
		{"..", false},
		{"../escape", false},
		{"/tmp/escape", false},
		{`dir\escape`, false},
		{"line\nbreak", false},
		{"null\x00byte", false},
		{strings.Repeat("a", 256), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidName(tt.name); got != tt.want {
				t.Fatalf("ValidName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestSaveWritesEachFileInItsOwnDirectory(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	first, err := Save(root, filepath.Join(".schmux", "attachments"), "notes.txt", strings.NewReader("one"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Save(root, filepath.Join(".schmux", "attachments"), "notes.txt", strings.NewReader("two"))
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("same name produced the same path %q", first)
	}
	for rel, want := range map[string]string{first: "one", second: "two"} {
		if filepath.Base(rel) != "notes.txt" || !strings.HasPrefix(rel, filepath.Join(".schmux", "attachments")+string(filepath.Separator)) {
			t.Fatalf("unexpected relative path %q", rel)
		}
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil || string(data) != want {
			t.Fatalf("%s = %q, %v; want %q", rel, data, err, want)
		}
		info, err := os.Stat(filepath.Join(dir, rel))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v, %v; want 0600", rel, info.Mode().Perm(), err)
		}
	}
}

func TestSaveRemovesItsDirectoryWhenTheSourceFails(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	_, err = Save(root, "staged", "data.csv", iotest.ErrReader(errors.New("connection reset")))
	if !errors.Is(err, ErrReceive) {
		t.Fatalf("err = %v, want ErrReceive", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "staged"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed save left %v (err %v)", entries, err)
	}
}

func TestSaveRejectsInvalidName(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := Save(root, ".", "../escape", bytes.NewReader(nil)); err == nil {
		t.Fatal("Save accepted ../escape")
	}
}

func TestSaveRejectsEscapingSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, ".schmux")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	if _, err := Save(root, filepath.Join(".schmux", "attachments"), "data.csv", strings.NewReader("x")); err == nil {
		t.Fatal("Save followed a symlink out of the root")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("wrote outside root: %v, %v", entries, err)
	}
}

// The literal below is pinned identically in
// assets/dashboard/src/lib/attachments.test.ts so the Go and TypeScript
// writers of this block cannot drift.
func TestAppendFileList(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		paths  []string
		want   string
	}{
		{"with prompt", "do it", []string{"/a/b.csv", "/c/d.txt"}, "do it\n\nFile attachments:\n/a/b.csv\n/c/d.txt"},
		{"empty prompt", "", []string{"/a/b.csv"}, "File attachments:\n/a/b.csv"},
		{"no paths", "do it", nil, "do it"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AppendFileList(tt.prompt, tt.paths); got != tt.want {
				t.Fatalf("AppendFileList() = %q, want %q", got, tt.want)
			}
		})
	}
}
