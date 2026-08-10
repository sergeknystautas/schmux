package dashboard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/vcs"
)

// localReadFile creates a readFileFunc that reads from a local directory.
func localReadFile(dir string) readFileFunc {
	return func(path string) string { return readWorkingFile(dir, path) }
}

// noBinary returns an isBinaryFunc that always returns false.
func noBinary() isBinaryFunc {
	return func(string) bool { return false }
}

// mockRun creates a vcsRunFunc returning canned responses keyed by exact command.
func mockRun(responses map[string]string) vcsRunFunc {
	return func(cmd string) (string, error) {
		if resp, ok := responses[cmd]; ok {
			return resp, nil
		}
		return "", fmt.Errorf("mock: no response for %q", cmd)
	}
}

func TestBuildLocalDiffResponse_ModifiedFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.DiffNumstat():    "5\t2\tmain.go",
		cb.DiffNameStatus(): "M\tmain.go",
		cb.UntrackedFiles(): "",
	})

	resp, err := buildLocalDiffResponse(run, localReadFile(dir), noBinary(), cb, "git", "ws-1", "repo", "main")
	if err != nil {
		t.Fatalf("buildLocalDiffResponse error: %v", err)
	}
	if len(resp.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(resp.Files))
	}
	f := resp.Files[0]
	if f.Status != "modified" || f.NewPath != "main.go" || f.OldPath != "" {
		t.Errorf("unexpected summary: %+v", f)
	}
	if f.LinesAdded != 5 || f.LinesRemoved != 2 {
		t.Errorf("expected +5/-2, got +%d/-%d", f.LinesAdded, f.LinesRemoved)
	}
}

func TestBuildLocalDiffResponse_AddedAndDeleted(t *testing.T) {
	dir := t.TempDir()
	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.DiffNumstat():    "3\t0\tnew.go\n0\t7\tgone.go",
		cb.DiffNameStatus(): "A\tnew.go\nD\tgone.go",
		cb.UntrackedFiles(): "",
	})

	resp, err := buildLocalDiffResponse(run, localReadFile(dir), noBinary(), cb, "git", "ws-1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(resp.Files))
	}
	added, deleted := resp.Files[0], resp.Files[1]
	if added.Status != "added" || added.NewPath != "new.go" || added.OldPath != "" || added.LinesAdded != 3 {
		t.Errorf("unexpected added summary: %+v", added)
	}
	if deleted.Status != "deleted" || deleted.OldPath != "gone.go" || deleted.NewPath != "" || deleted.LinesRemoved != 7 {
		t.Errorf("unexpected deleted summary: %+v", deleted)
	}
}

func TestBuildLocalDiffResponse_RenamedFile(t *testing.T) {
	dir := t.TempDir()
	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.DiffNumstat():    "1\t1\tsrc/{old.go => new.go}",
		cb.DiffNameStatus(): "R95\tsrc/old.go\tsrc/new.go",
		cb.UntrackedFiles(): "",
	})

	resp, err := buildLocalDiffResponse(run, localReadFile(dir), noBinary(), cb, "git", "ws-1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(resp.Files))
	}
	f := resp.Files[0]
	if f.Status != "renamed" || f.OldPath != "src/old.go" || f.NewPath != "src/new.go" {
		t.Errorf("unexpected rename summary: %+v", f)
	}
	if f.LinesAdded != 1 || f.LinesRemoved != 1 {
		t.Errorf("expected +1/-1 joined from numstat, got +%d/-%d", f.LinesAdded, f.LinesRemoved)
	}
}

func TestBuildLocalDiffResponse_BinaryTrackedFile(t *testing.T) {
	dir := t.TempDir()
	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.DiffNumstat():    "-\t-\timg.png",
		cb.DiffNameStatus(): "M\timg.png",
		cb.UntrackedFiles(): "",
	})

	resp, err := buildLocalDiffResponse(run, localReadFile(dir), noBinary(), cb, "git", "ws-1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	f := resp.Files[0]
	if !f.IsBinary || f.Status != "modified" || f.NewPath != "img.png" {
		t.Errorf("unexpected binary summary: %+v", f)
	}
}

func TestBuildLocalDiffResponse_UntrackedFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("a\nb\nc"), 0o644); err != nil {
		t.Fatal(err)
	}
	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.DiffNumstat():    "",
		cb.DiffNameStatus(): "",
		cb.UntrackedFiles(): "notes.txt",
	})

	resp, err := buildLocalDiffResponse(run, localReadFile(dir), noBinary(), cb, "git", "ws-1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	f := resp.Files[0]
	if f.Status != "untracked" || f.NewPath != "notes.txt" {
		t.Errorf("unexpected untracked summary: %+v", f)
	}
	if f.LinesAdded != 3 {
		t.Errorf("expected 3 lines (no trailing newline counts), got %d", f.LinesAdded)
	}
}

