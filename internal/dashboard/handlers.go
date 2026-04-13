package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/update"
	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

// pkgLogger is the package-level logger for dashboard handler helpers.
// Set during NewServer initialization.
var pkgLogger *log.Logger

// requireWorkspace extracts the workspaceID URL param, validates it, and
// looks up the workspace. Returns false if an error response was already
// written (caller should return).
func (s *Server) requireWorkspace(w http.ResponseWriter, r *http.Request) (state.Workspace, bool) {
	workspaceID := chi.URLParam(r, "workspaceID")
	if workspaceID == "" {
		writeJSONError(w, "workspace ID is required", http.StatusBadRequest)
		return state.Workspace{}, false
	}
	ws, ok := s.state.GetWorkspace(workspaceID)
	if !ok {
		writeJSONError(w, "workspace not found", http.StatusNotFound)
		return state.Workspace{}, false
	}
	return ws, true
}

// vcsTypeForWorkspace determines the VCS type for a workspace.
// Defaults to "git" unless the workspace's remote host flavor specifies otherwise.
func (s *Server) vcsTypeForWorkspace(ws state.Workspace) string {
	if ws.VCS != "" {
		return ws.VCS
	}
	if ws.RemoteHostID != "" {
		if host, found := s.state.GetRemoteHost(ws.RemoteHostID); found {
			if host.ProfileID != "" {
				if profile, found := s.config.GetRemoteProfile(host.ProfileID); found {
					if resolved, err := config.ResolveProfileFlavor(profile, host.Flavor); err == nil && resolved.VCS != "" {
						return resolved.VCS
					} else if profile.VCS != "" {
						return profile.VCS
					}
				}
			}
		}
	}
	return "git"
}

// localShellRun returns a function that executes shell command strings locally
// in the given directory via sh -c. This correctly handles quoted arguments
// and shell operators produced by vcs.CommandBuilder.
func localShellRun(ctx context.Context, dir string) func(string) (string, error) {
	return func(cmd string) (string, error) {
		c := exec.CommandContext(ctx, "sh", "-c", cmd)
		c.Dir = dir
		out, err := c.Output()
		return strings.TrimSpace(string(out)), err
	}
}

// writeJSONError writes a JSON error response with the given status code.
func writeJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		if pkgLogger != nil {
			pkgLogger.Error("writeJSONError: failed to encode response", "err", err)
		}
	}
}

// writeJSON encodes v as JSON to w, logging any encode error.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		if pkgLogger != nil {
			pkgLogger.Error("writeJSON: failed to encode response", "err", err)
		}
	}
}

// handleApp serves the React application entry point for UI routes.
func (s *Server) handleApp(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
		http.NotFound(w, r)
		return
	}
	if !s.requireAuthOrRedirect(w, r) {
		return
	}

	// Serve static files at root (e.g., favicon.ico) if they exist in dist.
	if path.Ext(r.URL.Path) != "" {
		if s.serveFileIfExists(w, r, r.URL.Path) {
			return
		}
	}

	s.serveAppIndex(w, r)
}

func (s *Server) serveFileIfExists(w http.ResponseWriter, r *http.Request, requestPath string) bool {
	cleanPath := filepath.Clean(strings.TrimPrefix(requestPath, "/"))
	if strings.HasPrefix(cleanPath, "..") {
		return false
	}
	// Try embedded FS first
	if s.dashboardFS != nil {
		if f, err := s.dashboardFS.Open(cleanPath); err == nil {
			defer f.Close()
			stat, err := f.Stat()
			if err == nil && !stat.IsDir() {
				http.ServeFileFS(w, r, s.dashboardFS, cleanPath)
				return true
			}
		}
	}
	// Fall back to disk
	distPath := s.getDashboardDistPath()
	filePath := filepath.Join(distPath, cleanPath)
	if _, err := os.Stat(filePath); err == nil {
		http.ServeFile(w, r, filePath)
		return true
	}
	return false
}

// serveAppIndex serves the built React index.html from the dist directory.
func (s *Server) serveAppIndex(w http.ResponseWriter, r *http.Request) {
	// Try embedded FS first
	if s.dashboardFS != nil {
		if content, err := fs.ReadFile(s.dashboardFS, "index.html"); err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(content)
			return
		}
	}
	// Fall back to disk
	distPath := s.getDashboardDistPath()
	filePath := filepath.Join(distPath, "index.html")

	content, err := os.ReadFile(filePath)
	if err != nil {
		writeJSONError(w, "Dashboard assets not built. Run `go run ./cmd/build-dashboard`.", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

// maxBodySize is the maximum request body size for JSON requests (1MB).
const maxBodySize = 1 << 20

// handleHealthz returns a simple health check response with version info.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	v := s.GetVersionInfo()
	response := map[string]any{
		"status":  "ok",
		"version": v.Current,
	}
	if v.Latest != "" {
		response["latest_version"] = v.Latest
		response["update_available"] = v.UpdateAvailable
	}
	if s.devMode {
		response["dev_mode"] = true
	}
	if s.config.GetDebugUI() {
		response["debug_mode"] = true
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("failed to encode response", "handler", "healthz", "err", err)
	}
}

// handleUpdate triggers an update and shuts down the daemon.
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	// Prevent concurrent updates
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.updateInProgress {
		writeJSONError(w, "update already in progress", http.StatusConflict)
		return
	}
	s.updateInProgress = true

	daemonLog := logging.Sub(s.logger, "daemon")
	daemonLog.Info("update requested via web UI")

	// Run update synchronously so we can report actual success/failure
	if err := update.Update(); err != nil {
		s.updateInProgress = false
		writeJSONError(w, fmt.Sprintf("update failed: %v", err), http.StatusInternalServerError)
		return
	}

	daemonLog.Info("update successful, shutting down")
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Update successful. Restart schmux to use the new version.",
	}); err != nil {
		s.logger.Error("failed to encode response", "handler", "update", "err", err)
	}

	// Shutdown after sending response
	if s.shutdown != nil {
		go s.shutdown()
	}
}

// UpdateNicknameRequest represents a request to update a session's nickname.
type UpdateNicknameRequest struct {
	Nickname string `json:"nickname"`
}

// shellSplit splits a command line string into arguments, respecting quotes.
// Delegates to shellutil.Split for the actual implementation.
func shellSplit(input string) ([]string, error) {
	return shellutil.Split(input)
}
