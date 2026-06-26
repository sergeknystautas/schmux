package dashboard

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
)

func discardLogger() *log.Logger { return log.NewWithOptions(io.Discard, log.Options{}) }

func postFence(t *testing.T, h *SpawnHandlers, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewReader(raw))
	rr := httptest.NewRecorder()
	h.handleSpawnPost(rr, req)
	return rr
}

func TestFenceOnRemoteHardFails(t *testing.T) {
	h := &SpawnHandlers{
		logger: discardLogger(),
		dependencyReport: func() detect.DependencyReport {
			return detect.DependencyReport{Statuses: []detect.DependencyStatus{
				{Dependency: detect.Dependency{ID: "fence"}, Detected: true, Command: "fence"},
			}}
		},
	}
	rr := postFence(t, h, map[string]any{
		"fence":             true,
		"remote_profile_id": "prof-1",
		"targets":           map[string]int{"claude": 1},
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("not supported for remote")) {
		t.Errorf("body = %q, want remote hard-fail message", rr.Body.String())
	}
}

func TestFenceOnUnavailableHardFails(t *testing.T) {
	h := &SpawnHandlers{
		logger:           discardLogger(),
		dependencyReport: func() detect.DependencyReport { return detect.DependencyReport{} }, // fence missing
	}
	rr := postFence(t, h, map[string]any{
		"fence":   true,
		"repo":    "git@github.com:u/r.git",
		"branch":  "main",
		"targets": map[string]int{"claude": 1},
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("fence not available")) {
		t.Errorf("body = %q, want unavailable hard-fail message", rr.Body.String())
	}
}

func TestFenceWhenDisabledHardFails(t *testing.T) {
	h := &SpawnHandlers{
		logger: discardLogger(),
		config: &config.Config{ConfigData: config.ConfigData{FenceMode: config.FenceModeDisabled}},
		dependencyReport: func() detect.DependencyReport {
			return detect.DependencyReport{Statuses: []detect.DependencyStatus{
				{Dependency: detect.Dependency{ID: "fence"}, Detected: true, Command: "fence"},
			}}
		},
	}
	rr := postFence(t, h, map[string]any{
		"fence":   true,
		"repo":    "git@github.com:u/r.git",
		"branch":  "main",
		"targets": map[string]int{"claude": 1},
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("fenced sessions are disabled")) {
		t.Errorf("body = %q, want disabled hard-fail message", rr.Body.String())
	}
}
