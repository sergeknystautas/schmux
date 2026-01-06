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
)

const (
	port        = 7337
	readTimeout  = 15 * time.Second
	writeTimeout = 15 * time.Second
)

// Server represents the dashboard HTTP server.
type Server struct {
	config     *config.Config
	state      *state.State
	session    *session.Manager
	httpServer *http.Server
}

// NewServer creates a new dashboard server.
func NewServer(cfg *config.Config, st *state.State, sm *session.Manager) *Server {
	return &Server{
		config:  cfg,
		state:   st,
		session: sm,
	}
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Static assets
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/spawn", s.handleSpawn)
	mux.HandleFunc("/sessions/", s.handleSessionDetail)
	mux.HandleFunc("/terminal.html", s.handleTerminalHTML)
	mux.HandleFunc("/styles.css", s.handleStatic)
	mux.HandleFunc("/app.js", s.handleStatic)

	// API routes
	mux.HandleFunc("/api/healthz", s.withCORS(s.handleHealthz))
	mux.HandleFunc("/api/sessions", s.withCORS(s.handleSessions))
	mux.HandleFunc("/api/spawn", s.withCORS(s.handleSpawnPost))
	mux.HandleFunc("/api/dispose/", s.withCORS(s.handleDispose))
	mux.HandleFunc("/api/config", s.withCORS(s.handleConfig))

	// WebSocket for terminal streaming
	mux.HandleFunc("/ws/terminal/", s.handleTerminalWebSocket)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("127.0.0.1:%d", port), // Bind to localhost only
		Handler:      mux,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	fmt.Printf("Dashboard server listening on http://localhost:%d\n", port)

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
		w.Header().Set("Access-Control-Allow-Origin", "*")
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


