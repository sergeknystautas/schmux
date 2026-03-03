package preview

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sergeknystautas/schmux/internal/state"
)

// testServerPort extracts the port number from a running httptest.Server.
func testServerPort(s *httptest.Server) int {
	_, p, _ := net.SplitHostPort(s.Listener.Addr().String())
	var port int
	_, _ = fmt.Sscanf(p, "%d", &port)
	return port
}

// newPreviewTestState creates a persistent state at a temp path, adds the given
// workspaces, and saves. Returns the state and the file path.
func newPreviewTestState(t *testing.T, workspaces ...state.Workspace) (*state.State, string) {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)
	for _, ws := range workspaces {
		if err := st.AddWorkspace(ws); err != nil {
			t.Fatalf("add workspace %s: %v", ws.ID, err)
		}
	}
	if len(workspaces) > 0 {
		if err := st.Save(); err != nil {
			t.Fatalf("save state: %v", err)
		}
	}
	return st, statePath
}

func TestNormalizeTargetHost(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		wantErr bool
	}{
		{name: "default empty", host: "", wantErr: false},
		{name: "localhost", host: "localhost", wantErr: false},
		{name: "ipv4 loopback", host: "127.0.0.1", wantErr: false},
		{name: "ipv6 loopback", host: "::1", wantErr: false},
		{name: "non loopback", host: "example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeTargetHost(tt.host)
			if tt.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestManagerCreateOrGetReuse(t *testing.T) {
	ws := state.Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: t.TempDir()}
	st, _ := newPreviewTestState(t, ws)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	port := testServerPort(upstream)

	m := NewManager(st, 3, 20, false, 53000, 10, false, "", "", nil, nil)
	defer m.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	first, err := m.CreateOrGet(ctx, ws, "127.0.0.1", port, "")
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}
	if first.Status != StatusReady {
		t.Fatalf("expected ready status, got %s", first.Status)
	}

	second, err := m.CreateOrGet(ctx, ws, "127.0.0.1", port, "")
	if err != nil {
		t.Fatalf("create preview second time: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected same preview id, got %s and %s", first.ID, second.ID)
	}
	if first.ProxyPort != second.ProxyPort {
		t.Fatalf("expected same proxy port, got %d and %d", first.ProxyPort, second.ProxyPort)
	}
}

func TestManagerRemoteWorkspaceUnsupported(t *testing.T) {
	ws := state.Workspace{ID: "ws-remote", Repo: "repo", Branch: "main", RemoteHostID: "rh-1"}
	st, _ := newPreviewTestState(t, ws)
	m := NewManager(st, 3, 20, false, 53000, 10, false, "", "", nil, nil)
	defer m.Stop()

	_, err := m.CreateOrGet(context.Background(), ws, "127.0.0.1", 5173, "")
	if err == nil {
		t.Fatal("expected error for remote workspace")
	}
}

func TestManagerStablePortSurvivesRestart(t *testing.T) {
	ws := state.Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: t.TempDir()}
	st, statePath := newPreviewTestState(t, ws)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	upstreamPort := testServerPort(upstream)

	// First "daemon run": create a preview, note its port.
	m1 := NewManager(st, 3, 20, false, 53000, 10, false, "", "", nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	first, err := m1.CreateOrGet(ctx, ws, "127.0.0.1", upstreamPort, "")
	cancel()
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}
	firstPort := first.ProxyPort
	m1.Stop()

	// Simulate restart: reload state from disk.
	st2, err := state.Load(statePath, nil)
	if err != nil {
		t.Fatalf("reload state: %v", err)
	}

	// Second "daemon run": the preview should come back on the same port.
	m2 := NewManager(st2, 3, 20, false, 53000, 10, false, "", "", nil, nil)
	defer m2.Stop()

	ws2, _ := st2.GetWorkspace("ws-1")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	second, err := m2.CreateOrGet(ctx2, ws2, "127.0.0.1", upstreamPort, "")
	cancel2()
	if err != nil {
		t.Fatalf("recreate preview after restart: %v", err)
	}
	if second.ProxyPort != firstPort {
		t.Fatalf("expected same proxy port %d after restart, got %d", firstPort, second.ProxyPort)
	}
}

