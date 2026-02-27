package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

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
