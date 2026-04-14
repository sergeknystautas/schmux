package detect

import (
	"testing"
)

func TestIsBuiltinToolName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want bool
	}{
		{"claude", true},
		{"codex", true},
		{"gemini", true},
		{"unknown-tool", false},
		{"", false},
		{"Claude", false}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsBuiltinToolName(tt.name)
			if got != tt.want {
				t.Errorf("IsBuiltinToolName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestOpencodeInBuiltinTools(t *testing.T) {
	t.Parallel()
	if !IsBuiltinToolName("opencode") {
		t.Error("opencode should be a builtin tool name")
	}
}
