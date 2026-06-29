package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestHandleLogsWebSocket_UnknownSource(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/ws/logs/bogus", nil)
	rr := httptest.NewRecorder()
	s.logsTestRouter().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestHandleFenceLogWebSocket_Unknown404(t *testing.T) {
	server, _, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/ws/logs/fence/nope", nil)
	rr := httptest.NewRecorder()
	server.logsTestRouter().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("unknown session: status = %d, want 404", rr.Code)
	}
}

func TestHandleFenceLogWebSocket_NotFenced404(t *testing.T) {
	server, _, st := newTestServer(t)
	st.AddSession(state.Session{ID: "plain", Target: "command", CreatedAt: time.Now(), Fence: false})
	req := httptest.NewRequest(http.MethodGet, "/ws/logs/fence/plain", nil)
	rr := httptest.NewRecorder()
	server.logsTestRouter().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("non-fenced session: status = %d, want 404", rr.Code)
	}
}

func TestHandleFenceLogWebSocket_FencedNot404(t *testing.T) {
	server, _, st := newTestServer(t)
	st.AddSession(state.Session{ID: "fenced", Target: "command", CreatedAt: time.Now(), Fence: true})
	req := httptest.NewRequest(http.MethodGet, "/ws/logs/fence/fenced", nil)
	rr := httptest.NewRecorder()
	server.logsTestRouter().ServeHTTP(rr, req)
	// A non-websocket request can't be upgraded (recorder has no Hijack), so the
	// handler returns without a status — what matters is validation passed: not 404.
	if rr.Code == http.StatusNotFound {
		t.Errorf("fenced session should pass validation, got 404")
	}
}

// logsTestRouter stands up just the logs routes so chi.URLParam works.
func (s *Server) logsTestRouter() http.Handler {
	r := chi.NewRouter()
	r.HandleFunc("/ws/logs/{source}", s.handleLogsWebSocket)
	r.HandleFunc("/ws/logs/fence/{id}", s.handleFenceLogWebSocket)
	return r
}
