package dashboard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/config"
)

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

	t.Run("localhost allowed with https when auth enabled", func(t *testing.T) {
		cfg := &config.Config{
			Network:       &config.NetworkConfig{Port: 7337},
			AccessControl: &config.AccessControlConfig{Enabled: true},
		}
		s := &Server{config: cfg}

		if !s.isAllowedOrigin("https://localhost:7337") {
			t.Error("https://localhost:7337 should be allowed when auth enabled")
		}
		if !s.isAllowedOrigin("https://127.0.0.1:7337") {
			t.Error("https://127.0.0.1:7337 should be allowed when auth enabled")
		}
		// http should NOT be allowed when auth enabled
		if s.isAllowedOrigin("http://localhost:7337") {
			t.Error("http://localhost:7337 should NOT be allowed when auth enabled")
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

	t.Run("any origin allowed when network_access enabled", func(t *testing.T) {
		cfg := &config.Config{
			Network: &config.NetworkConfig{
				Port:        7337,
				BindAddress: "0.0.0.0",
			},
		}
		s := &Server{config: cfg}

		if !s.isAllowedOrigin("http://192.168.1.100:7337") {
			t.Error("LAN IP should be allowed when network_access enabled")
		}
		if !s.isAllowedOrigin("http://any-hostname:8080") {
			t.Error("any origin should be allowed when network_access enabled")
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
			wsConns: make(map[string]*wsConn),
		}
		conn := &wsConn{conn: &websocket.Conn{}}
		sessionID := "session-123"

		s.RegisterWebSocket(sessionID, conn)

		stored := s.wsConns[sessionID]
		if stored != conn {
			t.Errorf("stored connection is not the same as the one registered")
		}
	})

	t.Run("register displaces existing connection", func(t *testing.T) {
		s := &Server{
			wsConns: make(map[string]*wsConn),
		}
		conn1 := &wsConn{closed: true} // Mark as closed so WriteMessage won't panic
		conn2 := &wsConn{closed: true}
		sessionID := "session-123"

		s.RegisterWebSocket(sessionID, conn1)
		s.RegisterWebSocket(sessionID, conn2)

		stored := s.wsConns[sessionID]
		if stored != conn2 {
			t.Errorf("second connection should replace first")
		}
	})

	t.Run("unregister removes connection", func(t *testing.T) {
		s := &Server{
			wsConns: make(map[string]*wsConn),
		}
		conn := &wsConn{conn: &websocket.Conn{}}
		sessionID := "session-123"

		s.RegisterWebSocket(sessionID, conn)
		s.UnregisterWebSocket(sessionID, conn)

		if _, exists := s.wsConns[sessionID]; exists {
			t.Errorf("entry should be deleted when connection is unregistered")
		}
	})

	t.Run("unregister wrong connection is no-op", func(t *testing.T) {
		s := &Server{
			wsConns: make(map[string]*wsConn),
		}
		conn1 := &wsConn{conn: &websocket.Conn{}}
		conn2 := &wsConn{conn: &websocket.Conn{}}
		sessionID := "session-123"

		s.RegisterWebSocket(sessionID, conn1)
		s.UnregisterWebSocket(sessionID, conn2) // Different connection

		if s.wsConns[sessionID] != conn1 {
			t.Errorf("original connection should remain when unregistering different connection")
		}
	})
}

func TestBroadcastToSession(t *testing.T) {
	// Note: BroadcastToSession tries to write to WebSocket connections,
	// which requires complex mocking. These tests verify registry behavior only.

	t.Run("clears entry even when connection exists", func(t *testing.T) {
		s := &Server{
			wsConns: make(map[string]*wsConn),
		}
		// Can't use a real websocket.Conn as it has internal state
		// Just verify the registry is cleared
		s.wsConns["session-123"] = &wsConn{conn: &websocket.Conn{}}

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
			wsConns: make(map[string]*wsConn),
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
