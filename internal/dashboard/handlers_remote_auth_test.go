package dashboard

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/tunnel"
	"golang.org/x/crypto/bcrypt"
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

func TestRenderPasswordPage_EscapesNonce(t *testing.T) {
	// A malicious nonce with HTML/JS injection
	maliciousNonce := `"><script>alert('xss')</script><input value="`
	output := renderPasswordPage(maliciousNonce, "", 5)

	if strings.Contains(output, "<script>") {
		t.Error("renderPasswordPage must HTML-escape the nonce value")
	}
	if !strings.Contains(output, "&lt;script&gt;") {
		t.Error("expected escaped script tag in output")
	}
}

func TestRenderPasswordPage_EscapesErrorMsg(t *testing.T) {
	maliciousMsg := `<img src=x onerror=alert(1)>`
	output := renderPasswordPage("", maliciousMsg, 0)

	if strings.Contains(output, "<img") {
		t.Error("renderPasswordPage must HTML-escape error messages")
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
	enabled := true
	server.config.RemoteAccess = &config.RemoteAccessConfig{Enabled: &enabled}
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

func TestHandleRemoteAccessSetPassword_RejectsShortPassword(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()

	tests := []struct {
		name     string
		password string
	}{
		{"empty", ""},
		{"too short 3 chars", "abc"},
		{"too short 5 chars", "abcde"},
		{"too short 7 chars", "abcdefg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.NewReader(fmt.Sprintf(`{"password":%q}`, tt.password))
			req, _ := http.NewRequest("POST", "/api/remote-access/set-password", body)
			req.Header.Set("Content-Type", "application/json")
			req.RemoteAddr = "127.0.0.1:12345"

			rr := httptest.NewRecorder()
			server.handleRemoteAccessSetPassword(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for password %q, got %d", tt.password, rr.Code)
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

	// Create a nonce directly (simulates token exchange)
	nonce := "rate-limit-test-nonce"
	server.remoteTokenMu.Lock()
	server.remoteNonces[nonce] = &remoteNonce{createdAt: time.Now()}
	server.remoteTokenMu.Unlock()

	// The rate limiter allows 5 requests per minute per IP
	// Send 6 POST requests from the same IP — 6th should be rate-limited
	for i := 0; i < 5; i++ {
		body := strings.NewReader("nonce=" + nonce + "&password=wrong")
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
	body := strings.NewReader("nonce=" + nonce + "&pin=wrong")
	req, _ := http.NewRequest("POST", "/remote-auth", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "1.2.3.4:12345"
	rr := httptest.NewRecorder()
	server.handleRemoteAuthPOST(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 Too Many Requests, got %d", rr.Code)
	}
}

func TestRequireAuthOrRedirect_DoesNotLeakToken(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	enabled := true
	server.config.RemoteAccess = &config.RemoteAccessConfig{Enabled: &enabled}
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// Unauthenticated request to /
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	rr := httptest.NewRecorder()

	result := server.requireAuthOrRedirect(rr, req)
	if result {
		t.Fatal("expected requireAuthOrRedirect to return false (redirect)")
	}

	// Should redirect to /remote-auth WITHOUT a token query param
	loc := rr.Header().Get("Location")
	if loc == "" {
		t.Fatal("expected Location header on redirect")
	}
	if strings.Contains(loc, "token=") {
		t.Errorf("redirect URL must NOT contain token, got: %s", loc)
	}
	if loc != "/remote-auth" {
		t.Errorf("expected redirect to /remote-auth, got: %s", loc)
	}
}

func TestRemoteAuthGET_NoTokenOrNonce_ShowsInstructions(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// GET /remote-auth with no query params
	req, _ := http.NewRequest("GET", "/remote-auth", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	rr := httptest.NewRecorder()

	server.handleRemoteAuthGET(rr, req)

	body := rr.Body.String()
	// Should show instructions, not a PIN form or error
	if strings.Contains(body, "Invalid or expired") {
		t.Error("should not show error message when no token/nonce provided")
	}
	if strings.Contains(body, `name="password"`) {
		t.Error("should not show password form when no token/nonce provided")
	}
	if !strings.Contains(body, "notification") {
		t.Error("should show instructions mentioning notification app")
	}
}

func TestRemoteAuthGET_TokenConsumedAndRedirectsToNonce(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	server.remoteTokenMu.Lock()
	token := server.remoteToken
	server.remoteTokenMu.Unlock()

	// GET with valid token
	req, _ := http.NewRequest("GET", "/remote-auth?token="+token, nil)
	rr := httptest.NewRecorder()
	server.handleRemoteAuthGET(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", rr.Code)
	}

	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "/remote-auth?nonce=") {
		t.Errorf("expected redirect to /remote-auth?nonce=..., got: %s", loc)
	}

	// Token should be consumed
	server.remoteTokenMu.Lock()
	remaining := server.remoteToken
	server.remoteTokenMu.Unlock()
	if remaining != "" {
		t.Error("token should be consumed (empty) after first use")
	}
}

func TestRemoteAuthGET_NonceShowsPasswordForm(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	server.remoteTokenMu.Lock()
	token := server.remoteToken
	server.remoteTokenMu.Unlock()

	// Exchange token for nonce
	req, _ := http.NewRequest("GET", "/remote-auth?token="+token, nil)
	rr := httptest.NewRecorder()
	server.handleRemoteAuthGET(rr, req)

	loc := rr.Header().Get("Location")
	nonce := strings.TrimPrefix(loc, "/remote-auth?nonce=")

	// GET with valid nonce should show password form
	req2, _ := http.NewRequest("GET", "/remote-auth?nonce="+nonce, nil)
	rr2 := httptest.NewRecorder()
	server.handleRemoteAuthGET(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr2.Code)
	}

	body := rr2.Body.String()
	if !strings.Contains(body, `name="password"`) {
		t.Error("expected password form in response")
	}
	if !strings.Contains(body, `name="nonce"`) {
		t.Error("expected hidden nonce field in response")
	}
}

func TestRemoteAuthGET_ExpiredNonceRejected(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// Create an expired nonce manually
	nonce := "expired-nonce-test"
	server.remoteTokenMu.Lock()
	server.remoteNonces[nonce] = &remoteNonce{
		createdAt: time.Now().Add(-6 * time.Minute),
	}
	server.remoteTokenMu.Unlock()

	req, _ := http.NewRequest("GET", "/remote-auth?nonce="+nonce, nil)
	rr := httptest.NewRecorder()
	server.handleRemoteAuthGET(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "Invalid or expired") {
		t.Error("expected 'Invalid or expired' message for expired nonce")
	}
}

func TestRemoteAuthPOST_WorksWithNonce(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// Set a password
	pinHash, _ := bcrypt.GenerateFromPassword([]byte("testpin123"), bcrypt.DefaultCost)
	server.config.RemoteAccess = &config.RemoteAccessConfig{PasswordHash: string(pinHash)}

	server.remoteTokenMu.Lock()
	token := server.remoteToken
	server.remoteTokenMu.Unlock()

	// Exchange token for nonce
	req, _ := http.NewRequest("GET", "/remote-auth?token="+token, nil)
	rr := httptest.NewRecorder()
	server.handleRemoteAuthGET(rr, req)

	loc := rr.Header().Get("Location")
	nonce := strings.TrimPrefix(loc, "/remote-auth?nonce=")

	// POST with valid nonce + correct password
	body := strings.NewReader("nonce=" + nonce + "&password=testpin123")
	req2, _ := http.NewRequest("POST", "/remote-auth", body)
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.RemoteAddr = "1.2.3.4:12345"
	rr2 := httptest.NewRecorder()
	server.handleRemoteAuthPOST(rr2, req2)

	if rr2.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect after successful auth, got %d", rr2.Code)
	}
	if rr2.Header().Get("Location") != "/" {
		t.Errorf("expected redirect to /, got: %s", rr2.Header().Get("Location"))
	}

	// Check that a session cookie was set
	cookies := rr2.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "schmux_remote" {
			found = true
		}
	}
	if !found {
		t.Error("expected schmux_remote cookie to be set")
	}
}

func TestRemoteAuthGET_ReplayedTokenRejected(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	server.remoteTokenMu.Lock()
	token := server.remoteToken
	server.remoteTokenMu.Unlock()

	// First use — should succeed (302 redirect to nonce)
	req, _ := http.NewRequest("GET", "/remote-auth?token="+token, nil)
	rr := httptest.NewRecorder()
	server.handleRemoteAuthGET(rr, req)
	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302 on first use, got %d", rr.Code)
	}

	// Second use — token consumed, should show error
	req2, _ := http.NewRequest("GET", "/remote-auth?token="+token, nil)
	rr2 := httptest.NewRecorder()
	server.handleRemoteAuthGET(rr2, req2)

	body := rr2.Body.String()
	if !strings.Contains(body, "Invalid or expired") {
		t.Error("replayed token should show 'Invalid or expired' message")
	}
}

func TestIsAllowedOrigin_RestrictedWhenTunnelActive(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()

	// Enable network access (bind to 0.0.0.0)
	server.config.Network = &config.NetworkConfig{
		Port:        7337,
		BindAddress: "0.0.0.0",
	}

	// Before tunnel: network_access=true should allow any origin
	if !server.isAllowedOrigin("http://evil.com") {
		t.Error("should allow any origin when no tunnel and network_access=true")
	}

	// Connect tunnel
	server.HandleTunnelConnected("https://abc-xyz.trycloudflare.com")

	// Tunnel origin should be allowed
	if !server.isAllowedOrigin("https://abc-xyz.trycloudflare.com") {
		t.Error("should allow tunnel origin when tunnel is active")
	}

	// Localhost should be allowed
	if !server.isAllowedOrigin("http://localhost:7337") {
		t.Error("should allow localhost when tunnel is active")
	}

	// Arbitrary origin should be REJECTED even with network_access=true
	if server.isAllowedOrigin("http://evil.com") {
		t.Error("should reject arbitrary origins when tunnel is active, even with network_access=true")
	}

	// After tunnel stops, should revert
	server.ClearRemoteAuth()
	if !server.isAllowedOrigin("http://evil.com") {
		t.Error("should allow any origin again after tunnel stops with network_access=true")
	}
}

func TestIsTrustedRequest_NoTunnel_LoopbackIsLocal(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	enabled := true
	server.config.RemoteAccess = &config.RemoteAccessConfig{Enabled: &enabled}

	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"

	if !server.isTrustedRequest(req) {
		t.Error("loopback request should be trusted when no tunnel is active")
	}
}

func TestIsTrustedRequest_TunnelActive_LoopbackWithCfHeader_IsNotLocal(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	enabled := true
	server.config.RemoteAccess = &config.RemoteAccessConfig{Enabled: &enabled}
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Cf-Connecting-IP", "1.2.3.4")

	if server.isTrustedRequest(req) {
		t.Error("loopback + Cf-Connecting-IP + tunnel should NOT be trusted")
	}
}

func TestIsTrustedRequest_TunnelActive_LoopbackNoCfHeader_IsLocal(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	enabled := true
	server.config.RemoteAccess = &config.RemoteAccessConfig{Enabled: &enabled}
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	// No Cf-Connecting-IP or X-Forwarded-For headers

	if !server.isTrustedRequest(req) {
		t.Error("loopback without forwarding headers should be trusted even with tunnel active")
	}
}

func TestIsTrustedRequest_RemoteAccessDisabled_AlwaysTrusted(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	// remote_access not enabled — no untrusted path exists

	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.50:12345"

	if !server.isTrustedRequest(req) {
		t.Error("all requests should be trusted when remote_access is disabled")
	}
}

func TestCSRF_RequiredForTunneledRequests(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	enabled := true
	server.config.RemoteAccess = &config.RemoteAccessConfig{Enabled: &enabled}
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// Build a valid remote cookie
	nowStr := fmt.Sprintf("%d", time.Now().Unix())
	server.remoteTokenMu.Lock()
	secret := server.remoteSessionSecret
	server.remoteTokenMu.Unlock()
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(nowStr))
	sig := hex.EncodeToString(mac.Sum(nil))

	// POST from 127.0.0.1 with Cf-Connecting-IP (tunneled), no CSRF token
	req, _ := http.NewRequest("POST", "/api/remote-access/off", nil)
	req.AddCookie(&http.Cookie{Name: "schmux_remote", Value: nowStr + "." + sig})
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Cf-Connecting-IP", "1.2.3.4")

	rr := httptest.NewRecorder()
	handler := server.withAuthAndCSRF(server.handleRemoteAccessOff)
	handler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for tunneled request without CSRF, got %d", rr.Code)
	}
}

func TestNormalizeIPForRateLimit_TunnelActive_UsesCfConnectingIP(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	req, _ := http.NewRequest("POST", "/remote-auth", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Cf-Connecting-IP", "203.0.113.42")

	ip := server.normalizeIPForRateLimit(req)
	if ip != "203.0.113.42" {
		t.Errorf("expected Cf-Connecting-IP value, got %q", ip)
	}
}

func TestNormalizeIPForRateLimit_NoTunnel_IgnoresHeaders(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	// No tunnel connected

	req, _ := http.NewRequest("POST", "/remote-auth", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Cf-Connecting-IP", "203.0.113.42")

	ip := server.normalizeIPForRateLimit(req)
	if ip != "127.0.0.1" {
		t.Errorf("expected RemoteAddr IP without tunnel, got %q", ip)
	}
}

func TestSetPassword_WithoutTunnel_DoesNotActivateAuth(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()

	// Set up config with a saveable path — NO tunnel connected
	configPath := filepath.Join(t.TempDir(), "config.json")
	server.config = config.CreateDefault(configPath)

	// Verify auth is not required before setting password
	if server.requiresAuth() {
		t.Fatal("auth should not be required before setting password")
	}

	// Set a password (no tunnel active)
	body := strings.NewReader(`{"password":"mypassword123"}`)
	req, _ := http.NewRequest("POST", "/api/remote-access/set-password", body)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	server.handleRemoteAccessSetPassword(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Auth should still NOT be required — no tunnel is active
	if server.requiresAuth() {
		t.Error("setting password without active tunnel should not activate auth")
	}

	// Local API requests should still work without cookies
	req2, _ := http.NewRequest("GET", "/api/config", nil)
	req2.RemoteAddr = "127.0.0.1:12345"
	rr2 := httptest.NewRecorder()
	handler := server.withAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Errorf("local request should succeed without auth after setting password (no tunnel), got %d", rr2.Code)
	}
}

func TestLocalRequestBypassesAuth_WhenTunnelActive(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	enabled := true
	server.config.RemoteAccess = &config.RemoteAccessConfig{Enabled: &enabled}
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// Auth is required (tunnel active)
	if !server.requiresAuth() {
		t.Fatal("auth should be required when tunnel is active")
	}

	// Local request without any cookies should still succeed
	req, _ := http.NewRequest("GET", "/api/config", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	handler := server.withAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("local request should bypass tunnel-only auth, got %d", rr.Code)
	}

	// Remote request without cookies should fail
	req2, _ := http.NewRequest("GET", "/api/config", nil)
	req2.RemoteAddr = "127.0.0.1:12345"
	req2.Header.Set("Cf-Connecting-IP", "1.2.3.4") // tunneled
	rr2 := httptest.NewRecorder()
	handler(rr2, req2)

	if rr2.Code != http.StatusUnauthorized {
		t.Errorf("tunneled request without auth should get 401, got %d", rr2.Code)
	}
}

func TestRequireAuthOrRedirect_LocalBypassesTunnelAuth(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()
	enabled := true
	server.config.RemoteAccess = &config.RemoteAccessConfig{Enabled: &enabled}
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// Local request — should pass through without redirect
	req, _ := http.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()

	result := server.requireAuthOrRedirect(rr, req)
	if !result {
		t.Error("local request should pass requireAuthOrRedirect when only tunnel auth is active")
	}

	// Remote request — should redirect
	req2, _ := http.NewRequest("GET", "/", nil)
	req2.RemoteAddr = "1.2.3.4:12345"
	rr2 := httptest.NewRecorder()

	result2 := server.requireAuthOrRedirect(rr2, req2)
	if result2 {
		t.Error("remote request without auth should be redirected")
	}
}

func TestSetPassword_InvalidatesExistingSessions(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}))
	defer server.CloseForTest()

	// Set up config with a saveable path
	configPath := filepath.Join(t.TempDir(), "config.json")
	server.config = config.CreateDefault(configPath)

	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// Get session secret and create a valid cookie
	nowStr := fmt.Sprintf("%d", time.Now().Unix())
	server.remoteTokenMu.Lock()
	secret := server.remoteSessionSecret
	server.remoteTokenMu.Unlock()

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(nowStr))
	sig := hex.EncodeToString(mac.Sum(nil))
	cookieValue := nowStr + "." + sig

	// Verify cookie is valid
	if !server.validateRemoteCookie(cookieValue) {
		t.Fatal("cookie should be valid before password change")
	}

	// Change password
	body := strings.NewReader(`{"password":"newpin123"}`)
	req, _ := http.NewRequest("POST", "/api/remote-access/set-password", body)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"
	rr := httptest.NewRecorder()
	server.handleRemoteAccessSetPassword(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify old cookie is now invalid
	if server.validateRemoteCookie(cookieValue) {
		t.Error("old cookie should be invalid after password change")
	}
}
