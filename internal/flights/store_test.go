package flights

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
	data, err := encodeSnapshot(snapshot{Flights: fl})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeSnapshot(t *testing.T, path string, snap snapshot) {
	t.Helper()
	data, err := encodeSnapshot(snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSnapshotData(path, data); err != nil {
		t.Fatal(err)
	}
}

// loadSnapshot reads and parses a snapshot file the way the manager does.
func loadSnapshot(t *testing.T, path string) (snapshot, error) {
	t.Helper()
	data, _, err := readSnapshotFile(path)
	if err != nil {
		return snapshot{}, err
	}
	return parseSnapshot(data, path)
}

func TestLoadSnapshot_MissingAndEmptyFilesAreEmpty(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{"missing": filepath.Join(dir, "missing.json"), "zero bytes": empty} {
		t.Run(name, func(t *testing.T) {
			snap, err := loadSnapshot(t, path)
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

	writeSnapshot(t, path, want)
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
	got, err := loadSnapshot(t, path)
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

func TestLoadSnapshot_RejectsMalformedFilesAndLeavesThemUntouched(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"not json", []byte("{not json"), "decode"},
		{"trailing data", append(encode(t), []byte(`{}`)...), "decode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "flights.json")
			if err := os.WriteFile(path, tt.data, 0o600); err != nil {
				t.Fatal(err)
			}

			_, err := loadSnapshot(t, path)

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
	backlog.CleanupState = CleanupDisposing
	planning := backlogFlight("fl-planning", 0)
	planning.WorkflowState = StatePlanning
	path := filepath.Join(t.TempDir(), "flights.json")
	writeSnapshot(t, path, snapshot{Flights: []Flight{backlog, planning}})
	got, err := loadSnapshot(t, path)
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

	snap, err := loadSnapshot(t, path)

	if err != nil {
		t.Fatalf("loadSnapshot() error = %v", err)
	}
	if len(snap.Flights) != 3 {
		t.Fatalf("flights = %d, want 3", len(snap.Flights))
	}
}

func TestLoadSnapshot_AcceptsExistingCleanupVocabulary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flights.json")
	raw := `{"version":1,"flights":[{"id":"fl-a","repo":"https://example.com/r.git","prompt":"p","planner_model":"m","executor_model":"m","workflow_state":"executing","cleanup_state":"terminating","order":0,"workspace":{"id":"wa","requested_branch":"b","workspace_id":"ws-1","path":"/w","outcome":"succeeded","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},"operations":[{"id":"op-1","action":"terminate","correlation_id":"c","status":"pending","resource":"workspace","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}],"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := loadSnapshot(t, path)
	if err != nil {
		t.Fatalf("loadSnapshot() error = %v", err)
	}
	if got := snap.Flights[0].CleanupState; got != CleanupDisposing {
		t.Errorf("CleanupState = %q, want %q", got, CleanupDisposing)
	}
	if got := snap.Flights[0].Operations[0].Action; got != ActionDispose {
		t.Errorf("Action = %q, want %q", got, ActionDispose)
	}
}

func TestLoadSnapshot_AcceptsHistoricalFlightVocabulary(t *testing.T) {
	legacy := startedFlight("fl-legacy", StateExecuting)
	legacy.WorkflowState = "reviewing"
	legacy.Sessions = append(legacy.Sessions, SessionAssignment{
		ID: "sa-pilot", Role: "pilot", Target: "pilot-model", Outcome: StatusSucceeded,
		CreatedAt: t0, UpdatedAt: t0,
	})
	legacy.Operations = append(legacy.Operations, Operation{
		ID: "op-review", Action: "review", CorrelationID: "c-review", Status: StatusSucceeded,
		CreatedAt: t0, UpdatedAt: t0,
	})
	path := filepath.Join(t.TempDir(), "flights.json")
	if err := os.WriteFile(path, encode(t, backlogFlight("fl-backlog", 0), legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	snap, err := loadSnapshot(t, path)

	if err != nil {
		t.Fatalf("loadSnapshot() error = %v", err)
	}
	if len(snap.Flights) != 2 {
		t.Fatalf("flights = %d, want 2", len(snap.Flights))
	}
	got := snap.Flights[1]
	if got.WorkflowState != StateExecuting {
		t.Errorf("WorkflowState = %q, want %q", got.WorkflowState, StateExecuting)
	}
	if got.Sessions[0].Role != RolePilot {
		t.Errorf("Role = %q, want %q", got.Sessions[0].Role, RolePilot)
	}
	if got.Operations[0].Action != ActionReview {
		t.Errorf("Action = %q, want %q", got.Operations[0].Action, ActionReview)
	}
}

func TestSnapshot_PreservesUnknownFlightFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flights.json")
	raw := `{"flights":[{"id":"fl-a","repo":"https://example.com/r.git","prompt":"p","planner_model":"m","executor_model":"m","workflow_state":"backlog","cleanup_state":"idle","order":0,"future_field":{"nested":[1,2]},"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := loadSnapshot(t, path)
	if err != nil {
		t.Fatalf("loadSnapshot() error = %v", err)
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != raw {
		t.Fatalf("load changed snapshot: %s (err %v)", after, err)
	}

	saved := filepath.Join(t.TempDir(), "flights.json")
	writeSnapshot(t, saved, snap)
	data, err := os.ReadFile(saved)
	if err != nil {
		t.Fatal(err)
	}
	var fields struct {
		Flights []struct {
			Future json.RawMessage `json:"future_field"`
		} `json:"flights"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("decode saved snapshot: %v\n%s", err, data)
	}
	var got, want any
	if err := json.Unmarshal(fields.Flights[0].Future, &got); err != nil {
		t.Fatalf("decode future field: %v\n%s", err, data)
	}
	if err := json.Unmarshal([]byte(`{"nested":[1,2]}`), &want); err != nil {
		t.Fatal(err)
	}
	if len(fields.Flights) != 1 || !reflect.DeepEqual(got, want) {
		t.Fatalf("future field lost: %s", data)
	}
}

func TestSnapshot_PreservesUnknownNestedFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flights.json")
	raw := `{"flights":[{"id":"fl-a","repo":"https://example.com/r.git","prompt":"p","planner_model":"m","executor_model":"m","workflow_state":"planning","cleanup_state":"idle","order":0,"workspace":{"id":"wa","requested_branch":"b","workspace_id":"ws","path":"/w","outcome":"succeeded","workspace_future":"keep","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},"sessions":[{"id":"sa","role":"pilot","target":"m","outcome":"succeeded","session_future":"keep","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}],"operations":[{"id":"op","action":"review","correlation_id":"c","status":"succeeded","operation_future":"keep","resources":[{"kind":"workspace","status":"succeeded","resource_future":"keep"}],"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}],"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := loadSnapshot(t, path)
	if err != nil {
		t.Fatalf("loadSnapshot() error = %v", err)
	}

	saved := filepath.Join(t.TempDir(), "flights.json")
	writeSnapshot(t, saved, snap)
	data, err := os.ReadFile(saved)
	if err != nil {
		t.Fatal(err)
	}
	var fields struct {
		Flights []struct {
			Workspace struct {
				Future string `json:"workspace_future"`
			} `json:"workspace"`
			Sessions []struct {
				Future string `json:"session_future"`
			} `json:"sessions"`
			Operations []struct {
				Future    string `json:"operation_future"`
				Resources []struct {
					Future string `json:"resource_future"`
				} `json:"resources"`
			} `json:"operations"`
		} `json:"flights"`
	}
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("decode saved snapshot: %v\n%s", err, data)
	}
	if len(fields.Flights) != 1 {
		t.Fatalf("flights = %d, want 1", len(fields.Flights))
	}
	f := fields.Flights[0]
	if f.Workspace.Future != "keep" || len(f.Sessions) != 1 || f.Sessions[0].Future != "keep" ||
		len(f.Operations) != 1 || f.Operations[0].Future != "keep" ||
		len(f.Operations[0].Resources) != 1 || f.Operations[0].Resources[0].Future != "keep" {
		t.Fatalf("nested future fields lost: %s", data)
	}
}

func TestSnapshot_PreservesExistingVersionFields(t *testing.T) {
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
			snap, err := loadSnapshot(t, path)
			if err != nil {
				t.Fatalf("loadSnapshot() error = %v", err)
			}
			if len(snap.Flights) != 0 {
				t.Fatalf("flights = %+v, want empty", snap.Flights)
			}
			var before map[string]json.RawMessage
			if err := json.Unmarshal([]byte(raw), &before); err != nil {
				t.Fatal(err)
			}
			_, hadVersion := before["version"]
			after, err := os.ReadFile(path)
			if err != nil || string(after) != raw {
				t.Fatalf("load changed file: %s (err %v)", after, err)
			}
			saved := filepath.Join(t.TempDir(), "flights.json")
			writeSnapshot(t, saved, snap)
			data, err := os.ReadFile(saved)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if _, exists := fields["version"]; hadVersion && !exists {
				t.Fatalf("save dropped version: %s", data)
			}
		})
	}
}

// A flight whose monitor records are inconsistent is quarantined like any
// other invalid row: it stays out of the active flights, the raw row is kept
// for the next save, and the file on disk is left untouched.
func TestLoadSnapshotQuarantinesInvalidMonitorRecords(t *testing.T) {
	turn := PlannerTurn{SessionID: "planner", StartSeq: 1, EndSeq: 2, Outcome: "completed"}
	for name, mutate := range map[string]func(*FlightMonitor){
		"duplicate turn": func(m *FlightMonitor) { m.Turns = append(m.Turns, m.Turns[0]) },
		"invalid status": func(m *FlightMonitor) { m.Turns[0].Status = "corrupt" },
		"invalid source": func(m *FlightMonitor) { m.Turns[0].Turn.EndSeq = 0 },
		"invalid message": func(m *FlightMonitor) {
			m.Messages = []FlightMessage{{ID: "message", Kind: "notification", SourceTurnID: monitorTurnID(turn), RecipientSessionID: "pilot", Status: "corrupt"}}
		},
		"missing source": func(m *FlightMonitor) {
			m.Messages = []FlightMessage{{ID: "message", Kind: "notification", SourceTurnID: "missing", RecipientSessionID: "pilot", Status: MessageStatusPending}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := startedFlight("fl-1", StatePlanning)
			f.Monitor = &FlightMonitor{Turns: []MonitoredTurn{{ID: monitorTurnID(turn), Turn: turn, Status: MonitoredTurnPending}}}
			mutate(f.Monitor)
			path := filepath.Join(t.TempDir(), "flights.json")
			original := encode(t, f)
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}

			snap, err := loadSnapshot(t, path)

			if err != nil {
				t.Fatalf("loadSnapshot() error = %v", err)
			}
			if len(snap.Flights) != 0 || len(snap.InvalidFlights) != 1 {
				t.Fatalf("invalid %s was not quarantined: flights=%d invalid=%d", name, len(snap.Flights), len(snap.InvalidFlights))
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(original) {
				t.Fatalf("invalid snapshot was modified: %s", after)
			}
		})
	}
}
