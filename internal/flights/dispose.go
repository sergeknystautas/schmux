package flights

import (
	"context"
	"fmt"
	"time"

	"github.com/sergeknystautas/schmux/internal/disposal"
	"github.com/sergeknystautas/schmux/internal/flighthistory"
)

// collectFlightSessionIDs returns the current and prior session IDs from a
// flight, deduplicated and in stable order. Dispose closes exactly these
// sessions, by ID.
func collectFlightSessionIDs(f *Flight) []string {
	if len(f.Sessions) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, len(f.Sessions))
	for _, s := range f.Sessions {
		if s.SessionID != "" {
			if _, ok := seen[s.SessionID]; !ok {
				seen[s.SessionID] = struct{}{}
				out = append(out, s.SessionID)
			}
		}
		for _, p := range s.PriorSessionIDs {
			if p == "" {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out
}

// DestructiveCleaner is the shared full-workspace disposal service.
type DestructiveCleaner interface {
	DisposeAll(ctx context.Context, req disposal.Request, observe func(disposal.ResourceOutcome)) (disposal.Result, error)
	DisposeSessions(ctx context.Context, sessionIDs []string, observe func(disposal.ResourceOutcome)) ([]string, error)
}

// DisposeConfirmation is the explicit confirmation dispose requires.
type DisposeConfirmation struct {
	Confirmed bool
}

// DisposeResult describes a completed disposal.
type DisposeResult struct {
	// Flight is the record as it stood when it was removed, including the
	// dispose operation's resource outcomes.
	Flight  Flight
	Cleanup disposal.Result
	// Removed is true once the flight record has been deleted.
	Removed bool
}

// Dispose destroys the resources a flight owns and then removes the flight
// record. It is destructive and requires confirmation.
//
// The flight owns exactly two things: the sessions it recorded, by session ID,
// and the workspace that carries its owner_flight_id stamp. The workspace is
// found by that stamp alone -- never by the workspace ID the flight recorded
// at takeoff, which may since have been freed and reallocated to unrelated
// work. A flight whose workspace carries no stamp skips the workspace step.
//
// The disposing state and a pending operation are persisted before any
// destructive work, the flight's evidence is archived before the first
// destructive step, and every resource outcome is persisted as it arrives. A
// partial failure leaves the record in the disposing state with the failed
// operation, and repeating Dispose retries what is left: every step treats an
// already-absent resource as cleaned. The record is removed only after all
// cleanup succeeds and its final row is archived.
//
// A stamped workspace with unpushed commits refuses the whole dispose before
// any write. A dirty tree does not: uncommitted file changes are purged with
// the workspace.
func (m *Manager) Dispose(ctx context.Context, flightID string, conf DisposeConfirmation) (DisposeResult, error) {
	unlock := m.lockFlight(flightID)
	defer unlock()

	// Step 0: the workspace the flight owns must hold no unpushed commits.
	// This runs before the disposing mark, so a refused dispose changes
	// nothing at all.
	if _, ok := m.Get(flightID); !ok {
		return DisposeResult{}, fmt.Errorf("%w: %s", ErrNotFound, flightID)
	}
	if !conf.Confirmed {
		return DisposeResult{}, conflict("disposing flight %s requires confirmation", flightID)
	}
	if owned, hasOwned := m.deps.Workspaces.OwnedWorkspace(flightID); hasOwned {
		n, err := m.deps.Workspaces.UnpushedCommits(ctx, owned.ID)
		if err != nil {
			return DisposeResult{}, fmt.Errorf("flights: verify workspace %s before disposal: %w", owned.ID, err)
		}
		if n > 0 {
			return DisposeResult{}, conflict("disposing flight %s would destroy workspace %s with %d unpushed commit(s); push or merge them first", flightID, owned.ID, n)
		}
	}

	opID := randomToken("op-")
	corrID := randomToken("corr-")
	startAudit := AuditContext{Actor: flighthistory.ActorUser, Cause: "dispose_started", CorrelationID: corrID}
	var final Flight
	err := m.editAs(startAudit, func(flights *[]Flight) error {
		for i := range *flights {
			f := &(*flights)[i]
			if f.ID != flightID {
				continue
			}
			if !conf.Confirmed {
				return conflict("disposing flight %s requires confirmation", f.ID)
			}
			now := m.deps.Now()
			f.CleanupState = CleanupDisposing
			f.Operations = append(f.Operations, Operation{
				ID: opID, Action: ActionDispose, CorrelationID: corrID, Status: StatusPending,
				Resource: "workspace", CreatedAt: now, UpdatedAt: now,
			})
			f.UpdatedAt = now
			final = f.clone()
			return nil
		}
		return fmt.Errorf("%w: %s", ErrNotFound, flightID)
	})
	if err != nil {
		return DisposeResult{}, err
	}

	// The sessions are destroyed by cleanup, so their evidence is archived
	// first. A failure leaves the flight disposing with its pending operation:
	// the user retries, and nothing has been destroyed.
	if err := m.captureEvidence(ctx, final); err != nil {
		return DisposeResult{}, err
	}

	// The observer persists through the manager's snapshot lock only. It must
	// not take the per-flight lock this call already holds.
	observe := func(o disposal.ResourceOutcome) {
		outcome := resourceOutcome(o.Kind, o.ID, o.Err)
		if _, err := m.progress(flightID, opID, func(f *Flight, op *Operation, now time.Time) {
			op.addResource(outcome, now)
		}); err != nil {
			m.logger.Error("flights: failed to persist cleanup outcome", "flight", flightID, "kind", o.Kind, "id", o.ID, "err", err)
		}
	}
	fail := func(cleanup disposal.Result, err error) (DisposeResult, error) {
		if _, finishErr := m.finish(flightID, opID, err, nil); finishErr != nil && finishErr != err {
			m.logger.Error("flights: failed to record cleanup failure", "flight", flightID, "err", finishErr)
		}
		return DisposeResult{Cleanup: cleanup}, err
	}

	// Step 1: the flight's own sessions, by ID.
	disposed, err := m.deps.Cleaner.DisposeSessions(ctx, collectFlightSessionIDs(&final), observe)
	if err != nil {
		return fail(disposal.Result{SessionsDisposed: disposed}, err)
	}

	// Step 2: the workspace that carries this flight's stamp, if any.
	var cleanup disposal.Result
	cleanup.SessionsDisposed = disposed
	owned, hasOwned := m.deps.Workspaces.OwnedWorkspace(flightID)
	if hasOwned {
		result, err := m.deps.Cleaner.DisposeAll(ctx, disposal.Request{
			WorkspaceID: owned.ID,
			ForcePurge:  true,
		}, observe)
		cleanup.SessionsDisposed = append(cleanup.SessionsDisposed, result.SessionsDisposed...)
		cleanup.WorkspacePurged = result.WorkspacePurged
		cleanup.WorkspaceMissing = result.WorkspaceMissing
		if err != nil {
			return fail(cleanup, err)
		}
	} else {
		observe(disposal.ResourceOutcome{Kind: "workspace"})
		m.logger.Info("flights: flight owns no workspace, skipping", "flight", flightID)
	}

	// Step 3: record the success, archive the final row, remove the record.
	successAudit := AuditContext{
		Actor: flighthistory.ActorUser, Cause: "dispose_succeeded", CorrelationID: corrID,
	}
	// editAs, not editFlightAs: the archived final row must be the row as
	// persisted, so UpdatedAt is set once here and the clone taken after it.
	err = m.editAs(successAudit, func(flights *[]Flight) error {
		for i := range *flights {
			f := &(*flights)[i]
			if f.ID != flightID {
				continue
			}
			op := f.operation(opID)
			if op == nil {
				return fmt.Errorf("%w: %s", ErrNotFound, flightID)
			}
			now := m.deps.Now()
			op.Status = StatusSucceeded
			op.UpdatedAt = now
			f.UpdatedAt = now
			final = f.clone()
			return nil
		}
		return fmt.Errorf("%w: %s", ErrNotFound, flightID)
	})
	if err != nil {
		return DisposeResult{Cleanup: cleanup}, err
	}
	if err := m.captureFinalRow(final); err != nil {
		return DisposeResult{Cleanup: cleanup}, err
	}

	removeAudit := AuditContext{
		Actor: flighthistory.ActorUser, Cause: "dispose_removed", CorrelationID: corrID,
	}
	err = m.editAs(removeAudit, func(flights *[]Flight) error {
		for i := range *flights {
			f := &(*flights)[i]
			if f.ID != flightID {
				continue
			}
			final = f.clone()
			*flights = append((*flights)[:i], (*flights)[i+1:]...)
			return nil
		}
		return fmt.Errorf("%w: %s", ErrNotFound, flightID)
	})
	if err != nil {
		return DisposeResult{Cleanup: cleanup}, err
	}
	return DisposeResult{Flight: final, Cleanup: cleanup, Removed: true}, nil
}

// captureEvidence archives the flight's evidence before cleanup destroys it.
// Without an archiver there is nothing to do.
func (m *Manager) captureEvidence(ctx context.Context, flight Flight) error {
	if m.deps.Evidence == nil {
		return nil
	}
	if err := m.deps.Evidence.Capture(ctx, flight, false); err != nil {
		return fmt.Errorf("flights: archive evidence for %s before disposal: %w", flight.ID, err)
	}
	return nil
}

func (m *Manager) captureFinalRow(flight Flight) error {
	if m.deps.Evidence == nil {
		return nil
	}
	if err := m.deps.Evidence.CaptureFinalRow(flight); err != nil {
		return fmt.Errorf("flights: archive final row for %s: %w", flight.ID, err)
	}
	return nil
}
