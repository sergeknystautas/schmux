package flightreview

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/flighthistory"
	"github.com/sergeknystautas/schmux/internal/flights"
)

// SummarySchemaVersion is the version of the derived summary format.
const SummarySchemaVersion = 1

// excerptLimit caps the verbatim operator text kept with an intervention.
const excerptLimit = 280

// phaseRank orders workflow states; a user-caused move to a lower rank is a
// manual undo.
func phaseRank(s flights.WorkflowState) int {
	switch s {
	case flights.StateBacklog:
		return 0
	case flights.StatePlanning:
		return 1
	case flights.StateExecuting:
		return 2
	case flights.StateReviewing:
		return 3
	case flights.StateCompleting:
		return 4
	}
	return -1
}

// observedRow is a ledger event with the state it recorded for the row.
type observedRow struct {
	event flighthistory.Event
	state flighthistory.RowState
}

// visitObservation is the first time a phase visit was seen.
type visitObservation struct {
	visit     uint64
	phase     flights.WorkflowState
	entered   *time.Time
	enteredBy flighthistory.Actor
	sources   []SourceReference
}

// DeriveSummary computes a flight's summary from its evidence alone. It reads
// no clock and no filesystem, and invents no timestamp, actor, duration, or
// count the evidence does not prove: unknown stays named in DataQuality.
func DeriveSummary(ev Evidence) Summary {
	f := ev.Flight
	s := Summary{SchemaVersion: SummarySchemaVersion, FlightID: f.ID}
	s.Counts.ByKind = map[InterventionKind]int{}
	s.Counts.Failures = map[string]int{}
	s.PhasePath = []PhaseVisit{}
	s.Interventions = []Intervention{}

	if f.ID == "" {
		s.Coverage = CoverageUnlinked
		ref := SourceReference{Kind: "evidence", ID: "none"}
		if len(ev.Transcripts) > 0 {
			ref = SourceReference{Kind: "transcript", ID: ev.Transcripts[0].Role + "-" + ev.Transcripts[0].SessionID}
		}
		s.Source = SourceReferences{}
		for _, key := range SourceFieldKeys {
			s.Source[key] = []SourceReference{ref}
		}
		return s
	}

	d := &deriver{ev: ev, f: f, s: &s, flightRef: SourceReference{Kind: "flight", ID: f.ID}}
	d.decodeEvents()
	d.derivePhases()
	d.findTakeoff()
	d.collectInterventions()
	d.deriveOutcome()
	d.deriveCounts()
	d.deriveCoverage()
	d.deriveWaiting()
	d.deriveSources()
	return s
}

type deriver struct {
	ev        Evidence
	f         flights.Flight
	s         *Summary
	flightRef SourceReference

	rows      []observedRow
	visits    []visitObservation
	takeoffAt *time.Time

	removed           bool
	removedEvent      *flighthistory.Event
	leftBacklog       bool
	workspaceLost     bool
	allSessionsGone   bool
	externalChange    bool
	reachedCompleting bool
	completed         bool
}

func (d *deriver) decodeEvents() {
	for _, e := range d.ev.Events {
		d.rows = append(d.rows, observedRow{event: e, state: e.State})
		switch e.Change {
		case flighthistory.ChangeBaseline:
			d.s.DataQuality.BaselineImported = true
		case flighthistory.ChangeRecovered:
			d.s.DataQuality.RecoveredEvents++
		case flighthistory.ChangeRemoved:
			d.removed = true
			event := e
			d.removedEvent = &event
		}
	}
	if d.ev.Manifest != nil && d.ev.Manifest.Final {
		d.removed = true
	}
}

func eventRef(e flighthistory.Event) SourceReference {
	return SourceReference{Kind: "event", ID: e.EventID}
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	t = t.UTC()
	return &t
}

