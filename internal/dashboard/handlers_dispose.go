package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/logging"
)

func (s *Server) handleDispose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from chi URL param
	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		writeJSONError(w, "session ID is required", http.StatusBadRequest)
		return
	}

	// Get workspace ID before dispose for preview cleanup
	sess, hasSess := s.state.GetSession(sessionID)
	workspaceID := ""
	if hasSess {
		workspaceID = sess.WorkspaceID
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.config.DisposeGracePeriod()+10*time.Second)
	sessionLog := logging.Sub(s.logger, "session")
	if err := s.session.Dispose(ctx, sessionID); err != nil {
		cancel()
		sessionLog.Error("dispose failed", "session_id", sessionID, "err", err)
		writeJSONError(w, fmt.Sprintf("Failed to dispose session: %v", err), http.StatusInternalServerError)
		return
	}
	cancel()
	sessionLog.Info("dispose success", "session_id", sessionID)

	// Clean up rotation lock for disposed session
	s.rotationLocksMu.Lock()
	delete(s.rotationLocks, sessionID)
	s.rotationLocksMu.Unlock()

	// Immediately reconcile previews for this workspace to clean up stale preview tabs
	if s.previewManager != nil && workspaceID != "" {
		go func() {
			if changed, _ := s.previewManager.ReconcileWorkspace(workspaceID); changed {
				s.BroadcastSessions()
			}
		}()
	}

	// Broadcast update to WebSocket clients
	go s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		s.logger.Error("failed to encode response", "handler", "dispose-session", "err", err)
	}
}

// handleDisposeWorkspace handles workspace disposal requests.
func (s *Server) handleDisposeWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract workspace ID from chi URL param
	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		writeJSONError(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	// Block disposal of the workspace that is live in dev mode
	if devPath := s.devSourceWorkspacePath(); devPath != "" {
		if ws, ok := s.state.GetWorkspace(workspaceID); ok && ws.Path == devPath {
			writeJSONError(w, "cannot dispose workspace that is live in dev mode", http.StatusConflict)
			return
		}
	}

	workspaceLog := logging.Sub(s.logger, "workspace")
	if err := s.workspace.Dispose(workspaceID); err != nil {
		workspaceLog.Error("dispose failed", "workspace_id", workspaceID, "err", err)
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.previewManager != nil {
		if err := s.previewManager.DeleteWorkspace(workspaceID); err != nil {
			previewLog := logging.Sub(s.logger, "preview")
			previewLog.Warn("dispose cleanup failed", "workspace_id", workspaceID, "err", err)
		}
	}
	workspaceLog.Info("dispose success", "workspace_id", workspaceID)

	// Broadcast update to WebSocket clients
	go s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		s.logger.Error("failed to encode response", "handler", "dispose-workspace", "err", err)
	}
}

// handleDisposeWorkspaceAll handles workspace disposal requests including all sessions.
func (s *Server) handleDisposeWorkspaceAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract workspace ID from chi URL param
	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		writeJSONError(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	// Block disposal of the workspace that is live in dev mode
	if devPath := s.devSourceWorkspacePath(); devPath != "" {
		if ws, ok := s.state.GetWorkspace(workspaceID); ok && ws.Path == devPath {
			writeJSONError(w, "cannot dispose workspace that is live in dev mode", http.StatusConflict)
			return
		}
	}

	// First, dispose all sessions in the workspace concurrently
	sessions := s.state.GetSessions()
	var wsSessions []string
	for _, sess := range sessions {
		if sess.WorkspaceID == workspaceID {
			wsSessions = append(wsSessions, sess.ID)
		}
	}

	type disposeResult struct {
		sessionID string
		err       error
	}
	results := make(chan disposeResult, len(wsSessions))
	for _, sid := range wsSessions {
		go func(id string) {
			ctx, cancel := context.WithTimeout(context.Background(), s.config.DisposeGracePeriod()+10*time.Second)
			defer cancel()
			results <- disposeResult{sessionID: id, err: s.session.Dispose(ctx, id)}
		}(sid)
	}

	var sessionsDisposed []string
	workspaceLog := logging.Sub(s.logger, "workspace")
	for range wsSessions {
		res := <-results
		if res.err != nil {
			workspaceLog.Error("dispose-all session failed", "session_id", res.sessionID, "err", res.err)
		} else {
			sessionsDisposed = append(sessionsDisposed, res.sessionID)
			workspaceLog.Info("dispose-all session disposed", "session_id", res.sessionID)
		}
	}

	// Clean up rotation locks for disposed sessions
	s.rotationLocksMu.Lock()
	for _, sid := range sessionsDisposed {
		delete(s.rotationLocks, sid)
	}
	s.rotationLocksMu.Unlock()

	// Then dispose the workspace
	if err := s.workspace.Dispose(workspaceID); err != nil {
		workspaceLog.Error("dispose-all workspace failed", "workspace_id", workspaceID, "err", err)
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.previewManager != nil {
		if err := s.previewManager.DeleteWorkspace(workspaceID); err != nil {
			previewLog := logging.Sub(s.logger, "preview")
			previewLog.Warn("dispose-all cleanup failed", "workspace_id", workspaceID, "err", err)
		}
	}
	workspaceLog.Info("dispose-all success", "workspace_id", workspaceID, "sessions_disposed", len(sessionsDisposed))

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status":            "ok",
		"sessions_disposed": len(sessionsDisposed),
	}); err != nil {
		s.logger.Error("failed to encode response", "handler", "dispose-all", "err", err)
	}
}
