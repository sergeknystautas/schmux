package flights

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sergeknystautas/schmux/internal/fileutil"
	"github.com/sergeknystautas/schmux/internal/jsoncompat"
)

// readSnapshotFile reads the raw snapshot bytes. A missing file reports
// absent rather than empty so callers can tell the two apart.
func readSnapshotFile(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// parseSnapshot decodes snapshot bytes into flights, the unknown top-level
// fields, and the raw rows this version cannot decode.
func parseSnapshot(data []byte, path string) (snapshot, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return snapshot{Flights: []Flight{}}, nil
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	var top map[string]json.RawMessage
	if err := dec.Decode(&top); err != nil {
		return snapshot{}, fmt.Errorf("flights: decode %s: %w", path, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return snapshot{}, fmt.Errorf("flights: decode %s: unexpected data after snapshot", path)
	}
	var rawFlights []json.RawMessage
	if raw, exists := top["flights"]; exists && string(raw) != "null" {
		if err := json.Unmarshal(raw, &rawFlights); err != nil {
			return snapshot{}, fmt.Errorf("flights: decode %s: %w", path, err)
		}
	}
	delete(top, "flights")
	snap := decodeFlights(rawFlights)
	if len(top) > 0 {
		snap.ExtraFields = jsoncompat.Clone(top)
	}
	return snap, nil
}

func normalizeLegacyFlight(f *Flight) {
	if f.CleanupState == "terminating" {
		f.CleanupState = CleanupDisposing
	}
	for i := range f.Operations {
		if f.Operations[i].Action == "terminate" {
			f.Operations[i].Action = ActionDispose
		}
	}
}

func decodeFlights(rawFlights []json.RawMessage) snapshot {
	snap := snapshot{Flights: []Flight{}}
	flightIDs := map[string]bool{}
	assignmentIDs := map[string]bool{}
	operationIDs := map[string]bool{}
	backlogOrders := map[int]string{}

	for _, raw := range rawFlights {
		var f Flight
		if err := json.Unmarshal(raw, &f); err != nil {
			snap.InvalidFlights = append(snap.InvalidFlights, jsoncompat.CloneRaw(raw))
			continue
		}
		normalizeLegacyFlight(&f)
		trialFlights := cloneBoolMap(flightIDs)
		trialAssignments := cloneBoolMap(assignmentIDs)
		trialOperations := cloneBoolMap(operationIDs)
		trialOrders := cloneIntStringMap(backlogOrders)
		if err := validateFlight(f, trialFlights, trialAssignments, trialOperations, trialOrders); err != nil {
			snap.InvalidFlights = append(snap.InvalidFlights, jsoncompat.CloneRaw(raw))
			continue
		}
		flightIDs, assignmentIDs, operationIDs, backlogOrders = trialFlights, trialAssignments, trialOperations, trialOrders
		snap.Flights = append(snap.Flights, f)
	}
	return snap
}

func cloneBoolMap(source map[string]bool) map[string]bool {
	out := make(map[string]bool, len(source)+1)
	for key, value := range source {
		out[key] = value
	}
	return out
}

func cloneIntStringMap(source map[int]string) map[int]string {
	out := make(map[int]string, len(source)+1)
	for key, value := range source {
		out[key] = value
	}
	return out
}

func encodeSnapshot(snap snapshot) ([]byte, error) {
	rows := make([]json.RawMessage, 0, len(snap.Flights)+len(snap.InvalidFlights))
	for _, f := range snap.Flights {
		data, err := json.Marshal(f)
		if err != nil {
			return nil, fmt.Errorf("flights: encode snapshot: %w", err)
		}
		rows = append(rows, data)
	}
	rows = append(rows, snap.InvalidFlights...)
	flightsData, err := json.Marshal(rows)
	if err != nil {
		return nil, fmt.Errorf("flights: encode snapshot: %w", err)
	}
	fields := map[string]json.RawMessage{"flights": flightsData}
	for name, value := range snap.ExtraFields {
		if _, exists := fields[name]; !exists {
			fields[name] = value
		}
	}
	data, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("flights: encode snapshot: %w", err)
	}
	return data, nil
}

func writeSnapshotData(path string, data []byte) error {
	if err := fileutil.AtomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("flights: write %s: %w", path, err)
	}
	return nil
}

func validateFlight(f Flight, flightIDs, assignmentIDs, operationIDs map[string]bool, backlogOrders map[int]string) error {
	if f.ID == "" {
		return errors.New("flight with empty id")
	}
	if flightIDs[f.ID] {
		return fmt.Errorf("duplicate flight id %q", f.ID)
	}
	flightIDs[f.ID] = true
	if !f.WorkflowState.valid() {
		return fmt.Errorf("flight %s: invalid workflow state %q", f.ID, f.WorkflowState)
	}
	if !f.CleanupState.valid() {
		return fmt.Errorf("flight %s: invalid cleanup state %q", f.ID, f.CleanupState)
	}

	if f.WorkflowState == StateBacklog {
		if f.Order < 0 {
			return fmt.Errorf("flight %s: invalid backlog order %d", f.ID, f.Order)
		}
		if other, dup := backlogOrders[f.Order]; dup {
			return fmt.Errorf("flights %s and %s share backlog order %d", other, f.ID, f.Order)
		}
		backlogOrders[f.Order] = f.ID
	}

	if ws := f.Workspace; ws != nil {
		if err := claimID(assignmentIDs, ws.ID, "assignment"); err != nil {
			return fmt.Errorf("flight %s: %w", f.ID, err)
		}
		if !ws.Outcome.valid() {
			return fmt.Errorf("flight %s: invalid workspace assignment status %q", f.ID, ws.Outcome)
		}
	}
	for _, s := range f.Sessions {
		if err := claimID(assignmentIDs, s.ID, "assignment"); err != nil {
			return fmt.Errorf("flight %s: %w", f.ID, err)
		}
		if !s.Role.valid() {
			return fmt.Errorf("flight %s: invalid session role %q", f.ID, s.Role)
		}
		if !s.Outcome.valid() {
			return fmt.Errorf("flight %s: invalid session assignment status %q", f.ID, s.Outcome)
		}
	}
	for _, o := range f.Operations {
		if err := claimID(operationIDs, o.ID, "operation"); err != nil {
			return fmt.Errorf("flight %s: %w", f.ID, err)
		}
		if !o.Action.valid() {
			return fmt.Errorf("flight %s: invalid operation action %q", f.ID, o.Action)
		}
		if !o.Status.valid() {
			return fmt.Errorf("flight %s: invalid operation status %q", f.ID, o.Status)
		}
	}
	if err := validateFlightMonitor(f.Monitor); err != nil {
		return fmt.Errorf("flight %s: %w", f.ID, err)
	}
	if err := validatePilotStop(f); err != nil {
		return fmt.Errorf("flight %s: %w", f.ID, err)
	}
	return nil
}

