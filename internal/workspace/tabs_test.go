package workspace

import (
	"fmt"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// newTestState creates a fresh state.State for testing.
func newTestState(t *testing.T) *state.State {
	t.Helper()
	statePath := t.TempDir() + "/state.json"
	return state.New(statePath, nil)
}

// newTestManager creates a workspace Manager backed by a real state.State.
func newTestManager(t *testing.T, st *state.State) *Manager {
	t.Helper()
	cfg := &config.Config{}
	cfg.WorkspacePath = t.TempDir()
	return New(cfg, st, t.TempDir()+"/state.json", testLogger())
}

func TestMutateTabsAndSave(t *testing.T) {
	st := newTestState(t)
	m := newTestManager(t, st)

	var broadcastCalled bool
	m.SetBroadcastFn(func() { broadcastCalled = true })

	executed := false
	err := m.mutateTabsAndSave(func() error {
		executed = true
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !executed {
		t.Fatal("function was not executed")
	}
	if !broadcastCalled {
		t.Fatal("broadcast was not called")
	}
}

func TestMutateTabsAndSave_ErrorSkipsSaveAndBroadcast(t *testing.T) {
	st := newTestState(t)
	m := newTestManager(t, st)

	var broadcastCalled bool
	m.SetBroadcastFn(func() { broadcastCalled = true })

	err := m.mutateTabsAndSave(func() error {
		return fmt.Errorf("something failed")
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if broadcastCalled {
		t.Fatal("broadcast should not be called on error")
	}
}

func TestSeedSystemTabs_GitWorkspace(t *testing.T) {
	st := newTestState(t)
	m := newTestManager(t, st)
	m.SetBroadcastFn(func() {})

	ws := state.Workspace{ID: "ws1", Repo: "repo", Branch: "main", Path: "/tmp/ws1", VCS: "git"}
	st.AddWorkspace(ws)

	err := m.SeedSystemTabs("ws1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tabs := st.GetWorkspaceTabs("ws1")
	if len(tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(tabs))
	}
	// AddTab prepends, so last seeded is first
	if tabs[0].Kind != "diff" {
		t.Errorf("expected first tab to be diff, got %s", tabs[0].Kind)
	}
	if tabs[0].ID != "sys-diff-ws1" {
		t.Errorf("expected diff tab ID sys-diff-ws1, got %s", tabs[0].ID)
	}
	if tabs[0].Route != "/diff/ws1" {
		t.Errorf("expected diff route /diff/ws1, got %s", tabs[0].Route)
	}
	if tabs[0].Closable {
		t.Error("diff tab should not be closable")
	}
	if tabs[1].Kind != "git" {
		t.Errorf("expected second tab to be git, got %s", tabs[1].Kind)
	}
	if tabs[1].ID != "sys-git-ws1" {
		t.Errorf("expected git tab ID sys-git-ws1, got %s", tabs[1].ID)
	}
	if tabs[1].Route != "/commits/ws1" {
		t.Errorf("expected git route /commits/ws1, got %s", tabs[1].Route)
	}
}

func TestSeedSystemTabs_NonVCSWorkspace(t *testing.T) {
	st := newTestState(t)
	m := newTestManager(t, st)
	m.SetBroadcastFn(func() {})

	ws := state.Workspace{ID: "ws1", Repo: "repo", Branch: "main", Path: "/tmp/ws1", VCS: "none"}
	st.AddWorkspace(ws)

	err := m.SeedSystemTabs("ws1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tabs := st.GetWorkspaceTabs("ws1")
	if len(tabs) != 0 {
		t.Fatalf("expected 0 tabs for non-VCS workspace, got %d", len(tabs))
	}
}

func TestOpenCommitTab(t *testing.T) {
	st := newTestState(t)
	m := newTestManager(t, st)
	m.SetBroadcastFn(func() {})

	ws := state.Workspace{ID: "ws1", Repo: "repo", Branch: "main", Path: "/tmp/ws1"}
	st.AddWorkspace(ws)

	tab, err := m.OpenCommitTab("ws1", "abc123def456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tab.Kind != "commit" {
		t.Errorf("expected kind commit, got %s", tab.Kind)
	}
	if tab.Route != "/commits/ws1/abc123d" {
		t.Errorf("expected route /commits/ws1/abc123d, got %s", tab.Route)
	}
	if tab.Label != "commit abc123d" {
		t.Errorf("expected label 'commit abc123d', got %s", tab.Label)
	}
	if !tab.Closable {
		t.Error("commit tab should be closable")
	}
	if tab.Meta["hash"] != "abc123def456" {
		t.Errorf("expected meta hash abc123def456, got %s", tab.Meta["hash"])
	}
	if tab.ID == "" {
		t.Error("tab ID should be set")
	}
}

func TestOpenMarkdownTab(t *testing.T) {
	st := newTestState(t)
	m := newTestManager(t, st)
	m.SetBroadcastFn(func() {})

	ws := state.Workspace{ID: "ws1", Repo: "repo", Branch: "main", Path: "/tmp/ws1"}
	st.AddWorkspace(ws)

	tab, err := m.OpenMarkdownTab("ws1", "docs/README.md")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tab.Kind != "markdown" {
		t.Errorf("expected kind markdown, got %s", tab.Kind)
	}
	if tab.Route != "/diff/ws1/md/docs%2FREADME.md" {
		t.Errorf("expected encoded route, got %s", tab.Route)
	}
	if tab.Label != "README.md" {
		t.Errorf("expected label README.md, got %s", tab.Label)
	}
	if !tab.Closable {
		t.Error("markdown tab should be closable")
	}
	if tab.Meta["filepath"] != "docs/README.md" {
		t.Errorf("expected meta filepath, got %s", tab.Meta["filepath"])
	}
}

func TestOpenPreviewTab(t *testing.T) {
	st := newTestState(t)
	m := newTestManager(t, st)
	m.SetBroadcastFn(func() {})

	ws := state.Workspace{ID: "ws1", Repo: "repo", Branch: "main", Path: "/tmp/ws1"}
	st.AddWorkspace(ws)

	tab, err := m.OpenPreviewTab("ws1", "prev-123", 3000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tab.Kind != "preview" {
		t.Errorf("expected kind preview, got %s", tab.Kind)
	}
	if tab.ID != "sys-preview-prev-123" {
		t.Errorf("expected ID sys-preview-prev-123, got %s", tab.ID)
	}
	if tab.Route != "/preview/ws1/prev-123" {
		t.Errorf("expected route /preview/ws1/prev-123, got %s", tab.Route)
	}
	if tab.Label != "web:3000" {
		t.Errorf("expected label web:3000, got %s", tab.Label)
	}
	if !tab.Closable {
		t.Error("preview tab should be closable")
	}
	if tab.Meta["preview_id"] != "prev-123" {
		t.Errorf("expected meta preview_id prev-123, got %s", tab.Meta["preview_id"])
	}
}

func TestOpenResolveConflictTab(t *testing.T) {
	st := newTestState(t)
	m := newTestManager(t, st)
	m.SetBroadcastFn(func() {})

	ws := state.Workspace{ID: "ws1", Repo: "repo", Branch: "main", Path: "/tmp/ws1"}
	st.AddWorkspace(ws)

	tab, err := m.OpenResolveConflictTab("ws1", "abc123def456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tab.Kind != "resolve-conflict" {
		t.Errorf("expected kind resolve-conflict, got %s", tab.Kind)
	}
	if tab.ID != "sys-resolve-conflict-abc123d" {
		t.Errorf("expected ID sys-resolve-conflict-abc123d, got %s", tab.ID)
	}
	if tab.Route != "/resolve-conflict/ws1/sys-resolve-conflict-abc123d" {
		t.Errorf("expected route, got %s", tab.Route)
	}
	if !tab.Closable {
		t.Error("resolve-conflict tab should be closable")
	}
	if tab.Meta["hash"] != "abc123d" {
		t.Errorf("expected meta hash abc123d, got %s", tab.Meta["hash"])
	}
}

func TestOpenCommitTab_Dedup(t *testing.T) {
	st := newTestState(t)
	m := newTestManager(t, st)
	m.SetBroadcastFn(func() {})

	ws := state.Workspace{ID: "ws1", Repo: "repo", Branch: "main", Path: "/tmp/ws1"}
	st.AddWorkspace(ws)

	_, err := m.OpenCommitTab("ws1", "abc123def456")
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	_, err = m.OpenCommitTab("ws1", "abc123def456")
	if err != nil {
		t.Fatalf("second open: %v", err)
	}

	tabs := st.GetWorkspaceTabs("ws1")
	commitCount := 0
	for _, tab := range tabs {
		if tab.Kind == "commit" {
			commitCount++
		}
	}
	if commitCount != 1 {
		t.Errorf("expected 1 commit tab after dedup, got %d", commitCount)
	}
}

func TestCloseTab(t *testing.T) {
	st := newTestState(t)
	m := newTestManager(t, st)
	m.SetBroadcastFn(func() {})

	ws := state.Workspace{ID: "ws1", Repo: "repo", Branch: "main", Path: "/tmp/ws1", VCS: "none"}
	st.AddWorkspace(ws)

	tab, _ := m.OpenCommitTab("ws1", "abc123def456")

	err := m.CloseTab("ws1", tab.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tabs := st.GetWorkspaceTabs("ws1")
	if len(tabs) != 0 {
		t.Fatalf("expected 0 tabs after close, got %d", len(tabs))
	}
}

func TestCloseTab_NonClosable(t *testing.T) {
	st := newTestState(t)
	m := newTestManager(t, st)
	m.SetBroadcastFn(func() {})

	ws := state.Workspace{ID: "ws1", Repo: "repo", Branch: "main", Path: "/tmp/ws1", VCS: "git"}
	st.AddWorkspace(ws)
	m.SeedSystemTabs("ws1")

	// diff tab is not closable
	tabs := st.GetWorkspaceTabs("ws1")
	var diffTab state.Tab
	for _, t := range tabs {
		if t.Kind == "diff" {
			diffTab = t
			break
		}
	}

	err := m.CloseTab("ws1", diffTab.ID)
	if err == nil {
		t.Fatal("expected error for non-closable tab")
	}
}

type mockCloseHook struct {
	canClose    bool
	canCloseErr error
	closeCalled bool
	closeErr    error
}

func (h *mockCloseHook) CanClose(wsID string, tab state.Tab) (bool, error) {
	return h.canClose, h.canCloseErr
}

func (h *mockCloseHook) OnTabClose(wsID string, tab state.Tab) error {
	h.closeCalled = true
	return h.closeErr
}

func TestCloseTab_HookCalled(t *testing.T) {
	st := newTestState(t)
	m := newTestManager(t, st)
	m.SetBroadcastFn(func() {})

	hook := &mockCloseHook{canClose: true}
	m.RegisterTabCloseHook("commit", hook)

	ws := state.Workspace{ID: "ws1", Repo: "repo", Branch: "main", Path: "/tmp/ws1", VCS: "none"}
	st.AddWorkspace(ws)
	tab, _ := m.OpenCommitTab("ws1", "abc123def456")

	err := m.CloseTab("ws1", tab.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hook.closeCalled {
		t.Fatal("hook OnTabClose was not called")
	}
}

func TestCloseTab_CanCloseRefused(t *testing.T) {
	st := newTestState(t)
	m := newTestManager(t, st)
	m.SetBroadcastFn(func() {})

	hook := &mockCloseHook{canClose: false}
	m.RegisterTabCloseHook("commit", hook)

	ws := state.Workspace{ID: "ws1", Repo: "repo", Branch: "main", Path: "/tmp/ws1", VCS: "none"}
	st.AddWorkspace(ws)
	tab, _ := m.OpenCommitTab("ws1", "abc123def456")

	err := m.CloseTab("ws1", tab.ID)
	if err == nil {
		t.Fatal("expected error when CanClose returns false")
	}
	// Tab should still exist
	tabs := st.GetWorkspaceTabs("ws1")
	if len(tabs) != 1 {
		t.Fatalf("expected tab to remain after CanClose refusal, got %d tabs", len(tabs))
	}
}

func TestCloseTab_HookFailure_RollsBack(t *testing.T) {
	st := newTestState(t)
	m := newTestManager(t, st)
	m.SetBroadcastFn(func() {})

	hook := &mockCloseHook{canClose: true, closeErr: fmt.Errorf("cleanup failed")}
	m.RegisterTabCloseHook("commit", hook)

	ws := state.Workspace{ID: "ws1", Repo: "repo", Branch: "main", Path: "/tmp/ws1", VCS: "none"}
	st.AddWorkspace(ws)
	tab, _ := m.OpenCommitTab("ws1", "abc123def456")

	err := m.CloseTab("ws1", tab.ID)
	if err == nil {
		t.Fatal("expected error from hook failure")
	}
	// Tab should be rolled back
	tabs := st.GetWorkspaceTabs("ws1")
	if len(tabs) != 1 {
		t.Fatalf("expected tab to be rolled back after hook failure, got %d tabs", len(tabs))
	}
}

func TestAddWorkspaceWithTabs_SeedsSystemTabs(t *testing.T) {
	st := newTestState(t)
	m := newTestManager(t, st)
	m.SetBroadcastFn(func() {})

	ws := state.Workspace{ID: "ws1", Repo: "repo", Branch: "main", Path: "/tmp/ws1", VCS: "git"}
	err := m.AddWorkspaceWithTabs(ws)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tabs := st.GetWorkspaceTabs("ws1")
	if len(tabs) != 2 {
		t.Fatalf("expected 2 system tabs, got %d", len(tabs))
	}
}

func TestAddWorkspaceWithTabs_NoVCS_NoTabs(t *testing.T) {
	st := newTestState(t)
	m := newTestManager(t, st)
	m.SetBroadcastFn(func() {})

	ws := state.Workspace{ID: "ws1", Repo: "repo", Branch: "main", Path: "/tmp/ws1", VCS: "none"}
	err := m.AddWorkspaceWithTabs(ws)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tabs := st.GetWorkspaceTabs("ws1")
	if len(tabs) != 0 {
		t.Fatalf("expected 0 tabs for non-VCS workspace, got %d", len(tabs))
	}
}
