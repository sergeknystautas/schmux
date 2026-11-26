package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// writePending writes the pending file with one JSON line per entry.
func writePending(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir for pending file: %v", err)
	}
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write pending file: %v", err)
	}
}

// repoLine renders an entry as one pending-file line.
func repoLine(t *testing.T, r state.Repo) string {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal repo: %v", err)
	}
	return string(data)
}

func TestAddPending_NoFile(t *testing.T) {
	m, _, _, _ := newTestManager(t)

	if err := m.AddPending(filepath.Join(t.TempDir(), "pending-repos.jsonl")); err != nil {
		t.Errorf("AddPending with no pending file: err = %v, want nil", err)
	}
}

func TestAddPending_EveryField(t *testing.T) {
	m, _, _, _ := newTestManager(t)
	pending := filepath.Join(t.TempDir(), "pending-repos.jsonl")

	first := state.Repo{
		Name:                  "my.repo",
		URL:                   "https://github.com/acme/my.repo.git",
		BarePath:              filepath.Join("owner", "x.git"),
		VCS:                   "git",
		OverlayPaths:          []string{"x", "y"},
		OverlayNudgeDismissed: true,
		GitHubLogin:           "octocat",
	}
	second := first
	second.Name = "other"

	writePending(t, pending, repoLine(t, first), repoLine(t, second))

	if err := m.AddPending(pending); err != nil {
		t.Fatalf("AddPending: %v", err)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Error("pending file should be gone")
	}

	entries := m.List()
	if len(entries) != 2 {
		t.Fatalf("stored entries = %d, want 2", len(entries))
	}
	if !reflect.DeepEqual(entries[0], first) {
		t.Errorf("stored entry = %+v, want %+v", entries[0], first)
	}
	if !reflect.DeepEqual(entries[1], second) {
		t.Errorf("stored entry = %+v, want %+v", entries[1], second)
	}

	got, ok := m.GetByURL(first.URL)
	if !ok {
		t.Fatal("GetByURL: not found")
	}
	if got.Name != "other" {
		t.Errorf("GetByURL(%q).Name = %q, want %q", first.URL, got.Name, "other")
	}
}

func TestAddPending_NameStored(t *testing.T) {
	m, _, _, _ := newTestManager(t)

	if _, err := m.Add(state.Repo{Name: "app", URL: "https://github.com/acme/app.git"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	pending := filepath.Join(t.TempDir(), "pending-repos.jsonl")
	writePending(t, pending, `{"name":"app","url":"https://github.com/other/app.git"}`)

	if err := m.AddPending(pending); err != nil {
		t.Fatalf("AddPending: %v", err)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Error("pending file should be gone")
	}

	stored, _ := m.Get("app")
	if stored.URL != "https://github.com/acme/app.git" {
		t.Errorf("stored URL = %q, want the first entry's URL", stored.URL)
	}
	if got := m.List(); len(got) != 1 {
		t.Errorf("stored entries = %d, want 1", len(got))
	}
}

func TestAddPending_SameNameTwice(t *testing.T) {
	m, _, _, _ := newTestManager(t)
	pending := filepath.Join(t.TempDir(), "pending-repos.jsonl")

	writePending(t, pending,
		`{"name":"app","url":"https://github.com/acme/app.git"}`,
		`{"name":"app","url":"https://github.com/other/app.git"}`,
	)

	if err := m.AddPending(pending); err != nil {
		t.Fatalf("AddPending: %v", err)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Error("pending file should be gone")
	}

	entries := m.List()
	if len(entries) != 1 {
		t.Fatalf("stored entries = %d, want 1", len(entries))
	}
	if entries[0].URL != "https://github.com/acme/app.git" {
		t.Errorf("stored URL = %q, want the first line's URL", entries[0].URL)
	}
}

func TestAddPending_BadLine(t *testing.T) {
	m, _, _, _ := newTestManager(t)
	pending := filepath.Join(t.TempDir(), "pending-repos.jsonl")

	writePending(t, pending,
		`{"name":"first","url":"https://github.com/acme/first.git"}`,
		``,
		`{"name":"broken","url":`,
		``,
		`{"name":"second","url":"https://github.com/acme/second.git"}`,
	)

	if err := m.AddPending(pending); err != nil {
		t.Fatalf("AddPending: %v", err)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Error("pending file should be gone")
	}

	for _, name := range []string{"first", "second"} {
		if _, ok := m.Get(name); !ok {
			t.Errorf("Get(%s): not found", name)
		}
	}
	if got := m.List(); len(got) != 2 {
		t.Errorf("stored entries = %d, want 2", len(got))
	}
}

func TestAddPending_LongLine(t *testing.T) {
	m, _, _, _ := newTestManager(t)
	pending := filepath.Join(t.TempDir(), "pending-repos.jsonl")

	paths := make([]string, 0, 700)
	for i := 0; i < 700; i++ {
		paths = append(paths, strings.Repeat("p", 100))
	}
	want := state.Repo{
		Name:         "big",
		URL:          "https://github.com/acme/big.git",
		BarePath:     "big.git",
		VCS:          "git",
		OverlayPaths: paths,
	}
	writePending(t, pending, repoLine(t, want))

	if err := m.AddPending(pending); err != nil {
		t.Fatalf("AddPending: %v", err)
	}
	got, ok := m.Get("big")
	if !ok {
		t.Fatal("Get(big): not found")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stored entry = %+v, want %+v", got, want)
	}
}

func TestAddPending_SaveFails(t *testing.T) {
	dir := t.TempDir()

	// The state file's parent is a regular file, so saving the state fails.
	blocker := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	cfg := config.CreateDefault(filepath.Join(dir, "config.json"))
	cfg.WorktreeBasePath = filepath.Join(dir, "repos")
	st := state.New(filepath.Join(blocker, "state.json"), nil)
	m := New(cfg, st, nil)

	pending := filepath.Join(dir, "pending-repos.jsonl")
	writePending(t, pending, `{"name":"app","url":"https://github.com/acme/app.git"}`)

	if err := m.AddPending(pending); err == nil {
		t.Error("AddPending with an unwritable state: want error, got nil")
	}
	if _, err := os.Stat(pending); err != nil {
		t.Errorf("pending file should be kept: %v", err)
	}

	// With the parent replaced by a directory, the next run succeeds and the
	// pending file is gone.
	if err := os.Remove(blocker); err != nil {
		t.Fatalf("remove blocker: %v", err)
	}
	if err := os.MkdirAll(blocker, 0755); err != nil {
		t.Fatalf("mkdir blocker: %v", err)
	}
	if err := m.AddPending(pending); err != nil {
		t.Fatalf("AddPending after the state path is writable: %v", err)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Error("pending file should be gone")
	}
}
