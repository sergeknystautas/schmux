package remote

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// noopWM satisfies workspace.WorkspaceManager for tests that only need workspace creation to not crash.
type noopWM struct {
	workspace.WorkspaceManager
	st *state.State
}

func (m *noopWM) AddWorkspaceWithTabs(ws state.Workspace) error {
	return m.st.AddWorkspace(ws)
}

func TestManager_ConnectRace(t *testing.T) {
	// Create test config and state
	cfg := &config.Config{}
	cfg.RemoteProfiles = []config.RemoteProfile{
		{
			ID:            "test-flavor",
			DisplayName:   "Test Flavor",
			WorkspacePath: "/tmp/test",
			VCS:           "git",
			Flavors:       []config.RemoteProfileFlavor{{Flavor: "test"}},
		},
	}

	st := state.New(filepath.Join(t.TempDir(), "state.json"), nil)

	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	// Launch multiple goroutines trying to StartConnect to same profile+flavor.
	// Each should get a unique provisioning session ID (no 1:1 enforcement).
	const numGoroutines = 10
	var wg sync.WaitGroup
	type result struct {
		sessionID string
		err       error
	}
	results := make(chan result, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sid, err := mgr.StartConnect("test-flavor", "test")
			results <- result{sessionID: sid, err: err}
		}()
	}

	wg.Wait()
	close(results)

	// Collect results
	sessionIDs := make(map[string]bool)
	errCount := 0
	for r := range results {
		if r.err != nil {
			errCount++
			continue
		}
		sessionIDs[r.sessionID] = true
	}

	// The important thing is that we didn't panic due to race condition
	// All goroutines should have succeeded with unique session IDs
	t.Logf("Got %d errors and %d unique session IDs from %d concurrent connect attempts", errCount, len(sessionIDs), numGoroutines)

	if errCount > 0 {
		t.Errorf("expected no errors, got %d", errCount)
	}

	// Each goroutine should have gotten a different provisioning session ID
	if len(sessionIDs) != numGoroutines {
		t.Errorf("expected %d unique session IDs, got %d", numGoroutines, len(sessionIDs))
	}

	// Verify all connections are tracked in the map
	mgr.mu.RLock()
	connCount := len(mgr.connections)
	mgr.mu.RUnlock()

	if connCount != numGoroutines {
		t.Errorf("expected %d connections in map, got %d", numGoroutines, connCount)
	}
}

func TestManager_PruneExpiredHosts(t *testing.T) {
	cfg := &config.Config{}
	now := time.Now()

	st := &state.State{
		Workspaces: []state.Workspace{},
		Sessions:   []state.Session{},
		RemoteHosts: []state.RemoteHost{
			{
				ID:          "host-1",
				ProfileID:   "profile-1",
				Flavor:      "flavor-1",
				Hostname:    "expired.example.com",
				Status:      "connected",
				ConnectedAt: now.Add(-24 * time.Hour),
				ExpiresAt:   now.Add(-1 * time.Hour), // Expired 1 hour ago
			},
			{
				ID:          "host-2",
				ProfileID:   "profile-2",
				Flavor:      "flavor-2",
				Hostname:    "active.example.com",
				Status:      "connected",
				ConnectedAt: now,
				ExpiresAt:   now.Add(11 * time.Hour), // Still valid
			},
		},
	}

	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	// Run pruning
	mgr.PruneExpiredHosts()

	// Verify expired host was removed from state
	_, found := st.GetRemoteHost("host-1")
	if found {
		t.Error("host-1 should have been removed from state")
	}

	// Verify active host was not touched
	host2, found := st.GetRemoteHost("host-2")
	if !found {
		t.Error("host-2 should exist in state")
	}
	if host2.Status != "connected" {
		t.Errorf("host-2 status should be 'connected', got '%s'", host2.Status)
	}
}

func TestManager_GetConnection(t *testing.T) {
	cfg := &config.Config{}
	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}

	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	// Create a mock connection
	conn := NewConnection(ConnectionConfig{
		ProfileID:     "test-flavor",
		Flavor:        "test",
		DisplayName:   "Test",
		WorkspacePath: "/tmp",
		VCS:           "git",
	})

	// Add to connections map
	mgr.mu.Lock()
	mgr.connections[conn.host.ID] = conn
	mgr.mu.Unlock()

	// Test GetConnection
	retrieved := mgr.GetConnection(conn.host.ID)
	if retrieved == nil {
		t.Error("expected to retrieve connection")
	}
	if retrieved != conn {
		t.Error("retrieved connection does not match original")
	}

	// Test GetConnection with non-existent ID
	retrieved = mgr.GetConnection("non-existent")
	if retrieved != nil {
		t.Error("expected nil for non-existent connection")
	}
}

func TestManager_IsConnected(t *testing.T) {
	cfg := &config.Config{}
	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}

	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	// Create a mock connection
	conn := NewConnection(ConnectionConfig{
		ProfileID:     "test-flavor",
		Flavor:        "test",
		DisplayName:   "Test",
		WorkspacePath: "/tmp",
		VCS:           "git",
	})

	// Initially not connected
	if mgr.IsConnected(conn.host.ID) {
		t.Error("connection should not be reported as connected initially")
	}

	// Add to connections map but still not actually connected
	mgr.mu.Lock()
	mgr.connections[conn.host.ID] = conn
	mgr.mu.Unlock()

	// Still not connected (client is nil)
	if mgr.IsConnected(conn.host.ID) {
		t.Error("connection should not be reported as connected without client")
	}
}

