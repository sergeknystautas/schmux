package dashboard

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
)

func TestValidateCSRF_EmptyToken(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	// No X-CSRF-Token header

	if s.validateCSRF(req) {
		t.Error("expected false for missing CSRF token")
	}
}

func TestValidateCSRF_EmptyTokenWithWhitespace(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("X-CSRF-Token", "   ")

	if s.validateCSRF(req) {
		t.Error("expected false for whitespace-only CSRF token")
	}
}

func TestValidateCSRF_MissingCookie(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("X-CSRF-Token", "some-token")
	// No cookie set

	if s.validateCSRF(req) {
		t.Error("expected false for missing CSRF cookie")
	}
}

func TestValidateCSRF_Mismatch(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("X-CSRF-Token", "token-a")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "token-b"})

	if s.validateCSRF(req) {
		t.Error("expected false for mismatched CSRF tokens")
	}
}

func TestValidateCSRF_Match(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("X-CSRF-Token", "matching-token")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "matching-token"})

	if !s.validateCSRF(req) {
		t.Error("expected true for matching CSRF tokens")
	}
}

func TestValidateCSRF_MatchWithWhitespace(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("X-CSRF-Token", "  matching-token  ")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "  matching-token  "})

	if !s.validateCSRF(req) {
		t.Error("expected true for matching CSRF tokens with whitespace trimmed")
	}
}

func TestDecodeSessionSecret_Valid(t *testing.T) {
	// Create a valid base64-encoded secret (32 bytes -> 43 chars in base64 raw)
	secret := "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXoxMjM0NTY" // 32 bytes decoded
	key, err := decodeSessionSecret(secret)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if key == nil {
		t.Error("expected non-nil key")
	}
}

func TestDecodeSessionSecret_InvalidBase64(t *testing.T) {
	// Invalid base64 characters should fail
	secret := "not-valid-base64!!!"
	_, err := decodeSessionSecret(secret)
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestRandomToken(t *testing.T) {
	token, err := randomToken(32)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}
	// Token should be base64 encoded 32 bytes
	if len(token) < 40 {
		t.Errorf("token seems too short: %d chars", len(token))
	}

	// Ensure tokens are unique
	token2, _ := randomToken(32)
	if token == token2 {
		t.Error("expected unique tokens")
	}
}

func TestRandomToken_DifferentLengths(t *testing.T) {
	tests := []int{16, 32, 64}
	for _, length := range tests {
		token, err := randomToken(length)
		if err != nil {
			t.Errorf("length %d: expected no error, got %v", length, err)
		}
		if token == "" {
			t.Errorf("length %d: expected non-empty token", length)
		}
	}
}

// makeSessionCookie constructs a signed session cookie value identical to the
// format produced by handleAuthGitHubCallback. Used to exercise parseSessionCookie.
func makeSessionCookie(t *testing.T, key []byte, session authSession) string {
	t.Helper()
	payload, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	sig := signPayload(key, payload)
	return base64.RawStdEncoding.EncodeToString(payload) + "." +
		base64.RawStdEncoding.EncodeToString(sig)
}

func TestParseSessionCookie_Valid(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	s := &Server{authSessionKey: key}
	want := authSession{
		GitHubID:  42,
		Login:     "alice",
		Name:      "Alice",
		AvatarURL: "https://example/a.png",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}

	got, err := s.parseSessionCookie(makeSessionCookie(t, key, want))
	if err != nil {
		t.Fatalf("parseSessionCookie: %v", err)
	}
	if got.Login != want.Login || got.GitHubID != want.GitHubID || got.AvatarURL != want.AvatarURL {
		t.Errorf("parsed session mismatch: got %+v want %+v", got, want)
	}
}

func TestParseSessionCookie_InvalidFormat(t *testing.T) {
	s := &Server{authSessionKey: []byte("0123456789abcdef0123456789abcdef")}
	cases := []struct {
		name  string
		value string
	}{
		{"no-separator", "abcdef"},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.parseSessionCookie(tc.value); err == nil {
				t.Error("expected error for malformed cookie")
			}
		})
	}
}

func TestParseSessionCookie_BadBase64Payload(t *testing.T) {
	s := &Server{authSessionKey: []byte("0123456789abcdef0123456789abcdef")}
	// Payload uses characters outside RawStdEncoding alphabet.
	if _, err := s.parseSessionCookie("!!!invalid!!!.deadbeef"); err == nil {
		t.Error("expected error for invalid base64 payload")
	}
}

