//go:build !nobuildmonitor && !nogithub

package dashboard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/buildmonitor"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func TestApplyBuildMonitor_ConvertsNameKeysToSlug(t *testing.T) {
	cfg := &config.Config{}
	req := &contracts.BuildMonitorConfig{
		Enabled:                     true,
		IntervalSeconds:             90,
		Target:                      " claude ",
		AutoWorkspaceOnFirstFailure: true,
		Repos: map[string]contracts.BuildMonitorRepoConfig{
			"My Repo": {Enabled: true, GitHubLogin: "octocat"},
		},
	}
	applyBuildMonitor(cfg, req)
	if _, ok := cfg.BuildMonitor.Repos["my-repo"]; !ok {
		t.Fatalf("expected slug key 'my-repo', got keys %v", cfg.BuildMonitor.Repos)
	}
	if cfg.BuildMonitor.Repos["my-repo"].GitHubLogin != "octocat" {
		t.Fatalf("expected GitHubLogin octocat, got %q", cfg.BuildMonitor.Repos["my-repo"].GitHubLogin)
	}
	if cfg.BuildMonitor.IntervalSeconds != 90 {
		t.Fatalf("expected IntervalSeconds 90, got %d", cfg.BuildMonitor.IntervalSeconds)
	}
	if cfg.BuildMonitor.Target != "claude" {
		t.Fatalf("expected trimmed Target claude, got %q", cfg.BuildMonitor.Target)
	}
	if !cfg.BuildMonitor.AutoWorkspaceOnFirstFailure {
		t.Fatal("expected AutoWorkspaceOnFirstFailure true")
	}
}

func TestApplyBuildMonitor_NilInput(t *testing.T) {
	cfg := &config.Config{}
	applyBuildMonitor(cfg, (*contracts.BuildMonitorConfig)(nil))
	if cfg.BuildMonitor != nil {
		t.Fatal("expected nil BuildMonitor after nil input")
	}
}

func TestCollectUnitDirectives(t *testing.T) {
	st := &buildmonitor.UnitState{Workflows: []buildmonitor.WorkflowState{
		{WorkflowID: 1, Name: "CI", RunID: 11, Status: "completed", Conclusion: "failure", FirstFailureRunID: 11, HeadSHA: "abc"},
		{WorkflowID: 2, Name: "Lint", RunID: 22, Status: "completed", Conclusion: "failure", FirstFailureRunID: 22, HeadSHA: "abc"},
	}}
	events := []buildmonitor.TransitionEvent{
		{WorkflowID: 1, Kind: buildmonitor.TransitionEnteredFailure, RunID: 11},
		{WorkflowID: 2, Kind: buildmonitor.TransitionEnteredFailure, FromUnknown: true, RunID: 22},
	}
	base := launchDirective{slug: "repo-a", repoName: "Repo A", repoURL: "https://github.com/o/r", repo: "o/r", login: "octocat"}
	got := collectUnitDirectives(base, events, st, "2026-08-13T08:00:00Z")
	if len(got) != 1 {
		t.Fatalf("got %d directives, want 1 (FromUnknown excluded): %+v", len(got), got)
	}
	if got[0].workflow.WorkflowID != 1 || got[0].workflow.HeadSHA != "abc" || got[0].slug != "repo-a" {
		t.Fatalf("directive = %+v", got[0])
	}
	if got := collectUnitDirectives(base, events, st, "2026-08-13T08:05:00Z"); len(got) != 0 {
		t.Fatalf("duplicate check produced directives: %+v", got)
	}
}

