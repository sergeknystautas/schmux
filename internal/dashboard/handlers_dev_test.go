package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergeknystautas/schmux/internal/tunnel"
)

func TestHandleDevSimulateTunnel(t *testing.T) {
	t.Run("returns token and URL when dev mode enabled", func(t *testing.T) {
		server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}, nil))
		defer server.CloseForTest()
		server.devMode = true

		req := httptest.NewRequest(http.MethodPost, "/api/dev/simulate-tunnel", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		rr := httptest.NewRecorder()

		server.handleDevSimulateTunnel(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var result struct {
			URL   string `json:"url"`
			Token string `json:"token"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if result.URL == "" {
			t.Error("expected non-empty URL")
		}
		if result.Token == "" {
			t.Error("expected non-empty token")
		}
		if !server.requiresAuth() {
			t.Error("requiresAuth should be true after simulating tunnel")
		}
	})

	t.Run("rejects non-POST", func(t *testing.T) {
		server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}, nil))
		defer server.CloseForTest()
		server.devMode = true

		req := httptest.NewRequest(http.MethodGet, "/api/dev/simulate-tunnel", nil)
		rr := httptest.NewRecorder()
		server.handleDevSimulateTunnel(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", rr.Code)
		}
	})
}

func TestHandleDevSimulateTunnelStop(t *testing.T) {
	t.Run("clears tunnel state", func(t *testing.T) {
		server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}, nil))
		defer server.CloseForTest()
		server.devMode = true

		server.HandleTunnelConnected("https://fake-tunnel.trycloudflare.com")
		if !server.requiresAuth() {
			t.Fatal("tunnel should be active")
		}

		req := httptest.NewRequest(http.MethodPost, "/api/dev/simulate-tunnel-stop", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		rr := httptest.NewRecorder()
		server.handleDevSimulateTunnelStop(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		if server.requiresAuth() {
			t.Error("requiresAuth should be false after stopping simulated tunnel")
		}
	})
}
