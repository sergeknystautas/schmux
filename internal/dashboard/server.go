package dashboard

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	dashboardassets "github.com/sergeknystautas/schmux/assets"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/assets"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/difftool"
	"github.com/sergeknystautas/schmux/internal/emergence"
	"github.com/sergeknystautas/schmux/internal/floormanager"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/logging"
	"github.com/sergeknystautas/schmux/internal/lore"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/persona"
	"github.com/sergeknystautas/schmux/internal/preview"
	"github.com/sergeknystautas/schmux/internal/remote"
	"github.com/sergeknystautas/schmux/internal/repofeed"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/tunnel"
	"github.com/sergeknystautas/schmux/internal/update"
	"github.com/sergeknystautas/schmux/internal/version"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

const (
	readTimeout       = 15 * time.Second
	writeTimeout      = time.Duration(0) // Disabled: WebSocket, SSE, and terminal streams are long-lived connections.
	readHeaderTimeout = 5 * time.Second
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
	Shutdown          func()          // Callback to trigger daemon shutdown
	DevRestart        func()          // Callback to trigger dev mode restart (exit code 42)
	DevProxy          bool            // When true, proxy non-API routes to Vite dev server
	DevMode           bool            // When true, dev mode API endpoints are enabled
	ShutdownCtx       context.Context // Context cancelled on daemon shutdown; defaults to context.Background()
	DashboardDistPath string          // Optional explicit dashboard dist path override (mainly for tests)
}

// Server represents the dashboard HTTP server.
type Server struct {
	config       *config.Config
	state        state.StateStore
	statePath    string
	session      *session.Manager
	workspace    workspace.WorkspaceManager
	httpServer   *http.Server
	ipv6Listener net.Listener
	logger       *log.Logger
	shutdown     func() // Callback to trigger daemon shutdown
	devRestart   func() // Callback to trigger dev mode restart (exit code 42)
	devProxy     bool   // When true, proxy non-API routes to Vite dev server
	devMode      bool   // When true, dev mode API endpoints are enabled
	shutdownCtx  context.Context

	// WebSocket connection registry: sessionID -> active connection (for terminal)
	// Only one connection per session; new connections displace old ones.
	wsConns   map[string][]*wsConn
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

	// Model manager (catalog, resolution, enablement)
	models *models.Manager

	// GitHub PR discovery
	prDiscovery github.DiscoveryProvider

	// GitHub CLI auth status
	githubStatus contracts.GitHubStatus

	// Remote host manager
	remoteManager *remote.Manager

	// Workspace preview proxy manager
	previewManager  *preview.Manager
	previewDetect   map[string]time.Time // workspaceID:port -> last detect time
	previewDetectMu sync.Mutex

	// Tunnel manager for remote access
	tunnelManager *tunnel.Manager

	// Remote access auth state (embedded for field promotion)
	remoteAuthState

	// Rate limiter for connection endpoint
	connectLimiter *RateLimiter

	// Linear sync resolve conflict state (embedded for field promotion)
	linearSyncState

	// Embedded dashboard assets (nil if not available)
	dashboardFS fs.FS

	// Optional explicit dashboard dist path override.
	dashboardDistPath string

	// Cached default branches: repoURL -> {branch, fetchedAt}
	defaultBranchCache   map[string]defaultBranchEntry
	defaultBranchCacheMu sync.RWMutex

	// Lore proposal storage
	loreStore *lore.ProposalStore

	// Lore LLM executor for curation requests
	loreExecutor func(ctx context.Context, prompt string, timeout time.Duration) (string, error)

	// Lore instruction store for private layer management
	loreInstructionStore *lore.InstructionStore

	// Dashboard.sx provision status (in-memory, resets on restart)
	dsxProvision   dsxProvisionStatus
	dsxProvisionMu sync.Mutex

	// Streaming executor for observable curation
	streamingExecutor StreamingExecutorFunc

	// Curation state tracking for WebSocket broadcast
	curationTracker *CurationTracker

	// Persona manager
	personaManager *persona.Manager

	// Floor manager
	floorManager *floormanager.Manager

	// Emergence system
	emergenceStore         *emergence.Store
	emergenceMetadataStore *emergence.MetadataStore

	// Subreddit next generation time tracking
	nextSubredditGeneration atomic.Pointer[time.Time]

	// Repofeed publisher and consumer
	repofeedPublisher *repofeed.Publisher
	repofeedConsumer  *repofeed.Consumer
}

// dsxProvisionStatus tracks the progress of dashboard.sx cert provisioning.
type dsxProvisionStatus struct {
	Status  string `json:"status"`  // "", "registered", "provisioning", "complete", "error"
	Domain  string `json:"domain"`  // e.g. "47293.dashboard.sx"
	Message string `json:"message"` // human-readable status message
}

// remoteAuthState groups remote access authentication fields.
type remoteAuthState struct {
	remoteToken          string
	remoteTokenCreatedAt time.Time
	remoteTokenFailures  map[string]int
	remoteTokenMu        sync.Mutex
	remoteSessionSecret  []byte
	remoteTunnelURL      string
	remoteNonces         map[string]*remoteNonce
	remoteAuthLimiter    *RateLimiter
}

// linearSyncState groups linear sync conflict resolution fields.
type linearSyncState struct {
	linearSyncResolveConflictStates   map[string]*LinearSyncResolveConflictState
	linearSyncResolveConflictStatesMu sync.RWMutex
	crTrackers                        map[string]*session.SessionTracker
	crTrackersMu                      sync.RWMutex
}

// versionInfo holds version information.
type versionInfo struct {
	Current         string
	Latest          string
	UpdateAvailable bool
	CheckError      error
}

// defaultBranchEntry is a cached default branch value with a timestamp.
type defaultBranchEntry struct {
	branch    string
	fetchedAt time.Time
}

const defaultBranchCacheTTL = 5 * time.Minute

