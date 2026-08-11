package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
)

// (Task 3 adds "os" and "os/exec" to this import list for its hook helpers.)

// addPushWorkspace registers the clone as a workspace on the given branch.
func addPushWorkspace(t *testing.T, st *state.State, workspaceID, remoteDir, cloneDir, branch string) {
	t.Helper()
	st.AddWorkspace(state.Workspace{
		ID:     workspaceID,
		Repo:   remoteDir,
		Branch: branch,
		Path:   cloneDir,
	})
}

// commitFile creates one commit touching a single file and returns its sha.
func commitFile(t *testing.T, dir, name, content, message string) string {
	t.Helper()
	writeFile(t, dir, name, content)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", message)
	return strings.TrimSpace(runGitOut(t, dir, "rev-parse", "HEAD"))
}

func TestPushCommits_MalformedHash(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "main")

	for _, bad := range []string{
		"",                      // empty
		"abc123",                // abbreviated
		"main",                  // refname
		"--force",               // flag-shaped
		"HEAD",                  // symbolic
		strings.Repeat("G", 40), // non-hex
		strings.Repeat("a", 39), // wrong length
	} {
		_, err := m.PushCommits(context.Background(), workspaceID, bad, "default", false, false)
		if !errors.Is(err, ErrMalformedHash) {
			t.Errorf("PushCommits(%q) error = %v, want ErrMalformedHash", bad, err)
		}
	}
}

func TestPushCommits_InvalidTarget(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "main")
	sha := runGitOut(t, cloneDir, "rev-parse", "HEAD")

	_, err := m.PushCommits(context.Background(), workspaceID, sha, "upstream", false, false)
	if !errors.Is(err, ErrInvalidPushTarget) {
		t.Errorf("PushCommits(target=upstream) error = %v, want ErrInvalidPushTarget", err)
	}
}

func TestPushCommits_StaleHash(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "main")

	// A tree object sha: full-length hex but not a commit.
	treeSha := runGitOut(t, cloneDir, "rev-parse", "HEAD^{tree}")
	if _, err := m.PushCommits(context.Background(), workspaceID, treeSha, "default", false, false); !errors.Is(err, ErrStaleHash) {
		t.Errorf("PushCommits(tree sha) error = %v, want ErrStaleHash", err)
	}

	// A commit that was uncommitted: still in the object db, no longer an ancestor of HEAD.
	gone := commitFile(t, cloneDir, "gone.txt", "x", "will be uncommitted")
	runGit(t, cloneDir, "reset", "--hard", "HEAD~1")
	runGit(t, cloneDir, "checkout", ".") // clean tree after reset
	if _, err := m.PushCommits(context.Background(), workspaceID, gone, "default", false, false); !errors.Is(err, ErrStaleHash) {
		t.Errorf("PushCommits(uncommitted sha) error = %v, want ErrStaleHash", err)
	}

	// An unknown sha.
	unknown := strings.Repeat("0", 39) + "1"
	if _, err := m.PushCommits(context.Background(), workspaceID, unknown, "default", false, false); !errors.Is(err, ErrStaleHash) {
		t.Errorf("PushCommits(unknown sha) error = %v, want ErrStaleHash", err)
	}
}

func TestPushCommits_DirtyWorkspaceRejected(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "main")

	sha := commitFile(t, cloneDir, "work.txt", "v1", "committed work")
	writeFile(t, cloneDir, "dirty.txt", "uncommitted")

	result, err := m.PushCommits(context.Background(), workspaceID, sha, "default", false, false)
	if err != nil {
		t.Fatalf("PushCommits() error: %v", err)
	}
	if result.Success || result.Reason != PushReasonDirty {
		t.Errorf("want reason %q, got: %+v", PushReasonDirty, result)
	}
}