func TestParseSessionCookie_BadBase64Signature(t *testing.T) {
	s := &Server{authSessionKey: []byte("0123456789abcdef0123456789abcdef")}
	payload, _ := json.Marshal(authSession{Login: "bob", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	value := base64.RawStdEncoding.EncodeToString(payload) + ".***bad-sig***"
	if _, err := s.parseSessionCookie(value); err == nil {
		t.Error("expected error for invalid base64 signature")
	}
}

func TestParseSessionCookie_BadSignature(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	wrongKey := []byte("ffffffffffffffffffffffffffffffff")
	s := &Server{authSessionKey: key}
	cookie := makeSessionCookie(t, wrongKey, authSession{
		Login:     "mallory",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	_, err := s.parseSessionCookie(cookie)
	if err == nil || !strings.Contains(err.Error(), "signature") {
		t.Errorf("expected signature error, got %v", err)
	}
}

func TestParseSessionCookie_BadJSON(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	s := &Server{authSessionKey: key}
	// Sign garbage bytes — signature is valid but JSON unmarshaling fails.
	garbage := []byte("not-json{")
	sig := signPayload(key, garbage)
	value := base64.RawStdEncoding.EncodeToString(garbage) + "." +
		base64.RawStdEncoding.EncodeToString(sig)
	if _, err := s.parseSessionCookie(value); err == nil {
		t.Error("expected JSON unmarshal error")
	}
}

func TestParseSessionCookie_Expired(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	s := &Server{authSessionKey: key}
	cookie := makeSessionCookie(t, key, authSession{
		Login:     "stale",
		ExpiresAt: time.Now().Add(-1 * time.Minute).Unix(),
	})
	_, err := s.parseSessionCookie(cookie)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("expected expired error, got %v", err)
	}
}

func TestAuthCookieSecure(t *testing.T) {
	skipUnderVendorlocked(t)
	cases := []struct {
		name string
		base string
		want bool
	}{
		{"https", "https://example.com", true},
		{"http", "http://example.com", false},
		{"empty", "", false},
		{"malformed", "://[::bad", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.CreateDefault(filepath.Join(t.TempDir(), "config.json"))
			cfg.Network = &config.NetworkConfig{PublicBaseURL: tc.base}
			s := &Server{config: cfg}
			if got := s.authCookieSecure(); got != tc.want {
				t.Errorf("authCookieSecure(%q) = %v, want %v", tc.base, got, tc.want)
			}
		})
	}
}

// minimalServerWithConfig builds a Server with just enough plumbing to exercise
// auth/csrf middleware. The config defaults to no auth and no remote access.
func minimalServerWithConfig(t *testing.T) (*Server, *config.Config) {
	t.Helper()
	cfg := config.CreateDefault(filepath.Join(t.TempDir(), "config.json"))
	cfg.Network = &config.NetworkConfig{}
	return &Server{config: cfg}, cfg
}

// boolPtr returns a pointer to b, used for *bool fields like RemoteAccess.Enabled.
func boolPtr(b bool) *bool { return &b }

func TestCsrfMiddleware_BypassesGET(t *testing.T) {
	s, cfg := minimalServerWithConfig(t)
	cfg.RemoteAccess = &config.RemoteAccessConfig{Enabled: boolPtr(true)}

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	req.RemoteAddr = "8.8.8.8:1234" // non-loopback to make sure GET still bypasses
	rr := httptest.NewRecorder()
	s.csrfMiddleware(next).ServeHTTP(rr, req)

	if !called {
		t.Fatal("next handler should have been called for GET")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestCsrfMiddleware_BypassesTrustedLocalPost(t *testing.T) {
	// Default config: remote access not enabled → all requests are trusted.
	s, _ := minimalServerWithConfig(t)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	req := httptest.NewRequest(http.MethodPost, "/api/x", nil)
	rr := httptest.NewRecorder()
	s.csrfMiddleware(next).ServeHTTP(rr, req)

	if !called {
		t.Fatal("trusted POST should pass through")
	}
}

func TestCsrfMiddleware_RejectsUntrustedPostWithoutToken(t *testing.T) {
	s, cfg := minimalServerWithConfig(t)
	cfg.RemoteAccess = &config.RemoteAccessConfig{Enabled: boolPtr(true)}

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	req := httptest.NewRequest(http.MethodPost, "/api/x", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	rr := httptest.NewRecorder()
	s.csrfMiddleware(next).ServeHTTP(rr, req)

	if called {
		t.Error("untrusted POST without CSRF token should be blocked")
	}
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestCsrfMiddleware_AcceptsUntrustedPostWithValidToken(t *testing.T) {
	s, cfg := minimalServerWithConfig(t)
	cfg.RemoteAccess = &config.RemoteAccessConfig{Enabled: boolPtr(true)}

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	req := httptest.NewRequest(http.MethodPost, "/api/x", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	req.Header.Set("X-CSRF-Token", "tok")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "tok"})
	rr := httptest.NewRecorder()
	s.csrfMiddleware(next).ServeHTTP(rr, req)

	if !called {
		t.Error("POST with matching CSRF token should pass through")
	}
}

func TestAuthMiddleware_BypassWhenAuthNotRequired(t *testing.T) {
	// Default: no auth, no tunnel → requiresAuth is false.
	s, _ := minimalServerWithConfig(t)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	rr := httptest.NewRecorder()
	s.authMiddleware(next).ServeHTTP(rr, req)

	if !called {
		t.Fatal("expected handler to be called when auth not required")
	}
}

func TestAuthMiddleware_TunnelBypassForLocal(t *testing.T) {
	// Tunnel active (remoteSessionSecret set) + no GitHub OAuth →
	// local trusted requests should bypass auth.
	s, _ := minimalServerWithConfig(t)
	s.remoteSessionSecret = []byte("secret")

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	rr := httptest.NewRecorder()
	s.authMiddleware(next).ServeHTTP(rr, req)

	if !called {
		t.Error("local trusted request should bypass tunnel-only auth")
	}
}

func TestAuthMiddleware_RejectsTunnelRequestWithoutCookie(t *testing.T) {
	// Tunnel + remote access on + non-loopback IP + no cookie → 401.
	s, cfg := minimalServerWithConfig(t)
	cfg.RemoteAccess = &config.RemoteAccessConfig{Enabled: boolPtr(true)}
	s.remoteSessionSecret = []byte("secret")

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	req.RemoteAddr = "203.0.113.10:443"
	rr := httptest.NewRecorder()
	s.authMiddleware(next).ServeHTTP(rr, req)

	if called {
		t.Error("expected 401 for unauthenticated tunnel request")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_AcceptsValidGitHubCookie(t *testing.T) {
	// Auth enabled (GitHub OAuth) + valid signed cookie → bypass.
	s, cfg := minimalServerWithConfig(t)
	cfg.AccessControl = &config.AccessControlConfig{Enabled: true}
	s.authSessionKey = []byte("0123456789abcdef0123456789abcdef")

	cookie := makeSessionCookie(t, s.authSessionKey, authSession{
		Login:     "alice",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
	rr := httptest.NewRecorder()
	s.authMiddleware(next).ServeHTTP(rr, req)

	if !called {
		t.Errorf("expected handler called for valid cookie, got status %d", rr.Code)
	}
}

func TestAuthMiddleware_RejectsExpiredCookie(t *testing.T) {
	s, cfg := minimalServerWithConfig(t)
	cfg.AccessControl = &config.AccessControlConfig{Enabled: true}
	s.authSessionKey = []byte("0123456789abcdef0123456789abcdef")

	cookie := makeSessionCookie(t, s.authSessionKey, authSession{
		Login:     "alice",
		ExpiresAt: time.Now().Add(-time.Hour).Unix(),
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not run for expired cookie")
	})
	req := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	req.AddCookie(&http.Cookie{Name: authCookieName, Value: cookie})
	rr := httptest.NewRecorder()
	s.authMiddleware(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestSignPayload(t *testing.T) {
	key := []byte("test-secret-key")
	payload := []byte("hello world")

	// Deterministic: same inputs always produce the same output
	sig1 := signPayload(key, payload)
	sig2 := signPayload(key, payload)
	if len(sig1) != 32 { // SHA-256 produces 32 bytes
		t.Errorf("signPayload length = %d, want 32", len(sig1))
	}
	if string(sig1) != string(sig2) {
		t.Error("signPayload should be deterministic")
	}

	// Different payload produces different signature
	sig3 := signPayload(key, []byte("different payload"))
	if string(sig1) == string(sig3) {
		t.Error("different payloads should produce different signatures")
	}

	// Different key produces different signature
	sig4 := signPayload([]byte("different-key"), payload)
	if string(sig1) == string(sig4) {
		t.Error("different keys should produce different signatures")
	}

	// Empty payload still produces a valid 32-byte signature
	sig5 := signPayload(key, []byte{})
	if len(sig5) != 32 {
		t.Errorf("signPayload with empty payload length = %d, want 32", len(sig5))
	}
}
