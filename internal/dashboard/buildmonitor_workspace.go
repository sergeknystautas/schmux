//go:build !nobuildmonitor && !nogithub

package dashboard

import (
	"context"
	"errors"
	"time"

	"github.com/sergeknystautas/schmux/internal/buildmonitor"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspacestatus"
)

// buildMonitorNow is a test seam for TTL/backoff decisions.
var buildMonitorNow = time.Now

// workspaceStatusTTL bounds how long a terminal (success/failure) CI result
// is reused for an unchanged head SHA; reruns reuse the SHA.
const workspaceStatusTTL = 5 * time.Minute

// workspaceStatusMaxBackoff caps the rate-limit backoff.
const workspaceStatusMaxBackoff = 30 * time.Minute

// checkUnitWorkspaces refreshes CI/PR status for the live local workspaces of
// one monitored repo, using the unit's already-resolved identity token. Marks
// refreshed workspace IDs in live and reports whether any status changed.
func (s *Server) checkUnitWorkspaces(ctx context.Context, repoURL, defaultBranch, token string, unit *buildmonitor.UnitState, live map[string]bool) bool {
	baseRepo, err := github.ParseRepoURL(repoURL)
	if err != nil {
		return false
	}
	changed := false
	for _, w := range s.state.GetWorkspaces() {
		if w.Repo != repoURL || !w.RemoteBranchExists || w.RemoteHostID != "" || w.Status == state.WorkspaceStatusRecyclable {
			continue
		}
		head, err := s.workspace.GetRemoteBranchHead(ctx, w.ID)
		if err != nil || head.SHA == "" || !github.IsGitHubURL(head.RemoteURL) {
			continue
		}
		ciRepo := baseRepo
		if w.RemoteBranchIsFork {
			if ciRepo, err = github.ParseRepoURL(head.RemoteURL); err != nil {
				continue
			}
		}
		live[w.ID] = true
		if s.refreshWorkspaceStatus(ctx, token, w, baseRepo, ciRepo, defaultBranch, head.SHA, unit) {
			changed = true
		}
	}
	return changed
}

// refreshWorkspaceStatus updates one workspace's cache entry; reports change.
func (s *Server) refreshWorkspaceStatus(ctx context.Context, token string, w state.Workspace, baseRepo, ciRepo github.RepoInfo, defaultBranch, headSHA string, unit *buildmonitor.UnitState) bool {
	now := buildMonitorNow()
	prev, hadPrev := s.workspaceStatus.Lookup(w.ID)
	if hadPrev && (prev.Repo != ciRepo || prev.Branch != w.Branch) {
		// Entry identity is (repo, branch): rebind on change.
		prev, hadPrev = workspacestatus.Entry{}, false
	}
	if hadPrev && now.Before(prev.BackoffUntil) {
		return false
	}

	next := workspacestatus.Entry{Repo: ciRepo, Branch: w.Branch, HeadSHA: headSHA, FetchedAt: now}
	skipCI := hadPrev && prev.Terminal && prev.HeadSHA == headSHA && now.Sub(prev.FetchedAt) < workspaceStatusTTL
	switch {
	case skipCI:
		next.Status.CIStatus, next.Status.CIURL = prev.Status.CIStatus, prev.Status.CIURL
		next.Terminal = true
		next.FetchedAt = prev.FetchedAt
	case w.Branch == defaultBranch && !w.RemoteBranchIsFork:
		// The unit already fetched this branch's runs; reuse them.
		next.Status.CIStatus, next.Status.CIURL = workspacestatus.Aggregate(unitStateRuns(unit), headSHA)
		next.Terminal = next.Status.CIStatus == workspacestatus.CISuccess || next.Status.CIStatus == workspacestatus.CIFailure
	default:
		runs, err := github.ListRepoRuns(ctx, token, ciRepo, w.Branch)
		if err != nil {
			return s.handleWorkspaceStatusError(w.ID, ciRepo, w.Branch, err, prev, hadPrev)
		}
		next.Status.CIStatus, next.Status.CIURL = workspacestatus.Aggregate(runs, headSHA)
		next.Terminal = next.Status.CIStatus == workspacestatus.CISuccess || next.Status.CIStatus == workspacestatus.CIFailure
	}

	// PRs appear without new commits, so this lookup runs every pass.
	pr, err := github.FetchOpenPRForBranch(ctx, token, baseRepo, ciRepo.Owner+":"+w.Branch)
	if err != nil {
		return s.handleWorkspaceStatusError(w.ID, ciRepo, w.Branch, err, prev, hadPrev)
	}
	if pr != nil {
		next.Status.PRNumber, next.Status.PRURL = pr.Number, pr.HTMLURL
	}

	s.workspaceStatus.Store(w.ID, next)
	return next.Status != prev.Status
}

// unitStateRuns converts a unit's per-workflow snapshot back to run records
// so the default-branch aggregation reuses the unit's fetch.
func unitStateRuns(unit *buildmonitor.UnitState) []github.WorkflowRun {
	if unit == nil {
		return nil
	}
	runs := make([]github.WorkflowRun, 0, len(unit.Workflows))
	for _, wf := range unit.Workflows {
		if wf.RunID == 0 {
			continue
		}
		runs = append(runs, github.WorkflowRun{
			ID: wf.RunID, WorkflowID: wf.WorkflowID, RunNumber: wf.RunNumber,
			Status: wf.Status, Conclusion: wf.Conclusion, HeadSHA: wf.HeadSHA, HTMLURL: wf.HTMLURL,
		})
	}
	return runs
}

// handleWorkspaceStatusError: rate limits back off (identity preserved so the
// rebind check doesn't discard the entry); other errors keep stale data.
func (s *Server) handleWorkspaceStatusError(workspaceID string, ciRepo github.RepoInfo, branch string, err error, prev workspacestatus.Entry, hadPrev bool) bool {
	if github.IsUnauthorized(err) {
		// Identity token invalid; the unit check surfaces this on the build
		// monitor screen. Keep stale chips until the next successful pass.
		s.logger.Warn("workspace status: unauthorized", "workspace", workspaceID)
		return false
	}
	var rle *github.RateLimitError
	if errors.As(err, &rle) {
		e := prev
		if !hadPrev {
			e = workspacestatus.Entry{Repo: ciRepo, Branch: branch}
		}
		e.Backoff = e.Backoff * 2
		if e.Backoff == 0 {
			e.Backoff = 2 * s.config.GetBuildMonitorIntervalSeconds()
		}
		if e.Backoff > workspaceStatusMaxBackoff {
			e.Backoff = workspaceStatusMaxBackoff
		}
		e.BackoffUntil = buildMonitorNow().Add(e.Backoff)
		s.workspaceStatus.Store(workspaceID, e)
		s.logger.Debug("workspace status rate limited", "workspace", workspaceID, "backoff", e.Backoff)
		return false
	}
	s.logger.Debug("workspace status refresh failed", "workspace", workspaceID, "err", err)
	return false
}
