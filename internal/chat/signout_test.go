package chat

import "testing"

func TestMatchSignOutStatement(t *testing.T) {
	cases := []struct {
		name     string
		protocol string
		text     string
		want     bool
	}{
		{"claude login prompt", ProtocolClaude, "Invalid API key. Please run /login to authenticate", true},
		{"claude not logged in", ProtocolClaude, "You are not logged in. Run /login first", true},
		{"claude case insensitive", ProtocolClaude, "PLEASE RUN /LOGIN", true},
		{"claude usage limit must not match", ProtocolClaude, "You've hit your usage limit. Usage resets at 5pm", false},
		{"claude tool failure must not match", ProtocolClaude, "Bash command failed with exit code 1", false},
		{"codex not logged in", ProtocolCodex, "Codex is not logged in", true},
		{"codex login required", ProtocolCodex, "login required: run codex login", true},
		{"codex usage limit must not match", ProtocolCodex, "You've used all of your available usage", false},
		{"protocol isolation: claude text on codex protocol", ProtocolCodex, "Please run /login", false},
		{"protocol isolation: codex text on claude protocol", ProtocolClaude, "Codex is not logged in", false},
		{"empty text", ProtocolClaude, "", false},
		{"unknown protocol", "opencode", "Please run /login", false},
	}
	for _, tc := range cases {
		if got := MatchSignOutStatement(tc.protocol, tc.text); got != tc.want {
			t.Errorf("%s: MatchSignOutStatement(%q, %q) = %v, want %v", tc.name, tc.protocol, tc.text, got, tc.want)
		}
	}
}
