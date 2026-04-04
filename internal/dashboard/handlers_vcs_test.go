package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/state"
)

// makeWorkspaceRequest creates an HTTP request with chi route context for a workspace endpoint.
func makeWorkspaceRequest(t *testing.T, method, path, workspaceID string, body []byte) *http.Request {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceID", workspaceID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return req
}

// setWorkspaceAhead updates a workspace's Ahead field in state.
func setWorkspaceAhead(t *testing.T, st *state.State, wsID string, ahead int) {
	t.Helper()
	ws, ok := st.GetWorkspace(wsID)
	if !ok {
		t.Fatalf("workspace %q not found", wsID)
	}
	ws.Ahead = ahead
	if err := st.UpdateWorkspace(ws); err != nil {
		t.Fatalf("failed to update workspace: %v", err)
	}
}

func TestHandleGitUncommit_Guards(t *testing.T) {
	server, _, st := newTestServer(t)

	ws := state.Workspace{
		ID:     "ws-uncommit",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   t.TempDir(),
	}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	t.Run("rejects when no commits ahead", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"hash": "abc123"})
		req := makeWorkspaceRequest(t, http.MethodPost, "/api/workspaces/ws-uncommit/uncommit", "ws-uncommit", body)
		rr := httptest.NewRecorder()
		server.handleUncommit(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("rejects empty hash", func(t *testing.T) {
		setWorkspaceAhead(t, st, "ws-uncommit", 1)

		body, _ := json.Marshal(map[string]string{"hash": ""})
		req := makeWorkspaceRequest(t, http.MethodPost, "/api/workspaces/ws-uncommit/uncommit", "ws-uncommit", body)
		rr := httptest.NewRecorder()
		server.handleUncommit(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("rejects missing hash field", func(t *testing.T) {
		setWorkspaceAhead(t, st, "ws-uncommit", 1)

		body := []byte(`{}`)
		req := makeWorkspaceRequest(t, http.MethodPost, "/api/workspaces/ws-uncommit/uncommit", "ws-uncommit", body)
		rr := httptest.NewRecorder()
		server.handleUncommit(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("rejects malformed body", func(t *testing.T) {
		setWorkspaceAhead(t, st, "ws-uncommit", 1)

		req := makeWorkspaceRequest(t, http.MethodPost, "/api/workspaces/ws-uncommit/uncommit", "ws-uncommit", []byte(`{not json}`))
		rr := httptest.NewRecorder()
		server.handleUncommit(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("rejects workspace not found", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"hash": "abc123"})
		req := makeWorkspaceRequest(t, http.MethodPost, "/api/workspaces/nonexistent/uncommit", "nonexistent", body)
		rr := httptest.NewRecorder()
		server.handleUncommit(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}

func TestHandleGitAmend_Guards(t *testing.T) {
	server, _, st := newTestServer(t)

	ws := state.Workspace{
		ID:     "ws-amend",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   t.TempDir(),
	}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	t.Run("rejects when no commits ahead", func(t *testing.T) {
		body, _ := json.Marshal(map[string][]string{"files": {"file.go"}})
		req := makeWorkspaceRequest(t, http.MethodPost, "/api/workspaces/ws-amend/amend", "ws-amend", body)
		rr := httptest.NewRecorder()
		server.handleAmend(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("rejects empty files list", func(t *testing.T) {
		setWorkspaceAhead(t, st, "ws-amend", 1)

		body, _ := json.Marshal(map[string][]string{"files": {}})
		req := makeWorkspaceRequest(t, http.MethodPost, "/api/workspaces/ws-amend/amend", "ws-amend", body)
		rr := httptest.NewRecorder()
		server.handleAmend(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		setWorkspaceAhead(t, st, "ws-amend", 1)

		body, _ := json.Marshal(map[string][]string{"files": {"../../etc/passwd"}})
		req := makeWorkspaceRequest(t, http.MethodPost, "/api/workspaces/ws-amend/amend", "ws-amend", body)
		rr := httptest.NewRecorder()
		server.handleAmend(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("rejects absolute path", func(t *testing.T) {
		setWorkspaceAhead(t, st, "ws-amend", 1)

		body, _ := json.Marshal(map[string][]string{"files": {"/etc/passwd"}})
		req := makeWorkspaceRequest(t, http.MethodPost, "/api/workspaces/ws-amend/amend", "ws-amend", body)
		rr := httptest.NewRecorder()
		server.handleAmend(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}

func TestHandleGitDiscard_Guards(t *testing.T) {
	server, _, st := newTestServer(t)

	ws := state.Workspace{
		ID:     "ws-discard",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   t.TempDir(),
	}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	t.Run("rejects malformed body", func(t *testing.T) {
		// Malformed JSON that's not empty — should be rejected, not treated as discard-all
		req := makeWorkspaceRequest(t, http.MethodPost, "/api/workspaces/ws-discard/discard", "ws-discard", []byte(`{invalid`))
		rr := httptest.NewRecorder()
		server.handleDiscard(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("rejects path traversal in files", func(t *testing.T) {
		body, _ := json.Marshal(map[string][]string{"files": {"../../../etc/shadow"}})
		req := makeWorkspaceRequest(t, http.MethodPost, "/api/workspaces/ws-discard/discard", "ws-discard", body)
		rr := httptest.NewRecorder()
		server.handleDiscard(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("rejects workspace not found", func(t *testing.T) {
		req := makeWorkspaceRequest(t, http.MethodPost, "/api/workspaces/nonexistent/discard", "nonexistent", nil)
		rr := httptest.NewRecorder()
		server.handleDiscard(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}

func TestHandleGitGraph_RejectsUnsupportedVCSWorkspace(t *testing.T) {
	server, _, st := newTestServer(t)

	ws := state.Workspace{
		ID:     "ws-unsupported",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   t.TempDir(),
		VCS:    "mercurial",
	}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	req := makeWorkspaceRequest(t, http.MethodGet, "/api/workspaces/ws-unsupported/commit-graph", "ws-unsupported", nil)
	rr := httptest.NewRecorder()
	server.handleWorkspaceCommitGraph(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "not available") {
		t.Errorf("expected response to contain 'not available', got: %s", rr.Body.String())
	}
}

// Note: TestHandleGitCommitStage_RejectsNonGitWorkspace, TestHandleGitAmend_RejectsNonGitWorkspace,
// TestHandleGitDiscard_RejectsNonGitWorkspace, and TestHandleGitUncommit_RejectsNonGitWorkspace
// were removed — these handlers are now VCS-agnostic via CommandBuilder.

func TestHandleGitCommitStage_Guards(t *testing.T) {
	server, _, st := newTestServer(t)

	ws := state.Workspace{
		ID:     "ws-stage",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   t.TempDir(),
	}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	t.Run("rejects malformed body", func(t *testing.T) {
		req := makeWorkspaceRequest(t, http.MethodPost, "/api/workspaces/ws-stage/stage", "ws-stage", []byte(`not json`))
		rr := httptest.NewRecorder()
		server.handleStage(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		body, _ := json.Marshal(map[string][]string{"files": {"../../secrets.env"}})
		req := makeWorkspaceRequest(t, http.MethodPost, "/api/workspaces/ws-stage/stage", "ws-stage", body)
		rr := httptest.NewRecorder()
		server.handleStage(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}
