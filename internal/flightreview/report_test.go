package flightreview

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/sergeknystautas/schmux/internal/flighthistory"
	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// --- HTML helpers: assertions walk the parsed tree, not the raw text. ---

func parseHTML(t *testing.T, data []byte) *html.Node {
	t.Helper()
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("report is not parseable HTML: %v", err)
	}
	return doc
}

func walk(n *html.Node, visit func(*html.Node)) {
	visit(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, visit)
	}
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func text(n *html.Node) string {
	var b strings.Builder
	walk(n, func(c *html.Node) {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	})
	return strings.Join(strings.Fields(b.String()), " ")
}

func elements(n *html.Node, tag string) []*html.Node {
	var out []*html.Node
	walk(n, func(c *html.Node) {
		if c.Type == html.ElementNode && c.Data == tag {
			out = append(out, c)
		}
	})
	return out
}

func byID(n *html.Node, id string) *html.Node {
	var found *html.Node
	walk(n, func(c *html.Node) {
		if c.Type == html.ElementNode && attr(c, "id") == id {
			found = c
		}
	})
	return found
}

func byClass(n *html.Node, class string) []*html.Node {
	var out []*html.Node
	walk(n, func(c *html.Node) {
		if c.Type == html.ElementNode && slicesContains(strings.Fields(attr(c, "class")), class) {
			out = append(out, c)
		}
	})
	return out
}

func slicesContains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// --- fixtures ---

var reportReq = ReportRequest{Since: t0, Until: t0.Add(30 * 24 * time.Hour), Cohort: CohortDisposed}

func opMessage(id string, label ChatLabel, excerpt string) Intervention {
	iv := Intervention{
		ID: "iv-" + id, Kind: KindOperatorMessage, Classification: "unclassified", ChatLabel: label,
		SessionRole: "planner", SessionID: "s-" + id, Excerpt: excerpt, ChatRecordID: "chat-" + id,
		Sources: []SourceReference{{Kind: "chat_record", ID: "chat-" + id, Line: 3}},
	}
	at := t0.Add(time.Hour)
	iv.At = &at
	return iv
}

func summaryOf(id string, mods ...func(*Summary)) Summary {
	s := Summary{
		SchemaVersion: SummarySchemaVersion, FlightID: id, Coverage: CoverageLinkedLegacy, PrimaryOutcome: OutcomeFlightCompleted,
		PhasePath:     []PhaseVisit{},
		Interventions: []Intervention{},
		Counts:        SummaryCounts{ByKind: map[InterventionKind]int{}, Failures: map[string]int{}, ByLabel: map[string]int{}},
		Source:        SourceReferences{},
	}
	for _, mod := range mods {
		mod(&s)
	}
	s.Counts.Interventions = len(s.Interventions)
	for _, iv := range s.Interventions {
		s.Counts.ByKind[iv.Kind]++
		if iv.ChatLabel != "" {
			s.Counts.ByLabel[string(iv.ChatLabel)]++
		}
	}
	return s
}

func withInterventions(ivs ...Intervention) func(*Summary) {
	return func(s *Summary) { s.Interventions = append(s.Interventions, ivs...) }
}

func recordOf(s Summary, mods ...func(*flightRecord)) flightRecord {
	removed := t0.Add(2 * time.Hour)
	r := flightRecord{Summary: s, Flight: flights.Flight{ID: s.FlightID, ShortName: "Flight " + s.FlightID, Repo: "r"}, Removed: &removed}
	for _, mod := range mods {
		mod(&r)
	}
	return r
}

func render(t *testing.T, records []flightRecord, q ledgerQuality, previous string, req ReportRequest) (*html.Node, []byte) {
	t.Helper()
	model := buildReport(records, q, parseDecisions(previous), req)
	data, err := model.render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return parseHTML(t, data), data
}

func fourFlights() []flightRecord {
	return []flightRecord{
		recordOf(summaryOf("fl-a", withInterventions(opMessage("a1", LabelRouting, "wrong recipient"), opMessage("a2", LabelWorkflow, "undo that")))),
		recordOf(summaryOf("fl-b", withInterventions(opMessage("b1", LabelRouting, "send to the executor")))),
		recordOf(summaryOf("fl-c")),
		recordOf(summaryOf("fl-d", func(s *Summary) {
			s.Coverage = CoverageMetadataOnly
			s.DataQuality.BaselineImported = true
			s.DataQuality.MissingTranscripts = []string{"planner-s-d"}
		})),
	}
}

