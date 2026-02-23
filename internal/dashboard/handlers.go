package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/nudgenik"
	"github.com/sergeknystautas/schmux/internal/update"
)

// extractPathSegment extracts a path segment between a prefix and suffix from a URL path.
// Example: extractPathSegment("/api/workspaces/ws-123/dispose", "/api/workspaces/", "/dispose") returns "ws-123"
func extractPathSegment(path, prefix, suffix string) string {
	s := strings.TrimPrefix(path, prefix)
	if suffix != "" {
		s = strings.TrimSuffix(s, suffix)
	}
	return s
}

// writeJSONError writes a JSON error response with the given status code.
func writeJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
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
	distPath := s.getDashboardDistPath()
	cleanPath := filepath.Clean(strings.TrimPrefix(requestPath, "/"))
	if strings.HasPrefix(cleanPath, "..") {
		return false
	}
	filePath := filepath.Join(distPath, cleanPath)
	if _, err := os.Stat(filePath); err == nil {
		http.ServeFile(w, r, filePath)
		return true
	}
	return false
}

// serveAppIndex serves the built React index.html from the dist directory.
func (s *Server) serveAppIndex(w http.ResponseWriter, r *http.Request) {
	distPath := s.getDashboardDistPath()
	filePath := filepath.Join(distPath, "index.html")

	content, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "Dashboard assets not built. Run `npm install` and `npm run build` in assets/dashboard.", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

// maxBodySize is the maximum request body size for JSON requests (1MB).
const maxBodySize = 1 << 20

// handleWorkspacesScan scans the workspace directory and reconciles with state.
func (s *Server) handleWorkspacesScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, err := s.workspace.Scan()
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to scan workspaces: %v", err), http.StatusInternalServerError)
		return
	}
	if s.previewManager != nil {
		previewLog := logging.Sub(s.logger, "preview")
		for _, removed := range result.Removed {
			if err := s.previewManager.DeleteWorkspace(removed.ID); err != nil {
				previewLog.Warn("scan cleanup failed", "workspace_id", removed.ID, "err", err)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleHealthz returns a simple health check response with version info.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleUpdate triggers an update and shuts down the daemon.
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Prevent concurrent updates
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.updateInProgress {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "update already in progress"})
		return
	}
	s.updateInProgress = true

	daemonLog := logging.Sub(s.logger, "daemon")
	daemonLog.Info("update requested via web UI")

	// Run update synchronously so we can report actual success/failure
	if err := update.Update(); err != nil {
		s.updateInProgress = false
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("update failed: %v", err)})
		return
	}

	daemonLog.Info("update successful, shutting down")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Update successful. Restart schmux to use the new version.",
	})

	// Shutdown after sending response
	if s.shutdown != nil {
		go s.shutdown()
	}
}

// UpdateNicknameRequest represents a request to update a session's nickname.
type UpdateNicknameRequest struct {
	Nickname string `json:"nickname"`
}

