package contracts

import (
	"encoding/json"
	"testing"
)

func TestFlightContracts_JSONShape(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		wantKeys []string
		noKeys   []string
	}{
		{
			name:     "summary omits unset optional fields",
			value:    FlightSummaryResponse{ID: "fl-1", Repo: "r", WorkflowState: FlightStateBacklog, CleanupState: FlightCleanupIdle},
			wantKeys: []string{"id", "repo", "planner_model", "executor_model", "workflow_state", "cleanup_state", "order", "created_at", "updated_at"},
			noKeys:   []string{"short_name", "workspace", "sessions", "last_error", "execution_started_at", "report_target", "report_path"},
		},
		{
			name:     "detail carries the execution evidence once set",
			value:    FlightDetailResponse{ID: "fl-1", ExecutionStartedAt: "t", ReportTarget: "/r-execution.md", ReportPath: "/r-execution.md", Monitor: &FlightMonitor{Turns: []FlightMonitoredTurn{{ID: "turn-1", Execution: &FlightExecutionCheck{Skill: "executing-plans not found"}}}}},
			wantKeys: []string{"execution_started_at", "report_target", "report_path", "monitor"},
		},
		{
			name:     "executor turn check omits an empty report error",
			value:    FlightExecutionCheck{Skill: "executing-plans not found", ReportPath: "/r-execution.md"},
			wantKeys: []string{"skill", "report_path", "report_verified"},
			noKeys:   []string{"report_error"},
		},
		{
			name: "detail carries prompt and operations",
			value: FlightDetailResponse{ID: "fl-1", Prompt: "p", Operations: []FlightOperation{{ID: "op-1", Action: "takeoff"}},
				Workspace: &FlightWorkspaceAssignment{ID: "wa-1", WorkspaceID: "ws-1", Available: true}},
			wantKeys: []string{"prompt", "operations", "workspace", "workflow_state", "cleanup_state"},
			noKeys:   []string{"artifact_path", "spec_path", "plan_path", "cancel_reason", "done_at", "done"},
		},
		{
			name:     "transition response omits removed unless set",
			value:    FlightTransitionResponse{Flight: FlightDetailResponse{ID: "fl-1"}},
			wantKeys: []string{"flight"},
			noKeys:   []string{"removed"},
		},
		{
			name:     "dispose request is explicit",
			value:    FlightDisposeRequest{WorkspaceID: "ws-1"},
			wantKeys: []string{"workspace_id", "confirm"},
		},
		{
			name:     "update request omits untouched fields",
			value:    UpdateFlightRequest{},
			wantKeys: nil,
			noKeys:   []string{"repo", "prompt", "short_name", "planner_model", "executor_model"},
		},
		{
			name:     "reorder request names the full list",
			value:    ReorderFlightsRequest{FlightIDs: []string{"a", "b"}},
			wantKeys: []string{"flight_ids"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]json.RawMessage
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			for _, k := range tt.wantKeys {
				if _, ok := got[k]; !ok {
					t.Errorf("missing key %q in %s", k, data)
				}
			}
			for _, k := range tt.noKeys {
				if _, ok := got[k]; ok {
					t.Errorf("unexpected key %q in %s", k, data)
				}
			}
		})
	}
}

func TestFlightStateConstantsMatchTheDocumentedValues(t *testing.T) {
	want := map[string]string{
		string(FlightStateBacklog): "backlog", string(FlightStatePlanning): "planning",
		string(FlightStateExecuting): "executing", string(FlightStateReviewing): "reviewing",
		string(FlightStateCompleting): "completing",
		string(FlightCleanupIdle):     "idle", string(FlightCleanupDisposing): "disposing",
		string(FlightRolePlanner): "planner", string(FlightRoleExecutor): "executor", string(FlightRoleCommit): "commit",
		string(FlightRolePilot): "pilot",
	}
	for got, w := range want {
		if got != w {
			t.Errorf("constant = %q, want %q", got, w)
		}
	}
}

// An operation the pilot asked for names the deciding turn; a manual
// operation does not.
func TestFlightOperationProvenance(t *testing.T) {
	automatic, err := json.Marshal(FlightOperation{
		ID: "op-1", Action: "execution", CorrelationID: "c-1", Status: "succeeded",
		SourceTurnID: "turn-pilot-3",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(automatic, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["source_turn_id"]; !ok {
		t.Errorf("an automatic operation has no source turn: %s", automatic)
	}

	manual, err := json.Marshal(FlightOperation{ID: "op-2", Action: "execution", CorrelationID: "c-2", Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	got = map[string]json.RawMessage{}
	if err := json.Unmarshal(manual, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["source_turn_id"]; ok {
		t.Errorf("a manual operation names a source turn: %s", manual)
	}
}
