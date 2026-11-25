//go:build !nobuildmonitor

package buildmonitor

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const (
	RemediationClaimed    = "claimed"
	RemediationLaunched   = "launched"
	RemediationJoined     = "joined" // an agent was already working; nothing launched
	RemediationFailed     = "failed"
	maxRecentRemediations = 50
)

// FailureInfo is everything the launcher knows about one workflow failure.
// Workflow is a snapshot taken when the failure was detected.
type FailureInfo struct {
	RepoName string // schmux repo display name
	Repo     string // owner/repo
	Workflow WorkflowState
}

// PlanLaunches returns the workflow IDs that should get a remediation
// session for this check pass, in event order (ApplyTransitions emits
// events in state order). FromUnknown failures are excluded: a workflow
// already failing at first observation is recorded as baseline, not
// auto-remediated.
func PlanLaunches(events []TransitionEvent) []int64 {
	var ids []int64
	for _, e := range events {
		if e.Kind == TransitionEnteredFailure && !e.FromUnknown {
			ids = append(ids, e.WorkflowID)
		}
	}
	return ids
}

// ClaimRemediation atomically reserves a workflow run for auto-remediation in
// the caller's state mutation. A run already present in the durable ledger is
// never claimed twice, regardless of later status snapshots.
func ClaimRemediation(s *UnitState, workflow WorkflowState, observedAt string) bool {
	if workflow.WorkflowID == 0 || workflow.FirstFailureRunID == 0 {
		return false
	}
	if remediationIndex(s, workflow.WorkflowID, workflow.FirstFailureRunID) >= 0 {
		return false
	}
	s.RecentRemediations = append(s.RecentRemediations, RemediationRecord{
		WorkflowID:      workflow.WorkflowID,
		WorkflowName:    workflow.Name,
		RunID:           workflow.FirstFailureRunID,
		HeadSHA:         workflow.HeadSHA,
		FirstObservedAt: observedAt,
		Status:          RemediationClaimed,
	})
	trimRemediations(s)
	return true
}

// RecordManualLaunch records a "Launch workspace" remediation of workflow's
// current run: on the ledger, and as the row's session, so the page links the
// running agent instead of offering another launch. The row's session is set
// whatever the workflow's state: the agent works in the workflow's dedicated
// workspace, which outlives the failure episode.
func RecordManualLaunch(s *UnitState, workflow WorkflowState, sessionID, observedAt string) {
	if i := remediationIndex(s, workflow.WorkflowID, workflow.RunID); i >= 0 {
		r := &s.RecentRemediations[i]
		r.Status, r.SessionID, r.LaunchError = RemediationLaunched, sessionID, ""
	} else {
		s.RecentRemediations = append(s.RecentRemediations, RemediationRecord{
			WorkflowID:      workflow.WorkflowID,
			WorkflowName:    workflow.Name,
			RunID:           workflow.RunID,
			HeadSHA:         workflow.HeadSHA,
			FirstObservedAt: observedAt,
			Status:          RemediationLaunched,
			SessionID:       sessionID,
		})
		trimRemediations(s)
	}
	for i := range s.Workflows {
		w := &s.Workflows[i]
		if w.WorkflowID == workflow.WorkflowID {
			w.SessionID, w.LaunchError = sessionID, ""
		}
	}
}

// trimRemediations bounds the ledger to the newest maxRecentRemediations.
func trimRemediations(s *UnitState) {
	if len(s.RecentRemediations) > maxRecentRemediations {
		s.RecentRemediations = append([]RemediationRecord(nil), s.RecentRemediations[len(s.RecentRemediations)-maxRecentRemediations:]...)
	}
}

// remediationIndex finds the ledger record for one workflow run.
func remediationIndex(s *UnitState, workflowID, runID int64) int {
	for i := range s.RecentRemediations {
		if s.RecentRemediations[i].WorkflowID == workflowID && s.RecentRemediations[i].RunID == runID {
			return i
		}
	}
	return -1
}

// StampJoined records in the durable ledger that a run joined the agent
// already working on its workflow. The row already points at that agent.
func StampJoined(s *UnitState, workflowID, runID int64, sessionID string) bool {
	i := remediationIndex(s, workflowID, runID)
	if i < 0 {
		return false
	}
	r := &s.RecentRemediations[i]
	if r.Status == RemediationJoined && r.SessionID == sessionID && r.LaunchError == "" {
		return false
	}
	r.Status, r.SessionID, r.LaunchError = RemediationJoined, sessionID, ""
	return true
}

// StampLaunch records a launch outcome in the durable ledger. It also points
// the workflow row at the launched agent: the agent works in the workflow's
// dedicated workspace, so it belongs to the row even when the failure
// episode moved on. A failed launch has no session and leaves the row's agent
// alone. The launch error belongs to the episode, so it is only recorded
// while the same episode is still active.
func StampLaunch(s *UnitState, workflowID, episodeRunID int64, sessionID, launchErr string) bool {
	changed := false
	if i := remediationIndex(s, workflowID, episodeRunID); i >= 0 {
		r := &s.RecentRemediations[i]
		status := RemediationLaunched
		if launchErr != "" {
			status = RemediationFailed
		}
		if r.SessionID != sessionID || r.LaunchError != launchErr || r.Status != status {
			r.SessionID = sessionID
			r.LaunchError = launchErr
			r.Status = status
			changed = true
		}
	}
	for i := range s.Workflows {
		w := &s.Workflows[i]
		if w.WorkflowID != workflowID {
			continue
		}
		if sessionID != "" && w.SessionID != sessionID {
			w.SessionID = sessionID
			changed = true
		}
		if isFailing(w) && w.FirstFailureRunID == episodeRunID && w.LaunchError != launchErr {
			w.LaunchError = launchErr
			changed = true
		}
		return changed
	}
	return changed
}

var workflowSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

// WorkflowSlug slugifies a workflow name the way the dashboard slugs repo
// names: lowercase, runs of non-alphanumerics collapse to '-', trimmed.
func WorkflowSlug(name string) string {
	return strings.Trim(workflowSlugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

// workflowSlugOrBuild is the workflow's slug, with a fallback for names that
// slug to nothing.
func workflowSlugOrBuild(workflowName string) string {
	slug := WorkflowSlug(workflowName)
	if slug == "" {
		slug = "build"
	}
	return slug
}

// WorkspaceName is the dedicated workspace a monitored workflow works in:
// "build-monitor/<repo-slug>/<workflow-slug>". The two segments keep the
// repo and the workflow apart, so no two workflows share a workspace.
func WorkspaceName(repoSlug, workflowName string) string {
	return fmt.Sprintf("build-monitor/%s/%s", repoSlug, workflowSlugOrBuild(workflowName))
}

// WorkspaceLabel is the workspace's display name: its name with the
// separators flattened.
func WorkspaceLabel(repoSlug, workflowName string) string {
	return strings.ReplaceAll(WorkspaceName(repoSlug, workflowName), "/", "-")
}

// WorkspaceBranch is the branch a monitored workflow's dedicated workspace is
// created on. It is stable per workflow, so the workspace survives the
// failure episode that created it.
func WorkspaceBranch(workflowName string) string {
	return "fix/" + workflowSlugOrBuild(workflowName)
}

// ShortSHA returns the first 8 characters of a commit SHA.
func ShortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// failureContext is the JSON shape of failure.json.
type failureContext struct {
	Repo       string            `json:"repo"`
	RepoName   string            `json:"repo_name"`
	Workflow   string            `json:"workflow"`
	Path       string            `json:"path"`
	WorkflowID int64             `json:"workflow_id"`
	RunID      int64             `json:"run_id"`
	RunNumber  int               `json:"run_number"`
	RunURL     string            `json:"run_url"`
	HeadSHA    string            `json:"head_sha"`
	FailedJobs []FailedJob       `json:"failed_jobs"`
	LogErrors  map[string]string `json:"log_download_errors,omitempty"` // job ID → error
}

// ContextFile is one file of a workflow's failure context. The spawn copies
// it into the session's workspace as a file attachment.
type ContextFile struct {
	Name string
	Data []byte
}

// FailureFiles renders one workflow's failure context: failure.json, then
// <job-id>.log per downloaded log in failed-job order. logErrors records
// jobs whose download failed.
func FailureFiles(info FailureInfo, logs map[int64][]byte, logErrors map[int64]string) ([]ContextFile, error) {
	fc := failureContext{
		Repo:       info.Repo,
		RepoName:   info.RepoName,
		Workflow:   info.Workflow.Name,
		Path:       info.Workflow.Path,
		WorkflowID: info.Workflow.WorkflowID,
		RunID:      info.Workflow.RunID,
		RunNumber:  info.Workflow.RunNumber,
		RunURL:     info.Workflow.HTMLURL,
		HeadSHA:    info.Workflow.HeadSHA,
		FailedJobs: info.Workflow.FailedJobs,
	}
	if len(logErrors) > 0 {
		fc.LogErrors = make(map[string]string, len(logErrors))
		for id, msg := range logErrors {
			fc.LogErrors[fmt.Sprintf("%d", id)] = msg
		}
	}
	data, err := json.MarshalIndent(fc, "", "  ")
	if err != nil {
		return nil, err
	}
	files := []ContextFile{{Name: "failure.json", Data: data}}
	for _, j := range info.Workflow.FailedJobs {
		if content, ok := logs[j.ID]; ok {
			files = append(files, ContextFile{Name: fmt.Sprintf("%d.log", j.ID), Data: content})
		}
	}
	return files, nil
}

// BuildPrompt renders the agent prompt for one workflow failure. The spawn
// appends the paths of the FailureFiles attachments.
func BuildPrompt(info FailureInfo) string {
	var b strings.Builder
	w := info.Workflow
	fmt.Fprintf(&b, "CI failure: workflow %q on %s (run #%d) failed.\n", w.Name, info.Repo, w.RunNumber)
	fmt.Fprintf(&b, "Run: %s\n", w.HTMLURL)
	fmt.Fprintf(&b, "Failing commit: %s\n\n", w.HeadSHA)
	b.WriteString("First, investigate the failure. Before changing any code, send the user an update that covers all four points:\n")
	b.WriteString("1. Issue: explain the observed failure.\n")
	b.WriteString("2. Cause: explain what caused the failure to start happening, with supporting evidence.\n")
	b.WriteString("3. Reproduction: explain how you reproduced the failure and what you observed.\n")
	b.WriteString("4. Proposed fix: explain the specific change you would make and why.\n\n")
	fmt.Fprintf(&b, "For the investigation, move this branch to the failing commit: git reset --hard %s\n", w.HeadSHA)
	b.WriteString("(Do not use git checkout for this — it detaches HEAD.)\n\n")
	if len(w.FailedJobs) > 0 {
		b.WriteString("Failed jobs:\n")
		for _, j := range w.FailedJobs {
			fmt.Fprintf(&b, "- %s (%s)\n", j.Name, j.HTMLURL)
		}
		b.WriteString("\n")
	}
	b.WriteString("Failure details: the attached failure.json. Full job logs: the attached <job-id>.log files.\n\n")
	b.WriteString("After sending the investigation update, fix the root cause on this branch and validate the fix.\n")
	return b.String()
}
