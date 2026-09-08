package workspace

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/models"
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

// The per-repo validator drops a preset with a bad kind and keeps the
// rest, because a repo config is shared and one bad entry must not
// invalidate the file.
func TestValidateWorkspaceQuickLaunchKind(t *testing.T) {
	prompt := "fix it"
	repoCfg := &contracts.RepoConfig{
		QuickLaunch: []contracts.QuickLaunch{
			{Name: "good chat", Target: "claude", Prompt: &prompt, Kind: "chat"},
			{Name: "bad kind", Target: "claude", Prompt: &prompt, Kind: "terminal"},
			{Name: "chat command", Command: "npm test", Kind: "chat"},
			{Name: "fenced command", Command: "npm test", Fence: true},
		},
	}
	logger := log.NewWithOptions(io.Discard, log.Options{})
	mm := models.New(&config.Config{}, nil, "", logger)
	valid := validateWorkspaceQuickLaunch("test", repoCfg, mm, logger)
	names := make(map[string]bool, len(valid))
	for _, p := range valid {
		names[p.Name] = true
	}
	if !names["good chat"] {
		t.Error("good chat should survive validation")
	}
	if names["bad kind"] {
		t.Error("bad kind should be dropped")
	}
	if names["chat command"] {
		t.Error("chat command should be dropped (chat requires a target)")
	}
	if !names["fenced command"] {
		t.Error("fenced command should survive validation")
	}
}
