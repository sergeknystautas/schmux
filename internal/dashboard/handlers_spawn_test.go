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
			spawnH := newTestSpawnHandlers(server)
			rr := postSpawnJSON(t, spawnH.handleSpawnPost, tt.body)
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
	spawnH := newTestSpawnHandlers(server)
	req := httptest.NewRequest("POST", "/api/spawn", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	spawnH.handleSpawnPost(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleSessions_EmptyState(t *testing.T) {
	server, _, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rr := httptest.NewRecorder()
	server.sessionHandlers.handleSessions(rr, req)

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
	spawnH := newTestSpawnHandlers(server)

	if len(cfg.Repos) != 0 {
		t.Fatalf("expected 0 repos, got %d", len(cfg.Repos))
	}

	body := SpawnRequest{
		Repo:    "https://github.com/anthropics/claude-code.git",
		Branch:  "main",
		Targets: map[string]int{"command": 1},
		Prompt:  "hello",
	}
	postSpawnJSON(t, spawnH.handleSpawnPost, body)

	_, found := cfg.FindRepoByURL("https://github.com/anthropics/claude-code.git")
	if !found {
		t.Error("expected git URL to be registered in config after spawn request")
	}
}

func TestHandleSpawnPost_GitURLExistingRepoSkipsRegistration(t *testing.T) {
	server, cfg, _ := newTestServer(t)
	spawnH := newTestSpawnHandlers(server)

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
	postSpawnJSON(t, spawnH.handleSpawnPost, body)

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
	spawnH := newTestSpawnHandlers(server)

	body := SpawnRequest{
		Repo:    "https://github.com/anthropics/claude-code.git",
		Branch:  "main",
		Targets: map[string]int{"command": 1},
		Prompt:  "hello",
	}
	postSpawnJSON(t, spawnH.handleSpawnPost, body)

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
		"remote_profile_id": "profile-od",
		"remote_flavor": "od",
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
	if req.RemoteProfileID != "profile-od" {
		t.Errorf("RemoteProfileID = %q, want %q", req.RemoteProfileID, "profile-od")
	}
}

// TestHandleSpawnPost_RemoteHostID_PassedToSpawnRemote verifies that when
// remote_host_id is provided in the request body, it flows through to the
// SpawnRemote call (via the local remoteHostID variable).
func TestHandleSpawnPost_RemoteHostID_PassedToSpawnRemote(t *testing.T) {
	server, _, st := newTestServer(t)
	spawnH := newTestSpawnHandlers(server)

	// Register a remote host in state so the auto-detect path finds its flavor
	st.AddRemoteHost(state.RemoteHost{
		ID:        "host-existing",
		ProfileID: "profile-od",
		Flavor:    "od",
		Status:    state.RemoteHostStatusConnected,
		Hostname:  "dev001.example.com",
	})

	// Send spawn request with remote_host_id
	body := SpawnRequest{
		RemoteProfileID: "profile-od",
		RemoteFlavor:    "od",
		RemoteHostID:    "host-existing",
		Targets:         map[string]int{"claude": 1},
		Prompt:          "hello",
	}
	rr := postSpawnJSON(t, spawnH.handleSpawnPost, body)

	// We expect the handler to try SpawnRemote and fail (no remote manager set),
	// but the important thing is it got past validation - meaning remote_host_id
	// was properly deserialized and the request was accepted as a remote spawn.
	// Without the fix, remote_host_id would be empty and the spawn would either
	// fail differently or create a new connection.
	if rr.Code == http.StatusBadRequest {
		t.Errorf("spawn with remote_host_id should not return 400; body: %s", rr.Body.String())
	}
}

func postCheckBranchConflictJSON(t *testing.T, handler http.HandlerFunc, repo, branch string) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(map[string]string{"repo": repo, "branch": branch})
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}
	req := httptest.NewRequest("POST", "/api/check-branch-conflict", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

// TestHandleCheckBranchConflict_RecyclableWorkspaceNoConflict verifies that a
// recyclable workspace does not trigger a branch conflict. The branch is
// technically still checked out in the worktree on disk, but recyclable
// workspaces are available for reuse so they should not block new spawns.
func TestHandleCheckBranchConflict_RecyclableWorkspaceNoConflict(t *testing.T) {
	server, cfg, st := newTestServer(t)
	cfg.SourceCodeManagement = config.SourceCodeManagementGitWorktree
	spawnH := newTestSpawnHandlers(server)

	repoURL := "https://github.com/example/repo.git"

	// Add a recyclable workspace holding the branch
	st.AddWorkspace(state.Workspace{
		ID:     "repo-001",
		Repo:   repoURL,
		Branch: "feature-x",
		Path:   t.TempDir(),
		Status: state.WorkspaceStatusRecyclable,
	})

	rr := postCheckBranchConflictJSON(t, spawnH.handleCheckBranchConflict, repoURL, "feature-x")
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var result struct {
		Conflict    bool   `json:"conflict"`
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Conflict {
		t.Errorf("recyclable workspace should not cause branch conflict, but got conflict with workspace %s", result.WorkspaceID)
	}
}

// TestHandleCheckBranchConflict_RunningWorkspaceConflicts verifies that a
// running workspace correctly triggers a branch conflict.
func TestHandleCheckBranchConflict_RunningWorkspaceConflicts(t *testing.T) {
	server, cfg, st := newTestServer(t)
	cfg.SourceCodeManagement = config.SourceCodeManagementGitWorktree
	spawnH := newTestSpawnHandlers(server)

	repoURL := "https://github.com/example/repo.git"

	st.AddWorkspace(state.Workspace{
		ID:     "repo-001",
		Repo:   repoURL,
		Branch: "feature-x",
		Path:   t.TempDir(),
		Status: state.WorkspaceStatusRunning,
	})

	rr := postCheckBranchConflictJSON(t, spawnH.handleCheckBranchConflict, repoURL, "feature-x")
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var result struct {
		Conflict    bool   `json:"conflict"`
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !result.Conflict {
		t.Error("running workspace should cause branch conflict")
	}
	if result.WorkspaceID != "repo-001" {
		t.Errorf("conflict workspace_id = %q, want %q", result.WorkspaceID, "repo-001")
	}
}

// TestHandleSpawnPost_RecyclableBranchNotBlocked verifies that the server-side
// branch conflict guard in handleSpawnPost does not reject a spawn request when
// the only workspace holding that branch is recyclable.
func TestHandleSpawnPost_RecyclableBranchNotBlocked(t *testing.T) {
	server, cfg, st := newTestServer(t)
	cfg.SourceCodeManagement = config.SourceCodeManagementGitWorktree
	cfg.Repos = append(cfg.Repos, config.Repo{
		Name:     "repo",
		URL:      "https://github.com/example/repo.git",
		BarePath: "repo.git",
	})
	spawnH := newTestSpawnHandlers(server)

	// Add a recyclable workspace holding the branch
	st.AddWorkspace(state.Workspace{
		ID:     "repo-001",
		Repo:   "https://github.com/example/repo.git",
		Branch: "feature-x",
		Path:   t.TempDir(),
		Status: state.WorkspaceStatusRecyclable,
	})

	body := SpawnRequest{
		Repo:    "https://github.com/example/repo.git",
		Branch:  "feature-x",
		Targets: map[string]int{"command": 1},
		Prompt:  "hello",
	}
	rr := postSpawnJSON(t, spawnH.handleSpawnPost, body)

	// The request should NOT be rejected with 409 Conflict.
	// It will fail later (workspace manager not fully wired in unit tests),
	// but the branch conflict guard must not be the reason.
	if rr.Code == http.StatusConflict {
		t.Errorf("spawn should not be blocked by recyclable workspace; body: %s", rr.Body.String())
	}
}

// TestHandleSpawnPost_RunningBranchBlocked verifies that the server-side
// branch conflict guard correctly rejects a spawn when a running workspace
// holds the branch.
func TestHandleSpawnPost_RunningBranchBlocked(t *testing.T) {
	server, cfg, st := newTestServer(t)
	cfg.SourceCodeManagement = config.SourceCodeManagementGitWorktree
	cfg.Repos = append(cfg.Repos, config.Repo{
		Name:     "repo",
		URL:      "https://github.com/example/repo.git",
		BarePath: "repo.git",
	})
	spawnH := newTestSpawnHandlers(server)

	st.AddWorkspace(state.Workspace{
		ID:     "repo-001",
		Repo:   "https://github.com/example/repo.git",
		Branch: "feature-x",
		Path:   t.TempDir(),
		Status: state.WorkspaceStatusRunning,
	})

	body := SpawnRequest{
		Repo:    "https://github.com/example/repo.git",
		Branch:  "feature-x",
		Targets: map[string]int{"command": 1},
		Prompt:  "hello",
	}
	rr := postSpawnJSON(t, spawnH.handleSpawnPost, body)

	if rr.Code != http.StatusConflict {
		t.Errorf("spawn should be blocked by running workspace; got status %d, want %d; body: %s",
			rr.Code, http.StatusConflict, rr.Body.String())
	}
}

func TestResolveQuickLaunchFromPresets_PersonaID(t *testing.T) {
	server, _, _ := newTestServer(t)
	spawnH := newTestSpawnHandlers(server)
	prompt := "review the code"
	presets := []contracts.QuickLaunch{
		{Name: "review", Target: "claude", Prompt: &prompt, PersonaID: "reviewer"},
		{Name: "build", Command: "make build", PersonaID: "builder"},
	}

	// Agent preset carries persona_id
	resolved := spawnH.resolveQuickLaunchFromPresets(presets, "review")
	if resolved == nil {
		t.Fatal("expected resolved quick launch for 'review'")
	}
	if resolved.PersonaID != "reviewer" {
		t.Errorf("got persona_id %q, want %q", resolved.PersonaID, "reviewer")
	}

	// Command preset carries persona_id (even though it's unused, the data should flow through)
	resolved = spawnH.resolveQuickLaunchFromPresets(presets, "build")
	if resolved == nil {
		t.Fatal("expected resolved quick launch for 'build'")
	}
	if resolved.PersonaID != "builder" {
		t.Errorf("got persona_id %q, want %q", resolved.PersonaID, "builder")
	}
}
