package daemon

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/repo"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/telemetry"
)

// migrationEnv is a schmux directory with a config file at configPath.
type migrationEnv struct {
	dir        string
	configPath string
	statePath  string
}

func newMigrationEnv(t *testing.T, configJSON string) migrationEnv {
	t.Helper()
	dir := t.TempDir()
	schmuxdir.Set(dir)
	t.Cleanup(func() { schmuxdir.Set("") })
	env := migrationEnv{
		dir:        dir,
		configPath: filepath.Join(dir, "config.json"),
		statePath:  filepath.Join(dir, "state.json"),
	}
	env.writeConfig(t, configJSON)
	return env
}

func (e migrationEnv) writeConfig(t *testing.T, configJSON string) {
	t.Helper()
	body := strings.ReplaceAll(configJSON, "WS", filepath.Join(e.dir, "workspaces"))
	if err := os.WriteFile(e.configPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// start loads config and state and builds the managers, as the daemon does.
func (e migrationEnv) start(t *testing.T, cfg *config.Config) *repo.Manager {
	t.Helper()
	logger := log.NewWithOptions(io.Discard, log.Options{})
	st, err := state.Load(e.statePath, logger)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	d := &Daemon{}
	repos, _, _, _ := d.initManagers(cfg, st, nil, e.statePath, e.dir, &telemetry.NoopTelemetry{}, e.dir, logger, logger, logger)
	return repos
}

// assertMoved checks that the entries are in state.json, and that neither the
// pending file nor a repos key in config.json is left.
func (e migrationEnv) assertMoved(t *testing.T, want []state.Repo) {
	t.Helper()
	onDisk, err := state.Load(e.statePath, nil)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if got := onDisk.GetRepos(); !reflect.DeepEqual(got, want) {
		t.Errorf("state.json repos = %+v, want %+v", got, want)
	}
	if _, err := os.Stat(config.PendingReposPath(e.configPath)); !os.IsNotExist(err) {
		t.Errorf("pending file should be gone, stat err = %v", err)
	}
	data, err := os.ReadFile(e.configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if _, ok := top["repos"]; ok {
		t.Error("config.json still has a repos key")
	}
}

func (e migrationEnv) backups(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(e.configPath + ".*.bak")
	if err != nil {
		t.Fatalf("glob backups: %v", err)
	}
	return matches
}

// TestInitManagers_MovesConfigReposIntoState runs the migration the way a
// `schmux start` does: the parent process loads the config, then the daemon
// loads it again and builds its managers.
func TestInitManagers_MovesConfigReposIntoState(t *testing.T) {
	env := newMigrationEnv(t, `{
  "workspace_path": "WS",
  "repos": [
    {"name": "my.repo", "url": "https://github.com/acme/app.git", "bare_path": "acme/app.git",
     "overlay_paths": [".env"], "overlay_nudge_dismissed": true, "github_login": "octocat"},
    {"name": "app-clone", "url": "https://github.com/acme/app.git", "vcs": "git-clone"}
  ]
}`)

	if _, err := config.Load(env.configPath); err != nil {
		t.Fatalf("parent config.Load: %v", err)
	}
	cfg, err := config.Load(env.configPath)
	if err != nil {
		t.Fatalf("daemon config.Load: %v", err)
	}
	repos := env.start(t, cfg)

	want := []state.Repo{
		{Name: "my.repo", URL: "https://github.com/acme/app.git", BarePath: "acme/app.git",
			OverlayPaths: []string{".env"}, OverlayNudgeDismissed: true, GitHubLogin: "octocat"},
		{Name: "app-clone", URL: "https://github.com/acme/app.git", BarePath: "acme/app.git", VCS: "git-clone"},
	}
	if got := repos.List(); !reflect.DeepEqual(got, want) {
		t.Errorf("manager repos = %+v, want %+v", got, want)
	}
	env.assertMoved(t, want)
	if got := env.backups(t); len(got) != 1 {
		t.Errorf("backups = %v, want exactly one", got)
	}
}

// TestReload_AddsHandEditedRepos proves a repos key written into config.json
// while the daemon runs ends up in the manager after the reload.
func TestReload_AddsHandEditedRepos(t *testing.T) {
	env := newMigrationEnv(t, `{"workspace_path": "WS"}`)
	cfg, err := config.Load(env.configPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	repos := env.start(t, cfg)

	env.writeConfig(t, `{"workspace_path": "WS", "repos": [{"name": "late", "url": "https://github.com/acme/late.git"}]}`)
	if err := cfg.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	want := []state.Repo{{Name: "late", URL: "https://github.com/acme/late.git", BarePath: "late.git"}}
	if got := repos.List(); !reflect.DeepEqual(got, want) {
		t.Errorf("manager repos = %+v, want %+v", got, want)
	}
	env.assertMoved(t, want)
}

// TestInitManagers_AddsReposAfterInvalidConfigIsFixed proves the repos taken
// out of a config that then fails validation are not lost: the next start
// after the config is fixed adds them.
func TestInitManagers_AddsReposAfterInvalidConfigIsFixed(t *testing.T) {
	env := newMigrationEnv(t, `{
  "workspace_path": "WS",
  "run_targets": [{"name": "", "command": "echo hi"}],
  "repos": [{"name": "app", "url": "https://github.com/acme/app.git"}]
}`)
	if _, err := config.Load(env.configPath); err == nil {
		t.Fatal("config.Load with an invalid run target: want error, got nil")
	}

	env.writeConfig(t, `{"workspace_path": "WS", "run_targets": [{"name": "t", "command": "echo hi"}]}`)
	cfg, err := config.Load(env.configPath)
	if err != nil {
		t.Fatalf("config.Load after the fix: %v", err)
	}
	repos := env.start(t, cfg)

	want := []state.Repo{{Name: "app", URL: "https://github.com/acme/app.git", BarePath: "app.git"}}
	if got := repos.List(); !reflect.DeepEqual(got, want) {
		t.Errorf("manager repos = %+v, want %+v", got, want)
	}
	env.assertMoved(t, want)
}
