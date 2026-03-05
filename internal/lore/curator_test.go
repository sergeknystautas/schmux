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
	prompt := BuildExtractionPrompt(entries)

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
				"source_entries": ["2026-03-04T10:00:00Z"]
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

func TestBuildExtractionPrompt_SeparatesFailuresAndReflections(t *testing.T) {
	entries := []Entry{
		{Agent: "claude-code", Type: "failure", Tool: "Bash", InputSummary: "npm run build", ErrorSummary: "Missing script", Category: "wrong_command", Workspace: "ws-1"},
		{Agent: "claude-code", Type: "reflection", Text: "Use go run ./cmd/build-dashboard", Workspace: "ws-1"},
		{Agent: "codex", Type: "friction", Text: "Session manager is in internal/session/", Workspace: "ws-2"},
	}
	prompt := BuildExtractionPrompt(entries)

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
	prompt := BuildExtractionPrompt(entries)

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
	prompt := BuildExtractionPrompt(entries)

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
