package dashboard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func TestBuildSessionsResponsePastebin(t *testing.T) {
	t.Run("quick launch and pastebin both returned", func(t *testing.T) {
		server, _, st := newTestServer(t)
		wsPath := t.TempDir()
		st.AddWorkspace(state.Workspace{
			ID: "ws-both", Repo: "test", Branch: "main", Path: wsPath,
			Status: state.WorkspaceStatusRunning,
		})
		writeRepoConfig(t, wsPath, `{"quick_launch":[{"name":"tests","command":"./test.sh"}],"pastebin":["claude: fix the bug","  multi\n    line"]}`)

		server.workspace.(*workspace.Manager).RefreshWorkspaceConfig(state.Workspace{
			ID: "ws-both", Repo: "test", Branch: "main", Path: wsPath,
		})

		item := mustFindItem(t, server.sessionHandlers.buildSessionsResponse(), "ws-both")
		if len(item.QuickLaunch) != 1 || item.QuickLaunch[0] != "tests" {
			t.Errorf("QuickLaunch = %v, want [tests]", item.QuickLaunch)
		}
		if len(item.Pastebin) != 2 {
			t.Fatalf("Pastebin len = %d, want 2", len(item.Pastebin))
		}
		if item.Pastebin[0] != "claude: fix the bug" {
			t.Errorf("Pastebin[0] = %q, want exact text", item.Pastebin[0])
		}
		if item.Pastebin[1] != "  multi\n    line" {
			t.Errorf("Pastebin[1] = %q, want preserved multiline with indentation", item.Pastebin[1])
		}
	})

	t.Run("pastebin only workspace returns only pastebin", func(t *testing.T) {
		server, _, st := newTestServer(t)
		wsPath := t.TempDir()
		st.AddWorkspace(state.Workspace{
			ID: "ws-pb", Repo: "test", Branch: "main", Path: wsPath,
			Status: state.WorkspaceStatusRunning,
		})
		writeRepoConfig(t, wsPath, `{"pastebin":["just clip"]}`)

		server.workspace.(*workspace.Manager).RefreshWorkspaceConfig(state.Workspace{
			ID: "ws-pb", Repo: "test", Branch: "main", Path: wsPath,
		})

		item := mustFindItem(t, server.sessionHandlers.buildSessionsResponse(), "ws-pb")
		if len(item.QuickLaunch) != 0 {
			t.Errorf("QuickLaunch = %v, want empty", item.QuickLaunch)
		}
		if len(item.Pastebin) != 1 || item.Pastebin[0] != "just clip" {
			t.Errorf("Pastebin = %v, want [just clip]", item.Pastebin)
		}
	})

	t.Run("no cached repo config omits pastebin", func(t *testing.T) {
		server, _, st := newTestServer(t)
		wsPath := t.TempDir()
		st.AddWorkspace(state.Workspace{
			ID: "ws-empty", Repo: "test", Branch: "main", Path: wsPath,
			Status: state.WorkspaceStatusRunning,
		})
		// No .schmux/config.json exists — nothing to cache.

		item := mustFindItem(t, server.sessionHandlers.buildSessionsResponse(), "ws-empty")
		if len(item.Pastebin) != 0 {
			t.Errorf("Pastebin = %v, want empty when no repo config", item.Pastebin)
		}
	})

	t.Run("response construction copies the cached slice", func(t *testing.T) {
		server, _, st := newTestServer(t)
		wsPath := t.TempDir()
		st.AddWorkspace(state.Workspace{
			ID: "ws-copy", Repo: "test", Branch: "main", Path: wsPath,
			Status: state.WorkspaceStatusRunning,
		})
		writeRepoConfig(t, wsPath, `{"pastebin":["original"]}`)

		server.workspace.(*workspace.Manager).RefreshWorkspaceConfig(state.Workspace{
			ID: "ws-copy", Repo: "test", Branch: "main", Path: wsPath,
		})

		// First call returns the cached slice; mutate it and re-call to prove
		// the second call returns an independent copy.
		first := server.sessionHandlers.buildSessionsResponse()
		item1 := mustFindItem(t, first, "ws-copy")
		item1.Pastebin[0] = "MUTATED"
		item1.Pastebin = append(item1.Pastebin, "extra")

		second := server.sessionHandlers.buildSessionsResponse()
		item2 := mustFindItem(t, second, "ws-copy")
		if len(item2.Pastebin) != 1 || item2.Pastebin[0] != "original" {
			t.Errorf("Pastebin = %v, want [original] (caller mutation leaked into response)", item2.Pastebin)
		}
	})
}

func writeRepoConfig(t *testing.T, wsPath, body string) {
	t.Helper()
	dir := filepath.Join(wsPath, ".schmux")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustFindItem(t *testing.T, items []WorkspaceResponseItem, id string) WorkspaceResponseItem {
	t.Helper()
	for _, it := range items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("workspace %s not in response", id)
	return WorkspaceResponseItem{}
}