// handleUpdateNickname handles session nickname update requests.
func (s *Server) handleUpdateNickname(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPatch {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	// Extract session ID from URL: /api/sessions-nickname/{session-id}
	sessionID := strings.TrimPrefix(r.URL.Path, "/api/sessions-nickname/")
	if sessionID == "" {
		writeJSONError(w, "session ID is required", http.StatusBadRequest)
		return
	}

	var req UpdateNicknameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, fmt.Sprintf("Invalid request: %v", err), http.StatusBadRequest)
		return
	}

	// Update nickname (and rename tmux session)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
	err := s.session.RenameSession(ctx, sessionID, req.Nickname)
	cancel()
	if err != nil {
		// Check if this is a nickname conflict error
		if strings.Contains(err.Error(), "already in use") {
			writeJSONError(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSONError(w, fmt.Sprintf("Failed to rename session: %v", err), http.StatusInternalServerError)
		return
	}

	// Broadcast update to WebSocket clients
	go s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleAskNudgenik handles GET requests to ask NudgeNik about a session's output.
// GET /api/askNudgenik/{sessionId}
//
// Combines extraction of the latest session response with the Claude CLI call.
// The response extraction happens internally on the server side.
func (s *Server) handleAskNudgenik(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract session ID from URL: /api/askNudgenik/{session-id}
	sessionID := strings.TrimPrefix(r.URL.Path, "/api/askNudgenik/")
	if sessionID == "" {
		http.Error(w, "session ID is required", http.StatusBadRequest)
		return
	}

	// Get session from state
	sess, found := s.state.GetSession(sessionID)
	if !found {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	ctx := context.Background()
	result, err := nudgenik.AskForSession(ctx, s.config, sess)
	if err != nil {
		nudgenikLog := logging.Sub(s.logger, "nudgenik")
		switch {
		case errors.Is(err, nudgenik.ErrDisabled):
			nudgenikLog.Info("nudgenik is disabled")
			http.Error(w, "Nudgenik is disabled. Configure a target in settings.", http.StatusServiceUnavailable)
		case errors.Is(err, nudgenik.ErrNoResponse):
			nudgenikLog.Info("no response extracted", "session_id", sessionID)
			http.Error(w, "No response found in session output", http.StatusBadRequest)
		case errors.Is(err, nudgenik.ErrTargetNotFound):
			nudgenikLog.Warn("target not found in config")
			http.Error(w, "Nudgenik target not found", http.StatusServiceUnavailable)
		case errors.Is(err, nudgenik.ErrTargetNoSecrets):
			nudgenikLog.Warn("target missing required secrets")
			http.Error(w, "Nudgenik target missing required secrets", http.StatusServiceUnavailable)
		default:
			nudgenikLog.Error("failed to ask", "session_id", sessionID, "err", err)
			http.Error(w, fmt.Sprintf("Failed to ask nudgenik: %v", err), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleHasNudgenik handles GET requests to check if nudgenik is available globally.
// Returns available: true only when a nudgenik target is configured.
func (s *Server) handleHasNudgenik(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	available := nudgenik.IsEnabled(s.config)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"available": available})
}

// shellSplit splits a command line string into arguments, respecting quotes.
// Handles single quotes, double quotes, and backslash escaping.
// This prevents breakage when workspace paths contain spaces.
func shellSplit(input string) ([]string, error) {
	var args []string
	var current strings.Builder
	var inSingleQuote, inDoubleQuote bool
	var escaped bool

	for i := 0; i < len(input); i++ {
		c := input[i]

		if escaped {
			// Previous char was backslash, add this char literally
			current.WriteByte(c)
			escaped = false
			continue
		}

		switch c {
		case '\\':
			if inSingleQuote {
				// Backslash is literal in single quotes
				current.WriteByte(c)
			} else {
				// Set escaped flag for next character
				escaped = true
			}

		case '\'':
			if inDoubleQuote {
				// Single quote is literal inside double quotes
				current.WriteByte(c)
			} else {
				// Toggle single quote mode
				inSingleQuote = !inSingleQuote
			}

		case '"':
			if inSingleQuote {
				// Double quote is literal inside single quotes
				current.WriteByte(c)
			} else {
				// Toggle double quote mode
				inDoubleQuote = !inDoubleQuote
			}

		case ' ', '\t', '\n', '\r':
			if inSingleQuote || inDoubleQuote {
				// Whitespace is literal inside quotes
				current.WriteByte(c)
			} else {
				// Whitespace outside quotes separates arguments
				if current.Len() > 0 {
					args = append(args, current.String())
					current.Reset()
				}
			}

		default:
			current.WriteByte(c)
		}
	}

	// Handle unterminated quotes
	if inSingleQuote || inDoubleQuote {
		return nil, fmt.Errorf("unterminated quote in command")
	}

	// Add final argument if any
	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args, nil
}
