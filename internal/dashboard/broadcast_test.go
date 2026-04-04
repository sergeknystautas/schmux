package dashboard

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/state"
)

// dialTestDashboardWS creates a test HTTP server with the dashboard WebSocket
// route and dials a WebSocket connection. Returns the connection and a cleanup func.
func dialTestDashboardWS(t *testing.T, srv *Server) (*websocket.Conn, func()) {
	t.Helper()
	r := chi.NewRouter()
	r.HandleFunc("/ws/dashboard", srv.handleDashboardWebSocket)
	ts := httptest.NewServer(r)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/dashboard"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		ts.Close()
		t.Fatal(err)
	}
	return conn, func() { conn.Close(); ts.Close() }
}

// readDashboardMsg reads a JSON message from the WebSocket with a timeout.
func readDashboardMsg(t *testing.T, conn *websocket.Conn, timeout time.Duration) map[string]interface{} {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(timeout))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read WS message: %v", err)
	}
	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("failed to unmarshal WS message: %v", err)
	}
	return msg
}

func TestDashboardWebSocket_InitialState(t *testing.T) {
	srv, _, st := newTestServer(t)

	// Add a workspace so the initial state has something
	st.AddWorkspace(state.Workspace{
		ID:     "ws-1",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
	})

	conn, cleanup := dialTestDashboardWS(t, srv)
	defer cleanup()

	// First message should be the sessions snapshot
	msg := readDashboardMsg(t, conn, 2*time.Second)
	if msg["type"] != "sessions" {
		t.Errorf("first message type = %q, want %q", msg["type"], "sessions")
	}
	workspaces, ok := msg["workspaces"].([]interface{})
	if !ok {
		t.Fatalf("workspaces field missing or wrong type: %v", msg["workspaces"])
	}
	if len(workspaces) != 1 {
		t.Errorf("expected 1 workspace, got %d", len(workspaces))
	}
}

func TestDashboardWebSocket_BroadcastDelivery(t *testing.T) {
	srv, _, st := newTestServer(t)

	conn, cleanup := dialTestDashboardWS(t, srv)
	defer cleanup()

	// Consume initial state messages (sessions + github_status)
	readDashboardMsg(t, conn, 2*time.Second)

	// Add state and trigger broadcast
	st.AddWorkspace(state.Workspace{
		ID:     "ws-new",
		Repo:   "https://example.com/new.git",
		Branch: "feat",
		Path:   t.TempDir(),
	})
	srv.BroadcastSessions()

	// Wait for debounce (100ms) + margin
	msg := readDashboardMsg(t, conn, 500*time.Millisecond)
	if msg["type"] != "sessions" {
		t.Errorf("broadcast message type = %q, want %q", msg["type"], "sessions")
	}
	workspaces, ok := msg["workspaces"].([]interface{})
	if !ok {
		t.Fatalf("workspaces field missing or wrong type")
	}
	if len(workspaces) != 1 {
		t.Errorf("expected 1 workspace in broadcast, got %d", len(workspaces))
	}
}

func TestDashboardWebSocket_MultipleClients(t *testing.T) {
	srv, _, _ := newTestServer(t)

	conn1, cleanup1 := dialTestDashboardWS(t, srv)
	defer cleanup1()
	conn2, cleanup2 := dialTestDashboardWS(t, srv)
	defer cleanup2()

	// Consume initial state from both
	readDashboardMsg(t, conn1, 2*time.Second)
	readDashboardMsg(t, conn2, 2*time.Second)

	// Trigger broadcast
	srv.BroadcastSessions()

	// Both clients should receive the broadcast
	msg1 := readDashboardMsg(t, conn1, 500*time.Millisecond)
	msg2 := readDashboardMsg(t, conn2, 500*time.Millisecond)

	if msg1["type"] != "sessions" {
		t.Errorf("client 1 message type = %q, want sessions", msg1["type"])
	}
	if msg2["type"] != "sessions" {
		t.Errorf("client 2 message type = %q, want sessions", msg2["type"])
	}
}

