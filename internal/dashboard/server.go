package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
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
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/lore"
	"github.com/sergeknystautas/schmux/internal/preview"
	"github.com/sergeknystautas/schmux/internal/remote"
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

// ServerOptions holds optional configuration for NewServer.
type ServerOptions struct {
	Shutdown    func()          // Callback to trigger daemon shutdown
	DevRestart  func()          // Callback to trigger dev mode restart (exit code 42)
	DevProxy    bool            // When true, proxy non-API routes to Vite dev server
	DevMode     bool            // When true, dev mode API endpoints are enabled
	ShutdownCtx context.Context // Context cancelled on daemon shutdown; defaults to context.Background()
}

// Server represents the dashboard HTTP server.
type Server struct {
	config      *config.Config
	state       state.StateStore
	statePath   string
	session     *session.Manager
	workspace   workspace.WorkspaceManager
	httpServer  *http.Server
	shutdown    func() // Callback to trigger daemon shutdown
	devRestart  func() // Callback to trigger dev mode restart (exit code 42)
	devProxy    bool   // When true, proxy non-API routes to Vite dev server
	devMode     bool   // When true, dev mode API endpoints are enabled
	shutdownCtx context.Context

	// WebSocket connection registry: sessionID -> active connection (for terminal)
	// Only one connection per session; new connections displace old ones.
	wsConns   map[string]*wsConn
	wsConnsMu sync.RWMutex

	// Sessions WebSocket connections (for /ws/sessions real-time updates)
	sessionsConns    map[*wsConn]bool
	sessionsConnsMu  sync.RWMutex
	broadcastTimer   *time.Timer
	broadcastMu      sync.Mutex
	broadcastDone    chan struct{}
	broadcastExited  chan struct{}
	broadcastReady   chan struct{}
	broadcastOnce    sync.Once
	broadcastStopped bool

	// Per-session rotation locks to prevent concurrent rotations
	rotationLocks   map[string]*sync.Mutex
	rotationLocksMu sync.RWMutex

	// Version info: current version and latest available version
	versionInfo      versionInfo
	versionInfoMu    sync.RWMutex
	updateInProgress bool
	updateMu         sync.Mutex

	authSessionKey []byte

	// GitHub PR discovery
	prDiscovery *github.Discovery

	// Remote host manager
	remoteManager *remote.Manager

	// Workspace preview proxy manager
	previewManager  *preview.Manager
	previewDetect   map[string]time.Time // workspaceID:port -> last detect time
	previewDetectMu sync.Mutex

	// Rate limiter for connection endpoint
	connectLimiter *RateLimiter

	// Linear sync resolve conflict operation states (in-memory, keyed by workspace ID)
	linearSyncResolveConflictStates   map[string]*LinearSyncResolveConflictState
	linearSyncResolveConflictStatesMu sync.RWMutex

	// Lore proposal storage
	loreStore *lore.ProposalStore

	// Lore curator for manual curation
	loreCurator *lore.Curator
}

// versionInfo holds version information.
type versionInfo struct {
	Current         string
	Latest          string
	UpdateAvailable bool
	CheckError      error
}

// NewServer creates a new dashboard server.
func NewServer(cfg *config.Config, st state.StateStore, statePath string, sm *session.Manager, wm workspace.WorkspaceManager, prd *github.Discovery, opts ServerOptions) *Server {
	shutdownCtx := opts.ShutdownCtx
	if shutdownCtx == nil {
		shutdownCtx = context.Background()
	}
	s := &Server{
		config:                          cfg,
		state:                           st,
		statePath:                       statePath,
		session:                         sm,
		workspace:                       wm,
		prDiscovery:                     prd,
		shutdown:                        opts.Shutdown,
		devRestart:                      opts.DevRestart,
		devProxy:                        opts.DevProxy,
		devMode:                         opts.DevMode,
		shutdownCtx:                     shutdownCtx,
		wsConns:                         make(map[string]*wsConn),
		sessionsConns:                   make(map[*wsConn]bool),
		rotationLocks:                   make(map[string]*sync.Mutex),
		broadcastDone:                   make(chan struct{}),
		broadcastExited:                 make(chan struct{}),
		broadcastReady:                  make(chan struct{}),
		linearSyncResolveConflictStates: make(map[string]*LinearSyncResolveConflictState),
		connectLimiter:                  NewRateLimiter(3, 1*time.Minute), // 3 connects per minute
		previewDetect:                   make(map[string]time.Time),
	}
	s.previewManager = preview.NewManager(
		st,
		cfg.GetPreviewMaxPerWorkspace(),
		cfg.GetPreviewMaxGlobal(),
		time.Duration(cfg.GetPreviewIdleTimeoutMs())*time.Millisecond,
	)
	s.session.SetOutputCallback(s.handleSessionOutputChunk)
	if mgr, ok := wm.(*workspace.Manager); ok {
		mgr.SetWorkspaceLockedFn(func(workspaceID string) bool {
			state := s.getLinearSyncResolveConflictState(workspaceID)
			return state != nil && state.Status == "in_progress"
		})
	}
	go s.broadcastLoop()
	go s.previewReconcileLoop()
	// Start rate limiter cleanup goroutine
	go s.connectLimiter.startCleanup(10 * time.Minute)
	// Scan existing sessions for web server ports
	go s.scanExistingSessionsForPreviews()
	return s
}

