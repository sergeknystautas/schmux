package dashboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/vcs"
)

func TestBuildDiffFileContent_Modified(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.ShowFile("main.go", "HEAD"): "old\n",
	})

	oldC, newC := buildDiffFileContent(run, localReadFile(dir), cb, "main.go", "", "")
	if oldC != "old\n" || newC != "new\n" {
		t.Errorf("got old=%q new=%q", oldC, newC)
	}
}

func TestBuildDiffFileContent_AddedFileHasNoOldSide(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{}) // ShowFile errors → empty old side

	oldC, newC := buildDiffFileContent(run, localReadFile(dir), cb, "new.go", "", "")
	if oldC != "" || newC != "hello\n" {
		t.Errorf("got old=%q new=%q", oldC, newC)
	}
}

func TestBuildDiffFileContent_DeletedFileOldPathOnly(t *testing.T) {
	dir := t.TempDir()
	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.ShowFile("gone.go", "HEAD"): "was here\n",
	})

	oldC, newC := buildDiffFileContent(run, localReadFile(dir), cb, "", "gone.go", "")
	if oldC != "was here\n" || newC != "" {
		t.Errorf("got old=%q new=%q", oldC, newC)
	}
}

func TestBuildDiffFileContent_RenamedUsesOldPathForOldSide(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("moved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.ShowFile("old.go", "HEAD"): "original\n",
	})

	oldC, newC := buildDiffFileContent(run, localReadFile(dir), cb, "new.go", "old.go", "")
	if oldC != "original\n" || newC != "moved\n" {
		t.Errorf("got old=%q new=%q", oldC, newC)
	}
}

func TestBuildDiffFileContent_CommitMode(t *testing.T) {
	dir := t.TempDir()
	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.ShowFile("main.go", "abc123^"): "parent version\n",
		cb.ShowFile("main.go", "abc123"):  "commit version\n",
	})

	oldC, newC := buildDiffFileContent(run, localReadFile(dir), cb, "main.go", "", "abc123")
	if oldC != "parent version\n" || newC != "commit version\n" {
		t.Errorf("got old=%q new=%q", oldC, newC)
	}
}

func TestBuildDiffFileContent_CommitModeNullByteSniff(t *testing.T) {
	dir := t.TempDir()
	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.ShowFile("blob.bin", "abc123^"): "bin\x00ary",
		cb.ShowFile("blob.bin", "abc123"):  "bin\x00ary",
	})

	oldC, newC := buildDiffFileContent(run, localReadFile(dir), cb, "blob.bin", "", "abc123")
	if oldC != "" || newC != "" {
		t.Errorf("null-byte content should be blanked, got old=%q new=%q", oldC, newC)
	}
}

func TestStripNullByteContent(t *testing.T) {
	if got := stripNullByteContent("plain text"); got != "plain text" {
		t.Errorf("plain text should pass through, got %q", got)
	}
	if got := stripNullByteContent("has\x00null"); got != "" {
		t.Errorf("null-byte content should blank, got %q", got)
	}
	// Only the first 8KB is checked (matches old getFileAtCommit behavior).
	big := strings.Repeat("a", 9000) + "\x00"
	if got := stripNullByteContent(big); got != big {
		t.Errorf("null byte past 8KB should pass through")
	}
}
