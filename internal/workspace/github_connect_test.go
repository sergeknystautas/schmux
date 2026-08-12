package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// connectFixture builds a workspace equivalent to one made by CreateLocalRepo:
// plain git init, one empty commit on a schmux-style branch, repo "local:talkback".
func connectFixture(t *testing.T) (m *Manager, st *state.State, cfg *config.Config, wsPath string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg = config.CreateDefault(filepath.Join(t.TempDir(), "config.json"))
	cfg.WorkspacePath = tmpDir
	cfg.Repos = []config.Repo{{Name: "talkback", URL: "local:talkback", BarePath: "talkback.git"}}
	st = state.New(statePath, nil)

	wsPath = filepath.Join(tmpDir, "talkback-001")
	if err := os.MkdirAll(wsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wsPath, "init")
	gitIn(t, wsPath, "config", "user.email", "test@test")
	gitIn(t, wsPath, "config", "user.name", "test")
	gitIn(t, wsPath, "checkout", "-b", "feature/prompt-branch")
	gitIn(t, wsPath, "commit", "--allow-empty", "-m", "Initial commit")

	st.AddWorkspace(state.Workspace{ID: "talkback-001", Repo: "local:talkback", Branch: "feature/prompt-branch", Path: wsPath})
	m = New(cfg, st, statePath, testLogger())
	return m, st, cfg, wsPath
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// bareRemote creates an empty bare repo to stand in for GitHub.
func bareRemote(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "remote.git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "--bare")
	return dir
}

func neededSteps(plan []contracts.GitHubConnectPlanStep) []string {
	var out []string
	for _, p := range plan {
		if p.Needed {
			out = append(out, p.Step)
		}
	}
	return out
}

func TestDetectGitHubConnect_FreshLocalRepo(t *testing.T) {
	t.Parallel()
	m, _, _, _ := connectFixture(t)
	d, err := m.DetectGitHubConnect(context.Background(), "talkback-001")
	if err != nil {
		t.Fatal(err)
	}
	if !d.Eligible || !d.StateRepoIsLocal || !d.ConfigURLIsLocal {
		t.Errorf("fresh local repo should be fully eligible: %+v", d)
	}
	if d.RepoName != "talkback" {
		t.Errorf("RepoName = %q, want talkback", d.RepoName)
	}
	if d.OriginURL != "" || d.RemoteReachable || d.RemoteHasRefs {
		t.Errorf("no origin expected: %+v", d)
	}
	want := []string{ConnectStepSetOrigin, ConnectStepCreateRepo, ConnectStepUpdateConfig, ConnectStepLinkWorkspaces, ConnectStepInitialPush}
	got := neededSteps(BuildConnectPlan(d))
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("plan = %v, want %v", got, want)
	}
}

// The talkback-001 repair case: origin set by hand, history pushed, but
// config/state still say local:.
func TestDetectGitHubConnect_OriginSetManually(t *testing.T) {
	t.Parallel()
	m, _, _, wsPath := connectFixture(t)
	remote := bareRemote(t)
	gitIn(t, wsPath, "remote", "add", "origin", remote)
	gitIn(t, wsPath, "push", "origin", "HEAD:refs/heads/main")

	d, err := m.DetectGitHubConnect(context.Background(), "talkback-001")
	if err != nil {
		t.Fatal(err)
	}
	if d.OriginURL != remote || !d.RemoteReachable || !d.RemoteHasRefs {
		t.Errorf("remote should be reachable with refs: %+v", d)
	}
	want := []string{ConnectStepUpdateConfig, ConnectStepLinkWorkspaces}
	got := neededSteps(BuildConnectPlan(d))
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("plan = %v, want %v", got, want)
	}
}

