package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	URL         string `json:"url"`
	Status      string `json:"status"`
	LastError   string `json:"last_error,omitempty"`
}

func (s *Server) handleWorkspaceRoutes(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if strings.HasSuffix(path, "/previews") || strings.Contains(path, "/previews/") {
		s.handleWorkspacePreviews(w, r)
		return
	}
	s.handleLinearSync(w, r)
}

func (s *Server) handleWorkspacePreviews(w http.ResponseWriter, r *http.Request) {
	if s.previewManager == nil {
		http.Error(w, "preview manager not available", http.StatusServiceUnavailable)
		return
	}

	workspaceID, previewID, err := parseWorkspacePreviewPath(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ws, found := s.state.GetWorkspace(workspaceID)
	if !found {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}

	// In network access mode, preview URLs only work for local clients. Block non-local callers.
	if s.config.GetNetworkAccess() && !s.isLocalRequest(r) {
		http.Error(w, "preview is only available to local clients in network-access mode", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		if previewID != "" {
			http.NotFound(w, r)
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
		json.NewEncoder(w).Encode(resp)
		return
	case http.MethodPost:
		if previewID != "" {
			http.NotFound(w, r)
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
		json.NewEncoder(w).Encode(toPreviewResponse(previewItem))
		return
	case http.MethodDelete:
		if previewID == "" {
			http.Error(w, "preview ID is required", http.StatusBadRequest)
			return
		}
		if err := s.previewManager.Delete(workspaceID, previewID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func parseWorkspacePreviewPath(rawPath string) (workspaceID string, previewID string, err error) {
	trimmed := strings.TrimPrefix(rawPath, "/api/workspaces/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] != "previews" {
		return "", "", fmt.Errorf("invalid preview path")
	}
	workspaceID = parts[0]
	if len(parts) > 2 {
		previewID = parts[2]
	}
	return workspaceID, previewID, nil
}

// toPreviewResponse converts a WorkspacePreview to API response.
// Note: Preview proxy always serves HTTP on localhost. Most browsers allow HTTP
// to localhost even from HTTPS pages (localhost is a secure context). If this
// becomes an issue, preview requests would need to be routed through the dashboard.
func toPreviewResponse(p state.WorkspacePreview) previewResponse {
	return previewResponse{
		ID:          p.ID,
		WorkspaceID: p.WorkspaceID,
		TargetHost:  p.TargetHost,
		TargetPort:  p.TargetPort,
		ProxyPort:   p.ProxyPort,
		URL:         "http://localhost:" + strconv.Itoa(p.ProxyPort),
		Status:      p.Status,
		LastError:   p.LastError,
	}
}
