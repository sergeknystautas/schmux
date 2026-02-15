package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/tunnel"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func newTestServerWithTunnel(t *testing.T, tunnelMgr *tunnel.Manager) *Server {
	t.Helper()
	cfg := &config.Config{WorkspacePath: t.TempDir()}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(), ServerOptions{})
	server.SetTunnelManager(tunnelMgr)
	return server
}

func TestHandleRemoteAccessStatus(t *testing.T) {
	mgr := tunnel.NewManager(tunnel.ManagerConfig{})
	server := newTestServerWithTunnel(t, mgr)

	req, _ := http.NewRequest("GET", "/api/remote-access/status", nil)
	rr := httptest.NewRecorder()

	server.handleRemoteAccessStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var status tunnel.TunnelStatus
	if err := json.NewDecoder(rr.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if status.State != tunnel.StateOff {
		t.Errorf("expected state 'off', got %q", status.State)
	}
}

func TestHandleRemoteAccessStatus_NoManager(t *testing.T) {
	cfg := &config.Config{WorkspacePath: t.TempDir()}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(), ServerOptions{})

	req, _ := http.NewRequest("GET", "/api/remote-access/status", nil)
	rr := httptest.NewRecorder()

	server.handleRemoteAccessStatus(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

func TestHandleRemoteAccessStatus_MethodNotAllowed(t *testing.T) {
	mgr := tunnel.NewManager(tunnel.ManagerConfig{})
	server := newTestServerWithTunnel(t, mgr)

	req, _ := http.NewRequest("POST", "/api/remote-access/status", nil)
	rr := httptest.NewRecorder()

	server.handleRemoteAccessStatus(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestHandleRemoteAccessOn_ReturnsErrorWhenDisabled(t *testing.T) {
	mgr := tunnel.NewManager(tunnel.ManagerConfig{Disabled: true})
	disabled := true
	cfg := &config.Config{
		WorkspacePath: t.TempDir(),
		RemoteAccess:  &config.RemoteAccessConfig{Disabled: &disabled},
	}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(), ServerOptions{})
	server.SetTunnelManager(mgr)

	req, _ := http.NewRequest("POST", "/api/remote-access/on", nil)
	rr := httptest.NewRecorder()

	server.handleRemoteAccessOn(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestHandleRemoteAccessOn_MethodNotAllowed(t *testing.T) {
	mgr := tunnel.NewManager(tunnel.ManagerConfig{})
	server := newTestServerWithTunnel(t, mgr)

	req, _ := http.NewRequest("GET", "/api/remote-access/on", nil)
	rr := httptest.NewRecorder()

	server.handleRemoteAccessOn(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestHandleRemoteAccessOn_NoManager(t *testing.T) {
	cfg := &config.Config{WorkspacePath: t.TempDir()}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(), ServerOptions{})

	req, _ := http.NewRequest("POST", "/api/remote-access/on", nil)
	rr := httptest.NewRecorder()

	server.handleRemoteAccessOn(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

func TestHandleRemoteAccessOff_MethodNotAllowed(t *testing.T) {
	mgr := tunnel.NewManager(tunnel.ManagerConfig{})
	server := newTestServerWithTunnel(t, mgr)

	req, _ := http.NewRequest("GET", "/api/remote-access/off", nil)
	rr := httptest.NewRecorder()

	server.handleRemoteAccessOff(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestHandleRemoteAccessOff_NoManager(t *testing.T) {
	cfg := &config.Config{WorkspacePath: t.TempDir()}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(), ServerOptions{})

	req, _ := http.NewRequest("POST", "/api/remote-access/off", nil)
	rr := httptest.NewRecorder()

	server.handleRemoteAccessOff(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

func TestHandleRemoteAccessOff_ReturnsOffState(t *testing.T) {
	mgr := tunnel.NewManager(tunnel.ManagerConfig{})
	server := newTestServerWithTunnel(t, mgr)

	req, _ := http.NewRequest("POST", "/api/remote-access/off", nil)
	rr := httptest.NewRecorder()

	server.handleRemoteAccessOff(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var status tunnel.TunnelStatus
	if err := json.NewDecoder(rr.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if status.State != tunnel.StateOff {
		t.Errorf("expected state 'off', got %q", status.State)
	}
}