// Origin set to a repo that exists but has never been pushed to.
func TestDetectGitHubConnect_EmptyRemote(t *testing.T) {
	t.Parallel()
	m, _, _, wsPath := connectFixture(t)
	remote := bareRemote(t)
	gitIn(t, wsPath, "remote", "add", "origin", remote)

	d, err := m.DetectGitHubConnect(context.Background(), "talkback-001")
	if err != nil {
		t.Fatal(err)
	}
	if !d.RemoteReachable || d.RemoteHasRefs {
		t.Errorf("expected reachable empty remote: %+v", d)
	}
	want := []string{ConnectStepUpdateConfig, ConnectStepLinkWorkspaces, ConnectStepInitialPush}
	got := neededSteps(BuildConnectPlan(d))
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("plan = %v, want %v", got, want)
	}
}

// Origin set but pointing at a repo that doesn't exist (deleted, or a
// connect run that died before creating it).
func TestDetectGitHubConnect_UnreachableRemote(t *testing.T) {
	t.Parallel()
	m, _, _, wsPath := connectFixture(t)
	gitIn(t, wsPath, "remote", "add", "origin", filepath.Join(t.TempDir(), "does-not-exist.git"))

	d, err := m.DetectGitHubConnect(context.Background(), "talkback-001")
	if err != nil {
		t.Fatal(err)
	}
	if d.RemoteReachable {
		t.Errorf("remote should be unreachable: %+v", d)
	}
	got := neededSteps(BuildConnectPlan(d))
	want := []string{ConnectStepCreateRepo, ConnectStepUpdateConfig, ConnectStepLinkWorkspaces, ConnectStepInitialPush}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("plan = %v, want %v", got, want)
	}
}

func TestDetectGitHubConnect_FullyConnected(t *testing.T) {
	t.Parallel()
	m, st, cfg, wsPath := connectFixture(t)
	remote := bareRemote(t)
	gitIn(t, wsPath, "remote", "add", "origin", remote)
	cfg.Repos = []config.Repo{{Name: "talkback", URL: remote, BarePath: "talkback.git"}}
	ws, _ := st.GetWorkspace("talkback-001")
	ws.Repo = remote
	st.UpdateWorkspace(ws)

	d, err := m.DetectGitHubConnect(context.Background(), "talkback-001")
	if err != nil {
		t.Fatal(err)
	}
	if d.Eligible {
		t.Errorf("fully connected workspace must not be eligible: %+v", d)
	}
}

// fakeGH stands in for the gh CLI: CreateRepo makes a local bare repo and
// RepoURL returns its path, so the full pipeline (including the push) runs
// for real against the filesystem.
type fakeGH struct {
	base    string
	created []string
	fail    error // when set, CreateRepo returns it
}

func (f *fakeGH) CreateRepo(_ context.Context, owner, name string, private bool) error {
	if f.fail != nil {
		return f.fail
	}
	dir := f.RepoURL(owner, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	cmd := exec.Command("git", "init", "--bare")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bare init: %v: %s", err, out)
	}
	f.created = append(f.created, owner+"/"+name)
	return nil
}

func (f *fakeGH) RepoURL(owner, name string) string {
	return filepath.Join(f.base, owner, name+".git")
}

func stepStatus(res *contracts.GitHubConnectResult, step string) string {
	for _, s := range res.Steps {
		if s.Step == step {
			return s.Status
		}
	}
	return "<missing>"
}

