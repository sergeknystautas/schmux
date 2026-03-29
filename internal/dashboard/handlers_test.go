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
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/persona"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func TestWriteJSON_SetsContentType(t *testing.T) {
	rr := httptest.NewRecorder()
	writeJSON(rr, map[string]string{"hello": "world"})

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
}

func TestHandleHasNudgenik(t *testing.T) {
	t.Run("disabled when no target configured", func(t *testing.T) {
		cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
		st := state.New("", nil)
		statePath := t.TempDir() + "/state.json"
		wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
		sm := session.New(cfg, st, statePath, wm, log.NewWithOptions(io.Discard, log.Options{}))
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, ServerOptions{})

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
		st := state.New("", nil)
		statePath := t.TempDir() + "/state.json"
		wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
		sm := session.New(cfg, st, statePath, wm, log.NewWithOptions(io.Discard, log.Options{}))
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, ServerOptions{})

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
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	sm := session.New(cfg, st, statePath, wm, log.NewWithOptions(io.Discard, log.Options{}))
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, ServerOptions{})

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
			name:       "valid session id, nudgenik not configured",
			sessionID:  "test-session-123",
			wantStatus: http.StatusInternalServerError, // 500 — nudgenik fails without config
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a GET request with session ID in URL path
			req, _ := http.NewRequest("GET", "/api/askNudgenik/"+tt.sessionID, nil)
			// Set up chi route context with wildcard param
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.sessionID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			rr := httptest.NewRecorder()

			server.handleAskNudgenik(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}
		})
	}
}

func TestResolveQuickLaunchByName(t *testing.T) {
	cfg := config.CreateDefault(filepath.Join(t.TempDir(), "config.json"))
	cfg.WorkspacePath = t.TempDir()
	cfg.RunTargets = []config.RunTarget{}
	if err := cfg.Save(); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	mm := models.New(cfg, nil, "")
	wm.SetModelManager(mm)
	sm := session.New(cfg, st, statePath, wm, log.NewWithOptions(io.Discard, log.Options{}))
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, ServerOptions{})
	server.SetModelManager(mm)

	ws := state.Workspace{
		ID:     "ws-1",
		Repo:   "repo-url",
		Branch: "main",
		Path:   filepath.Join(cfg.WorkspacePath, "ws-1"),
	}
	if err := os.MkdirAll(filepath.Join(ws.Path, ".schmux"), 0755); err != nil {
		t.Fatalf("failed to create workspace config dir: %v", err)
	}
	configContent := `{"quick_launch":[{"name":"Run","command":"echo run"},{"name":"Fix","target":"claude","prompt":"do it"}]}`
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
	if resolved.Target != "claude" || resolved.Prompt == "" {
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
		st := state.New("", nil)
		statePath := t.TempDir() + "/state.json"
		wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
		sm := session.New(cfg, st, statePath, wm, log.NewWithOptions(io.Discard, log.Options{}))
		server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, ServerOptions{})

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
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	sm := session.New(cfg, st, statePath, wm, log.NewWithOptions(io.Discard, log.Options{}))
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, ServerOptions{})

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

	// "POST request is rejected" subtest removed: chi handles 405
	// responses automatically via r.Get route registration.

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
			"spec review",
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
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	sm := session.New(cfg, st, statePath, wm, log.NewWithOptions(io.Discard, log.Options{}))
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, ServerOptions{})

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

	// "POST request is rejected" subtest removed: chi handles 405
	// responses automatically via r.Get route registration.
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
	st := state.New("", nil)
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	sm := session.New(cfg, st, statePath, wm, log.NewWithOptions(io.Discard, log.Options{}))
	server := NewServer(cfg, st, statePath, sm, wm, github.NewDiscovery(nil), log.NewWithOptions(io.Discard, log.Options{}), contracts.GitHubStatus{}, ServerOptions{})

	// "POST method accepted, GET rejected" subtest removed: chi handles 405
	// responses automatically via r.Post route registration.

	t.Run("concurrent updates are rejected", func(t *testing.T) {
		// Simulate an update already in progress
		server.updateMu.Lock()
		server.updateInProgress = true
		server.updateMu.Unlock()
		t.Cleanup(func() {
			server.updateMu.Lock()
			server.updateInProgress = false
			server.updateMu.Unlock()
		})

		req, _ := http.NewRequest("POST", "/api/update", nil)
		rr := httptest.NewRecorder()
		server.handleUpdate(rr, req)

		if rr.Code != http.StatusConflict {
			t.Errorf("expected 409 Conflict when update in progress, got %d", rr.Code)
		}
	})
}

