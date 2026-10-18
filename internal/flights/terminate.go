package flights

import (
	"context"
	"fmt"
	"time"

	"github.com/sergeknystautas/schmux/internal/disposal"
)

// collectFlightSessionIDs returns the current and prior session IDs from a
// flight, deduplicated and in stable order. Pass them to the disposal service
// so terminate only removes chat conversations that belong to this flight
// rather than every chat directory under its workspace ID.
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

// TerminateConfirmation is the explicit confirmation terminate requires: the
// workspace the caller believes will be destroyed, and a positive confirmation.
type TerminateConfirmation struct {
	WorkspaceID string
	Confirmed   bool
}

// TerminateResult describes a completed termination.
type TerminateResult struct {
	// Flight is the record as it stood when it was removed, including the
	// terminate operation's resource outcomes.
	Flight  Flight
	Cleanup disposal.Result
	// Removed is true once the flight record has been deleted.
	Removed bool
}

// Terminate destroys everything a started flight owns -- every session in its
// workspace, this flight's chat conversation records, and the workspace
// itself, force-purged -- and then removes the flight record. It is
// destructive and requires confirmation.
//
// The terminating state and a pending operation are persisted before any
// destructive work, and every resource outcome is persisted as it arrives. A
// partial failure leaves the record in the terminating state with the failed
// operation, and repeating Terminate retries what is left: every step treats an
// already-absent resource as cleaned. The record is removed only after all
// cleanup succeeds.
func (m *Manager) Terminate(ctx context.Context, flightID string, conf TerminateConfirmation) (TerminateResult, error) {
	unlock := m.lockFlight(flightID)
	defer unlock()

	opID := randomToken("op-")
	var workspaceID string
	var branchRepo, branchName string
	var chatSessionIDs []string
	err := m.editFlight(flightID, func(f *Flight) error {
		if f.WorkflowState == StateBacklog {
			return conflict("flight %s is in the backlog and owns no resources; delete it instead", f.ID)
		}
		if f.Workspace == nil {
			return conflict("flight %s has no workspace; take off first", f.ID)
		}
		workspaceID = f.Workspace.WorkspaceID
		branchRepo = f.Repo
		branchName = f.Workspace.Branch
		chatSessionIDs = collectFlightSessionIDs(f)
		if !conf.Confirmed {
			return conflict("terminating flight %s destroys its workspace %s and requires confirmation", f.ID, workspaceID)
		}
		if conf.WorkspaceID != workspaceID {
			return conflict("flight %s owns workspace %q, not %q", f.ID, workspaceID, conf.WorkspaceID)
		}
		now := m.deps.Now()
		f.CleanupState = CleanupTerminating
		f.Operations = append(f.Operations, Operation{
			ID: opID, Action: ActionTerminate, CorrelationID: randomToken("corr-"), Status: StatusPending,
			Resource: "workspace", CreatedAt: now, UpdatedAt: now,
		})
		return nil
	})
	if err != nil {
		return TerminateResult{}, err
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
		return TerminateResult{Cleanup: cleanup}, err
	}

	var final Flight
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
		return TerminateResult{Cleanup: cleanup}, err
	}
	return TerminateResult{Flight: final, Cleanup: cleanup, Removed: true}, nil
}
