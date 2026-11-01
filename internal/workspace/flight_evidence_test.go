package workspace

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
)

func TestFlightEvidenceCollectsGitBranchHeadAndDiffCounts(t *testing.T) {
	origin := gitTestWorkTree(t)
	work := filepath.Join(t.TempDir(), "work")
	runGit(t, filepath.Dir(work), "clone", origin, work)
	runGit(t, work, "checkout", "-b", "feature/evidence")
	writeFile(t, work, "committed.txt", "one\ntwo\n")
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-m", "add committed file")
	writeFile(t, work, "README.md", "changed readme\n")
	head := strings.TrimSpace(runGitOut(t, work, "rev-parse", "HEAD"))
	base := strings.TrimSpace(runGitOut(t, work, "rev-parse", "origin/main"))

	st := newTestState(t)
	if err := st.AddWorkspace(state.Workspace{ID: "ws-1", Repo: "https://example.com/r.git", Branch: "feature/evidence", Path: work}); err != nil {
		t.Fatal(err)
	}
	m := newTestManager(t, st)

	got, err := m.FlightEvidence(context.Background(), "ws-1", "", "")
	if err != nil {
		t.Fatalf("FlightEvidence: %v", err)
	}

	if got.Error != "" {
		t.Errorf("Error = %q, want empty", got.Error)
	}
	if got.VCS != "git" || got.Branch != "feature/evidence" || got.HeadSHA != head {
		t.Errorf("identity = vcs %q branch %q head %q, want git/feature/evidence/%s", got.VCS, got.Branch, got.HeadSHA, head)
	}
	if got.BaseBranch != "main" || got.BaseSHA != base {
		t.Errorf("base = %q %q, want main %s", got.BaseBranch, got.BaseSHA, base)
	}
	if !got.Dirty || got.Ahead != 1 || got.Behind != 0 {
		t.Errorf("dirty/ahead/behind = %v/%d/%d, want true/1/0", got.Dirty, got.Ahead, got.Behind)
	}
	if got.FilesChanged < 1 || got.LinesAdded < 1 {
		t.Errorf("diff counts = %d files, +%d -%d", got.FilesChanged, got.LinesAdded, got.LinesRemoved)
	}
	if !slices.Contains(got.ChangedFiles, "README.md") {
		t.Errorf("ChangedFiles = %v, want README.md", got.ChangedFiles)
	}
	// Collection must not change the workspace.
	if status := strings.TrimSpace(runGitOut(t, work, "status", "--porcelain")); status != "M README.md" {
		t.Errorf("workspace status after collection = %q", status)
	}
}

func TestFlightEvidenceRecordsCollectionErrorWithoutLosingIdentity(t *testing.T) {
	st := newTestState(t)
	gone := filepath.Join(t.TempDir(), "removed-workspace")
	if err := st.AddWorkspace(state.Workspace{ID: "ws-gone", Repo: "https://example.com/r.git", Branch: "feature/lost", Path: gone, VCS: "git"}); err != nil {
		t.Fatal(err)
	}
	m := newTestManager(t, st)

	got, err := m.FlightEvidence(context.Background(), "ws-gone", "", "")

	if err == nil {
		t.Fatal("FlightEvidence reported no error for a workspace directory that no longer exists")
	}
	if got.Error == "" {
		t.Errorf("Error field empty; the collection failure must be recorded in the evidence")
	}
	if got.VCS != "git" || got.Branch != "feature/lost" {
		t.Errorf("identity lost: vcs %q branch %q", got.VCS, got.Branch)
	}
	if _, statErr := os.Stat(gone); !os.IsNotExist(statErr) {
		t.Errorf("collection created the missing workspace directory: %v", statErr)
	}
}

func TestFlightEvidenceUsesRemoteBranchWhenWorkspaceStateIsGone(t *testing.T) {
	_, origin, sha := setupOriginClone(t)
	m := newTestManager(t, newTestState(t))

	got, err := m.FlightEvidence(context.Background(), "ws-gone", origin, "feature")

	if err != nil {
		t.Fatalf("FlightEvidence: %v", err)
	}
	if got.Error != "" || got.RepoURL != origin || got.Branch != "feature" {
		t.Fatalf("identity = %+v, want remote origin feature without an error", got)
	}
	if got.HeadSHA != sha || !got.RemoteBranchExists {
		t.Errorf("remote evidence = head %q exists %v, want %s/true", got.HeadSHA, got.RemoteBranchExists, sha)
	}
}

func TestFlightEvidenceUnknownWorkspaceReturnsError(t *testing.T) {
	m := newTestManager(t, newTestState(t))

	got, err := m.FlightEvidence(context.Background(), "nope", "", "")

	if err == nil || got.Error == "" {
		t.Fatalf("err = %v, state = %+v; want an error recorded in both", err, got)
	}
}