func TestIsValidVCSHash(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		s    string
		want bool
	}{
		{"valid SHA-1 lowercase", "da39a3ee5e6b4b0d3255bfef95601890afd80709", true},
		{"valid SHA-1 uppercase", "DA39A3EE5E6B4B0D3255BFEF95601890AFD80709", true},
		{"valid SHA-1 mixed case", "Da39a3Ee5e6b4b0d3255bfEF95601890aFd80709", true},
		{"valid SHA-256 (64 chars)", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", true},
		{"too short (39 chars)", "da39a3ee5e6b4b0d3255bfef95601890afd8070", false},
		{"empty string", "", false},
		{"non-hex characters", "ga39a3ee5e6b4b0d3255bfef95601890afd80709", false},
		{"spaces in hash", "da39a3ee 5e6b4b0d 3255bfef 95601890 afd80709", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidVCSHash(tt.s)
			if got != tt.want {
				t.Errorf("isValidVCSHash(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestIsPathWithinDir(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		fullPath string
		baseDir  string
		want     bool
	}{
		{"file within dir", "/home/user/workspace/file.go", "/home/user/workspace", true},
		{"nested file", "/home/user/workspace/sub/file.go", "/home/user/workspace", true},
		{"dir equals base", "/home/user/workspace", "/home/user/workspace", true},
		{"path traversal escape", "/home/user/workspace/../secrets/key", "/home/user/workspace", false},
		{"sibling dir with prefix match", "/home/user/workspace-evil/file.go", "/home/user/workspace", false},
		{"completely outside", "/etc/passwd", "/home/user/workspace", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPathWithinDir(tt.fullPath, tt.baseDir)
			if got != tt.want {
				t.Errorf("isPathWithinDir(%q, %q) = %v, want %v", tt.fullPath, tt.baseDir, got, tt.want)
			}
		})
	}
}

func TestParseNumstat(t *testing.T) {
	t.Parallel()

	t.Run("parses normal numstat output", func(t *testing.T) {
		input := "10\t3\tsrc/main.go\n5\t0\tsrc/util.go\n"
		files := parseNumstat(input)
		if len(files) != 2 {
			t.Fatalf("expected 2 files, got %d", len(files))
		}
		if files[0].Path != "src/main.go" || files[0].Added != 10 || files[0].Deleted != 3 {
			t.Errorf("files[0] = %+v, want Path=src/main.go Added=10 Deleted=3", files[0])
		}
		if files[1].Path != "src/util.go" || files[1].Added != 5 || files[1].Deleted != 0 {
			t.Errorf("files[1] = %+v, want Path=src/util.go Added=5 Deleted=0", files[1])
		}
	})

	t.Run("handles binary files (dash for added/deleted)", func(t *testing.T) {
		input := "-\t-\timage.png\n"
		files := parseNumstat(input)
		if len(files) != 1 {
			t.Fatalf("expected 1 file, got %d", len(files))
		}
		// Binary files have "-" which Sscanf can't parse as int → stays 0
		if files[0].Path != "image.png" || files[0].Added != 0 || files[0].Deleted != 0 {
			t.Errorf("binary file = %+v, want Path=image.png Added=0 Deleted=0", files[0])
		}
	})

	t.Run("empty input returns nil", func(t *testing.T) {
		files := parseNumstat("")
		if files != nil {
			t.Errorf("expected nil, got %v", files)
		}
	})

	t.Run("skips malformed lines", func(t *testing.T) {
		input := "10\t3\tvalid.go\ngarbage\n5\t0\tother.go\n"
		files := parseNumstat(input)
		if len(files) != 2 {
			t.Fatalf("expected 2 files (skipping garbage), got %d", len(files))
		}
	})
}

func TestFormatPersonaPrompt(t *testing.T) {
	t.Parallel()

	t.Run("with expectations", func(t *testing.T) {
		p := &persona.Persona{
			Name:         "QA Engineer",
			Expectations: "Find bugs before users do.",
			Prompt:       "You are a QA engineer.",
		}
		got := formatPersonaPrompt(p)
		if !strings.Contains(got, "## Persona: QA Engineer") {
			t.Error("missing persona header")
		}
		if !strings.Contains(got, "### Behavioral Expectations") {
			t.Error("missing expectations section")
		}
		if !strings.Contains(got, "### Instructions") {
			t.Error("missing instructions section")
		}
	})

	t.Run("without expectations", func(t *testing.T) {
		p := &persona.Persona{
			Name:   "Coder",
			Prompt: "Write code.",
		}
		got := formatPersonaPrompt(p)
		if strings.Contains(got, "### Behavioral Expectations") {
			t.Error("should not include expectations section when empty")
		}
		if !strings.Contains(got, "### Instructions") {
			t.Error("missing instructions section")
		}
	})
}