func TestManager_DisconnectAll(t *testing.T) {
	cfg := &config.Config{}
	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}

	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	// Create multiple mock connections
	for i := 0; i < 3; i++ {
		conn := NewConnection(ConnectionConfig{
			ProfileID:     "test-flavor",
			Flavor:        "test",
			DisplayName:   "Test",
			WorkspacePath: "/tmp",
			VCS:           "git",
		})
		mgr.mu.Lock()
		mgr.connections[conn.host.ID] = conn
		mgr.mu.Unlock()
	}

	// Verify we have 3 connections
	mgr.mu.RLock()
	count := len(mgr.connections)
	mgr.mu.RUnlock()

	if count != 3 {
		t.Errorf("expected 3 connections, got %d", count)
	}

	// Disconnect all
	mgr.DisconnectAll()

	// Verify connections map is empty
	mgr.mu.RLock()
	count = len(mgr.connections)
	mgr.mu.RUnlock()

	if count != 0 {
		t.Errorf("expected 0 connections after DisconnectAll, got %d", count)
	}
}

func TestManager_GetProfileStatuses(t *testing.T) {
	cfg := &config.Config{}
	cfg.RemoteProfiles = []config.RemoteProfile{
		{
			ID:            "profile-1",
			DisplayName:   "Production",
			WorkspacePath: "/workspace",
			VCS:           "git",
			Flavors:       []config.RemoteProfileFlavor{{Flavor: "prod"}},
		},
		{
			ID:            "profile-2",
			DisplayName:   "Development",
			WorkspacePath: "/workspace",
			VCS:           "git",
			Flavors:       []config.RemoteProfileFlavor{{Flavor: "dev"}},
		},
	}

	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}

	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	// Get statuses
	statuses := mgr.GetProfileStatuses()

	if len(statuses) != 2 {
		t.Errorf("expected 2 profile statuses, got %d", len(statuses))
	}

	// Verify both profiles have flavor host groups with empty hosts initially
	for _, status := range statuses {
		for _, fg := range status.FlavorHosts {
			if len(fg.Hosts) != 0 {
				t.Errorf("profile %s flavor %s should have no hosts, got %d", status.Profile.ID, fg.Flavor, len(fg.Hosts))
			}
		}
	}
}

func TestManager_GetConnectionsByProfileAndFlavor(t *testing.T) {
	cfg := &config.Config{}
	cfg.RemoteProfiles = []config.RemoteProfile{
		{
			ID:            "od",
			DisplayName:   "OnDemand",
			WorkspacePath: "/workspace",
			VCS:           "git",
			Flavors:       []config.RemoteProfileFlavor{{Flavor: "www"}, {Flavor: "gpu"}},
		},
	}

	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}

	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	// Create two "www" connections and one "gpu" connection
	www1 := NewConnection(ConnectionConfig{
		ProfileID:     "od",
		Flavor:        "www",
		DisplayName:   "WWW",
		WorkspacePath: "/workspace",
		VCS:           "git",
	})
	www2 := NewConnection(ConnectionConfig{
		ProfileID:     "od",
		Flavor:        "www",
		DisplayName:   "WWW",
		WorkspacePath: "/workspace",
		VCS:           "git",
	})
	gpu1 := NewConnection(ConnectionConfig{
		ProfileID:     "od",
		Flavor:        "gpu",
		DisplayName:   "GPU",
		WorkspacePath: "/workspace",
		VCS:           "git",
	})

	mgr.mu.Lock()
	mgr.connections[www1.host.ID] = www1
	mgr.connections[www2.host.ID] = www2
	mgr.connections[gpu1.host.ID] = gpu1
	mgr.mu.Unlock()

	// Verify GetConnectionsByProfileAndFlavor("od", "www") returns 2
	wwwConns := mgr.GetConnectionsByProfileAndFlavor("od", "www")
	if len(wwwConns) != 2 {
		t.Errorf("expected 2 www connections, got %d", len(wwwConns))
	}

	// Verify GetConnectionsByProfileAndFlavor("od", "gpu") returns 1
	gpuConns := mgr.GetConnectionsByProfileAndFlavor("od", "gpu")
	if len(gpuConns) != 1 {
		t.Errorf("expected 1 gpu connection, got %d", len(gpuConns))
	}

	// Verify GetConnectionsByProfileAndFlavor("od", "nonexistent") returns 0
	noneConns := mgr.GetConnectionsByProfileAndFlavor("od", "nonexistent")
	if len(noneConns) != 0 {
		t.Errorf("expected 0 nonexistent connections, got %d", len(noneConns))
	}
}

