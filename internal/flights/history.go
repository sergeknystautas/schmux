package flights

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/sergeknystautas/schmux/internal/flighthistory"
)

// AuditContext says who caused a snapshot edit and why. The caller supplies it;
// it is never guessed from field diffs.
type AuditContext struct {
	Actor         flighthistory.Actor
	Cause         string
	CorrelationID string
	SourceTurnID  string
}

func systemAudit(cause string) AuditContext {
	return AuditContext{Actor: flighthistory.ActorSystem, Cause: cause}
}

func monitorAudit(cause string) AuditContext {
	return AuditContext{Actor: flighthistory.ActorMonitor, Cause: cause}
}

func userAudit(cause string) AuditContext {
	return AuditContext{Actor: flighthistory.ActorUser, Cause: cause}
}

// operationAudit attributes a change made while a recorded operation runs: the
// operation names the action, correlation, and deciding pilot turn.
func (m *Manager) operationAudit(flightID, opID, suffix string) AuditContext {
	audit := systemAudit("operation" + suffix)
	if f, ok := m.Get(flightID); ok {
		if op := f.operation(opID); op != nil {
			audit.Cause = string(op.Action) + suffix
			audit.CorrelationID = op.CorrelationID
			audit.SourceTurnID = op.SourceTurnID
		}
	}
	return audit
}

func snapshotDigestHex(sum [sha256.Size]byte) string { return hex.EncodeToString(sum[:]) }

// HistoryState is the bounded projection of the row that each ledger event
// records: its workflow position, models, workspace, and collection sizes.
// Its size does not depend on how many turns or messages the row holds.
func (f Flight) HistoryState() flighthistory.RowState {
	s := flighthistory.RowState{
		WorkflowState:      string(f.WorkflowState),
		CleanupState:       string(f.CleanupState),
		PhaseVisit:         f.PhaseVisit,
		PhaseTimeUncertain: f.PhaseTimeUncertain,
		PlannerModel:       f.PlannerModel,
		ExecutorModel:      f.ExecutorModel,
		Operations:         len(f.Operations),
		UpdatedAt:          f.UpdatedAt,
	}
	if !f.PhaseStartedAt.IsZero() {
		started := f.PhaseStartedAt
		s.PhaseStartedAt = &started
	}
	if f.Workspace != nil {
		s.WorkspaceID = f.Workspace.WorkspaceID
	}
	if f.Monitor != nil {
		s.MonitorTurns = len(f.Monitor.Turns)
		s.MonitorMessages = len(f.Monitor.Messages)
	}
	return s
}

