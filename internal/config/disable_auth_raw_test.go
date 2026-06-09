package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDisableAuthRaw_OnStrictInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// enabled=true with NO tls/public_base_url => strict-invalid => Load() would reject.
	writeFile(t, path, `{
  "access_control": {"enabled": true, "provider": "github"},
  "recycle_workspaces": true
}`)

	if err := DisableAuthRaw(path); err != nil {
		t.Fatalf("DisableAuthRaw: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		AccessControl struct {
			Enabled bool `json:"enabled"`
		} `json:"access_control"`
		RecycleWorkspaces bool `json:"recycle_workspaces"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.AccessControl.Enabled {
		t.Error("expected access_control.enabled=false")
	}
	if !raw.RecycleWorkspaces {
		t.Error("expected unrelated field recycle_workspaces preserved")
	}
}
