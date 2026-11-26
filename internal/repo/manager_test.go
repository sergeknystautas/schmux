package repo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// newTestManager builds a manager over a temporary state file and a temporary
// worktree base path, so nothing it touches is the developer's own. It returns
// the manager, the state store, the config, and the state file's path.
func newTestManager(t *testing.T) (*Manager, *state.State, *config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.CreateDefault(filepath.Join(dir, "config.json"))
	cfg.WorktreeBasePath = filepath.Join(dir, "repos")
	statePath := filepath.Join(dir, "state.json")
	st := state.New(statePath, nil)
	return New(cfg, st, nil), st, cfg, statePath
}

// createBareRepo creates a bare repo at path whose origin is origin.
func createBareRepo(t *testing.T, path, origin string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	cmd := exec.Command("git", "init", "--bare", "-b", "main")
	cmd.Dir = path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare in %s: %v\n%s", path, err, out)
	}
	if origin == "" {
		return
	}
	cmd = exec.Command("git", "config", "remote.origin.url", origin)
	cmd.Dir = path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config in %s: %v\n%s", path, err, out)
	}
}

func TestAdd_EmptyURL(t *testing.T) {
	m, _, _, _ := newTestManager(t)

	_, err := m.Add(state.Repo{Name: "app"})
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("Add with an empty URL: err = %v, want ErrInvalid", err)
	}
	if got := m.List(); len(got) != 0 {
		t.Errorf("stored entries = %d, want 0", len(got))
	}
}

