package flights

import (
	"context"
	"fmt"
	"time"

	"github.com/sergeknystautas/schmux/internal/disposal"
)

// collectFlightSessionIDs returns the current and prior session IDs from a
// flight, deduplicated and in stable order. Pass them to the disposal service
// so dispose only removes chat conversations that belong to this flight rather
// than every chat directory under its workspace ID.
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
}

// DisposeConfirmation is the explicit confirmation dispose requires: the
// workspace the caller believes will be destroyed, and a positive confirmation.
type DisposeConfirmation struct {
	WorkspaceID string
	Confirmed   bool
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

// Dispose destroys everything a started flight owns -- every session in its
// workspace, this flight's chat conversation records, and the workspace
// itself, force-purged -- and then removes the flight record. It is
// destructive and requires confirmation.
//
// A flight with no workspace assignment (today, a backlog flight or one whose
// recorded workspace was disposed out from under it) owns nothing to clean:
// the record is removed at once. A confirmation is still required, and the
// workspace_id in the request must be empty for that case.
//
// For a flight that owns a workspace, the disposing state and a pending
// operation are persisted before any destructive work, and every resource
// outcome is persisted as it arrives. A partial failure leaves the record in
// the disposing state with the failed operation, and repeating Dispose
// retries what is left: every step treats an already-absent resource as
// cleaned. The record is removed only after all cleanup succeeds.
func (m *Manager) Dispose(ctx context.Context, flightID string, conf DisposeConfirmation) (DisposeResult, error) {
	unlock := m.lockFlight(flightID)
	defer unlock()

	opID := randomToken("op-")
	var final Flight
	var workspaceless bool
	err := m.edit(func(flights *[]Flight) error {
		for i := range *flights {
			f := &(*flights)[i]
			if f.ID != flightID {
				continue
			}
			if f.Workspace == nil {
				if !conf.Confirmed {
					return conflict("disposing flight %s removes its record and requires confirmation", f.ID)
				}
				if conf.WorkspaceID != "" {
					return conflict("flight %s owns no workspace, not %q", f.ID, conf.WorkspaceID)
				}
				final = f.clone()
				*flights = append((*flights)[:i], (*flights)[i+1:]...)
				workspaceless = true
				return nil
			}
			if !conf.Confirmed {
				return conflict("disposing flight %s destroys its workspace %s and requires confirmation", f.ID, f.Workspace.WorkspaceID)
			}
			if conf.WorkspaceID != f.Workspace.WorkspaceID {
				return conflict("flight %s owns workspace %q, not %q", f.ID, f.Workspace.WorkspaceID, conf.WorkspaceID)
			}
			now := m.deps.Now()
			f.CleanupState = CleanupDisposing
			f.Operations = append(f.Operations, Operation{
				ID: opID, Action: ActionDispose, CorrelationID: randomToken("corr-"), Status: StatusPending,
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
	if workspaceless {
		return DisposeResult{Flight: final, Removed: true}, nil
	}

	workspaceID := final.Workspace.WorkspaceID
	branchRepo := final.Repo
	branchName := final.Workspace.Branch
	chatSessionIDs := collectFlightSessionIDs(&final)

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
	cleanup, err := m.deps.Cleaner.DisposeAll(ctx, disposal.Request{
		WorkspaceID:    workspaceID,
		ForcePurge:     true,
		ChatSessionIDs: chatSessionIDs,
		BranchRepo:     branchRepo,
		BranchName:     branchName,
	}, observe)
	if err != nil {
		if _, finishErr := m.finish(flightID, opID, err, nil); finishErr != nil && finishErr != err {
			m.logger.Error("flights: failed to record cleanup failure", "flight", flightID, "err", finishErr)
		}
		return DisposeResult{Cleanup: cleanup}, err
	}

	err = m.edit(func(flights *[]Flight) error {
		for i := range *flights {
			f := &(*flights)[i]
			if f.ID != flightID {
				continue
			}
			now := m.deps.Now()
			op := f.operation(opID)
			op.Status = StatusSucceeded
			op.UpdatedAt = now
			f.UpdatedAt = now
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
