package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// addRepoRequest builds a POST /api/repos request.
func addRepoRequest(t *testing.T, body contracts.RepoAddRequest) *http.Request {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/repos", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// routeRepoRemove sends DELETE /api/repos/{repo}, with the name escaped the
// way the browser escapes it, through a chi router registered as the server
// registers the route, so the handler sees the parameter chi gives it.
func routeRepoRemove(wsH *WorkspaceHandlers, rr *httptest.ResponseRecorder, name string) {
	r := chi.NewRouter()
	r.Delete("/api/repos/{repo}", wsH.handleRepoRemove)
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/api/repos/"+url.PathEscape(name), nil))
}

// TestHandleRepoAdd_GitDerivesName proves the name comes from the URL for a
// git repo: the name in the request is ignored.
func TestHandleRepoAdd_GitDerivesName(t *testing.T) {
	server, _, _ := newTestServer(t)
	wsH := newTestWorkspaceHandlers(server)

	req := addRepoRequest(t, contracts.RepoAddRequest{
		URL:  "https://github.com/anthropics/claude-code.git",
		Name: "ignored",
	})
	rr := httptest.NewRecorder()
	wsH.handleRepoAdd(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp contracts.Repo
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Name != "claude-code" {
		t.Errorf("name = %q, want %q", resp.Name, "claude-code")
	}
	if resp.BarePath != "claude-code.git" {
		t.Errorf("bare_path = %q, want %q", resp.BarePath, "claude-code.git")
	}

	// The entry is in the state the daemon writes.
	onDisk, err := state.Load(server.statePath, nil)
	if err != nil {
		t.Fatalf("Load state: %v", err)
	}
	stored, found := onDisk.GetRepo("claude-code")
	if !found {
		t.Fatalf("state.json has no claude-code entry: %+v", onDisk.GetRepos())
	}
	if stored.URL != "https://github.com/anthropics/claude-code.git" {
		t.Errorf("stored URL = %q", stored.URL)
	}

	// The overlay directory exists for the repo.
	overlayDir, err := workspace.OverlayDir("claude-code")
	if err != nil {
		t.Fatalf("overlay dir: %v", err)
	}
	if _, err := os.Stat(overlayDir); err != nil {
		t.Errorf("overlay directory %q should exist: %v", overlayDir, err)
	}
}

// TestHandleRepoAdd_GitCloneStored proves a git-clone repo keeps its VCS.
func TestHandleRepoAdd_GitCloneStored(t *testing.T) {
	server, _, _ := newTestServer(t)
	wsH := newTestWorkspaceHandlers(server)

	req := addRepoRequest(t, contracts.RepoAddRequest{
		URL: "https://github.com/anthropics/claude-code.git",
		VCS: "git-clone",
	})
	rr := httptest.NewRecorder()
	wsH.handleRepoAdd(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp contracts.Repo
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.VCS != "git-clone" {
		t.Errorf("vcs = %q, want %q", resp.VCS, "git-clone")
	}
}

// TestHandleRepoAdd_TwoURLsOneName proves two URLs that derive the same name
// get distinct names.
func TestHandleRepoAdd_TwoURLsOneName(t *testing.T) {
	server, _, _ := newTestServer(t)
	wsH := newTestWorkspaceHandlers(server)

	first := httptest.NewRecorder()
	wsH.handleRepoAdd(first, addRepoRequest(t, contracts.RepoAddRequest{URL: "https://github.com/acme/app.git"}))
	if first.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	wsH.handleRepoAdd(second, addRepoRequest(t, contracts.RepoAddRequest{URL: "https://github.com/other/app.git"}))
	if second.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", second.Code, second.Body.String())
	}

	var a, b contracts.Repo
	if err := json.NewDecoder(first.Body).Decode(&a); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	if err := json.NewDecoder(second.Body).Decode(&b); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if a.Name == b.Name {
		t.Errorf("both entries named %q, want distinct names", a.Name)
	}
	if len(server.repos.List()) != 2 {
		t.Errorf("stored entries = %d, want 2", len(server.repos.List()))
	}
}

// TestHandleRepoAdd_SaplingName proves a sapling repo takes its name from the
// request, sanitized.
func TestHandleRepoAdd_SaplingName(t *testing.T) {
	server, _, _ := newTestServer(t)
	wsH := newTestWorkspaceHandlers(server)

	rr := httptest.NewRecorder()
	wsH.handleRepoAdd(rr, addRepoRequest(t, contracts.RepoAddRequest{
		URL:  "sl:my.repo",
		VCS:  "sapling",
		Name: "my.repo",
	}))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp contracts.Repo
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Name != "my-repo" {
		t.Errorf("name = %q, want %q", resp.Name, "my-repo")
	}
	if resp.BarePath != "my-repo" {
		t.Errorf("bare_path = %q, want %q", resp.BarePath, "my-repo")
	}
}

// TestHandleRepoAdd_Rejections proves the bad requests are rejected.
func TestHandleRepoAdd_Rejections(t *testing.T) {
	server, _, _ := newTestServer(t)
	wsH := newTestWorkspaceHandlers(server)

	// A taken name.
	if rr := httptest.NewRecorder(); true {
		wsH.handleRepoAdd(rr, addRepoRequest(t, contracts.RepoAddRequest{
			URL:  "sl:my.repo",
			VCS:  "sapling",
			Name: "my.repo",
		}))
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 for the first add, got %d: %s", rr.Code, rr.Body.String())
		}
	}
	taken := httptest.NewRecorder()
	wsH.handleRepoAdd(taken, addRepoRequest(t, contracts.RepoAddRequest{
		URL:  "sl:other",
		VCS:  "sapling",
		Name: "my.repo",
	}))
	if taken.Code != http.StatusBadRequest {
		t.Errorf("a taken name: got %d, want 400", taken.Code)
	}

	// A name that sanitizes to nothing.
	empty := httptest.NewRecorder()
	wsH.handleRepoAdd(empty, addRepoRequest(t, contracts.RepoAddRequest{
		URL:  "sl:other",
		VCS:  "sapling",
		Name: "...",
	}))
	if empty.Code != http.StatusBadRequest {
		t.Errorf("an unsanitizable name: got %d, want 400", empty.Code)
	}

	// No URL.
	noURL := httptest.NewRecorder()
	wsH.handleRepoAdd(noURL, addRepoRequest(t, contracts.RepoAddRequest{VCS: "git"}))
	if noURL.Code != http.StatusBadRequest {
		t.Errorf("an empty URL: got %d, want 400", noURL.Code)
	}

	if got := server.repos.List(); len(got) != 1 {
		t.Errorf("stored entries = %d, want 1", len(got))
	}
}

// TestHandleRepoRemove proves a repo is removed, its unused base is cleaned
// up, and an unknown name is a 404.
func TestHandleRepoRemove(t *testing.T) {
	server, _, st := newTestServer(t)
	wsH := newTestWorkspaceHandlers(server)

	baseDir := filepath.Join(server.config.GetWorktreeBasePath(), "myrepo.git")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}
	// The base is a real bare repo, so the cleanup can list its worktrees.
	gitInit := exec.Command("git", "init", "--bare", "-b", "main")
	gitInit.Dir = baseDir
	if out, err := gitInit.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	seedRepos(t, st, []state.Repo{
		{Name: "myrepo", URL: "https://github.com/user/myrepo.git", BarePath: "myrepo.git"},
		{Name: "other", URL: "https://github.com/user/other.git", BarePath: "other.git"},
	})
	if err := st.AddRepoBase(state.RepoBase{RepoURL: "https://github.com/user/myrepo.git", Path: baseDir}); err != nil {
		t.Fatalf("AddRepoBase: %v", err)
	}

	rr := httptest.NewRecorder()
	routeRepoRemove(wsH, rr, "myrepo")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want %q", resp["status"], "ok")
	}

	if _, ok := server.repos.Get("myrepo"); ok {
		t.Error("myrepo should be gone")
	}
	remaining := server.repos.List()
	if len(remaining) != 1 || remaining[0].Name != "other" {
		t.Errorf("remaining entries = %+v, want [other]", remaining)
	}
	if _, err := os.Stat(baseDir); !os.IsNotExist(err) {
		t.Errorf("the unused base directory should be gone: %v", err)
	}

	unknown := httptest.NewRecorder()
	routeRepoRemove(wsH, unknown, "missing")
	if unknown.Code != http.StatusNotFound {
		t.Errorf("an unknown name: got %d, want 404", unknown.Code)
	}
}

