package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestHandleTabCreate_Html(t *testing.T) {
	srv, _, st := newTestServer(t)
	wsH := newTestWorkspaceHandlers(srv)
	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-tab-html",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
	}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	body, _ := json.Marshal(createTabRequest{
		Kind:     "html",
		Filepath: "reports/coverage.html",
	})
	req := makeTabRequest(t, http.MethodPost, "/api/workspaces/ws-tab-html/tabs", "ws-tab-html", "", body)
	rr := httptest.NewRecorder()
	wsH.handleTabCreate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("POST tabs: status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var result map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["route"] == "" {
		t.Fatal("response missing route")
	}

	tabs := st.GetWorkspaceTabs("ws-tab-html")
	var htmlCount int
	for _, tab := range tabs {
		if tab.Kind == "html" {
			htmlCount++
		}
	}
	if htmlCount != 1 {
		t.Fatalf("expected 1 html tab, got %d", htmlCount)
	}
}

func TestHandleTabCreate_Mermaid(t *testing.T) {
	srv, _, st := newTestServer(t)
	wsH := newTestWorkspaceHandlers(srv)
	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-tab-mermaid",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
	}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	body, _ := json.Marshal(createTabRequest{
		Kind:     "mermaid",
		Filepath: "docs/architecture.mmd",
	})
	req := makeTabRequest(t, http.MethodPost, "/api/workspaces/ws-tab-mermaid/tabs", "ws-tab-mermaid", "", body)
	rr := httptest.NewRecorder()
	wsH.handleTabCreate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("POST tabs: status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var result map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["route"] != "/diff/ws-tab-mermaid/mmd/docs%2Farchitecture.mmd" {
		t.Fatalf("unexpected route %q", result["route"])
	}

	tabs := st.GetWorkspaceTabs("ws-tab-mermaid")
	if len(tabs) != 1 || tabs[0].Kind != "mermaid" {
		t.Fatalf("expected one mermaid tab, got %+v", tabs)
	}
}

func TestHandleTabCreate_FileNavigation(t *testing.T) {
	srv, _, st := newTestServer(t)
	wsH := newTestWorkspaceHandlers(srv)
	workspacePath := t.TempDir()
	for _, file := range []string{"README.md", "screenshot.png", "main.go"} {
		if err := os.WriteFile(filepath.Join(workspacePath, file), []byte("content"), 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}
	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-tab-file",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   workspacePath,
	}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	tests := []struct {
		name       string
		filePath   string
		navigation string
		route      string
		tabKind    string
	}{
		{
			name:       "markdown creates a tab",
			filePath:   "README.md",
			navigation: "tab",
			route:      "/diff/ws-tab-file/md/README.md",
			tabKind:    "markdown",
		},
		{
			name:       "image navigates directly",
			filePath:   "screenshot.png",
			navigation: "direct",
			route:      "/diff/ws-tab-file/img/screenshot.png",
		},
		{
			name:       "source navigates to selected diff",
			filePath:   "main.go",
			navigation: "direct",
			route:      "/diff/ws-tab-file?file=main.go",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(st.GetWorkspaceTabs("ws-tab-file"))
			body, _ := json.Marshal(createTabRequest{Kind: "file", Filepath: tt.filePath})
			req := makeTabRequest(t, http.MethodPost, "/api/workspaces/ws-tab-file/tabs", "ws-tab-file", "", body)
			rr := httptest.NewRecorder()
			wsH.handleTabCreate(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("POST tabs: status = %d, body = %s", rr.Code, rr.Body.String())
			}
			var result map[string]string
			if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if result["navigation"] != tt.navigation || result["route"] != tt.route {
				t.Fatalf("unexpected navigation response: %+v", result)
			}

			tabs := st.GetWorkspaceTabs("ws-tab-file")
			if tt.tabKind == "" {
				if len(tabs) != before {
					t.Fatalf("direct navigation created a tab: %+v", tabs)
				}
				return
			}
			if len(tabs) != before+1 || tabs[len(tabs)-1].Kind != tt.tabKind {
				t.Fatalf("expected a %s tab, got %+v", tt.tabKind, tabs)
			}
		})
	}
}

func TestHandleTabCreate_FileNavigationRejectsSymlink(t *testing.T) {
	srv, _, st := newTestServer(t)
	wsH := newTestWorkspaceHandlers(srv)
	workspacePath := t.TempDir()
	realPath := filepath.Join(workspacePath, "README.md")
	if err := os.WriteFile(realPath, []byte("content"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.Symlink(realPath, filepath.Join(workspacePath, "linked.md")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if err := st.AddWorkspace(state.Workspace{ID: "ws-tab-symlink", Path: workspacePath}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	body, _ := json.Marshal(createTabRequest{Kind: "file", Filepath: "linked.md"})
	req := makeTabRequest(t, http.MethodPost, "/api/workspaces/ws-tab-symlink/tabs", "ws-tab-symlink", "", body)
	rr := httptest.NewRecorder()
	wsH.handleTabCreate(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for symlink, got %d: %s", rr.Code, rr.Body.String())
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
