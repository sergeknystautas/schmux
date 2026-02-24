package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func TestHandleDebugTmuxLeak(t *testing.T) {
	t.Run("returns JSON response with tmux counts", func(t *testing.T) {
		server, _, _ := newTestServer(t)

		req := httptest.NewRequest(http.MethodGet, "/api/debug/tmux-leak", nil)
		rr := httptest.NewRecorder()

		server.handleDebugTmuxLeak(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", ct)
		}

		var resp map[string]any
		if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode JSON: %v", err)
		}

		// Check tmux_sessions structure
		tmuxSessions, ok := resp["tmux_sessions"].(map[string]any)
		if !ok {
			t.Fatal("tmux_sessions should be a map")
		}
		if _, ok := tmuxSessions["count"]; !ok {
			t.Error("tmux_sessions.count should exist")
		}

		// Check os_processes structure
		osProcesses, ok := resp["os_processes"].(map[string]any)
		if !ok {
			t.Fatal("os_processes should be a map")
		}
		if _, ok := osProcesses["attach_session_process_count"]; !ok {
			t.Error("os_processes.attach_session_process_count should exist")
		}
		if _, ok := osProcesses["tmux_process_count"]; !ok {
			t.Error("os_processes.tmux_process_count should exist")
		}
	})

	t.Run("rejects non-GET requests", func(t *testing.T) {
		server, _, _ := newTestServer(t)

		req := httptest.NewRequest(http.MethodPost, "/api/debug/tmux-leak", nil)
		rr := httptest.NewRecorder()

		server.handleDebugTmuxLeak(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", rr.Code)
		}
	})
}

func TestCollectTmuxSessionCount(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows - tmux not available")
	}

	t.Run("returns count when tmux is available", func(t *testing.T) {
		count, err := collectTmuxSessionCount()

		// If tmux is installed and server is running, we should get a count
		// If tmux is not installed or server is not running, we expect an error
		if err != nil {
			t.Logf("tmux not available or server not running (this is OK): %v", err)
			return
		}

		if count < 0 {
			t.Errorf("count should be non-negative, got %d", count)
		}
		t.Logf("tmux session count: %d", count)
	})
}

func TestCollectTmuxProcessCounts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows - ps command differs")
	}

	t.Run("returns process counts when ps is available", func(t *testing.T) {
		attachCount, tmuxCount, err := collectTmuxProcessCounts()

		if err != nil {
			t.Logf("ps command failed (this is OK in some environments): %v", err)
			return
		}

		if attachCount < 0 {
			t.Errorf("attachCount should be non-negative, got %d", attachCount)
		}
		if tmuxCount < 0 {
			t.Errorf("tmuxCount should be non-negative, got %d", tmuxCount)
		}

		// tmuxCount should be at least attachCount (since attach-session processes contain "tmux")
		if tmuxCount < attachCount {
			t.Errorf("tmuxCount (%d) should be >= attachCount (%d)", tmuxCount, attachCount)
		}

		t.Logf("tmux process counts: attach=%d, tmux=%d", attachCount, tmuxCount)
	})
}