// derivePhases builds the phase path from the rows the ledger observed, plus
// the flight row itself. A visit's duration needs both of its boundaries.
func (d *deriver) derivePhases() {
	seen := map[uint64]int{}
	observe := func(row flighthistory.RowState, actor flighthistory.Actor, ref SourceReference) {
		if row.PhaseVisit == 0 {
			return
		}
		if i, ok := seen[row.PhaseVisit]; ok {
			d.visits[i].sources = appendUnique(d.visits[i].sources, ref)
			return
		}
		var entered *time.Time
		if !row.PhaseTimeUncertain && row.PhaseStartedAt != nil {
			entered = timePtr(*row.PhaseStartedAt)
		}
		seen[row.PhaseVisit] = len(d.visits)
		d.visits = append(d.visits, visitObservation{
			visit: row.PhaseVisit, phase: flights.WorkflowState(row.WorkflowState), entered: entered, enteredBy: actor, sources: []SourceReference{ref},
		})
	}
	for _, r := range d.rows {
		observe(r.state, r.event.Actor, eventRef(r.event))
	}
	observe(d.f.HistoryState(), "", d.flightRef)
	sort.SliceStable(d.visits, func(i, j int) bool { return d.visits[i].visit < d.visits[j].visit })

	for _, v := range d.visits {
		if v.phase != flights.StateBacklog {
			d.leftBacklog = true
		}
	}
	if d.f.WorkflowState != flights.StateBacklog {
		d.leftBacklog = true
	}

	if len(d.visits) > 0 && d.visits[0].visit > 1 {
		d.s.DataQuality.MissingBoundaries = append(d.s.DataQuality.MissingBoundaries,
			fmt.Sprintf("phase visits 1-%d were not observed (before the first record)", d.visits[0].visit-1))
	}
	for i, v := range d.visits {
		pv := PhaseVisit{Phase: v.phase, Visit: v.visit, EnteredAt: v.entered, EnteredBy: v.enteredBy, Sources: v.sources}
		if v.entered == nil {
			d.s.DataQuality.MissingBoundaries = append(d.s.DataQuality.MissingBoundaries,
				fmt.Sprintf("phase visit %d: entry time is unknown", v.visit))
		}
		if i+1 < len(d.visits) {
			next := d.visits[i+1]
			switch {
			case next.visit != v.visit+1:
				d.s.DataQuality.MissingBoundaries = append(d.s.DataQuality.MissingBoundaries,
					fmt.Sprintf("phase visit %d: exit was not observed (visit %d is the next record)", v.visit, next.visit))
			case next.entered != nil:
				pv.LeftAt = next.entered
				if v.entered != nil && !next.entered.Before(*v.entered) {
					seconds := int64(next.entered.Sub(*v.entered) / time.Second)
					pv.DurationSeconds = &seconds
				}
			}
		}
		d.s.PhasePath = append(d.s.PhasePath, pv)
		if v.phase == flights.StateCompleting {
			d.reachedCompleting = true
		}
	}
	if d.f.WorkflowState == flights.StateCompleting {
		d.reachedCompleting = true
	}
}

// phaseAt names the visit the flight was in at t, when both are known.
func (d *deriver) phaseAt(t *time.Time) (flights.WorkflowState, uint64) {
	if t == nil {
		return "", 0
	}
	var phase flights.WorkflowState
	var visit uint64
	for _, v := range d.visits {
		if v.entered != nil && !v.entered.After(*t) {
			phase, visit = v.phase, v.visit
		}
	}
	return phase, visit
}

func (d *deriver) findTakeoff() {
	for _, o := range d.f.Operations {
		if o.Action != flights.ActionTakeoff || o.Status != flights.StatusSucceeded {
			continue
		}
		if t := timePtr(o.CreatedAt); t != nil && (d.takeoffAt == nil || t.Before(*d.takeoffAt)) {
			d.takeoffAt = t
		}
	}
}

func parseTS(ts string) *time.Time {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return nil
	}
	return timePtr(t)
}

