package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/state"
)

func (s *Server) handleDispose(w http.ResponseWriter, r *http.Request) {
	// Extract session ID from chi URL param
	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		writeJSONError(w, "session ID is required", http.StatusBadRequest)
		return
	}

	// Mark as disposing and broadcast immediately for visual feedback
	sessionLog := logging.Sub(s.logger, "session")
	prevStatus, markErr := s.session.MarkSessionDisposing(sessionID)
	if markErr != nil {
		sessionLog.Warn("mark disposing failed, proceeding with dispose", "session_id", sessionID, "err", markErr)
	} else if prevStatus == "disposing" {
		// Idempotent: already disposing, return success
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	} else {
		s.BroadcastSessions()
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.config.DisposeGracePeriod()+10*time.Second)
	if err := s.session.Dispose(ctx, sessionID); err != nil {
		cancel()
		sessionLog.Error("dispose failed", "session_id", sessionID, "err", err)
		// Revert status on failure
		if markErr == nil {
			s.session.RevertSessionStatus(sessionID, prevStatus)
			s.BroadcastSessions()
		}
		writeJSONError(w, fmt.Sprintf("Failed to dispose session: %v", err), http.StatusInternalServerError)
		return
	}
	cancel()
	sessionLog.Info("dispose success", "session_id", sessionID)

	// Clean up rotation lock for disposed session
	s.rotationLocksMu.Lock()
	delete(s.rotationLocks, sessionID)
	s.rotationLocksMu.Unlock()

	// Delete previews owned by this session
	if s.previewManager != nil {
		if deleted, err := s.previewManager.DeleteBySession(sessionID); err != nil {
			previewLog := logging.Sub(s.logger, "preview")
			previewLog.Warn("preview cleanup on dispose failed", "session_id", sessionID, "err", err)
		} else if deleted > 0 {
			go s.BroadcastSessions()
		}
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

	// Mark as disposing and broadcast immediately for visual feedback
	workspaceLog := logging.Sub(s.logger, "workspace")
	prevWsStatus, markErr := s.workspace.MarkWorkspaceDisposing(workspaceID)
	if markErr != nil {
		workspaceLog.Warn("mark disposing failed, proceeding with dispose", "workspace_id", workspaceID, "err", markErr)
	} else if prevWsStatus == "disposing" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	} else {
		s.BroadcastSessions()
	}

	// Use an independent context so disposal completes even if the client disconnects
	wsCtx, wsCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer wsCancel()

	if err := s.workspace.Dispose(wsCtx, workspaceID); err != nil {
		workspaceLog.Error("dispose failed", "workspace_id", workspaceID, "err", err)
		if markErr == nil {
			s.workspace.RevertWorkspaceStatus(workspaceID, prevWsStatus)
			s.BroadcastSessions()
		}
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

	// Mark workspace as disposing and broadcast immediately
	workspaceLog := logging.Sub(s.logger, "workspace")
	prevWsStatus, markErr := s.workspace.MarkWorkspaceDisposing(workspaceID)
	if markErr != nil {
		workspaceLog.Warn("mark disposing failed, proceeding with dispose", "workspace_id", workspaceID, "err", markErr)
	} else if prevWsStatus == "disposing" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	// Mark all sessions as disposing
	sessions := s.state.GetSessions()
	var wsSessions []string
	for _, sess := range sessions {
		if sess.WorkspaceID == workspaceID {
			wsSessions = append(wsSessions, sess.ID)
			if _, sessMarkErr := s.session.MarkSessionDisposing(sess.ID); sessMarkErr != nil {
				workspaceLog.Warn("failed to mark session disposing", "session_id", sess.ID, "err", sessMarkErr)
			}
		}
	}

	// Broadcast immediately — everything grays out at once
	s.BroadcastSessions()

	// Dispose all sessions concurrently
	type disposeResult struct {
		sessionID string
		err       error
	}
	results := make(chan disposeResult, len(wsSessions))
	for _, sid := range wsSessions {
		go func(id string) {
			// Use a generous fixed timeout independent of DisposeGracePeriod.
			// DisposeGracePeriod controls the interactive user-facing delay,
			// but bulk disposal (especially under CPU/IO contention) needs
			// enough headroom for tmux subprocess operations to complete.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			results <- disposeResult{sessionID: id, err: s.session.Dispose(ctx, id)}
		}(sid)
	}

	var sessionsDisposed []string
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

	// Then dispose the workspace — use an independent context since session
	// disposal above may have consumed part of the client's timeout budget.
	// Use DisposeForce to skip safety checks: the user explicitly asked to
	// destroy everything, and sessions were already disposed above.
	wsCtx, wsCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer wsCancel()
	if err := s.workspace.DisposeForce(wsCtx, workspaceID); err != nil {
		workspaceLog.Error("dispose-all workspace failed", "workspace_id", workspaceID, "err", err)
		if markErr == nil {
			s.workspace.RevertWorkspaceStatus(workspaceID, prevWsStatus)
			go s.BroadcastSessions()
		}
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

	go s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status":            "ok",
		"sessions_disposed": len(sessionsDisposed),
	}); err != nil {
		s.logger.Error("failed to encode response", "handler", "dispose-all", "err", err)
	}
}

func (s *Server) handlePurgeWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		writeJSONError(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := s.workspace.Purge(ctx, workspaceID); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.previewManager != nil {
		s.previewManager.DeleteWorkspace(workspaceID)
	}

	go s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handlePurgeAll(w http.ResponseWriter, r *http.Request) {
	repoURL := r.URL.Query().Get("repo")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	purged, err := s.workspace.PurgeAll(ctx, repoURL)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	go s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"purged": purged,
	})
}

func (s *Server) handleGetRecyclableWorkspaces(w http.ResponseWriter, r *http.Request) {
	workspaces := s.state.GetWorkspaces()
	total := 0
	byRepo := make(map[string]int)
	for _, ws := range workspaces {
		if ws.Status != state.WorkspaceStatusRecyclable {
			continue
		}
		total++
		repoName := ws.Repo
		if rc, found := s.config.FindRepoByURL(ws.Repo); found {
			repoName = rc.Name
		}
		byRepo[repoName]++
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total":   total,
		"by_repo": byRepo,
	})
}
