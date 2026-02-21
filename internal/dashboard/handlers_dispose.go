package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (s *Server) handleDispose(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL: /api/sessions/{id}/dispose
	sessionID := extractPathSegment(r.URL.Path, "/api/sessions/", "/dispose")
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
	if err := s.session.Dispose(ctx, sessionID); err != nil {
		cancel()
		fmt.Printf("[session] dispose error: session_id=%s error=%v\n", sessionID, err)
		writeJSONError(w, fmt.Sprintf("Failed to dispose session: %v", err), http.StatusInternalServerError)
		return
	}
	cancel()
	fmt.Printf("[session] dispose success: session_id=%s\n", sessionID)

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
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleDisposeWorkspace handles workspace disposal requests.
func (s *Server) handleDisposeWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract workspace ID from URL: /api/workspaces/{id}/dispose
	workspaceID := extractPathSegment(r.URL.Path, "/api/workspaces/", "/dispose")
	if workspaceID == "" {
		writeJSONError(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	// Block disposal of the workspace that is live in dev mode
	if devPath := s.devSourceWorkspacePath(); devPath != "" {
		if ws, ok := s.state.GetWorkspace(workspaceID); ok && ws.Path == devPath {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"error": "cannot dispose workspace that is live in dev mode"})
			return
		}
	}

	if err := s.workspace.Dispose(workspaceID); err != nil {
		fmt.Printf("[workspace] dispose error: workspace_id=%s error=%v\n", workspaceID, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest) // 400 for client-side errors like dirty state
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if s.previewManager != nil {
		if err := s.previewManager.DeleteWorkspace(workspaceID); err != nil {
			fmt.Printf("[preview] dispose cleanup warning: workspace_id=%s error=%v\n", workspaceID, err)
		}
	}
	fmt.Printf("[workspace] dispose success: workspace_id=%s\n", workspaceID)

	// Broadcast update to WebSocket clients
	go s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleDisposeWorkspaceAll handles workspace disposal requests including all sessions.
func (s *Server) handleDisposeWorkspaceAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract workspace ID from URL: /api/workspaces/{id}/dispose-all
	workspaceID := extractPathSegment(r.URL.Path, "/api/workspaces/", "/dispose-all")
	if workspaceID == "" {
		writeJSONError(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	// Block disposal of the workspace that is live in dev mode
	if devPath := s.devSourceWorkspacePath(); devPath != "" {
		if ws, ok := s.state.GetWorkspace(workspaceID); ok && ws.Path == devPath {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"error": "cannot dispose workspace that is live in dev mode"})
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
	for range wsSessions {
		res := <-results
		if res.err != nil {
			fmt.Printf("[workspace] dispose-all error: failed to dispose session %s: %v\n", res.sessionID, res.err)
		} else {
			sessionsDisposed = append(sessionsDisposed, res.sessionID)
			fmt.Printf("[workspace] dispose-all: disposed session %s\n", res.sessionID)
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
		fmt.Printf("[workspace] dispose-all error: workspace_id=%s error=%v\n", workspaceID, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if s.previewManager != nil {
		if err := s.previewManager.DeleteWorkspace(workspaceID); err != nil {
			fmt.Printf("[preview] dispose-all cleanup warning: workspace_id=%s error=%v\n", workspaceID, err)
		}
	}
	fmt.Printf("[workspace] dispose-all success: workspace_id=%s sessions_disposed=%d\n", workspaceID, len(sessionsDisposed))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":            "ok",
		"sessions_disposed": len(sessionsDisposed),
	})
}
