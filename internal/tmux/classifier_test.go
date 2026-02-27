package tmux

import "testing"

func TestIsSeparatorLine(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "dashes separator", text: "--------------------------------------------", want: true},
		{name: "equals separator", text: "============================================", want: true},
		{name: "underscores separator", text: "____________________________________________", want: true},
		{name: "short line not separator", text: "-----", want: false},
		{name: "exactly 10 chars is separator", text: "----------", want: true},
		{name: "9 chars too short", text: "---------", want: false},
		{name: "mixed chars below 80%", text: "----abc---def---ghi---jkl", want: false},
		{name: "mostly same char above 80%", text: "----------x-", want: true},
		{name: "empty string", text: "", want: false},
		{name: "regular text", text: "This is a normal sentence", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSeparatorLine(tt.text)
			if got != tt.want {
				t.Errorf("IsSeparatorLine(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestIsPromptLine(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "chevron prompt", text: "❯ command", want: true},
		{name: "angle bracket prompt", text: "› command", want: true},
		{name: "indented chevron", text: "  ❯ command", want: true},
		{name: "regular text", text: "This is not a prompt", want: false},
		{name: "empty string", text: "", want: false},
		{name: "bare chevron", text: "❯", want: true},
		{name: "number at start", text: "1. Option", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPromptLine(tt.text)
			if got != tt.want {
				t.Errorf("IsPromptLine(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestIsChoiceLine(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "dot-separated choice", text: "1. Option A", want: true},
		{name: "paren-separated choice", text: "2) Option B", want: true},
		{name: "multi-digit", text: "12. Twelfth option", want: true},
		{name: "with chevron prefix", text: "❯ 3. Selected", want: true},
		{name: "with angle bracket prefix", text: "› 1. First", want: true},
		{name: "no dot or paren", text: "1 Option", want: false},
		{name: "letter before dot", text: "a. Not a choice", want: false},
		{name: "empty string", text: "", want: false},
		{name: "regular text", text: "Hello world", want: false},
		{name: "just a number and dot", text: "1.", want: true},
		{name: "indented choice", text: "  5) Five", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsChoiceLine(tt.text)
			if got != tt.want {
				t.Errorf("IsChoiceLine(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestIsAgentStatusLine(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "status prefix", text: "⎿ Writing file...", want: true},
		{name: "indented status", text: "  ⎿ Processing", want: true},
		{name: "regular text", text: "This is output", want: false},
		{name: "empty string", text: "", want: false},
		{name: "bare prefix", text: "⎿", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAgentStatusLine(tt.text)
			if got != tt.want {
				t.Errorf("IsAgentStatusLine(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}
