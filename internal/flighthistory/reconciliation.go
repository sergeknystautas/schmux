package flighthistory

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type ReconciliationKind string

const (
	ReconcileResourceMissing  ReconciliationKind = "resource_missing"
	ReconcileResourceReplaced ReconciliationKind = "resource_replaced"
	ReconcileWorkspaceChanged ReconciliationKind = "workspace_changed_outside_flight"
)

// ReconciliationEvent is one observed difference between a flight's recorded
// assignments and the resources that actually exist. It records when the
// difference was noticed, not when it happened.
type ReconciliationEvent struct {
	SchemaVersion int                `json:"schema_version"`
	EventID       string             `json:"event_id"`
	RecordedAt    time.Time          `json:"recorded_at"`
	Kind          ReconciliationKind `json:"kind"`
	FlightID      string             `json:"flight_id"`
	ResourceKind  string             `json:"resource_kind"`
	ResourceID    string             `json:"resource_id"`
	LastKnown     json.RawMessage    `json:"last_known,omitempty"`
	Source        string             `json:"source"`
}

// ReconciliationWriter appends reconciliation events to one JSONL file.
type ReconciliationWriter struct {
	path  string
	now   func() time.Time
	newID func() string

	mu sync.Mutex
}

// NewReconciliationWriter returns a writer for path. The file is not created
// until the first append.
func NewReconciliationWriter(path string, now func() time.Time, newID func() string) *ReconciliationWriter {
	return &ReconciliationWriter{path: path, now: now, newID: newID}
}

// Append writes each event as one complete JSON line and syncs the file.
func (w *ReconciliationWriter) Append(events []ReconciliationEvent) error {
	if len(events) == 0 {
		return nil
	}
	var buf []byte
	for _, ev := range events {
		if ev.SchemaVersion == 0 {
			ev.SchemaVersion = SchemaVersion
		}
		if ev.EventID == "" {
			ev.EventID = w.newID()
		}
		if ev.RecordedAt.IsZero() {
			ev.RecordedAt = w.now().UTC()
		}
		line, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("marshal reconciliation event %s: %w", ev.EventID, err)
		}
		buf = append(append(buf, line...), '\n')
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return appendLines(w.path, buf)
}

// ReadReconciliation decodes every valid event in order and returns the lines
// that did not decode. The file is never modified; a missing file is empty.
func ReadReconciliation(path string) ([]ReconciliationEvent, []MalformedLine, error) {
	var events []ReconciliationEvent
	malformed, err := readLines(path, func(line []byte) error {
		var ev ReconciliationEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return err
		}
		events = append(events, ev)
		return nil
	})
	return events, malformed, err
}
