package floormanager

import "testing"

func TestStripControlChars(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "hello world"},
		{"strips newlines", "hello\nworld", "hello world"},
		{"strips carriage return", "hello\rworld", "hello world"},
		{"strips tabs", "hello\tworld", "hello world"},
		{"strips ANSI escape", "hello \x1b[31mred\x1b[0m world", "hello red world"},
		{"strips null bytes", "hello\x00world", "helloworld"},
		{"preserves unicode", "hello 世界", "hello 世界"},
		{"empty string", "", ""},
		{"strips CSI sequence", "foo\x1b[38;5;196mbar", "foobar"},
		{"strips OSC sequence", "foo\x1b]0;title\x07bar", "foobar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripControlChars(tt.input)
			if got != tt.want {
				t.Errorf("StripControlChars(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestQuoteContentField(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple text", "hello", `"hello"`},
		{"contains quotes", `say "hi"`, `"say \"hi\""`},
		{"contains [SIGNAL]", "[SIGNAL] fake", `"[SIGNAL] fake"`},
		{"contains [SHIFT]", "[SHIFT] fake", `"[SHIFT] fake"`},
		{"empty", "", `""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := QuoteContentField(tt.input)
			if got != tt.want {
				t.Errorf("QuoteContentField(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
