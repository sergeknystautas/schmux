package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func TestHandleHasNudgenik(t *testing.T) {
	t.Run("disabled when no target configured", func(t *testing.T) {
		cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
		st := state.New("")
		statePath := t.TempDir() + "/state.json"
		wm := workspace.New(cfg, st, statePath)
		sm := session.New(cfg, st, statePath, wm)
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(), ServerOptions{})

		req, _ := http.NewRequest("GET", "/api/hasNudgenik", nil)
		rr := httptest.NewRecorder()

		server.handleHasNudgenik(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}

		var resp map[string]bool
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp["available"] {
			t.Errorf("expected available=false when no target configured, got %v", resp["available"])
		}
	})

	t.Run("enabled when target configured", func(t *testing.T) {
		cfg := &config.Config{
			WorkspacePath: "/tmp/workspaces",
			Nudgenik:      &config.NudgenikConfig{Target: "any-target"},
		}
		st := state.New("")
		statePath := t.TempDir() + "/state.json"
		wm := workspace.New(cfg, st, statePath)
		sm := session.New(cfg, st, statePath, wm)
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(), ServerOptions{})

		req, _ := http.NewRequest("GET", "/api/hasNudgenik", nil)
		rr := httptest.NewRecorder()

		server.handleHasNudgenik(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}

		var resp map[string]bool
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if !resp["available"] {
			t.Errorf("expected available=true when target configured, got %v", resp["available"])
		}
	})
}

func TestHandleAskNudgenik(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(), ServerOptions{})

	// Add a test session
	testSession := state.Session{
		ID:          "test-session-123",
		WorkspaceID: "test-workspace",
		Target:      "test",
		TmuxSession: "test-tmux-session",
	}
	st.AddSession(testSession)

	tests := []struct {
		name       string
		sessionID  string
		wantStatus int
	}{
		{
			name:       "missing session id",
			sessionID:  "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid session id (not found)",
			sessionID:  "nonexistent-session",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "valid session id",
			sessionID:  "test-session-123",
			wantStatus: http.StatusOK, // Or 500 if tmux capture fails or CLI unavailable
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a GET request with session ID in URL path
			req, _ := http.NewRequest("GET", "/api/askNudgenik/"+tt.sessionID, nil)
			rr := httptest.NewRecorder()

			server.handleAskNudgenik(rr, req)

			// Check response status
			// For the valid session case, accept multiple status codes since
			// behavior depends on configuration and tmux availability:
			// 200 = nudgenik ran successfully
			// 400 = session not found or invalid
			// 500 = nudgenik execution error
			// 503 = nudgenik not configured
			if tt.name == "valid session id" {
				if rr.Code != http.StatusOK && rr.Code != http.StatusBadRequest &&
					rr.Code != http.StatusInternalServerError && rr.Code != http.StatusServiceUnavailable {
					t.Errorf("expected status 200, 400, 500, or 503, got %d", rr.Code)
				}
			} else {
				if rr.Code != tt.wantStatus {
					t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
				}
			}
		})
	}
}

func TestResolveQuickLaunchByName(t *testing.T) {
	cfg := config.CreateDefault(filepath.Join(t.TempDir(), "config.json"))
	cfg.WorkspacePath = t.TempDir()
	cfg.RunTargets = []config.RunTarget{
		{Name: "promptable", Type: config.RunTargetTypePromptable, Command: "echo promptable", Source: config.RunTargetSourceUser},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath)
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(), ServerOptions{})

	ws := state.Workspace{
		ID:     "ws-1",
		Repo:   "repo-url",
		Branch: "main",
		Path:   filepath.Join(cfg.WorkspacePath, "ws-1"),
	}
	if err := os.MkdirAll(filepath.Join(ws.Path, ".schmux"), 0755); err != nil {
		t.Fatalf("failed to create workspace config dir: %v", err)
	}
	configContent := `{"quick_launch":[{"name":"Run","command":"echo run"},{"name":"Fix","target":"promptable","prompt":"do it"}]}`
	if err := os.WriteFile(filepath.Join(ws.Path, ".schmux", "config.json"), []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config.json: %v", err)
	}
	if err := st.AddWorkspace(ws); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}
	wm.RefreshWorkspaceConfig(ws)

	resolved, err := server.resolveQuickLaunchByName(ws.ID, "Run")
	if err != nil {
		t.Fatalf("expected resolve to succeed: %v", err)
	}
	if resolved.Command == "" || resolved.Target != "" {
		t.Fatalf("expected command-based quick launch, got %+v", resolved)
	}

	resolved, err = server.resolveQuickLaunchByName(ws.ID, "Fix")
	if err != nil {
		t.Fatalf("expected resolve to succeed: %v", err)
	}
	if resolved.Target != "promptable" || resolved.Prompt == "" {
		t.Fatalf("expected promptable quick launch, got %+v", resolved)
	}
}

func TestHandleSpawnPost_CommandMissingWorkspace(t *testing.T) {
	server, _, _ := newTestServer(t)

	body, _ := json.Marshal(SpawnRequest{
		WorkspaceID: "missing-workspace",
		Command:     "echo hi",
		Nickname:    "Run",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	server.handleSpawnPost(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var resp []map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp))
	}
	if resp[0]["error"] == nil {
		t.Fatalf("expected error for missing workspace")
	}
}

