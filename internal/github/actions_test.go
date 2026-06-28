//go:build !nogithub

package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListWorkflows_DecodesEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth header = %q", got)
		}
		w.Write([]byte(`{"total_count":2,"workflows":[
			{"id":1,"name":"CI","path":".github/workflows/ci.yml","state":"active"},
			{"id":2,"name":"Old","path":".github/workflows/old.yml","state":"disabled_manually"}]}`))
	}))
	defer srv.Close()
	old := apiBaseURL
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = old }()

	workflows, err := ListWorkflows(context.Background(), "tok", RepoInfo{Owner: "o", Repo: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) != 2 || workflows[0].Name != "CI" || workflows[1].State != "disabled_manually" {
		t.Fatalf("workflows=%+v", workflows)
	}
}

func TestListRepoRuns_DecodesEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth header = %q", got)
		}
		if got := r.URL.Query().Get("branch"); got != "main" {
			t.Errorf("branch = %q", got)
		}
		w.Write([]byte(`{"total_count":1,"workflow_runs":[
			{"id":7,"workflow_id":1,"run_number":3,"status":"completed","conclusion":"failure","html_url":"https://x/run/7","created_at":"2026-06-05T00:00:00Z"}]}`))
	}))
	defer srv.Close()
	old := apiBaseURL
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = old }()

	runs, err := ListRepoRuns(context.Background(), "tok", RepoInfo{Owner: "o", Repo: "r"}, "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != 7 || runs[0].WorkflowID != 1 || runs[0].Conclusion != "failure" {
		t.Fatalf("runs=%+v", runs)
	}
}

func TestListRepoRuns_401Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	old := apiBaseURL
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = old }()

	_, err := ListRepoRuns(context.Background(), "tok", RepoInfo{Owner: "o", Repo: "r"}, "main")
	if !IsUnauthorized(err) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
}

func TestListRunJobs_DecodesEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"total_count":2,"jobs":[
			{"id":1,"name":"build","status":"completed","conclusion":"success","html_url":"https://x/j/1"},
			{"id":2,"name":"test","status":"completed","conclusion":"failure","html_url":"https://x/j/2"}]}`))
	}))
	defer srv.Close()
	old := apiBaseURL
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = old }()

	jobs, err := ListRunJobs(context.Background(), "tok", RepoInfo{Owner: "o", Repo: "r"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 || jobs[1].Conclusion != "failure" {
		t.Fatalf("jobs=%+v", jobs)
	}
}

func TestDownloadJobLogs_ReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth header = %q", got)
		}
		if r.URL.Path != "/repos/o/r/actions/jobs/99/logs" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Write([]byte("line 1\nline 2\n"))
	}))
	defer srv.Close()
	old := apiBaseURL
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = old }()

	data, err := DownloadJobLogs(context.Background(), "tok", RepoInfo{Owner: "o", Repo: "r"}, 99)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "line 1\nline 2\n" {
		t.Fatalf("data = %q", data)
	}
}

func TestDownloadJobLogs_FollowsRedirect(t *testing.T) {
	// GitHub answers the logs endpoint with a 302 to a signed URL; what our
	// code owes is following it and returning the redirected body. (Whether
	// the Authorization header survives the hop is stdlib policy — Go
	// compares hostnames ignoring ports — and is not asserted here.)
	logSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("redirected log"))
	}))
	defer logSrv.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, logSrv.URL+"/signed", http.StatusFound)
	}))
	defer srv.Close()
	old := apiBaseURL
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = old }()

	data, err := DownloadJobLogs(context.Background(), "tok", RepoInfo{Owner: "o", Repo: "r"}, 99)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "redirected log" {
		t.Fatalf("data = %q", data)
	}
}

func TestDownloadJobLogs_KeepsTailWhenOversized(t *testing.T) {
	big := make([]byte, maxJobLogBytes+10)
	for i := range big {
		big[i] = 'a'
	}
	copy(big[len(big)-4:], "TAIL")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(big)
	}))
	defer srv.Close()
	old := apiBaseURL
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = old }()

	data, err := DownloadJobLogs(context.Background(), "tok", RepoInfo{Owner: "o", Repo: "r"}, 99)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != maxJobLogBytes {
		t.Fatalf("len = %d, want %d", len(data), maxJobLogBytes)
	}
	if string(data[len(data)-4:]) != "TAIL" {
		t.Fatalf("tail = %q, want TAIL", data[len(data)-4:])
	}
}

func TestDownloadJobLogs_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	old := apiBaseURL
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = old }()

	_, err := DownloadJobLogs(context.Background(), "tok", RepoInfo{Owner: "o", Repo: "r"}, 99)
	if !IsNotFound(err) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// A permission 403 (org OAuth-app restriction, missing scope, private repo)
// still has rate-limit quota and must NOT be reported as a rate limit — that
// misleads operators into chasing a throttling problem that doesn't exist.
func TestListWorkflows_403PermissionIsForbiddenNotRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "4959")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"the org has enabled OAuth App access restrictions"}`))
	}))
	defer srv.Close()
	old := apiBaseURL
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = old }()

	_, err := ListWorkflows(context.Background(), "tok", RepoInfo{Owner: "o", Repo: "r"})
	if !IsForbidden(err) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if _, ok := err.(*RateLimitError); ok {
		t.Fatalf("permission 403 must not be a RateLimitError, got %v", err)
	}
}

// A genuine primary rate limit zeroes the remaining counter; that case must
// still surface as a RateLimitError.
func TestListWorkflows_403RateLimitWhenRemainingZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer srv.Close()
	old := apiBaseURL
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = old }()

	_, err := ListWorkflows(context.Background(), "tok", RepoInfo{Owner: "o", Repo: "r"})
	if _, ok := err.(*RateLimitError); !ok {
		t.Fatalf("err = %T (%v), want *RateLimitError", err, err)
	}
}

// A 429 is always a rate limit regardless of headers.
func TestListWorkflows_429IsRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	old := apiBaseURL
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = old }()

	_, err := ListWorkflows(context.Background(), "tok", RepoInfo{Owner: "o", Repo: "r"})
	if _, ok := err.(*RateLimitError); !ok {
		t.Fatalf("err = %T (%v), want *RateLimitError", err, err)
	}
}
