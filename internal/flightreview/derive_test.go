package flightreview

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/flighthistory"
	"github.com/sergeknystautas/schmux/internal/flights"
)

func at(minutes int) time.Time { return t0.Add(time.Duration(minutes) * time.Minute) }

func op(id string, action flights.OperationAction, when time.Time, status flights.OperationStatus, sourceTurn string) flights.Operation {
	o := flights.Operation{ID: id, Action: action, CorrelationID: "corr-" + id, Status: status, CreatedAt: when, UpdatedAt: when, SourceTurnID: sourceTurn}
	if sourceTurn != "" {
		o.PhaseVisit = 1
	}
	return o
}

// executingFlight is a flight in its third phase visit: takeoff at minute 1,
// pilot-started execution at minute 20.
func executingFlight() flights.Flight {
	f := startedRow("fl-1", flights.StateExecuting)
	f.PhaseVisit = 3
	f.PhaseStartedAt = at(20)
	f.Operations = []flights.Operation{
		op("op-takeoff", flights.ActionTakeoff, at(1), flights.StatusSucceeded, ""),
		op("op-exec", flights.ActionExecution, at(20), flights.StatusSucceeded, "turn-1"),
	}
	assign(&f, flights.RolePlanner, "s-plan")
	assign(&f, flights.RoleExecutor, "s-exec")
	f.Sessions[0].UpdatedAt = at(2)
	f.Sessions[1].UpdatedAt = at(21)
	return f
}

var eventSeq int

func event(f flights.Flight, change flighthistory.Change, actor flighthistory.Actor, cause string, when time.Time) flighthistory.Event {
	eventSeq++
	return flighthistory.Event{
		SchemaVersion: flighthistory.SchemaVersion, EventID: fmt.Sprintf("ev-%d", eventSeq), RecordedAt: when,
		Change: change, Actor: actor, Cause: cause, SnapshotSHA256: "sha",
		FlightID: f.ID, RowSHA256: "row", State: f.HistoryState(),
	}
}

func inPhase(f flights.Flight, state flights.WorkflowState, visit uint64, entered time.Time) flights.Flight {
	f.WorkflowState, f.PhaseVisit, f.PhaseStartedAt = state, visit, entered
	return f
}

func missingEvent(id, kind, resourceID string, when time.Time) flighthistory.ReconciliationEvent {
	return flighthistory.ReconciliationEvent{
		SchemaVersion: 1, EventID: id, RecordedAt: when, Kind: flighthistory.ReconcileResourceMissing,
		FlightID: "fl-1", ResourceKind: kind, ResourceID: resourceID, Source: "unknown",
	}
}

func userMsg(id, text string, when time.Time) chat.Record {
	return chat.Record{Ts: when.Format(time.RFC3339Nano), Type: chat.RecordUserMessage, ID: id, Text: text}
}

func transcript(role, sessionID string, records ...chat.Record) Transcript {
	t := Transcript{Role: role, SessionID: sessionID}
	for i, rec := range records {
		rec.Seq = uint64(i + 1)
		t.Records = append(t.Records, TranscriptRecord{Seq: i + 1, Line: i + 1, Record: rec})
	}
	return t
}

func interventionsOfKind(s Summary, kind InterventionKind) []Intervention {
	var out []Intervention
	for _, iv := range s.Interventions {
		if iv.Kind == kind {
			out = append(out, iv)
		}
	}
	return out
}

// undoEvidence is a flight whose user moved it back to planning after both
// of its sessions disappeared.
func undoEvidence() Evidence {
	exec := executingFlight()
	final := inPhase(exec, flights.StatePlanning, 4, at(60))
	return Evidence{
		Flight: final,
		Events: []flighthistory.Event{
			event(inPhase(exec, flights.StateBacklog, 1, at(0)), flighthistory.ChangeCreated, flighthistory.ActorUser, "create_flight", at(0)),
			event(inPhase(exec, flights.StatePlanning, 2, at(1)), flighthistory.ChangeUpdated, flighthistory.ActorUser, "takeoff", at(1)),
			event(exec, flighthistory.ChangeUpdated, flighthistory.ActorPilot, "execution", at(20)),
			event(final, flighthistory.ChangeUpdated, flighthistory.ActorUser, "edit_flight", at(60)),
		},
		Reconciliation: []flighthistory.ReconciliationEvent{
			missingEvent("re-1", "session", "s-exec", at(55)),
			missingEvent("re-2", "session", "s-plan", at(58)),
		},
	}
}

