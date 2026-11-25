package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

type cpFixture struct {
	h            *ClientPerformanceHandlers
	server       *Server
	st           *state.State
	spawned      []SpawnRequest
	ensureCalls  int
	registered   []cpRegistration
	unregistered []string
	broadcasts   int
}

type cpRegistration struct {
	name string
	reg  workspace.DedicatedRegistration
}

func newCPFixture(t *testing.T) *cpFixture {
	t.Helper()
	server, cfg, st := newTestServer(t)
	cfg.Repos = []config.Repo{{Name: "schmux", URL: "https://example.com/schmux.git"}}
	cfg.ClientPerformance = &config.ClientPerformanceConfig{Enabled: boolPtr(true), Repo: "schmux", Target: "command"}
	cfg.ChatSessions = true
	f := &cpFixture{st: st, h: server.clientPerf, server: server}
	f.h.broadcast = func() { f.broadcasts++ }
	f.h.targetExists = func(string) bool { return true }
	f.h.hasChatMode = func(string) bool { return true }
	f.h.isRunning = func(context.Context, string) bool { return false }
	f.h.ensure = func(context.Context) (*state.Workspace, error) {
		f.ensureCalls++
		if _, ok := st.GetWorkspace("ws-perf"); !ok {
			if err := st.AddWorkspace(state.Workspace{
				ID:        "ws-perf",
				Repo:      "https://example.com/schmux.git",
				Branch:    clientPerformanceBranch,
				Dedicated: clientPerformanceWorkspace,
				Path:      t.TempDir(),
			}); err != nil {
				t.Fatal(err)
			}
		}
		ws, _ := st.GetWorkspace("ws-perf")
		return &ws, nil
	}
	f.h.register = func(name string, reg workspace.DedicatedRegistration) {
		f.registered = append(f.registered, cpRegistration{name: name, reg: reg})
	}
	f.h.unregister = func(name string) error {
		f.unregistered = append(f.unregistered, name)
		return nil
	}
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
	code, launched, body := f.post(t)
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

func (f *cpFixture) post(t *testing.T) (int, contracts.ClientPerformanceSessionResponse, string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/client-performance/session", nil)
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
			code, _, body := f.post(t)
			if code != http.StatusBadRequest || !strings.Contains(body, tc.want) {
				t.Fatalf("code=%d body=%s want 400 containing %q", code, body, tc.want)
			}
			if len(f.spawned) != 0 {
				t.Fatalf("spawned=%d, want none", len(f.spawned))
			}
			if f.ensureCalls != 0 {
				t.Fatalf("ensure calls=%d, want none before validation", f.ensureCalls)
			}
		})
	}
}

func TestClientPerformance_LaunchCreatesDedicatedWorkspace(t *testing.T) {
	f := newCPFixture(t)
	code, resp, body := f.post(t)
	if code != http.StatusOK || resp.WorkspaceID != "ws-perf" || resp.SessionID != "sess-new" {
		t.Fatalf("code=%d response=%+v body=%s", code, resp, body)
	}
	if len(f.spawned) != 1 {
		t.Fatalf("spawns=%+v", f.spawned)
	}
	req := f.spawned[0]
	if req.WorkspaceID != "ws-perf" || req.Kind != state.SessionKindChat || req.Targets["command"] != 1 || req.Nickname != "Client performance" {
		t.Fatalf("spawn=%+v", req)
	}
	if !strings.Contains(req.Prompt, "Recording format:") || strings.Contains(req.Prompt, "Read that file") {
		t.Fatalf("prompt requires unavailable documentation: %s", req.Prompt)
	}
}

func TestClientPerformance_ReturnsRunningTrackedAgent(t *testing.T) {
	f := newCPFixture(t)
	_ = f.st.AddWorkspace(state.Workspace{ID: "ws-perf", Repo: "https://example.com/schmux.git", Branch: clientPerformanceBranch, Dedicated: clientPerformanceWorkspace, Path: t.TempDir()})
	_ = f.st.AddSession(state.Session{ID: "agent", WorkspaceID: "ws-perf", Kind: state.SessionKindChat})
	if err := f.h.writeState(contracts.ClientPerformanceSessionResponse{WorkspaceID: "ws-perf", SessionID: "agent"}); err != nil {
		t.Fatal(err)
	}
	f.h.isRunning = func(context.Context, string) bool { return true }
	code, resp, body := f.post(t)
	if code != http.StatusOK || resp.SessionID != "agent" || resp.WorkspaceID != "ws-perf" {
		t.Fatalf("code=%d response=%+v body=%s", code, resp, body)
	}
	if len(f.spawned) != 0 {
		t.Fatalf("spawned=%+v, want none", f.spawned)
	}
}

