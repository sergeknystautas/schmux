package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
)

func TestHandleShareIntent(t *testing.T) {
	t.Run("sets intent_shared true", func(t *testing.T) {
		server, _, st := newTestServer(t)
		server.config.Repofeed = &config.RepofeedConfig{Enabled: true}
		ws := addWorkspaceToServer(t, st, "ws-si-1")

		wsH := newTestWorkspaceHandlers(server)
		body, _ := json.Marshal(map[string]bool{"share": true})
		req := makeWorkspaceRequest(t, http.MethodPost,
			"/api/workspaces/"+ws.ID+"/share-intent", ws.ID, body)
		rr := httptest.NewRecorder()
		wsH.handleShareIntent(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		updated, ok := st.GetWorkspace(ws.ID)
		if !ok {
			t.Fatal("workspace not found after update")
		}
		if !updated.IntentShared {
			t.Error("expected IntentShared to be true")
		}
	})

	t.Run("sets intent_shared false", func(t *testing.T) {
		server, _, st := newTestServer(t)
		server.config.Repofeed = &config.RepofeedConfig{Enabled: true}
		ws := addWorkspaceToServer(t, st, "ws-si-2")

		// First set to true
		wsState, _ := st.GetWorkspace(ws.ID)
		wsState.IntentShared = true
		st.UpdateWorkspace(wsState)

		wsH := newTestWorkspaceHandlers(server)
		body, _ := json.Marshal(map[string]bool{"share": false})
		req := makeWorkspaceRequest(t, http.MethodPost,
			"/api/workspaces/"+ws.ID+"/share-intent", ws.ID, body)
		rr := httptest.NewRecorder()
		wsH.handleShareIntent(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		updated, _ := st.GetWorkspace(ws.ID)
		if updated.IntentShared {
			t.Error("expected IntentShared to be false")
		}
	})

	t.Run("returns 404 when repofeed disabled", func(t *testing.T) {
		server, _, st := newTestServer(t)
		// Repofeed not enabled (default)
		ws := addWorkspaceToServer(t, st, "ws-si-3")

		wsH := newTestWorkspaceHandlers(server)
		body, _ := json.Marshal(map[string]bool{"share": true})
		req := makeWorkspaceRequest(t, http.MethodPost,
			"/api/workspaces/"+ws.ID+"/share-intent", ws.ID, body)
		rr := httptest.NewRecorder()
		wsH.handleShareIntent(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("returns 404 for unknown workspace", func(t *testing.T) {
		server, _, _ := newTestServer(t)
		server.config.Repofeed = &config.RepofeedConfig{Enabled: true}

		wsH := newTestWorkspaceHandlers(server)
		body, _ := json.Marshal(map[string]bool{"share": true})
		req := makeWorkspaceRequest(t, http.MethodPost,
			"/api/workspaces/ws-nonexistent/share-intent", "ws-nonexistent", body)
		rr := httptest.NewRecorder()
		wsH.handleShareIntent(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("returns 400 for invalid body", func(t *testing.T) {
		server, _, st := newTestServer(t)
		server.config.Repofeed = &config.RepofeedConfig{Enabled: true}
		ws := addWorkspaceToServer(t, st, "ws-si-5")

		wsH := newTestWorkspaceHandlers(server)
		req := makeWorkspaceRequest(t, http.MethodPost,
			"/api/workspaces/"+ws.ID+"/share-intent", ws.ID, []byte("not-json"))
		rr := httptest.NewRecorder()
		wsH.handleShareIntent(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}
