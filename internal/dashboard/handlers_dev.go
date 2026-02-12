package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// devRestartManifest is written to ~/.schmux/dev-restart.json before triggering
// a dev restart. The wrapper script reads this to know what to build/restart.
type devRestartManifest struct {
	WorkspaceID   string `json:"workspace_id"`
	WorkspacePath string `json:"workspace_path"`
	Type          string `json:"type"` // "frontend", "backend", or "both"
	Timestamp     string `json:"timestamp"`
}

// devStateInfo is read from ~/.schmux/dev-state.json, written by the wrapper
// script on each daemon start.
type devStateInfo struct {
	SourceWorkspace string `json:"source_workspace"`
}

// devBuildStatus is read from ~/.schmux/dev-build-status.json, written by
// the wrapper script after each build attempt.
type devBuildStatus struct {
	Success       bool   `json:"success"`
	WorkspacePath string `json:"workspace_path"`
	Error         string `json:"error"`
	At            string `json:"at"`
}

// handleDevStatus returns the current dev mode state.
func (s *Server) handleDevStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	schmuxDir := filepath.Join(homeDir, ".schmux")

	response := map[string]any{
		"active": true,
	}

	// Read dev state (source workspace)
	if data, err := os.ReadFile(filepath.Join(schmuxDir, "dev-state.json")); err == nil {
		var ds devStateInfo
		if json.Unmarshal(data, &ds) == nil {
			response["source_workspace"] = ds.SourceWorkspace
		}
	}

	// Read last build status
	if data, err := os.ReadFile(filepath.Join(schmuxDir, "dev-build-status.json")); err == nil {
		var bs devBuildStatus
		if json.Unmarshal(data, &bs) == nil {
			response["last_build"] = bs
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleDevRebuild triggers a dev mode rebuild/restart for a workspace.
func (s *Server) handleDevRebuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		WorkspaceID string `json:"workspace_id"`
		Type        string `json:"type"` // "frontend", "backend", or "both"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.WorkspaceID == "" {
		http.Error(w, "workspace_id is required", http.StatusBadRequest)
		return
	}
	if req.Type == "" {
		req.Type = "both"
	}
	if req.Type != "frontend" && req.Type != "backend" && req.Type != "both" {
		http.Error(w, "type must be frontend, backend, or both", http.StatusBadRequest)
		return
	}

	// Validate workspace exists
	ws, ok := s.state.GetWorkspace(req.WorkspaceID)
	if !ok {
		http.Error(w, "Workspace not found", http.StatusNotFound)
		return
	}

	// Write restart manifest
	homeDir, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	manifest := devRestartManifest{
		WorkspaceID:   req.WorkspaceID,
		WorkspacePath: ws.Path,
		Type:          req.Type,
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	manifestPath := filepath.Join(homeDir, ".schmux", "dev-restart.json")
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		http.Error(w, "Failed to write restart manifest", http.StatusInternalServerError)
		return
	}

	fmt.Printf("[dev] rebuild requested: workspace=%s type=%s\n", req.WorkspaceID, req.Type)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "rebuilding"})

	// Trigger dev restart after sending response
	if s.devRestart != nil {
		go s.devRestart()
	}
}
