package dashboard

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/tunnel"
)

func TestValidateRemoteCookie_ExpiredCookie(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	// Simulate tunnel connect to generate session secret
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// Create a cookie with a timestamp 25 hours in the past
	oldTimestamp := fmt.Sprintf("%d", time.Now().Add(-25*time.Hour).Unix())
	server.remoteTokenMu.Lock()
	secret := server.remoteSessionSecret
	server.remoteTokenMu.Unlock()

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(oldTimestamp))
	sig := hex.EncodeToString(mac.Sum(nil))

	cookieValue := oldTimestamp + "." + sig

	if server.validateRemoteCookie(cookieValue) {
		t.Error("expected expired cookie to be rejected")
	}
}

func TestValidateRemoteCookie_FreshCookie(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// Create a cookie with the current timestamp
	nowStr := fmt.Sprintf("%d", time.Now().Unix())
	server.remoteTokenMu.Lock()
	secret := server.remoteSessionSecret
	server.remoteTokenMu.Unlock()

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(nowStr))
	sig := hex.EncodeToString(mac.Sum(nil))

	cookieValue := nowStr + "." + sig

	if !server.validateRemoteCookie(cookieValue) {
		t.Error("expected fresh cookie to be accepted")
	}
}

func TestClearRemoteAuth_InvalidatesCookies(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// Create a valid cookie
	nowStr := fmt.Sprintf("%d", time.Now().Unix())
	server.remoteTokenMu.Lock()
	secret := server.remoteSessionSecret
	server.remoteTokenMu.Unlock()

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(nowStr))
	sig := hex.EncodeToString(mac.Sum(nil))
	cookieValue := nowStr + "." + sig

	// Verify it's valid before clearing
	if !server.validateRemoteCookie(cookieValue) {
		t.Fatal("cookie should be valid before clear")
	}

	// Clear auth (simulates tunnel stop)
	server.ClearRemoteAuth()

	// Cookie should now be invalid
	if server.validateRemoteCookie(cookieValue) {
		t.Error("cookie should be invalid after ClearRemoteAuth")
	}
}

func TestRenderPinPage_EscapesToken(t *testing.T) {
	// A malicious token with HTML/JS injection
	maliciousToken := `"><script>alert('xss')</script><input value="`
	output := renderPinPage(maliciousToken, "", 5)

	if strings.Contains(output, "<script>") {
		t.Error("renderPinPage must HTML-escape the token value")
	}
	if !strings.Contains(output, "&lt;script&gt;") {
		t.Error("expected escaped script tag in output")
	}
}

func TestRenderPinPage_EscapesErrorMsg(t *testing.T) {
	maliciousMsg := `<img src=x onerror=alert(1)>`
	output := renderPinPage("", maliciousMsg, 0)

	if strings.Contains(output, "<img") {
		t.Error("renderPinPage must HTML-escape error messages")
	}
	if !strings.Contains(output, "&lt;img") {
		t.Error("expected escaped img tag in output")
	}
}

func TestRequiresAuth_TrueWhenTunnelActive(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	// Simulate tunnel connected — sets remoteSessionSecret
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// Auth is NOT enabled (no GitHub OAuth) but tunnel IS active
	if !server.requiresAuth() {
		t.Error("requiresAuth should return true when tunnel is active")
	}
}

func TestRequiresAuth_FalseWhenNoTunnel(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	// No tunnel connected, no auth enabled — should NOT require auth
	if server.requiresAuth() {
		t.Error("requiresAuth should return false when tunnel is not active and auth is disabled")
	}
}

func TestRequiresAuth_FalseAfterTunnelStops(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	if !server.requiresAuth() {
		t.Fatal("should require auth while tunnel is active")
	}

	server.ClearRemoteAuth()

	if server.requiresAuth() {
		t.Error("requiresAuth should return false after tunnel stops and secret is cleared")
	}
}

func TestRemoteAccessOff_RequiresCSRFWhenRemoteSession(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// Build a valid remote cookie
	nowStr := fmt.Sprintf("%d", time.Now().Unix())
	server.remoteTokenMu.Lock()
	secret := server.remoteSessionSecret
	server.remoteTokenMu.Unlock()
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(nowStr))
	sig := hex.EncodeToString(mac.Sum(nil))

	// POST with valid remote cookie but NO CSRF token from a non-local address
	req, _ := http.NewRequest("POST", "/api/remote-access/off", nil)
	req.AddCookie(&http.Cookie{Name: "schmux_remote", Value: nowStr + "." + sig})
	req.RemoteAddr = "1.2.3.4:12345" // non-local to trigger CSRF check

	rr := httptest.NewRecorder()
	handler := server.withAuthAndCSRF(server.handleRemoteAccessOff)
	handler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden without CSRF token, got %d", rr.Code)
	}
}

func TestHandleRemoteAccessSetPin_RejectsShortPin(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()

	tests := []struct {
		name string
		pin  string
	}{
		{"empty", ""},
		{"too short 3 chars", "abc"},
		{"too short 5 chars", "abcde"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.NewReader(fmt.Sprintf(`{"pin":%q}`, tt.pin))
			req, _ := http.NewRequest("POST", "/api/remote-access/set-pin", body)
			req.Header.Set("Content-Type", "application/json")
			req.RemoteAddr = "127.0.0.1:12345"

			rr := httptest.NewRecorder()
			server.handleRemoteAccessSetPin(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for PIN %q, got %d", tt.pin, rr.Code)
			}
		})
	}
}

func TestRemoteAccessOff_AllowsLocalRequestWithoutCSRF(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// Build a valid remote cookie
	nowStr := fmt.Sprintf("%d", time.Now().Unix())
	server.remoteTokenMu.Lock()
	secret := server.remoteSessionSecret
	server.remoteTokenMu.Unlock()
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(nowStr))
	sig := hex.EncodeToString(mac.Sum(nil))

	// POST from localhost — should NOT require CSRF
	req, _ := http.NewRequest("POST", "/api/remote-access/off", nil)
	req.AddCookie(&http.Cookie{Name: "schmux_remote", Value: nowStr + "." + sig})
	req.RemoteAddr = "127.0.0.1:12345" // local

	rr := httptest.NewRecorder()
	handler := server.withAuthAndCSRF(server.handleRemoteAccessOff)
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for local request without CSRF, got %d", rr.Code)
	}
}

func TestRemoteAuth_RateLimiting(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// Get the token
	server.remoteTokenMu.Lock()
	token := server.remoteToken
	server.remoteTokenMu.Unlock()

	// The rate limiter allows 5 requests per minute per IP
	// Send 6 POST requests from the same IP — 6th should be rate-limited
	for i := 0; i < 5; i++ {
		body := strings.NewReader("token=" + token + "&pin=wrong")
		req, _ := http.NewRequest("POST", "/remote-auth", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "1.2.3.4:12345"
		rr := httptest.NewRecorder()
		server.handleRemoteAuthPOST(rr, req)
		// These may fail for various reasons but should NOT be 429
		if rr.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d should not be rate-limited", i+1)
		}
	}

	// 6th request should be rate-limited
	body := strings.NewReader("token=" + token + "&pin=wrong")
	req, _ := http.NewRequest("POST", "/remote-auth", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "1.2.3.4:12345"
	rr := httptest.NewRecorder()
	server.handleRemoteAuthPOST(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 Too Many Requests, got %d", rr.Code)
	}
}
