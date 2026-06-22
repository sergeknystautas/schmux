//go:build !nobuildmonitor

package buildmonitor

import (
	"context"

	"github.com/sergeknystautas/schmux/internal/github"
)

// Actions is the subset of the GitHub Actions client that CheckUnit needs.
type Actions interface {
	ListWorkflows(ctx context.Context, token string, info github.RepoInfo) ([]github.Workflow, error)
	ListRepoRuns(ctx context.Context, token string, info github.RepoInfo, branch string) ([]github.WorkflowRun, error)
	ListRunJobs(ctx context.Context, token string, info github.RepoInfo, runID int64) ([]github.WorkflowJob, error)
}

// Unit is the resolved input for a build-monitor check.
type Unit struct {
	Slug     string
	RepoName string
	Repo     string // owner/repo
	Info     github.RepoInfo
	Branch   string
	Token    string
}

// CheckUnit fetches the latest run of every active workflow on the unit's
// branch and returns a snapshot.
func CheckUnit(ctx context.Context, client Actions, u Unit) *UnitState {
	s := &UnitState{RepoName: u.RepoName, Repo: u.Repo, Branch: u.Branch}
	workflows, err := client.ListWorkflows(ctx, u.Token, u.Info)
	if err != nil {
		s.LastError = classify(err)
		return s
	}
	runs, err := client.ListRepoRuns(ctx, u.Token, u.Info, u.Branch)
	if err != nil {
		s.LastError = classify(err)
		return s
	}
	for _, wf := range workflows {
		if wf.State != "active" {
			continue
		}
		ws := WorkflowState{Name: wf.Name, Path: wf.Path, WorkflowID: wf.ID}
		newest := newestRun(runs, wf.ID)
		if newest == nil {
			s.Workflows = append(s.Workflows, ws)
			continue
		}

		if newest.Status != "completed" {
			// Newest run is pending (queued, in_progress, etc.)
			ws.RunID = newest.ID
			ws.RunNumber = newest.RunNumber
			ws.Status = newest.Status
			ws.Conclusion = ""
			ws.HTMLURL = newest.HTMLURL
			ws.HeadSHA = newest.HeadSHA
		} else {
			// Newest run is completed
			ws.RunID = newest.ID
			ws.RunNumber = newest.RunNumber
			ws.Status = newest.Status
			ws.Conclusion = newest.Conclusion
			ws.HTMLURL = newest.HTMLURL
			ws.HeadSHA = newest.HeadSHA
			if newest.Conclusion == "failure" {
				jobs, err := client.ListRunJobs(ctx, u.Token, u.Info, newest.ID)
				if err != nil {
					s.LastError = classify(err)
				} else {
					for _, j := range jobs {
						if j.Conclusion == "failure" {
							ws.FailedJobs = append(ws.FailedJobs, FailedJob{ID: j.ID, Name: j.Name, HTMLURL: j.HTMLURL})
						}
					}
				}
			}
		}
		s.Workflows = append(s.Workflows, ws)
	}
	return s
}

func newestRun(runs []github.WorkflowRun, workflowID int64) *github.WorkflowRun {
	for i := range runs {
		if runs[i].WorkflowID == workflowID {
			return &runs[i]
		}
	}
	return nil
}

func classify(err error) string {
	switch {
	case github.IsUnauthorized(err):
		return "unauthorized"
	case github.IsNotFound(err):
		return "not found"
	default:
		return err.Error()
	}
}
