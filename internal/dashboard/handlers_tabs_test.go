package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-tab-create",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
	}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	body, _ := json.Marshal(createTabRequest{
		Kind:     "markdown",
		Label:    "README.md",
		Route:    "/diff/ws-tab-create/md/README.md",
		Closable: true,
		Meta:     map[string]string{"filepath": "README.md"},
	})
	req := makeTabRequest(t, http.MethodPost, "/api/workspaces/ws-tab-create/tabs", "ws-tab-create", "", body)
	rr := httptest.NewRecorder()
	srv.handleTabCreate(rr, req)

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
	if result["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", result["status"])
	}

	// Verify tab was created in state.
	tabs := st.GetWorkspaceTabs("ws-tab-create")
	var mdCount int
	for _, tab := range tabs {
		if tab.Kind == "markdown" {
			mdCount++
		}
	}
	if mdCount != 1 {
		t.Fatalf("expected 1 markdown tab, got %d", mdCount)
	}
}

func TestHandleTabCreate_DisallowedKind(t *testing.T) {
	srv, _, st := newTestServer(t)
	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-tab-kind",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
	}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	body, _ := json.Marshal(createTabRequest{
		Kind:     "preview",
		Label:    "Preview",
		Route:    "/preview/ws-tab-kind/p1",
		Closable: true,
	})
	req := makeTabRequest(t, http.MethodPost, "/api/workspaces/ws-tab-kind/tabs", "ws-tab-kind", "", body)
	rr := httptest.NewRecorder()
	srv.handleTabCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for disallowed kind, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleTabCreate_WorkspaceNotFound(t *testing.T) {
	srv, _, _ := newTestServer(t)

	body, _ := json.Marshal(createTabRequest{
		Kind:     "markdown",
		Label:    "README.md",
		Route:    "/diff/nonexistent/md/README.md",
		Closable: true,
	})
	req := makeTabRequest(t, http.MethodPost, "/api/workspaces/nonexistent/tabs", "nonexistent", "", body)
	rr := httptest.NewRecorder()
	srv.handleTabCreate(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing workspace, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleTabCreate_InvalidBody(t *testing.T) {
	srv, _, st := newTestServer(t)
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
	srv.handleTabCreate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid body, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleTabDelete(t *testing.T) {
	srv, _, st := newTestServer(t)
	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-tab-del",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
	}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	tab := state.Tab{
		ID:       "tab-del-1",
		Kind:     "markdown",
		Label:    "README.md",
		Route:    "/diff/ws-tab-del/md/README.md",
		Closable: true,
	}
	if err := st.AddTab("ws-tab-del", tab); err != nil {
		t.Fatalf("failed to add tab: %v", err)
	}

	req := makeTabRequest(t, http.MethodDelete, "/api/workspaces/ws-tab-del/tabs/tab-del-1", "ws-tab-del", "tab-del-1", nil)
	rr := httptest.NewRecorder()
	srv.handleTabDelete(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("DELETE tab: status = %d, body = %s", rr.Code, rr.Body.String())
	}

	tabs := st.GetWorkspaceTabs("ws-tab-del")
	for _, tt := range tabs {
		if tt.ID == "tab-del-1" {
			t.Fatal("tab should have been removed")
		}
	}
}

func TestHandleTabDelete_RemovesResolveConflictRecord(t *testing.T) {
	srv, _, st := newTestServer(t)
	hash := "abcdef1"
	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-tab-rc",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
		Tabs: []state.Tab{
			{
				ID:       "sys-resolve-conflict-abcdef1",
				Kind:     "resolve-conflict",
				Label:    "Conflict abcdef1",
				Route:    "/resolve-conflict/ws-tab-rc/sys-resolve-conflict-abcdef1",
				Closable: true,
				Meta:     map[string]string{"hash": hash},
			},
		},
	}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}
	if err := st.UpsertResolveConflict("ws-tab-rc", state.ResolveConflict{
		Type:        "linear_sync_resolve_conflict",
		WorkspaceID: "ws-tab-rc",
		Status:      "done",
		Hash:        hash,
		StartedAt:   time.Now().Format(time.RFC3339),
		Steps:       []state.ResolveConflictStep{},
	}); err != nil {
		t.Fatalf("failed to add resolve conflict: %v", err)
	}
	srv.setLinearSyncResolveConflictState("ws-tab-rc", &LinearSyncResolveConflictState{
		ResolveConflict: state.ResolveConflict{
			Type:        "linear_sync_resolve_conflict",
			WorkspaceID: "ws-tab-rc",
			Status:      "done",
			Hash:        hash,
			StartedAt:   time.Now().Format(time.RFC3339),
			Steps:       []state.ResolveConflictStep{},
		},
	})

	req := makeTabRequest(
		t,
		http.MethodDelete,
		"/api/workspaces/ws-tab-rc/tabs/sys-resolve-conflict-abcdef1",
		"ws-tab-rc",
		"sys-resolve-conflict-abcdef1",
		nil,
	)
	rr := httptest.NewRecorder()
	srv.handleTabDelete(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("DELETE resolve-conflict tab: status = %d, body = %s", rr.Code, rr.Body.String())
	}

	if _, found := st.GetResolveConflict("ws-tab-rc", hash); found {
		t.Fatal("resolve-conflict record should have been removed")
	}
	if srv.getLinearSyncResolveConflictState("ws-tab-rc") != nil {
		t.Fatal("live resolve-conflict state should have been cleared")
	}
}

func TestEnsureResolveConflictTab_UsesShortHashKey(t *testing.T) {
	srv, _, st := newTestServer(t)
	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-tab-short",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
	}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	srv.ensureResolveConflictTab("ws-tab-short", "abcdef123456")

	tabs := st.GetWorkspaceTabs("ws-tab-short")
	for _, tab := range tabs {
		if tab.Kind != "resolve-conflict" {
			continue
		}
		if tab.ID != "sys-resolve-conflict-abcdef1" {
			t.Fatalf("resolve-conflict tab id = %q, want short-hash key", tab.ID)
		}
		if tab.Route != "/resolve-conflict/ws-tab-short/sys-resolve-conflict-abcdef1" {
			t.Fatalf("resolve-conflict tab route = %q", tab.Route)
		}
		if tab.Meta["hash"] != "abcdef1" {
			t.Fatalf("resolve-conflict tab hash = %q", tab.Meta["hash"])
		}
		return
	}
	t.Fatal("resolve-conflict tab not found")
}

func TestHandleTabDelete_NonClosable(t *testing.T) {
	srv, _, st := newTestServer(t)
	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-tab-nc",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
	}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	// AddWorkspace seeds a non-closable "diff" tab automatically.
	tabs := st.GetWorkspaceTabs("ws-tab-nc")
	var diffTabID string
	for _, tab := range tabs {
		if tab.Kind == "diff" {
			diffTabID = tab.ID
			break
		}
	}
	if diffTabID == "" {
		t.Fatal("no diff tab found after workspace creation")
	}

	req := makeTabRequest(t, http.MethodDelete, "/api/workspaces/ws-tab-nc/tabs/"+diffTabID, "ws-tab-nc", diffTabID, nil)
	rr := httptest.NewRecorder()
	srv.handleTabDelete(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("DELETE non-closable tab: status = %d, want 400", rr.Code)
	}
}

func TestHandleTabDelete_NotFound(t *testing.T) {
	srv, _, st := newTestServer(t)
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
	srv.handleTabDelete(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing tab: status = %d, want 404", rr.Code)
	}
}
