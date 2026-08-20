package workspace

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// setupOriginClone creates a bare origin with one commit on branch "feature"
// and a clone checked out on it. Returns (cloneDir, originDir, headSHA).
func setupOriginClone(t *testing.T) (string, string, string) {
	t.Helper()
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	gitCmd(t, base, "init", "--bare", origin)
	work := filepath.Join(base, "seed")
	gitCmd(t, base, "clone", origin, work)
	gitCmd(t, work, "checkout", "-b", "feature")
	gitCmd(t, work, "commit", "--allow-empty", "-m", "c1")
	gitCmd(t, work, "push", "-u", "origin", "feature")
	sha := gitCmd(t, work, "rev-parse", "HEAD")
	return work, origin, sha
}

func TestGitGetRemoteBranchHead_Origin(t *testing.T) {
	work, origin, sha := setupOriginClone(t)
	m := newTestManager(t, newTestState(t))
	head, err := m.gitBackend.GetRemoteBranchHead(context.Background(), work, "feature")
	if err != nil {
		t.Fatalf("GetRemoteBranchHead: %v", err)
	}
	if head.SHA != sha {
		t.Errorf("SHA = %q, want %q", head.SHA, sha)
	}
	if head.RemoteURL != origin {
		t.Errorf("RemoteURL = %q, want %q", head.RemoteURL, origin)
	}
}

func TestGitGetRemoteBranchHead_TrackingFork(t *testing.T) {
	work, _, sha := setupOriginClone(t)
	// Simulate a fork remote: add second remote, retarget tracking, drop origin ref.
	base := t.TempDir()
	fork := filepath.Join(base, "fork.git")
	gitCmd(t, base, "init", "--bare", fork)
	gitCmd(t, work, "remote", "add", "fork", fork)
	gitCmd(t, work, "push", "-u", "fork", "feature")
	gitCmd(t, work, "update-ref", "-d", "refs/remotes/origin/feature")
	m := newTestManager(t, newTestState(t))
	head, err := m.gitBackend.GetRemoteBranchHead(context.Background(), work, "feature")
	if err != nil {
		t.Fatalf("GetRemoteBranchHead: %v", err)
	}
	if head.SHA != sha {
		t.Errorf("SHA = %q, want %q", head.SHA, sha)
	}
	if head.RemoteURL != fork {
		t.Errorf("RemoteURL = %q, want fork URL %q", head.RemoteURL, fork)
	}
}

func TestGitGetRemoteBranchHead_NoRemote(t *testing.T) {
	base := t.TempDir()
	work := filepath.Join(base, "solo")
	gitCmd(t, base, "init", work)
	gitCmd(t, work, "checkout", "-b", "feature")
	gitCmd(t, work, "commit", "--allow-empty", "-m", "c1")
	m := newTestManager(t, newTestState(t))
	if _, err := m.gitBackend.GetRemoteBranchHead(context.Background(), work, "feature"); err == nil {
		t.Fatal("expected error for branch with no remote")
	}
}

func TestGitStatusReportsRemoteHeadSHA(t *testing.T) {
	work, _, sha := setupOriginClone(t)
	m := newTestManager(t, newTestState(t))
	_, _, _, _, _, _, _, _, _, _, _, _, remoteHeadSHA := m.gitStatus(context.Background(), "ws-test", RefreshTriggerExplicit, work, "https://example.com/repo.git")
	if remoteHeadSHA != sha {
		t.Errorf("remoteHeadSHA = %q, want %q", remoteHeadSHA, sha)
	}

	// A new push moves the remote ref; the next status reflects it.
	gitCmd(t, work, "commit", "--allow-empty", "-m", "c2")
	gitCmd(t, work, "push", "origin", "feature")
	newSHA := gitCmd(t, work, "rev-parse", "HEAD")
	_, _, _, _, _, _, _, _, _, _, _, _, remoteHeadSHA = m.gitStatus(context.Background(), "ws-test", RefreshTriggerExplicit, work, "https://example.com/repo.git")
	if remoteHeadSHA != newSHA {
		t.Errorf("after push remoteHeadSHA = %q, want %q", remoteHeadSHA, newSHA)
	}
}

func TestGitStatusRemoteHeadSHAEmptyWithoutRemote(t *testing.T) {
	base := t.TempDir()
	work := filepath.Join(base, "solo")
	gitCmd(t, base, "init", work)
	gitCmd(t, work, "checkout", "-b", "feature")
	gitCmd(t, work, "commit", "--allow-empty", "-m", "c1")
	m := newTestManager(t, newTestState(t))
	_, _, _, _, _, _, _, _, _, _, _, _, remoteHeadSHA := m.gitStatus(context.Background(), "ws-test", RefreshTriggerExplicit, work, "https://example.com/repo.git")
	if remoteHeadSHA != "" {
		t.Errorf("remoteHeadSHA = %q, want empty for local-only branch", remoteHeadSHA)
	}
}

func TestUpdateVCSStatusStoresRemoteHeadSHA(t *testing.T) {
	work, _, sha := setupOriginClone(t)
	st := newTestState(t)
	m := newTestManager(t, st)
	if err := st.AddWorkspace(state.Workspace{
		ID: "ws-head", Repo: "https://example.com/repo.git", Branch: "feature", Path: work,
	}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	if _, err := m.UpdateVCSStatus(context.Background(), "ws-head"); err != nil {
		t.Fatalf("UpdateVCSStatus: %v", err)
	}
	w, ok := st.GetWorkspace("ws-head")
	if !ok {
		t.Fatal("workspace missing")
	}
	if w.RemoteHeadSHA != sha {
		t.Errorf("state RemoteHeadSHA = %q, want %q", w.RemoteHeadSHA, sha)
	}
}
