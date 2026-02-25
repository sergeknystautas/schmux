package config

import (
	"encoding/json"
	"testing"
)

func TestFloorManagerConfigDefaults(t *testing.T) {
	cfg := &Config{}
	if cfg.GetFloorManagerEnabled() != false {
		t.Error("expected floor manager disabled by default")
	}
	if cfg.GetFloorManagerTarget() != "" {
		t.Error("expected empty target by default")
	}
	if cfg.GetFloorManagerRotationThreshold() != 150 {
		t.Errorf("expected default rotation threshold 150, got %d", cfg.GetFloorManagerRotationThreshold())
	}
	if cfg.GetFloorManagerDebounceMs() != 2000 {
		t.Errorf("expected default debounce 2000, got %d", cfg.GetFloorManagerDebounceMs())
	}
}

func TestFloorManagerConfigJSON(t *testing.T) {
	raw := `{"floor_manager":{"enabled":true,"target":"claude-sonnet","rotation_threshold":200,"debounce_ms":3000}}`
	cfg := &Config{}
	if err := json.Unmarshal([]byte(raw), cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.GetFloorManagerEnabled() {
		t.Error("expected floor manager enabled")
	}
	if cfg.GetFloorManagerTarget() != "claude-sonnet" {
		t.Errorf("expected target claude-sonnet, got %s", cfg.GetFloorManagerTarget())
	}
	if cfg.GetFloorManagerRotationThreshold() != 200 {
		t.Errorf("expected rotation threshold 200, got %d", cfg.GetFloorManagerRotationThreshold())
	}
	if cfg.GetFloorManagerDebounceMs() != 3000 {
		t.Errorf("expected debounce 3000, got %d", cfg.GetFloorManagerDebounceMs())
	}
}
