package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

func postSpawnJSON(t *testing.T, handler http.HandlerFunc, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}
	req := httptest.NewRequest("POST", "/api/spawn", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func TestHandleSpawnPost_ValidationErrors(t *testing.T) {
	tests := []struct {
		name       string
		body       SpawnRequest
		wantCode   int
		wantSubstr string
	}{
		{
			name:       "missing repo and branch",
			body:       SpawnRequest{Targets: map[string]int{"claude": 1}, Prompt: "hello"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "repo is required",
		},
		{
			name:       "missing branch",
			body:       SpawnRequest{Repo: "https://github.com/foo/bar", Targets: map[string]int{"claude": 1}, Prompt: "hello"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "branch is required",
		},
		{
			name:       "missing command and targets",
			body:       SpawnRequest{Repo: "https://github.com/foo/bar", Branch: "main"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "either command or targets is required",
		},
		{
			name:       "both command and targets",
			body:       SpawnRequest{Repo: "https://github.com/foo/bar", Branch: "main", Command: "echo hi", Targets: map[string]int{"claude": 1}},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "cannot specify both command and targets",
		},
		{
			name:       "resume with command",
			body:       SpawnRequest{Repo: "https://github.com/foo/bar", Branch: "main", Command: "echo hi", Resume: true},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "cannot use command mode with resume",
		},
		{
			name:       "resume with prompt",
			body:       SpawnRequest{Repo: "https://github.com/foo/bar", Branch: "main", Targets: map[string]int{"claude": 1}, Resume: true, Prompt: "hello"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "cannot use prompt with resume",
		},
		{
			name:       "quick launch with command",
			body:       SpawnRequest{WorkspaceID: "ws-1", QuickLaunchName: "preset", Command: "echo hi"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "cannot specify quick_launch_name with command or targets",
		},
		{
			name:       "quick launch with targets",
			body:       SpawnRequest{WorkspaceID: "ws-1", QuickLaunchName: "preset", Targets: map[string]int{"claude": 1}},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "cannot specify quick_launch_name with command or targets",
		},
		{
			name:       "quick launch without workspace",
			body:       SpawnRequest{QuickLaunchName: "preset"},
			wantCode:   http.StatusBadRequest,
			wantSubstr: "workspace_id is required for quick_launch_name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _, _ := newTestServer(t)
			rr := postSpawnJSON(t, server.handleSpawnPost, tt.body)
			if rr.Code != tt.wantCode {
				t.Errorf("got status %d, want %d; body: %s", rr.Code, tt.wantCode, rr.Body.String())
			}
			if tt.wantSubstr != "" {
				body := rr.Body.String()
				if !bytes.Contains([]byte(body), []byte(tt.wantSubstr)) {
					t.Errorf("response body %q should contain %q", body, tt.wantSubstr)
				}
			}
		})
	}
}

func TestHandleSpawnPost_InvalidJSON(t *testing.T) {
	server, _, _ := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/spawn", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleSpawnPost(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleSessions_EmptyState(t *testing.T) {
	server, _, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rr := httptest.NewRecorder()
	server.handleSessions(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusOK)
	}

	// Response should be valid JSON
	var result interface{}
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
}

func TestParseNudgeSummary_Empty(t *testing.T) {
	state, summary := parseNudgeSummary("")
	if state != "" || summary != "" {
		t.Errorf("parseNudgeSummary(\"\") = (%q, %q), want (\"\", \"\")", state, summary)
	}
}

func TestParseNudgeSummary_Whitespace(t *testing.T) {
	state, summary := parseNudgeSummary("   ")
	if state != "" || summary != "" {
		t.Errorf("parseNudgeSummary(\"   \") = (%q, %q), want (\"\", \"\")", state, summary)
	}
}

func TestHandleSpawnPost_GitURLRegistersRepo(t *testing.T) {
	server, cfg, _ := newTestServer(t)

	if len(cfg.Repos) != 0 {
		t.Fatalf("expected 0 repos, got %d", len(cfg.Repos))
	}

	body := SpawnRequest{
		Repo:    "https://github.com/anthropics/claude-code.git",
		Branch:  "main",
		Targets: map[string]int{"command": 1},
		Prompt:  "hello",
	}
	postSpawnJSON(t, server.handleSpawnPost, body)

	_, found := cfg.FindRepoByURL("https://github.com/anthropics/claude-code.git")
	if !found {
		t.Error("expected git URL to be registered in config after spawn request")
	}
}

func TestHandleSpawnPost_GitURLExistingRepoSkipsRegistration(t *testing.T) {
	server, cfg, _ := newTestServer(t)

	cfg.Repos = append(cfg.Repos, config.Repo{
		Name:     "claude-code",
		URL:      "https://github.com/anthropics/claude-code.git",
		BarePath: "claude-code.git",
	})

	body := SpawnRequest{
		Repo:    "https://github.com/anthropics/claude-code.git",
		Branch:  "main",
		Targets: map[string]int{"command": 1},
		Prompt:  "hello",
	}
	postSpawnJSON(t, server.handleSpawnPost, body)

	count := 0
	for _, r := range cfg.Repos {
		if r.URL == "https://github.com/anthropics/claude-code.git" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 repo entry, got %d", count)
	}
}

func TestHandleSpawnPost_GitURLGeneratesCorrectName(t *testing.T) {
	server, cfg, _ := newTestServer(t)

	body := SpawnRequest{
		Repo:    "https://github.com/anthropics/claude-code.git",
		Branch:  "main",
		Targets: map[string]int{"command": 1},
		Prompt:  "hello",
	}
	postSpawnJSON(t, server.handleSpawnPost, body)

	repo, found := cfg.FindRepoByURL("https://github.com/anthropics/claude-code.git")
	if !found {
		t.Fatal("repo not registered")
	}
	if repo.Name != "claude-code" {
		t.Errorf("repo name = %q, want %q", repo.Name, "claude-code")
	}
	if repo.BarePath != "claude-code.git" {
		t.Errorf("repo bare path = %q, want %q", repo.BarePath, "claude-code.git")
	}
}

// TestSpawnRequest_RemoteHostID_Deserialization verifies Bug 3:
// The SpawnRequest struct must have the remote_host_id JSON tag so it is
// deserialized from the request body. Previously, the field was missing and
// spawning on an existing host created a NEW connection instead of reusing.
func TestSpawnRequest_RemoteHostID_Deserialization(t *testing.T) {
	payload := `{
		"remote_flavor_id": "flavor-od",
		"remote_host_id": "host-abc123",
		"targets": {"claude": 1},
		"prompt": "hello"
	}`

	var req SpawnRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		t.Fatalf("failed to unmarshal SpawnRequest: %v", err)
	}

	if req.RemoteHostID != "host-abc123" {
		t.Errorf("RemoteHostID = %q, want %q", req.RemoteHostID, "host-abc123")
	}
	if req.RemoteFlavorID != "flavor-od" {
		t.Errorf("RemoteFlavorID = %q, want %q", req.RemoteFlavorID, "flavor-od")
	}
}

// TestHandleSpawnPost_RemoteHostID_PassedToSpawnRemote verifies that when
// remote_host_id is provided in the request body, it flows through to the
// SpawnRemote call (via the local remoteHostID variable).
func TestHandleSpawnPost_RemoteHostID_PassedToSpawnRemote(t *testing.T) {
	server, _, st := newTestServer(t)

	// Register a remote host in state so the auto-detect path finds its flavor
	st.AddRemoteHost(state.RemoteHost{
		ID:       "host-existing",
		FlavorID: "flavor-od",
		Status:   state.RemoteHostStatusConnected,
		Hostname: "dev001.example.com",
	})

	// Send spawn request with remote_host_id
	body := SpawnRequest{
		RemoteFlavorID: "flavor-od",
		RemoteHostID:   "host-existing",
		Targets:        map[string]int{"claude": 1},
		Prompt:         "hello",
	}
	rr := postSpawnJSON(t, server.handleSpawnPost, body)

	// We expect the handler to try SpawnRemote and fail (no remote manager set),
	// but the important thing is it got past validation - meaning remote_host_id
	// was properly deserialized and the request was accepted as a remote spawn.
	// Without the fix, remote_host_id would be empty and the spawn would either
	// fail differently or create a new connection.
	if rr.Code == http.StatusBadRequest {
		t.Errorf("spawn with remote_host_id should not return 400; body: %s", rr.Body.String())
	}
}

func TestResolveQuickLaunchFromPresets_PersonaID(t *testing.T) {
	server, _, _ := newTestServer(t)
	prompt := "review the code"
	presets := []contracts.QuickLaunch{
		{Name: "review", Target: "claude", Prompt: &prompt, PersonaID: "reviewer"},
		{Name: "build", Command: "make build", PersonaID: "builder"},
	}

	// Agent preset carries persona_id
	resolved := server.resolveQuickLaunchFromPresets(presets, "review")
	if resolved == nil {
		t.Fatal("expected resolved quick launch for 'review'")
	}
	if resolved.PersonaID != "reviewer" {
		t.Errorf("got persona_id %q, want %q", resolved.PersonaID, "reviewer")
	}

	// Command preset carries persona_id (even though it's unused, the data should flow through)
	resolved = server.resolveQuickLaunchFromPresets(presets, "build")
	if resolved == nil {
		t.Fatal("expected resolved quick launch for 'build'")
	}
	if resolved.PersonaID != "builder" {
		t.Errorf("got persona_id %q, want %q", resolved.PersonaID, "builder")
	}
}