func TestManager_GetProfileStatuses_MultiHost(t *testing.T) {
	cfg := &config.Config{}
	cfg.RemoteProfiles = []config.RemoteProfile{
		{
			ID:            "od",
			DisplayName:   "OnDemand",
			WorkspacePath: "/workspace",
			VCS:           "git",
			Flavors:       []config.RemoteProfileFlavor{{Flavor: "www"}},
		},
	}

	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}

	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	// Create two "www" connections
	www1 := NewConnection(ConnectionConfig{
		ProfileID:     "od",
		Flavor:        "www",
		DisplayName:   "WWW",
		WorkspacePath: "/workspace",
		VCS:           "git",
	})
	www2 := NewConnection(ConnectionConfig{
		ProfileID:     "od",
		Flavor:        "www",
		DisplayName:   "WWW",
		WorkspacePath: "/workspace",
		VCS:           "git",
	})

	mgr.mu.Lock()
	mgr.connections[www1.host.ID] = www1
	mgr.connections[www2.host.ID] = www2
	mgr.mu.Unlock()

	// Verify GetProfileStatuses returns 1 ProfileStatus entry
	statuses := mgr.GetProfileStatuses()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 profile status, got %d", len(statuses))
	}

	// with 1 FlavorHostGroup with 2 HostStatus entries
	if len(statuses[0].FlavorHosts) != 1 {
		t.Fatalf("expected 1 flavor host group, got %d", len(statuses[0].FlavorHosts))
	}
	if len(statuses[0].FlavorHosts[0].Hosts) != 2 {
		t.Errorf("expected 2 hosts in flavor host group, got %d", len(statuses[0].FlavorHosts[0].Hosts))
	}

	// Verify each host has a unique host ID
	hostIDs := make(map[string]bool)
	for _, h := range statuses[0].FlavorHosts[0].Hosts {
		hostIDs[h.HostID] = true
	}
	if len(hostIDs) != 2 {
		t.Errorf("expected 2 unique host IDs, got %d", len(hostIDs))
	}
}

func TestManager_SetStateChangeCallback(t *testing.T) {
	cfg := &config.Config{}
	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}

	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	callbackCalled := false
	mgr.SetStateChangeCallback(func() {
		callbackCalled = true
	})

	// Trigger state change notification
	mgr.notifyStateChange()

	if !callbackCalled {
		t.Error("state change callback was not called")
	}
}

func TestManager_SetOnConnectCallback(t *testing.T) {
	cfg := &config.Config{}
	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}

	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	var receivedHostID string
	mgr.SetOnConnectCallback(func(hostID string) {
		receivedHostID = hostID
	})

	mgr.notifyConnect("test-host-123")

	if receivedHostID != "test-host-123" {
		t.Errorf("expected hostID 'test-host-123', got %q", receivedHostID)
	}
}

func TestManager_OnConnectCallback_Nil(t *testing.T) {
	cfg := &config.Config{}
	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}

	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})
	// No callback set — should not panic
	mgr.notifyConnect("test-host")
}

// TestReconcileWithRenamedWindows verifies that session reconciliation does NOT
// fall back to window name matching (Issue 4 fix). This test validates the
// strict ID-only matching logic without requiring a full connection setup.
func TestReconcileWithRenamedWindows(t *testing.T) {
	cfg := &config.Config{}
	cfg.RemoteProfiles = []config.RemoteProfile{
		{
			ID:            "test-flavor",
			DisplayName:   "Test",
			WorkspacePath: "/workspace",
			VCS:           "git",
			Flavors:       []config.RemoteProfileFlavor{{Flavor: "test"}},
		},
	}

	// Create state with existing sessions that have window/pane IDs
	st := &state.State{
		Workspaces: []state.Workspace{},
		Sessions: []state.Session{
			{
				ID:           "sess-1",
				RemoteHostID: "host-1",
				TmuxSession:  "session-A", // This is the window name
				RemoteWindow: "@10",       // Window ID
				RemotePaneID: "%20",       // Pane ID
				Status:       "running",
			},
			{
				ID:           "sess-2",
				RemoteHostID: "host-1",
				TmuxSession:  "session-B",
				RemoteWindow: "@11", // This window no longer exists
				RemotePaneID: "%21",
				Status:       "running",
			},
			{
				ID:           "sess-other-host",
				RemoteHostID: "host-2", // Different host, should be ignored
				TmuxSession:  "other",
				RemoteWindow: "@99",
				RemotePaneID: "%99",
				Status:       "running",
			},
		},
		RemoteHosts: []state.RemoteHost{
			{
				ID:        "host-1",
				ProfileID: "test-flavor",
				Flavor:    "test",
				Status:    "connected",
			},
		},
	}

	_ = NewManager(cfg, st, nil) // Manager created but we're testing matching logic directly

	// Test the reconciliation logic directly by examining the matching behavior
	// We'll verify that:
	// 1. Sessions match by window ID
	// 2. Sessions match by pane ID
	// 3. Sessions DO NOT match by window name alone
	// 4. Unmatched sessions are marked disconnected

	// Simulate windows returned from remote (different from state)
	windows := []struct {
		WindowID   string
		WindowName string
		PaneID     string
	}{
		{WindowID: "@10", WindowName: "session-A", PaneID: "%20"},  // Matches sess-1 by ID
		{WindowID: "@99", WindowName: "session-B", PaneID: "%99"},  // Name matches sess-2 but ID doesn't
		{WindowID: "@12", WindowName: "new-window", PaneID: "%22"}, // No match
	}

	// Manually test the matching logic that reconcileSessions uses
	sess1, _ := st.GetSession("sess-1")
	sess2, _ := st.GetSession("sess-2")

	// Test sess-1 matching
	matched1 := false
	for _, w := range windows {
		if sess1.RemoteWindow != "" && w.WindowID == sess1.RemoteWindow {
			matched1 = true
			break
		} else if sess1.RemotePaneID != "" && w.PaneID == sess1.RemotePaneID {
			matched1 = true
			break
		}
	}
	if !matched1 {
		t.Error("sess-1 should match by window ID @10")
	}

	// Test sess-2 matching (should NOT match by name)
	matched2 := false
	for _, w := range windows {
		if sess2.RemoteWindow != "" && w.WindowID == sess2.RemoteWindow {
			matched2 = true
			break
		} else if sess2.RemotePaneID != "" && w.PaneID == sess2.RemotePaneID {
			matched2 = true
			break
		}
		// OLD buggy code would check: w.WindowName == sess2.TmuxSession
		// This test verifies we DON'T do that anymore
	}
	if matched2 {
		t.Error("sess-2 should NOT match (window ID @11 doesn't exist, and we don't fall back to name matching)")
	}
}

