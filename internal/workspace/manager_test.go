package workspace

import (
	"testing"

	"github.com/sergek/schmux/internal/config"
	"github.com/sergek/schmux/internal/state"
)

func TestExtractWorkspaceNumber(t *testing.T) {
	tests := []struct {
		id      string
		want    int
		wantErr bool
	}{
		{"test-001", 1, false},
		{"test-002", 2, false},
		{"test-123", 123, false},
		{"myproject-999", 999, false},
		{"invalid", 0, true},
		{"test-abc", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got, err := extractWorkspaceNumber(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractWorkspaceNumber() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("extractWorkspaceNumber() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	cfg := &config.Config{
		WorkspacePath: "/tmp/workspaces",
	}
	st := state.New()

	m := New(cfg, st)
	if m == nil {
		t.Error("New() returned nil")
	}
	if m.config != cfg {
		t.Error("config not set correctly")
	}
	if m.state != st {
		t.Error("state not set correctly")
	}
}

func TestGetWorkspacesForRepo(t *testing.T) {
	st := state.New()

	// Add some workspaces
	st.Workspaces = []state.Workspace{
		{ID: "test-001", Repo: "test", Path: "/tmp/test-001"},
		{ID: "test-002", Repo: "test", Path: "/tmp/test-002"},
		{ID: "other-001", Repo: "other", Path: "/tmp/other-001"},
	}

	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	m := New(cfg, st)

	workspaces := m.getWorkspacesForRepo("test")
	if len(workspaces) != 2 {
		t.Errorf("expected 2 workspaces, got %d", len(workspaces))
	}

	workspaces = m.getWorkspacesForRepo("other")
	if len(workspaces) != 1 {
		t.Errorf("expected 1 workspace, got %d", len(workspaces))
	}

	workspaces = m.getWorkspacesForRepo("nonexistent")
	if len(workspaces) != 0 {
		t.Errorf("expected 0 workspaces, got %d", len(workspaces))
	}
}

func TestMarkInUseAndRelease(t *testing.T) {
	st := state.New()

	w := state.Workspace{
		ID:     "test-001",
		Repo:   "test",
		Path:   "/tmp/test-001",
		InUse:  false,
		Usable: true,
	}

	st.AddWorkspace(w)

	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	m := New(cfg, st)

	// Mark as in use
	err := m.MarkInUse("test-001", "session-123")
	if err != nil {
		t.Errorf("MarkInUse() error = %v", err)
	}

	retrieved, _ := st.GetWorkspace("test-001")
	if !retrieved.InUse {
		t.Error("expected InUse to be true")
	}
	if retrieved.SessionID != "session-123" {
		t.Errorf("expected SessionID session-123, got %s", retrieved.SessionID)
	}

	// Release
	err = m.Release("test-001")
	if err != nil {
		t.Errorf("Release() error = %v", err)
	}

	retrieved, _ = st.GetWorkspace("test-001")
	if retrieved.InUse {
		t.Error("expected InUse to be false")
	}
	if retrieved.SessionID != "" {
		t.Errorf("expected SessionID to be empty, got %s", retrieved.SessionID)
	}
}