func TestDeriveSummaryExcludesPilotMessageButCountsOperatorMessage(t *testing.T) {
	f := executingFlight()
	f.Monitor = &flights.FlightMonitor{Messages: []flights.FlightMessage{{
		ID: "m1", Kind: "pilot_message", RecipientSessionID: "s-exec", Text: "Please use postgres",
		ChatMessageID: "chat-pilot", Status: flights.MessageStatusAccepted, CreatedAt: at(40), UpdatedAt: at(40),
	}}}
	ev := Evidence{Flight: f, Transcripts: []Transcript{
		transcript("executor", "s-exec",
			userMsg("u-exec-launch", "launch prompt", at(21).Add(-time.Second)),
			userMsg("chat-pilot", "Please use postgres", at(40)),
		),
		transcript("planner", "s-plan",
			userMsg("u-plan-launch", "launch prompt", at(2).Add(-time.Second)),
			userMsg("chat-operator", "Please use postgres", at(50)),
		),
	}}

	got := DeriveSummary(ev)

	if got.Counts.Interventions != 1 {
		t.Fatalf("interventions = %d, want 1 (only the operator's): %+v", got.Counts.Interventions, got.Interventions)
	}
	iv := got.Interventions[0]
	if iv.Kind != KindOperatorMessage || iv.ChatRecordID != "chat-operator" || iv.SessionRole != "planner" || iv.SessionID != "s-plan" {
		t.Errorf("intervention = %+v", iv)
	}
	if iv.Excerpt != "Please use postgres" || iv.Classification != "unclassified" {
		t.Errorf("excerpt/classification = %q / %q", iv.Excerpt, iv.Classification)
	}
	if iv.ElapsedSinceTakeoff == nil || *iv.ElapsedSinceTakeoff != 49*60 {
		t.Errorf("elapsed since takeoff = %v, want %d", iv.ElapsedSinceTakeoff, 49*60)
	}
	if got.Counts.PilotMessagesExcluded != 1 || got.Counts.LaunchPromptsExcluded != 2 || got.Counts.Unclassified != 1 {
		t.Errorf("counts = %+v", got.Counts)
	}
}

func TestDeriveSummaryCountsControlRecordsAndQueueChanges(t *testing.T) {
	f := executingFlight()
	control := func(line string, when time.Time) chat.Record {
		return chat.Record{Ts: when.Format(time.RFC3339Nano), Type: chat.RecordControl, Line: json.RawMessage(line)}
	}
	queue := chat.NewUserMessageQueue("u-held", true)
	queue.Ts = at(45).Format(time.RFC3339Nano)
	ev := Evidence{Flight: f, Transcripts: []Transcript{transcript("executor", "s-exec",
		control(`{"type":"control_request","request":{"subtype":"interrupt"}}`, at(30)),
		control(`{"type":"control_response","response":{"response":{"behavior":"allow","updatedInput":{}}}}`, at(31)),
		control(`{"type":"control_response","response":{"response":{"behavior":"allow","updatedInput":{"answers":{"q":"a"}}}}}`, at(32)),
		control(`{"type":"something_else"}`, at(33)),
		queue,
	)}}

	got := DeriveSummary(ev)

	for kind, want := range map[InterventionKind]int{KindInterrupt: 1, KindApproval: 1, KindAnswer: 1, KindQueueChange: 1} {
		if n := len(interventionsOfKind(got, kind)); n != want {
			t.Errorf("%s interventions = %d, want %d", kind, n, want)
		}
	}
	if got.Counts.Interventions != 4 {
		t.Errorf("interventions = %d, want 4 (an unrecognized control line is not counted)", got.Counts.Interventions)
	}
}

