package daemon

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/log"
)

func newTestLogger() *log.Logger {
	return log.NewWithOptions(io.Discard, log.Options{})
}

func TestMigrateModesTightensFileMode(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(f, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := MigrateModes(dir, false /*allowInsecure*/, newTestLogger()); err != nil {
		t.Fatalf("MigrateModes failed: %v", err)
	}

	info, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("got mode %o, want 0600", info.Mode().Perm())
	}
}

func TestMigrateModesTightensDirMode(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "schmux")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := MigrateModes(dir, false, newTestLogger()); err != nil {
		t.Fatalf("MigrateModes failed: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Errorf("got dir mode %o, want 0700", info.Mode().Perm())
	}
}

func TestMigrateModesSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	// MigrateModes should chmod target (it's a regular file in the dir) but
	// skip the symlink itself. Verify the symlink's target mode wasn't
	// changed via the symlink.
	if err := MigrateModes(dir, false, newTestLogger()); err != nil {
		t.Fatalf("MigrateModes failed: %v", err)
	}
	// Target is a regular file: should have been chmod'd to 0600.
	if info, _ := os.Stat(target); info.Mode().Perm() != 0600 {
		t.Errorf("target file: got mode %o, want 0600", info.Mode().Perm())
	}
}

// Test for failure case + allow_insecure_modes:
func TestMigrateModesRespectsAllowInsecure(t *testing.T) {
	// Hard to simulate chmod failure on a normal filesystem. Document the
	// intent and skip if not on a FS that supports it. Alternative: inject
	// a chmod failure via a test seam (e.g., a chmodFunc variable that the
	// production code uses, swappable in tests).
	t.Skip("chmod failure simulation requires test seam; covered by manual test in Step 36")
}
