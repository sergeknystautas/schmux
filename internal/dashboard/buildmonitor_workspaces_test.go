//go:build !nobuildmonitor && !nogithub

package dashboard

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/log"
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

// runGit runs a git command in dir and fails the test when it errors.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=schmux", "GIT_AUTHOR_EMAIL=schmux@example.com",
		"GIT_COMMITTER_NAME=schmux", "GIT_COMMITTER_EMAIL=schmux@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// isolateGitEnv points HOME at a temporary directory whose git config signs
// nothing and runs no hooks, so the developer's global git config cannot
// affect the test — neither the git calls below nor the clone the workspace
// manager makes.
func isolateGitEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	gitConfig := "[user]\n\temail = schmux@example.com\n\tname = schmux\n" +
		"[commit]\n\tgpgsign = false\n" +
		"[init]\n\tdefaultBranch = main\n"
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(gitConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

// fakeGitHubRepo creates a local git repo whose path contains "github.com":
// the monitor only watches GitHub URLs, and git clones a local path like any
// other remote.
func fakeGitHubRepo(t *testing.T, name string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	isolateGitEnv(t)
	dir := filepath.Join(t.TempDir(), "github.com", "acme", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init")
	runGit(t, dir, "symbolic-ref", "HEAD", "refs/heads/main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
	return dir
}

// newBuildMonitorSyncServer builds a server over a real workspace manager,
// watching the repos whose slug is in monitored.
func newBuildMonitorSyncServer(t *testing.T, repos []config.Repo, monitored map[string]bool) (*Server, *config.Config, *state.State) {
	t.Helper()
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })

	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = t.TempDir()
	cfg.WorktreeBasePath = t.TempDir()
	cfg.Repos = repos
	bmRepos := map[string]config.BuildMonitorRepoConfig{}
	for slug, on := range monitored {
		if on {
			bmRepos[slug] = config.BuildMonitorRepoConfig{Enabled: true, GitHubLogin: "octocat"}
		}
	}
	cfg.BuildMonitor = &config.BuildMonitorConfig{Enabled: true, Repos: bmRepos}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	statePath := filepath.Join(t.TempDir(), "state.json")
	stStore := state.New(statePath, nil)
	logger := log.NewWithOptions(io.Discard, log.Options{})
	wm := workspace.New(cfg, stStore, statePath, logger)
	sm := session.New(cfg, stStore, statePath, wm, nil, logger)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	t.Cleanup(shutdownCancel)
	server := NewServer(cfg, stStore, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
	server.SetModelManager(models.New(cfg, nil, "", logger))
	t.Cleanup(func() { server.CloseForTest() })
	return server, cfg, stStore
}

// writeUnitState writes the durable unit file for a monitored repo.
func writeUnitState(t *testing.T, slug string, st *buildmonitor.UnitState) {
	t.Helper()
	if err := buildmonitor.WriteState(buildMonitorUnitStatePath(slug), st); err != nil {
		t.Fatal(err)
	}
}

// Every watched workflow gets its own dedicated workspace, registered with
// the repo it is watched in and the branch its agent works on.
func TestSyncBuildMonitorWorkspaces_RegistersEachWorkflow(t *testing.T) {
	bachGodot := fakeGitHubRepo(t, "bach-godot")
	other := fakeGitHubRepo(t, "other")
	repos := []config.Repo{
		{Name: "bach-godot", URL: bachGodot, BarePath: "bach-godot.git"},
		{Name: "other", URL: other, BarePath: "other.git"},
	}
	server, _, _ := newBuildMonitorSyncServer(t, repos, map[string]bool{"bach-godot": true, "other": true})
	writeUnitState(t, "bach-godot", &buildmonitor.UnitState{
		RepoName: "bach-godot", Repo: "acme/bach-godot", Branch: "main",
		Workflows: []buildmonitor.WorkflowState{
			{Name: "CI", WorkflowID: 1},
			{Name: "CI Image", WorkflowID: 2},
		},
	})

	server.syncBuildMonitorWorkspaces()

	ws, err := server.workspace.EnsureDedicated(context.Background(), "build-monitor/bach-godot/ci-image")
	if err != nil {
		t.Fatalf("EnsureDedicated: %v", err)
	}
	if ws.Label != "build-monitor-bach-godot-ci-image" {
		t.Errorf("label = %q, want build-monitor-bach-godot-ci-image", ws.Label)
	}
	if ws.Branch != "fix/ci-image" {
		t.Errorf("branch = %q, want fix/ci-image", ws.Branch)
	}
	if ws.Repo != bachGodot {
		t.Errorf("repo = %q, want the monitored repo %q", ws.Repo, bachGodot)
	}
}

// A repo that stops being monitored gives up its workspaces' names; the
// workspaces themselves stay, with the user's work in them.
func TestSyncBuildMonitorWorkspaces_UnregistersUnmonitoredRepo(t *testing.T) {
	bachGodot := fakeGitHubRepo(t, "bach-godot")
	other := fakeGitHubRepo(t, "other")
	repos := []config.Repo{
		{Name: "bach-godot", URL: bachGodot, BarePath: "bach-godot.git"},
		{Name: "other", URL: other, BarePath: "other.git"},
	}
	server, _, st := newBuildMonitorSyncServer(t, repos, map[string]bool{"bach-godot": true})
	if err := st.AddWorkspace(state.Workspace{
		ID: "ws-other", Repo: other, Branch: "fix/ci", Path: t.TempDir(),
		Dedicated: "build-monitor/other/ci",
	}); err != nil {
		t.Fatal(err)
	}

	server.syncBuildMonitorWorkspaces()

	for _, name := range server.workspace.RegisteredDedicated("build-monitor/") {
		if name == "build-monitor/other/ci" {
			t.Errorf("name %q still registered for an unmonitored repo", name)
		}
	}
	ws, found := st.GetWorkspace("ws-other")
	if !found {
		t.Fatal("the workspace itself must survive the unregistration")
	}
	if ws.Dedicated != "" {
		t.Errorf("workspace dedicated = %q, want it cleared", ws.Dedicated)
	}
}

// A workflow missing from one check pass keeps its workspace: a transient
// gap must not orphan it.
func TestSyncBuildMonitorWorkspaces_KeepsMissingWorkflow(t *testing.T) {
	bachGodot := fakeGitHubRepo(t, "bach-godot")
	repos := []config.Repo{{Name: "bach-godot", URL: bachGodot, BarePath: "bach-godot.git"}}
	server, _, st := newBuildMonitorSyncServer(t, repos, map[string]bool{"bach-godot": true})
	writeUnitState(t, "bach-godot", &buildmonitor.UnitState{
		RepoName: "bach-godot", Repo: "acme/bach-godot", Branch: "main",
		Workflows: []buildmonitor.WorkflowState{{Name: "CI", WorkflowID: 1}},
	})
	if err := st.AddWorkspace(state.Workspace{
		ID: "ws-gone", Repo: bachGodot, Branch: "fix/gone", Path: t.TempDir(),
		Dedicated: "build-monitor/bach-godot/gone",
	}); err != nil {
		t.Fatal(err)
	}

	server.syncBuildMonitorWorkspaces()

	names := server.workspace.RegisteredDedicated("build-monitor/")
	found := false
	for _, name := range names {
		if name == "build-monitor/bach-godot/gone" {
			found = true
		}
	}
	if !found {
		t.Errorf("names = %v, want the missing workflow's name kept", names)
	}
}

// Disabling the feature gives up every Build Monitor workspace name.
func TestSyncBuildMonitorWorkspaces_DisabledUnregistersAll(t *testing.T) {
	bachGodot := fakeGitHubRepo(t, "bach-godot")
	repos := []config.Repo{{Name: "bach-godot", URL: bachGodot, BarePath: "bach-godot.git"}}
	server, cfg, st := newBuildMonitorSyncServer(t, repos, map[string]bool{"bach-godot": true})
	if err := st.AddWorkspace(state.Workspace{
		ID: "ws-ci", Repo: bachGodot, Branch: "fix/ci", Path: t.TempDir(),
		Dedicated: "build-monitor/bach-godot/ci",
	}); err != nil {
		t.Fatal(err)
	}
	cfg.BuildMonitor.Enabled = false

	server.syncBuildMonitorWorkspaces()

	if names := server.workspace.RegisteredDedicated("build-monitor/"); len(names) != 0 {
		t.Errorf("names = %v, want none while the feature is disabled", names)
	}
}