// --- tests ---

func TestRenderReportShowsDataQualityBeforeConclusions(t *testing.T) {
	q := ledgerQuality{MalformedEvents: 2, MalformedReconciliation: 1, MalformedClassifications: 3, FailedClassifications: 4}
	records := fourFlights()
	records[0].Summary.DataQuality.MalformedTranscriptLines = 5
	records[0].Summary.DataQuality.RecoveredEvents = 1
	records[2].Summary.DataQuality.GitStateError = "workspace directory is not available"
	doc, _ := render(t, records, q, "", reportReq)

	var order []string
	for _, section := range elements(doc, "section") {
		order = append(order, attr(section, "id"))
	}
	want := []string{"data-quality", "cohort", "orphans", "interventions", "bottlenecks", "candidates", "exceptions", "agenda"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("section order = %v, want %v", order, want)
	}
	quality := text(byID(doc, "data-quality"))
	for _, expect := range []string{
		"Baseline-imported flights", "Missing transcripts", "Malformed ledger lines", "Git state failures",
		"Recovered events", "Failed classifications", "metadata_only", "linked_legacy",
	} {
		if !strings.Contains(quality, expect) {
			t.Errorf("data-quality section does not mention %q:\n%s", expect, quality)
		}
	}
	if !strings.Contains(quality, "2 events") || !strings.Contains(quality, "1 reconciliation") {
		t.Errorf("malformed ledger counts missing: %s", quality)
	}
}

func TestRenderReportEveryAggregateNamesCoveredAndCohortDenominators(t *testing.T) {
	doc, _ := render(t, fourFlights(), ledgerQuality{}, "", reportReq)

	aggregates := byClass(doc, "aggregate")
	if len(aggregates) < 5 {
		t.Fatalf("only %d aggregates rendered", len(aggregates))
	}
	for _, a := range aggregates {
		num, covered, cohort := attr(a, "data-numerator"), attr(a, "data-covered"), attr(a, "data-cohort")
		for name, v := range map[string]string{"numerator": num, "covered": covered, "cohort": cohort} {
			if _, err := strconv.Atoi(v); err != nil {
				t.Errorf("aggregate %q has no numeric %s (%q)", text(a), name, v)
			}
		}
		label := text(a)
		if !strings.Contains(label, num+" of "+covered+" covered") || !strings.Contains(label, cohort+" in cohort") {
			t.Errorf("aggregate text %q does not state %s of %s covered, %s in cohort", label, num, covered, cohort)
		}
		n, _ := strconv.Atoi(num)
		c, _ := strconv.Atoi(covered)
		k, _ := strconv.Atoi(cohort)
		if n > c || c > k {
			t.Errorf("aggregate %q violates numerator <= covered <= cohort", label)
		}
	}
	// fl-d is metadata only, so the intervention evidence covers 3 of 4.
	found := false
	for _, a := range aggregates {
		if attr(a, "data-name") == "flights-with-interventions" {
			found = true
			if attr(a, "data-numerator") != "2" || attr(a, "data-covered") != "3" || attr(a, "data-cohort") != "4" {
				t.Errorf("flights-with-interventions = %s/%s/%s, want 2/3/4", attr(a, "data-numerator"), attr(a, "data-covered"), attr(a, "data-cohort"))
			}
		}
	}
	if !found {
		t.Error("no flights-with-interventions aggregate")
	}
	if !strings.Contains(text(byID(doc, "cohort")), "censored") {
		t.Error("the cohort section does not state the censoring rule")
	}
}

