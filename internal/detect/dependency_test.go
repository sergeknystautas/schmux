package detect

import (
	"context"
	"reflect"
	"testing"
)

func TestInstallForOS_FilterAndOrder(t *testing.T) {
	methods := []InstallMethod{
		{OS: "macos", Label: "Homebrew", Command: "brew install x", Requires: "homebrew"},
		{OS: "any", Label: "npm", Command: "npm i -g x", Requires: "npm"},
		{OS: "linux", Label: "apt", Command: "apt install x"},
	}
	// macOS without homebrew: npm (any, npm present) should lead the brew method.
	got := InstallForOS(methods, "darwin", map[string]bool{"npm": true})
	if len(got) != 2 || got[0].Label != "npm" || got[1].Label != "Homebrew" {
		t.Fatalf("macOS order wrong: %+v", got)
	}
	// Linux only sees linux + any.
	gotL := InstallForOS(methods, "linux", nil)
	if len(gotL) != 2 || gotL[0].Label != "apt" && gotL[1].Label != "apt" {
		t.Fatalf("linux filter wrong: %+v", gotL)
	}
}

func TestDeriveInstall(t *testing.T) {
	d := &Descriptor{
		Detect: []DetectEntry{
			{Type: "homebrew_cask", Name: "claude-code"},
			{Type: "npm_global", Package: "@anthropic-ai/claude-code"},
		},
		Install: []InstallMethod{{OS: "any", Label: "Install script", URL: "https://claude.com/download"}},
	}
	got := deriveInstall(d)
	want := []InstallMethod{
		{OS: "macos", Label: "Homebrew", Command: "brew install --cask claude-code", Requires: "homebrew"},
		{OS: "any", Label: "npm", Command: "npm i -g @anthropic-ai/claude-code", Requires: "npm"},
		{OS: "any", Label: "Install script", URL: "https://claude.com/download"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deriveInstall:\n got %+v\nwant %+v", got, want)
	}
}

func TestDetectDependencies_AgentsFromManagerList(t *testing.T) {
	rep := DetectDependencies(context.Background(), []Tool{{Name: "claude", Command: "claude", Source: "PATH"}})
	var sawClaude, sawTmux bool
	for _, s := range rep.Statuses {
		if s.ID == "claude" {
			sawClaude = true
			if !s.Detected {
				t.Errorf("claude should be detected from the agents list")
			}
		}
		if s.ID == "tmux" {
			sawTmux = true // native; detection depends on host, just confirm presence in report
		}
	}
	if !sawClaude || !sawTmux {
		t.Fatalf("report missing entries: claude=%v tmux=%v", sawClaude, sawTmux)
	}
}

func TestAllDependencies_GroupedOrder(t *testing.T) {
	deps := AllDependencies()
	// agents must all precede the first non-agent.
	seenNonAgent := false
	for _, d := range deps {
		if d.Group != "agents" {
			seenNonAgent = true
		} else if seenNonAgent {
			t.Fatalf("agent %q appeared after a non-agent dependency", d.ID)
		}
	}
}

func TestDependencyReportStatus(t *testing.T) {
	rep := DependencyReport{Statuses: []DependencyStatus{
		{Dependency: Dependency{ID: "fence"}, Detected: true, Command: "fence"},
		{Dependency: Dependency{ID: "tmux"}, Detected: false},
	}}
	st, ok := rep.Status("fence")
	if !ok || !st.Detected || st.Command != "fence" {
		t.Errorf("Status(fence) = %+v, %v; want detected fence", st, ok)
	}
	if _, ok := rep.Status("missing"); ok {
		t.Errorf("Status(missing) ok = true, want false")
	}
}