// NewServer creates a new dashboard server.
func NewServer(cfg *config.Config, st state.StateStore, statePath string, sm *session.Manager, wm workspace.WorkspaceManager, prd github.DiscoveryProvider, logger *log.Logger, ghStatus contracts.GitHubStatus, opts ServerOptions) *Server {
	// Set package-level logger for standalone helper functions
	pkgLogger = logger

	shutdownCtx := opts.ShutdownCtx
	if shutdownCtx == nil {
		shutdownCtx = context.Background()
	}
	s := &Server{
		config:             cfg,
		state:              st,
		statePath:          statePath,
		session:            sm,
		workspace:          wm,
		prDiscovery:        prd,
		logger:             logger,
		githubStatus:       ghStatus,
		shutdown:           opts.Shutdown,
		devRestart:         opts.DevRestart,
		devProxy:           opts.DevProxy,
		devMode:            opts.DevMode,
		shutdownCtx:        shutdownCtx,
		dashboardDistPath:  opts.DashboardDistPath,
		wsConns:            make(map[string][]*wsConn),
		sessionsConns:      make(map[*wsConn]bool),
		rotationLocks:      make(map[string]*sync.Mutex),
		broadcastDone:      make(chan struct{}),
		broadcastExited:    make(chan struct{}),
		broadcastReady:     make(chan struct{}),
		connectLimiter:     NewRateLimiter(3, 1*time.Minute), // 3 connects per minute
		previewDetect:      make(map[string]time.Time),
		defaultBranchCache: make(map[string]defaultBranchEntry),
		curationTracker:    NewCurationTracker(),
		remoteAuthState: remoteAuthState{
			remoteAuthLimiter: NewRateLimiter(5, 1*time.Minute), // 5 auth attempts per minute per IP
			remoteNonces:      make(map[string]*remoteNonce),
		},
		linearSyncState: linearSyncState{
			linearSyncResolveConflictStates: make(map[string]*LinearSyncResolveConflictState),
			crTrackers:                      make(map[string]*session.SessionTracker),
		},
	}
	s.dashboardFS = dashboardassets.FS()

	s.previewManager = preview.NewManager(
		st,
		cfg.GetPreviewMaxPerWorkspace(),
		cfg.GetPreviewMaxGlobal(),
		cfg.GetNetworkAccess(),
		cfg.GetPreviewPortBase(),
		cfg.GetPreviewPortBlockSize(),
		cfg.GetTLSEnabled(),
		cfg.GetTLSCertPath(),
		cfg.GetTLSKeyPath(),
		logging.Sub(logger, "preview"),
		detectListeningPortsByPID,
	)
	s.session.SetOutputCallback(s.handleSessionOutputChunk)

	// Initialize persona manager
	personasDir := filepath.Join(filepath.Dir(statePath), "personas")
	s.personaManager = persona.NewManager(personasDir)
	if err := s.personaManager.EnsureBuiltins(); err != nil {
		logger.Warn("failed to ensure built-in personas", "err", err)
	}

	if mgr, ok := wm.(*workspace.Manager); ok {
		mgr.SetOnLockChangeFn(s.BroadcastWorkspaceLocked)
		mgr.SetSyncProgressFn(func(workspaceID string, current, total int) {
			s.BroadcastWorkspaceLockedWithProgress(workspaceID, current, total)
		})
	}
	go s.broadcastLoop()
	go s.previewReconcileLoop()
	// Start rate limiter cleanup goroutines
	go s.connectLimiter.startCleanup(10 * time.Minute)
	go s.remoteAuthLimiter.startCleanup(10 * time.Minute)
	// Clean up stale agent-port previews
	go s.pruneAgentPreviews()
	return s
}

// SetModelManager sets the model manager for model catalog and resolution.
func (s *Server) SetModelManager(mm *models.Manager) {
	s.models = mm
	// Set up callback to broadcast catalog updates to WebSocket clients
	mm.SetOnCatalogUpdated(func() {
		s.BroadcastCatalogUpdated()
	})
}

