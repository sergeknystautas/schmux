package dashboard

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
)

const (
	authCookieName      = "schmux_auth"
	oauthStateCookie    = "schmux_oauth_state"
	oauthStateMaxAgeSec = 300
	csrfCookieName      = "schmux_csrf"
	oauthHTTPTimeout    = 10 * time.Second
)

// oauthClient is an HTTP client with timeout for OAuth requests.
var oauthClient = &http.Client{Timeout: oauthHTTPTimeout}

type authSession struct {
	GitHubID  int64  `json:"github_id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	ExpiresAt int64  `json:"expires_at"`
}

type githubTokenResponse struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

type githubUserResponse struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

func (s *Server) authEnabled() bool {
	return s.config.GetAuthEnabled()
}

func (s *Server) authRedirectURI() (string, error) {
	base := strings.TrimRight(s.config.GetPublicBaseURL(), "/")
	if base == "" {
		return "", fmt.Errorf("public_base_url is required")
	}
	if _, err := url.Parse(base); err != nil {
		return "", fmt.Errorf("invalid public_base_url: %w", err)
	}
	return base + "/auth/callback", nil
}

func (s *Server) authCookieSecure() bool {
	base := s.config.GetPublicBaseURL()
	parsed, err := url.Parse(base)
	if err != nil {
		return false
	}
	return parsed.Scheme == "https"
}

func (s *Server) withAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authEnabled() {
			h(w, r)
			return
		}
		if _, err := s.authenticateRequest(r); err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

func (s *Server) withAuthHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authEnabled() {
			h.ServeHTTP(w, r)
			return
		}
		if _, err := s.authenticateRequest(r); err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func (s *Server) requireAuthOrRedirect(w http.ResponseWriter, r *http.Request) bool {
	if !s.authEnabled() {
		return true
	}
	if _, err := s.authenticateRequest(r); err != nil {
		http.Redirect(w, r, "/auth/login", http.StatusFound)
		return false
	}
	return true
}

func (s *Server) authenticateRequest(r *http.Request) (*authSession, error) {
	cookie, err := r.Cookie(authCookieName)
	if err != nil {
		return nil, err
	}
	return s.parseSessionCookie(cookie.Value)
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled() {
		http.Error(w, "Auth disabled", http.StatusNotFound)
		return
	}
	secrets, err := config.GetAuthSecrets()
	if err != nil || secrets.GitHub == nil || strings.TrimSpace(secrets.GitHub.ClientID) == "" {
		http.Error(w, "GitHub auth not configured", http.StatusInternalServerError)
		return
	}

	state, err := randomToken(32)
	if err != nil {
		http.Error(w, "Failed to generate auth state", http.StatusInternalServerError)
		return
	}

	redirectURI, err := s.authRedirectURI()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	params := url.Values{}
	params.Set("client_id", secrets.GitHub.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("state", state)
	params.Set("scope", "read:user")

	s.setCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    state,
		Path:     "/",
		MaxAge:   oauthStateMaxAgeSec,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.authCookieSecure(),
	})

	authURL := "https://github.com/login/oauth/authorize?" + params.Encode()
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled() {
		http.Error(w, "Auth disabled", http.StatusNotFound)
		return
	}

	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		http.Error(w, "Missing OAuth parameters", http.StatusBadRequest)
		return
	}

	stateCookie, err := r.Cookie(oauthStateCookie)
	if err != nil || stateCookie.Value == "" || stateCookie.Value != state {
		http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
		return
	}

	token, err := s.exchangeGitHubToken(code, state)
	if err != nil {
		http.Error(w, fmt.Sprintf("OAuth exchange failed: %v", err), http.StatusBadRequest)
		return
	}

	user, err := s.fetchGitHubUser(token)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch GitHub user: %v", err), http.StatusBadRequest)
		return
	}

	if err := s.setSessionCookie(w, user); err != nil {
		http.Error(w, fmt.Sprintf("Failed to set session: %v", err), http.StatusInternalServerError)
		return
	}

	// Clear state cookie
	s.setCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.authCookieSecure(),
	})

	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.validateCSRF(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	s.setCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.authCookieSecure(),
	})
	s.setCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: false,
		SameSite: http.SameSiteStrictMode,
		Secure:   s.authCookieSecure(),
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled() {
		http.Error(w, "Auth disabled", http.StatusNotFound)
		return
	}

	session, err := s.authenticateRequest(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
}

func (s *Server) exchangeGitHubToken(code, state string) (string, error) {
	secrets, err := config.GetAuthSecrets()
	if err != nil || secrets.GitHub == nil {
		return "", errors.New("GitHub auth not configured")
	}
	redirectURI, err := s.authRedirectURI()
	if err != nil {
		return "", err
	}

	payload := url.Values{}
	payload.Set("client_id", secrets.GitHub.ClientID)
	payload.Set("client_secret", secrets.GitHub.ClientSecret)
	payload.Set("code", code)
	payload.Set("redirect_uri", redirectURI)
	payload.Set("state", state)

	req, err := http.NewRequest(http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(payload.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := oauthClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var tokenResp githubTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", err
	}
	if tokenResp.Error != "" {
		return "", fmt.Errorf("oauth error: %s", tokenResp.ErrorDesc)
	}
	if tokenResp.AccessToken == "" {
		return "", errors.New("missing access_token")
	}
	return tokenResp.AccessToken, nil
}

func (s *Server) fetchGitHubUser(token string) (*githubUserResponse, error) {
	if token == "" {
		return nil, errors.New("missing access token")
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "schmux")

	resp, err := oauthClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github api error: %s", strings.TrimSpace(string(body)))
	}

	var user githubUserResponse
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, err
	}
	if user.ID == 0 || user.Login == "" {
		return nil, errors.New("invalid GitHub user response")
	}
	return &user, nil
}

func (s *Server) setSessionCookie(w http.ResponseWriter, user *githubUserResponse) error {
	key, err := s.sessionKey()
	if err != nil {
		return err
	}

	ttl := time.Duration(s.config.GetAuthSessionTTLMinutes()) * time.Minute
	session := authSession{
		GitHubID:  user.ID,
		Login:     user.Login,
		Name:      user.Name,
		AvatarURL: user.AvatarURL,
		ExpiresAt: time.Now().Add(ttl).Unix(),
	}

	payload, err := json.Marshal(session)
	if err != nil {
		return err
	}

	signature := signPayload(key, payload)
	value := base64.RawStdEncoding.EncodeToString(payload) + "." + base64.RawStdEncoding.EncodeToString(signature)

	s.setCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.authCookieSecure(),
	})

	csrfToken, err := randomToken(32)
	if err != nil {
		return err
	}
	s.setCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    csrfToken,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: false,
		SameSite: http.SameSiteStrictMode,
		Secure:   s.authCookieSecure(),
	})
	return nil
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
