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
}

// WorkflowState is the latest-run snapshot for one active workflow in a
// monitored repo.
type WorkflowState struct {
	Name       string      `json:"name"`
	Path       string      `json:"path"`
	RunID      int64       `json:"run_id,omitempty"`
	RunNumber  int         `json:"run_number,omitempty"`
	Status     string      `json:"status,omitempty"`
	Conclusion string      `json:"conclusion,omitempty"`
	HTMLURL    string      `json:"html_url,omitempty"`
	FailedJobs []FailedJob `json:"failed_jobs,omitempty"`
}

// FailedJob holds the name and link of a failed CI job.
type FailedJob struct {
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
