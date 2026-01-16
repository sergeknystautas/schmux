package dashboard

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/sergek/schmux/internal/config"
	"github.com/sergek/schmux/internal/session"
	"github.com/sergek/schmux/internal/state"
	"github.com/sergek/schmux/internal/workspace"
)

const (
	port         = 7337
	readTimeout  = 15 * time.Second
	writeTimeout = 15 * time.Second
)

// Server represents the dashboard HTTP server.
type Server struct {
	config     *config.Config
	state      state.StateStore
	statePath  string
	session    *session.Manager
	workspace  workspace.WorkspaceManager
	httpServer *http.Server
}

// NewServer creates a new dashboard server.
func NewServer(cfg *config.Config, st state.StateStore, statePath string, sm *session.Manager, wm workspace.WorkspaceManager) *Server {
	return &Server{
		config:    cfg,
		state:     st,
		statePath: statePath,
		session:   sm,
		workspace: wm,
	}
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Static assets - all UI routes go through handleApp
	mux.HandleFunc("/", s.handleApp)
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(filepath.Join(s.getDashboardDistPath(), "assets")))))

	// API routes
	mux.HandleFunc("/api/healthz", s.withCORS(s.handleHealthz))
	mux.HandleFunc("/api/hasNudgenik", s.withCORS(s.handleHasNudgenik))
	mux.HandleFunc("/api/askNudgenik/", s.withCORS(s.handleAskNudgenik))
	mux.HandleFunc("/api/workspaces/scan", s.withCORS(s.handleWorkspacesScan))
	mux.HandleFunc("/api/workspaces/", s.withCORS(s.handleRefreshOverlay))
	mux.HandleFunc("/api/sessions", s.withCORS(s.handleSessions))
	mux.HandleFunc("/api/sessions-nickname/", s.withCORS(s.handleUpdateNickname))
	mux.HandleFunc("/api/spawn", s.withCORS(s.handleSpawnPost))
	mux.HandleFunc("/api/dispose/", s.withCORS(s.handleDispose))
	mux.HandleFunc("/api/dispose-workspace/", s.withCORS(s.handleDisposeWorkspace))
	mux.HandleFunc("/api/config", s.withCORS(s.handleConfig))
	mux.HandleFunc("/api/detect-tools", s.withCORS(s.handleDetectTools))
	mux.HandleFunc("/api/variants", s.withCORS(s.handleVariants))
	mux.HandleFunc("/api/variants/", s.withCORS(s.handleVariant))
	mux.HandleFunc("/api/builtin-quick-launch", s.withCORS(s.handleBuiltinQuickLaunch))
	mux.HandleFunc("/api/diff/", s.withCORS(s.handleDiff))
	mux.HandleFunc("/api/open-vscode/", s.withCORS(s.handleOpenVSCode))
	mux.HandleFunc("/api/overlays", s.withCORS(s.handleOverlays))

	// WebSocket for terminal streaming
	mux.HandleFunc("/ws/terminal/", s.handleTerminalWebSocket)

	// Bind address based on network_access config
	bindAddr := "127.0.0.1"
	if s.config.GetNetworkAccess() {
		bindAddr = "0.0.0.0"
	}

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", bindAddr, port),
		Handler:      mux,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	if s.config.GetNetworkAccess() {
		fmt.Printf("Dashboard server listening on http://0.0.0.0:%d (accessible from local network)\n", port)
	} else {
		fmt.Printf("Dashboard server listening on http://localhost:%d (localhost only)\n", port)
	}

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// Stop stops the HTTP server.
func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}

	return nil
}

// withCORS wraps a handler with CORS headers.
func (s *Server) withCORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// When network access is enabled, allow requests from LAN IPs
		// Otherwise only allow localhost
		allowedOrigin := ""
		if origin == "http://localhost:7337" || origin == "http://127.0.0.1:7337" {
			allowedOrigin = origin
		} else if s.config.GetNetworkAccess() && origin != "" {
			// Allow any origin when network access is enabled
			// (could be more restrictive by checking for private IP ranges)
			allowedOrigin = origin
		}

		if origin != "" && allowedOrigin == "" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		if allowedOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		h(w, r)
	}
}

// getAssetPath returns the path to the assets directory.
// Tries multiple locations to support different deployment scenarios.
func (s *Server) getAssetPath() string {
	// List of candidate paths in order of preference
	candidates := []string{
		// Relative to current working directory (for development)
		"./assets/dashboard",
		// Relative to executable (for installed binary)
		filepath.Join(filepath.Dir(os.Args[0]), "../assets/dashboard"),
		// Absolute path from module root (if working dir is set correctly)
		filepath.Join(".", "assets", "dashboard"),
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// Fallback - return first candidate even if it doesn't exist
	// (will result in 404s but won't crash)
	return candidates[0]
}

// getDashboardDistPath returns the path to the built dashboard assets.
func (s *Server) getDashboardDistPath() string {
	assetPath := s.getAssetPath()
	return filepath.Join(assetPath, "dist")
}
