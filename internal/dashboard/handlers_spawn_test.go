package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