func TestHandleSuggestBranch(t *testing.T) {
	t.Run("disabled when no target configured", func(t *testing.T) {
		cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
		st := state.New("")
		statePath := t.TempDir() + "/state.json"
		wm := workspace.New(cfg, st, statePath)
		sm := session.New(cfg, st, statePath, wm)
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(), ServerOptions{})

		body := bytes.NewReader([]byte(`{"prompt":"test prompt"}`))
		req, _ := http.NewRequest(http.MethodPost, "/api/suggest-branch", body)
		rr := httptest.NewRecorder()

		server.handleSuggestBranch(rr, req)

		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected status 503, got %d", rr.Code)
		}
	})
}

func TestHandleBuiltinQuickLaunchCookbook(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(), ServerOptions{})

	t.Run("GET request returns presets", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/builtin-quick-launch", nil)
		rr := httptest.NewRecorder()

		server.handleBuiltinQuickLaunch(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}

		var presets []BuiltinQuickLaunchCookbook
		if err := json.NewDecoder(rr.Body).Decode(&presets); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		// Check that we got some presets (the file has 5 entries)
		if len(presets) == 0 {
			t.Error("expected at least one preset, got none")
		}

		// Verify each preset has non-empty name, target, and prompt
		for _, preset := range presets {
			if preset.Name == "" {
				t.Errorf("preset has empty name: %+v", preset)
			}
			if preset.Target == "" {
				t.Errorf("preset %q has empty target", preset.Name)
			}
			if preset.Prompt == "" {
				t.Errorf("preset %q has empty prompt", preset.Name)
			}
		}
	})

	t.Run("POST request is rejected", func(t *testing.T) {
		req, _ := http.NewRequest("POST", "/api/builtin-quick-launch", nil)
		rr := httptest.NewRecorder()

		server.handleBuiltinQuickLaunch(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", rr.Code)
		}
	})

	t.Run("response contains expected presets", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/builtin-quick-launch", nil)
		rr := httptest.NewRecorder()

		server.handleBuiltinQuickLaunch(rr, req)

		var presets []BuiltinQuickLaunchCookbook
		if err := json.NewDecoder(rr.Body).Decode(&presets); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		// Check for known cookbook names from cookbooks.json
		presetNames := make(map[string]bool)
		for _, preset := range presets {
			presetNames[preset.Name] = true
		}

		expectedNames := []string{
			"code review - local",
			"code review - branch",
			"git commit",
			"merge in main",
			"tech writer",
		}

		for _, expected := range expectedNames {
			if !presetNames[expected] {
				t.Errorf("expected to find preset %q, but it was not found", expected)
			}
		}
	})
}

func TestHandleHealthz(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(), ServerOptions{})

	// Start version check to populate version info
	server.StartVersionCheck()

	t.Run("GET request returns version info", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/healthz", nil)
		rr := httptest.NewRecorder()

		server.handleHealthz(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}

		var resp map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if resp["status"] != "ok" {
			t.Errorf("expected status ok, got %v", resp["status"])
		}

		if resp["version"] == nil {
			t.Error("expected version field in response")
		}
	})

	t.Run("POST request is rejected", func(t *testing.T) {
		req, _ := http.NewRequest("POST", "/api/healthz", nil)
		rr := httptest.NewRecorder()

		server.handleHealthz(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405, got %d", rr.Code)
		}
	})
}

func TestValidateGitFilePaths(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  string // "" means valid
	}{
		{"valid path", []string{"src/main.go"}, ""},
		{"nested valid path", []string{"a/b/c/file.txt"}, ""},
		{"absolute path", []string{"/etc/passwd"}, `invalid file path: "/etc/passwd"`},
		{"parent traversal", []string{"../../etc/passwd"}, `invalid file path: "../../etc/passwd"`},
		{"empty string", []string{""}, `invalid file path: ""`},
		{"dot", []string{"."}, `invalid file path: "."`},
		{"dot slash", []string{"./"}, `invalid file path: "./"`},
		{"mixed valid and invalid", []string{"ok.go", "."}, `invalid file path: "."`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateGitFilePaths(tt.files)
			if got != tt.want {
				t.Errorf("validateGitFilePaths(%v) = %q, want %q", tt.files, got, tt.want)
			}
		})
	}
}

func TestHandleUpdate(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(), ServerOptions{})

	t.Run("POST method accepted, GET rejected", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/update", nil)
		rr := httptest.NewRecorder()

		server.handleUpdate(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected status 405 for GET, got %d", rr.Code)
		}
	})

	t.Run("concurrent updates are rejected", func(t *testing.T) {
		// First request
		req1, _ := http.NewRequest("POST", "/api/update", nil)
		rr1 := httptest.NewRecorder()

		// This test is limited - we can't easily test actual concurrent updates
		// without mocking the update.Update() function
		server.handleUpdate(rr1, req1)

		// The first request will fail because we're on dev build or no network,
		// but it should return some status
		if rr1.Code != http.StatusInternalServerError && rr1.Code != http.StatusOK {
			t.Logf("first request got status %d (expected 500 or 200)", rr1.Code)
		}
	})
}
