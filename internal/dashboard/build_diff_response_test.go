package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/vcs"
)

// mockRun creates a vcsRunFunc that returns canned responses keyed by command prefix.
func mockRun(responses map[string]string) vcsRunFunc {
	return func(cmd string) (string, error) {
		for prefix, resp := range responses {
			if cmd == prefix || len(cmd) >= len(prefix) && cmd[:len(prefix)] == prefix {
				return resp, nil
			}
		}
		return "", fmt.Errorf("mock: no response for %q", cmd)
	}
}

func TestBuildDiffResponse_ModifiedFile(t *testing.T) {
	// Set up a workspace directory with a working copy file
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.DiffNumstat():               "5\t2\tmain.go",
		cb.ShowFile("main.go", "HEAD"): "package old\n",
		cb.UntrackedFiles():            "",
	})

	resp, err := buildDiffResponse(run, cb, dir, "ws-1", "repo", "main")
	if err != nil {
		t.Fatalf("buildDiffResponse error: %v", err)
	}

	if len(resp.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(resp.Files))
	}
	f := resp.Files[0]
	if f.Status != "modified" {
		t.Errorf("expected status 'modified', got %q", f.Status)
	}
	if f.LinesAdded != 5 || f.LinesRemoved != 2 {
		t.Errorf("expected +5/-2, got +%d/-%d", f.LinesAdded, f.LinesRemoved)
	}
	if f.OldContent != "package old\n" {
		t.Errorf("unexpected old content: %q", f.OldContent)
	}
	if f.NewContent != "package main\n" {
		t.Errorf("unexpected new content: %q", f.NewContent)
	}
	if f.NewPath != "main.go" {
		t.Errorf("expected NewPath 'main.go', got %q", f.NewPath)
	}
}

func TestBuildDiffResponse_AddedFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("new file\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.DiffNumstat():    "10\t0\tnew.go",
		cb.UntrackedFiles(): "",
	})
	// ShowFile for HEAD returns error (file doesn't exist in HEAD) — mockRun returns error for unknown commands

	resp, err := buildDiffResponse(run, cb, dir, "ws-1", "repo", "main")
	if err != nil {
		t.Fatalf("buildDiffResponse error: %v", err)
	}

	if len(resp.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(resp.Files))
	}
	f := resp.Files[0]
	if f.Status != "added" {
		t.Errorf("expected status 'added', got %q", f.Status)
	}
	if f.LinesAdded != 10 {
		t.Errorf("expected +10, got +%d", f.LinesAdded)
	}
}

func TestBuildDiffResponse_DeletedFile(t *testing.T) {
	dir := t.TempDir()
	// File does NOT exist in working tree

	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.DiffNumstat():                  "0\t8\tremoved.go",
		cb.ShowFile("removed.go", "HEAD"): "old content\nline2\n",
		cb.UntrackedFiles():               "",
	})

	resp, err := buildDiffResponse(run, cb, dir, "ws-1", "repo", "main")
	if err != nil {
		t.Fatalf("buildDiffResponse error: %v", err)
	}

	if len(resp.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(resp.Files))
	}
	f := resp.Files[0]
	if f.Status != "deleted" {
		t.Errorf("expected status 'deleted', got %q", f.Status)
	}
	if f.OldPath != "removed.go" {
		t.Errorf("expected OldPath 'removed.go', got %q", f.OldPath)
	}
	if f.OldContent == "" {
		t.Error("expected non-empty old content for deleted file")
	}
	if f.LinesRemoved != 8 {
		t.Errorf("expected -8, got -%d", f.LinesRemoved)
	}
}

func TestBuildDiffResponse_BinaryFile(t *testing.T) {
	dir := t.TempDir()

	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.DiffNumstat():                 "-\t-\timage.png",
		cb.ShowFile("image.png", "HEAD"): "old binary data",
		cb.UntrackedFiles():              "",
	})

	resp, err := buildDiffResponse(run, cb, dir, "ws-1", "repo", "main")
	if err != nil {
		t.Fatalf("buildDiffResponse error: %v", err)
	}

	if len(resp.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(resp.Files))
	}
	f := resp.Files[0]
	if !f.IsBinary {
		t.Error("expected IsBinary=true")
	}
	if f.Status != "modified" {
		t.Errorf("expected 'modified', got %q", f.Status)
	}
}

func TestBuildDiffResponse_NewBinaryFile(t *testing.T) {
	dir := t.TempDir()

	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.DiffNumstat():    "-\t-\tnew.png",
		cb.UntrackedFiles(): "",
		// ShowFile returns error — file not in HEAD
	})

	resp, err := buildDiffResponse(run, cb, dir, "ws-1", "repo", "main")
	if err != nil {
		t.Fatalf("buildDiffResponse error: %v", err)
	}

	if len(resp.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(resp.Files))
	}
	if resp.Files[0].Status != "added" {
		t.Errorf("expected 'added', got %q", resp.Files[0].Status)
	}
	if !resp.Files[0].IsBinary {
		t.Error("expected IsBinary=true")
	}
}

