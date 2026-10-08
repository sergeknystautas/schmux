package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
)

func schmuxCheckout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+schmuxModulePath+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

type cpFixture struct {
	h       *ClientPerformanceHandlers
	st      *state.State
	spawned []session.SpawnOptions
	created []string
}

func newCPFixture(t *testing.T) *cpFixture {
	t.Helper()
	server, cfg, st := newTestServer(t)
	cfg.Repos = []config.Repo{{Name: "schmux", URL: "https://example.com/schmux.git"}}
	cfg.ClientPerformance = &config.ClientPerformanceConfig{Enabled: boolPtr(true), Repo: "schmux", Target: "command"}
	cfg.ChatSessions = true
	f := &cpFixture{st: st}
	f.h = newClientPerformanceHandlers(server)
	f.h.hasChatMode = func(string) bool { return true }
	f.h.getOrCreate = func(_ context.Context, repoURL, branch string) (*state.Workspace, error) {
		f.created = append(f.created, repoURL+"@"+branch)
		ws := state.Workspace{ID: "ws-new", Repo: repoURL, Branch: branch, Path: schmuxCheckout(t)}
		if err := st.AddWorkspace(ws); err != nil {
			t.Fatal(err)
		}
		return &ws, nil
	}
	f.h.spawn = func(_ context.Context, opts session.SpawnOptions) (*state.Session, error) {
		f.spawned = append(f.spawned, opts)
		return &state.Session{ID: "sess-new", WorkspaceID: opts.WorkspaceID}, nil
	}
	return f
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
		})
	}
}

func TestClientPerformance_EmptyIdsCreatesAndSpawns(t *testing.T) {
	f := newCPFixture(t)
	code, resp, body := f.post(t, contracts.ClientPerformanceSessionRequest{})
	if code != http.StatusOK {
		t.Fatalf("code=%d body=%s", code, body)
	}
	if f.created[0] != "https://example.com/schmux.git@client-performance" {
		t.Errorf("created %v", f.created)
	}
	s := f.spawned[0]
	if s.WorkspaceID != "ws-new" || s.TargetName != "command" || s.Kind != state.SessionKindChat || s.Prompt == "" {
		t.Errorf("spawn opts %+v", s)
	}
	if resp != (contracts.ClientPerformanceSessionResponse{WorkspaceID: "ws-new", SessionID: "sess-new"}) {
		t.Errorf("resp %+v", resp)
	}
}

func TestClientPerformance_RunningSessionReturnedAsSent(t *testing.T) {
	f := newCPFixture(t)
	_ = f.st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "https://example.com/schmux.git", Branch: clientPerformanceBranch, Path: schmuxCheckout(t)})
	_ = f.st.AddSession(state.Session{ID: "sess-1", WorkspaceID: "ws-1", Target: "command", Kind: state.SessionKindChat, Pid: os.Getpid()})
	code, resp, _ := f.post(t, contracts.ClientPerformanceSessionRequest{WorkspaceID: "ws-1", SessionID: "sess-1"})
	if code != http.StatusOK || resp.SessionID != "sess-1" || len(f.spawned) != 0 {
		t.Fatalf("code=%d resp=%+v spawned=%d", code, resp, len(f.spawned))
	}
}

func TestClientPerformance_EndedSessionRespawnsInLiveWorkspace(t *testing.T) {
	f := newCPFixture(t)
	_ = f.st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "https://example.com/schmux.git", Branch: clientPerformanceBranch, Path: schmuxCheckout(t)})
	_ = f.st.AddSession(state.Session{ID: "sess-1", WorkspaceID: "ws-1", Target: "command", Kind: state.SessionKindChat, Pid: 999999})
	code, resp, _ := f.post(t, contracts.ClientPerformanceSessionRequest{WorkspaceID: "ws-1", SessionID: "sess-1"})
	if code != http.StatusOK || resp.WorkspaceID != "ws-1" || resp.SessionID != "sess-new" || len(f.created) != 0 {
		t.Fatalf("code=%d resp=%+v created=%v", code, resp, f.created)
	}
}

func TestClientPerformance_StaleWorkspaceIdsFallThrough(t *testing.T) {
	cases := []struct {
		name string
		ws   state.Workspace
	}{
		{"disposing", state.Workspace{ID: "ws-1", Repo: "https://example.com/schmux.git", Branch: clientPerformanceBranch, Status: state.WorkspaceStatusDisposing}},
		{"different repo", state.Workspace{ID: "ws-1", Repo: "https://example.com/other.git", Branch: clientPerformanceBranch}},
		{"different branch", state.Workspace{ID: "ws-1", Repo: "https://example.com/schmux.git", Branch: "main"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCPFixture(t)
			tc.ws.Path = schmuxCheckout(t)
			_ = f.st.AddWorkspace(tc.ws)
			code, resp, _ := f.post(t, contracts.ClientPerformanceSessionRequest{WorkspaceID: "ws-1", SessionID: "gone"})
			if code != http.StatusOK || resp.WorkspaceID != "ws-new" || len(f.created) != 1 {
				t.Fatalf("code=%d resp=%+v created=%v", code, resp, f.created)
			}
		})
	}
}

func TestClientPerformance_RefusesNonSchmuxCheckout(t *testing.T) {
	f := newCPFixture(t)
	f.h.getOrCreate = func(_ context.Context, repoURL, branch string) (*state.Workspace, error) {
		ws := state.Workspace{ID: "ws-plain", Repo: repoURL, Branch: branch, Path: t.TempDir()}
		_ = f.st.AddWorkspace(ws)
		return &ws, nil
	}
	code, _, body := f.post(t, contracts.ClientPerformanceSessionRequest{})
	if code != http.StatusBadRequest || !bytes.Contains([]byte(body), []byte("is not a schmux checkout")) || len(f.spawned) != 0 {
		t.Fatalf("code=%d body=%s spawned=%d", code, body, len(f.spawned))
	}
}
