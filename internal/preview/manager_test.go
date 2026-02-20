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

	"github.com/sergeknystautas/schmux/internal/state"
)

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
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)
	ws := state.Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: t.TempDir()}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("add workspace: %v", err)
	}
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	_, portStr, _ := net.SplitHostPort(upstream.Listener.Addr().String())
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)

	m := NewManager(st, 3, 20, false, 53000, 10)
	defer m.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	first, err := m.CreateOrGet(ctx, ws, "127.0.0.1", port)
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}
	if first.Status != StatusReady {
		t.Fatalf("expected ready status, got %s", first.Status)
	}

	second, err := m.CreateOrGet(ctx, ws, "127.0.0.1", port)
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
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)
	ws := state.Workspace{ID: "ws-remote", Repo: "repo", Branch: "main", RemoteHostID: "rh-1"}
	m := NewManager(st, 3, 20, false, 53000, 10)
	defer m.Stop()

	_, err := m.CreateOrGet(context.Background(), ws, "127.0.0.1", 5173)
	if err == nil {
		t.Fatal("expected error for remote workspace")
	}
}

func TestManagerStablePortSurvivesRestart(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)
	ws := state.Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: t.TempDir()}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("add workspace: %v", err)
	}
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	_, portStr, _ := net.SplitHostPort(upstream.Listener.Addr().String())
	var upstreamPort int
	_, _ = fmt.Sscanf(portStr, "%d", &upstreamPort)

	// First "daemon run": create a preview, note its port.
	m1 := NewManager(st, 3, 20, false, 53000, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	first, err := m1.CreateOrGet(ctx, ws, "127.0.0.1", upstreamPort)
	cancel()
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}
	firstPort := first.ProxyPort
	m1.Stop()

	// Simulate restart: reload state from disk.
	st2, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("reload state: %v", err)
	}

	// Second "daemon run": the preview should come back on the same port.
	m2 := NewManager(st2, 3, 20, false, 53000, 10)
	defer m2.Stop()

	ws2, _ := st2.GetWorkspace("ws-1")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	second, err := m2.CreateOrGet(ctx2, ws2, "127.0.0.1", upstreamPort)
	cancel2()
	if err != nil {
		t.Fatalf("recreate preview after restart: %v", err)
	}
	if second.ProxyPort != firstPort {
		t.Fatalf("expected same proxy port %d after restart, got %d", firstPort, second.ProxyPort)
	}
}

func TestManagerDifferentWorkspacesGetDifferentBlocks(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)
	ws1 := state.Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: t.TempDir()}
	ws2 := state.Workspace{ID: "ws-2", Repo: "repo", Branch: "main", Path: t.TempDir()}
	if err := st.AddWorkspace(ws1); err != nil {
		t.Fatalf("add ws1: %v", err)
	}
	if err := st.AddWorkspace(ws2); err != nil {
		t.Fatalf("add ws2: %v", err)
	}
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}

	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upstream1.Close()
	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upstream2.Close()

	parsePort := func(s *httptest.Server) int {
		_, p, _ := net.SplitHostPort(s.Listener.Addr().String())
		var port int
		_, _ = fmt.Sscanf(p, "%d", &port)
		return port
	}

	m := NewManager(st, 3, 20, false, 53000, 10)
	defer m.Stop()

	ctx := context.Background()
	p1, err := m.CreateOrGet(ctx, ws1, "127.0.0.1", parsePort(upstream1))
	if err != nil {
		t.Fatalf("create preview ws1: %v", err)
	}
	p2, err := m.CreateOrGet(ctx, ws2, "127.0.0.1", parsePort(upstream2))
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
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)
	ws := state.Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: t.TempDir()}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("add workspace: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	_, portStr, _ := net.SplitHostPort(upstream.Listener.Addr().String())
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)

	m := NewManager(st, 3, 20, false, 53000, 10)
	defer m.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p, err := m.CreateOrGet(ctx, ws, "127.0.0.1", port)
	if err != nil {
		t.Fatalf("create preview: %v", err)
	}
	upstream.Close()

	p.LastHealthyAt = time.Now().Add(-2 * staleGrace)
	if err := st.UpsertPreview(p); err != nil {
		t.Fatalf("upsert preview: %v", err)
	}
	if err := st.Save(); err != nil {
		t.Fatalf("save state: %v", err)
	}

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
