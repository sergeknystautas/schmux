//go:build !nosubreddit

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
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func TestHandleSubreddit(t *testing.T) {
	t.Run("disabled when no target configured", func(t *testing.T) {
		cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
		st := state.New("", nil)
		statePath := t.TempDir() + "/state.json"
		wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
		sm := session.New(cfg, st, statePath, wm, nil, log.NewWithOptions(io.Discard, log.Options{}))
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, nil, ServerOptions{})

		req, _ := http.NewRequest("GET", "/api/subreddit", nil)
		rr := httptest.NewRecorder()

		server.handleSubreddit(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp["enabled"] != false {
			t.Errorf("expected enabled=false, got %v", resp["enabled"])
		}
	})
}
