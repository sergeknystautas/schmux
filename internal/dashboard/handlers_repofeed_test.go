//go:build !norepofeed

package dashboard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/repofeed"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func newTestRepofeedServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{}
	cfg.WorkspacePath = "/tmp/workspaces"
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	logger := log.NewWithOptions(io.Discard, log.Options{})
	wm := workspace.New(cfg, st, statePath, logger)
	sm := session.New(cfg, st, statePath, wm, nil, logger)
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{})
	return server
}

func TestHandleRepofeedDismiss_Success(t *testing.T) {
	server := newTestRepofeedServer(t)
	dismissed := repofeed.NewDismissedStore()
	server.SetRepofeedDismissed(dismissed)

	body := `{"developer":"alice@example.com","workspace_id":"ws-001"}`
	req, _ := http.NewRequest("POST", "/api/repofeed/dismiss", strings.NewReader(body))
	rr := httptest.NewRecorder()

	server.handleRepofeedDismiss(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if !dismissed.IsDismissed("alice@example.com", "ws-001") {
		t.Error("expected intent to be dismissed after API call")
	}
}

func TestHandleRepofeedDismiss_MissingFields(t *testing.T) {
	server := newTestRepofeedServer(t)
	dismissed := repofeed.NewDismissedStore()
	server.SetRepofeedDismissed(dismissed)

	body := `{"developer":"alice@example.com"}`
	req, _ := http.NewRequest("POST", "/api/repofeed/dismiss", strings.NewReader(body))
	rr := httptest.NewRecorder()

	server.handleRepofeedDismiss(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing workspace_id, got %d", rr.Code)
	}
}

func TestHandleRepofeedDismiss_NoStore(t *testing.T) {
	server := newTestRepofeedServer(t)
	// Don't set dismissed store

	body := `{"developer":"alice@example.com","workspace_id":"ws-001"}`
	req, _ := http.NewRequest("POST", "/api/repofeed/dismiss", strings.NewReader(body))
	rr := httptest.NewRecorder()

	server.handleRepofeedDismiss(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

func TestHandleRepofeedOutgoing_Empty(t *testing.T) {
	server := newTestRepofeedServer(t)

	req, _ := http.NewRequest("GET", "/api/repofeed/outgoing", nil)
	rr := httptest.NewRecorder()

	server.handleRepofeedOutgoing(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestHandleRepofeedIncoming_Empty(t *testing.T) {
	server := newTestRepofeedServer(t)

	req, _ := http.NewRequest("GET", "/api/repofeed/incoming", nil)
	rr := httptest.NewRecorder()

	server.handleRepofeedIncoming(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}
