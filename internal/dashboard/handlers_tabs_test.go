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

// makeTabRequest creates an HTTP request with chi route context for a tab endpoint.
func makeTabRequest(t *testing.T, method, path, workspaceID, tabID string, body []byte) *http.Request {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceID", workspaceID)
	if tabID != "" {
		rctx.URLParams.Add("tabID", tabID)
	}
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return req
}

func TestHandleTabCreate(t *testing.T) {
	srv, _, st := newTestServer(t)
	wsH := newTestWorkspaceHandlers(srv)
	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-tab-create",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
	}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	body, _ := json.Marshal(createTabRequest{
		Kind: "commit",
		Hash: "abc123def456",
	})
	req := makeTabRequest(t, http.MethodPost, "/api/workspaces/ws-tab-create/tabs", "ws-tab-create", "", body)
	rr := httptest.NewRecorder()
	wsH.handleTabCreate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("POST tabs: status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var result map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["id"] == "" {
		t.Fatal("response missing tab id")
	}
	if result["route"] == "" {
		t.Fatal("response missing route")
	}
	if result["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", result["status"])
	}

	// Verify tab was created in state.
	tabs := st.GetWorkspaceTabs("ws-tab-create")
	var commitCount int
	for _, tab := range tabs {
		if tab.Kind == "commit" {
			commitCount++
		}
	}
	if commitCount != 1 {
		t.Fatalf("expected 1 commit tab, got %d", commitCount)
	}
}

func TestHandleTabCreate_DisallowedKind(t *testing.T) {
	srv, _, st := newTestServer(t)
	wsH := newTestWorkspaceHandlers(srv)
	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-tab-kind",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
	}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	body, _ := json.Marshal(createTabRequest{
		Kind: "preview",
	})
	req := makeTabRequest(t, http.MethodPost, "/api/workspaces/ws-tab-kind/tabs", "ws-tab-kind", "", body)
	rr := httptest.NewRecorder()
	wsH.handleTabCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for disallowed kind, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleTabCreate_WorkspaceNotFound(t *testing.T) {
	srv, _, _ := newTestServer(t)
	wsH := newTestWorkspaceHandlers(srv)

	body, _ := json.Marshal(createTabRequest{
		Kind: "commit",
		Hash: "abc123",
	})
	req := makeTabRequest(t, http.MethodPost, "/api/workspaces/nonexistent/tabs", "nonexistent", "", body)
	rr := httptest.NewRecorder()
	wsH.handleTabCreate(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for missing workspace, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleTabCreate_InvalidBody(t *testing.T) {
	srv, _, st := newTestServer(t)
	wsH := newTestWorkspaceHandlers(srv)
	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-tab-bad-body",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
	}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	req := makeTabRequest(t, http.MethodPost, "/api/workspaces/ws-tab-bad-body/tabs", "ws-tab-bad-body", "", []byte(`{not json}`))
	rr := httptest.NewRecorder()
	wsH.handleTabCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid body, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleTabDelete(t *testing.T) {
	srv, _, st := newTestServer(t)
	wsH := newTestWorkspaceHandlers(srv)
	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-tab-del",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
	}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	tab, err := srv.workspace.OpenCommitTab("ws-tab-del", "abc123def456")
	if err != nil {
		t.Fatalf("failed to open commit tab: %v", err)
	}

	req := makeTabRequest(t, http.MethodDelete, "/api/workspaces/ws-tab-del/tabs/"+tab.ID, "ws-tab-del", tab.ID, nil)
	rr := httptest.NewRecorder()
	wsH.handleTabDelete(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("DELETE tab: status = %d, body = %s", rr.Code, rr.Body.String())
	}

	tabs := st.GetWorkspaceTabs("ws-tab-del")
	for _, tt := range tabs {
		if tt.ID == tab.ID {
			t.Fatal("tab should have been removed")
		}
	}
}

func TestHandleTabDelete_NonClosable(t *testing.T) {
	srv, _, st := newTestServer(t)
	wsH := newTestWorkspaceHandlers(srv)
	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-tab-nc",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
	}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	// Add a non-closable diff tab directly to state
	if err := st.AddTab("ws-tab-nc", state.Tab{
		ID: "sys-diff-ws-tab-nc", Kind: "diff",
		Route: "/diff/ws-tab-nc", Closable: false,
	}); err != nil {
		t.Fatalf("failed to add diff tab: %v", err)
	}

	tabs := st.GetWorkspaceTabs("ws-tab-nc")
	var diffTabID string
	for _, tab := range tabs {
		if tab.Kind == "diff" {
			diffTabID = tab.ID
			break
		}
	}
	if diffTabID == "" {
		t.Fatal("no diff tab found after seeding")
	}

	req := makeTabRequest(t, http.MethodDelete, "/api/workspaces/ws-tab-nc/tabs/"+diffTabID, "ws-tab-nc", diffTabID, nil)
	rr := httptest.NewRecorder()
	wsH.handleTabDelete(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("DELETE non-closable tab: status = %d, want 400", rr.Code)
	}
}

func TestHandleTabDelete_NotFound(t *testing.T) {
	srv, _, st := newTestServer(t)
	wsH := newTestWorkspaceHandlers(srv)
	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-tab-nf",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
	}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	req := makeTabRequest(t, http.MethodDelete, "/api/workspaces/ws-tab-nf/tabs/nonexistent-tab", "ws-tab-nf", "nonexistent-tab", nil)
	rr := httptest.NewRecorder()
	wsH.handleTabDelete(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing tab: status = %d, want 404", rr.Code)
	}
}
