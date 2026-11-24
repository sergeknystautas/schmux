package dashboard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/buildmonitor"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/state"
)

// TestBuildSessionsResponse_IncludesLeftoverRecyclableWorkspace verifies that a
// leftover record whose status is still "recyclable" surfaces like any other
// workspace — the status constant is gone, so nothing hides it.
func TestBuildSessionsResponse_IncludesLeftoverRecyclableWorkspace(t *testing.T) {
	server, _, st := newTestServer(t)

	st.AddWorkspace(state.Workspace{
		ID:     "active-001",
		Repo:   "test",
		Branch: "main",
		Path:   "/tmp/active",
		Status: state.WorkspaceStatusRunning,
	})
	st.AddWorkspace(state.Workspace{
		ID:     "recycled-001",
		Repo:   "test",
		Branch: "old-branch",
		Path:   "/tmp/recycled",
		Status: "recyclable",
	})

	response := server.sessionHandlers.buildSessionsResponse()

	seen := make(map[string]bool, len(response))
	for _, item := range response {
		seen[item.ID] = true
	}
	for _, id := range []string{"active-001", "recycled-001"} {
		if !seen[id] {
			t.Errorf("leftover recyclable workspace %q missing from buildSessionsResponse", id)
		}
	}
}

func TestBuildSessionsResponse_CarriesDedicated(t *testing.T) {
	server, _, st := newTestServer(t)

	if err := st.AddWorkspace(state.Workspace{
		ID:        "ws-dedicated",
		Repo:      "test",
		Branch:    "fix/client-performance",
		Path:      t.TempDir(),
		Status:    state.WorkspaceStatusRunning,
		Dedicated: "client-performance",
	}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	if err := st.AddWorkspace(state.Workspace{
		ID:     "ws-plain",
		Repo:   "test",
		Branch: "main",
		Path:   t.TempDir(),
		Status: state.WorkspaceStatusRunning,
	}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}

	response := server.sessionHandlers.buildSessionsResponse()

	seen := map[string]string{}
	for _, item := range response {
		seen[item.ID] = item.Dedicated
	}
	if got := seen["ws-dedicated"]; got != "client-performance" {
		t.Errorf("ws-dedicated dedicated = %q, want client-performance", got)
	}
	if got, ok := seen["ws-plain"]; !ok {
		t.Error("ws-plain missing from buildSessionsResponse")
	} else if got != "" {
		t.Errorf("ws-plain dedicated = %q, want empty", got)
	}
}

func TestBuildSessionsResponse_SurfacesFenceFlag(t *testing.T) {
	server, _, st := newTestServer(t)

	st.AddWorkspace(state.Workspace{
		ID:     "ws-fence",
		Repo:   "test",
		Branch: "main",
		Path:   "/tmp/ws-fence",
		Status: state.WorkspaceStatusRunning,
	})
	st.AddSession(state.Session{
		ID:          "sess-fenced",
		WorkspaceID: "ws-fence",
		Target:      "claude",
		TmuxSession: "sess-fenced",
		Fence:       true,
	})
	st.AddSession(state.Session{
		ID:          "sess-open",
		WorkspaceID: "ws-fence",
		Target:      "claude",
		TmuxSession: "sess-open",
	})

	response := server.sessionHandlers.buildSessionsResponse()

	var sawFenced, sawOpen bool
	for _, ws := range response {
		for _, s := range ws.Sessions {
			switch s.ID {
			case "sess-fenced":
				sawFenced = true
				if !s.Fence {
					t.Error("sess-fenced: response Fence = false, want true")
				}
			case "sess-open":
				sawOpen = true
				if s.Fence {
					t.Error("sess-open: response Fence = true, want false")
				}
			}
		}
	}
	if !sawFenced {
		t.Fatal("sess-fenced not found in response")
	}
	if !sawOpen {
		t.Fatal("sess-open not found in response")
	}
}

func TestBuildSessionsResponseSurfacesCIIconFromMonitor(t *testing.T) {
	server, _, st := newTestServer(t)
	repo := github.RepoInfo{Owner: "acme", Repo: "widget"}
	if err := st.AddWorkspace(state.Workspace{
		ID: "ws1", Repo: "https://github.com/acme/widget", Branch: "main",
		Path: t.TempDir(), Status: state.WorkspaceStatusRunning,
		RemoteBranchExists: true, RemoteHeadSHA: "head-sha",
	}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	server.buildMonitor.SetEnabledForTest(true)
	server.buildMonitor.SetRepoMetaForTest(repo, true, "")
	server.buildMonitor.RecordCommitForTest(repo, "main", "head-sha", buildmonitor.StatusSuccess, "https://run", true)
	server.prTracker.entries["ws1"] = PRRef{Number: 7, URL: "https://pr"}

	items := server.sessionHandlers.buildSessionsResponse()
	var item *WorkspaceResponseItem
	for i := range items {
		if items[i].ID == "ws1" {
			item = &items[i]
		}
	}
	if item == nil {
		t.Fatal("workspace ws1 missing from response")
	}
	if item.CIStatus != "success" || item.CIURL != "https://run" {
		t.Errorf("ci = (%q, %q), want (success, https://run)", item.CIStatus, item.CIURL)
	}
	if item.PRNumber != 7 || item.PRURL != "https://pr" {
		t.Errorf("pr = (%d, %q), want (7, https://pr)", item.PRNumber, item.PRURL)
	}
}

func TestBuildSessionsResponseOmitsStatusWhenProviderNil(t *testing.T) {
	server, _, st := newTestServer(t)
	st.AddWorkspace(state.Workspace{
		ID:     "ws1",
		Repo:   "test",
		Branch: "main",
		Path:   "/tmp/ws1",
		Status: state.WorkspaceStatusRunning,
	})
	// Both providers nil — chip absent, no PR.
	server.buildMonitor = nil
	server.prTracker = nil

	items := server.sessionHandlers.buildSessionsResponse()
	for _, item := range items {
		if item.CIStatus != "" || item.PRNumber != 0 {
			t.Errorf("expected empty status fields, got %q %d", item.CIStatus, item.PRNumber)
		}
	}
}

func TestBuildSessionsResponseQueuedForUnwatchedActiveRepo(t *testing.T) {
	// Regression: a brand-new workspace whose head the monitor has not yet
	// checked should surface queued when the repo has active workflows — the
	// status from a previous check pass is not required.
	server, _, st := newTestServer(t)
	repo := github.RepoInfo{Owner: "acme", Repo: "widget"}
	if err := st.AddWorkspace(state.Workspace{
		ID: "ws1", Repo: "https://github.com/acme/widget", Branch: "feature",
		Path: t.TempDir(), Status: state.WorkspaceStatusRunning,
		RemoteBranchExists: true, RemoteHeadSHA: "new-sha",
	}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	server.buildMonitor.SetEnabledForTest(true)
	server.buildMonitor.SetRepoMetaForTest(repo, true, "")

	items := server.sessionHandlers.buildSessionsResponse()
	var item *WorkspaceResponseItem
	for i := range items {
		if items[i].ID == "ws1" {
			item = &items[i]
		}
	}
	if item == nil {
		t.Fatal("ws1 missing from response")
	}
	if item.CIStatus != "queued" || item.CIURL != "" {
		t.Errorf("ci = (%q, %q), want (queued, \"\")", item.CIStatus, item.CIURL)
	}
}

func TestBuildSessionsResponseChipAbsentWhenNoWorkflows(t *testing.T) {
	server, _, st := newTestServer(t)
	repo := github.RepoInfo{Owner: "acme", Repo: "widget"}
	if err := st.AddWorkspace(state.Workspace{
		ID: "ws1", Repo: "https://github.com/acme/widget", Branch: "feature",
		Path: t.TempDir(), Status: state.WorkspaceStatusRunning,
		RemoteBranchExists: true, RemoteHeadSHA: "sha",
	}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	// Repo has no workflows → repo not active → chip absent.
	server.buildMonitor.SetEnabledForTest(true)
	server.buildMonitor.SetRepoMetaForTest(repo, false, "")

	items := server.sessionHandlers.buildSessionsResponse()
	for _, item := range items {
		if item.ID == "ws1" && item.CIStatus != "" {
			t.Errorf("expected absent chip, got CIStatus=%q", item.CIStatus)
		}
	}
}

func TestBuildSessionsResponseResolvedPath(t *testing.T) {
	server, _, st := newTestServer(t)
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	configured := filepath.Join(root, "configured")
	if err := os.Symlink(target, configured); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(configured)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	for _, ws := range []state.Workspace{
		{ID: "local", Repo: "test", Branch: "main", Path: configured, Status: state.WorkspaceStatusRunning},
		{ID: "remote", Repo: "test", Branch: "main", Path: configured, Status: state.WorkspaceStatusRunning, RemoteHostID: "host"},
		{ID: "missing", Repo: "test", Branch: "main", Path: filepath.Join(root, "missing"), Status: state.WorkspaceStatusRunning},
	} {
		if err := st.AddWorkspace(ws); err != nil {
			t.Fatalf("AddWorkspace(%s): %v", ws.ID, err)
		}
	}

	items := server.sessionHandlers.buildSessionsResponse()
	for _, tc := range []struct {
		id   string
		want string
	}{
		{id: "local", want: resolved},
		{id: "remote"},
		{id: "missing"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			for _, item := range items {
				if item.ID == tc.id {
					if item.ResolvedPath != tc.want {
						t.Errorf("ResolvedPath = %q, want %q", item.ResolvedPath, tc.want)
					}
					return
				}
			}
			t.Fatalf("workspace %s missing from response", tc.id)
		})
	}
}