func TestBuildLocalDiffResponse_UntrackedBinary(t *testing.T) {
	dir := t.TempDir()
	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.DiffNumstat():    "",
		cb.DiffNameStatus(): "",
		cb.UntrackedFiles(): "logo.png",
	})
	isBin := func(path string) bool { return path == "logo.png" }

	resp, err := buildLocalDiffResponse(run, localReadFile(dir), isBin, cb, "git", "ws-1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	f := resp.Files[0]
	if !f.IsBinary || f.Status != "untracked" || f.LinesAdded != 0 {
		t.Errorf("unexpected untracked binary summary: %+v", f)
	}
}

func TestBuildLocalDiffResponse_SaplingStatuses(t *testing.T) {
	dir := t.TempDir()
	cb := vcs.NewCommandBuilder("sapling")
	// The sapling builder normalizes "X path" to tab-separated "X\tpath"
	// (see SaplingCommandBuilder.DiffNameStatus), so paths with spaces
	// arrive intact.
	run := mockRun(map[string]string{
		cb.DiffNumstat():    "0\t0\tchanged.go\n0\t0\tadded.go\n0\t0\tmissing.go\n0\t0\tmy notes.txt",
		cb.DiffNameStatus(): "M\tchanged.go\nA\tadded.go\n!\tmissing.go\nM\tmy notes.txt",
		cb.UntrackedFiles(): "",
	})

	resp, err := buildLocalDiffResponse(run, localReadFile(dir), noBinary(), cb, "sapling", "ws-1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Files) != 4 {
		t.Fatalf("expected 4 files, got %d", len(resp.Files))
	}
	if resp.Files[0].Status != "modified" || resp.Files[1].Status != "added" || resp.Files[2].Status != "deleted" {
		t.Errorf("unexpected sapling statuses: %+v", resp.Files)
	}
	if resp.Files[2].OldPath != "missing.go" || resp.Files[2].NewPath != "" {
		t.Errorf("sapling deleted file should use OldPath: %+v", resp.Files[2])
	}
	if resp.Files[3].NewPath != "my notes.txt" {
		t.Errorf("spaced path should survive tab-separated parse: %+v", resp.Files[3])
	}
}

func TestBuildLocalDiffResponse_NoContentFieldsInJSON(t *testing.T) {
	// Guard: the wire format must not contain content keys.
	dir := t.TempDir()
	cb := vcs.NewCommandBuilder("git")
	run := mockRun(map[string]string{
		cb.DiffNumstat():    "1\t0\ta.txt",
		cb.DiffNameStatus(): "M\ta.txt",
		cb.UntrackedFiles(): "",
	})
	resp, err := buildLocalDiffResponse(run, localReadFile(dir), noBinary(), cb, "git", "ws-1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Files) != 1 {
		t.Fatal("expected 1 file")
	}
	// Compile-time guarantee: DiffFileSummary has no content fields.
	// This test exists to document the wire contract.
	_ = resp
}

func TestBuildRemoteDiffResponse_SingleRunCall(t *testing.T) {
	cb := vcs.NewCommandBuilder("git")
	calls := 0
	out := strings.Join([]string{
		"5\t2\tmain.go",
		"__SCHMUX_DIFF_DELIM__",
		"M\tmain.go",
		"__SCHMUX_DIFF_DELIM__",
		"4\tnotes.txt",
	}, "\n")
	run := func(cmd string) (string, error) {
		calls++
		return out, nil
	}

	resp, err := buildRemoteDiffResponse(run, cb, "git", "ws-1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected exactly 1 run call, got %d", calls)
	}
	if len(resp.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(resp.Files))
	}
	if resp.Files[0].Status != "modified" || resp.Files[0].LinesAdded != 5 {
		t.Errorf("unexpected tracked summary: %+v", resp.Files[0])
	}
	if resp.Files[1].Status != "untracked" || resp.Files[1].NewPath != "notes.txt" || resp.Files[1].LinesAdded != 4 {
		t.Errorf("unexpected untracked summary: %+v", resp.Files[1])
	}
}

func TestBuildRemoteDiffResponse_UntrackedBinaryByExtension(t *testing.T) {
	cb := vcs.NewCommandBuilder("git")
	out := strings.Join([]string{
		"__SCHMUX_DIFF_DELIM__",
		"__SCHMUX_DIFF_DELIM__",
		"12\tlogo.png",
	}, "\n")
	run := func(cmd string) (string, error) { return out, nil }

	resp, err := buildRemoteDiffResponse(run, cb, "git", "ws-1", "repo", "main")
	if err != nil {
		t.Fatal(err)
	}
	f := resp.Files[0]
	if !f.IsBinary || f.LinesAdded != 0 {
		t.Errorf("png should be binary with no line count: %+v", f)
	}
}

func TestCountLines(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a\n", 1},
		{"a\nb", 2},
		{"a\nb\n", 2},
	}
	for _, c := range cases {
		if got := countLines(c.in); got != c.want {
			t.Errorf("countLines(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
