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
