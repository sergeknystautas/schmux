package flights

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func backlogFlight(id string, order int) Flight {
	return Flight{
		ID: id, Repo: "https://example.com/r.git", Prompt: "do it",
		PlannerModel: "planner", ExecutorModel: "executor",
		WorkflowState: StateBacklog, CleanupState: CleanupIdle, Order: order,
		CreatedAt: t0, UpdatedAt: t0,
	}
}

func startedFlight(id string, state WorkflowState) Flight {
	f := backlogFlight(id, 0)
	f.WorkflowState = state
	f.Workspace = &WorkspaceAssignment{
		ID: "wa-" + id, RequestedBranch: "schmux/flight/" + id, WorkspaceID: "repo-" + id,
		Path: "/ws/" + id, Branch: "schmux/flight/" + id, Outcome: StatusSucceeded,
		CreatedAt: t0, UpdatedAt: t0,
	}
	return f
}

func encode(t *testing.T, fl ...Flight) []byte {
	t.Helper()
	data, err := json.Marshal(snapshot{Flights: fl})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestLoadSnapshot_MissingAndEmptyFilesAreEmpty(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{"missing": filepath.Join(dir, "missing.json"), "zero bytes": empty} {
		t.Run(name, func(t *testing.T) {
			snap, err := loadSnapshot(path)
			if err != nil {
				t.Fatalf("loadSnapshot() error = %v, want nil", err)
			}
			if len(snap.Flights) != 0 {
				t.Fatalf("flights = %d, want 0", len(snap.Flights))
			}
		})
	}
}

func TestSnapshot_RoundTripsWithPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flights.json")
	started := startedFlight("fl-b", StatePlanning)
	started.Sessions = []SessionAssignment{{
		ID: "sa-1", Role: RolePlanner, Target: "planner", SessionID: "repo-fl-b-s1",
		PriorSessionIDs: []string{"old-1"}, Outcome: StatusSucceeded, CreatedAt: t0, UpdatedAt: t0,
	}}
	started.Operations = []Operation{{
		ID: "op-1", Action: ActionTakeoff, CorrelationID: "c-1", Status: StatusSucceeded,
		Resources: []ResourceOutcome{{Kind: "workspace", ID: "repo-fl-b", Status: StatusSucceeded}},
		CreatedAt: t0, UpdatedAt: t0,
	}}
	want := snapshot{Flights: []Flight{backlogFlight("fl-a", 0), started}}

	if err := writeSnapshot(path, want); err != nil {
		t.Fatalf("writeSnapshot() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["version"]; exists {
		t.Fatalf("saved snapshot contains version: %s", data)
	}
	got, err := loadSnapshot(path)
	if err != nil {
		t.Fatalf("loadSnapshot() error = %v", err)
	}

	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("round trip mismatch:\n got %s\nwant %s", gotJSON, wantJSON)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestLoadSnapshot_RejectsInvalidFilesAndLeavesThemUntouched(t *testing.T) {
	dupWorkspaceAssign := startedFlight("fl-b", StatePlanning)
	dupWorkspaceAssign.Workspace.ID = "wa-fl-a"
	sessA := startedFlight("fl-a", StatePlanning)
	sessA.Sessions = []SessionAssignment{{ID: "sa-1", Role: RolePlanner, Target: "t", Outcome: StatusFailed, CreatedAt: t0, UpdatedAt: t0}}
	sessB := startedFlight("fl-b", StatePlanning)
	sessB.Sessions = []SessionAssignment{{ID: "sa-1", Role: RolePlanner, Target: "t", Outcome: StatusFailed, CreatedAt: t0, UpdatedAt: t0}}
	opA := startedFlight("fl-a", StatePlanning)
	opA.Operations = []Operation{{ID: "op-1", Action: ActionTakeoff, CorrelationID: "c", Status: StatusFailed, CreatedAt: t0, UpdatedAt: t0}}
	opB := startedFlight("fl-b", StatePlanning)
	opB.Operations = []Operation{{ID: "op-1", Action: ActionTakeoff, CorrelationID: "c", Status: StatusFailed, CreatedAt: t0, UpdatedAt: t0}}
	badRole := startedFlight("fl-a", StatePlanning)
	badRole.Sessions = []SessionAssignment{{ID: "sa-1", Role: "pilot", Target: "t", Outcome: StatusFailed}}
	badAction := startedFlight("fl-a", StatePlanning)
	badAction.Operations = []Operation{{ID: "op-1", Action: "launch", CorrelationID: "c", Status: StatusFailed}}
	badStatus := startedFlight("fl-a", StatePlanning)
	badStatus.Operations = []Operation{{ID: "op-1", Action: ActionTakeoff, CorrelationID: "c", Status: "weird"}}
	badWorkflow := backlogFlight("fl-a", 0)
	badWorkflow.WorkflowState = "flying"
	badCleanup := startedFlight("fl-a", StatePlanning)
	badCleanup.CleanupState = "scrubbing"
	noID := backlogFlight("", 0)

	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"not json", []byte("{not json"), "decode"},
		{"trailing data", append(encode(t), []byte(`{}`)...), "decode"},
		{"duplicate flight id", encode(t, backlogFlight("fl-a", 0), backlogFlight("fl-a", 1)), "duplicate flight"},
		{"empty flight id", encode(t, noID), "id"},
		{"duplicate workspace assignment id", encode(t, startedFlight("fl-a", StatePlanning), dupWorkspaceAssign), "duplicate assignment"},
		{"duplicate session assignment id", encode(t, sessA, sessB), "duplicate assignment"},
		{"duplicate operation id", encode(t, opA, opB), "duplicate operation"},
		{"invalid workflow state", encode(t, badWorkflow), "workflow state"},
		{"invalid cleanup state", encode(t, badCleanup), "cleanup state"},
		{"invalid session role", encode(t, badRole), "role"},
		{"invalid operation action", encode(t, badAction), "action"},
		{"invalid operation status", encode(t, badStatus), "status"},
		{"duplicate backlog order", encode(t, backlogFlight("fl-a", 3), backlogFlight("fl-b", 3)), "backlog order"},
		{"negative backlog order", encode(t, backlogFlight("fl-a", -1)), "backlog order"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "flights.json")
			if err := os.WriteFile(path, tt.data, 0o600); err != nil {
				t.Fatal(err)
			}

			_, err := loadSnapshot(path)

			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("loadSnapshot() error = %v, want containing %q", err, tt.want)
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil || string(after) != string(tt.data) {
				t.Fatalf("file changed after rejected load: %q (err %v)", after, readErr)
			}
		})
	}
}

