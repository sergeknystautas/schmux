# Remote Access Token + PIN Authentication — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Replace OAuth-based auth for remote access with token + PIN two-factor authentication that works with ephemeral Cloudflare tunnel URLs.

**Architecture:** The tunnel manager's preconditions change from requiring GitHub OAuth to requiring a bcrypt PIN hash. When a tunnel connects, the server generates a one-time token and sends a notification with an auth URL. The user opens the link, enters their PIN, and gets a session cookie. All state lives on the Server struct — no new packages needed.

**Tech Stack:** Go stdlib (`crypto/rand`, `crypto/hmac`, `crypto/sha256`, `encoding/hex`), `golang.org/x/crypto/bcrypt`, existing `internal/tunnel`, `internal/dashboard`, `internal/config` packages.

**Design doc:** `docs/spec/remote-access-auth.md`

---

### Task 1: Add bcrypt dependency

**Files:**

- Modify: `go.mod`

**Step 1: Add the bcrypt dependency**

```bash
cd /Users/stefanomaz/code/workspaces/schmux-003 && go get golang.org/x/crypto/bcrypt
```

**Step 2: Verify it compiles**

```bash
go build ./cmd/schmux
```

Expected: BUILD SUCCESS

**Step 3: Commit**

```bash
git commit -am "Add golang.org/x/crypto/bcrypt dependency for PIN hashing"
```

---

### Task 2: Add PinHash to config and ManagerConfig

Replace `AuthEnabled` + `AllowedUsersSet` with `PinHashSet` in the tunnel manager, and add `pin_hash` field to config.

**Files:**

- Modify: `internal/config/config.go:234-239` (RemoteAccessConfig struct)
- Modify: `internal/config/config.go` (add GetRemoteAccessPinHash getter)
- Modify: `internal/tunnel/manager.go:31-39` (ManagerConfig struct)
- Modify: `internal/tunnel/manager.go:77-86` (Start preconditions)
- Modify: `internal/tunnel/manager_test.go` (update tests)
- Modify: `internal/api/contracts/config.go:271-275` (RemoteAccess struct)

**Step 1: Write the failing tests**

Update `internal/tunnel/manager_test.go`:

- Remove `TestTunnelState_StartRequiresAuth` and `TestTunnelState_StartRequiresAllowlist`
- Add `TestTunnelState_StartRequiresPinHash`:
  ```go
  func TestTunnelState_StartRequiresPinHash(t *testing.T) {
      m := NewManager(ManagerConfig{PinHashSet: false})
      err := m.Start()
      if err == nil {
          t.Fatal("expected error when PIN not configured")
      }
      if !strings.Contains(err.Error(), "PIN") {
          t.Errorf("error should mention PIN, got: %s", err.Error())
      }
  }
  ```