func TestManagerDifferentWorkspacesGetDifferentBlocks(t *testing.T) {
	ws1 := state.Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: t.TempDir()}
	ws2 := state.Workspace{ID: "ws-2", Repo: "repo", Branch: "main", Path: t.TempDir()}
	st, _ := newPreviewTestState(t, ws1, ws2)

	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upstream1.Close()
	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upstream2.Close()

	m := NewManager(st, 3, 20, false, 53000, 10, false, "", "", nil, nil)
	defer m.Stop()

	ctx := context.Background()
	p1, err := m.CreateOrGet(ctx, ws1, "127.0.0.1", testServerPort(upstream1), "")
	if err != nil {
		t.Fatalf("create preview ws1: %v", err)
	}
	p2, err := m.CreateOrGet(ctx, ws2, "127.0.0.1", testServerPort(upstream2), "")
	if err != nil {
		t.Fatalf("create preview ws2: %v", err)
	}

	// Ports must not overlap.
	if p1.ProxyPort == p2.ProxyPort {
		t.Fatalf("workspaces got same proxy port %d", p1.ProxyPort)
	}

	// Blocks must be different (ws1 gets block 1, ws2 gets block 2).
	w1, _ := st.GetWorkspace("ws-1")
	w2, _ := st.GetWorkspace("ws-2")
	if w1.PortBlock == w2.PortBlock {
		t.Fatalf("workspaces got same port block %d", w1.PortBlock)
	}

	// Ports must be in different blocks (no overlap possible).
	block1Base := 53000 + (w1.PortBlock-1)*10
	block2Base := 53000 + (w2.PortBlock-1)*10
	if p1.ProxyPort < block1Base || p1.ProxyPort >= block1Base+10 {
		t.Fatalf("ws1 proxy port %d outside expected block %d-%d", p1.ProxyPort, block1Base, block1Base+9)
	}
	if p2.ProxyPort < block2Base || p2.ProxyPort >= block2Base+10 {
		t.Fatalf("ws2 proxy port %d outside expected block %d-%d", p2.ProxyPort, block2Base, block2Base+9)
	}
}

func TestManagerReconcileWorkspaceRemovesStalePreview(t *testing.T) {
	ws := state.Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: t.TempDir()}
	st, _ := newPreviewTestState(t, ws)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	port := testServerPort(upstream)

	// Port detector that returns nothing (simulates PID no longer owning port)
	noPortsDetector := func(pid int) []ListeningPort { return nil }

	st.AddSession(state.Session{ID: "sess-1", WorkspaceID: "ws-1", Pid: 12345})

	m := NewManager(st, 3, 20, false, 53000, 10, false, "", "", nil, noPortsDetector)
	defer m.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p, err := m.CreateOrGet(ctx, ws, "127.0.0.1", port, "sess-1")
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}
	upstream.Close()

	changed, err := m.ReconcileWorkspace(ws.ID)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !changed {
		t.Fatal("expected reconcile to report changes")
	}
	if _, found := st.GetPreview(p.ID); found {
		t.Fatal("expected stale preview to be removed")
	}
}

func TestManagerReconcileDeletesWhenSessionDead(t *testing.T) {
	ws := state.Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: t.TempDir()}
	st, _ := newPreviewTestState(t, ws)
	st.AddSession(state.Session{ID: "sess-dead", WorkspaceID: "ws-1", Pid: 999999})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	m := NewManager(st, 3, 20, false, 53000, 10, false, "", "", nil, nil)
	defer m.Stop()

	ctx := context.Background()
	p, err := m.CreateOrGet(ctx, ws, "127.0.0.1", testServerPort(upstream), "sess-dead")
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}

	// Remove the session to simulate it being disposed
	st.RemoveSession("sess-dead")

	changed, err := m.ReconcileWorkspace(ws.ID)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !changed {
		t.Fatal("expected reconcile to report changes")
	}
	if _, found := st.GetPreview(p.ID); found {
		t.Fatal("expected preview to be removed when source session is gone")
	}
}