func TestRunGitHubConnect_FreshLocalRepo_AllSteps(t *testing.T) {
	t.Parallel()
	m, st, cfg, wsPath := connectFixture(t)
	gh := &fakeGH{base: t.TempDir()}

	res, err := m.RunGitHubConnect(context.Background(), "talkback-001",
		contracts.GitHubConnectRequest{Owner: "sergeknystautas", Name: "talkback", Visibility: "private", DefaultBranch: "main"}, gh)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("expected success, steps: %+v", res.Steps)
	}
	for _, step := range []string{ConnectStepSetOrigin, ConnectStepCreateRepo, ConnectStepUpdateConfig, ConnectStepLinkWorkspaces, ConnectStepInitialPush} {
		if got := stepStatus(res, step); got != "done" {
			t.Errorf("step %s = %s, want done", step, got)
		}
	}
	// origin now points at the fake repo
	if got := gitIn(t, wsPath, "config", "--get", "remote.origin.url"); got != gh.RepoURL("sergeknystautas", "talkback") {
		t.Errorf("origin = %q", got)
	}
	// config URL flipped, name/bare_path preserved
	repo, found := cfg.FindRepo("talkback")
	if !found || repo.URL != gh.RepoURL("sergeknystautas", "talkback") || repo.BarePath != "talkback.git" {
		t.Errorf("config entry: %+v", repo)
	}
	// workspace relinked
	ws, _ := st.GetWorkspace("talkback-001")
	if ws.Repo != gh.RepoURL("sergeknystautas", "talkback") {
		t.Errorf("workspace repo = %q", ws.Repo)
	}
	// remote default branch created from HEAD, local branch untouched
	remoteRefs := gitIn(t, gh.RepoURL("sergeknystautas", "talkback"), "for-each-ref", "--format=%(refname)")
	if remoteRefs != "refs/heads/main" {
		t.Errorf("remote refs = %q, want only refs/heads/main", remoteRefs)
	}
	if branch := gitIn(t, wsPath, "rev-parse", "--abbrev-ref", "HEAD"); branch != "feature/prompt-branch" {
		t.Errorf("local branch changed to %q", branch)
	}
}

func TestRunGitHubConnect_RepairOnly(t *testing.T) {
	t.Parallel()
	m, st, cfg, wsPath := connectFixture(t)
	remote := bareRemote(t)
	gitIn(t, wsPath, "remote", "add", "origin", remote)
	gitIn(t, wsPath, "push", "origin", "HEAD:refs/heads/main")

	res, err := m.RunGitHubConnect(context.Background(), "talkback-001", contracts.GitHubConnectRequest{}, &fakeGH{base: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("steps: %+v", res.Steps)
	}
	if got := stepStatus(res, ConnectStepSetOrigin); got != "skipped" {
		t.Errorf("set_origin = %s, want skipped", got)
	}
	if got := stepStatus(res, ConnectStepCreateRepo); got != "skipped" {
		t.Errorf("create_repo = %s, want skipped", got)
	}
	if got := stepStatus(res, ConnectStepInitialPush); got != "skipped" {
		t.Errorf("initial_push = %s, want skipped", got)
	}
	// verbatim origin URL adopted into config and state
	repo, _ := cfg.FindRepo("talkback")
	if repo.URL != remote {
		t.Errorf("config URL = %q, want %q (verbatim origin)", repo.URL, remote)
	}
	ws, _ := st.GetWorkspace("talkback-001")
	if ws.Repo != remote {
		t.Errorf("state repo = %q, want %q", ws.Repo, remote)
	}
}

func TestRunGitHubConnect_CreateFails_StopsAndReportsNotRun(t *testing.T) {
	t.Parallel()
	m, st, cfg, _ := connectFixture(t)
	gh := &fakeGH{base: t.TempDir(), fail: fmt.Errorf("name already exists")}

	res, err := m.RunGitHubConnect(context.Background(), "talkback-001",
		contracts.GitHubConnectRequest{Owner: "sergeknystautas", Name: "talkback"}, gh)
	if err != nil {
		t.Fatal(err)
	}
	if res.Success {
		t.Error("expected failure")
	}
	if got := stepStatus(res, ConnectStepCreateRepo); got != "failed" {
		t.Errorf("create_repo = %s, want failed", got)
	}
	for _, step := range []string{ConnectStepUpdateConfig, ConnectStepLinkWorkspaces, ConnectStepInitialPush} {
		if got := stepStatus(res, step); got != "not_run" {
			t.Errorf("step %s = %s, want not_run", step, got)
		}
	}
	// config and state untouched
	repo, _ := cfg.FindRepo("talkback")
	if repo.URL != "local:talkback" {
		t.Errorf("config must be untouched, got %q", repo.URL)
	}
	ws, _ := st.GetWorkspace("talkback-001")
	if ws.Repo != "local:talkback" {
		t.Errorf("state must be untouched, got %q", ws.Repo)
	}
}

// Re-running after a failed create with a different name repoints origin.
func TestRunGitHubConnect_RetryWithNewName_SetsURL(t *testing.T) {
	t.Parallel()
	m, _, _, wsPath := connectFixture(t)
	gh := &fakeGH{base: t.TempDir()}

	// First run fails at create; origin is left pointing at the taken name.
	gh.fail = fmt.Errorf("name already exists")
	if _, err := m.RunGitHubConnect(context.Background(), "talkback-001",
		contracts.GitHubConnectRequest{Owner: "sergeknystautas", Name: "talkback"}, gh); err != nil {
		t.Fatal(err)
	}
	// Second run with a new name succeeds and repoints origin.
	gh.fail = nil
	res, err := m.RunGitHubConnect(context.Background(), "talkback-001",
		contracts.GitHubConnectRequest{Owner: "sergeknystautas", Name: "talkback2", DefaultBranch: "main"}, gh)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("steps: %+v", res.Steps)
	}
	if got := gitIn(t, wsPath, "config", "--get", "remote.origin.url"); got != gh.RepoURL("sergeknystautas", "talkback2") {
		t.Errorf("origin = %q, want repointed URL", got)
	}
}

