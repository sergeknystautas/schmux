package dashboard

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"golang.org/x/crypto/bcrypt"
)

const maxPinAttempts = 5

const minPinLength = 6

const remoteSessionMaxAge = 24 * time.Hour

func (s *Server) handleRemoteAuth(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleRemoteAuthGET(w, r)
	case http.MethodPost:
		s.handleRemoteAuthPOST(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRemoteAuthGET(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")

	s.remoteTokenMu.Lock()
	validToken := s.remoteToken
	failures := s.remoteTokenFailures
	s.remoteTokenMu.Unlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if token == "" || validToken == "" || token != validToken {
		fmt.Fprint(w, renderPinPage("", "Invalid or expired link. Please request a new one from the dashboard.", 0))
		return
	}

	if failures >= maxPinAttempts {
		fmt.Fprint(w, renderPinPage("", "Too many failed attempts. This link has been locked. Please restart the tunnel.", 0))
		return
	}

	remaining := maxPinAttempts - failures
	fmt.Fprint(w, renderPinPage(token, "", remaining))
}

func (s *Server) handleRemoteAuthPOST(w http.ResponseWriter, r *http.Request) {
	// Rate limit by IP
	ip := s.normalizeIPForRateLimit(r.RemoteAddr)
	if !s.remoteAuthLimiter.Allow(ip) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, renderPinPage("", "Too many attempts. Please wait a minute before trying again.", 0))
		return
	}

	token := r.FormValue("token")
	pin := r.FormValue("pin")

	s.remoteTokenMu.Lock()
	validToken := s.remoteToken
	failures := s.remoteTokenFailures

	if token == "" || validToken == "" || token != validToken {
		s.remoteTokenMu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, renderPinPage("", "Invalid or expired link. Please request a new one from the dashboard.", 0))
		return
	}

	if failures >= maxPinAttempts {
		s.remoteToken = ""
		s.remoteTokenMu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, renderPinPage("", "Too many failed attempts. This link has been locked. Please restart the tunnel.", 0))
		return
	}
	s.remoteTokenMu.Unlock()

	// Get PIN hash from config
	pinHash := ""
	if s.config != nil {
		pinHash = s.config.GetRemoteAccessPinHash()
	}
	if pinHash == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, renderPinPage("", "PIN not configured on server. Run: schmux remote set-pin", 0))
		return
	}

	// Verify PIN with bcrypt
	err := bcrypt.CompareHashAndPassword([]byte(pinHash), []byte(pin))
	if err != nil {
		s.remoteTokenMu.Lock()
		s.remoteTokenFailures++
		newFailures := s.remoteTokenFailures
		if newFailures >= maxPinAttempts {
			s.remoteToken = ""
			s.remoteTokenMu.Unlock()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, renderPinPage("", "Too many failed attempts. This link has been locked. Please restart the tunnel.", 0))
			return
		}
		s.remoteTokenMu.Unlock()
		remaining := maxPinAttempts - newFailures
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, renderPinPage(token, "Incorrect PIN.", remaining))
		return
	}

	// Success — set remote session cookie
	s.remoteTokenMu.Lock()
	s.remoteToken = "" // one-time use
	secret := s.remoteSessionSecret
	s.remoteTokenMu.Unlock()

	s.setRemoteSessionCookie(w, secret)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) setRemoteSessionCookie(w http.ResponseWriter, secret []byte) {
	now := fmt.Sprintf("%d", time.Now().Unix())
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(now))
	sig := hex.EncodeToString(mac.Sum(nil))

	http.SetCookie(w, &http.Cookie{
		Name:     "schmux_remote",
		Value:    now + "." + sig,
		Path:     "/",
		MaxAge:   int(remoteSessionMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	// Set CSRF cookie so remote sessions can make state-changing requests
	csrfToken, err := randomToken(32)
	if err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    csrfToken,
		Path:     "/",
		MaxAge:   int(remoteSessionMaxAge.Seconds()),
		HttpOnly: false,
		SameSite: http.SameSiteStrictMode,
		Secure:   true,
	})
}

