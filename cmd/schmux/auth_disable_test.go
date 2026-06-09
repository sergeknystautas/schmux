package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDisableAuth_WritesConfigAndRestarts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"access_control":{"enabled":true,"provider":"github"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	restarted := false
	if err := disableAuth(path, func() error { restarted = true; return nil }); err != nil {
		t.Fatalf("disableAuth: %v", err)
	}

	data, _ := os.ReadFile(path)
	if want := `"enabled": false`; !strings.Contains(string(data), want) {
		t.Errorf("config not updated; got: %s", data)
	}
	if !restarted {
		t.Error("expected restart callback to be invoked")
	}
}
