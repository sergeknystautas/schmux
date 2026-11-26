//go:build !nogithub

package github

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestSeedLoadsPRs(t *testing.T) {
	prs := []contracts.PullRequest{{Number: 1, RepoURL: "u1"}, {Number: 2, RepoURL: "u2"}}
	d := NewDiscovery(nil)
	d.Seed(prs)
	prs[0].Title = "mutated"

	got, _, _ := d.GetPRs()
	if len(got) != 2 || got[0].Title != "" {
		t.Errorf("GetPRs() = %+v, want two PRs with an empty first Title", got)
	}
	for _, want := range []struct {
		repoURL string
		number  int
	}{{"u1", 1}, {"u2", 2}} {
		if _, found := d.FindPR(want.repoURL, want.number); !found {
			t.Errorf("FindPR(%q, %d) not found in %+v", want.repoURL, want.number, got)
		}
	}
}

func TestRefresh_IncludesPrivateRepoWithToken(t *testing.T) {
	// Mock GitHub API: bach-godot is private, schmux is public.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/lordbaltogames/bach-godot/pulls":
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"message":"Not Found"}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"number":     3,
					"title":      "Fix puzzle placement",
					"body":       "Fixes the bug",
					"state":      "open",
					"html_url":   "https://github.com/lordbaltogames/bach-godot/pull/3",
					"created_at": "2026-08-15T10:00:00Z",
					"user":       map[string]string{"login": "dev"},
					"head": map[string]interface{}{
						"ref": "fix/puzzle",
						"repo": map[string]interface{}{
							"fork":  false,
							"owner": map[string]string{"login": "lordbaltogames"},
						},
					},
					"base": map[string]interface{}{"ref": "main"},
				},
			})
		case "/repos/sergeknystautas/schmux/pulls":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"number":     26,
					"title":      "Docs update",
					"body":       "",
					"state":      "open",
					"html_url":   "https://github.com/sergeknystautas/schmux/pull/26",
					"created_at": "2026-03-08T03:04:02Z",
					"user":       map[string]string{"login": "sergeknystautas"},
					"head": map[string]interface{}{
						"ref": "docs",
						"repo": map[string]interface{}{
							"fork":  false,
							"owner": map[string]string{"login": "sergeknystautas"},
						},
					},
					"base": map[string]interface{}{"ref": "main"},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	origBase := apiBaseURL
	defer func() { setAPIBaseURL(origBase) }()
	setAPIBaseURL(server.URL)

	// Configure repos: bach-godot has a github_login, schmux does not.
	repos := []state.Repo{
		{Name: "bach-godot", URL: "https://github.com/lordbaltogames/bach-godot", GitHubLogin: "testuser"},
		{Name: "schmux", URL: "https://github.com/sergeknystautas/schmux"},
	}

	cfg := &config.Config{}

	// Seed the token store so GetGitHubToken("testuser") returns a token.
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	if err := config.SaveGitHubIdentity("testuser", "fake-oauth-token", "repo"); err != nil {
		t.Fatalf("failed to save identity: %v", err)
	}

	d := NewDiscovery(nil)
	prs, _, err := d.Refresh(repos, cfg)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	if len(prs) != 2 {
		t.Fatalf("expected 2 PRs, got %d", len(prs))
	}

	// Check that both PRs are present.
	found := map[int]bool{}
	for _, pr := range prs {
		found[pr.Number] = true
	}
	if !found[3] {
		t.Error("expected PR #3 from private repo bach-godot")
	}
	if !found[26] {
		t.Error("expected PR #26 from public repo schmux")
	}
}

