package dashboard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/config"
)

// TestIsValidResourceID removed - now tests moved to validation_test.go after refactoring

func TestCheckWSOrigin(t *testing.T) {
	skipUnderVendorlocked(t)
	t.Run("allows localhost when auth not required", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Network = &config.NetworkConfig{Port: 7337}
		s := &Server{config: cfg}

		req := httptest.NewRequest("GET", "/ws/terminal/test", nil)
		req.Header.Set("Origin", "http://localhost:7337")
		if !s.checkWSOrigin(req) {
			t.Error("should allow localhost origin")
		}
	})

	t.Run("allows empty origin when auth not required", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Network = &config.NetworkConfig{Port: 7337}
		s := &Server{config: cfg}

		req := httptest.NewRequest("GET", "/ws/terminal/test", nil)
		// No Origin header
		if !s.checkWSOrigin(req) {
			t.Error("should allow empty origin when auth not required (CLI/curl clients)")
		}
	})

	t.Run("rejects unknown origin", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Network = &config.NetworkConfig{Port: 7337}
		s := &Server{config: cfg}

		req := httptest.NewRequest("GET", "/ws/terminal/test", nil)
		req.Header.Set("Origin", "http://evil.com")
		if s.checkWSOrigin(req) {
			t.Error("should reject unknown origin")
		}
	})

	t.Run("allows configured public_base_url", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Network = &config.NetworkConfig{
			Port:          7337,
			PublicBaseURL: "https://schmux.example.com:7337",
		}
		cfg.AccessControl = &config.AccessControlConfig{Enabled: true}
		s := &Server{config: cfg}

		req := httptest.NewRequest("GET", "/ws/terminal/test", nil)
		req.Header.Set("Origin", "https://schmux.example.com:7337")
		if !s.checkWSOrigin(req) {
			t.Error("should allow configured public_base_url origin")
		}
	})

	t.Run("rejects empty origin when auth required", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Network = &config.NetworkConfig{Port: 7337}
		cfg.AccessControl = &config.AccessControlConfig{
			Enabled: true,
		}
		s := &Server{config: cfg}

		req := httptest.NewRequest("GET", "/ws/terminal/test", nil)
		// No Origin header — with auth required, isAllowedOrigin("") returns false
		if s.checkWSOrigin(req) {
			t.Error("should reject empty origin when auth required")
		}
	})
}

