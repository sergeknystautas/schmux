package dashboard

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/state"
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

// quickLaunchWorkspace registers a workspace whose .schmux/config.json
// holds the given presets, and returns its ID. The per-repo path is used
// because it round-trips the contract struct without touching global
// config on disk.
func quickLaunchWorkspace(t *testing.T, server *Server, presetsJSON string) string {
	t.Helper()
	ws := state.Workspace{
		ID:     "ws-ql",
		Repo:   "repo-url",
		Branch: "main",
		Path:   filepath.Join(server.config.WorkspacePath, "ws-ql"),
	}
	if err := os.MkdirAll(filepath.Join(ws.Path, ".schmux"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"quick_launch":` + presetsJSON + `}`
	if err := os.WriteFile(filepath.Join(ws.Path, ".schmux", "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := server.state.AddWorkspace(ws); err != nil {
		t.Fatal(err)
	}
	// The WorkspaceManager interface doesn't expose RefreshWorkspaceConfig,
	// but the dashboard always uses the concrete *Manager, so this assertion
	// holds for every server produced by newTestServer. The model manager
	// must also be wired so the per-repo validator can resolve target names.
	mgr, ok := server.workspace.(interface {
		RefreshWorkspaceConfig(state.Workspace)
		SetModelManager(*models.Manager)
	})
	if !ok {
		t.Fatal("workspace manager does not implement the required methods")
	}
	mgr.SetModelManager(server.models)
	mgr.RefreshWorkspaceConfig(ws)
	return ws.ID
}

// A preset with fence:true must reach the fence gate even though the
// caller sent no fence field. With fence unavailable the gate hard-fails,
// which is the observable proof the merge happened.
func TestQuickLaunchPresetFenceReachesGate(t *testing.T) {
	server, _, _ := newTestServer(t)
	spawnH := newTestSpawnHandlers(server)
	spawnH.dependencyReport = func() detect.DependencyReport { return detect.DependencyReport{} } // fence missing
	wsID := quickLaunchWorkspace(t, server, `[{"name":"fenced build","command":"make build","fence":true}]`)

	rr := postFence(t, spawnH, map[string]any{
		"workspace_id":      wsID,
		"quick_launch_name": "fenced build",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("fence not available")) {
		t.Errorf("body = %q, want the fence-unavailable message", rr.Body.String())
	}
}

// A preset with kind:"chat" must reach the chat gate. chat_sessions is
// off by default, so the gate hard-fails with its own message.
func TestQuickLaunchPresetChatReachesGate(t *testing.T) {
	server, _, _ := newTestServer(t)
	spawnH := newTestSpawnHandlers(server)
	wsID := quickLaunchWorkspace(t, server, `[{"name":"chat review","target":"claude","prompt":"review it","kind":"chat"}]`)

	rr := postFence(t, spawnH, map[string]any{
		"workspace_id":      wsID,
		"quick_launch_name": "chat review",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("chat sessions are disabled")) {
		t.Errorf("body = %q, want the chat-disabled message", rr.Body.String())
	}
}

// Fence merges as OR: a caller asking for fence still gets it when the
// preset does not, so the /quick path's wizard checkbox keeps working.
func TestQuickLaunchCallerFenceStillApplies(t *testing.T) {
	server, _, _ := newTestServer(t)
	spawnH := newTestSpawnHandlers(server)
	spawnH.dependencyReport = func() detect.DependencyReport { return detect.DependencyReport{} } // fence missing
	wsID := quickLaunchWorkspace(t, server, `[{"name":"plain build","command":"make build"}]`)

	rr := postFence(t, spawnH, map[string]any{
		"workspace_id":      wsID,
		"quick_launch_name": "plain build",
		"fence":             true,
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rr.Code, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("fence not available")) {
		t.Errorf("body = %q, want the fence-unavailable message", rr.Body.String())
	}
}
