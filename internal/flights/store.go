package flights

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/sergeknystautas/schmux/internal/fileutil"
)

// loadSnapshot reads and validates the flights file. A missing or zero-byte
// file is an empty snapshot. Any other problem is an error and the file is
// left untouched.
func loadSnapshot(path string) (snapshot, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return snapshot{Flights: []Flight{}}, nil
	}
	if err != nil {
		return snapshot{}, fmt.Errorf("flights: read %s: %w", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return snapshot{Flights: []Flight{}}, nil
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	var snap snapshot
	if err := dec.Decode(&snap); err != nil {
		return snapshot{}, fmt.Errorf("flights: decode %s: %w", path, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return snapshot{}, fmt.Errorf("flights: decode %s: unexpected data after snapshot", path)
	}
	if err := validateSnapshot(snap); err != nil {
		return snapshot{}, fmt.Errorf("flights: invalid %s: %w", path, err)
	}
	if snap.Flights == nil {
		snap.Flights = []Flight{}
	}
	return snap, nil
}

// writeSnapshot persists the snapshot atomically with owner-only permissions.
func writeSnapshot(path string, snap snapshot) error {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("flights: encode snapshot: %w", err)
	}
	if err := fileutil.AtomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("flights: write %s: %w", path, err)
	}
	return nil
}

func validateSnapshot(snap snapshot) error {
	flightIDs := map[string]bool{}
	assignmentIDs := map[string]bool{}
	operationIDs := map[string]bool{}
	backlogOrders := map[int]string{}

	for _, f := range snap.Flights {
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