func TestIsAllowedOrigin(t *testing.T) {
	skipUnderVendorlocked(t)
	t.Run("empty origin returns false", func(t *testing.T) {
		cfg := &config.Config{}
		s := &Server{config: cfg}

		if s.isAllowedOrigin("") {
			t.Error("empty origin should return false")
		}
	})

	t.Run("localhost allowed with http when auth disabled", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Network = &config.NetworkConfig{Port: 7337}
		s := &Server{config: cfg}

		if !s.isAllowedOrigin("http://localhost:7337") {
			t.Error("http://localhost:7337 should be allowed when auth disabled")
		}
		if !s.isAllowedOrigin("http://127.0.0.1:7337") {
			t.Error("http://127.0.0.1:7337 should be allowed when auth disabled")
		}
	})

	t.Run("localhost allowed with https when TLS enabled", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Network = &config.NetworkConfig{
			Port: 7337,
			TLS: &config.TLSConfig{
				CertPath: "/path/to/cert.pem",
				KeyPath:  "/path/to/key.pem",
			},
		}
		cfg.AccessControl = &config.AccessControlConfig{Enabled: true}
		s := &Server{config: cfg}

		if !s.isAllowedOrigin("https://localhost:7337") {
			t.Error("https://localhost:7337 should be allowed when TLS enabled")
		}
		if !s.isAllowedOrigin("https://127.0.0.1:7337") {
			t.Error("https://127.0.0.1:7337 should be allowed when TLS enabled")
		}
		// http should NOT be allowed when TLS enabled
		if s.isAllowedOrigin("http://localhost:7337") {
			t.Error("http://localhost:7337 should NOT be allowed when TLS enabled")
		}
	})

	t.Run("configured public_base_url allowed", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Network = &config.NetworkConfig{
			Port:          7337,
			PublicBaseURL: "https://schmux.local:7337",
		}
		cfg.AccessControl = &config.AccessControlConfig{Enabled: true}
		s := &Server{config: cfg}

		if !s.isAllowedOrigin("https://schmux.local:7337") {
			t.Error("configured public_base_url should be allowed")
		}
	})

	t.Run("http version of public_base_url allowed when auth disabled", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Network = &config.NetworkConfig{
			Port:          7337,
			PublicBaseURL: "https://schmux.local:7337",
		}
		s := &Server{config: cfg}

		if !s.isAllowedOrigin("http://schmux.local:7337") {
			t.Error("http version of public_base_url should be allowed when auth disabled")
		}
	})

	t.Run("random origin rejected when network_access disabled", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Network = &config.NetworkConfig{Port: 7337}
		s := &Server{config: cfg}

		if s.isAllowedOrigin("http://evil.com") {
			t.Error("random origin should be rejected when network_access disabled")
		}
		if s.isAllowedOrigin("http://192.168.1.100:7337") {
			t.Error("LAN IP should be rejected when network_access disabled")
		}
	})

	t.Run("same-port origins allowed when network_access enabled", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Network = &config.NetworkConfig{
			Port:        7337,
			BindAddress: "0.0.0.0",
		}
		s := &Server{config: cfg}

		if !s.isAllowedOrigin("http://192.168.1.100:7337") {
			t.Error("LAN IP on same port should be allowed when network_access enabled")
		}
		if s.isAllowedOrigin("http://any-hostname:8080") {
			t.Error("different port should be rejected even when network_access enabled")
		}
		if s.isAllowedOrigin("https://evil.com") {
			t.Error("unrelated origin should be rejected even when network_access enabled")
		}
	})

	t.Run("default port used when not configured", func(t *testing.T) {
		cfg := &config.Config{}
		s := &Server{config: cfg}

		// Default port is 7337
		if !s.isAllowedOrigin("http://localhost:7337") {
			t.Error("localhost with default port should be allowed")
		}
	})

	t.Run("dashboard_hostname origin allowed", func(t *testing.T) {
		localHost, err := os.Hostname()
		if err != nil {
			t.Skip("cannot get hostname")
		}
		cfg := &config.Config{}
		cfg.Network = &config.NetworkConfig{
			Port:              7337,
			DashboardHostname: localHost,
		}
		s := &Server{config: cfg}

		if !s.isAllowedOrigin("http://" + localHost + ":7337") {
			t.Errorf("dashboard_hostname origin %q should be allowed", localHost)
		}
		if s.isAllowedOrigin("http://evil.com:7337") {
			t.Error("non-dashboard origin should be rejected")
		}
	})

	t.Run("dashboard_hostname with TLS uses https origin", func(t *testing.T) {
		localHost, err := os.Hostname()
		if err != nil {
			t.Skip("cannot get hostname")
		}
		cfg := &config.Config{}
		cfg.Network = &config.NetworkConfig{
			Port:              7337,
			DashboardHostname: localHost,
			TLS: &config.TLSConfig{
				CertPath: "/path/to/cert.pem",
				KeyPath:  "/path/to/key.pem",
			},
		}
		s := &Server{config: cfg}

		if !s.isAllowedOrigin("https://" + localHost + ":7337") {
			t.Errorf("dashboard_hostname with TLS should allow https origin for %q", localHost)
		}
		if s.isAllowedOrigin("http://" + localHost + ":7337") {
			t.Error("http origin should be rejected when TLS enabled")
		}
	})

	t.Run("non-local dashboard_hostname is ignored", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Network = &config.NetworkConfig{
			Port:              7337,
			DashboardHostname: "not.a.local.host.example.com",
		}
		s := &Server{config: cfg}

		if s.isAllowedOrigin("http://not.a.local.host.example.com:7337") {
			t.Error("non-local dashboard_hostname origin should not be allowed")
		}
	})
}

func TestCorsMiddleware(t *testing.T) {
	skipUnderVendorlocked(t)
	newServer := func() *Server {
		cfg := &config.Config{}
		cfg.Network = &config.NetworkConfig{Port: 7337}
		return &Server{
			config: cfg,
			logger: log.NewWithOptions(io.Discard, log.Options{}),
		}
	}

	t.Run("rejects disallowed origin with 403", func(t *testing.T) {
		s := newServer()
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })

		req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		req.Header.Set("Origin", "https://evil.com")
		rr := httptest.NewRecorder()
		s.corsMiddleware(next).ServeHTTP(rr, req)

		if called {
			t.Error("next handler should not run for disallowed origin")
		}
		if rr.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", rr.Code)
		}
	})

	t.Run("allows known origin and sets ACAO header", func(t *testing.T) {
		s := newServer()
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })

		req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		req.Header.Set("Origin", "http://localhost:7337")
		rr := httptest.NewRecorder()
		s.corsMiddleware(next).ServeHTTP(rr, req)

		if !called {
			t.Error("next handler should run for allowed origin")
		}
		if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:7337" {
			t.Errorf("ACAO = %q, want %q", got, "http://localhost:7337")
		}
		// Auth disabled → no credentials header.
		if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "" {
			t.Errorf("ACAC should be empty when auth disabled, got %q", got)
		}
	})

	t.Run("sets credentials header when auth enabled", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Network = &config.NetworkConfig{Port: 7337}
		cfg.AccessControl = &config.AccessControlConfig{Enabled: true}
		s := &Server{
			config: cfg,
			logger: log.NewWithOptions(io.Discard, log.Options{}),
		}

		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
		req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		req.Header.Set("Origin", "http://localhost:7337")
		rr := httptest.NewRecorder()
		s.corsMiddleware(next).ServeHTTP(rr, req)

		if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Errorf("ACAC = %q, want true", got)
		}
	})

	t.Run("OPTIONS preflight short-circuits to 200", func(t *testing.T) {
		s := newServer()
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })

		req := httptest.NewRequest(http.MethodOptions, "/api/x", nil)
		req.Header.Set("Origin", "http://localhost:7337")
		rr := httptest.NewRecorder()
		s.corsMiddleware(next).ServeHTTP(rr, req)

		if called {
			t.Error("next handler should NOT run for OPTIONS preflight")
		}
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
		// Methods/Headers must still be present so the browser can read them.
		if got := rr.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") {
			t.Errorf("ACAM should list POST, got %q", got)
		}
		if got := rr.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "X-CSRF-Token") {
			t.Errorf("ACAH should list X-CSRF-Token, got %q", got)
		}
	})

	t.Run("missing origin header passes through without ACAO", func(t *testing.T) {
		s := newServer()
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })

		req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
		// No Origin header — same-origin request.
		rr := httptest.NewRecorder()
		s.corsMiddleware(next).ServeHTTP(rr, req)

		if !called {
			t.Error("next should run for same-origin request")
		}
		if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("ACAO should be empty for missing origin, got %q", got)
		}
	})
}

