package flights

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// ExecutionCheck is the daemon's check of an executor turn: whether the
// session used the execution skill, and whether the report exists. Neither
// is taken from the executor's own claim.
type ExecutionCheck struct {
	Skill          string `json:"skill"`       // one of the three spec texts
	ReportPath     string `json:"report_path"` // the flight's ReportTarget
	ReportVerified bool   `json:"report_verified"`
	ReportError    string `json:"report_error,omitempty"` // "not written yet" or the failure; empty when verified
}

// errReportNotWritten is the report check's text while no file exists at the
// report target.
var errReportNotWritten = errors.New("not written yet")

// executingPlansEvidence reports the earliest use of the executing-plans skill
// by the named sessions: a Claude Skill tool call or a Codex read of the skill
// file. Only records of those sessions count.
func executingPlansEvidence(turns []MonitoredTurn, sessionIDs map[string]bool) string {
	return skillEvidence(turns, sessionIDs, "executing-plans")
}

// skillEvidence reports the earliest use of a skill by the named sessions. A
// namespaced name counts too, so "superpowers:test-rules-review" is evidence
// of the test-rules-review skill.
func skillEvidence(turns []MonitoredTurn, sessionIDs map[string]bool, skill string) string {
	var found *SkillUse
	for _, t := range turns {
		if !sessionIDs[t.Turn.SessionID] {
			continue
		}
		for _, use := range t.Turn.Skills {
			if use.Skill != skill && !strings.HasSuffix(use.Skill, ":"+skill) {
				continue
			}
			if found == nil || use.Seq < found.Seq {
				use := use
				found = &use
			}
		}
	}
	switch {
	case found == nil:
		return skill + " not found"
	case found.Evidence == "invoked":
		return fmt.Sprintf("%s invoked at record %d", skill, found.Seq)
	default:
		return fmt.Sprintf("%s skill file read at record %d", skill, found.Seq)
	}
}

// verifyExecutionReport checks the report is a regular, nonempty file inside
// the workspace, modified after execution started so a file left from before
// does not count. Git-ignore status is not checked: the report is a working
// document, not a committed one.
func verifyExecutionReport(ws *WorkspaceAssignment, path string, startedAt time.Time) error {
	if path == "" {
		return errors.New("the flight has no report target")
	}
	if startedAt.IsZero() {
		return errors.New("the flight has no execution start time")
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return errReportNotWritten
	}
	if err := checkArtifact(path); err != nil {
		return err
	}
	if _, _, err := insideWorkspace(ws, "report", path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.ModTime().Before(startedAt) {
		return fmt.Errorf("%s was not modified since execution started at %s", path, startedAt.Format(time.RFC3339))
	}
	return nil
}

// recordExecutionCheck checks a completed executor turn and retains the result
// on it. The first turn whose report verifies also marks the report verified
// on the flight; later turns keep the flight's path and still carry their own
// check.
func (m *Manager) recordExecutionCheck(ctx context.Context, flightID string, t MonitoredTurn) {
	f, ok := m.Get(flightID)
	if !ok || f.Monitor == nil {
		return
	}
	executor := f.sessionFor(RoleExecutor)
	if executor == nil {
		return
	}
	sessions := map[string]bool{executor.SessionID: true}
	for _, id := range executor.PriorSessionIDs {
		sessions[id] = true
	}
	var startedAt time.Time
	if f.ExecutionStartedAt != nil {
		startedAt = *f.ExecutionStartedAt
	}
	check := ExecutionCheck{
		Skill:      executingPlansEvidence(f.Monitor.Turns, sessions),
		ReportPath: f.ReportTarget,
	}
	if err := verifyExecutionReport(f.Workspace, f.ReportTarget, startedAt); err != nil {
		check.ReportError = err.Error()
	} else {
		check.ReportVerified = true
	}
	if ctx.Err() != nil {
		return
	}
	err := m.editFlight(flightID, func(f *Flight) error {
		if f.Monitor == nil {
			return fmt.Errorf("monitor: flight %s has no monitor", f.ID)
		}
		for i := range f.Monitor.Turns {
			if f.Monitor.Turns[i].ID != t.ID {
				continue
			}
			c := check
			f.Monitor.Turns[i].Execution = &c
			f.Monitor.Turns[i].UpdatedAt = m.deps.Now()
			if check.ReportVerified && f.ReportPath == "" {
				f.ReportPath = f.ReportTarget
			}
			return nil
		}
		return fmt.Errorf("monitor: flight %s has no turn %s", f.ID, t.ID)
	})
	if err != nil {
		m.logger.Warn("monitor: retain execution check failed", "err", err)
	}
}