func TestLoadSnapshot_AcceptsManuallyEditedStages(t *testing.T) {
	backlog := startedFlight("fl-backlog", StateBacklog)
	backlog.CleanupState = CleanupTerminating
	planning := backlogFlight("fl-planning", 0)
	planning.WorkflowState = StatePlanning
	path := filepath.Join(t.TempDir(), "flights.json")
	if err := writeSnapshot(path, snapshot{Flights: []Flight{backlog, planning}}); err != nil {
		t.Fatal(err)
	}
	got, err := loadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Flights) != 2 || got.Flights[0].Workspace == nil || got.Flights[1].Workspace != nil {
		t.Fatalf("manual stage edits lost resource assignments: %+v", got.Flights)
	}
}

func TestLoadSnapshot_AcceptsStartedFlightsWithSharedOrderValues(t *testing.T) {
	// Order is only meaningful for backlog flights; started flights may repeat it.
	path := filepath.Join(t.TempDir(), "flights.json")
	if err := os.WriteFile(path, encode(t, startedFlight("fl-a", StatePlanning), startedFlight("fl-b", StateDone), backlogFlight("fl-c", 0)), 0o600); err != nil {
		t.Fatal(err)
	}

	snap, err := loadSnapshot(path)

	if err != nil {
		t.Fatalf("loadSnapshot() error = %v", err)
	}
	if len(snap.Flights) != 3 {
		t.Fatalf("flights = %d, want 3", len(snap.Flights))
	}
}

func TestLoadSnapshot_IgnoresExistingVersionFields(t *testing.T) {
	for _, raw := range []string{
		`{"flights":[]}`,
		`{"version":1,"flights":[]}`,
		`{"version":2,"flights":[]}`,
		`{"version":999,"flights":[]}`,
		`{"version":"anything","flights":[]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "flights.json")
			if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			snap, err := loadSnapshot(path)
			if err != nil {
				t.Fatalf("loadSnapshot() error = %v", err)
			}
			if len(snap.Flights) != 0 {
				t.Fatalf("flights = %+v, want empty", snap.Flights)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != raw {
				t.Fatalf("load changed file: %s (err %v)", after, err)
			}
		})
	}
}