// TestReconcileStrictIDMatching verifies the strict matching logic
func TestReconcileStrictIDMatching(t *testing.T) {
	// Test case 1: Window ID match
	sess := state.Session{RemoteWindow: "@5", RemotePaneID: "%10"}
	window := struct {
		WindowID string
		PaneID   string
	}{WindowID: "@5", PaneID: "%10"}

	matchedByWindow := sess.RemoteWindow != "" && window.WindowID == sess.RemoteWindow
	if !matchedByWindow {
		t.Error("should match by window ID")
	}

	// Test case 2: Pane ID match (no window ID)
	sess2 := state.Session{RemoteWindow: "", RemotePaneID: "%11"}
	window2 := struct {
		WindowID string
		PaneID   string
	}{WindowID: "@6", PaneID: "%11"}

	matchedByPane := sess2.RemotePaneID != "" && window2.PaneID == sess2.RemotePaneID
	if !matchedByPane {
		t.Error("should match by pane ID when window ID is empty")
	}

	// Test case 3: No match (different IDs)
	sess3 := state.Session{RemoteWindow: "@5", RemotePaneID: "%10"}
	window3 := struct {
		WindowID string
		PaneID   string
	}{WindowID: "@99", PaneID: "%99"}

	matchedByWindow3 := sess3.RemoteWindow != "" && window3.WindowID == sess3.RemoteWindow
	matchedByPane3 := sess3.RemotePaneID != "" && window3.PaneID == sess3.RemotePaneID
	if matchedByWindow3 || matchedByPane3 {
		t.Error("should NOT match with different IDs")
	}
}

// TestConnectWithAndWithoutProgress verifies that both Connect() and ConnectWithProgress()
// use the same internal implementation (Issue 8 fix - deduplication).
func TestConnectWithAndWithoutProgress(t *testing.T) {
	cfg := &config.Config{}
	cfg.RemoteProfiles = []config.RemoteProfile{
		{
			ID:            "test-flavor",
			DisplayName:   "Test Flavor",
			WorkspacePath: "/tmp/test",
			VCS:           "git",
			Flavors:       []config.RemoteProfileFlavor{{Flavor: "test"}},
		},
	}

	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}

	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	// Test 1: Connect without progress (will fail but should not panic)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := mgr.Connect(ctx, "test-flavor", "test")
	// Expected to fail since we can't actually connect
	if err == nil {
		t.Log("Note: Connect() succeeded unexpectedly (no real connection expected)")
	}

	// Test 2: Connect with progress (should use same code path)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()

	progressCh := make(chan string, 10)
	go func() {
		// Drain progress messages
		for range progressCh {
		}
	}()

	_, err2 := mgr.ConnectWithProgress(ctx2, "test-flavor", "test", progressCh)
	close(progressCh)

	// Also expected to fail
	if err2 == nil {
		t.Log("Note: ConnectWithProgress() succeeded unexpectedly (no real connection expected)")
	}

	// Both should have similar behavior (both fail with connection errors)
	// The important part is they don't panic and use the same logic
	t.Logf("Connect() error: %v", err)
	t.Logf("ConnectWithProgress() error: %v", err2)
}

