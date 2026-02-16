package dashboard

import (
	"crypto/hmac"
	crypto_rand "crypto/rand"
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

const maxPasswordAttempts = 5

const minPasswordLength = 6

const remoteSessionMaxAge = 24 * time.Hour

type remoteNonce struct {
	createdAt time.Time
}

const nonceMaxAge = 5 * time.Minute

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
	nonce := r.URL.Query().Get("nonce")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Case 1: Token provided — consume it and redirect to a nonce
	if token != "" {
		s.remoteTokenMu.Lock()
		validToken := s.remoteToken
		if token != validToken || validToken == "" {
			s.remoteTokenMu.Unlock()
			fmt.Fprint(w, renderPasswordPage("", "Invalid or expired link.", 0))
			return
		}
		// Consume token (one-time use)
		s.remoteToken = ""

		// Generate nonce
		nonceBytes := make([]byte, 16)
		if _, err := crypto_rand.Read(nonceBytes); err != nil {
			s.remoteTokenMu.Unlock()
			fmt.Fprint(w, renderPasswordPage("", "Internal error.", 0))
			return
		}
		nonceValue := hex.EncodeToString(nonceBytes)

		// Clean expired nonces
		now := time.Now()
		for k, v := range s.remoteNonces {
			if now.Sub(v.createdAt) > nonceMaxAge {
				delete(s.remoteNonces, k)
			}
		}

		s.remoteNonces[nonceValue] = &remoteNonce{createdAt: now}
		s.remoteTokenMu.Unlock()

		http.Redirect(w, r, "/remote-auth?nonce="+nonceValue, http.StatusFound)
		return
	}

	// Case 2: Nonce provided — validate and show PIN form
	if nonce != "" {
		s.remoteTokenMu.Lock()
		n, exists := s.remoteNonces[nonce]
		failures := s.remoteTokenFailures
		s.remoteTokenMu.Unlock()

		if !exists || time.Since(n.createdAt) > nonceMaxAge {
			fmt.Fprint(w, renderPasswordPage("", "Invalid or expired link.", 0))
			return
		}

		if failures >= maxPasswordAttempts {
			fmt.Fprint(w, renderPasswordPage("", "Too many failed attempts. This link has been locked.", 0))
			return
		}

		remaining := maxPasswordAttempts - failures
		fmt.Fprint(w, renderPasswordPage(nonce, "", remaining))
		return
	}

	// Case 3: Neither token nor nonce — show instructions
	fmt.Fprint(w, renderInstructionsPage())
}

func (s *Server) handleRemoteAuthPOST(w http.ResponseWriter, r *http.Request) {
	// Rate limit by IP
	ip := s.normalizeIPForRateLimit(r)
	if !s.remoteAuthLimiter.Allow(ip) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, renderPasswordPage("", "Too many attempts. Please wait a minute before trying again.", 0))
		return
	}

	nonce := r.FormValue("nonce")
	password := r.FormValue("password")

	s.remoteTokenMu.Lock()
	n, nonceExists := s.remoteNonces[nonce]
	failures := s.remoteTokenFailures

	if nonce == "" || !nonceExists || time.Since(n.createdAt) > nonceMaxAge {
		s.remoteTokenMu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, renderPasswordPage("", "Invalid or expired link.", 0))
		return
	}

	if failures >= maxPasswordAttempts {
		delete(s.remoteNonces, nonce)
		s.remoteTokenMu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, renderPasswordPage("", "Too many failed attempts. This link has been locked.", 0))
		return
	}
	s.remoteTokenMu.Unlock()

	// Get password hash from config
	passwordHash := ""
	if s.config != nil {
		passwordHash = s.config.GetRemoteAccessPasswordHash()
	}
	if passwordHash == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, renderPasswordPage("", "Password not configured.", 0))
		return
	}

	// Verify password with bcrypt
	err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password))
	if err != nil {
		s.remoteTokenMu.Lock()
		s.remoteTokenFailures++
		newFailures := s.remoteTokenFailures
		if newFailures >= maxPasswordAttempts {
			delete(s.remoteNonces, nonce)
			s.remoteTokenMu.Unlock()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, renderPasswordPage("", "Too many failed attempts. This link has been locked.", 0))
			return
		}
		s.remoteTokenMu.Unlock()
		remaining := maxPasswordAttempts - newFailures
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, renderPasswordPage(nonce, "Incorrect password.", remaining))
		return
	}

	// Success — delete nonce, set remote session cookie
	s.remoteTokenMu.Lock()
	delete(s.remoteNonces, nonce)
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

