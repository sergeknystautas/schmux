//go:build !noautolearn

package autolearn

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/schema"
)

func TestFrictionBuildPrompt(t *testing.T) {
	entries := []Entry{
		{Text: "use go run ./cmd/build-dashboard", Type: "reflection", Agent: "claude-code", Workspace: "ws-1"},
		{Tool: "Bash", InputSummary: "npm run build", ErrorSummary: "command not found", Type: "failure", Category: "wrong_command", Agent: "claude-code", Workspace: "ws-1"},
	}
	prompt := BuildFrictionPrompt(entries, nil, nil)

	// Should contain friction data
	if !strings.Contains(prompt, "npm run build") {
		t.Error("prompt should contain failure input")
	}
	if !strings.Contains(prompt, "use go run ./cmd/build-dashboard") {
		t.Error("prompt should contain reflection text")
	}
	// Should NOT contain existing titles section when none provided
	if strings.Contains(prompt, "ALREADY EXTRACTED") {
		t.Error("prompt should not contain existing learnings when none provided")
	}
	// Should request learnings output
	if !strings.Contains(prompt, "learnings") {
		t.Error("prompt should request learnings output")
	}
	// Should mention cross-kind output
	if !strings.Contains(prompt, "skill") {
		t.Error("prompt should mention skill kind")
	}
}

func TestFrictionBuildPrompt_IncludesExistingTitles(t *testing.T) {
	entries := []Entry{
		{Agent: "claude-code", Type: "reflection", Text: "build is slow", Workspace: "ws-1"},
	}
	existingTitles := []string{
		"Always use go run ./cmd/build-dashboard instead of npm run build",
		"Run tests before committing",
	}
	prompt := BuildFrictionPrompt(entries, existingTitles, nil)

	if !strings.Contains(prompt, "ALREADY EXTRACTED LEARNINGS") {
		t.Error("prompt should contain ALREADY EXTRACTED LEARNINGS section")
	}
	if !strings.Contains(prompt, "Always use go run ./cmd/build-dashboard") {
		t.Error("prompt should include existing title text")
	}
	if !strings.Contains(prompt, "Run tests before committing") {
		t.Error("prompt should include all existing titles")
	}
	if !strings.Contains(prompt, "do NOT re-extract") {
		t.Error("prompt should instruct LLM not to re-extract")
	}
}

func TestFrictionBuildPrompt_OmitsExistingTitlesWhenEmpty(t *testing.T) {
	entries := []Entry{
		{Agent: "claude-code", Type: "reflection", Text: "insight", Workspace: "ws-1"},
	}
	prompt := BuildFrictionPrompt(entries, []string{}, nil)

	if strings.Contains(prompt, "ALREADY EXTRACTED") {
		t.Error("prompt should not contain existing learnings section when list is empty")
	}
}

func TestFrictionBuildPrompt_IncludesDismissedTitles(t *testing.T) {
	entries := []Entry{
		{Agent: "claude-code", Type: "reflection", Text: "build is slow", Workspace: "ws-1"},
	}
	dismissed := []string{
		"Always run npm audit before deploying",
		"Use yarn instead of npm",
	}
	prompt := BuildFrictionPrompt(entries, nil, dismissed)

	if !strings.Contains(prompt, "PREVIOUSLY REJECTED LEARNINGS") {
		t.Error("prompt should contain PREVIOUSLY REJECTED LEARNINGS section")
	}
	if !strings.Contains(prompt, "Always run npm audit before deploying") {
		t.Error("prompt should include dismissed title text")
	}
	if !strings.Contains(prompt, "Use yarn instead of npm") {
		t.Error("prompt should include all dismissed titles")
	}
	if !strings.Contains(prompt, "do NOT re-propose") {
		t.Error("prompt should instruct LLM not to re-propose dismissed learnings")
	}
}

func TestFrictionBuildPrompt_OmitsDismissedTitlesWhenEmpty(t *testing.T) {
	entries := []Entry{
		{Agent: "claude-code", Type: "reflection", Text: "insight", Workspace: "ws-1"},
	}
	prompt := BuildFrictionPrompt(entries, nil, []string{})

	if strings.Contains(prompt, "PREVIOUSLY REJECTED") {
		t.Error("prompt should not contain rejected learnings section when list is empty")
	}
}

