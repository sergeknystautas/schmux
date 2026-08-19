package dashboard

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspacestatus"
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

func TestBuildSessionsResponseIncludesWorkspaceStatus(t *testing.T) {
	server, _, st := newTestServer(t)
	st.AddWorkspace(state.Workspace{
		ID:     "ws1",
		Repo:   "test",
		Branch: "main",
		Path:   "/tmp/ws1",
		Status: state.WorkspaceStatusRunning,
	})
	server.workspaceStatus.Store("ws1", workspacestatus.Entry{
		Status: workspacestatus.Status{CIStatus: workspacestatus.CISuccess, CIURL: "https://run", PRNumber: 7, PRURL: "https://pr"},
	})

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
	if item.CIStatus != "success" || item.CIURL != "https://run" || item.PRNumber != 7 || item.PRURL != "https://pr" {
		t.Errorf("status fields = %q %q %d %q", item.CIStatus, item.CIURL, item.PRNumber, item.PRURL)
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

	items := server.sessionHandlers.buildSessionsResponse()
	for _, item := range items {
		if item.CIStatus != "" || item.PRNumber != 0 {
			t.Errorf("expected empty status fields, got %q %d", item.CIStatus, item.PRNumber)
		}
	}
}
