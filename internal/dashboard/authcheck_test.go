package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/authcheck"
	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/state"
)

// newAuthCheckServer builds a Server over a fresh state seeded with the
// scope matrix: local first-party chat (in scope), remote chat (out),
// terminal (out), and the sign-in helpers covering Claude and Codex (in
// scope under their own SignInProtocol). models resolves no endpoint
// routing for target "claude" / "codex".
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
		{ID: "helper-claude", WorkspaceID: "ws-1", Target: "command", CreatedAt: now, SignInProtocol: chat.ProtocolClaude, SignedOut: true},
		{ID: "helper-codex", WorkspaceID: "ws-1", Target: "command", CreatedAt: now, SignInProtocol: chat.ProtocolCodex, SignedOut: true},
		{ID: "helper-remote", WorkspaceID: "ws-1", Target: "command", RemoteHostID: "host-1", CreatedAt: now, SignInProtocol: chat.ProtocolClaude, SignedOut: true},
		{ID: "term-2", WorkspaceID: "ws-1", Target: "claude", CreatedAt: now},
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

func TestHandleChatTurnError_Claude401InvalidatesCacheAndSignsOutAllClaudeChats(t *testing.T) {
	s := newAuthCheckServer(t)
	if err := s.state.AddSession(state.Session{
		ID: "chat-2", WorkspaceID: "ws-1", Target: "claude",
		Kind: state.SessionKindChat, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	t.Setenv("AUTHCHECK_CALLS", calls)
	orig := os.Getenv("PATH")
	os.Setenv("PATH", dir)
	t.Cleanup(func() { os.Setenv("PATH", orig) })
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\nprintf '%s' \"$*\" > \"$AUTHCHECK_CALLS\"\n"), 0o755); err != nil {
		t.Fatalf("stub claude: %v", err)
	}

	s.HandleChatTurnError("chat-1", chat.TurnErrorEvent{
		Protocol:       chat.ProtocolClaude,
		Text:           "Failed to authenticate. API Error: 401 OAuth access token has been revoked.",
		APIErrorStatus: http.StatusUnauthorized,
	})

	got, err := os.ReadFile(calls)
	if err != nil {
		t.Fatalf("read claude invocation: %v", err)
	}
	if string(got) != "auth logout" {
		t.Errorf("claude args = %q, want %q", got, "auth logout")
	}
	for _, id := range []string{"chat-1", "chat-2"} {
		sess, _ := s.state.GetSession(id)
		if !sess.SignedOut {
			t.Errorf("session %s should be signed out after Anthropic rejected the shared credential", id)
		}
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

	// RunAuthCheck runs the tool and applies the answer synchronously; only
	// the broadcast is dispatched on a goroutine. Assert once on return.
	s.RunAuthCheck(chat.ProtocolClaude)

	sess, _ := s.state.GetSession("chat-1")
	if sess.SignedOut {
		t.Fatal("logged-in answer did not clear the flag")
	}
}

func TestRunAuthCheck_NoAnswerChangesNothing(t *testing.T) {
	s := newAuthCheckServer(t)
	s.state.UpdateSessionFunc("chat-1", func(sess *state.Session) { sess.SignedOut = true })

	orig := os.Getenv("PATH")
	os.Setenv("PATH", t.TempDir()) // no binaries: exec fails -> NoAnswer
	t.Cleanup(func() { os.Setenv("PATH", orig) })

	// Synchronous, as above: NoAnswer returns before any state change could
	// have been applied, so the flag is read once, immediately.
	s.RunAuthCheck(chat.ProtocolClaude)

	sess, _ := s.state.GetSession("chat-1")
	if !sess.SignedOut {
		t.Error("NoAnswer must change nothing")
	}
	helper, _ := s.state.GetSession("helper-claude")
	if !helper.SignedOut {
		t.Error("NoAnswer must not flip a helper from signed-out to signed-in")
	}
}

// TestApplyAuthAnswer_ProtocolMatrix: a single conclusive answer applies to
// every in-scope chat and helper of the same protocol. Codex helpers are
// isolated from claude helpers; ordinary terminals and remote helpers are
// never touched. One save, one broadcast per call.
func TestApplyAuthAnswer_ProtocolMatrix(t *testing.T) {
	cases := []struct {
		name      string
		protocol  string
		answer    authcheck.Result
		wantSet   []string // sessions expected to end signed_out=true
		wantClear []string // sessions expected to end signed_out=false
		untouched []string // sessions whose SignedOut must not move (left at starting value)
	}{
		{
			name:      "claude logged out signs out chats and helpers, leaves codex helper alone",
			protocol:  chat.ProtocolClaude,
			answer:    authcheck.LoggedOut,
			wantSet:   []string{"chat-1", "helper-claude"},
			wantClear: []string{},
			untouched: []string{"helper-codex", "helper-remote", "term-1", "term-2", "chat-remote"},
		},
		{
			name:      "claude logged in clears chats and claude helper, leaves codex helper alone",
			protocol:  chat.ProtocolClaude,
			answer:    authcheck.LoggedIn,
			wantClear: []string{"chat-1", "helper-claude"},
			untouched: []string{"helper-codex", "helper-remote", "term-1", "term-2", "chat-remote"},
		},
		{
			name:      "codex logged in only touches the codex helper",
			protocol:  chat.ProtocolCodex,
			answer:    authcheck.LoggedIn,
			wantClear: []string{"helper-codex"},
			untouched: []string{"chat-1", "helper-claude", "helper-remote", "term-1", "term-2", "chat-remote"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newAuthCheckServer(t)
			// Start every session signed_out so we can observe clear vs.
			// untouched (an untouched session that started true must still
			// be true after the call).
			for _, id := range []string{"chat-1", "chat-remote", "helper-claude", "helper-codex", "helper-remote", "term-1", "term-2"} {
				s.state.UpdateSessionFunc(id, func(p *state.Session) { p.SignedOut = true })
			}

			s.applyAuthAnswer(tc.protocol, tc.answer)

			for _, id := range tc.wantSet {
				sess, _ := s.state.GetSession(id)
				if !sess.SignedOut {
					t.Errorf("%s: expected signed_out=true after %s", id, tc.name)
				}
			}
			for _, id := range tc.wantClear {
				sess, _ := s.state.GetSession(id)
				if sess.SignedOut {
					t.Errorf("%s: expected signed_out=false after %s", id, tc.name)
				}
			}
			for _, id := range tc.untouched {
				sess, _ := s.state.GetSession(id)
				if !sess.SignedOut {
					t.Errorf("%s: expected signed_out=true (untouched) after %s", id, tc.name)
				}
			}
		})
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

// TestSessionsSummaryCarriesSignInProtocol: only local sign-in helpers
// expose sign_in_protocol, and they expose the existing signed_out field.
func TestSessionsSummaryCarriesSignInProtocol(t *testing.T) {
	s := newAuthCheckServer(t)
	if err := s.state.AddSession(state.Session{
		ID: "helper-1", WorkspaceID: "ws-1", Target: "command",
		CreatedAt: time.Now(), SignedOut: true, SignInProtocol: chat.ProtocolClaude,
	}); err != nil {
		t.Fatalf("AddSession helper: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rr := httptest.NewRecorder()
	s.sessionHandlers.handleSessions(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var resp []struct {
		Sessions []struct {
			ID             string `json:"id"`
			SignInProtocol string `json:"sign_in_protocol"`
			SignedOut      bool   `json:"signed_out"`
		} `json:"sessions"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var helper, terminal bool
	for _, ws := range resp {
		for _, sess := range ws.Sessions {
			switch sess.ID {
			case "helper-1":
				helper = true
				if sess.SignInProtocol != chat.ProtocolClaude {
					t.Errorf("helper sign_in_protocol = %q, want %q", sess.SignInProtocol, chat.ProtocolClaude)
				}
				if !sess.SignedOut {
					t.Error("helper should expose signed_out=true")
				}
			case "term-1":
				terminal = true
				if sess.SignInProtocol != "" {
					t.Errorf("ordinary terminal should not expose sign_in_protocol, got %q", sess.SignInProtocol)
				}
			}
		}
	}
	if !helper {
		t.Fatal("helper-1 missing from summary")
	}
	if !terminal {
		t.Fatal("term-1 missing from summary")
	}
}
