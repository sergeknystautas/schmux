package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/state"
)

func TestHandleDiff_RejectsNonGitWorkspace(t *testing.T) {
	server, _, st := newTestServer(t)

	ws := state.Workspace{
		ID:     "ws-sapling",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   t.TempDir(),
		VCS:    "sapling",
	}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/diff/ws-sapling", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "ws-sapling")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	server.handleDiff(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "not available") {
		t.Errorf("expected response to contain 'not available', got: %s", rr.Body.String())
	}
}
