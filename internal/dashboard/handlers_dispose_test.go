package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/state"
)

// makeSessionRequest creates an HTTP request with chi route context for a session endpoint.
func makeSessionRequest(t *testing.T, method, path, sessionID string, body []byte) *http.Request {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionID", sessionID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return req
}

func TestHandleDispose_Guards(t *testing.T) {
	server, _, _ := newTestServer(t)

	t.Run("rejects missing session ID", func(t *testing.T) {
		req := makeSessionRequest(t, http.MethodDelete, "/api/sessions//dispose", "", nil)
		rr := httptest.NewRecorder()
		server.handleDispose(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}

func TestHandleDisposeWorkspace_Guards(t *testing.T) {
	server, _, _ := newTestServer(t)

	t.Run("rejects missing workspace ID", func(t *testing.T) {
		req := makeWorkspaceRequest(t, http.MethodDelete, "/api/workspaces//dispose", "", nil)
		rr := httptest.NewRecorder()
		server.handleDisposeWorkspace(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}

func TestHandleDisposeWorkspaceAll_Guards(t *testing.T) {
	server, _, _ := newTestServer(t)

	t.Run("rejects missing workspace ID", func(t *testing.T) {
		req := makeWorkspaceRequest(t, http.MethodDelete, "/api/workspaces//dispose-all", "", nil)
		rr := httptest.NewRecorder()
		server.handleDisposeWorkspaceAll(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}

func TestHandleUpdateNickname_Guards(t *testing.T) {
	server, _, st := newTestServer(t)

	ws := state.Workspace{
		ID:     "ws-nick",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   t.TempDir(),
	}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}
	sess := state.Session{
		ID:          "sess-nick",
		WorkspaceID: "ws-nick",
		Nickname:    "agent-1",
	}
	if err := st.AddSession(sess); err != nil {
		t.Fatalf("failed to add session: %v", err)
	}

	t.Run("rejects missing session ID", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"nickname": "new-name"})
		req := makeSessionRequest(t, http.MethodPatch, "/api/sessions//nickname", "", body)
		rr := httptest.NewRecorder()
		server.handleUpdateNickname(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("rejects malformed body", func(t *testing.T) {
		req := makeSessionRequest(t, http.MethodPatch, "/api/sessions/sess-nick/nickname", "sess-nick", []byte(`{broken`))
		rr := httptest.NewRecorder()
		server.handleUpdateNickname(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}