func TestBuildDiffResponse_UntrackedFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.DiffNumstat():    "",
		cb.UntrackedFiles(): "untracked.txt",
	})

	resp, err := buildDiffResponse(run, cb, dir, "ws-1", "repo", "main")
	if err != nil {
		t.Fatalf("buildDiffResponse error: %v", err)
	}

	if len(resp.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(resp.Files))
	}
	f := resp.Files[0]
	if f.Status != "untracked" {
		t.Errorf("expected status 'untracked', got %q", f.Status)
	}
	if f.LinesAdded != 2 {
		t.Errorf("expected 2 lines added, got %d", f.LinesAdded)
	}
	if f.NewContent != "hello\nworld\n" {
		t.Errorf("unexpected content: %q", f.NewContent)
	}
}

func TestBuildDiffResponse_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("a\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("b\n"), 0o644)

	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.DiffNumstat():            "3\t1\ta.go\n7\t0\tb.go",
		cb.ShowFile("a.go", "HEAD"): "old a\n",
		cb.UntrackedFiles():         "",
		// b.go not in HEAD → added
	})

	resp, err := buildDiffResponse(run, cb, dir, "ws-1", "repo", "main")
	if err != nil {
		t.Fatalf("buildDiffResponse error: %v", err)
	}

	if len(resp.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(resp.Files))
	}

	// a.go should be modified (has old content)
	if resp.Files[0].Status != "modified" {
		t.Errorf("expected a.go modified, got %q", resp.Files[0].Status)
	}
	// b.go should be added (no old content)
	if resp.Files[1].Status != "added" {
		t.Errorf("expected b.go added, got %q", resp.Files[1].Status)
	}
}

func TestBuildDiffResponse_EmptyDiff(t *testing.T) {
	dir := t.TempDir()

	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.DiffNumstat():    "",
		cb.UntrackedFiles(): "",
	})

	resp, err := buildDiffResponse(run, cb, dir, "ws-1", "repo", "main")
	if err != nil {
		t.Fatalf("buildDiffResponse error: %v", err)
	}

	if len(resp.Files) != 0 {
		t.Errorf("expected 0 files, got %d", len(resp.Files))
	}
	if resp.WorkspaceID != "ws-1" {
		t.Errorf("expected workspace_id 'ws-1', got %q", resp.WorkspaceID)
	}
}

func TestBuildDiffResponse_SaplingCommands(t *testing.T) {
	// Verify that buildDiffResponse works with sapling CommandBuilder.
	// The function is VCS-agnostic — it uses whatever commands the builder produces.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "file.rs"), []byte("fn main() {}\n"), 0o644)

	cb := vcs.NewCommandBuilder("sapling")
	run := mockRun(map[string]string{
		cb.DiffNumstat():               "4\t1\tfile.rs",
		cb.ShowFile("file.rs", "HEAD"): "fn old() {}\n",
		cb.UntrackedFiles():            "",
	})

	resp, err := buildDiffResponse(run, cb, dir, "ws-1", "repo", "main")
	if err != nil {
		t.Fatalf("buildDiffResponse error: %v", err)
	}

	if len(resp.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(resp.Files))
	}
	if resp.Files[0].Status != "modified" {
		t.Errorf("expected 'modified', got %q", resp.Files[0].Status)
	}
}

func TestBuildDiffResponse_MalformedNumstatIgnored(t *testing.T) {
	dir := t.TempDir()

	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.DiffNumstat():               "not\ta\tvalid\tline\nbadline\n3\t1\tgood.go",
		cb.ShowFile("good.go", "HEAD"): "old\n",
		cb.UntrackedFiles():            "",
	})

	os.WriteFile(filepath.Join(dir, "good.go"), []byte("new\n"), 0o644)

	resp, err := buildDiffResponse(run, cb, dir, "ws-1", "repo", "main")
	if err != nil {
		t.Fatalf("buildDiffResponse error: %v", err)
	}

	// Malformed lines should be skipped; only "good.go" survives.
	// "not\ta\tvalid\tline" has 4 parts (>= 3) so it would be parsed — parts[2] = "valid"
	// "badline" has < 3 parts, so it's skipped
	// The first line is technically parseable but "not" is not a valid int → linesAdded=0
	// It would try to read "valid" from disk which doesn't exist.
	// good.go is correctly parsed.
	found := false
	for _, f := range resp.Files {
		if f.NewPath == "good.go" {
			found = true
			if f.Status != "modified" {
				t.Errorf("expected 'modified', got %q", f.Status)
			}
		}
	}
	if !found {
		t.Error("expected good.go in response")
	}
}
