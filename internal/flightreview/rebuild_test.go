package flightreview

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/flighthistory"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func readSummaryFile(t *testing.T, flightID string) ([]byte, Summary) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(archiveDir(t, flightID), "summary.json"))
	if err != nil {
		t.Fatalf("summary.json: %v", err)
	}
	var s Summary
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	return data, s
}

func TestRebuildSummariesWritesSummaryJSONMode0600AndRebuildsByteIdentical(t *testing.T) {
	isolate(t)
	row := executingFlight()
	row.Operations = row.Operations[:1] // a pilot-started operation needs a retained pilot turn to load
	writeConversation(t, "ws-fl-1", "s-plan", "launch")
	writeConversation(t, "ws-fl-1", "s-exec", "launch")
	inspector := &fakeInspector{states: map[string]workspace.VCSArchiveState{"ws-fl-1": {VCS: "git", HeadSHA: "abc"}}}
	svc := newTestService(inspector)
	manager := testManager(t, row)
	if err := svc.ImportLegacy(context.Background(), manager); err != nil {
		t.Fatal(err)
	}
	ledger := flighthistory.NewLedger(flighthistory.EventsPath(), nil, nil)
	if err := ledger.Append([]flighthistory.Event{event(row, flighthistory.ChangeBaseline, flighthistory.ActorSystem, "baseline", at(100))}); err != nil {
		t.Fatal(err)
	}

	if err := svc.RebuildSummaries(context.Background(), manager); err != nil {
		t.Fatalf("RebuildSummaries: %v", err)
	}

	first, summary := readSummaryFile(t, "fl-1")
	if summary.FlightID != "fl-1" || summary.Coverage != CoverageLinkedLegacy || !summary.DataQuality.BaselineImported {
		t.Errorf("summary = flight %q coverage %s baseline %v", summary.FlightID, summary.Coverage, summary.DataQuality.BaselineImported)
	}
	if mode := modeOf(t, filepath.Join(archiveDir(t, "fl-1"), "summary.json")); mode != 0o600 {
		t.Errorf("summary.json mode = %v, want 0600", mode)
	}
	if err := svc.RebuildSummaries(context.Background(), manager); err != nil {
		t.Fatal(err)
	}
	second, _ := readSummaryFile(t, "fl-1")
	if string(first) != string(second) {
		t.Errorf("rebuild from identical evidence differs:\n%s\n%s", first, second)
	}
}

func TestRebuildSummariesIncludesDisposedFlightsFromTheirFinalRow(t *testing.T) {
	isolate(t)
	row := completedFlight()
	svc := newTestService(&fakeInspector{states: map[string]workspace.VCSArchiveState{"ws-fl-1": {VCS: "git", HeadSHA: "abc"}}})
	if err := svc.Capture(context.Background(), row, true); err != nil {
		t.Fatal(err)
	}
	removed := event(row, flighthistory.ChangeRemoved, flighthistory.ActorUser, "dispose_removed", at(130))
	if err := flighthistory.NewLedger(flighthistory.EventsPath(), nil, nil).Append([]flighthistory.Event{removed}); err != nil {
		t.Fatal(err)
	}

	if err := svc.RebuildSummaries(context.Background(), testManager(t)); err != nil {
		t.Fatalf("RebuildSummaries: %v", err)
	}

	_, summary := readSummaryFile(t, "fl-1")
	if summary.PrimaryOutcome != OutcomeFlightCompleted || !summary.CleanupFriction {
		t.Errorf("outcome = %s cleanup friction = %v, want a completed flight whose disposal is friction", summary.PrimaryOutcome, summary.CleanupFriction)
	}
}

func TestRebuildSummariesReportsAMalformedManifestAndContinues(t *testing.T) {
	isolate(t)
	svc := newTestService(&fakeInspector{})
	good, broken := backlogRow("fl-good"), backlogRow("fl-broken")
	broken.Order = 1
	manager := testManager(t, good, broken)
	if err := svc.ImportLegacy(context.Background(), manager); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archiveDir(t, "fl-broken"), "manifest.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := svc.RebuildSummaries(context.Background(), manager)

	if err == nil {
		t.Fatal("RebuildSummaries hid a malformed manifest")
	}
	if _, statErr := os.Stat(filepath.Join(archiveDir(t, "fl-good"), "summary.json")); statErr != nil {
		t.Errorf("a healthy flight was skipped because another failed: %v", statErr)
	}
}
