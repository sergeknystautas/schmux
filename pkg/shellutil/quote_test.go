package shellutil

import "testing"

func TestQuote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple string",
			input:    "hello world",
			expected: "'hello world'",
		},
		{
			name:     "string with single quote",
			input:    "don't",
			expected: "'don'\\''t'",
		},
		{
			name:     "string with multiple single quotes",
			input:    "it's a 'test'",
			expected: "'it'\\''s a '\\''test'\\'''",
		},
		{
			name:     "string with newline",
			input:    "hello\nworld",
			expected: "'hello\nworld'",
		},
		{
			name:     "string with newline and single quote",
			input:    "hello\nit's me",
			expected: "'hello\nit'\\''s me'",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "''",
		},
		{
			name:     "string with backslash",
			input:    "path\\to\\file",
			expected: "'path\\to\\file'",
		},
		{
			name:     "string with double quotes",
			input:    `say "hello"`,
			expected: `'say "hello"'`,
		},
		{
			name:     "string with spaces",
			input:    "hello world",
			expected: `'hello world'`,
		},
		{
			name:     "string with special chars",
			input:    "test;ls",
			expected: `'test;ls'`,
		},
		{
			name:     "string with variable",
			input:    "$HOME/path",
			expected: `'$HOME/path'`,
		},
		{
			name:     "null byte preserved",
			input:    "dangerous\x00command",
			expected: "'dangerous\x00command'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Quote(tt.input)
			if got != tt.expected {
				t.Errorf("Quote(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestQuoteIfNeeded(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"bare command", "agy", "agy"},
		{"bare flag", "--continue", "--continue"},
		{"bare short flag", "-c", "-c"},
		{"safe model id", "claude-opus-4-6", "claude-opus-4-6"},
		{"safe dotted id", "gemini-2.5-pro", "gemini-2.5-pro"},
		{"spaces and parens quoted", "Claude Opus 4.6 (Thinking)", "'Claude Opus 4.6 (Thinking)'"},
		{"spaces quoted", "Gemini 3.5 Flash (Low)", "'Gemini 3.5 Flash (Low)'"},
		{"empty quoted", "", "''"},
		{"single quote escaped", "don't", "'don'\\''t'"},
		{"shell metachar quoted", "a;b", "'a;b'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := QuoteIfNeeded(tt.input)
			if got != tt.expected {
				t.Errorf("QuoteIfNeeded(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
