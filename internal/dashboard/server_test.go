package dashboard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/config"
)

func TestIsValidResourceID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"valid short ID", "ws-abc123", true},
		{"valid UUID-style", "a1b2c3d4-e5f6-7890-abcd-ef1234567890", true},
		{"empty string", "", false},
		{"contains slash", "ws/evil", false},
		{"contains backslash", "ws\\evil", false},
		{"contains dot", "ws.evil", false},
		{"contains null byte", "ws\x00evil", false},
		{"path traversal", "../etc/passwd", false},
		{"too long", strings.Repeat("a", 129), false},
		{"exactly 128 chars", strings.Repeat("a", 128), true},
		{"simple name", "my-workspace", true},
		{"with underscores", "my_workspace_123", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidResourceID(tt.id)
			if got != tt.want {
				t.Errorf("isValidResourceID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestCheckWSOrigin(t *testing.T) {
	t.Run("allows localhost when auth not required", func(t *testing.T) {
		cfg := &config.Config{
			Network: &config.NetworkConfig{Port: 7337},
		}
		s := &Server{config: cfg}

		req := httptest.NewRequest("GET", "/ws/terminal/test", nil)
		req.Header.Set("Origin", "http://localhost:7337")
		if !s.checkWSOrigin(req) {
			t.Error("should allow localhost origin")
		}
	})

	t.Run("allows empty origin when auth not required", func(t *testing.T) {
		cfg := &config.Config{
			Network: &config.NetworkConfig{Port: 7337},
		}
		s := &Server{config: cfg}

		req := httptest.NewRequest("GET", "/ws/terminal/test", nil)
		// No Origin header
		if !s.checkWSOrigin(req) {
			t.Error("should allow empty origin when auth not required (CLI/curl clients)")
		}
	})

	t.Run("rejects unknown origin", func(t *testing.T) {
		cfg := &config.Config{
			Network: &config.NetworkConfig{Port: 7337},
		}
		s := &Server{config: cfg}

		req := httptest.NewRequest("GET", "/ws/terminal/test", nil)
		req.Header.Set("Origin", "http://evil.com")
		if s.checkWSOrigin(req) {
			t.Error("should reject unknown origin")
		}
	})

	t.Run("allows configured public_base_url", func(t *testing.T) {
		cfg := &config.Config{
			Network: &config.NetworkConfig{
				Port:          7337,
				PublicBaseURL: "https://schmux.example.com:7337",
			},
			AccessControl: &config.AccessControlConfig{Enabled: true},
		}
		s := &Server{config: cfg}

		req := httptest.NewRequest("GET", "/ws/terminal/test", nil)
		req.Header.Set("Origin", "https://schmux.example.com:7337")
		if !s.checkWSOrigin(req) {
			t.Error("should allow configured public_base_url origin")
		}
	})

	t.Run("rejects empty origin when auth required", func(t *testing.T) {
		cfg := &config.Config{
			Network: &config.NetworkConfig{Port: 7337},
			AccessControl: &config.AccessControlConfig{
				Enabled: true,
			},
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
	t.Run("empty origin returns false", func(t *testing.T) {
		cfg := &config.Config{}
		s := &Server{config: cfg}

		if s.isAllowedOrigin("") {
			t.Error("empty origin should return false")
		}
	})

	t.Run("localhost allowed with http when auth disabled", func(t *testing.T) {
		cfg := &config.Config{
			Network: &config.NetworkConfig{Port: 7337},
		}
		s := &Server{config: cfg}

		if !s.isAllowedOrigin("http://localhost:7337") {
			t.Error("http://localhost:7337 should be allowed when auth disabled")
		}
		if !s.isAllowedOrigin("http://127.0.0.1:7337") {
			t.Error("http://127.0.0.1:7337 should be allowed when auth disabled")
		}
	})

	t.Run("localhost allowed with https when TLS enabled", func(t *testing.T) {
		cfg := &config.Config{
			Network: &config.NetworkConfig{
				Port: 7337,
				TLS: &config.TLSConfig{
					CertPath: "/path/to/cert.pem",
					KeyPath:  "/path/to/key.pem",
				},
			},
			AccessControl: &config.AccessControlConfig{Enabled: true},
		}
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
		cfg := &config.Config{
			Network: &config.NetworkConfig{
				Port:          7337,
				PublicBaseURL: "https://schmux.local:7337",
			},
			AccessControl: &config.AccessControlConfig{Enabled: true},
		}
		s := &Server{config: cfg}

		if !s.isAllowedOrigin("https://schmux.local:7337") {
			t.Error("configured public_base_url should be allowed")
		}
	})

	t.Run("http version of public_base_url allowed when auth disabled", func(t *testing.T) {
		cfg := &config.Config{
			Network: &config.NetworkConfig{
				Port:          7337,
				PublicBaseURL: "https://schmux.local:7337",
			},
		}
		s := &Server{config: cfg}

		if !s.isAllowedOrigin("http://schmux.local:7337") {
			t.Error("http version of public_base_url should be allowed when auth disabled")
		}
	})

	t.Run("random origin rejected when network_access disabled", func(t *testing.T) {
		cfg := &config.Config{
			Network: &config.NetworkConfig{Port: 7337},
		}
		s := &Server{config: cfg}

		if s.isAllowedOrigin("http://evil.com") {
			t.Error("random origin should be rejected when network_access disabled")
		}
		if s.isAllowedOrigin("http://192.168.1.100:7337") {
			t.Error("LAN IP should be rejected when network_access disabled")
		}
	})

	t.Run("same-port origins allowed when network_access enabled", func(t *testing.T) {
		cfg := &config.Config{
			Network: &config.NetworkConfig{
				Port:        7337,
				BindAddress: "0.0.0.0",
			},
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
		cfg := &config.Config{
			Network: &config.NetworkConfig{
				Port:              7337,
				DashboardHostname: localHost,
			},
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
		cfg := &config.Config{
			Network: &config.NetworkConfig{
				Port:              7337,
				DashboardHostname: localHost,
				TLS: &config.TLSConfig{
					CertPath: "/path/to/cert.pem",
					KeyPath:  "/path/to/key.pem",
				},
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
		cfg := &config.Config{
			Network: &config.NetworkConfig{
				Port:              7337,
				DashboardHostname: "not.a.local.host.example.com",
			},
		}
		s := &Server{config: cfg}

		if s.isAllowedOrigin("http://not.a.local.host.example.com:7337") {
			t.Error("non-local dashboard_hostname origin should not be allowed")
		}
	})
}

func TestGetRotationLock(t *testing.T) {
	t.Run("returns same mutex for same sessionID", func(t *testing.T) {
		s := &Server{
			rotationLocks: make(map[string]*sync.Mutex),
		}
		lock1 := s.getRotationLock("session-123")
		lock2 := s.getRotationLock("session-123")

		if lock1 != lock2 {
			t.Errorf("getRotationLock should return same mutex for same sessionID")
		}
	})

	t.Run("returns different mutexes for different sessionIDs", func(t *testing.T) {
		s := &Server{
			rotationLocks: make(map[string]*sync.Mutex),
		}
		lock1 := s.getRotationLock("session-123")
		lock2 := s.getRotationLock("session-456")

		if lock1 == lock2 {
			t.Errorf("getRotationLock should return different mutexes for different sessionIDs")
		}
	})

	t.Run("concurrent calls are safe", func(t *testing.T) {
		s := &Server{
			rotationLocks: make(map[string]*sync.Mutex),
		}
		sessionID := "session-concurrent"
		var wg sync.WaitGroup
		calls := 10

		for i := 0; i < calls; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.getRotationLock(sessionID)
			}()
		}
		wg.Wait()

		// Should only have one entry in the map
		if len(s.rotationLocks) != 1 {
			t.Errorf("expected 1 entry, got %d", len(s.rotationLocks))
		}
	})
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

func TestBroadcastToSession(t *testing.T) {
	// Note: BroadcastToSession tries to write to WebSocket connections,
	// which requires complex mocking. These tests verify registry behavior only.

	t.Run("clears entry even when connection exists", func(t *testing.T) {
		s := &Server{
			wsConns: make(map[string][]*wsConn),
		}
		// Can't use a real websocket.Conn as it has internal state
		// Just verify the registry is cleared
		s.wsConns["session-123"] = []*wsConn{{conn: &websocket.Conn{}}}

		// This will panic on WriteMessage, but entry should be cleared first
		func() {
			defer func() {
				// Expected to panic due to nil conn internals
				_ = recover()
			}()
			s.BroadcastToSession("session-123", "test", "message")
		}()

		// Entry should be cleared after broadcast attempt
		if _, exists := s.wsConns["session-123"]; exists {
			t.Errorf("entry should be cleared after broadcast attempt")
		}
	})

	t.Run("returns 0 for session with no connections", func(t *testing.T) {
		s := &Server{
			wsConns: make(map[string][]*wsConn),
		}

		count := s.BroadcastToSession("nonexistent", "test", "message")
		if count != 0 {
			t.Errorf("expected 0 for nonexistent session, got %d", count)
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
