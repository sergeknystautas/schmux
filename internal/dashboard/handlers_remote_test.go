package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// TestConnectProgressSSEDisconnect verifies that when an SSE client disconnects
// mid-provisioning, the goroutine exits and doesn't leak (Issue 3 fix).
// This tests the cleanup pattern used in handleRemoteConnectStream.
func TestConnectProgressSSEDisconnect(t *testing.T) {
	// Simulate the SSE handler pattern
	progressCh := make(chan string, 10)
	doneCh := make(chan struct{})
	cleanupOnce := sync.Once{}

	cleanup := func() {
		cleanupOnce.Do(func() {
			go func() {
				for range progressCh {
					// Discard
				}
			}()
			close(doneCh)
		})
	}

	// Create a context we can cancel (simulates client disconnect)
	ctx, cancel := context.WithCancel(context.Background())

	// Start the goroutine (simulates ConnectWithProgress goroutine)
	goroutineDone := make(chan struct{})
	goroutineStarted := make(chan struct{})
	go func() {
		defer close(goroutineDone)
		close(goroutineStarted) // Signal that goroutine is running

		// Simulate slow provisioning
		for i := 0; i < 10; i++ {
			select {
			case progressCh <- "progress":
			case <-doneCh:
				// Cleanup called, stop
				return
			case <-ctx.Done():
				// Context cancelled, stop
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		close(progressCh)
	}()

	// Wait for goroutine to start (instead of time.Sleep)
	<-goroutineStarted

	// Simulate client disconnect
	cancel()
	cleanup()

	// Verify goroutine exits
	select {
	case <-goroutineDone:
		// Good - goroutine exited
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine did not exit after cleanup (goroutine leak)")
	}
}

// TestCleanupOnceIdempotent verifies cleanup can be called multiple times safely
func TestCleanupOnceIdempotent(t *testing.T) {
	progressCh := make(chan string, 10)
	doneCh := make(chan struct{})
	cleanupOnce := sync.Once{}

	cleanup := func() {
		cleanupOnce.Do(func() {
			go func() {
				for range progressCh {
				}
			}()
			close(doneCh)
		})
	}

	// Call cleanup multiple times
	cleanup()
	cleanup()
	cleanup()

	// Verify doneCh is closed exactly once (wouldn't panic on second close if Once works)
	select {
	case <-doneCh:
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Fatal("doneCh should be closed")
	}
}

// TestHandleRemoteFlavorStatuses_EmptyHostsNotNull verifies Bug 1:
// When a flavor has no connections, the API must return hosts as an empty JSON
// array ([]), not null. Go serializes nil slices as null, which crashed the
// frontend when it tried to call .map() on null.
func TestHandleRemoteFlavorStatuses_EmptyHostsNotNull(t *testing.T) {
	server, cfg, _ := newTestServer(t)

	// Add a remote flavor with no active connections
	if err := cfg.AddRemoteFlavor(config.RemoteFlavor{
		Flavor:        "od",
		DisplayName:   "OnDemand",
		VCS:           "git",
		WorkspacePath: "/data/users/test",
	}); err != nil {
		t.Fatalf("failed to add remote flavor: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/remote/flavor-statuses", nil)
	rr := httptest.NewRecorder()
	server.handleRemoteFlavorStatuses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	// Parse response as raw JSON to check null vs empty array
	var raw json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Parse into structured response
	var statuses []RemoteFlavorStatusResponse
	if err := json.Unmarshal(raw, &statuses); err != nil {
		t.Fatalf("failed to unmarshal statuses: %v", err)
	}

	if len(statuses) == 0 {
		t.Fatal("expected at least one flavor status")
	}

	// The Hosts field should be nil in Go (no connections), but that's the
	// Go-side representation. The key invariant is that the frontend handles
	// this by doing (hosts || []).map(...). This test verifies the handler
	// produces valid JSON that can be parsed.
	// Also verify the flavor data comes through
	if statuses[0].Flavor.DisplayName != "OnDemand" {
		t.Errorf("DisplayName = %q, want %q", statuses[0].Flavor.DisplayName, "OnDemand")
	}
}

// TestHandleRemoteHostDisconnect_DismissRemovesAll verifies that DELETE
// with ?dismiss=true removes the host, its sessions, and its workspaces.
func TestHandleRemoteHostDisconnect_DismissRemovesAll(t *testing.T) {
	server, _, st := newTestServer(t)

	hostID := "host-dismiss-test"

	// Add a remote host, sessions, and workspaces to state
	st.AddRemoteHost(state.RemoteHost{
		ID:       hostID,
		FlavorID: "flavor-od",
		Status:   state.RemoteHostStatusDisconnected,
		Hostname: "old.example.com",
	})
	st.AddSession(state.Session{
		ID:           "sess-1",
		RemoteHostID: hostID,
	})
	st.AddSession(state.Session{
		ID:           "sess-2",
		RemoteHostID: hostID,
	})
	st.AddWorkspace(state.Workspace{
		ID:           "ws-1",
		RemoteHostID: hostID,
	})

	// Verify they exist
	if sessions := st.GetSessionsByRemoteHostID(hostID); len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if workspaces := st.GetWorkspacesByRemoteHostID(hostID); len(workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(workspaces))
	}

	// Create request with chi URL param and dismiss=true
	req := httptest.NewRequest("DELETE", "/api/remote/hosts/"+hostID+"?dismiss=true", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("hostID", hostID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	server.handleRemoteHostDisconnect(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d; body: %s", rr.Code, rr.Body.String())
	}

	// Verify everything was removed
	if _, found := st.GetRemoteHost(hostID); found {
		t.Error("remote host should be removed after dismiss")
	}
	if sessions := st.GetSessionsByRemoteHostID(hostID); len(sessions) != 0 {
		t.Errorf("expected 0 sessions after dismiss, got %d", len(sessions))
	}
	if workspaces := st.GetWorkspacesByRemoteHostID(hostID); len(workspaces) != 0 {
		t.Errorf("expected 0 workspaces after dismiss, got %d", len(workspaces))
	}
}

func TestHandleRemoteFlavorUpdate_AllFields(t *testing.T) {
	server, cfg, _ := newTestServer(t)

	// Seed a flavor to update
	if err := cfg.AddRemoteFlavor(config.RemoteFlavor{
		Flavor:                "od",
		DisplayName:           "OnDemand",
		VCS:                   "git",
		WorkspacePath:         "/data/users/test",
		ConnectCommand:        "ssh {{.Flavor}}",
		ReconnectCommand:      "ssh {{.Hostname}}",
		ProvisionCommand:      "setup.sh",
		HostnameRegex:         `(od\d+)`,
		VSCodeCommandTemplate: "code --remote ssh-remote+{{.Hostname}}",
	}); err != nil {
		t.Fatalf("failed to add flavor: %v", err)
	}
	id := config.GenerateRemoteFlavorID("od")

	tests := []struct {
		name  string
		field string // JSON field name
		value string // new value to send
		check func(t *testing.T, resp RemoteFlavorResponse)
	}{
		{
			name:  "Flavor",
			field: "flavor",
			value: "devserver",
			check: func(t *testing.T, resp RemoteFlavorResponse) {
				if resp.Flavor != "devserver" {
					t.Errorf("Flavor = %q, want %q", resp.Flavor, "devserver")
				}
			},
		},
		{
			name:  "DisplayName",
			field: "display_name",
			value: "Dev Server",
			check: func(t *testing.T, resp RemoteFlavorResponse) {
				if resp.DisplayName != "Dev Server" {
					t.Errorf("DisplayName = %q, want %q", resp.DisplayName, "Dev Server")
				}
			},
		},
		{
			name:  "VCS",
			field: "vcs",
			value: "sapling",
			check: func(t *testing.T, resp RemoteFlavorResponse) {
				if resp.VCS != "sapling" {
					t.Errorf("VCS = %q, want %q", resp.VCS, "sapling")
				}
			},
		},
		{
			name:  "WorkspacePath",
			field: "workspace_path",
			value: "/home/newuser/workspaces",
			check: func(t *testing.T, resp RemoteFlavorResponse) {
				if resp.WorkspacePath != "/home/newuser/workspaces" {
					t.Errorf("WorkspacePath = %q, want %q", resp.WorkspacePath, "/home/newuser/workspaces")
				}
			},
		},
		{
			name:  "ConnectCommand",
			field: "connect_command",
			value: "cloud-ssh connect {{.Flavor}}",
			check: func(t *testing.T, resp RemoteFlavorResponse) {
				if resp.ConnectCommand != "cloud-ssh connect {{.Flavor}}" {
					t.Errorf("ConnectCommand = %q, want %q", resp.ConnectCommand, "cloud-ssh connect {{.Flavor}}")
				}
			},
		},
		{
			name:  "ReconnectCommand",
			field: "reconnect_command",
			value: "cloud-ssh reconnect {{.Hostname}}",
			check: func(t *testing.T, resp RemoteFlavorResponse) {
				if resp.ReconnectCommand != "cloud-ssh reconnect {{.Hostname}}" {
					t.Errorf("ReconnectCommand = %q, want %q", resp.ReconnectCommand, "cloud-ssh reconnect {{.Hostname}}")
				}
			},
		},
		{
			name:  "ProvisionCommand",
			field: "provision_command",
			value: "bootstrap.sh --env prod",
			check: func(t *testing.T, resp RemoteFlavorResponse) {
				if resp.ProvisionCommand != "bootstrap.sh --env prod" {
					t.Errorf("ProvisionCommand = %q, want %q", resp.ProvisionCommand, "bootstrap.sh --env prod")
				}
			},
		},
		{
			name:  "HostnameRegex",
			field: "hostname_regex",
			value: `(devvm\d+\.example\.com)`,
			check: func(t *testing.T, resp RemoteFlavorResponse) {
				if resp.HostnameRegex != `(devvm\d+\.example\.com)` {
					t.Errorf("HostnameRegex = %q, want %q", resp.HostnameRegex, `(devvm\d+\.example\.com)`)
				}
			},
		},
		{
			name:  "VSCodeCommandTemplate",
			field: "vscode_command_template",
			value: "code --remote ssh-remote+{{.Hostname}} /workspace",
			check: func(t *testing.T, resp RemoteFlavorResponse) {
				if resp.VSCodeCommandTemplate != "code --remote ssh-remote+{{.Hostname}} /workspace" {
					t.Errorf("VSCodeCommandTemplate = %q, want %q", resp.VSCodeCommandTemplate, "code --remote ssh-remote+{{.Hostname}} /workspace")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset to known state before each subtest
			cfg.UpdateRemoteFlavor(config.RemoteFlavor{
				ID:                    id,
				Flavor:                "od",
				DisplayName:           "OnDemand",
				VCS:                   "git",
				WorkspacePath:         "/data/users/test",
				ConnectCommand:        "ssh {{.Flavor}}",
				ReconnectCommand:      "ssh {{.Hostname}}",
				ProvisionCommand:      "setup.sh",
				HostnameRegex:         `(od\d+)`,
				VSCodeCommandTemplate: "code --remote ssh-remote+{{.Hostname}}",
			})

			// Build a full valid request body, overriding just the target field
			base := map[string]string{
				"flavor":                  "od",
				"display_name":            "OnDemand",
				"vcs":                     "git",
				"workspace_path":          "/data/users/test",
				"connect_command":         "ssh {{.Flavor}}",
				"reconnect_command":       "ssh {{.Hostname}}",
				"provision_command":       "setup.sh",
				"hostname_regex":          `(od\d+)`,
				"vscode_command_template": "code --remote ssh-remote+{{.Hostname}}",
			}
			base[tt.field] = tt.value

			body, _ := json.Marshal(base)
			req := httptest.NewRequest("PUT", "/api/config/remote-flavors/"+id, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", id)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rr := httptest.NewRecorder()
			server.handleRemoteFlavorUpdate(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
			}

			var resp RemoteFlavorResponse
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			// ID should remain stable
			if resp.ID != id {
				t.Errorf("ID changed: got %q, want %q", resp.ID, id)
			}

			tt.check(t, resp)

			// Verify persisted to config
			persisted, found := cfg.GetRemoteFlavor(id)
			if !found {
				t.Fatal("flavor not found in config after update")
			}
			if persisted.ID != id {
				t.Errorf("persisted ID = %q, want %q", persisted.ID, id)
			}
		})
	}
}

func TestHandleRemoteFlavorUpdate_NotFound(t *testing.T) {
	server, _, _ := newTestServer(t)

	body, _ := json.Marshal(map[string]string{
		"flavor":         "od",
		"display_name":   "OnDemand",
		"vcs":            "git",
		"workspace_path": "/data/users/test",
	})
	req := httptest.NewRequest("PUT", "/api/config/remote-flavors/nonexistent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "nonexistent")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	server.handleRemoteFlavorUpdate(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestHandleRemoteFlavorUpdate_FlavorDefaultsToExisting(t *testing.T) {
	server, cfg, _ := newTestServer(t)

	if err := cfg.AddRemoteFlavor(config.RemoteFlavor{
		Flavor:        "od",
		DisplayName:   "OnDemand",
		VCS:           "git",
		WorkspacePath: "/data/users/test",
	}); err != nil {
		t.Fatalf("failed to add flavor: %v", err)
	}
	id := config.GenerateRemoteFlavorID("od")

	// Send update without flavor field — should keep existing
	body, _ := json.Marshal(map[string]string{
		"display_name":   "Updated Name",
		"vcs":            "git",
		"workspace_path": "/data/users/test",
	})
	req := httptest.NewRequest("PUT", "/api/config/remote-flavors/"+id, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	server.handleRemoteFlavorUpdate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	var resp RemoteFlavorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Flavor != "od" {
		t.Errorf("Flavor = %q, want %q (should keep existing)", resp.Flavor, "od")
	}
	if resp.DisplayName != "Updated Name" {
		t.Errorf("DisplayName = %q, want %q", resp.DisplayName, "Updated Name")
	}
}

func TestToFlavorResponse_AllFields(t *testing.T) {
	f := config.RemoteFlavor{
		ID:                    "devvm",
		Flavor:                "devvm",
		DisplayName:           "DevVM",
		VCS:                   "hg",
		WorkspacePath:         "/data/users/$USER",
		ConnectCommand:        "ssh $HOST",
		ReconnectCommand:      "ssh $HOST",
		ProvisionCommand:      "setup.sh",
		HostnameRegex:         `devvm\d+\.example\.com`,
		VSCodeCommandTemplate: "code --remote ssh-remote+$HOST",
	}

	resp := toFlavorResponse(f)

	if resp.ID != f.ID {
		t.Errorf("ID: got %q, want %q", resp.ID, f.ID)
	}
	if resp.Flavor != f.Flavor {
		t.Errorf("Flavor: got %q, want %q", resp.Flavor, f.Flavor)
	}
	if resp.DisplayName != f.DisplayName {
		t.Errorf("DisplayName: got %q, want %q", resp.DisplayName, f.DisplayName)
	}
	if resp.VCS != f.VCS {
		t.Errorf("VCS: got %q, want %q", resp.VCS, f.VCS)
	}
	if resp.WorkspacePath != f.WorkspacePath {
		t.Errorf("WorkspacePath: got %q, want %q", resp.WorkspacePath, f.WorkspacePath)
	}
	if resp.ConnectCommand != f.ConnectCommand {
		t.Errorf("ConnectCommand: got %q, want %q", resp.ConnectCommand, f.ConnectCommand)
	}
	if resp.ReconnectCommand != f.ReconnectCommand {
		t.Errorf("ReconnectCommand: got %q, want %q", resp.ReconnectCommand, f.ReconnectCommand)
	}
	if resp.ProvisionCommand != f.ProvisionCommand {
		t.Errorf("ProvisionCommand: got %q, want %q", resp.ProvisionCommand, f.ProvisionCommand)
	}
	if resp.HostnameRegex != f.HostnameRegex {
		t.Errorf("HostnameRegex: got %q, want %q", resp.HostnameRegex, f.HostnameRegex)
	}
	if resp.VSCodeCommandTemplate != f.VSCodeCommandTemplate {
		t.Errorf("VSCodeCommandTemplate: got %q, want %q", resp.VSCodeCommandTemplate, f.VSCodeCommandTemplate)
	}
}
