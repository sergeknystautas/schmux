# Remote Cookie UA Binding Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Bind the `schmux_remote` session cookie to the client's User-Agent to prevent stolen cookies from being replayed on a different device, and reduce the TTL from 24h to 12h.

**Architecture:** Embed a truncated SHA-256 hash of the User-Agent in the cookie payload. The HMAC signature covers both the timestamp and the UA hash, preventing tampering. Validation recomputes the UA hash from the incoming request and rejects mismatches.

**Tech Stack:** Go stdlib (`crypto/sha256`, `crypto/hmac`), existing test infrastructure.

---

### Task 1: Update cookie format and validation

**Files:**

- Modify: `internal/dashboard/handlers_remote_auth.go:25` (TTL constant)
- Modify: `internal/dashboard/handlers_remote_auth.go:243-257` (`setRemoteSessionCookie`)
- Modify: `internal/dashboard/handlers_remote_auth.go:275-303` (`validateRemoteCookie`)
- Modify: `internal/dashboard/handlers_remote_auth.go:239` (caller passes request)
- Modify: `internal/dashboard/auth.go:162-175` (`authenticateRequest` passes request to validate)

**Step 1: Write failing tests for UA binding**

Add these tests to `internal/dashboard/handlers_remote_auth_test.go`:

```go
func TestValidateRemoteCookie_RejectsWrongUA(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}, nil))
	defer server.CloseForTest()
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	// Create a cookie bound to one UA
	server.remoteTokenMu.Lock()
	secret := server.remoteSessionSecret
	server.remoteTokenMu.Unlock()

	originalUA := "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)"
	cookieValue := makeRemoteCookieWithUA(secret, originalUA)

	// Same UA: should pass
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("User-Agent", originalUA)
	if !server.validateRemoteCookie(cookieValue, req) {
		t.Error("expected cookie to be accepted with matching UA")
	}

	// Different UA: should fail
	req2, _ := http.NewRequest("GET", "/", nil)
	req2.Header.Set("User-Agent", "curl/7.88.0")
	if server.validateRemoteCookie(cookieValue, req2) {
		t.Error("expected cookie to be rejected with different UA")
	}
}

func TestValidateRemoteCookie_EmptyUA(t *testing.T) {
	server := newTestServerWithTunnel(t, tunnel.NewManager(tunnel.ManagerConfig{}, nil))
	defer server.CloseForTest()
	server.HandleTunnelConnected("https://test.trycloudflare.com")

	server.remoteTokenMu.Lock()
	secret := server.remoteSessionSecret
	server.remoteTokenMu.Unlock()

	// Cookie created with empty UA
	cookieValue := makeRemoteCookieWithUA(secret, "")

	// Empty UA request: should pass (bound to "none" sentinel)
	req, _ := http.NewRequest("GET", "/", nil)
	// No User-Agent header set
	if !server.validateRemoteCookie(cookieValue, req) {
		t.Error("expected cookie to be accepted when both creation and validation have empty UA")
	}

	// Non-empty UA request: should fail
	req2, _ := http.NewRequest("GET", "/", nil)
	req2.Header.Set("User-Agent", "Mozilla/5.0")
	if server.validateRemoteCookie(cookieValue, req2) {
		t.Error("expected cookie created with empty UA to be rejected when request has a UA")
	}
}

func TestRemoteSessionMaxAge_Is12Hours(t *testing.T) {
	if remoteSessionMaxAge != 12*time.Hour {
		t.Errorf("expected remoteSessionMaxAge to be 12h, got %v", remoteSessionMaxAge)
	}
}
```

Add this test helper (in the test file):

```go
// makeRemoteCookieWithUA creates a remote cookie value bound to the given User-Agent.
func makeRemoteCookieWithUA(secret []byte, userAgent string) string {
	nowStr := fmt.Sprintf("%d", time.Now().Unix())
	uaHash := uaFingerprint(userAgent)
	payload := nowStr + "." + uaHash
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/dashboard/ -run "TestValidateRemoteCookie_RejectsWrongUA|TestValidateRemoteCookie_EmptyUA|TestRemoteSessionMaxAge_Is12Hours" -count=1
```

