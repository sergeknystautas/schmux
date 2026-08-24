package dashboard

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/buildmonitor"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestBuildSessionsResponse_ExcludesRecyclableWorkspaces(t *testing.T) {
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
		Status: state.WorkspaceStatusRecyclable,
	})

	response := server.sessionHandlers.buildSessionsResponse()

	for _, item := range response {
		if item.ID == "recycled-001" {
			t.Error("recyclable workspace should not appear in buildSessionsResponse")
		}
	}

	found := false
	for _, item := range response {
		if item.ID == "active-001" {
			found = true
			break
		}
	}
	if !found {
		t.Error("active workspace should appear in buildSessionsResponse")
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
	server.buildMonitor.RecordCommitForTest(repo, "head-sha", buildmonitor.StatusSuccess, "https://run", true)
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
