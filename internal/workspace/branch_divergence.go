package workspace

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// divergenceMaxCommits caps each direction's listing; totals report the full count.
const divergenceMaxCommits = 10

// divergenceLogFormat: hash, short hash, author, ISO committer date, subject — US-delimited.
const divergenceLogFormat = "%H%x1f%h%x1f%an%x1f%cI%x1f%s"

// GetBranchDivergence lists how the workspace branch differs from origin/<branch>
// in both directions. Fetches origin first so the result (and any force push it
// authorizes) reflects the remote now.
func (m *Manager) GetBranchDivergence(ctx context.Context, workspaceID string) (*contracts.BranchDivergenceResponse, error) {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}
	workspacePath := w.Path
	branch := w.Branch

	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin")
	fetchCmd.Dir = workspacePath
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git fetch origin failed: %w: %s", err, string(output))
	}

	localHead, err := gitOutput(ctx, workspacePath, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("git rev-parse HEAD failed: %w", err)
	}

	originRef := "origin/" + branch
	remoteHead, err := gitOutput(ctx, workspacePath, "rev-parse", "--verify", originRef)
	originExists := err == nil

	// Local range: vs origin/<branch>. With no remote branch nothing is on the
	// remote side, so every reachable commit is "ahead" (capped like any other
	// listing) — the caller falls through to a normal push in that case anyway.
	localRange := "HEAD"
	if originExists {
		localRange = originRef + "..HEAD"
	}

	localCommits, localTotal, err := divergenceList(ctx, workspacePath, localRange)
	if err != nil {
		return nil, err
	}
	remoteCommits := []contracts.DivergenceCommit{}
	remoteTotal := 0
	if originExists {
		remoteCommits, remoteTotal, err = divergenceList(ctx, workspacePath, "HEAD.."+originRef)
		if err != nil {
			return nil, err
		}
	}

	return &contracts.BranchDivergenceResponse{
		Branch:        branch,
		LocalHead:     localHead,
		RemoteHead:    remoteHead,
		LocalCommits:  localCommits,
		RemoteCommits: remoteCommits,
		LocalTotal:    localTotal,
		RemoteTotal:   remoteTotal,
	}, nil
}

// divergenceList returns up to divergenceMaxCommits commits in the range plus the full count.
func divergenceList(ctx context.Context, dir, rangeSpec string) ([]contracts.DivergenceCommit, int, error) {
	countOut, err := gitOutput(ctx, dir, "rev-list", "--count", rangeSpec)
	if err != nil {
		return nil, 0, fmt.Errorf("git rev-list --count %s failed: %w", rangeSpec, err)
	}
	total, err := strconv.Atoi(strings.TrimSpace(countOut))
	if err != nil {
		return nil, 0, fmt.Errorf("parsing rev-list count %q: %w", countOut, err)
	}

	logOut, err := gitOutput(ctx, dir, "log", "--format="+divergenceLogFormat,
		"-n", strconv.Itoa(divergenceMaxCommits), rangeSpec)
	if err != nil {
		return nil, 0, fmt.Errorf("git log %s failed: %w", rangeSpec, err)
	}

	// Non-nil so the API encodes an empty direction as [] rather than null —
	// the dashboard dereferences .length on these lists.
	commits := []contracts.DivergenceCommit{}
	for _, line := range strings.Split(strings.TrimSpace(logOut), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\x1f", 5)
		if len(fields) != 5 {
			continue
		}
		commits = append(commits, contracts.DivergenceCommit{
			Hash:      fields[0],
			ShortHash: fields[1],
			Author:    fields[2],
			Timestamp: fields[3],
			Subject:   fields[4],
		})
	}
	return commits, total, nil
}

// gitOutput runs a git command in dir and returns trimmed stdout.
func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