// TestHandleRepoRemove_ArbitraryNames proves stored names with spaces, slashes
// and percent signs are removed when they are sent escaped.
func TestHandleRepoRemove_ArbitraryNames(t *testing.T) {
	for _, name := range []string{"a b", "a/b", "100%"} {
		t.Run(name, func(t *testing.T) {
			server, _, st := newTestServer(t)
			wsH := newTestWorkspaceHandlers(server)
			seedRepos(t, st, []state.Repo{{Name: name, URL: "https://github.com/user/" + name}})

			rr := httptest.NewRecorder()
			routeRepoRemove(wsH, rr, name)
			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
			}
			if _, ok := server.repos.Get(name); ok {
				t.Errorf("repo %q should be gone", name)
			}
		})
	}
}

// TestRepoEntries_OnReposChanged proves the callback fires once per success
// and not at all on a failure.
func TestRepoEntries_OnReposChanged(t *testing.T) {
	server, _, _ := newTestServer(t)
	wsH := newTestWorkspaceHandlers(server)
	calls := 0
	wsH.onReposChanged = func() { calls++ }

	add := httptest.NewRecorder()
	wsH.handleRepoAdd(add, addRepoRequest(t, contracts.RepoAddRequest{URL: "https://github.com/acme/app.git"}))
	if add.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", add.Code, add.Body.String())
	}
	if calls != 1 {
		t.Errorf("callback calls after a successful add = %d, want 1", calls)
	}

	remove := httptest.NewRecorder()
	routeRepoRemove(wsH, remove, "app")
	if remove.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", remove.Code, remove.Body.String())
	}
	if calls != 2 {
		t.Errorf("callback calls after a successful remove = %d, want 2", calls)
	}

	failed := httptest.NewRecorder()
	routeRepoRemove(wsH, failed, "missing")
	if failed.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", failed.Code, failed.Body.String())
	}
	if calls != 2 {
		t.Errorf("callback calls after a failed remove = %d, want 2", calls)
	}
}