func TestDeriveSummaryCountsManualRoutesButNotPilotRoutesTakeoffOrCompletedDisposal(t *testing.T) {
	f := executingFlight()
	f.Operations = append(f.Operations,
		op("op-review", flights.ActionReview, at(40), flights.StatusFailed, ""),
		op("op-review-retry", flights.ActionReview, at(42), flights.StatusSucceeded, ""),
		op("op-next", flights.ActionNextReview, at(44), flights.StatusSucceeded, "turn-9"),
	)

	got := DeriveSummary(Evidence{Flight: f})

	routes := interventionsOfKind(got, KindManualRoute)
	if len(routes) != 2 || routes[0].Sources[0].ID != "op-review" || routes[1].Sources[0].ID != "op-review-retry" {
		t.Fatalf("manual routes = %+v, want the failed attempt and its retry only", routes)
	}
	if got.Counts.Interventions != 2 {
		t.Errorf("interventions = %d, want 2", got.Counts.Interventions)
	}
	if routes[1].ElapsedSincePrevious == nil || *routes[1].ElapsedSincePrevious != 120 {
		t.Errorf("elapsed since previous = %v, want 120", routes[1].ElapsedSincePrevious)
	}
}

func TestDeriveSummaryCountsManualUndoWithKilledSessions(t *testing.T) {
	got := DeriveSummary(undoEvidence())

	if got.PrimaryOutcome != OutcomeManualUndoRecovery {
		t.Fatalf("primary outcome = %s, want manual_undo_recovery", got.PrimaryOutcome)
	}
	undo := interventionsOfKind(got, KindManualUndo)
	if len(undo) != 1 {
		t.Fatalf("manual undo interventions = %+v", undo)
	}
	if !slices.Equal(undo[0].MissingSessionIDs, []string{"s-exec", "s-plan"}) {
		t.Errorf("undo links sessions %v, want both vanished sessions", undo[0].MissingSessionIDs)
	}
	kills := interventionsOfKind(got, KindSessionKill)
	if len(kills) != 2 || kills[0].Source != "unknown" {
		t.Errorf("session kills = %+v, want two with source unknown", kills)
	}
	if got.Counts.ManualUndos != 1 || got.Counts.SessionsKilled != 2 {
		t.Errorf("counts = %+v", got.Counts)
	}
	wantDurations := map[uint64]int64{1: 60, 2: 19 * 60, 3: 40 * 60}
	for _, visit := range got.PhasePath {
		want, known := wantDurations[visit.Visit]
		switch {
		case known && (visit.DurationSeconds == nil || *visit.DurationSeconds != want):
			t.Errorf("visit %d duration = %v, want %d", visit.Visit, visit.DurationSeconds, want)
		case !known && visit.DurationSeconds != nil:
			t.Errorf("visit %d has duration %d although its exit was never observed", visit.Visit, *visit.DurationSeconds)
		}
	}
	if len(got.PhasePath) != 4 {
		t.Errorf("phase path = %d visits, want 4", len(got.PhasePath))
	}
}

func TestDeriveSummaryMarksOrphanWithoutReplacingOutcome(t *testing.T) {
	got := DeriveSummary(undoEvidence())
	if !got.OrphanedActive || got.PrimaryOutcome != OutcomeManualUndoRecovery {
		t.Errorf("orphaned = %v, outcome = %s; want orphaned flag beside manual_undo_recovery", got.OrphanedActive, got.PrimaryOutcome)
	}

	t.Run("a lost workspace orphans an active flight", func(t *testing.T) {
		ev := Evidence{Flight: executingFlight(), Reconciliation: []flighthistory.ReconciliationEvent{missingEvent("re-w", "workspace", "ws-fl-1", at(70))}}
		got := DeriveSummary(ev)
		if !got.OrphanedActive || got.PrimaryOutcome != OutcomeWorkspaceLostUnknown {
			t.Errorf("orphaned = %v, outcome = %s", got.OrphanedActive, got.PrimaryOutcome)
		}
	})
	t.Run("one surviving session is not an orphan", func(t *testing.T) {
		ev := Evidence{Flight: executingFlight(), Reconciliation: []flighthistory.ReconciliationEvent{missingEvent("re-s", "session", "s-exec", at(70))}}
		got := DeriveSummary(ev)
		if got.OrphanedActive {
			t.Error("flight with a surviving session was flagged orphaned")
		}
	})
	t.Run("a healthy active flight is active", func(t *testing.T) {
		got := DeriveSummary(Evidence{Flight: executingFlight()})
		if got.OrphanedActive || got.PrimaryOutcome != OutcomeActive || got.OutcomePendingUser {
			t.Errorf("orphaned = %v, outcome = %s, pending = %v", got.OrphanedActive, got.PrimaryOutcome, got.OutcomePendingUser)
		}
	})
}

