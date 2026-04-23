package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOllamaEndpoint_RoundTrip(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	schmuxDir := filepath.Join(tmpHome, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(schmuxDir, "config.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.GetOllamaEndpoint(); got != "" {
		t.Errorf("default: got %q, want empty", got)
	}
	if err := c.SetOllamaEndpoint("http://localhost:11434"); err != nil {
		t.Fatal(err)
	}
	if got := c.GetOllamaEndpoint(); got != "http://localhost:11434" {
		t.Errorf("after set: got %q", got)
	}
	c2, _ := Load(configPath)
	if got := c2.GetOllamaEndpoint(); got != "http://localhost:11434" {
		t.Errorf("after reload: got %q", got)
	}
}