func TestPushCommits_NothingToPush(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "main")

	// HEAD is already on origin/main (fresh clone, nothing local).
	sha := runGitOut(t, cloneDir, "rev-parse", "HEAD")
	result, err := m.PushCommits(context.Background(), workspaceID, sha, "default", false, false)
	if err != nil {
		t.Fatalf("PushCommits() error: %v", err)
	}
	if result.Success || result.Reason != PushReasonNothingToPush {
		t.Errorf("want reason %q, got: %+v", PushReasonNothingToPush, result)
	}
}

func TestPushCommits_DivergedFromDefault(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "main")

	// Local commit...
	sha := commitFile(t, cloneDir, "local.txt", "local", "local commit")

	// ...while someone else pushes to origin/main.
	otherDir := filepath.Join(t.TempDir(), "other")
	runGit(t, filepath.Dir(otherDir), "clone", remoteDir, "other")
	runGit(t, otherDir, "config", "user.email", "other@test.com")
	runGit(t, otherDir, "config", "user.name", "Other")
	commitFile(t, otherDir, "other.txt", "other", "other commit")
	runGit(t, otherDir, "push", "origin", "main")

	result, err := m.PushCommits(context.Background(), workspaceID, sha, "default", false, false)
	if err != nil {
		t.Fatalf("PushCommits() error: %v", err)
	}
	if result.Success || result.Reason != PushReasonDiverged {
		t.Errorf("want reason %q, got: %+v", PushReasonDiverged, result)
	}
}

func TestPushCommits_NoRemoteDefault(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "feature")

	runGit(t, cloneDir, "checkout", "-b", "feature")
	sha := commitFile(t, cloneDir, "f.txt", "f", "feature commit")

	// Delete main on the remote; PushCommits' fetch --prune must notice.
	runGit(t, remoteDir, "update-ref", "-d", "refs/heads/main")

	result, err := m.PushCommits(context.Background(), workspaceID, sha, "default", false, false)
	if err != nil {
		t.Fatalf("PushCommits() error: %v", err)
	}
	if result.Success || result.Reason != PushReasonNoRemoteDefault {
		t.Errorf("want reason %q, got: %+v", PushReasonNoRemoteDefault, result)
	}
}

func TestPushCommits_SaplingUnsupported(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	st.AddWorkspace(state.Workspace{
		ID: workspaceID, Repo: remoteDir, Branch: "main", Path: cloneDir, VCS: "sapling",
	})
	sha := strings.Repeat("a", 40)
	result, err := m.PushCommits(context.Background(), workspaceID, sha, "default", false, false)
	if err != nil {
		t.Fatalf("PushCommits() error: %v", err)
	}
	if result.Success || result.Reason != PushReasonUnsupported {
		t.Errorf("want reason %q, got: %+v", PushReasonUnsupported, result)
	}
}

func TestPushCommits_WorkspaceLocked(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "main")
	sha := runGitOut(t, cloneDir, "rev-parse", "HEAD")

	if !m.LockWorkspace(workspaceID) {
		t.Fatal("could not take workspace lock for test")
	}
	defer m.UnlockWorkspace(workspaceID)

	_, err := m.PushCommits(context.Background(), workspaceID, sha, "default", false, false)
	if !errors.Is(err, ErrWorkspaceLocked) {
		t.Errorf("PushCommits() error = %v, want ErrWorkspaceLocked", err)
	}
}

// installPushCounter adds a post-receive hook to the bare remote that appends
// one line per push, and returns a func that reads the count.
func installPushCounter(t *testing.T, remoteDir string) func() int {
	t.Helper()
	logPath := filepath.Join(remoteDir, "push-count.log")
	hook := "#!/bin/sh\necho push >> \"" + logPath + "\"\n"
	hookPath := filepath.Join(remoteDir, "hooks", "post-receive")
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		t.Fatalf("failed to create hooks dir: %v", err)
	}
	if err := os.WriteFile(hookPath, []byte(hook), 0o755); err != nil {
		t.Fatalf("failed to install post-receive hook: %v", err)
	}
	return func() int {
		data, err := os.ReadFile(logPath)
		if err != nil {
			return 0
		}
		return len(strings.Fields(string(data)))
	}
}

