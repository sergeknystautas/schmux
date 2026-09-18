package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUIPanelsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"ui":{"panels":{"planUsage":false,"serverLoad":true,"eventMonitor":false}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	panels := cfg.GetUIPanels()
	if panels == nil {
		t.Fatal("expected panels, got nil")
	}
	if !panels["serverLoad"] || panels["eventMonitor"] {
		t.Errorf("panels = %+v", panels)
	}
	if cfg.GetPlanUsagePanelEnabled() {
		t.Error("Plan Usage should be disabled by an explicit panel override")
	}
	// Copy semantics: mutating the returned map must not touch config.
	panels["serverLoad"] = false
	if !cfg.GetUIPanels()["serverLoad"] {
		t.Error("GetUIPanels returned an aliased map")
	}
}

func TestUIPanelsAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"tmux_binary":"tmux"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.GetUIPanels(); got != nil {
		t.Errorf("expected nil panels, got %+v", got)
	}
	if cfg.GetServerLoadPanelEnabled() {
		t.Error("Server Load should be disabled when preferences are absent")
	}
	if cfg.GetPlanUsagePanelEnabled() {
		t.Error("Plan Usage should be disabled when preferences are absent")
	}
}

func TestUIPanelsMissingKeysStayDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	body := `{"ui":{"panels":{"eventMonitor":true}}}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.GetEventMonitorEnabled() {
		t.Error("Event Monitor should be explicitly enabled")
	}
	if cfg.GetPlanUsagePanelEnabled() || cfg.GetServerLoadPanelEnabled() {
		t.Error("missing panel keys should remain disabled")
	}
}
