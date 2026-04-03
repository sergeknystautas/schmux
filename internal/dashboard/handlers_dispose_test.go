package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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

func TestHandleUpdateXtermTitle(t *testing.T) {
	server, _, st := newTestServer(t)

	st.AddWorkspace(state.Workspace{
		ID:     "ws-title",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   t.TempDir(),
	})
	st.AddSession(state.Session{
		ID:          "sess-title",
		WorkspaceID: "ws-title",
		Target:      "claude",
	})

	t.Run("updates title successfully", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"title": "Working on feature X"})
		req := makeSessionRequest(t, http.MethodPut, "/api/sessions-xterm-title/sess-title", "sess-title", body)
		rr := httptest.NewRecorder()
		server.handleUpdateXtermTitle(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		sess, _ := st.GetSession("sess-title")
		if sess.XtermTitle != "Working on feature X" {
			t.Errorf("XtermTitle = %q, want %q", sess.XtermTitle, "Working on feature X")
		}
	})

	t.Run("rejects missing session ID", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"title": "test"})
		req := makeSessionRequest(t, http.MethodPut, "/api/sessions-xterm-title/", "", body)
		rr := httptest.NewRecorder()
		server.handleUpdateXtermTitle(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("rejects malformed body", func(t *testing.T) {
		req := makeSessionRequest(t, http.MethodPut, "/api/sessions-xterm-title/sess-title", "sess-title", []byte(`{broken`))
		rr := httptest.NewRecorder()
		server.handleUpdateXtermTitle(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("clears title with empty string", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"title": ""})
		req := makeSessionRequest(t, http.MethodPut, "/api/sessions-xterm-title/sess-title", "sess-title", body)
		rr := httptest.NewRecorder()
		server.handleUpdateXtermTitle(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		sess, _ := st.GetSession("sess-title")
		if sess.XtermTitle != "" {
			t.Errorf("XtermTitle should be empty after clearing, got %q", sess.XtermTitle)
		}
	})
}

func TestHandleDispose_NonexistentSession(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := makeSessionRequest(t, http.MethodDelete, "/api/sessions/nonexistent-id/dispose", "nonexistent-id", nil)
	rr := httptest.NewRecorder()
	server.handleDispose(rr, req)

	// MarkSessionDisposing fails silently (warns), then Dispose returns error → 500
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for nonexistent session, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleDispose_SuccessfulDispose(t *testing.T) {
	server, _, st := newTestServer(t)

	st.AddWorkspace(state.Workspace{
		ID:     "ws-disp",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   t.TempDir(),
	})
	st.AddSession(state.Session{
		ID:          "sess-disp",
		WorkspaceID: "ws-disp",
		Target:      "command",
		TmuxSession: "schmux-nonexistent-xyz",
		Status:      "stopped",
	})

	req := makeSessionRequest(t, http.MethodDelete, "/api/sessions/sess-disp/dispose", "sess-disp", nil)
	rr := httptest.NewRecorder()
	server.handleDispose(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", resp["status"])
	}

	// Session should be gone from state
	if _, found := st.GetSession("sess-disp"); found {
		t.Error("session should have been removed from state")
	}
}

func TestHandleDispose_AlreadyDisposing(t *testing.T) {
	server, _, st := newTestServer(t)

	st.AddWorkspace(state.Workspace{
		ID:     "ws-adis",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   t.TempDir(),
	})
	st.AddSession(state.Session{
		ID:          "sess-adis",
		WorkspaceID: "ws-adis",
		Target:      "command",
		TmuxSession: "schmux-nonexistent-adis",
		Status:      state.SessionStatusDisposing,
	})

	req := makeSessionRequest(t, http.MethodDelete, "/api/sessions/sess-adis/dispose", "sess-adis", nil)
	rr := httptest.NewRecorder()
	server.handleDispose(rr, req)

	// Idempotent — should return OK without re-disposing
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 (idempotent), got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", resp["status"])
	}
}

func TestHandleDisposeWorkspace_NonexistentWorkspace(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := makeWorkspaceRequest(t, http.MethodDelete, "/api/workspaces/nonexistent-ws/dispose", "nonexistent-ws", nil)
	rr := httptest.NewRecorder()
	server.handleDisposeWorkspace(rr, req)

	// MarkWorkspaceDisposing fails, then Dispose also fails → 400
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for nonexistent workspace, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleDisposeWorkspace_NoGitRepo(t *testing.T) {
	server, _, st := newTestServer(t)

	// Temp dir without git — Dispose runs git safety checks and fails
	wsDir := t.TempDir()
	st.AddWorkspace(state.Workspace{
		ID:     "ws-nogit",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   wsDir,
	})

	req := makeWorkspaceRequest(t, http.MethodDelete, "/api/workspaces/ws-nogit/dispose", "ws-nogit", nil)
	rr := httptest.NewRecorder()
	server.handleDisposeWorkspace(rr, req)

	// Safety check fails → 400
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 (safety check failure), got %d: %s", rr.Code, rr.Body.String())
	}

	// Workspace status should be reverted (not stuck in "disposing")
	ws, found := st.GetWorkspace("ws-nogit")
	if !found {
		t.Fatal("workspace should still exist after failed dispose")
	}
	if ws.Status == "disposing" {
		t.Error("workspace status should have been reverted after failed dispose")
	}
}

func TestHandleDisposeWorkspaceAll_NonexistentWorkspace(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := makeWorkspaceRequest(t, http.MethodDelete, "/api/workspaces/nonexistent-ws/dispose-all", "nonexistent-ws", nil)
	rr := httptest.NewRecorder()
	server.handleDisposeWorkspaceAll(rr, req)

	// MarkWorkspaceDisposing fails, then DisposeForce also fails → 400
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for nonexistent workspace, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleDisposeWorkspaceAll_WithSessions(t *testing.T) {
	server, _, st := newTestServer(t)

	wsDir := t.TempDir()
	st.AddWorkspace(state.Workspace{
		ID:     "ws-all",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   wsDir,
	})
	st.AddSession(state.Session{
		ID:          "sess-all-1",
		WorkspaceID: "ws-all",
		Target:      "command",
		TmuxSession: "schmux-nonexistent-all1",
		Status:      "stopped",
	})
	st.AddSession(state.Session{
		ID:          "sess-all-2",
		WorkspaceID: "ws-all",
		Target:      "command",
		TmuxSession: "schmux-nonexistent-all2",
		Status:      "stopped",
	})

	req := makeWorkspaceRequest(t, http.MethodDelete, "/api/workspaces/ws-all/dispose-all", "ws-all", nil)
	rr := httptest.NewRecorder()
	server.handleDisposeWorkspaceAll(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", resp["status"])
	}
	disposed, ok := resp["sessions_disposed"].(float64)
	if !ok || disposed != 2 {
		t.Errorf("expected sessions_disposed=2, got %v", resp["sessions_disposed"])
	}

	// Both sessions should be gone
	if _, found := st.GetSession("sess-all-1"); found {
		t.Error("session sess-all-1 should have been removed")
	}
	if _, found := st.GetSession("sess-all-2"); found {
		t.Error("session sess-all-2 should have been removed")
	}

	// Workspace should be gone
	if _, found := st.GetWorkspace("ws-all"); found {
		t.Error("workspace should have been removed")
	}
}

func TestHandlePurgeWorkspace(t *testing.T) {
	server, _, st := newTestServer(t)

	workspacePath := filepath.Join(t.TempDir(), "test-001")
	os.MkdirAll(workspacePath, 0755)
	exec.Command("git", "init", "-q", workspacePath).Run()

	st.AddWorkspace(state.Workspace{
		ID:     "test-001",
		Repo:   "test",
		Branch: "main",
		Path:   workspacePath,
		Status: state.WorkspaceStatusRecyclable,
	})

	req := makeWorkspaceRequest(t, http.MethodDelete, "/api/workspaces/test-001/purge", "test-001", nil)
	rr := httptest.NewRecorder()
	server.handlePurgeWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	_, found := st.GetWorkspace("test-001")
	if found {
		t.Error("workspace should be removed after purge")
	}
}

func TestHandlePurgeAll(t *testing.T) {
	server, _, st := newTestServer(t)

	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("test-%03d", i+1)
		path := filepath.Join(t.TempDir(), id)
		os.MkdirAll(path, 0755)
		exec.Command("git", "init", "-q", path).Run()
		st.AddWorkspace(state.Workspace{
			ID: id, Repo: "test", Branch: "main", Path: path,
			Status: state.WorkspaceStatusRecyclable,
		})
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/workspaces/purge", nil)
	rr := httptest.NewRecorder()
	server.handlePurgeAll(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}
