package flights

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// recordPlannerTurn persists a completed planner or pilot response. Idempotent on
// (SessionID, EndSeq): a second call with the same key is a no-op. The
// returned id is the work key the monitor and the manager use to deduplicate.
func (m *Manager) recordPlannerTurn(flightID string, turn PlannerTurn) (string, error) {
	if turn.SessionID == "" || turn.EndSeq == 0 {
		return "", errors.New("monitor: recordPlannerTurn requires session_id and end_seq")
	}
	if turn.Outcome == "" {
		turn.Outcome = "completed"
	}
	id := monitorTurnID(turn)
	var foundExisting bool
	err := m.editFlight(flightID, func(f *Flight) error {
		if f.Monitor == nil {
			f.Monitor = &FlightMonitor{}
		}
		for _, t := range f.Monitor.Turns {
			if t.ID == id {
				// Already retained. A later attempt may still want to
				// append a classification, but the turn itself is final.
				foundExisting = true
				return nil
			}
		}
		now := m.deps.Now()
		f.Monitor.Turns = append(f.Monitor.Turns, MonitoredTurn{
			ID:        id,
			Turn:      turn,
			Status:    MonitoredTurnPending,
			CreatedAt: now,
			UpdatedAt: now,
		})
		return nil
	})
	if err != nil {
		return "", err
	}
	if foundExisting {
		return id, nil
	}
	return id, nil
}

// recordMonitorAttempt appends one classification attempt to the named turn.
// The definition snapshot is stored so a future YAML edit does not erase the
// evidence of what was asked. The turn's Status is updated to classified when
// the attempt succeeded, failed when it errored, or skipped when the role has
// no definitions.
func (m *Manager) recordMonitorAttempt(flightID, turnID string, attempt MonitorAttempt) error {
	if attempt.CreatedAt.IsZero() {
		attempt.CreatedAt = m.deps.Now()
	}
	return m.editFlight(flightID, func(f *Flight) error {
		if f.Monitor == nil {
			return fmt.Errorf("monitor: flight %s has no monitor", f.ID)
		}
		for i := range f.Monitor.Turns {
			if f.Monitor.Turns[i].ID != turnID {
				continue
			}
			t := &f.Monitor.Turns[i]
			t.Attempts = append(t.Attempts, attempt.clone())
			t.UpdatedAt = m.deps.Now()
			switch {
			case attempt.Classification != nil:
				t.Status = MonitoredTurnClassified
			case attempt.Error != "":
				t.Status = MonitoredTurnFailed
			default:
				t.Status = MonitoredTurnSkipped
			}
			return nil
		}
		return fmt.Errorf("monitor: flight %s has no turn %s", f.ID, turnID)
	})
}

// recordFlightMessage inserts a pending message and deduplicates by source
// turn and kind: interesting rulings produce one combined notification, and
// replaying a pilot decision cannot send guidance twice. Returns the persisted message
// with its assigned id.
func (m *Manager) recordFlightMessage(flightID string, msg FlightMessage) (FlightMessage, error) {
	if msg.RecipientSessionID == "" {
		return FlightMessage{}, errors.New("monitor: recordFlightMessage requires recipient")
	}
	if msg.Kind == "" {
		msg.Kind = "notification"
	}
	if msg.Status == "" {
		msg.Status = MessageStatusPending
	}
	if !msg.Status.valid() {
		return FlightMessage{}, fmt.Errorf("monitor: invalid status %q", msg.Status)
	}
	if msg.ID == "" {
		msg.ID = randomToken("msg-")
	}
	now := m.deps.Now()
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = now
	}
	msg.UpdatedAt = now
	var persisted FlightMessage
	err := m.editFlight(flightID, func(f *Flight) error {
		if f.Monitor == nil {
			f.Monitor = &FlightMonitor{}
		}
		if msg.SourceTurnID != "" {
			for _, existing := range f.Monitor.Messages {
				if existing.SourceTurnID == msg.SourceTurnID && existing.Kind == msg.Kind {
					// Already recorded a notification for this turn. Return
					// the existing one so the caller doesn't double-send.
					persisted = existing
					return nil
				}
			}
		}
		f.Monitor.Messages = append(f.Monitor.Messages, msg)
		persisted = msg
		return nil
	})
	if err != nil {
		return FlightMessage{}, err
	}
	return persisted, nil
}

// updateFlightMessage rewrites the named message with the supplied mutator.
// The mutator must not change the message id; otherwise the change is
// rejected so a retry can never alias a different message.
func (m *Manager) updateFlightMessage(flightID, messageID string, mutate func(msg *FlightMessage)) error {
	return m.editFlight(flightID, func(f *Flight) error {
		if f.Monitor == nil {
			return fmt.Errorf("monitor: flight %s has no monitor", f.ID)
		}
		for i := range f.Monitor.Messages {
			if f.Monitor.Messages[i].ID != messageID {
				continue
			}
			if mutate != nil {
				mutate(&f.Monitor.Messages[i])
			}
			f.Monitor.Messages[i].UpdatedAt = m.deps.Now()
			return nil
		}
		return fmt.Errorf("monitor: flight %s has no message %s", f.ID, messageID)
	})
}

