package floormanager

import (
	"testing"
)

func TestShouldInject(t *testing.T) {
	tests := []struct {
		name     string
		prev     string
		curr     string
		expected bool
	}{
		{"working to error", "working", "error", true},
		{"working to needs_input", "working", "needs_input", true},
		{"working to needs_testing", "working", "needs_testing", true},
		{"working to completed", "working", "completed", true},
		{"working to working", "working", "working", false},
		{"needs_input to working", "needs_input", "working", false},
		{"error to working", "error", "working", false},
		{"empty to working", "", "working", false},
		{"empty to error", "", "error", true},
		{"empty to needs_input", "", "needs_input", true},
		{"completed to error", "completed", "error", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldInject(tt.prev, tt.curr)
			if got != tt.expected {
				t.Errorf("shouldInject(%q, %q) = %v, want %v", tt.prev, tt.curr, got, tt.expected)
			}
		})
	}
}

func TestFormatSignalMessage(t *testing.T) {
	tests := []struct {
		name     string
		nickname string
		prev     string
		state    string
		message  string
		intent   string
		blockers string
		want     string
	}{
		{
			name:     "minimal",
			nickname: "claude-1",
			prev:     "working",
			state:    "completed",
			message:  "Auth module finished",
			want:     `[SIGNAL] claude-1: working -> completed "Auth module finished"`,
		},
		{
			name:     "with intent and blockers",
			nickname: "claude-1",
			prev:     "working",
			state:    "needs_input",
			message:  "Need clarification",
			intent:   "Implementing OAuth2",
			blockers: "Unknown token format",
			want:     `[SIGNAL] claude-1: working -> needs_input "Need clarification" intent="Implementing OAuth2" blocked="Unknown token format"`,
		},
		{
			name:     "with intent only",
			nickname: "claude-1",
			prev:     "working",
			state:    "error",
			message:  "Build failed",
			intent:   "Running tests",
			want:     `[SIGNAL] claude-1: working -> error "Build failed" intent="Running tests"`,
		},
		{
			name:     "empty prev state",
			nickname: "agent-2",
			prev:     "",
			state:    "error",
			message:  "Crashed",
			want:     `[SIGNAL] agent-2: -> error "Crashed"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatSignalMessage(tt.nickname, tt.prev, tt.state, tt.message, tt.intent, tt.blockers)
			if got != tt.want {
				t.Errorf("FormatSignalMessage() =\n  %q\nwant:\n  %q", got, tt.want)
			}
		})
	}
}
