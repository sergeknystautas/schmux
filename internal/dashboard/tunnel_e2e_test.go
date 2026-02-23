package dashboard

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/tunnel"
	"golang.org/x/crypto/bcrypt"
)

// tunnelTestServer wraps a *Server with an httptest.Server running the full
// route mux. This simulates the real HTTP stack including middleware (CORS,
// auth, CSRF) so we can test end-to-end behavior as cloudflared would see it.
type tunnelTestServer struct {
	server     *Server
	httpServer *httptest.Server
}

// newTunnelTestServer creates a test server with the full middleware chain
// wired up the same way as production Start(). The password is pre-set and
// remote access is enabled in config.
func newTunnelTestServer(t *testing.T, password string) *tunnelTestServer {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = t.TempDir()

	// Hash the password and configure remote access
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	enabled := true
	cfg.RemoteAccess = &config.RemoteAccessConfig{
		Enabled:      &enabled,
		PasswordHash: string(hash),
	}
	cfg.Network = &config.NetworkConfig{Port: 7337}
	if err := cfg.Save(); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	s := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}, nil))
	s.config = cfg

	// Build chi router matching production Start() route registration
	r := chi.NewRouter()

	// App handler (serves SPA, redirects unauthenticated to /remote-auth)
	r.HandleFunc("/*", s.handleApp)

	// Remote auth (unauthenticated — token/nonce/password protected)
	r.Get("/remote-auth", s.handleRemoteAuthGET)
	r.Post("/remote-auth", s.handleRemoteAuthPOST)

	// WebSocket endpoints
	r.HandleFunc("/ws/dashboard", s.handleDashboardWebSocket)

	// API routes with CORS + auth middleware
	r.Route("/api", func(r chi.Router) {
		r.Use(s.corsMiddleware)
		r.Use(s.authMiddleware)

		r.Get("/healthz", s.handleHealthz)
		r.Get("/sessions", s.handleSessions)
		r.Get("/remote-access/status", s.handleRemoteAccessStatus)

		r.Group(func(r chi.Router) {
			r.Use(s.csrfMiddleware)

			r.Post("/spawn", s.handleSpawnPost)
			r.Post("/remote-access/on", s.handleRemoteAccessOn)
			r.Post("/remote-access/off", s.handleRemoteAccessOff)
			r.Post("/remote-access/set-password", s.handleRemoteAccessSetPassword)
			r.Get("/config", s.handleConfigGet)
			r.Put("/config", s.handleConfigUpdate)
			r.Post("/config", s.handleConfigUpdate)
		})
	})

	ts := httptest.NewServer(r)
	t.Cleanup(func() {
		ts.Close()
		s.CloseForTest()
	})

	return &tunnelTestServer{server: s, httpServer: ts}
}

// simulateTunnelConnect simulates what the daemon does when cloudflared
// connects: generates a token, sets the session secret, and returns the
// auth URL that would be sent via ntfy.
func (tts *tunnelTestServer) simulateTunnelConnect(t *testing.T) string {
	t.Helper()
	tunnelURL := "https://test-tunnel.trycloudflare.com"
	tts.server.HandleTunnelConnected(tunnelURL)
	tts.server.remoteTokenMu.Lock()
	token := tts.server.remoteToken
	tts.server.remoteTokenMu.Unlock()
	return tunnelURL + "/remote-auth?token=" + token
}

// makeRemoteCookie creates a valid remote session cookie value for testing.
func (tts *tunnelTestServer) makeRemoteCookie(t *testing.T) string {
	t.Helper()
	nowStr := fmt.Sprintf("%d", time.Now().Unix())
	tts.server.remoteTokenMu.Lock()
	secret := tts.server.remoteSessionSecret
	tts.server.remoteTokenMu.Unlock()

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(nowStr))
	sig := hex.EncodeToString(mac.Sum(nil))
	return nowStr + "." + sig
}