func TestDeriveSummaryRequiresEvidenceForFinishedOutsideFlight(t *testing.T) {
	lost := []flighthistory.ReconciliationEvent{missingEvent("re-w", "workspace", "ws-fl-1", at(70))}

	t.Run("a missing workspace alone proves nothing", func(t *testing.T) {
		got := DeriveSummary(Evidence{Flight: executingFlight(), Reconciliation: lost})
		if got.PrimaryOutcome != OutcomeWorkspaceLostUnknown || got.OutcomePendingUser {
			t.Errorf("outcome = %s pending = %v", got.PrimaryOutcome, got.OutcomePendingUser)
		}
	})

	branch := &GitState{VCS: "git", Branch: "feature/x", HeadSHA: "abc123", BaseBranch: "main", BaseSHA: "def456", Ahead: 2}
	pending := DeriveSummary(Evidence{Flight: executingFlight(), Reconciliation: lost, GitState: branch})
	t.Run("branch evidence is only provisional until confirmed", func(t *testing.T) {
		if pending.PrimaryOutcome != OutcomeFinishedOutside || !pending.OutcomePendingUser || pending.ConfirmedByUser {
			t.Errorf("outcome = %s pending = %v confirmed = %v", pending.PrimaryOutcome, pending.OutcomePendingUser, pending.ConfirmedByUser)
		}
	})
	t.Run("a confirmed decision settles it without touching the derived summary", func(t *testing.T) {
		decided := ApplyDecisions(pending, DecisionContext{ConfirmedExternalCompletion: map[string]bool{"fl-1": true}})
		if decided.PrimaryOutcome != OutcomeFinishedOutside || decided.OutcomePendingUser || !decided.ConfirmedByUser {
			t.Errorf("decided outcome = %s pending = %v confirmed = %v", decided.PrimaryOutcome, decided.OutcomePendingUser, decided.ConfirmedByUser)
		}
		if !pending.OutcomePendingUser || pending.ConfirmedByUser {
			t.Error("ApplyDecisions modified the derived summary")
		}
		other := ApplyDecisions(pending, DecisionContext{ConfirmedExternalCompletion: map[string]bool{"fl-other": true}})
		if !other.OutcomePendingUser {
			t.Error("a decision about another flight settled this one")
		}
	})
	t.Run("a collection error is not branch evidence", func(t *testing.T) {
		broken := &GitState{VCS: "git", Branch: "feature/x", Error: "workspace directory is not available"}
		got := DeriveSummary(Evidence{Flight: executingFlight(), Reconciliation: lost, GitState: broken})
		if got.PrimaryOutcome != OutcomeWorkspaceLostUnknown {
			t.Errorf("outcome = %s, want workspace_lost_unknown", got.PrimaryOutcome)
		}
	})
}

func completedFlight() flights.Flight {
	f := executingFlight()
	f.WorkflowState, f.PhaseVisit, f.PhaseStartedAt = flights.StateCompleting, 5, at(90)
	f.Operations = append(f.Operations,
		op("op-review", flights.ActionReview, at(60), flights.StatusSucceeded, ""),
		op("op-complete", flights.ActionCompletion, at(90), flights.StatusSucceeded, "turn-3"),
	)
	assign(&f, flights.RoleReviewer, "s-rev")
	assign(&f, flights.RoleCommit, "s-commit")
	return f
}