func TestRenderReportSeparatesSingleFlightExceptionsFromCandidates(t *testing.T) {
	doc, _ := render(t, fourFlights(), ledgerQuality{}, "", reportReq)

	candidates := text(byID(doc, "candidates"))
	exceptions := text(byID(doc, "exceptions"))
	if !strings.Contains(candidates, "routing_correction") || !strings.Contains(candidates, "2 of") {
		t.Errorf("routing_correction (two flights) is not a candidate: %s", candidates)
	}
	if strings.Contains(candidates, "workflow_correction") {
		t.Errorf("a single-flight label became a candidate: %s", candidates)
	}
	if !strings.Contains(exceptions, "workflow_correction") || !strings.Contains(exceptions, "fl-a") {
		t.Errorf("the single-flight finding is missing from exceptions: %s", exceptions)
	}

	t.Run("one flight plus a user-confirmed mechanism is a candidate", func(t *testing.T) {
		previous := "# Decisions\n\n## Confirmed mechanisms\n- c-workflow_correction: the pilot never offers undo\n"
		doc, _ := render(t, fourFlights(), ledgerQuality{}, previous, reportReq)
		if !strings.Contains(text(byID(doc, "candidates")), "workflow_correction") {
			t.Error("a confirmed mechanism did not promote the single-flight finding")
		}
	})
	t.Run("each candidate states its experiment, removed intervention, new failure, and counter-metric", func(t *testing.T) {
		for _, field := range []string{"Smallest experiment", "Intervention removed", "New failure it could introduce", "Counter-metric"} {
			if !strings.Contains(candidates, field) {
				t.Errorf("candidate lacks %q", field)
			}
		}
	})
}

func TestRenderReportShowsOrphanInventory(t *testing.T) {
	orphan := recordOf(summaryOf("fl-o", func(s *Summary) {
		s.OrphanedActive = true
		s.PrimaryOutcome = OutcomeFinishedOutside
		s.OutcomePendingUser = true
	}), func(r *flightRecord) {
		r.Removed = nil
		r.Flight.Workspace = &flights.WorkspaceAssignment{WorkspaceID: "ws-o", Branch: "feature/orphan", Path: "/ws/o"}
		r.Git = &GitState{VCS: "git", Branch: "feature/orphan", HeadSHA: "abc1234", Ahead: 3}
	})
	records := append(fourFlights(), orphan)
	req := reportReq
	req.Cohort = CohortActivity
	orphan.Active = []time.Time{t0.Add(time.Hour)}
	records[len(records)-1] = orphan

	doc, _ := render(t, records, ledgerQuality{}, "", req)

	section := byID(doc, "orphans")
	rows := elements(section, "tr")
	var orphanRow string
	for _, row := range rows {
		if strings.Contains(text(row), "fl-o") {
			orphanRow = text(row)
		}
	}
	if orphanRow == "" {
		t.Fatalf("orphan fl-o is not in the inventory: %s", text(section))
	}
	for _, expect := range []string{"feature/orphan", "abc1234", "finished_outside_flight", "pending", "Confirm"} {
		if !strings.Contains(orphanRow, expect) {
			t.Errorf("orphan row lacks %q: %s", expect, orphanRow)
		}
	}
	cohort := text(byID(doc, "cohort"))
	if !strings.Contains(cohort, "orphaned_active") {
		t.Errorf("orphans are not distinguished in the funnel: %s", cohort)
	}
}

func TestRenderReportCarriesUnresolvedPreviousDecisions(t *testing.T) {
	previous := `# Flight review decisions — 2026-09-30

## Accepted
- c-routing_correction: pilot confirms the recipient before sending

## Rejected
- c-status_query: not worth it

## Needs data
- c-environment_unblock: need two more weeks of flights

## Deferred
- c-product_guidance: revisit after the planner change

## Confirmed external completions
- fl-a: finished outside the flight
`
	doc, _ := render(t, fourFlights(), ledgerQuality{}, previous, reportReq)

	agenda := text(byID(doc, "agenda"))
	for _, carried := range []string{"c-environment_unblock", "need two more weeks", "c-product_guidance", "revisit after the planner change"} {
		if !strings.Contains(agenda, carried) {
			t.Errorf("unresolved item %q was not carried forward: %s", carried, agenda)
		}
	}
	for _, settled := range []string{"c-status_query", "not worth it"} {
		if strings.Contains(agenda, settled) {
			t.Errorf("settled item %q was carried forward", settled)
		}
	}
	if !strings.Contains(agenda, "Needs data") || !strings.Contains(agenda, "Deferred") {
		t.Errorf("carried items do not say how they were left: %s", agenda)
	}
	for _, status := range []string{"Accepted", "Rejected", "Needs data", "Deferred"} {
		if !strings.Contains(agenda, status) {
			t.Errorf("agenda does not offer the %q decision", status)
		}
	}
}

