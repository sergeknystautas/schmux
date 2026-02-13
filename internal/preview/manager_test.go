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

	m := NewManager(st, 3, 20, time.Hour)
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
	m := NewManager(st, 3, 20, time.Hour)
	defer m.Stop()

	_, err := m.CreateOrGet(context.Background(), ws, "127.0.0.1", 5173)
	if err == nil {
		t.Fatal("expected error for remote workspace")
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

	m := NewManager(st, 3, 20, time.Hour)
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
