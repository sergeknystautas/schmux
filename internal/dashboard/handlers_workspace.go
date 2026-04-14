package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/preview"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// WorkspaceHandlers groups HTTP handlers for workspace-level operations including
// previews, tabs, overlays, dispose, repos, and backburner.
type WorkspaceHandlers struct {
	config         *config.Config
	state          state.StateStore
	workspace      workspace.WorkspaceManager
	session        *session.Manager
	logger         *log.Logger
	previewManager *preview.Manager

	// Rotation locks from Server (shared reference).
	rotationLocks   map[string]*sync.Mutex
	rotationLocksMu *sync.RWMutex

	// Callbacks into Server methods that cannot be extracted.
	broadcastSessions                    func()
	isTrustedRequest                     func(r *http.Request) bool
	lookupPortOwner                      func(port int) (int, error)
	devSourceWorkspacePath               func() string
	requireWorkspace                     func(w http.ResponseWriter, r *http.Request) (state.Workspace, bool)
	getLinearSyncResolveConflictState    func(workspaceID string) *LinearSyncResolveConflictState
	deleteLinearSyncResolveConflictState func(workspaceID string)
	triggerRepofeedPublish               func()
}

// previewResponse is a type alias for contracts.PreviewResponse.
type previewResponse = contracts.PreviewResponse

// validateWorkspaceID is a chi middleware that validates the {workspaceID} URL parameter.
// Rejects requests with IDs that contain path separators, null bytes, dots, or exceed 128 chars.
func validateWorkspaceID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workspaceID := chi.URLParam(r, "workspaceID")
		if !isValidResourceID(workspaceID) {
			writeJSONError(w, "invalid workspace ID", http.StatusBadRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isValidResourceID checks that an ID extracted from a URL path is safe:
// non-empty, no path separators, no null bytes, reasonable length.
func isValidResourceID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	if strings.ContainsAny(id, "/\\.\x00") {
		return false
	}
	return true
}

// previewsWorkspaceCheck validates workspace ID and returns the workspace.
// Returns false if an error response was written.
func (h *WorkspaceHandlers) previewsWorkspaceCheck(w http.ResponseWriter, r *http.Request) (string, state.Workspace, bool) {
	workspaceID := chi.URLParam(r, "workspaceID")
	if !isValidResourceID(workspaceID) {
		writeJSONError(w, "invalid workspace ID", http.StatusBadRequest)
		return "", state.Workspace{}, false
	}

	if h.previewManager == nil {
		writeJSONError(w, "preview manager not available", http.StatusServiceUnavailable)
		return "", state.Workspace{}, false
	}

	ws, found := h.state.GetWorkspace(workspaceID)
	if !found {
		writeJSONError(w, "workspace not found", http.StatusNotFound)
		return "", state.Workspace{}, false
	}

	// In network access mode, preview URLs only work for local clients.
	if h.config.GetNetworkAccess() && !h.isTrustedRequest(r) {
		writeJSONError(w, "preview is only available to local clients in network-access mode", http.StatusForbidden)
		return "", state.Workspace{}, false
	}

	return workspaceID, ws, true
}

// handlePreviewsList handles GET /api/workspaces/{workspaceID}/previews
func (h *WorkspaceHandlers) handlePreviewsList(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.previewsWorkspaceCheck(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	previews, err := h.previewManager.List(ctx, workspaceID)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("failed to list previews: %v", err), http.StatusInternalServerError)
		return
	}
	resp := make([]previewResponse, 0, len(previews))
	for _, p := range previews {
		resp = append(resp, toPreviewResponse(p))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handlePreviewsDelete handles DELETE /api/workspaces/{workspaceID}/previews/{previewID}
func (h *WorkspaceHandlers) handlePreviewsDelete(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.previewsWorkspaceCheck(w, r)
	if !ok {
		return
	}

	previewID := chi.URLParam(r, "previewID")
	if previewID == "" {
		writeJSONError(w, "preview ID is required", http.StatusBadRequest)
		return
	}

	if err := h.previewManager.Delete(workspaceID, previewID); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// toPreviewResponse converts a WorkspacePreview to API response.
// The frontend constructs the preview URL using window.location.hostname and proxy_port.
func toPreviewResponse(p state.WorkspacePreview) previewResponse {
	return previewResponse{
		ID:              p.ID,
		WorkspaceID:     p.WorkspaceID,
		TargetHost:      p.TargetHost,
		TargetPort:      p.TargetPort,
		ProxyPort:       p.ProxyPort,
		Status:          p.Status,
		LastError:       p.LastError,
		ServerPID:       p.ServerPID,
		SourceSessionID: p.SourceSessionID,
	}
}

type createPreviewRequest struct {
	TargetPort      int    `json:"target_port"`
	TargetHost      string `json:"target_host"`
	SourceSessionID string `json:"source_session_id"`
}

func (h *WorkspaceHandlers) handlePreviewsCreate(w http.ResponseWriter, r *http.Request) {
	_, ws, ok := h.previewsWorkspaceCheck(w, r)
	if !ok {
		return
	}

	var req createPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.TargetPort <= 0 || req.TargetPort > 65535 {
		writeJSONError(w, "target_port must be between 1 and 65535", http.StatusBadRequest)
		return
	}

	host := req.TargetHost
	if host == "" {
		host = "127.0.0.1"
	}
	host, err := preview.NormalizeTargetHost(host)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.SourceSessionID != "" {
		if _, found := h.state.GetSession(req.SourceSessionID); !found {
			writeJSONError(w, "source session not found", http.StatusBadRequest)
			return
		}
	}

	lookupFn := preview.LookupPortOwner
	if h.lookupPortOwner != nil {
		lookupFn = h.lookupPortOwner
	}
	ownerPID, err := lookupFn(req.TargetPort)
	if err != nil {
		writeJSONError(w, "nothing is listening on that port", http.StatusUnprocessableEntity)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, created, err := h.previewManager.CreateOrGet(ctx, ws, host, req.TargetPort, req.SourceSessionID, ownerPID)
	if err != nil {
		if errors.Is(err, preview.ErrLimitReached) {
			writeJSONError(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if created {
		previewLog := logging.Sub(h.logger, "preview")
		previewLog.Info("created", "host", host, "port", req.TargetPort, "session", req.SourceSessionID, "server_pid", ownerPID, "trigger", "post-api")
		go h.broadcastSessions()
	}

	w.Header().Set("Content-Type", "application/json")
	if created {
		w.WriteHeader(http.StatusCreated)
	} else {
		w.WriteHeader(http.StatusOK)
	}
	json.NewEncoder(w).Encode(toPreviewResponse(result))
}