// BroadcastCatalogUpdated sends a catalog updated event to all connected dashboard WebSocket clients.
func (s *Server) BroadcastCatalogUpdated() {
	type catalogUpdate struct {
		Type string `json:"type"`
	}
	data, _ := json.Marshal(catalogUpdate{Type: "catalog_updated"})
	s.broadcastToAllDashboardConns(data)
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

// SetLoreInstructionStore sets the lore instruction store for private layer management.
func (s *Server) SetLoreInstructionStore(store *lore.InstructionStore) {
	s.loreInstructionStore = store
}

// SetEmergenceStore sets the emergence store for spawn entry management.
func (s *Server) SetEmergenceStore(store *emergence.Store) {
	s.emergenceStore = store
}

// SetEmergenceMetadataStore sets the emergence metadata store.
func (s *Server) SetEmergenceMetadataStore(store *emergence.MetadataStore) {
	s.emergenceMetadataStore = store
}

// SetLoreExecutor sets the LLM executor for lore curation requests.
func (s *Server) SetLoreExecutor(exec func(ctx context.Context, prompt string, timeout time.Duration) (string, error)) {
	s.loreExecutor = exec
}

// StreamingExecutorFunc is a function that runs a streaming one-shot execution.
type StreamingExecutorFunc func(ctx context.Context, prompt, schemaLabel string, timeout time.Duration, dir string, onEvent func(oneshot.StreamEvent)) (string, error)

// SetStreamingExecutor sets the streaming executor for observable curation.
func (s *Server) SetStreamingExecutor(fn StreamingExecutorFunc) {
	s.streamingExecutor = fn
}

// SetTunnelManager sets the tunnel manager for remote access.
func (s *Server) SetTunnelManager(tm *tunnel.Manager) {
	s.tunnelManager = tm
}

// SetFloorManager sets the floor manager for status and end-shift endpoints.
func (s *Server) SetFloorManager(fm *floormanager.Manager) {
	s.floorManager = fm
}

// OnFloorManagerToggle is a callback for config changes to floor_manager.enabled.
var OnFloorManagerToggle func(enabled bool)

// HandleTunnelConnected handles a newly connected tunnel by generating an auth token and sending notifications.
func (s *Server) HandleTunnelConnected(tunnelURL string) {
	// Generate one-time token (32 bytes, hex-encoded)
	tokenBytes := make([]byte, 32)
	if _, err := crypto_rand.Read(tokenBytes); err != nil {
		logging.Sub(s.logger, "remote-access").Error("failed to generate token", "err", err)
		return
	}
	token := hex.EncodeToString(tokenBytes)

	// Generate new session secret (32 bytes) — invalidates old remote cookies
	secretBytes := make([]byte, 32)
	if _, err := crypto_rand.Read(secretBytes); err != nil {
		logging.Sub(s.logger, "remote-access").Error("failed to generate session secret", "err", err)
		return
	}

	s.remoteTokenMu.Lock()
	s.remoteToken = token
	s.remoteTokenCreatedAt = time.Now()
	s.remoteTokenFailures = make(map[string]int)
	s.remoteSessionSecret = secretBytes
	s.remoteTunnelURL = tunnelURL
	s.remoteTokenMu.Unlock()

	// Build auth URL
	authURL := strings.TrimRight(tunnelURL, "/") + "/remote-auth?token=" + token
	logging.Sub(s.logger, "remote-access").Info("auth URL generated")

	// Send notifications with auth URL
	if s.config != nil {
		ntfyTopic := s.config.GetRemoteAccessNtfyTopic()
		notifyCmd := s.config.GetRemoteAccessNotifyCommand()
		nc := tunnel.NotifyConfig{
			TunnelURL: tunnelURL, // command gets base URL only (no token)
		}
		if ntfyTopic != "" {
			nc.NtfyURL = "https://ntfy.sh/" + ntfyTopic
		}
		nc.Command = notifyCmd
		if err := nc.Send(authURL, "schmux remote access"); err != nil {
			logging.Sub(s.logger, "remote-access").Error("notification error", "err", err)
		}
	}
}

// ClearRemoteAuth clears the remote auth state (token, failures). Called when tunnel stops.
func (s *Server) ClearRemoteAuth() {
	s.remoteTokenMu.Lock()
	s.remoteToken = ""
	s.remoteTokenFailures = make(map[string]int)
	s.remoteSessionSecret = nil
	s.remoteTunnelURL = ""
	s.remoteNonces = make(map[string]*remoteNonce)
	s.remoteTokenMu.Unlock()
}

// LogDashboardAssetPath logs where dashboard assets are being served from.
func (s *Server) LogDashboardAssetPath() {
	if s.dashboardFS != nil {
		s.logger.Info("serving from embedded assets")
		return
	}
	path := s.getDashboardDistPath()
	// Determine source type for clearer message
	if strings.HasPrefix(path, filepath.Join(os.Getenv("HOME"), ".schmux")) {
		s.logger.Info("serving from cached assets", "path", path)
	} else if strings.HasPrefix(path, ".") {
		abs, _ := filepath.Abs(path)
		s.logger.Info("serving from local build", "path", abs)
	} else {
		s.logger.Info("serving from", "path", path)
	}
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	cleanupDelay := time.Duration(s.config.GetExternalDiffCleanupAfterMs()) * time.Millisecond
	deleted, scheduled := difftool.SweepAndScheduleTempDirs(cleanupDelay, logging.Sub(s.logger, "difftool"))
	s.logger.Info("difftool temp dirs cleanup", "deleted", deleted, "scheduled", scheduled)

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

	r := chi.NewRouter()

	// Public routes (no middleware)
	r.Get("/remote-auth", s.handleRemoteAuthGET)
	r.Post("/remote-auth", s.handleRemoteAuthPOST)
	r.HandleFunc("/auth/login", s.handleAuthLogin)
	r.HandleFunc("/auth/callback", s.handleAuthCallback)
	r.HandleFunc("/auth/logout", s.handleAuthLogout)

	// /auth/me — CORS + Auth but outside /api (frontend calls /auth/me directly)
	r.Group(func(r chi.Router) {
		r.Use(s.corsMiddleware)
		r.Use(s.authMiddleware)
		r.Get("/auth/me", s.handleAuthMe)
	})

	// WebSocket routes (inline auth, no CORS middleware)
	r.HandleFunc("/ws/terminal/{id}", s.handleTerminalWebSocket)
	r.HandleFunc("/ws/provision/{id}", s.handleProvisionWebSocket)
	r.HandleFunc("/ws/dashboard", s.handleDashboardWebSocket)

	// App shell + static assets
	if s.devProxy {
		viteProxy := createDevProxyHandler("http://localhost:5173")
		r.Handle("/*", viteProxy)
		s.logger.Info("dev-proxy enabled: proxying to Vite", "target", "http://localhost:5173")
	} else {
		var assetHandler http.Handler
		if s.dashboardFS != nil {
			assetsSubFS, _ := fs.Sub(s.dashboardFS, "assets")
			assetHandler = http.StripPrefix("/assets/", http.FileServer(http.FS(assetsSubFS)))
		} else {
			assetHandler = http.StripPrefix("/assets/", http.FileServer(http.Dir(filepath.Join(s.getDashboardDistPath(), "assets"))))
		}
		r.With(s.authMiddleware).Handle("/assets/*", assetHandler)
		r.HandleFunc("/*", s.handleApp)
	}

	// API routes — CORS + Auth by default
	r.Route("/api", func(r chi.Router) {
		r.Use(s.corsMiddleware)
		r.Use(s.authMiddleware)

		// Read-only endpoints (no CSRF needed)
		r.Get("/healthz", s.handleHealthz)
		r.Get("/sessions", s.handleSessions)
		r.Get("/recent-branches", s.handleRecentBranches)
		r.Get("/detect-tools", s.handleDetectTools)
		r.Get("/models", s.handleModels)
		r.Get("/user-models", s.handleGetUserModels)
		r.Put("/user-models", s.handleSetUserModels)
		r.Get("/builtin-quick-launch", s.handleBuiltinQuickLaunch)
		r.Get("/commit/prompt", s.handleCommitPrompt)
		r.Get("/diff/*", s.handleDiff)
		r.Get("/file/*", s.handleFile)
		r.Get("/overlays", s.handleOverlays)
		r.Get("/prs", s.handlePRs)
		r.Get("/hasNudgenik", s.handleHasNudgenik)
		r.Get("/askNudgenik/*", s.handleAskNudgenik)
		r.Get("/subreddit", s.handleSubreddit)
		r.Get("/repofeed", s.handleRepofeedList)
		r.Get("/repofeed/{slug}", s.handleRepofeedRepo)
		r.Get("/floor-manager", s.handleGetFloorManager)
		r.Get("/remote/hosts", s.handleRemoteHosts)
		r.Get("/remote/hosts/connect/stream", s.handleRemoteConnectStream)
		r.Get("/remote/flavor-statuses", s.handleRemoteFlavorStatuses)
		r.Get("/remote-access/status", s.handleRemoteAccessStatus)

		r.Get("/tls/validate", s.handleTLSValidate)
		r.Get("/debug/tmux-leak", s.handleDebugTmuxLeak)

		r.Get("/sessions/{sessionID}/events", s.handleGetSessionEvents)
		r.Get("/sessions/{sessionID}/capture", s.handleCaptureSession)
		r.Get("/branches", s.handleGetBranches)

		r.Get("/github/status", s.handleGetGitHubStatus)
		r.Get("/features", s.handleGetFeatures)

		// Dashboard.sx callbacks (no additional CSRF — hit by browser redirect before HTTPS is configured)
		r.HandleFunc("/dashboardsx/callback", s.handleDashboardSXCallback)
		r.HandleFunc("/dashboardsx/provision-status", s.handleDashboardSXProvisionStatus)

		// State-changing endpoints (add CSRF)
		r.Group(func(r chi.Router) {
			r.Use(s.csrfMiddleware)

			r.Post("/spawn", s.handleSpawnPost)
			r.Post("/update", s.handleUpdate)
			r.Post("/workspaces/scan", s.handleWorkspacesScan)
			r.Post("/suggest-branch", s.handleSuggestBranch)
			r.Post("/prepare-branch-spawn", s.handlePrepareBranchSpawn)
			r.Post("/check-branch-conflict", s.handleCheckBranchConflict)
			r.Post("/recent-branches/refresh", s.handleRecentBranchesRefresh)
			r.Post("/commit/generate", s.handleCommitGenerate)
			r.Post("/overlays/scan", s.handleOverlayScan)
			r.Post("/overlays/add", s.handleOverlayAdd)
			r.Post("/overlays/dismiss-nudge", s.handleDismissNudge)
			r.Post("/prs/refresh", s.handlePRRefresh)
			r.Post("/prs/checkout", s.handlePRCheckout)
			r.Post("/remote/hosts/connect", s.handleRemoteHostConnect)
			r.Post("/remote-access/on", s.handleRemoteAccessOn)
			r.Post("/remote-access/off", s.handleRemoteAccessOff)
			r.Post("/remote-access/set-password", s.handleRemoteAccessSetPassword)
			r.Post("/remote-access/test-notification", s.handleRemoteAccessTestNotification)
			r.Post("/clipboard-paste", s.handleClipboardPaste)
			r.Post("/floor-manager/end-shift", s.handleEndShift)

			// Session routes
			r.Post("/sessions/{sessionID}/dispose", s.handleDispose)
			r.Post("/sessions/{sessionID}/tell", s.handleTellSession)
			r.Put("/sessions-nickname/{sessionID}", s.handleUpdateNickname)
			r.Patch("/sessions-nickname/{sessionID}", s.handleUpdateNickname)
			r.Put("/sessions-xterm-title/{sessionID}", s.handleUpdateXtermTitle)

			// Config routes
			r.Get("/config", s.handleConfigGet)
			r.Put("/config", s.handleConfigUpdate)
			r.Post("/config", s.handleConfigUpdate)

			// Auth secrets routes
			r.Get("/auth/secrets", s.handleAuthSecretsGet)
			r.Put("/auth/secrets", s.handleAuthSecretsUpdate)
			r.Post("/auth/secrets", s.handleAuthSecretsUpdate)

			// Model routes
			r.Get("/models/{name}/configured", s.handleModelConfigured)
			r.Post("/models/{name}/secrets", s.handleModelSecretsPost)
			r.Delete("/models/{name}/secrets", s.handleModelSecretsDelete)

			// Diff/VSCode routes
			r.Post("/diff-external/*", s.handleDiffExternal)
			r.Post("/open-vscode/*", s.handleOpenVSCode)

			// Remote flavor routes
			r.Get("/config/remote-flavors", s.handleGetRemoteFlavors)
			r.Post("/config/remote-flavors", s.handleCreateRemoteFlavor)
			r.Get("/config/remote-flavors/{id}", s.handleRemoteFlavorGet)
			r.Put("/config/remote-flavors/{id}", s.handleRemoteFlavorUpdate)
			r.Delete("/config/remote-flavors/{id}", s.handleRemoteFlavorDelete)

			// Persona routes
			r.Get("/personas", s.handleListPersonas)
			r.Post("/personas", s.handleCreatePersona)
			r.Get("/personas/{id}", s.handleGetPersona)
			r.Put("/personas/{id}", s.handleUpdatePersona)
			r.Delete("/personas/{id}", s.handleDeletePersona)

			// Remote host routes
			r.Post("/remote/hosts/{hostID}/reconnect", s.handleRemoteHostReconnect)
			r.Delete("/remote/hosts/{hostID}", s.handleRemoteHostDisconnect)

			// Workspace routes (nested group)
			r.Route("/workspaces/{workspaceID}", func(r chi.Router) {
				r.Use(validateWorkspaceID)
				// Inspect route
				r.Get("/inspect", s.handleInspectWorkspace)
				// Preview routes
				r.Get("/previews", s.handlePreviewsList)
				r.Post("/previews", s.handlePreviewsCreate)
				r.Delete("/previews/{previewID}", s.handlePreviewsDelete)

				// Git graph/commit routes
				r.Get("/git-graph", s.handleWorkspaceGitGraph)
				r.Get("/git-commit/{hash}", s.handleWorkspaceGitCommit)

				// Linear sync routes
				r.Post("/linear-sync-from-main", s.handleLinearSyncFromMain)
				r.Post("/linear-sync-to-main", s.handleLinearSyncToMain)
				r.Post("/push-to-branch", s.handlePushToBranch)
				r.Post("/linear-sync-resolve-conflict", s.handleLinearSyncResolveConflict)
				r.Delete("/linear-sync-resolve-conflict-state", s.handleDeleteLinearSyncResolveConflictState)

				// Git operation routes
				r.Post("/git-commit-stage", s.handleGitCommitStage)
				r.Post("/git-amend", s.handleGitAmend)
				r.Post("/git-discard", s.handleGitDiscard)
				r.Post("/git-uncommit", s.handleGitUncommit)
				r.Post("/refresh-overlay", s.handleRefreshOverlay)

				// Workspace dispose routes
				r.Post("/dispose", s.handleDisposeWorkspace)
				r.Post("/dispose-all", s.handleDisposeWorkspaceAll)
			})

			// Lore routes
			r.Get("/lore/status", s.handleLoreStatus)
			r.Get("/lore/curations/active", s.handleLoreCurationsActive)
			r.Route("/lore/{repo}", func(r chi.Router) {
				r.Use(validateLoreRepo)
				r.Get("/proposals", s.handleLoreProposals)
				r.Get("/proposals/{proposalID}", s.handleLoreProposalGet)
				r.Post("/proposals/{proposalID}/dismiss", s.handleLoreDismiss)
				r.Post("/proposals/{proposalID}/rules/{ruleID}", s.handleLoreRuleUpdate)
				r.Post("/proposals/{proposalID}/merge", s.handleLoreMerge)
				r.Post("/proposals/{proposalID}/apply-merge", s.handleLoreApplyMerge)
				r.Get("/entries", s.handleLoreEntries)
				r.Delete("/entries", s.handleLoreEntriesClear)
				r.Post("/curate", s.handleLoreCurate)
				r.Get("/curations", s.handleLoreCurationsList)
				r.Get("/curations/{curationID}/log", s.handleLoreCurationLog)
			})

			// Emergence routes
			r.Route("/emergence/{repo}", func(r chi.Router) {
				r.Use(validateEmergenceRepo)
				r.Get("/entries", s.handleListSpawnEntries)
				r.Get("/entries/all", s.handleListAllSpawnEntries)
				r.Post("/entries", s.handleCreateSpawnEntry)
				r.Put("/entries/{id}", s.handleUpdateSpawnEntry)
				r.Delete("/entries/{id}", s.handleDeleteSpawnEntry)
				r.Post("/entries/{id}/pin", s.handlePinSpawnEntry)
				r.Post("/entries/{id}/dismiss", s.handleDismissSpawnEntry)
				r.Post("/entries/{id}/use", s.handleRecordSpawnEntryUse)
				r.Get("/prompt-history", s.handlePromptHistory)
				r.Post("/curate", s.handleEmergenceCurate)
			})
		})

		// Dev-mode routes
		if s.devMode {
			r.Get("/dev/status", s.handleDevStatus)
			r.Get("/dev/events/history", s.handleEventsHistory)
			r.Group(func(r chi.Router) {
				r.Use(s.csrfMiddleware)
				r.Post("/dev/rebuild", s.handleDevRebuild)
				r.Post("/dev/simulate-tunnel", s.handleDevSimulateTunnel)
				r.Post("/dev/simulate-tunnel-stop", s.handleDevSimulateTunnelStop)
				r.Post("/dev/clear-password", s.handleDevClearPassword)
				r.Post("/dev/diagnostic-append", s.handleDiagnosticAppend)
			})
		}
	})

	// Bind address from config
	bindAddr := s.config.GetBindAddress()

	port := s.config.GetPort()
	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", bindAddr, port),
		Handler:           r,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	scheme := "http"
	if s.config.GetTLSEnabled() {
		scheme = "https"
	}
	if s.config.GetNetworkAccess() {
		s.logger.Info("listening", "addr", fmt.Sprintf("%s://0.0.0.0:%d", scheme, port), "network", true)
	} else {
		s.logger.Info("listening", "addr", fmt.Sprintf("%s://localhost:%d", scheme, port), "network", false)
	}

	if bindAddr == "127.0.0.1" {
		ln6, err := net.Listen("tcp6", fmt.Sprintf("[::1]:%d", port))
		if err == nil {
			s.ipv6Listener = ln6
			s.logger.Info("also listening on IPv6 loopback", "addr", ln6.Addr().String())
			go func() {
				if s.config.GetTLSEnabled() {
					_ = s.httpServer.ServeTLS(ln6, s.config.GetTLSCertPath(), s.config.GetTLSKeyPath())
				} else {
					_ = s.httpServer.Serve(ln6)
				}
			}()
		}
	}

	if s.config.GetTLSEnabled() {
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

	if s.ipv6Listener != nil {
		s.ipv6Listener.Close()
	}
	if err := s.httpServer.Shutdown(ctx); err != nil {
		// Graceful shutdown timed out (e.g. active WebSocket/proxy connections
		// in dev mode). Force-close and continue cleanup instead of failing
		// the entire shutdown — failing here blocks dev restart (exit code 42).
		s.logger.Warn("graceful shutdown timed out, forcing close", "err", err)
		s.httpServer.Close()
	}
	if s.previewManager != nil {
		s.previewManager.Stop()
	}
	// Stop rate limiter cleanup goroutines
	s.connectLimiter.Stop()
	s.remoteAuthLimiter.Stop()

	return nil
}

// CloseForTest stops background goroutines without requiring a running HTTP server.
// This is intended for tests that create a Server via NewServer but don't call ListenAndServe.
func (s *Server) CloseForTest() {
	s.broadcastOnce.Do(func() {
		s.broadcastMu.Lock()
		s.broadcastStopped = true
		if s.broadcastTimer != nil {
			s.broadcastTimer.Stop()
		}
		s.broadcastMu.Unlock()
		close(s.broadcastDone)
	})
	<-s.broadcastExited
	if s.previewManager != nil {
		s.previewManager.Stop()
	}
	s.connectLimiter.Stop()
	s.remoteAuthLimiter.Stop()
}

func (s *Server) isTrustedRequest(r *http.Request) bool {
	// If remote access is not enabled, there's no untrusted path — all requests are trusted
	if !s.config.GetRemoteAccessEnabled() {
		return true
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return false
	}

	// If tunnel is active and the request has a forwarding header,
	// it's a remote request proxied through cloudflared, not a genuine local request
	s.remoteTokenMu.Lock()
	hasTunnel := len(s.remoteSessionSecret) > 0
	s.remoteTokenMu.Unlock()

	if hasTunnel {
		if r.Header.Get("Cf-Connecting-IP") != "" || r.Header.Get("X-Forwarded-For") != "" {
			return false
		}
	}

	return true
}

// corsMiddleware is a chi-compatible middleware for CORS headers and origin validation.
// Returns 403 Forbidden if the request origin is not allowed.
// Sets Access-Control-Allow-Credentials when auth is enabled.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Validate origin
		if origin != "" && !s.isAllowedOrigin(origin) {
			logging.Sub(s.logger, "daemon").Info("rejected origin", "origin", origin, "method", r.Method, "path", r.URL.Path)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Set CORS headers
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			if s.config.GetAuthEnabled() || s.requiresAuth() {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, PUT, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Test-only helper wrapper - kept for backward compatibility with existing tests.
// Production code should use corsMiddleware via r.Use() instead.
func (s *Server) withCORS(h http.HandlerFunc) http.HandlerFunc {
	return s.corsMiddleware(http.HandlerFunc(h)).ServeHTTP
}

// isAllowedOrigin checks if a request origin should be permitted.
// Allowed origins:
//   - The configured public_base_url (https when TLS enabled, http when disabled)
//   - localhost or 127.0.0.1 on the configured port
//   - Any origin if network_access is enabled
func (s *Server) isAllowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}

	port := s.config.GetPort()
	tlsEnabled := s.config.GetTLSEnabled()

	// When a remote tunnel is active, restrict origins to localhost and the tunnel URL only
	s.remoteTokenMu.Lock()
	tunnelURL := s.remoteTunnelURL
	hasTunnel := len(s.remoteSessionSecret) > 0
	s.remoteTokenMu.Unlock()

	if hasTunnel {
		// Allow tunnel origin
		if tunnelURL != "" {
			if tunnelOrigin, err := normalizeOrigin(tunnelURL); err == nil && origin == tunnelOrigin {
				return true
			}
		}
		// Allow localhost only (not arbitrary origins)
		if origin == fmt.Sprintf("http://localhost:%d", port) ||
			origin == fmt.Sprintf("http://127.0.0.1:%d", port) ||
			origin == fmt.Sprintf("https://localhost:%d", port) ||
			origin == fmt.Sprintf("https://127.0.0.1:%d", port) {
			return true
		}
		return false
	}

	// Allow configured public_base_url
	if base := s.config.GetPublicBaseURL(); base != "" {
		// Allow the exact configured origin
		if configuredOrigin, err := normalizeOrigin(base); err == nil && origin == configuredOrigin {
			return true
		}
		// When TLS is disabled, also allow http version of the hostname
		if !tlsEnabled {
			if parsed, err := url.Parse(base); err == nil {
				if origin == "http://"+parsed.Host {
					return true
				}
			}
		}
	}

	// Allow configured dashboard_hostname
	if dashURL := s.config.GetDashboardURL(); dashURL != "" {
		if configuredOrigin, err := normalizeOrigin(dashURL); err == nil && origin == configuredOrigin {
			return true
		}
	}

	// Allow localhost
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}
	if origin == fmt.Sprintf("%s://localhost:%d", scheme, port) ||
		origin == fmt.Sprintf("%s://127.0.0.1:%d", scheme, port) {
		return true
	}

	// When network access is enabled, allow origins on the same port (any host).
	// This permits access from LAN IPs (e.g., http://192.168.1.5:7337) while
	// blocking unrelated websites (e.g., https://evil.com).
	// CSRF tokens provide defense-in-depth for state-changing requests.
	if s.config.GetNetworkAccess() {
		parsed, err := url.Parse(origin)
		if err == nil && parsed.Port() == fmt.Sprintf("%d", port) {
			return true
		}
	}

	return false
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
	if s.dashboardDistPath != "" {
		return s.dashboardDistPath
	}

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
	proxy.Transport = &http.Transport{
		DisableKeepAlives: true,
	}

	// Customize the director to handle WebSocket upgrade for Vite HMR
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
	}

	return proxy
}

