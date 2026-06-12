//go:build !nobuildmonitor && !nogithub

package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/buildmonitor"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func TestApplyBuildMonitor_ConvertsNameKeysToSlug(t *testing.T) {
	cfg := &config.Config{}
	req := &contracts.BuildMonitorConfig{
		Enabled:                     true,
		Interval:                    10,
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
	if cfg.BuildMonitor.Interval != 10 {
		t.Fatalf("expected Interval 10, got %d", cfg.BuildMonitor.Interval)
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
	got := collectUnitDirectives(base, events, st)
	if len(got) != 1 {
		t.Fatalf("got %d directives, want 1 (FromUnknown excluded): %+v", len(got), got)
	}
	if got[0].workflow.WorkflowID != 1 || got[0].workflow.HeadSHA != "abc" || got[0].slug != "repo-a" {
		t.Fatalf("directive = %+v", got[0])
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