// validatePilotStop checks the pilot's stop record: it names a real phase, a
// nonblank reason, and the turn and notification the decision answered.
func validatePilotStop(f Flight) error {
	stop := f.PilotStop
	if stop == nil {
		return nil
	}
	if !stop.WorkflowState.valid() {
		return fmt.Errorf("pilot stop has invalid workflow state %q", stop.WorkflowState)
	}
	if strings.TrimSpace(stop.Reason) == "" {
		return errors.New("pilot stop has a blank reason")
	}
	if stop.SourceTurnID == "" || stop.SourceNotificationID == "" {
		return errors.New("pilot stop has no source turn or notification")
	}
	if f.Monitor == nil {
		return errors.New("pilot stop has no monitor to reference")
	}
	turnFound := false
	for _, t := range f.Monitor.Turns {
		if t.ID == stop.SourceTurnID {
			turnFound = true
			break
		}
	}
	if !turnFound {
		return fmt.Errorf("pilot stop references missing turn %s", stop.SourceTurnID)
	}
	notificationFound := false
	for _, msg := range f.Monitor.Messages {
		if msg.ID == stop.SourceNotificationID && msg.Kind == "notification" {
			notificationFound = true
			break
		}
	}
	if !notificationFound {
		return fmt.Errorf("pilot stop references missing notification %s", stop.SourceNotificationID)
	}
	return nil
}

func validateFlightMonitor(m *FlightMonitor) error {
	if m == nil {
		return nil
	}
	turns, messages := map[string]bool{}, map[string]bool{}
	for _, t := range m.Turns {
		if err := claimID(turns, t.ID, "monitor turn"); err != nil {
			return err
		}
		if t.Turn.SessionID == "" || t.Turn.StartSeq == 0 || t.Turn.EndSeq < t.Turn.StartSeq || t.ID != monitorTurnID(t.Turn) {
			return fmt.Errorf("invalid monitor source for %s", t.ID)
		}
		switch t.Turn.Outcome {
		case "completed", "failed", "interrupted":
		default:
			return fmt.Errorf("invalid turn outcome %q", t.Turn.Outcome)
		}
		switch t.Status {
		case MonitoredTurnPending, MonitoredTurnClassified, MonitoredTurnSkipped, MonitoredTurnFailed:
		default:
			return fmt.Errorf("invalid monitor turn status %q", t.Status)
		}
	}
	for _, msg := range m.Messages {
		if err := claimID(messages, msg.ID, "monitor message"); err != nil {
			return err
		}
		if !msg.Status.valid() || msg.RecipientSessionID == "" {
			return fmt.Errorf("invalid monitor message %s", msg.ID)
		}
		switch msg.Kind {
		case "notification", "rejection":
			if !turns[msg.SourceTurnID] {
				return fmt.Errorf("message %s references missing turn %s", msg.ID, msg.SourceTurnID)
			}
		case "pilot_message":
			if msg.SenderSessionID == "" {
				return fmt.Errorf("pilot message %s has no sender", msg.ID)
			}
		case "execution_notice":
			// Recorded by Enter execution for the pilot; it has no source turn.
		case "review_notice":
			// Recorded by Enter review for the pilot; it has no source turn.
		case "next_reviewer":
			// The pilot's request for another reviewer; it names its source turn.
			if msg.SourceTurnID == "" || !turns[msg.SourceTurnID] {
				return fmt.Errorf("next reviewer %s references missing turn %s", msg.ID, msg.SourceTurnID)
			}
		case "pilot_stop":
			// The pilot's stop record; it names its source turn.
			if msg.SourceTurnID == "" || !turns[msg.SourceTurnID] {
				return fmt.Errorf("pilot stop %s references missing turn %s", msg.ID, msg.SourceTurnID)
			}
		default:
			return fmt.Errorf("invalid message kind %q", msg.Kind)
		}
	}
	for _, msg := range m.Messages {
		if msg.SourceNotificationID != "" {
			found := false
			for _, source := range m.Messages {
				if source.ID == msg.SourceNotificationID && source.Kind == "notification" {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("message %s references missing notification %s", msg.ID, msg.SourceNotificationID)
			}
		}
	}
	return nil
}

// claimID records id in seen, failing on an empty or repeated id.
func claimID(seen map[string]bool, id, kind string) error {
	if id == "" {
		return fmt.Errorf("%s with empty id", kind)
	}
	if seen[id] {
		return fmt.Errorf("duplicate %s id %q", kind, id)
	}
	seen[id] = true
	return nil
}