func (s *Server) validateRemoteCookie(value string) bool {
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 {
		return false
	}

	// Check timestamp expiry
	ts, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}
	if time.Since(time.Unix(ts, 0)) > remoteSessionMaxAge {
		return false
	}

	s.remoteTokenMu.Lock()
	secret := s.remoteSessionSecret
	s.remoteTokenMu.Unlock()

	if len(secret) == 0 {
		return false
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0]))
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(parts[1]), []byte(expected))
}

func (s *Server) handleRemoteAccessSetPin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Pin string `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Pin) < minPinLength {
		http.Error(w, fmt.Sprintf("PIN must be at least %d characters", minPinLength), http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Pin), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to hash PIN", http.StatusInternalServerError)
		return
	}

	if s.config.RemoteAccess == nil {
		s.config.RemoteAccess = &config.RemoteAccessConfig{}
	}
	s.config.RemoteAccess.PinHash = string(hash)
	if err := s.config.Save(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save config: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func renderPinPage(token string, errorMsg string, attemptsRemaining int) string {
	tokenField := ""
	if token != "" {
		tokenField = `<input type="hidden" name="token" value="` + html.EscapeString(token) + `">`
	}

	errorHTML := ""
	if errorMsg != "" {
		errorHTML = `<div class="error">` + html.EscapeString(errorMsg) + `</div>`
	}

	formHTML := ""
	if token != "" {
		attemptsHTML := ""
		if attemptsRemaining > 0 && attemptsRemaining < maxPinAttempts {
			attemptsHTML = fmt.Sprintf(`<div class="attempts">%d attempt(s) remaining</div>`, attemptsRemaining)
		}
		formHTML = `<form method="POST" action="/remote-auth">
			` + tokenField + `
			<label for="pin">Enter PIN</label>
			<input type="password" id="pin" name="pin" autofocus required placeholder="Your PIN or passphrase">
			` + attemptsHTML + `
			<button type="submit">Authenticate</button>
		</form>`
	}

	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>schmux — Remote Access</title>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body {
	font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
	display: flex; align-items: center; justify-content: center;
	min-height: 100vh; padding: 1rem;
	background: #f5f5f5; color: #333;
}
@media (prefers-color-scheme: dark) {
	body { background: #1a1a2e; color: #e0e0e0; }
	.card { background: #16213e; border-color: #0f3460; }
	input { background: #1a1a2e; color: #e0e0e0; border-color: #0f3460; }
	input:focus { border-color: #4a90d9; }
	button { background: #4a90d9; }
	button:hover { background: #357abd; }
	.error { background: #3d1515; border-color: #e74c3c; color: #ff6b6b; }
	.attempts { color: #999; }
}
.card {
	background: #fff; border: 1px solid #ddd; border-radius: 12px;
	padding: 2rem; max-width: 400px; width: 100%;
	box-shadow: 0 2px 8px rgba(0,0,0,0.1);
}
h1 { font-size: 1.25rem; margin-bottom: 0.5rem; }
.subtitle { font-size: 0.875rem; color: #888; margin-bottom: 1.5rem; }
form { display: flex; flex-direction: column; gap: 0.75rem; }
label { font-size: 0.875rem; font-weight: 500; }
input[type="password"] {
	padding: 0.75rem; border: 1px solid #ddd; border-radius: 8px;
	font-size: 1rem; outline: none;
}
input[type="password"]:focus { border-color: #2563eb; }
button {
	padding: 0.75rem; background: #2563eb; color: #fff; border: none;
	border-radius: 8px; font-size: 1rem; cursor: pointer; font-weight: 500;
}
button:hover { background: #1d4ed8; }
.error {
	background: #fef2f2; border: 1px solid #fecaca; color: #dc2626;
	padding: 0.75rem; border-radius: 8px; font-size: 0.875rem; margin-bottom: 0.5rem;
}
.attempts { font-size: 0.8rem; color: #666; text-align: center; }
</style>
</head>
<body>
<div class="card">
	<h1>schmux Remote Access</h1>
	<p class="subtitle">Authenticate to access the dashboard</p>
	` + errorHTML + `
	` + formHTML + `
</div>
</body>
</html>`
}