// SetRemoteManager sets the remote manager for remote workspace support.
func (s *Server) SetRemoteManager(rm *remote.Manager) {
	s.remoteManager = rm
	// Also set it on the session manager
	s.session.SetRemoteManager(rm)
}

// SetLoreStore sets the lore proposal store for the dashboard API.
func (s *Server) SetLoreStore(store *lore.ProposalStore) {
	s.loreStore = store
}

// SetLoreCurator sets the lore curator for manual curation requests.
func (s *Server) SetLoreCurator(c *lore.Curator) {
	s.loreCurator = c
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
	if s.devProxy {
		// In dev mode, proxy all non-API routes to Vite dev server
		viteProxy := createDevProxyHandler("http://localhost:5173")
		mux.Handle("/", viteProxy)
		fmt.Println("[daemon] dev-proxy enabled: proxying to Vite at http://localhost:5173")
	} else {
		mux.HandleFunc("/", s.handleApp)
		mux.Handle("/assets/", s.withAuthHandler(http.StripPrefix("/assets/", http.FileServer(http.Dir(filepath.Join(s.getDashboardDistPath(), "assets"))))))
	}

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
	mux.HandleFunc("/api/workspaces/", s.withCORS(s.withAuth(s.handleWorkspaceRoutes)))
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
	mux.HandleFunc("/api/models", s.withCORS(s.withAuth(s.handleModels)))
	mux.HandleFunc("/api/models/", s.withCORS(s.withAuth(s.handleModel)))
	mux.HandleFunc("/api/builtin-quick-launch", s.withCORS(s.withAuth(s.handleBuiltinQuickLaunch)))
	mux.HandleFunc("/api/commit/prompt", s.withCORS(s.withAuth(s.handleCommitPrompt)))
	mux.HandleFunc("/api/commit/generate", s.withCORS(s.withAuth(s.handleCommitGenerate)))
	mux.HandleFunc("/api/diff/", s.withCORS(s.withAuth(s.handleDiff)))
	mux.HandleFunc("/api/diff-external/", s.withCORS(s.withAuth(s.handleDiffExternal)))
	mux.HandleFunc("/api/open-vscode/", s.withCORS(s.withAuth(s.handleOpenVSCode)))
	mux.HandleFunc("/api/overlays", s.withCORS(s.withAuth(s.handleOverlays)))
	mux.HandleFunc("/api/overlays/scan", s.withCORS(s.withAuth(s.handleOverlayScan)))
	mux.HandleFunc("/api/overlays/add", s.withCORS(s.withAuth(s.handleOverlayAdd)))
	mux.HandleFunc("/api/overlays/dismiss-nudge", s.withCORS(s.withAuth(s.handleDismissNudge)))
	mux.HandleFunc("/api/prs", s.withCORS(s.withAuth(s.handlePRs)))
	mux.HandleFunc("/api/prs/refresh", s.withCORS(s.withAuth(s.handlePRRefresh)))
	mux.HandleFunc("/api/prs/checkout", s.withCORS(s.withAuth(s.handlePRCheckout)))

	// Lore routes
	mux.HandleFunc("/api/lore/", s.withCORS(s.withAuth(s.handleLoreRouter)))

	// Remote workspace routes
	mux.HandleFunc("/api/config/remote-flavors", s.withCORS(s.withAuth(s.handleRemoteFlavors)))
	mux.HandleFunc("/api/config/remote-flavors/", s.withCORS(s.withAuth(s.handleRemoteFlavor)))
	mux.HandleFunc("/api/remote/hosts", s.withCORS(s.withAuth(s.handleRemoteHosts)))
	mux.HandleFunc("/api/remote/hosts/connect", s.withCORS(s.withAuth(s.handleRemoteHostConnect)))
	mux.HandleFunc("/api/remote/hosts/connect/stream", s.withCORS(s.withAuth(s.handleRemoteConnectStream)))
	mux.HandleFunc("/api/remote/hosts/", s.withCORS(s.withAuth(s.handleRemoteHostRoute)))
	mux.HandleFunc("/api/remote/flavor-statuses", s.withCORS(s.withAuth(s.handleRemoteFlavorStatuses)))

	// Dev mode routes (only registered when --dev-mode is active)
	if s.devMode {
		mux.HandleFunc("/api/dev/status", s.withCORS(s.withAuth(s.handleDevStatus)))
		mux.HandleFunc("/api/dev/rebuild", s.withCORS(s.withAuth(s.handleDevRebuild)))
	}

	// WebSocket for terminal streaming
	mux.HandleFunc("/ws/terminal/", s.handleTerminalWebSocket)

	// WebSocket for provisioning terminal (remote host setup)
	mux.HandleFunc("/ws/provision/", s.handleProvisionWebSocket)

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

// Stop stops the HTTP server. Idempotent - safe to call multiple times.
func (s *Server) Stop() error {
	// Use sync.Once to ensure cleanup happens exactly once
	s.broadcastOnce.Do(func() {
		// Set stopped flag to prevent new broadcasts
		s.broadcastMu.Lock()
		s.broadcastStopped = true
		// Stop and drain the timer
		if s.broadcastTimer != nil {
			s.broadcastTimer.Stop()
			select {
			case <-s.broadcastTimer.C:
			default:
			}
		}
		s.broadcastMu.Unlock()

		// Signal the broadcast loop to exit
		close(s.broadcastDone)
	})

	// Wait for broadcastLoop goroutine to exit
	<-s.broadcastExited

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown server: %w", err)
	}
	if s.previewManager != nil {
		s.previewManager.Stop()
	}
	// Stop rate limiter cleanup goroutine
	s.connectLimiter.Stop()

	return nil
}

