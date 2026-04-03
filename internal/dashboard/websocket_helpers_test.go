package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// withWorkspaceIDParam injects a chi route context with the given workspaceID.
func withWorkspaceIDParam(r *http.Request, workspaceID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceID", workspaceID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// --- requireWorkspace tests ---

func TestRequireWorkspace_EmptyID(t *testing.T) {
	s, _, _ := newTestServer(t)

	req, _ := http.NewRequest("GET", "/api/workspaces//git-graph", nil)
	req = withWorkspaceIDParam(req, "")
	rr := httptest.NewRecorder()

	ws, ok := s.requireWorkspace(rr, req)
	if ok {
		t.Fatal("expected ok=false for empty workspace ID")
	}
	if ws.ID != "" {
		t.Errorf("expected zero-value workspace, got ID=%q", ws.ID)
	}
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}

	// Verify response is JSON (not plain text)
	var errResp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
		t.Fatalf("expected JSON response, got decode error: %v", err)
	}
	if errResp["error"] == "" {
		t.Error("expected non-empty error field in JSON response")
	}
}

func TestRequireWorkspace_NotFound(t *testing.T) {
	s, _, _ := newTestServer(t)

	req, _ := http.NewRequest("GET", "/api/workspaces/nonexistent/git-graph", nil)
	req = withWorkspaceIDParam(req, "nonexistent")
	rr := httptest.NewRecorder()

	ws, ok := s.requireWorkspace(rr, req)
	if ok {
		t.Fatal("expected ok=false for nonexistent workspace")
	}
	if ws.ID != "" {
		t.Errorf("expected zero-value workspace, got ID=%q", ws.ID)
	}
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rr.Code)
	}

	var errResp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
		t.Fatalf("expected JSON response, got decode error: %v", err)
	}
	if errResp["error"] == "" {
		t.Error("expected non-empty error field in JSON response")
	}
}

func TestRequireWorkspace_Found(t *testing.T) {
	s, _, _ := newTestServer(t)

	s.state.AddWorkspace(state.Workspace{ID: "ws-test", Repo: "https://github.com/test/repo"})

	req, _ := http.NewRequest("GET", "/api/workspaces/ws-test/git-graph", nil)
	req = withWorkspaceIDParam(req, "ws-test")
	rr := httptest.NewRecorder()

	ws, ok := s.requireWorkspace(rr, req)
	if !ok {
		t.Fatal("expected ok=true for existing workspace")
	}
	if ws.ID != "ws-test" {
		t.Errorf("expected workspace ID %q, got %q", "ws-test", ws.ID)
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200 (no error written), got %d", rr.Code)
	}
}

// --- clearNudgeOnInput tests ---

func TestClearNudgeOnInput_InteractiveChars(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"enter", "\r"},
		{"tab", "\t"},
		{"backtab", "\x1b[Z"},
		{"escape", "\x1b"},
		{"enter in mixed input", "hello\r"},
		{"tab in mixed input", "cd\t"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _ := newTestServer(t)
			s.state.AddSession(state.Session{ID: "sess-1", Nudge: "needs attention"})

			s.clearNudgeOnInput("sess-1", tt.input)

			// Wait briefly for the async goroutine to complete
			time.Sleep(50 * time.Millisecond)

			sess, ok := s.state.GetSession("sess-1")
			if !ok {
				t.Fatal("session not found")
			}
			if sess.Nudge != "" {
				t.Errorf("expected nudge cleared, got %q", sess.Nudge)
			}
		})
	}
}

func TestClearNudgeOnInput_NonInteractiveChars(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"regular char", "a"},
		{"escape sequence (not bare)", "\x1b[A"},
		{"space", " "},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _ := newTestServer(t)
			s.state.AddSession(state.Session{ID: "sess-1", Nudge: "needs attention"})

			s.clearNudgeOnInput("sess-1", tt.input)

			sess, ok := s.state.GetSession("sess-1")
			if !ok {
				t.Fatal("session not found")
			}
			if sess.Nudge != "needs attention" {
				t.Errorf("expected nudge preserved, got %q", sess.Nudge)
			}
		})
	}
}

func TestClearNudgeOnInput_NoNudge(t *testing.T) {
	s, _, _ := newTestServer(t)
	s.state.AddSession(state.Session{ID: "sess-1", Nudge: ""})

	// Should not panic or error when there's no nudge to clear
	s.clearNudgeOnInput("sess-1", "\r")

	time.Sleep(50 * time.Millisecond)

	sess, ok := s.state.GetSession("sess-1")
	if !ok {
		t.Fatal("session not found")
	}
	if sess.Nudge != "" {
		t.Errorf("expected empty nudge, got %q", sess.Nudge)
	}
}

// --- vcsTypeForWorkspace tests ---

func TestVcsTypeForWorkspace_DefaultGit(t *testing.T) {
	s, _, _ := newTestServer(t)
	ws := state.Workspace{ID: "ws-1", Repo: "https://github.com/test/repo"}
	vcsType := s.vcsTypeForWorkspace(ws)
	if vcsType != "git" {
		t.Errorf("expected git, got %q", vcsType)
	}
}

func TestVcsTypeForWorkspace_RemoteHostNoFlavor(t *testing.T) {
	s, _, _ := newTestServer(t)
	// Add a remote host with no flavor
	s.state.AddRemoteHost(state.RemoteHost{ID: "host-1", ProfileID: ""})
	ws := state.Workspace{ID: "ws-1", Repo: "https://github.com/test/repo", RemoteHostID: "host-1"}
	vcsType := s.vcsTypeForWorkspace(ws)
	if vcsType != "git" {
		t.Errorf("expected git, got %q", vcsType)
	}
}

