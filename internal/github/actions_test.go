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