func TestDeriveSummaryUsesOutcomePrecedence(t *testing.T) {
	lost := []flighthistory.ReconciliationEvent{missingEvent("re-w", "workspace", "ws-fl-1", at(120))}
	branch := &GitState{VCS: "git", HeadSHA: "abc", Ahead: 1}
	created := func(f flights.Flight) flighthistory.Event {
		return event(inPhase(f, flights.StateBacklog, 1, at(0)), flighthistory.ChangeCreated, flighthistory.ActorUser, "create_flight", at(0))
	}
	removed := func(f flights.Flight) flighthistory.Event {
		return event(f, flighthistory.ChangeRemoved, flighthistory.ActorUser, "dispose_removed", at(130))
	}

	for _, tc := range []struct {
		name string
		ev   Evidence
		want Outcome
	}{
		{"completed beats undo, loss, and branch evidence", func() Evidence {
			ev := undoEvidence()
			ev.Flight = completedFlight()
			ev.Events = append(ev.Events, event(completedFlight(), flighthistory.ChangeUpdated, flighthistory.ActorPilot, "completion", at(90)))
			ev.Reconciliation = append(ev.Reconciliation, lost...)
			ev.GitState = branch
			return ev
		}(), OutcomeFlightCompleted},
		{"branch evidence beats undo and loss", func() Evidence {
			ev := undoEvidence()
			ev.Reconciliation = append(ev.Reconciliation, lost...)
			ev.GitState = branch
			return ev
		}(), OutcomeFinishedOutside},
		{"undo beats loss", func() Evidence {
			ev := undoEvidence()
			ev.Reconciliation = append(ev.Reconciliation, lost...)
			return ev
		}(), OutcomeManualUndoRecovery},
		{"loss beats early disposal", Evidence{Flight: executingFlight(), Reconciliation: lost, Events: []flighthistory.Event{removed(executingFlight())}}, OutcomeWorkspaceLostUnknown},
		{"early disposal of a started flight", Evidence{Flight: executingFlight(), Events: []flighthistory.Event{created(executingFlight()), removed(executingFlight())}}, OutcomeDisposedEarly},
		{"backlog flight removed", Evidence{Flight: backlogRow("fl-1"), Events: []flighthistory.Event{created(backlogRow("fl-1")), removed(backlogRow("fl-1"))}}, OutcomeBacklogRemoved},
		{"active", Evidence{Flight: executingFlight()}, OutcomeActive},
		{"completed then disposed is still completed", Evidence{Flight: completedFlight(), Events: []flighthistory.Event{removed(completedFlight())}}, OutcomeFlightCompleted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveSummary(tc.ev); got.PrimaryOutcome != tc.want {
				t.Errorf("outcome = %s, want %s", got.PrimaryOutcome, tc.want)
			}
		})
	}

	t.Run("reaching completing without review or commit evidence is not completion", func(t *testing.T) {
		f := executingFlight()
		f.WorkflowState = flights.StateCompleting
		if got := DeriveSummary(Evidence{Flight: f}); got.PrimaryOutcome == OutcomeFlightCompleted {
			t.Error("flight_completed without recorded review and commit evidence")
		}
	})
	t.Run("disposal after a completed result is reported as cleanup friction, not intervention", func(t *testing.T) {
		got := DeriveSummary(Evidence{Flight: completedFlight(), Events: []flighthistory.Event{removed(completedFlight())}})
		if !got.CleanupFriction || len(interventionsOfKind(got, KindDisposal)) != 0 {
			t.Errorf("cleanup friction = %v, disposal interventions = %v", got.CleanupFriction, interventionsOfKind(got, KindDisposal))
		}
	})
	t.Run("disposal before a result is an intervention", func(t *testing.T) {
		ev := Evidence{Flight: executingFlight(), Events: []flighthistory.Event{removed(executingFlight())}}
		got := DeriveSummary(ev)
		if len(interventionsOfKind(got, KindDisposal)) != 1 {
			t.Errorf("interventions = %+v, want one disposal", got.Interventions)
		}
	})
}

