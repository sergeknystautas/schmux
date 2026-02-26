package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// withSessionID injects a chi route context with the given sessionID param.
func withSessionID(req *http.Request, sessionID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("sessionID", sessionID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// withWorkspaceID injects a chi route context with the given workspaceID param.
func withWorkspaceID(req *http.Request, workspaceID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceID", workspaceID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// --- Tell handler tests ---

func TestHandleTellSession_SessionNotFound(t *testing.T) {
	server, _, _ := newTestServer(t)

	body, _ := json.Marshal(tellRequest{Message: "hello"})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/nonexistent/tell", bytes.NewReader(body))
	req = withSessionID(req, "nonexistent")
	rr := httptest.NewRecorder()

	server.handleTellSession(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestHandleTellSession_EmptyMessage(t *testing.T) {
	server, _, _ := newTestServer(t)

	body, _ := json.Marshal(tellRequest{Message: ""})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/sess-1/tell", bytes.NewReader(body))
	req = withSessionID(req, "sess-1")
	rr := httptest.NewRecorder()

	server.handleTellSession(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestHandleTellSession_InvalidJSON(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/sess-1/tell", bytes.NewReader([]byte("not json")))
	req = withSessionID(req, "sess-1")
	rr := httptest.NewRecorder()

	server.handleTellSession(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestHandleTellSession_LocalNoTmuxSession(t *testing.T) {
	server, _, st := newTestServer(t)

	// Add a session with no TmuxSession (not running)
	st.AddSession(state.Session{
		ID:          "sess-no-tmux",
		WorkspaceID: "ws-1",
		Target:      "claude",
		TmuxSession: "",
	})

	body, _ := json.Marshal(tellRequest{Message: "hello"})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/sess-no-tmux/tell", bytes.NewReader(body))
	req = withSessionID(req, "sess-no-tmux")
	rr := httptest.NewRecorder()

	server.handleTellSession(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rr.Code)
	}
}

func TestHandleTellSession_RemoteNoManager(t *testing.T) {
	server, _, st := newTestServer(t)

	// Add a remote session (remoteManager is nil on test server)
	st.AddSession(state.Session{
		ID:           "sess-remote",
		WorkspaceID:  "ws-1",
		Target:       "claude",
		RemoteHostID: "host-1",
		RemotePaneID: "%0",
	})

	body, _ := json.Marshal(tellRequest{Message: "hello"})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/sess-remote/tell", bytes.NewReader(body))
	req = withSessionID(req, "sess-remote")
	rr := httptest.NewRecorder()

	server.handleTellSession(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

// --- Events handler tests ---

func TestHandleGetSessionEvents_SessionNotFound(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/nonexistent/events", nil)
	req = withSessionID(req, "nonexistent")
	rr := httptest.NewRecorder()

	server.handleGetSessionEvents(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestHandleGetSessionEvents_NoEventsFile(t *testing.T) {
	server, _, st := newTestServer(t)

	wsPath := t.TempDir()
	st.AddWorkspace(state.Workspace{
		ID:   "ws-events",
		Repo: "https://github.com/user/repo.git",
		Path: wsPath,
	})
	st.AddSession(state.Session{
		ID:          "sess-events",
		WorkspaceID: "ws-events",
		Target:      "claude",
		TmuxSession: "tmux-sess",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-events/events", nil)
	req = withSessionID(req, "sess-events")
	rr := httptest.NewRecorder()

	server.handleGetSessionEvents(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var events []json.RawMessage
	if err := json.NewDecoder(rr.Body).Decode(&events); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestHandleGetSessionEvents_ReadsEventsFile(t *testing.T) {
	server, _, st := newTestServer(t)

	wsPath := t.TempDir()
	st.AddWorkspace(state.Workspace{
		ID:   "ws-events",
		Repo: "https://github.com/user/repo.git",
		Path: wsPath,
	})
	st.AddSession(state.Session{
		ID:          "sess-events",
		WorkspaceID: "ws-events",
		Target:      "claude",
		TmuxSession: "tmux-sess",
	})

	// Write events file
	eventsDir := filepath.Join(wsPath, ".schmux", "events")
	os.MkdirAll(eventsDir, 0755)
	eventsFile := filepath.Join(eventsDir, "sess-events.jsonl")
	os.WriteFile(eventsFile, []byte(
		`{"ts":"2024-01-01T00:00:00Z","type":"status","state":"working","message":"Session spawned"}
{"ts":"2024-01-01T00:01:00Z","type":"status","state":"needs_input","message":"Need clarification"}
{"ts":"2024-01-01T00:02:00Z","type":"failure","category":"tool","tool":"bash","error":"command not found"}
`), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-events/events", nil)
	req = withSessionID(req, "sess-events")
	rr := httptest.NewRecorder()

	server.handleGetSessionEvents(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var events []json.RawMessage
	if err := json.NewDecoder(rr.Body).Decode(&events); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("expected 3 events, got %d", len(events))
	}
}

func TestHandleGetSessionEvents_TypeFilter(t *testing.T) {
	server, _, st := newTestServer(t)

	wsPath := t.TempDir()
	st.AddWorkspace(state.Workspace{
		ID:   "ws-filter",
		Repo: "https://github.com/user/repo.git",
		Path: wsPath,
	})
	st.AddSession(state.Session{
		ID:          "sess-filter",
		WorkspaceID: "ws-filter",
		Target:      "claude",
		TmuxSession: "tmux-sess",
	})

	eventsDir := filepath.Join(wsPath, ".schmux", "events")
	os.MkdirAll(eventsDir, 0755)
	eventsFile := filepath.Join(eventsDir, "sess-filter.jsonl")
	os.WriteFile(eventsFile, []byte(
		`{"ts":"2024-01-01T00:00:00Z","type":"status","state":"working","message":"working"}
{"ts":"2024-01-01T00:01:00Z","type":"failure","category":"tool","error":"err"}
{"ts":"2024-01-01T00:02:00Z","type":"status","state":"completed","message":"done"}
`), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-filter/events?type=status", nil)
	req = withSessionID(req, "sess-filter")
	rr := httptest.NewRecorder()

	server.handleGetSessionEvents(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var events []json.RawMessage
	json.NewDecoder(rr.Body).Decode(&events)
	if len(events) != 2 {
		t.Errorf("expected 2 status events, got %d", len(events))
	}
}

func TestHandleGetSessionEvents_LastN(t *testing.T) {
	server, _, st := newTestServer(t)

	wsPath := t.TempDir()
	st.AddWorkspace(state.Workspace{
		ID:   "ws-lastn",
		Repo: "https://github.com/user/repo.git",
		Path: wsPath,
	})
	st.AddSession(state.Session{
		ID:          "sess-lastn",
		WorkspaceID: "ws-lastn",
		Target:      "claude",
		TmuxSession: "tmux-sess",
	})

	eventsDir := filepath.Join(wsPath, ".schmux", "events")
	os.MkdirAll(eventsDir, 0755)
	eventsFile := filepath.Join(eventsDir, "sess-lastn.jsonl")
	os.WriteFile(eventsFile, []byte(
		`{"ts":"2024-01-01T00:00:00Z","type":"status","state":"working","message":"one"}
{"ts":"2024-01-01T00:01:00Z","type":"status","state":"working","message":"two"}
{"ts":"2024-01-01T00:02:00Z","type":"status","state":"working","message":"three"}
`), 0644)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-lastn/events?last=2", nil)
	req = withSessionID(req, "sess-lastn")
	rr := httptest.NewRecorder()

	server.handleGetSessionEvents(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var events []json.RawMessage
	json.NewDecoder(rr.Body).Decode(&events)
	if len(events) != 2 {
		t.Errorf("expected 2 events (last 2), got %d", len(events))
	}

	// Verify we got the last 2, not the first 2
	var evt struct {
		Message string `json:"message"`
	}
	json.Unmarshal(events[0], &evt)
	if evt.Message != "two" {
		t.Errorf("expected first event to be 'two', got %q", evt.Message)
	}
}

func TestHandleGetSessionEvents_RemoteNoManager(t *testing.T) {
	server, _, st := newTestServer(t)

	st.AddWorkspace(state.Workspace{
		ID:           "ws-remote",
		Repo:         "https://github.com/user/repo.git",
		RemoteHostID: "host-1",
		RemotePath:   "/remote/path",
	})
	st.AddSession(state.Session{
		ID:           "sess-remote",
		WorkspaceID:  "ws-remote",
		Target:       "claude",
		RemoteHostID: "host-1",
		RemotePaneID: "%0",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-remote/events", nil)
	req = withSessionID(req, "sess-remote")
	rr := httptest.NewRecorder()

	server.handleGetSessionEvents(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

// --- Capture handler tests ---

func TestHandleCaptureSession_SessionNotFound(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/nonexistent/capture", nil)
	req = withSessionID(req, "nonexistent")
	rr := httptest.NewRecorder()

	server.handleCaptureSession(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestHandleCaptureSession_LocalNoTmuxSession(t *testing.T) {
	server, _, st := newTestServer(t)

	st.AddSession(state.Session{
		ID:          "sess-no-tmux",
		WorkspaceID: "ws-1",
		Target:      "claude",
		TmuxSession: "",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-no-tmux/capture", nil)
	req = withSessionID(req, "sess-no-tmux")
	rr := httptest.NewRecorder()

	server.handleCaptureSession(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rr.Code)
	}
}

func TestHandleCaptureSession_RemoteNoManager(t *testing.T) {
	server, _, st := newTestServer(t)

	st.AddSession(state.Session{
		ID:           "sess-remote",
		WorkspaceID:  "ws-1",
		Target:       "claude",
		RemoteHostID: "host-1",
		RemotePaneID: "%0",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-remote/capture", nil)
	req = withSessionID(req, "sess-remote")
	rr := httptest.NewRecorder()

	server.handleCaptureSession(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

func TestHandleCaptureSession_DefaultLines(t *testing.T) {
	server, _, st := newTestServer(t)

	// Add session with tmux — capture will fail because tmux isn't running,
	// but we can verify the handler doesn't panic and returns an error response.
	st.AddSession(state.Session{
		ID:          "sess-tmux",
		WorkspaceID: "ws-1",
		Target:      "claude",
		TmuxSession: "nonexistent-tmux-session",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-tmux/capture", nil)
	req = withSessionID(req, "sess-tmux")
	rr := httptest.NewRecorder()

	server.handleCaptureSession(rr, req)

	// tmux capture will fail because the session doesn't exist, returning 500
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 (tmux not available), got %d", rr.Code)
	}
}

func TestHandleCaptureSession_CustomLines(t *testing.T) {
	server, _, st := newTestServer(t)

	st.AddSession(state.Session{
		ID:          "sess-tmux",
		WorkspaceID: "ws-1",
		Target:      "claude",
		TmuxSession: "nonexistent-tmux",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/sess-tmux/capture?lines=100", nil)
	req = withSessionID(req, "sess-tmux")
	rr := httptest.NewRecorder()

	server.handleCaptureSession(rr, req)

	// Will fail at tmux level, but validates query param parsing doesn't panic
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 (tmux not available), got %d", rr.Code)
	}
}

// --- Inspect handler tests ---

func TestHandleInspectWorkspace_NotFound(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/nonexistent/inspect", nil)
	req = withWorkspaceID(req, "nonexistent")
	rr := httptest.NewRecorder()

	server.handleInspectWorkspace(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestHandleInspectWorkspace_LocalWorkspace(t *testing.T) {
	server, cfg, st := newTestServer(t)

	// Set up a workspace pointing to a real git repo (the test temp dir won't have git,
	// but the handler should still return 200 with empty/zero fields)
	wsPath := t.TempDir()
	cfg.Repos = []config.Repo{{Name: "testrepo", URL: "https://github.com/user/testrepo.git"}}
	st.AddWorkspace(state.Workspace{
		ID:   "ws-inspect",
		Repo: "https://github.com/user/testrepo.git",
		Path: wsPath,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/ws-inspect/inspect", nil)
	req = withWorkspaceID(req, "ws-inspect")
	rr := httptest.NewRecorder()

	server.handleInspectWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp inspectResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp.WorkspaceID != "ws-inspect" {
		t.Errorf("expected workspace_id 'ws-inspect', got %q", resp.WorkspaceID)
	}
	if resp.Repo != "testrepo" {
		t.Errorf("expected repo 'testrepo', got %q", resp.Repo)
	}
}

func TestHandleInspectWorkspace_RemoteNoManager(t *testing.T) {
	server, _, st := newTestServer(t)

	st.AddWorkspace(state.Workspace{
		ID:           "ws-remote",
		Repo:         "https://github.com/user/repo.git",
		RemoteHostID: "host-1",
		RemotePath:   "/remote/path",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/ws-remote/inspect", nil)
	req = withWorkspaceID(req, "ws-remote")
	rr := httptest.NewRecorder()

	server.handleInspectWorkspace(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rr.Code)
	}
}

func TestHandleInspectWorkspace_FallbackRepoName(t *testing.T) {
	server, _, st := newTestServer(t)

	wsPath := t.TempDir()
	// Don't add repo to config — should fall back to the raw URL
	st.AddWorkspace(state.Workspace{
		ID:   "ws-norepo",
		Repo: "https://github.com/user/unknown.git",
		Path: wsPath,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/ws-norepo/inspect", nil)
	req = withWorkspaceID(req, "ws-norepo")
	rr := httptest.NewRecorder()

	server.handleInspectWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp inspectResponse
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Repo != "https://github.com/user/unknown.git" {
		t.Errorf("expected raw repo URL as fallback, got %q", resp.Repo)
	}
}

// --- Branches handler tests ---

func TestHandleGetBranches_Empty(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/branches", nil)
	rr := httptest.NewRecorder()

	server.handleGetBranches(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var entries []branchEntry
	if err := json.NewDecoder(rr.Body).Decode(&entries); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestHandleGetBranches_LocalWorkspaces(t *testing.T) {
	server, cfg, st := newTestServer(t)

	wsPath := t.TempDir()
	cfg.Repos = []config.Repo{{Name: "myrepo", URL: "https://github.com/user/myrepo.git"}}
	st.AddWorkspace(state.Workspace{
		ID:   "ws-branch-1",
		Repo: "https://github.com/user/myrepo.git",
		Path: wsPath,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/branches", nil)
	rr := httptest.NewRecorder()

	server.handleGetBranches(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var entries []branchEntry
	if err := json.NewDecoder(rr.Body).Decode(&entries); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].WorkspaceID != "ws-branch-1" {
		t.Errorf("expected workspace_id 'ws-branch-1', got %q", entries[0].WorkspaceID)
	}
	if entries[0].Repo != "myrepo" {
		t.Errorf("expected repo 'myrepo', got %q", entries[0].Repo)
	}
}

func TestHandleGetBranches_WithSessions(t *testing.T) {
	server, _, st := newTestServer(t)

	wsPath := t.TempDir()
	st.AddWorkspace(state.Workspace{
		ID:   "ws-with-sessions",
		Repo: "https://github.com/user/repo.git",
		Path: wsPath,
	})
	st.AddSession(state.Session{
		ID:          "sess-1",
		WorkspaceID: "ws-with-sessions",
		Target:      "claude",
		TmuxSession: "tmux-1",
		Nudge:       "working",
	})
	st.AddSession(state.Session{
		ID:          "sess-2",
		WorkspaceID: "ws-with-sessions",
		Target:      "codex",
		TmuxSession: "tmux-2",
		Nudge:       "needs_input",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/branches", nil)
	rr := httptest.NewRecorder()

	server.handleGetBranches(rr, req)

	var entries []branchEntry
	json.NewDecoder(rr.Body).Decode(&entries)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].SessionCount != 2 {
		t.Errorf("expected 2 sessions, got %d", entries[0].SessionCount)
	}
	if len(entries[0].SessionStates) != 2 {
		t.Errorf("expected 2 session states, got %d", len(entries[0].SessionStates))
	}
}

func TestHandleGetBranches_RemoteDisconnected(t *testing.T) {
	server, _, st := newTestServer(t)

	st.AddWorkspace(state.Workspace{
		ID:           "ws-disconnected",
		Repo:         "https://github.com/user/repo.git",
		RemoteHostID: "host-1",
		RemotePath:   "/remote/path",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/branches", nil)
	rr := httptest.NewRecorder()

	server.handleGetBranches(rr, req)

	var entries []branchEntry
	json.NewDecoder(rr.Body).Decode(&entries)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if !entries[0].Disconnected {
		t.Error("expected disconnected=true for remote workspace with no manager")
	}
}