// RegisterWebSocket registers a WebSocket connection for a session.
// Multiple connections per session are supported (multi-client).
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
			break
		}
	}
	if len(s.wsConns[sessionID]) == 0 {
		delete(s.wsConns, sessionID)
	}
}

// BroadcastToSession sends a message to all WebSocket connections for a session
// and closes them. Returns the number of connections notified.
func (s *Server) BroadcastToSession(sessionID string, msgType string, content string) int {
	s.wsConnsMu.Lock()
	conns := s.wsConns[sessionID]
	delete(s.wsConns, sessionID)
	s.wsConnsMu.Unlock()

	if len(conns) == 0 {
		return 0
	}

	msg := WSOutputMessage{Type: msgType, Content: content}
	data, err := json.Marshal(msg)
	if err != nil {
		return 0
	}

	count := 0
	for _, conn := range conns {
		if conn.IsClosed() {
			continue
		}
		_ = conn.WriteMessage(websocket.TextMessage, data)
		conn.Close()
		count++
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
			s.logger.Warn("version check failed", "err", err)
		} else if available {
			s.logger.Info("update available", "current", version.Version, "latest", latest)
		}
	}()
}

// GetVersionInfo returns a copy of the current version info.
func (s *Server) GetVersionInfo() versionInfo {
	s.versionInfoMu.RLock()
	defer s.versionInfoMu.RUnlock()
	return s.versionInfo
}

