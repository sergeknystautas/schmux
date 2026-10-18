package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/disposal"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func (h *WorkspaceHandlers) handleDispose(w http.ResponseWriter, r *http.Request) {
	// Extract session ID from chi URL param
	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" {
		writeJSONError(w, "session ID is required", http.StatusBadRequest)
		return
	}

	// Mark as disposing and broadcast immediately for visual feedback
	sessionLog := logging.Sub(h.logger, "session")
	prevStatus, markErr := h.session.MarkSessionDisposing(sessionID)
	if markErr != nil {
		sessionLog.Warn("mark disposing failed, proceeding with dispose", "session_id", sessionID, "err", markErr)
	} else if prevStatus == "disposing" {
		// Idempotent: already disposing, return success
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	} else {
		h.broadcastSessions()
	}

	ctx, cancel := context.WithTimeout(context.Background(), h.config.DisposeGracePeriod()+10*time.Second)
	if err := h.session.Dispose(ctx, sessionID); err != nil {
		cancel()
		sessionLog.Error("dispose failed", "session_id", sessionID, "err", err)
		// Revert status on failure
		if markErr == nil {
			h.session.RevertSessionStatus(sessionID, prevStatus)
			h.broadcastSessions()
		}
		writeJSONError(w, fmt.Sprintf("Failed to dispose session: %v", err), http.StatusInternalServerError)
		return
	}
	cancel()
	sessionLog.Info("dispose success", "session_id", sessionID)

	// Clean up rotation lock for disposed session
	h.rotationLocksMu.Lock()
	delete(h.rotationLocks, sessionID)
	h.rotationLocksMu.Unlock()

	// Delete previews owned by this session
	if h.previewManager != nil {
		if deleted, err := h.previewManager.DeleteBySession(sessionID); err != nil {
			previewLog := logging.Sub(h.logger, "preview")
			previewLog.Warn("preview cleanup on dispose failed", "session_id", sessionID, "err", err)
		} else if deleted > 0 {
			go h.broadcastSessions()
		}
	}

	// Broadcast update to WebSocket clients
	go h.broadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		h.logger.Error("failed to encode response", "handler", "dispose-session", "err", err)
	}
}

// handleDisposeWorkspace handles workspace disposal requests.
func (h *WorkspaceHandlers) handleDisposeWorkspace(w http.ResponseWriter, r *http.Request) {
	// Extract workspace ID from chi URL param
	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		writeJSONError(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	// Block disposal of the workspace that is live in dev mode
	if devPath := h.devSourceWorkspacePath(); devPath != "" {
		if ws, ok := h.state.GetWorkspace(workspaceID); ok && ws.Path == devPath {
			writeJSONError(w, "cannot dispose workspace that is live in dev mode", http.StatusConflict)
			return
		}
	}

	// Mark as disposing and broadcast immediately for visual feedback
	workspaceLog := logging.Sub(h.logger, "workspace")
	prevWsStatus, markErr := h.workspace.MarkWorkspaceDisposing(workspaceID)
	if markErr != nil {
		workspaceLog.Warn("mark disposing failed, proceeding with dispose", "workspace_id", workspaceID, "err", markErr)
	} else if prevWsStatus == "disposing" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	} else {
		h.broadcastSessions()
	}

	// Use an independent context so disposal completes even if the client disconnects.
	// 5 minutes accommodates large worktrees on slow filesystems (NFS, FUSE).
	wsCtx, wsCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer wsCancel()

	if err := h.workspace.Dispose(wsCtx, workspaceID); err != nil {
		workspaceLog.Error("dispose failed", "workspace_id", workspaceID, "err", err)
		if markErr == nil {
			h.workspace.RevertWorkspaceStatus(workspaceID, prevWsStatus)
			h.broadcastSessions()
		}
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if h.previewManager != nil {
		if err := h.previewManager.DeleteWorkspace(workspaceID); err != nil {
			previewLog := logging.Sub(h.logger, "preview")
			previewLog.Warn("dispose cleanup failed", "workspace_id", workspaceID, "err", err)
		}
	}
	workspaceLog.Info("dispose success", "workspace_id", workspaceID)

	// Broadcast update to WebSocket clients
	go h.broadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		h.logger.Error("failed to encode response", "handler", "dispose-workspace", "err", err)
	}
}

// handleDisposeWorkspaceAll handles workspace disposal requests including all sessions.
func (h *WorkspaceHandlers) handleDisposeWorkspaceAll(w http.ResponseWriter, r *http.Request) {
	// Extract workspace ID from chi URL param
	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		writeJSONError(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	// Block disposal of the workspace that is live in dev mode
	if devPath := h.devSourceWorkspacePath(); devPath != "" {
		if ws, ok := h.state.GetWorkspace(workspaceID); ok && ws.Path == devPath {
			writeJSONError(w, "cannot dispose workspace that is live in dev mode", http.StatusConflict)
			return
		}
	}

	// Optional body. An absent body is valid and means "dispose only".
	var req contracts.DisposeWorkspaceAllRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeJSONError(w, "invalid request body", http.StatusBadRequest)
			return
		}
	}

	result, err := h.disposalService().DisposeAll(r.Context(), disposal.Request{
		WorkspaceID:        workspaceID,
		DeleteRemoteBranch: req.DeleteRemoteBranch,
	}, nil)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, workspace.ErrRemoteBranchNotMerged) {
			status = http.StatusConflict
		}
		writeJSONError(w, err.Error(), status)
		return
	}
	if result.WorkspaceMissing {
		writeJSONError(w, fmt.Sprintf("workspace not found: %s", workspaceID), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if result.AlreadyDisposing {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status":            "ok",
		"sessions_disposed": len(result.SessionsDisposed),
	}); err != nil {
		h.logger.Error("failed to encode response", "handler", "dispose-all", "err", err)
	}
}

// disposalService builds the shared full-workspace disposal service from the
// handler's dependencies.
func (h *WorkspaceHandlers) disposalService() *disposal.Service {
	svc := &disposal.Service{
		Sessions:   h.session,
		Workspaces: h.workspace,
		State:      h.state,
		Broadcast:  h.broadcastSessions,
		SessionDisposed: func(sessionID string) {
			h.rotationLocksMu.Lock()
			delete(h.rotationLocks, sessionID)
			h.rotationLocksMu.Unlock()
		},
		Logger: h.logger,
	}
	if h.previewManager != nil {
		svc.Previews = h.previewManager
	}
	return svc
}

func (h *WorkspaceHandlers) handlePurgeWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		writeJSONError(w, "workspace ID is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := h.workspace.Purge(ctx, workspaceID); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if h.previewManager != nil {
		h.previewManager.DeleteWorkspace(workspaceID)
	}

	go h.broadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *WorkspaceHandlers) handlePurgeAll(w http.ResponseWriter, r *http.Request) {
	repoURL := r.URL.Query().Get("repo")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	purged, err := h.workspace.PurgeAll(ctx, repoURL)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	go h.broadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"purged": purged,
	})
}

func (h *WorkspaceHandlers) handleGetRecyclableWorkspaces(w http.ResponseWriter, r *http.Request) {
	workspaces := h.state.GetWorkspaces()
	total := 0
	byRepo := make(map[string]int)
	for _, ws := range workspaces {
		if ws.Status != state.WorkspaceStatusRecyclable {
			continue
		}
		total++
		repoName := ws.Repo
		if rc, found := h.config.FindRepoByURL(ws.Repo); found {
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
