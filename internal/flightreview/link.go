package flightreview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

// FindUnlinkedEvidence scans the chat store for conversations that no flight
// has ever been assigned but that match one by workspace, branch, prompt text,
// or creation time. Matches are candidates for the user to confirm; nothing is
// attached to a flight.
func FindUnlinkedEvidence(ctx context.Context, manager *flights.Manager) ([]UnlinkedCandidate, error) {
	known := manager.List()
	archived, err := archivedFlightIDs()
	if err != nil {
		return nil, err
	}
	inManager := map[string]bool{}
	for _, f := range known {
		inManager[f.ID] = true
	}
	for _, id := range archived {
		if inManager[id] {
			continue
		}
		if row, ok, err := archivedFlightRow(rootDirFor(id)); err != nil {
			return nil, err
		} else if ok {
			known = append(known, row)
		}
	}

	assigned := map[string]bool{}
	for _, f := range known {
		for _, a := range f.Sessions {
			assigned[a.SessionID] = true
			for _, prior := range a.PriorSessionIDs {
				assigned[prior] = true
			}
		}
	}

	chatRoot := filepath.Join(schmuxdir.Get(), "chat")
	workspaces, err := os.ReadDir(chatRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []UnlinkedCandidate
	for _, ws := range workspaces {
		if !ws.IsDir() {
			continue
		}
		sessions, err := os.ReadDir(filepath.Join(chatRoot, ws.Name()))
		if err != nil {
			return nil, err
		}
		for _, sess := range sessions {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !sess.IsDir() || assigned[sess.Name()] {
				continue
			}
			path := chat.ConversationPath(schmuxdir.ChatSessionDir(ws.Name(), sess.Name()))
			tr, err := ReadTranscript(path, "", sess.Name())
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return nil, err
			}
			out = append(out, candidatesFor(ws.Name(), sess.Name(), tr, known)...)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].FlightID != out[j].FlightID {
			return out[i].FlightID < out[j].FlightID
		}
		return out[i].SessionID < out[j].SessionID
	})
	return out, nil
}

func rootDirFor(flightID string) string {
	return filepath.Join(schmuxdir.FlightHistoryDir(), flightID)
}

// candidatesFor matches one conversation against every known flight.
func candidatesFor(workspaceID, sessionID string, tr Transcript, known []flights.Flight) []UnlinkedCandidate {
	var first *TranscriptRecord
	var observed time.Time
	var allText strings.Builder
	for i := range tr.Records {
		rec := tr.Records[i]
		if t := parseTS(rec.Record.Ts); t != nil && (observed.IsZero() || t.Before(observed)) {
			observed = *t
		}
		if rec.Record.Type != chat.RecordUserMessage {
			continue
		}
		if first == nil {
			first = &tr.Records[i]
		}
		allText.WriteString(rec.Record.Text)
		allText.WriteByte('\n')
	}
	if first == nil {
		return nil
	}
	text := allText.String()
	var out []UnlinkedCandidate
	for _, f := range known {
		var kinds []string
		if f.Workspace != nil {
			if f.Workspace.WorkspaceID == workspaceID {
				kinds = append(kinds, "workspace_id")
			}
			if branch := f.Workspace.Branch; branch != "" && strings.Contains(text, branch) {
				kinds = append(kinds, "branch")
			}
		}
		if prompt := strings.TrimSpace(f.Prompt); prompt != "" && strings.Contains(text, prompt) {
			kinds = append(kinds, "prompt")
		}
		if !observed.IsZero() && !observed.Before(f.CreatedAt) && !observed.After(f.UpdatedAt) {
			kinds = append(kinds, "timing")
		}
		if len(kinds) == 0 {
			continue
		}
		out = append(out, UnlinkedCandidate{
			SessionID: sessionID, WorkspaceID: workspaceID, FlightID: f.ID, MatchKinds: kinds,
			TranscriptLine: first.Line, Excerpt: truncateExcerpt(first.Record.Text), FirstObservedAt: observed, Confirmed: false,
		})
	}
	return out
}
