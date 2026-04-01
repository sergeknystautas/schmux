# dashboard.sx Status Alerts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface dashboard.sx heartbeat failures and certificate expiry warnings on the home page instead of burying them in logs.

**Architecture:** Add a `DashboardSXStatus` struct to state, updated by the heartbeat loop and daemon startup. Expose it in the config API response. Render alerts on the home page when the last heartbeat wasn't 200 or the cert is expiring within 30 days.

**Tech Stack:** Go (state, heartbeat, API contracts, daemon), TypeScript/React (home page), Vitest (frontend tests)

---

### Task 1: Add DashboardSXStatus to state

**Files:**

- Modify: `internal/state/state.go:19-36` (State struct, add getter/setter)
- Modify: `internal/state/interfaces.go:73-75` (add to StateStore interface)

- [ ] **Step 1: Write the failing test**

Create `internal/state/state_dashboardsx_test.go`:

```go
//go:build !nodashboardsx

package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDashboardSXStatus(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	os.WriteFile(statePath, []byte(`{"workspaces":[],"sessions":[]}`), 0644)
	st, err := Load(statePath, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Initially nil
	if got := st.GetDashboardSXStatus(); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}

	// Set and read back
	now := time.Now().Truncate(time.Second)
	status := &DashboardSXStatus{
		LastHeartbeatTime:   now,
		LastHeartbeatStatus: 403,
		LastHeartbeatError:  "registration not found",
		CertDomain:          "12540.dashboard.sx",
		CertExpiresAt:       now.Add(10 * 24 * time.Hour),
	}
	st.SetDashboardSXStatus(status)

	got := st.GetDashboardSXStatus()
	if got == nil {
		t.Fatal("expected non-nil status")
	}
	if got.LastHeartbeatStatus != 403 {
		t.Errorf("expected status 403, got %d", got.LastHeartbeatStatus)
	}
	if got.LastHeartbeatError != "registration not found" {
		t.Errorf("expected error message, got %q", got.LastHeartbeatError)
	}
	if got.CertDomain != "12540.dashboard.sx" {
		t.Errorf("expected domain, got %q", got.CertDomain)
	}

	// Verify persistence
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	st2, err := Load(statePath, nil)
	if err != nil {
		t.Fatal(err)
	}
	got2 := st2.GetDashboardSXStatus()
	if got2 == nil {
		t.Fatal("expected non-nil after reload")
	}
	if got2.LastHeartbeatStatus != 403 {
		t.Errorf("expected 403 after reload, got %d", got2.LastHeartbeatStatus)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/state/ -run TestDashboardSXStatus -v`
Expected: FAIL — `DashboardSXStatus` type not defined, methods not found.

- [ ] **Step 3: Add the struct and field to state**

In `internal/state/state.go`, add the struct before the `State` struct definition (after line 17):

```go
// DashboardSXStatus tracks the last heartbeat response and certificate expiry.
type DashboardSXStatus struct {
	LastHeartbeatTime   time.Time `json:"last_heartbeat_time,omitempty"`
	LastHeartbeatStatus int       `json:"last_heartbeat_status,omitempty"`
	LastHeartbeatError  string    `json:"last_heartbeat_error,omitempty"`
	CertDomain          string   `json:"cert_domain,omitempty"`
	CertExpiresAt       time.Time `json:"cert_expires_at,omitempty"`
}
```

Add field to `State` struct (after `Previews`):

```go
	DashboardSX *DashboardSXStatus `json:"dashboard_sx,omitempty"`
```

- [ ] **Step 4: Add getter and setter**

In `internal/state/state.go`, after the `GetNeedsRestart`/`SetNeedsRestart` block (after line 903):

```go
// GetDashboardSXStatus returns a copy of the dashboard.sx status, or nil.
func (s *State) GetDashboardSXStatus() *DashboardSXStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.DashboardSX == nil {
		return nil
	}
	cp := *s.DashboardSX
	return &cp
}

// SetDashboardSXStatus sets the dashboard.sx status.
func (s *State) SetDashboardSXStatus(status *DashboardSXStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.DashboardSX = status
}
```

