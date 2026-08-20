package workspace

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// DeleteRemoteBranch deletes the workspace's branch from origin.
//
// A successful push to the default branch proves only that local HEAD landed
// there; it does not prove that every commit on origin/<branch> landed. Commits
// that live only on the remote are unrecoverable once the ref is gone, so this
// method proves containment itself and makes the proof binding with a lease:
// the delete lands only if origin/<branch> still equals the SHA whose
// containment was proved.
func (m *Manager) DeleteRemoteBranch(ctx context.Context, workspaceID string) error {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}
	if w.RemoteBranchIsFork || w.RemoteHostID != "" || (w.VCS != "" && w.VCS != "git") || w.Branch == "" {
		return ErrRemoteBranchNotDeletable
	}

	defaultBranch, err := m.GetDefaultBranch(ctx, w.Repo)
	if err != nil {
		defaultBranch = "main" // same fallback as PushCommits
	}
	if w.Branch == defaultBranch {
		return ErrRemoteBranchNotDeletable
	}

	if !m.LockWorkspace(workspaceID) {
		return ErrWorkspaceLocked
	}
	defer m.UnlockWorkspace(workspaceID)

	dir := w.Path
	if !m.gitHasOriginRemote(ctx, dir) {
		return ErrRemoteBranchNotDeletable
	}

	run := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}

	// Fetch with prune for the same reason PushCommits does: a branch already
	// deleted on the remote otherwise leaves a stale tracking ref that poisons
	// both the ancestor check and the lease.
	if out, err := run("fetch", "origin", "--prune"); err != nil {
		return fmt.Errorf("git fetch origin --prune failed: %w: %s", err, out)
	}

	remoteRef := "refs/remotes/origin/" + w.Branch
	sha, err := run("rev-parse", "--verify", "--quiet", remoteRef)
	if err != nil || sha == "" {
		// Already gone. Deletion is idempotent so a stale RemoteBranchExists
		// never blocks cleanup.
		m.logger.Info("delete-remote-branch: already absent", "workspace", workspaceID, "branch", w.Branch)
		return nil
	}

	if _, err := run("merge-base", "--is-ancestor", sha, "refs/remotes/origin/"+defaultBranch); err != nil {
		return ErrRemoteBranchNotMerged
	}

	m.logger.Info("delete-remote-branch: deleting", "workspace", workspaceID, "branch", w.Branch, "sha", sha)
	lease := fmt.Sprintf("--force-with-lease=refs/heads/%s:%s", w.Branch, sha)
	if out, err := run("push", lease, "origin", "--delete", "refs/heads/"+w.Branch); err != nil {
		return fmt.Errorf("failed to delete origin/%s: %w: %s", w.Branch, err, out)
	}
	return nil
}