func (s *Server) handleRemoteAccessSetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Password) < minPasswordLength {
		http.Error(w, fmt.Sprintf("Password must be at least %d characters", minPasswordLength), http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Failed to hash password", http.StatusInternalServerError)
		return
	}

	if s.config.RemoteAccess == nil {
		s.config.RemoteAccess = &config.RemoteAccessConfig{}
	}
	s.config.RemoteAccess.PasswordHash = string(hash)
	if err := s.config.Save(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save config: %v", err), http.StatusInternalServerError)
		return
	}

	// Regenerate session secret to invalidate existing remote cookies,
	// but only if a tunnel is currently active (secret already exists).
	// Setting the secret when no tunnel is active would activate auth
	// for local requests, locking out the local dashboard.
	s.remoteTokenMu.Lock()
	hasTunnel := len(s.remoteSessionSecret) > 0
	s.remoteTokenMu.Unlock()
	if hasTunnel {
		newSecret := make([]byte, 32)
		if _, err := crypto_rand.Read(newSecret); err == nil {
			s.remoteTokenMu.Lock()
			s.remoteSessionSecret = newSecret
			s.remoteTokenMu.Unlock()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func renderInstructionsPage() string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Authenticate</title>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body {
	font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
	display: flex; align-items: center; justify-content: center;
	min-height: 100vh; padding: 1rem;
	background: #fff; color: #111;
}
@media (prefers-color-scheme: dark) {
	body { background: #111; color: #e0e0e0; }
}
.card {
	max-width: 360px; width: 100%; padding: 2.5rem 2rem; text-align: center;
}
.card p { font-size: 0.95rem; line-height: 1.5; color: #555; }
@media (prefers-color-scheme: dark) {
	.card p { color: #999; }
}
</style>
</head>
<body>
<div class="card">
	<p>Check your notification app for the authentication link.</p>
</div>
</body>
</html>`
}

func renderPasswordPage(nonce string, errorMsg string, attemptsRemaining int) string {
	nonceField := ""
	if nonce != "" {
		nonceField = `<input type="hidden" name="nonce" value="` + html.EscapeString(nonce) + `">`
	}

	errorHTML := ""
	if errorMsg != "" {
		errorHTML = `<div class="error">` + html.EscapeString(errorMsg) + `</div>`
	}

	formHTML := ""
	if nonce != "" {
		attemptsHTML := ""
		if attemptsRemaining > 0 && attemptsRemaining < maxPasswordAttempts {
			attemptsHTML = fmt.Sprintf(`<div class="attempts">%d attempt(s) remaining</div>`, attemptsRemaining)
		}
		formHTML = `<form method="POST" action="/remote-auth">
			` + nonceField + `
			<label for="password">Password</label>
			<input type="password" id="password" name="password" autofocus required>
			` + attemptsHTML + `
			<button type="submit">Continue</button>
		</form>`
	}

	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Authenticate</title>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body {
	font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
	display: flex; align-items: center; justify-content: center;
	min-height: 100vh; padding: 1rem;
	background: #fff; color: #111;
}
@media (prefers-color-scheme: dark) {
	body { background: #111; color: #e0e0e0; }
	.card { border-color: #333; }
	input { background: #1a1a1a; color: #e0e0e0; border-color: #333; }
	input:focus { border-color: #888; }
	button { background: #fff; color: #111; }
	button:hover { background: #ddd; }
	.error { background: #2a1515; border-color: #dc2626; color: #ff6b6b; }
	.attempts { color: #999; }
}
.card {
	max-width: 360px; width: 100%; padding: 2.5rem 2rem;
}
form { display: flex; flex-direction: column; gap: 1rem; }
label { font-size: 0.8rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.05em; color: #555; }
input[type="password"] {
	padding: 0.75rem; border: 1px solid #ddd; border-radius: 6px;
	font-size: 1rem; outline: none; transition: border-color 0.15s;
}
input[type="password"]:focus { border-color: #111; }
button {
	padding: 0.75rem; background: #111; color: #fff; border: none;
	border-radius: 6px; font-size: 0.95rem; cursor: pointer; font-weight: 500;
	transition: background 0.15s;
}
button:hover { background: #333; }
.error {
	background: #fef2f2; border: 1px solid #fecaca; color: #dc2626;
	padding: 0.75rem; border-radius: 6px; font-size: 0.875rem; margin-bottom: 0.5rem;
}
.attempts { font-size: 0.8rem; color: #999; text-align: center; }
</style>
</head>
<body>
<div class="card">
	` + errorHTML + `
	` + formHTML + `
</div>
</body>
</html>`
}
