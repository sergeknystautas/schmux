# HTTPS Config UI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make HTTPS a first-class, independent config option in the Access tab with server-side certificate validation.

**Architecture:** Add a `POST /api/tls/validate` endpoint that validates cert/key paths and extracts hostname. Restructure the Access tab into a cascading layout (Network → HTTPS → GitHub Auth) plus independent Remote Access. Decouple TLS from auth in server startup.

**Tech Stack:** Go (crypto/x509, crypto/tls), React/TypeScript, existing ModalProvider pattern

---

### Task 1: Add TLS validation endpoint — Go contract types

**Files:**

- Modify: `internal/api/contracts/config.go` (after line 104, the TLS type)

**Step 1: Add the request/response types**

Add after the existing `TLSUpdate` struct:

```go
// TLSValidateRequest is the request body for POST /api/tls/validate.
type TLSValidateRequest struct {
	CertPath string `json:"cert_path"`
	KeyPath  string `json:"key_path"`
}

// TLSValidateResponse is the response from POST /api/tls/validate.
type TLSValidateResponse struct {
	Valid    bool   `json:"valid"`
	Hostname string `json:"hostname,omitempty"`
	Expires  string `json:"expires,omitempty"`
	Error    string `json:"error,omitempty"`
}
```

**Step 2: Regenerate TypeScript types**

Run: `go run ./cmd/gen-types`

**Step 3: Commit**

```bash
git add internal/api/contracts/config.go assets/dashboard/src/lib/types.generated.ts
git commit -m "feat: add TLS validation request/response contract types"
```

---

### Task 2: Add TLS validation handler — Go backend

**Files:**

- Create: `internal/dashboard/handlers_tls.go`
- Modify: `internal/dashboard/server.go` (route registration, ~line 391)

**Step 1: Write the handler test**

Create `internal/dashboard/handlers_tls_test.go`:

```go
package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

func TestHandleTLSValidate_MissingPaths(t *testing.T) {
	s := &Server{}
	body, _ := json.Marshal(contracts.TLSValidateRequest{CertPath: "", KeyPath: ""})
	req := httptest.NewRequest(http.MethodPost, "/api/tls/validate", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleTLSValidate(w, req)

	var resp contracts.TLSValidateResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Valid {
		t.Error("expected invalid for empty paths")
	}
	if resp.Error == "" {
		t.Error("expected error message")
	}
}

func TestHandleTLSValidate_NonexistentFiles(t *testing.T) {
	s := &Server{}
	body, _ := json.Marshal(contracts.TLSValidateRequest{
		CertPath: "/nonexistent/cert.pem",
		KeyPath:  "/nonexistent/key.pem",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tls/validate", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleTLSValidate(w, req)

	var resp contracts.TLSValidateResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Valid {
		t.Error("expected invalid for nonexistent files")
	}
}

func TestHandleTLSValidate_MethodNotAllowed(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/tls/validate", nil)
	w := httptest.NewRecorder()

	s.handleTLSValidate(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/dashboard/ -run TestHandleTLSValidate -v`
Expected: FAIL (method not found)

**Step 3: Write the handler**

Create `internal/dashboard/handlers_tls.go`:

