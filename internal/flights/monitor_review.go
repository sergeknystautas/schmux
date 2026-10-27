package flights

import (
	"context"
	"fmt"
)

// ReviewCheck is the daemon's skill evidence for a reviewer turn. It records
// whether the reviewer's session invoked the test-rules-review skill, read its
// skill file, or never used it. The daemon never inspects the workspace or a
// command's output to produce it.

// testRulesReviewEvidence reports the earliest use of the test-rules-review
// skill by the named sessions: a Claude Skill tool call or a Codex read of
// the skill file. Only records of those sessions count.
func testRulesReviewEvidence(turns []MonitoredTurn, sessionIDs map[string]bool) string {
	return skillEvidence(turns, sessionIDs, "test-rules-review")
}

// recordReviewCheck records the reviewer's skill evidence on a completed
// reviewer turn. It never reads the workspace: the evidence comes from the
// session's own records. The evidence is scoped to the session that produced
// the turn, so a later reviewer never inherits an earlier reviewer's skill
// use.
func (m *Manager) recordReviewCheck(ctx context.Context, flightID string, t MonitoredTurn) {
	f, ok := m.Get(flightID)
	if !ok || f.Monitor == nil {
		return
	}
	if t.Turn.SessionID == "" {
		return
	}
	check := ReviewCheck{Skill: testRulesReviewEvidence(f.Monitor.Turns, map[string]bool{t.Turn.SessionID: true})}
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
			f.Monitor.Turns[i].Review = &c
			f.Monitor.Turns[i].UpdatedAt = m.deps.Now()
			return nil
		}
		return fmt.Errorf("monitor: flight %s has no turn %s", f.ID, t.ID)
	})
	if err != nil {
		m.logger.Warn("monitor: retain review check failed", "err", err)
	}
}
