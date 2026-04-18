package config

import (
	"encoding/json"
	"testing"
)

func TestSecurityAllowInsecureModesDefaultsFalse(t *testing.T) {
	cfg := CreateDefault(t.TempDir() + "/config.json")
	if cfg.GetAllowInsecureModes() {
		t.Error("default should be false")
	}
}

func TestSecurityAllowInsecureModesParsedFromConfig(t *testing.T) {
	jsonInput := `{"security":{"allow_insecure_modes":true}}`
	var cfg Config
	if err := json.Unmarshal([]byte(jsonInput), &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.GetAllowInsecureModes() {
		t.Error("expected true after parsing")
	}
}