// TestRepoEntries_SameURLSpawnAndPost proves a repo posted to one server and
// spawned on another is the same entry.
func TestRepoEntries_SameURLSpawnAndPost(t *testing.T) {
	const repoURL = "https://github.com/anthropics/claude-code.git"

	postedServer, _, postedState := newTestServer(t)
	postedH := newTestWorkspaceHandlers(postedServer)
	rr := httptest.NewRecorder()
	postedH.handleRepoAdd(rr, addRepoRequest(t, contracts.RepoAddRequest{URL: repoURL}))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	spawnedServer, _, _ := newTestServer(t)
	spawnH := newTestSpawnHandlers(spawnedServer)
	spawnBody, err := json.Marshal(SpawnRequest{
		Repo:    repoURL,
		Branch:  "main",
		Targets: map[string]int{"command": 1},
		Prompt:  "hello",
	})
	if err != nil {
		t.Fatalf("marshal spawn request: %v", err)
	}
	spawnReq := httptest.NewRequest(http.MethodPost, "/api/spawn", bytes.NewReader(spawnBody))
	spawnReq.Header.Set("Content-Type", "application/json")
	spawnRR := httptest.NewRecorder()
	spawnH.handleSpawnPost(spawnRR, spawnReq)
	if spawnRR.Code != http.StatusOK {
		t.Fatalf("expected 200 from the spawn, got %d: %s", spawnRR.Code, spawnRR.Body.String())
	}

	posted, found := postedServer.repos.GetByURL(repoURL)
	if !found {
		t.Fatal("posted repo not found")
	}
	spawned, found := spawnedServer.repos.GetByURL(repoURL)
	if !found {
		t.Fatal("spawned repo not found")
	}
	if !reflect.DeepEqual(posted, spawned) {
		t.Errorf("posted entry %+v and spawned entry %+v differ", posted, spawned)
	}
	_ = postedState
}
