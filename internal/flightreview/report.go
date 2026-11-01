package flightreview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"sort"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/flighthistory"
	"github.com/sergeknystautas/schmux/internal/flights"
)

// Cohort says which flights a report covers. The two rules are never mixed
// without a label: a report names exactly the one it used.
type Cohort string

const (
	// CohortDisposed is flights the ledger recorded as removed in the range.
	CohortDisposed Cohort = "disposed"
	// CohortActivity is flights removed in the range plus any flight, active or
	// disposed, with recorded activity in it.
	CohortActivity Cohort = "activity"
)

// ReportRequest selects a report. Since is inclusive and Until is exclusive.
type ReportRequest struct {
	Since  time.Time
	Until  time.Time
	Cohort Cohort
	// PreviousDecisions is the text of the newest decisions file, if any.
	PreviousDecisions string
}

// Report is a rendered review plus the counts the caller may want to log.
type Report struct {
	HTML             []byte
	FlightCount      int
	CoveredCount     int
	DataQualityCount int
}

// flightRecord is everything the report knows about one flight.
type flightRecord struct {
	Summary Summary
	Flight  flights.Flight
	Git     *GitState
	// Removed is when the ledger recorded the flight's removal.
	Removed *time.Time
	// Active lists the times of recorded activity that was not the system
	// importing or recovering rows.
	Active                   []time.Time
	FailedClassifications    int
	MalformedClassifications int
}

// ledgerQuality describes the evidence stores as a whole.
type ledgerQuality struct {
	MalformedEvents          int
	MalformedReconciliation  int
	MalformedClassifications int
	FailedClassifications    int
	UnlinkedCandidates       []UnlinkedCandidate
	RefreshErrors            []string
}

// SetFlights attaches the flights manager the service reads when it renders a
// report. The daemon builds the service before the manager, so it is attached
// afterwards.
func (s *Service) SetFlights(manager *flights.Manager) { s.flights = manager }

// RenderReport refreshes the archives, reconciles resources, labels any
// unlabeled operator messages, rebuilds every summary, and renders the
// evidence-first review for the requested cohort. A failure to refresh or to
// classify is shown in the report's data-quality section instead of stopping
// it.
func (s *Service) RenderReport(ctx context.Context, req ReportRequest) (Report, error) {
	if req.Cohort != CohortDisposed && req.Cohort != CohortActivity {
		return Report{}, fmt.Errorf("unknown cohort %q: use %q or %q", req.Cohort, CohortDisposed, CohortActivity)
	}
	if req.Since.IsZero() || req.Until.IsZero() || !req.Until.After(req.Since) {
		return Report{}, errors.New("report range must have a start before its end")
	}
	if s.flights == nil {
		return Report{}, errors.New("flights are unavailable; no manager is attached to the review service")
	}

	var quality ledgerQuality
	note := func(format string, args ...any) {
		quality.RefreshErrors = append(quality.RefreshErrors, fmt.Sprintf(format, args...))
	}
	if err := s.ImportLegacy(ctx, s.flights); err != nil {
		note("archive refresh: %v", err)
	}
	if _, err := s.Reconcile(s.flights); err != nil {
		note("reconciliation: %v", err)
	}
	if err := s.RebuildSummaries(ctx, s.flights); err != nil {
		note("summary rebuild: %v", err)
	}
	records, err := s.loadRecords()
	if err != nil {
		return Report{}, err
	}
	cohort := selectCohort(records, req)
	for _, r := range cohort {
		if err := s.ClassifyInterventions(ctx, r.Summary.FlightID, false); err != nil {
			note("classification of %s: %v", r.Summary.FlightID, err)
		}
	}
	if len(cohort) > 0 {
		if records, err = s.loadRecords(); err != nil {
			return Report{}, err
		}
	}

	l, err := readLedgers()
	if err != nil {
		return Report{}, err
	}
	quality.MalformedEvents, quality.MalformedReconciliation = len(l.malformedEvents), len(l.malformedReconciliation)
	if unlinked, err := FindUnlinkedEvidence(ctx, s.flights); err != nil {
		note("unlinked evidence scan: %v", err)
	} else {
		quality.UnlinkedCandidates = unlinked
	}

	model := buildReport(records, quality, parseDecisions(req.PreviousDecisions), req)
	data, err := model.render()
	if err != nil {
		return Report{}, err
	}
	return Report{HTML: data, FlightCount: len(model.Rows), CoveredCount: model.interventionCovered, DataQualityCount: model.findings}, nil
}

