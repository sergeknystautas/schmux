package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
)

func writeGoModule(t *testing.T, dir, module string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+module+"\n"), 0600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
}

func TestHandleDevStatusDetectsSchmuxWithoutManagedSourceWorkspace(t *testing.T) {
	server, _, st := newTestServer(t)
	schmuxPath := t.TempDir()
	otherPath := t.TempDir()
	writeGoModule(t, schmuxPath, schmuxModulePath)
	writeGoModule(t, otherPath, "example.com/other")

	for _, workspace := range []state.Workspace{
		{ID: "schmux-002", Repo: "https://example.com/fork/schmux", Branch: "main", Path: schmuxPath},
		{ID: "other-001", Repo: "https://example.com/other", Branch: "main", Path: otherPath},
	} {
		if err := st.AddWorkspace(workspace); err != nil {
			t.Fatalf("add workspace %s: %v", workspace.ID, err)
		}
	}

	rr := httptest.NewRecorder()
	server.handleDevStatus(rr, httptest.NewRequest(http.MethodGet, "/api/dev/status", nil))

	var response struct {
		SchmuxWorkspaces []string `json:"schmux_workspaces"`
		SourceWorkspace  string   `json:"source_workspace"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if response.SourceWorkspace != "" {
		t.Fatalf("source_workspace = %q, want empty for fresh deployment", response.SourceWorkspace)
	}
	if len(response.SchmuxWorkspaces) != 1 || response.SchmuxWorkspaces[0] != "schmux-002" {
		t.Fatalf("schmux_workspaces = %v, want [schmux-002]", response.SchmuxWorkspaces)
	}
}

func TestHandleDevRebuildRejectsNonSchmuxWorkspace(t *testing.T) {
	server, _, st := newTestServer(t)
	path := t.TempDir()
	writeGoModule(t, path, "example.com/other")
	if err := st.AddWorkspace(state.Workspace{
		ID: "other-001", Repo: "https://example.com/other", Branch: "main", Path: path,
	}); err != nil {
		t.Fatalf("add workspace: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/dev/rebuild", bytes.NewBufferString(`{"workspace_id":"other-001","type":"both"}`))
	server.handleDevRebuild(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rr.Code, rr.Body.String())
	}
}
