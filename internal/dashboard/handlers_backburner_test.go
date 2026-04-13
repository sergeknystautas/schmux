package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleBackburnerWorkspace(t *testing.T) {
	t.Run("sets backburner true", func(t *testing.T) {
		server, cfg, st := newTestServer(t)
		cfg.BackburnerEnabled = true
		ws := addWorkspaceToServer(t, st, "ws-bb-1")

		wsH := newTestWorkspaceHandlers(server)
		body, _ := json.Marshal(map[string]bool{"backburner": true})
		req := makeWorkspaceRequest(t, http.MethodPost,
			"/api/workspaces/"+ws.ID+"/backburner", ws.ID, body)
		rr := httptest.NewRecorder()
		wsH.handleBackburnerWorkspace(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		updated, ok := st.GetWorkspace(ws.ID)
		if !ok {
			t.Fatal("workspace not found after update")
		}
		if !updated.Backburner {
			t.Error("expected Backburner to be true")
		}
	})

	t.Run("sets backburner false", func(t *testing.T) {
		server, cfg, st := newTestServer(t)
		cfg.BackburnerEnabled = true
		ws := addWorkspaceToServer(t, st, "ws-bb-2")

		// First set to true
		wsState, _ := st.GetWorkspace(ws.ID)
		wsState.Backburner = true
		st.UpdateWorkspace(wsState)

		wsH := newTestWorkspaceHandlers(server)
		body, _ := json.Marshal(map[string]bool{"backburner": false})
		req := makeWorkspaceRequest(t, http.MethodPost,
			"/api/workspaces/"+ws.ID+"/backburner", ws.ID, body)
		rr := httptest.NewRecorder()
		wsH.handleBackburnerWorkspace(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		updated, _ := st.GetWorkspace(ws.ID)
		if updated.Backburner {
			t.Error("expected Backburner to be false")
		}
	})

	t.Run("returns 404 when feature disabled", func(t *testing.T) {
		server, cfg, st := newTestServer(t)
		cfg.BackburnerEnabled = false
		ws := addWorkspaceToServer(t, st, "ws-bb-3")

		wsH := newTestWorkspaceHandlers(server)
		body, _ := json.Marshal(map[string]bool{"backburner": true})
		req := makeWorkspaceRequest(t, http.MethodPost,
			"/api/workspaces/"+ws.ID+"/backburner", ws.ID, body)
		rr := httptest.NewRecorder()
		wsH.handleBackburnerWorkspace(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("returns 404 for unknown workspace", func(t *testing.T) {
		server, cfg, _ := newTestServer(t)
		cfg.BackburnerEnabled = true

		wsH := newTestWorkspaceHandlers(server)
		body, _ := json.Marshal(map[string]bool{"backburner": true})
		req := makeWorkspaceRequest(t, http.MethodPost,
			"/api/workspaces/ws-nonexistent/backburner", "ws-nonexistent", body)
		rr := httptest.NewRecorder()
		wsH.handleBackburnerWorkspace(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("returns 400 for invalid request body", func(t *testing.T) {
		server, cfg, st := newTestServer(t)
		cfg.BackburnerEnabled = true
		ws := addWorkspaceToServer(t, st, "ws-bb-4")

		wsH := newTestWorkspaceHandlers(server)
		req := makeWorkspaceRequest(t, http.MethodPost,
			"/api/workspaces/"+ws.ID+"/backburner", ws.ID, []byte("not-json"))
		rr := httptest.NewRecorder()
		wsH.handleBackburnerWorkspace(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}
