package lore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/schema"
)

func TestBuildCuratorPrompt(t *testing.T) {
	files := map[string]string{
		"CLAUDE.md": "# Project\n\n## Build\ngo build",
	}
	entries := []Entry{
		{Text: "use go run ./cmd/build-dashboard", Type: "reflection", Agent: "claude-code"},
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
		Type:      "reflection",
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
	if proposal.CurrentFiles["CLAUDE.md"] != "# Project" {
		t.Errorf("expected current file content '# Project', got %q", proposal.CurrentFiles["CLAUDE.md"])
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

func TestCuratorResponseSchemaRegistered(t *testing.T) {
	// Verify the lore-curator schema is registered (via init()) and produces valid JSON
	schemaJSON, err := schema.Get(schema.LabelLoreCurator)
	if err != nil {
		t.Fatalf("lore-curator schema not registered: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(schemaJSON), &parsed); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	if parsed["type"] != "object" {
		t.Errorf("expected schema type=object, got %v", parsed["type"])
	}

	// Verify all CuratorResponse fields appear in the schema properties
	props, ok := parsed["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties to be an object")
	}
	for _, field := range []string{"proposed_files", "diff_summary", "entries_used", "entries_discarded"} {
		if _, exists := props[field]; !exists {
			t.Errorf("expected property %q in schema", field)
		}
	}
}

func TestBuildCuratorPrompt_SeparatesFailuresAndReflections(t *testing.T) {
	files := map[string]string{
		"CLAUDE.md": "# Project\n\n## Build\ngo build",
	}
	entries := []Entry{
		{Agent: "claude-code", Type: "failure", Tool: "Bash", InputSummary: "npm run build", ErrorSummary: "Missing script", Category: "wrong_command", Workspace: "ws-1"},
		{Agent: "claude-code", Type: "reflection", Text: "Use go run ./cmd/build-dashboard", Workspace: "ws-1"},
		{Agent: "codex", Type: "friction", Text: "Session manager is in internal/session/", Workspace: "ws-2"},
	}
	prompt := BuildCuratorPrompt(files, entries)

	// Should have separate sections
	if !strings.Contains(prompt, "FAILURE RECORDS:") {
		t.Error("prompt should contain FAILURE RECORDS section")
	}
	if !strings.Contains(prompt, "FRICTION REFLECTIONS:") {
		t.Error("prompt should contain FRICTION REFLECTIONS section")
	}

	// Failures should be formatted with tool and category
	if !strings.Contains(prompt, "[Bash]") {
		t.Error("prompt should contain tool name in failure record")
	}
	if !strings.Contains(prompt, "[wrong_command]") {
		t.Error("prompt should contain category in failure record")
	}
	if !strings.Contains(prompt, "npm run build") {
		t.Error("prompt should contain input summary")
	}

	// Reflections should contain text
	if !strings.Contains(prompt, "Use go run ./cmd/build-dashboard") {
		t.Error("prompt should contain reflection text")
	}

	// Should contain synthesize rule in system prompt
	if !strings.Contains(prompt, "SYNTHESIZE") {
		t.Error("prompt should contain SYNTHESIZE instruction")
	}
}
