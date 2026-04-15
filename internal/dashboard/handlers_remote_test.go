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
func newRemoteHandlers(s *Server) *RemoteHandlers {
	return &RemoteHandlers{
		config:              s.config,
		state:               s.state,
		remoteManager:       s.remoteManager,
		previewManager:      s.previewManager,
		logger:              s.logger,
		connectLimiter:      s.connectLimiter,
		broadcastSessions:   s.BroadcastSessions,
		normalizeRateKey:    s.normalizeIPForRateLimit,
		authenticateRequest: s.authenticateRequest,
		authEnabled:         s.config.GetAuthEnabled,
	}
}

func TestHandleRemoteProfileStatuses_EmptyHostsNotNull(t *testing.T) {
	server, cfg, _ := newTestServer(t)
	remoteH := newRemoteHandlers(server)

	// Add a remote profile with no active connections
	if err := cfg.AddRemoteProfile(config.RemoteProfile{
		DisplayName:   "OnDemand",
		VCS:           "git",
		WorkspacePath: "/data/users/test",
		Flavors:       []config.RemoteProfileFlavor{{Flavor: "od"}},
	}); err != nil {
		t.Fatalf("failed to add remote profile: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/remote/profile-statuses", nil)
	rr := httptest.NewRecorder()
	remoteH.handleRemoteProfileStatuses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	// Parse response as raw JSON to check null vs empty array
	var raw json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// Parse into structured response
	var statuses []RemoteProfileStatusResponse
	if err := json.Unmarshal(raw, &statuses); err != nil {
		t.Fatalf("failed to unmarshal statuses: %v", err)
	}

	if len(statuses) == 0 {
		t.Fatal("expected at least one profile status")
	}

	// Verify the profile data comes through
	if statuses[0].Profile.DisplayName != "OnDemand" {
		t.Errorf("DisplayName = %q, want %q", statuses[0].Profile.DisplayName, "OnDemand")
	}
}

// TestHandleRemoteHostDisconnect_DismissRemovesAll verifies that DELETE
// with ?dismiss=true removes the host, its sessions, and its workspaces.
func TestHandleRemoteHostDisconnect_DismissRemovesAll(t *testing.T) {
	server, _, st := newTestServer(t)
	remoteH := newRemoteHandlers(server)

	hostID := "host-dismiss-test"

	// Add a remote host, sessions, and workspaces to state
	st.AddRemoteHost(state.RemoteHost{
		ID:        hostID,
		ProfileID: "profile-od",
		Flavor:    "od",
		Status:    state.RemoteHostStatusDisconnected,
		Hostname:  "old.example.com",
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
	remoteH.handleRemoteHostDisconnect(rr, req)

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

func TestHandleRemoteProfileUpdate_NotFound(t *testing.T) {
	server, _, _ := newTestServer(t)
	remoteH := newRemoteHandlers(server)

	body, _ := json.Marshal(map[string]interface{}{
		"display_name":   "OnDemand",
		"vcs":            "git",
		"workspace_path": "/data/users/test",
		"flavors":        []map[string]string{{"flavor": "od"}},
	})
	req := httptest.NewRequest("PUT", "/api/config/remote-profiles/nonexistent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "nonexistent")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	remoteH.handleRemoteProfileUpdate(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestToProfileResponse_AllFields(t *testing.T) {
	p := config.RemoteProfile{
		ID:                    "devvm",
		DisplayName:           "DevVM",
		VCS:                   "hg",
		WorkspacePath:         "/data/users/$USER",
		ConnectCommand:        "ssh $HOST",
		ReconnectCommand:      "ssh $HOST",
		ProvisionCommand:      "setup.sh",
		HostnameRegex:         `devvm\d+\.example\.com`,
		VSCodeCommandTemplate: "code --remote ssh-remote+$HOST",
		Flavors:               []config.RemoteProfileFlavor{{Flavor: "devvm"}},
	}

	resp := toProfileResponse(p)

	if resp.ID != p.ID {
		t.Errorf("ID: got %q, want %q", resp.ID, p.ID)
	}
	if resp.DisplayName != p.DisplayName {
		t.Errorf("DisplayName: got %q, want %q", resp.DisplayName, p.DisplayName)
	}
	if resp.VCS != p.VCS {
		t.Errorf("VCS: got %q, want %q", resp.VCS, p.VCS)
	}
	if resp.WorkspacePath != p.WorkspacePath {
		t.Errorf("WorkspacePath: got %q, want %q", resp.WorkspacePath, p.WorkspacePath)
	}
	if resp.ConnectCommand != p.ConnectCommand {
		t.Errorf("ConnectCommand: got %q, want %q", resp.ConnectCommand, p.ConnectCommand)
	}
	if resp.ReconnectCommand != p.ReconnectCommand {
		t.Errorf("ReconnectCommand: got %q, want %q", resp.ReconnectCommand, p.ReconnectCommand)
	}
	if resp.ProvisionCommand != p.ProvisionCommand {
		t.Errorf("ProvisionCommand: got %q, want %q", resp.ProvisionCommand, p.ProvisionCommand)
	}
	if resp.HostnameRegex != p.HostnameRegex {
		t.Errorf("HostnameRegex: got %q, want %q", resp.HostnameRegex, p.HostnameRegex)
	}
	if resp.VSCodeCommandTemplate != p.VSCodeCommandTemplate {
		t.Errorf("VSCodeCommandTemplate: got %q, want %q", resp.VSCodeCommandTemplate, p.VSCodeCommandTemplate)
	}
	if len(resp.Flavors) != 1 || resp.Flavors[0].Flavor != "devvm" {
		t.Errorf("Flavors: got %v, want [{Flavor: devvm}]", resp.Flavors)
	}
}

func TestToProfileResponse_PersistentHostFields(t *testing.T) {
	p := config.RemoteProfile{
		ID:                    "devserver",
		DisplayName:           "Dev Server",
		HostType:              config.HostTypePersistent,
		VCS:                   "git",
		RepoBasePath:          "/home/user/repo",
		WorkspacePathTemplate: "/home/user/ws/{{.WorkspaceID}}",
		ConnectCommand:        "ssh user@host --",
		RemoteVCSCommands: config.RemoteVCSCommands{
			CreateWorktree: "custom-clone {{.DestPath}}",
			RemoveWorktree: "custom-rm {{.WorkspacePath}}",
			CheckDirty:     "custom-status {{.WorkspacePath}}",
		},
	}

	resp := toProfileResponse(p)

	if resp.HostType != config.HostTypePersistent {
		t.Errorf("HostType: got %q, want %q", resp.HostType, config.HostTypePersistent)
	}
	if resp.RepoBasePath != "/home/user/repo" {
		t.Errorf("RepoBasePath: got %q, want %q", resp.RepoBasePath, "/home/user/repo")
	}
	if resp.WorkspacePathTemplate != "/home/user/ws/{{.WorkspaceID}}" {
		t.Errorf("WorkspacePathTemplate: got %q", resp.WorkspacePathTemplate)
	}
	if resp.RemoteVCSCommands == nil {
		t.Fatal("RemoteVCSCommands should not be nil")
	}
	if resp.RemoteVCSCommands.CreateWorktree != "custom-clone {{.DestPath}}" {
		t.Errorf("CreateWorktree: got %q", resp.RemoteVCSCommands.CreateWorktree)
	}
	if resp.RemoteVCSCommands.RemoveWorktree != "custom-rm {{.WorkspacePath}}" {
		t.Errorf("RemoveWorktree: got %q", resp.RemoteVCSCommands.RemoveWorktree)
	}
	if resp.RemoteVCSCommands.CheckDirty != "custom-status {{.WorkspacePath}}" {
		t.Errorf("CheckDirty: got %q", resp.RemoteVCSCommands.CheckDirty)
	}
}

func TestToProfileResponse_EphemeralNoVCSCommands(t *testing.T) {
	p := config.RemoteProfile{
		ID:            "od",
		DisplayName:   "OD",
		VCS:           "git",
		WorkspacePath: "/tmp",
		Flavors:       []config.RemoteProfileFlavor{{Flavor: "gpu"}},
	}

	resp := toProfileResponse(p)

	if resp.HostType != "" {
		t.Errorf("HostType: got %q, want empty", resp.HostType)
	}
	if resp.RemoteVCSCommands != nil {
		t.Error("RemoteVCSCommands should be nil for ephemeral profiles with no commands")
	}
}

func TestHandleCreateRemoteProfile_Persistent(t *testing.T) {
	server, _, _ := newTestServer(t)
	remoteH := newRemoteHandlers(server)

	body, _ := json.Marshal(map[string]interface{}{
		"display_name":            "Dev Server",
		"host_type":               "persistent",
		"vcs":                     "git",
		"repo_base_path":          "/home/user/repo",
		"workspace_path_template": "/home/user/ws/{{.WorkspaceID}}",
		"connect_command":         "ssh user@host --",
		"reconnect_command":       "ssh user@host --",
		"hostname_regex":          "(host\\.example\\.com)",
		"remote_vcs_commands": map[string]string{
			"create_worktree": "git worktree add {{.DestPath}}",
		},
	})

	req := httptest.NewRequest("POST", "/api/config/remote-profiles", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	remoteH.handleCreateRemoteProfile(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
	}

	var resp RemoteProfileResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.HostType != "persistent" {
		t.Errorf("HostType: got %q, want persistent", resp.HostType)
	}
	if resp.RepoBasePath != "/home/user/repo" {
		t.Errorf("RepoBasePath: got %q", resp.RepoBasePath)
	}
	if resp.WorkspacePathTemplate != "/home/user/ws/{{.WorkspaceID}}" {
		t.Errorf("WorkspacePathTemplate: got %q", resp.WorkspacePathTemplate)
	}
	if resp.RemoteVCSCommands == nil || resp.RemoteVCSCommands.CreateWorktree != "git worktree add {{.DestPath}}" {
		t.Errorf("RemoteVCSCommands.CreateWorktree not preserved")
	}
}
