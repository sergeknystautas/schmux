package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/preview"
	"github.com/sergeknystautas/schmux/internal/state"
)

type previewCreateRequest struct {
	TargetHost string `json:"target_host"`
	TargetPort int    `json:"target_port"`
}

type previewResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	TargetHost  string `json:"target_host"`
	TargetPort  int    `json:"target_port"`
	ProxyPort   int    `json:"proxy_port"`
	Status      string `json:"status"`
	LastError   string `json:"last_error,omitempty"`
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
func (s *Server) previewsWorkspaceCheck(w http.ResponseWriter, r *http.Request) (string, state.Workspace, bool) {
	workspaceID := chi.URLParam(r, "workspaceID")
	if !isValidResourceID(workspaceID) {
		http.Error(w, "invalid workspace ID", http.StatusBadRequest)
		return "", state.Workspace{}, false
	}

	if s.previewManager == nil {
		http.Error(w, "preview manager not available", http.StatusServiceUnavailable)
		return "", state.Workspace{}, false
	}

	ws, found := s.state.GetWorkspace(workspaceID)
	if !found {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return "", state.Workspace{}, false
	}

	// In network access mode, preview URLs only work for local clients.
	if s.config.GetNetworkAccess() && !s.isTrustedRequest(r) {
		http.Error(w, "preview is only available to local clients in network-access mode", http.StatusForbidden)
		return "", state.Workspace{}, false
	}

	return workspaceID, ws, true
}

// handlePreviewsList handles GET /api/workspaces/{workspaceID}/previews
func (s *Server) handlePreviewsList(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := s.previewsWorkspaceCheck(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	previews, err := s.previewManager.List(ctx, workspaceID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list previews: %v", err), http.StatusInternalServerError)
		return
	}
	resp := make([]previewResponse, 0, len(previews))
	for _, p := range previews {
		resp = append(resp, toPreviewResponse(p))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handlePreviewsCreate handles POST /api/workspaces/{workspaceID}/previews
func (s *Server) handlePreviewsCreate(w http.ResponseWriter, r *http.Request) {
	_, ws, ok := s.previewsWorkspaceCheck(w, r)
	if !ok {
		return
	}

	var req previewCreateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodySize)).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	previewItem, err := s.previewManager.CreateOrGet(ctx, ws, req.TargetHost, req.TargetPort)
	if err != nil {
		statusCode := http.StatusInternalServerError
		switch {
		case strings.Contains(err.Error(), "limit"):
			statusCode = http.StatusConflict
		case err == preview.ErrRemoteUnsupported:
			statusCode = http.StatusUnprocessableEntity
		case err == preview.ErrTargetHostNotAllowed || strings.Contains(err.Error(), "target port"):
			statusCode = http.StatusBadRequest
		}
		http.Error(w, err.Error(), statusCode)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toPreviewResponse(previewItem))
}

// handlePreviewsDelete handles DELETE /api/workspaces/{workspaceID}/previews/{previewID}
func (s *Server) handlePreviewsDelete(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := s.previewsWorkspaceCheck(w, r)
	if !ok {
		return
	}

	previewID := chi.URLParam(r, "previewID")
	if previewID == "" {
		http.Error(w, "preview ID is required", http.StatusBadRequest)
		return
	}

	if err := s.previewManager.Delete(workspaceID, previewID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// toPreviewResponse converts a WorkspacePreview to API response.
// The frontend constructs the preview URL using window.location.hostname and proxy_port.
func toPreviewResponse(p state.WorkspacePreview) previewResponse {
	return previewResponse{
		ID:          p.ID,
		WorkspaceID: p.WorkspaceID,
		TargetHost:  p.TargetHost,
		TargetPort:  p.TargetPort,
		ProxyPort:   p.ProxyPort,
		Status:      p.Status,
		LastError:   p.LastError,
	}
}
