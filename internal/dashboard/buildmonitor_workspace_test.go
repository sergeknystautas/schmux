//go:build !nobuildmonitor && !nogithub

package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
	"github.com/sergeknystautas/schmux/internal/workspacestatus"
)

// stubHeadWM overrides remote-head resolution; everything else delegates.
type stubHeadWM struct {
	workspace.WorkspaceManager
	head workspace.RemoteBranchHead
	err  error
}

func (m *stubHeadWM) GetRemoteBranchHead(_ context.Context, _ string) (workspace.RemoteBranchHead, error) {
	return m.head, m.err
}

// setupUnifiedPass wires a test server with one monitored repo, one identity
// token, one eligible workspace, and a fake GitHub API. Returns the server
// and the fake API's request log.
func setupUnifiedPass(t *testing.T, workflowRunsJSON, pullsJSON string) (*Server, *[]string) {
	t.Helper()
	return setupUnifiedPassWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/actions/workflows"):
			w.Write([]byte(`{"workflows": [{"id": 1, "name": "CI", "path": ".github/workflows/ci.yml", "state": "active"}]}`))
		case strings.HasSuffix(r.URL.Path, "/actions/runs"):
			w.Write([]byte(workflowRunsJSON))
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			w.Write([]byte(pullsJSON))
		default:
			w.Write([]byte(`{}`))
		}
	})
}

// setupUnifiedPassWithHandler is setupUnifiedPass with a caller-supplied fake
// GitHub API handler (for error-path tests).
func setupUnifiedPassWithHandler(t *testing.T, handler http.HandlerFunc) (*Server, *[]string) {
	t.Helper()
	srv, cfg, st := newTestServer(t)

	cfg.Repos = append(cfg.Repos, config.Repo{Name: "widget", URL: "https://github.com/acme/widget"})
	cfg.BuildMonitor = &config.BuildMonitorConfig{
		Enabled: true,
		Repos:   map[string]config.BuildMonitorRepoConfig{"widget": {Enabled: true, GitHubLogin: "tester"}},
	}
	if err := config.SaveSecretsFile(&config.SecretsFile{Auth: config.AuthSecrets{GitHub: &config.GitHubSecrets{
		Identities: map[string]config.GitHubIdentity{"tester": {Login: "tester", Token: "tok"}},
	}}}); err != nil {
		t.Fatalf("SaveSecretsFile: %v", err)
	}

	if err := st.AddWorkspace(state.Workspace{
		ID: "ws1", Repo: "https://github.com/acme/widget", Branch: "feature",
		Path: t.TempDir(), Status: state.WorkspaceStatusRunning,
		RemoteBranchExists: true,
	}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	stub := &stubHeadWM{
		WorkspaceManager: srv.workspace,
		head:             workspace.RemoteBranchHead{SHA: "abc", RemoteURL: "https://github.com/acme/widget"},
	}
	srv.workspace = stub
	srv.sessionHandlers.workspace = stub

	requests := &[]string{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, r.URL.Path+"?"+r.URL.RawQuery)
		handler(w, r)
	}))
	t.Cleanup(api.Close)
	t.Cleanup(github.SetAPIBaseURLForTest(api.URL))
	return srv, requests
}

func TestPassPopulatesWorkspaceStatus(t *testing.T) {
	runs := `{"workflow_runs": [{"id": 9, "workflow_id": 1, "run_number": 1, "status": "completed", "conclusion": "success", "head_sha": "abc", "html_url": "https://run"}]}`
	pulls := `[{"number": 42, "html_url": "https://pr"}]`
	srv, _ := setupUnifiedPass(t, runs, pulls)

	srv.runBuildMonitorCheckPass(context.Background())

	got, ok := srv.workspaceStatus.Get("ws1")
	if !ok {
		t.Fatal("no status cached for ws1")
	}
	want := workspacestatus.Status{CIStatus: workspacestatus.CISuccess, CIURL: "https://run", PRNumber: 42, PRURL: "https://pr"}
	if got != want {
		t.Errorf("status = %+v, want %+v", got, want)
	}
}

func TestPassSkipsWorkspaceOfUnmonitoredRepo(t *testing.T) {
	runs := `{"workflow_runs": []}`
	srv, _ := setupUnifiedPass(t, runs, `[]`)
	if err := srv.state.AddWorkspace(state.Workspace{
		ID: "ws-other", Repo: "https://github.com/other/repo", Branch: "b",
		Path: t.TempDir(), Status: state.WorkspaceStatusRunning, RemoteBranchExists: true,
	}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}

	srv.runBuildMonitorCheckPass(context.Background())

	if _, ok := srv.workspaceStatus.Get("ws-other"); ok {
		t.Error("unmonitored repo's workspace got a status")
	}
}

func TestPassDropsEntriesForDisappearedWorkspaces(t *testing.T) {
	runs := `{"workflow_runs": []}`
	srv, _ := setupUnifiedPass(t, runs, `[]`)
	srv.workspaceStatus.Store("gone", workspacestatus.Entry{Status: workspacestatus.Status{CIStatus: workspacestatus.CISuccess}})

	srv.runBuildMonitorCheckPass(context.Background())

	if _, ok := srv.workspaceStatus.Get("gone"); ok {
		t.Error("stale entry survived the pass")
	}
}

