package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/tmux"
)

// newFenceAnalyzeHandler builds a minimal SpawnHandlers over a fresh state
// seeded with one fenced and one non-fenced session sharing a workspace.
func newFenceAnalyzeHandler(t *testing.T, cfg *config.Config) *SpawnHandlers {
	t.Helper()
	st := state.New(filepath.Join(t.TempDir(), "state.json"), nil)
	wsDir := t.TempDir()
	if err := st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "git@github.com:u/r.git", Branch: "main", Path: wsDir}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	if err := st.AddSession(state.Session{ID: "fenced-1", WorkspaceID: "ws-1", Fence: true, Target: "command", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	if err := st.AddSession(state.Session{ID: "plain-1", WorkspaceID: "ws-1", Fence: false, Target: "command", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	return &SpawnHandlers{logger: discardLogger(), state: st, config: cfg}
}

func fenceAnalyzeEnabledConfig() *config.Config {
	enabled := true
	return &config.Config{ConfigData: config.ConfigData{
		FenceMode:    config.FenceModeOptionalOn,
		FenceAnalyze: &config.FenceAnalyzeConfig{Enabled: &enabled, Target: "command"},
	}}
}

func postFenceAnalyze(t *testing.T, h *SpawnHandlers, sessionID string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/api/sessions/{sessionID}/fence-analyze", h.handleFenceAnalyze)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/fence-analyze", strings.NewReader("{}"))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestFenceAnalyze_UnknownSession404(t *testing.T) {
	h := newFenceAnalyzeHandler(t, fenceAnalyzeEnabledConfig())
	rr := postFenceAnalyze(t, h, "nope")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown session: status = %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}

func TestFenceAnalyze_NotFenced404(t *testing.T) {
	h := newFenceAnalyzeHandler(t, fenceAnalyzeEnabledConfig())
	rr := postFenceAnalyze(t, h, "plain-1") // Fence: false
	if rr.Code != http.StatusNotFound {
		t.Fatalf("non-fenced session: status = %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}

func TestFenceAnalyze_Disabled400(t *testing.T) {
	cfg := &config.Config{ConfigData: config.ConfigData{
		FenceAnalyze: &config.FenceAnalyzeConfig{Target: "command"}, // Enabled nil = disabled
	}}
	h := newFenceAnalyzeHandler(t, cfg)
	rr := postFenceAnalyze(t, h, "fenced-1")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("disabled: status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("disabled")) {
		t.Errorf("body = %q, want disabled message", rr.Body.String())
	}
}

func TestFenceAnalyze_EmptyTarget400(t *testing.T) {
	enabled := true
	cfg := &config.Config{ConfigData: config.ConfigData{
		FenceAnalyze: &config.FenceAnalyzeConfig{Enabled: &enabled}, // Target empty
	}}
	h := newFenceAnalyzeHandler(t, cfg)
	rr := postFenceAnalyze(t, h, "fenced-1")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("empty target: status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("target")) {
		t.Errorf("body = %q, want target message", rr.Body.String())
	}
}

func TestCaptureFenceAnalysisTerminal_UnavailableRecordsDiagnostic(t *testing.T) {
	st := state.New(filepath.Join(t.TempDir(), "state.json"), nil)
	if err := st.AddSession(state.Session{ID: "gone", TmuxSession: "gone"}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}
	manager := session.New(&config.Config{}, st, "", nil, nil, discardLogger())
	h := &SpawnHandlers{session: manager, logger: discardLogger()}
	path := filepath.Join(t.TempDir(), fenceAnalysisTerminalFilename)

	if err := h.captureFenceAnalysisTerminal(context.Background(), "gone", path); err != nil {
		t.Fatalf("captureFenceAnalysisTerminal: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capture diagnostic: %v", err)
	}
	if !strings.HasPrefix(string(data), "Terminal capture unavailable:") {
		t.Errorf("capture diagnostic = %q", data)
	}
}

// TestFenceAnalyze_Success spawns a fenced analysis agent end-to-end and
// confirms the new session is fenced. Requires tmux (skips otherwise), mirroring
// TestSpawn_SaplingLabelLandsOnWorkspaceViaHandler. schmuxdir is redirected to a
// temp dir so fence.Wrap writes nothing under the real home.
func TestFenceAnalyze_Success(t *testing.T) {
	// Spawn reads the new pane's PID, so the fenced command has to still be
	// running when it does; if the pane has already exited, tmux has torn the
	// session down and the read comes back empty. The analysis target below is
	// `claude`, which CI does not install — and installing it is a large
	// download for the one thing needed here, a process that stays up. Stand
	// one in instead. Fence itself stays real (CI installs it via
	// scripts/install-fence.sh); only the agent it wraps is substituted. Set
	// before the first tmux call, since tmux seeds a new session's environment
	// from the client that created it.
	stubDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubDir, "claude"), []byte("#!/bin/sh\nexec sleep 600\n"), 0o755); err != nil {
		t.Fatalf("write claude stand-in: %v", err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	socketName := fmt.Sprintf("schmux-test-fa-%d", time.Now().UnixNano())
	tmuxServer := tmux.NewTmuxServer("tmux", socketName, nil)
	if err := tmuxServer.Check(); err != nil {
		t.Skip("tmux not available")
	}
	t.Cleanup(func() {
		ctx := context.Background()
		if names, err := tmuxServer.ListSessions(ctx); err == nil {
			for _, n := range names {
				_ = tmuxServer.KillSession(ctx, n)
			}
		}
	})

	server, cfg, st := newTestServerWithTmux(t, tmuxServer)
	spawnH := newTestSpawnHandlers(server)
	spawnH.dependencyReport = func() detect.DependencyReport {
		return detect.DependencyReport{Statuses: []detect.DependencyStatus{
			{Dependency: detect.Dependency{ID: "fence"}, Detected: true, Command: "fence"},
		}}
	}
	enabled := true
	cfg.FenceAnalyze = &config.FenceAnalyzeConfig{Enabled: &enabled, Target: "claude"}
	cfg.FenceMode = config.FenceModeOptionalOn

	// Keep fence.Wrap's writes out of the real home dir.
	prevDir := schmuxdir.Get()
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set(prevDir) })

	// Seed a fenced target session whose workspace the analyzer will reuse.
	targetWS := t.TempDir()
	if err := st.AddWorkspace(state.Workspace{ID: "ws-tgt", Repo: "git@github.com:u/r.git", Branch: "main", Path: targetWS}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	const sourceTmuxSession = "fence-analysis-source"
	if err := tmuxServer.CreateSession(
		context.Background(),
		sourceTmuxSession,
		targetWS,
		"printf 'source-session-marker: build failed under fence\\n'; sleep 600",
	); err != nil {
		t.Fatalf("create source tmux session: %v", err)
	}
	if err := st.AddSession(state.Session{
		ID:          "sess-tgt",
		WorkspaceID: "ws-tgt",
		Fence:       true,
		Target:      "command",
		TmuxSession: sourceTmuxSession,
		TmuxSocket:  tmuxServer.SocketName(),
		CreatedAt:   time.Now(),
	}); err != nil {
		t.Fatalf("AddSession: %v", err)
	}

	rr := postFenceAnalyze(t, spawnH, "sess-tgt")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}

	var result SessionResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.SessionID == "" {
		t.Fatal("empty session_id in response")
	}

	created, ok := st.GetSession(result.SessionID)
	if !ok {
		t.Fatalf("spawned session %s not in state", result.SessionID)
	}
	if !created.Fence {
		t.Errorf("spawned session Fence = false, want true (analyzer must run fenced)")
	}
	if result.Nickname != fenceAnalyzeNickname {
		t.Errorf("nickname = %q, want %q", result.Nickname, fenceAnalyzeNickname)
	}

	// The endpoint (not the fenced spawn) generates the capabilities doc, into
	// the workspace fence dir the analyzer's read grant covers.
	capDoc := filepath.Join(schmuxdir.FenceWorkspaceDir("ws-tgt"), "fence-capabilities.md")
	if _, err := os.Stat(capDoc); err != nil {
		t.Errorf("capabilities doc not written by endpoint: %v", err)
	}

	// Analysis input is a real source-session capture, not a prompt assertion:
	// this proves the endpoint supplies terminal evidence that monitor.log does
	// not contain.
	terminalCapture := filepath.Join(
		schmuxdir.FenceLaunchDir("ws-tgt", "sess-tgt"),
		fenceAnalysisTerminalFilename,
	)
	captured, err := os.ReadFile(terminalCapture)
	if err != nil {
		t.Fatalf("read terminal capture: %v", err)
	}
	if !bytes.Contains(captured, []byte("source-session-marker: build failed under fence")) {
		t.Errorf("terminal capture omitted source session output: %q", captured)
	}
}
