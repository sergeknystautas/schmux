package dashboard

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
)

// TestDismissNudge_Stored proves the dismissal lands on the stored repo
// entry, in the state the daemon writes.
func TestDismissNudge_Stored(t *testing.T) {
	server, _, st := newTestServer(t)
	seedRepos(t, st, []state.Repo{{Name: "myrepo", URL: "https://github.com/user/myrepo.git"}})
	wsH := newTestWorkspaceHandlers(server)

	body := []byte(`{"repo_name":"myrepo"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/overlays/dismiss-nudge", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	wsH.handleDismissNudge(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	stored, found := server.repos.Get("myrepo")
	if !found {
		t.Fatal("myrepo not found")
	}
	if !stored.OverlayNudgeDismissed {
		t.Error("OverlayNudgeDismissed = false, want true")
	}

	// The change is on disk.
	onDisk, err := state.Load(server.statePath, nil)
	if err != nil {
		t.Fatalf("Load state: %v", err)
	}
	diskEntry, found := onDisk.GetRepo("myrepo")
	if !found {
		t.Fatal("myrepo not found on disk")
	}
	if !diskEntry.OverlayNudgeDismissed {
		t.Error("stored OverlayNudgeDismissed = false, want true")
	}
}

// TestDismissNudge_UnknownRepo proves an unknown repo name is rejected.
func TestDismissNudge_UnknownRepo(t *testing.T) {
	server, _, _ := newTestServer(t)
	wsH := newTestWorkspaceHandlers(server)

	body := []byte(`{"repo_name":"missing"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/overlays/dismiss-nudge", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	wsH.handleDismissNudge(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("Unknown repo")) {
		t.Errorf("expected error to mention 'Unknown repo', got %q", rr.Body.String())
	}
}

// TestOverlayAdd_Stored proves the overlay paths land on the stored repo
// entry, in the state the daemon writes.
func TestOverlayAdd_Stored(t *testing.T) {
	server, _, st := newTestServer(t)
	seedRepos(t, st, []state.Repo{{Name: "myrepo", URL: "https://github.com/user/myrepo.git"}})
	wsH := newTestWorkspaceHandlers(server)

	wsPath := t.TempDir()
	if err := st.AddWorkspace(state.Workspace{
		ID:   "ws-1",
		Repo: "https://github.com/user/myrepo.git",
		Path: wsPath,
	}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}

	body := []byte(`{"workspace_id":"ws-1","repo_name":"myrepo","custom_paths":[".tool-versions"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/overlay-add", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	wsH.handleOverlayAdd(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	stored, found := server.repos.Get("myrepo")
	if !found {
		t.Fatal("myrepo not found")
	}
	if !reflect.DeepEqual(stored.OverlayPaths, []string{".tool-versions"}) {
		t.Errorf("OverlayPaths = %v, want [.tool-versions]", stored.OverlayPaths)
	}

	// The change is on disk.
	onDisk, err := state.Load(server.statePath, nil)
	if err != nil {
		t.Fatalf("Load state: %v", err)
	}
	diskEntry, found := onDisk.GetRepo("myrepo")
	if !found {
		t.Fatal("myrepo not found on disk")
	}
	if !reflect.DeepEqual(diskEntry.OverlayPaths, []string{".tool-versions"}) {
		t.Errorf("stored OverlayPaths = %v, want [.tool-versions]", diskEntry.OverlayPaths)
	}
}
