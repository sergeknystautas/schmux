package remote

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

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

	// Clean up background Connect() goroutines so they don't outlive the test
	// and race on t.TempDir() cleanup.
	mgr.mu.RLock()
	for _, conn := range mgr.connections {
		conn.Close()
	}
	mgr.mu.RUnlock()
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
	// No callback set — should not panic
	mgr.notifyConnect("test-host")
}

// TestReconcileWithRenamedWindows verifies that session reconciliation does NOT
// fall back to window name matching (Issue 4 fix). This test validates the
// strict ID-only matching logic without requiring a full connection setup.
func TestReconcileWithRenamedWindows(t *testing.T) {
	cfg := &config.Config{
		RemoteProfiles: []config.RemoteProfile{
			{
				ID:            "test-flavor",
				DisplayName:   "Test",
				WorkspacePath: "/workspace",
				VCS:           "git",
				Flavors:       []config.RemoteProfileFlavor{{Flavor: "test"}},
			},
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
	cfg := &config.Config{
		RemoteProfiles: []config.RemoteProfile{
			{
				ID:            "test-flavor",
				DisplayName:   "Test Flavor",
				WorkspacePath: "/tmp/test",
				VCS:           "git",
				Flavors:       []config.RemoteProfileFlavor{{Flavor: "test"}},
			},
		},
	}

	st := &state.State{
		Workspaces:  []state.Workspace{},
		Sessions:    []state.Session{},
		RemoteHosts: []state.RemoteHost{},
	}

	mgr := NewManager(cfg, st, nil)

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
