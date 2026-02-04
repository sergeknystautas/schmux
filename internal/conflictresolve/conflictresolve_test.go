package conflictresolve

import (
	"strings"
	"testing"
)

func TestParseResult(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantErr    bool
		wantResult OneshotResult
	}{
		{
			name: "valid JSON with actions",
			input: `{
				"all_resolved": true,
				"confidence": "high",
				"summary": "Merged both changes",
				"files": {
					"foo.go": {"action": "modified", "description": "merged imports"}
				}
			}`,
			wantResult: OneshotResult{
				AllResolved: true,
				Confidence:  "high",
				Summary:     "Merged both changes",
				Files:       map[string]FileAction{"foo.go": {Action: "modified", Description: "merged imports"}},
			},
		},
		{
			name: "deleted file action",
			input: `{
				"all_resolved": true,
				"confidence": "high",
				"summary": "Removed obsolete file",
				"files": {
					"old.go": {"action": "deleted", "description": "removed by incoming commit"}
				}
			}`,
			wantResult: OneshotResult{
				AllResolved: true,
				Confidence:  "high",
				Summary:     "Removed obsolete file",
				Files:       map[string]FileAction{"old.go": {Action: "deleted", Description: "removed by incoming commit"}},
			},
		},
		{
			name:    "markdown-wrapped JSON",
			input:   "```json\n{}\n```",
			wantErr: true,
		},
		{
			name:    "extra text around JSON",
			input:   "Here is the result:\n{}\nHope this helps!",
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   "{not valid json}",
			wantErr: true,
		},
		{
			name:    "no JSON object",
			input:   "just some text",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseResult(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.AllResolved != tt.wantResult.AllResolved {
				t.Errorf("AllResolved: got %v, want %v", result.AllResolved, tt.wantResult.AllResolved)
			}
			if result.Confidence != tt.wantResult.Confidence {
				t.Errorf("Confidence: got %q, want %q", result.Confidence, tt.wantResult.Confidence)
			}
			if result.Summary != tt.wantResult.Summary {
				t.Errorf("Summary: got %q, want %q", result.Summary, tt.wantResult.Summary)
			}
			if len(result.Files) != len(tt.wantResult.Files) {
				t.Errorf("Files count: got %d, want %d", len(result.Files), len(tt.wantResult.Files))
			}
			for k, v := range tt.wantResult.Files {
				got, ok := result.Files[k]
				if !ok {
					t.Errorf("Files missing key %q", k)
					continue
				}
				if got.Action != v.Action {
					t.Errorf("Files[%q].Action: got %q, want %q", k, got.Action, v.Action)
				}
				if got.Description != v.Description {
					t.Errorf("Files[%q].Description: got %q, want %q", k, got.Description, v.Description)
				}
			}
		})
	}
}

func TestBuildPrompt(t *testing.T) {
	prompt := BuildPrompt("/tmp/workspace", "abc123", "def456", "Add feature X", []string{
		"internal/foo.go",
	})

	checks := []string{
		"/tmp/workspace",
		"abc123",
		"def456",
		"Add feature X",
		"internal/foo.go",
		"all_resolved",
		"confidence",
		"modified",
		"deleted",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("prompt missing expected content: %q", check)
		}
	}

	// Should NOT contain file contents - only paths
	if strings.Contains(prompt, "<<<<<<< HEAD") {
		t.Error("prompt should not contain file contents, only file paths")
	}
}
