package lore

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/schema"
)

func TestBuildExtractionPrompt(t *testing.T) {
	entries := []Entry{
		{Text: "use go run ./cmd/build-dashboard", Type: "reflection", Agent: "claude-code", Workspace: "ws-1"},
		{Tool: "Bash", InputSummary: "npm run build", ErrorSummary: "command not found", Type: "failure", Category: "wrong_command", Agent: "claude-code", Workspace: "ws-1"},
	}
	prompt := BuildExtractionPrompt(entries, nil, nil)

	// Should contain friction data
	if !strings.Contains(prompt, "npm run build") {
		t.Error("prompt should contain failure input")
	}
	if !strings.Contains(prompt, "use go run ./cmd/build-dashboard") {
		t.Error("prompt should contain reflection text")
	}
	// Should NOT contain instruction file content (extraction is blind)
	if strings.Contains(prompt, "INSTRUCTION FILES") {
		t.Error("extraction prompt must not include instruction files")
	}
	// Should NOT contain existing rules section when none provided
	if strings.Contains(prompt, "ALREADY EXTRACTED") {
		t.Error("prompt should not contain existing rules when none provided")
	}
	// Should request discrete rules output
	if !strings.Contains(prompt, "rules") {
		t.Error("prompt should request rules output")
	}
}

func TestParseExtractionResponse(t *testing.T) {
	response := `{
		"rules": [
			{
				"text": "Use go run ./cmd/build-dashboard instead of npm",
				"category": "build",
				"suggested_layer": "repo_public",
				"source_entries": [{"type": "failure", "input_summary": "npm run build", "error_summary": "command not found"}]
			}
		],
		"discarded_entries": ["2026-03-04T08:00:00Z"]
	}`
	result, err := ParseExtractionResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(result.Rules))
	}
	if result.Rules[0].Category != "build" {
		t.Errorf("unexpected category: %s", result.Rules[0].Category)
	}
	if result.Rules[0].SuggestedLayer != "repo_public" {
		t.Errorf("unexpected layer: %s", result.Rules[0].SuggestedLayer)
	}
}

func TestParseExtractionResponseStructuredSources(t *testing.T) {
	response := `{
		"rules": [{
			"text": "Always run tests from root",
			"category": "testing",
			"suggested_layer": "repo_private",
			"source_entries": [
				{"type": "failure", "input_summary": "cd sub && go test", "error_summary": "module not found"},
				{"type": "reflection", "text": "tests must run from root"}
			]
		}],
		"discarded_entries": []
	}`
	result, err := ParseExtractionResponse(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(result.Rules))
	}
	if len(result.Rules[0].SourceEntries) != 2 {
		t.Fatalf("expected 2 source entries, got %d", len(result.Rules[0].SourceEntries))
	}
	if result.Rules[0].SourceEntries[0].Type != "failure" {
		t.Errorf("expected failure type, got %s", result.Rules[0].SourceEntries[0].Type)
	}
	if result.Rules[0].SourceEntries[0].InputSummary != "cd sub && go test" {
		t.Errorf("expected input summary, got %s", result.Rules[0].SourceEntries[0].InputSummary)
	}
}

