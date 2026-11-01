package workspace

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// VCSArchiveState is the read-only snapshot of a workspace's work that a flight
// archive keeps after the workspace is gone. A collection failure is recorded
// in Error beside whatever identity was known, so the rest of the archive
// stays valid.
type VCSArchiveState struct {
	VCS                string   `json:"vcs,omitempty"`
	RepoURL            string   `json:"repo_url,omitempty"`
	Branch             string   `json:"branch,omitempty"`
	BaseBranch         string   `json:"base_branch,omitempty"`
	HeadSHA            string   `json:"head_sha,omitempty"`
	BaseSHA            string   `json:"base_sha,omitempty"`
	Dirty              bool     `json:"dirty"`
	Ahead              int      `json:"ahead"`
	Behind             int      `json:"behind"`
	FilesChanged       int      `json:"files_changed"`
	LinesAdded         int      `json:"lines_added"`
	LinesRemoved       int      `json:"lines_removed"`
	RemoteBranchExists bool     `json:"remote_branch_exists"`
	ChangedFiles       []string `json:"changed_files,omitempty"`
	Error              string   `json:"error,omitempty"`
}

// FlightEvidence reads a workspace's branch, head and base commits, change
// counts, and changed-file names through its VCS backend without modifying the
// workspace or its recorded state. Head and base commits are collected for git
// workspaces only; fields a backend cannot supply stay empty. On failure it
// returns the state gathered so far with Error set, together with the error.
func (m *Manager) FlightEvidence(ctx context.Context, workspaceID, repoURL, branch string) (VCSArchiveState, error) {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		out := VCSArchiveState{VCS: "git", RepoURL: repoURL, Branch: branch}
		if repoURL == "" || branch == "" {
			err := fmt.Errorf("workspace not found: %s", workspaceID)
			out.Error = err.Error()
			return out, err
		}
		// The workspace row can vanish while its remote branch still proves what
		// happened; query that durable identity before declaring the work lost.
		sha, err := m.GetRemoteHeadSHA(ctx, repoURL, branch)
		if err != nil || sha == "" {
			if err == nil {
				err = fmt.Errorf("workspace %s is gone and remote branch %s does not exist", workspaceID, branch)
			}
			out.Error = err.Error()
			return out, err
		}
		out.HeadSHA, out.RemoteBranchExists = sha, true
		return out, nil
	}
	out := VCSArchiveState{VCS: w.VCS, RepoURL: w.Repo, Branch: w.Branch}
	if out.VCS == "" {
		out.VCS = "git"
	}
	fail := func(err error) (VCSArchiveState, error) {
		out.Error = err.Error()
		return out, err
	}
	if w.RemoteHostID != "" {
		return fail(fmt.Errorf("workspace %s is on a remote host; its work cannot be inspected locally", workspaceID))
	}
	if info, err := os.Stat(w.Path); err != nil || !info.IsDir() {
		return fail(fmt.Errorf("workspace directory %s is not available: %v", w.Path, err))
	}

	// Hold the gate's read side so a sync that wants the workspace waits for
	// this read to finish, as UpdateGitStatus does.
	gate := m.getWorkspaceGate(workspaceID)
	gate.RLock()
	defer gate.RUnlock()

	backend := m.backendForWorkspace(workspaceID)
	status, err := backend.GetStatus(ctx, w.Path)
	if err != nil {
		return fail(fmt.Errorf("read status: %w", err))
	}
	out.Dirty = status.Dirty
	out.Ahead = status.AheadOfDefault
	out.Behind = status.BehindDefault
	out.FilesChanged = status.FilesChanged
	out.LinesAdded = status.LinesAdded
	out.LinesRemoved = status.LinesRemoved
	out.RemoteBranchExists = status.RemoteBranchExists
	if status.CurrentBranch != "" {
		out.Branch = status.CurrentBranch
	}

	files, err := backend.GetChangedFiles(ctx, w.Path)
	if err != nil {
		return fail(fmt.Errorf("list changed files: %w", err))
	}
	for _, f := range files {
		out.ChangedFiles = append(out.ChangedFiles, f.Path)
	}

	if gitBackend, ok := backend.(*GitBackend); ok {
		head, err := m.runGit(ctx, workspaceID, RefreshTriggerExplicit, w.Path, "rev-parse", "HEAD")
		if err != nil {
			return fail(fmt.Errorf("resolve head: %w", err))
		}
		out.HeadSHA = strings.TrimSpace(string(head))
		if base := gitBackend.detectDefaultBranch(ctx, w.Path); base != "" {
			out.BaseBranch = base
			if sha, err := m.runGit(ctx, workspaceID, RefreshTriggerExplicit, w.Path, "rev-parse", "origin/"+base); err == nil {
				out.BaseSHA = strings.TrimSpace(string(sha))
			}
		}
	}
	return out, nil
}
