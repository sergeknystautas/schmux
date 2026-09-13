package dashboard

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/state"
)

// newAuthHandlers mirrors newRestartHandler (handlers_restart_test.go:24).
func newAuthHandlers(t *testing.T) *SpawnHandlers {
	t.Helper()
	st := state.New(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "git@github.com:u/r.git", Branch: "main", Path: t.TempDir()}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	now := time.Now()
	sessions := []state.Session{
		{ID: "chat-1", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, CreatedAt: now},
		{ID: "chat-codex", WorkspaceID: "ws-1", Target: "codex", Kind: state.SessionKindChat, ChatProtocol: "codex-app-server", CreatedAt: now},
		{ID: "chat-remote", WorkspaceID: "ws-1", Target: "claude", Kind: state.SessionKindChat, RemoteHostID: "host-1", CreatedAt: now},
		{ID: "term-1", WorkspaceID: "ws-1", Target: "claude", CreatedAt: now},
	}
	for _, sess := range sessions {
		if err := st.AddSession(sess); err != nil {
			t.Fatalf("AddSession %s: %v", sess.ID, err)
		}
	}
	cfg := &config.Config{}
	return &SpawnHandlers{
		logger: discardLogger(),
		state:  st,
		config: cfg,
		models: models.New(cfg, []detect.Tool{{Name: "claude"}, {Name: "codex"}}, "", discardLogger()),
	}
}

func postAuth(t *testing.T, h *SpawnHandlers, suffix string, sessionID string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/api/sessions/{sessionID}/reauth", h.handleReauth)
	r.Post("/api/sessions/{sessionID}/auth-check", h.handleAuthCheck)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/"+suffix, strings.NewReader(""))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestAuthEndpointsGuards(t *testing.T) {
	h := newAuthHandlers(t)

	cases := []struct {
		suffix    string
		sessionID string
		want      int
	}{
		{"reauth", "no-such-session", http.StatusNotFound},
		{"auth-check", "no-such-session", http.StatusNotFound},
		{"reauth", "term-1", http.StatusBadRequest},
		{"auth-check", "term-1", http.StatusBadRequest},
		{"reauth", "chat-remote", http.StatusConflict},
		{"auth-check", "chat-remote", http.StatusConflict},
	}
	for _, tc := range cases {
		if got := postAuth(t, h, tc.suffix, tc.sessionID).Code; got != tc.want {
			t.Errorf("%s %s: status = %d, want %d", tc.suffix, tc.sessionID, got, tc.want)
		}
	}
}

func TestAuthCheckRunsChecker(t *testing.T) {
	h := newAuthHandlers(t)
	protocols := []string{}
	done := make(chan struct{})
	h.runAuthCheck = func(protocol string) {
		protocols = append(protocols, protocol)
		close(done)
	}
	postAuth(t, h, "auth-check", "chat-codex")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runAuthCheck was not invoked")
	}
	if len(protocols) != 1 || protocols[0] != "codex-app-server" {
		t.Errorf("auth-check should run the session's protocol check, got %v", protocols)
	}
}

// The claude login must run through the REPL's /login dialog, not the
// standalone `claude auth login` subcommand: as of Claude Code 2.1.270 the
// standalone prompt ("Paste code here if prompted >") accepts the pasted
// code without echoing a single character and exits the process on a bad
// code, which in the dashboard looks like a terminal that ignores
// keystrokes and then dies. The REPL dialog echoes the code and stays up.
func TestReauthClaudeUsesREPLLogin(t *testing.T) {
	got := reauthCommands[chat.ProtocolClaude]
	if !strings.HasSuffix(got, "claude /login") {
		t.Fatalf("claude reauth command = %q, want it to end with the REPL form %q", got, "claude /login")
	}
	if strings.Contains(got, "claude auth login") {
		t.Fatalf("claude reauth command %q uses the standalone subcommand, which does not echo the pasted code", got)
	}
}
