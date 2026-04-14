//go:build !norepofeed

package dashboard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/events"
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

func TestHandleRepofeedPublishPreview_NoPublisher(t *testing.T) {
	server := newTestRepofeedServer(t)
	// Don't set a publisher — repofeedPublisher is nil

	req, _ := http.NewRequest("GET", "/api/repofeed/publish/preview", nil)
	rr := httptest.NewRecorder()

	server.handleRepofeedPublishPreview(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["has_data"] != false {
		t.Errorf("expected has_data=false, got %v", resp["has_data"])
	}
}

func TestHandleRepofeedPublishPreview_WithData(t *testing.T) {
	server := newTestRepofeedServer(t)
	publisher := repofeed.NewPublisher(repofeed.PublisherConfig{
		DeveloperEmail: "test@example.com",
		DisplayName:    "Test Dev",
	})
	server.SetRepofeedPublisher(publisher)

	// Simulate a spawn event so publisher has data
	evt := events.StatusEvent{Type: "status", State: "working", Intent: "fix the login page"}
	data, _ := json.Marshal(evt)
	publisher.HandleEvent(context.Background(), "ws-001-session-abc", events.RawEvent{Type: "status"}, data)

	req, _ := http.NewRequest("GET", "/api/repofeed/publish/preview", nil)
	rr := httptest.NewRecorder()

	server.handleRepofeedPublishPreview(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["has_data"] != true {
		t.Errorf("expected has_data=true, got %v", resp["has_data"])
	}
	devFile, ok := resp["developer_file"].(map[string]interface{})
	if !ok {
		t.Fatal("expected developer_file in response")
	}
	if devFile["developer"] != "test@example.com" {
		t.Errorf("developer = %v, want test@example.com", devFile["developer"])
	}
}

func TestHandleRepofeedPublishPreview_EmptyActivities(t *testing.T) {
	server := newTestRepofeedServer(t)
	publisher := repofeed.NewPublisher(repofeed.PublisherConfig{
		DeveloperEmail: "test@example.com",
		DisplayName:    "Test Dev",
	})
	server.SetRepofeedPublisher(publisher)

	// No events — publisher has no activities
	req, _ := http.NewRequest("GET", "/api/repofeed/publish/preview", nil)
	rr := httptest.NewRecorder()

	server.handleRepofeedPublishPreview(rr, req)

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["has_data"] != false {
		t.Errorf("expected has_data=false with no activities, got %v", resp["has_data"])
	}
}

func TestHandleRepofeedPublishPush_NoPublisher(t *testing.T) {
	server := newTestRepofeedServer(t)

	req, _ := http.NewRequest("POST", "/api/repofeed/publish/push", strings.NewReader("{}"))
	rr := httptest.NewRecorder()

	server.handleRepofeedPublishPush(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

func TestHandleRepofeedPublishPush_Disabled(t *testing.T) {
	server := newTestRepofeedServer(t)
	publisher := repofeed.NewPublisher(repofeed.PublisherConfig{
		DeveloperEmail: "test@example.com",
	})
	server.SetRepofeedPublisher(publisher)
	// config.GetRepofeedEnabled() defaults to false

	req, _ := http.NewRequest("POST", "/api/repofeed/publish/push", strings.NewReader("{}"))
	rr := httptest.NewRecorder()

	server.handleRepofeedPublishPush(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when disabled, got %d", rr.Code)
	}
}

func TestHandleRepofeedPublishPush_ConcurrentReturns409(t *testing.T) {
	cfg := &config.Config{}
	cfg.WorkspacePath = "/tmp/workspaces"
	cfg.Repofeed = &config.RepofeedConfig{Enabled: true}
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	logger := log.NewWithOptions(io.Discard, log.Options{})
	wm := workspace.New(cfg, st, statePath, logger)
	sm := session.New(cfg, st, statePath, wm, nil, logger)
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{})

	publisher := repofeed.NewPublisher(repofeed.PublisherConfig{
		DeveloperEmail: "test@example.com",
	})
	server.SetRepofeedPublisher(publisher)

	// Hold the lock manually
	unlock := publisher.LockForPush()
	if unlock == nil {
		t.Fatal("initial lock should succeed")
	}
	defer unlock()

	// Attempt push while locked — should get 409
	req, _ := http.NewRequest("POST", "/api/repofeed/publish/push", strings.NewReader("{}"))
	rr := httptest.NewRecorder()

	server.handleRepofeedPublishPush(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409 when lock held, got %d", rr.Code)
	}
}