// findMonitoredTurn returns a copy of the named turn, or false if absent.
// The work key is its id, derived from the source session and end sequence.
func (m *Manager) findMonitoredTurn(flightID, turnID string) (MonitoredTurn, bool) {
	f, ok := m.Get(flightID)
	if !ok || f.Monitor == nil {
		return MonitoredTurn{}, false
	}
	for _, t := range f.Monitor.Turns {
		if t.ID == turnID {
			return t, true
		}
	}
	return MonitoredTurn{}, false
}

// findMonitoredMessage returns a copy of the named message, or false.
func (m *Manager) findMonitoredMessage(flightID, messageID string) (FlightMessage, bool) {
	f, ok := m.Get(flightID)
	if !ok || f.Monitor == nil {
		return FlightMessage{}, false
	}
	for _, msg := range f.Monitor.Messages {
		if msg.ID == messageID {
			return msg, true
		}
	}
	return FlightMessage{}, false
}

// RetryMonitor validates the item, marks eligible failed work pending, and
// wakes the worker. It performs no HTTP request while holding the manager
// mutex. A retry on a successful or already-pending item is a conflict;
// unknown delivery is a conflict because the operator must inspect it first.
func (m *Manager) RetryMonitor(ctx context.Context, flightID, itemID string) error {
	unlock := m.lockFlight(flightID)
	defer unlock()
	f, ok := m.Get(flightID)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, flightID)
	}
	if f.CleanupState != CleanupIdle {
		return conflict("flight %s is %s", flightID, f.CleanupState)
	}
	if f.Monitor == nil {
		return fmt.Errorf("%w: monitor on flight %s", ErrNotFound, flightID)
	}
	for _, t := range f.Monitor.Turns {
		if t.ID != itemID {
			continue
		}
		switch t.Status {
		case MonitoredTurnFailed:
			return m.editFlight(flightID, func(f *Flight) error {
				for j := range f.Monitor.Turns {
					if f.Monitor.Turns[j].ID != itemID {
						continue
					}
					f.Monitor.Turns[j].Status = MonitoredTurnPending
					f.Monitor.Turns[j].UpdatedAt = m.deps.Now()
					return nil
				}
				return nil
			})
		case MonitoredTurnPending, MonitoredTurnClassified, MonitoredTurnSkipped:
			return conflict("monitor: turn %s is %s; only failed turns can be retried", itemID, t.Status)
		}
	}
	for _, msg := range f.Monitor.Messages {
		if msg.ID != itemID {
			continue
		}
		switch msg.Status {
		case MessageStatusFailed:
			return m.editFlight(flightID, func(f *Flight) error {
				for j := range f.Monitor.Messages {
					if f.Monitor.Messages[j].ID != itemID {
						continue
					}
					f.Monitor.Messages[j].Status = MessageStatusPending
					f.Monitor.Messages[j].Error = ""
					f.Monitor.Messages[j].UpdatedAt = m.deps.Now()
					return nil
				}
				return nil
			})
		case MessageStatusPending, MessageStatusSending, MessageStatusQueued, MessageStatusAccepted:
			return conflict("monitor: message %s is %s; only failed deliveries can be retried", itemID, msg.Status)
		case MessageStatusUnknown:
			return conflict("monitor: message %s has unknown delivery; inspect it rather than retrying", itemID)
		}
	}
	return fmt.Errorf("%w: monitor item %s on flight %s", ErrNotFound, itemID, flightID)
}

func monitorSessionErrorPrefix(sessionID string) string {
	return "monitor: session " + sessionID + ": "
}

// recordMonitorSessionError surfaces an ingestion failure for one assigned
// session (runtime unavailable, subscription failed) on the flight for
// inspection. An identical repeated error does not rewrite flights.json or
// broadcast; the worker reports it again at every recovery tick.
func (m *Manager) recordMonitorSessionError(flightID, sessionID string, err error) error {
	msg := monitorSessionErrorPrefix(sessionID) + err.Error()
	if f, ok := m.Get(flightID); ok && f.Monitor != nil && f.Monitor.Error == msg {
		return nil
	}
	return m.editFlight(flightID, func(f *Flight) error {
		if f.Monitor == nil {
			f.Monitor = &FlightMonitor{}
		}
		f.Monitor.Error = msg
		return nil
	})
}

// clearMonitorSessionError removes the retained error once the named
// session's subscription succeeds. An error recorded for a different session
// stays visible.
func (m *Manager) clearMonitorSessionError(flightID, sessionID string) error {
	prefix := monitorSessionErrorPrefix(sessionID)
	if f, ok := m.Get(flightID); !ok || f.Monitor == nil || !strings.HasPrefix(f.Monitor.Error, prefix) {
		return nil
	}
	return m.editFlight(flightID, func(f *Flight) error {
		if f.Monitor != nil && strings.HasPrefix(f.Monitor.Error, prefix) {
			f.Monitor.Error = ""
		}
		return nil
	})
}

// monitorTurnID is the canonical work key the manager deduplicates by.
// Centralized so tests and the live worker share one definition.
func monitorTurnID(t PlannerTurn) string {
	return fmt.Sprintf("turn-%s-%d", sanitizeMonitorID(t.SessionID), t.EndSeq)
}

// sanitizeMonitorID strips characters that would be unsafe in a log line or
// a flight-internal id. Session ids are already uuid-shaped in practice, but
// a future harness might use a different shape.
func sanitizeMonitorID(id string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, id)
}