// installPushRejector adds a pre-receive hook that rejects a specific commit sha.
func installPushRejector(t *testing.T, remoteDir, badSha string) {
	t.Helper()
	badSha = strings.TrimSpace(badSha)
	hook := "#!/bin/sh\nwhile read old new ref; do\n  if [ \"$new\" = \"" + badSha + "\" ]; then\n    echo \"rejected by test hook\" >&2\n    exit 1\n  fi\ndone\nexit 0\n"
	hookPath := filepath.Join(remoteDir, "hooks", "pre-receive")
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		t.Fatalf("failed to create hooks dir: %v", err)
	}
	if err := os.WriteFile(hookPath, []byte(hook), 0o755); err != nil {
		t.Fatalf("failed to install pre-receive hook: %v", err)
	}
}

func TestPushCommits_BulkPartialToDefault(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "feature")
	counter := installPushCounter(t, remoteDir)

	runGit(t, cloneDir, "checkout", "-b", "feature")
	sha1 := commitFile(t, cloneDir, "a.txt", "a", "commit a")
	sha2 := commitFile(t, cloneDir, "b.txt", "b", "commit b")
	_ = commitFile(t, cloneDir, "c.txt", "c", "commit c")

	// Push only up to the second of three commits, in one push.
	result, err := m.PushCommits(context.Background(), workspaceID, sha2, "default", false, false)
	if err != nil {
		t.Fatalf("PushCommits() error: %v", err)
	}
	if !result.Success || result.PushesSucceeded != 1 || result.TotalCommits != 2 {
		t.Errorf("want success with 1 push / 2 commits, got: %+v", result)
	}
	if counter() != 1 {
		t.Errorf("remote saw %d pushes, want 1", counter())
	}
	if got := strings.TrimSpace(runGitOut(t, remoteDir, "rev-parse", "main")); got != sha2 {
		t.Errorf("origin/main = %s, want %s", got, sha2)
	}
	_ = sha1

	// Partial push must NOT retrack the branch onto origin/main.
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "feature@{upstream}")
	cmd.Dir = cloneDir
	if out, err := cmd.CombinedOutput(); err == nil && strings.TrimSpace(string(out)) == "origin/main" {
		t.Errorf("partial push must not set upstream to origin/main")
	}
}

func TestPushCommits_PerCommitToDefault(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "feature")
	counter := installPushCounter(t, remoteDir)

	runGit(t, cloneDir, "checkout", "-b", "feature")
	commitFile(t, cloneDir, "a.txt", "a", "commit a")
	commitFile(t, cloneDir, "b.txt", "b", "commit b")
	head := commitFile(t, cloneDir, "c.txt", "c", "commit c")

	result, err := m.PushCommits(context.Background(), workspaceID, head, "default", true, false)
	if err != nil {
		t.Fatalf("PushCommits() error: %v", err)
	}
	if !result.Success || result.PushesSucceeded != 3 || result.TotalCommits != 3 {
		t.Errorf("want success with 3 pushes / 3 commits, got: %+v", result)
	}
	if counter() != 3 {
		t.Errorf("remote saw %d pushes, want 3", counter())
	}
	if got := strings.TrimSpace(runGitOut(t, remoteDir, "rev-parse", "main")); got != head {
		t.Errorf("origin/main = %s, want %s", got, head)
	}
	// Full push to default from a feature branch DOES retrack onto origin/main.
	if got := strings.TrimSpace(runGitOut(t, cloneDir, "rev-parse", "--abbrev-ref", "feature@{upstream}")); got != "origin/main" {
		t.Errorf("upstream = %q, want origin/main", got)
	}
}

