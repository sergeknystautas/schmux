package flightreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/flighthistory"
	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

// Capture archives one flight's evidence: the complete row, every current and
// prior assigned session transcript, and the workspace's git state when the
// workspace is still inspectable. A transcript or workspace that cannot be
// read is recorded in the manifest, not treated as empty. final also writes
// final-flight.json for callers capturing a row that is already final.
func (s *Service) Capture(ctx context.Context, flight flights.Flight, final bool) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("collect evidence for flight %s: %w", flight.ID, err)
	}
	flightJSON, err := json.Marshal(flight)
	if err != nil {
		return fmt.Errorf("marshal flight %s: %w", flight.ID, err)
	}
	opts := flighthistory.ArchiveOptions{SchmuxVersion: s.opts.Version, Final: final, Now: s.opts.Now}

	var transcripts []flighthistory.TranscriptInput
	var gitJSON []byte
	if ws := flight.Workspace; ws != nil && ws.WorkspaceID != "" {
		transcripts = transcriptInputs(flight, ws.WorkspaceID)
		if s.opts.Workspaces != nil {
			gitState, gitErr := s.opts.Workspaces.FlightEvidence(ctx, ws.WorkspaceID, flight.Repo, ws.Branch)
			if gitErr != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return fmt.Errorf("collect evidence for flight %s: %w", flight.ID, ctxErr)
				}
				opts.Missing = append(opts.Missing, flighthistory.MissingSource{
					Kind: "workspace", ID: ws.WorkspaceID, Source: ws.Path, ObservedAt: s.opts.Now().UTC(), Error: gitErr.Error(),
				})
			}
			if gitJSON, err = json.Marshal(gitState); err != nil {
				return fmt.Errorf("marshal git state for flight %s: %w", flight.ID, err)
			}
		}
	}
	return flighthistory.ArchiveFlight(flight.ID, flightJSON, transcripts, gitJSON, opts)
}

// CaptureFinalRow records the completed row after destructive cleanup without
// trying to reread resources that cleanup has just removed.
func (s *Service) CaptureFinalRow(flight flights.Flight) error {
	flightJSON, err := json.Marshal(flight)
	if err != nil {
		return fmt.Errorf("marshal flight %s: %w", flight.ID, err)
	}
	opts := flighthistory.ArchiveOptions{SchmuxVersion: s.opts.Version, Final: true, Now: s.opts.Now}
	return flighthistory.ArchiveFlight(flight.ID, flightJSON, nil, nil, opts)
}

// transcriptInputs lists the conversation log of every session a flight ever
// assigned, current and prior, named by the role it served.
func transcriptInputs(flight flights.Flight, workspaceID string) []flighthistory.TranscriptInput {
	var inputs []flighthistory.TranscriptInput
	add := func(role flights.SessionRole, sessionID string) {
		if sessionID == "" {
			return
		}
		inputs = append(inputs, flighthistory.TranscriptInput{
			Role:      string(role),
			SessionID: sessionID,
			Source:    chat.ConversationPath(schmuxdir.ChatSessionDir(workspaceID, sessionID)),
		})
	}
	for _, assignment := range flight.Sessions {
		add(assignment.Role, assignment.SessionID)
		for _, prior := range assignment.PriorSessionIDs {
			add(assignment.Role, prior)
		}
	}
	return inputs
}

// ImportLegacy archives every current flight now, rather than waiting for
// disposal, so flights that predate the archive are preserved while their
// evidence still exists. A flight that fails does not stop the others; the
// errors are joined.
func (s *Service) ImportLegacy(ctx context.Context, manager *flights.Manager) error {
	var errs []error
	for _, flight := range manager.List() {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if err := s.Capture(ctx, flight, false); err != nil {
			s.opts.Logger.Error("legacy flight import failed", "flight", flight.ID, "err", err)
			errs = append(errs, fmt.Errorf("flight %s: %w", flight.ID, err))
		}
	}
	return errors.Join(errs...)
}
