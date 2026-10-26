package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientPerformanceDefaults(t *testing.T) {
	cfg := CreateDefault(filepath.Join(t.TempDir(), "config.json"))
	if cfg.GetClientPerformanceEnabled() {
		t.Error("enabled should default to false")
	}
	if cfg.GetClientPerformanceRepo() != "" || cfg.GetClientPerformanceTarget() != "" {
		t.Error("repo and target should default to empty")
	}
}

func TestClientPerformanceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := CreateDefault(path)
	enabled := true
	cfg.ClientPerformance = &ClientPerformanceConfig{Enabled: &enabled, Repo: "schmux", Target: " claude-opus-4-6 "}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.GetClientPerformanceEnabled() {
		t.Error("enabled lost on reload")
	}
	if loaded.GetClientPerformanceRepo() != "schmux" {
		t.Errorf("repo = %q", loaded.GetClientPerformanceRepo())
	}
	if loaded.GetClientPerformanceTarget() != "claude-opus-4-6" {
		t.Errorf("target = %q, want trimmed", loaded.GetClientPerformanceTarget())
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `"client_performance"`) {
		t.Error("client_performance key missing from file")
	}
}

func TestClientPerformanceLegacyTargetMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := CreateDefault(path)
	cfg.ClientPerformance = &ClientPerformanceConfig{Target: "claude-opus"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ClientPerformance.Target != "claude-opus-4-6" {
		t.Errorf("target = %q, want migrated claude-opus-4-6", loaded.ClientPerformance.Target)
	}
}
