package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/assets"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/difftool"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/update"
	"github.com/sergeknystautas/schmux/internal/version"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

const (
	readTimeout  = 15 * time.Second
	writeTimeout = 15 * time.Second
)

// wsConn wraps a websocket.Conn with a mutex for concurrent write safety.
// The gorilla/websocket package is not concurrent-safe for writes,
// so we need to serialize writes per connection.
type wsConn struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	closed bool
}

// WriteMessage writes a message to the websocket connection in a thread-safe manner.
func (w *wsConn) WriteMessage(messageType int, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return fmt.Errorf("websocket connection closed")
	}
	return w.conn.WriteMessage(messageType, data)
}

// ReadMessage reads a message from the websocket connection.
func (w *wsConn) ReadMessage() (messageType int, p []byte, err error) {
	return w.conn.ReadMessage()
}

// Close closes the underlying websocket connection in a thread-safe manner.
// Once closed, WriteMessage calls will fail without attempting to write.
func (w *wsConn) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil // Already closed
	}
	w.closed = true
	return w.conn.Close()
}

// IsClosed returns whether the connection has been closed.
func (w *wsConn) IsClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

// Server represents the dashboard HTTP server.
type Server struct {
	config     *config.Config
	state      state.StateStore
	statePath  string
	session    *session.Manager
	workspace  workspace.WorkspaceManager
	httpServer *http.Server
	shutdown   func() // Callback to trigger daemon shutdown

	// WebSocket connection registry: sessionID -> list of active connections (for terminal)
	wsConns   map[string][]*wsConn
	wsConnsMu sync.RWMutex

	// Sessions WebSocket connections (for /ws/sessions real-time updates)
	sessionsConns   map[*wsConn]bool
	sessionsConnsMu sync.RWMutex
	lastBroadcast   time.Time
	lastBroadcastMu sync.Mutex

	// Per-session rotation locks to prevent concurrent rotations
	rotationLocks   map[string]*sync.Mutex
	rotationLocksMu sync.RWMutex

	// Version info: current version and latest available version
	versionInfo      versionInfo
	versionInfoMu    sync.RWMutex
	updateInProgress bool
	updateMu         sync.Mutex

	authSessionKey []byte
}

// versionInfo holds version information.
type versionInfo struct {
	Current         string
	Latest          string
	UpdateAvailable bool
	CheckError      error
}

// NewServer creates a new dashboard server.
func NewServer(cfg *config.Config, st state.StateStore, statePath string, sm *session.Manager, wm workspace.WorkspaceManager, shutdown func()) *Server {
	return &Server{
		config:        cfg,
		state:         st,
		statePath:     statePath,
		session:       sm,
		workspace:     wm,
		shutdown:      shutdown,
		wsConns:       make(map[string][]*wsConn),
		sessionsConns: make(map[*wsConn]bool),
		rotationLocks: make(map[string]*sync.Mutex),
	}
}