func (s *Server) isLocalRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
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

// createDevProxyHandler creates a reverse proxy handler to the Vite dev server.
func createDevProxyHandler(targetURL string) http.Handler {
	target, err := url.Parse(targetURL)
	if err != nil {
		// Fallback: return a handler that returns 502
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Dev proxy misconfigured", http.StatusBadGateway)
		})
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	// Customize the director to handle WebSocket upgrade for Vite HMR
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
	}

	return proxy
}

// RegisterWebSocket registers a WebSocket connection for a session.
// If a connection already exists for this session, it is closed and replaced.
func (s *Server) RegisterWebSocket(sessionID string, conn *wsConn) {
	s.wsConnsMu.Lock()
	defer s.wsConnsMu.Unlock()

	// Close existing connection if present
	if existingConn := s.wsConns[sessionID]; existingConn != nil && !existingConn.IsClosed() {
		// Send displacement message
		msg := WSOutputMessage{Type: "displaced", Content: "Session opened in another window"}
		data, _ := json.Marshal(msg)
		_ = existingConn.WriteMessage(websocket.TextMessage, data)
		_ = existingConn.Close()
	}

	s.wsConns[sessionID] = conn
}

// UnregisterWebSocket removes a WebSocket connection for a session.
func (s *Server) UnregisterWebSocket(sessionID string, conn *wsConn) {
	s.wsConnsMu.Lock()
	defer s.wsConnsMu.Unlock()
	if s.wsConns[sessionID] == conn {
		delete(s.wsConns, sessionID)
	}
}