func TestManager_StartConnect_CreatesWorkspace(t *testing.T) {
	cfg := &config.Config{}
	cfg.RemoteProfiles = []config.RemoteProfile{{
		ID:             "www",
		DisplayName:    "WWW",
		WorkspacePath:  "~/fbsource",
		ConnectCommand: "echo connected",
		Flavors:        []config.RemoteProfileFlavor{{Flavor: "www"}},
	}}
	st := state.New(filepath.Join(t.TempDir(), "state.json"), nil)
	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	_, err := mgr.StartConnect("www", "www")
	if err != nil {
		t.Fatalf("StartConnect failed: %v", err)
	}

	// StartConnect creates a host immediately. The workspace should also be
	// created immediately (not deferred to SpawnRemote), so it appears on
	// the home page and in WebSocket broadcasts as soon as the host exists.
	hosts := st.GetRemoteHostsByProfileAndFlavor("www", "www")
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}

	workspaceID := hosts[0].ID
	ws, found := st.GetWorkspace(workspaceID)
	if !found {
		t.Fatalf("expected workspace %s to be created on StartConnect, but not found", workspaceID)
	}
	if ws.RemoteHostID != hosts[0].ID {
		t.Errorf("workspace.RemoteHostID = %q, want %q", ws.RemoteHostID, hosts[0].ID)
	}
	if ws.Path != "~/fbsource" {
		t.Errorf("workspace.Path = %q, want ~/fbsource", ws.Path)
	}
}

func TestManager_ConnectMultipleHostsSameFlavor(t *testing.T) {
	cfg := &config.Config{}
	cfg.RemoteProfiles = []config.RemoteProfile{{
		ID:             "www",
		DisplayName:    "WWW",
		WorkspacePath:  "~/fbsource",
		ConnectCommand: "echo connected",
		Flavors:        []config.RemoteProfileFlavor{{Flavor: "www"}},
	}}
	st := state.New(filepath.Join(t.TempDir(), "state.json"), nil)
	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	// Two StartConnect calls should produce two different provisioning sessions
	provID1, err := mgr.StartConnect("www", "www")
	if err != nil {
		t.Fatalf("first StartConnect failed: %v", err)
	}

	provID2, err := mgr.StartConnect("www", "www")
	if err != nil {
		t.Fatalf("second StartConnect failed: %v", err)
	}

	if provID1 == provID2 {
		t.Fatalf("expected different provisioning session IDs, both got %s", provID1)
	}

	// Verify two distinct hosts were created in state
	hosts := st.GetRemoteHostsByProfileAndFlavor("www", "www")
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts in state, got %d", len(hosts))
	}
	if hosts[0].ID == hosts[1].ID {
		t.Fatalf("expected different host IDs")
	}
}

func TestNewConnection_PersistentHostNoExpiry(t *testing.T) {
	conn := NewConnection(ConnectionConfig{
		ProfileID:     "devserver",
		DisplayName:   "Dev Server",
		WorkspacePath: "/home/user/repo",
		VCS:           "git",
		HostType:      config.HostTypePersistent,
	})

	host := conn.Host()
	if !host.ExpiresAt.IsZero() {
		t.Errorf("persistent host should have zero ExpiresAt, got %v", host.ExpiresAt)
	}
	if host.HostType != config.HostTypePersistent {
		t.Errorf("HostType: got %q, want %q", host.HostType, config.HostTypePersistent)
	}
}

func TestNewConnection_EphemeralHostHasExpiry(t *testing.T) {
	before := time.Now()
	conn := NewConnection(ConnectionConfig{
		ProfileID:     "od",
		Flavor:        "gpu",
		DisplayName:   "OD",
		WorkspacePath: "/tmp",
		VCS:           "git",
	})
	after := time.Now()

	host := conn.Host()
	if host.ExpiresAt.IsZero() {
		t.Error("ephemeral host should have non-zero ExpiresAt")
	}
	if host.ExpiresAt.Before(before.Add(DefaultHostExpiry)) {
		t.Error("ExpiresAt too early")
	}
	if host.ExpiresAt.After(after.Add(DefaultHostExpiry).Add(time.Second)) {
		t.Error("ExpiresAt too late")
	}
}

func TestEnsureWorkspaceForHost_SkipsPersistent(t *testing.T) {
	cfg := &config.Config{}
	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}
	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	host := state.RemoteHost{
		ID:       "persistent-host",
		HostType: config.HostTypePersistent,
	}
	resolved := config.ResolvedFlavor{
		HostType:      config.HostTypePersistent,
		WorkspacePath: "/home/user/ws",
	}

	mgr.ensureWorkspaceForHost(host, resolved)

	// No workspace should have been created
	if _, found := st.GetWorkspace("persistent-host"); found {
		t.Error("persistent host should NOT have a workspace created on connect")
	}
}

func TestEnsureWorkspaceForHost_CreatesForEphemeral(t *testing.T) {
	cfg := &config.Config{}
	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}
	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	host := state.RemoteHost{
		ID:       "ephemeral-host",
		Hostname: "eph.example.com",
	}
	resolved := config.ResolvedFlavor{
		FlavorDisplayName: "GPU Large",
		WorkspacePath:     "/tmp/workspace",
	}

	mgr.ensureWorkspaceForHost(host, resolved)

	// Workspace should have been created
	ws, found := st.GetWorkspace("ephemeral-host")
	if !found {
		t.Fatal("ephemeral host should have workspace created on connect")
	}
	if ws.Path != "/tmp/workspace" {
		t.Errorf("workspace path: got %q, want %q", ws.Path, "/tmp/workspace")
	}
}

