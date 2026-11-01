package daemon

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/flightreview"
	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func TestFlightBranchSuggester_NotConfiguredReturnsErrDisabled(t *testing.T) {
	cfg := config.CreateDefault(filepath.Join(t.TempDir(), "config.json"))

	_, err := flightBranchSuggester{cfg: cfg}.SuggestBranch(context.Background(), "Fix login")

	if !errors.Is(err, oneshot.ErrDisabled) {
		t.Fatalf("SuggestBranch() error = %v, want oneshot.ErrDisabled", err)
	}
}

func TestInitFlights(t *testing.T) {
	logger := log.New(io.Discard)

	t.Run("a missing flights file starts empty", func(t *testing.T) {
		schmuxdir.Set(t.TempDir())
		t.Cleanup(func() { schmuxdir.Set("") })
		cfg := config.CreateDefault(filepath.Join(t.TempDir(), "config.json"))
		wm := workspace.New(cfg, state.New("", nil), "", logger)

		mgr, _ := initFlights(cfg, wm, state.New("", nil), logger)
		if mgr == nil {
			t.Fatal("initFlights() disabled flights for a missing snapshot")
		}
		if n := len(mgr.List()); n != 0 {
			t.Errorf("flights = %d, want 0", n)
		}
	})

	for _, tc := range []struct {
		name, data, wantError string
	}{
		{"malformed JSON", `{"flights":[`, "decode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			schmuxdir.Set(home)
			t.Cleanup(func() { schmuxdir.Set("") })
			bad := []byte(tc.data)
			if err := os.WriteFile(schmuxdir.FlightsPath(), bad, 0o600); err != nil {
				t.Fatal(err)
			}
			cfg := config.CreateDefault(filepath.Join(t.TempDir(), "config.json"))
			wm := workspace.New(cfg, state.New("", nil), "", logger)

			var logs bytes.Buffer
			if mgr, _ := initFlights(cfg, wm, state.New("", nil), log.New(&logs)); mgr != nil {
				t.Fatal("initFlights() enabled writes after a load failure")
			}
			if output := logs.String(); !strings.Contains(output, "continuing without flights") || !strings.Contains(output, tc.wantError) {
				t.Errorf("missing load failure diagnostic: %s", output)
			}

			after, err := os.ReadFile(schmuxdir.FlightsPath())
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(bad) {
				t.Errorf("flights file changed: %q", after)
			}
		})
	}

	t.Run("an invalid row disables only that row", func(t *testing.T) {
		home := t.TempDir()
		schmuxdir.Set(home)
		t.Cleanup(func() { schmuxdir.Set("") })
		bad := []byte(`{"flights":[{"id":""}]}`)
		if err := os.WriteFile(schmuxdir.FlightsPath(), bad, 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := config.CreateDefault(filepath.Join(t.TempDir(), "config.json"))
		wm := workspace.New(cfg, state.New("", nil), "", logger)

		mgr, _ := initFlights(cfg, wm, state.New("", nil), logger)
		if mgr == nil {
			t.Fatal("initFlights() disabled all flights for one invalid row")
		}
		if got := mgr.List(); len(got) != 0 {
			t.Fatalf("invalid row became active: %+v", got)
		}
		after, err := os.ReadFile(schmuxdir.FlightsPath())
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(bad) {
			t.Fatalf("startup changed snapshot: %s", after)
		}
	})

	t.Run("unreadable snapshot disables only flights", func(t *testing.T) {
		schmuxdir.Set(t.TempDir())
		t.Cleanup(func() { schmuxdir.Set("") })
		if err := os.Mkdir(schmuxdir.FlightsPath(), 0o700); err != nil {
			t.Fatal(err)
		}
		cfg := config.CreateDefault(filepath.Join(t.TempDir(), "config.json"))
		wm := workspace.New(cfg, state.New("", nil), "", logger)
		var logs bytes.Buffer
		if mgr, _ := initFlights(cfg, wm, state.New("", nil), log.New(&logs)); mgr != nil {
			t.Fatal("initFlights() enabled writes for an unreadable snapshot")
		}
		if output := logs.String(); !strings.Contains(output, "flights: read") {
			t.Errorf("missing read failure diagnostic: %s", output)
		}
		if info, err := os.Stat(schmuxdir.FlightsPath()); err != nil || !info.IsDir() {
			t.Fatalf("snapshot path changed: info=%v err=%v", info, err)
		}
	})
}

func TestFlightNamer_EmptyTargetIsNamingOff(t *testing.T) {
	logger := log.New(io.Discard)
	cfg := &config.Config{}
	namer := flightNamer{cfg: cfg, logger: logger}
	_, err := namer.Name(context.Background(), "x")
	if !errors.Is(err, flights.ErrNamingOff) {
		t.Errorf("Name with empty target err = %v, want flights.ErrNamingOff", err)
	}
}