// tunneledRequest creates an HTTP request that simulates being proxied
// through cloudflared: RemoteAddr is loopback, Cf-Connecting-IP is the
// remote client's real IP.
func tunneledRequest(method, path string, body *strings.Reader) *http.Request {
	var req *http.Request
	if body != nil {
		req, _ = http.NewRequest(method, path, body)
	} else {
		req, _ = http.NewRequest(method, path, nil)
	}
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("Cf-Connecting-IP", "203.0.113.50")
	req.Header.Set("X-Forwarded-For", "203.0.113.50")
	return req
}

// localRequest creates an HTTP request that simulates a genuine local
// browser connection: RemoteAddr is loopback, no forwarding headers.
func localRequest(method, path string) *http.Request {
	req, _ := http.NewRequest(method, path, nil)
	req.RemoteAddr = "127.0.0.1:54321"
	return req
}

// --- Category 1: Full Auth Flow End-to-End ---

func TestTunnelE2E_FullAuthLifecycle(t *testing.T) {
	password := "test-secure-pw-123"
	tts := newTunnelTestServer(t, password)

	// Step 1: Tunnel connects, generates token
	tts.simulateTunnelConnect(t)

	tts.server.remoteTokenMu.Lock()
	token := tts.server.remoteToken
	tts.server.remoteTokenMu.Unlock()
	if token == "" {
		t.Fatal("expected token to be generated on tunnel connect")
	}

	// Step 2: Remote client clicks auth URL → token consumed, nonce issued
	req := tunneledRequest("GET", "/remote-auth?token="+token, nil)
	rr := httptest.NewRecorder()
	tts.server.handleRemoteAuthGET(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("token exchange: expected 302, got %d", rr.Code)
	}
	loc := rr.Header().Get("Location")
	if !strings.HasPrefix(loc, "/remote-auth?nonce=") {
		t.Fatalf("expected redirect to nonce URL, got: %s", loc)
	}
	nonce := strings.TrimPrefix(loc, "/remote-auth?nonce=")

	// Step 3: Token is consumed — replay fails
	req2 := tunneledRequest("GET", "/remote-auth?token="+token, nil)
	rr2 := httptest.NewRecorder()
	tts.server.handleRemoteAuthGET(rr2, req2)
	if strings.Contains(rr2.Body.String(), `name="password"`) {
		t.Fatal("replayed token should not show password form")
	}

	// Step 4: Nonce shows password form
	req3 := tunneledRequest("GET", "/remote-auth?nonce="+nonce, nil)
	rr3 := httptest.NewRecorder()
	tts.server.handleRemoteAuthGET(rr3, req3)
	if !strings.Contains(rr3.Body.String(), `name="password"`) {
		t.Fatal("valid nonce should show password form")
	}

	// Step 5: Correct password → session cookie issued
	body := strings.NewReader("nonce=" + nonce + "&password=" + password)
	req4 := tunneledRequest("POST", "/remote-auth", body)
	req4.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr4 := httptest.NewRecorder()
	tts.server.handleRemoteAuthPOST(rr4, req4)

	if rr4.Code != http.StatusFound {
		t.Fatalf("password submit: expected 302, got %d: %s", rr4.Code, rr4.Body.String())
	}

	var sessionCookie, csrfCookie string
	for _, c := range rr4.Result().Cookies() {
		if c.Name == "schmux_remote" {
			sessionCookie = c.Value
			if !c.HttpOnly {
				t.Error("session cookie should be HttpOnly")
			}
			if !c.Secure {
				t.Error("session cookie should be Secure")
			}
		}
		if c.Name == csrfCookieName {
			csrfCookie = c.Value
		}
	}
	if sessionCookie == "" {
		t.Fatal("expected schmux_remote cookie after successful auth")
	}
	if csrfCookie == "" {
		t.Fatal("expected CSRF cookie after successful auth")
	}

	// Step 6: Authenticated API request succeeds
	req5 := tunneledRequest("GET", "/api/remote-access/status", nil)
	req5.AddCookie(&http.Cookie{Name: "schmux_remote", Value: sessionCookie})
	rr5 := httptest.NewRecorder()
	handler := tts.server.withCORS(tts.server.withAuth(tts.server.handleRemoteAccessStatus))
	handler(rr5, req5)

	if rr5.Code != http.StatusOK {
		t.Errorf("authenticated request: expected 200, got %d", rr5.Code)
	}

	// Step 7: Tunnel stops → cookie invalidated
	tts.server.ClearRemoteAuth()

	req6 := tunneledRequest("GET", "/api/remote-access/status", nil)
	req6.AddCookie(&http.Cookie{Name: "schmux_remote", Value: sessionCookie})
	rr6 := httptest.NewRecorder()
	handler(rr6, req6)

	// Auth is no longer required (no tunnel), so request should succeed
	// even without a valid cookie — this is correct behavior.
	if tts.server.requiresAuth() {
		t.Error("auth should not be required after tunnel stops")
	}

	// Step 8: Re-start tunnel → old cookie is invalid
	tts.simulateTunnelConnect(t)
	if !tts.server.requiresAuth() {
		t.Fatal("auth should be required when tunnel re-starts")
	}

	req7 := tunneledRequest("GET", "/api/healthz", nil)
	req7.AddCookie(&http.Cookie{Name: "schmux_remote", Value: sessionCookie})
	rr7 := httptest.NewRecorder()
	healthHandler := tts.server.withAuth(tts.server.handleHealthz)
	healthHandler(rr7, req7)

	if rr7.Code != http.StatusUnauthorized {
		t.Errorf("old cookie after tunnel restart: expected 401, got %d", rr7.Code)
	}
}