func TestPushCommits_PerCommitMidLoopRejection(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "feature")

	runGit(t, cloneDir, "checkout", "-b", "feature")
	sha1 := commitFile(t, cloneDir, "a.txt", "a", "commit a")
	sha2 := commitFile(t, cloneDir, "b.txt", "b", "commit b")
	head := commitFile(t, cloneDir, "c.txt", "c", "commit c")
	installPushRejector(t, remoteDir, sha2)

	result, err := m.PushCommits(context.Background(), workspaceID, head, "default", true, false)
	if err != nil {
		t.Fatalf("PushCommits() error: %v", err)
	}
	if result.Success {
		t.Errorf("want failure, got: %+v", result)
	}
	if result.Reason != PushReasonPushRejected || result.FailedHash != sha2 || result.PushesSucceeded != 1 {
		t.Errorf("want push_rejected at %s after 1 push, got: %+v", sha2, result)
	}
	if result.Message == "" || !strings.Contains(result.Message, "rejected") {
		t.Errorf("message should carry the git output, got: %q", result.Message)
	}
	// Remote sits at the last successful push — a consistent state.
	if got := strings.TrimSpace(runGitOut(t, remoteDir, "rev-parse", "main")); got != sha1 {
		t.Errorf("origin/main = %s, want %s (last successful push)", got, sha1)
	}
}

func TestPushCommits_BranchCreateAndPerCommit(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "feature")
	counter := installPushCounter(t, remoteDir)

	runGit(t, cloneDir, "checkout", "-b", "feature")
	commitFile(t, cloneDir, "a.txt", "a", "commit a")
	head := commitFile(t, cloneDir, "b.txt", "b", "commit b")

	// origin/feature doesn't exist: base falls back to the fork point with origin/main.
	result, err := m.PushCommits(context.Background(), workspaceID, head, "branch", true, false)
	if err != nil {
		t.Fatalf("PushCommits() error: %v", err)
	}
	if !result.Success || result.PushesSucceeded != 2 || result.TargetBranch != "feature" {
		t.Errorf("want success with 2 pushes to feature, got: %+v", result)
	}
	if counter() != 2 {
		t.Errorf("remote saw %d pushes, want 2", counter())
	}
	if got := strings.TrimSpace(runGitOut(t, remoteDir, "rev-parse", "feature")); got != head {
		t.Errorf("origin/feature = %s, want %s", got, head)
	}
}

func TestPushCommits_BranchPartialToExistingRemote(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "feature")

	runGit(t, cloneDir, "checkout", "-b", "feature")
	commitFile(t, cloneDir, "a.txt", "a", "commit a")
	runGit(t, cloneDir, "push", "origin", "feature")
	sha2 := commitFile(t, cloneDir, "b.txt", "b", "commit b")
	_ = commitFile(t, cloneDir, "c.txt", "c", "commit c")

	// Push only sha2; base is origin/feature so exactly one commit goes out.
	result, err := m.PushCommits(context.Background(), workspaceID, sha2, "branch", true, false)
	if err != nil {
		t.Fatalf("PushCommits() error: %v", err)
	}
	if !result.Success || result.PushesSucceeded != 1 || result.TotalCommits != 1 {
		t.Errorf("want success with 1 push / 1 commit, got: %+v", result)
	}
	if got := strings.TrimSpace(runGitOut(t, remoteDir, "rev-parse", "feature")); got != sha2 {
		t.Errorf("origin/feature = %s, want %s", got, sha2)
	}
}

