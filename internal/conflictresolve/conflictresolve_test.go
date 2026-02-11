package conflictresolve

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/oneshot"
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
			name: "Claude Code envelope with structured_output",
			input: `{
				"type": "result",
				"subtype": "success",
				"is_error": false,
				"duration_ms": 224676,
				"result": "{\"all_resolved\": true, \"confidence\": \"high\", \"summary\": \"Merged\", \"files\": {\"foo.go\": {\"action\": \"modified\", \"description\": \"merged\"}}}",
				"structured_output": {
					"all_resolved": true,
					"confidence": "high",
					"summary": "Merged",
					"files": {"foo.go": {"action": "modified", "description": "merged"}}
				}
			}`,
			wantResult: OneshotResult{
				AllResolved: true,
				Confidence:  "high",
				Summary:     "Merged",
				Files:       map[string]FileAction{"foo.go": {Action: "modified", Description: "merged"}},
			},
		},
		{
			name: "Claude Code envelope with result string only",
			input: `{
				"type": "result",
				"subtype": "success",
				"result": "{\"all_resolved\": true, \"confidence\": \"high\", \"summary\": \"Done\", \"files\": {\"bar.go\": {\"action\": \"deleted\", \"description\": \"removed\"}}}"
			}`,
			wantResult: OneshotResult{
				AllResolved: true,
				Confidence:  "high",
				Summary:     "Done",
				Files:       map[string]FileAction{"bar.go": {Action: "deleted", Description: "removed"}},
			},
		},
		{
			name: "Claude Code envelope with null structured_output falls back to result",
			input: `{
				"type": "result",
				"structured_output": null,
				"result": "{\"all_resolved\": false, \"confidence\": \"low\", \"summary\": \"Partial\", \"files\": {\"x.go\": {\"action\": \"modified\", \"description\": \"tried\"}}}"
			}`,
			wantResult: OneshotResult{
				AllResolved: false,
				Confidence:  "low",
				Summary:     "Partial",
				Files:       map[string]FileAction{"x.go": {Action: "modified", Description: "tried"}},
			},
		},
		{
			name:    "envelope with invalid structured_output and no result",
			input:   `{"type": "result", "structured_output": "not an object"}`,
			wantErr: true,
		},
		{
			name: "spurious text before JSON payload",
			input: `blah{"all_resolved": true, "confidence": "high", "summary": "Resolved", "files": {"foo.go": {"action": "modified", "description": "merged"}}}`,
			wantResult: OneshotResult{
				AllResolved: true,
				Confidence:  "high",
				Summary:     "Resolved",
				Files:       map[string]FileAction{"foo.go": {Action: "modified", Description: "merged"}},
			},
		},
		{
			name: "spurious text before envelope JSON",
			input: `some output prefix{"type": "result", "structured_output": {"all_resolved": true, "confidence": "high", "summary": "Done", "files": {"a.go": {"action": "modified", "description": "fixed"}}}}`,
			wantResult: OneshotResult{
				AllResolved: true,
				Confidence:  "high",
				Summary:     "Done",
				Files:       map[string]FileAction{"a.go": {Action: "modified", Description: "fixed"}},
			},
		},
		{
			name: "spurious text before and after JSON payload",
			input: `prefix{"all_resolved": true, "confidence": "high", "summary": "Ok", "files": {"b.go": {"action": "deleted", "description": "removed"}}}trailing garbage`,
			wantResult: OneshotResult{
				AllResolved: true,
				Confidence:  "high",
				Summary:     "Ok",
				Files:       map[string]FileAction{"b.go": {Action: "deleted", Description: "removed"}},
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

// testConfig returns a minimal config with conflict resolution enabled.
func testConfig(target string) *config.Config {
	return &config.Config{
		ConflictResolve: &config.ConflictResolveConfig{
			Target:    target,
			TimeoutMs: 30000,
		},
	}
}

func TestExecute_RawResponseOnParseError(t *testing.T) {
	original := executorFunc
	defer func() { executorFunc = original }()

	tests := []struct {
		name            string
		cfg             *config.Config
		mockResponse    string
		mockErr         error
		wantRawResponse string
		wantErr         error
		wantErrContains string
	}{
		{
			name:            "disabled when no target configured",
			cfg:             &config.Config{},
			wantRawResponse: "",
			wantErr:         ErrDisabled,
		},
		{
			name:            "target not found from oneshot",
			cfg:             testConfig("nonexistent"),
			mockErr:         oneshot.ErrTargetNotFound,
			wantRawResponse: "",
			wantErr:         ErrTargetNotFound,
		},
		{
			name:            "execution failure returns empty raw response",
			cfg:             testConfig("claude"),
			mockErr:         errors.New("process exited with code 1"),
			wantRawResponse: "",
			wantErrContains: "oneshot execute",
		},
		{
			name:            "invalid JSON returns raw response",
			cfg:             testConfig("claude"),
			mockResponse:    "Certainly! I'd be happy to help resolve these conflicts.",
			wantRawResponse: "Certainly! I'd be happy to help resolve these conflicts.",
			wantErr:         ErrInvalidResponse,
		},
		{
			name:            "markdown-wrapped JSON returns raw response",
			cfg:             testConfig("claude"),
			mockResponse:    "```json\n{\"all_resolved\": true}\n```",
			wantRawResponse: "```json\n{\"all_resolved\": true}\n```",
			wantErr:         ErrInvalidResponse,
		},
		{
			name:            "empty response returns empty raw response",
			cfg:             testConfig("claude"),
			mockResponse:    "",
			wantRawResponse: "",
			wantErr:         ErrInvalidResponse,
		},
		{
			name:            "whitespace-only response returns empty raw response",
			cfg:             testConfig("claude"),
			mockResponse:    "   \n\t  ",
			wantRawResponse: "   \n\t  ",
			wantErr:         ErrInvalidResponse,
		},
		{
			name: "valid JSON returns empty raw response",
			cfg:  testConfig("claude"),
			mockResponse: `{
				"all_resolved": true,
				"confidence": "high",
				"summary": "Resolved",
				"files": {"foo.go": {"action": "modified", "description": "merged"}}
			}`,
			wantRawResponse: "",
		},
		{
			name: "Claude Code envelope is unwrapped successfully",
			cfg:  testConfig("claude"),
			mockResponse: `{
				"type": "result",
				"subtype": "success",
				"structured_output": {
					"all_resolved": true,
					"confidence": "high",
					"summary": "Resolved via envelope",
					"files": {"foo.go": {"action": "modified", "description": "merged"}}
				}
			}`,
			wantRawResponse: "",
		},
		{
			name:         "spurious prefix before envelope is unwrapped successfully",
			cfg:          testConfig("claude"),
			mockResponse: `blah{"type": "result", "structured_output": {"all_resolved": true, "confidence": "high", "summary": "Resolved", "files": {"foo.go": {"action": "modified", "description": "merged"}}}}`,
			wantRawResponse: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executorFunc = func(_ context.Context, _ *config.Config, _, _, _ string, _ time.Duration, _ string) (string, error) {
				return tt.mockResponse, tt.mockErr
			}

			_, rawResponse, err := Execute(context.Background(), tt.cfg, "test prompt", "/tmp")

			if rawResponse != tt.wantRawResponse {
				t.Errorf("rawResponse: got %q, want %q", rawResponse, tt.wantRawResponse)
			}

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("error: got %v, want %v", err, tt.wantErr)
				}
			} else if tt.wantErrContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("error: got %v, want containing %q", err, tt.wantErrContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
