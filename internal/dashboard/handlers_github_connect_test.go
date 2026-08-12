package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// isolateFromCLIs empties PATH so the handler's `gh` and `git` probes fail
// deterministically. Without it, a developer machine with an authenticated gh
// would have the status handler call out to the GitHub API for owner listing.
func isolateFromCLIs(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", "")
}

func TestHandleGitHubConnectStatus_UnknownWorkspace(t *testing.T) {
	server, _, _ := newTestServer(t)
	gitH := newTestGitHandlers(server)

	req := makeWorkspaceRequest(t, http.MethodGet, "/api/workspaces/nope/github-connect", "nope", nil)
	rr := httptest.NewRecorder()
	gitH.handleGitHubConnectStatus(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleGitHubConnectStatus_LocalRepoWorkspace(t *testing.T) {
	isolateFromCLIs(t)
	server, cfg, st := newTestServer(t)
	cfg.Repos = []config.Repo{{Name: "talkback", URL: "local:talkback", BarePath: "talkback.git"}}
	gitH := newTestGitHandlers(server)

	if err := st.AddWorkspace(state.Workspace{
		ID:     "talkback-001",
		Repo:   "local:talkback",
		Branch: "feature/prompt-branch",
		Path:   t.TempDir(),
	}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	req := makeWorkspaceRequest(t, http.MethodGet, "/api/workspaces/talkback-001/github-connect", "talkback-001", nil)
	rr := httptest.NewRecorder()
	gitH.handleGitHubConnectStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp contracts.GitHubConnectStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Eligible {
		t.Error("expected a local: workspace to be connect-eligible")
	}
	if !resp.StateRepoIsLocal || !resp.ConfigURLIsLocal {
		t.Errorf("expected state and config to be flagged local, got state=%v config=%v",
			resp.StateRepoIsLocal, resp.ConfigURLIsLocal)
	}
	if resp.Name != "talkback" {
		t.Errorf("expected name prefill %q, got %q", "talkback", resp.Name)
	}
	if resp.DefaultBranch != "main" {
		t.Errorf("expected default branch prefill %q, got %q", "main", resp.DefaultBranch)
	}
	// gh is unreachable, so no owners may be offered and repo creation must be
	// reported as blocked rather than silently attempted.
	if resp.GH.Available {
		t.Error("expected gh to be reported unavailable with an empty PATH")
	}
	if len(resp.Owners) != 0 {
		t.Errorf("expected no owners, got %v", resp.Owners)
	}

	needed := map[string]bool{}
	for _, p := range resp.Plan {
		needed[p.Step] = p.Needed
		if p.Reason == "" {
			t.Errorf("plan step %q has no reason", p.Step)
		}
	}
	for _, step := range []string{"set_origin", "create_repo", "update_config", "link_workspaces", "initial_push"} {
		if _, ok := needed[step]; !ok {
			t.Errorf("plan is missing step %q", step)
		}
	}
	// Nothing is connected yet, so every step is outstanding.
	for step, isNeeded := range needed {
		if !isNeeded {
			t.Errorf("expected step %q to be needed for a fresh local: workspace", step)
		}
	}
}

func TestHandleGitHubConnect_InvalidBody(t *testing.T) {
	server, _, _ := newTestServer(t)
	gitH := newTestGitHandlers(server)

	req := makeWorkspaceRequest(t, http.MethodPost, "/api/workspaces/talkback-001/github-connect", "talkback-001", []byte("not json"))
	rr := httptest.NewRecorder()
	gitH.handleGitHubConnect(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleGitHubConnect_UnknownWorkspace(t *testing.T) {
	isolateFromCLIs(t)
	server, _, _ := newTestServer(t)
	gitH := newTestGitHandlers(server)

	req := makeWorkspaceRequest(t, http.MethodPost, "/api/workspaces/nope/github-connect", "nope", []byte(`{}`))
	rr := httptest.NewRecorder()
	gitH.handleGitHubConnect(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

// A remote-backed workspace has nothing to connect; the handler must reject it
// rather than run the pipeline.
func TestHandleGitHubConnect_NotEligible(t *testing.T) {
	isolateFromCLIs(t)
	server, _, st := newTestServer(t)
	gitH := newTestGitHandlers(server)

	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-remote",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   t.TempDir(),
	}); err != nil {
		t.Fatalf("failed to add workspace: %v", err)
	}

	req := makeWorkspaceRequest(t, http.MethodPost, "/api/workspaces/ws-remote/github-connect", "ws-remote", []byte(`{}`))
	rr := httptest.NewRecorder()
	gitH.handleGitHubConnect(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d: %s", rr.Code, rr.Body.String())
	}
}