func TestBuildExtractionPrompt_SeparatesFailuresAndReflections(t *testing.T) {
	entries := []Entry{
		{Agent: "claude-code", Type: "failure", Tool: "Bash", InputSummary: "npm run build", ErrorSummary: "Missing script", Category: "wrong_command", Workspace: "ws-1"},
		{Agent: "claude-code", Type: "reflection", Text: "Use go run ./cmd/build-dashboard", Workspace: "ws-1"},
		{Agent: "codex", Type: "friction", Text: "Session manager is in internal/session/", Workspace: "ws-2"},
	}
	prompt := BuildExtractionPrompt(entries, nil, nil)

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

func TestBuildExtractionPrompt_DeduplicatesEntries(t *testing.T) {
	entries := []Entry{
		{Agent: "claude-code", Type: "failure", Tool: "Bash", InputSummary: "cargo test --all", ErrorSummary: "compilation failed", Category: "build_error", Workspace: "ws-1"},
		{Agent: "claude-code", Type: "failure", Tool: "Bash", InputSummary: "cargo test --all", ErrorSummary: "compilation failed", Category: "build_error", Workspace: "ws-2"},
		{Agent: "claude-code", Type: "reflection", Text: "use the cargo wrapper script", Workspace: "ws-1"},
		{Agent: "claude-code", Type: "reflection", Text: "use the cargo wrapper script", Workspace: "ws-2"},
	}
	prompt := BuildExtractionPrompt(entries, nil, nil)

	// Each should appear only once despite duplicates
	if strings.Count(prompt, "cargo test --all") != 1 {
		t.Errorf("duplicate failure should be deduped, found %d occurrences", strings.Count(prompt, "cargo test --all"))
	}
	if strings.Count(prompt, "use the cargo wrapper script") != 1 {
		t.Errorf("duplicate reflection should be deduped, found %d occurrences", strings.Count(prompt, "use the cargo wrapper script"))
	}
}

func TestBuildExtractionPrompt_SkipsEmptyReflections(t *testing.T) {
	entries := []Entry{
		{Agent: "claude-code", Type: "reflection", Text: "", Workspace: "ws-1"},
		{Agent: "claude-code", Type: "reflection", Text: "none", Workspace: "ws-1"},
		{Agent: "claude-code", Type: "reflection", Text: "actual insight", Workspace: "ws-1"},
	}
	prompt := BuildExtractionPrompt(entries, nil, nil)

	if !strings.Contains(prompt, "actual insight") {
		t.Error("should contain real reflection text")
	}
	// Should not contain empty or "none" entries
	if strings.Contains(prompt, "NONE") || strings.Contains(prompt, "\nnone\n") {
		t.Error("should skip 'none' reflections")
	}
}

func TestReadFileFromBareRepo(t *testing.T) {
	bareDir := initBareRepo(t)
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

func TestExtractionResponseSchemaRegistered(t *testing.T) {
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

	props, ok := parsed["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties to be an object")
	}
	for _, field := range []string{"rules", "discarded_entries"} {
		if _, exists := props[field]; !exists {
			t.Errorf("expected property %q in schema", field)
		}
	}
}

func TestParseExtractionResponse_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantErr   bool
		wantRules int // expected number of rules if no error
	}{
		{
			name:      "bare JSON",
			input:     `{"rules":[{"text":"rule1","category":"build","suggested_layer":"repo_public","source_entries":[]}],"discarded_entries":[]}`,
			wantRules: 1,
		},
		{
			name:      "json fenced",
			input:     "```json\n{\"rules\":[],\"discarded_entries\":[]}\n```",
			wantRules: 0,
		},
		{
			name:      "plain fenced",
			input:     "```\n{\"rules\":[],\"discarded_entries\":[]}\n```",
			wantRules: 0,
		},
		{
			name:      "leading whitespace",
			input:     "  \n  {\"rules\":[],\"discarded_entries\":[]}  \n  ",
			wantRules: 0,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace only",
			input:   "   \n  ",
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   "{not valid json}",
			wantErr: true,
		},
		{
			name:      "missing closing fence",
			input:     "```json\n{\"rules\":[],\"discarded_entries\":[]}",
			wantRules: 0,
		},
		{
			name:      "optional fields missing",
			input:     `{"rules":[]}`,
			wantRules: 0,
		},
		{
			name:      "fence with trailing text",
			input:     "```json\n{\"rules\":[],\"discarded_entries\":[]}\n```\nsome trailing text",
			wantRules: 0,
		},
		{
			name:      "text before fence",
			input:     "Here is my analysis of the failures.\n\n```json\n{\"rules\":[],\"discarded_entries\":[]}\n```",
			wantRules: 0,
		},
		{
			name:      "text before and after fence",
			input:     "Let me analyze...\n\nHere's my proposal:\n\n```json\n{\"rules\":[],\"discarded_entries\":[]}\n```\n\nThat covers the main issues.",
			wantRules: 0,
		},
		{
			name:      "prose-wrapped JSON without fences",
			input:     "After analyzing the entries, here is my extraction:\n\n{\"rules\":[],\"discarded_entries\":[]}\n\nI hope this helps.",
			wantRules: 0,
		},
		{
			name:      "prose-wrapped JSON with rules",
			input:     "As requested, here are the results:\n{\"rules\":[{\"text\":\"rule1\",\"category\":\"build\",\"suggested_layer\":\"repo_public\",\"source_entries\":[]}],\"discarded_entries\":[]}",
			wantRules: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseExtractionResponse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseExtractionResponse() expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseExtractionResponse() unexpected error: %v", err)
			}
			if len(got.Rules) != tt.wantRules {
				t.Errorf("Rules count = %d, want %d", len(got.Rules), tt.wantRules)
			}
		})
	}
}

