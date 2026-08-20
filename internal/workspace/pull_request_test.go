package workspace

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// newPRTestManager builds a manager backed by a real bare origin that has
// branch "feature/audio" pushed to it. Returns the manager and the origin URL.
func newPRTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	base := t.TempDir()

	origin := filepath.Join(base, "origin.git")
	gitCmd(t, base, "init", "--bare", origin)
	seed := filepath.Join(base, "seed")
	gitCmd(t, base, "clone", origin, seed)
	gitCmd(t, seed, "commit", "--allow-empty", "-m", "base")
	gitCmd(t, seed, "branch", "-M", "main")
	gitCmd(t, seed, "push", "-u", "origin", "main")
	gitCmd(t, seed, "checkout", "-b", "feature/audio")
	gitCmd(t, seed, "commit", "--allow-empty", "-m", "pr work")
	gitCmd(t, seed, "push", "-u", "origin", "feature/audio")

	statePath := filepath.Join(base, "state.json")
	cfg := &config.Config{}
	cfg.WorkspacePath = filepath.Join(base, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(base, "repos")
	cfg.Repos = []config.Repo{{Name: "repo", URL: origin, BarePath: "repo.git"}}

	st := state.New(statePath, nil)
	return New(cfg, st, statePath, testLogger()), origin
}

// TestCheckoutPR_UsesOriginBranch is the regression test for PR workspaces
// landing on a synthetic "pr/N" branch with no remote counterpart. A same-repo
// PR must sit on its real head branch so remote tracking (and therefore CI
// status, PR chips, and drift detection) works.
func TestCheckoutPR_UsesOriginBranch(t *testing.T) {
	m, origin := newPRTestManager(t)

	pr := contracts.PullRequest{
		Number:       3,
		RepoURL:      origin,
		SourceBranch: "feature/audio",
		TargetBranch: "main",
	}

	w, err := m.CheckoutPR(context.Background(), pr)
	if err != nil {
		t.Fatalf("CheckoutPR() error = %v", err)
	}

	if w.Branch != "feature/audio" {
		t.Errorf("workspace branch = %q, want %q (synthetic pr/N strands the workspace)", w.Branch, "feature/audio")
	}
	if w.Branch == "pr/3" {
		t.Error("workspace is on the synthetic PR branch — remote tracking is lost")
	}

	// The whole point: the branch must have an origin counterpart.
	exists, err := m.gitRemoteBranchExists(context.Background(), w.Path, w.Branch)
	if err != nil {
		t.Fatalf("gitRemoteBranchExists() error = %v", err)
	}
	if !exists {
		t.Errorf("refs/remotes/origin/%s missing — RemoteBranchExists would be false", w.Branch)
	}
}

// TestCheckoutPR_FallsBackToPRRefOffOrigin verifies that a PR whose head is not
// on origin (the fork case) still routes through refs/pull/N/head rather than
// silently checking out a branch that does not exist.
func TestCheckoutPR_FallsBackToPRRefOffOrigin(t *testing.T) {
	m, origin := newPRTestManager(t)

	pr := contracts.PullRequest{
		Number:       9,
		RepoURL:      origin,
		SourceBranch: "contributor-only-branch",
		IsFork:       true,
		ForkOwner:    "contributor",
	}

	// A plain bare repo has no refs/pull/*, so the fallback fetch must fail.
	// That failure is the proof the fork path was taken instead of checking
	// out a nonexistent origin branch.
	_, err := m.CheckoutPR(context.Background(), pr)
	if err == nil {
		t.Fatal("CheckoutPR() should fail when the head branch is absent from origin and no PR ref exists")
	}
	if !strings.Contains(err.Error(), "PR ref") {
		t.Errorf("error = %q, want it to come from the PR-ref fallback", err.Error())
	}
}
