package session

import (
	"context"
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
	if len(got) != 1 || got[0] != "api.z.ai" {
		t.Fatalf("fenceAllowedDomains = %v, want [api.z.ai]", got)
	}
}

func TestFenceAllowedDomainsNoEndpoint(t *testing.T) {
	model := detect.Model{
		ID: "claude",
		Runners: map[string]detect.RunnerSpec{
			"claude": {},
		},
	}
	if got := fenceAllowedDomains(ResolvedTarget{ToolName: "claude", Model: &model}); got != nil {
		t.Fatalf("fenceAllowedDomains = %v, want nil", got)
	}
}
