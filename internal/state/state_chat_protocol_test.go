package state

import "testing"

func TestSession_EffectiveChatProtocol(t *testing.T) {
	if got := (Session{Kind: SessionKindChat}).EffectiveChatProtocol(); got != "claude-stream-json" {
		t.Fatalf("empty field: got %q", got)
	}
	if got := (Session{Kind: SessionKindChat, ChatProtocol: "codex-app-server"}).EffectiveChatProtocol(); got != "codex-app-server" {
		t.Fatalf("set field: got %q", got)
	}
}