func TestPushCommits_BranchDivergedNeedsConfirmThenForce(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "feature")

	// Push a feature branch, advance main, rebase → diverged from origin/feature.
	runGit(t, cloneDir, "checkout", "-b", "feature")
	commitFile(t, cloneDir, "a.txt", "a", "commit a")
	runGit(t, cloneDir, "push", "origin", "feature")
	runGit(t, cloneDir, "checkout", "main")
	commitFile(t, cloneDir, "main.txt", "m", "main update")
	runGit(t, cloneDir, "push", "origin", "main")
	runGit(t, cloneDir, "checkout", "feature")
	runGit(t, cloneDir, "fetch", "origin")
	runGit(t, cloneDir, "rebase", "origin/main")
	head := strings.TrimSpace(runGitOut(t, cloneDir, "rev-parse", "HEAD"))

	// confirm=false → needs_confirm with the commits that would be overwritten.
	result, err := m.PushCommits(context.Background(), workspaceID, head, "branch", false, false)
	if err != nil {
		t.Fatalf("PushCommits() error: %v", err)
	}
	if result.Success || !result.NeedsConfirm || len(result.DivergedCommits) == 0 {
		t.Errorf("want needs_confirm with diverged commits, got: %+v", result)
	}

	// confirm=true → force push succeeds.
	result, err = m.PushCommits(context.Background(), workspaceID, head, "branch", false, true)
	if err != nil {
		t.Fatalf("PushCommits() error: %v", err)
	}
	if !result.Success {
		t.Errorf("want success with confirm=true, got: %+v", result)
	}
	if got := strings.TrimSpace(runGitOut(t, remoteDir, "rev-parse", "feature")); got != head {
		t.Errorf("origin/feature = %s, want %s", got, head)
	}
}

func TestPushCommits_BranchBehindRejected(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "feature")

	runGit(t, cloneDir, "checkout", "-b", "feature")
	sha := commitFile(t, cloneDir, "a.txt", "a", "commit a")
	runGit(t, cloneDir, "push", "origin", "feature")

	// Someone else advances origin/feature.
	otherDir := filepath.Join(t.TempDir(), "other")
	runGit(t, filepath.Dir(otherDir), "clone", remoteDir, "other")
	runGit(t, otherDir, "config", "user.email", "other@test.com")
	runGit(t, otherDir, "config", "user.name", "Other")
	runGit(t, otherDir, "checkout", "feature")
	commitFile(t, otherDir, "other.txt", "o", "other commit")
	runGit(t, otherDir, "push", "origin", "feature")

	result, err := m.PushCommits(context.Background(), workspaceID, sha, "branch", false, false)
	if err != nil {
		t.Fatalf("PushCommits() error: %v", err)
	}
	if result.Success || result.Reason != PushReasonBehind {
		t.Errorf("want reason %q, got: %+v", PushReasonBehind, result)
	}
}

func TestPushCommits_RemoteBranchDeletedUpstreamRecreates(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "feature")

	runGit(t, cloneDir, "checkout", "-b", "feature")
	head := commitFile(t, cloneDir, "a.txt", "a", "commit a")
	runGit(t, cloneDir, "push", "origin", "feature")

	// Delete the branch on the remote. The local origin/feature tracking ref is
	// now stale — PushCommits' fetch --prune must clear it and cleanly recreate.
	runGit(t, remoteDir, "update-ref", "-d", "refs/heads/feature")

	result, err := m.PushCommits(context.Background(), workspaceID, head, "branch", false, false)
	if err != nil {
		t.Fatalf("PushCommits() error: %v", err)
	}
	if !result.Success {
		t.Errorf("want clean recreate after remote deletion, got: %+v", result)
	}
	if got := strings.TrimSpace(runGitOut(t, remoteDir, "rev-parse", "feature")); got != head {
		t.Errorf("origin/feature = %s, want %s", got, head)
	}
}

func TestPushCommits_NoBaseRejectsPerCommit(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "feature")

	runGit(t, cloneDir, "checkout", "-b", "feature")
	head := commitFile(t, cloneDir, "a.txt", "a", "commit a")
	// No origin/feature AND no origin/main → no base for a per-commit range.
	runGit(t, remoteDir, "update-ref", "-d", "refs/heads/main")

	result, err := m.PushCommits(context.Background(), workspaceID, head, "branch", true, false)
	if err != nil {
		t.Fatalf("PushCommits() error: %v", err)
	}
	if result.Success || result.Reason != PushReasonNoBase {
		t.Errorf("want reason %q, got: %+v", PushReasonNoBase, result)
	}
}

