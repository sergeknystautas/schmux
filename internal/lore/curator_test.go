package lore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildCuratorPrompt(t *testing.T) {
	files := map[string]string{
		"CLAUDE.md": "# Project\n\n## Build\ngo build",
	}
	entries := []Entry{
		{Text: "use go run ./cmd/build-dashboard", Type: "operational"},
	}
	prompt := BuildCuratorPrompt(files, entries)
	if !strings.Contains(prompt, "CLAUDE.md") {
		t.Error("prompt should contain filename")
	}
	if !strings.Contains(prompt, "go build") {
		t.Error("prompt should contain file content")
	}
	if !strings.Contains(prompt, "use go run ./cmd/build-dashboard") {
		t.Error("prompt should contain entry text")
	}
}

func TestParseCuratorResponse(t *testing.T) {
	response := `{
		"proposed_files": {"CLAUDE.md": "# Updated"},
		"diff_summary": "Added 1 item",
		"entries_used": ["entry-1"],
		"entries_discarded": {"entry-2": "already covered"}
	}`
	result, err := ParseCuratorResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProposedFiles["CLAUDE.md"] != "# Updated" {
		t.Errorf("unexpected proposed content: %s", result.ProposedFiles["CLAUDE.md"])
	}
	if result.DiffSummary != "Added 1 item" {
		t.Errorf("unexpected summary: %s", result.DiffSummary)
	}
}

func TestCurate_NoRawEntries(t *testing.T) {
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	os.MkdirAll(repoDir, 0755)
	os.WriteFile(filepath.Join(repoDir, "CLAUDE.md"), []byte("# Project"), 0644)

	// Empty lore file
	lorePath := filepath.Join(dir, "lore.jsonl")
	os.WriteFile(lorePath, []byte(""), 0644)

	c := &Curator{
		InstructionFiles: []string{"CLAUDE.md"},
	}
	proposal, err := c.Curate(context.Background(), "myrepo", repoDir, lorePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposal != nil {
		t.Error("expected nil proposal when there are no raw entries")
	}
}

func TestCurate_WithEntries(t *testing.T) {
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "repo")
	os.MkdirAll(repoDir, 0755)
	os.WriteFile(filepath.Join(repoDir, "CLAUDE.md"), []byte("# Project"), 0644)

	lorePath := filepath.Join(dir, "lore.jsonl")
	AppendEntry(lorePath, Entry{
		Timestamp: time.Now().UTC(),
		Workspace: "ws-1",
		Agent:     "claude-code",
		Type:      "operational",
		Text:      "always run tests with --race",
	})

	// Mock LLM that returns a valid curator response
	mockExecutor := func(ctx context.Context, prompt string, timeout time.Duration) (string, error) {
		resp := CuratorResponse{
			ProposedFiles:    map[string]string{"CLAUDE.md": "# Project\n\n## Testing\nRun with --race flag."},
			DiffSummary:      "Added testing section",
			EntriesUsed:      []string{"always run tests with --race"},
			EntriesDiscarded: map[string]string{},
		}
		data, _ := json.Marshal(resp)
		return string(data), nil
	}

	c := &Curator{
		InstructionFiles: []string{"CLAUDE.md"},
		Executor:         mockExecutor,
	}

	proposal, err := c.Curate(context.Background(), "myrepo", repoDir, lorePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proposal == nil {
		t.Fatal("expected non-nil proposal")
	}
	if proposal.DiffSummary != "Added testing section" {
		t.Errorf("unexpected summary: %s", proposal.DiffSummary)
	}
	if _, ok := proposal.ProposedFiles["CLAUDE.md"]; !ok {
		t.Error("expected CLAUDE.md in proposed files")
	}
	if proposal.Repo != "myrepo" {
		t.Errorf("expected repo=myrepo, got %s", proposal.Repo)
	}
}

func TestReadFileFromBareRepo(t *testing.T) {
	bareDir := initBareRepo(t) // reuse helper from apply_test.go
	content, err := ReadFileFromRepo(context.Background(), bareDir, "CLAUDE.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "# Project\n" {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestReadFileFromBareRepo_NotFound(t *testing.T) {
	bareDir := initBareRepo(t)
	_, err := ReadFileFromRepo(context.Background(), bareDir, "NONEXISTENT.md")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}