// LogDashboardAssetPath logs where dashboard assets are being served from.
func (s *Server) LogDashboardAssetPath() {
	path := s.getDashboardDistPath()
	// Determine source type for clearer message
	if strings.HasPrefix(path, filepath.Join(os.Getenv("HOME"), ".schmux")) {
		fmt.Printf("[daemon] serving from cached assets: %s\n", path)
	} else if strings.HasPrefix(path, ".") {
		abs, _ := filepath.Abs(path)
		fmt.Printf("[daemon] serving from local build: %s\n", abs)
	} else {
		fmt.Printf("[daemon] serving from: %s\n", path)
	}
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	cleanupDelay := time.Duration(s.config.GetExternalDiffCleanupAfterMs()) * time.Millisecond
	deleted, scheduled := difftool.SweepAndScheduleTempDirs(cleanupDelay, func(format string, args ...interface{}) {
		fmt.Printf(format, args...)
	})
	fmt.Printf("[session] difftool temp dirs cleanup: deleted=%d scheduled=%d\n", deleted, scheduled)

	if s.config.GetAuthEnabled() {
		secret, err := config.EnsureSessionSecret()
		if err != nil {
			return fmt.Errorf("failed to initialize auth session secret: %w", err)
		}
		key, err := decodeSessionSecret(secret)
		if err != nil {
			return fmt.Errorf("failed to parse auth session secret: %w", err)
		}
		s.authSessionKey = key
	}

	mux := http.NewServeMux()

	// Static assets - all UI routes go through handleApp
	mux.HandleFunc("/", s.handleApp)
	mux.Handle("/assets/", s.withAuthHandler(http.StripPrefix("/assets/", http.FileServer(http.Dir(filepath.Join(s.getDashboardDistPath(), "assets"))))))

	// Auth routes
	mux.HandleFunc("/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/auth/callback", s.handleAuthCallback)
	mux.HandleFunc("/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("/auth/me", s.withCORS(s.withAuth(s.handleAuthMe)))

	// API routes
	mux.HandleFunc("/api/healthz", s.withCORS(s.withAuth(s.handleHealthz)))
	mux.HandleFunc("/api/update", s.withCORS(s.withAuth(s.handleUpdate)))
	mux.HandleFunc("/api/auth/secrets", s.withCORS(s.withAuth(s.handleAuthSecrets)))
	mux.HandleFunc("/api/hasNudgenik", s.withCORS(s.withAuth(s.handleHasNudgenik)))
	mux.HandleFunc("/api/askNudgenik/", s.withCORS(s.withAuth(s.handleAskNudgenik)))
	mux.HandleFunc("/api/workspaces/scan", s.withCORS(s.withAuth(s.handleWorkspacesScan)))
	mux.HandleFunc("/api/workspaces/", s.withCORS(s.withAuth(s.handleLinearSync)))
	mux.HandleFunc("/api/sessions", s.withCORS(s.withAuth(s.handleSessions)))
	mux.HandleFunc("/api/sessions-nickname/", s.withCORS(s.withAuth(s.handleUpdateNickname)))
	mux.HandleFunc("/api/spawn", s.withCORS(s.withAuth(s.handleSpawnPost)))
	mux.HandleFunc("/api/check-branch-conflict", s.withCORS(s.withAuth(s.handleCheckBranchConflict)))
	mux.HandleFunc("/api/recent-branches", s.withCORS(s.withAuth(s.handleRecentBranches)))
	mux.HandleFunc("/api/suggest-branch", s.withCORS(s.withAuth(s.handleSuggestBranch)))
	mux.HandleFunc("/api/prepare-branch-spawn", s.withCORS(s.withAuth(s.handlePrepareBranchSpawn)))
	mux.HandleFunc("/api/sessions/", s.withCORS(s.withAuth(s.handleDispose)))
	mux.HandleFunc("/api/config", s.withCORS(s.withAuth(s.handleConfig)))
	mux.HandleFunc("/api/detect-tools", s.withCORS(s.withAuth(s.handleDetectTools)))
	mux.HandleFunc("/api/variants", s.withCORS(s.withAuth(s.handleVariants)))
	mux.HandleFunc("/api/variants/", s.withCORS(s.withAuth(s.handleVariant)))
	mux.HandleFunc("/api/builtin-quick-launch", s.withCORS(s.withAuth(s.handleBuiltinQuickLaunch)))
	mux.HandleFunc("/api/diff/", s.withCORS(s.withAuth(s.handleDiff)))
	mux.HandleFunc("/api/diff-external/", s.withCORS(s.withAuth(s.handleDiffExternal)))
	mux.HandleFunc("/api/open-vscode/", s.withCORS(s.withAuth(s.handleOpenVSCode)))
	mux.HandleFunc("/api/overlays", s.withCORS(s.withAuth(s.handleOverlays)))

	// WebSocket for terminal streaming
	mux.HandleFunc("/ws/terminal/", s.handleTerminalWebSocket)

	// WebSocket for real-time dashboard state updates
	mux.HandleFunc("/ws/dashboard", s.handleDashboardWebSocket)

	// Bind address from config
	bindAddr := s.config.GetBindAddress()

	port := s.config.GetPort()
	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", bindAddr, port),
		Handler:      mux,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	scheme := "http"
	if s.config.GetAuthEnabled() {
		scheme = "https"
	}
	if s.config.GetNetworkAccess() {
		fmt.Printf("[daemon] listening on %s://0.0.0.0:%d (accessible from local network)\n", scheme, port)
	} else {
		fmt.Printf("[daemon] listening on %s://localhost:%d (localhost only)\n", scheme, port)
	}

	if s.config.GetAuthEnabled() {
		certPath := s.config.GetTLSCertPath()
		keyPath := s.config.GetTLSKeyPath()
		if err := s.httpServer.ListenAndServeTLS(certPath, keyPath); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
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

// withCORS wraps a handler with CORS headers and origin validation.
// Returns 403 Forbidden if the request origin is not allowed.
// Sets Access-Control-Allow-Credentials when auth is enabled.
func (s *Server) withCORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Validate origin
		if origin != "" && !s.isAllowedOrigin(origin) {
			fmt.Printf("[daemon] rejected origin: %s for %s %s\n", origin, r.Method, r.URL.Path)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Set CORS headers
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			if s.config.GetAuthEnabled() {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		h(w, r)
	}
}

// isAllowedOrigin checks if a request origin should be permitted.
// Allowed origins:
//   - The configured public_base_url (https when auth enabled, http when disabled)
//   - localhost or 127.0.0.1 on the configured port
//   - Any origin if network_access is enabled
func (s *Server) isAllowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}

	port := s.config.GetPort()
	authEnabled := s.config.GetAuthEnabled()

	// Allow configured public_base_url
	if base := s.config.GetPublicBaseURL(); base != "" {
		// Allow the exact configured origin
		if configuredOrigin, err := normalizeOrigin(base); err == nil && origin == configuredOrigin {
			return true
		}
		// When auth is disabled, also allow http version of the hostname
		if !authEnabled {
			if parsed, err := url.Parse(base); err == nil {
				if origin == "http://"+parsed.Host {
					return true
				}
			}
		}
	}

	// Allow localhost
	scheme := "http"
	if authEnabled {
		scheme = "https"
	}
	if origin == fmt.Sprintf("%s://localhost:%d", scheme, port) ||
		origin == fmt.Sprintf("%s://127.0.0.1:%d", scheme, port) {
		return true
	}

	// Allow any origin if network access is enabled
	return s.config.GetNetworkAccess()
}

// normalizeOrigin extracts scheme://host from a URL string.
func normalizeOrigin(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid origin")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

// getDashboardDistPath returns the path to the built dashboard assets.
// Prioritizes local build for development, falls back to cached assets.
func (s *Server) getDashboardDistPath() string {
	// Local dev build - check FIRST (before cached assets)
	candidates := []string{
		"./assets/dashboard/dist",
		filepath.Join(filepath.Dir(os.Args[0]), "../assets/dashboard/dist"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil {
			return candidate
		}
	}

	// User cache (downloaded assets) - fallback if local build not found
	if userAssetsDir, err := assets.GetUserAssetsDir(); err == nil {
		if _, err := os.Stat(filepath.Join(userAssetsDir, "index.html")); err == nil {
			return userAssetsDir
		}
	}

	// Fallback - return first candidate even if it doesn't exist
	// (will result in 404s but won't crash)
	return candidates[0]
}

// RegisterWebSocket registers a WebSocket connection for a session.
func (s *Server) RegisterWebSocket(sessionID string, conn *wsConn) {
	s.wsConnsMu.Lock()
	defer s.wsConnsMu.Unlock()
	s.wsConns[sessionID] = append(s.wsConns[sessionID], conn)
}

// UnregisterWebSocket removes a WebSocket connection for a session.
func (s *Server) UnregisterWebSocket(sessionID string, conn *wsConn) {
	s.wsConnsMu.Lock()
	defer s.wsConnsMu.Unlock()
	conns := s.wsConns[sessionID]
	for i, c := range conns {
		if c == conn {
			s.wsConns[sessionID] = append(conns[:i], conns[i+1:]...)
			if len(s.wsConns[sessionID]) == 0 {
				delete(s.wsConns, sessionID)
			}
			return
		}
	}
}

// BroadcastToSession sends a message to all WebSocket connections for a session
// and closes them. Returns the number of connections notified.
func (s *Server) BroadcastToSession(sessionID string, msgType string, content string) int {
	s.wsConnsMu.Lock()
	conns := s.wsConns[sessionID]
	// Clear the entry so we don't re-notify the same connections
	delete(s.wsConns, sessionID)
	s.wsConnsMu.Unlock()

	count := 0
	for _, conn := range conns {
		msg := WSOutputMessage{Type: msgType, Content: content}
		data, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		if err := conn.WriteMessage(websocket.TextMessage, data); err == nil {
			count++
		}
		conn.Close()
	}
	return count
}

// getRotationLock returns the rotation mutex for a session, creating it if needed.
func (s *Server) getRotationLock(sessionID string) *sync.Mutex {
	s.rotationLocksMu.Lock()
	defer s.rotationLocksMu.Unlock()

	if _, exists := s.rotationLocks[sessionID]; !exists {
		s.rotationLocks[sessionID] = &sync.Mutex{}
	}
	return s.rotationLocks[sessionID]
}

// StartVersionCheck starts an async version check.
func (s *Server) StartVersionCheck() {
	// Initialize current version immediately so it's available via API
	s.versionInfoMu.Lock()
	s.versionInfo = versionInfo{
		Current: version.Version,
	}
	s.versionInfoMu.Unlock()

	go func() {
		latest, available, err := update.CheckForUpdate()
		s.versionInfoMu.Lock()
		s.versionInfo = versionInfo{
			Current:         version.Version,
			Latest:          latest,
			UpdateAvailable: available,
			CheckError:      err,
		}
		s.versionInfoMu.Unlock()
		if err != nil {
			fmt.Printf("[daemon] version check failed: %v\n", err)
		} else if available {
			fmt.Printf("[daemon] update available: %s -> %s\n", version.Version, latest)
		}
	}()
}

// GetVersionInfo returns a copy of the current version info.
func (s *Server) GetVersionInfo() versionInfo {
	s.versionInfoMu.RLock()
	defer s.versionInfoMu.RUnlock()
	return s.versionInfo
}

// RegisterDashboardConn registers a WebSocket connection for dashboard updates.
func (s *Server) RegisterDashboardConn(conn *wsConn) {
	s.sessionsConnsMu.Lock()
	defer s.sessionsConnsMu.Unlock()
	s.sessionsConns[conn] = true
}

// UnregisterDashboardConn removes a WebSocket connection for dashboard updates.
func (s *Server) UnregisterDashboardConn(conn *wsConn) {
	s.sessionsConnsMu.Lock()
	defer s.sessionsConnsMu.Unlock()
	delete(s.sessionsConns, conn)
}

// BroadcastSessions sends the current sessions state to all connected WebSocket clients.
// Debounces to avoid overwhelming clients during rapid changes.
func (s *Server) BroadcastSessions() {
	// Debounce: skip if we broadcast less than 500ms ago
	s.lastBroadcastMu.Lock()
	if time.Since(s.lastBroadcast) < 500*time.Millisecond {
		s.lastBroadcastMu.Unlock()
		return
	}
	s.lastBroadcast = time.Now()
	s.lastBroadcastMu.Unlock()

	// Build the sessions response
	data := s.buildSessionsResponse()

	// Marshal to JSON with type field
	payload, err := json.Marshal(map[string]interface{}{
		"type":       "sessions",
		"workspaces": data,
	})
	if err != nil {
		fmt.Printf("[ws/dashboard] failed to marshal response: %v\n", err)
		return
	}

	// Send to all connected clients
	s.sessionsConnsMu.RLock()
	conns := make([]*wsConn, 0, len(s.sessionsConns))
	for conn := range s.sessionsConns {
		conns = append(conns, conn)
	}
	s.sessionsConnsMu.RUnlock()

	for _, conn := range conns {
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			// Connection error - unregister to avoid repeated failures
			s.UnregisterDashboardConn(conn)
			// Close the connection to clean up resources
			conn.Close()
		}
	}
}

// handleDashboardWebSocket handles WebSocket connections for real-time dashboard updates.
func (s *Server) handleDashboardWebSocket(w http.ResponseWriter, r *http.Request) {
	// Authenticate if auth is enabled
	if s.config.GetAuthEnabled() {
		if _, err := s.authenticateRequest(r); err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Upgrade connection
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true
			}
			return s.isAllowedOrigin(origin)
		},
	}

	rawConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Printf("[ws/dashboard] upgrade error: %v\n", err)
		return
	}

	// Wrap the connection in a wsConn for concurrent write safety
	conn := &wsConn{conn: rawConn}
	defer conn.Close()

	// Register connection
	s.RegisterDashboardConn(conn)
	defer s.UnregisterDashboardConn(conn)

	// Send initial full state with type field
	data := s.buildSessionsResponse()
	payload, err := json.Marshal(map[string]interface{}{
		"type":       "sessions",
		"workspaces": data,
	})
	if err != nil {
		fmt.Printf("[ws/dashboard] failed to marshal initial response: %v\n", err)
		return
	}
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		return
	}

	// Keep connection alive - read messages (client doesn't send any, but we need to detect close)
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			// Connection closed
			break
		}
	}
}
