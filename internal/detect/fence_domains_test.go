package detect

import (
	"strings"
	"testing"
)

func TestFenceDomainsFromDescriptors(t *testing.T) {
	tests := map[string][]string{
		"claude":      {"platform.claude.com", "downloads.claude.ai"},
		"codex":       {"chatgpt.com", "ab.chatgpt.com", "auth.openai.com"},
		"antigravity": {"oauth2.googleapis.com", "antigravity-unleash.goog", "cloudcode-pa.googleapis.com", "daily-cloudcode-pa.googleapis.com", "www.googleapis.com", "lh3.googleusercontent.com"},
		"gemini":      nil,
		"opencode":    nil,
	}
	for tool, want := range tests {
		a := GetAdapter(tool)
		if a == nil {
			t.Fatalf("GetAdapter(%q) = nil", tool)
		}
		if got := a.FenceDomains(); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s FenceDomains() = %v, want %v", tool, got, want)
		}
	}
}
