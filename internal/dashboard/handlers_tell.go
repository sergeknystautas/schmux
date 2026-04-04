package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type tellRequest struct {
	Message string `json:"message"`
}

func (s *Server) handleTellSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")

	var req tellRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		writeJSONError(w, "message is required", http.StatusBadRequest)
		return
	}

	// Look up session
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

	// Prefix with [from FM] server-side
	text := fmt.Sprintf("[from FM] %s", req.Message)

	// Get the runtime (works for both local and remote sessions)
	runtime, err := s.session.GetTracker(sessionID)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("failed to get session runtime: %v", err), http.StatusInternalServerError)
		return
	}

	// Clear any partial input before injecting the message to prevent
	// collision with operator typing. See injector.go for details.
	_ = runtime.SendTmuxKeyName("C-u")
	if _, err := runtime.SendInput(text); err != nil {
		writeJSONError(w, fmt.Sprintf("failed to send message: %v", err), http.StatusInternalServerError)
		return
	}
	if err := runtime.SendTmuxKeyName("Enter"); err != nil {
		writeJSONError(w, fmt.Sprintf("failed to send Enter: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]string{"status": "ok"})
}
