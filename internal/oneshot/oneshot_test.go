package oneshot

import (
	"context"
	"testing"
)

func TestBuildOneShotCommand(t *testing.T) {
	tests := []struct {
		name         string
		agentName    string
		agentCommand string
		want         []string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "claude simple",
			agentName:    "claude",
			agentCommand: "claude",
			want:         []string{"claude", "-p", "--model", "haiku"},
			wantErr:      false,
		},
		{
			name:         "claude with path",
			agentName:    "claude",
			agentCommand: "/home/user/.local/bin/claude",
			want:         []string{"/home/user/.local/bin/claude", "-p", "--model", "haiku"},
			wantErr:      false,
		},
		{
			name:         "codex simple",
			agentName:    "codex",
			agentCommand: "codex",
			want:         []string{"codex", "exec", "--json"},
			wantErr:      false,
		},
		{
			name:         "gemini interactive",
			agentName:    "gemini",
			agentCommand: "gemini -i",
			want:         []string{"gemini"},
			wantErr:      false,
		},
		{
			name:         "gemini simple",
			agentName:    "gemini",
			agentCommand: "gemini",
			want:         []string{"gemini"},
			wantErr:      false,
		},
		{
			name:         "unknown agent",
			agentName:    "unknown",
			agentCommand: "unknown",
			want:         nil,
			wantErr:      true,
			errContains:  "unknown agent",
		},
		{
			name:         "empty command",
			agentName:    "claude",
			agentCommand: "",
			want:         nil,
			wantErr:      true,
			errContains:  "empty command",
		},
		{
			name:         "gemini with other flags preserved",
			agentName:    "gemini",
			agentCommand: "gemini -i --verbose",
			want:         []string{"gemini", "--verbose"},
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildOneShotCommand(tt.agentName, tt.agentCommand)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildOneShotCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !contains(err.Error(), tt.errContains) {
					t.Errorf("buildOneShotCommand() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}
			if !equalSlices(got, tt.want) {
				t.Errorf("buildOneShotCommand() = %v, want %v", got, tt.want)
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
			_, err := Execute(ctx, tt.agentName, tt.agentCmd, tt.prompt)
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

func TestParseGeminiOneShot(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strips credentials line",
			input:    "Loaded cached credentials.\nHello, world!",
			expected: "Hello, world!",
		},
		{
			name:     "no credentials line",
			input:    "Hello, world!",
			expected: "Hello, world!",
		},
		{
			name:     "credentials at end",
			input:    "Hello, world!\nLoaded cached credentials.",
			expected: "Hello, world!",
		},
		{
			name:     "multiple credentials lines",
			input:    "Loaded cached credentials.\nSome text\nLoaded cached credentials.\nMore text",
			expected: "Some text\nMore text",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseGeminiOneShot(tt.input)
			if got != tt.expected {
				t.Errorf("parseGeminiOneShot() = %q, want %q", got, tt.expected)
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
			name:      "claude passes through",
			agentName: "claude",
			output:    "Hello, world!",
			expected:  "Hello, world!",
		},
		{
			name:      "codex passes through",
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
		{
			name:      "gemini strips credentials",
			agentName: "gemini",
			output:    "Loaded cached credentials.\nResponse here",
			expected:  "Response here",
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