func TestDashboardWebSocket_DisconnectCleanup(t *testing.T) {
	srv, _, _ := newTestServer(t)

	conn, cleanup := dialTestDashboardWS(t, srv)

	// Consume initial state
	readDashboardMsg(t, conn, 2*time.Second)

	// Verify connection is registered
	srv.sessionsConnsMu.RLock()
	countBefore := len(srv.sessionsConns)
	srv.sessionsConnsMu.RUnlock()
	if countBefore != 1 {
		t.Fatalf("expected 1 registered conn, got %d", countBefore)
	}

	// Close client connection
	conn.Close()
	cleanup()

	// Trigger broadcast — this will detect the write failure and clean up.
	srv.BroadcastSessions()

	// Poll for cleanup completion instead of using time.Sleep
	deadline := time.After(2 * time.Second)
	for {
		srv.sessionsConnsMu.RLock()
		count := len(srv.sessionsConns)
		srv.sessionsConnsMu.RUnlock()
		if count == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("expected 0 registered conns after disconnect, still have %d", count)
		case <-time.After(10 * time.Millisecond):
			// Retry
		}
	}
}

func TestBroadcastOverlayChange(t *testing.T) {
	srv, _, _ := newTestServer(t)

	conn, cleanup := dialTestDashboardWS(t, srv)
	defer cleanup()

	// Consume initial state messages (sessions + github_status)
	readDashboardMsg(t, conn, 2*time.Second)

	// Send overlay change
	srv.BroadcastOverlayChange(OverlayChangeEvent{
		RelPath:            "src/main.go",
		SourceWorkspaceID:  "ws-1",
		SourceBranch:       "main",
		TargetWorkspaceIDs: []string{"ws-2"},
		Timestamp:          time.Now().Unix(),
	})

	// Should receive immediately (not debounced)
	msg := readDashboardMsg(t, conn, 500*time.Millisecond)
	if msg["type"] != "overlay_change" {
		t.Errorf("message type = %q, want overlay_change", msg["type"])
	}
	if msg["rel_path"] != "src/main.go" {
		t.Errorf("rel_path = %q, want src/main.go", msg["rel_path"])
	}
	if msg["source_workspace_id"] != "ws-1" {
		t.Errorf("source_workspace_id = %q, want ws-1", msg["source_workspace_id"])
	}
}

func TestBroadcastWorkspaceLocked(t *testing.T) {
	srv, _, _ := newTestServer(t)

	conn, cleanup := dialTestDashboardWS(t, srv)
	defer cleanup()

	// Consume initial state
	readDashboardMsg(t, conn, 2*time.Second)

	// Send lock message
	srv.BroadcastWorkspaceLocked("ws-1", true)

	msg := readDashboardMsg(t, conn, 500*time.Millisecond)
	if msg["type"] != "workspace_locked" {
		t.Errorf("message type = %q, want workspace_locked", msg["type"])
	}
	if msg["workspace_id"] != "ws-1" {
		t.Errorf("workspace_id = %q, want ws-1", msg["workspace_id"])
	}
	if msg["locked"] != true {
		t.Errorf("locked = %v, want true", msg["locked"])
	}
}

func TestBroadcast_IncludesTabs(t *testing.T) {
	srv, _, st := newTestServer(t)

	st.AddWorkspace(state.Workspace{
		ID:     "ws-tab",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
	})

	conn, cleanup := dialTestDashboardWS(t, srv)
	defer cleanup()

	msg := readDashboardMsg(t, conn, 2*time.Second)
	if msg["type"] != "sessions" {
		t.Fatalf("expected sessions message, got %s", msg["type"])
	}

	workspaces, ok := msg["workspaces"].([]interface{})
	if !ok || len(workspaces) == 0 {
		t.Fatal("no workspaces in broadcast")
	}
	ws := workspaces[0].(map[string]interface{})
	tabs, ok := ws["tabs"].([]interface{})
	if !ok {
		t.Fatal("tabs field missing from workspace response")
	}
	if len(tabs) < 2 {
		t.Fatalf("expected at least 2 tabs (diff + git), got %d", len(tabs))
	}
	firstTab := tabs[0].(map[string]interface{})
	if firstTab["kind"] != "diff" {
		t.Errorf("first tab kind = %q, want diff", firstTab["kind"])
	}
}