// Spec matrix row 5: a workspace not on its recorded branch (detached HEAD or
// wrong branch) must be rejected before anything is pushed. Note that
// `git rev-parse --abbrev-ref HEAD` reports the literal string "HEAD" on a
// detached checkout without erroring, so the check compares branch names.
func TestPushCommits_DetachedHeadRejected(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "main")

	sha := commitFile(t, cloneDir, "a.txt", "a", "commit a")
	runGit(t, cloneDir, "checkout", "--detach")

	_, err := m.PushCommits(context.Background(), workspaceID, sha, "default", false, false)
	if err == nil || !strings.Contains(err.Error(), "not on branch") {
		t.Errorf("PushCommits() on detached HEAD: error = %v, want 'not on branch'", err)
	}
	// Nothing must have been pushed.
	if got := strings.TrimSpace(runGitOut(t, remoteDir, "rev-parse", "main")); got == sha {
		t.Errorf("origin/main advanced to %s despite detached-HEAD rejection", sha)
	}
}

func TestPushCommits_WrongBranchRejected(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	// Workspace state claims "feature", but the clone is still on main.
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "feature")

	sha := commitFile(t, cloneDir, "a.txt", "a", "commit a")

	_, err := m.PushCommits(context.Background(), workspaceID, sha, "branch", false, false)
	if err == nil || !strings.Contains(err.Error(), "not on branch") {
		t.Errorf("PushCommits() on wrong branch: error = %v, want 'not on branch'", err)
	}
}

// Spec matrix row 14: the selected commit is already on origin/<branch>.
func TestPushCommits_BranchNothingToPush(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "feature")

	runGit(t, cloneDir, "checkout", "-b", "feature")
	pushed := commitFile(t, cloneDir, "a.txt", "a", "commit a")
	runGit(t, cloneDir, "push", "origin", "feature")
	commitFile(t, cloneDir, "b.txt", "b", "commit b") // local-only, above the selected commit

	result, err := m.PushCommits(context.Background(), workspaceID, pushed, "branch", false, false)
	if err != nil {
		t.Fatalf("PushCommits() error: %v", err)
	}
	if result.Success || result.Reason != PushReasonNothingToPush {
		t.Errorf("want reason %q, got: %+v", PushReasonNothingToPush, result)
	}
}

// Spec matrix rows 1/20: a dead context fails cleanly (the fetch is the first
// git call to notice) and nothing is pushed.
func TestPushCommits_CanceledContext(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "main")

	before := strings.TrimSpace(runGitOut(t, remoteDir, "rev-parse", "main"))
	sha := commitFile(t, cloneDir, "a.txt", "a", "commit a")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := m.PushCommits(ctx, workspaceID, sha, "default", false, false); err == nil {
		t.Error("PushCommits() with canceled context should error")
	}
	if got := strings.TrimSpace(runGitOut(t, remoteDir, "rev-parse", "main")); got != before {
		t.Errorf("origin/main moved from %s to %s despite canceled context", before, got)
	}
}

// On the default branch the branch target IS the default branch but via
// force-with-lease — reject it.
func TestPushCommits_BranchTargetOnDefaultBranchRejected(t *testing.T) {
	t.Parallel()
	remoteDir, cloneDir, m, st, workspaceID := setupPushTest(t)
	m.setDefaultBranch(remoteDir, "main")
	addPushWorkspace(t, st, workspaceID, remoteDir, cloneDir, "main")

	before := strings.TrimSpace(runGitOut(t, remoteDir, "rev-parse", "main"))
	sha := commitFile(t, cloneDir, "a.txt", "a", "commit a")

	result, err := m.PushCommits(context.Background(), workspaceID, sha, "branch", false, false)
	if err != nil {
		t.Fatalf("PushCommits() error: %v", err)
	}
	if result.Success || result.Reason != PushReasonUnsupported {
		t.Errorf("want reason %q, got: %+v", PushReasonUnsupported, result)
	}
	if !strings.Contains(result.Message, "default") {
		t.Errorf("message should point at the default target, got: %q", result.Message)
	}
	if got := strings.TrimSpace(runGitOut(t, remoteDir, "rev-parse", "main")); got != before {
		t.Errorf("origin/main moved from %s to %s despite rejection", before, got)
	}
}