// --- Category 2: Attack Simulation ---

func TestTunnelE2E_Attack_DirectAPIAccessWithoutAuth(t *testing.T) {
	tts := newTunnelTestServer(t, "secure-password-123")
	tts.simulateTunnelConnect(t)

	endpoints := []struct {
		method string
		path   string
	}{
		{"GET", "/api/healthz"},
		{"GET", "/api/config"},
		{"GET", "/api/sessions"},
		{"GET", "/api/remote-access/status"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			req := tunneledRequest(ep.method, ep.path, nil)
			rr := httptest.NewRecorder()
			handler := tts.server.withAuth(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			handler(rr, req)

			if rr.Code != http.StatusUnauthorized {
				t.Errorf("unauthenticated tunneled request to %s: expected 401, got %d", ep.path, rr.Code)
			}
		})
	}
}

func TestTunnelE2E_Attack_ForgedCfHeaderWithoutTunnel(t *testing.T) {
	tts := newTunnelTestServer(t, "secure-password-123")
	// No tunnel connected — forged Cf-Connecting-IP should be ignored

	req := localRequest("GET", "/api/healthz")
	req.Header.Set("Cf-Connecting-IP", "1.2.3.4")

	// Without a tunnel, auth is not required at all, so the forged
	// header should not cause any issues.
	if tts.server.requiresAuth() {
		t.Fatal("auth should not be required without tunnel")
	}

	// isTrustedRequest should return true even with forged header when no tunnel
	if !tts.server.isTrustedRequest(req) {
		t.Error("forged Cf-Connecting-IP should be ignored when no tunnel is active")
	}
}

func TestTunnelE2E_Attack_ForgedCfHeaderWithTunnel(t *testing.T) {
	tts := newTunnelTestServer(t, "secure-password-123")
	tts.simulateTunnelConnect(t)

	// Request from loopback with Cf-Connecting-IP — should be treated as remote
	req := localRequest("GET", "/api/healthz")
	req.Header.Set("Cf-Connecting-IP", "1.2.3.4")

	if tts.server.isTrustedRequest(req) {
		t.Error("request with Cf-Connecting-IP should NOT be local when tunnel is active")
	}

	// Should require auth and fail without cookie
	rr := httptest.NewRecorder()
	handler := tts.server.withAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("forged header request: expected 401, got %d", rr.Code)
	}
}

