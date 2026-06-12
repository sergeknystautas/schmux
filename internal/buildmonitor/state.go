//go:build !nobuildmonitor

package buildmonitor

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/sergeknystautas/schmux/internal/fileutil"
)

// UnitState is a snapshot of the last check for a single monitored repo.
type UnitState struct {
	RepoName  string          `json:"repo_name"`
	Repo      string          `json:"repo"`
	Branch    string          `json:"branch"`
	Workflows []WorkflowState `json:"workflows,omitempty"`
	CheckedAt string          `json:"checked_at,omitempty"`
	LastError string          `json:"last_error,omitempty"`
	// RemediationWorkspaceID is the workspace this feature created for the
	// current failure episode; RemediationSHA is the commit of the failure
	// that started it. Carried while any workflow is failing, cleared when
	// none is. Additional workflow failures get sessions in this workspace.
	RemediationWorkspaceID string `json:"remediation_workspace_id,omitempty"`
	RemediationSHA         string `json:"remediation_sha,omitempty"`
}

// WorkflowState is the latest-run snapshot for one active workflow in a
// monitored repo.
type WorkflowState struct {
	Name       string      `json:"name"`
	Path       string      `json:"path"`
	WorkflowID int64       `json:"workflow_id,omitempty"`
	RunID      int64       `json:"run_id,omitempty"`
	RunNumber  int         `json:"run_number,omitempty"`
	Status     string      `json:"status,omitempty"`
	Conclusion string      `json:"conclusion,omitempty"`
	HTMLURL    string      `json:"html_url,omitempty"`
	HeadSHA    string      `json:"head_sha,omitempty"`
	FailedJobs []FailedJob `json:"failed_jobs,omitempty"`
	// FirstFailureRunID is the run that moved this workflow into the failing
	// state. Set on non-failure→failure, carried while failing, cleared on
	// recovery. Phase C reads it to know which run triggered remediation.
	FirstFailureRunID int64 `json:"first_failure_run_id,omitempty"`
	// SessionID is the session launched to fix this workflow's failure;
	// LaunchError records why a launch failed. Carried while failing,
	// cleared on recovery.
	SessionID   string `json:"session_id,omitempty"`
	LaunchError string `json:"launch_error,omitempty"`
}

// FailedJob holds the id, name, and link of a failed CI job.
type FailedJob struct {
	ID      int64  `json:"id,omitempty"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
}

// ReadState reads a unit state file. Returns nil, nil if the file does not exist.
func ReadState(path string) (*UnitState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s UnitState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// WriteState atomically writes a unit state file.
func WriteState(path string, s *UnitState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(path, data, 0o600)
}
