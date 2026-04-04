package dashboard

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type captureResponse struct {
	SessionID string `json:"session_id"`
	Lines     int    `json:"lines"`
	Output    string `json:"output"`
}

func (s *Server) handleCaptureSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")

	lines := 50
	if linesStr := r.URL.Query().Get("lines"); linesStr != "" {
		if n, err := strconv.Atoi(linesStr); err == nil && n > 0 {
			lines = n
		}
	}

	sess, ok := s.state.GetSession(sessionID)
	if !ok {
		writeJSONError(w, "session not found", http.StatusNotFound)
		return
	}

	// Pre-flight: check that the session is actually reachable
	if sess.RemoteHostID != "" {
		if s.remoteManager == nil {
			writeJSONError(w, "remote manager not available", http.StatusServiceUnavailable)
			return
		}
		conn := s.remoteManager.GetConnection(sess.RemoteHostID)
		if conn == nil {
			writeJSONError(w, "remote host not connected", http.StatusServiceUnavailable)
			return
		}
	} else if sess.TmuxSession == "" {
		writeJSONError(w, "session is not running", http.StatusConflict)
		return
	}

	// Get the runtime (works for both local and remote sessions)
	runtime, err := s.session.GetTracker(sessionID)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("failed to get session runtime: %v", err), http.StatusInternalServerError)
		return
	}

	output, err := runtime.CaptureLastLines(r.Context(), lines)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("failed to capture output: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, captureResponse{
		SessionID: sessionID,
		Lines:     lines,
		Output:    output,
	})
}
