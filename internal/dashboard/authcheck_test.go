package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/state"
)

// newAuthCheckServer builds a Server over a fresh state seeded with the
// scope matrix: local first-party chat (in scope), remote chat (out),
// terminal (out). models resolves no endpoint routing for target "claude".
func newAuthCheckServer(t *testing.T) *Server {
	t.Helper()
	server, _, st := newTestServer(t)
	if err := st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "git@github.com:u/r.git", Branch: "main", Path: t.TempDir()}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	now := time.Now()
	sessions := []state.Session{
		{ID: "chat-1", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, CreatedAt: now},
		{ID: "chat-remote", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, RemoteHostID: "host-1", CreatedAt: now},
		{ID: "term-1", WorkspaceID: "ws-1", Target: "claude", CreatedAt: now},
	}
	for _, sess := range sessions {
		if err := st.AddSession(sess); err != nil {
			t.Fatalf("AddSession %s: %v", sess.ID, err)
		}
	}
	return server
}

func TestHandleChatTurnError_SetsOnMatchInScopeOnly(t *testing.T) {
	s := newAuthCheckServer(t)

	// Every turn error also fires the async protocol check; point PATH at
	// an empty dir so it answers NoAnswer and changes nothing — the
	// matcher is what's under test, and the real claude binary (present
	// and logged in on dev machines) must not race the assertions.
	orig := os.Getenv("PATH")
	os.Setenv("PATH", t.TempDir())
	t.Cleanup(func() { os.Setenv("PATH", orig) })

	s.HandleChatTurnError("chat-1", chat.TurnErrorEvent{Protocol: chat.ProtocolClaude, Text: "Invalid API key. Please run /login"})
	sess, _ := s.state.GetSession("chat-1")
	if !sess.SignedOut {
		t.Error("in-scope chat session should be signed out after matching turn error")
	}

	s.HandleChatTurnError("chat-remote", chat.TurnErrorEvent{Protocol: chat.ProtocolClaude, Text: "Please run /login"})
	sess, _ = s.state.GetSession("chat-remote")
	if sess.SignedOut {
		t.Error("remote chat session must never be set")
	}

	s.HandleChatTurnError("term-1", chat.TurnErrorEvent{Protocol: chat.ProtocolClaude, Text: "Please run /login"})
	sess, _ = s.state.GetSession("term-1")
	if sess.SignedOut {
		t.Error("terminal session must never be set")
	}

	// Non-matching error text sets nothing: reset, fire a usage-limit
	// error, assert the flag stays clear.
	s.state.UpdateSessionFunc("chat-1", func(p *state.Session) { p.SignedOut = false })
	s.HandleChatTurnError("chat-1", chat.TurnErrorEvent{Protocol: chat.ProtocolClaude, Text: "You've hit your usage limit"})
	sess, _ = s.state.GetSession("chat-1")
	if sess.SignedOut {
		t.Error("non-matching error text must not set the flag")
	}
}

// TestHandleChatTurnError_RoutedTargetOutOfScope: a chat session whose
// target routes through a non-first-party endpoint (ANTHROPIC_BASE_URL)
// never gets the flag — its login is not the HOME login (acceptance 8).
func TestHandleChatTurnError_RoutedTargetOutOfScope(t *testing.T) {
	s := newAuthCheckServer(t)

	orig := os.Getenv("PATH")
	os.Setenv("PATH", t.TempDir())
	t.Cleanup(func() { os.Setenv("PATH", orig) })

	s.SetModelManager(models.New(&config.Config{}, []detect.Tool{{Name: "claude", Command: "claude"}}, "", discardLogger()))
	s.models.SetRegistryModels([]detect.Model{
		{
			ID: "routed-model", Provider: "third-party",
			Runners: map[string]detect.RunnerSpec{
				"claude": {ModelValue: "v", Endpoint: "https://gateway.example"},
			},
		},
	})
	if err := s.state.AddSession(state.Session{ID: "chat-routed", WorkspaceID: "ws-1", Target: "routed-model", Kind: state.SessionKindChat, ChatProtocol: chat.ProtocolClaude, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if !s.models.RoutesToEndpoint("routed-model") {
		t.Fatal("test setup: routed-model should route to an endpoint")
	}

	s.HandleChatTurnError("chat-routed", chat.TurnErrorEvent{Protocol: chat.ProtocolClaude, Text: "Please run /login"})
	sess, _ := s.state.GetSession("chat-routed")
	if sess.SignedOut {
		t.Error("endpoint-routed chat session must never be set")
	}
}

func TestRunAuthCheck_AppliesToInScopeSessionsOfProtocol(t *testing.T) {
	s := newAuthCheckServer(t)
	// Seed one flag to verify clearing.
	s.state.UpdateSessionFunc("chat-1", func(sess *state.Session) { sess.SignedOut = true })

	dir := t.TempDir()
	orig := os.Getenv("PATH")
	os.Setenv("PATH", dir)
	t.Cleanup(func() { os.Setenv("PATH", orig) })
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\necho '{\"loggedIn\": true}'"), 0o755); err != nil {
		t.Fatalf("stub claude: %v", err)
	}

	s.RunAuthCheck(chat.ProtocolClaude)

	deadline := time.Now().Add(2 * time.Second)
	for {
		sess, _ := s.state.GetSession("chat-1")
		if !sess.SignedOut {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("logged-in answer did not clear the flag")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRunAuthCheck_NoAnswerChangesNothing(t *testing.T) {
	s := newAuthCheckServer(t)
	s.state.UpdateSessionFunc("chat-1", func(sess *state.Session) { sess.SignedOut = true })

	orig := os.Getenv("PATH")
	os.Setenv("PATH", t.TempDir()) // no binaries: exec fails -> NoAnswer
	t.Cleanup(func() { os.Setenv("PATH", orig) })

	s.RunAuthCheck(chat.ProtocolClaude)

	time.Sleep(100 * time.Millisecond) // NoAnswer returns without goroutine racing long
	sess, _ := s.state.GetSession("chat-1")
	if !sess.SignedOut {
		t.Error("NoAnswer must change nothing")
	}
}

// TestSessionsSummaryCarriesSignedOutFields: the session summary must emit
// both signed_out and chat_protocol — the banner's per-protocol copy keys
// off chat_protocol, and a missing field silently falls every codex
// session back to the claude banner (found in implementation review).
func TestSessionsSummaryCarriesSignedOutFields(t *testing.T) {
	s := newAuthCheckServer(t)
	if err := s.state.AddSession(state.Session{
		ID: "chat-codex", WorkspaceID: "ws-1", Target: "codex",
		Kind: state.SessionKindChat, ChatProtocol: chat.ProtocolCodex,
		SignedOut: true,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rr := httptest.NewRecorder()
	s.sessionHandlers.handleSessions(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var resp []struct {
		Sessions []struct {
			ID           string `json:"id"`
			ChatProtocol string `json:"chat_protocol"`
			SignedOut    bool   `json:"signed_out"`
		} `json:"sessions"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var found bool
	for _, ws := range resp {
		for _, sess := range ws.Sessions {
			if sess.ID != "chat-codex" {
				continue
			}
			found = true
			if !sess.SignedOut {
				t.Error("summary should carry signed_out=true")
			}
			if sess.ChatProtocol != chat.ProtocolCodex {
				t.Errorf("summary chat_protocol = %q, want %q", sess.ChatProtocol, chat.ProtocolCodex)
			}
		}
	}
	if !found {
		t.Fatal("chat-codex missing from summary")
	}
}