func TestFrictionBuildPrompt_SeparatesFailuresAndReflections(t *testing.T) {
	entries := []Entry{
		{Agent: "claude-code", Type: "failure", Tool: "Bash", InputSummary: "npm run build", ErrorSummary: "Missing script", Category: "wrong_command", Workspace: "ws-1"},
		{Agent: "claude-code", Type: "reflection", Text: "Use go run ./cmd/build-dashboard", Workspace: "ws-1"},
		{Agent: "codex", Type: "friction", Text: "Session manager is in internal/session/", Workspace: "ws-2"},
	}
	prompt := BuildFrictionPrompt(entries, nil, nil)

	if !strings.Contains(prompt, "FAILURE RECORDS:") {
		t.Error("prompt should contain FAILURE RECORDS section")
	}
	if !strings.Contains(prompt, "FRICTION REFLECTIONS:") {
		t.Error("prompt should contain FRICTION REFLECTIONS section")
	}
	if !strings.Contains(prompt, "[Bash]") {
		t.Error("prompt should contain tool name in failure record")
	}
	if !strings.Contains(prompt, "[wrong_command]") {
		t.Error("prompt should contain category in failure record")
	}
	if !strings.Contains(prompt, "SYNTHESIZE") {
		t.Error("prompt should contain SYNTHESIZE instruction")
	}
}

func TestFrictionBuildPrompt_DeduplicatesEntries(t *testing.T) {
	entries := []Entry{
		{Agent: "claude-code", Type: "failure", Tool: "Bash", InputSummary: "cargo test --all", ErrorSummary: "compilation failed", Category: "build_error", Workspace: "ws-1"},
		{Agent: "claude-code", Type: "failure", Tool: "Bash", InputSummary: "cargo test --all", ErrorSummary: "compilation failed", Category: "build_error", Workspace: "ws-2"},
		{Agent: "claude-code", Type: "reflection", Text: "use the cargo wrapper script", Workspace: "ws-1"},
		{Agent: "claude-code", Type: "reflection", Text: "use the cargo wrapper script", Workspace: "ws-2"},
	}
	prompt := BuildFrictionPrompt(entries, nil, nil)

	if strings.Count(prompt, "cargo test --all") != 1 {
		t.Errorf("duplicate failure should be deduped, found %d occurrences", strings.Count(prompt, "cargo test --all"))
	}
	if strings.Count(prompt, "use the cargo wrapper script") != 1 {
		t.Errorf("duplicate reflection should be deduped, found %d occurrences", strings.Count(prompt, "use the cargo wrapper script"))
	}
}

func TestFrictionBuildPrompt_SkipsEmptyReflections(t *testing.T) {
	entries := []Entry{
		{Agent: "claude-code", Type: "reflection", Text: "", Workspace: "ws-1"},
		{Agent: "claude-code", Type: "reflection", Text: "none", Workspace: "ws-1"},
		{Agent: "claude-code", Type: "reflection", Text: "actual insight", Workspace: "ws-1"},
	}
	prompt := BuildFrictionPrompt(entries, nil, nil)

	if !strings.Contains(prompt, "actual insight") {
		t.Error("should contain real reflection text")
	}
	if strings.Contains(prompt, "NONE") || strings.Contains(prompt, "\nnone\n") {
		t.Error("should skip 'none' reflections")
	}
}

func TestFrictionResponseSchemaRegistered(t *testing.T) {
	schemaJSON, err := schema.Get(schema.LabelAutolearnFriction)
	if err != nil {
		t.Fatalf("autolearn-friction schema not registered: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(schemaJSON), &parsed); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	if parsed["type"] != "object" {
		t.Errorf("expected schema type=object, got %v", parsed["type"])
	}

	props, ok := parsed["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties to be an object")
	}
	for _, field := range []string{"learnings", "discarded_entries"} {
		if _, exists := props[field]; !exists {
			t.Errorf("expected property %q in schema", field)
		}
	}
}

func TestFrictionReadFileFromRepo(t *testing.T) {
	bareDir := initBareRepoForFriction(t)
	content, err := ReadFileFromRepo(context.Background(), bareDir, "CLAUDE.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "# Project\n" {
		t.Errorf("unexpected content: %q", content)
	}
}

func TestFrictionReadFileFromRepo_NotFound(t *testing.T) {
	bareDir := initBareRepoForFriction(t)
	_, err := ReadFileFromRepo(context.Background(), bareDir, "NONEXISTENT.md")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

// initBareRepoForFriction creates a bare git repo for testing ReadFileFromRepo.
func initBareRepoForFriction(t *testing.T) (bareDir string) {
	t.Helper()
	dir := t.TempDir()

	normalDir := filepath.Join(dir, "normal")
	os.MkdirAll(normalDir, 0755)
	runCmd(t, normalDir, "git", "init")
	runCmd(t, normalDir, "git", "config", "user.email", "test@test.com")
	runCmd(t, normalDir, "git", "config", "user.name", "test")
	os.WriteFile(filepath.Join(normalDir, "CLAUDE.md"), []byte("# Project\n"), 0644)
	runCmd(t, normalDir, "git", "add", "CLAUDE.md")
	runCmd(t, normalDir, "git", "commit", "-m", "initial")

	bareDir = filepath.Join(dir, "bare.git")
	runCmd(t, dir, "git", "clone", "--bare", normalDir, bareDir)
	return bareDir
}

func runCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v: %s", name, args, err, string(output))
	}
}