// loadRecords reads every flight's summary, row, git state, and ledger times.
func (s *Service) loadRecords() ([]flightRecord, error) {
	l, err := readLedgers()
	if err != nil {
		return nil, err
	}
	live := map[string]flights.Flight{}
	for _, f := range s.flights.List() {
		live[f.ID] = f
	}
	ids, err := archivedFlightIDs()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	for id := range live {
		if !seen[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	var records []flightRecord
	for _, id := range ids {
		var row *flights.Flight
		if f, ok := live[id]; ok {
			row = &f
		}
		ev, err := loadEvidence(id, l, row)
		if err != nil {
			return nil, fmt.Errorf("flight %s: %w", id, err)
		}
		if ev.Flight.ID == "" {
			continue
		}
		summary := DeriveSummary(ev)
		rec := flightRecord{Summary: summary, Flight: ev.Flight, Git: ev.GitState}
		for _, e := range ev.Events {
			switch e.Change {
			case flighthistory.ChangeRemoved:
				at := e.RecordedAt
				rec.Removed = &at
			case flighthistory.ChangeBaseline, flighthistory.ChangeRecovered:
			default:
				rec.Active = append(rec.Active, e.RecordedAt)
			}
		}
		for _, iv := range summary.Interventions {
			if iv.At != nil {
				rec.Active = append(rec.Active, *iv.At)
			}
		}
		if !ev.Flight.UpdatedAt.IsZero() {
			rec.Active = append(rec.Active, ev.Flight.UpdatedAt)
		}
		latest := latestLabels(ev.Classifications)
		for _, a := range ev.Classifications {
			if a.Error != "" {
				if _, ok := latest[a.InterventionEventID]; !ok {
					rec.FailedClassifications++
				}
			}
		}
		_, malformed, err := readClassifications(id)
		if err != nil {
			return nil, err
		}
		rec.MalformedClassifications = len(malformed)
		records = append(records, rec)
	}
	return records, nil
}

func inRange(t time.Time, req ReportRequest) bool {
	return !t.Before(req.Since) && t.Before(req.Until)
}

// selectCohort applies the request's inclusion rule.
func selectCohort(records []flightRecord, req ReportRequest) []flightRecord {
	var out []flightRecord
	for _, r := range records {
		include := r.Removed != nil && inRange(*r.Removed, req)
		if !include && req.Cohort == CohortActivity {
			for _, t := range r.Active {
				if inRange(t, req) {
					include = true
					break
				}
			}
		}
		if include {
			out = append(out, r)
		}
	}
	return out
}

// parsedDecisions is the part of a decisions file a report reads.
type parsedDecisions struct {
	// Unresolved are items left as needs-data or deferred.
	Unresolved []carriedItem
	// ConfirmedExternal lists flights the user confirmed finished outside.
	ConfirmedExternal map[string]bool
	// ConfirmedMechanisms lists candidates the user confirmed a mechanism for.
	ConfirmedMechanisms map[string]bool
}

type carriedItem struct {
	Status string
	Text   string
}

// parseDecisions reads the Markdown the /flight-review skill writes: bullets
// under "Needs data" and "Deferred" are unresolved; bullets under the two
// "Confirmed" headings supply the overlay.
func parseDecisions(md string) parsedDecisions {
	d := parsedDecisions{ConfirmedExternal: map[string]bool{}, ConfirmedMechanisms: map[string]bool{}}
	section := ""
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(line)
		if heading, ok := strings.CutPrefix(line, "##"); ok {
			section = strings.ToLower(strings.TrimSpace(strings.TrimLeft(heading, "#")))
			continue
		}
		item, ok := strings.CutPrefix(line, "- ")
		if !ok {
			continue
		}
		item = strings.TrimSpace(item)
		id, _, _ := strings.Cut(item, ":")
		id = strings.TrimSpace(id)
		if fields := strings.Fields(id); len(fields) > 0 {
			id = fields[0]
		}
		switch {
		case section == "needs data":
			d.Unresolved = append(d.Unresolved, carriedItem{"Needs data", item})
		case section == "deferred":
			d.Unresolved = append(d.Unresolved, carriedItem{"Deferred", item})
		case strings.HasPrefix(section, "confirmed external"):
			if id == "" {
				continue
			}
			d.ConfirmedExternal[id] = true
		case strings.HasPrefix(section, "confirmed mechanism"):
			if id == "" {
				continue
			}
			d.ConfirmedMechanisms[id] = true
		}
	}
	return d
}

// --- view model ---

// ratio is an aggregate with its denominators: how many flights it counts,
// how many had the evidence it needs, and how many the cohort holds.
type ratio struct {
	Name, Label          string
	Num, Covered, Cohort int
}

func (r ratio) Text() string {
	return fmt.Sprintf("%d of %d covered (%d in cohort)", r.Num, r.Covered, r.Cohort)
}

type qualityLine struct {
	Label string
	Value string
	Ratio *ratio
}

type coverageRow struct {
	Name string
	N    int
}

type phaseDurationRow struct {
	Phase    string
	Ratio    ratio
	P50, P90 string
}

type cohortRow struct {
	ID, Name, Repo, Outcome, Coverage, Duration, PhasePath, Waiting string
	Orphaned                                                        bool
	Interventions, Reviewers, Failures                              int
}

type orphanRow struct {
	ID, Name, Workspace, Branch, Evidence, Outcome, Action string
}

type interventionRow struct {
	Flight, At, Elapsed, Phase, Kind, Classification, Label, Who, Excerpt, Evidence string
}

type bottleneckRow struct {
	Key, Title     string
	Affected       ratio
	Interventions  int
	Representative []string
}

type candidateRow struct {
	ID, Title     string
	Affected      ratio
	Evidence      []string
	Confirmed     bool
	Experiment    string
	Removes       string
	NewFailure    string
	CounterMetric string
}

type exceptionRow struct {
	Subject, Finding, Evidence string
}

type agendaRow struct {
	N      int
	ID     string
	Title  string
	Status string // empty for new candidates, else how a previous review left it
	Detail string
}

type reportModel struct {
	Title      string
	Range      string
	CohortRule string

	CohortN             int
	Quality             []qualityLine
	Coverage            []coverageRow
	Aggregates          []ratio
	Funnel              []coverageRow
	Durations           []phaseDurationRow
	Rows                []cohortRow
	Orphans             []orphanRow
	Interventions       []interventionRow
	Bottlenecks         []bottleneckRow
	Candidates          []candidateRow
	Exceptions          []exceptionRow
	Agenda              []agendaRow
	RefreshErrors       []string
	interventionCovered int
	findings            int
}

// interventionCovered reports whether a flight's intervention evidence is
// complete enough to count it.
func interventionCovered(s Summary) bool {
	return s.Coverage != CoverageMetadataOnly && s.Coverage != CoverageUnlinked && len(s.DataQuality.MissingTranscripts) == 0
}

func durationText(seconds int64) string {
	d := time.Duration(seconds) * time.Second
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", seconds)
}

func percentile(sorted []int64, p float64) int64 {
	idx := int(float64(len(sorted))*p+0.999999) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

var phaseOrder = []flights.WorkflowState{
	flights.StateBacklog, flights.StatePlanning, flights.StateExecuting, flights.StateReviewing, flights.StateCompleting,
}

// groupKey names the bucket an intervention falls into for bottlenecks and
// candidates: the Jev label for operator messages, the action otherwise.
func groupKey(iv Intervention) (key, title string) {
	if iv.Kind == KindOperatorMessage {
		if iv.ChatLabel == "" {
			return "unlabeled", "Operator messages without a label"
		}
		return string(iv.ChatLabel), "Chat: " + string(iv.ChatLabel)
	}
	return "kind-" + string(iv.Kind), "Action: " + string(iv.Kind)
}

type candidateText struct{ Experiment, Removes, NewFailure, CounterMetric string }

// candidateTexts are starting points for the meeting, not findings: the
// evidence is the repeated pattern, and the user decides what to try.
var candidateTexts = map[string]candidateText{
	"product_guidance":    {"Ask the clarifying questions the user keeps answering during planning, for the repos affected.", "Mid-flight requirement answers.", "Slower planning and more questions before takeoff.", "Time from prompt to takeoff; intervention-free completion rate."},
	"routing_correction":  {"Have the pilot confirm the recipient role before sending guidance.", "Messages the user redirects to the right role.", "Guidance delayed by a confirmation step.", "Pilot message delivery failures; time to first guidance."},
	"workflow_correction": {"Offer an explicit undo or next-reviewer step in the pilot's options.", "Manual undo and phase overrides.", "The pilot rewinds a phase too eagerly.", "Manual undo count; phase revisit count."},
	"environment_unblock": {"Detect the blocking permission, tool, or credential before the agent starts.", "Messages that unblock the environment.", "A preflight check that blocks healthy flights.", "Time to first agent action; spawn failures."},
	"approval":            {"Pre-approve the gated actions the user keeps approving, for a bounded scope.", "Explicit permission approvals.", "An agent takes a destructive action without a human check.", "Destructive actions taken; reverted changes."},
	"status_query":        {"Surface the pilot's current status where the user already looks.", "Messages asking what happened.", "More notifications to read.", "Status messages per flight; unread notices."},
	"manual_takeover":     {"Find why the user stopped trusting the agent at those points and address the first cause.", "Work the user finished by hand.", "Hiding a real quality problem.", "Flights finished outside the workflow; review findings."},
	"unlabeled":           {"Classify these messages before deciding anything.", "Unknown until labeled.", "Acting on an unexamined pattern.", "Share of operator messages labeled."},
}

var kindText = candidateText{
	"Find what prompted this user action and whether the pilot or monitor could have made it.",
	"The counted user action.",
	"The automation acts where the user would have declined.",
	"Intervention-free completion rate; manual undo count.",
}

func textsFor(key string) candidateText {
	if t, ok := candidateTexts[key]; ok {
		return t
	}
	return kindText
}

func affectedOf(records []flightRecord, key string) (flightsAffected []string, ivs []Intervention) {
	for _, r := range records {
		hit := false
		for _, iv := range r.Summary.Interventions {
			if k, _ := groupKey(iv); k == key {
				ivs = append(ivs, iv)
				hit = true
			}
		}
		if hit {
			flightsAffected = append(flightsAffected, r.Summary.FlightID)
		}
	}
	return flightsAffected, ivs
}

func evidenceIDs(ivs []Intervention, limit int) []string {
	var ids []string
	for _, iv := range ivs {
		if len(ids) == limit {
			break
		}
		ids = append(ids, iv.ID)
	}
	return ids
}

func refText(refs []SourceReference) string {
	parts := make([]string, len(refs))
	for i, ref := range refs {
		parts[i] = ref.Kind + ":" + ref.ID
		if ref.Line > 0 {
			parts[i] += fmt.Sprintf("@%d", ref.Line)
		}
	}
	return strings.Join(parts, " ")
}

// buildReport computes every section from the cohort's summaries. Pure: it
// reads no clock and no filesystem.
func buildReport(all []flightRecord, q ledgerQuality, dec parsedDecisions, req ReportRequest) reportModel {
	records := selectCohort(all, req)
	overlay := DecisionContext{ConfirmedExternalCompletion: dec.ConfirmedExternal}
	for i := range records {
		records[i].Summary = ApplyDecisions(records[i].Summary, overlay)
	}
	m := reportModel{
		Title:         "Flight retrospective",
		Range:         req.Since.UTC().Format("2006-01-02") + " to " + req.Until.UTC().Add(-time.Second).Format("2006-01-02"),
		CohortN:       len(records),
		RefreshErrors: q.RefreshErrors,
	}
	switch req.Cohort {
	case CohortDisposed:
		m.CohortRule = "Disposed in range: flights the ledger recorded as removed between these dates."
	default:
		m.CohortRule = "Activity in range: flights removed in range, plus active or disposed flights with recorded activity in range."
	}
	n := len(records)

	// Covered denominators.
	covered, linked, notActive := 0, 0, 0
	for _, r := range records {
		s := r.Summary
		if interventionCovered(s) {
			covered++
		}
		if s.Coverage != CoverageUnlinked {
			linked++
		}
		if s.PrimaryOutcome != OutcomeActive {
			notActive++
		}
	}
	m.interventionCovered = covered
	count := func(pred func(Summary) bool) int {
		c := 0
		for _, r := range records {
			if pred(r.Summary) {
				c++
			}
		}
		return c
	}
	coveredAnd := func(pred func(Summary) bool) int {
		return count(func(s Summary) bool { return interventionCovered(s) && pred(s) })
	}

	// 1. Data quality and coverage.
	coverageCounts := map[EvidenceCoverage]int{}
	for _, r := range records {
		coverageCounts[r.Summary.Coverage]++
	}
	for _, c := range []EvidenceCoverage{CoverageLiveCapture, CoverageLinkedLegacy, CoveragePartialLegacy, CoverageMetadataOnly, CoverageUnlinked} {
		m.Coverage = append(m.Coverage, coverageRow{string(c), coverageCounts[c]})
	}
	transcriptsMissing, transcriptLines, recovered, gitFailures, failedClass, malformedClass := 0, 0, 0, 0, 0, 0
	for _, r := range records {
		dq := r.Summary.DataQuality
		transcriptsMissing += len(dq.MissingTranscripts)
		transcriptLines += dq.MalformedTranscriptLines
		recovered += dq.RecoveredEvents
		if dq.GitStateError != "" {
			gitFailures++
		}
		failedClass += r.FailedClassifications
		malformedClass += r.MalformedClassifications
	}
	baseline := count(func(s Summary) bool { return s.DataQuality.BaselineImported })
	missingFlights := count(func(s Summary) bool { return len(s.DataQuality.MissingTranscripts) > 0 })
	gitFlights := count(func(s Summary) bool { return s.DataQuality.GitStateError != "" })
	q.MalformedClassifications += malformedClass
	q.FailedClassifications += failedClass
	whole := func(name, label string, num int) *ratio {
		return &ratio{Name: name, Label: label, Num: num, Covered: n, Cohort: n}
	}
	m.Quality = []qualityLine{
		{Label: "Baseline-imported flights", Ratio: whole("baseline-imported", "Baseline-imported flights", baseline)},
		{Label: "Missing transcripts", Ratio: whole("missing-transcripts", "Flights with missing transcripts", missingFlights), Value: fmt.Sprintf("%d transcripts missing in total", transcriptsMissing)},
		{Label: "Git state failures", Ratio: whole("git-failures", "Git state failures", gitFlights)},
		{Label: "Malformed ledger lines", Value: fmt.Sprintf("%d events, %d reconciliation, %d classification, %d transcript lines", q.MalformedEvents, q.MalformedReconciliation, q.MalformedClassifications, transcriptLines)},
		{Label: "Recovered events", Value: fmt.Sprintf("%d (a snapshot write whose event never landed)", recovered)},
		{Label: "Failed classifications", Value: fmt.Sprintf("%d operator messages have only failed Jev attempts", q.FailedClassifications)},
		{Label: "Unlinked conversations", Value: fmt.Sprintf("%d candidates awaiting your confirmation (never attached automatically)", len(q.UnlinkedCandidates))},
	}
	m.findings = baseline + missingFlights + gitFlights + q.MalformedEvents + q.MalformedReconciliation +
		q.MalformedClassifications + transcriptLines + recovered + q.FailedClassifications + len(q.RefreshErrors)
	for _, c := range q.UnlinkedCandidates {
		m.Exceptions = append(m.Exceptions, exceptionRow{
			Subject: c.FlightID, Finding: "Unlinked conversation " + c.SessionID + " may belong to this flight (matched " + strings.Join(c.MatchKinds, ", ") + ")",
			Evidence: fmt.Sprintf("session:%s line %d", c.SessionID, c.TranscriptLine),
		})
	}

	// 2. Cohort table, funnel, and aggregates.
	m.Aggregates = []ratio{
		{"flights-completed-in-flight", "Completed in flight", count(func(s Summary) bool { return s.PrimaryOutcome == OutcomeFlightCompleted }), linked, n},
		{"flights-with-interventions", "Flights with user interventions after takeoff", coveredAnd(func(s Summary) bool { return s.Counts.Interventions > 0 }), covered, n},
		{"intervention-free-completions", "Completed with no user intervention after takeoff", coveredAnd(func(s Summary) bool { return s.PrimaryOutcome == OutcomeFlightCompleted && s.Counts.Interventions == 0 }), covered, n},
		{"flights-with-failures", "Flights with at least one failure", count(func(s Summary) bool { return s.Counts.FailureTotal > 0 }), linked, n},
		{"flights-with-known-durations", "Flights whose every phase duration is known", count(func(s Summary) bool {
			return s.PrimaryOutcome != OutcomeActive && len(s.DataQuality.MissingBoundaries) == 0
		}), notActive, n},
	}
	outcomes := map[Outcome]int{}
	for _, r := range records {
		outcomes[r.Summary.PrimaryOutcome]++
	}
	for _, o := range outcomePrecedence {
		label := string(o)
		if o == OutcomeActive {
			label += " (censored in duration metrics)"
		}
		m.Funnel = append(m.Funnel, coverageRow{label, outcomes[o]})
	}
	m.Funnel = append(m.Funnel, coverageRow{"orphaned_active (a status flag, not an outcome)", count(func(s Summary) bool { return s.OrphanedActive })})
	durations := map[flights.WorkflowState][]int64{}
	contributing := map[flights.WorkflowState]map[string]bool{}
	for _, r := range records {
		for _, v := range r.Summary.PhasePath {
			if v.DurationSeconds == nil {
				continue
			}
			durations[v.Phase] = append(durations[v.Phase], *v.DurationSeconds)
			if contributing[v.Phase] == nil {
				contributing[v.Phase] = map[string]bool{}
			}
			contributing[v.Phase][r.Summary.FlightID] = true
		}
	}
	for _, phase := range phaseOrder {
		vals := durations[phase]
		sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
		row := phaseDurationRow{Phase: string(phase), P50: "unknown", P90: "unknown",
			Ratio: ratio{Name: "phase-" + string(phase), Label: "Flights with a known " + string(phase) + " duration", Num: len(contributing[phase]), Covered: linked, Cohort: n}}
		if len(vals) > 0 {
			row.P50, row.P90 = durationText(percentile(vals, 0.5)), durationText(percentile(vals, 0.9))
		}
		m.Durations = append(m.Durations, row)
	}
	for _, r := range records {
		s := r.Summary
		outcome := string(s.PrimaryOutcome)
		switch {
		case s.ConfirmedByUser:
			outcome += " (confirmed by user)"
		case s.OutcomePendingUser:
			outcome += " (pending confirmation)"
		}
		var path []string
		var known int64
		for _, v := range s.PhasePath {
			path = append(path, string(v.Phase))
			if v.DurationSeconds != nil {
				known += *v.DurationSeconds
			}
		}
		duration := "unknown"
		switch {
		case len(s.DataQuality.MissingBoundaries) == 0 && s.PrimaryOutcome != OutcomeActive:
			duration = durationText(known)
		case known > 0:
			duration = "incomplete: " + durationText(known) + " known"
		}
		waiting := "unknown"
		if s.WaitingSeconds != nil {
			waiting = durationText(*s.WaitingSeconds)
		}
		name := r.Flight.ShortName
		if name == "" {
			name = r.Flight.Prompt
		}
		m.Rows = append(m.Rows, cohortRow{
			ID: s.FlightID, Name: name, Repo: r.Flight.Repo, Outcome: outcome, Coverage: string(s.Coverage), Duration: duration,
			PhasePath: strings.Join(path, " → "), Waiting: waiting, Orphaned: s.OrphanedActive,
			Interventions: s.Counts.Interventions, Reviewers: s.Counts.Reviewers, Failures: s.Counts.FailureTotal,
		})
	}

	// 3. Orphan inventory.
	for _, r := range records {
		s := r.Summary
		if !s.OrphanedActive {
			continue
		}
		row := orphanRow{ID: s.FlightID, Name: r.Flight.ShortName, Outcome: string(s.PrimaryOutcome)}
		if s.OutcomePendingUser {
			row.Outcome += " (pending confirmation)"
		}
		if ws := r.Flight.Workspace; ws != nil {
			row.Workspace, row.Branch = ws.WorkspaceID, ws.Branch
		}
		row.Evidence = "no branch or PR evidence collected"
		if g := r.Git; g != nil && g.Error == "" && g.HeadSHA != "" {
			head := g.HeadSHA
			if len(head) > 7 {
				head = head[:7]
			}
			row.Branch = g.Branch
			row.Evidence = fmt.Sprintf("branch %s at %s, %d ahead of %s; PR data is not collected", g.Branch, head, g.Ahead, g.BaseBranch)
		}
		switch s.PrimaryOutcome {
		case OutcomeFinishedOutside:
			row.Action = "Confirm whether this branch was the result you used, then dispose the record."
		case OutcomeManualUndoRecovery:
			row.Action = "Decide whether to resume planning or dispose; its sessions are gone."
		default:
			row.Action = "Locate the work, or dispose the record if nothing is recoverable."
		}
		m.Orphans = append(m.Orphans, row)
	}

	// 4. Intervention ledger.
	for _, r := range records {
		for _, iv := range r.Summary.Interventions {
			row := interventionRow{
				Flight: r.Summary.FlightID, At: "unknown", Elapsed: "unknown", Phase: string(iv.Phase), Kind: string(iv.Kind),
				Classification: iv.Classification, Excerpt: iv.Excerpt, Evidence: refText(iv.Sources),
			}
			if iv.At != nil {
				row.At = iv.At.UTC().Format(time.RFC3339)
			}
			if iv.ElapsedSinceTakeoff != nil {
				row.Elapsed = durationText(*iv.ElapsedSinceTakeoff)
			}
			if iv.Kind == KindOperatorMessage {
				row.Label = string(iv.ChatLabel)
				if row.Label == "" {
					row.Label = "unlabeled"
				}
			}
			row.Who = strings.TrimSpace(iv.SessionRole + " " + iv.SessionID)
			m.Interventions = append(m.Interventions, row)
		}
	}

	// 5-6 and 7. Bottlenecks, candidates, exceptions.
	coveredRecords := make([]flightRecord, 0, n)
	for _, r := range records {
		if interventionCovered(r.Summary) {
			coveredRecords = append(coveredRecords, r)
		}
	}
	keys := map[string]string{}
	for _, r := range coveredRecords {
		for _, iv := range r.Summary.Interventions {
			k, title := groupKey(iv)
			keys[k] = title
		}
	}
	for k, title := range keys {
		affected, ivs := affectedOf(coveredRecords, k)
		row := bottleneckRow{
			Key: k, Title: title, Interventions: len(ivs),
			Affected: ratio{Name: "bottleneck-" + k, Label: title, Num: len(affected), Covered: covered, Cohort: n},
		}
		for _, iv := range ivs {
			if len(row.Representative) < 2 && iv.Excerpt != "" {
				row.Representative = append(row.Representative, iv.Excerpt)
			}
		}
		m.Bottlenecks = append(m.Bottlenecks, row)
	}
	sort.Slice(m.Bottlenecks, func(i, j int) bool {
		a, b := m.Bottlenecks[i], m.Bottlenecks[j]
		if a.Affected.Num != b.Affected.Num {
			return a.Affected.Num > b.Affected.Num
		}
		if a.Interventions != b.Interventions {
			return a.Interventions > b.Interventions
		}
		return a.Key < b.Key
	})
	for _, b := range m.Bottlenecks {
		affected, ivs := affectedOf(coveredRecords, b.Key)
		id := "c-" + b.Key
		confirmed := dec.ConfirmedMechanisms[id]
		if len(affected) < 2 && !confirmed {
			m.Exceptions = append(m.Exceptions, exceptionRow{
				Subject: strings.Join(affected, ", "), Finding: b.Title + " appeared in one flight only; an exception, not a recurring pattern",
				Evidence: strings.Join(evidenceIDs(ivs, 6), " "),
			})
			continue
		}
		t := textsFor(b.Key)
		m.Candidates = append(m.Candidates, candidateRow{
			ID: id, Title: b.Title, Affected: b.Affected, Evidence: evidenceIDs(ivs, 6), Confirmed: confirmed,
			Experiment: t.Experiment, Removes: t.Removes, NewFailure: t.NewFailure, CounterMetric: t.CounterMetric,
		})
	}
	for _, r := range records {
		s := r.Summary
		gap := func(finding string) {
			m.Exceptions = append(m.Exceptions, exceptionRow{Subject: s.FlightID, Finding: finding, Evidence: refText(s.Source[FieldDataQuality])})
		}
		if s.Coverage == CoverageMetadataOnly {
			gap("Metadata only: no transcript or git state, so its interventions are unknown, not zero")
		}
		for _, t := range s.DataQuality.MissingTranscripts {
			gap("Transcript " + t + " is missing")
		}
		if s.DataQuality.GitStateError != "" {
			gap("Git state could not be collected: " + s.DataQuality.GitStateError)
		}
		if s.OutcomePendingUser {
			gap("Outcome " + string(s.PrimaryOutcome) + " awaits your confirmation")
		}
	}

	// 8. Decision agenda.
	for i, c := range m.Candidates {
		m.Agenda = append(m.Agenda, agendaRow{N: i + 1, ID: c.ID, Title: c.Title})
	}
	for _, item := range dec.Unresolved {
		id, _, _ := strings.Cut(item.Text, ":")
		m.Agenda = append(m.Agenda, agendaRow{N: len(m.Agenda) + 1, ID: strings.TrimSpace(id), Status: item.Status, Detail: item.Text})
	}
	return m
}

func (m reportModel) render() ([]byte, error) {
	var buf bytes.Buffer
	if err := reportTemplate.Execute(&buf, m); err != nil {
		return nil, fmt.Errorf("render report: %w", err)
	}
	return buf.Bytes(), nil
}

var reportTemplate = template.Must(template.New("report").Parse(reportHTML))