// TestBuildMonitorGetServesHydratedStateAfterRestart is the restart
// regression: the durable unit files must reach the page immediately after
// server construction, before any check pass runs.
func TestBuildMonitorGetServesHydratedStateAfterRestart(t *testing.T) {
	schmuxdir.Set(t.TempDir())
	defer schmuxdir.Set("")

	st := &buildmonitor.UnitState{
		RepoName: "Repo A", Repo: "o/r", Branch: "main", HeadSHA: "h1",
		CheckedAt: "2026-08-20T10:00:00Z",
		Workflows: []buildmonitor.WorkflowState{
			{WorkflowID: 1, Name: "CI", RunID: 11, Status: "completed", Conclusion: "success", HeadSHA: "h1", HTMLURL: "u"},
		},
	}
	if err := buildmonitor.WriteState(buildMonitorUnitStatePath("repo-a"), st); err != nil {
		t.Fatal(err)
	}
	// A watched feature-branch head recorded by the previous process — it
	// exists in no unit snapshot, only in the persisted commit store.
	commits := `[{"owner":"o","repo":"r","branch":"feature","sha":"f1","status":"success","url":"https://run9","terminal":true,"fetched_at":"2026-08-20T10:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(buildMonitorStateDir(), "commits.json"), []byte(commits), 0o600); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = t.TempDir()
	cfg.Repos = []config.Repo{{Name: "Repo A", URL: "https://github.com/o/r"}}
	cfg.BuildMonitor = &config.BuildMonitorConfig{
		Enabled: true,
		Repos:   map[string]config.BuildMonitorRepoConfig{"repo-a": {Enabled: true, GitHubLogin: "octocat"}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	statePath := filepath.Join(t.TempDir(), "state.json")
	stStore := state.New(statePath, nil)
	logger := log.NewWithOptions(io.Discard, log.Options{})
	wm := workspace.New(cfg, stStore, statePath, logger)
	sm := session.New(cfg, stStore, statePath, wm, nil, logger)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	defer shutdownCancel()
	server := NewServer(cfg, stStore, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
	server.SetModelManager(models.New(cfg, nil, "", logger))
	defer server.CloseForTest()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/build-monitor", nil)
	server.handleBuildMonitorGet(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", w.Code, w.Body.String())
	}
	var resp contracts.BuildMonitorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Units) != 1 {
		t.Fatalf("units = %d, want 1: %+v", len(resp.Units), resp.Units)
	}
	u := resp.Units[0]
	if u.Status != "success" {
		t.Errorf("status = %q, want success (hydrated from durable state)", u.Status)
	}
	if len(u.Workflows) != 1 || u.Workflows[0].RunID != 11 {
		t.Errorf("workflows = %+v, want the persisted CI row", u.Workflows)
	}
	if u.HeadSHA != "h1" || u.Branch != "main" {
		t.Errorf("head = %q branch = %q, want h1/main", u.HeadSHA, u.Branch)
	}

	// The feature-branch head from the persisted commit store must survive
	// the restart too — this is what workspace CI chips read.
	info := github.RepoInfo{Owner: "o", Repo: "r"}
	if status, url, ok := server.buildMonitor.Status(info, "feature", "f1"); !ok || status != "success" || url != "https://run9" {
		t.Errorf("watched head after restart = (%q, %q, %v), want (success, https://run9, true)", status, url, ok)
	}
}

func launchRequest(t *testing.T, slug, runID string) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost,
		"/api/build-monitor/repos/"+slug+"/failures/"+runID+"/launch-workspace", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("slug", slug)
	rctx.URLParams.Add("runID", runID)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	return httptest.NewRecorder(), r
}

func TestHandleBuildMonitorLaunch_Validation(t *testing.T) {
	schmuxdir.Set(t.TempDir())
	defer schmuxdir.Set("")

	t.Run("feature disabled is 400", func(t *testing.T) {
		s := &Server{config: &config.Config{}}
		w, r := launchRequest(t, "repo-a", "11")
		s.handleBuildMonitorLaunch(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, want 400", w.Code)
		}
	})

	t.Run("no target is 400", func(t *testing.T) {
		s := &Server{config: &config.Config{ConfigData: config.ConfigData{BuildMonitor: &config.BuildMonitorConfig{Enabled: true}}}}
		w, r := launchRequest(t, "repo-a", "11")
		s.handleBuildMonitorLaunch(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, want 400", w.Code)
		}
	})

	t.Run("unmonitored repo is 404", func(t *testing.T) {
		s := &Server{config: &config.Config{ConfigData: config.ConfigData{BuildMonitor: &config.BuildMonitorConfig{
			Enabled: true, Target: "claude",
		}}}}
		w, r := launchRequest(t, "repo-a", "11")
		s.handleBuildMonitorLaunch(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("code = %d, want 404", w.Code)
		}
	})

	t.Run("run not in failing state is 404", func(t *testing.T) {
		cfg := &config.Config{
			ConfigData: config.ConfigData{
				Repos: []config.Repo{{Name: "Repo A", URL: "https://github.com/o/r"}},
				BuildMonitor: &config.BuildMonitorConfig{
					Enabled: true, Target: "claude",
					Repos: map[string]config.BuildMonitorRepoConfig{
						"repo-a": {Enabled: true, GitHubLogin: "octocat"},
					},
				},
			},
		}
		st := &buildmonitor.UnitState{Workflows: []buildmonitor.WorkflowState{
			{WorkflowID: 1, RunID: 11, Status: "completed", Conclusion: "success"},
		}}
		if err := buildmonitor.WriteState(buildMonitorUnitStatePath("repo-a"), st); err != nil {
			t.Fatal(err)
		}
		s := &Server{config: cfg}
		w, r := launchRequest(t, "repo-a", "11")
		s.handleBuildMonitorLaunch(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("code = %d, want 404: %s", w.Code, w.Body.String())
		}
	})
}

// TestBuildMonitorInputs_HeadsUseStoredWorkspaceFields pins the single-source
// rule: the watch set is built from the same stored workspace fields the
// sessions read path uses (RemoteHeadSHA + fork repo) — never from a separate
// git derivation — so the monitor and the CI chips cannot ask about
// different commits.
func TestBuildMonitorInputs_HeadsUseStoredWorkspaceFields(t *testing.T) {
	server, cfg, st := newTestServer(t)
	if err := config.SaveGitHubIdentity("octocat", "tok", "repo"); err != nil {
		t.Fatal(err)
	}
	cfg.Repos = []config.Repo{{Name: "Widget", URL: "https://github.com/acme/widget"}}
	cfg.BuildMonitor = &config.BuildMonitorConfig{
		Enabled: true,
		Repos:   map[string]config.BuildMonitorRepoConfig{"widget": {Enabled: true, GitHubLogin: "octocat"}},
	}
	seed := []state.Workspace{
		{ID: "ws-base", Repo: "https://github.com/acme/widget", Branch: "feat", Path: t.TempDir(),
			RemoteBranchExists: true, RemoteHeadSHA: "f1"},
		{ID: "ws-fork", Repo: "https://github.com/acme/widget", Branch: "fix", Path: t.TempDir(),
			RemoteBranchExists: true, RemoteHeadSHA: "f2",
			RemoteBranchIsFork: true, RemoteBranchURL: "https://github.com/forker/widget"},
		{ID: "ws-nosha", Repo: "https://github.com/acme/widget", Branch: "wip", Path: t.TempDir(),
			RemoteBranchExists: true},
	}
	for _, w := range seed {
		if err := st.AddWorkspace(w); err != nil {
			t.Fatal(err)
		}
	}

	_, heads, _, _ := server.buildMonitorInputs(context.Background(), cfg.GetRepos(), cfg.GetBuildMonitorRepos())

	bySHA := map[string]buildmonitor.HeadInput{}
	for _, h := range heads {
		bySHA[h.SHA] = h
	}
	if len(heads) != 2 {
		t.Fatalf("heads = %+v, want exactly the two workspaces with a stored remote head", heads)
	}
	base := bySHA["f1"]
	if base.Info.Owner != "acme" || base.Info.Repo != "widget" || base.Branch != "feat" {
		t.Errorf("base head = %+v, want acme/widget@feat", base)
	}
	fork := bySHA["f2"]
	if fork.Info.Owner != "forker" || fork.Info.Repo != "widget" || fork.Branch != "fix" {
		t.Errorf("fork head = %+v, want forker/widget@fix (fork-resolved from stored RemoteBranchURL)", fork)
	}
}