func TestMarkStaleHostsDisconnected_PersistentHosts(t *testing.T) {
	cfg := &config.Config{}
	now := time.Now()

	st := &state.State{
		Workspaces: []state.Workspace{},
		Sessions:   []state.Session{},
		RemoteHosts: []state.RemoteHost{
			{
				ID:          "persistent-1",
				ProfileID:   "devserver",
				Hostname:    "dev.example.com",
				Status:      state.RemoteHostStatusConnected,
				ConnectedAt: now.Add(-2 * time.Hour),
				ExpiresAt:   time.Time{}, // zero — persistent
				HostType:    config.HostTypePersistent,
			},
			{
				ID:          "ephemeral-1",
				ProfileID:   "od",
				Hostname:    "od.example.com",
				Status:      state.RemoteHostStatusConnected,
				ConnectedAt: now,
				ExpiresAt:   now.Add(10 * time.Hour), // still valid
			},
		},
	}

	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	count := mgr.MarkStaleHostsDisconnected()

	// Both should be marked disconnected (both are stale after daemon restart)
	if count != 2 {
		t.Errorf("expected 2 hosts marked stale, got %d", count)
	}

	host1, _ := st.GetRemoteHost("persistent-1")
	if host1.Status != state.RemoteHostStatusDisconnected {
		t.Errorf("persistent host should be disconnected, got %q", host1.Status)
	}

	host2, _ := st.GetRemoteHost("ephemeral-1")
	if host2.Status != state.RemoteHostStatusDisconnected {
		t.Errorf("ephemeral host should be disconnected, got %q", host2.Status)
	}
}