```go
package dashboard

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// handleTLSValidate validates TLS certificate and key paths.
func (s *Server) handleTLSValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req contracts.TLSValidateRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, contracts.TLSValidateResponse{Error: "Invalid request body"})
		return
	}

	certPath := expandHome(req.CertPath)
	keyPath := expandHome(req.KeyPath)

	if certPath == "" || keyPath == "" {
		writeJSON(w, contracts.TLSValidateResponse{Error: "Both cert_path and key_path are required"})
		return
	}

	// Check files exist
	if _, err := os.Stat(certPath); err != nil {
		writeJSON(w, contracts.TLSValidateResponse{Error: fmt.Sprintf("Certificate file not found: %s", req.CertPath)})
		return
	}
	if _, err := os.Stat(keyPath); err != nil {
		writeJSON(w, contracts.TLSValidateResponse{Error: fmt.Sprintf("Key file not found: %s", req.KeyPath)})
		return
	}

	// Validate cert+key pair
	_, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		writeJSON(w, contracts.TLSValidateResponse{Error: fmt.Sprintf("Certificate and key do not match: %v", err)})
		return
	}

	// Parse cert to extract hostname and expiry
	certData, err := os.ReadFile(certPath)
	if err != nil {
		writeJSON(w, contracts.TLSValidateResponse{Error: fmt.Sprintf("Cannot read certificate: %v", err)})
		return
	}

	block, _ := pem.Decode(certData)
	if block == nil {
		writeJSON(w, contracts.TLSValidateResponse{Error: "No PEM block found in certificate file"})
		return
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		writeJSON(w, contracts.TLSValidateResponse{Error: fmt.Sprintf("Cannot parse certificate: %v", err)})
		return
	}

	// Extract hostname: prefer SAN DNS names, fall back to CN
	hostname := ""
	if len(cert.DNSNames) > 0 {
		hostname = cert.DNSNames[0]
	} else if cert.Subject.CommonName != "" {
		hostname = cert.Subject.CommonName
	}

	expires := cert.NotAfter.Format(time.RFC3339)

	writeJSON(w, contracts.TLSValidateResponse{
		Valid:    true,
		Hostname: hostname,
		Expires:  expires,
	})
}

// expandHome expands ~ to the user's home directory.
func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return home + path[1:]
		}
	}
	return path
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/dashboard/ -run TestHandleTLSValidate -v`
Expected: PASS

**Step 5: Register the route**

In `internal/dashboard/server.go`, add after line ~391 (near the other API routes):

```go
mux.HandleFunc("/api/tls/validate", s.withCORS(s.withAuthAndCSRF(s.handleTLSValidate)))
```

Note: Use `withAuth` not `withAuthAndCSRF` if this causes issues when auth is not yet configured. Since the user is configuring TLS _before_ auth, this endpoint may need to be accessible without auth. Check whether other config endpoints (like `/api/config`) use auth — they do (`withAuth`). The config endpoints are accessible without auth when auth is disabled (the `withAuth` middleware passes through when auth is off). So `withCORS(s.withAuth(s.handleTLSValidate))` is the right choice — no CSRF needed for a validation-only endpoint.

```go
mux.HandleFunc("/api/tls/validate", s.withCORS(s.withAuth(s.handleTLSValidate)))
```

**Step 6: Commit**

```bash
git add internal/dashboard/handlers_tls.go internal/dashboard/handlers_tls_test.go internal/dashboard/server.go
git commit -m "feat: add POST /api/tls/validate endpoint"
```

---

### Task 3: Add TLS validate API function — TypeScript

**Files:**

- Modify: `assets/dashboard/src/lib/api.ts`

**Step 1: Add the API function**

Add near the other auth/config API functions (around line 284):

```typescript
export async function validateTLS(certPath: string, keyPath: string): Promise<TLSValidateResponse> {
  const res = await fetch('/api/tls/validate', {
    method: 'POST',
    headers: jsonHeaders(),
    body: JSON.stringify({ cert_path: certPath, key_path: keyPath }),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}
```

This uses the generated `TLSValidateResponse` type from `types.generated.ts`. Add the import if not already imported.

**Step 2: Commit**

```bash
git add assets/dashboard/src/lib/api.ts
git commit -m "feat: add validateTLS API function"
```

---

### Task 4: Decouple TLS from auth in server startup

**Files:**

- Modify: `internal/dashboard/server.go` (lines 474-498)

**Step 1: Write the test**

The server startup logic is hard to unit test directly (it binds ports). Instead, verify via the existing config methods. Add to an existing test file or create `internal/config/tls_test.go`:

```go
package config

import "testing"

func TestGetTLSEnabled_Independent(t *testing.T) {
	cfg := &Config{
		Network: &NetworkConfig{
			TLS: &TLSConfig{
				CertPath: "/tmp/cert.pem",
				KeyPath:  "/tmp/key.pem",
			},
		},
	}
	if !cfg.GetTLSEnabled() {
		t.Error("expected TLS enabled when cert+key are set")
	}
	if cfg.GetAuthEnabled() {
		t.Error("expected auth disabled when access_control is nil")
	}
}
```

**Step 2: Run test**

