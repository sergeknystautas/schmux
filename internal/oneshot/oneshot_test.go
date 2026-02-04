package oneshot

import (
	"context"
	"testing"

	"github.com/sergeknystautas/schmux/internal/detect"
)

func TestBuildOneShotCommand(t *testing.T) {
	tests := []struct {
		name         string
		agentName    string
		agentCommand string
		jsonSchema   string
		want         []string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "claude simple",
			agentName:    "claude",
			agentCommand: "claude",
			jsonSchema:   "",
			want:         []string{"claude", "-p", "--output-format", "json"},
			wantErr:      false,
		},
		{
			name:         "claude with path",
			agentName:    "claude",
			agentCommand: "/home/user/.local/bin/claude",
			jsonSchema:   "",
			want:         []string{"/home/user/.local/bin/claude", "-p", "--output-format", "json"},
			wantErr:      false,
		},
		{
			name:         "claude with json schema",
			agentName:    "claude",
			agentCommand: "claude",
			jsonSchema:   `{"type":"object"}`,
			want:         []string{"claude", "-p", "--output-format", "json", "--json-schema", `{"type":"object"}`},
			wantErr:      false,
		},
		{
			name:         "codex simple",
			agentName:    "codex",
			agentCommand: "codex",
			jsonSchema:   "",
			want:         []string{"codex", "exec", "--json"},
			wantErr:      false,
		},
		{
			name:         "codex with json schema",
			agentName:    "codex",
			agentCommand: "codex",
			jsonSchema:   "/tmp/schema.json",
			want:         []string{"codex", "exec", "--json", "--output-schema", "/tmp/schema.json"},
			wantErr:      false,
		},
		{
			name:         "gemini not supported",
			agentName:    "gemini",
			agentCommand: "gemini",
			jsonSchema:   "",
			want:         nil,
			wantErr:      true,
			errContains:  "not supported",
		},
		{
			name:         "unknown agent",
			agentName:    "unknown",
			agentCommand: "unknown",
			jsonSchema:   "",
			want:         nil,
			wantErr:      true,
			errContains:  "unknown tool",
		},
		{
			name:         "empty command",
			agentName:    "claude",
			agentCommand: "",
			jsonSchema:   "",
			want:         nil,
			wantErr:      true,
			errContains:  "empty command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := detect.BuildCommandParts(tt.agentName, tt.agentCommand, detect.ToolModeOneshot, tt.jsonSchema)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildCommandParts() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !contains(err.Error(), tt.errContains) {
					t.Errorf("BuildCommandParts() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}
			if !equalSlices(got, tt.want) {
				t.Errorf("BuildCommandParts() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExecuteInputValidation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		agentName   string
		agentCmd    string
		prompt      string
		wantErr     bool
		errContains string
	}{
		{
			name:        "empty agent name",
			agentName:   "",
			agentCmd:    "claude",
			prompt:      "test",
			wantErr:     true,
			errContains: "agent name cannot be empty",
		},
		{
			name:        "empty agent command",
			agentName:   "claude",
			agentCmd:    "",
			prompt:      "test",
			wantErr:     true,
			errContains: "agent command cannot be empty",
		},
		{
			name:        "empty prompt",
			agentName:   "claude",
			agentCmd:    "claude",
			prompt:      "",
			wantErr:     true,
			errContains: "prompt cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Execute(ctx, tt.agentName, tt.agentCmd, tt.prompt, "", nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !contains(err.Error(), tt.errContains) {
					t.Errorf("Execute() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}

func TestParseClaudeStructuredOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "extracts structured_output",
			input:    `{"structured_output":{"branch":"feature/test","nickname":"Test"},"duration_ms":1000}`,
			expected: `{"branch":"feature/test","nickname":"Test"}`,
		},
		{
			name:     "returns as-is when not json",
			input:    "plain text output",
			expected: "plain text output",
		},
		{
			name:     "returns as-is when json without structured_output",
			input:    `{"result":"something"}`,
			expected: `{"result":"something"}`,
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseClaudeStructuredOutput(tt.input)
			if got != tt.expected {
				t.Errorf("parseClaudeStructuredOutput() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestParseCodexJSONLOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "extracts agent_message from jsonl",
			input: `{"type":"thread.started","thread_id":"abc"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"{\"branch\":\"feature/test\",\"nickname\":\"Test\"}"}}`,
			expected: `{"branch":"feature/test","nickname":"Test"}`,
		},
		{
			name:     "returns as-is for plain text",
			input:    "plain text output",
			expected: "plain text output",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name: "malformed json lines ignored",
			input: `not json
{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"{\"result\":\"value\"}"}}`,
			expected: `{"result":"value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCodexJSONLOutput(tt.input)
			if got != tt.expected {
				t.Errorf("parseCodexJSONLOutput() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestParseResponse(t *testing.T) {
	tests := []struct {
		name      string
		agentName string
		output    string
		expected  string
	}{
		{
			name:      "claude extracts structured output",
			agentName: "claude",
			output:    `{"structured_output":{"greeting":"hello"},"duration_ms":1000}`,
			expected:  `{"greeting":"hello"}`,
		},
		{
			name:      "claude returns as-is when no structured output",
			agentName: "claude",
			output:    `{"result":"something"}`,
			expected:  `{"result":"something"}`,
		},
		{
			name:      "codex extracts agent message",
			agentName: "codex",
			output:    `{"type":"item.completed","item":{"type":"agent_message","text":"{\"greeting\":\"hello\"}"}}`,
			expected:  `{"greeting":"hello"}`,
		},
		{
			name:      "codex returns as-is for plain text",
			agentName: "codex",
			output:    "Some output",
			expected:  "Some output",
		},
		{
			name:      "unknown agent passes through",
			agentName: "unknown",
			output:    "Fallback output",
			expected:  "Fallback output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseResponse(tt.agentName, tt.output)
			if got != tt.expected {
				t.Errorf("parseResponse() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// Helper functions
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
