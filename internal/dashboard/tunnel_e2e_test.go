//go:build !notunnel

package dashboard

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
			cfgH := &ConfigHandlers{
				config: s.config,
				state:  s.state,
				models: s.models,
				logger: s.logger,
			}
			r.Get("/config", cfgH.handleConfigGet)
			r.Put("/config", cfgH.handleConfigUpdate)
			r.Post("/config", cfgH.handleConfigUpdate)
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
// Uses the testUA constant for the UA fingerprint binding.
func (tts *tunnelTestServer) makeRemoteCookie(t *testing.T) string {
	t.Helper()
	nowStr := fmt.Sprintf("%d", time.Now().Unix())
	tts.server.remoteTokenMu.Lock()
	secret := tts.server.remoteSessionSecret
	tts.server.remoteTokenMu.Unlock()

	uaHash := uaFingerprint(testUA)
	payload := nowStr + "." + uaHash
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

// tunneledRequest creates an HTTP request that simulates being proxied
// through cloudflared: RemoteAddr is loopback, Cf-Connecting-IP is the
// remote client's real IP. Sets User-Agent to testUA for cookie validation.
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
	req.Header.Set("User-Agent", testUA)
	return req
}

// localRequest creates an HTTP request that simulates a genuine local
// browser connection: RemoteAddr is loopback, no forwarding headers.
func localRequest(method, path string) *http.Request {
	req, _ := http.NewRequest(method, path, nil)
	req.RemoteAddr = "127.0.0.1:54321"
	return req
}

// --- Category 2: Attack Simulation ---

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
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", testUA)
	if !tts.server.validateRemoteCookie(cookie1, req) {
		t.Fatal("cookie should be valid during first tunnel session")
	}

	// Stop tunnel, start a new one (different session secret)
	tts.server.ClearRemoteAuth()
	tts.simulateTunnelConnect(t)

	// Old cookie from previous tunnel session should be rejected
	if tts.server.validateRemoteCookie(cookie1, req) {
		t.Error("cookie from previous tunnel session should be rejected after restart")
	}

	// New cookie should work
	cookie2 := tts.makeRemoteCookie(t)
	if !tts.server.validateRemoteCookie(cookie2, req) {
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
	header.Set("User-Agent", testUA)

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