// historyDigest is the digest of the row as history identifies it. The
// monitor's subscription error is live status about a session's runtime (for
// example a session that no longer exists), retried every tick; it is not
// part of the flight's history, so an edit that changes only it leaves the
// digest alone and records no event.
func historyDigest(f Flight) (string, error) {
	if f.Monitor != nil {
		monitor := *f.Monitor
		monitor.Error = ""
		f.Monitor = &monitor
		if len(monitor.Turns) == 0 && len(monitor.Messages) == 0 {
			f.Monitor = nil
		}
	}
	data, err := json.Marshal(f)
	if err != nil {
		return "", fmt.Errorf("marshal flight %s: %w", f.ID, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func historyDigests(flights []Flight) (map[string]string, error) {
	digests := make(map[string]string, len(flights))
	for _, f := range flights {
		digest, err := historyDigest(f)
		if err != nil {
			return nil, err
		}
		digests[f.ID] = digest
	}
	return digests, nil
}

// rowEvent fills the per-row fields of an event from a flight.
func rowEvent(base flighthistory.Event, change flighthistory.Change, f Flight, digest string) flighthistory.Event {
	ev := base
	ev.Change = change
	ev.FlightID = f.ID
	ev.RowSHA256 = digest
	ev.State = f.HistoryState()
	return ev
}

// auditEvents compares the rows before and after an edit and returns one event
// per created, updated, or removed flight. Related events share one event ID,
// timestamp, cause, correlation, and snapshot digest. A removed row's event
// carries its last digest and state; its content is in the archive.
func (m *Manager) auditEvents(audit AuditContext, before, after []Flight, sum [sha256.Size]byte) ([]flighthistory.Event, error) {
	oldDigests, err := historyDigests(before)
	if err != nil {
		return nil, err
	}
	newDigests, err := historyDigests(after)
	if err != nil {
		return nil, err
	}
	base := flighthistory.Event{
		SchemaVersion:  flighthistory.SchemaVersion,
		EventID:        randomToken("fe-"),
		RecordedAt:     m.deps.Now().UTC(),
		Actor:          audit.Actor,
		Cause:          audit.Cause,
		CorrelationID:  audit.CorrelationID,
		SourceTurnID:   audit.SourceTurnID,
		SnapshotSHA256: snapshotDigestHex(sum),
	}
	var events []flighthistory.Event
	for _, f := range after {
		old, existed := oldDigests[f.ID]
		switch {
		case !existed:
			events = append(events, rowEvent(base, flighthistory.ChangeCreated, f, newDigests[f.ID]))
		case old != newDigests[f.ID]:
			events = append(events, rowEvent(base, flighthistory.ChangeUpdated, f, newDigests[f.ID]))
		}
	}
	for _, f := range before {
		if _, kept := newDigests[f.ID]; kept {
			continue
		}
		events = append(events, rowEvent(base, flighthistory.ChangeRemoved, f, oldDigests[f.ID]))
	}
	return events, nil
}

// appendEvents records events and remembers each flight's last recorded
// event. The caller holds m.mu.
func (m *Manager) appendEvents(events []flighthistory.Event) error {
	if m.history == nil || len(events) == 0 {
		return nil
	}
	m.recoverPendingEvents()
	all := append(m.pendingEvents, events...)
	m.pendingEvents = all
	if err := m.history.Append(all); err != nil {
		return fmt.Errorf("flights: record history: %w", err)
	}
	m.pendingEvents = nil
	for _, ev := range all {
		if ev.Change == flighthistory.ChangeRemoved {
			delete(m.lastEvent, ev.FlightID)
		} else {
			m.lastEvent[ev.FlightID] = ev
		}
	}
	return nil
}

func (m *Manager) recoverPendingEvents() {
	now := m.deps.Now().UTC()
	for i, ev := range m.pendingEvents {
		if ev.Change == flighthistory.ChangeRemoved {
			continue
		}
		m.pendingEvents[i].Change = flighthistory.ChangeRecovered
		m.pendingEvents[i].Cause = "recovery"
		m.pendingEvents[i].RecordedAt = now
	}
}

// syncHistory reconciles the ledger with the snapshot just loaded. With no
// events, every row is imported as a baseline; otherwise any row whose digest
// differs from its last recorded event, or that vanished without a removal
// event, was changed by a snapshot write whose event never landed and is
// recovered now, at recovery time. History failures are logged, not fatal:
// the next load finds the same gap and retries. The caller holds m.mu.
func (m *Manager) syncHistory(rows []Flight, sum [sha256.Size]byte) {
	if m.history == nil {
		return
	}
	m.lastEvent = map[string]flighthistory.Event{}
	events, malformed, err := flighthistory.ReadEvents(m.deps.HistoryPath)
	if err != nil {
		m.logger.Error("flights: read history failed; skipping reconciliation", "path", m.deps.HistoryPath, "err", err)
		return
	}
	if len(malformed) > 0 {
		m.logger.Warn("flights: history has malformed lines; retained", "path", m.deps.HistoryPath, "lines", len(malformed))
	}
	for _, ev := range events {
		if ev.Change == flighthistory.ChangeRemoved {
			delete(m.lastEvent, ev.FlightID)
		} else {
			m.lastEvent[ev.FlightID] = ev
		}
	}
	current, err := historyDigests(rows)
	if err != nil {
		m.logger.Error("flights: digest flights for history failed", "err", err)
		return
	}
	base := flighthistory.Event{Actor: flighthistory.ActorSystem, SnapshotSHA256: snapshotDigestHex(sum)}
	var pending []flighthistory.Event
	if len(events) == 0 {
		base.Cause = "baseline"
		for _, f := range rows {
			pending = append(pending, rowEvent(base, flighthistory.ChangeBaseline, f, current[f.ID]))
		}
	} else {
		base.Cause = "recovery"
		for _, f := range rows {
			if last, ok := m.lastEvent[f.ID]; ok && last.RowSHA256 == current[f.ID] {
				continue
			}
			pending = append(pending, rowEvent(base, flighthistory.ChangeRecovered, f, current[f.ID]))
		}
		var gone []string
		for id := range m.lastEvent {
			if _, ok := current[id]; !ok {
				gone = append(gone, id)
			}
		}
		sort.Strings(gone)
		for _, id := range gone {
			// The row is gone, so its last recorded digest and state are all
			// the removal can name.
			last := m.lastEvent[id]
			ev := base
			ev.Change, ev.FlightID, ev.RowSHA256, ev.State = flighthistory.ChangeRemoved, id, last.RowSHA256, last.State
			pending = append(pending, ev)
		}
	}
	if err := m.appendEvents(pending); err != nil {
		m.logger.Error("flights: history reconciliation failed; retrying at next load", "err", err)
	}
}