// GetNextSubredditGeneration returns the scheduled time for next digest generation.
func (s *Server) GetNextSubredditGeneration() *time.Time {
	return s.nextSubredditGeneration.Load()
}

// SetNextSubredditGeneration sets the scheduled time for next digest generation.
func (s *Server) SetNextSubredditGeneration(t time.Time) {
	s.nextSubredditGeneration.Store(&t)
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
		logging.Sub(s.logger, "ws/dashboard").Error("failed to marshal response", "err", err)
		return
	}

	// Build linear sync resolve conflict state payloads
	var crPayloads [][]byte
	for _, crState := range s.getAllLinearSyncResolveConflictStates() {
		crPayload, err := json.Marshal(crState)
		if err != nil {
			logging.Sub(s.logger, "ws/dashboard").Error("failed to marshal linear sync resolve conflict state", "err", err)
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
		// Send GitHub status as a separate message
		ghPayload, err := json.Marshal(map[string]interface{}{
			"type":          "github_status",
			"github_status": s.githubStatus,
		})
		if err == nil {
			conn.WriteMessage(websocket.TextMessage, ghPayload)
		}
	}
}

// OverlayChangeEvent represents a file change that was propagated across workspaces.
type OverlayChangeEvent struct {
	Type               string   `json:"type"`
	RelPath            string   `json:"rel_path"`
	SourceWorkspaceID  string   `json:"source_workspace_id"`
	SourceBranch       string   `json:"source_branch"`
	TargetWorkspaceIDs []string `json:"target_workspace_ids"`
	Timestamp          int64    `json:"timestamp"`
	UnifiedDiff        string   `json:"unified_diff"`
}

