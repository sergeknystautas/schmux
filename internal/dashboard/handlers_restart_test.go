package dashboard

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/state"
)

// newRestartHandler builds a SpawnHandlers over a fresh state seeded with
// sessions covering each restart guard. models is wired so the resume_id_args
// check can resolve builtin targets.
func newRestartHandler(t *testing.T) *SpawnHandlers {
	t.Helper()
	st := state.New(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "git@github.com:u/r.git", Branch: "main", Path: t.TempDir()}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	now := time.Now()
	sessions := []state.Session{
		{ID: "no-resume", WorkspaceID: "ws-1", Target: "claude", ResumeID: "", CreatedAt: now},
		{ID: "remote-1", WorkspaceID: "ws-1", Target: "claude", ResumeID: "conv", RemoteHostID: "host-1", CreatedAt: now},
		{ID: "disposing-1", WorkspaceID: "ws-1", Target: "claude", ResumeID: "conv", Status: "disposing", CreatedAt: now},
		{ID: "gemini-1", WorkspaceID: "ws-1", Target: "gemini", ResumeID: "conv", CreatedAt: now},
	}
	for _, s := range sessions {
		if err := st.AddSession(s); err != nil {
			t.Fatalf("AddSession %s: %v", s.ID, err)
		}
	}
	return &SpawnHandlers{
		logger: discardLogger(),
		state:  st,
		config: &config.Config{},
		models: &models.Manager{},
	}
}

func postRestart(t *testing.T, h *SpawnHandlers, sessionID string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/api/sessions/{sessionID}/restart", h.handleRestart)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/restart", strings.NewReader("{}"))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestHandleRestart_Guards(t *testing.T) {
	h := newRestartHandler(t)

	cases := []struct {
		id       string
		wantCode int
		wantBody string
	}{
		{"nope", http.StatusNotFound, ""},
		{"no-resume", http.StatusBadRequest, "resume id"},
		{"remote-1", http.StatusBadRequest, "local"},
		{"disposing-1", http.StatusConflict, "disposing"},
		{"gemini-1", http.StatusBadRequest, "resume by id"},
	}
	for _, c := range cases {
		rr := postRestart(t, h, c.id)
		if rr.Code != c.wantCode {
			t.Errorf("%s: status = %d, want %d; body=%s", c.id, rr.Code, c.wantCode, rr.Body.String())
		}
		if c.wantBody != "" && !strings.Contains(rr.Body.String(), c.wantBody) {
			t.Errorf("%s: body = %q, want containing %q", c.id, rr.Body.String(), c.wantBody)
		}
	}
}
