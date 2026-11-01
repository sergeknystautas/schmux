package dashboard

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/flighthistory"
	"github.com/sergeknystautas/schmux/internal/flightreview"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

// A broadcast first reconciles flight resources, so a workspace that vanished
// is recorded before the dashboard shows the flight as healthy.
func TestDoBroadcastReconcilesFlightResourcesFirst(t *testing.T) {
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	e := newFlightEnv(t)
	e.server.flightReview = flightreview.NewService(flightreview.Options{
		State: e.st,
		Now:   func() time.Time { return time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC) },
	})
	planned := e.planned(t)
	if err := e.st.RemoveWorkspace(planned.Workspace.WorkspaceID); err != nil {
		t.Fatal(err)
	}

	e.server.doBroadcast()

	events, malformed, err := flighthistory.ReadReconciliation(filepath.Join(flighthistory.Root(), "reconciliation.jsonl"))
	if err != nil || len(malformed) != 0 {
		t.Fatalf("read reconciliation: err=%v malformed=%v", err, malformed)
	}
	if len(events) == 0 || events[0].Kind != flighthistory.ReconcileResourceMissing || events[0].FlightID != planned.ID || events[0].ResourceKind != "workspace" {
		t.Fatalf("events = %+v, want the missing workspace of %s", events, planned.ID)
	}
}

func TestDoBroadcastWithoutReviewServiceStillBroadcasts(t *testing.T) {
	e := newFlightEnv(t)

	e.server.doBroadcast() // must not panic with no review service wired
}
