//go:build !nogithub

package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func withFakeAPI(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	orig := apiBaseURL
	apiBaseURL = srv.URL
	t.Cleanup(func() { apiBaseURL = orig })
}

func TestFetchOpenPRForBranch_Match(t *testing.T) {
	withFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/widget/pulls" {
			t.Errorf("path = %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("head") != "acme:feature" || q.Get("state") != "open" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth = %q", got)
		}
		w.Write([]byte(`[{"number": 42, "html_url": "https://github.com/acme/widget/pull/42"}]`))
	})
	pr, err := FetchOpenPRForBranch(context.Background(), "tok", RepoInfo{Owner: "acme", Repo: "widget"}, "acme:feature")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if pr == nil || pr.Number != 42 || pr.HTMLURL != "https://github.com/acme/widget/pull/42" {
		t.Errorf("pr = %+v", pr)
	}
}

func TestFetchOpenPRForBranch_NoMatch(t *testing.T) {
	withFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	})
	pr, err := FetchOpenPRForBranch(context.Background(), "tok", RepoInfo{Owner: "acme", Repo: "widget"}, "acme:feature")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if pr != nil {
		t.Errorf("pr = %+v, want nil", pr)
	}
}

func TestFetchOpenPRForBranch_RateLimit(t *testing.T) {
	withFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := FetchOpenPRForBranch(context.Background(), "tok", RepoInfo{Owner: "acme", Repo: "widget"}, "acme:feature")
	var rle *RateLimitError
	if !errors.As(err, &rle) || rle.RetryAfterSec != 30 {
		t.Fatalf("err = %v, want RateLimitError{30}", err)
	}
}

func TestFetchOpenPRForBranch_NotFound(t *testing.T) {
	withFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := FetchOpenPRForBranch(context.Background(), "tok", RepoInfo{Owner: "acme", Repo: "widget"}, "acme:feature")
	if !IsNotFound(err) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
