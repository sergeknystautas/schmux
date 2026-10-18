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

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

func TestInitFlights(t *testing.T) {
	logger := log.New(io.Discard)

	t.Run("a missing flights file starts empty", func(t *testing.T) {
		schmuxdir.Set(t.TempDir())
		t.Cleanup(func() { schmuxdir.Set("") })
		cfg := config.CreateDefault(filepath.Join(t.TempDir(), "config.json"))
		wm := workspace.New(cfg, state.New("", nil), "", logger)

		mgr := initFlights(cfg, wm, logger)
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
			if mgr := initFlights(cfg, wm, log.New(&logs)); mgr != nil {
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

		mgr := initFlights(cfg, wm, logger)
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
		if mgr := initFlights(cfg, wm, log.New(&logs)); mgr != nil {
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
