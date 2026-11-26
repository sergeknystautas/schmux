package state

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestRepos_AddGetUpdateRemove(t *testing.T) {
	s := New("", nil)

	for _, name := range []string{"a", "b", "c"} {
		if err := s.AddRepo(Repo{Name: name, URL: "https://example.com/" + name + ".git"}); err != nil {
			t.Fatalf("AddRepo(%s): %v", name, err)
		}
	}

	// Order is kept.
	var names []string
	for _, r := range s.GetRepos() {
		names = append(names, r.Name)
	}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Errorf("GetRepos() names = %v, want [a b c]", names)
	}

	// A second entry with the same name is rejected, and the first is kept.
	if err := s.AddRepo(Repo{Name: "a", URL: "https://example.com/other.git"}); err == nil {
		t.Error("AddRepo with a duplicate name: want error, got nil")
	}
	got, ok := s.GetRepo("a")
	if !ok {
		t.Fatal("GetRepo(a): not found")
	}
	if got.URL != "https://example.com/a.git" {
		t.Errorf("GetRepo(a).URL = %q, want the first entry's URL", got.URL)
	}

	// Update replaces the entry with that name.
	got.URL = "https://example.com/a2.git"
	got.BarePath = "a.git"
	if err := s.UpdateRepo(got); err != nil {
		t.Fatalf("UpdateRepo(a): %v", err)
	}
	updated, ok := s.GetRepo("a")
	if !ok {
		t.Fatal("GetRepo(a) after update: not found")
	}
	if updated.URL != "https://example.com/a2.git" || updated.BarePath != "a.git" {
		t.Errorf("GetRepo(a) after update = %+v, want the updated fields", updated)
	}

	// Unknown names error.
	if err := s.UpdateRepo(Repo{Name: "missing"}); err == nil {
		t.Error("UpdateRepo on an unknown name: want error, got nil")
	}
	if err := s.RemoveRepo("missing"); err == nil {
		t.Error("RemoveRepo on an unknown name: want error, got nil")
	}

	// Remove drops one entry and keeps the order of the rest.
	if err := s.RemoveRepo("a"); err != nil {
		t.Fatalf("RemoveRepo(a): %v", err)
	}
	if _, ok := s.GetRepo("a"); ok {
		t.Error("GetRepo(a) after removal: still found")
	}
	names = nil
	for _, r := range s.GetRepos() {
		names = append(names, r.Name)
	}
	if !reflect.DeepEqual(names, []string{"b", "c"}) {
		t.Errorf("GetRepos() names after removal = %v, want [b c]", names)
	}
}

func TestRepos_GetByURLReturnsLast(t *testing.T) {
	s := New("", nil)

	const url = "https://example.com/shared.git"
	if err := s.AddRepo(Repo{Name: "first", URL: url}); err != nil {
		t.Fatalf("AddRepo(first): %v", err)
	}
	if err := s.AddRepo(Repo{Name: "second", URL: url}); err != nil {
		t.Fatalf("AddRepo(second): %v", err)
	}

	got, ok := s.GetRepoByURL(url)
	if !ok {
		t.Fatal("GetRepoByURL: not found")
	}
	if got.Name != "second" {
		t.Errorf("GetRepoByURL(%q).Name = %q, want %q", url, got.Name, "second")
	}

	if _, ok := s.GetRepoByURL("https://example.com/absent.git"); ok {
		t.Error("GetRepoByURL on an unknown URL: want not found")
	}
}

func TestRepos_Copies(t *testing.T) {
	s := New("", nil)

	entry := Repo{
		Name:         "a",
		URL:          "https://example.com/a.git",
		OverlayPaths: []string{"one"},
	}
	if err := s.AddRepo(entry); err != nil {
		t.Fatalf("AddRepo(a): %v", err)
	}

	// The caller's slice is not aliased by the store.
	entry.OverlayPaths[0] = "mutated"
	stored, ok := s.GetRepo("a")
	if !ok {
		t.Fatal("GetRepo(a): not found")
	}
	if stored.OverlayPaths[0] != "one" {
		t.Errorf("GetRepo(a).OverlayPaths[0] = %q after mutating the caller's slice, want %q", stored.OverlayPaths[0], "one")
	}

	// The store hands out copies.
	list := s.GetRepos()
	list[0].OverlayPaths[0] = "mutated-list"
	byName, _ := s.GetRepo("a")
	if byName.OverlayPaths[0] != "one" {
		t.Errorf("GetRepo(a).OverlayPaths[0] = %q after mutating GetRepos' copy, want %q", byName.OverlayPaths[0], "one")
	}
	byURL, _ := s.GetRepoByURL("https://example.com/a.git")
	if byURL.OverlayPaths[0] != "one" {
		t.Errorf("GetRepoByURL().OverlayPaths[0] = %q after mutating GetRepos' copy, want %q", byURL.OverlayPaths[0], "one")
	}
}

func TestRepos_SurviveSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := New(path, nil)

	want := Repo{
		Name:                  "a",
		URL:                   "https://example.com/a.git",
		BarePath:              "a.git",
		VCS:                   "git",
		OverlayPaths:          []string{"x", "y"},
		OverlayNudgeDismissed: true,
		GitHubLogin:           "octocat",
	}
	if err := s.AddRepo(want); err != nil {
		t.Fatalf("AddRepo(a): %v", err)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := loaded.GetRepo("a")
	if !ok {
		t.Fatal("GetRepo(a) after Load: not found")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetRepo(a) after Load = %+v, want %+v", got, want)
	}
}