func TestManagerReconcileRecreatesListener(t *testing.T) {
	ws := state.Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: t.TempDir()}
	st, _ := newPreviewTestState(t, ws)
	st.AddSession(state.Session{ID: "sess-1", WorkspaceID: "ws-1", Pid: 12345})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	port := testServerPort(upstream)

	// Port detector that always reports the port as owned
	alwaysOwns := func(pid int) []ListeningPort {
		return []ListeningPort{{Host: "127.0.0.1", Port: port}}
	}

	m := NewManager(st, 3, 20, false, 53000, 10, false, "", "", nil, alwaysOwns)
	defer m.Stop()

	ctx := context.Background()
	p, err := m.CreateOrGet(ctx, ws, "127.0.0.1", port, "sess-1")
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}

	// Manually stop the entry to simulate daemon restart (listener gone, state persisted)
	m.mu.Lock()
	m.stopEntryLocked(p.ID)
	m.mu.Unlock()

	changed, err := m.ReconcileWorkspace(ws.ID)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !changed {
		t.Fatal("expected reconcile to report changes (listener recreated)")
	}
	// Preview should still exist (not deleted)
	if _, found := st.GetPreview(p.ID); !found {
		t.Fatal("expected preview to still exist after listener recreation")
	}
}

func TestManagerWebSocketProxying(t *testing.T) {
	// Start an upstream WebSocket server that echoes messages.
	upgrader := websocket.Upgrader{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upstream upgrade failed: %v", err)
			return
		}
		defer conn.Close()
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	port := testServerPort(upstream)

	// Create preview proxy pointing at the upstream.
	ws := state.Workspace{ID: "ws-ws", Repo: "repo", Branch: "main", Path: t.TempDir()}
	st, _ := newPreviewTestState(t, ws)
	m := NewManager(st, 3, 20, false, 53000, 10, false, "", "", nil, nil)
	defer m.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	preview, err := m.CreateOrGet(ctx, ws, "127.0.0.1", port, "")
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}

	// Connect WebSocket through the proxy.
	proxyURL := fmt.Sprintf("ws://127.0.0.1:%d/", preview.ProxyPort)
	conn, _, err := websocket.DefaultDialer.Dial(proxyURL, nil)
	if err != nil {
		t.Fatalf("websocket dial through proxy: %v", err)
	}
	defer conn.Close()

	// Send a message and verify echo.
	want := "hello through proxy"
	if err := conn.WriteMessage(websocket.TextMessage, []byte(want)); err != nil {
		t.Fatalf("write message: %v", err)
	}
	_, got, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	if string(got) != want {
		t.Fatalf("expected %q, got %q", want, string(got))
	}
}

func TestManagerDeleteBySession(t *testing.T) {
	ws := state.Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: t.TempDir()}
	st, _ := newPreviewTestState(t, ws)

	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream1.Close()
	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream2.Close()

	m := NewManager(st, 3, 20, false, 53000, 10, false, "", "", nil, nil)
	defer m.Stop()

	ctx := context.Background()
	p1, err := m.CreateOrGet(ctx, ws, "127.0.0.1", testServerPort(upstream1), "sess-a")
	if err != nil {
		t.Fatalf("create p1: %v", err)
	}
	p2, err := m.CreateOrGet(ctx, ws, "127.0.0.1", testServerPort(upstream2), "sess-b")
	if err != nil {
		t.Fatalf("create p2: %v", err)
	}

	deleted, err := m.DeleteBySession("sess-a")
	if err != nil {
		t.Fatalf("delete by session: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted, got %d", deleted)
	}

	if _, found := st.GetPreview(p1.ID); found {
		t.Fatal("expected p1 to be deleted")
	}
	if _, found := st.GetPreview(p2.ID); !found {
		t.Fatal("expected p2 to still exist")
	}
}

func TestManagerCreateOrGetSetsSourceSessionID(t *testing.T) {
	ws := state.Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: t.TempDir()}
	st, _ := newPreviewTestState(t, ws)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	port := testServerPort(upstream)

	m := NewManager(st, 3, 20, false, 53000, 10, false, "", "", nil, nil)
	defer m.Stop()

	ctx := context.Background()
	p, err := m.CreateOrGet(ctx, ws, "127.0.0.1", port, "sess-abc")
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}
	if p.SourceSessionID != "sess-abc" {
		t.Fatalf("expected source session 'sess-abc', got %q", p.SourceSessionID)
	}

	// Second call reuses existing, source session ID stays
	p2, err := m.CreateOrGet(ctx, ws, "127.0.0.1", port, "sess-xyz")
	if err != nil {
		t.Fatalf("reuse preview: %v", err)
	}
	if p2.SourceSessionID != "sess-abc" {
		t.Fatalf("expected original source session preserved, got %q", p2.SourceSessionID)
	}
}
