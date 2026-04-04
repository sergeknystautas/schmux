//go:build !notunnel

package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/tunnel"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func newTestServerWithTunnel(t *testing.T, tunnelMgr *tunnel.Manager) *Server {
	t.Helper()
	cfg := &config.Config{WorkspacePath: t.TempDir()}
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	sm := session.New(cfg, st, statePath, wm, nil, log.NewWithOptions(io.Discard, log.Options{}))
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, nil, ServerOptions{})
	server.SetModelManager(models.New(cfg, nil, "", log.NewWithOptions(io.Discard, log.Options{})))
	server.SetTunnelManager(tunnelMgr)
	return server
}

func TestHandleRemoteAccessStatus(t *testing.T) {
	mgr := tunnel.NewManager(tunnel.ManagerConfig{}, nil)
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
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	sm := session.New(cfg, st, statePath, wm, nil, log.NewWithOptions(io.Discard, log.Options{}))
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, nil, ServerOptions{})

	req, _ := http.NewRequest("GET", "/api/remote-access/status", nil)
	rr := httptest.NewRecorder()

	server.handleRemoteAccessStatus(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

// TestHandleRemoteAccessStatus_MethodNotAllowed removed: chi handles 405
// responses automatically via r.Get route registration.

func TestHandleRemoteAccessOn_ReturnsErrorWhenDisabled(t *testing.T) {
	mgr := tunnel.NewManager(tunnel.ManagerConfig{Disabled: func() bool { return true }}, nil)
	cfg := &config.Config{
		WorkspacePath: t.TempDir(),
		// RemoteAccess.Enabled not set — defaults to false (disabled)
	}
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	sm := session.New(cfg, st, statePath, wm, nil, log.NewWithOptions(io.Discard, log.Options{}))
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, nil, ServerOptions{})
	server.SetTunnelManager(mgr)

	req, _ := http.NewRequest("POST", "/api/remote-access/on", nil)
	rr := httptest.NewRecorder()

	server.handleRemoteAccessOn(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

// TestHandleRemoteAccessOn_MethodNotAllowed removed: chi handles 405
// responses automatically via r.Post route registration.

func TestHandleRemoteAccessOn_NoManager(t *testing.T) {
	cfg := &config.Config{WorkspacePath: t.TempDir()}
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	sm := session.New(cfg, st, statePath, wm, nil, log.NewWithOptions(io.Discard, log.Options{}))
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, nil, ServerOptions{})

	req, _ := http.NewRequest("POST", "/api/remote-access/on", nil)
	rr := httptest.NewRecorder()

	server.handleRemoteAccessOn(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

// TestHandleRemoteAccessOff_MethodNotAllowed removed: chi handles 405
// responses automatically via r.Post route registration.

func TestHandleRemoteAccessOff_NoManager(t *testing.T) {
	cfg := &config.Config{WorkspacePath: t.TempDir()}
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	sm := session.New(cfg, st, statePath, wm, nil, log.NewWithOptions(io.Discard, log.Options{}))
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, nil, ServerOptions{})

	req, _ := http.NewRequest("POST", "/api/remote-access/off", nil)
	rr := httptest.NewRecorder()

	server.handleRemoteAccessOff(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

func TestHandleRemoteAccessOff_ReturnsOffState(t *testing.T) {
	mgr := tunnel.NewManager(tunnel.ManagerConfig{}, nil)
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
