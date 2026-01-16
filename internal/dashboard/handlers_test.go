package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergek/schmux/internal/config"
	"github.com/sergek/schmux/internal/session"
	"github.com/sergek/schmux/internal/state"
	"github.com/sergek/schmux/internal/workspace"
)

func TestHandleHasNudgenik(t *testing.T) {
	// SPIKE: Test that hasNudgenik always returns true (CLI tools available)
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)
	server := NewServer(cfg, st, statePath, sm, wm)

	// Create a GET request
	req, _ := http.NewRequest("GET", "/api/hasNudgenik", nil)
	rr := httptest.NewRecorder()

	server.handleHasNudgenik(rr, req)

	// Check response
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var resp map[string]bool
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp["available"] {
		t.Errorf("expected available=true, got %v", resp["available"])
	}
}

func TestHandleAskNudgenik(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)
	server := NewServer(cfg, st, statePath, sm, wm)

	// Add a test session
	testSession := state.Session{
		ID:          "test-session-123",
		WorkspaceID: "test-workspace",
		Target:       "test",
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
			// For the valid session case, accept 200 (success), 400 (no response extracted), or 500 (tmux/CLI failed)
			// since tmux may not be available in test environments
			if tt.name == "valid session id" {
				if rr.Code != http.StatusOK && rr.Code != http.StatusBadRequest && rr.Code != http.StatusInternalServerError {
					t.Errorf("expected status 200, 400, or 500, got %d", rr.Code)
				}
			} else {
				if rr.Code != tt.wantStatus {
					t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
				}
			}
		})
	}
}

func TestHandleBuiltinQuickLaunch(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)
	sm := session.New(cfg, st, statePath, wm)
	server := NewServer(cfg, st, statePath, sm, wm)

	t.Run("GET request returns presets", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/builtin-quick-launch", nil)
		rr := httptest.NewRecorder()

		server.handleBuiltinQuickLaunch(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}

		var presets []BuiltinQuickLaunch
		if err := json.NewDecoder(rr.Body).Decode(&presets); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		// Check that we got some presets (the file has 4 entries)
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

		var presets []BuiltinQuickLaunch
		if err := json.NewDecoder(rr.Body).Decode(&presets); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		// Check for known preset names from builtin_quick_launch.json
		presetNames := make(map[string]bool)
		for _, preset := range presets {
			presetNames[preset.Name] = true
		}

		expectedNames := []string{
			"code review - local",
			"code review - branch",
			"git commit",
			"merge in main",
		}

		for _, expected := range expectedNames {
			if !presetNames[expected] {
				t.Errorf("expected to find preset %q, but it was not found", expected)
			}
		}
	})
}
