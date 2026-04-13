package dashboard

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
)

const (
	authCookieName = "schmux_auth"
	csrfCookieName = "schmux_csrf"
)

type authSession struct {
	GitHubID  int64  `json:"github_id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	ExpiresAt int64  `json:"expires_at"`
}

func (s *Server) authEnabled() bool {
	return s.config.GetAuthEnabled()
}

// requiresAuth returns true if requests must be authenticated.
// This is true when GitHub OAuth is enabled OR when a remote tunnel is active.
func (s *Server) requiresAuth() bool {
	if s.authEnabled() {
		return true
	}
	// If tunnel is active (session secret exists), require auth
	s.remoteTokenMu.Lock()
	hasSecret := len(s.remoteSessionSecret) > 0
	s.remoteTokenMu.Unlock()
	return hasSecret
}

func (s *Server) authCookieSecure() bool {
	base := s.config.GetPublicBaseURL()
	parsed, err := url.Parse(base)
	if err != nil {
		return false
	}
	return parsed.Scheme == "https"
}

// authMiddleware is a chi-compatible middleware for authentication.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.requiresAuth() {
			next.ServeHTTP(w, r)
			return
		}
		// Local requests bypass tunnel-only auth (but not GitHub OAuth).
		// The local user should always have unrestricted access.
		if !s.authEnabled() && s.isTrustedRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		if _, err := s.authenticateRequest(r); err != nil {
			writeJSONError(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// csrfMiddleware is a chi-compatible middleware for CSRF validation.
// Used for state-changing endpoints that need cross-site request forgery protection.
// Local requests (from loopback) are exempt from CSRF checks.
func (s *Server) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if !s.isTrustedRequest(r) && !s.validateCSRF(r) {
				writeJSONError(w, "Forbidden", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAuthOrRedirect(w http.ResponseWriter, r *http.Request) bool {
	if !s.requiresAuth() {
		return true
	}
	if !s.authEnabled() && s.isTrustedRequest(r) {
		return true
	}
	if _, err := s.authenticateRequest(r); err != nil {
		// If tunnel is active but GitHub OAuth is not enabled,
		// redirect to the remote PIN auth page instead of /auth/login.
		if !s.authEnabled() {
			s.remoteTokenMu.Lock()
			hasSecret := len(s.remoteSessionSecret) > 0
			s.remoteTokenMu.Unlock()
			if hasSecret {
				http.Redirect(w, r, "/remote-auth", http.StatusFound)
				return false
			}
		}
		http.Redirect(w, r, "/auth/login", http.StatusFound)
		return false
	}
	return true
}

func (s *Server) authenticateRequest(r *http.Request) (*authSession, error) {
	// Try GitHub OAuth cookie first
	cookie, err := r.Cookie(authCookieName)
	if err == nil {
		return s.parseSessionCookie(cookie.Value)
	}

	// Try remote session cookie
	remoteCookie, err := r.Cookie("schmux_remote")
	if err == nil {
		if s.validateRemoteCookie(remoteCookie.Value, r) {
			return &authSession{Login: "remote"}, nil
		}
	}

	return nil, errors.New("no valid auth session")
}

func (s *Server) parseSessionCookie(value string) (*authSession, error) {
	key, err := s.sessionKey()
	if err != nil {
		return nil, err
	}

	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid session cookie")
	}
	payload, err := base64.RawStdEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("invalid session cookie")
	}
	sig, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("invalid session cookie")
	}

	expected := signPayload(key, payload)
	if !hmac.Equal(sig, expected) {
		return nil, errors.New("invalid session signature")
	}

	var session authSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return nil, err
	}
	if session.ExpiresAt <= time.Now().Unix() {
		return nil, errors.New("session expired")
	}
	return &session, nil
}

func signPayload(key, payload []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return mac.Sum(nil)
}

func (s *Server) sessionKey() ([]byte, error) {
	if len(s.authSessionKey) > 0 {
		return s.authSessionKey, nil
	}
	secret, err := config.GetSessionSecret()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("session secret missing")
	}
	return decodeSessionSecret(secret)
}

func decodeSessionSecret(secret string) ([]byte, error) {
	key, err := base64.RawStdEncoding.DecodeString(secret)
	if err != nil {
		return nil, fmt.Errorf("invalid session secret: %w", err)
	}
	return key, nil
}

func randomToken(length int) (string, error) {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(buf), nil
}

func (s *Server) setCookie(w http.ResponseWriter, cookie *http.Cookie) {
	http.SetCookie(w, cookie)
}

func (s *Server) validateCSRF(r *http.Request) bool {
	token := strings.TrimSpace(r.Header.Get("X-CSRF-Token"))
	if token == "" {
		return false
	}
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil {
		return false
	}
	cookieValue := strings.TrimSpace(cookie.Value)
	if cookieValue == "" {
		return false
	}
	return hmac.Equal([]byte(cookieValue), []byte(token))
}
