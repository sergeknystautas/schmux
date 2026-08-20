//go:build !nogithub

package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckVisibility(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		token      string
		wantPublic bool
		wantErr    bool
		wantRate   bool
		wantAuth   string // expected Authorization header value
	}{
		{
			name:       "public repo",
			statusCode: 200,
			body:       `{"private": false}`,
			wantPublic: true,
		},
		{
			name:       "private repo unauthenticated",
			statusCode: 404,
			body:       `{"message": "Not Found"}`,
			wantPublic: false,
		},
		{
			name:       "not found unauthenticated",
			statusCode: 404,
			body:       `{"message": "Not Found"}`,
			wantPublic: false,
		},
		{
			name:       "rate limited",
			statusCode: 403,
			body:       `{"message": "rate limit exceeded"}`,
			wantErr:    true,
			wantRate:   true,
		},
		{
			name:       "private repo authenticated",
			statusCode: 200,
			body:       `{"private": true}`,
			token:      "tok123",
			wantPublic: true,
			wantAuth:   "Bearer tok123",
		},
		{
			name:       "public repo authenticated",
			statusCode: 200,
			body:       `{"private": false}`,
			token:      "tok123",
			wantPublic: true,
			wantAuth:   "Bearer tok123",
		},
		{
			name:       "not found authenticated",
			statusCode: 404,
			body:       `{"message": "Not Found"}`,
			token:      "tok123",
			wantErr:    true,
			wantAuth:   "Bearer tok123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("User-Agent") != userAgent {
					t.Errorf("expected User-Agent %q, got %q", userAgent, r.Header.Get("User-Agent"))
				}
				if r.Header.Get("Accept") != "application/vnd.github+json" {
					t.Errorf("expected Accept header for GitHub API")
				}
				if tt.wantAuth != "" {
					if got := r.Header.Get("Authorization"); got != tt.wantAuth {
						t.Errorf("expected Authorization %q, got %q", tt.wantAuth, got)
					}
				} else {
					if got := r.Header.Get("Authorization"); got != "" {
						t.Errorf("expected no Authorization header, got %q", got)
					}
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()

			// Override the API base URL for testing
			origBase := apiBaseURL
			defer func() { setAPIBaseURL(origBase) }()
			setAPIBaseURL(server.URL)

			isPublic, err := CheckVisibility(RepoInfo{Owner: "user", Repo: "repo"}, tt.token)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckVisibility() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantRate {
				if _, ok := err.(*RateLimitError); !ok {
					t.Errorf("expected RateLimitError, got %T", err)
				}
			}
			if isPublic != tt.wantPublic {
				t.Errorf("CheckVisibility() = %v, want %v", isPublic, tt.wantPublic)
			}
		})
	}
}

func TestFetchOpenPRs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prs := []map[string]interface{}{
			{
				"number":     42,
				"title":      "Add feature X",
				"body":       "This adds feature X",
				"state":      "open",
				"html_url":   "https://github.com/user/repo/pull/42",
				"created_at": "2025-01-15T10:00:00Z",
				"user":       map[string]string{"login": "someone"},
				"head": map[string]interface{}{
					"ref": "feature-x",
					"repo": map[string]interface{}{
						"fork":  false,
						"owner": map[string]string{"login": "user"},
					},
				},
				"base": map[string]interface{}{
					"ref": "main",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(prs)
	}))
	defer server.Close()

	origBase := apiBaseURL
	defer func() { setAPIBaseURL(origBase) }()
	setAPIBaseURL(server.URL)

	prs, err := FetchOpenPRs(RepoInfo{Owner: "user", Repo: "repo"}, "repo", "git@github.com:user/repo.git", "")
	if err != nil {
		t.Fatalf("FetchOpenPRs() error = %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("expected 1 PR, got %d", len(prs))
	}
	pr := prs[0]
	if pr.Number != 42 {
		t.Errorf("expected PR #42, got #%d", pr.Number)
	}
	if pr.Title != "Add feature X" {
		t.Errorf("expected title %q, got %q", "Add feature X", pr.Title)
	}
	if pr.Author != "someone" {
		t.Errorf("expected author %q, got %q", "someone", pr.Author)
	}
	if pr.SourceBranch != "feature-x" {
		t.Errorf("expected source branch %q, got %q", "feature-x", pr.SourceBranch)
	}
	if pr.TargetBranch != "main" {
		t.Errorf("expected target branch %q, got %q", "main", pr.TargetBranch)
	}
	if pr.RepoName != "repo" {
		t.Errorf("expected repo name %q, got %q", "repo", pr.RepoName)
	}
	if pr.IsFork {
		t.Error("expected non-fork PR")
	}
}

func TestFetchOpenPRs_RateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message": "rate limit exceeded"}`))
	}))
	defer server.Close()

	origBase := apiBaseURL
	defer func() { setAPIBaseURL(origBase) }()
	setAPIBaseURL(server.URL)

	_, err := FetchOpenPRs(RepoInfo{Owner: "user", Repo: "repo"}, "repo", "git@github.com:user/repo.git", "")
	if err == nil {
		t.Fatal("expected error")
	}
	rle, ok := err.(*RateLimitError)
	if !ok {
		t.Fatalf("expected RateLimitError, got %T", err)
	}
	if rle.RetryAfterSec != 120 {
		t.Errorf("expected RetryAfterSec 120, got %d", rle.RetryAfterSec)
	}
}

func TestFetchOpenPRs_AuthHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok456" {
			t.Errorf("expected Authorization %q, got %q", "Bearer tok456", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	}))
	defer server.Close()

	origBase := apiBaseURL
	defer func() { setAPIBaseURL(origBase) }()
	setAPIBaseURL(server.URL)

	_, err := FetchOpenPRs(RepoInfo{Owner: "user", Repo: "repo"}, "repo", "git@github.com:user/repo.git", "tok456")
	if err != nil {
		t.Fatalf("FetchOpenPRs() error = %v", err)
	}
}

func TestFetchOpenPRs_NoAuthHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("expected no Authorization header, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	}))
	defer server.Close()

	origBase := apiBaseURL
	defer func() { setAPIBaseURL(origBase) }()
	setAPIBaseURL(server.URL)

	_, err := FetchOpenPRs(RepoInfo{Owner: "user", Repo: "repo"}, "repo", "git@github.com:user/repo.git", "")
	if err != nil {
		t.Fatalf("FetchOpenPRs() error = %v", err)
	}
}

// setAPIBaseURL is a test helper to override the API base URL.
func setAPIBaseURL(url string) {
	apiBaseURL = url
}