func TestClientPerformance_AgentEndedRelaunchesInSameWorkspace(t *testing.T) {
	f := newCPFixture(t)
	_ = f.st.AddWorkspace(state.Workspace{ID: "ws-perf", Repo: "https://example.com/schmux.git", Branch: clientPerformanceBranch, Dedicated: clientPerformanceWorkspace, Path: t.TempDir()})
	_ = f.st.AddSession(state.Session{ID: "agent", WorkspaceID: "ws-perf", Kind: state.SessionKindChat})
	if err := f.h.writeState(contracts.ClientPerformanceSessionResponse{WorkspaceID: "ws-perf", SessionID: "agent"}); err != nil {
		t.Fatal(err)
	}
	f.h.isRunning = func(context.Context, string) bool { return false }
	code, resp, body := f.post(t)
	if code != http.StatusOK {
		t.Fatalf("code=%d response=%+v body=%s", code, resp, body)
	}
	if len(f.spawned) != 1 || f.spawned[0].WorkspaceID != "ws-perf" {
		t.Fatalf("spawn=%+v", f.spawned)
	}
}

func TestClientPerformance_TrackedAgentInOtherWorkspaceRelaunches(t *testing.T) {
	f := newCPFixture(t)
	_ = f.st.AddWorkspace(state.Workspace{ID: "ws-old", Repo: "https://example.com/schmux.git", Branch: clientPerformanceBranch, Path: t.TempDir()})
	_ = f.st.AddSession(state.Session{ID: "agent", WorkspaceID: "ws-old", Kind: state.SessionKindChat})
	if err := f.h.writeState(contracts.ClientPerformanceSessionResponse{WorkspaceID: "ws-old", SessionID: "agent"}); err != nil {
		t.Fatal(err)
	}
	f.h.isRunning = func(context.Context, string) bool { return true }
	code, resp, body := f.post(t)
	if code != http.StatusOK {
		t.Fatalf("code=%d response=%+v body=%s", code, resp, body)
	}
	if len(f.spawned) != 1 || f.spawned[0].WorkspaceID != "ws-perf" {
		t.Fatalf("spawn=%+v", f.spawned)
	}
	if resp.WorkspaceID != "ws-perf" {
		t.Errorf("workspace_id=%q, want ws-perf", resp.WorkspaceID)
	}
}

