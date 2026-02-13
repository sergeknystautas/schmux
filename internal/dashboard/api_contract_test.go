package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func newTestServer(t *testing.T) (*Server, *config.Config, *state.State) {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = t.TempDir()
	cfg.RunTargets = []config.RunTarget{
		{Name: "promptable", Type: config.RunTargetTypePromptable, Command: "echo promptable", Source: config.RunTargetSourceUser},
		{Name: "command", Type: config.RunTargetTypeCommand, Command: "echo command", Source: config.RunTargetSourceUser},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(), ServerOptions{})
	return server, cfg, st
}

func TestAPIContract_Healthz(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	rr := httptest.NewRecorder()
	server.handleHealthz(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", resp["status"])
	}
}

func TestAPIContract_SpawnValidation(t *testing.T) {
	server, _, _ := newTestServer(t)

	t.Run("missing repo", func(t *testing.T) {
		body, _ := json.Marshal(SpawnRequest{
			Branch:  "main",
			Targets: map[string]int{"promptable": 1},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		server.handleSpawnPost(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rr.Code)
		}
	})

	t.Run("missing branch", func(t *testing.T) {
		body, _ := json.Marshal(SpawnRequest{
			Repo:    "https://example.com/repo.git",
			Targets: map[string]int{"promptable": 1},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		server.handleSpawnPost(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rr.Code)
		}
	})

	t.Run("prompt required for promptable", func(t *testing.T) {
		body, _ := json.Marshal(SpawnRequest{
			Repo:    "https://example.com/repo.git",
			Branch:  "main",
			Targets: map[string]int{"promptable": 1},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		server.handleSpawnPost(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rr.Code)
		}
		var results []map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&results); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0]["error"] == nil {
			t.Fatalf("expected error for missing prompt")
		}
	})

	t.Run("prompt forbidden for command", func(t *testing.T) {
		body, _ := json.Marshal(SpawnRequest{
			Repo:    "https://example.com/repo.git",
			Branch:  "main",
			Prompt:  "do thing",
			Targets: map[string]int{"command": 1},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		server.handleSpawnPost(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rr.Code)
		}
		var results []map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&results); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0]["error"] == nil {
			t.Fatalf("expected error for prompt on command target")
		}
	})

	t.Run("unknown target", func(t *testing.T) {
		body, _ := json.Marshal(SpawnRequest{
			Repo:    "https://example.com/repo.git",
			Branch:  "main",
			Targets: map[string]int{"missing": 1},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		server.handleSpawnPost(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rr.Code)
		}
		var results []map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&results); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 result, got %d", len(results))
		}
		if results[0]["error"] == nil {
			t.Fatalf("expected error for unknown target")
		}
	})
}

