package daemon

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/config"
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

		mgr, err := initFlights(cfg, wm, logger)

		if err != nil {
			t.Fatalf("initFlights() error = %v", err)
		}
		if n := len(mgr.List()); n != 0 {
			t.Errorf("flights = %d, want 0", n)
		}
	})

	t.Run("an invalid flights file is a startup error and is left untouched", func(t *testing.T) {
		home := t.TempDir()
		schmuxdir.Set(home)
		t.Cleanup(func() { schmuxdir.Set("") })
		bad := []byte(`{"version":99,"flights":[]}`)
		if err := os.WriteFile(schmuxdir.FlightsPath(), bad, 0o600); err != nil {
			t.Fatal(err)
		}
		cfg := config.CreateDefault(filepath.Join(t.TempDir(), "config.json"))
		wm := workspace.New(cfg, state.New("", nil), "", logger)

		if _, err := initFlights(cfg, wm, logger); err == nil {
			t.Fatal("initFlights() error = nil, want an unsupported-version error")
		}

		after, _ := os.ReadFile(schmuxdir.FlightsPath())
		if string(after) != string(bad) {
			t.Errorf("flights file changed: %q", after)
		}
	})
}
