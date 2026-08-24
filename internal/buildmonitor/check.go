//go:build !nobuildmonitor

package buildmonitor

import (
	"context"

	"github.com/sergeknystautas/schmux/internal/github"
)

// Actions is the subset of the GitHub Actions client that a check pass needs.
type Actions interface {
	ListWorkflows(ctx context.Context, token string, info github.RepoInfo) ([]github.Workflow, error)
	ListRepoRuns(ctx context.Context, token string, info github.RepoInfo, branch string) ([]github.WorkflowRun, error)
	ListRunJobs(ctx context.Context, token string, info github.RepoInfo, runID int64) ([]github.WorkflowJob, error)
}

func classify(err error) string {
	switch {
	case github.IsUnauthorized(err):
		return "unauthorized"
	case github.IsForbidden(err):
		return "forbidden (check repo access / org SSO authorization)"
	case github.IsNotFound(err):
		return "not found"
	default:
		return err.Error()
	}
}
