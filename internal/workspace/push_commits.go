package workspace

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// Failure reason codes for PushCommits (PushCommitsResult.Reason).
// The behavior contract is documented in docs/api.md (push-commits endpoint).
const (
	PushReasonDirty           = "dirty"
	PushReasonNothingToPush   = "nothing_to_push"
	PushReasonBehind          = "behind"
	PushReasonDiverged        = "diverged"
	PushReasonNoRemoteDefault = "no_remote_default"
	PushReasonNoBase          = "no_base"
	PushReasonPushRejected    = "push_rejected"
	PushReasonUnsupported     = "unsupported"
	PushReasonNoOrigin        = "no_origin"
)

// fullCommitShaRe matches a full sha1 (40) or sha256 (64) hex object name.
// Checked before the value reaches any git invocation so flag-shaped or
// abbreviated input can never be misinterpreted.
var fullCommitShaRe = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// PushCommits pushes the current branch's commits up to (and including) hash
// to origin, either as one push or one push per commit (oldest first) so CI
// builds each commit separately. target "default" pushes to
// origin/<defaultBranch> fast-forward only; target "branch" pushes to
// origin/<workspace branch> with --force-with-lease, where confirm gates the
// diverged case.
func (m *Manager) PushCommits(ctx context.Context, workspaceID, hash, target string, perCommit, confirm bool) (*contracts.PushCommitsResult, error) {
	hash = strings.TrimSpace(hash)
	if !fullCommitShaRe.MatchString(hash) {
		return nil, ErrMalformedHash
	}
	if target != "default" && target != "branch" {
		return nil, ErrInvalidPushTarget
	}
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}
	if w.VCS == "sapling" {
		return &contracts.PushCommitsResult{
			Reason:  PushReasonUnsupported,
			Message: "per-commit push is not supported for sapling workspaces",
		}, nil
	}
	if !m.LockWorkspace(workspaceID) {
		return nil, ErrWorkspaceLocked
	}
	defer m.UnlockWorkspace(workspaceID)

	dir := w.Path

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// A workspace without an origin remote (e.g. a repo created via the
	// "new repository" spawn path) has nowhere to push. Fail structurally
	// instead of letting `git fetch origin` blow up as a 500.
	if !m.gitHasOriginRemote(ctx, dir) {
		return &contracts.PushCommitsResult{
			Reason:  PushReasonNoOrigin,
			Message: "workspace has no origin remote",
		}, nil
	}

	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	isAncestor := func(a, b string) bool {
		_, err := run("merge-base", "--is-ancestor", a, b)
		return err == nil
	}

	// The checkout must actually be on the workspace's recorded branch (spec
	// matrix #5). `git rev-parse --abbrev-ref HEAD` reports the literal string
	// "HEAD" on a detached checkout without erroring, so a name comparison
	// catches both detached HEAD and a wrong-branch checkout.
	currentBranch, err := m.gitCurrentBranch(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve current branch: %w", err)
	}
	if currentBranch != w.Branch {
		return nil, fmt.Errorf("workspace is not on branch %q (checkout is at %q)", w.Branch, currentBranch)
	}

	// Fetch with prune so a branch deleted on the remote doesn't leave a stale
	// tracking ref that would poison ancestor checks and the force-with-lease lease.
	m.logger.Info("push-commits: fetching origin", "workspace", workspaceID)
	if out, err := run("fetch", "origin", "--prune"); err != nil {
		return nil, fmt.Errorf("git fetch origin --prune failed: %w: %s", err, out)
	}

	// The hash must name a commit that is on the current branch.
	if _, err := run("cat-file", "-e", hash+"^{commit}"); err != nil {
		return nil, ErrStaleHash
	}
	if !isAncestor(hash, "HEAD") {
		return nil, ErrStaleHash
	}

	defaultBranch, err := m.GetDefaultBranch(ctx, w.Repo)
	if err != nil {
		defaultBranch = "main" // same fallback as GetGitGraph
	}
	targetBranch := defaultBranch
	if target == "branch" {
		targetBranch = w.Branch
	}
	targetRef := "origin/" + targetBranch
	res := &contracts.PushCommitsResult{TargetBranch: targetBranch, PerCommit: perCommit}

	// On the default branch the branch target IS the default branch, but via
	// force-with-lease — reject and point at the fast-forward-only default
	// target instead.
	if target == "branch" && w.Branch == defaultBranch {
		res.Reason = PushReasonUnsupported
		res.Message = `workspace is on the default branch - push with target "default" instead`
		return res, nil
	}

	// Reject any local dirt (same rule as LinearSyncToDefault / PushToBranch).
	dirty, _, _, linesAdded, linesRemoved, filesChanged, _, _, remoteBranchIsFork, _, _, _, _, _ := m.gitStatus(ctx, workspaceID, RefreshTriggerExplicit, dir, w.Repo)
	if dirty || linesAdded != 0 || linesRemoved != 0 || filesChanged != 0 {
		res.Reason = PushReasonDirty
		res.Message = "workspace has local changes - commit or discard them before pushing"
		return res, nil
	}
	// The UI hides the branch target for fork remotes; reject if called anyway
	// (spec matrix #19) — origin/<branch> would silently create a new branch on
	// origin instead of updating the fork.
	if target == "branch" && remoteBranchIsFork {
		res.Reason = PushReasonUnsupported
		res.Message = "remote branch lives on a fork - pushing to forks is not supported"
		return res, nil
	}

	_, targetRefErr := run("rev-parse", "--verify", targetRef)
	targetExists := targetRefErr == nil

	force := false
	switch target {
	case "default":
		if !targetExists {
			res.Reason = PushReasonNoRemoteDefault
			res.Message = fmt.Sprintf("origin/%s does not exist on the remote", targetBranch)
			return res, nil
		}
		// Relative to the hash exactly three states are possible (spec matrix #6-#8).
		if isAncestor(hash, targetRef) {
			res.Reason = PushReasonNothingToPush
			res.Message = fmt.Sprintf("commit is already on origin/%s", targetBranch)
			return res, nil
		}
		if !isAncestor(targetRef, hash) {
			res.Reason = PushReasonDiverged
			res.Message = fmt.Sprintf("branch has diverged from origin/%s - pull from %s first", targetBranch, targetBranch)
			return res, nil
		}
	case "branch":
		force = true
		if targetExists {
			if isAncestor("HEAD", targetRef) {
				res.Reason = PushReasonBehind
				res.Message = "local branch is behind origin - pull or merge first"
				return res, nil
			}
			if isAncestor(hash, targetRef) {
				res.Reason = PushReasonNothingToPush
				res.Message = fmt.Sprintf("commit is already on origin/%s", targetBranch)
				return res, nil
			}
			if !isAncestor(targetRef, hash) && !confirm {
				// Diverged (typical after a rebase): confirm gates the force push.
				out, logErr := run("log", "--oneline", hash+".."+targetRef)
				if logErr != nil {
					m.logger.Warn("push-commits: failed to get diverged commits", "err", logErr)
				}
				for _, line := range strings.Split(out, "\n") {
					if line != "" {
						res.DivergedCommits = append(res.DivergedCommits, line)
					}
				}
				res.NeedsConfirm = true
				return res, nil
			}
		}
	}

	// Push base: the newest commit the target ref already has. For a missing
	// remote branch, fall back to the fork point with origin/<default>.
	base := ""
	if targetExists {
		base = targetRef
	} else if _, err := run("rev-parse", "--verify", "origin/"+defaultBranch); err == nil {
		if mb, err := run("merge-base", hash, "origin/"+defaultBranch); err == nil {
			base = mb
		}
	}
	if base == "" && perCommit {
		res.Reason = PushReasonNoBase
		res.Message = "cannot determine a base for per-commit pushes - push all at once instead"
		return res, nil
	}

	// Build the push list and commit count.
	var toPush []string
	if perCommit {
		out, err := run("rev-list", "--reverse", base+".."+hash)
		if err != nil {
			return nil, fmt.Errorf("git rev-list failed: %w: %s", err, out)
		}
		for _, line := range strings.Split(out, "\n") {
			if line != "" {
				toPush = append(toPush, line)
			}
		}
		res.TotalCommits = len(toPush)
	} else {
		toPush = []string{hash}
		countRange := hash
		if base != "" {
			countRange = base + ".." + hash
		}
		out, err := run("rev-list", "--count", countRange)
		if err != nil {
			return nil, fmt.Errorf("git rev-list --count failed: %w: %s", err, out)
		}
		fmt.Sscanf(out, "%d", &res.TotalCommits)
	}
	if len(toPush) == 0 {
		res.Reason = PushReasonNothingToPush
		res.Message = fmt.Sprintf("no commits to push to origin/%s", targetBranch)
		return res, nil
	}

	// Push loop. Each completed push is a consistent fast-forward state on the
	// remote, so stopping mid-loop is always safe; a retry recomputes the range
	// from the remote's new position and resumes naturally.
	for _, sha := range toPush {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		args := []string{"push"}
		if force {
			args = append(args, "--force-with-lease")
		}
		// Fully-qualified refs/heads/ destination: never misresolved to a tag.
		args = append(args, "origin", sha+":refs/heads/"+targetBranch)
		m.logger.Info("push-commits: pushing", "workspace", workspaceID, "sha", sha, "target", targetBranch)
		if out, err := run(args...); err != nil {
			res.Reason = PushReasonPushRejected
			res.FailedHash = sha
			res.Message = out
			return res, nil
		}
		res.PushesSucceeded++
	}
	res.Success = true

	// Full push to default from a feature branch keeps the retracking behavior
	// of LinearSyncToDefault. Partial pushes never touch tracking.
	// currentBranch was verified against w.Branch before the fetch.
	headSha, headErr := run("rev-parse", "HEAD")
	if target == "default" && headErr == nil && hash == headSha && currentBranch != defaultBranch {
		if out, err := run("branch", "--set-upstream-to=origin/"+defaultBranch); err != nil {
			m.logger.Warn("push-commits: set-upstream failed", "output", out)
		}
		if out, err := run("merge", "--ff-only", "origin/"+defaultBranch); err != nil {
			m.logger.Warn("push-commits: git merge --ff-only failed", "output", out)
		}
	}

	if m.telemetry != nil {
		m.telemetry.Track("push_commits", map[string]any{
			"workspace_id":     workspaceID,
			"target":           target,
			"per_commit":       perCommit,
			"pushes_succeeded": res.PushesSucceeded,
		})
	}
	return res, nil
}