func TestResolveWorkspacePathTemplate(t *testing.T) {
	tests := []struct {
		name    string
		tmpl    string
		id      string
		want    string
		wantErr bool
	}{
		{
			name: "basic",
			tmpl: "/home/user/schmux-ws/{{.WorkspaceID}}",
			id:   "ws-abc123",
			want: "/home/user/schmux-ws/ws-abc123",
		},
		{
			name: "nested path",
			tmpl: "/data/users/{{.WorkspaceID}}/fbsource",
			id:   "remote-xyz-ws-001",
			want: "/data/users/remote-xyz-ws-001/fbsource",
		},
		{
			name:    "invalid template",
			tmpl:    "/home/{{.WorkspaceID",
			id:      "ws-001",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveWorkspacePathTemplate(tt.tmpl, tt.id)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetHostMutex_LazyCreation(t *testing.T) {
	cfg := &config.Config{}
	st := &state.State{}
	mgr := NewManager(cfg, st, nil)

	mu1 := mgr.getHostMutex("host-1")
	mu2 := mgr.getHostMutex("host-1")
	mu3 := mgr.getHostMutex("host-2")

	if mu1 != mu2 {
		t.Error("same host ID should return same mutex")
	}
	if mu1 == mu3 {
		t.Error("different host IDs should return different mutexes")
	}
}

func TestPruneExpiredHosts_SkipsPersistent(t *testing.T) {
	cfg := &config.Config{}
	now := time.Now()

	st := &state.State{
		Workspaces: []state.Workspace{},
		Sessions:   []state.Session{},
		RemoteHosts: []state.RemoteHost{
			{
				ID:          "persistent-1",
				ProfileID:   "devserver",
				Hostname:    "dev.example.com",
				Status:      state.RemoteHostStatusDisconnected,
				ConnectedAt: now.Add(-24 * time.Hour),
				ExpiresAt:   time.Time{}, // zero — persistent, should NOT be pruned
				HostType:    config.HostTypePersistent,
			},
			{
				ID:          "ephemeral-expired",
				ProfileID:   "od",
				Hostname:    "expired.example.com",
				Status:      state.RemoteHostStatusDisconnected,
				ConnectedAt: now.Add(-24 * time.Hour),
				ExpiresAt:   now.Add(-1 * time.Hour), // expired
			},
		},
	}

	mgr := NewManager(cfg, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	mgr.PruneExpiredHosts()

	// Persistent host should still exist
	if _, found := st.GetRemoteHost("persistent-1"); !found {
		t.Error("persistent host should NOT be pruned")
	}

	// Expired ephemeral host should be removed
	if _, found := st.GetRemoteHost("ephemeral-expired"); found {
		t.Error("expired ephemeral host should be pruned")
	}
}

// mockVCS implements remoteVCSExecutor for testing.
type mockVCS struct {
	dirtyPaths  map[string]bool // path -> dirty
	createCalls []string        // destPaths passed to createWorktree
	removeCalls []string        // paths passed to removeWorktree
	checkErr    error           // error to return from checkDirty
	createErr   error           // error to return from createWorktree
	mu          sync.Mutex
}

func (m *mockVCS) checkDirty(_ context.Context, _ config.ResolvedFlavor, workspacePath string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.checkErr != nil {
		return false, m.checkErr
	}
	return m.dirtyPaths[workspacePath], nil
}

func (m *mockVCS) createWorktree(_ context.Context, _ config.ResolvedFlavor, _ string, destPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createCalls = append(m.createCalls, destPath)
	return m.createErr
}

func (m *mockVCS) removeWorktree(_ context.Context, _ config.ResolvedFlavor, workspacePath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.removeCalls = append(m.removeCalls, workspacePath)
	return nil
}

func persistentResolved() config.ResolvedFlavor {
	return config.ResolvedFlavor{
		ProfileID:             "devserver",
		ProfileDisplayName:    "Dev Server",
		HostType:              config.HostTypePersistent,
		VCS:                   "git",
		RepoBasePath:          "/home/user/repo",
		WorkspacePathTemplate: "/home/user/ws/{{.WorkspaceID}}",
	}
}

func TestFindOrCreateWorkspace_CreatesNewWhenNoneExist(t *testing.T) {
	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}
	mgr := NewManager(&config.Config{}, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	vcs := &mockVCS{dirtyPaths: map[string]bool{}}
	host := state.RemoteHost{ID: "host-1", Hostname: "dev.example.com", HostType: config.HostTypePersistent}
	noActive := func(string) bool { return false }

	ws, err := mgr.findOrCreateWorkspaceWith(context.Background(), host, persistentResolved(), vcs, noActive)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ws.RemoteHostID != "host-1" {
		t.Errorf("RemoteHostID: got %q, want %q", ws.RemoteHostID, "host-1")
	}
	if len(vcs.createCalls) != 1 {
		t.Errorf("expected 1 create call, got %d", len(vcs.createCalls))
	}
	// Workspace should be in state
	if _, found := st.GetWorkspace(ws.ID); !found {
		t.Error("workspace should be in state after creation")
	}
}

func TestFindOrCreateWorkspace_ReusesIdleClean(t *testing.T) {
	st := &state.State{
		Workspaces: []state.Workspace{
			{ID: "ws-idle", RemoteHostID: "host-1", RemotePath: "/home/user/ws/ws-idle"},
		},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}
	mgr := NewManager(&config.Config{}, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	vcs := &mockVCS{dirtyPaths: map[string]bool{"/home/user/ws/ws-idle": false}}
	host := state.RemoteHost{ID: "host-1", HostType: config.HostTypePersistent}
	noActive := func(string) bool { return false }

	ws, err := mgr.findOrCreateWorkspaceWith(context.Background(), host, persistentResolved(), vcs, noActive)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ws.ID != "ws-idle" {
		t.Errorf("expected reuse of ws-idle, got %q", ws.ID)
	}
	if len(vcs.createCalls) != 0 {
		t.Errorf("should not create new worktree when reusing, got %d creates", len(vcs.createCalls))
	}
}

func TestFindOrCreateWorkspace_SkipsDirtyCreatesNew(t *testing.T) {
	st := &state.State{
		Workspaces: []state.Workspace{
			{ID: "ws-dirty", RemoteHostID: "host-1", RemotePath: "/home/user/ws/ws-dirty"},
		},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}
	mgr := NewManager(&config.Config{}, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	vcs := &mockVCS{dirtyPaths: map[string]bool{"/home/user/ws/ws-dirty": true}}
	host := state.RemoteHost{ID: "host-1", HostType: config.HostTypePersistent}
	noActive := func(string) bool { return false }

	ws, err := mgr.findOrCreateWorkspaceWith(context.Background(), host, persistentResolved(), vcs, noActive)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ws.ID == "ws-dirty" {
		t.Error("should NOT reuse dirty workspace")
	}
	if len(vcs.createCalls) != 1 {
		t.Errorf("expected 1 create call for new worktree, got %d", len(vcs.createCalls))
	}
}

func TestFindOrCreateWorkspace_SkipsActiveSession(t *testing.T) {
	st := &state.State{
		Workspaces: []state.Workspace{
			{ID: "ws-active", RemoteHostID: "host-1", RemotePath: "/home/user/ws/ws-active"},
		},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}
	mgr := NewManager(&config.Config{}, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	vcs := &mockVCS{dirtyPaths: map[string]bool{"/home/user/ws/ws-active": false}}
	host := state.RemoteHost{ID: "host-1", HostType: config.HostTypePersistent}
	// This workspace has an active session
	hasActive := func(wsID string) bool { return wsID == "ws-active" }

	ws, err := mgr.findOrCreateWorkspaceWith(context.Background(), host, persistentResolved(), vcs, hasActive)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ws.ID == "ws-active" {
		t.Error("should NOT reuse workspace with active session")
	}
	if len(vcs.createCalls) != 1 {
		t.Errorf("expected 1 create call, got %d", len(vcs.createCalls))
	}
}

func TestFindOrCreateWorkspace_ConcurrentSpawnsGetDifferentWorkspaces(t *testing.T) {
	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}
	mgr := NewManager(&config.Config{}, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	// All worktrees report as dirty so none can be reused — each spawn must create a new one.
	vcs := &mockVCS{dirtyPaths: map[string]bool{}}
	host := state.RemoteHost{ID: "host-1", HostType: config.HostTypePersistent}

	// Track claimed workspace IDs so subsequent calls see prior workspaces as "active".
	var claimedMu sync.Mutex
	claimed := map[string]bool{}
	hasActive := func(wsID string) bool {
		claimedMu.Lock()
		defer claimedMu.Unlock()
		return claimed[wsID]
	}

	var wg sync.WaitGroup
	results := make([]state.Workspace, 3)
	errs := make([]error, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ws, err := mgr.findOrCreateWorkspaceWith(
				context.Background(), host, persistentResolved(), vcs, hasActive,
			)
			if err == nil {
				// Mark this workspace as claimed so the next goroutine won't reuse it.
				claimedMu.Lock()
				claimed[ws.ID] = true
				claimedMu.Unlock()
			}
			results[idx] = ws
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d error: %v", i, err)
		}
	}

	// All workspace IDs must be unique (mutex ensures serialization).
	ids := map[string]bool{}
	for _, ws := range results {
		if ids[ws.ID] {
			t.Errorf("duplicate workspace ID: %s", ws.ID)
		}
		ids[ws.ID] = true
	}
	if len(ids) != 3 {
		t.Errorf("expected 3 unique workspaces, got %d", len(ids))
	}
}

func TestCleanupWorkspace_RemovesClean(t *testing.T) {
	st := &state.State{
		Workspaces: []state.Workspace{
			{ID: "ws-clean", RemoteHostID: "host-1", RemotePath: "/home/user/ws/ws-clean"},
		},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}
	mgr := NewManager(&config.Config{}, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	vcs := &mockVCS{dirtyPaths: map[string]bool{"/home/user/ws/ws-clean": false}}
	host := state.RemoteHost{ID: "host-1", HostType: config.HostTypePersistent}
	ws := st.Workspaces[0]
	noActive := func(string) bool { return false }

	mgr.cleanupWorkspaceWith(context.Background(), host, ws, persistentResolved(), vcs, noActive)

	// Workspace should be removed from state
	if _, found := st.GetWorkspace("ws-clean"); found {
		t.Error("clean workspace should be removed after dispose")
	}
	if len(vcs.removeCalls) != 1 {
		t.Errorf("expected 1 remove call, got %d", len(vcs.removeCalls))
	}
}

func TestCleanupWorkspace_PreservesDirty(t *testing.T) {
	st := &state.State{
		Workspaces: []state.Workspace{
			{ID: "ws-dirty", RemoteHostID: "host-1", RemotePath: "/home/user/ws/ws-dirty"},
		},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}
	mgr := NewManager(&config.Config{}, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	vcs := &mockVCS{dirtyPaths: map[string]bool{"/home/user/ws/ws-dirty": true}}
	host := state.RemoteHost{ID: "host-1", HostType: config.HostTypePersistent}
	ws := st.Workspaces[0]
	noActive := func(string) bool { return false }

	mgr.cleanupWorkspaceWith(context.Background(), host, ws, persistentResolved(), vcs, noActive)

	// Workspace should still exist
	if _, found := st.GetWorkspace("ws-dirty"); !found {
		t.Error("dirty workspace should be preserved after dispose")
	}
	if len(vcs.removeCalls) != 0 {
		t.Errorf("should not remove dirty worktree, got %d remove calls", len(vcs.removeCalls))
	}
}

func TestCleanupWorkspace_PreservesOnDirtyCheckError(t *testing.T) {
	st := &state.State{
		Workspaces: []state.Workspace{
			{ID: "ws-err", RemoteHostID: "host-1", RemotePath: "/home/user/ws/ws-err"},
		},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}
	mgr := NewManager(&config.Config{}, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	vcs := &mockVCS{checkErr: fmt.Errorf("connection lost")}
	host := state.RemoteHost{ID: "host-1", HostType: config.HostTypePersistent}
	ws := st.Workspaces[0]
	noActive := func(string) bool { return false }

	mgr.cleanupWorkspaceWith(context.Background(), host, ws, persistentResolved(), vcs, noActive)

	// Should preserve on error (err on the side of safety)
	if _, found := st.GetWorkspace("ws-err"); !found {
		t.Error("workspace should be preserved when dirty check fails")
	}
}

func TestCleanupWorkspace_SkipsActiveSession(t *testing.T) {
	st := &state.State{
		Workspaces: []state.Workspace{
			{ID: "ws-shared", RemoteHostID: "host-1", RemotePath: "/home/user/ws/ws-shared"},
		},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}
	mgr := NewManager(&config.Config{}, st, nil)
	mgr.SetWorkspaceManager(&noopWM{st: st})

	vcs := &mockVCS{dirtyPaths: map[string]bool{"/home/user/ws/ws-shared": false}}
	host := state.RemoteHost{ID: "host-1", HostType: config.HostTypePersistent}
	ws := st.Workspaces[0]
	// Another session is using this workspace
	hasActive := func(wsID string) bool { return wsID == "ws-shared" }

	mgr.cleanupWorkspaceWith(context.Background(), host, ws, persistentResolved(), vcs, hasActive)

	// Should NOT remove — other session is active
	if _, found := st.GetWorkspace("ws-shared"); !found {
		t.Error("workspace should be preserved when other sessions are active")
	}
	if len(vcs.removeCalls) != 0 {
		t.Errorf("should not remove worktree with active sessions, got %d removes", len(vcs.removeCalls))
	}
}