// BroadcastWorkspaceLocked sends a workspace lock state change to all connected dashboard WebSocket clients.
// Sent in real-time (not debounced) so clients see lock/unlock immediately.
func (s *Server) BroadcastWorkspaceLocked(workspaceID string, locked bool) {
	payload, err := json.Marshal(map[string]interface{}{
		"type":         "workspace_locked",
		"workspace_id": workspaceID,
		"locked":       locked,
	})
	if err != nil {
		return
	}
	s.broadcastToAllDashboardConns(payload)
}

// BroadcastWorkspaceLockedWithProgress sends a workspace lock message with sync progress info.
func (s *Server) BroadcastWorkspaceLockedWithProgress(workspaceID string, current, total int) {
	payload, err := json.Marshal(map[string]interface{}{
		"type":         "workspace_locked",
		"workspace_id": workspaceID,
		"locked":       true,
		"sync_progress": map[string]int{
			"current": current,
			"total":   total,
		},
	})
	if err != nil {
		return
	}
	s.broadcastToAllDashboardConns(payload)
}

// BroadcastWorkspaceUnlockedWithSyncResult sends an unlock message that includes
// sync completion metadata for linear-sync-from-main.
func (s *Server) BroadcastWorkspaceUnlockedWithSyncResult(workspaceID string, result *workspace.LinearSyncResult, err error) {
	syncResult := map[string]interface{}{}
	if err != nil {
		syncResult["success"] = false
		syncResult["message"] = err.Error()
	} else if result != nil {
		syncResult["success"] = result.Success
		syncResult["success_count"] = result.SuccessCount
		if result.ConflictingHash != "" {
			syncResult["conflicting_hash"] = result.ConflictingHash
		}
		if result.Branch != "" {
			syncResult["branch"] = result.Branch
		}
	}

	payload, marshalErr := json.Marshal(map[string]interface{}{
		"type":         "workspace_locked",
		"workspace_id": workspaceID,
		"locked":       false,
		"sync_result":  syncResult,
	})
	if marshalErr != nil {
		return
	}
	s.broadcastToAllDashboardConns(payload)
}

