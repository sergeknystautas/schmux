package flightreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sergeknystautas/schmux/internal/fileutil"
	"github.com/sergeknystautas/schmux/internal/flighthistory"
	"github.com/sergeknystautas/schmux/internal/flights"
)

// ledgers holds both append-only ledgers, grouped by flight.
type ledgers struct {
	events         map[string][]flighthistory.Event
	reconciliation map[string][]flighthistory.ReconciliationEvent
	// malformed lines are reported by the review, never repaired.
	malformedEvents         []flighthistory.MalformedLine
	malformedReconciliation []flighthistory.MalformedLine
}

func readLedgers() (ledgers, error) {
	l := ledgers{
		events:         map[string][]flighthistory.Event{},
		reconciliation: map[string][]flighthistory.ReconciliationEvent{},
	}
	events, malformed, err := flighthistory.ReadEvents(flighthistory.EventsPath())
	if err != nil {
		return l, err
	}
	l.malformedEvents = malformed
	for _, e := range events {
		l.events[e.FlightID] = append(l.events[e.FlightID], e)
	}
	recon, malformed, err := flighthistory.ReadReconciliation(flighthistory.ReconciliationPath())
	if err != nil {
		return l, err
	}
	l.malformedReconciliation = malformed
	for _, e := range recon {
		l.reconciliation[e.FlightID] = append(l.reconciliation[e.FlightID], e)
	}
	return l, nil
}

// archivedFlightIDs lists every flight directory in the history root.
func archivedFlightIDs() ([]string, error) {
	entries, err := os.ReadDir(flighthistory.Root())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && flighthistory.ValidateID(e.Name()) == nil {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func readJSONFile(path string, v any) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return true, fmt.Errorf("%s: %w", path, err)
	}
	return true, nil
}

// rowFromEvent is the partial row a ledger event proves: its identity and
// the workflow state it recorded. Nothing else is filled in.
func rowFromEvent(e flighthistory.Event) flights.Flight {
	f := flights.Flight{
		ID:                 e.FlightID,
		WorkflowState:      flights.WorkflowState(e.State.WorkflowState),
		CleanupState:       flights.CleanupState(e.State.CleanupState),
		PlannerModel:       e.State.PlannerModel,
		ExecutorModel:      e.State.ExecutorModel,
		PhaseVisit:         e.State.PhaseVisit,
		PhaseTimeUncertain: e.State.PhaseTimeUncertain,
		UpdatedAt:          e.State.UpdatedAt,
	}
	if e.State.PhaseStartedAt != nil {
		f.PhaseStartedAt = *e.State.PhaseStartedAt
	}
	if e.State.WorkspaceID != "" {
		f.Workspace = &flights.WorkspaceAssignment{WorkspaceID: e.State.WorkspaceID}
	}
	return f
}

// archivedFlightRow returns the final row of a disposed flight, or the last
// row the archive captured of a live one.
func archivedFlightRow(dir string) (flights.Flight, bool, error) {
	var row flights.Flight
	for _, name := range []string{"final-flight.json", "flight.json"} {
		found, err := readJSONFile(filepath.Join(dir, name), &row)
		if err != nil {
			return flights.Flight{}, false, err
		}
		if found {
			return row, true, nil
		}
	}
	return flights.Flight{}, false, nil
}

// loadEvidence reads one flight's archive and ledger entries. live is the
// manager's current row for the flight, preferred over the archived snapshot.
func loadEvidence(flightID string, l ledgers, live *flights.Flight) (Evidence, error) {
	dir, err := flighthistory.FlightDir(flightID)
	if err != nil {
		return Evidence{}, err
	}
	ev := Evidence{Events: l.events[flightID], Reconciliation: l.reconciliation[flightID]}

	row, found, err := archivedFlightRow(dir)
	if err != nil {
		return Evidence{}, err
	}
	switch {
	case live != nil:
		ev.Flight = *live
	case found:
		ev.Flight = row
	case len(ev.Events) > 0:
		// No row survives: the ledger names only what it recorded. The
		// summary derives from these fields and reports the rest as missing.
		ev.Flight = rowFromEvent(ev.Events[len(ev.Events)-1])
	}

	var manifest flighthistory.Manifest
	if found, err := readJSONFile(filepath.Join(dir, "manifest.json"), &manifest); err != nil {
		return Evidence{}, err
	} else if found {
		ev.Manifest = &manifest
		names := make([]string, 0, len(manifest.Transcripts))
		for name := range manifest.Transcripts {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			base := strings.TrimSuffix(name, ".jsonl")
			role, session, ok := strings.Cut(base, "-")
			if !ok {
				return Evidence{}, fmt.Errorf("flight %s: transcript %q does not name a role and session", flightID, name)
			}
			tr, err := ReadTranscript(filepath.Join(dir, "transcripts", name), role, session)
			if err != nil {
				return Evidence{}, err
			}
			ev.Transcripts = append(ev.Transcripts, tr)
		}
	}
	attempts, _, err := readClassifications(flightID)
	if err != nil {
		return Evidence{}, err
	}
	ev.Classifications = attempts
	var git GitState
	if found, err := readJSONFile(filepath.Join(dir, "git-state.json"), &git); err != nil {
		return Evidence{}, err
	} else if found {
		ev.GitState = &git
	}
	return ev, nil
}

// RebuildSummaries recomputes summary.json for every archived flight and every
// flight the manager holds. Each summary is derived only from that flight's
// ledger entries, archive, and row, and carries no wall-clock field, so
// identical evidence rewrites identical bytes. A flight that cannot be read is
// reported and does not stop the others.
func (s *Service) RebuildSummaries(ctx context.Context, manager *flights.Manager) error {
	l, err := readLedgers()
	if err != nil {
		return fmt.Errorf("read ledgers: %w", err)
	}
	live := map[string]flights.Flight{}
	for _, f := range manager.List() {
		live[f.ID] = f
	}
	ids, err := archivedFlightIDs()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	for id := range live {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	var errs []error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if err := s.rebuildOne(id, l, live); err != nil {
			errs = append(errs, fmt.Errorf("flight %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) rebuildOne(id string, l ledgers, live map[string]flights.Flight) error {
	var row *flights.Flight
	if f, ok := live[id]; ok {
		row = &f
	}
	ev, err := loadEvidence(id, l, row)
	if err != nil {
		return err
	}
	dir, err := flighthistory.FlightDir(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(DeriveSummary(ev), "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(filepath.Join(dir, "summary.json"), append(data, '\n'), 0o600)
}
