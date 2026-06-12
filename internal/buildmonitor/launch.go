//go:build !nobuildmonitor

package buildmonitor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// StampWorkspace records the episode workspace on the unit. The first
// stamp wins; later calls are refused so the episode keeps one workspace.
func StampWorkspace(s *UnitState, workspaceID, sha string) bool {
	if s.RemediationWorkspaceID != "" {
		return false
	}
	s.RemediationWorkspaceID = workspaceID
	s.RemediationSHA = sha
	return true
}

// StampLaunch records a launch outcome on a workflow, guarded by the
// failure episode: it applies only while the workflow is still failing on
// the same FirstFailureRunID the launch was planned for.
func StampLaunch(s *UnitState, workflowID, episodeRunID int64, sessionID, launchErr string) bool {
	for i := range s.Workflows {
		w := &s.Workflows[i]
		if w.WorkflowID != workflowID {
			continue
		}
		if !isFailing(w) || w.FirstFailureRunID != episodeRunID {
			return false
		}
		w.SessionID = sessionID
		w.LaunchError = launchErr
		return true
	}
	return false
}

var workflowSlugRe = regexp.MustCompile(`[^a-z0-9]+`)

// WorkflowSlug slugifies a workflow name the way the dashboard slugs repo
// names: lowercase, runs of non-alphanumerics collapse to '-', trimmed.
func WorkflowSlug(name string) string {
	return strings.Trim(workflowSlugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

// ContextDir is the worktree-relative directory holding one workflow's
// failure context.
func ContextDir(workflowName string) string {
	return filepath.Join(".schmux", "build-monitor", WorkflowSlug(workflowName))
}

// ShortSHA returns the first 8 characters of a commit SHA.
func ShortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

// FixBranch names a remediation branch the way workspace branches are named
// in practice (type/descriptive-name): fix/<workflow-slug>-<short-sha>.
// Falls back to "build" when the workflow name slugs to nothing.
func FixBranch(workflowName, sha string) string {
	slug := WorkflowSlug(workflowName)
	if slug == "" {
		slug = "build"
	}
	return fmt.Sprintf("fix/%s-%s", slug, ShortSHA(sha))
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

// WriteWorkspaceContext writes one workflow's failure context into a
// workspace: <ContextDir>/failure.json plus logs/<job-id>.log per
// downloaded log. logErrors records jobs whose download failed. Returns
// the absolute context directory.
func WriteWorkspaceContext(workspacePath string, info FailureInfo, logs map[int64][]byte, logErrors map[int64]string) (string, error) {
	dir := filepath.Join(workspacePath, ContextDir(info.Workflow.Name))
	logsDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return "", err
	}
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
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "failure.json"), data, 0o644); err != nil {
		return "", err
	}
	for jobID, content := range logs {
		if err := os.WriteFile(filepath.Join(logsDir, fmt.Sprintf("%d.log", jobID)), content, 0o644); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// BuildPrompt renders the agent prompt for one workflow failure.
// contextDir is the worktree-relative directory from ContextDir.
func BuildPrompt(info FailureInfo, contextDir string) string {
	var b strings.Builder
	w := info.Workflow
	fmt.Fprintf(&b, "CI failure: workflow %q on %s (run #%d) failed.\n", w.Name, info.Repo, w.RunNumber)
	fmt.Fprintf(&b, "Run: %s\n", w.HTMLURL)
	fmt.Fprintf(&b, "Failing commit: %s\n\n", w.HeadSHA)
	fmt.Fprintf(&b, "First, move this branch to the failing commit: git reset --hard %s\n", w.HeadSHA)
	b.WriteString("(Do not use git checkout for this — it detaches HEAD.)\n\n")
	if len(w.FailedJobs) > 0 {
		b.WriteString("Failed jobs:\n")
		for _, j := range w.FailedJobs {
			fmt.Fprintf(&b, "- %s (%s)\n", j.Name, j.HTMLURL)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Failure details: %s/failure.json. Full job logs: %s/logs/.\n\n", contextDir, contextDir)
	b.WriteString("Other remediation sessions for the same commit may share this workspace.\n")
	b.WriteString("Diagnose the root cause from the logs, fix it on this branch, and validate the fix.\n")
	return b.String()
}
