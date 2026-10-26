package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type cpFixture struct {
	h       *ClientPerformanceHandlers
	server  *Server
	st      *state.State
	spawned []SpawnRequest
}

func newCPFixture(t *testing.T) *cpFixture {
	t.Helper()
	server, cfg, st := newTestServer(t)
	cfg.Repos = []config.Repo{{Name: "schmux", URL: "https://example.com/schmux.git"}}
	cfg.ClientPerformance = &config.ClientPerformanceConfig{Enabled: boolPtr(true), Repo: "schmux", Target: "command"}
	cfg.ChatSessions = true
	f := &cpFixture{st: st, h: newClientPerformanceHandlers(server), server: server}
	f.h.broadcast = func() {}
	f.h.targetExists = func(string) bool { return true }
	f.h.hasChatMode = func(string) bool { return true }
	f.h.isRunning = func(context.Context, string) bool { return false }
	f.h.executeSpawn = func(req SpawnRequest) ([]SessionResult, int, error) {
		f.spawned = append(f.spawned, req)
		wsID := req.WorkspaceID
		if wsID == "" {
			wsID = "ws-new"
		}
		if _, ok := st.GetWorkspace(wsID); !ok {
			if err := st.AddWorkspace(state.Workspace{ID: wsID, Repo: req.Repo, Branch: req.Branch, Path: t.TempDir()}); err != nil {
				t.Fatal(err)
			}
		}
		if err := st.AddSession(state.Session{ID: "sess-new", WorkspaceID: wsID, Kind: state.SessionKindChat}); err != nil {
			t.Fatal(err)
		}
		return []SessionResult{{SessionID: "sess-new", WorkspaceID: wsID}}, http.StatusOK, nil
	}
	return f
}

func TestClientPerformance_DashboardSnapshotHasOptionalAssociationAndVisibleErrors(t *testing.T) {
	f := newCPFixture(t)
	f.server.devMode = true
	msg := f.server.sessionsMessage()
	if got := msg["client_performance"].(contracts.ClientPerformanceSessionResponse); got.WorkspaceID != "" || got.SessionID != "" || msg["client_performance_error"] != nil {
		t.Fatalf("fresh snapshot=%+v", msg)
	}
	code, launched, body := f.post(t, contracts.ClientPerformanceSessionRequest{})
	if code != http.StatusOK {
		t.Fatalf("launch status=%d body=%s", code, body)
	}
	msg = f.server.sessionsMessage()
	if got := msg["client_performance"].(contracts.ClientPerformanceSessionResponse); got != launched {
		t.Fatalf("snapshot association=%+v want=%+v", got, launched)
	}
	if err := os.WriteFile(filepath.Join(schmuxdir.Get(), "client-performance.json"), []byte("invalid JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	msg = f.server.sessionsMessage()
	if message, ok := msg["client_performance_error"].(string); !ok || message == "" {
		t.Fatalf("state read error hidden: %+v", msg)
	}
}
func (f *cpFixture) post(t *testing.T, req contracts.ClientPerformanceSessionRequest) (int, contracts.ClientPerformanceSessionResponse, string) {
	t.Helper()
	body, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/client-performance/session", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	f.h.handleEnsureSession(rr, r)
	var resp contracts.ClientPerformanceSessionResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	return rr.Code, resp, rr.Body.String()
}
func TestClientPerformance_ConfigChecks(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*config.Config, *cpFixture)
		want string
	}{
		{"disabled", func(c *config.Config, _ *cpFixture) { c.ClientPerformance.Enabled = nil }, "client performance recording is disabled"},
		{"repo unset", func(c *config.Config, _ *cpFixture) { c.ClientPerformance.Repo = "" }, "client_performance.repo is not set"},
		{"repo not in config", func(c *config.Config, _ *cpFixture) { c.ClientPerformance.Repo = "other" }, "client_performance.repo other is not a configured repo"},
		{"target unset", func(c *config.Config, _ *cpFixture) { c.ClientPerformance.Target = "" }, "client_performance.target is not set"},
		{"target not in config", func(_ *config.Config, f *cpFixture) { f.h.targetExists = func(string) bool { return false } }, "client_performance.target command is not a configured target"},
		{"chat sessions off", func(c *config.Config, _ *cpFixture) { c.ChatSessions = false }, "chat sessions are disabled (chat_sessions)"},
		{"target without chat mode", func(_ *config.Config, f *cpFixture) { f.h.hasChatMode = func(string) bool { return false } }, "target command has no chat mode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCPFixture(t)
			tc.mut(f.h.config, f)
			code, _, body := f.post(t, contracts.ClientPerformanceSessionRequest{})
			if code != http.StatusBadRequest || !bytes.Contains([]byte(body), []byte(tc.want)) {
				t.Fatalf("code=%d body=%s want 400 containing %q", code, body, tc.want)
			}
			if len(f.spawned) != 0 {
				t.Fatalf("spawned=%d, want none", len(f.spawned))
			}
		})
	}
}

