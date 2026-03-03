package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/models"
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
		{Name: "command", Command: "echo command"},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	sm := session.New(cfg, st, statePath, wm, log.NewWithOptions(io.Discard, log.Options{}))
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, ServerOptions{
		ShutdownCtx: shutdownCtx,
	})
	server.SetModelManager(models.New(cfg, nil))
	t.Cleanup(server.CloseForTest)
	t.Cleanup(shutdownCancel)
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
			Targets: map[string]int{"claude": 1},
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
			Targets: map[string]int{"claude": 1},
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
			Targets: map[string]int{"claude": 1},
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

	t.Run("image attachments rejected with resume", func(t *testing.T) {
		body, _ := json.Marshal(SpawnRequest{
			Repo:             "https://example.com/repo.git",
			Branch:           "main",
			Targets:          map[string]int{"claude": 1},
			Resume:           true,
			ImageAttachments: []string{"iVBORw0KGgo="},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		server.handleSpawnPost(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rr.Code)
		}
	})

	t.Run("image attachments rejected with command", func(t *testing.T) {
		body, _ := json.Marshal(SpawnRequest{
			Repo:             "https://example.com/repo.git",
			Branch:           "main",
			Command:          "echo hello",
			ImageAttachments: []string{"iVBORw0KGgo="},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		server.handleSpawnPost(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rr.Code)
		}
	})

	t.Run("image attachments rejected with remote flavor", func(t *testing.T) {
		body, _ := json.Marshal(SpawnRequest{
			RemoteFlavorID:   "some-flavor",
			Targets:          map[string]int{"claude": 1},
			Prompt:           "do stuff",
			ImageAttachments: []string{"iVBORw0KGgo="},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		server.handleSpawnPost(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rr.Code)
		}
	})

	t.Run("too many image attachments rejected", func(t *testing.T) {
		body, _ := json.Marshal(SpawnRequest{
			Repo:             "https://example.com/repo.git",
			Branch:           "main",
			Targets:          map[string]int{"claude": 1},
			Prompt:           "do stuff",
			ImageAttachments: []string{"a", "b", "c", "d", "e", "f"},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		server.handleSpawnPost(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rr.Code)
		}
	})

	t.Run("valid image attachments accepted", func(t *testing.T) {
		body, _ := json.Marshal(SpawnRequest{
			Repo:             "https://example.com/repo.git",
			Branch:           "main",
			Targets:          map[string]int{"claude": 1},
			Prompt:           "build a login page",
			ImageAttachments: []string{"iVBORw0KGgo="},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		server.handleSpawnPost(rr, req)
		if rr.Code == http.StatusBadRequest {
			t.Fatalf("expected non-400 status for valid image attachments, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("exactly 5 image attachments accepted", func(t *testing.T) {
		body, _ := json.Marshal(SpawnRequest{
			Repo:             "https://example.com/repo.git",
			Branch:           "main",
			Targets:          map[string]int{"claude": 1},
			Prompt:           "do stuff",
			ImageAttachments: []string{"a", "b", "c", "d", "e"},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		server.handleSpawnPost(rr, req)
		if rr.Code == http.StatusBadRequest {
			t.Fatalf("expected non-400 for exactly 5 attachments, got %d: %s", rr.Code, rr.Body.String())
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

	required := []string{"workspace_path", "repos", "run_targets", "quick_launch", "models", "nudgenik", "sessions", "xterm", "access_control", "needs_restart"}
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

func TestAPIContract_ConfigUpdatePreservesBarePath(t *testing.T) {
	server, cfg, _ := newTestServer(t)

	// Set up existing repos with namespaced BarePath
	cfg.Repos = []config.Repo{
		{Name: "myrepo", URL: "https://github.com/owner/myrepo.git", BarePath: "owner/myrepo.git"},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// POST config update with the same repo (simulating dashboard save)
	body := []byte(`{"repos":[{"name":"myrepo","url":"https://github.com/owner/myrepo.git"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	server.handleConfigUpdate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify BarePath was preserved in the live config
	if len(cfg.Repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(cfg.Repos))
	}
	if cfg.Repos[0].BarePath != "owner/myrepo.git" {
		t.Errorf("BarePath = %q, want %q (should be preserved from existing config)", cfg.Repos[0].BarePath, "owner/myrepo.git")
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
	st := state.New(statePath, nil)
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	sm := session.New(cfg, st, statePath, wm, log.NewWithOptions(io.Discard, log.Options{}))
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, ServerOptions{})
	t.Cleanup(server.CloseForTest)

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
		name     string
		method   string
		path     string
		fn       func(http.ResponseWriter, *http.Request)
		paramKey string
	}{
		{"dispose missing id", http.MethodPost, "/api/sessions//dispose", server.handleDispose, "sessionID"},
		{"dispose workspace missing id", http.MethodPost, "/api/workspaces//dispose", server.handleDisposeWorkspace, "workspaceID"},
		{"diff missing id", http.MethodGet, "/api/diff/", server.handleDiff, ""},
		{"open vscode missing id", http.MethodPost, "/api/open-vscode/", server.handleOpenVSCode, ""},
		{"sessions nickname missing id", http.MethodPut, "/api/sessions-nickname/", server.handleUpdateNickname, "sessionID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.paramKey != "" {
				rctx := chi.NewRouteContext()
				rctx.URLParams.Add(tt.paramKey, "")
				req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			}
			rr := httptest.NewRecorder()
			tt.fn(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", rr.Code)
			}
		})
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
		// Add chi route context with empty "id" param (simulates chi routing)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
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
		// Add chi route context with "id" param (simulates chi routing)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "dead-session")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
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
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceID", "nonexistent")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr := httptest.NewRecorder()
	server.handleWorkspaceGitGraph(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rr.Code)
	}
}

// TestGitGraphEndpoint_MethodNotAllowed is no longer needed because chi
// handles 405 responses automatically via r.Get route registration.

func TestAPIContract_DisposeBlockedByDevMode(t *testing.T) {
	// Use os.MkdirTemp instead of t.TempDir() for HOME to avoid flaky
	// TempDir cleanup failures on macOS (Spotlight/FSEvents create files
	// during os.RemoveAll, causing ENOTEMPTY).
	tmpHome, err := os.MkdirTemp("", "test-home-*")
	if err != nil {
		t.Fatalf("failed to create temp HOME: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpHome) })
	t.Setenv("HOME", tmpHome)
	schmuxDir := filepath.Join(tmpHome, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		t.Fatalf("failed to create .schmux dir: %v", err)
	}

	// Create server with dev mode enabled
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = t.TempDir()
	cfg.RunTargets = []config.RunTarget{
		{Name: "build", Command: "echo build"},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	sm := session.New(cfg, st, statePath, wm, log.NewWithOptions(io.Discard, log.Options{}))
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, ServerOptions{
		DevMode:     true,
		ShutdownCtx: shutdownCtx,
	})
	t.Cleanup(server.CloseForTest)
	t.Cleanup(shutdownCancel)

	wsPath := t.TempDir()
	ws := state.Workspace{
		ID:     "ws-dev-live",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   wsPath,
	}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	// Write dev-state.json pointing to this workspace's path
	devState, _ := json.Marshal(devStateInfo{SourceWorkspace: wsPath})
	if err := os.WriteFile(filepath.Join(schmuxDir, "dev-state.json"), devState, 0644); err != nil {
		t.Fatalf("failed to write dev-state.json: %v", err)
	}

	t.Run("dispose blocked for dev-live workspace", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/workspaces/ws-dev-live/dispose", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("workspaceID", "ws-dev-live")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rr := httptest.NewRecorder()
		server.handleDisposeWorkspace(rr, req)
		if rr.Code != http.StatusConflict {
			t.Fatalf("expected status 409, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp map[string]string
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp["error"] == "" {
			t.Fatal("expected error message in response")
		}
	})

	t.Run("dispose-all blocked for dev-live workspace", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/workspaces/ws-dev-live/dispose-all", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("workspaceID", "ws-dev-live")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rr := httptest.NewRecorder()
		server.handleDisposeWorkspaceAll(rr, req)
		if rr.Code != http.StatusConflict {
			t.Fatalf("expected status 409, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("dispose allowed for non-dev-live workspace", func(t *testing.T) {
		otherPath := t.TempDir()
		ws2 := state.Workspace{
			ID:     "ws-other",
			Repo:   "https://example.com/repo.git",
			Branch: "feature",
			Path:   otherPath,
		}
		if err := st.AddWorkspace(ws2); err != nil {
			t.Fatalf("failed to add workspace: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/workspaces/ws-other/dispose", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("workspaceID", "ws-other")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rr := httptest.NewRecorder()
		server.handleDisposeWorkspace(rr, req)
		// Should NOT be 409 — the workspace is not dev-live, so it proceeds
		// (may fail for other reasons like git safety, but not 409)
		if rr.Code == http.StatusConflict {
			t.Fatal("expected non-409 status for non-dev-live workspace, got 409")
		}
	})

	t.Run("dispose allowed when dev mode off", func(t *testing.T) {
		// Create a non-dev-mode server
		shutdownCtx2, shutdownCancel2 := context.WithCancel(context.Background())
		server2 := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, ServerOptions{
			DevMode:     false,
			ShutdownCtx: shutdownCtx2,
		})
		t.Cleanup(server2.CloseForTest)
		t.Cleanup(shutdownCancel2)
		// Re-add the workspace (it may have been disposed above)
		ws3 := state.Workspace{
			ID:     "ws-dev-live-2",
			Repo:   "https://example.com/repo.git",
			Branch: "main",
			Path:   wsPath,
		}
		_ = st.AddWorkspace(ws3)

		req := httptest.NewRequest(http.MethodPost, "/api/workspaces/ws-dev-live-2/dispose", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("workspaceID", "ws-dev-live-2")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rr := httptest.NewRecorder()
		server2.handleDisposeWorkspace(rr, req)
		if rr.Code == http.StatusConflict {
			t.Fatal("expected non-409 when dev mode is off, got 409")
		}
	})
}
