//go:build !nogithub

package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestPRRefreshSavesPRs(t *testing.T) {
	server, _, _ := newTestServer(t)

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/app/pulls" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]interface{}{{"number": 7}})
	}))
	defer fake.Close()
	defer github.SetAPIBaseURLForTest(fake.URL)()

	if _, err := server.repos.Add(state.Repo{Name: "app", URL: "https://github.com/acme/app"}); err != nil {
		t.Fatalf("repos.Add() error = %v", err)
	}

	rr := httptest.NewRecorder()
	server.handlePRRefresh(rr, httptest.NewRequest(http.MethodPost, "/api/prs/refresh", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body)
	}
	var resp contracts.PRRefreshResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v, body = %s", err, rr.Body)
	}
	if len(resp.PullRequests) != 1 || resp.PullRequests[0].Number != 7 {
		t.Errorf("response PRs = %+v, want only #7, body = %s", resp.PullRequests, rr.Body)
	}

	data, err := os.ReadFile(server.statePath)
	if err != nil {
		t.Fatalf("read state file: %v, body = %s", err, rr.Body)
	}
	var saved struct {
		PullRequests []contracts.PullRequest `json:"pull_requests"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("decode state file: %v, state = %s", err, data)
	}
	if len(saved.PullRequests) != 1 || saved.PullRequests[0].Number != 7 {
		t.Errorf("saved pull_requests = %+v, want only #7, body = %s, state = %s", saved.PullRequests, rr.Body, data)
	}
}