- Update `TestTunnelState_StartRequiresNotDisabled` to use `PinHashSet: true` instead of `AuthEnabled`/`AllowedUsersSet`.

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/tunnel/ -run TestTunnelState
```

Expected: FAIL (field names don't exist yet on ManagerConfig)

**Step 3: Implement the changes**

In `internal/tunnel/manager.go`, replace `ManagerConfig`:

```go
type ManagerConfig struct {
    Disabled       bool
    PinHashSet     bool
    Port           int
    SchmuxBinDir   string
    TimeoutMinutes int
    OnStatusChange func(TunnelStatus)
}
```

In `Start()`, replace the two auth checks with:

```go
if !m.config.PinHashSet {
    return fmt.Errorf("remote access requires a PIN (run: schmux remote set-pin)")
}
```

In `internal/config/config.go`, add `PinHash` to `RemoteAccessConfig`:

```go
type RemoteAccessConfig struct {
    Disabled       *bool                     `json:"disabled,omitempty"`
    TimeoutMinutes int                       `json:"timeout_minutes,omitempty"`
    PinHash        string                    `json:"pin_hash,omitempty"`
    Notify         *RemoteAccessNotifyConfig `json:"notify,omitempty"`
}
```

Add getter:

```go
func (c *Config) GetRemoteAccessPinHash() string {
    if c == nil || c.RemoteAccess == nil {
        return ""
    }
    return c.RemoteAccess.PinHash
}
```

In `internal/api/contracts/config.go`, add `PinHashSet` to `RemoteAccess`:

```go
type RemoteAccess struct {
    Disabled       bool               `json:"disabled"`
    TimeoutMinutes int                `json:"timeout_minutes"`
    PinHashSet     bool               `json:"pin_hash_set"`
    Notify         RemoteAccessNotify `json:"notify"`
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/tunnel/ -run TestTunnelState
```

Expected: PASS

**Step 5: Commit**

```bash
git commit -am "Replace AuthEnabled/AllowedUsersSet with PinHashSet in tunnel manager"
```

---

### Task 3: Update daemon wiring to use PinHashSet

**Files:**

- Modify: `internal/daemon/daemon.go:387-410` (tunnel manager creation)

**Step 1: Update daemon.go**

Change the TunnelManager creation from:

```go
tunnelMgr := tunnel.NewManager(tunnel.ManagerConfig{
    Disabled:        cfg.GetRemoteAccessDisabled(),
    AuthEnabled:     cfg.GetAuthEnabled(),
    AllowedUsersSet: cfg.GetAuthEnabled(),
    ...
})
```

To:

```go
tunnelMgr := tunnel.NewManager(tunnel.ManagerConfig{
    Disabled:     cfg.GetRemoteAccessDisabled(),
    PinHashSet:   cfg.GetRemoteAccessPinHash() != "",
    Port:         cfg.GetPort(),
    SchmuxBinDir: filepath.Join(filepath.Dir(statePath), "bin"),
    TimeoutMinutes: cfg.GetRemoteAccessTimeoutMinutes(),
    OnStatusChange: func(status tunnel.TunnelStatus) {
        server.BroadcastTunnelStatus(status)
        if status.State == tunnel.StateConnected && status.URL != "" {
            server.HandleTunnelConnected(status.URL)
        }
    },
})
```

Note: the `HandleTunnelConnected` call replaces the inline notification code. The notification sending moves to the server (Task 5).

**Step 2: Verify build**

```bash
go build ./cmd/schmux
```

Expected: BUILD FAIL (HandleTunnelConnected doesn't exist yet — that's fine, we'll stub it)

Add a stub method to `internal/dashboard/server.go` so it compiles:

```go
// HandleTunnelConnected handles a newly connected tunnel by generating an auth token and sending notifications.
func (s *Server) HandleTunnelConnected(tunnelURL string) {
    // TODO: implement in Task 5
    fmt.Printf("[remote-access] tunnel connected: %s\n", tunnelURL)
}
```

**Step 3: Verify build**

```bash
go build ./cmd/schmux
```

Expected: BUILD SUCCESS

**Step 4: Run tests**

```bash
go test ./...
```

Expected: PASS

**Step 5: Commit**

```bash
git commit -am "Update daemon to use PinHashSet and delegate notifications to server"
```

---

### Task 4: Add remote auth state and HandleTunnelConnected to Server

Add the remote auth state fields and implement the full `HandleTunnelConnected` method that generates a token, resets state, and sends notifications.

**Files:**

- Modify: `internal/dashboard/server.go` (add fields to Server struct, implement HandleTunnelConnected, add ClearRemoteAuth helper)
- Test: `internal/dashboard/handlers_remote_access_test.go` (new test file)

**Step 1: Write the failing test**

Create `internal/dashboard/handlers_remote_access_test.go`:

```go
package dashboard

import (
    "testing"
)

func TestHandleTunnelConnected_GeneratesToken(t *testing.T) {
    s := &Server{}
    s.HandleTunnelConnected("https://test.trycloudflare.com")

    s.remoteTokenMu.Lock()
    defer s.remoteTokenMu.Unlock()

    if s.remoteToken == "" {
        t.Fatal("expected remoteToken to be set")
    }
    if len(s.remoteToken) != 64 { // 32 bytes hex = 64 chars
        t.Errorf("expected 64-char hex token, got %d chars", len(s.remoteToken))
    }
    if s.remoteTokenFailures != 0 {
        t.Errorf("expected 0 failures, got %d", s.remoteTokenFailures)
    }
    if len(s.remoteSessionSecret) == 0 {
        t.Error("expected remoteSessionSecret to be set")
    }
}

func TestClearRemoteAuth_ResetsState(t *testing.T) {
    s := &Server{}
    s.HandleTunnelConnected("https://test.trycloudflare.com")
    s.ClearRemoteAuth()

    s.remoteTokenMu.Lock()
    defer s.remoteTokenMu.Unlock()

    if s.remoteToken != "" {
        t.Error("expected remoteToken to be empty after clear")
    }
    if s.remoteTokenFailures != 0 {
        t.Errorf("expected 0 failures after clear, got %d", s.remoteTokenFailures)
    }
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/dashboard/ -run TestHandleTunnelConnected -count=1
```

Expected: FAIL (fields don't exist yet)

**Step 3: Implement**

Add to `Server` struct in `internal/dashboard/server.go`:

```go
// Remote access auth state
remoteToken         string
remoteTokenFailures int
remoteTokenMu       sync.Mutex
remoteSessionSecret []byte
```

Replace the stub `HandleTunnelConnected` with the full implementation:

```go
func (s *Server) HandleTunnelConnected(tunnelURL string) {
    // Generate one-time token (32 bytes, hex-encoded)
    tokenBytes := make([]byte, 32)
    if _, err := crypto_rand.Read(tokenBytes); err != nil {
        fmt.Printf("[remote-access] failed to generate token: %v\n", err)
        return
    }
    token := hex.EncodeToString(tokenBytes)

    // Generate new session secret (32 bytes) — invalidates old remote cookies
    secretBytes := make([]byte, 32)
    if _, err := crypto_rand.Read(secretBytes); err != nil {
        fmt.Printf("[remote-access] failed to generate session secret: %v\n", err)
        return
    }

    s.remoteTokenMu.Lock()
    s.remoteToken = token
    s.remoteTokenFailures = 0
    s.remoteSessionSecret = secretBytes
    s.remoteTokenMu.Unlock()

    // Build auth URL
    authURL := strings.TrimRight(tunnelURL, "/") + "/remote-auth?token=" + token
    fmt.Printf("[remote-access] auth URL: %s\n", authURL)

    // Send notifications
    if s.config != nil {
        ntfyTopic := s.config.GetRemoteAccessNtfyTopic()
        notifyCmd := s.config.GetRemoteAccessNotifyCommand()
        nc := tunnel.NotifyConfig{}
        if ntfyTopic != "" {
            nc.NtfyURL = "https://ntfy.sh/" + ntfyTopic
        }
        nc.Command = notifyCmd
        if err := nc.Send(authURL, "schmux remote access"); err != nil {
            fmt.Printf("[remote-access] notification error: %v\n", err)
        }
    }
}
```

Add `ClearRemoteAuth` method:

```go
func (s *Server) ClearRemoteAuth() {
    s.remoteTokenMu.Lock()
    s.remoteToken = ""
    s.remoteTokenFailures = 0
    s.remoteTokenMu.Unlock()
}
```

Add imports: `crypto/rand` (aliased as `crypto_rand`), `encoding/hex`.

**Step 4: Run tests**

```bash
go test ./internal/dashboard/ -run TestHandleTunnelConnected -count=1
go test ./internal/dashboard/ -run TestClearRemoteAuth -count=1
```

Expected: PASS

**Step 5: Commit**

```bash
git commit -am "Add remote auth state and HandleTunnelConnected to server"
```

---

### Task 5: Clear remote auth on tunnel stop

When the tunnel stops, call `ClearRemoteAuth` to invalidate tokens and remote cookies.

**Files:**

- Modify: `internal/dashboard/server.go:832-855` (BroadcastTunnelStatus)

**Step 1: Add cleanup to BroadcastTunnelStatus**

In `BroadcastTunnelStatus`, after broadcasting to WebSocket clients, add:

```go
// Clear remote auth state when tunnel goes off or errors
if status.State == tunnel.StateOff || status.State == tunnel.StateError {
    s.ClearRemoteAuth()
}
```

**Step 2: Verify build and tests**

```bash
go build ./cmd/schmux && go test ./internal/dashboard/ -count=1
```

Expected: PASS

**Step 3: Commit**

```bash
git commit -am "Clear remote auth state when tunnel stops"
```

---

### Task 6: PIN entry page handler (GET /remote-auth)

Serve a self-contained HTML PIN entry page when the user opens the auth URL from their notification.

**Files:**

- Create: `internal/dashboard/handlers_remote_auth.go`
- Test: add to `internal/dashboard/handlers_remote_access_test.go`

**Step 1: Write the failing test**

Add to `internal/dashboard/handlers_remote_access_test.go`:

```go
func TestHandleRemoteAuthGET_ValidToken(t *testing.T) {
    s := &Server{}
    s.HandleTunnelConnected("https://test.trycloudflare.com")

    s.remoteTokenMu.Lock()
    token := s.remoteToken
    s.remoteTokenMu.Unlock()

    req := httptest.NewRequest("GET", "/remote-auth?token="+token, nil)
    w := httptest.NewRecorder()
    s.handleRemoteAuth(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("expected 200, got %d", w.Code)
    }
    if !strings.Contains(w.Body.String(), "<form") {
        t.Error("expected HTML form in response")
    }
    if !strings.Contains(w.Body.String(), token) {
        t.Error("expected token in hidden form field")
    }
}

func TestHandleRemoteAuthGET_InvalidToken(t *testing.T) {
    s := &Server{}
    s.HandleTunnelConnected("https://test.trycloudflare.com")

    req := httptest.NewRequest("GET", "/remote-auth?token=badtoken", nil)
    w := httptest.NewRecorder()
    s.handleRemoteAuth(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("expected 200, got %d", w.Code)
    }
    if !strings.Contains(w.Body.String(), "Invalid") {
        t.Error("expected error message about invalid token")
    }
}

func TestHandleRemoteAuthGET_NoToken(t *testing.T) {
    s := &Server{}
    req := httptest.NewRequest("GET", "/remote-auth", nil)
    w := httptest.NewRecorder()
    s.handleRemoteAuth(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("expected 200, got %d", w.Code)
    }
    body := w.Body.String()
    if !strings.Contains(body, "Invalid") || !strings.Contains(body, "expired") {
        t.Error("expected error message about invalid/expired token")
    }
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/dashboard/ -run TestHandleRemoteAuth -count=1
```

Expected: FAIL (handler doesn't exist)

**Step 3: Implement**

Create `internal/dashboard/handlers_remote_auth.go` with:

```go
package dashboard

import (
    "fmt"
    "net/http"
)

const maxPinAttempts = 5

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
```

Add `renderPinPage` function that returns a self-contained HTML string with:

- Inline CSS, dark/light mode via `prefers-color-scheme`
- Hidden `token` field in form
- `pin` text input
- Error message display area
- Remaining attempts display
- POST to `/remote-auth`

```go
func renderPinPage(token string, errorMsg string, attemptsRemaining int) string {
    // ... returns full HTML string with inline styles, form, etc.
}
```

**Step 4: Run tests**

```bash
go test ./internal/dashboard/ -run TestHandleRemoteAuth -count=1
```

Expected: PASS

**Step 5: Commit**

```bash
git commit -am "Add PIN entry page handler (GET /remote-auth)"
```

---

### Task 7: PIN validation handler (POST /remote-auth)

Validate token + PIN, set session cookie on success, handle failures and lockout.

**Files:**

- Modify: `internal/dashboard/handlers_remote_auth.go` (add handleRemoteAuthPOST)
- Test: add to `internal/dashboard/handlers_remote_access_test.go`

**Step 1: Write the failing tests**

Add to test file:

```go
func TestHandleRemoteAuthPOST_CorrectPin(t *testing.T) {
    cfg := &config.Config{}
    // Set bcrypt hash of "test-pin"
    hash, _ := bcrypt.GenerateFromPassword([]byte("test-pin"), bcrypt.DefaultCost)
    cfg.RemoteAccess = &config.RemoteAccessConfig{PinHash: string(hash)}

    s := &Server{config: cfg}
    s.HandleTunnelConnected("https://test.trycloudflare.com")

    s.remoteTokenMu.Lock()
    token := s.remoteToken
    s.remoteTokenMu.Unlock()

    form := url.Values{"token": {token}, "pin": {"test-pin"}}
    req := httptest.NewRequest("POST", "/remote-auth", strings.NewReader(form.Encode()))
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    w := httptest.NewRecorder()
    s.handleRemoteAuth(w, req)

    if w.Code != http.StatusFound {
        t.Errorf("expected 302 redirect, got %d", w.Code)
    }
    // Check cookie was set
    cookies := w.Result().Cookies()
    found := false
    for _, c := range cookies {
        if c.Name == "schmux_remote" {
            found = true
        }
    }
    if !found {
        t.Error("expected schmux_remote cookie to be set")
    }
    // Token should be cleared after successful auth
    s.remoteTokenMu.Lock()
    if s.remoteToken != "" {
        t.Error("expected token to be cleared after successful auth")
    }
    s.remoteTokenMu.Unlock()
}

func TestHandleRemoteAuthPOST_WrongPin(t *testing.T) {
    cfg := &config.Config{}
    hash, _ := bcrypt.GenerateFromPassword([]byte("correct-pin"), bcrypt.DefaultCost)
    cfg.RemoteAccess = &config.RemoteAccessConfig{PinHash: string(hash)}

    s := &Server{config: cfg}
    s.HandleTunnelConnected("https://test.trycloudflare.com")

    s.remoteTokenMu.Lock()
    token := s.remoteToken
    s.remoteTokenMu.Unlock()

    form := url.Values{"token": {token}, "pin": {"wrong-pin"}}
    req := httptest.NewRequest("POST", "/remote-auth", strings.NewReader(form.Encode()))
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    w := httptest.NewRecorder()
    s.handleRemoteAuth(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("expected 200 (re-rendered form), got %d", w.Code)
    }
    body := w.Body.String()
    if !strings.Contains(body, "Incorrect") {
        t.Error("expected error message about incorrect PIN")
    }

    s.remoteTokenMu.Lock()
    if s.remoteTokenFailures != 1 {
        t.Errorf("expected 1 failure, got %d", s.remoteTokenFailures)
    }
    s.remoteTokenMu.Unlock()
}

func TestHandleRemoteAuthPOST_Lockout(t *testing.T) {
    cfg := &config.Config{}
    hash, _ := bcrypt.GenerateFromPassword([]byte("correct-pin"), bcrypt.DefaultCost)
    cfg.RemoteAccess = &config.RemoteAccessConfig{PinHash: string(hash)}

    s := &Server{config: cfg}
    s.HandleTunnelConnected("https://test.trycloudflare.com")

    s.remoteTokenMu.Lock()
    token := s.remoteToken
    s.remoteTokenFailures = maxPinAttempts - 1 // One attempt remaining
    s.remoteTokenMu.Unlock()

    form := url.Values{"token": {token}, "pin": {"wrong-pin"}}
    req := httptest.NewRequest("POST", "/remote-auth", strings.NewReader(form.Encode()))
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
    w := httptest.NewRecorder()
    s.handleRemoteAuth(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("expected 200, got %d", w.Code)
    }
    body := w.Body.String()
    if !strings.Contains(body, "locked") || !strings.Contains(body, "Too many") {
        t.Error("expected lockout message")
    }

    // Token should be invalidated
    s.remoteTokenMu.Lock()
    if s.remoteToken != "" {
        t.Error("expected token to be invalidated after lockout")
    }
    s.remoteTokenMu.Unlock()
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/dashboard/ -run TestHandleRemoteAuthPOST -count=1
```

Expected: FAIL

**Step 3: Implement handleRemoteAuthPOST**

```go
func (s *Server) handleRemoteAuthPOST(w http.ResponseWriter, r *http.Request) {
    token := r.FormValue("token")
    pin := r.FormValue("pin")

    s.remoteTokenMu.Lock()
    validToken := s.remoteToken
    failures := s.remoteTokenFailures

    // Check token
    if token == "" || validToken == "" || token != validToken {
        s.remoteTokenMu.Unlock()
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        fmt.Fprint(w, renderPinPage("", "Invalid or expired link.", 0))
        return
    }

    // Check lockout
    if failures >= maxPinAttempts {
        s.remoteToken = "" // invalidate
        s.remoteTokenMu.Unlock()
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        fmt.Fprint(w, renderPinPage("", "Too many failed attempts. This link has been locked.", 0))
        return
    }
    s.remoteTokenMu.Unlock()

    // Get PIN hash from config
    pinHash := s.config.GetRemoteAccessPinHash()
    if pinHash == "" {
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        fmt.Fprint(w, renderPinPage("", "PIN not configured on server.", 0))
        return
    }

    // Verify PIN with bcrypt
    err := bcrypt.CompareHashAndPassword([]byte(pinHash), []byte(pin))
    if err != nil {
        s.remoteTokenMu.Lock()
        s.remoteTokenFailures++
        newFailures := s.remoteTokenFailures
        if newFailures >= maxPinAttempts {
            s.remoteToken = "" // lockout
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
```

Add cookie helper:

```go
func (s *Server) setRemoteSessionCookie(w http.ResponseWriter, secret []byte) {
    // HMAC-sign current timestamp
    now := fmt.Sprintf("%d", time.Now().Unix())
    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(now))
    sig := hex.EncodeToString(mac.Sum(nil))

    http.SetCookie(w, &http.Cookie{
        Name:     "schmux_remote",
        Value:    now + "." + sig,
        Path:     "/",
        HttpOnly: true,
        Secure:   true,
        SameSite: http.SameSiteLaxMode,
    })
}
```

**Step 4: Run tests**

```bash
go test ./internal/dashboard/ -run TestHandleRemoteAuthPOST -count=1
```

Expected: PASS

**Step 5: Commit**

```bash
git commit -am "Add PIN validation handler with brute-force lockout"
```

---

### Task 8: Add remote cookie validation to auth middleware

Modify `authenticateRequest` to accept `schmux_remote` cookies alongside GitHub OAuth cookies.

**Files:**

- Modify: `internal/dashboard/auth.go:115-121` (authenticateRequest)
- Test: add to `internal/dashboard/handlers_remote_access_test.go`

**Step 1: Write the failing test**

```go
func TestAuthenticateRequest_RemoteCookie(t *testing.T) {
    s := &Server{}
    s.HandleTunnelConnected("https://test.trycloudflare.com")

    s.remoteTokenMu.Lock()
    secret := s.remoteSessionSecret
    s.remoteTokenMu.Unlock()

    // Create a valid remote cookie
    now := fmt.Sprintf("%d", time.Now().Unix())
    mac := hmac.New(sha256.New, secret)
    mac.Write([]byte(now))
    sig := hex.EncodeToString(mac.Sum(nil))
    cookieValue := now + "." + sig

    req := httptest.NewRequest("GET", "/api/sessions", nil)
    req.AddCookie(&http.Cookie{Name: "schmux_remote", Value: cookieValue})

    _, err := s.authenticateRequest(req)
    if err != nil {
        t.Errorf("expected remote cookie to be valid, got: %v", err)
    }
}

func TestAuthenticateRequest_ExpiredRemoteCookie(t *testing.T) {
    s := &Server{}
    s.HandleTunnelConnected("https://test.trycloudflare.com")

    s.remoteTokenMu.Lock()
    secret := s.remoteSessionSecret
    s.remoteTokenMu.Unlock()

    // Invalidate by changing secret (simulates tunnel restart)
    s.ClearRemoteAuth()
    s.HandleTunnelConnected("https://new-tunnel.trycloudflare.com")

    // Old cookie should fail
    now := fmt.Sprintf("%d", time.Now().Unix())
    mac := hmac.New(sha256.New, secret) // old secret
    mac.Write([]byte(now))
    sig := hex.EncodeToString(mac.Sum(nil))
    cookieValue := now + "." + sig

    req := httptest.NewRequest("GET", "/api/sessions", nil)
    req.AddCookie(&http.Cookie{Name: "schmux_remote", Value: cookieValue})

    _, err := s.authenticateRequest(req)
    if err == nil {
        t.Error("expected old remote cookie to be rejected")
    }
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/dashboard/ -run TestAuthenticateRequest_Remote -count=1
```

Expected: FAIL

**Step 3: Implement**

Modify `authenticateRequest` in `internal/dashboard/auth.go`:

```go
func (s *Server) authenticateRequest(r *http.Request) (*authSession, error) {
    // Try GitHub OAuth cookie first
    cookie, err := r.Cookie(authCookieName)
    if err == nil {
        return s.parseSessionCookie(cookie.Value)
    }

    // Try remote session cookie
    remoteCookie, err := r.Cookie("schmux_remote")
    if err == nil {
        if s.validateRemoteCookie(remoteCookie.Value) {
            return &authSession{Login: "remote"}, nil
        }
    }

    return nil, errors.New("no valid auth session")
}
```

Add `validateRemoteCookie`:

```go
func (s *Server) validateRemoteCookie(value string) bool {
    parts := strings.SplitN(value, ".", 2)
    if len(parts) != 2 {
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
```

**Step 4: Run tests**

```bash
go test ./internal/dashboard/ -run TestAuthenticateRequest_Remote -count=1
```

Expected: PASS

**Step 5: Commit**

```bash
git commit -am "Add remote cookie validation to auth middleware"
```

---

### Task 9: Register /remote-auth route (unauthenticated)

**Files:**

- Modify: `internal/dashboard/server.go:274-350` (route registration in Start())

**Step 1: Add the route**

In the `Start()` method, add the `/remote-auth` route BEFORE the auth routes. It must NOT be wrapped with `withAuth`:

```go
// Remote auth route (unauthenticated — token-protected)
mux.HandleFunc("/remote-auth", s.handleRemoteAuth)
```

Place it right after the static asset routes (around line 288) and before the `/auth/` routes.

**Step 2: Verify build and tests**

```bash
go build ./cmd/schmux && go test ./internal/dashboard/ -count=1
```

Expected: PASS

**Step 3: Commit**

```bash
git commit -am "Register /remote-auth route (unauthenticated, token-protected)"
```

---

### Task 10: Set-PIN endpoint (POST /api/remote-access/set-pin)

**Files:**

- Modify: `internal/dashboard/handlers_remote_access.go` (add handleRemoteAccessSetPin)
- Modify: `internal/dashboard/server.go` (register route)
- Test: add to `internal/dashboard/handlers_remote_access_test.go`

**Step 1: Write the failing test**

```go
func TestHandleRemoteAccessSetPin(t *testing.T) {
    tmpDir := t.TempDir()
    configPath := filepath.Join(tmpDir, "config.json")
    cfg := config.CreateDefault(configPath)
    cfg.RemoteAccess = &config.RemoteAccessConfig{}

    s := &Server{config: cfg}

    body := `{"pin":"my-secret-pin"}`
    req := httptest.NewRequest("POST", "/api/remote-access/set-pin", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    s.handleRemoteAccessSetPin(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
    }

    // Verify PIN hash was stored
    hash := cfg.GetRemoteAccessPinHash()
    if hash == "" {
        t.Fatal("expected pin_hash to be set")
    }

    // Verify bcrypt comparison works
    err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("my-secret-pin"))
    if err != nil {
        t.Errorf("bcrypt compare failed: %v", err)
    }
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/dashboard/ -run TestHandleRemoteAccessSetPin -count=1
```

Expected: FAIL

**Step 3: Implement**

In `internal/dashboard/handlers_remote_access.go`, add:

```go
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
    if req.Pin == "" {
        http.Error(w, "PIN cannot be empty", http.StatusBadRequest)
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
```

Register route in `server.go` Start():

```go
mux.HandleFunc("/api/remote-access/set-pin", s.withCORS(s.withAuth(s.handleRemoteAccessSetPin)))
```

Add bcrypt import to `handlers_remote_access.go`.

**Step 4: Run tests**

```bash
go test ./internal/dashboard/ -run TestHandleRemoteAccessSetPin -count=1
```

Expected: PASS

**Step 5: Commit**

```bash
git commit -am "Add set-pin endpoint for remote access PIN configuration"
```

---

### Task 11: Update config handler to include pin_hash_set

**Files:**

- Modify: `internal/dashboard/handlers.go` (handleConfigGet — update RemoteAccess field)

**Step 1: Update handleConfigGet**

Find where `RemoteAccess` is populated in the config response and add `PinHashSet`:

```go
RemoteAccess: contracts.RemoteAccess{
    Disabled:       cfg.GetRemoteAccessDisabled(),
    TimeoutMinutes: cfg.GetRemoteAccessTimeoutMinutes(),
    PinHashSet:     cfg.GetRemoteAccessPinHash() != "",
    Notify: contracts.RemoteAccessNotify{
        NtfyTopic: cfg.GetRemoteAccessNtfyTopic(),
        Command:   cfg.GetRemoteAccessNotifyCommand(),
    },
},
```

**Step 2: Regenerate TypeScript types**

```bash
go run ./cmd/gen-types
```

**Step 3: Verify build**

```bash
go build ./cmd/schmux
```

Expected: BUILD SUCCESS

**Step 4: Commit**

```bash
git commit -am "Add pin_hash_set to config API response"
```

---

### Task 12: CLI set-pin command

**Files:**

- Modify: `cmd/schmux/remote.go` (add set-pin case)
- Modify: `pkg/cli/daemon_client.go` (add RemoteAccessSetPin method)

**Step 1: Add client method**

In `pkg/cli/daemon_client.go`, add:

```go
func (c *Client) RemoteAccessSetPin(pin string) error {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    body := fmt.Sprintf(`{"pin":%q}`, pin)
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/remote-access/set-pin", strings.NewReader(body))
    if err != nil {
        return fmt.Errorf("failed to create request: %w", err)
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return fmt.Errorf("failed to connect to daemon: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        respBody, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("%s", strings.TrimSpace(string(respBody)))
    }
    return nil
}
```

**Step 2: Add CLI command**

In `cmd/schmux/remote.go`, add case to the switch:

```go
case "set-pin":
    fmt.Print("Enter PIN for remote access: ")
    pin1, err := readPassword()
    if err != nil {
        return fmt.Errorf("failed to read PIN: %w", err)
    }
    fmt.Print("Confirm PIN: ")
    pin2, err := readPassword()
    if err != nil {
        return fmt.Errorf("failed to read PIN: %w", err)
    }
    if pin1 != pin2 {
        return fmt.Errorf("PINs do not match")
    }
    if len(pin1) < 4 {
        return fmt.Errorf("PIN must be at least 4 characters")
    }
    if err := cmd.client.RemoteAccessSetPin(pin1); err != nil {
        return fmt.Errorf("failed to set PIN: %w", err)
    }
    fmt.Println("PIN set successfully")
```

Add `readPassword` helper that reads a line from stdin (use `golang.org/x/term` for hidden input, or fall back to `bufio` reader):

```go
func readPassword() (string, error) {
    fd := int(os.Stdin.Fd())
    if term.IsTerminal(fd) {
        pass, err := term.ReadPassword(fd)
        fmt.Println() // newline after hidden input
        if err != nil {
            return "", err
        }
        return strings.TrimSpace(string(pass)), nil
    }
    reader := bufio.NewReader(os.Stdin)
    line, err := reader.ReadString('\n')
    if err != nil {
        return "", err
    }
    return strings.TrimSpace(line), nil
}
```

Update usage string to include `set-pin`:

```go
fmt.Println("Usage: schmux remote <on|off|status|set-pin>")
```

**Step 3: Add `golang.org/x/term` dependency (already in golang.org/x/crypto tree)**

```bash
go get golang.org/x/term
```

**Step 4: Verify build**

```bash
go build ./cmd/schmux
```

Expected: BUILD SUCCESS

**Step 5: Commit**

```bash
git commit -am "Add 'schmux remote set-pin' CLI command"
```

---

### Task 13: Update dashboard RemoteAccessPanel for PIN status

**Files:**

- Modify: `assets/dashboard/src/components/RemoteAccessPanel.tsx`

**Step 1: Update the panel**

Replace the `authEnabled` check with `pinHashSet`:

```tsx
const pinConfigured = config?.remote_access?.pin_hash_set;
```

Update the warning message:

```tsx
{
  !pinConfigured && remoteAccessStatus.state === 'off' && (
    <div className="remote-access-panel__warning">
      PIN not set — run <code>schmux remote set-pin</code>
    </div>
  );
}
```

Disable the Start button when PIN is not configured:

```tsx
disabled={loading || remoteAccessStatus.state === 'starting' || (!pinConfigured && !isActive)}
```

**Step 2: Verify build**

```bash
go run ./cmd/build-dashboard
```

Expected: BUILD SUCCESS

**Step 3: Commit**

```bash
git commit -am "Update RemoteAccessPanel to show PIN status instead of auth requirement"
```

---

### Task 14: Update API docs

**Files:**

- Modify: `docs/api.md`

**Step 1: Update API docs**

Add documentation for:

- `GET /remote-auth?token=<token>` — PIN entry page (unauthenticated)
- `POST /remote-auth` — PIN validation (unauthenticated, token-protected)
- `POST /api/remote-access/set-pin` — Set PIN (authenticated)
- Update `remote_access` config response to include `pin_hash_set`
- Document `schmux_remote` cookie in the auth section

**Step 2: Commit**

```bash
git commit -am "Document remote access auth endpoints in API docs"
```

---

### Task 15: Integration test — full build + test suite

**Step 1: Build Go binary**

```bash
go build ./cmd/schmux
```

Expected: BUILD SUCCESS

**Step 2: Build dashboard**

```bash
go run ./cmd/build-dashboard
```

Expected: BUILD SUCCESS

**Step 3: Run all Go tests**

```bash
go test ./...
```

Expected: PASS (all tests including new remote auth tests)

**Step 4: Run full test suite**

```bash
./test.sh
```

Expected: PASS
