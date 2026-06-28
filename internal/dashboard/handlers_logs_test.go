package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestHandleLogsWebSocket_UnknownSource(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/ws/logs/bogus", nil)
	// chi URLParam needs a route context; simulate via the chi router below.
	rr := httptest.NewRecorder()
	r := s.logsTestRouter()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

// logsTestRouter stands up just the logs route so the handler's chi.URLParam
// lookup works without the full server.
func (s *Server) logsTestRouter() http.Handler {
	r := chi.NewRouter()
	r.HandleFunc("/ws/logs/{source}", s.handleLogsWebSocket)
	return r
}
