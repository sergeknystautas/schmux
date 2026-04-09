package schmuxdir_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/lore"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func TestDownstreamFunctionsUseCustomDir(t *testing.T) {
	tmpDir := t.TempDir()
	schmuxdir.Set(tmpDir)
	defer schmuxdir.Reset()

	home, _ := os.UserHomeDir()
	defaultDir := filepath.Join(home, ".schmux")

	// Verify schmuxdir.Get() returns custom dir
	if got := schmuxdir.Get(); got != tmpDir {
		t.Fatalf("schmuxdir.Get() = %q, want %q", got, tmpDir)
	}

	// Verify config.ConfigExists() checks under custom dir, not ~/.schmux
	exists := config.ConfigExists()
	if exists {
		t.Errorf("config.ConfigExists() returned true for empty temp dir -- likely still checking ~/.schmux instead of custom dir")
	}

	// Verify lore.LoreStateDir() returns path under custom dir
	loreDir, err := lore.LoreStateDir("test-repo")
	if err != nil {
		t.Fatalf("lore.LoreStateDir() returned error: %v", err)
	}
	if !strings.HasPrefix(loreDir, tmpDir) {
		t.Errorf("lore.LoreStateDir() = %q, want prefix %q", loreDir, tmpDir)
	}
	if strings.HasPrefix(loreDir, defaultDir) {
		t.Errorf("lore.LoreStateDir() = %q, must NOT start with default %q", loreDir, defaultDir)
	}

	wantLore := filepath.Join(tmpDir, "lore", "test-repo")
	if loreDir != wantLore {
		t.Errorf("lore.LoreStateDir() = %q, want %q", loreDir, wantLore)
	}
}

// TestTwoInstancesNoInterference verifies that two schmux instances with
// different config directories create files only in their own directory
// and never write to the other's directory.
func TestTwoInstancesNoInterference(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	defer schmuxdir.Reset()

	// --- Instance A: create config and secrets ---
	schmuxdir.Set(dirA)

	okA, err := config.EnsureExists()
	if err != nil {
		t.Fatalf("instance A: EnsureExists() error: %v", err)
	}
	if !okA {
		t.Fatal("instance A: EnsureExists() returned false")
	}

	secretA, err := config.EnsureSessionSecret()
	if err != nil {
		t.Fatalf("instance A: EnsureSessionSecret() error: %v", err)
	}
	if secretA == "" {
		t.Fatal("instance A: EnsureSessionSecret() returned empty string")
	}

	// --- Instance B: create config and secrets ---
	schmuxdir.Set(dirB)

	okB, err := config.EnsureExists()
	if err != nil {
		t.Fatalf("instance B: EnsureExists() error: %v", err)
	}
	if !okB {
		t.Fatal("instance B: EnsureExists() returned false")
	}

	secretB, err := config.EnsureSessionSecret()
	if err != nil {
		t.Fatalf("instance B: EnsureSessionSecret() error: %v", err)
	}
	if secretB == "" {
		t.Fatal("instance B: EnsureSessionSecret() returned empty string")
	}

	// --- Verify isolation ---

	// Each dir has its own config.json
	if _, err := os.Stat(filepath.Join(dirA, "config.json")); err != nil {
		t.Errorf("instance A: config.json not found in %s", dirA)
	}
	if _, err := os.Stat(filepath.Join(dirB, "config.json")); err != nil {
		t.Errorf("instance B: config.json not found in %s", dirB)
	}

	// Each dir has its own secrets.json
	if _, err := os.Stat(filepath.Join(dirA, "secrets.json")); err != nil {
		t.Errorf("instance A: secrets.json not found in %s", dirA)
	}
	if _, err := os.Stat(filepath.Join(dirB, "secrets.json")); err != nil {
		t.Errorf("instance B: secrets.json not found in %s", dirB)
	}

	// Session secrets are different (generated independently)
	if secretA == secretB {
		t.Errorf("session secrets are identical across instances -- expected independent generation")
	}

	// Verify configs are independently loadable
	schmuxdir.Set(dirA)
	cfgA, err := config.Load(filepath.Join(dirA, "config.json"))
	if err != nil {
		t.Fatalf("instance A: failed to load config: %v", err)
	}

	schmuxdir.Set(dirB)
	cfgB, err := config.Load(filepath.Join(dirB, "config.json"))
	if err != nil {
		t.Fatalf("instance B: failed to load config: %v", err)
	}

	// Both are valid default configs
	if cfgA.GetPort() != 7337 {
		t.Errorf("instance A: unexpected port %d", cfgA.GetPort())
	}
	if cfgB.GetPort() != 7337 {
		t.Errorf("instance B: unexpected port %d", cfgB.GetPort())
	}

	// Modify instance B's config and verify A is untouched
	schmuxdir.Set(dirB)
	cfgB.Network = &config.NetworkConfig{Port: 7338}
	if err := cfgB.Save(); err != nil {
		t.Fatalf("instance B: failed to save modified config: %v", err)
	}

	// Reload A -- should still have default port
	schmuxdir.Set(dirA)
	cfgAReloaded, err := config.Load(filepath.Join(dirA, "config.json"))
	if err != nil {
		t.Fatalf("instance A: failed to reload config: %v", err)
	}
	if cfgAReloaded.GetPort() != 7337 {
		t.Errorf("instance A port changed to %d after modifying instance B -- cross-talk!", cfgAReloaded.GetPort())
	}

	// Reload B -- should have new port
	cfgBReloaded, err := config.Load(filepath.Join(dirB, "config.json"))
	if err != nil {
		t.Fatalf("instance B: failed to reload config: %v", err)
	}
	if cfgBReloaded.GetPort() != 7338 {
		t.Errorf("instance B port = %d, want 7338", cfgBReloaded.GetPort())
	}

	// Verify no files leaked from one dir to the other
	entriesA, _ := os.ReadDir(dirA)
	entriesB, _ := os.ReadDir(dirB)

	// Both should have exactly config.json and secrets.json
	filesA := fileNames(entriesA)
	filesB := fileNames(entriesB)

	for _, f := range filesA {
		if f != "config.json" && f != "secrets.json" {
			t.Errorf("instance A: unexpected file %q", f)
		}
	}
	for _, f := range filesB {
		if f != "config.json" && f != "secrets.json" {
			t.Errorf("instance B: unexpected file %q", f)
		}
	}
}

func fileNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
