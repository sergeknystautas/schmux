package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/tmux"
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

	// Prefix with [from FM] server-side
	text := fmt.Sprintf("[from FM] %s", req.Message)

	// Branch: local vs remote
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
		if _, err := conn.SendKeys(r.Context(), sess.RemotePaneID, text+"\n"); err != nil {
			writeJSONError(w, fmt.Sprintf("failed to send message: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		tmuxSession := sess.TmuxSession
		if tmuxSession == "" {
			writeJSONError(w, "session is not running", http.StatusConflict)
			return
		}
		ctx := context.Background()
		// Clear any partial input before injecting the message to prevent
		// collision with operator typing. See injector.go for details.
		_ = tmux.SendKeys(ctx, tmuxSession, "C-u")
		if err := tmux.SendLiteral(ctx, tmuxSession, text); err != nil {
			writeJSONError(w, fmt.Sprintf("failed to send message: %v", err), http.StatusInternalServerError)
			return
		}
		if err := tmux.SendKeys(ctx, tmuxSession, "Enter"); err != nil {
			writeJSONError(w, fmt.Sprintf("failed to send Enter: %v", err), http.StatusInternalServerError)
			return
		}
	}

	writeJSON(w, map[string]string{"status": "ok"})
}
