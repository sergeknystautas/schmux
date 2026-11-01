// Package flighthistory owns the append-only local flight evidence ledgers and
// the versioned per-flight archive. It is deliberately independent of
// internal/flights so the flights manager can write to it without an import
// cycle.
package flighthistory

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SchemaVersion is the version stamped on every ledger event. Version 1
// copied the complete flight row into every event; version 2 records the
// row's digest and workflow state instead, so an event's size does not grow
// with the flight's monitor history.
const SchemaVersion = 2

type Change string

const (
	ChangeCreated   Change = "flight_created"
	ChangeUpdated   Change = "flight_updated"
	ChangeRecovered Change = "flight_recovered"
	ChangeRemoved   Change = "flight_removed"
	ChangeBaseline  Change = "baseline_imported"
)

type Actor string

const (
	ActorUser    Actor = "user"
	ActorPilot   Actor = "pilot"
	ActorMonitor Actor = "monitor"
	ActorSystem  Actor = "system"
)

// Event is one changed flight row recorded after a snapshot write. It names
// the row and says what state it was in; it never carries the row itself.
// The row's content lives in flights.json while the flight exists and in the
// per-flight archive after disposal.
type Event struct {
	SchemaVersion  int       `json:"schema_version"`
	EventID        string    `json:"event_id"`
	RecordedAt     time.Time `json:"recorded_at"`
	Change         Change    `json:"change"`
	Actor          Actor     `json:"actor"`
	Cause          string    `json:"cause"`
	CorrelationID  string    `json:"correlation_id,omitempty"`
	SourceTurnID   string    `json:"source_turn_id,omitempty"`
	SnapshotSHA256 string    `json:"snapshot_sha256"`
	FlightID       string    `json:"flight_id"`
	// RowSHA256 is the digest of the complete row after this change, as the
	// flights manager marshals it for history. Startup compares it with the
	// current row to find a snapshot write whose event never landed.
	RowSHA256 string `json:"row_sha256"`
	// State is the workflow position the row held after this change: the
	// fields the review derives timing and attribution from.
	State RowState `json:"state"`
}

// RowState is the bounded projection of a flight row that each event records.
// It is a few hundred bytes regardless of how many monitor turns, messages,
// or operations the row has accumulated. The counts say how much of each
// collection the row held, so a reader can tell which collection a change
// touched without the ledger storing the collection.
type RowState struct {
	WorkflowState      string     `json:"workflow_state"`
	CleanupState       string     `json:"cleanup_state,omitempty"`
	PhaseVisit         uint64     `json:"phase_visit,omitempty"`
	PhaseStartedAt     *time.Time `json:"phase_started_at,omitempty"`
	PhaseTimeUncertain bool       `json:"phase_time_uncertain,omitempty"`
	PlannerModel       string     `json:"planner_model,omitempty"`
	ExecutorModel      string     `json:"executor_model,omitempty"`
	WorkspaceID        string     `json:"workspace_id,omitempty"`
	Operations         int        `json:"operations,omitempty"`
	MonitorTurns       int        `json:"monitor_turns,omitempty"`
	MonitorMessages    int        `json:"monitor_messages,omitempty"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// MalformedLine is a ledger line that did not decode. It is reported, never
// repaired or dropped from the file.
type MalformedLine struct {
	Line  int    `json:"line"`
	Bytes []byte `json:"bytes"`
	Error string `json:"error"`
}

// Ledger appends events to one JSONL file.
type Ledger struct {
	path  string
	now   func() time.Time
	newID func() string

	mu sync.Mutex
}

// NewLedger returns a ledger for path. It does not touch the filesystem until
// the first append, so inspecting a read-only installation writes nothing.
func NewLedger(path string, now func() time.Time, newID func() string) *Ledger {
	return &Ledger{path: path, now: now, newID: newID}
}

// Append writes each event as one complete JSON line and syncs the file.
// Events missing a schema version, ID, or timestamp receive them here.
func (l *Ledger) Append(events []Event) error {
	if len(events) == 0 {
		return nil
	}
	var buf []byte
	for _, ev := range events {
		if ev.SchemaVersion == 0 {
			ev.SchemaVersion = SchemaVersion
		}
		if ev.EventID == "" {
			ev.EventID = l.newID()
		}
		if ev.RecordedAt.IsZero() {
			ev.RecordedAt = l.now().UTC()
		}
		line, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("marshal flight event %s: %w", ev.EventID, err)
		}
		buf = append(append(buf, line...), '\n')
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return appendLines(l.path, buf)
}

// appendLines appends complete lines to path (mode 0600, parent 0700) and
// syncs. Shared by every append-only ledger in this package.
func appendLines(path string, lines []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	if endsMidLine(path) {
		// A torn trailing line stays as evidence; the new events start on a
		// fresh line instead of being glued to it.
		lines = append([]byte{'\n'}, lines...)
	}
	if _, err := f.Write(lines); err != nil {
		f.Close()
		return fmt.Errorf("append %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync %s: %w", path, err)
	}
	return f.Close()
}

// ReadEvents decodes every valid event in order. Lines that fail to decode,
// or that carry another schema version, are returned with their 1-based line
// number and original bytes. The file is never modified. A missing file is an
// empty ledger.
func ReadEvents(path string) ([]Event, []MalformedLine, error) {
	var events []Event
	malformed, err := readLines(path, func(line []byte) error {
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			return err
		}
		if ev.SchemaVersion != SchemaVersion {
			return fmt.Errorf("schema_version %d is not %d", ev.SchemaVersion, SchemaVersion)
		}
		events = append(events, ev)
		return nil
	})
	return events, malformed, err
}

// readLines feeds each line to decode and collects the ones decode rejects.
func readLines(path string, decode func(line []byte) error) ([]MalformedLine, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var malformed []MalformedLine
	r := bufio.NewReader(f)
	for n := 1; ; n++ {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if derr := decode(line); derr != nil {
				malformed = append(malformed, MalformedLine{Line: n, Bytes: line, Error: derr.Error()})
			}
		}
		if errors.Is(err, io.EOF) {
			return malformed, nil
		}
		if err != nil {
			return malformed, fmt.Errorf("read %s: %w", path, err)
		}
	}
}

// endsMidLine reports whether path is non-empty and does not end in a newline.
func endsMidLine(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return false
	}
	var last [1]byte
	if _, err := f.ReadAt(last[:], info.Size()-1); err != nil {
		return false
	}
	return last[0] != '\n'
}

// AppendJSONLines marshals each value onto its own line and appends them to
// path with the same guarantees as the ledgers: mode 0600, directories 0700,
// a synced write, and a torn trailing line left intact rather than extended.
// Other packages use it for the append-only files they own.
func AppendJSONLines(path string, values []any) error {
	var buf []byte
	for _, v := range values {
		line, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("marshal record for %s: %w", path, err)
		}
		buf = append(append(buf, line...), '\n')
	}
	if len(buf) == 0 {
		return nil
	}
	return appendLines(path, buf)
}

// ReadJSONLines feeds each line of path to decode and returns the lines it
// rejected, with their 1-based numbers. The file is never modified and a
// missing file is empty.
func ReadJSONLines(path string, decode func(line []byte) error) ([]MalformedLine, error) {
	return readLines(path, decode)
}