func TestDeriveSummaryLabelsLegacyCoverageAndMissingBoundaries(t *testing.T) {
	f := executingFlight()
	baseline := event(f, flighthistory.ChangeBaseline, flighthistory.ActorSystem, "baseline", at(100))
	transcripts := []Transcript{transcript("planner", "s-plan"), transcript("executor", "s-exec")}
	git := &GitState{VCS: "git", HeadSHA: "abc"}

	t.Run("metadata only", func(t *testing.T) {
		got := DeriveSummary(Evidence{Flight: f, Events: []flighthistory.Event{baseline}})
		if got.Coverage != CoverageMetadataOnly || !got.DataQuality.BaselineImported {
			t.Errorf("coverage = %s baseline = %v", got.Coverage, got.DataQuality.BaselineImported)
		}
		if len(got.PhasePath) != 1 || got.PhasePath[0].Visit != 3 || got.PhasePath[0].EnteredAt == nil || got.PhasePath[0].DurationSeconds != nil {
			t.Errorf("phase path = %+v, want only the observed visit, with no duration", got.PhasePath)
		}
		if len(got.DataQuality.MissingBoundaries) == 0 {
			t.Error("no missing-boundary item for the visits before the baseline")
		}
	})
	t.Run("linked legacy", func(t *testing.T) {
		got := DeriveSummary(Evidence{Flight: f, Events: []flighthistory.Event{baseline}, Transcripts: transcripts, GitState: git})
		if got.Coverage != CoverageLinkedLegacy {
			t.Errorf("coverage = %s", got.Coverage)
		}
	})
	t.Run("partial legacy names the missing transcript", func(t *testing.T) {
		got := DeriveSummary(Evidence{Flight: f, Events: []flighthistory.Event{baseline}, Transcripts: transcripts[:1]})
		if got.Coverage != CoveragePartialLegacy || !slices.Equal(got.DataQuality.MissingTranscripts, []string{"executor-s-exec"}) || !got.DataQuality.GitStateMissing {
			t.Errorf("coverage = %s missing = %v git missing = %v", got.Coverage, got.DataQuality.MissingTranscripts, got.DataQuality.GitStateMissing)
		}
	})
	t.Run("live capture still reports a missing transcript", func(t *testing.T) {
		created := event(inPhase(f, flights.StateBacklog, 1, at(0)), flighthistory.ChangeCreated, flighthistory.ActorUser, "create_flight", at(0))
		got := DeriveSummary(Evidence{Flight: f, Events: []flighthistory.Event{created}, Transcripts: transcripts[:1], GitState: git})
		if got.Coverage != CoverageLiveCapture || len(got.DataQuality.MissingTranscripts) != 1 {
			t.Errorf("coverage = %s missing = %v", got.Coverage, got.DataQuality.MissingTranscripts)
		}
	})
	t.Run("git collection error is surfaced", func(t *testing.T) {
		broken := &GitState{VCS: "git", Error: "workspace directory is not available"}
		got := DeriveSummary(Evidence{Flight: f, Events: []flighthistory.Event{baseline}, Transcripts: transcripts, GitState: broken})
		if got.DataQuality.GitStateError == "" || got.Coverage == CoverageLinkedLegacy {
			t.Errorf("git error = %q coverage = %s", got.DataQuality.GitStateError, got.Coverage)
		}
	})
	t.Run("evidence with no flight row is unlinked", func(t *testing.T) {
		got := DeriveSummary(Evidence{Transcripts: transcripts})
		if got.Coverage != CoverageUnlinked {
			t.Errorf("coverage = %s", got.Coverage)
		}
	})
	t.Run("malformed transcript lines are counted", func(t *testing.T) {
		bad := transcript("planner", "s-plan")
		bad.Malformed = []flighthistory.MalformedLine{{Line: 3, Bytes: []byte("x")}, {Line: 9, Bytes: []byte("y")}}
		got := DeriveSummary(Evidence{Flight: f, Transcripts: []Transcript{bad}})
		if got.DataQuality.MalformedTranscriptLines != 2 {
			t.Errorf("malformed transcript lines = %d, want 2", got.DataQuality.MalformedTranscriptLines)
		}
	})
	t.Run("recovered events are counted", func(t *testing.T) {
		got := DeriveSummary(Evidence{Flight: f, Events: []flighthistory.Event{baseline, event(f, flighthistory.ChangeRecovered, flighthistory.ActorSystem, "recovery", at(110))}})
		if got.DataQuality.RecoveredEvents != 1 {
			t.Errorf("recovered events = %d, want 1", got.DataQuality.RecoveredEvents)
		}
	})
}

