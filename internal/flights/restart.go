package flights

import (
	"errors"
	"strings"
)

// FollowSessionRestart re-points every assignment that names oldID to newID
// after the dashboard restarts a session in place: the old process is disposed
// and a new session is spawned seeded from the prior conversation record, so
// the new session continues the same conversation with the same record
// sequence numbers. The replaced ID joins PriorSessionIDs so disposal still
// deletes its conversation. Retained monitor evidence follows the new ID so
// the subscription cursor, pending deliveries, and pilot-decision validation
// continue instead of stalling on a session that no longer exists. A session
// no flight references is a no-op.
func (m *Manager) FollowSessionRestart(oldID, newID string) error {
	if oldID == "" || newID == "" || oldID == newID {
		return errors.New("flights: session restart requires distinct old and new session IDs")
	}
	var touched []string
	for _, f := range m.List() {
		for _, s := range f.Sessions {
			if s.SessionID == oldID {
				touched = append(touched, f.ID)
				break
			}
		}
	}
	for _, id := range touched {
		unlock := m.lockFlight(id)
		err := m.editFlight(id, func(f *Flight) error {
			now := m.deps.Now()
			for i := range f.Sessions {
				s := &f.Sessions[i]
				if s.SessionID != oldID {
					continue
				}
				s.PriorSessionIDs = append(s.PriorSessionIDs, oldID)
				s.SessionID = newID
				s.UpdatedAt = now
			}
			if f.Monitor == nil {
				return nil
			}
			// Turn IDs are derived from (session, end_seq) and the snapshot
			// validator enforces that; renaming the source renames the key.
			renamed := map[string]string{}
			for i := range f.Monitor.Turns {
				t := &f.Monitor.Turns[i]
				if t.Turn.SessionID != oldID {
					continue
				}
				t.Turn.SessionID = newID
				id := monitorTurnID(t.Turn)
				renamed[t.ID] = id
				t.ID = id
			}
			for i := range f.Monitor.Messages {
				msg := &f.Monitor.Messages[i]
				if id, ok := renamed[msg.SourceTurnID]; ok {
					msg.SourceTurnID = id
				}
				if msg.SenderSessionID == oldID {
					msg.SenderSessionID = newID
				}
				if msg.RecipientSessionID == oldID {
					msg.RecipientSessionID = newID
				}
			}
			// An operation and a stop name the pilot turn that decided them.
			// The turn was renamed with its session, so they follow it; the
			// visit and the start evidence they carry stay where they were.
			for i := range f.Operations {
				if id, ok := renamed[f.Operations[i].SourceTurnID]; ok {
					f.Operations[i].SourceTurnID = id
				}
			}
			if f.PilotStop != nil {
				if id, ok := renamed[f.PilotStop.SourceTurnID]; ok {
					f.PilotStop.SourceTurnID = id
				}
			}
			if strings.HasPrefix(f.Monitor.Error, monitorSessionErrorPrefix(oldID)) {
				f.Monitor.Error = ""
			}
			return nil
		})
		unlock()
		if err != nil {
			return err
		}
	}
	return nil
}