func TestAPIContract_ConfigGet(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rr := httptest.NewRecorder()
	server.handleConfigGet(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	required := []string{"workspace_path", "repos", "run_targets", "quick_launch", "models", "nudgenik", "terminal", "sessions", "xterm", "access_control", "needs_restart"}
	for _, key := range required {
		if _, ok := resp[key]; !ok {
			t.Fatalf("expected key %q in config response", key)
		}
	}
}

func TestAPIContract_ConfigUpdateValidation(t *testing.T) {
	server, _, _ := newTestServer(t)

	body := []byte(`{"repos":[{"name":"demo","url":""}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	server.handleConfigUpdate(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rr.Code)
	}
}

func TestAPIContract_SessionsShape(t *testing.T) {
	server, _, st := newTestServer(t)

	ws := state.Workspace{
		ID:     "ws-1",
		Repo:   "repo-url",
		Branch: "main",
		Path:   "/tmp/ws-1",
	}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	sess := state.Session{
		ID:          "sess-1",
		WorkspaceID: "ws-1",
		Target:      "command",
		TmuxSession: "tmux-1",
		CreatedAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Pid:         999999,
	}
	if err := st.AddSession(sess); err != nil {
		t.Fatalf("failed to add session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rr := httptest.NewRecorder()
	server.handleSessions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(resp))
	}
	if _, ok := resp[0]["sessions"]; !ok {
		t.Fatalf("expected sessions field in workspace response")
	}
}

func TestAPIContract_SessionsQuickLaunchNamesOnly(t *testing.T) {
	cfg := config.CreateDefault(filepath.Join(t.TempDir(), "config.json"))
	cfg.WorkspacePath = t.TempDir()
	if err := cfg.Save(); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(), ServerOptions{})

	ws := state.Workspace{
		ID:     "ws-quick",
		Repo:   "repo-url",
		Branch: "main",
		Path:   filepath.Join(cfg.WorkspacePath, "ws-quick"),
	}
	if err := os.MkdirAll(filepath.Join(ws.Path, ".schmux"), 0755); err != nil {
		t.Fatalf("failed to create workspace config dir: %v", err)
	}
	configContent := `{"quick_launch":[{"name":"Run","command":"echo run"}]}`
	if err := os.WriteFile(filepath.Join(ws.Path, ".schmux", "config.json"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config.json: %v", err)
	}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}
	wm.RefreshWorkspaceConfig(ws)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	rr := httptest.NewRecorder()
	server.handleSessions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(resp))
	}
	ql, ok := resp[0]["quick_launch"]
	if !ok {
		t.Fatalf("expected quick_launch field in workspace response")
	}
	list, ok := ql.([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("expected quick_launch list with 1 entry, got %#v", ql)
	}
	if _, ok := list[0].(string); !ok {
		t.Fatalf("expected quick_launch entry to be string, got %#v", list[0])
	}
}

func TestAPIContract_MissingIDErrors(t *testing.T) {
	server, _, _ := newTestServer(t)

	tests := []struct {
		name   string
		method string
		path   string
		fn     func(http.ResponseWriter, *http.Request)
	}{
		{"dispose missing id", http.MethodPost, "/api/sessions//dispose", server.handleDispose},
		{"dispose workspace missing id", http.MethodPost, "/api/workspaces//dispose", server.handleLinearSync},
		{"diff missing id", http.MethodGet, "/api/diff/", server.handleDiff},
		{"open vscode missing id", http.MethodPost, "/api/open-vscode/", server.handleOpenVSCode},
		{"sessions nickname missing id", http.MethodPut, "/api/sessions-nickname/", server.handleUpdateNickname},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()
			tt.fn(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", rr.Code)
			}
		})
	}
}

func TestAPIContract_WorkspacePreviewCreateAndList(t *testing.T) {
	server, _, st := newTestServer(t)
	ws := state.Workspace{
		ID:     "ws-preview",
		Repo:   "repo-url",
		Branch: "main",
		Path:   t.TempDir(),
	}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	body := bytes.NewBufferString(`{"target_host":"127.0.0.1","target_port":5173}`)
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/ws-preview/previews", body)
	rr := httptest.NewRecorder()
	server.handleWorkspaceRoutes(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/workspaces/ws-preview/previews", nil)
	rr = httptest.NewRecorder()
	server.handleWorkspaceRoutes(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var previews []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&previews); err != nil {
		t.Fatalf("failed to decode previews: %v", err)
	}
	if len(previews) != 1 {
		t.Fatalf("expected 1 preview, got %d", len(previews))
	}
}

func TestAPIContract_WorkspacePreviewRemoteWorkspaceBlocked(t *testing.T) {
	server, _, st := newTestServer(t)
	ws := state.Workspace{
		ID:           "ws-remote",
		Repo:         "repo-url",
		Branch:       "main",
		Path:         t.TempDir(),
		RemoteHostID: "rh-1",
	}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	body := bytes.NewBufferString(`{"target_host":"127.0.0.1","target_port":5173}`)
	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/ws-remote/previews", body)
	rr := httptest.NewRecorder()
	server.handleWorkspaceRoutes(rr, req)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected status 422, got %d", rr.Code)
	}
}

func TestAPIContract_DetectTools(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/detect-tools", nil)
	rr := httptest.NewRecorder()
	server.handleDetectTools(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := resp["tools"]; !ok {
		t.Fatalf("expected tools field in response")
	}
}

func TestAPIContract_Overlays(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/overlays", nil)
	rr := httptest.NewRecorder()
	server.handleOverlays(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := resp["overlays"]; !ok {
		t.Fatalf("expected overlays field in response")
	}
}

func TestAPIContract_WebSocketErrors(t *testing.T) {
	server, _, st := newTestServer(t)

	t.Run("missing session id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ws/terminal/", nil)
		rr := httptest.NewRecorder()
		server.handleTerminalWebSocket(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rr.Code)
		}
	})

	t.Run("session not running", func(t *testing.T) {
		sess := state.Session{
			ID:          "dead-session",
			WorkspaceID: "ws-dead",
			Target:      "command",
			TmuxSession: "tmux-dead",
			CreatedAt:   time.Now(),
			Pid:         999999,
		}
		if err := st.AddSession(sess); err != nil {
			t.Fatalf("failed to add session: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/ws/terminal/dead-session", nil)
		rr := httptest.NewRecorder()
		server.handleTerminalWebSocket(rr, req)
		if rr.Code != http.StatusGone {
			t.Fatalf("expected status 410, got %d", rr.Code)
		}
	})
}

func TestGitGraphEndpoint_UnknownWorkspace(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/nonexistent/git-graph", nil)
	rr := httptest.NewRecorder()
	server.handleWorkspaceGitGraph(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rr.Code)
	}
}

func TestGitGraphEndpoint_MethodNotAllowed(t *testing.T) {
	server, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/workspaces/ws-123/git-graph", nil)
	rr := httptest.NewRecorder()
	server.handleWorkspaceGitGraph(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", rr.Code)
	}
}
