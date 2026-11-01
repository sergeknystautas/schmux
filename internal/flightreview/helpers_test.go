package flightreview

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/flighthistory"
	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

// isolate points every schmux path at a fresh temporary directory.
func isolate(t *testing.T) {
	t.Helper()
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
}

func backlogRow(id string) flights.Flight {
	return flights.Flight{
		ID: id, Repo: "https://example.com/r.git", Prompt: "do it",
		PlannerModel: "planner", ExecutorModel: "executor",
		WorkflowState: flights.StateBacklog, CleanupState: flights.CleanupIdle,
		PhaseVisit: 1, PhaseStartedAt: t0, CreatedAt: t0, UpdatedAt: t0,
	}
}

func startedRow(id string, state flights.WorkflowState) flights.Flight {
	f := backlogRow(id)
	f.WorkflowState = state
	f.Workspace = &flights.WorkspaceAssignment{
		ID: "wa-" + id, RequestedBranch: "feature/" + id, WorkspaceID: "ws-" + id,
		Path: "/ws/" + id, Branch: "feature/" + id, Outcome: flights.StatusSucceeded,
		CreatedAt: t0, UpdatedAt: t0,
	}
	return f
}

func assign(f *flights.Flight, role flights.SessionRole, sessionID string, prior ...string) {
	f.Sessions = append(f.Sessions, flights.SessionAssignment{
		ID: "as-" + string(role) + "-" + f.ID, Role: role, Target: "model",
		SessionID: sessionID, PriorSessionIDs: prior,
		Outcome: flights.StatusSucceeded, CreatedAt: t0, UpdatedAt: t0,
	})
}

// testManager loads rows into a flights manager through a snapshot file.
func testManager(t *testing.T, rows ...flights.Flight) *flights.Manager {
	t.Helper()
	raw := make([]json.RawMessage, len(rows))
	for i, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		raw[i] = data
	}
	data, err := json.Marshal(map[string]any{"flights": raw})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "flights.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	m := flights.New(flights.Dependencies{}, log.New(io.Discard))
	if err := m.Load(path); err != nil {
		t.Fatalf("load flights: %v", err)
	}
	return m
}

// writeConversation creates a session's conversation log with one user message.
func writeConversation(t *testing.T, workspaceID, sessionID, text string) string {
	t.Helper()
	path := chat.ConversationPath(schmuxdir.ChatSessionDir(workspaceID, sessionID))
	l, err := chat.OpenLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append(chat.NewUserMessage(text, nil)); err != nil {
		t.Fatal(err)
	}
	return path
}

// fakeInspector returns canned workspace evidence.
type fakeInspector struct {
	states map[string]workspace.VCSArchiveState
	errs   map[string]error
	calls  []string
	ctxs   []context.Context
}

func (f *fakeInspector) FlightEvidence(ctx context.Context, id, _, _ string) (workspace.VCSArchiveState, error) {
	f.calls = append(f.calls, id)
	f.ctxs = append(f.ctxs, ctx)
	if err := ctx.Err(); err != nil {
		return workspace.VCSArchiveState{}, err
	}
	return f.states[id], f.errs[id]
}

func archiveDir(t *testing.T, flightID string) string {
	t.Helper()
	dir, err := flighthistory.FlightDir(flightID)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func readManifest(t *testing.T, flightID string) flighthistory.Manifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(archiveDir(t, flightID), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m flighthistory.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
