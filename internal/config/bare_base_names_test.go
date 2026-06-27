package config

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestBareBaseNamesOnDisk(t *testing.T) {
	tmpDir := t.TempDir()
	reposDir := filepath.Join(tmpDir, "repos")
	for _, name := range []string{"bach.git", "schmux.git", "not-a-base", "nested.git"} {
		if err := os.MkdirAll(filepath.Join(reposDir, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}
	// A plain file ending in .git should be ignored (only directories count).
	if err := os.WriteFile(filepath.Join(reposDir, "stray.git"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write stray: %v", err)
	}

	cfg := &Config{}
	cfg.WorktreeBasePath = reposDir
	got := cfg.BareBaseNamesOnDisk()
	sort.Strings(got)

	want := []string{"bach", "nested", "schmux"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestBareBaseNamesOnDisk_MissingDir(t *testing.T) {
	cfg := &Config{}
	cfg.WorktreeBasePath = filepath.Join(t.TempDir(), "does-not-exist")
	if got := cfg.BareBaseNamesOnDisk(); got != nil {
		t.Fatalf("expected nil for missing dir, got %v", got)
	}
}