- [ ] **Step 5: Add to StateStore interface**

In `internal/state/interfaces.go`, after the `SetNeedsRestart` line (after line 75):

```go
	// DashboardSX status
	GetDashboardSXStatus() *DashboardSXStatus
	SetDashboardSXStatus(status *DashboardSXStatus)
```

- [ ] **Step 6: Add to mock state stores**

Any test file that implements `StateStore` needs the new methods. Search for all implementations:

In `internal/workspace/manager_test.go`, after the `SetNeedsRestart` method (after line 708):

```go
func (m *mockStateStore) GetDashboardSXStatus() *state.DashboardSXStatus {
	return m.state.GetDashboardSXStatus()
}

func (m *mockStateStore) SetDashboardSXStatus(status *state.DashboardSXStatus) {
	m.state.SetDashboardSXStatus(status)
}
```

Search for any other mock state stores (`grep -r "StateStore" --include="*_test.go"`) and add the same methods.

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/state/ -run TestDashboardSXStatus -v`
Expected: PASS

- [ ] **Step 8: Run full backend tests**

Run: `go test ./...`
Expected: PASS (mock state stores compile, no interface violations)

---

### Task 2: Change Heartbeat to return HTTP status code

**Files:**

- Modify: `internal/dashboardsx/client.go:47-65` (Heartbeat method)
- Modify: `internal/dashboardsx/client.go:196-218` (post helper)
- Modify: `internal/dashboardsx/client_test.go`
- Modify: `internal/dashboardsx/disabled.go:61` (stub)

- [ ] **Step 1: Write the failing test**

In `internal/dashboardsx/client_test.go`, add a test (or modify the existing heartbeat test) to check that `Heartbeat()` returns a status code:

```go
func TestHeartbeatReturnsStatusCode(t *testing.T) {
	tests := []struct {
		name           string
		serverStatus   int
		expectedStatus int
		expectErr      bool
	}{
		{"success", 200, 200, false},
		{"forbidden", 403, 403, true},
		{"server error", 500, 500, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.serverStatus)
				w.Write([]byte(`{"ok":true}`))
			}))
			defer srv.Close()

			client := NewClient(srv.URL, "test-key", "12345")
			status, err := client.Heartbeat()
			if status != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, status)
			}
			if tc.expectErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dashboardsx/ -run TestHeartbeatReturnsStatusCode -v`
Expected: FAIL — `Heartbeat()` returns `error`, not `(int, error)`.

- [ ] **Step 3: Change post helper to return status code**

In `internal/dashboardsx/client.go`, change the `post` method signature and implementation:

```go
// post sends a POST request with JSON body and returns the HTTP status code and response body.
func (c *Client) post(path string, body interface{}) (int, []byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.HTTPClient.Post(c.ServiceURL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return 0, nil, fmt.Errorf("%s request failed: %w", path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("failed to read %s response: %w", path, err)
	}

	if resp.StatusCode >= 400 {
		return resp.StatusCode, nil, fmt.Errorf("%s returned %d: %s", path, resp.StatusCode, string(respBody))
	}

	return resp.StatusCode, respBody, nil
}
```

- [ ] **Step 4: Update Heartbeat to return (int, error)**

```go
// Heartbeat sends a keep-alive signal to dashboard.sx.
func (c *Client) Heartbeat() (int, error) {
	body := HeartbeatRequest{
		InstanceKey: c.InstanceKey,
	}
	c.log("POST %s/heartbeat", c.ServiceURL)
	status, _, err := c.post("/heartbeat", body)
	if err != nil {
		c.log("  → error: %v", err)
	} else {
		c.log("  → OK")
	}
	return status, err
}
```

- [ ] **Step 5: Update all other callers of post**

Update every method that calls `c.post(...)` to handle the new 3-return signature. These are:

- `CertProvisioningStart`: change `data, err := c.post(...)` to `_, data, err := c.post(...)`
- `CallbackExchange`: same change
- `DNSChallengeCreate`: change `_, err := c.post(...)` to `_, _, err := c.post(...)`

- [ ] **Step 6: Update disabled.go stub**

In `internal/dashboardsx/disabled.go`, the `StartHeartbeat` stub is fine (it doesn't call `Heartbeat`), but if there's a stub for `Heartbeat` on the `Client`, no change is needed since the disabled build doesn't define the method — it uses a bare struct. No change needed.

- [ ] **Step 7: Run tests**

Run: `go test ./internal/dashboardsx/ -v`
Expected: PASS — all existing tests and new test pass.

---

### Task 3: Update StartHeartbeat to persist status to state

**Files:**

- Modify: `internal/dashboardsx/heartbeat.go`
- Modify: `internal/dashboardsx/disabled.go:61` (stub signature)
- Create: `internal/dashboardsx/heartbeat_state_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/dashboardsx/heartbeat_state_test.go`:

```go
//go:build !nodashboardsx

package dashboardsx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type mockStatusWriter struct {
	mu     sync.Mutex
	status *HeartbeatStatus
}

func (m *mockStatusWriter) SetHeartbeatStatus(s *HeartbeatStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.status = &cp
}

func (m *mockStatusWriter) get() *HeartbeatStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status == nil {
		return nil
	}
	cp := *m.status
	return &cp
}

func TestStartHeartbeatPersistsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte("registration not found"))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-key", "12345")
	writer := &mockStatusWriter{}

	ctx, cancel := context.WithCancel(context.Background())
	go StartHeartbeat(ctx, client, writer)

	// Wait for initial heartbeat to be processed
	deadline := time.After(2 * time.Second)
	for {
		if s := writer.get(); s != nil {
			if s.StatusCode != 403 {
				t.Errorf("expected 403, got %d", s.StatusCode)
			}
			if s.Error == "" {
				t.Error("expected non-empty error")
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for heartbeat status")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	cancel()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dashboardsx/ -run TestStartHeartbeatPersistsStatus -v`
Expected: FAIL — `StartHeartbeat` has wrong signature, `HeartbeatStatus` not defined.

- [ ] **Step 3: Define HeartbeatStatus and StatusWriter interface**

In `internal/dashboardsx/heartbeat.go`, add before `StartHeartbeat`:

```go
// HeartbeatStatus is the result of a single heartbeat attempt.
type HeartbeatStatus struct {
	Time       time.Time
	StatusCode int
	Error      string
}

// HeartbeatStatusWriter persists heartbeat results.
type HeartbeatStatusWriter interface {
	SetHeartbeatStatus(status *HeartbeatStatus)
}
```

- [ ] **Step 4: Update StartHeartbeat to accept StatusWriter and persist results**

```go
// StartHeartbeat runs a background heartbeat loop that sends keep-alive
// signals to the dashboard.sx service. It sends one immediately, then
// every 24h ± 2h (randomized to prevent surveillance).
// The goroutine exits when ctx is cancelled.
func StartHeartbeat(ctx context.Context, client *Client, writer HeartbeatStatusWriter) {
	recordHeartbeat := func() {
		statusCode, err := client.Heartbeat()
		s := &HeartbeatStatus{
			Time:       time.Now(),
			StatusCode: statusCode,
		}
		if err != nil {
			s.Error = err.Error()
			if pkgLogger != nil {
				pkgLogger.Error("heartbeat failed", "err", err)
			}
		} else {
			if pkgLogger != nil {
				pkgLogger.Info("heartbeat sent")
			}
		}
		if writer != nil {
			writer.SetHeartbeatStatus(s)
		}
	}

	// Send initial heartbeat immediately
	recordHeartbeat()

	for {
		interval := heartbeatInterval()
		timer := time.NewTimer(interval)
		select {
		case <-timer.C:
			recordHeartbeat()
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
}
```

- [ ] **Step 5: Update disabled.go stub**

In `internal/dashboardsx/disabled.go`, update the `StartHeartbeat` stub:

```go
func StartHeartbeat(_ context.Context, _ *Client, _ HeartbeatStatusWriter) {}
```

Also add the types to the disabled build so they compile:

```go
type HeartbeatStatus struct {
	Time       time.Time
	StatusCode int
	Error      string
}

type HeartbeatStatusWriter interface {
	SetHeartbeatStatus(status *HeartbeatStatus)
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/dashboardsx/ -v`
Expected: PASS

---

### Task 4: Wire daemon startup to populate state and pass state to heartbeat

**Files:**

- Modify: `internal/daemon/daemon.go:400-423`
- Modify: `internal/dashboardsx/acme.go` (add `GetCertDomain`)

- [ ] **Step 1: Add GetCertDomain to acme.go**

In `internal/dashboardsx/acme.go`, after `GetCertExpiry` (after line 255):

```go
// GetCertDomain parses the certificate and returns the common name.
func GetCertDomain() (string, error) {
	certPath, err := CertPath()
	if err != nil {
		return "", err
	}

	data, err := os.ReadFile(certPath)
	if err != nil {
		return "", err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return "", fmt.Errorf("no PEM block found in certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}

	return cert.Subject.CommonName, nil
}
```

Add stub in `internal/dashboardsx/disabled.go`:

```go
func GetCertDomain() (string, error) {
	return "", fmt.Errorf("dashboardsx is not available in this build")
}
```

- [ ] **Step 2: Create a state adapter for HeartbeatStatusWriter**

The daemon needs to bridge `dashboardsx.HeartbeatStatusWriter` to `state.StateStore`. In `internal/daemon/daemon.go`, add a small adapter (before or inside the `Run` function, or as a private type in the file):

```go
// heartbeatStateWriter adapts state.StateStore to dashboardsx.HeartbeatStatusWriter.
type heartbeatStateWriter struct {
	state state.StateStore
}

func (w *heartbeatStateWriter) SetHeartbeatStatus(s *dashboardsx.HeartbeatStatus) {
	existing := w.state.GetDashboardSXStatus()
	if existing == nil {
		existing = &state.DashboardSXStatus{}
	}
	existing.LastHeartbeatTime = s.Time
	existing.LastHeartbeatStatus = s.StatusCode
	existing.LastHeartbeatError = s.Error
	w.state.SetDashboardSXStatus(existing)
	w.state.Save()
}
```

- [ ] **Step 3: Update the dashboardsx startup block in daemon.go**

Replace the existing block at lines 400-423 with:

```go
	// Populate dashboard.sx status and start background services
	if cfg.GetDashboardSXEnabled() {
		// Populate cert info in state
		dxStatus := st.GetDashboardSXStatus()
		if dxStatus == nil {
			dxStatus = &state.DashboardSXStatus{}
		}
		if domain, err := dashboardsx.GetCertDomain(); err == nil {
			dxStatus.CertDomain = domain
		}
		if expiry, err := dashboardsx.GetCertExpiry(); err == nil {
			dxStatus.CertExpiresAt = expiry
		}
		st.SetDashboardSXStatus(dxStatus)
		st.Save()

		// Start heartbeat and auto-renewal goroutines
		instanceKey, err := dashboardsx.EnsureInstanceKey()
		if err != nil {
			logger.Warn("failed to read instance key", "err", err)
		} else {
			serviceURL := dashboardsx.DefaultServiceURL
			if cfg.Network != nil && cfg.Network.DashboardSX != nil && cfg.Network.DashboardSX.ServiceURL != "" {
				serviceURL = cfg.Network.DashboardSX.ServiceURL
			}
			client := dashboardsx.NewClient(serviceURL, instanceKey, cfg.GetDashboardSXCode())

			writer := &heartbeatStateWriter{state: st}
			go dashboardsx.StartHeartbeat(d.shutdownCtx, client, writer)

			if email := cfg.GetDashboardSXEmail(); email != "" {
				go dashboardsx.StartAutoRenewal(d.shutdownCtx, client, email)
			}
		}
	}
```

- [ ] **Step 4: Run backend tests**

Run: `go test ./...`
Expected: PASS

---

### Task 5: Add DashboardSXStatus to API contract and handler

**Files:**

- Modify: `internal/api/contracts/config.go:140-173` (ConfigResponse)
- Modify: `internal/dashboard/handlers_config.go:207` (populate field)

- [ ] **Step 1: Add struct and field to contracts**

In `internal/api/contracts/config.go`, add the struct (before `ConfigResponse`):

```go
// DashboardSXStatus represents dashboard.sx heartbeat and certificate status.
type DashboardSXStatus struct {
	LastHeartbeatTime   string `json:"last_heartbeat_time,omitempty"`
	LastHeartbeatStatus int    `json:"last_heartbeat_status,omitempty"`
	LastHeartbeatError  string `json:"last_heartbeat_error,omitempty"`
	CertDomain          string `json:"cert_domain,omitempty"`
	CertExpiresAt       string `json:"cert_expires_at,omitempty"`
}
```

Note: Use `string` for times here (ISO 8601 format) since this is the API contract sent to the frontend. The handler will format them.

Add field to `ConfigResponse` (after `NeedsRestart`):

```go
	DashboardSXStatus *DashboardSXStatus `json:"dashboard_sx_status,omitempty"`
```

- [ ] **Step 2: Populate in handler**

In `internal/dashboard/handlers_config.go`, after the `NeedsRestart` line (line 207):

```go
		NeedsRestart: s.state.GetNeedsRestart(),
```

Add:

```go
		DashboardSXStatus: func() *contracts.DashboardSXStatus {
			st := s.state.GetDashboardSXStatus()
			if st == nil {
				return nil
			}
			return &contracts.DashboardSXStatus{
				LastHeartbeatTime:   st.LastHeartbeatTime.Format(time.RFC3339),
				LastHeartbeatStatus: st.LastHeartbeatStatus,
				LastHeartbeatError:  st.LastHeartbeatError,
				CertDomain:          st.CertDomain,
				CertExpiresAt:       st.CertExpiresAt.Format(time.RFC3339),
			}
		}(),
```

Import `time` in the file if not already imported.

- [ ] **Step 3: Regenerate TypeScript types**

Run: `go run ./cmd/gen-types`

This will add the `DashboardSXStatus` interface and the `dashboard_sx_status` field to `ConfigResponse` in `assets/dashboard/src/lib/types.generated.ts`.

- [ ] **Step 4: Run backend tests**

Run: `go test ./...`
Expected: PASS

---

### Task 6: Render alerts on the home page

**Files:**

- Modify: `assets/dashboard/src/routes/HomePage.tsx`

- [ ] **Step 1: Write the failing test**

Create `assets/dashboard/src/routes/HomePage.dashboardsx.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import HomePage from './HomePage';

// Mock all the contexts and hooks
vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    workspaces: [],
    sessions: [],
    loading: false,
    connected: true,
    subredditUpdateCount: 0,
    repofeedUpdateCount: 0,
  }),
}));

vi.mock('../contexts/FeaturesContext', () => ({
  useFeatures: () => ({
    features: { github: false, subreddit: false, repofeed: false },
  }),
}));

vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}));

vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({ alert: vi.fn() }),
}));

vi.mock('../hooks/useFloorManager', () => ({
  useFloorManager: () => ({ enabled: false, running: false }),
}));

vi.mock('../hooks/useTerminalStream', () => ({
  useTerminalStream: () => ({ containerRef: { current: null } }),
}));

vi.mock('../lib/navigation', () => ({
  navigateToWorkspace: vi.fn(),
  usePendingNavigation: () => ({ setPendingNavigation: vi.fn() }),
}));

vi.mock('../lib/api', () => ({
  scanWorkspaces: vi.fn(),
  getRecentBranches: vi.fn().mockResolvedValue([]),
  refreshRecentBranches: vi.fn().mockResolvedValue([]),
  prepareBranchSpawn: vi.fn(),
  getPRs: vi.fn().mockResolvedValue({ pull_requests: [] }),
  refreshPRs: vi.fn().mockResolvedValue({ pull_requests: [] }),
  checkoutPR: vi.fn(),
  getOverlays: vi.fn().mockResolvedValue({ overlays: [] }),
  dismissOverlayNudge: vi.fn(),
  getErrorMessage: vi.fn(),
  linearSyncFromMain: vi.fn(),
  getGitGraph: vi.fn(),
  getSubreddit: vi.fn().mockResolvedValue(null),
  getRepofeedList: vi.fn().mockResolvedValue(null),
}));

const mockConfig = {
  workspace_path: '/dev',
  source_code_management: 'git-worktree',
  repos: [],
  run_targets: [],
  quick_launch: [],
  runners: {},
  models: [],
  nudgenik: { target: '' },
  branch_suggest: {},
  conflict_resolve: {},
  sessions: {
    dashboard_poll_interval_ms: 5000,
    git_status_poll_interval_ms: 10000,
    git_clone_timeout_ms: 600000,
    git_status_timeout_ms: 60000,
  },
  xterm: {
    query_timeout_ms: 5000,
    operation_timeout_ms: 10000,
    use_webgl: true,
    sync_check_enabled: false,
  },
  network: { bind_address: '0.0.0.0', port: 7337 },
  access_control: { enabled: false, provider: '', session_ttl_minutes: 0 },
  pr_review: { target: '' },
  commit_message: { target: '' },
  desync: { enabled: false, target: '' },
  io_workspace_telemetry: { enabled: false },
  notifications: {
    sound_disabled: false,
    confirm_before_close: false,
    suggest_dispose_after_push: true,
  },
  lore: { enabled: false },
  subreddit: {
    enabled: false,
    target: '',
    interval: 30,
    checking_range: 48,
    max_posts: 30,
    max_age: 14,
  },
  repofeed: {
    publish_interval_seconds: 30,
    fetch_interval_seconds: 60,
    completed_retention_hours: 48,
  },
  floor_manager: { enabled: false, target: '', rotation_threshold: 150, debounce_ms: 2000 },
  remote_access: { enabled: false, timeout_minutes: 120, password_hash_set: false, notify: {} },
  system_capabilities: { iterm2_available: false },
  needs_restart: false,
};

let configOverride = {};

vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({
    config: { ...mockConfig, ...configOverride },
    loading: false,
    getRepoName: (url: string) => url,
  }),
  useRequireConfig: () => {},
}));

describe('dashboard.sx alerts on HomePage', () => {
  beforeEach(() => {
    configOverride = {};
  });

  it('does not render alerts section when no dashboard_sx_status', () => {
    render(
      <MemoryRouter>
        <HomePage />
      </MemoryRouter>
    );
    expect(screen.queryByText('dashboard.sx alerts')).not.toBeInTheDocument();
  });

  it('does not render alerts when heartbeat is 200 and cert not expiring', () => {
    const future = new Date(Date.now() + 60 * 24 * 60 * 60 * 1000).toISOString(); // 60 days
    configOverride = {
      dashboard_sx_status: {
        last_heartbeat_time: new Date().toISOString(),
        last_heartbeat_status: 200,
        last_heartbeat_error: '',
        cert_domain: '12540.dashboard.sx',
        cert_expires_at: future,
      },
    };
    render(
      <MemoryRouter>
        <HomePage />
      </MemoryRouter>
    );
    expect(screen.queryByText('dashboard.sx alerts')).not.toBeInTheDocument();
  });

  it('renders heartbeat alert when status is not 200', () => {
    configOverride = {
      dashboard_sx_status: {
        last_heartbeat_time: '2026-04-01T14:30:00Z',
        last_heartbeat_status: 403,
        last_heartbeat_error: '/heartbeat returned 403: registration not found',
        cert_domain: '12540.dashboard.sx',
        cert_expires_at: new Date(Date.now() + 60 * 24 * 60 * 60 * 1000).toISOString(),
      },
    };
    render(
      <MemoryRouter>
        <HomePage />
      </MemoryRouter>
    );
    expect(screen.getByText('dashboard.sx alerts')).toBeInTheDocument();
    expect(screen.getByText(/403/)).toBeInTheDocument();
  });

  it('renders cert expiry alert when within 30 days', () => {
    const soon = new Date(Date.now() + 12 * 24 * 60 * 60 * 1000).toISOString(); // 12 days
    configOverride = {
      dashboard_sx_status: {
        last_heartbeat_time: new Date().toISOString(),
        last_heartbeat_status: 200,
        last_heartbeat_error: '',
        cert_domain: '12540.dashboard.sx',
        cert_expires_at: soon,
      },
    };
    render(
      <MemoryRouter>
        <HomePage />
      </MemoryRouter>
    );
    expect(screen.getByText('dashboard.sx alerts')).toBeInTheDocument();
    expect(screen.getByText(/expires in 12 days/)).toBeInTheDocument();
  });

  it('renders both alerts when heartbeat failed and cert expiring', () => {
    const soon = new Date(Date.now() + 5 * 24 * 60 * 60 * 1000).toISOString();
    configOverride = {
      dashboard_sx_status: {
        last_heartbeat_time: '2026-04-01T14:30:00Z',
        last_heartbeat_status: 500,
        last_heartbeat_error: '/heartbeat returned 500: internal error',
        cert_domain: '12540.dashboard.sx',
        cert_expires_at: soon,
      },
    };
    render(
      <MemoryRouter>
        <HomePage />
      </MemoryRouter>
    );
    expect(screen.getByText('dashboard.sx alerts')).toBeInTheDocument();
    expect(screen.getByText(/500/)).toBeInTheDocument();
    expect(screen.getByText(/expires in 5 days/)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./test.sh --quick`
Expected: FAIL — `dashboard_sx_status` not recognized, alert rendering code not written.

- [ ] **Step 3: Add the alerts section to HomePage**

In `assets/dashboard/src/routes/HomePage.tsx`, add the computation and rendering.

Near the top of the `HomePage` component (after the `config` destructuring around line 265), compute alerts:

```tsx
// dashboard.sx alerts
const dxStatus = config.dashboard_sx_status;
const dxAlerts: { key: string; text: string }[] = [];
if (dxStatus) {
  if (dxStatus.last_heartbeat_status && dxStatus.last_heartbeat_status !== 200) {
    const time = dxStatus.last_heartbeat_time
      ? new Date(dxStatus.last_heartbeat_time).toLocaleString()
      : 'unknown';
    dxAlerts.push({
      key: 'heartbeat',
      text: `heartbeat: ${time} ${dxStatus.last_heartbeat_status} ${dxStatus.last_heartbeat_error || ''}`.trim(),
    });
  }
  if (dxStatus.cert_expires_at) {
    const daysLeft = Math.ceil(
      (new Date(dxStatus.cert_expires_at).getTime() - Date.now()) / (1000 * 60 * 60 * 24)
    );
    if (daysLeft <= 30) {
      dxAlerts.push({
        key: 'cert',
        text: `certificate: ${dxStatus.cert_domain || 'unknown'} expires in ${daysLeft} days`,
      });
    }
  }
}
```

In the JSX, before the hero section (around line 793, after `<div className={styles.leftColumn}>`):

```tsx
{
  dxAlerts.length > 0 && (
    <div className="banner banner--warning mb-md" data-testid="dashboardsx-alerts">
      <strong>dashboard.sx alerts</strong>
      {dxAlerts.map((a) => (
        <div key={a.key}>{a.text}</div>
      ))}
    </div>
  );
}
```

- [ ] **Step 4: Run tests**

Run: `./test.sh --quick`
Expected: PASS

---

### Task 7: Update docs/api.md and run full test suite

**Files:**

- Modify: `docs/api.md`

- [ ] **Step 1: Update API docs**

In `docs/api.md`, find the `ConfigResponse` documentation and add the new field. Add a `DashboardSXStatus` object definition documenting all fields.

- [ ] **Step 2: Run full test suite**

Run: `./test.sh`
Expected: PASS (includes typecheck, api doc check, all backend and frontend tests)