func TestDeriveSummaryCountsFailuresByTaxonomy(t *testing.T) {
	f := executingFlight()
	f.Sessions[0].Outcome = flights.StatusFailed
	failedDispose := op("op-d", flights.ActionDispose, at(70), flights.StatusFailed, "")
	failedDispose.Error = "branch delete failed"
	failedOther := op("op-x", flights.ActionReview, at(71), flights.StatusFailed, "")
	failedOther.Error = "boom"
	f.Operations = append(f.Operations, failedDispose, failedOther)
	f.PilotStop = &flights.PilotStop{WorkflowState: flights.StateExecuting, Reason: "off the rails", SourceTurnID: "turn-1", CreatedAt: at(65)}
	f.Monitor = &flights.FlightMonitor{
		Messages: []flights.FlightMessage{{ID: "m1", Status: flights.MessageStatusFailed, UpdatedAt: at(30)}},
		Turns: []flights.MonitoredTurn{{
			ID:        "turn-1",
			Attempts:  []flights.MonitorAttempt{{Error: "jev unavailable"}},
			Artifacts: &flights.ArtifactReport{Error: "spec path outside docs"},
			Execution: &flights.ExecutionCheck{ReportError: "report is empty"},
			Review:    &flights.ReviewCheck{Skill: "test-rules-review not found"},
		}},
	}

	got := DeriveSummary(Evidence{Flight: f})

	want := map[string]int{
		"spawn_failure": 1, "delivery_failure": 1, "classifier_failure": 1, "report_verification_failure": 2,
		"review_skill_failure": 1, "manual_stop": 1, "disposal_failure": 1, "unclassified_error": 1,
	}
	if !reflect.DeepEqual(got.Counts.Failures, want) {
		t.Errorf("failures = %v, want %v", got.Counts.Failures, want)
	}
	if got.Counts.FailureTotal != 9 {
		t.Errorf("failure total = %d, want 9", got.Counts.FailureTotal)
	}

	t.Run("a report not yet written is not a failure", func(t *testing.T) {
		f := executingFlight()
		f.Monitor = &flights.FlightMonitor{Turns: []flights.MonitoredTurn{{ID: "turn-1", Execution: &flights.ExecutionCheck{ReportError: "not written yet"}}}}
		if got := DeriveSummary(Evidence{Flight: f}); got.Counts.FailureTotal != 0 {
			t.Errorf("failures = %v", got.Counts.Failures)
		}
	})
}

func TestDeriveSummaryMeasuresWaitFromFailedDeliveryToNextUserAction(t *testing.T) {
	f := executingFlight()
	f.Monitor = &flights.FlightMonitor{Messages: []flights.FlightMessage{{ID: "m1", Status: flights.MessageStatusFailed, CreatedAt: at(29), UpdatedAt: at(30)}}}
	f.Operations = append(f.Operations, op("op-retry", flights.ActionReview, at(45), flights.StatusSucceeded, ""))

	got := DeriveSummary(Evidence{Flight: f})
	if got.WaitingSeconds == nil || *got.WaitingSeconds != 15*60 {
		t.Errorf("waiting = %v, want %d", got.WaitingSeconds, 15*60)
	}

	f.Operations = f.Operations[:2]
	if got := DeriveSummary(Evidence{Flight: f}); got.WaitingSeconds != nil {
		t.Errorf("waiting = %d with no later user action, want unknown", *got.WaitingSeconds)
	}
}

