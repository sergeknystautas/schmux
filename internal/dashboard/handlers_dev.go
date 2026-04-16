package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

// viteClient is used for pause/resume requests to the Vite dev server.
// Keep-alives are disabled because Vite restarts frequently during
// workspace switching, which leaves stale pooled connections.
var viteClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		DisableKeepAlives: true,
	},
}

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

// devViteProxyPort is the port the dev-runner spawns Vite on (`vite --port N
// --strictPort`). Hardcoded here and in tools/dev-runner/src/App.tsx as
// VITE_PORT; the two must change together in the same commit. Drift is only
// possible during a mid-session backend/both workspace switch to a branch
// with a different constant, and is recoverable by restarting ./dev.sh.
const devViteProxyPort = 7338

// devBuildStatus is read from ~/.schmux/dev-build-status.json, written by
// the wrapper script after each build attempt.
type devBuildStatus struct {
	Success       bool   `json:"success"`
	WorkspacePath string `json:"workspace_path"`
	Error         string `json:"error"`
	At            string `json:"at"`
}

// devSourceWorkspacePath returns the path of the workspace currently serving
// dev mode, or "" if dev mode is off or the state file cannot be read.
func (s *Server) devSourceWorkspacePath() string {
	if !s.devMode {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(schmuxdir.Get(), "dev-state.json"))
	if err != nil {
		return ""
	}
	var ds devStateInfo
	if json.Unmarshal(data, &ds) != nil {
		return ""
	}
	return ds.SourceWorkspace
}

// handleDevStatus returns the current dev mode state.
func (s *Server) handleDevStatus(w http.ResponseWriter, r *http.Request) {
	schmuxDir := schmuxdir.Get()

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
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("failed to encode response", "handler", "dev-status", "err", err)
	}
}

// handleDevRebuild triggers a dev mode rebuild/restart for a workspace.
func (s *Server) handleDevRebuild(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkspaceID string `json:"workspace_id"`
		Type        string `json:"type"` // "frontend", "backend", or "both"
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.WorkspaceID == "" {
		writeJSONError(w, "workspace_id is required", http.StatusBadRequest)
		return
	}
	if req.Type == "" {
		req.Type = "both"
	}
	if req.Type != "frontend" && req.Type != "backend" && req.Type != "both" {
		writeJSONError(w, "type must be frontend, backend, or both", http.StatusBadRequest)
		return
	}

	// Validate workspace exists
	ws, ok := s.state.GetWorkspace(req.WorkspaceID)
	if !ok {
		writeJSONError(w, "Workspace not found", http.StatusNotFound)
		return
	}

	// Write restart manifest
	manifest := devRestartManifest{
		WorkspaceID:   req.WorkspaceID,
		WorkspacePath: ws.Path,
		Type:          req.Type,
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		writeJSONError(w, "Internal error", http.StatusInternalServerError)
		return
	}

	manifestPath := filepath.Join(schmuxdir.Get(), "dev-restart.json")
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		writeJSONError(w, "Failed to write restart manifest", http.StatusInternalServerError)
		return
	}

	s.logger.Info("rebuild requested", "workspace", req.WorkspaceID, "type", req.Type)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "rebuilding"}); err != nil {
		s.logger.Error("failed to encode response", "handler", "dev-rebuild", "err", err)
	}

	// Trigger dev restart after sending response
	if s.devRestart != nil {
		go s.devRestart()
	}
}

// pauseViteWatch tells the Vite dev server to suppress HMR updates.
// This prevents transform errors from transient conflict markers during
// git rebase/merge operations. Safe to call when not in dev mode (no-op).
func (s *Server) pauseViteWatch() {
	if !s.devMode {
		return
	}
	resp, err := viteClient.Post(fmt.Sprintf("http://localhost:%d/__dev/pause-watch", devViteProxyPort), "", strings.NewReader(""))
	if err != nil {
		s.logger.Warn("failed to pause Vite watch", "err", err)
		return
	}
	resp.Body.Close()
}

// resumeViteWatch tells the Vite dev server to resume HMR updates.
// If any files changed while paused, Vite will trigger a full page reload.
func (s *Server) resumeViteWatch() {
	if !s.devMode {
		return
	}
	resp, err := viteClient.Post(fmt.Sprintf("http://localhost:%d/__dev/resume-watch", devViteProxyPort), "", strings.NewReader(""))
	if err != nil {
		s.logger.Warn("failed to resume Vite watch", "err", err)
		return
	}
	resp.Body.Close()
}

func (s *Server) handleDevSimulateTunnel(w http.ResponseWriter, r *http.Request) {
	tunnelURL := fmt.Sprintf("https://fake-tunnel-%d.trycloudflare.com", time.Now().UnixNano())
	s.HandleTunnelConnected(tunnelURL)

	s.remoteTokenMu.Lock()
	token := s.remoteToken
	s.remoteTokenMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"url":   tunnelURL,
		"token": token,
	}); err != nil {
		s.logger.Error("failed to encode response", "handler", "dev-simulate-tunnel", "err", err)
	}
}

func (s *Server) handleDevSimulateTunnelStop(w http.ResponseWriter, r *http.Request) {
	s.ClearRemoteAuth()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"ok": "true"}); err != nil {
		s.logger.Error("failed to encode response", "handler", "dev-simulate-tunnel-stop", "err", err)
	}
}

// handleDevLogLevel handles GET (read) and POST (change) for the daemon log level.
func (s *Server) handleDevLogLevel(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"level": strings.ToLower(logging.GetLevel().String()),
		})
		return
	}

	var req struct {
		Level string `json:"level"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	parsed, err := log.ParseLevel(strings.ToLower(req.Level))
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Invalid log level %q: valid levels are debug, info, warn, error", req.Level), http.StatusBadRequest)
		return
	}

	prev := logging.GetLevel()
	logging.SetLevel(parsed)
	s.logger.Info("log level changed", "from", strings.ToLower(prev.String()), "to", strings.ToLower(parsed.String()))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"level": strings.ToLower(parsed.String()),
	})
}

func (s *Server) handleDevClearPassword(w http.ResponseWriter, r *http.Request) {
	s.config.SetRemoteAccessPasswordHash("")
	if err := s.config.Save(); err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to save config: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"ok": "true"}); err != nil {
		s.logger.Error("failed to encode response", "handler", "dev-clear-password", "err", err)
	}
}