Run: `go test ./internal/config/ -run TestGetTLSEnabled_Independent -v`
Expected: PASS (this verifies the methods are already independent)

**Step 3: Change server startup to use GetTLSEnabled**

In `internal/dashboard/server.go`, replace lines 474-491:

```go
	scheme := "http"
	if s.config.GetTLSEnabled() {
		scheme = "https"
	}
	if s.config.GetNetworkAccess() {
		fmt.Printf("[daemon] listening on %s://0.0.0.0:%d (accessible from local network)\n", scheme, port)
	} else {
		fmt.Printf("[daemon] listening on %s://localhost:%d (localhost only)\n", scheme, port)
	}

	if s.config.GetTLSEnabled() {
		certPath := s.config.GetTLSCertPath()
		keyPath := s.config.GetTLSKeyPath()
		if err := s.httpServer.ListenAndServeTLS(certPath, keyPath); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	}
```

This changes `GetAuthEnabled()` → `GetTLSEnabled()` in two places.

**Step 4: Run all tests**

Run: `go test ./internal/dashboard/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/dashboard/server.go internal/config/tls_test.go
git commit -m "feat: decouple TLS from auth in server startup"
```

---

### Task 5: Restructure Access tab — add HTTPS section, move Remote Access

**Files:**

- Modify: `assets/dashboard/src/routes/ConfigPage.tsx`

This is the largest task. It restructures the Access tab (step 5, starting at line 2260) into the new cascading layout.

**Step 1: Add HTTPS state variables**

Near the existing auth state variables (lines 186-194), add:

```typescript
const [httpsEnabled, setHttpsEnabled] = useState(false);
const [tlsCertPath, setTlsCertPath] = useState('');
const [tlsKeyPath, setTlsKeyPath] = useState('');
const [tlsHostname, setTlsHostname] = useState('');
const [tlsExpires, setTlsExpires] = useState('');
const [tlsModalOpen, setTlsModalOpen] = useState(false);
const [tlsModalCertPath, setTlsModalCertPath] = useState('');
const [tlsModalKeyPath, setTlsModalKeyPath] = useState('');
const [tlsModalValidating, setTlsModalValidating] = useState(false);
const [tlsModalError, setTlsModalError] = useState('');
const [tlsModalHostname, setTlsModalHostname] = useState('');
const [tlsModalExpires, setTlsModalExpires] = useState('');
const [tlsModalValid, setTlsModalValid] = useState(false);
```

**Step 2: Update load logic**

In the load useEffect (around line 423-428), derive `httpsEnabled` from whether TLS paths are set:

```typescript
const certPath = data.network?.tls?.cert_path || '';
const keyPath = data.network?.tls?.key_path || '';
setTlsCertPath(certPath);
setTlsKeyPath(keyPath);
setHttpsEnabled(certPath !== '' && keyPath !== '');
// Keep existing auth state loading
setAuthEnabled(data.access_control?.enabled || false);
setAuthProvider(data.access_control?.provider || 'github');
setAuthSessionTTLMinutes(data.access_control?.session_ttl_minutes || 1440);
// Dashboard URL is now derived, not loaded from config directly
// (but still load it for backward compat display)
setAuthPublicBaseURL(data.network?.public_base_url || '');
```

Also load hostname by calling validate on initial load if certs are configured (or add a separate GET endpoint — but reusing validate is simpler for now).

**Step 3: Update save logic**

In the save function (around lines 666-701), update the network section:

```typescript
network: {
  bind_address: networkAccess ? '0.0.0.0' : '127.0.0.1',
  public_base_url: httpsEnabled && tlsHostname
    ? `https://${tlsHostname}:${port || 7337}`
    : '',
  tls: httpsEnabled
    ? { cert_path: tlsCertPath, key_path: tlsKeyPath }
    : { cert_path: '', key_path: '' },
},
```

**Step 4: Add TLS modal validation handler**

```typescript
const handleTlsValidate = async () => {
  setTlsModalValidating(true);
  setTlsModalError('');
  setTlsModalHostname('');
  setTlsModalExpires('');
  setTlsModalValid(false);
  try {
    const result = await validateTLS(tlsModalCertPath, tlsModalKeyPath);
    if (result.valid) {
      setTlsModalHostname(result.hostname || '');
      setTlsModalExpires(result.expires || '');
      setTlsModalValid(true);
    } else {
      setTlsModalError(result.error || 'Validation failed');
    }
  } catch (err) {
    setTlsModalError(getErrorMessage(err, 'Failed to validate certificates'));
  } finally {
    setTlsModalValidating(false);
  }
};