func TestBroadcast_ResolveConflictTabUsesPersistedTab(t *testing.T) {
	srv, _, st := newTestServer(t)

	st.AddWorkspace(state.Workspace{
		ID:     "ws-conflict",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
		Tabs: []state.Tab{
			{
				ID:        "sys-resolve-conflict-abcdef1",
				Kind:      "resolve-conflict",
				Label:     "Conflict abcdef1",
				Route:     "/resolve-conflict/ws-conflict/sys-resolve-conflict-abcdef1",
				Closable:  true,
				Meta:      map[string]string{"hash": "abcdef1"},
				CreatedAt: time.Now(),
			},
		},
		ResolveConflicts: []state.ResolveConflict{
			{
				Type:        "linear_sync_resolve_conflict",
				WorkspaceID: "ws-conflict",
				Status:      "in_progress",
				Hash:        "abcdef1",
				StartedAt:   time.Now().Format(time.RFC3339),
				Steps:       []state.ResolveConflictStep{},
			},
		},
	})

	// Set in-memory state to simulate a genuinely running resolution.
	srv.setLinearSyncResolveConflictState("ws-conflict", &LinearSyncResolveConflictState{
		ResolveConflict: state.ResolveConflict{
			Status: "in_progress",
			Hash:   "abcdef1",
		},
	})

	conn, cleanup := dialTestDashboardWS(t, srv)
	defer cleanup()

	msg := readDashboardMsg(t, conn, 2*time.Second)
	if msg["type"] != "sessions" {
		t.Fatalf("expected sessions message, got %s", msg["type"])
	}

	workspaces, ok := msg["workspaces"].([]interface{})
	if !ok || len(workspaces) == 0 {
		t.Fatal("no workspaces in broadcast")
	}

	var ws map[string]interface{}
	for _, item := range workspaces {
		candidate := item.(map[string]interface{})
		if candidate["id"] == "ws-conflict" {
			ws = candidate
			break
		}
	}
	if ws == nil {
		t.Fatal("ws-conflict not found in broadcast")
	}

	tabs, ok := ws["tabs"].([]interface{})
	if !ok {
		t.Fatal("tabs field missing from workspace response")
	}

	found := false
	for _, raw := range tabs {
		tab := raw.(map[string]interface{})
		if tab["kind"] != "resolve-conflict" {
			continue
		}
		found = true
		if tab["id"] != "sys-resolve-conflict-abcdef1" {
			t.Fatalf("resolve-conflict id = %q", tab["id"])
		}
		if tab["label"] != "Conflict abcdef1" {
			t.Fatalf("resolve-conflict label = %q", tab["label"])
		}
		if tab["closable"] != false {
			t.Fatalf("resolve-conflict closable = %v, want false", tab["closable"])
		}
	}
	if !found {
		t.Fatal("expected persisted resolve-conflict tab in broadcast")
	}
}