func TestFlightNamer_UnknownTargetIsARealFailure(t *testing.T) {
	logger := log.New(io.Discard)
	cfg := &config.Config{}
	cfg.FlightName = &config.FlightNameConfig{Target: "no-such-model"}
	namer := flightNamer{cfg: cfg, logger: logger}
	_, err := namer.Name(context.Background(), "x")
	if errors.Is(err, flights.ErrNamingOff) {
		t.Errorf("Name with unknown target returned ErrNamingOff")
	}
	if !errors.Is(err, oneshot.ErrTargetNotFound) {
		t.Errorf("Name with unknown target err = %v, want oneshot.ErrTargetNotFound", err)
	}
}

func TestInitFlightsArchivesExistingFlightsBeforeMonitoring(t *testing.T) {
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	row := `{"id":"fl-legacy","repo":"https://example.com/r.git","prompt":"legacy work","planner_model":"p","executor_model":"e",` +
		`"workflow_state":"backlog","cleanup_state":"idle","order":0,"phase_visit":1,` +
		`"phase_started_at":"2026-10-01T09:00:00Z","created_at":"2026-10-01T09:00:00Z","updated_at":"2026-10-01T09:00:00Z"}`
	if err := os.WriteFile(schmuxdir.FlightsPath(), []byte(`{"flights":[`+row+`]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard)
	cfg := config.CreateDefault(filepath.Join(t.TempDir(), "config.json"))
	st := state.New("", nil)
	wm := workspace.New(cfg, st, "", logger)

	mgr, review := initFlights(cfg, wm, st, logger)

	if mgr == nil || review == nil {
		t.Fatalf("initFlights() = %v, %v; want a manager and a review service", mgr, review)
	}
	archived := filepath.Join(schmuxdir.FlightHistoryDir(), "fl-legacy")
	for _, name := range []string{"manifest.json", "flight.json"} {
		if _, err := os.Stat(filepath.Join(archived, name)); err != nil {
			t.Errorf("legacy flight not archived at startup: %v", err)
		}
	}
	events, err := os.ReadFile(filepath.Join(schmuxdir.FlightHistoryDir(), "events.jsonl"))
	if err != nil || !strings.Contains(string(events), `"change":"baseline_imported"`) {
		t.Errorf("baseline event missing from the ledger: %q (err %v)", events, err)
	}
}

func TestInitFlightsReconcilesMissingWorkspaceAtStartup(t *testing.T) {
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	row := `{"id":"fl-orphan","repo":"https://example.com/r.git","prompt":"orphaned work","planner_model":"p","executor_model":"e",` +
		`"workflow_state":"executing","cleanup_state":"idle","order":0,"phase_visit":2,` +
		`"workspace":{"id":"wa-1","requested_branch":"feature/x","workspace_id":"repo-001","path":"/gone","outcome":"succeeded","created_at":"2026-10-01T09:00:00Z","updated_at":"2026-10-01T09:00:00Z"},` +
		`"phase_started_at":"2026-10-01T09:00:00Z","created_at":"2026-10-01T09:00:00Z","updated_at":"2026-10-01T09:00:00Z"}`
	if err := os.WriteFile(schmuxdir.FlightsPath(), []byte(`{"flights":[`+row+`]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	logger := log.New(io.Discard)
	cfg := config.CreateDefault(filepath.Join(t.TempDir(), "config.json"))
	st := state.New("", nil)
	wm := workspace.New(cfg, st, "", logger)

	if mgr, _ := initFlights(cfg, wm, st, logger); mgr == nil {
		t.Fatal("initFlights() disabled flights")
	}

	ledger, err := os.ReadFile(filepath.Join(schmuxdir.FlightHistoryDir(), "reconciliation.jsonl"))
	if err != nil || !strings.Contains(string(ledger), `"kind":"resource_missing"`) || !strings.Contains(string(ledger), `"flight_id":"fl-orphan"`) {
		t.Errorf("startup did not record the missing workspace: %q (err %v)", ledger, err)
	}
}

func TestInitFlightsAttachesTheManagerToTheReviewService(t *testing.T) {
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	logger := log.New(io.Discard)
	cfg := config.CreateDefault(filepath.Join(t.TempDir(), "config.json"))
	st := state.New("", nil)
	wm := workspace.New(cfg, st, "", logger)

	_, review := initFlights(cfg, wm, st, logger)

	now := time.Now()
	_, err := review.RenderReport(context.Background(), flightreview.ReportRequest{
		Since: now.Add(-24 * time.Hour), Until: now.Add(24 * time.Hour), Cohort: flightreview.CohortActivity,
	})
	if err != nil {
		t.Fatalf("RenderReport through the daemon's service: %v", err)
	}
}
