package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRepoConfigFenceBlock(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".schmux"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"fence":{"presets":["golang","tmux"],"allowed_domains":["mcp.posthog.com"]}}`
	if err := os.WriteFile(filepath.Join(ws, ".schmux", "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	rc, err := LoadRepoConfig(ws)
	if err != nil {
		t.Fatalf("LoadRepoConfig: %v", err)
	}
	if rc == nil || rc.Fence == nil {
		t.Fatalf("Fence = nil, want parsed block")
	}
	if len(rc.Fence.Presets) != 2 || rc.Fence.Presets[0] != "golang" || rc.Fence.Presets[1] != "tmux" {
		t.Errorf("Presets = %v, want [golang tmux]", rc.Fence.Presets)
	}
	if len(rc.Fence.AllowedDomains) != 1 || rc.Fence.AllowedDomains[0] != "mcp.posthog.com" {
		t.Errorf("AllowedDomains = %v, want [mcp.posthog.com]", rc.Fence.AllowedDomains)
	}
}
