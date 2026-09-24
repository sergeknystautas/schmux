package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// TestLoadRepoConfigPastebinPreservesMultiline verifies LoadRepoConfig keeps
// multiline clips, leading indentation, and file order intact.
func TestLoadRepoConfigPastebinPreservesMultiline(t *testing.T) {
	tmpDir := t.TempDir()
	schmuxDir := filepath.Join(tmpDir, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "{\n  \"pastebin\": [\n    \"  line one\\n    line two\",\n    \"second\"\n  ]\n}"
	if err := os.WriteFile(filepath.Join(schmuxDir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	rc, err := LoadRepoConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadRepoConfig: %v", err)
	}
	if rc == nil {
		t.Fatal("LoadRepoConfig returned nil config")
	}
	if len(rc.Pastebin) != 2 {
		t.Fatalf("Pastebin len = %d, want 2", len(rc.Pastebin))
	}
	if rc.Pastebin[0] != "  line one\n    line two" {
		t.Errorf("Pastebin[0] = %q, want preserved multiline with indentation", rc.Pastebin[0])
	}
	if rc.Pastebin[1] != "second" {
		t.Errorf("Pastebin[1] = %q, want %q", rc.Pastebin[1], "second")
	}
}

// TestRefreshWorkspaceConfigCachesPastebinOnly verifies a pastebin-only config
// is cached even when quick_launch is absent or invalid (fence-only / empty
// quick_launch entry).
func TestRefreshWorkspaceConfigCachesPastebinOnly(t *testing.T) {
	t.Run("absent quick_launch", func(t *testing.T) {
		setupPastebinOnly(t, `{"pastebin": ["only clip"]}`)

		cfg := mgr.GetWorkspaceConfig("ws-pastebin")
		if cfg == nil {
			t.Fatal("expected config to be cached for pastebin-only repo")
		}
		if len(cfg.Pastebin) != 1 || cfg.Pastebin[0] != "only clip" {
			t.Errorf("Pastebin = %v, want [only clip]", cfg.Pastebin)
		}
	})

	t.Run("invalid quick_launch", func(t *testing.T) {
		// quick_launch entry missing both command and target is invalid.
		setupPastebinOnly(t, `{"quick_launch":[{"name":"bad"}],"pastebin":["kept"]}`)

		cfg := mgr.GetWorkspaceConfig("ws-pastebin")
		if cfg == nil {
			t.Fatal("expected config to be cached when only pastebin is valid")
		}
		if len(cfg.Pastebin) != 1 || cfg.Pastebin[0] != "kept" {
			t.Errorf("Pastebin = %v, want [kept]", cfg.Pastebin)
		}
	})
}

// TestRefreshWorkspaceConfigPastebinSkipsWhitespaceOnly verifies that
// whitespace-only clips are skipped while valid clips survive in order.
func TestRefreshWorkspaceConfigPastebinSkipsWhitespaceOnly(t *testing.T) {
	setupPastebinOnly(t, `{"pastebin":["   ", "valid", "\t\n", "also valid"]}`)

	cfg := mgr.GetWorkspaceConfig("ws-pastebin")
	if cfg == nil {
		t.Fatal("expected config to be cached")
	}
	want := []string{"valid", "also valid"}
	if len(cfg.Pastebin) != len(want) {
		t.Fatalf("Pastebin = %v, want %v", cfg.Pastebin, want)
	}
	for i, w := range want {
		if cfg.Pastebin[i] != w {
			t.Errorf("Pastebin[%d] = %q, want %q", i, cfg.Pastebin[i], w)
		}
	}
}

// TestGetWorkspaceConfigPastebinReturnsCopy verifies GetWorkspaceConfig
// returns a copied Pastebin slice that the caller may mutate without
// affecting the cache.
func TestGetWorkspaceConfigPastebinReturnsCopy(t *testing.T) {
	setupPastebinOnly(t, `{"pastebin":["first"]}`)

	got := mgr.GetWorkspaceConfig("ws-pastebin")
	if got == nil {
		t.Fatal("expected cached config")
	}
	got.Pastebin[0] = "MUTATED"
	got.Pastebin = append(got.Pastebin, "extra")

	again := mgr.GetWorkspaceConfig("ws-pastebin")
	if again == nil {
		t.Fatal("expected cached config on second call")
	}
	if len(again.Pastebin) != 1 || again.Pastebin[0] != "first" {
		t.Errorf("Pastebin = %v, want [first] (caller mutation leaked)", again.Pastebin)
	}
}

// TestRefreshWorkspaceConfigPastebinEvictsWhenAllInvalid verifies that
// removing or emptying all valid pastebin entries evicts the cache through
// the existing refresh behavior.
func TestRefreshWorkspaceConfigPastebinEvictsWhenAllInvalid(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = tmpDir
	st := state.New(statePath, nil)
	mgr = New(cfg, st, statePath, testLogger())

	ws := state.Workspace{
		ID:     "ws-evict",
		Repo:   "http://example.com/repo",
		Branch: "main",
		Path:   filepath.Join(tmpDir, "ws-evict"),
	}

	schmuxDir := filepath.Join(ws.Path, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath1 := filepath.Join(schmuxDir, "config.json")
	if err := os.WriteFile(configPath1, []byte(`{"pastebin":["first","second"]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr.RefreshWorkspaceConfig(ws)
	if got := mgr.GetWorkspaceConfig(ws.ID); got == nil || len(got.Pastebin) != 2 {
		t.Fatalf("expected 2 cached clips, got %+v", got)
	}

	// Replace contents with only whitespace-only clips — should evict.
	if err := os.WriteFile(configPath1, []byte(`{"pastebin":["   ","\n\t"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr.RefreshWorkspaceConfig(ws)
	if got := mgr.GetWorkspaceConfig(ws.ID); got != nil {
		t.Errorf("expected cache eviction when only invalid clips remain, got %+v", got)
	}

	// Restore valid clips and re-refresh — should re-cache.
	if err := os.WriteFile(configPath1, []byte(`{"pastebin":["restored"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr.RefreshWorkspaceConfig(ws)
	got := mgr.GetWorkspaceConfig(ws.ID)
	if got == nil || len(got.Pastebin) != 1 || got.Pastebin[0] != "restored" {
		t.Errorf("expected restored clip [restored], got %+v", got)
	}
}

// setupPastebinOnly wires a manager that has one workspace with the given
// `.schmux/config.json` body already on disk and refreshed. Tests call it
// before reading mgr.GetWorkspaceConfig.
func setupPastebinOnly(t *testing.T, body string) {
	t.Helper()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = tmpDir
	st := state.New(statePath, nil)
	mgr = New(cfg, st, statePath, testLogger())

	ws := state.Workspace{
		ID:     "ws-pastebin",
		Repo:   "http://example.com/repo",
		Branch: "main",
		Path:   filepath.Join(tmpDir, "ws-pastebin"),
	}
	schmuxDir := filepath.Join(ws.Path, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(schmuxDir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr.RefreshWorkspaceConfig(ws)
}

// mgr is a package-level fixture used by the helper above. Each test that
// calls setupPastebinOnly reassigns it to a fresh manager so tests stay
// independent.
var mgr *Manager