func TestTunnelE2E_CSRFProtectedEndpoint_SucceedsWithValidCSRF(t *testing.T) {
	tts := newTunnelTestServer(t, "secure-password-123")
	tts.simulateTunnelConnect(t)

	cookie := tts.makeRemoteCookie(t)
	csrfToken := "test-csrf-token-value"

	// POST from tunnel origin with valid cookie AND valid CSRF token
	req := tunneledRequest("POST", "/api/remote-access/off", nil)
	req.AddCookie(&http.Cookie{Name: "schmux_remote", Value: cookie})
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfToken})
	req.Header.Set("X-CSRF-Token", csrfToken)
	req.Header.Set("Origin", "https://test-tunnel.trycloudflare.com")

	rr := httptest.NewRecorder()
	handler := tts.server.withCORS(tts.server.withAuthAndCSRF(tts.server.handleRemoteAccessOff))
	handler(rr, req)

	if rr.Code == http.StatusForbidden {
		t.Errorf("request with valid CSRF token should not be forbidden, got %d", rr.Code)
	}
	if rr.Code == http.StatusUnauthorized {
		t.Errorf("request with valid session cookie should not be unauthorized, got %d", rr.Code)
	}
	// Should succeed (200 OK from handleRemoteAccessOff)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestTunnelE2E_Attack_CSRFFromMaliciousOrigin(t *testing.T) {
	tts := newTunnelTestServer(t, "secure-password-123")
	tts.simulateTunnelConnect(t)

	cookie := tts.makeRemoteCookie(t)

	// POST from malicious origin with valid cookie but no CSRF token
	req := tunneledRequest("POST", "/api/remote-access/off", nil)
	req.AddCookie(&http.Cookie{Name: "schmux_remote", Value: cookie})
	req.Header.Set("Origin", "https://evil.com")

	rr := httptest.NewRecorder()
	handler := tts.server.withCORS(tts.server.withAuthAndCSRF(tts.server.handleRemoteAccessOff))
	handler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("CSRF from malicious origin: expected 403, got %d", rr.Code)
	}
}

func TestTunnelE2E_Attack_CORSFromMaliciousOrigin(t *testing.T) {
	tts := newTunnelTestServer(t, "secure-password-123")
	tts.simulateTunnelConnect(t)

	// GET with malicious origin should be rejected by CORS middleware
	req := tunneledRequest("GET", "/api/healthz", nil)
	req.Header.Set("Origin", "https://evil.com")

	rr := httptest.NewRecorder()
	handler := tts.server.withCORS(tts.server.withAuth(tts.server.handleHealthz))
	handler(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("malicious origin: expected 403, got %d", rr.Code)
	}
}