const handleTlsModalSave = () => {
  setTlsCertPath(tlsModalCertPath);
  setTlsKeyPath(tlsModalKeyPath);
  setTlsHostname(tlsModalHostname);
  setTlsExpires(tlsModalExpires);
  setTlsModalOpen(false);
};

const openTlsModal = () => {
  setTlsModalCertPath(tlsCertPath);
  setTlsModalKeyPath(tlsKeyPath);
  setTlsModalError('');
  setTlsModalHostname(tlsHostname);
  setTlsModalExpires(tlsExpires);
  setTlsModalValid(tlsCertPath !== '' && tlsKeyPath !== '');
  setTlsModalOpen(true);
};
```

**Step 5: Add HTTPS toggle handler with cascade**

```typescript
const handleHttpsToggle = (enabled: boolean) => {
  if (!enabled && authEnabled) {
    // Disabling HTTPS while auth is enabled — confirm
    confirm('Disabling HTTPS will also disable GitHub Authentication. Continue?', {
      confirmText: 'Disable Both',
      danger: true,
    }).then((confirmed) => {
      if (confirmed) {
        setHttpsEnabled(false);
        setAuthEnabled(false);
        setTlsCertPath('');
        setTlsKeyPath('');
        setTlsHostname('');
        setTlsExpires('');
      }
    });
  } else {
    setHttpsEnabled(enabled);
    if (!enabled) {
      setTlsCertPath('');
      setTlsKeyPath('');
      setTlsHostname('');
      setTlsExpires('');
    }
  }
};
```

**Step 6: Restructure the Access tab JSX**

Replace the Access tab content (lines 2260-2705) with the new cascading layout:

1. **Network section** — keep as-is (lines 2268-2323)
2. **HTTPS section** — new section with:
   - Enable toggle
   - When enabled: cert/key display (read-only), "Configure" button, hostname display, dashboard URL display, expiry warning if < 30 days
3. **GitHub Auth section** — restructured:
   - Toggle greyed out with badge when HTTPS is off or certs not configured
   - Dashboard URL shown read-only (derived from HTTPS hostname + port)
   - Session TTL
   - OAuth credentials (existing modal)
   - Remove: cert path field, key path field, editable dashboard URL field
4. **Remote Access section** — moved to bottom, visually separated

**Step 7: Add TLS modal JSX**

Add the modal markup (rendered when `tlsModalOpen` is true). Pattern follows authSecretsModal:

```tsx
{
  tlsModalOpen && (
    <div className="modal-overlay" role="dialog" aria-modal="true">
      <div className="modal modal--wide">
        <div className="modal__header">
          <h2 className="modal__title">Configure TLS Certificates</h2>
        </div>
        <div className="modal__body">
          <div className="form-group">
            <label className="form-group__label">Certificate Path</label>
            <input
              type="text"
              className="input"
              placeholder="~/.schmux/tls/schmux.local.pem"
              value={tlsModalCertPath}
              onChange={(e) => {
                setTlsModalCertPath(e.target.value);
                setTlsModalValid(false);
              }}
            />
          </div>
          <div className="form-group">
            <label className="form-group__label">Key Path</label>
            <input
              type="text"
              className="input"
              placeholder="~/.schmux/tls/schmux.local-key.pem"
              value={tlsModalKeyPath}
              onChange={(e) => {
                setTlsModalKeyPath(e.target.value);
                setTlsModalValid(false);
              }}
            />
          </div>
          {tlsModalError && (
            <p style={{ color: 'var(--color-error)', marginTop: 'var(--spacing-sm)' }}>
              {tlsModalError}
            </p>
          )}
          {tlsModalValid && (
            <div style={{ marginTop: 'var(--spacing-sm)' }}>
              <p style={{ color: 'var(--color-success)' }}>
                ✓ Valid certificate for <strong>{tlsModalHostname}</strong>
              </p>
              <p className="text-muted">
                Expires: {new Date(tlsModalExpires).toLocaleDateString()}
              </p>
            </div>
          )}
        </div>
        <div className="modal__footer">
          <button className="btn" onClick={() => setTlsModalOpen(false)}>
            Cancel
          </button>
          {!tlsModalValid ? (
            <button
              className="btn btn--primary"
              onClick={handleTlsValidate}
              disabled={!tlsModalCertPath.trim() || !tlsModalKeyPath.trim() || tlsModalValidating}
            >
              {tlsModalValidating ? 'Validating...' : 'Validate'}
            </button>
          ) : (
            <button className="btn btn--primary" onClick={handleTlsModalSave}>
              Save
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
```

**Step 8: Update change detection**

In the `hasChanges` function and `originalConfig` snapshot, add `httpsEnabled`, `tlsCertPath`, `tlsKeyPath` and remove the direct `authTlsCertPath`/`authTlsKeyPath` comparisons (since those are now handled via the HTTPS section).

**Step 9: Update validation logic**

Replace the auth validation (lines 351-371) to remove TLS path checks from auth (those are now HTTPS section concerns). Auth validation just checks that HTTPS is enabled and configured:

```typescript
if (authEnabled) {
  if (!httpsEnabled || !tlsCertPath || !tlsKeyPath) {
    localAuthWarnings.push('HTTPS must be configured before enabling GitHub authentication.');
  }
  if (!authClientIdSet || !authClientSecretSet) {
    localAuthWarnings.push('GitHub client credentials are not configured.');
  }
}
```

**Step 10: Run frontend tests**

Run: `cd assets/dashboard && npx vitest run`
Expected: PASS (existing tests should still pass — they test component rendering, not the specific Access tab layout)

**Step 11: Build dashboard**

Run: `go run ./cmd/build-dashboard`
Expected: Build succeeds

**Step 12: Commit**

```bash
git add assets/dashboard/src/routes/ConfigPage.tsx assets/dashboard/src/lib/api.ts
git commit -m "feat: restructure Access tab with independent HTTPS section"
```

---

### Task 6: Update API docs

**Files:**

- Modify: `docs/api.md`

**Step 1: Add TLS validate endpoint documentation**

Add the new endpoint to the API docs:

````markdown
### POST /api/tls/validate

Validates TLS certificate and key file paths. Returns hostname and expiry info.

**Request:**

```json
{
  "cert_path": "~/.schmux/tls/schmux.local.pem",
  "key_path": "~/.schmux/tls/schmux.local-key.pem"
}
```
````

**Response (valid):**

```json
{
  "valid": true,
  "hostname": "schmux.local",
  "expires": "2027-01-15T00:00:00Z"
}
```

**Response (invalid):**

```json
{
  "valid": false,
  "error": "Certificate and key do not match"
}
```

````

**Step 2: Commit**

```bash
git add docs/api.md
git commit -m "docs: add POST /api/tls/validate endpoint"
````

---

### Task 7: Manual smoke test

**Step 1: Build and run**

```bash
go build ./cmd/schmux && ./schmux daemon-run
```

**Step 2: Verify Access tab layout**

- Open http://localhost:7337/config?tab=access
- Verify 4 sections visible: Network, HTTPS, GitHub Authentication, Remote Access
- HTTPS section has enable toggle
- GitHub Auth section is greyed out with "Requires HTTPS" badge

**Step 3: Test HTTPS flow**

- Enable HTTPS toggle
- Click "Configure" to open TLS modal
- Enter cert/key paths, click Validate
- Verify hostname and expiry shown
- Click Save
- Verify hostname and dashboard URL appear in HTTPS section
- Verify GitHub Auth section is now enabled

**Step 4: Test cascade disable**

- With both HTTPS and auth enabled, disable HTTPS
- Verify confirmation dialog appears
- Confirm — both HTTPS and auth should disable

**Step 5: Save and verify config**

- Save config
- Check `~/.schmux/config.json` — TLS paths and public_base_url should be set correctly
- Restart daemon — should serve on HTTPS if certs configured