func TestNormalizeOrigin(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"https with host", "https://example.com:8080", "https://example.com:8080", false},
		{"http with host", "http://localhost:7337", "http://localhost:7337", false},
		{"with path is stripped", "https://example.com/path?q=1", "https://example.com", false},
		{"empty string errors", "", "", true},
		{"missing scheme errors", "example.com", "", true},
		{"missing host errors", "http://", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeOrigin(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Errorf("normalizeOrigin(%q) = %q, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Errorf("normalizeOrigin(%q) returned error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("normalizeOrigin(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRegisterUnregisterWebSocket(t *testing.T) {
	t.Run("register adds connection", func(t *testing.T) {
		s := &Server{
			wsConns: make(map[string][]*wsConn),
		}
		conn := &wsConn{conn: &websocket.Conn{}}
		sessionID := "session-123"

		s.RegisterWebSocket(sessionID, conn)

		stored := s.wsConns[sessionID]
		if len(stored) != 1 || stored[0] != conn {
			t.Errorf("stored connection is not the same as the one registered")
		}
	})

	t.Run("register allows multiple connections", func(t *testing.T) {
		s := &Server{
			wsConns: make(map[string][]*wsConn),
		}
		conn1 := &wsConn{closed: true}
		conn2 := &wsConn{closed: true}
		sessionID := "session-123"

		s.RegisterWebSocket(sessionID, conn1)
		s.RegisterWebSocket(sessionID, conn2)

		stored := s.wsConns[sessionID]
		if len(stored) != 2 {
			t.Errorf("expected 2 connections, got %d", len(stored))
		}
	})

	t.Run("unregister removes connection", func(t *testing.T) {
		s := &Server{
			wsConns: make(map[string][]*wsConn),
		}
		conn := &wsConn{conn: &websocket.Conn{}}
		sessionID := "session-123"

		s.RegisterWebSocket(sessionID, conn)
		s.UnregisterWebSocket(sessionID, conn)

		if _, exists := s.wsConns[sessionID]; exists {
			t.Errorf("entry should be deleted when last connection is unregistered")
		}
	})

	t.Run("unregister wrong connection is no-op", func(t *testing.T) {
		s := &Server{
			wsConns: make(map[string][]*wsConn),
		}
		conn1 := &wsConn{conn: &websocket.Conn{}}
		conn2 := &wsConn{conn: &websocket.Conn{}}
		sessionID := "session-123"

		s.RegisterWebSocket(sessionID, conn1)
		s.UnregisterWebSocket(sessionID, conn2) // Different connection

		stored := s.wsConns[sessionID]
		if len(stored) != 1 || stored[0] != conn1 {
			t.Errorf("original connection should remain when unregistering different connection")
		}
	})
}

func TestDevProxyHandler(t *testing.T) {
	// Create a mock Vite server
	viteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<!DOCTYPE html><html>Vite Dev Server</html>"))
	}))
	defer viteServer.Close()

	// Extract port from viteServer.URL (format: http://127.0.0.1:PORT)
	viteURL := viteServer.URL

	// Create dev proxy handler
	handler := createDevProxyHandler(viteURL)

	// Test that requests are proxied
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "Vite Dev Server") {
		t.Errorf("expected body to contain 'Vite Dev Server', got %s", string(body))
	}
}