// Origin URL already registered under another config entry → merge, no duplicate.
func TestRunGitHubConnect_MergesIntoExistingConfigEntry(t *testing.T) {
	t.Parallel()
	m, st, cfg, wsPath := connectFixture(t)
	remote := bareRemote(t)
	gitIn(t, wsPath, "remote", "add", "origin", remote)
	gitIn(t, wsPath, "push", "origin", "HEAD:refs/heads/main")
	if err := cfg.AddRepo(config.Repo{Name: "existing", URL: remote, BarePath: "existing.git"}); err != nil {
		t.Fatal(err)
	}

	res, err := m.RunGitHubConnect(context.Background(), "talkback-001", contracts.GitHubConnectRequest{}, &fakeGH{base: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("steps: %+v", res.Steps)
	}
	if _, found := cfg.FindRepo("talkback"); found {
		t.Error("local: entry should be deleted (merged into existing)")
	}
	count := 0
	for _, r := range cfg.GetRepos() {
		if r.URL == remote {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 entry for %q, got %d", remote, count)
	}
	ws, _ := st.GetWorkspace("talkback-001")
	if ws.Repo != remote {
		t.Errorf("workspace should link to merged URL, got %q", ws.Repo)
	}
}

func TestRunGitHubConnect_MissingTarget(t *testing.T) {
	t.Parallel()
	m, _, _, _ := connectFixture(t)
	_, err := m.RunGitHubConnect(context.Background(), "talkback-001", contracts.GitHubConnectRequest{}, &fakeGH{base: t.TempDir()})
	if !errors.Is(err, ErrConnectMissingTarget) {
		t.Errorf("expected ErrConnectMissingTarget, got %v", err)
	}
}

func TestRunGitHubConnect_NotEligible(t *testing.T) {
	t.Parallel()
	m, st, cfg, _ := connectFixture(t)
	cfg.Repos = []config.Repo{{Name: "talkback", URL: "https://github.com/x/y", BarePath: "talkback.git"}}
	ws, _ := st.GetWorkspace("talkback-001")
	ws.Repo = "https://github.com/x/y"
	st.UpdateWorkspace(ws)
	_, err := m.RunGitHubConnect(context.Background(), "talkback-001", contracts.GitHubConnectRequest{}, &fakeGH{base: t.TempDir()})
	if !errors.Is(err, ErrNotConnectEligible) {
		t.Errorf("expected ErrNotConnectEligible, got %v", err)
	}
}