func truncateExcerpt(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= excerptLimit {
		return text
	}
	cut := excerptLimit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

func (d *deriver) roleOf(sessionID string) (role string, assignment *flights.SessionAssignment) {
	for i := range d.f.Sessions {
		a := &d.f.Sessions[i]
		if a.SessionID == sessionID {
			return string(a.Role), a
		}
		for _, prior := range a.PriorSessionIDs {
			if prior == sessionID {
				return string(a.Role), a
			}
		}
	}
	return "", nil
}

func (d *deriver) add(iv Intervention) {
	if iv.Classification == "" {
		iv.Classification = "unclassified"
	}
	if iv.At != nil {
		if d.takeoffAt != nil && iv.At.Before(*d.takeoffAt) {
			return // the initial request, not later user work
		}
		iv.Phase, iv.PhaseVisit = d.phaseAt(iv.At)
	}
	d.s.Interventions = append(d.s.Interventions, iv)
}

func (d *deriver) collectInterventions() {
	d.operationInterventions()
	d.eventInterventions()
	d.reconciliationInterventions()
	d.transcriptInterventions()
	d.sortInterventions()
}

// operationInterventions counts user-started workflow operations. An
// operation with a source turn came from the pilot; takeoff is the initial
// request; disposal is handled with the ledger's removal event.
func (d *deriver) operationInterventions() {
	failedBefore := map[flights.OperationAction]bool{}
	ops := append([]flights.Operation(nil), d.f.Operations...)
	sort.SliceStable(ops, func(i, j int) bool { return ops[i].CreatedAt.Before(ops[j].CreatedAt) })
	for _, o := range ops {
		switch o.Action {
		case flights.ActionExecution, flights.ActionReview, flights.ActionNextReview, flights.ActionCompletion, flights.ActionDone:
		default:
			continue
		}
		retry := failedBefore[o.Action]
		if o.Status == flights.StatusFailed {
			failedBefore[o.Action] = true
		}
		if o.SourceTurnID != "" {
			continue
		}
		class := "workflow_override"
		if retry {
			class = "retry"
		}
		d.add(Intervention{
			ID: "iv-op-" + o.ID, Kind: KindManualRoute, At: timePtr(o.CreatedAt), Classification: class,
			Sources: []SourceReference{{Kind: "operation", ID: o.ID}},
		})
	}
}

func (d *deriver) eventInterventions() {
	var previous *observedRow
	for i := range d.rows {
		r := &d.rows[i]
		prev := previous
		previous = r
		if r.event.Actor != flighthistory.ActorUser {
			continue
		}
		if r.event.Change == flighthistory.ChangeRemoved {
			// The removed row is the flight's final state, so removal needs no
			// earlier row to know whether the flight ever left the backlog.
			if r.state.WorkflowState == string(flights.StateBacklog) && !d.leftBacklog {
				continue
			}
			// Disposal after a completed result is cleanup friction, decided
			// once the outcome is known.
			d.add(Intervention{
				ID: "iv-evt-" + r.event.EventID, Kind: KindDisposal, At: timePtr(r.event.RecordedAt),
				Sources: []SourceReference{eventRef(r.event)},
			})
			continue
		}
		if prev != nil && r.event.Change == flighthistory.ChangeUpdated && r.event.Cause == "edit_flight" {
			old, cur := prev.state, r.state
			if old.WorkflowState == string(flights.StateBacklog) {
				continue // authoring the request before takeoff
			}
			iv := Intervention{
				ID: "iv-evt-" + r.event.EventID, At: timePtr(r.event.RecordedAt), Classification: "workflow_override",
				Sources: []SourceReference{eventRef(r.event)},
			}
			oldRank, curRank := phaseRank(flights.WorkflowState(old.WorkflowState)), phaseRank(flights.WorkflowState(cur.WorkflowState))
			switch {
			case curRank < oldRank:
				iv.Kind = KindManualUndo
				iv.MissingSessionIDs = d.sessionsLostBetween(prev.event.RecordedAt, r.event.RecordedAt)
				for _, rec := range d.reconciliationBetween(prev.event.RecordedAt, r.event.RecordedAt) {
					iv.Sources = append(iv.Sources, SourceReference{Kind: "reconciliation", ID: rec.EventID})
				}
			case curRank > oldRank:
				iv.Kind = KindWorkflowEdit
			case cur.PlannerModel != old.PlannerModel || cur.ExecutorModel != old.ExecutorModel:
				iv.Kind = KindModelEdit
			default:
				iv.Kind = KindFlightEdit
				iv.Classification = "unclassified"
			}
			d.add(iv)
		}
	}
}

func (d *deriver) reconciliationBetween(after, until time.Time) []flighthistory.ReconciliationEvent {
	var out []flighthistory.ReconciliationEvent
	for _, rec := range d.ev.Reconciliation {
		if rec.Kind == flighthistory.ReconcileResourceMissing && rec.ResourceKind == "session" &&
			rec.RecordedAt.After(after) && !rec.RecordedAt.After(until) {
			out = append(out, rec)
		}
	}
	return out
}

func (d *deriver) sessionsLostBetween(after, until time.Time) []string {
	set := map[string]bool{}
	for _, rec := range d.reconciliationBetween(after, until) {
		set[rec.ResourceID] = true
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (d *deriver) reconciliationInterventions() {
	for _, rec := range d.ev.Reconciliation {
		ref := SourceReference{Kind: "reconciliation", ID: rec.EventID}
		switch {
		case rec.Kind == flighthistory.ReconcileResourceMissing && rec.ResourceKind == "session":
			role, _ := d.roleOf(rec.ResourceID)
			d.add(Intervention{
				ID: "iv-rec-" + rec.EventID, Kind: KindSessionKill, At: timePtr(rec.RecordedAt),
				SessionRole: role, SessionID: rec.ResourceID, Source: rec.Source, Sources: []SourceReference{ref},
			})
		case rec.Kind == flighthistory.ReconcileResourceMissing && rec.ResourceKind == "workspace":
			d.workspaceLost = true
		case rec.Kind == flighthistory.ReconcileWorkspaceChanged:
			d.externalChange = true
			d.add(Intervention{
				ID: "iv-rec-" + rec.EventID, Kind: KindExternalTakeover, At: timePtr(rec.RecordedAt),
				Source: rec.Source, Unconfirmed: true, Sources: []SourceReference{ref},
			})
		}
	}
}

// controlKind classifies a control line schmux sent a harness. Lines that are
// none of an interrupt, approval, or answer are not user work.
func controlKind(line json.RawMessage) (InterventionKind, string, bool) {
	var m struct {
		Type    string `json:"type"`
		Method  string `json:"method"`
		Request struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
		Response struct {
			Response struct {
				Behavior     string `json:"behavior"`
				UpdatedInput struct {
					Answers json.RawMessage `json:"answers"`
				} `json:"updatedInput"`
			} `json:"response"`
		} `json:"response"`
		Result struct {
			Decision string          `json:"decision"`
			Answers  json.RawMessage `json:"answers"`
		} `json:"result"`
	}
	if json.Unmarshal(line, &m) != nil {
		return "", "", false
	}
	switch {
	case m.Type == "control_request" && m.Request.Subtype == "interrupt", m.Method == "turn/interrupt":
		return KindInterrupt, "unclassified", true
	case m.Type == "control_response" && len(m.Response.Response.UpdatedInput.Answers) > 0:
		return KindAnswer, "question", true
	case m.Type == "control_response" && m.Response.Response.Behavior != "":
		return KindApproval, "permission", true
	case len(m.Result.Answers) > 0:
		return KindAnswer, "question", true
	case m.Result.Decision != "":
		return KindApproval, "permission", true
	}
	return "", "", false
}

func (d *deriver) transcriptInterventions() {
	pilotMessages := map[string]bool{}
	if d.f.Monitor != nil {
		for _, m := range d.f.Monitor.Messages {
			if m.ChatMessageID != "" {
				pilotMessages[m.ChatMessageID] = true
			}
		}
	}
	for _, tr := range d.ev.Transcripts {
		role, assignment := d.roleOf(tr.SessionID)
		if role == "" {
			role = tr.Role
		}
		d.s.DataQuality.MalformedTranscriptLines += len(tr.Malformed)
		seenUser := false
		for _, rec := range tr.Records {
			r := rec.Record
			at := parseTS(r.Ts)
			ref := SourceReference{Kind: "chat_record", ID: r.ID, Line: rec.Line}
			if r.ID == "" {
				ref.ID = fmt.Sprintf("%s#%d", tr.SessionID, rec.Seq)
			}
			iv := Intervention{
				ID: fmt.Sprintf("iv-chat-%s-%d", tr.SessionID, rec.Seq), At: at, SessionRole: role, SessionID: tr.SessionID,
				ChatRecordID: r.ID, Sources: []SourceReference{ref},
			}
			switch r.Type {
			case chat.RecordUserMessage:
				first := !seenUser
				seenUser = true
				if pilotMessages[r.ID] {
					d.s.Counts.PilotMessagesExcluded++
					continue
				}
				// The launch prompt is written before the spawn that
				// carries it returns, so it precedes the moment the flight
				// recorded the session.
				if first && assignment != nil && at != nil && !at.After(assignment.UpdatedAt) {
					d.s.Counts.LaunchPromptsExcluded++
					continue
				}
				if at == nil {
					d.s.DataQuality.MissingBoundaries = append(d.s.DataQuality.MissingBoundaries,
						fmt.Sprintf("chat record %s has no parseable timestamp", ref.ID))
				}
				iv.Kind, iv.Excerpt = KindOperatorMessage, truncateExcerpt(r.Text)
			case chat.RecordControl:
				kind, class, ok := controlKind(r.Line)
				if !ok {
					continue
				}
				iv.Kind, iv.Classification = kind, class
			case chat.RecordUserMessageQueue:
				iv.Kind = KindQueueChange
			case chat.RecordClaudeTakeover:
				iv.Kind = KindExternalTakeover
			default:
				continue
			}
			d.add(iv)
		}
	}
}

func (d *deriver) sortInterventions() {
	ivs := d.s.Interventions
	sort.SliceStable(ivs, func(i, j int) bool {
		a, b := ivs[i], ivs[j]
		switch {
		case a.At == nil && b.At != nil:
			return false
		case a.At != nil && b.At == nil:
			return true
		case a.At != nil && !a.At.Equal(*b.At):
			return a.At.Before(*b.At)
		}
		return a.ID < b.ID
	})
	var previous *time.Time
	for i := range ivs {
		if ivs[i].At == nil {
			continue
		}
		if d.takeoffAt != nil {
			seconds := int64(ivs[i].At.Sub(*d.takeoffAt) / time.Second)
			ivs[i].ElapsedSinceTakeoff = &seconds
		}
		if previous != nil {
			seconds := int64(ivs[i].At.Sub(*previous) / time.Second)
			ivs[i].ElapsedSincePrevious = &seconds
		}
		previous = ivs[i].At
	}
}

// deriveOutcome picks the primary outcome by precedence and the orphan flag.
func (d *deriver) deriveOutcome() {
	f := d.f
	reviewed, committed := false, false
	for _, o := range f.Operations {
		if o.Status != flights.StatusSucceeded {
			continue
		}
		reviewed = reviewed || o.Action == flights.ActionReview || o.Action == flights.ActionNextReview
		committed = committed || o.Action == flights.ActionCompletion
	}
	for _, a := range f.Sessions {
		reviewed = reviewed || a.Role == flights.RoleReviewer
		committed = committed || (a.Role == flights.RoleCommit && a.Outcome == flights.StatusSucceeded)
	}
	d.completed = d.reachedCompleting && reviewed && committed

	current := map[string]bool{}
	for _, a := range f.Sessions {
		if a.SessionID != "" && a.Outcome == flights.StatusSucceeded {
			current[a.SessionID] = true
		}
	}
	lostSessions := map[string]bool{}
	for _, rec := range d.ev.Reconciliation {
		if rec.Kind == flighthistory.ReconcileResourceMissing && rec.ResourceKind == "session" {
			lostSessions[rec.ResourceID] = true
		}
	}
	d.allSessionsGone = len(current) > 0
	for id := range current {
		if !lostSessions[id] {
			d.allSessionsGone = false
		}
	}
	d.s.OrphanedActive = !d.removed && d.leftBacklog && (d.workspaceLost || d.allSessionsGone)

	branchEvidence := d.ev.GitState != nil && d.ev.GitState.Error == "" && d.ev.GitState.HeadSHA != "" &&
		(d.ev.GitState.Ahead > 0 || d.ev.GitState.RemoteBranchExists)
	// Commits on a healthy active branch are the flight's own work; branch
	// evidence only suggests an outside finish once something else says the
	// flight lost its workspace or sessions, or someone worked there.
	outsideSignal := d.workspaceLost || d.removed || d.externalChange || d.s.OrphanedActive

	candidates := map[Outcome]bool{OutcomeActive: true}
	if d.completed {
		candidates[OutcomeFlightCompleted] = true
	}
	if branchEvidence && !d.reachedCompleting && d.leftBacklog && outsideSignal {
		candidates[OutcomeFinishedOutside] = true
	}
	for _, iv := range d.s.Interventions {
		if iv.Kind == KindManualUndo {
			candidates[OutcomeManualUndoRecovery] = true
		}
	}
	if d.workspaceLost {
		candidates[OutcomeWorkspaceLostUnknown] = true
	}
	if d.removed && d.leftBacklog {
		candidates[OutcomeDisposedEarly] = true
	}
	if d.removed && !d.leftBacklog {
		candidates[OutcomeBacklogRemoved] = true
	}
	d.s.PrimaryOutcome = choosePrimary(candidates)
	d.s.OutcomePendingUser = d.s.PrimaryOutcome == OutcomeFinishedOutside
	d.s.CleanupFriction = d.removed && d.completed

	if d.completed {
		// Disposing a finished flight is cleanup friction, not an intervention.
		kept := d.s.Interventions[:0]
		for _, iv := range d.s.Interventions {
			if iv.Kind != KindDisposal {
				kept = append(kept, iv)
			}
		}
		d.s.Interventions = kept
	}
}

// choosePrimary returns the first outcome in precedence order that applies.
func choosePrimary(candidates map[Outcome]bool) Outcome {
	for _, outcome := range outcomePrecedence {
		if candidates[outcome] {
			return outcome
		}
	}
	return OutcomeActive
}

func (d *deriver) deriveCounts() {
	c := &d.s.Counts
	c.Interventions = len(d.s.Interventions)
	for _, iv := range d.s.Interventions {
		c.ByKind[iv.Kind]++
		if iv.Classification == "unclassified" {
			c.Unclassified++
		}
		switch iv.Kind {
		case KindManualUndo:
			c.ManualUndos++
		case KindSessionKill:
			c.SessionsKilled++
		}
	}
	for _, a := range d.f.Sessions {
		if a.Role == flights.RoleReviewer {
			if a.SessionID != "" {
				c.Reviewers++
			}
			c.Reviewers += len(a.PriorSessionIDs)
		}
		c.SessionReplacements += len(a.PriorSessionIDs)
	}
	c.PhaseVisits = len(d.s.PhasePath)
	d.countLabels()
	d.countFailures()
}

// countLabels attaches each operator message's newest successful Jev label.
// Messages with none stay unlabeled; unclear and mixed keep their own buckets.
func (d *deriver) countLabels() {
	latest := latestLabels(d.ev.Classifications)
	c := &d.s.Counts
	c.ByLabel = map[string]int{}
	for i := range d.s.Interventions {
		iv := &d.s.Interventions[i]
		if iv.Kind != KindOperatorMessage {
			continue
		}
		attempt, ok := latest[iv.ID]
		if !ok {
			c.Unlabeled++
			continue
		}
		iv.ChatLabel = attempt.Choice
		c.ByLabel[string(attempt.Choice)]++
		if attempt.Confidence != nil && *attempt.Confidence < LowConfidenceThreshold {
			c.LowConfidence++
		}
		iv.Sources = appendUnique(iv.Sources, SourceReference{Kind: "classification", ID: attempt.ID})
	}
}

func (d *deriver) countFailures() {
	failures := d.s.Counts.Failures
	assignmentSpawns, opSpawns := 0, 0
	for _, a := range d.f.Sessions {
		if a.Outcome == flights.StatusFailed {
			assignmentSpawns++
		}
	}
	for _, o := range d.f.Operations {
		if o.Status != flights.StatusFailed {
			continue
		}
		spawn := false
		for _, r := range o.Resources {
			if r.Kind == "session" && r.Status == flights.StatusFailed {
				spawn = true
			}
		}
		switch {
		case o.Action == flights.ActionDispose:
			failures["disposal_failure"]++
		case spawn:
			opSpawns++
		default:
			failures["unclassified_error"]++
		}
	}
	// A failed spawn is recorded on the operation and the assignment; count
	// it once.
	if n := max(assignmentSpawns, opSpawns); n > 0 {
		failures["spawn_failure"] = n
	}
	if m := d.f.Monitor; m != nil {
		for _, msg := range m.Messages {
			if msg.Status == flights.MessageStatusFailed {
				failures["delivery_failure"]++
			}
		}
		for _, t := range m.Turns {
			for _, a := range t.Attempts {
				if a.Error != "" {
					failures["classifier_failure"]++
				}
			}
			if t.Artifacts != nil && t.Artifacts.Error != "" {
				failures["report_verification_failure"]++
			}
			if t.Execution != nil && t.Execution.ReportError != "" && t.Execution.ReportError != "not written yet" {
				failures["report_verification_failure"]++
			}
			if t.Review != nil && strings.HasSuffix(t.Review.Skill, " not found") {
				failures["review_skill_failure"]++
			}
		}
	}
	if d.f.PilotStop != nil {
		failures["manual_stop"]++
	}
	for _, n := range failures {
		d.s.Counts.FailureTotal += n
	}
}

// deriveCoverage labels how much evidence stands behind the summary.
func (d *deriver) deriveCoverage() {
	dq := &d.s.DataQuality
	available := map[string]bool{}
	for _, tr := range d.ev.Transcripts {
		available[tr.Role+"-"+tr.SessionID] = true
	}
	if d.f.Workspace != nil {
		expected := map[string]bool{}
		for _, a := range d.f.Sessions {
			for _, id := range append([]string{a.SessionID}, a.PriorSessionIDs...) {
				if id != "" {
					expected[string(a.Role)+"-"+id] = true
				}
			}
		}
		for key := range expected {
			if !available[key] {
				dq.MissingTranscripts = append(dq.MissingTranscripts, key)
			}
		}
		sort.Strings(dq.MissingTranscripts)
	}
	gitOK := d.ev.GitState != nil && d.ev.GitState.Error == ""
	if d.f.Workspace != nil {
		dq.GitStateMissing = d.ev.GitState == nil
		if d.ev.GitState != nil {
			dq.GitStateError = d.ev.GitState.Error
		}
	}
	linked := len(available)
	if gitOK {
		linked++
	}
	complete := len(dq.MissingTranscripts) == 0 && (d.f.Workspace == nil || gitOK)
	switch {
	case len(d.rows) > 0 && d.rows[0].event.Change == flighthistory.ChangeCreated:
		d.s.Coverage = CoverageLiveCapture
	case linked == 0:
		d.s.Coverage = CoverageMetadataOnly
	case complete:
		d.s.Coverage = CoverageLinkedLegacy
	default:
		d.s.Coverage = CoveragePartialLegacy
	}
}

// deriveWaiting measures the union of intervals from a failed delivery to the
// next counted user action. With no such pair the wait is unknown, not zero.
func (d *deriver) deriveWaiting() {
	if d.f.Monitor == nil {
		return
	}
	type span struct{ from, to time.Time }
	var spans []span
	for _, msg := range d.f.Monitor.Messages {
		if msg.Status != flights.MessageStatusFailed || msg.UpdatedAt.IsZero() {
			continue
		}
		for _, iv := range d.s.Interventions {
			if iv.At != nil && iv.At.After(msg.UpdatedAt) {
				spans = append(spans, span{msg.UpdatedAt, *iv.At})
				break
			}
		}
	}
	if len(spans) == 0 {
		return
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].from.Before(spans[j].from) })
	var total time.Duration
	cur := spans[0]
	for _, sp := range spans[1:] {
		if sp.from.After(cur.to) {
			total += cur.to.Sub(cur.from)
			cur = sp
			continue
		}
		if sp.to.After(cur.to) {
			cur.to = sp.to
		}
	}
	total += cur.to.Sub(cur.from)
	seconds := int64(total / time.Second)
	d.s.WaitingSeconds = &seconds
}

func appendUnique(refs []SourceReference, ref SourceReference) []SourceReference {
	for _, existing := range refs {
		if existing == ref {
			return refs
		}
	}
	return append(refs, ref)
}

// deriveSources names the evidence behind every derived field.
func (d *deriver) deriveSources() {
	src := SourceReferences{}
	add := func(key string, refs ...SourceReference) {
		for _, ref := range refs {
			src[key] = appendUnique(src[key], ref)
		}
	}
	manifestRef := SourceReference{Kind: "manifest", ID: d.f.ID}
	add(FieldCoverage, d.flightRef)
	if d.ev.Manifest != nil {
		add(FieldCoverage, manifestRef)
		add(FieldDataQuality, manifestRef)
	}
	if len(d.rows) > 0 {
		add(FieldCoverage, eventRef(d.rows[0].event))
	}
	for _, tr := range d.ev.Transcripts {
		add(FieldCoverage, SourceReference{Kind: "transcript", ID: tr.Role + "-" + tr.SessionID})
	}
	if d.ev.GitState != nil {
		gitRef := SourceReference{Kind: "git_state", ID: d.f.ID}
		add(FieldCoverage, gitRef)
		add(FieldOutcome, gitRef)
	}
	add(FieldOutcome, d.flightRef)
	if d.removedEvent != nil {
		add(FieldOutcome, eventRef(*d.removedEvent))
	}
	for _, rec := range d.ev.Reconciliation {
		ref := SourceReference{Kind: "reconciliation", ID: rec.EventID}
		add(FieldOutcome, ref)
		add(FieldOrphaned, ref)
	}
	add(FieldOrphaned, d.flightRef)
	for _, v := range d.s.PhasePath {
		add(FieldPhasePath, v.Sources...)
	}
	add(FieldPhasePath, d.flightRef)
	for _, iv := range d.s.Interventions {
		add(FieldInterventions, iv.Sources...)
		add(FieldCounts, iv.Sources...)
	}
	add(FieldInterventions, d.flightRef)
	add(FieldCounts, d.flightRef)
	add(FieldDataQuality, d.flightRef)
	add(FieldWaitingSeconds, d.flightRef)
	if d.f.Monitor != nil {
		for _, msg := range d.f.Monitor.Messages {
			if msg.Status == flights.MessageStatusFailed {
				add(FieldWaitingSeconds, SourceReference{Kind: "flight_message", ID: msg.ID})
			}
		}
	}
	d.s.Source = src
}

// ApplyDecisions overlays the user's confirmed decisions onto a derived
// summary and returns the result. The summary passed in is not modified, so
// the derived view stays reproducible from evidence alone.
func ApplyDecisions(s Summary, decisions DecisionContext) Summary {
	if s.PrimaryOutcome == OutcomeFinishedOutside && s.OutcomePendingUser && decisions.ConfirmedExternalCompletion[s.FlightID] {
		s.OutcomePendingUser = false
		s.ConfirmedByUser = true
	}
	return s
}
