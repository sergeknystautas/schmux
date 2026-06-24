package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func TestWrapForFenceDisabledReturnsUnchanged(t *testing.T) {
	got, err := (&Manager{}).wrapForFence(context.Background(), "/ws", "sess", false, "", nil, "echo hi")
	if err != nil {
		t.Fatalf("wrapForFence: %v", err)
	}
	if got != "echo hi" {
		t.Errorf("disabled wrapForFence = %q, want unchanged", got)
	}
}

func TestWrapForFenceMissingCommandErrors(t *testing.T) {
	_, err := (&Manager{}).wrapForFence(context.Background(), "/ws", "sess", true, "", nil, "echo hi")
	if err == nil || !strings.Contains(err.Error(), "fence not available") {
		t.Errorf("err = %v, want 'fence not available'", err)
	}
}

func TestWrapForFenceEnabledWraps(t *testing.T) {
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })
	got, err := (&Manager{}).wrapForFence(context.Background(), t.TempDir(), "sess-xyz", true, "fence", nil, "echo hi")
	if err != nil {
		t.Fatalf("wrapForFence: %v", err)
	}
	if !strings.HasPrefix(got, "fence -m --fence-log-file ") || !strings.Contains(got, "/bin/sh ") {
		t.Errorf("wrapped = %q, want a fence/sh wrapper", got)
	}
}

func TestFenceAllowedDomainsFromModelEndpoint(t *testing.T) {
	model := detect.Model{
		ID: "glm",
		Runners: map[string]detect.RunnerSpec{
			"claude": {Endpoint: "https://api.z.ai/api/anthropic"},
		},
	}
	got := fenceAllowedDomains(ResolvedTarget{ToolName: "claude", Model: &model})
	want := []string{"platform.claude.com", "downloads.claude.ai", "api.z.ai"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("fenceAllowedDomains = %v, want %v", got, want)
	}
}

func TestFenceAllowedDomainsIncludesClaudeDefaultsWithoutEndpoint(t *testing.T) {
	model := detect.Model{
		ID: "claude",
		Runners: map[string]detect.RunnerSpec{
			"claude": {},
		},
	}
	want := []string{"platform.claude.com", "downloads.claude.ai"}
	got := fenceAllowedDomains(ResolvedTarget{ToolName: "claude", Model: &model})
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("fenceAllowedDomains = %v, want %v", got, want)
	}
}

func TestFenceAllowedDomainsNoEndpointForOtherTool(t *testing.T) {
	model := detect.Model{
		ID: "codex",
		Runners: map[string]detect.RunnerSpec{
			"codex": {},
		},
	}
	if got := fenceAllowedDomains(ResolvedTarget{ToolName: "codex", Model: &model}); got != nil {
		t.Fatalf("fenceAllowedDomains = %v, want nil", got)
	}
}

func TestWrapForFenceAppliesRepoPresets(t *testing.T) {
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })

	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".schmux"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"fence":{"presets":["golang","tmux"],"allowed_domains":["mcp.posthog.com"]}}`
	if err := os.WriteFile(filepath.Join(ws, ".schmux", "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := (&Manager{}).wrapForFence(context.Background(), ws, "sess-1", true, "fence", nil, "echo hi"); err != nil {
		t.Fatalf("wrapForFence: %v", err)
	}

	settings, err := os.ReadFile(filepath.Join(schmuxdir.FenceLaunchDir("sess-1"), "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if !strings.Contains(string(settings), "mcp.posthog.com") {
		t.Errorf("settings missing repo domain: %s", settings)
	}
	if !strings.Contains(string(settings), `"allowAllUnixSockets": true`) {
		t.Errorf("settings missing tmux allowAllUnixSockets: %s", settings)
	}
	cmd, err := os.ReadFile(filepath.Join(schmuxdir.FenceLaunchDir("sess-1"), "cmd.sh"))
	if err != nil {
		t.Fatalf("read cmd.sh: %v", err)
	}
	if !strings.Contains(string(cmd), "export GOCACHE=") {
		t.Errorf("cmd.sh missing golang GOCACHE: %s", cmd)
	}
	if strings.Contains(string(cmd), "PIP_CACHE_DIR") {
		t.Errorf("python preset should not be active: %s", cmd)
	}
}