func TestTunnelE2E_Attack_CORSFromTunnelOriginAllowed(t *testing.T) {
	tts := newTunnelTestServer(t, "secure-password-123")
	tts.simulateTunnelConnect(t)

	cookie := tts.makeRemoteCookie(t)

	req := tunneledRequest("GET", "/api/remote-access/status", nil)
	req.Header.Set("Origin", "https://test-tunnel.trycloudflare.com")
	req.AddCookie(&http.Cookie{Name: "schmux_remote", Value: cookie})

	rr := httptest.NewRecorder()
	handler := tts.server.withCORS(tts.server.withAuth(tts.server.handleRemoteAccessStatus))
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("tunnel origin with valid cookie: expected 200, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "https://test-tunnel.trycloudflare.com" {
		t.Errorf("expected CORS origin to be tunnel URL, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestTunnelE2E_Attack_NonceReuseAfterSuccess(t *testing.T) {
	tts := newTunnelTestServer(t, "test-password-42")
	tts.simulateTunnelConnect(t)

	// Exchange token for nonce
	tts.server.remoteTokenMu.Lock()
	token := tts.server.remoteToken
	tts.server.remoteTokenMu.Unlock()

	req := tunneledRequest("GET", "/remote-auth?token="+token, nil)
	rr := httptest.NewRecorder()
	tts.server.handleRemoteAuthGET(rr, req)
	nonce := strings.TrimPrefix(rr.Header().Get("Location"), "/remote-auth?nonce=")

	// First password submission — success
	body := strings.NewReader("nonce=" + nonce + "&password=test-password-42")
	req2 := tunneledRequest("POST", "/remote-auth", body)
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr2 := httptest.NewRecorder()
	tts.server.handleRemoteAuthPOST(rr2, req2)
	if rr2.Code != http.StatusFound {
		t.Fatalf("first auth: expected 302, got %d", rr2.Code)
	}

	// Second submission with same nonce — should fail (nonce consumed)
	body2 := strings.NewReader("nonce=" + nonce + "&password=test-password-42")
	req3 := tunneledRequest("POST", "/remote-auth", body2)
	req3.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr3 := httptest.NewRecorder()
	tts.server.handleRemoteAuthPOST(rr3, req3)

	if rr3.Code == http.StatusFound {
		t.Error("nonce replay should not succeed — nonce should be consumed after first use")
	}
	if !strings.Contains(rr3.Body.String(), "Invalid or expired") {
		t.Error("expected 'Invalid or expired' message for replayed nonce")
	}
}

func TestTunnelE2E_Attack_CookieFromDifferentTunnelSession(t *testing.T) {
	tts := newTunnelTestServer(t, "secure-password-123")

	// First tunnel session
	tts.simulateTunnelConnect(t)
	cookie1 := tts.makeRemoteCookie(t)

	// Verify cookie works
	if !tts.server.validateRemoteCookie(cookie1) {
		t.Fatal("cookie should be valid during first tunnel session")
	}

	// Stop tunnel, start a new one (different session secret)
	tts.server.ClearRemoteAuth()
	tts.simulateTunnelConnect(t)

	// Old cookie from previous tunnel session should be rejected
	if tts.server.validateRemoteCookie(cookie1) {
		t.Error("cookie from previous tunnel session should be rejected after restart")
	}

	// New cookie should work
	cookie2 := tts.makeRemoteCookie(t)
	if !tts.server.validateRemoteCookie(cookie2) {
		t.Error("new cookie should be valid after tunnel restart")
	}
}

func TestTunnelE2E_Attack_PasswordBruteForce(t *testing.T) {
	tts := newTunnelTestServer(t, "correct-password")
	tts.simulateTunnelConnect(t)

	// Exchange token for nonce
	tts.server.remoteTokenMu.Lock()
	token := tts.server.remoteToken
	tts.server.remoteTokenMu.Unlock()

	req := tunneledRequest("GET", "/remote-auth?token="+token, nil)
	rr := httptest.NewRecorder()
	tts.server.handleRemoteAuthGET(rr, req)
	nonce := strings.TrimPrefix(rr.Header().Get("Location"), "/remote-auth?nonce=")

	// Exhaust password attempts (maxPasswordAttempts = 5)
	for i := 0; i < maxPasswordAttempts; i++ {
		body := strings.NewReader("nonce=" + nonce + "&password=wrong-guess-" + fmt.Sprintf("%d", i))
		req := tunneledRequest("POST", "/remote-auth", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		tts.server.handleRemoteAuthPOST(rr, req)
	}

	// Now try the correct password — should be locked out
	body := strings.NewReader("nonce=" + nonce + "&password=correct-password")
	req2 := tunneledRequest("POST", "/remote-auth", body)
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr2 := httptest.NewRecorder()
	tts.server.handleRemoteAuthPOST(rr2, req2)

	if rr2.Code == http.StatusFound {
		t.Error("correct password after lockout should still be rejected")
	}
	if !strings.Contains(rr2.Body.String(), "Too many attempts") {
		t.Errorf("expected lockout message, got: %s", rr2.Body.String())
	}
}

func TestTunnelE2E_Attack_RateLimitByIP(t *testing.T) {
	tts := newTunnelTestServer(t, "test-password")
	tts.simulateTunnelConnect(t)

	// Create a nonce to use
	nonce := "rate-limit-e2e-nonce"
	tts.server.remoteTokenMu.Lock()
	tts.server.remoteNonces[nonce] = &remoteNonce{createdAt: time.Now()}
	tts.server.remoteTokenMu.Unlock()

	// Same attacker IP (via Cf-Connecting-IP) should be rate-limited
	for i := 0; i < 5; i++ {
		body := strings.NewReader("nonce=" + nonce + "&password=wrong")
		req := tunneledRequest("POST", "/remote-auth", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		tts.server.handleRemoteAuthPOST(rr, req)
	}

	// 6th attempt from same IP should hit rate limit
	body := strings.NewReader("nonce=" + nonce + "&password=wrong")
	req := tunneledRequest("POST", "/remote-auth", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	tts.server.handleRemoteAuthPOST(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after rate limit exceeded, got %d", rr.Code)
	}
}

func TestTunnelE2E_Attack_RateLimitDifferentIPsNotAffected(t *testing.T) {
	tts := newTunnelTestServer(t, "test-password")
	tts.simulateTunnelConnect(t)

	// Create a nonce
	nonce := "rate-limit-multi-ip-nonce"
	tts.server.remoteTokenMu.Lock()
	tts.server.remoteNonces[nonce] = &remoteNonce{createdAt: time.Now()}
	tts.server.remoteTokenMu.Unlock()

	// Exhaust rate limit from IP A
	for i := 0; i < 5; i++ {
		body := strings.NewReader("nonce=" + nonce + "&password=wrong")
		req, _ := http.NewRequest("POST", "/remote-auth", body)
		req.RemoteAddr = "127.0.0.1:54321"
		req.Header.Set("Cf-Connecting-IP", "10.0.0.1")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		tts.server.handleRemoteAuthPOST(rr, req)
	}

	// IP B should still be allowed
	body := strings.NewReader("nonce=" + nonce + "&password=wrong")
	req, _ := http.NewRequest("POST", "/remote-auth", body)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("Cf-Connecting-IP", "10.0.0.2")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	tts.server.handleRemoteAuthPOST(rr, req)

	if rr.Code == http.StatusTooManyRequests {
		t.Error("different IP should not be affected by another IP's rate limit")
	}
}

// --- Category 3: WebSocket Auth via Tunnel ---

func TestTunnelE2E_WebSocket_DashboardRejectsUnauthTunneledRequest(t *testing.T) {
	tts := newTunnelTestServer(t, "ws-test-password")
	tts.simulateTunnelConnect(t)

	// Parse the test server URL into a WebSocket URL
	wsURL := "ws" + strings.TrimPrefix(tts.httpServer.URL, "http") + "/ws/dashboard"

	// Dial WITHOUT cookies — simulates unauthenticated tunneled access.
	// The request will come from localhost (httptest), so we need to add
	// Cf-Connecting-IP header to simulate tunnel.
	dialer := websocket.Dialer{
		HandshakeTimeout: 2 * time.Second,
	}
	header := http.Header{}
	header.Set("Cf-Connecting-IP", "203.0.113.50")

	_, resp, err := dialer.Dial(wsURL, header)
	if err == nil {
		t.Fatal("WebSocket dial should fail without auth cookie when tunnel is active")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestTunnelE2E_WebSocket_DashboardAcceptsAuthenticatedRequest(t *testing.T) {
	tts := newTunnelTestServer(t, "ws-test-password")
	tts.simulateTunnelConnect(t)

	cookie := tts.makeRemoteCookie(t)

	wsURL := "ws" + strings.TrimPrefix(tts.httpServer.URL, "http") + "/ws/dashboard"

	dialer := websocket.Dialer{
		HandshakeTimeout: 2 * time.Second,
	}
	header := http.Header{}
	header.Set("Cf-Connecting-IP", "203.0.113.50")

	// Construct a cookie header manually
	parsedURL, _ := url.Parse(tts.httpServer.URL)
	dialer.Jar = newSingleCookieJar(parsedURL, &http.Cookie{
		Name:  "schmux_remote",
		Value: cookie,
	})

	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("WebSocket dial with valid cookie should succeed: %v", err)
	}
	conn.Close()
}

func TestTunnelE2E_WebSocket_DashboardAllowsLocalWithoutAuth(t *testing.T) {
	tts := newTunnelTestServer(t, "ws-test-password")
	tts.simulateTunnelConnect(t)

	wsURL := "ws" + strings.TrimPrefix(tts.httpServer.URL, "http") + "/ws/dashboard"

	dialer := websocket.Dialer{
		HandshakeTimeout: 2 * time.Second,
	}
	// No Cf-Connecting-IP header — genuine local connection
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("local WebSocket connection should succeed without auth: %v", err)
	}
	conn.Close()
}

// --- Category 4: Concurrent Tunnel and Local Access ---

func TestTunnelE2E_LocalAccessUnaffectedByTunnel(t *testing.T) {
	tts := newTunnelTestServer(t, "local-test-pw-123")

	// Before tunnel: local API works
	req := localRequest("GET", "/api/remote-access/status")
	rr := httptest.NewRecorder()
	handler := tts.server.withAuth(tts.server.handleRemoteAccessStatus)
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("local request before tunnel: expected 200, got %d", rr.Code)
	}

	// Start tunnel
	tts.simulateTunnelConnect(t)

	// Local API still works without any cookie
	req2 := localRequest("GET", "/api/remote-access/status")
	rr2 := httptest.NewRecorder()
	handler(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Errorf("local request during tunnel: expected 200, got %d", rr2.Code)
	}

	// Remote request fails without cookie
	req3 := tunneledRequest("GET", "/api/remote-access/status", nil)
	rr3 := httptest.NewRecorder()
	handler(rr3, req3)

	if rr3.Code != http.StatusUnauthorized {
		t.Errorf("tunneled request without auth during tunnel: expected 401, got %d", rr3.Code)
	}

	// Stop tunnel: local still works
	tts.server.ClearRemoteAuth()

	req4 := localRequest("GET", "/api/remote-access/status")
	rr4 := httptest.NewRecorder()
	handler(rr4, req4)

	if rr4.Code != http.StatusOK {
		t.Errorf("local request after tunnel stops: expected 200, got %d", rr4.Code)
	}
}

func TestTunnelE2E_LocalCSRFExemptDuringTunnel(t *testing.T) {
	tts := newTunnelTestServer(t, "csrf-test-pw")
	tts.simulateTunnelConnect(t)

	// Local POST without CSRF — should succeed (local bypass)
	req := localRequest("POST", "/api/remote-access/off")
	rr := httptest.NewRecorder()
	handler := tts.server.withAuthAndCSRF(tts.server.handleRemoteAccessOff)
	handler(rr, req)

	if rr.Code == http.StatusForbidden {
		t.Error("local POST should not require CSRF token")
	}

	// Tunneled POST without CSRF — should be rejected
	req2 := tunneledRequest("POST", "/api/remote-access/off", nil)
	cookie := tts.makeRemoteCookie(t)
	req2.AddCookie(&http.Cookie{Name: "schmux_remote", Value: cookie})
	rr2 := httptest.NewRecorder()
	handler(rr2, req2)

	if rr2.Code != http.StatusForbidden {
		t.Errorf("tunneled POST without CSRF: expected 403, got %d", rr2.Code)
	}
}

func TestTunnelE2E_LocalAndRemoteConcurrentSessions(t *testing.T) {
	tts := newTunnelTestServer(t, "concurrent-test-pw")
	tts.simulateTunnelConnect(t)

	remoteCookie := tts.makeRemoteCookie(t)

	// Both local and remote should be able to read status simultaneously
	localReq := localRequest("GET", "/api/remote-access/status")
	localRR := httptest.NewRecorder()

	remoteReq := tunneledRequest("GET", "/api/remote-access/status", nil)
	remoteReq.AddCookie(&http.Cookie{Name: "schmux_remote", Value: remoteCookie})
	remoteRR := httptest.NewRecorder()

	handler := tts.server.withAuth(tts.server.handleRemoteAccessStatus)
	handler(localRR, localReq)
	handler(remoteRR, remoteReq)

	if localRR.Code != http.StatusOK {
		t.Errorf("local concurrent request: expected 200, got %d", localRR.Code)
	}
	if remoteRR.Code != http.StatusOK {
		t.Errorf("remote concurrent request: expected 200, got %d", remoteRR.Code)
	}

	// Both should return the same status
	var localStatus, remoteStatus tunnel.TunnelStatus
	json.NewDecoder(localRR.Body).Decode(&localStatus)
	json.NewDecoder(remoteRR.Body).Decode(&remoteStatus)

	if localStatus.State != remoteStatus.State {
		t.Errorf("local and remote should see same state: local=%q remote=%q",
			localStatus.State, remoteStatus.State)
	}
}

func TestTunnelE2E_PasswordChangeRevokesRemoteSessions(t *testing.T) {
	tts := newTunnelTestServer(t, "old-password-123")
	tts.simulateTunnelConnect(t)

	// Get a valid remote cookie
	cookie := tts.makeRemoteCookie(t)

	// Verify it works
	req := tunneledRequest("GET", "/api/remote-access/status", nil)
	req.AddCookie(&http.Cookie{Name: "schmux_remote", Value: cookie})
	rr := httptest.NewRecorder()
	handler := tts.server.withAuth(tts.server.handleRemoteAccessStatus)
	handler(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("cookie should work before password change: got %d", rr.Code)
	}

	// Local user changes password
	body := strings.NewReader(`{"password":"new-password-456"}`)
	setReq := localRequest("POST", "/api/remote-access/set-password")
	setReq.Body = http.MaxBytesReader(nil, setReq.Body, maxBodySize)
	// We have to re-create the body since localRequest doesn't accept a body
	setReq, _ = http.NewRequest("POST", "/api/remote-access/set-password", body)
	setReq.RemoteAddr = "127.0.0.1:54321"
	setReq.Header.Set("Content-Type", "application/json")
	setRR := httptest.NewRecorder()
	tts.server.handleRemoteAccessSetPassword(setRR, setReq)
	if setRR.Code != http.StatusOK {
		t.Fatalf("set password: expected 200, got %d: %s", setRR.Code, setRR.Body.String())
	}

	// Old remote cookie should now be invalid
	req2 := tunneledRequest("GET", "/api/remote-access/status", nil)
	req2.AddCookie(&http.Cookie{Name: "schmux_remote", Value: cookie})
	rr2 := httptest.NewRecorder()
	handler(rr2, req2)
	if rr2.Code != http.StatusUnauthorized {
		t.Errorf("old cookie after password change: expected 401, got %d", rr2.Code)
	}

	// Local user is unaffected
	req3 := localRequest("GET", "/api/remote-access/status")
	rr3 := httptest.NewRecorder()
	handler(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Errorf("local access after password change: expected 200, got %d", rr3.Code)
	}
}

// --- Helpers ---

// singleCookieJar is a minimal http.CookieJar that returns a fixed cookie
// for any URL. Used to pass cookies to the WebSocket dialer.
type singleCookieJar struct {
	url    *url.URL
	cookie *http.Cookie
}

func newSingleCookieJar(u *url.URL, c *http.Cookie) *singleCookieJar {
	return &singleCookieJar{url: u, cookie: c}
}

func (j *singleCookieJar) SetCookies(_ *url.URL, _ []*http.Cookie) {}

func (j *singleCookieJar) Cookies(_ *url.URL) []*http.Cookie {
	return []*http.Cookie{j.cookie}
}
