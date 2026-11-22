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

func TestSanitizeRepoName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "dotted name", input: "lordbalto.games", want: "lordbalto-games"},
		{name: "multiple dots", input: "a.b.c", want: "a-b-c"},
		{name: "leading dot trimmed", input: ".github", want: "github"},
		{name: "leading dots trimmed", input: "..x", want: "x"},
		{name: "valid characters kept", input: "Plain_Name-2", want: "Plain_Name-2"},
		{name: "empty name", input: "", want: ""},
		{name: "slash", input: "a/b", want: "a-b"},
		{name: "backslash", input: `a\b`, want: "a-b"},
		{name: "path traversal", input: "../etc", want: "etc"},
		{name: "space and colon", input: "my repo:v2", want: "my-repo-v2"},
		{name: "quotes and shell characters", input: `a"b'c$d`, want: "a-b-c-d"},
		{name: "non-ASCII letter", input: "café", want: "caf"},
		{name: "trailing invalid trimmed", input: "foo.", want: "foo"},
		{name: "only invalid characters", input: "...", want: ""},
		{name: "longer than 128 cut", input: strings.Repeat("a", 130), want: strings.Repeat("a", 128)},
		{name: "cut lands on dash", input: strings.Repeat("a", 127) + ".b", want: strings.Repeat("a", 127)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeRepoName(tt.input); got != tt.want {
				t.Errorf("SanitizeRepoName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
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