func TestDeriveSummarySourceReferencesEveryDerivedField(t *testing.T) {
	ev := undoEvidence()
	ev.Transcripts = []Transcript{transcript("planner", "s-plan",
		userMsg("launch", "go", at(2).Add(-time.Second)),
		userMsg("chat-1", "use sqlite", at(61)),
	)}

	got := DeriveSummary(ev)

	for _, key := range SourceFieldKeys {
		if len(got.Source[key]) == 0 {
			t.Errorf("derived field %q has no source reference", key)
		}
	}
	for _, visit := range got.PhasePath {
		if len(visit.Sources) == 0 {
			t.Errorf("phase visit %d has no source", visit.Visit)
		}
	}
	for _, iv := range got.Interventions {
		if len(iv.Sources) == 0 {
			t.Errorf("intervention %s has no source", iv.ID)
		}
	}
	chatIV := interventionsOfKind(got, KindOperatorMessage)
	if len(chatIV) != 1 || chatIV[0].Sources[0].Kind != "chat_record" || chatIV[0].Sources[0].ID != "chat-1" || chatIV[0].Sources[0].Line != 2 {
		t.Errorf("operator message sources = %+v", chatIV)
	}
	// Even a bare row, with nothing else, names a source for every field.
	for _, key := range SourceFieldKeys {
		if len(DeriveSummary(Evidence{Flight: executingFlight()}).Source[key]) == 0 {
			t.Errorf("bare flight: field %q has no source reference", key)
		}
	}
}

func TestDeriveSummaryRebuildIsByteIdenticalForIdenticalEvidence(t *testing.T) {
	build := func() Evidence {
		eventSeq = 0 // event IDs are evidence: identical evidence needs identical IDs
		ev := undoEvidence()
		ev.Transcripts = []Transcript{transcript("planner", "s-plan", userMsg("chat-1", "use sqlite", at(61)))}
		ev.GitState = &GitState{VCS: "git", HeadSHA: "abc", Ahead: 1}
		return ev
	}
	first, err := json.Marshal(DeriveSummary(build()))
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(DeriveSummary(build()))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("rebuild differs:\n%s\n%s", first, second)
	}
	var decoded map[string]any
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatal(err)
	}
	for key := range decoded {
		if key == "generated_at" {
			t.Error("summary carries a wall-clock generated_at field")
		}
	}
}

func TestFindUnlinkedEvidenceReturnsCandidatesWithoutAttachingThem(t *testing.T) {
	isolate(t)
	row := startedRow("fl-1", flights.StateExecuting)
	row.Prompt = "Fix the login bug"
	assign(&row, flights.RolePlanner, "s-assigned")
	assigned := writeConversation(t, "ws-fl-1", "s-assigned", "Fix the login bug")
	writeConversation(t, "ws-fl-1", "s-orphan", "Fix the login bug, please")
	writeConversation(t, "ws-other", "s-unrelated", "what is the weather")
	manager := testManager(t, row)

	got, err := FindUnlinkedEvidence(t.Context(), manager)
	if err != nil {
		t.Fatalf("FindUnlinkedEvidence: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("candidates = %+v, want exactly the orphan conversation", got)
	}
	c := got[0]
	if c.SessionID != "s-orphan" || c.WorkspaceID != "ws-fl-1" || c.FlightID != "fl-1" || c.Confirmed {
		t.Errorf("candidate = %+v", c)
	}
	if !slices.Contains(c.MatchKinds, "workspace_id") || !slices.Contains(c.MatchKinds, "prompt") {
		t.Errorf("match kinds = %v, want workspace_id and prompt", c.MatchKinds)
	}
	if c.Excerpt != "Fix the login bug, please" || c.TranscriptLine != 1 || c.FirstObservedAt.IsZero() {
		t.Errorf("candidate evidence = %+v", c)
	}
	after := manager.List()
	if len(after) != 1 || len(after[0].Sessions) != 1 || after[0].Sessions[0].SessionID != "s-assigned" {
		t.Errorf("flight assignments changed: %+v", after[0].Sessions)
	}
	_ = assigned
}