func TestRefresh_SkipsRepoGitHubDoesNotReturn(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	authByPath := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		authByPath[r.URL.Path] = r.Header.Get("Authorization")
		mu.Unlock()

		switch r.URL.Path {
		case "/repos/acme/broken/pulls":
			w.WriteHeader(http.StatusInternalServerError)
		case "/repos/acme/ok/pulls":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"number":     5,
					"title":      "Docs update",
					"body":       "",
					"state":      "open",
					"html_url":   "https://github.com/acme/ok/pull/5",
					"created_at": "2026-03-08T03:04:02Z",
					"user":       map[string]string{"login": "dev"},
					"head": map[string]interface{}{
						"ref": "docs",
						"repo": map[string]interface{}{
							"fork":  false,
							"owner": map[string]string{"login": "acme"},
						},
					},
					"base": map[string]interface{}{"ref": "main"},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	defer server.Close()

	origBase := apiBaseURL
	defer func() { setAPIBaseURL(origBase) }()
	setAPIBaseURL(server.URL)

	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	if err := config.SaveGitHubIdentity("testuser", "fake-oauth-token", "repo"); err != nil {
		t.Fatalf("failed to save identity: %v", err)
	}

	repos := []state.Repo{
		{Name: "private", URL: "https://github.com/acme/private"},
		{Name: "locked", URL: "https://github.com/acme/locked", GitHubLogin: "testuser"},
		{Name: "broken", URL: "https://github.com/acme/broken"},
		{Name: "ok", URL: "https://github.com/acme/ok"},
		{Name: "gitlab", URL: "https://gitlab.com/acme/other"},
	}

	var logBuf bytes.Buffer
	d := NewDiscovery(log.NewWithOptions(&logBuf, log.Options{}))
	prs, _, err := d.Refresh(repos, &config.Config{})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(prs) != 1 || prs[0].Number != 5 || prs[0].RepoURL != "https://github.com/acme/ok" {
		t.Errorf("Refresh() prs = %+v, want only PR #5 from https://github.com/acme/ok", prs)
	}

	// A 404 skips the repo at Info, with or without a token; any other failure logs at Error.
	logLines := strings.Split(logBuf.String(), "\n")
	for _, want := range []struct{ url, level, msg string }{
		{"https://github.com/acme/private", "INFO", "skipping repo GitHub did not return"},
		{"https://github.com/acme/locked", "INFO", "skipping repo GitHub did not return"},
		{"https://github.com/acme/broken", "ERRO", "error fetching PRs"},
	} {
		found := false
		for _, line := range logLines {
			if strings.Contains(line, "url="+want.url) && strings.Contains(line, want.level) && strings.Contains(line, want.msg) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no %s %q line for %s; log:\n%s", want.level, want.msg, want.url, logBuf.String())
		}
	}

	mu.Lock()
	defer mu.Unlock()
	for _, path := range paths {
		if !strings.HasSuffix(path, "/pulls") {
			t.Errorf("unexpected request %q, want only /pulls requests (all: %q)", path, paths)
		}
		if strings.Contains(path, "gitlab") || strings.Contains(path, "acme/other") {
			t.Errorf("non-GitHub repo was requested: %q (all: %q)", path, paths)
		}
	}

	// The "locked" repo sends its identity's token, so its 404 arrives with
	// credentials. The token-less repos must not send one.
	if got := authByPath["/repos/acme/locked/pulls"]; got != "Bearer fake-oauth-token" {
		t.Errorf("Authorization for /repos/acme/locked/pulls = %q, want %q", got, "Bearer fake-oauth-token")
	}
	for _, path := range []string{"/repos/acme/private/pulls", "/repos/acme/ok/pulls", "/repos/acme/broken/pulls"} {
		if got := authByPath[path]; got != "" {
			t.Errorf("Authorization for %s = %q, want none", path, got)
		}
	}
}

func TestRefresh_RateLimitKeepsPreviousPRs(t *testing.T) {
	var rateLimited atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/app/pulls" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if rateLimited.Load() {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{{"number": 1}})
	}))
	defer server.Close()

	origBase := apiBaseURL
	defer func() { setAPIBaseURL(origBase) }()
	setAPIBaseURL(server.URL)

	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })

	repos := []state.Repo{{Name: "app", URL: "https://github.com/acme/app"}}
	d := NewDiscovery(nil)

	prs, _, err := d.Refresh(repos, &config.Config{})
	if err != nil || len(prs) != 1 {
		t.Fatalf("first Refresh() = %+v, err = %v, want one PR and no error", prs, err)
	}

	rateLimited.Store(true)
	_, retryAfter, err := d.Refresh(repos, &config.Config{})
	var rle *RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("second Refresh() err = %v, want *RateLimitError", err)
	}
	if retryAfter == nil || *retryAfter != 60 {
		t.Fatalf("second Refresh() retryAfter = %v (err = %v), want 60", retryAfter, err)
	}

	cached, _, lastErr := d.GetPRs()
	if len(cached) != 1 || cached[0].Number != 1 {
		t.Errorf("GetPRs() after rate limit = %+v, want the earlier PR #1", cached)
	}
	if lastErr == "" {
		t.Errorf("GetPRs() lastError = %q, want non-empty", lastErr)
	}
}