func TestVcsTypeForWorkspace_RemoteHostWithProfile(t *testing.T) {
	s, cfg, _ := newTestServer(t)
	// Add a profile with VCS=sapling
	cfg.AddRemoteProfile(config.RemoteProfile{
		ID:            "profile-1",
		DisplayName:   "Test Profile",
		WorkspacePath: "~/workspace",
		VCS:           "sapling",
		Flavors:       []config.RemoteProfileFlavor{{Flavor: "test-flavor"}},
	})
	s.state.AddRemoteHost(state.RemoteHost{ID: "host-1", ProfileID: "profile-1", Flavor: "test-flavor"})
	ws := state.Workspace{ID: "ws-1", Repo: "https://github.com/test/repo", RemoteHostID: "host-1"}
	vcsType := s.vcsTypeForWorkspace(ws)
	if vcsType != "sapling" {
		t.Errorf("expected sapling, got %q", vcsType)
	}
}

func TestVcsTypeForWorkspace_RemoteHostProfileEmptyVCS(t *testing.T) {
	s, cfg, _ := newTestServer(t)
	// Add a profile with empty VCS (should default to git)
	cfg.AddRemoteProfile(config.RemoteProfile{
		ID:            "profile-2",
		DisplayName:   "Test Profile 2",
		WorkspacePath: "~/workspace",
		VCS:           "",
		Flavors:       []config.RemoteProfileFlavor{{Flavor: "test-flavor-2"}},
	})
	s.state.AddRemoteHost(state.RemoteHost{ID: "host-2", ProfileID: "profile-2", Flavor: "test-flavor-2"})
	ws := state.Workspace{ID: "ws-2", Repo: "https://github.com/test/repo", RemoteHostID: "host-2"}
	vcsType := s.vcsTypeForWorkspace(ws)
	if vcsType != "git" {
		t.Errorf("expected git, got %q", vcsType)
	}
}

func TestVcsTypeForWorkspace_RemoteHostNotFound(t *testing.T) {
	s, _, _ := newTestServer(t)
	// Workspace references a host that doesn't exist in state
	ws := state.Workspace{ID: "ws-1", Repo: "https://github.com/test/repo", RemoteHostID: "nonexistent-host"}
	vcsType := s.vcsTypeForWorkspace(ws)
	if vcsType != "git" {
		t.Errorf("expected git, got %q", vcsType)
	}
}

// --- startWSMessageReader tests ---

// mockWSReader is a fake WebSocket connection that delivers pre-canned messages.
type mockWSReader struct {
	messages []mockWSMsg
	idx      int
}

type mockWSMsg struct {
	msgType int
	data    []byte
	err     error
}

func (m *mockWSReader) ReadMessage() (int, []byte, error) {
	if m.idx >= len(m.messages) {
		return 0, nil, fmt.Errorf("connection closed")
	}
	msg := m.messages[m.idx]
	m.idx++
	return msg.msgType, msg.data, msg.err
}

func TestStartWSMessageReader_BinaryMessageRoutedAsInput(t *testing.T) {
	reader := &mockWSReader{
		messages: []mockWSMsg{
			{msgType: websocket.BinaryMessage, data: []byte("hello")},
		},
	}

	controlChan := startWSMessageReader(reader)

	select {
	case msg := <-controlChan:
		if msg.Type != "input" {
			t.Errorf("expected type 'input', got %q", msg.Type)
		}
		if msg.Data != "hello" {
			t.Errorf("expected data 'hello', got %q", msg.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestStartWSMessageReader_TextMessageStillParsedAsJSON(t *testing.T) {
	jsonMsg, _ := json.Marshal(WSMessage{Type: "resize", Data: `{"cols":80,"rows":24}`})
	reader := &mockWSReader{
		messages: []mockWSMsg{
			{msgType: websocket.TextMessage, data: jsonMsg},
		},
	}

	controlChan := startWSMessageReader(reader)

	select {
	case msg := <-controlChan:
		if msg.Type != "resize" {
			t.Errorf("expected type 'resize', got %q", msg.Type)
		}
		if msg.Data != `{"cols":80,"rows":24}` {
			t.Errorf("unexpected data: %q", msg.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestStartWSMessageReader_BinaryAndTextInterleaved(t *testing.T) {
	jsonMsg, _ := json.Marshal(WSMessage{Type: "gap", Data: `{"fromSeq":"5"}`})
	reader := &mockWSReader{
		messages: []mockWSMsg{
			{msgType: websocket.BinaryMessage, data: []byte("a")},
			{msgType: websocket.TextMessage, data: jsonMsg},
			{msgType: websocket.BinaryMessage, data: []byte("\x1b[A")},
		},
	}

	controlChan := startWSMessageReader(reader)

	// First: binary input 'a'
	msg := <-controlChan
	if msg.Type != "input" || msg.Data != "a" {
		t.Errorf("msg 1: expected input/a, got %s/%s", msg.Type, msg.Data)
	}

	// Second: text JSON gap
	msg = <-controlChan
	if msg.Type != "gap" {
		t.Errorf("msg 2: expected gap, got %s", msg.Type)
	}

	// Third: binary input arrow key
	msg = <-controlChan
	if msg.Type != "input" || msg.Data != "\x1b[A" {
		t.Errorf("msg 3: expected input/ESC[A, got %s/%q", msg.Type, msg.Data)
	}
}