Expected: compilation errors (new signature, `uaFingerprint` doesn't exist yet).

**Step 3: Implement the changes**

In `internal/dashboard/handlers_remote_auth.go`:

1. Change the TTL constant (line 25):

```go
const remoteSessionMaxAge = 12 * time.Hour
```

2. Add the `uaFingerprint` helper:

```go
// uaFingerprint returns the first 16 hex chars of SHA-256(userAgent).
// If userAgent is empty, it uses the sentinel "none" so the cookie is
// still bound (to "requests with no UA") rather than unbound.
func uaFingerprint(userAgent string) string {
	if userAgent == "" {
		userAgent = "none"
	}
	h := sha256.Sum256([]byte(userAgent))
	return hex.EncodeToString(h[:])[:16]
}
```

3. Update `setRemoteSessionCookie` (line 243) to accept `*http.Request` and embed the UA hash:

```go
func (s *Server) setRemoteSessionCookie(w http.ResponseWriter, r *http.Request, secret []byte) {
	now := fmt.Sprintf("%d", time.Now().Unix())
	uaHash := uaFingerprint(r.UserAgent())
	payload := now + "." + uaHash
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	http.SetCookie(w, &http.Cookie{
		Name:     "schmux_remote",
		Value:    payload + "." + sig,
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
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
	})
}
```

4. Update the caller in `handleRemoteAuthPOST` (line 239):

```go
s.setRemoteSessionCookie(w, r, secret)
```

5. Update `validateRemoteCookie` (line 275) to accept `*http.Request` and check UA:

```go
func (s *Server) validateRemoteCookie(value string, r *http.Request) bool {
	parts := strings.SplitN(value, ".", 3)
	if len(parts) != 3 {
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

	// Check UA fingerprint
	if parts[1] != uaFingerprint(r.UserAgent()) {
		return false
	}

	s.remoteTokenMu.Lock()
	secret := s.remoteSessionSecret
	s.remoteTokenMu.Unlock()

	if len(secret) == 0 {
		return false
	}

	// HMAC covers "timestamp.uaHash"
	payload := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(parts[2]), []byte(expected))
}
```

6. Update `authenticateRequest` in `auth.go` (line 172) to pass the request:

```go
if s.validateRemoteCookie(remoteCookie.Value, r) {
```

**Step 4: Run the new tests to verify they pass**

```bash
go test ./internal/dashboard/ -run "TestValidateRemoteCookie_RejectsWrongUA|TestValidateRemoteCookie_EmptyUA|TestRemoteSessionMaxAge_Is12Hours" -count=1
```

Expected: PASS

**Step 5: Commit**

```bash
git commit -m "feat(security): bind remote cookie to User-Agent and reduce TTL to 12h"
```

---

### Task 2: Update existing tests for new function signatures

**Files:**

- Modify: `internal/dashboard/handlers_remote_auth_test.go`
- Modify: `internal/dashboard/tunnel_e2e_test.go`

All existing tests that call `validateRemoteCookie(value)` need to pass an `*http.Request` as the second argument. All tests that construct cookies manually (using the old 2-part format) need to use the new 3-part format.

**Step 1: Update `handlers_remote_auth_test.go`**

For every test that calls `server.validateRemoteCookie(cookieValue)`, change to `server.validateRemoteCookie(cookieValue, req)` where `req` has a User-Agent header set. For tests that construct cookies manually, use `makeRemoteCookieWithUA`.

Affected tests and what to change:

- `TestValidateRemoteCookie_ExpiredCookie` (line 20): Use `makeRemoteCookieWithUA` with a past timestamp, pass request with matching UA. Since this test needs a custom timestamp (25h in the past), create the cookie manually with the 3-part format:

  ```go
  uaHash := uaFingerprint("TestAgent")
  payload := oldTimestamp + "." + uaHash
  mac.Write([]byte(payload))
  // ...
  cookieValue := payload + "." + sig
  req, _ := http.NewRequest("GET", "/", nil)
  req.Header.Set("User-Agent", "TestAgent")
  server.validateRemoteCookie(cookieValue, req)
  ```

- `TestValidateRemoteCookie_FreshCookie` (line 43): Same approach with current timestamp.

- `TestClearRemoteAuth_InvalidatesCookies` (line 65): Same approach.

- `TestRemoteAccessOff_RequiresCSRFWhenRemoteSession` (line 157): Update the cookie construction in the test to 3-part format. The cookie is added to a request that already has `RemoteAddr` set — also add a User-Agent header and construct the cookie with that UA.

- `TestRemoteAccessOff_AllowsLocalRequestWithoutCSRF` (line 217): Same as above.

- `TestCSRF_RequiredForTunneledRequests` (line 613): Same as above.

- `TestSetPassword_InvalidatesExistingSessions` (line 783): Same as above.

**Step 2: Update `tunnel_e2e_test.go`**

Update `makeRemoteCookie` helper (line 118) to produce the new 3-part format. Since the helper is used in tests that then call `validateRemoteCookie`, it also needs to accept a UA string, or use a default. Add a request parameter or use a default UA.

Simplest approach: give `makeRemoteCookie` a default UA and update `validateRemoteCookie` calls to pass a request with that UA:

```go
const testUserAgent = "TestAgent/1.0"

func (tts *tunnelTestServer) makeRemoteCookie(t *testing.T) string {
	t.Helper()
	nowStr := fmt.Sprintf("%d", time.Now().Unix())
	tts.server.remoteTokenMu.Lock()
	secret := tts.server.remoteSessionSecret
	tts.server.remoteTokenMu.Unlock()

	uaHash := uaFingerprint(testUserAgent)
	payload := nowStr + "." + uaHash
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}
```

Update callers of `validateRemoteCookie` in `tunnel_e2e_test.go` (lines 487, 496, 502) to pass a request with `testUserAgent`.

**Step 3: Run all tests**

```bash
go test ./internal/dashboard/ -count=1
```

Expected: PASS (all existing + new tests)

**Step 4: Commit**

```bash
git commit -m "test: update remote auth tests for UA-bound cookie format"
```

---

### Task 3: Run full test suite

**Step 1: Run the full test suite**

```bash
./test.sh --quick
```

Expected: PASS

**Step 2: If there are failures, fix them and re-run**

Any remaining callers of the old signatures will surface as compilation errors, which are straightforward to fix.