func TestParseDecisionsSkipsConfirmedItemsWithoutIDs(t *testing.T) {
	got := parseDecisions("## Confirmed external completions\n- : no id\n\n## Confirmed mechanisms\n- : no id\n")

	if len(got.ConfirmedExternal) != 0 || len(got.ConfirmedMechanisms) != 0 {
		t.Fatalf("confirmed decisions = %+v, want no entries without IDs", got)
	}
}

func TestRenderReportOverlaysConfirmedExternalCompletionWithoutHidingTheDerivedOutcome(t *testing.T) {
	pending := recordOf(summaryOf("fl-x", func(s *Summary) {
		s.PrimaryOutcome = OutcomeFinishedOutside
		s.OutcomePendingUser = true
	}))
	records := []flightRecord{pending}

	unconfirmed, _ := render(t, records, ledgerQuality{}, "", reportReq)
	confirmed, _ := render(t, records, ledgerQuality{}, "## Confirmed external completions\n- fl-x: used the branch as is\n", reportReq)

	if row := text(byID(unconfirmed, "cohort")); !strings.Contains(row, "pending confirmation") {
		t.Errorf("unconfirmed outcome is not marked pending: %s", row)
	}
	row := text(byID(confirmed, "cohort"))
	if !strings.Contains(row, "confirmed by user") || strings.Contains(row, "pending confirmation") {
		t.Errorf("confirmed outcome is not overlaid: %s", row)
	}
	if !strings.Contains(row, "finished_outside_flight") {
		t.Errorf("the derived outcome disappeared: %s", row)
	}
}

func TestRenderReportIsSelfContainedAndPrintSafe(t *testing.T) {
	records := fourFlights()
	records[0].Summary.Interventions[0].Excerpt = `<script>alert("x")</script> & <img src="http://evil/x.png">`
	doc, raw := render(t, records, ledgerQuality{}, "", reportReq)

	for _, tag := range []string{"script", "link", "iframe", "img", "object", "embed"} {
		if n := len(elements(doc, tag)); n != 0 {
			t.Errorf("report contains %d <%s> elements", n, tag)
		}
	}
	walk(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		for _, key := range []string{"src", "href", "action"} {
			if v := attr(n, key); strings.HasPrefix(v, "http:") || strings.HasPrefix(v, "https:") || strings.HasPrefix(v, "//") {
				t.Errorf("<%s %s=%q> reaches outside the file", n.Data, key, v)
			}
		}
	})
	css := text(elements(doc, "style")[0])
	for _, expect := range []string{"@media print", "print-color-adjust: exact", "color-scheme: light"} {
		if !strings.Contains(css, expect) {
			t.Errorf("inline CSS lacks %q", expect)
		}
	}
	if strings.Contains(css, "@import") || strings.Contains(css, "url(") {
		t.Error("inline CSS fetches external resources")
	}
	if len(elements(doc, "main")) != 1 || len(elements(doc, "table")) == 0 || len(elements(doc, "th")) == 0 {
		t.Error("report lacks semantic main/table/th structure")
	}
	if attr(elements(doc, "html")[0], "lang") == "" {
		t.Error("<html> has no lang")
	}
	if !bytes.Contains(raw, []byte("&lt;script&gt;")) {
		t.Error("operator text was not escaped")
	}
	if !strings.Contains(text(byID(doc, "interventions")), `<script>alert("x")</script>`) {
		t.Error("escaped operator text is not shown verbatim")
	}
}

func TestSelectCohortAppliesTheStatedRule(t *testing.T) {
	inRange, before, after := t0.Add(time.Hour), t0.Add(-time.Hour), t0.Add(40*24*time.Hour)
	mk := func(id string, removed *time.Time, active ...time.Time) flightRecord {
		return recordOf(summaryOf(id), func(r *flightRecord) { r.Removed, r.Active = removed, active })
	}
	records := []flightRecord{
		mk("disposed-in", &inRange),
		mk("disposed-before", &before),
		mk("disposed-after", &after),
		mk("active-in", nil, inRange),
		mk("active-out", nil, before),
		mk("disposed-before-active-in", &before, inRange),
	}
	ids := func(rs []flightRecord) string {
		var out []string
		for _, r := range rs {
			out = append(out, r.Summary.FlightID)
		}
		return strings.Join(out, ",")
	}

	disposed := selectCohort(records, ReportRequest{Since: t0, Until: t0.Add(30 * 24 * time.Hour), Cohort: CohortDisposed})
	activity := selectCohort(records, ReportRequest{Since: t0, Until: t0.Add(30 * 24 * time.Hour), Cohort: CohortActivity})

	if got := ids(disposed); got != "disposed-in" {
		t.Errorf("disposed cohort = %s", got)
	}
	if got := ids(activity); got != "disposed-in,active-in,disposed-before-active-in" {
		t.Errorf("activity cohort = %s", got)
	}
}

