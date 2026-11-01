package flightreview

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sergeknystautas/schmux/internal/flighthistory"
	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/state"
)

// workspaceSignature is the part of a workspace's state that reconciliation
// compares between observations.
type workspaceSignature struct {
	Branch       string
	Dirty        bool
	Ahead        int
	Behind       int
	FilesChanged int
}

// reconcileState is what one daemon lifetime remembers between passes. It
// suppresses duplicate observations; the ledger on disk is the record.
type reconcileState struct {
	mu     sync.Mutex
	seeded bool
	// missing holds the keys of resources recorded missing. A missing resource
	// never comes back: workspace numbers are reused after disposal, so an id
	// seen again belongs to a replacement the flight never owned.
	missing map[string]bool
	// sessions maps flightID|assignmentID to the session last seen there.
	sessions map[string]string
	// workspaces and signatureAt are per flight: the signature last seen and
	// when that signature was first seen. Session output after that time
	// explains a later change. The previous pass is not the baseline: output
	// triggers a broadcast, and so a pass, before git status reflects the edit.
	workspaces  map[string]workspaceSignature
	signatureAt map[string]time.Time
	writer      *flighthistory.ReconciliationWriter
}

func newReconcileState(now func() time.Time) *reconcileState {
	return &reconcileState{
		missing:     map[string]bool{},
		sessions:    map[string]string{},
		workspaces:  map[string]workspaceSignature{},
		signatureAt: map[string]time.Time{},
		writer:      flighthistory.NewReconciliationWriter(flighthistory.ReconciliationPath(), now, func() string { return randomID("re-") }),
	}
}

func missingKey(flightID string, kind flighthistory.ReconciliationKind, resourceKind, resourceID string) string {
	return flightID + "|" + string(kind) + "|" + resourceKind + "|" + resourceID
}

// Reconcile compares every active flight's recorded workspace and sessions with
// the resources that exist now and appends what differs to the reconciliation
// ledger. It reports whether anything was appended. It records when a
// difference was noticed, never when it happened, and it never broadcasts: the
// dashboard calls it just before a broadcast it is already making.
func (s *Service) Reconcile(manager *flights.Manager) (bool, error) {
	r := s.reconcile
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.seeded {
		prior, _, err := flighthistory.ReadReconciliation(flighthistory.ReconciliationPath())
		if err != nil {
			return false, fmt.Errorf("read reconciliation ledger: %w", err)
		}
		for _, ev := range prior {
			if ev.Kind == flighthistory.ReconcileResourceMissing {
				r.missing[missingKey(ev.FlightID, ev.Kind, ev.ResourceKind, ev.ResourceID)] = true
			}
		}
		r.seeded = true
	}

	now := s.opts.Now().UTC()
	var events []flighthistory.ReconciliationEvent
	var newlyMissing []string
	// Observations become the baseline only once their events are on disk, so
	// a failed append is noticed again on the next pass.
	nextSessions := map[string]string{}
	nextSignatures := map[string]workspaceSignature{}
	nextSignatureAt := map[string]time.Time{}
	// observe records a resource the first time it is missing and reports
	// whether the flight still holds it. Once recorded missing it stays
	// missing, so a reused id is neither this flight's resource nor a second
	// loss.
	observe := func(flightID, resourceKind, resourceID string, exists bool, lastKnown any) bool {
		key := missingKey(flightID, flighthistory.ReconcileResourceMissing, resourceKind, resourceID)
		if r.missing[key] {
			return false
		}
		if exists {
			return true
		}
		events = append(events, flighthistory.ReconciliationEvent{
			RecordedAt: now, Kind: flighthistory.ReconcileResourceMissing, FlightID: flightID,
			ResourceKind: resourceKind, ResourceID: resourceID, LastKnown: mustJSON(lastKnown), Source: "unknown",
		})
		newlyMissing = append(newlyMissing, key)
		return false
	}

	for _, flight := range manager.List() {
		// A backlog flight owns nothing, and a disposing flight is mid-cleanup:
		// its resources vanish on purpose.
		if flight.WorkflowState == flights.StateBacklog || flight.CleanupState == flights.CleanupDisposing {
			continue
		}
		ws := flight.Workspace
		if ws == nil || ws.WorkspaceID == "" || ws.Outcome != flights.StatusSucceeded {
			continue
		}
		workspace, workspaceFound := s.opts.State.GetWorkspace(ws.WorkspaceID)
		ownsWorkspace := observe(flight.ID, "workspace", ws.WorkspaceID, workspaceFound, ws)

		var activeSessions []state.Session
		for _, assignment := range flight.Sessions {
			if assignment.SessionID == "" || assignment.Outcome != flights.StatusSucceeded {
				continue
			}
			slot := flight.ID + "|" + assignment.ID
			if prev := r.sessions[slot]; prev != "" && prev != assignment.SessionID {
				events = append(events, flighthistory.ReconciliationEvent{
					RecordedAt: now, Kind: flighthistory.ReconcileResourceReplaced, FlightID: flight.ID,
					ResourceKind: "session", ResourceID: assignment.SessionID,
					LastKnown: mustJSON(map[string]string{
						"role": string(assignment.Role), "assignment_id": assignment.ID, "previous_session_id": prev,
					}),
					Source: "flight_assignment",
				})
			}
			nextSessions[slot] = assignment.SessionID
			session, sessionFound := s.opts.State.GetSession(assignment.SessionID)
			if observe(flight.ID, "session", assignment.SessionID, sessionFound, assignment) {
				activeSessions = append(activeSessions, session)
			}
		}

		if ownsWorkspace {
			sig := workspaceSignature{workspace.Branch, workspace.Dirty, workspace.Ahead, workspace.Behind, workspace.FilesChanged}
			prev, seen := r.workspaces[flight.ID]
			if seen && prev != sig && !anyOutputSince(activeSessions, r.signatureAt[flight.ID]) {
				events = append(events, flighthistory.ReconciliationEvent{
					RecordedAt: now, Kind: flighthistory.ReconcileWorkspaceChanged, FlightID: flight.ID,
					ResourceKind: "workspace", ResourceID: ws.WorkspaceID,
					LastKnown: mustJSON(map[string]any{"previous": prev, "current": sig}),
					Source:    "workspace_state",
				})
			}
			if !seen || prev != sig {
				nextSignatureAt[flight.ID] = now
			}
			nextSignatures[flight.ID] = sig
		}
	}

	if err := r.writer.Append(events); err != nil {
		return false, fmt.Errorf("append reconciliation events: %w", err)
	}
	for _, key := range newlyMissing {
		r.missing[key] = true
	}
	for slot, id := range nextSessions {
		r.sessions[slot] = id
	}
	for id, sig := range nextSignatures {
		r.workspaces[id] = sig
	}
	for id, at := range nextSignatureAt {
		r.signatureAt[id] = at
	}
	return len(events) > 0, nil
}

// anyOutputSince reports whether an assigned session produced output after
// the current workspace signature was first seen, which would explain a
// change away from it.
func anyOutputSince(sessions []state.Session, since time.Time) bool {
	for _, session := range sessions {
		if session.LastOutputAt.After(since) {
			return true
		}
	}
	return false
}

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(fmt.Sprintf("%q", err.Error()))
	}
	return data
}
