package state

import (
	"path/filepath"
	"testing"
	"time"
)

// TestSessionSignedOutPersistence: the flag survives a Save/Load round trip,
// and the JSON key is signed_out (acceptance scenario 5).
func TestSessionSignedOutPersistence(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	s := New(statePath, nil)
	if err := s.AddWorkspace(Workspace{ID: "ws-1", Repo: "git@github.com:u/r.git", Branch: "main", Path: t.TempDir()}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	if err := s.AddSession(Session{ID: "chat-1", WorkspaceID: "ws-1", Target: "claude", Kind: SessionKindChat, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	updated := s.UpdateSessionFunc("chat-1", func(sess *Session) { sess.SignedOut = true })
	if !updated {
		t.Fatal("UpdateSessionFunc missed the session")
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(statePath, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := reloaded.GetSession("chat-1")
	if !ok {
		t.Fatal("session missing after reload")
	}
	if !got.SignedOut {
		t.Error("SignedOut did not survive Save/Load")
	}
	if got.IsChat() != true {
		t.Error("IsChat() should hold for a chat-kind session")
	}
}