func TestRenderReportRejectsAnInvalidRequest(t *testing.T) {
	isolate(t)
	svc := newTestService(&fakeInspector{})
	svc.SetFlights(testManager(t))
	for name, req := range map[string]ReportRequest{
		"unknown cohort": {Since: t0, Until: t0.Add(time.Hour), Cohort: "everything"},
		"empty range":    {Since: t0.Add(time.Hour), Until: t0, Cohort: CohortDisposed},
		"zero dates":     {Cohort: CohortDisposed},
	} {
		if _, err := svc.RenderReport(context.Background(), req); err == nil {
			t.Errorf("%s: RenderReport accepted the request", name)
		}
	}
	if _, err := newTestService(&fakeInspector{}).RenderReport(context.Background(), reportReq); err == nil {
		t.Error("RenderReport ran with no flights manager attached")
	}
}

func TestRenderReportBuildsFromArchivedEvidence(t *testing.T) {
	isolate(t) // no TypeSafe key is saved: classification must fail visibly, not fatally
	row := executingFlight()
	row.Operations = row.Operations[:1]
	row.ShortName = "Archived flight"
	writeConversationAt(t, "ws-fl-1", "s-plan", userMsg("launch", "go", at(2).Add(-time.Second)), userMsg("chat-1", "please use postgres", at(30)))
	writeConversationAt(t, "ws-fl-1", "s-exec", userMsg("launch2", "go", at(21).Add(-time.Second)))
	inspector := &fakeInspector{states: map[string]workspace.VCSArchiveState{"ws-fl-1": {VCS: "git", Branch: "feature/fl-1", HeadSHA: "abc"}}}
	svc := NewService(Options{State: state.New("", nil), Workspaces: inspector, Now: func() time.Time { return t0.Add(time.Hour) }, Version: "test"})
	manager := testManager(t, row)
	svc.SetFlights(manager)
	ledger := flighthistory.NewLedger(flighthistory.EventsPath(), nil, nil)
	if err := ledger.Append([]flighthistory.Event{event(row, flighthistory.ChangeBaseline, flighthistory.ActorSystem, "baseline", at(40))}); err != nil {
		t.Fatal(err)
	}
	req := ReportRequest{Since: t0, Until: t0.Add(30 * 24 * time.Hour), Cohort: CohortActivity}

	report, err := svc.RenderReport(context.Background(), req)

	if err != nil {
		t.Fatalf("RenderReport: %v", err)
	}
	if report.FlightCount != 1 {
		t.Errorf("flight count = %d, want 1", report.FlightCount)
	}
	doc := parseHTML(t, report.HTML)
	if !strings.Contains(text(byID(doc, "interventions")), "please use postgres") {
		t.Errorf("operator message missing from the intervention ledger: %s", text(byID(doc, "interventions")))
	}
	if !strings.Contains(text(byID(doc, "data-quality")), "Baseline-imported flights") {
		t.Error("data quality section missing")
	}
	// Rendering refreshed the archive and rewrote the summary.
	if _, err := os.Stat(filepath.Join(archiveDir(t, "fl-1"), "summary.json")); err != nil {
		t.Errorf("summary.json was not rebuilt by the report: %v", err)
	}
	// With no key, the failed classification is visible, not fatal.
	attempts, _, _ := readClassifications("fl-1")
	if len(attempts) != 1 || attempts[0].Error == "" {
		t.Errorf("classification attempts = %+v, want one failed attempt", attempts)
	}
	if report.DataQualityCount == 0 {
		t.Error("report counted no data-quality findings for a baseline-imported flight with a failed classification")
	}
}