func TestPassDisabledClearsCache(t *testing.T) {
	srv, _ := setupUnifiedPass(t, `{"workflow_runs": []}`, `[]`)
	srv.workspaceStatus.Store("ws1", workspacestatus.Entry{Status: workspacestatus.Status{CIStatus: workspacestatus.CISuccess}})
	srv.config.BuildMonitor.Enabled = false

	srv.runBuildMonitorCheckPass(context.Background())

	if _, ok := srv.workspaceStatus.Get("ws1"); ok {
		t.Error("cache not cleared when build monitor disabled")
	}
}

func TestPassTerminalTTLSkipsCICall(t *testing.T) {
	runs := `{"workflow_runs": [{"id": 9, "workflow_id": 1, "run_number": 1, "status": "completed", "conclusion": "success", "head_sha": "abc", "html_url": "https://run"}]}`
	srv, requests := setupUnifiedPass(t, runs, `[]`)

	now := time.Unix(10000, 0)
	orig := buildMonitorNow
	buildMonitorNow = func() time.Time { return now }
	t.Cleanup(func() { buildMonitorNow = orig })

	srv.runBuildMonitorCheckPass(context.Background())
	countRuns := func() int {
		n := 0
		for _, r := range *requests {
			if strings.Contains(r, "/actions/runs?") && strings.Contains(r, "branch=feature") {
				n++
			}
		}
		return n
	}
	if countRuns() != 1 {
		t.Fatalf("workspace runs calls = %d, want 1", countRuns())
	}
	pullsBefore := len(*requests)

	now = now.Add(time.Minute) // within 5-min TTL, same SHA
	srv.runBuildMonitorCheckPass(context.Background())
	if countRuns() != 1 {
		t.Errorf("workspace runs calls after cached pass = %d, want 1", countRuns())
	}
	if len(*requests) == pullsBefore {
		t.Error("PR lookup was skipped; it must run every pass")
	}

	now = now.Add(5 * time.Minute) // TTL expired
	srv.runBuildMonitorCheckPass(context.Background())
	if countRuns() != 2 {
		t.Errorf("workspace runs calls after TTL = %d, want 2", countRuns())
	}
}

func TestPassDefaultBranchWorkspaceReusesUnitRuns(t *testing.T) {
	runs := `{"workflow_runs": [{"id": 9, "workflow_id": 1, "run_number": 1, "status": "completed", "conclusion": "success", "head_sha": "abc", "html_url": "https://run"}]}`
	srv, requests := setupUnifiedPass(t, runs, `[]`)
	// Re-point the workspace at the default branch.
	ws, ok := srv.state.GetWorkspace("ws1")
	if !ok {
		t.Fatal("ws1 missing")
	}
	ws.Branch = "main"
	srv.state.UpdateWorkspace(ws)

	srv.runBuildMonitorCheckPass(context.Background())

	// Exactly one runs call total (the unit's own default-branch fetch);
	// the workspace must not add a second.
	n := 0
	for _, r := range *requests {
		if strings.Contains(r, "/actions/runs?") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("total runs calls = %d, want 1 (workspace reuses unit runs)", n)
	}
	if got, ok := srv.workspaceStatus.Get("ws1"); !ok || got.CIStatus != workspacestatus.CISuccess {
		t.Errorf("default-branch workspace status = (%+v, %v)", got, ok)
	}
}

// A rate-limited workspace call must back off — no CI/PR calls for that
// workspace until the window (2× interval) passes — without killing the pass.
func TestPassRateLimitBacksOffWorkspaceCalls(t *testing.T) {
	now := time.Unix(50000, 0)
	orig := buildMonitorNow
	buildMonitorNow = func() time.Time { return now }
	t.Cleanup(func() { buildMonitorNow = orig })

	// 429 only the workspace branch's runs call; the unit's default-branch
	// ("main" fallback) call and everything else succeed.
	srv, requests := setupUnifiedPassWithHandler(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/actions/workflows"):
			w.Write([]byte(`{"workflows": []}`))
		case strings.HasSuffix(r.URL.Path, "/actions/runs") && strings.Contains(r.URL.RawQuery, "branch=feature"):
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
		case strings.HasSuffix(r.URL.Path, "/actions/runs"):
			w.Write([]byte(`{"workflow_runs": []}`))
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			w.Write([]byte(`[]`))
		default:
			w.Write([]byte(`{}`))
		}
	})
	count := func(substr string) int {
		n := 0
		for _, r := range *requests {
			if strings.Contains(r, substr) {
				n++
			}
		}
		return n
	}

	srv.runBuildMonitorCheckPass(context.Background())
	if got := count("branch=feature"); got != 1 {
		t.Fatalf("workspace runs calls = %d, want 1", got)
	}
	if got := count("/pulls?"); got != 0 {
		t.Fatalf("PR calls after rate-limited CI call = %d, want 0", got)
	}

	// Within the backoff window (2× 60s interval = 2min): workspace skipped.
	now = now.Add(time.Minute)
	srv.runBuildMonitorCheckPass(context.Background())
	if got := count("branch=feature"); got != 1 {
		t.Errorf("workspace runs calls during backoff = %d, want 1", got)
	}

	// After the window: the workspace is retried.
	now = now.Add(2 * time.Minute)
	srv.runBuildMonitorCheckPass(context.Background())
	if got := count("branch=feature"); got != 2 {
		t.Errorf("workspace runs calls after backoff = %d, want 2", got)
	}
}
