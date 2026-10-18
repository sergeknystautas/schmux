package chat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func useSchmuxHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	schmuxdir.Set(home)
	t.Cleanup(func() { schmuxdir.Set("") })
	return home
}

func TestDeleteConversations_LeavesOtherSessionsUnderTheSameWorkspaceID(t *testing.T) {
	useSchmuxHome(t)
	keepPath := filepath.Join(schmuxdir.ChatSessionDir("ws-001", "ws-001-cccc3333"), "conversation.jsonl")
	for _, p := range []string{
		filepath.Join(schmuxdir.ChatSessionDir("ws-001", "ws-001-aaaa1111"), "conversation.jsonl"),
		filepath.Join(schmuxdir.ChatSessionDir("ws-001", "ws-001-bbbb2222"), "conversation.jsonl"),
		keepPath,
	} {
		writeFile(t, p, "x")
	}

	deleted, err := DeleteConversations("ws-001", []string{"ws-001-aaaa1111", "ws-001-bbbb2222"})
	if err != nil {
		t.Fatalf("DeleteConversations() error = %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}
	if _, statErr := os.Stat(keepPath); statErr != nil {
		t.Errorf("unselected session was removed: %v", statErr)
	}
}

func TestDeleteConversations_RejectsUnconfinedIDs(t *testing.T) {
	home := useSchmuxHome(t)
	keep := filepath.Join(home, "chat", "ws-001", "ws-001-aaaa1111", "conversation.jsonl")
	writeFile(t, keep, "x")
	outside := filepath.Join(home, "outside.txt")
	writeFile(t, outside, "x")

	tests := []struct {
		name   string
		wsID   string
		sessID string
	}{
		{"empty workspace", "", "sess"},
		{"dot workspace", ".", "sess"},
		{"parent workspace", "..", "sess"},
		{"nested workspace", "ws-001/ws-001-aaaa1111", "sess"},
		{"absolute workspace", home, "sess"},
		{"empty session", "ws-001", ""},
		{"dot session", "ws-001", "."},
		{"parent session", "ws-001", ".."},
		{"nested session", "ws-001", "ws-001-aaaa1111/conversation.jsonl"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deleted, err := DeleteConversations(tt.wsID, []string{tt.sessID})
			if err == nil {
				t.Fatalf("DeleteConversations(%q, %q) error = nil, want error", tt.wsID, tt.sessID)
			}
			if deleted != 0 {
				t.Fatalf("deleted = %d, want 0", deleted)
			}
			for _, p := range []string{keep, outside} {
				if _, statErr := os.Stat(p); statErr != nil {
					t.Fatalf("%s was removed by rejected id: %v", p, statErr)
				}
			}
		})
	}
}

func TestDeleteConversations_MissingSessionIsOk(t *testing.T) {
	useSchmuxHome(t)

	deleted, err := DeleteConversations("ws-001", []string{"never-existed"})
	if err != nil {
		t.Fatalf("DeleteConversations() error = %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
}