// BroadcastToSession sends a message to the WebSocket connection for a session
// and closes it. Returns 1 if a connection was notified, 0 otherwise.
func (s *Server) BroadcastToSession(sessionID string, msgType string, content string) int {
	s.wsConnsMu.Lock()
	conn := s.wsConns[sessionID]
	// Clear the entry so we don't re-notify the same connection
	delete(s.wsConns, sessionID)
	s.wsConnsMu.Unlock()

	if conn == nil || conn.IsClosed() {
		return 0
	}

	msg := WSOutputMessage{Type: msgType, Content: content}
	data, err := json.Marshal(msg)
	if err != nil {
		return 0
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err == nil {
		conn.Close()
		return 1
	}
	conn.Close()
	return 0
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
// Uses trailing debounce: waits 100ms after the last call before broadcasting,
// coalescing rapid changes into a single broadcast. No events are dropped.
func (s *Server) BroadcastSessions() {
	s.broadcastMu.Lock()
	defer s.broadcastMu.Unlock()

	// Check if server has been stopped
	if s.broadcastStopped {
		return
	}

	// Lazy initialization: create timer on first use
	if s.broadcastTimer == nil {
		s.broadcastTimer = time.NewTimer(100 * time.Millisecond)
		close(s.broadcastReady)
		return
	}

	if !s.broadcastTimer.Stop() {
		// Timer already fired, drain the channel if possible
		select {
		case <-s.broadcastTimer.C:
		default:
		}
	}
	// Reset timer for 100ms from now
	s.broadcastTimer.Reset(100 * time.Millisecond)
}

// broadcastLoop waits for the debounce timer to fire, then broadcasts to all clients.
func (s *Server) broadcastLoop() {
	defer close(s.broadcastExited)
	// Wait for the timer to be initialized
	select {
	case <-s.broadcastReady:
	case <-s.broadcastDone:
		return
	}

	for {
		select {
		case <-s.broadcastTimer.C:
			// Check shutdown flag before broadcasting
			s.broadcastMu.Lock()
			stopped := s.broadcastStopped
			s.broadcastMu.Unlock()
			if !stopped {
				s.doBroadcast()
			}
		case <-s.broadcastDone:
			return
		}
	}
}

// doBroadcast performs the actual broadcast to all connected WebSocket clients.
func (s *Server) doBroadcast() {
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

	// Build linear sync resolve conflict state payloads
	var crPayloads [][]byte
	for _, crState := range s.getAllLinearSyncResolveConflictStates() {
		crPayload, err := json.Marshal(crState)
		if err != nil {
			fmt.Printf("[ws/dashboard] failed to marshal linear sync resolve conflict state: %v\n", err)
			continue
		}
		crPayloads = append(crPayloads, crPayload)
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
			s.UnregisterDashboardConn(conn)
			conn.Close()
			continue
		}
		// Send linear sync resolve conflict states as separate messages
		for _, crPayload := range crPayloads {
			if err := conn.WriteMessage(websocket.TextMessage, crPayload); err != nil {
				s.UnregisterDashboardConn(conn)
				conn.Close()
				break
			}
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

	// Send any active linear sync resolve conflict states
	for _, crState := range s.getAllLinearSyncResolveConflictStates() {
		crPayload, err := json.Marshal(crState)
		if err != nil {
			continue
		}
		if err := conn.WriteMessage(websocket.TextMessage, crPayload); err != nil {
			return
		}
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

// RateLimiter implements a simple token bucket rate limiter.
type RateLimiter struct {
	buckets   map[string]*bucket
	mu        sync.RWMutex
	rate      int           // requests per window
	window    time.Duration // time window
	cleanupCh chan struct{} // signal cleanup goroutine to stop
}

type bucket struct {
	tokens    int
	lastReset time.Time
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(rate int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		buckets:   make(map[string]*bucket),
		rate:      rate,
		window:    window,
		cleanupCh: make(chan struct{}),
	}
}

// Allow checks if a request should be allowed for the given key.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, exists := rl.buckets[key]
	now := time.Now()

	if !exists || now.Sub(b.lastReset) > rl.window {
		// Reset bucket
		rl.buckets[key] = &bucket{
			tokens:    rl.rate - 1,
			lastReset: now,
		}
		return true
	}

	if b.tokens > 0 {
		b.tokens--
		return true
	}

	return false
}

// cleanup removes stale buckets that haven't been used in 2x the window duration.
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	staleThreshold := rl.window * 2

	for key, b := range rl.buckets {
		if now.Sub(b.lastReset) > staleThreshold {
			delete(rl.buckets, key)
		}
	}
}

// startCleanup starts a goroutine that periodically cleans up stale buckets.
func (rl *RateLimiter) startCleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.cleanupCh:
			return
		}
	}
}

// Stop stops the cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.cleanupCh)
}

// normalizeIPForRateLimit extracts the IP address from a RemoteAddr string,
// stripping the port to prevent rate limit bypass via different ports.
func (s *Server) normalizeIPForRateLimit(remoteAddr string) string {
	// RemoteAddr format is typically "IP:port" or "[IPv6]:port"
	// We want to extract just the IP portion
	if idx := strings.LastIndex(remoteAddr, ":"); idx != -1 {
		ip := remoteAddr[:idx]
		// Remove IPv6 brackets if present
		ip = strings.Trim(ip, "[]")
		return ip
	}
	// No port found, return as-is
	return remoteAddr
}
