//go:build !nogithub

package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func TestRefresh_IncludesPrivateRepoWithToken(t *testing.T) {
	// Mock GitHub API: bach-godot is private, schmux is public.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/lordbaltogames/bach-godot":
			// Private repo: 404 without auth, 200 with auth.
			if r.Header.Get("Authorization") == "" {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, `{"message":"Not Found"}`)
				return
			}
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"private":true}`)
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
		case "/repos/sergeknystautas/schmux":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"private":false}`)
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
	repos := []config.Repo{
		{Name: "bach-godot", URL: "https://github.com/lordbaltogames/bach-godot", GitHubLogin: "testuser"},
		{Name: "schmux", URL: "https://github.com/sergeknystautas/schmux"},
	}

	cfg := &config.Config{ConfigData: config.ConfigData{Repos: repos}}

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

func TestRefresh_SkipsPrivateRepoWithoutToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/lordbaltogames/bach-godot":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		case "/repos/sergeknystautas/schmux":
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"private":false}`)
		case "/repos/sergeknystautas/schmux/pulls":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]interface{}{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	origBase := apiBaseURL
	defer func() { setAPIBaseURL(origBase) }()
	setAPIBaseURL(server.URL)

	repos := []config.Repo{
		{Name: "bach-godot", URL: "https://github.com/lordbaltogames/bach-godot"},
		{Name: "schmux", URL: "https://github.com/sergeknystautas/schmux"},
	}

	cfg := &config.Config{ConfigData: config.ConfigData{Repos: repos}}

	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })

	d := NewDiscovery(nil)
	prs, _, err := d.Refresh(repos, cfg)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}

	// Only the public repo's PRs (empty list) should come through.
	if len(prs) != 0 {
		t.Errorf("expected 0 PRs (private repo skipped), got %d", len(prs))
	}
}