func TestBuildExtractionPrompt_IncludesExistingRules(t *testing.T) {
	entries := []Entry{
		{Agent: "claude-code", Type: "reflection", Text: "build is slow", Workspace: "ws-1"},
	}
	existingRules := []string{
		"Always use go run ./cmd/build-dashboard instead of npm run build",
		"Run tests before committing",
	}
	prompt := BuildExtractionPrompt(entries, existingRules, nil)

	if !strings.Contains(prompt, "ALREADY EXTRACTED RULES") {
		t.Error("prompt should contain ALREADY EXTRACTED RULES section")
	}
	if !strings.Contains(prompt, "Always use go run ./cmd/build-dashboard") {
		t.Error("prompt should include existing rule text")
	}
	if !strings.Contains(prompt, "Run tests before committing") {
		t.Error("prompt should include all existing rules")
	}
	if !strings.Contains(prompt, "do NOT re-extract") {
		t.Error("prompt should instruct LLM not to re-extract")
	}
}

func TestBuildExtractionPrompt_OmitsExistingRulesWhenEmpty(t *testing.T) {
	entries := []Entry{
		{Agent: "claude-code", Type: "reflection", Text: "insight", Workspace: "ws-1"},
	}
	prompt := BuildExtractionPrompt(entries, []string{}, nil)

	if strings.Contains(prompt, "ALREADY EXTRACTED") {
		t.Error("prompt should not contain existing rules section when list is empty")
	}
}

func TestBuildExtractionPrompt_IncludesDismissedRules(t *testing.T) {
	entries := []Entry{
		{Agent: "claude-code", Type: "reflection", Text: "build is slow", Workspace: "ws-1"},
	}
	dismissed := []string{
		"Always run npm audit before deploying",
		"Use yarn instead of npm",
	}
	prompt := BuildExtractionPrompt(entries, nil, dismissed)

	if !strings.Contains(prompt, "PREVIOUSLY REJECTED RULES") {
		t.Error("prompt should contain PREVIOUSLY REJECTED RULES section")
	}
	if !strings.Contains(prompt, "Always run npm audit before deploying") {
		t.Error("prompt should include dismissed rule text")
	}
	if !strings.Contains(prompt, "Use yarn instead of npm") {
		t.Error("prompt should include all dismissed rules")
	}
	if !strings.Contains(prompt, "do NOT re-propose") {
		t.Error("prompt should instruct LLM not to re-propose dismissed rules")
	}
}

func TestBuildExtractionPrompt_OmitsDismissedRulesWhenEmpty(t *testing.T) {
	entries := []Entry{
		{Agent: "claude-code", Type: "reflection", Text: "insight", Workspace: "ws-1"},
	}
	prompt := BuildExtractionPrompt(entries, nil, []string{})

	if strings.Contains(prompt, "PREVIOUSLY REJECTED") {
		t.Error("prompt should not contain rejected rules section when list is empty")
	}
}