func TestAdd_GivenName(t *testing.T) {
	m, _, _, _ := newTestManager(t)

	entry, err := m.Add(state.Repo{Name: "my.repo", URL: "https://github.com/acme/my.repo.git"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if entry.Name != "my.repo" {
		t.Errorf("Name = %q, want %q", entry.Name, "my.repo")
	}
	if entry.BarePath != "my.repo.git" {
		t.Errorf("BarePath = %q, want %q", entry.BarePath, "my.repo.git")
	}
	stored, ok := m.Get("my.repo")
	if !ok {
		t.Fatal("Get(my.repo): not found")
	}
	if stored.URL != "https://github.com/acme/my.repo.git" {
		t.Errorf("stored URL = %q, want the added URL", stored.URL)
	}
}

func TestAdd_DuplicateName(t *testing.T) {
	m, _, _, _ := newTestManager(t)

	if _, err := m.Add(state.Repo{Name: "app", URL: "https://github.com/acme/app.git"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	_, err := m.Add(state.Repo{Name: "app", URL: "https://github.com/other/app.git"})
	if !errors.Is(err, ErrDuplicateName) {
		t.Errorf("Add with a duplicate name: err = %v, want ErrDuplicateName", err)
	}
	stored, _ := m.Get("app")
	if stored.URL != "https://github.com/acme/app.git" {
		t.Errorf("stored URL = %q, want the first entry's URL", stored.URL)
	}
	if got := m.List(); len(got) != 1 {
		t.Errorf("stored entries = %d, want 1", len(got))
	}
}

func TestAdd_DerivedName(t *testing.T) {
	m, _, _, _ := newTestManager(t)

	first, err := m.Add(state.Repo{URL: "https://github.com/acme/app.git"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if first.Name != "app" {
		t.Errorf("derived name = %q, want %q", first.Name, "app")
	}

	second, err := m.Add(state.Repo{URL: "https://github.com/other/app.git"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if second.Name != "other-app" {
		t.Errorf("derived name = %q, want %q", second.Name, "other-app")
	}
}

func TestAdd_AvoidsForeignBase(t *testing.T) {
	t.Run("foreign origin", func(t *testing.T) {
		m, _, cfg, _ := newTestManager(t)
		createBareRepo(t, filepath.Join(cfg.GetWorktreeBasePath(), "app.git"), "https://github.com/other/app.git")

		entry, err := m.Add(state.Repo{URL: "https://github.com/acme/app.git"})
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		if entry.Name != "acme-app" {
			t.Errorf("derived name = %q, want %q", entry.Name, "acme-app")
		}
	})

	t.Run("same origin", func(t *testing.T) {
		m, _, cfg, _ := newTestManager(t)
		createBareRepo(t, filepath.Join(cfg.GetWorktreeBasePath(), "app.git"), "https://github.com/acme/app.git")

		entry, err := m.Add(state.Repo{URL: "https://github.com/acme/app.git"})
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		if entry.Name != "app" {
			t.Errorf("derived name = %q, want %q", entry.Name, "app")
		}
	})
}

func TestAdd_UnderivableName(t *testing.T) {
	m, _, _, _ := newTestManager(t)

	_, err := m.Add(state.Repo{URL: "https://host/..."})
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("Add with an underivable name: err = %v, want ErrInvalid", err)
	}
	if got := m.List(); len(got) != 0 {
		t.Errorf("stored entries = %d, want 0", len(got))
	}
}

func TestAdd_BarePath(t *testing.T) {
	m, _, _, _ := newTestManager(t)

	given, err := m.Add(state.Repo{Name: "a", URL: "https://host/a", BarePath: "custom.git"})
	if err != nil {
		t.Fatalf("Add(a): %v", err)
	}
	if given.BarePath != "custom.git" {
		t.Errorf("BarePath = %q, want %q", given.BarePath, "custom.git")
	}

	sapling, err := m.Add(state.Repo{Name: "s", URL: "https://host/s", VCS: "sapling"})
	if err != nil {
		t.Fatalf("Add(s): %v", err)
	}
	if sapling.BarePath != "s" {
		t.Errorf("BarePath = %q, want %q", sapling.BarePath, "s")
	}

	// A second entry with a stored URL takes the first entry's bare path.
	second, err := m.Add(state.Repo{Name: "a2", URL: "https://host/a"})
	if err != nil {
		t.Fatalf("Add(a2): %v", err)
	}
	if second.BarePath != "custom.git" {
		t.Errorf("BarePath = %q, want %q", second.BarePath, "custom.git")
	}
}

func TestAdd_KeepsEveryField(t *testing.T) {
	m, st, _, _ := newTestManager(t)

	want := state.Repo{
		Name:                  "a",
		URL:                   "https://github.com/acme/app.git",
		BarePath:              "owner/app.git",
		VCS:                   "git",
		OverlayPaths:          []string{"x", "y"},
		OverlayNudgeDismissed: true,
		GitHubLogin:           "octocat",
	}
	got, err := m.Add(want)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Add returned %+v, want %+v", got, want)
	}

	stored, ok := m.Get("a")
	if !ok {
		t.Fatal("Get(a): not found")
	}
	if !reflect.DeepEqual(stored, want) {
		t.Errorf("stored entry = %+v, want %+v", stored, want)
	}
	if !reflect.DeepEqual(st.GetRepos(), []state.Repo{want}) {
		t.Errorf("state entries = %+v, want [%+v]", st.GetRepos(), want)
	}
}

func TestAdd_ConcurrentSameName(t *testing.T) {
	m, _, _, _ := newTestManager(t)

	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := m.Add(state.Repo{Name: "app", URL: "https://github.com/acme/app.git"})
			errs[i] = err
		}(i)
	}
	close(start)
	wg.Wait()

	var okCount, dupCount int
	for _, err := range errs {
		switch {
		case err == nil:
			okCount++
		case errors.Is(err, ErrDuplicateName):
			dupCount++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if okCount != 1 || dupCount != 1 {
		t.Errorf("got %d successful adds and %d duplicate-name errors, want 1 each", okCount, dupCount)
	}
	if got := m.List(); len(got) != 1 {
		t.Errorf("stored entries = %d, want 1", len(got))
	}
}

func TestAdd_SurvivesConfigReload(t *testing.T) {
	m, _, cfg, _ := newTestManager(t)

	if _, err := m.Add(state.Repo{Name: "app", URL: "https://github.com/acme/app.git"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save config: %v", err)
	}
	if err := cfg.Reload(); err != nil {
		t.Fatalf("Reload config: %v", err)
	}
	if _, ok := m.Get("app"); !ok {
		t.Error("Get(app) after a config reload: not found")
	}
}

func TestUpdates(t *testing.T) {
	m, _, _, statePath := newTestManager(t)

	if _, err := m.Add(state.Repo{Name: "app", URL: "https://github.com/acme/app.git"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := m.SetURL("app", "https://github.com/other/app.git"); err != nil {
		t.Fatalf("SetURL: %v", err)
	}
	entry, _ := m.Get("app")
	if entry.URL != "https://github.com/other/app.git" {
		t.Errorf("URL = %q, want the new URL", entry.URL)
	}
	if entry.BarePath != "app.git" {
		t.Errorf("BarePath = %q, want it unchanged", entry.BarePath)
	}

	if err := m.AddOverlayPaths("app", []string{"x", "y"}); err != nil {
		t.Fatalf("AddOverlayPaths: %v", err)
	}
	if err := m.AddOverlayPaths("app", []string{"y", "z"}); err != nil {
		t.Fatalf("AddOverlayPaths: %v", err)
	}
	entry, _ = m.Get("app")
	if !reflect.DeepEqual(entry.OverlayPaths, []string{"x", "y", "z"}) {
		t.Errorf("OverlayPaths = %v, want [x y z]", entry.OverlayPaths)
	}

	if err := m.DismissOverlayNudge("app"); err != nil {
		t.Fatalf("DismissOverlayNudge: %v", err)
	}
	entry, _ = m.Get("app")
	if !entry.OverlayNudgeDismissed {
		t.Error("OverlayNudgeDismissed = false, want true")
	}

	// An unknown name is not found.
	if err := m.SetURL("missing", "https://host/x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetURL on an unknown name: err = %v, want ErrNotFound", err)
	}
	if err := m.AddOverlayPaths("missing", []string{"x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("AddOverlayPaths on an unknown name: err = %v, want ErrNotFound", err)
	}
	if err := m.DismissOverlayNudge("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("DismissOverlayNudge on an unknown name: err = %v, want ErrNotFound", err)
	}

	// The changes are on disk.
	reloaded, err := state.Load(statePath, nil)
	if err != nil {
		t.Fatalf("Load state: %v", err)
	}
	stored, ok := reloaded.GetRepo("app")
	if !ok {
		t.Fatal("GetRepo(app) after Load: not found")
	}
	if stored.URL != "https://github.com/other/app.git" {
		t.Errorf("stored URL = %q, want the new URL", stored.URL)
	}
	if !reflect.DeepEqual(stored.OverlayPaths, []string{"x", "y", "z"}) {
		t.Errorf("stored OverlayPaths = %v, want [x y z]", stored.OverlayPaths)
	}
	if !stored.OverlayNudgeDismissed {
		t.Error("stored OverlayNudgeDismissed = false, want true")
	}
}

func TestSetURL_Empty(t *testing.T) {
	m, _, _, _ := newTestManager(t)

	if _, err := m.Add(state.Repo{Name: "app", URL: "https://github.com/acme/app.git"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	err := m.SetURL("app", "")
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("SetURL with an empty URL: err = %v, want ErrInvalid", err)
	}
	entry, _ := m.Get("app")
	if entry.URL != "https://github.com/acme/app.git" {
		t.Errorf("URL = %q, want it unchanged", entry.URL)
	}
}

func TestRemove(t *testing.T) {
	m, _, _, _ := newTestManager(t)

	if _, err := m.Add(state.Repo{Name: "app", URL: "https://github.com/acme/app.git"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	other := state.Repo{Name: "other", URL: "https://github.com/acme/other.git", BarePath: "other.git"}
	if _, err := m.Add(other); err != nil {
		t.Fatalf("Add(other): %v", err)
	}

	if err := m.Remove("app"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok := m.Get("app"); ok {
		t.Error("Get(app) after Remove: still found")
	}
	remaining := m.List()
	if len(remaining) != 1 || !reflect.DeepEqual(remaining[0], other) {
		t.Errorf("remaining entries = %+v, want [%+v]", remaining, other)
	}

	if err := m.Remove("app"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove on an unknown name: err = %v, want ErrNotFound", err)
	}
}