func TestClientPerformance_LaunchesDedicatedWorkspaceThroughSpawn(t *testing.T) {
	f := newCPFixture(t)
	code, resp, body := f.post(t, contracts.ClientPerformanceSessionRequest{})
	if code != http.StatusOK || resp.WorkspaceID != "ws-new" || resp.SessionID != "sess-new" {
		t.Fatalf("code=%d response=%+v body=%s", code, resp, body)
	}
	if len(f.spawned) != 1 {
		t.Fatalf("spawns=%+v", f.spawned)
	}
	req := f.spawned[0]
	if req.WorkspaceID != "" || req.Repo != "https://example.com/schmux.git" || !strings.HasPrefix(req.Branch, clientPerformanceBranch) || req.Kind != state.SessionKindChat || req.Targets["command"] != 1 {
		t.Fatalf("spawn=%+v", req)
	}
	if !strings.Contains(req.Prompt, "Recording format:") || strings.Contains(req.Prompt, "Read that file") {
		t.Fatalf("prompt requires unavailable documentation: %s", req.Prompt)
	}
}
func TestClientPerformance_TracksWorkspaceAndRunningAgent(t *testing.T) {
	f := newCPFixture(t)
	_ = f.st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "https://example.com/schmux.git", Branch: clientPerformanceBranch + "test", Path: t.TempDir()})
	_ = f.st.AddSession(state.Session{ID: "agent", WorkspaceID: "ws-1", Kind: state.SessionKindChat})
	f.h.isRunning = func(context.Context, string) bool { return true }
	code, resp, _ := f.post(t, contracts.ClientPerformanceSessionRequest{WorkspaceID: "ws-1"})
	if code != 200 || resp.SessionID != "agent" || len(f.spawned) != 0 {
		t.Fatalf("code=%d response=%+v spawned=%+v", code, resp, f.spawned)
	}
	f.h.isRunning = func(context.Context, string) bool { return false }
	_, _, _ = f.post(t, contracts.ClientPerformanceSessionRequest{WorkspaceID: "ws-1"})
	if len(f.spawned) != 1 || f.spawned[0].WorkspaceID != "ws-1" {
		t.Fatalf("spawn=%+v", f.spawned)
	}
}
func TestClientPerformance_RefusesUnrelatedWorkspace(t *testing.T) {
	f := newCPFixture(t)
	_ = f.st.AddWorkspace(state.Workspace{ID: "ws-other", Repo: "https://example.com/schmux.git", Branch: "main", Path: t.TempDir()})
	code, _, body := f.post(t, contracts.ClientPerformanceSessionRequest{WorkspaceID: "ws-other"})
	if code != 409 || len(f.spawned) != 0 {
		t.Fatalf("code=%d body=%s spawned=%+v", code, body, f.spawned)
	}
}
func TestClientPerformance_RecordingDeliveredInsideWorkspace(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "delivered", true: "send failure"}[fail], func(t *testing.T) {
			f := newCPFixture(t)
			physical, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(physical, alias); err != nil {
				t.Fatal(err)
			}
			_ = f.st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "https://example.com/schmux.git", Branch: clientPerformanceBranch + "test", Path: alias})
			_ = f.st.AddSession(state.Session{ID: "agent", WorkspaceID: "ws-1", Kind: state.SessionKindChat})
			if err := f.h.writeState(contracts.ClientPerformanceSessionResponse{WorkspaceID: "ws-1", SessionID: "agent"}); err != nil {
				t.Fatal(err)
			}
			var message string
			f.h.send = func(id, text string) error {
				if id != "agent" {
					t.Fatalf("recipient=%s", id)
				}
				message = text
				if fail {
					return errors.New("agent disconnected")
				}
				return nil
			}
			body := `{"version":1,"timeline":[]}`
			r := httptest.NewRequest(http.MethodPost, "/api/client-performance/recording?session_id=agent&filename=recording.json", strings.NewReader(body))
			rr := httptest.NewRecorder()
			f.h.handleRecording(rr, r)
			want := 200
			if fail {
				want = 409
			}
			if rr.Code != want {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
			}
			path := strings.Split(message, "File attachments:\n")[1]
			if !strings.HasPrefix(path, physical+string(os.PathSeparator)) {
				t.Fatalf("attachment escaped physical workspace: %s", path)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != body {
				t.Fatalf("data=%s err=%v", data, err)
			}
		})
	}
}
func TestClientPerformance_RecordingRejectsInvalidJSON(t *testing.T) {
	f := newCPFixture(t)
	_ = f.st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "https://example.com/schmux.git", Branch: clientPerformanceBranch + "test", Path: t.TempDir()})
	_ = f.st.AddSession(state.Session{ID: "agent", WorkspaceID: "ws-1", Kind: state.SessionKindChat})
	if err := f.h.writeState(contracts.ClientPerformanceSessionResponse{WorkspaceID: "ws-1", SessionID: "agent"}); err != nil {
		t.Fatal(err)
	}
	f.h.send = func(string, string) error { t.Fatal("sent invalid recording"); return nil }
	r := httptest.NewRequest(http.MethodPost, "/?session_id=agent&filename=recording.json", strings.NewReader("invalid"))
	rr := httptest.NewRecorder()
	f.h.handleRecording(rr, r)
	if rr.Code != 400 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestClientPerformance_ReturnsDurableWorkspaceAfterReload(t *testing.T) {
	f := newCPFixture(t)
	code, launched, body := f.post(t, contracts.ClientPerformanceSessionRequest{})
	if code != 200 {
		t.Fatalf("launch status=%d body=%s", code, body)
	}
	// A new handler reads the stored association, rather than inferring ownership from branch names.
	reloaded := &ClientPerformanceHandlers{config: f.h.config, state: f.st, workspace: f.h.workspace}
	rr := httptest.NewRecorder()
	reloaded.handleGetSession(rr, httptest.NewRequest(http.MethodGet, "/api/client-performance/session", nil))
	var got contracts.ClientPerformanceSessionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if rr.Code != 200 || got != launched {
		t.Fatalf("status=%d restored=%+v launched=%+v", rr.Code, got, launched)
	}
}