func TestBroadcast_ResolveConflictTabsUseMatchingPersistedRecords(t *testing.T) {
	srv, _, st := newTestServer(t)

	st.AddWorkspace(state.Workspace{
		ID:     "ws-conflicts",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
		Tabs: []state.Tab{
			{
				ID:        "sys-resolve-conflict-abcdef1",
				Kind:      "resolve-conflict",
				Label:     "Conflict abcdef1",
				Route:     "/resolve-conflict/ws-conflicts/sys-resolve-conflict-abcdef1",
				Closable:  true,
				Meta:      map[string]string{"hash": "abcdef1"},
				CreatedAt: time.Now(),
			},
			{
				ID:        "sys-resolve-conflict-1234567",
				Kind:      "resolve-conflict",
				Label:     "Conflict 1234567",
				Route:     "/resolve-conflict/ws-conflicts/sys-resolve-conflict-1234567",
				Closable:  true,
				Meta:      map[string]string{"hash": "1234567"},
				CreatedAt: time.Now(),
			},
			{
				ID:        "sys-resolve-conflict-running1",
				Kind:      "resolve-conflict",
				Label:     "Conflict running1",
				Route:     "/resolve-conflict/ws-conflicts/sys-resolve-conflict-running1",
				Closable:  true,
				Meta:      map[string]string{"hash": "running1"},
				CreatedAt: time.Now(),
			},
		},
		ResolveConflicts: []state.ResolveConflict{
			{
				Type:        "linear_sync_resolve_conflict",
				WorkspaceID: "ws-conflicts",
				Status:      "done",
				Hash:        "abcdef1",
				StartedAt:   time.Now().Format(time.RFC3339),
				Steps:       []state.ResolveConflictStep{},
			},
			{
				Type:        "linear_sync_resolve_conflict",
				WorkspaceID: "ws-conflicts",
				Status:      "in_progress",
				Hash:        "1234567",
				StartedAt:   time.Now().Format(time.RFC3339),
				Steps:       []state.ResolveConflictStep{},
			},
			{
				Type:        "linear_sync_resolve_conflict",
				WorkspaceID: "ws-conflicts",
				Status:      "in_progress",
				Hash:        "running1",
				StartedAt:   time.Now().Format(time.RFC3339),
				Steps:       []state.ResolveConflictStep{},
			},
		},
	})

	// Simulate a genuinely running resolution: set in-memory state for "running1".
	srv.setLinearSyncResolveConflictState("ws-conflicts", &LinearSyncResolveConflictState{
		ResolveConflict: state.ResolveConflict{
			Status: "in_progress",
			Hash:   "running1",
		},
	})

	conn, cleanup := dialTestDashboardWS(t, srv)
	defer cleanup()

	msg := readDashboardMsg(t, conn, 2*time.Second)
	workspaces := msg["workspaces"].([]interface{})
	ws := workspaces[0].(map[string]interface{})
	tabs := ws["tabs"].([]interface{})

	found := map[string]map[string]interface{}{}
	for _, raw := range tabs {
		tab := raw.(map[string]interface{})
		if tab["kind"] == "resolve-conflict" {
			found[tab["id"].(string)] = tab
		}
	}

	if found["sys-resolve-conflict-abcdef1"]["closable"] != true {
		t.Fatalf("done tab should be closable, got %v", found["sys-resolve-conflict-abcdef1"]["closable"])
	}
	// Stale persisted in_progress with no in-memory state (e.g. daemon restarted) should be closable
	if found["sys-resolve-conflict-1234567"]["closable"] != true {
		t.Fatalf("stale in-progress tab (no goroutine) should be closable, got %v", found["sys-resolve-conflict-1234567"]["closable"])
	}
	// Genuinely running resolution (in-memory state present) should NOT be closable
	if found["sys-resolve-conflict-running1"]["closable"] != false {
		t.Fatalf("running in-progress tab should not be closable, got %v", found["sys-resolve-conflict-running1"]["closable"])
	}
}

func TestBroadcastIncludesWorkspaceStatus(t *testing.T) {
	srv, _, st := newTestServer(t)

	st.AddWorkspace(state.Workspace{
		ID:     "ws-status",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   t.TempDir(),
		Status: state.WorkspaceStatusRunning,
	})

	conn, cleanup := dialTestDashboardWS(t, srv)
	defer cleanup()

	// Consume initial snapshot
	readDashboardMsg(t, conn, 500*time.Millisecond)

	srv.BroadcastSessions()

	msg := readDashboardMsg(t, conn, 500*time.Millisecond)
	workspaces := msg["workspaces"].([]interface{})
	ws := workspaces[0].(map[string]interface{})
	if ws["status"] != "running" {
		t.Errorf("expected status=running, got %v", ws["status"])
	}
}
