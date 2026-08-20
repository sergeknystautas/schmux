package workspace

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
)

// divergedSetup builds: feature branch pushed at commit A; local adds B;
// a second clone adds C and pushes; clone fetches. Returns the manager,
// state, workspace ID, and the subjects of the two divergent commits.
func divergedSetup(t *testing.T) (*Manager, *state.State, string) {
	t.Helper()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)

	runGit(t, cloneDir, "config", "user.name", "Alice")
	runGit(t, cloneDir, "config", "user.email", "alice@test.com")
	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "a.txt", "a")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "commit a")
	runGit(t, cloneDir, "push", "origin", "feature")

	// Local-only commit B
	writeFile(t, cloneDir, "b.txt", "b")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "local commit b")

	// Remote-only commit C from a second clone
	otherDir := filepath.Join(t.TempDir(), "other")
	runGit(t, filepath.Dir(otherDir), "clone", remoteDir, "other")
	runGit(t, otherDir, "config", "user.name", "Bob")
	runGit(t, otherDir, "config", "user.email", "bob@test.com")
	runGit(t, otherDir, "checkout", "feature")
	writeFile(t, otherDir, "c.txt", "c")
	runGit(t, otherDir, "add", ".")
	runGit(t, otherDir, "commit", "-m", "remote commit c")
	runGit(t, otherDir, "push", "origin", "feature")

	runGit(t, cloneDir, "fetch", "origin")

	st.AddWorkspace(state.Workspace{
		ID:     workspaceID,
		Repo:   remoteDir,
		Branch: "feature",
		Path:   cloneDir,
	})
	return m, st, workspaceID
}

func TestGetBranchDivergence_Diverged(t *testing.T) {
	t.Parallel()
	m, _, workspaceID := divergedSetup(t)

	res, err := m.GetBranchDivergence(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("GetBranchDivergence() error: %v", err)
	}
	if res.Branch != "feature" {
		t.Errorf("Branch = %q, want %q", res.Branch, "feature")
	}
	if res.LocalHead == "" || res.RemoteHead == "" {
		t.Errorf("LocalHead/RemoteHead must be set, got %+v", res)
	}
	if len(res.LocalCommits) != 1 || res.LocalCommits[0].Subject != "local commit b" {
		t.Errorf("LocalCommits = %+v, want one commit 'local commit b'", res.LocalCommits)
	}
	if len(res.RemoteCommits) != 1 || res.RemoteCommits[0].Subject != "remote commit c" {
		t.Errorf("RemoteCommits = %+v, want one commit 'remote commit c'", res.RemoteCommits)
	}
	if res.LocalCommits[0].Author != "Alice" || res.RemoteCommits[0].Author != "Bob" {
		t.Errorf("authors wrong: local=%q remote=%q", res.LocalCommits[0].Author, res.RemoteCommits[0].Author)
	}
	if res.LocalCommits[0].Hash == "" || res.LocalCommits[0].ShortHash == "" || res.LocalCommits[0].Timestamp == "" {
		t.Errorf("commit fields must be populated: %+v", res.LocalCommits[0])
	}
	if res.LocalTotal != 1 || res.RemoteTotal != 1 {
		t.Errorf("totals = %d/%d, want 1/1", res.LocalTotal, res.RemoteTotal)
	}
}

func TestGetBranchDivergence_AheadOnly(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)

	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "a.txt", "a")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "commit a")
	runGit(t, cloneDir, "push", "origin", "feature")
	writeFile(t, cloneDir, "b.txt", "b")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "local commit b")

	st.AddWorkspace(state.Workspace{ID: workspaceID, Repo: remoteDir, Branch: "feature", Path: cloneDir})

	res, err := m.GetBranchDivergence(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("GetBranchDivergence() error: %v", err)
	}
	if len(res.RemoteCommits) != 0 || res.RemoteTotal != 0 {
		t.Errorf("RemoteCommits should be empty for fast-forward, got %+v", res.RemoteCommits)
	}
	if len(res.LocalCommits) != 1 {
		t.Errorf("LocalCommits = %+v, want one commit", res.LocalCommits)
	}
}

func TestGetBranchDivergence_BehindOnly(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)

	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "a.txt", "a")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "commit a")
	runGit(t, cloneDir, "push", "origin", "feature")

	otherDir := filepath.Join(t.TempDir(), "other")
	runGit(t, filepath.Dir(otherDir), "clone", remoteDir, "other")
	runGit(t, otherDir, "config", "user.name", "Bob")
	runGit(t, otherDir, "config", "user.email", "bob@test.com")
	runGit(t, otherDir, "checkout", "feature")
	writeFile(t, otherDir, "c.txt", "c")
	runGit(t, otherDir, "add", ".")
	runGit(t, otherDir, "commit", "-m", "remote commit c")
	runGit(t, otherDir, "push", "origin", "feature")
	runGit(t, cloneDir, "fetch", "origin")

	st.AddWorkspace(state.Workspace{ID: workspaceID, Repo: remoteDir, Branch: "feature", Path: cloneDir})

	res, err := m.GetBranchDivergence(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("GetBranchDivergence() error: %v", err)
	}
	if len(res.LocalCommits) != 0 {
		t.Errorf("LocalCommits should be empty when behind, got %+v", res.LocalCommits)
	}
	if len(res.RemoteCommits) != 1 || res.RemoteCommits[0].Subject != "remote commit c" {
		t.Errorf("RemoteCommits = %+v, want one commit 'remote commit c'", res.RemoteCommits)
	}
}

func TestGetBranchDivergence_NoRemoteBranch(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)

	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "a.txt", "a")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "unpushed commit")

	st.AddWorkspace(state.Workspace{ID: workspaceID, Repo: remoteDir, Branch: "feature", Path: cloneDir})

	res, err := m.GetBranchDivergence(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("GetBranchDivergence() error: %v", err)
	}
	if res.RemoteHead != "" || len(res.RemoteCommits) != 0 {
		t.Errorf("remote side should be empty without origin/feature, got %+v", res)
	}
	// No remote branch: everything reachable is "ahead", newest first.
	if len(res.LocalCommits) == 0 || res.LocalCommits[0].Subject != "unpushed commit" {
		t.Errorf("LocalCommits[0] = %+v, want newest to be 'unpushed commit'", res.LocalCommits)
	}
}

func TestGetBranchDivergence_UnknownWorkspace(t *testing.T) {
	t.Parallel()
	_, _, m, _, _ := setupPushTest(t)
	if _, err := m.GetBranchDivergence(context.Background(), "nope"); err == nil {
		t.Error("GetBranchDivergence() should error for unknown workspace")
	}
}

// TestGetBranchDivergence_EmptyListsEncodeAsArrays guards the JSON contract:
// empty directions must marshal as [] not null — the dashboard dereferences
// .length on both lists.
func TestGetBranchDivergence_EmptyListsEncodeAsArrays(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)

	runGit(t, cloneDir, "checkout", "-b", "feature")
	writeFile(t, cloneDir, "a.txt", "a")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "unpushed commit")

	st.AddWorkspace(state.Workspace{ID: workspaceID, Repo: remoteDir, Branch: "feature", Path: cloneDir})

	res, err := m.GetBranchDivergence(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("GetBranchDivergence() error: %v", err)
	}
	out, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}
	if strings.Contains(string(out), `"remote_commits":null`) || strings.Contains(string(out), `"local_commits":null`) {
		t.Errorf("empty commit lists must encode as [], got %s", out)
	}
}
