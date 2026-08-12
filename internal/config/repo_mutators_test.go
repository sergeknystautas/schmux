package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func newSavableConfig(t *testing.T) *Config {
	t.Helper()
	cfg := CreateDefault(filepath.Join(t.TempDir(), "config.json"))
	cfg.Repos = []Repo{
		{Name: "talkback", URL: "local:talkback", BarePath: "talkback.git"},
		{Name: "other", URL: "https://github.com/x/other", BarePath: "other.git"},
	}
	return cfg
}

func TestSetRepoURL(t *testing.T) {
	cfg := newSavableConfig(t)
	// Prime the URL cache so we can prove it gets invalidated.
	if _, found := cfg.FindRepoByURL("local:talkback"); !found {
		t.Fatal("precondition: local:talkback should be findable")
	}

	if err := cfg.SetRepoURL("talkback", "https://github.com/sergeknystautas/talkback.git"); err != nil {
		t.Fatalf("SetRepoURL: %v", err)
	}
	repo, found := cfg.FindRepo("talkback")
	if !found || repo.URL != "https://github.com/sergeknystautas/talkback.git" {
		t.Errorf("URL not updated: %+v", repo)
	}
	if repo.BarePath != "talkback.git" {
		t.Errorf("bare_path must be preserved, got %q", repo.BarePath)
	}
	if _, found := cfg.FindRepoByURL("local:talkback"); found {
		t.Error("stale URL cache: local:talkback still resolves")
	}
	if _, found := cfg.FindRepoByURL("https://github.com/sergeknystautas/talkback.git"); !found {
		t.Error("new URL not findable — cache not rebuilt")
	}
}

func TestSetRepoURL_NotFound(t *testing.T) {
	cfg := newSavableConfig(t)
	if err := cfg.SetRepoURL("nope", "https://github.com/x/y"); err == nil {
		t.Error("expected error for unknown repo name")
	}
}

func TestRemoveRepo(t *testing.T) {
	cfg := newSavableConfig(t)
	if err := cfg.RemoveRepo("talkback"); err != nil {
		t.Fatalf("RemoveRepo: %v", err)
	}
	if _, found := cfg.FindRepo("talkback"); found {
		t.Error("repo should be gone")
	}
	if len(cfg.GetRepos()) != 1 {
		t.Errorf("expected 1 repo left, got %d", len(cfg.GetRepos()))
	}
	if err := cfg.RemoveRepo("talkback"); err == nil {
		t.Error("expected error removing a missing repo")
	}
}

func TestAddRepo(t *testing.T) {
	cfg := newSavableConfig(t)
	err := cfg.AddRepo(Repo{Name: "newone", URL: "https://github.com/x/newone", BarePath: "newone.git"})
	if err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	if _, found := cfg.FindRepo("newone"); !found {
		t.Error("added repo not findable")
	}
	if err := cfg.AddRepo(Repo{Name: "newone", URL: "https://github.com/x/dup"}); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Errorf("expected duplicate-name error, got %v", err)
	}
	if err := cfg.AddRepo(Repo{Name: "", URL: "https://github.com/x/y"}); err == nil {
		t.Error("expected error for empty name")
	}
}