// broadcastToAllDashboardConns sends a raw payload to all connected dashboard WebSocket clients.
func (s *Server) broadcastToAllDashboardConns(payload []byte) {
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
		}
	}
}

// BroadcastOverlayChange sends an overlay change event to all connected dashboard WebSocket clients.
func (s *Server) BroadcastOverlayChange(event OverlayChangeEvent) {
	event.Type = "overlay_change"
	payload, err := json.Marshal(event)
	if err != nil {
		logging.Sub(s.logger, "ws/dashboard").Error("failed to marshal overlay change", "err", err)
		return
	}
	s.broadcastToAllDashboardConns(payload)
}

// BroadcastTunnelStatus sends the current tunnel status to all dashboard WebSocket clients.
func (s *Server) BroadcastTunnelStatus(status tunnel.TunnelStatus) {
	// Clear remote auth state when tunnel goes off or errors
	if status.State == tunnel.StateOff || status.State == tunnel.StateError {
		s.ClearRemoteAuth()
	}

	msg := map[string]interface{}{
		"type": "remote_access_status",
		"data": status,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	s.broadcastToAllDashboardConns(data)
}

// BroadcastPendingNavigation sends a pending navigation event to all dashboard WebSocket clients.
// navType is "preview", and id1/id2 are workspaceId/previewId for preview navigation.
func (s *Server) BroadcastPendingNavigation(navType string, id1, id2 string) {
	msg := map[string]interface{}{
		"type":    "pending_navigation",
		"navType": navType,
		"id1":     id1,
		"id2":     id2,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	s.broadcastToAllDashboardConns(data)
}

// BroadcastCuratorEvent sends a curator stream event to all connected dashboard WebSocket clients.
func (s *Server) BroadcastCuratorEvent(event CuratorEvent) {
	msg := struct {
		Type  string       `json:"type"`
		Event CuratorEvent `json:"event"`
	}{
		Type:  "curator_event",
		Event: event,
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}
	s.broadcastToAllDashboardConns(payload)
}

// BroadcastEvent sends a raw event to all connected dashboard WebSocket clients.
// Used by the event monitor (dev mode only).
func (s *Server) BroadcastEvent(sessionID string, data json.RawMessage) {
	msg := struct {
		Type      string          `json:"type"`
		SessionID string          `json:"session_id"`
		Event     json.RawMessage `json:"event"`
	}{
		Type:      "event",
		SessionID: sessionID,
		Event:     data,
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}
	s.broadcastToAllDashboardConns(payload)
}

// handleDashboardWebSocket handles WebSocket connections for real-time dashboard updates.
func (s *Server) handleDashboardWebSocket(w http.ResponseWriter, r *http.Request) {
	// Authenticate if auth is required (GitHub OAuth or tunnel active)
	if s.requiresAuth() {
		// Local requests bypass tunnel-only auth (consistent with authMiddleware)
		if s.authEnabled() || !s.isTrustedRequest(r) {
			if _, err := s.authenticateRequest(r); err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
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
		logging.Sub(s.logger, "ws/dashboard").Error("upgrade error", "err", err)
		return
	}
	rawConn.SetReadLimit(64 * 1024) // 64KB max message size

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
		logging.Sub(s.logger, "ws/dashboard").Error("failed to marshal initial response", "err", err)
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

	// Send active curations so reconnecting clients see current state
	if runs := s.curationTracker.Active(); len(runs) > 0 {
		for _, run := range runs {
			msg := struct {
				Type string       `json:"type"`
				Run  *CurationRun `json:"run"`
			}{Type: "curator_state", Run: run}
			if curPayload, err := json.Marshal(msg); err == nil {
				if err := conn.WriteMessage(websocket.TextMessage, curPayload); err != nil {
					return
				}
			}
		}
	}

	// Send recently completed curations so reconnecting clients
	// learn about completions/errors that happened while disconnected.
	if runs := s.curationTracker.Recent(60 * time.Second); len(runs) > 0 {
		for _, run := range runs {
			msg := struct {
				Type string       `json:"type"`
				Run  *CurationRun `json:"run"`
			}{Type: "curator_state", Run: run}
			if curPayload, err := json.Marshal(msg); err == nil {
				if err := conn.WriteMessage(websocket.TextMessage, curPayload); err != nil {
					return
				}
			}
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
	maxKeys   int           // maximum number of keys (0 = default 10000)
	cleanupCh chan struct{} // signal cleanup goroutine to stop
	stopOnce  sync.Once
}

type bucket struct {
	tokens    int
	lastReset time.Time
}

const defaultMaxKeys = 10000

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(rate int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		buckets:   make(map[string]*bucket),
		rate:      rate,
		window:    window,
		maxKeys:   defaultMaxKeys,
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
		// Before adding a new key, enforce the cap.
		maxKeys := rl.maxKeys
		if maxKeys <= 0 {
			maxKeys = defaultMaxKeys
		}
		if !exists && len(rl.buckets) >= maxKeys {
			// Evict the oldest entry to make room.
			rl.evictOldestLocked()
		}
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

// evictOldestLocked removes the bucket with the oldest lastReset.
// Must be called with rl.mu held.
func (rl *RateLimiter) evictOldestLocked() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for k, b := range rl.buckets {
		if first || b.lastReset.Before(oldestTime) {
			oldestKey = k
			oldestTime = b.lastReset
			first = false
		}
	}
	if !first {
		delete(rl.buckets, oldestKey)
	}
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

// Stop stops the cleanup goroutine. Safe to call multiple times.
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() {
		close(rl.cleanupCh)
	})
}

// normalizeIPForRateLimit extracts the IP address from a request,
// using forwarding headers when a tunnel is active and the request arrives from loopback.
func (s *Server) normalizeIPForRateLimit(r *http.Request) string {
	ip := extractIPFromAddr(r.RemoteAddr)

	// Only trust forwarding headers when tunnel is active AND request is from loopback
	s.remoteTokenMu.Lock()
	hasTunnel := len(s.remoteSessionSecret) > 0
	s.remoteTokenMu.Unlock()

	if hasTunnel && net.ParseIP(ip) != nil && net.ParseIP(ip).IsLoopback() {
		if cfIP := strings.TrimSpace(r.Header.Get("Cf-Connecting-IP")); cfIP != "" {
			// Basic validation: reject values that are too long or contain unexpected characters
			if len(cfIP) <= 45 && net.ParseIP(cfIP) != nil {
				return cfIP
			}
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.SplitN(xff, ",", 2)
			xffIP := strings.TrimSpace(parts[0])
			if len(xffIP) <= 45 && net.ParseIP(xffIP) != nil {
				return xffIP
			}
		}
	}
	return ip
}

// extractIPFromAddr extracts the IP portion from a RemoteAddr string (IP:port or [IPv6]:port).
func extractIPFromAddr(remoteAddr string) string {
	if idx := strings.LastIndex(remoteAddr, ":"); idx != -1 {
		ip := remoteAddr[:idx]
		ip = strings.Trim(ip, "[]")
		return ip
	}
	return remoteAddr
}

func (s *Server) setCRTracker(tmuxName string, tracker *session.SessionTracker) {
	s.crTrackersMu.Lock()
	defer s.crTrackersMu.Unlock()
	s.crTrackers[tmuxName] = tracker
}

func (s *Server) getCRTracker(tmuxName string) *session.SessionTracker {
	s.crTrackersMu.RLock()
	defer s.crTrackersMu.RUnlock()
	return s.crTrackers[tmuxName]
}

func (s *Server) deleteCRTracker(tmuxName string) {
	s.crTrackersMu.Lock()
	defer s.crTrackersMu.Unlock()
	delete(s.crTrackers, tmuxName)
}

func (s *Server) cleanupCRTrackers(crState *LinearSyncResolveConflictState) {
	crState.mu.Lock()
	tmuxName := crState.TmuxSession
	crState.TmuxSession = ""
	crState.mu.Unlock()
	if tmuxName != "" {
		if tracker := s.getCRTracker(tmuxName); tracker != nil {
			tracker.Stop()
			s.deleteCRTracker(tmuxName)
		}
	}
}
