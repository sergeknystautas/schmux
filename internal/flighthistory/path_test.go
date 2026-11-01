package flighthistory

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func TestFlightDirRejectsUnconfinedIDs(t *testing.T) {
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })

	for _, id := range []string{"", "..", ".", "/abs", "a/b", `a\b`, "bad\x00id"} {
		if dir, err := FlightDir(id); err == nil {
			t.Errorf("FlightDir(%q) = %q, want error", id, dir)
		}
	}

	dir, err := FlightDir("fl-1234")
	if err != nil {
		t.Fatalf("FlightDir(fl-1234): %v", err)
	}
	if want := filepath.Join(Root(), "fl-1234"); dir != want {
		t.Errorf("FlightDir = %q, want %q", dir, want)
	}
	if !strings.HasPrefix(dir, Root()+string(filepath.Separator)) {
		t.Errorf("FlightDir %q escapes root %q", dir, Root())
	}
}

func TestRootPathsLiveUnderFlightHistoryDir(t *testing.T) {
	base := t.TempDir()
	schmuxdir.Set(base)
	t.Cleanup(func() { schmuxdir.Set("") })

	if want := filepath.Join(base, "flight-history"); Root() != want {
		t.Errorf("Root = %q, want %q", Root(), want)
	}
	if want := filepath.Join(Root(), "events.jsonl"); EventsPath() != want {
		t.Errorf("EventsPath = %q, want %q", EventsPath(), want)
	}
	if want := filepath.Join(Root(), "reconciliation.jsonl"); ReconciliationPath() != want {
		t.Errorf("ReconciliationPath = %q, want %q", ReconciliationPath(), want)
	}
}
