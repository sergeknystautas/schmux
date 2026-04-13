//go:build !nogithub

package dashboard

import (
	"crypto/subtle"
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
	oauthStateCookie    = "schmux_oauth_state"
	oauthStateMaxAgeSec = 300
	oauthHTTPTimeout    = 10 * time.Second
)

var oauthClient = &http.Client{Timeout: oauthHTTPTimeout}

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

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled() {
		writeJSONError(w, "Auth disabled", http.StatusNotFound)
		return
	}
	secrets, err := config.GetAuthSecrets()
	if err != nil || secrets.GitHub == nil || strings.TrimSpace(secrets.GitHub.ClientID) == "" {
		writeJSONError(w, "GitHub auth not configured", http.StatusInternalServerError)
		return
	}

	state, err := randomToken(32)
	if err != nil {
		writeJSONError(w, "Failed to generate auth state", http.StatusInternalServerError)
		return
	}

	redirectURI, err := s.authRedirectURI()
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
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
		writeJSONError(w, "Auth disabled", http.StatusNotFound)
		return
	}

	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		writeJSONError(w, "Missing OAuth parameters", http.StatusBadRequest)
		return
	}

	stateCookie, err := r.Cookie(oauthStateCookie)
	if err != nil || stateCookie.Value == "" || subtle.ConstantTimeCompare([]byte(stateCookie.Value), []byte(state)) != 1 {
		writeJSONError(w, "Invalid OAuth state", http.StatusBadRequest)
		return
	}

	token, err := s.exchangeGitHubToken(code, state)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("OAuth exchange failed: %v", err), http.StatusBadRequest)
		return
	}

	user, err := s.fetchGitHubUser(token)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to fetch GitHub user: %v", err), http.StatusBadRequest)
		return
	}

	if err := s.setSessionCookie(w, user); err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to set session: %v", err), http.StatusInternalServerError)
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
		writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.validateCSRF(r) {
		writeJSONError(w, "Forbidden", http.StatusForbidden)
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
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
		s.logger.Error("failed to encode response", "handler", "auth-logout", "err", err)
	}
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled() {
		writeJSONError(w, "Auth disabled", http.StatusNotFound)
		return
	}

	session, err := s.authenticateRequest(r)
	if err != nil {
		writeJSONError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(session); err != nil {
		s.logger.Error("failed to encode response", "handler", "auth-me", "err", err)
	}
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