func TestClientPerformance_FileStoresOnlySessionID(t *testing.T) {
	f := newCPFixture(t)
	if code, _, body := f.post(t); code != http.StatusOK {
		t.Fatalf("code=%d body=%s", code, body)
	}
	data, err := os.ReadFile(filepath.Join(schmuxdir.Get(), "client-performance.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]interface{}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 {
		t.Fatalf("stored keys=%v, want only session_id", stored)
	}
	if _, ok := stored["session_id"]; !ok {
		t.Fatalf("stored keys=%v, want session_id", stored)
	}
}

func TestClientPerformance_IgnoresLegacyWorkspaceID(t *testing.T) {
	f := newCPFixture(t)
	legacy := `{"workspace_id":"schmux-004","session_id":""}`
	if err := os.WriteFile(filepath.Join(schmuxdir.Get(), "client-performance.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	code, resp, body := f.post(t)
	if code != http.StatusOK {
		t.Fatalf("code=%d body=%s", code, body)
	}
	if resp.WorkspaceID != "ws-perf" {
		t.Errorf("workspace_id=%q, want ws-perf", resp.WorkspaceID)
	}
	if len(f.spawned) != 1 || f.spawned[0].WorkspaceID != "ws-perf" {
		t.Fatalf("spawn=%+v", f.spawned)
	}
}

func TestClientPerformance_SyncRegistration(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*config.Config)
		wantReg bool
	}{
		{"enabled", func(*config.Config) {}, true},
		{"disabled", func(c *config.Config) { c.ClientPerformance.Enabled = nil }, false},
		{"repo cleared", func(c *config.Config) { c.ClientPerformance.Repo = "" }, false},
		{"target cleared", func(c *config.Config) { c.ClientPerformance.Target = "" }, false},
		{"repo not configured", func(c *config.Config) { c.ClientPerformance.Repo = "other" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCPFixture(t)
			tc.mutate(f.h.config)
			f.h.syncRegistration()
			// The tab reads the agent from the sessions snapshot, and either
			// outcome can end the old agent's association.
			if f.broadcasts != 1 {
				t.Errorf("broadcasts=%d, want 1", f.broadcasts)
			}
			if tc.wantReg {
				if len(f.registered) != 1 {
					t.Fatalf("registered=%+v, want one entry", f.registered)
				}
				got := f.registered[0]
				if got.name != "client-performance" {
					t.Errorf("name=%q, want client-performance", got.name)
				}
				if got.reg.RepoURL != "https://example.com/schmux.git" || got.reg.Branch != "fix/client-performance" {
					t.Errorf("registration=%+v", got.reg)
				}
				if len(f.unregistered) != 0 {
					t.Errorf("unregistered=%v, want none", f.unregistered)
				}
				return
			}
			if len(f.registered) != 0 {
				t.Fatalf("registered=%+v, want none", f.registered)
			}
			if len(f.unregistered) != 1 || f.unregistered[0] != "client-performance" {
				t.Errorf("unregistered=%v, want [client-performance]", f.unregistered)
			}
		})
	}
}

func TestClientPerformance_IgnoresSessionOutsideDedicatedWorkspace(t *testing.T) {
	f := newCPFixture(t)
	// The agent's workspace lost the name (the repo changed): it is not the
	// caller's workspace anymore.
	_ = f.st.AddWorkspace(state.Workspace{ID: "ws-old", Repo: "https://example.com/schmux.git", Branch: clientPerformanceBranch, Path: t.TempDir()})
	_ = f.st.AddSession(state.Session{ID: "agent", WorkspaceID: "ws-old", Kind: state.SessionKindChat})
	if err := f.h.writeState(contracts.ClientPerformanceSessionResponse{WorkspaceID: "ws-old", SessionID: "agent"}); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	f.h.handleGetSession(rr, httptest.NewRequest(http.MethodGet, "/api/client-performance/session", nil))
	var got contracts.ClientPerformanceSessionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SessionID != "" || got.WorkspaceID != "" {
		t.Fatalf("the agent outside the dedicated workspace is still tracked: %+v", got)
	}

	code, resp, body := f.post(t)
	if code != http.StatusOK {
		t.Fatalf("code=%d body=%s", code, body)
	}
	if resp.WorkspaceID != "ws-perf" || resp.SessionID != "sess-new" {
		t.Fatalf("response=%+v", resp)
	}
}

// A browser tab that closes mid-launch must not cancel the workspace creation.
func TestClientPerformance_LaunchOutlivesItsRequest(t *testing.T) {
	f := newCPFixture(t)
	ensure := f.h.ensure
	f.h.ensure = func(ctx context.Context) (*state.Workspace, error) {
		if err := ctx.Err(); err != nil {
			t.Errorf("ensure context: %v", err)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("ensure context has no deadline")
		}
		return ensure(ctx)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rr := httptest.NewRecorder()
	f.h.handleEnsureSession(rr, httptest.NewRequest(http.MethodPost, "/api/client-performance/session", nil).WithContext(ctx))
	if rr.Code != http.StatusOK || f.ensureCalls != 1 {
		t.Fatalf("code=%d ensure calls=%d body=%s", rr.Code, f.ensureCalls, rr.Body.String())
	}
}

func TestServer_SyncDedicatedRegistrations(t *testing.T) {
	f := newCPFixture(t)

	// The daemon calls this after a config file change.
	f.server.SyncDedicatedRegistrations()
	if len(f.registered) != 1 {
		t.Fatalf("registered=%+v, want one entry", f.registered)
	}
	if f.registered[0].reg.RepoURL != "https://example.com/schmux.git" {
		t.Errorf("registration=%+v", f.registered[0].reg)
	}

	// A config change that drops the repo unregisters the name.
	f.h.config.ClientPerformance.Repo = ""
	f.server.SyncDedicatedRegistrations()
	if len(f.unregistered) != 1 || f.unregistered[0] != "client-performance" {
		t.Errorf("unregistered=%v, want [client-performance]", f.unregistered)
	}
}

// The started server registers once at startup and again when the config is
// saved through its own route.
func TestServer_StartAndConfigSaveRegisterClientPerformance(t *testing.T) {
	f := newCPFixture(t)
	f.h.config.Repos = append(f.h.config.GetRepos(), config.Repo{Name: "other", URL: "https://example.com/other.git"})
	if err := f.h.config.Save(); err != nil {
		t.Fatal(err)
	}
	f.h.config.SetListenPort(0)
	f.h.broadcast = func() {}
	registered := make(chan string, 4)
	f.h.register = func(_ string, reg workspace.DedicatedRegistration) { registered <- reg.RepoURL }
	nextRegistration := func(when, want string) {
		t.Helper()
		select {
		case got := <-registered:
			if got != want {
				t.Fatalf("%s registered %s, want %s", when, got, want)
			}
		default:
			t.Fatalf("%s registered nothing, want %s", when, want)
		}
	}

	startErr := make(chan error, 1)
	go func() { startErr <- f.server.Start() }()
	var addr net.Addr
	select {
	case addr = <-f.server.BoundAddr:
	case <-time.After(10 * time.Second): // failure backstop; Start signals BoundAddr on every path
		t.Fatal("Start never signalled BoundAddr")
	}
	if addr == nil {
		t.Fatalf("Start failed to bind: %v", <-startErr)
	}
	t.Cleanup(func() {
		_ = f.server.Stop()
		<-startErr
	})
	nextRegistration("startup", "https://example.com/schmux.git")

	req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("http://%s/api/config", addr), strings.NewReader(`{"client_performance":{"repo":"other"}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("config save status=%d body=%s", resp.StatusCode, body)
	}
	nextRegistration("config save", "https://example.com/other.git")
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
			_ = f.st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "https://example.com/schmux.git", Branch: "main", Dedicated: clientPerformanceWorkspace, Path: alias})
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
	_ = f.st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "https://example.com/schmux.git", Branch: clientPerformanceBranch, Dedicated: clientPerformanceWorkspace, Path: t.TempDir()})
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
	code, launched, body := f.post(t)
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
	if got.WorkspaceID != "ws-perf" {
		t.Errorf("workspace_id=%q, want the session's workspace ws-perf", got.WorkspaceID)
	}
}
